package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

func TestUserBashPersistsLiteralOutputForTheNextModelTurn(t *testing.T) {
	c := &scriptedCompleter{}
	file := filepath.Join(t.TempDir(), "chat.jsonl")
	a, workspace := newTestAgent(t, c, func(cfg *Config) { cfg.SessionFile = file })
	command := `!printf '%s\n' '/task @literal'; pwd; printf 'stderr-token\n' >&2; exit 7`
	ch, err := a.Submit(t.Context(), command)
	if err != nil {
		t.Fatal(err)
	}
	events := collect(t, ch)
	var result Event
	for _, ev := range events {
		if ev.Kind == EventToolFailed {
			result = ev
		}
	}
	for _, want := range []string{"/task @literal", workspace, "stderr-token", "7"} {
		if !strings.Contains(result.Output, want) {
			t.Fatalf("output missing %q: %q", want, result.Output)
		}
	}
	if !IsUserBashCall(result.CallID) || c.requests() != 0 || c.asideRequests() != 0 {
		t.Fatalf("command called a model or lost provenance: %+v; %d calls", result, c.requests())
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	replay, err := replaySessionFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if len(replay.messages) != 3 || messageContentText(replay.messages[0]) != command {
		t.Fatalf("journal did not retain one user command and call/result pair: %+v", replay.messages)
	}
	c.steps = []step{func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
		for _, msg := range messages {
			if msg.Role == "tool" && strings.Contains(messageContentText(msg), "stderr-token") {
				return textResponse("I can read that shell output."), nil
			}
		}
		t.Error("resumed model request lost the shell output")
		return textResponse("missing"), nil
	}}
	resumed, err := newAgent(a.config, c)
	if err != nil {
		t.Fatal(err)
	}
	defer resumed.Close()
	ch, err = resumed.Submit(t.Context(), "What did that command print?")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, ch)
	if c.requests() == 0 {
		t.Fatal("follow-up never reached the model")
	}
}

func TestUserBashHasNoStdinAndDoesNotKeepShellState(t *testing.T) {
	a, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	for _, command := range []string{`!if read line; then echo stdin-open; else echo stdin-eof; fi; cd /`, `!pwd`} {
		ch, err := a.Submit(t.Context(), command)
		if err != nil {
			t.Fatal(err)
		}
		events := collect(t, ch)
		want := "stdin-eof"
		if command == "!pwd" {
			want = workspace
		}
		found := false
		for _, ev := range events {
			if ev.Kind == EventToolEnd && strings.Contains(ev.Output, want) {
				found = true
			}
		}
		if !found {
			t.Fatalf("command %q did not print %q: %+v", command, want, events)
		}
	}
}

func TestUserBashRefusesEmptyAndBusyAndHonorsCancellation(t *testing.T) {
	a, workspace := newTestAgent(t, &scriptedCompleter{}, nil)
	if _, err := a.Submit(t.Context(), "!  "); err == nil {
		t.Fatal("empty command accepted")
	}
	a.mu.Lock()
	a.running = true
	a.mu.Unlock()
	_, err := a.Submit(t.Context(), "!echo do-not-steer")
	a.mu.Lock()
	a.running = false
	queued := len(a.steering)
	a.mu.Unlock()
	if err == nil || queued != 0 {
		t.Fatal("busy command became steering")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	ch, err := a.Submit(ctx, "!touch should-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, ch)
	if _, err := os.Stat(filepath.Join(workspace, "should-not-exist")); !os.IsNotExist(err) {
		t.Fatal("cancelled command ran")
	}
}
