package main

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/exec"
)

// A steer file hands over only complete lines, each once, in either spelling.
func TestSteerFileReadsEachCompleteLineOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "steer")
	s := newSteerFile(path)
	if got := s.Read(); got != nil {
		t.Fatalf("no file yet, got %q", got)
	}
	appendTo(t, path, "also add a doc comment\n{\"text\": \"and a test\"}\nhalf a sen")
	if got := s.Read(); strings.Join(got, "|") != "also add a doc comment|and a test" {
		t.Fatalf("first read = %q", got)
	}
	if got := s.Read(); got != nil {
		t.Fatalf("nothing new, got %q", got)
	}
	appendTo(t, path, "tence\n\n")
	if got := s.Read(); strings.Join(got, "|") != "half a sentence" {
		t.Fatalf("the partial line was not finished: %q", got)
	}
}

// steerAwareModel writes the note it was given into its answer, and on its
// first call has a person append a note to the steer file — the mid-run window
// a live harness steers into.
type steerAwareModel struct {
	steerPath string
	calls     int
}

func (m *steerAwareModel) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	m.calls++
	if m.calls == 1 {
		f, _ := os.OpenFile(m.steerPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		_, _ = f.WriteString("also add a doc comment\n")
		_ = f.Close()
	}
	answer := "wrote the function"
	for _, message := range messages {
		for _, part := range message.Content {
			if message.Role == "user" && strings.Contains(part.Text, "also add a doc comment") {
				answer = "wrote the function and its doc comment"
			}
		}
	}
	return &ai.Response{
		Choices: []ai.Choice{{Message: ai.Message{Role: "assistant", Content: []ai.ContentPart{{Type: "text", Text: answer}}}, FinishReason: "stop"}},
		Usage:   &ai.Usage{PromptTokens: 10, CompletionTokens: 5},
	}, nil
}

// The whole mid-run path through the real executor: a note appended to the
// steer file while the run works reaches the model, changes the deliverable,
// and the events file says so as it happens.
func TestExecAMidRunNoteChangesTheAnswerAndIsStreamed(t *testing.T) {
	dir := t.TempDir()
	steerPath, eventsPath := filepath.Join(dir, "steer"), filepath.Join(dir, "events.ndjson")
	space, err := exec.NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	events, err := openEventFile(eventsPath, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer events.Close()
	ctx := exec.WithTranscript(context.Background(), events)
	events.Status("started")

	model := &steerAwareModel{steerPath: steerPath}
	task := execTask("write a function", "", space.Root())
	task.Steer = newSteerFile(steerPath).Read
	outcome, err := exec.NewLinear(model, space, nil, 10, 1_000_000, time.Minute).Run(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	events.Status("ended: done")
	if outcome.Text != "wrote the function and its doc comment" || outcome.Steered != 1 {
		t.Fatalf("outcome %q steered %d: the note never reached the work", outcome.Text, outcome.Steered)
	}

	var kinds []string
	var steered bool
	f, _ := os.Open(eventsPath)
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var ev execEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			t.Fatalf("not one JSON object per line: %q", scanner.Text())
		}
		if ev.At == "" {
			t.Fatalf("an entry without a time: %q", scanner.Text())
		}
		kinds = append(kinds, ev.Kind)
		if ev.Kind == "note" && strings.Contains(ev.Text, "steered: also add a doc comment") {
			steered = true
		}
	}
	if !steered {
		t.Fatalf("no steered note among %v", kinds)
	}
	if kinds[0] != "status" || kinds[len(kinds)-1] != "status" {
		t.Fatalf("the run's start and end are not bracketed: %v", kinds)
	}
	var said bool
	for _, k := range kinds {
		said = said || k == "assistant"
	}
	if !said {
		t.Fatalf("the model's words were not streamed: %v", kinds)
	}
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}
