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
	"reflect"
	"time"
)

// ReplayRequest contains identities only. Clients cannot supply forged objects
// or payloads: every replay byte comes from a captured upstream DELETED frame.
type ReplayRequest struct {
	RuleID string   `json:"ruleID"`
	Order  []string `json:"order"`
}
type ReplayReceipt struct {
	RuleID        string    `json:"ruleID"`
	Request       uint64    `json:"request"`
	Order         []string  `json:"order"`
	PayloadSHA256 []string  `json:"payloadSHA256"`
	Completed     time.Time `json:"completed"`
}
type ReplayStreamState struct {
	Request   uint64            `json:"request"`
	RuleID    string            `json:"ruleID"`
	Forwarded map[string]string `json:"forwarded"`
}
type replayResult struct {
	receipt ReplayReceipt
	err     error
}
type replayCommand struct {
	ctx    context.Context
	rule   *RuleStatus
	frames []watchFrame
	result chan replayResult
}
type replayStream struct {
	request   requestContext
	forwarded map[string]map[string]string
	commands  chan replayCommand
	done      chan struct{}
}

func frameHash(frame watchFrame) string {
	hash := sha256.Sum256(frame.raw)
	return hex.EncodeToString(hash[:])
}
func (s *Server) openReplayStream(request requestContext) *replayStream {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.replayStreams == nil {
		s.replayStreams = map[uint64]*replayStream{}
	}
	stream := &replayStream{request: request, forwarded: map[string]map[string]string{}, commands: make(chan replayCommand, 1), done: make(chan struct{})}
	s.replayStreams[request.id] = stream
	return stream
}
func (s *Server) closeReplayStream(id uint64, stream *replayStream) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.replayStreams, id)
	close(stream.done)
}
func (s *Server) captureDeletion(request requestContext, frame watchFrame) error {
	if request.meta.Resource != "pods" || frame.event != "DELETED" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, rule := range s.rules {
		if !s.activeLocked(rule) || rule.Mode != "replay-deletion" || rule.LabelSelector != "" && rule.LabelSelector != request.meta.LabelSelector || !rule.matchesObject(frame.object) {
			continue
		}
		if rule.Captured[frame.object.UID] != "" {
			continue
		}
		if len(frame.raw) > 256<<10 {
			return fmt.Errorf("captured deletion frame exceeds 256 KiB bound")
		}
		if s.replayFrames == nil {
			s.replayFrames = map[string]map[string]watchFrame{}
		}
		if s.replayFrames[rule.ID] == nil {
			s.replayFrames[rule.ID] = map[string]watchFrame{}
		}
		if rule.Captured == nil {
			rule.Captured = map[string]string{}
		}
		copy := frame
		copy.raw = append(json.RawMessage(nil), frame.raw...)
		copy.rule = nil
		s.replayFrames[rule.ID][frame.object.UID] = copy
		hash := frameHash(copy)
		rule.Captured[frame.object.UID] = hash
		rule.CaptureOrder = append(rule.CaptureOrder, frame.object.UID)
		rule.Hits++
		s.recordLocked(Record{Request: request.id, Action: "capture-deletion-event", RuleID: rule.ID, Resource: "pods", Namespace: frame.object.Namespace, Name: frame.object.Name, UID: frame.object.UID, Event: frame.event, PayloadHash: hash})
	}
	return nil
}
func (s *Server) noteReplayOriginalForward(request requestContext, frame watchFrame) {
	if frame.event != "DELETED" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stream := s.replayStreams[request.id]
	if stream == nil {
		return
	}
	hash := frameHash(frame)
	for _, rule := range s.rules {
		if rule.Mode == "replay-deletion" && s.activeLocked(rule) && (rule.LabelSelector == "" || rule.LabelSelector == request.meta.LabelSelector) && rule.Captured[frame.object.UID] == hash {
			if stream.forwarded[rule.ID] == nil {
				stream.forwarded[rule.ID] = map[string]string{}
			}
			stream.forwarded[rule.ID][frame.object.UID] = hash
		}
	}
}

// The sole writer of a watch stream serializes these duplicate frames with
// normal upstream delivery; HTTP handlers never write concurrently to it.
func (s *Server) writeReplay(out io.Writer, request requestContext, command replayCommand) (result error) {
	receipt := ReplayReceipt{RuleID: command.rule.ID, Request: request.id}
	defer func() {
		receipt.Completed = time.Now().UTC()
		command.result <- replayResult{receipt: receipt, err: result}
		if result != nil {
			s.facilityError("requested deletion replay incomplete")
		}
	}()
	for _, frame := range command.frames {
		if err := command.ctx.Err(); err != nil {
			return err
		}
		s.mu.Lock()
		active := s.activeLocked(command.rule)
		s.mu.Unlock()
		if !active {
			return fmt.Errorf("replay rule ended before delivery completed")
		}
		if _, err := out.Write(append(append([]byte(nil), frame.raw...), '\n')); err != nil {
			return err
		}
		s.mu.Lock()
		command.rule.Replayed++
		s.mu.Unlock()
		s.recordFrame(request, frame, "replay-deletion-event", command.rule.ID)
		receipt.Order = append(receipt.Order, frame.object.UID)
		receipt.PayloadSHA256 = append(receipt.PayloadSHA256, frameHash(frame))
	}
	return nil
}

func (s *Server) queueReplay(ctx context.Context, request ReplayRequest) (replayCommand, *replayStream, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || len(s.errors) > 0 {
		return replayCommand{}, nil, fmt.Errorf("proxy has incomplete evidence or is shutting down")
	}
	var rule *RuleStatus
	for _, r := range s.rules {
		if r.ID == request.RuleID {
			rule = r
		}
	}
	if rule == nil || rule.Mode != "replay-deletion" || !s.activeLocked(rule) || rule.ReplayRequested || len(rule.CaptureOrder) != 2 {
		return replayCommand{}, nil, fmt.Errorf("replay requires one active, fully captured, unused rule")
	}
	want := []string{rule.CaptureOrder[1], rule.CaptureOrder[0], rule.CaptureOrder[1], rule.CaptureOrder[0]}
	if !reflect.DeepEqual(request.Order, want) {
		return replayCommand{}, nil, fmt.Errorf("replay must duplicate both captured old UIDs in reverse original order")
	}
	for _, r := range s.rules {
		if s.activeLocked(r) && r.Mode == "hold" && r.Namespace == rule.Namespace {
			return replayCommand{}, nil, fmt.Errorf("namespace must be resumed before replay")
		}
	}
	var selected *replayStream
	for _, stream := range s.replayStreams {
		if stream.request.meta.Resource != "pods" || rule.LabelSelector != "" && rule.LabelSelector != stream.request.meta.LabelSelector || stream.forwarded[rule.ID][want[0]] != rule.Captured[want[0]] || stream.forwarded[rule.ID][want[1]] != rule.Captured[want[1]] {
			continue
		}
		if selected != nil {
			return replayCommand{}, nil, fmt.Errorf("ambiguous controller watch streams for captured old UIDs")
		}
		selected = stream
	}
	if selected == nil {
		return replayCommand{}, nil, fmt.Errorf("no live watch stream has forwarded both original deletion frames")
	}
	command := replayCommand{ctx: ctx, rule: rule, result: make(chan replayResult, 1)}
	for _, uid := range want {
		frame, ok := s.replayFrames[rule.ID][uid]
		if !ok {
			return replayCommand{}, nil, fmt.Errorf("captured payload is no longer available")
		}
		command.frames = append(command.frames, frame)
	}
	select {
	case selected.commands <- command:
		rule.ReplayRequested = true
		s.recordLocked(Record{Request: selected.request.id, Action: "replay-requested", RuleID: rule.ID, Namespace: rule.Namespace, Resource: "pods", Detail: "reverse-original-order twice; exact captured old UIDs"})
	default:
		return replayCommand{}, nil, fmt.Errorf("bounded watch replay queue already occupied")
	}
	return command, selected, nil
}

func (s *Server) controlReplay(w http.ResponseWriter, r *http.Request) {
	var request ReplayRequest
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	decoder.DisallowUnknownFields()
	var extra interface{}
	if decoder.Decode(&request) != nil || decoder.Decode(&extra) != io.EOF {
		writeStatus(w, 400, "invalid replay request")
		return
	}
	command, stream, err := s.queueReplay(r.Context(), request)
	if err != nil {
		writeStatus(w, 409, err.Error())
		return
	}
	select {
	case result := <-command.result:
		if result.err != nil {
			writeStatus(w, 502, "replay delivery incomplete; preserve evidence")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(result.receipt)
	case <-stream.done:
		s.facilityError("watch closed during requested deletion replay")
		writeStatus(w, 502, "watch closed during replay")
	case <-r.Context().Done():
		s.facilityError("replay acknowledgement interrupted; delivery must be reviewed")
	}
}

func (s *Server) forgetReplayLocked(id string) {
	delete(s.replayFrames, id)
	for _, stream := range s.replayStreams {
		delete(stream.forwarded, id)
	}
}
