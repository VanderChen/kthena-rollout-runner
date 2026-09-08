// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package faultproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

type watchFrame struct {
	raw    json.RawMessage
	event  string
	object objectMeta
	rule   *RuleStatus
}

type watchBody struct {
	*io.PipeReader
	upstream io.ReadCloser
	cancel   context.CancelFunc
	once     sync.Once
}

func (b *watchBody) Close() error {
	b.once.Do(func() {
		b.cancel()
		_ = b.upstream.Close()
		_ = b.PipeReader.Close()
	})
	return nil
}

func (s *Server) response(response *http.Response) error {
	x := response.Request.Context().Value(contextKey{}).(requestContext)
	s.record(Record{Request: x.id, Action: "response", Method: x.meta.Method, Path: x.meta.Path, Status: response.StatusCode})
	if !x.meta.Watch || response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil
	}
	if !strings.Contains(response.Header.Get("Content-Type"), "application/json") || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		s.facilityError("upstream ignored explicit JSON watch negotiation")
		return fmt.Errorf("watch must use uncompressed JSON")
	}
	upstream := response.Body
	reader, writer := io.Pipe()
	ctx, cancel := context.WithCancel(response.Request.Context())
	response.Body = &watchBody{PipeReader: reader, upstream: upstream, cancel: cancel}
	response.ContentLength = -1
	response.Header.Del("Content-Length")
	s.work.Add(1)
	go func() { defer s.work.Done(); s.watch(ctx, cancel, upstream, writer, x) }()
	return nil
}

func (s *Server) watch(ctx context.Context, cancel context.CancelFunc, upstream io.ReadCloser, out *io.PipeWriter, request requestContext) {
	defer cancel()
	defer upstream.Close()
	defer out.Close()
	frames := make(chan watchFrame, 16)
	errors := make(chan error, 1)
	go func() {
		defer close(frames)
		decoder := json.NewDecoder(upstream)
		for {
			var raw json.RawMessage
			if err := decoder.Decode(&raw); err != nil {
				errors <- err
				return
			}
			var envelope struct {
				Type   string `json:"type"`
				Object struct {
					Metadata objectMeta `json:"metadata"`
					Kind     string     `json:"kind"`
				} `json:"object"`
			}
			if err := json.Unmarshal(raw, &envelope); err != nil || envelope.Type == "" {
				errors <- fmt.Errorf("malformed JSON watch frame")
				return
			}
			select {
			case frames <- watchFrame{raw: raw, event: envelope.Type, object: envelope.Object.Metadata}:
			case <-ctx.Done():
				return
			}
		}
	}()
	var pending []watchFrame
	defer func() {
		if len(pending) > 0 {
			s.facilityError("watch closed with undelivered held frames; evidence incomplete")
		}
	}()
	bytesPending := 0
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for frames != nil || len(pending) > 0 {
		s.mu.Lock()
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case frame, ok := <-frames:
			if !ok {
				frames = nil
				select {
				case err := <-errors:
					if err != io.EOF && ctx.Err() == nil {
						s.facilityError("watch decoding failed")
						_ = out.CloseWithError(err)
						return
					}
				default:
				}
				continue
			}
			held, err := s.deliverOrHold(out, request, &frame)
			if err != nil {
				return
			}
			if held {
				pending = append(pending, frame)
				bytesPending += len(frame.raw)
				if len(pending) > s.maxQueue || bytesPending > s.maxQueueSize {
					s.facilityError("bounded paused watch queue overflow; evidence incomplete")
					_ = out.CloseWithError(fmt.Errorf("fault watch queue overflow"))
					return
				}
			}
		case <-changed:
		case <-tick.C:
		}
		kept := pending[:0]
		for _, frame := range pending {
			s.mu.Lock()
			active := s.activeLocked(frame.rule)
			s.mu.Unlock()
			if active {
				kept = append(kept, frame)
				continue
			}
			originalRule := frame.rule
			held, err := s.deliverOrHold(out, request, &frame)
			if err != nil {
				return
			}
			if held {
				kept = append(kept, frame)
				continue
			}
			bytesPending -= len(frame.raw)
			s.mu.Lock()
			originalRule.Released++
			s.mu.Unlock()
			s.recordFrame(request, frame, "release-event", originalRule.ID)
		}
		pending = kept
	}
}

func (s *Server) deliverOrHold(out io.Writer, request requestContext, frame *watchFrame) (bool, error) {
	s.mu.Lock()
	var selected *RuleStatus
	for _, rule := range s.rules {
		if !s.activeLocked(rule) || rule.InitialSync || rule.Resource != request.meta.Resource || !rule.matchesObject(frame.object) {
			continue
		}
		if rule.Mode == "hold" || rule.Mode == "drop-deletion" && (frame.event == "DELETED" || frame.object.DeletionTimestamp != nil) {
			selected = rule
			break
		}
	}
	if selected != nil {
		s.consumeLocked(selected)
		frame.rule = selected
	}
	s.mu.Unlock()
	if selected != nil {
		s.recordFrame(request, *frame, selected.Mode+"-event", selected.ID)
		return selected.Mode == "hold", nil
	}
	_, err := out.Write(append(append([]byte{}, frame.raw...), '\n'))
	if err == nil {
		s.recordFrame(request, *frame, "forward-event", "")
	}
	return false, err
}

func (s *Server) recordFrame(request requestContext, frame watchFrame, action, rule string) {
	hash := sha256.Sum256(frame.raw)
	s.record(Record{Request: request.id, Action: action, RuleID: rule, Resource: request.meta.Resource, Namespace: frame.object.Namespace, Name: frame.object.Name, UID: frame.object.UID, Event: frame.event, PayloadHash: hex.EncodeToString(hash[:])})
}
