// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package faultproxy

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
)

type Record struct {
	Sequence         uint64    `json:"sequence"`
	At               time.Time `json:"at"`
	Request          uint64    `json:"request,omitempty"`
	Action           string    `json:"action"`
	RuleID           string    `json:"ruleID,omitempty"`
	Method           string    `json:"method,omitempty"`
	Path             string    `json:"path,omitempty"`
	Resource         string    `json:"resource,omitempty"`
	Namespace        string    `json:"namespace,omitempty"`
	Name             string    `json:"name,omitempty"`
	UID              string    `json:"uid,omitempty"`
	Event            string    `json:"event,omitempty"`
	PayloadHash      string    `json:"payloadSHA256,omitempty"`
	Status           int       `json:"status,omitempty"`
	Detail           string    `json:"detail,omitempty"`
	LabelSelector    string    `json:"labelSelector,omitempty"`
	ObjectGeneration int64     `json:"objectGeneration,omitempty"`
	ListLimit        *int64    `json:"listLimit,omitempty"`
}

type State struct {
	Rules           []RuleStatus        `json:"rules"`
	Errors          []string            `json:"errors"`
	Sequence        uint64              `json:"sequence"`
	InFlightAllowed int                 `json:"inFlightAllowed"`
	ReplayStreams   []ReplayStreamState `json:"replayStreams,omitempty"`
}

type Server struct {
	proxy         *httputil.ReverseProxy
	token         string
	mu            sync.Mutex
	rules         []*RuleStatus
	changed       chan struct{}
	errors        []string
	inFlight      map[uint64]requestMeta
	journal       *json.Encoder
	sequence      uint64
	requests      atomic.Uint64
	work          sync.WaitGroup
	closed        bool
	maxQueue      int
	maxQueueSize  int
	replayFrames  map[string]map[string]watchFrame
	replayStreams map[uint64]*replayStream
}

type contextKey struct{}
type requestContext struct {
	id   uint64
	meta requestMeta
}

func New(upstream *url.URL, transport http.RoundTripper, controlToken string, journal io.Writer) (*Server, error) {
	if upstream == nil || upstream.Host == "" || upstream.User != nil || upstream.RawQuery != "" || upstream.Path != "" && upstream.Path != "/" || (upstream.Scheme != "http" && upstream.Scheme != "https") {
		return nil, fmt.Errorf("upstream must be a fixed HTTP(S) API server origin")
	}
	if len(controlToken) < 32 || journal == nil {
		return nil, fmt.Errorf("control token (at least32 bytes) and journal are required")
	}
	s := &Server{token: controlToken, changed: make(chan struct{}), journal: json.NewEncoder(journal), inFlight: map[uint64]requestMeta{}, maxQueue: 4096, maxQueueSize: 16 << 20}
	s.proxy = httputil.NewSingleHostReverseProxy(upstream)
	s.proxy.Transport = transport
	s.proxy.FlushInterval = -1
	s.proxy.ModifyResponse = s.response
	s.proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		if r.Context().Err() != nil {
			return
		}
		x := r.Context().Value(contextKey{}).(requestContext)
		s.record(Record{Request: x.id, Action: "upstream-error", Method: r.Method, Path: r.URL.Path, Status: 502})
		writeStatus(w, 502, "external API proxy upstream failed")
	}
	return s, nil
}

func (s *Server) record(r Record) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordLocked(r)
}
func (s *Server) recordLocked(r Record) {
	s.sequence++
	r.Sequence, r.At = s.sequence, time.Now().UTC()
	if err := s.journal.Encode(r); err != nil && len(s.errors) < 100 {
		s.errors = append(s.errors, "journal write failed")
	}
}
func (s *Server) changedLocked() {
	close(s.changed)
	s.changed = make(chan struct{})
}
func (s *Server) activeLocked(r *RuleStatus) bool {
	if r.Active && time.Now().After(r.Expires) {
		r.Active, r.EndReason = false, "expired"
		s.forgetReplayLocked(r.ID)
		s.recordLocked(Record{Action: "rule-expired", RuleID: r.ID})
		s.changedLocked()
	}
	return r.Active
}
func (s *Server) consumeLocked(r *RuleStatus) {
	r.Hits++
	if r.Remaining > 0 {
		r.Remaining--
		if r.Remaining == 0 {
			r.Active, r.EndReason = false, "count-exhausted"
			s.changedLocked()
		}
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.work.Add(1)
	defer s.work.Done()
	if r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		return
	}
	x := requestContext{id: s.requests.Add(1), meta: metadata(r)}
	r = r.WithContext(context.WithValue(r.Context(), contextKey{}, x))
	s.record(Record{Request: x.id, Action: "request", Method: r.Method, Path: r.URL.Path, Namespace: x.meta.Namespace, Resource: x.meta.Resource, Name: x.meta.Name, LabelSelector: x.meta.LabelSelector, ListLimit: x.meta.ListLimit, Detail: fmt.Sprintf("watch=%t initialEvents=%t", x.meta.Watch, x.meta.InitialEvents)})
	// Read an object only for explicitly selected HTTP fault identity checks.
	var body []byte
	needIdentity := false
	s.mu.Lock()
	for _, rule := range s.rules {
		if s.activeLocked(rule) && rule.matchesRequest(x.meta) && rule.needsObject(x.meta) {
			needIdentity = true
		}
	}
	s.mu.Unlock()
	if needIdentity && r.Body != nil {
		var err error
		body, err = io.ReadAll(io.LimitReader(r.Body, (8<<20)+1))
		r.Body.Close()
		if err != nil || len(body) > 8<<20 {
			writeStatus(w, 413, "selected fault request body exceeds bound")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
	}
	var object objectMeta
	if needIdentity {
		var err error
		object, err = requestObject(body, r.Header.Get("Content-Type"))
		if err != nil {
			s.facilityError("selected request object could not be decoded")
			writeStatus(w, 502, "selected fault request encoding not supported")
			return
		}
		if object.Namespace == "" {
			object.Namespace = x.meta.Namespace
		}
		if object.Name == "" {
			object.Name = x.meta.Name
		}
	}
	for {
		s.mu.Lock()
		var selected *RuleStatus
		for _, rule := range s.rules {
			if s.activeLocked(rule) && rule.matchesRequest(x.meta) && (!rule.needsObject(x.meta) || rule.matchesObject(object)) {
				selected = rule
				break
			}
		}
		if selected == nil {
			if !x.meta.Watch {
				s.inFlight[x.id] = x.meta
			}
			s.mu.Unlock()
			break
		}
		s.consumeLocked(selected)
		mode, status := selected.Mode, selected.StatusCode
		detail := "namespaced-request"
		if selected.Namespace != "" && x.meta.Namespace == "" {
			detail = "global-list-watch-reconnect-guard"
		}
		s.recordLocked(Record{Request: x.id, Action: mode + "-request", RuleID: selected.ID, Method: r.Method, Path: r.URL.Path, Namespace: x.meta.Namespace, Resource: x.meta.Resource, Name: object.Name, UID: object.UID, ObjectGeneration: object.Generation, LabelSelector: x.meta.LabelSelector, ListLimit: x.meta.ListLimit, Status: status, Detail: detail})
		s.mu.Unlock()
		if mode == "error" {
			writeStatus(w, status, "injected external controller API failure")
			return
		}
		if !s.waitRule(r.Context(), selected) {
			return
		}
		s.mu.Lock()
		selected.Released++
		s.recordLocked(Record{Request: x.id, Action: "release-request", RuleID: selected.ID, Path: r.URL.Path})
		s.mu.Unlock()
	}
	defer func() {
		s.mu.Lock()
		delete(s.inFlight, x.id)
		s.mu.Unlock()
	}()
	if x.meta.Watch {
		// Native clients understand JSON responses. Negotiate JSON explicitly so
		// the proxy never treats protobuf watch frames as text. This negotiation
		// is recorded and tested against a real typed Kubernetes client.
		r.Header = r.Header.Clone()
		r.Header.Set("Accept", "application/json")
		r.Header.Set("Accept-Encoding", "identity")
		s.record(Record{Request: x.id, Action: "watch-json-negotiation", Resource: x.meta.Resource})
	}
	s.proxy.ServeHTTP(w, r)
}

func requestObject(body []byte, contentType string) (objectMeta, error) {
	var envelope struct {
		Metadata      objectMeta           `json:"metadata"`
		Preconditions struct{ UID string } `json:"preconditions"`
	}
	if len(body) == 0 {
		return envelope.Metadata, nil
	}
	if strings.Contains(contentType, "protobuf") {
		object, _, err := scheme.Codecs.UniversalDeserializer().Decode(body, nil, nil)
		if err != nil {
			return objectMeta{}, err
		}
		body, err = json.Marshal(object)
		if err != nil {
			return objectMeta{}, err
		}
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return objectMeta{}, err
	}
	if envelope.Metadata.UID == "" {
		envelope.Metadata.UID = envelope.Preconditions.UID
	}
	return envelope.Metadata, nil
}

func (s *Server) waitRule(ctx context.Context, rule *RuleStatus) bool {
	for {
		s.mu.Lock()
		active, changed := s.activeLocked(rule), s.changed
		remaining := time.Until(rule.Expires)
		s.mu.Unlock()
		if !active {
			return true
		}
		timer := time.NewTimer(max(remaining, time.Millisecond))
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-changed:
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (s *Server) facilityError(message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.errors) < 100 {
		s.errors = append(s.errors, message)
	}
	s.recordLocked(Record{Action: "facility-error", Detail: message})
}

func writeStatus(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	reason := metav1.StatusReasonInternalError
	switch code {
	case 404:
		reason = metav1.StatusReasonNotFound
	case 409:
		reason = metav1.StatusReasonConflict
	case 429:
		reason = metav1.StatusReasonTooManyRequests
	case 503:
		reason = metav1.StatusReasonServiceUnavailable
	}
	_ = json.NewEncoder(w).Encode(metav1.Status{TypeMeta: metav1.TypeMeta{APIVersion: "v1", Kind: "Status"}, Status: "Failure", Code: int32(code), Reason: reason, Message: message})
}

func (s *Server) ControlHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.work.Add(1)
		defer s.work.Done()
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.token)) != 1 {
			writeStatus(w, http.StatusUnauthorized, "control authentication required")
			return
		}
		if r.Method == "GET" && r.URL.Path == "/v1/state" {
			s.mu.Lock()
			state := State{Errors: append([]string{}, s.errors...), Sequence: s.sequence, InFlightAllowed: len(s.inFlight)}
			for _, rule := range s.rules {
				s.activeLocked(rule)
				copy := *rule
				copy.Captured = map[string]string{}
				for uid, hash := range rule.Captured {
					copy.Captured[uid] = hash
				}
				copy.CaptureOrder = append([]string(nil), rule.CaptureOrder...)
				state.Rules = append(state.Rules, copy)
			}
			for id, stream := range s.replayStreams {
				for rule, forwarded := range stream.forwarded {
					copy := ReplayStreamState{Request: id, RuleID: rule, Forwarded: map[string]string{}}
					for uid, hash := range forwarded {
						copy.Forwarded[uid] = hash
					}
					state.ReplayStreams = append(state.ReplayStreams, copy)
				}
			}
			s.mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(state)
			return
		}
		if r.Method == "POST" && r.URL.Path == "/v1/replay" {
			s.controlReplay(w, r)
			return
		}
		if r.Method == "POST" && r.URL.Path == "/v1/rules" {
			var rule Rule
			decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&rule); err != nil {
				writeStatus(w, 400, "invalid rule JSON")
				return
			}
			var extra interface{}
			if decoder.Decode(&extra) != io.EOF {
				writeStatus(w, 400, "exactly one rule object is required")
				return
			}
			if err := rule.validate(); err != nil {
				writeStatus(w, 400, err.Error())
				return
			}
			s.mu.Lock()
			defer s.mu.Unlock()
			if s.closed {
				writeStatus(w, 503, "proxy is shutting down")
				return
			}
			for _, old := range s.rules {
				if old.ID == rule.ID {
					writeStatus(w, 409, "rule ID already used; preserve its original counters")
					return
				}
			}
			now := time.Now().UTC()
			state := &RuleStatus{Rule: rule, Active: true, Installed: now, Expires: now.Add(time.Duration(rule.DurationSeconds) * time.Second), Remaining: rule.Count}
			s.rules = append(s.rules, state)
			s.recordLocked(Record{Action: "rule-installed", RuleID: rule.ID, Namespace: rule.Namespace, Resource: rule.Resource, Name: rule.Name, Detail: rule.Mode})
			s.changedLocked()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(201)
			_ = json.NewEncoder(w).Encode(state)
			return
		}
		if r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/v1/rules/") {
			id := strings.TrimPrefix(r.URL.Path, "/v1/rules/")
			s.mu.Lock()
			defer s.mu.Unlock()
			for _, rule := range s.rules {
				if rule.ID == id {
					rule.Active, rule.EndReason = false, "cleared"
					s.forgetReplayLocked(id)
					s.recordLocked(Record{Action: "rule-cleared", RuleID: id})
					s.changedLocked()
					w.WriteHeader(204)
					return
				}
			}
			writeStatus(w, 404, "unknown rule")
			return
		}
		writeStatus(w, 404, "unknown control endpoint")
	})
}

func (s *Server) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	for _, rule := range s.rules {
		if rule.Active {
			rule.Active, rule.EndReason = false, "proxy-shutdown"
		}
	}
	s.changedLocked()
	// Keep deterministic shutdown evidence without exposing any credentials.
	var ids []string
	for _, rule := range s.rules {
		ids = append(ids, rule.ID)
	}
	sort.Strings(ids)
	s.recordLocked(Record{Action: "proxy-shutdown", Detail: strings.Join(ids, ",")})
}

// Wait is called after both HTTP listeners/connections have been closed.
func (s *Server) Wait() { s.work.Wait() }
