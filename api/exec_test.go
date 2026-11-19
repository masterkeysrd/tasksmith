package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestWriteNDJSONRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := ExecInputFrame{
		Type:    ExecInputSend,
		Content: "hello",
	}
	if err := WriteNDJSON(&buf, in); err != nil {
		t.Fatalf("WriteNDJSON: %v", err)
	}
	if !strings.HasSuffix(buf.String(), "}\n") {
		t.Fatalf("frame must end with a newline, got %q", buf.String())
	}

	reader := NewExecFrameReader(strings.NewReader(buf.String()))
	out, err := reader.NextInputFrame()
	if err != nil {
		t.Fatalf("NextInputFrame: %v", err)
	}
	if out.Type != ExecInputSend || out.Content != "hello" {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
	if _, err := reader.NextInputFrame(); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF at stream end, got %v", err)
	}
}

func TestExecInputFrameSendPayload(t *testing.T) {
	full := ExecInputFrame{Type: ExecInputSend, Send: &SendRequest{Content: "full", Command: "ls"}}
	if got := full.SendPayload(); got == nil || got.Command != "ls" {
		t.Fatalf("SendPayload must prefer the full SendRequest, got %+v", got)
	}

	bare := ExecInputFrame{Type: ExecInputSend, Content: "bare"}
	got := bare.SendPayload()
	if got == nil || got.Content != "bare" {
		t.Fatalf("bare content shorthand must produce a SendRequest, got %+v", got)
	}

	empty := ExecInputFrame{Type: ExecInputCancel}
	if got := empty.SendPayload(); got != nil {
		t.Fatalf("frame without send payload must return nil, got %+v", got)
	}
}

func TestExecFrameReaderOversize(t *testing.T) {
	huge := strings.Repeat("a", MaxExecFrameBytes+1)
	reader := NewExecFrameReader(strings.NewReader(`{"type":"` + huge + `"}`))
	err := reader.Next(&struct{}{})
	if !errors.Is(err, ErrExecFrameTooLarge) {
		t.Fatalf("oversize frame error = %v, want ErrExecFrameTooLarge", err)
	}
}

func TestExecFrameReaderSkipsBlankLines(t *testing.T) {
	reader := NewExecFrameReader(strings.NewReader("\n{\"type\":\"cancel\"}\n\n"))
	frame, err := reader.NextInputFrame()
	if err != nil {
		t.Fatalf("NextInputFrame: %v", err)
	}
	if frame.Type != ExecInputCancel {
		t.Fatalf("frame = %+v, want cancel", frame)
	}
	if err := reader.Next(&struct{}{}); !errors.Is(err, io.EOF) {
		t.Fatalf("expected io.EOF after trailing blanks, got %v", err)
	}
}

func TestWriteEventVerbatim(t *testing.T) {
	var buf bytes.Buffer
	ev := Event{
		Type:    EventCompleted,
		Topic:   SessionStreamTopic("sess-1"),
		Payload: CompletedEvent{SessionID: "sess-1"},
	}
	if err := WriteEvent(&buf, ev); err != nil {
		t.Fatalf("WriteEvent: %v", err)
	}

	back, err := NewExecFrameReader(bytes.NewReader(buf.Bytes())).NextEvent()
	if err != nil {
		t.Fatalf("NextEvent: %v", err)
	}
	if back.Type != EventCompleted {
		t.Fatalf("event type = %q, want completed", back.Type)
	}
	p, ok := back.Payload.(CompletedEvent)
	if !ok || p.SessionID != "sess-1" {
		t.Fatalf("payload round-trip mismatch: %#v", back.Payload)
	}
}

func TestExecHeaderAndCommandResultFraming(t *testing.T) {
	var buf bytes.Buffer
	header := ExecHeaderFrame{Type: ExecHeaderType, SessionID: "s1", Config: SessionConfig{Model: "m"}}
	if err := WriteNDJSON(&buf, header); err != nil {
		t.Fatalf("write header: %v", err)
	}
	res := ExecCommandResultFrame{Type: ExecCommandResultType, Command: "/usage", OK: true, Output: "calls: 1"}
	if err := WriteNDJSON(&buf, res); err != nil {
		t.Fatalf("write command result: %v", err)
	}

	lines := bufio.NewScanner(bytes.NewReader(buf.Bytes()))
	lines.Split(bufio.ScanLines)

	var raw []map[string]any
	for lines.Scan() {
		var m map[string]any
		if err := json.Unmarshal(lines.Bytes(), &m); err != nil {
			t.Fatalf("decode frame: %v", err)
		}
		raw = append(raw, m)
	}
	if len(raw) != 2 {
		t.Fatalf("frame count = %d, want 2", len(raw))
	}
	if raw[0]["type"] != ExecHeaderType || raw[0]["session_id"] != "s1" {
		t.Fatalf("header frame mismatch: %v", raw[0])
	}
	if raw[1]["type"] != ExecCommandResultType || raw[1]["ok"] != true {
		t.Fatalf("command result frame mismatch: %v", raw[1])
	}
}

func TestExecResultEnvelope(t *testing.T) {
	res := ExecResult{
		SessionID: "s1",
		Status:    ExecResultCompleted,
		Text:      "done",
		Usage:     &SessionUsage{SessionID: "s1"},
	}
	data, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var back ExecResult
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.Status != ExecResultCompleted || back.Usage == nil || back.SessionID != "s1" {
		t.Fatalf("envelope round-trip mismatch: %+v", back)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal raw: %v", err)
	}
	if _, ok := raw["changes"]; ok {
		t.Fatalf("empty changes must be omitted at top level, got %s", data)
	}
	if _, ok := raw["error"]; ok {
		t.Fatalf("empty error must be omitted, got %s", data)
	}
}

func TestExecInputPlanReviewRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := ExecInputFrame{
		Type: ExecInputPlanReview,
		PlanReview: &PlanReviewDecision{
			RequestID:  "pr-1",
			ToolCallID: "tc-1",
			Approved:   true,
			Mode:       ModeAcceptEdits,
		},
	}
	if err := WriteNDJSON(&buf, in); err != nil {
		t.Fatalf("WriteNDJSON: %v", err)
	}
	out, err := NewExecFrameReader(bytes.NewReader(buf.Bytes())).NextInputFrame()
	if err != nil {
		t.Fatalf("NextInputFrame: %v", err)
	}
	if out.Type != ExecInputPlanReview || out.PlanReview == nil || !out.PlanReview.Approved || out.PlanReview.Mode != ModeAcceptEdits {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}
