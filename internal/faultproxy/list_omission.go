// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package faultproxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net/http"
	"strings"
)

func (s *Server) omitListObject(response *http.Response, x requestContext) error {
	s.mu.Lock()
	var selected *RuleStatus
	for _, rule := range s.rules {
		if has(x.listOmissionRules, rule.ID) && rule.Mode == "omit-list-object" && s.activeLocked(rule) && rule.matchesRequest(x.meta) {
			selected = rule
			break
		}
	}
	s.mu.Unlock()
	if selected == nil {
		return nil
	}
	fail := func(err error) error {
		s.facilityError("selected List omission could not be proven: " + err.Error())
		return err
	}
	if !strings.Contains(response.Header.Get("Content-Type"), "application/json") || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		return fail(fmt.Errorf("List must use uncompressed JSON"))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	response.Body.Close()
	if err != nil {
		return fail(err)
	}
	if len(body) > 8<<20 {
		return fail(fmt.Errorf("List exceeds bound"))
	}
	var list map[string]json.RawMessage
	if err = json.Unmarshal(body, &list); err != nil {
		return fail(err)
	}
	var items []json.RawMessage
	if err = json.Unmarshal(list["items"], &items); err != nil {
		return fail(err)
	}
	kept := make([]json.RawMessage, 0, len(items))
	omitted := 0
	for _, item := range items {
		var entry struct {
			Metadata metav1.ObjectMeta `json:"metadata"`
		}
		if err = json.Unmarshal(item, &entry); err != nil {
			return fail(err)
		}
		o := entry.Metadata
		match := string(o.UID) == selected.OmitObject.UID && o.Name == selected.OmitObject.Name && o.Namespace == selected.Namespace
		owned := false
		for _, owner := range o.OwnerReferences {
			if string(owner.UID) == selected.OmitObject.OwnerUID && owner.Controller != nil && *owner.Controller {
				owned = true
			}
		}
		if match && owned {
			omitted++
		} else {
			kept = append(kept, item)
		}
	}
	if omitted > 1 {
		return fail(fmt.Errorf("duplicate selected UID in List"))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if omitted == 1 && s.activeLocked(selected) {
		list["items"], err = json.Marshal(kept)
		if err != nil {
			return err
		}
		modified, marshalErr := json.Marshal(list)
		if marshalErr != nil {
			return marshalErr
		}
		s.consumeLocked(selected)
		s.recordLocked(Record{Request: x.id, Action: "omit-list-object-response", RuleID: selected.ID, Method: x.meta.Method, Path: x.meta.Path, Namespace: x.meta.Namespace, Resource: x.meta.Resource, Name: selected.OmitObject.Name, UID: selected.OmitObject.UID, Status: response.StatusCode, PayloadHash: fmt.Sprintf("%x", sha256.Sum256(body)), Detail: fmt.Sprintf("items=%d->%d deliveredSHA256=%x", len(items), len(kept), sha256.Sum256(modified))})
		body = modified
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	response.ContentLength = int64(len(body))
	response.Header.Set("Content-Length", fmt.Sprint(len(body)))
	return nil
}
