package codeaf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/engine/steploop"
	"github.com/Agent-Field/aforge-v2/internal/swepro/internal/session/sessioncore"
)

func TestQuestionToolEventsReachStdoutInPinnedTypeScriptWireShape(t *testing.T) {
	workspace := validityTestRepo(t)
	var output bytes.Buffer
	runner := newPipeline(cliArgs{High: "provider/high"}, workspace, pipelineDeps{
		Backend: backendFunc(func(context.Context, turn) (turnResult, error) {
			return turnResult{}, nil
		}),
		Events: newEventWriter(&output),
	})
	defer runner.runtime.Close()
	if runner.runtime.initErr != nil {
		t.Fatal(runner.runtime.initErr)
	}

	input := json.RawMessage(`{"questions":[{"question":"Continue?","header":"Choice","options":[{"label":"Yes","description":"Continue now"}]}]}`)
	_, err := runner.runtime.registry.Execute(context.Background(), steploop.ToolCall{
		ID: "call-question", Name: "question", Input: input,
		SessionID: "ses-question", MessageID: "msg-question", Agent: "coder",
	})
	if err == nil || err.Error() != "The user dismissed this question" {
		t.Fatalf("question error = %v, want headless rejection", err)
	}

	seen := map[string]bool{}
	for _, line := range bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n")) {
		var value map[string]json.RawMessage
		if err := json.Unmarshal(line, &value); err != nil {
			t.Fatalf("event line %q: %v", line, err)
		}
		if len(value) != 3 || value["id"] == nil || value["type"] == nil || value["properties"] == nil {
			t.Fatalf("bus line keys = %v, want exactly id/type/properties", value)
		}
		var eventType string
		if err := json.Unmarshal(value["type"], &eventType); err != nil {
			t.Fatal(err)
		}
		seen[eventType] = true
	}
	if !seen["question.asked"] || !seen["question.rejected"] {
		t.Fatalf("stdout events = %v, want question.asked and question.rejected; stream=%s", seen, output.String())
	}
}

func TestPipelineStreamsTypeScriptBusEvents(t *testing.T) {
	workspace := validityTestRepo(t)
	var output bytes.Buffer
	runner := newPipeline(cliArgs{High: "provider/high"}, workspace, pipelineDeps{
		Backend: backendFunc(func(context.Context, turn) (turnResult, error) {
			return turnResult{}, nil
		}),
		Events: newEventWriter(&output),
	})
	defer runner.runtime.Close()
	if runner.runtime.initErr != nil {
		t.Fatal(runner.runtime.initErr)
	}
	if _, err := runner.runtime.durable.sessions.Create(context.Background(), sessioncore.CreateInput{
		ID: "ses_contract", Title: "contract",
	}); err != nil {
		t.Fatal(err)
	}

	lines := bytes.Split(bytes.TrimSpace(output.Bytes()), []byte("\n"))
	if len(lines) != 2 {
		t.Fatalf("session creation lines = %d, want created + compatibility update: %s", len(lines), output.String())
	}
	for index, wantType := range []string{"session.created", "session.updated"} {
		var value map[string]json.RawMessage
		if err := json.Unmarshal(lines[index], &value); err != nil {
			t.Fatal(err)
		}
		if len(value) != 3 || value["id"] == nil || value["type"] == nil || value["properties"] == nil {
			t.Fatalf("bus line keys = %v, want exactly id/type/properties", value)
		}
		var gotType string
		if err := json.Unmarshal(value["type"], &gotType); err != nil || gotType != wantType {
			t.Fatalf("bus line %d type = %q (%v), want %q", index, gotType, err, wantType)
		}
	}
}

func TestRunFormatSurfaceMatchesPinnedTypeScript(t *testing.T) {
	for _, format := range []string{"default", "json"} {
		t.Run("accept_"+format, func(t *testing.T) {
			args, err := parseArgs([]string{"run", "--format", format, "work"})
			if err != nil {
				t.Fatalf("parseArgs rejected TS format %q: %v", format, err)
			}
			if args.Format != format {
				t.Fatalf("format = %q, want %q", args.Format, format)
			}
		})
	}

	for _, format := range []string{"ndjson", "text", "pretty", "yaml"} {
		t.Run("reject_"+format, func(t *testing.T) {
			_, err := parseArgs([]string{"run", "--format", format, "work"})
			if err == nil || !strings.Contains(err.Error(), "default or json") {
				t.Fatalf("parseArgs format %q error = %v, want default/json rejection", format, err)
			}
		})
	}

	args, err := parseArgs([]string{"run", "work"})
	if err != nil {
		t.Fatal(err)
	}
	if args.Format != "json" {
		t.Fatalf("default format = %q, want json", args.Format)
	}
}

func TestTUIFailsLoudlyBeforePipelineStartup(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := runCLI(
		context.Background(), []string{"run", "--tui", "work"}, nil,
		&stdout, &stderr,
	)
	var exit *cliExitError
	if !errors.As(err, &exit) || exit.code != 1 {
		t.Fatalf("--tui error = %#v, want cli exit code 1", err)
	}
	if stdout.Len() != 0 {
		t.Fatalf("--tui stdout = %q, want empty", stdout.String())
	}
	const message = "--tui is unsupported in the Go port"
	if !strings.Contains(stderr.String(), message) ||
		!strings.Contains(stderr.String(), "headless NDJSON event stream") {
		t.Fatalf("--tui stderr = %q, want clear unsupported/headless message", stderr.String())
	}
}
