// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

// Package faultproxy provides an external, bounded fault injector. Kthena uses
// this proxy for controller API traffic; runner observations and admission use
// the actual API server independently.
package faultproxy

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Rule struct {
	ID              string        `json:"id"`
	Namespace       string        `json:"namespace,omitempty"`
	Resource        string        `json:"resource"`
	Name            string        `json:"name,omitempty"`
	Subresource     string        `json:"subresource,omitempty"`
	Methods         []string      `json:"methods,omitempty"`
	UIDs            []string      `json:"uids,omitempty"`
	OwnerUID        string        `json:"ownerUID,omitempty"`
	Mode            string        `json:"mode"` // error, hold, drop-deletion, replay-deletion
	Count           int           `json:"count"`
	StatusCode      int           `json:"statusCode,omitempty"`
	DurationSeconds int           `json:"durationSeconds"`
	InitialSync     bool          `json:"initialSync,omitempty"`
	CollectionOnly  bool          `json:"collectionOnly,omitempty"`
	Generation      int64         `json:"generation,omitempty"`
	LabelSelector   string        `json:"labelSelector,omitempty"`
	ListLimit       *int64        `json:"listLimit,omitempty"`
	OmitObject      *ListOmission `json:"omitObject,omitempty"`
}

// ListOmission describes one real object hidden from a controller-only List.
// It never changes the stored object, direct runner observations, or Watch.
type ListOmission struct {
	Name     string `json:"name"`
	UID      string `json:"uid"`
	OwnerUID string `json:"ownerUID"`
}

type RuleStatus struct {
	Rule
	Active          bool              `json:"active"`
	Installed       time.Time         `json:"installed"`
	Expires         time.Time         `json:"expires"`
	Hits            int               `json:"hits"`
	Released        int               `json:"released"`
	Remaining       int               `json:"remaining"`
	EndReason       string            `json:"endReason,omitempty"`
	Captured        map[string]string `json:"captured,omitempty"`
	CaptureOrder    []string          `json:"captureOrder,omitempty"`
	ReplayRequested bool              `json:"replayRequested,omitempty"`
	Replayed        int               `json:"replayed,omitempty"`
}

type requestMeta struct {
	Method, Path, Namespace, Resource, Name, Subresource string
	Watch, InitialEvents                                 bool
	LabelSelector                                        string
	ListLimit                                            *int64
}

func metadata(r *http.Request) requestMeta {
	m := requestMeta{Method: r.Method, Path: r.URL.Path, Watch: r.URL.Query().Get("watch") == "true" || r.URL.Query().Get("watch") == "1", InitialEvents: r.URL.Query().Get("sendInitialEvents") == "true"}
	m.LabelSelector = r.URL.Query().Get("labelSelector")
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	start := 0
	if len(parts) >= 3 && parts[0] == "api" {
		start = 2
	} else if len(parts) >= 4 && parts[0] == "apis" {
		start = 3
	} else {
		return m
	}
	if len(parts) > start+2 && parts[start] == "namespaces" {
		m.Namespace = parts[start+1]
		start += 2
	}
	if len(parts) > start {
		m.Resource = parts[start]
	}
	if len(parts) > start+1 {
		m.Name = parts[start+1]
	}
	if len(parts) > start+2 {
		m.Subresource = parts[start+2]
	}
	if m.Method == http.MethodGet && m.Name == "" && !m.Watch {
		limit := r.URL.Query().Get("limit")
		if limit == "" {
			limit = "0"
		}
		if value, err := strconv.ParseInt(limit, 10, 64); err == nil && value >= 0 {
			m.ListLimit = &value
		}
	}
	return m
}

func (r Rule) validate() error {
	if r.ID == "" || len(r.ID) > 80 || r.Resource == "" || strings.ContainsAny(r.Resource, "/ *") {
		return fmt.Errorf("explicit rule ID and single resource are required")
	}
	if r.DurationSeconds < 1 || r.DurationSeconds > 900 || r.Count == 0 || r.Count < -1 {
		return fmt.Errorf("rule requires bounded duration 1..900 seconds and count > 0 or -1")
	}
	if r.Namespace == "" && !r.InitialSync {
		return fmt.Errorf("namespace is required except for explicit global initial-sync faults")
	}
	if r.InitialSync && (r.Namespace != "" || r.Name != "" || r.Subresource != "" || len(r.UIDs) != 0 || r.OwnerUID != "" || len(r.Methods) != 1 || r.Methods[0] != http.MethodGet) {
		return fmt.Errorf("initial-sync fault must explicitly target global GET startup requests")
	}
	if r.CollectionOnly && (r.InitialSync || r.Name != "" || r.Subresource != "" || len(r.UIDs) != 0 || r.OwnerUID != "" || r.Generation != 0 || (r.Mode != "error" && r.Mode != "omit-list-object") || len(r.Methods) != 1 || (r.Methods[0] != http.MethodGet && r.Methods[0] != http.MethodDelete)) {
		return fmt.Errorf("collection-only errors require a namespaced List/DeleteCollection without object or initial-sync filters")
	}
	if r.Generation < 0 || r.Generation > 0 && (r.Mode != "error" || r.InitialSync || r.CollectionOnly || len(r.Methods) != 1 || r.Methods[0] != http.MethodPut || r.Name == "" || r.OwnerUID == "") {
		return fmt.Errorf("generation filter requires an exact owned object PUT")
	}
	if (r.LabelSelector != "" || r.ListLimit != nil) && !(r.Mode == "replay-deletion" && r.ListLimit == nil && !r.CollectionOnly) && (!r.CollectionOnly || len(r.Methods) != 1 || r.Methods[0] != http.MethodGet || r.ListLimit != nil && *r.ListLimit < 0) {
		return fmt.Errorf("selector filters require an exact replay Watch or collection GET; limits require nonnegative collection GET")
	}
	switch r.Mode {
	case "omit-list-object":
		if !r.CollectionOnly || r.Resource != "controllerrevisions" || r.Methods[0] != http.MethodGet || r.LabelSelector == "" || r.ListLimit == nil || *r.ListLimit != 0 || r.Count != -1 || r.StatusCode != 0 || r.OmitObject == nil || r.OmitObject.Name == "" || r.OmitObject.UID == "" || r.OmitObject.OwnerUID == "" {
			return fmt.Errorf("List omission requires an exact owned history identity and bounded unpaged selected GET")
		}
	case "error":
		if len(r.Methods) == 0 || r.StatusCode < 400 || r.StatusCode > 599 {
			return fmt.Errorf("error rule requires methods and HTTP error status")
		}
	case "hold":
		if r.Count != -1 || r.StatusCode != 0 || len(r.UIDs) != 0 || r.OwnerUID != "" {
			return fmt.Errorf("hold requires a namespace/resource barrier, count -1 and no object filter")
		}
	case "drop-deletion":
		if len(r.UIDs) == 0 || r.InitialSync || r.StatusCode != 0 || len(r.Methods) != 0 {
			return fmt.Errorf("drop-deletion requires finite old UIDs and a namespace")
		}
	case "replay-deletion":
		if r.Resource != "pods" || r.OwnerUID == "" || len(r.UIDs) != 2 || r.UIDs[0] == "" || r.UIDs[1] == "" || r.UIDs[0] == r.UIDs[1] || r.Count != -1 || r.InitialSync || r.Subresource != "" || r.Name != "" || r.StatusCode != 0 || len(r.Methods) != 0 {
			return fmt.Errorf("replay-deletion requires exactly two distinct owned old Pod UIDs and bounded capture lifetime")
		}
	default:
		return fmt.Errorf("unknown fault mode %q", r.Mode)
	}
	if r.OmitObject != nil && r.Mode != "omit-list-object" {
		return fmt.Errorf("omitted object is only valid for List omission")
	}
	for _, method := range r.Methods {
		if method != "GET" && method != "POST" && method != "PUT" && method != "PATCH" && method != "DELETE" {
			return fmt.Errorf("unsupported method %q", method)
		}
	}
	return nil
}

func has(values []string, value string) bool {
	for _, x := range values {
		if value == x {
			return true
		}
	}
	return false
}

func (r Rule) matchesRequest(m requestMeta) bool {
	if r.LabelSelector != "" && r.LabelSelector != m.LabelSelector || r.ListLimit != nil && (m.ListLimit == nil || *r.ListLimit != *m.ListLimit) {
		return false
	}
	if r.CollectionOnly && (m.Name != "" || m.Watch) {
		return false
	}
	if r.Mode == "drop-deletion" || r.Mode == "replay-deletion" || r.Resource != m.Resource || r.Subresource != m.Subresource || r.Name != "" && r.Name != m.Name && !(m.Method == "POST" && m.Name == "") || len(r.Methods) > 0 && !has(r.Methods, m.Method) {
		return false
	}
	if r.InitialSync {
		return m.Namespace == "" && m.Name == "" && (!m.Watch || m.InitialEvents)
	}
	if m.Namespace == r.Namespace {
		return true
	}
	// A new cluster-wide list/watch could expose paused namespace state after
	// reconnect. Hold that startup request too, and label the global guard in
	// the trace. Existing streams still forward unrelated namespace events.
	return r.Mode == "hold" && m.Method == "GET" && m.Namespace == "" && m.Name == ""
}

func (r Rule) needsObject(m requestMeta) bool {
	return len(r.UIDs) > 0 || r.OwnerUID != "" || r.Generation > 0 || r.Name != "" && m.Name == ""
}

type objectMeta struct {
	Name, Namespace, UID string
	Generation           int64
	OwnerReferences      []struct{ UID string }
	DeletionTimestamp    *string
}

func (r Rule) matchesObject(m objectMeta) bool {
	if r.Generation > 0 && r.Generation != m.Generation {
		return false
	}
	if r.Namespace != "" && r.Namespace != m.Namespace || r.Name != "" && r.Name != m.Name || len(r.UIDs) > 0 && !has(r.UIDs, m.UID) {
		return false
	}
	if r.OwnerUID != "" && r.OwnerUID != m.UID {
		for _, owner := range m.OwnerReferences {
			if owner.UID == r.OwnerUID {
				return true
			}
		}
		return false
	}
	return true
}
