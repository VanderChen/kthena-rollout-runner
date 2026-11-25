// Copyright 2026 The Kthena Rollout Runner Authors.
// SPDX-License-Identifier: Apache-2.0

package faultproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const servingGroupResource = "servinggroups"

// learnGroup records who owns one ServingGroup. UIDs are never reused, so the
// entry outlives the object and a recreated same-name group is a new entry.
func (s *Server) learnGroup(group objectMeta) {
	if group.UID == "" {
		return
	}
	owners := make([]string, 0, len(group.OwnerReferences))
	for _, owner := range group.OwnerReferences {
		owners = append(owners, owner.UID)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.groups == nil {
		s.groups = map[string][]string{}
	}
	s.groups[group.UID] = owners
}

// groupOwnersLocked resolves a ServingGroup owner for rule matching. A group
// the controller never read or created through this proxy cannot be resolved:
// the rule would silently not match, so the gap is latched as an evidence
// error instead (a restarted proxy is the expected cause).
func (s *Server) groupOwnersLocked(group string) ([]string, bool) {
	owners, known := s.groups[group]
	if !known && !s.unknownGroups[group] {
		if s.unknownGroups == nil {
			s.unknownGroups = map[string]bool{}
		}
		s.unknownGroups[group] = true
		message := "owner of ServingGroup " + group + " was never seen; owner-selected faults are incomplete"
		if len(s.errors) < 100 {
			s.errors = append(s.errors, message)
		}
		s.recordLocked(Record{Action: "facility-error", UID: group, Detail: message})
	}
	return owners, known
}

// learnGroupResponse reads the ServingGroups of a successful read or create
// response and hands the unchanged bytes on to the controller.
func (s *Server) learnGroupResponse(response *http.Response) error {
	fail := func(err error) error {
		s.facilityError("ServingGroup response could not be read for owner resolution: " + err.Error())
		return err
	}
	if !strings.Contains(response.Header.Get("Content-Type"), "application/json") || response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		return fail(fmt.Errorf("ServingGroup response must use uncompressed JSON"))
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
	response.Body.Close()
	if err != nil {
		return fail(err)
	}
	if len(body) > 8<<20 {
		return fail(fmt.Errorf("ServingGroup response exceeds bound"))
	}
	response.Body = io.NopCloser(bytes.NewReader(body))
	var envelope struct {
		Metadata objectMeta `json:"metadata"`
		Items    []struct {
			Metadata objectMeta `json:"metadata"`
		} `json:"items"`
	}
	if err = json.Unmarshal(body, &envelope); err != nil {
		return fail(err)
	}
	s.learnGroup(envelope.Metadata)
	for _, item := range envelope.Items {
		s.learnGroup(item.Metadata)
	}
	return nil
}
