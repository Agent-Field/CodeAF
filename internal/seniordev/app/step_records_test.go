//go:build !windows

package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/seniordev/bus"
)

func toolPartPayload(callID, tool, status string, input map[string]any, output, failure string) bus.Payload {
	state := map[string]any{"status": status, "input": input}
	switch status {
	case "completed":
		state["output"] = output
	case "error":
		state["error"] = failure
	}
	return bus.Payload{
		ID: "evt-" + callID + "-" + status, Type: "message.part.updated",
		Properties: map[string]any{
			"sessionID": "ses-1",
			"part": map[string]any{
				"id": "part-" + callID, "type": "tool",
				"callID": callID, "tool": tool, "state": state,
			},
		},
	}
}

func streamSteps(t *testing.T, stream []byte) []event {
	t.Helper()
	var steps []event
	for _, line := range bytes.Split(bytes.TrimSpace(stream), []byte("\n")) {
		var value event
		if err := json.Unmarshal(line, &value); err != nil || value.Type != "step" {
			continue
		}
		steps = append(steps, value)
	}
	return steps
}

// A step is a thing that happened: only completed and error have happened, and
// a tool part is republished as its state moves, so the same finished call
// arrives more than once and must be reported once.
func TestStepRecordsFireOncePerFinishedToolCall(t *testing.T) {
	var stream bytes.Buffer
	writer := newEventWriter(&stream)

	writer.busEvent(toolPartPayload("c1", "bash", "pending", map[string]any{"command": "go test ./..."}, "", ""))
	writer.busEvent(toolPartPayload("c1", "bash", "running", map[string]any{"command": "go test ./..."}, "", ""))
	if got := streamSteps(t, stream.Bytes()); len(got) != 0 {
		t.Fatalf("unfinished tool call produced %d step records, want 0", len(got))
	}

	writer.busEvent(toolPartPayload("c1", "bash", "completed", map[string]any{"command": "go test ./..."}, "ok  \tpkg\t0.3s", ""))
	writer.busEvent(toolPartPayload("c1", "bash", "completed", map[string]any{"command": "go test ./..."}, "ok  \tpkg\t0.3s", ""))

	steps := streamSteps(t, stream.Bytes())
	if len(steps) != 1 {
		t.Fatalf("step records = %d, want 1 (the republished part must not repeat)", len(steps))
	}
	if steps[0].Command != "bash: go test ./..." {
		t.Fatalf("command = %q, want %q", steps[0].Command, "bash: go test ./...")
	}
	if !strings.Contains(steps[0].Observation, "ok") {
		t.Fatalf("observation = %q, want the tool output", steps[0].Observation)
	}
}

// An error carries the failure as its observation: a reader showing steps
// should see why a call failed, not an empty result.
func TestStepRecordCarriesTheFailureOnError(t *testing.T) {
	var stream bytes.Buffer
	writer := newEventWriter(&stream)
	writer.busEvent(toolPartPayload(
		"c2", "edit", "error", map[string]any{"filePath": "internal/x.go"}, "", "file does not exist",
	))

	steps := streamSteps(t, stream.Bytes())
	if len(steps) != 1 {
		t.Fatalf("step records = %d, want 1", len(steps))
	}
	if steps[0].Command != "edit: internal/x.go" {
		t.Fatalf("command = %q, want %q", steps[0].Command, "edit: internal/x.go")
	}
	if steps[0].Observation != "file does not exist" {
		t.Fatalf("observation = %q, want the error", steps[0].Observation)
	}
}

// A tool result can be a whole file. The observation is capped, and the cap is
// applied on a rune boundary so the record always encodes.
func TestStepObservationIsCappedAndStaysValidUTF8(t *testing.T) {
	var stream bytes.Buffer
	writer := newEventWriter(&stream)
	// Three-byte runes, so a naive byte cut lands mid-rune.
	output := strings.Repeat("→", stepObservationMax)
	writer.busEvent(toolPartPayload(
		"c3", "read", "completed", map[string]any{"filePath": "big.txt"}, output, "",
	))

	steps := streamSteps(t, stream.Bytes())
	if len(steps) != 1 {
		t.Fatalf("step records = %d, want 1", len(steps))
	}
	if got := len(steps[0].Observation); got > stepObservationMax {
		t.Fatalf("observation = %d bytes, want at most %d", got, stepObservationMax)
	}
	if !utf8.ValidString(steps[0].Observation) {
		t.Fatal("observation was cut mid-rune and is not valid UTF-8")
	}
}

// A tool whose input names none of the identifying keys still renders a label
// rather than a bare tool name, so a new tool needs no change here.
func TestStepCommandFallsBackToTheFirstStringInput(t *testing.T) {
	record, ok := toolStepRecord(toolPartPayload(
		"c4", "custom", "completed", map[string]any{"zeta": "last", "alpha": "first"}, "done", "",
	))
	if !ok {
		t.Fatal("a completed tool call was not recognised as a step")
	}
	if record.command != "custom: first" {
		t.Fatalf("command = %q, want %q", record.command, "custom: first")
	}
}

// Anything that is not a finished tool part is not a step.
func TestNonToolPayloadsAreNotSteps(t *testing.T) {
	if _, ok := toolStepRecord(assistantPayload("m1", "coder", 1, 2, 3, 0.01)); ok {
		t.Fatal("an assistant message was read as a step")
	}
	if _, ok := toolStepRecord(bus.Payload{Type: "session.created"}); ok {
		t.Fatal("a session event was read as a step")
	}
}
