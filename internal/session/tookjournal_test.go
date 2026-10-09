package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// ── A CALL'S DURATION OUTLIVES THE WINDOW ───────────────────────────────────

// A FINISHED CALL STILL SAYS WHAT IT TOOK AFTER A REOPEN — the same figure the
// live EventToolFinished carried while the window was open.
//
// Without the journal line the surface drew Args and Output onto a reopened
// row and left the duration blank, which is exactly the lie a task room opened
// after landing used to tell (the row was there; the figure was not).
func TestACallsOwnDurationSurvivesAReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("call-slow", "bash", `{"command":"sleep 1"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = path })
	collect(t, mustSubmit(t, agent, "wait a second"))

	live := toolEntryFor(t, agent.Transcript(), "call-slow")
	if live.Took < 900*time.Millisecond {
		t.Fatalf("the live entry carries Took=%v, want about a second", live.Took)
	}
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	back := toolEntryFor(t, reopen(t, path).Transcript(), "call-slow")
	if back.Took != live.Took {
		t.Fatalf("the reopened call says Took=%v, want the live %v", back.Took, live.Took)
	}
	detached := toolEntryFor(t, ReadTranscript(path).Entries, "call-slow")
	if detached.Took != live.Took {
		t.Fatalf("the detached reading carries Took=%v, want the live %v", detached.Took, live.Took)
	}
}

// THE JOURNAL WRITES ONE `took` LINE PER FINISHED CALL, keyed by the call's
// own id — the same anchor EventToolFinished carries.
func TestTheJournalWritesOneTookLinePerFinishedCall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return &ai.Response{
				Choices: []ai.Choice{{Message: ai.Message{
					Role: "assistant",
					ToolCalls: []ai.ToolCall{
						{ID: "call-a", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"sleep 1; echo A"}`}},
						{ID: "call-b", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"sleep 1; echo B"}`}},
					},
				}}},
				Usage: &ai.Usage{PromptTokens: 20, CompletionTokens: 7, TotalTokens: 27},
			}, nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("both printed"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = path })
	collect(t, mustSubmit(t, agent, "run both"))
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	seen := map[string]int{}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry sessionEntry
		if json.Unmarshal([]byte(line), &entry) != nil || entry.Type != "took" || entry.Took == nil {
			continue
		}
		seen[entry.Took.CallID]++
		if entry.Took.DurationMS <= 0 {
			t.Fatalf("took line for %q carries DurationMS=%d", entry.Took.CallID, entry.Took.DurationMS)
		}
	}
	if seen["call-a"] != 1 || seen["call-b"] != 1 {
		t.Fatalf("journal took lines by call id: %v, want one each for call-a and call-b", seen)
	}
}

// AN OLDER JOURNAL WITHOUT `took` LINES REPLAYS WITH NO DURATION — the emptiness
// law, not a invented figure.
func TestAJournalWithNoTookLinesReplaysWithNoDuration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeLines(t, path,
		`{"type":"session","version":1,"id":"old","cwd":"/tmp","model":"test","timestamp":"2026-01-01T00:00:00Z"}`,
		`{"type":"message","role":"user","content":"run it","timestamp":"2026-01-01T00:00:01Z"}`,
		`{"type":"message","role":"assistant","toolCalls":[{"id":"call-1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"echo hi\"}"}}],"timestamp":"2026-01-01T00:00:02Z"}`,
		`{"type":"message","role":"tool","toolCallId":"call-1","content":"hi","timestamp":"2026-01-01T00:00:03Z"}`,
		`{"type":"message","role":"assistant","content":"done","timestamp":"2026-01-01T00:00:04Z"}`,
	)
	entry := toolEntryFor(t, ReadTranscript(path).Entries, "call-1")
	if entry.Took != 0 {
		t.Fatalf("an older journal invented Took=%v", entry.Took)
	}
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// ── A FAILED CALL STAYS FAILED ──────────────────────────────────────────────

// A CALL THAT SENT EventToolFailed LIVE IS Failed ON THE TRANSCRIPT AND AFTER A
// REOPEN, and a call that ended well is not — the mark is the journal's own
// `failed` line, never a reading of the output's words.
func TestAFailedCallIsMarkedFailedLiveAndOnReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return &ai.Response{
				Choices: []ai.Choice{{Message: ai.Message{
					Role: "assistant",
					ToolCalls: []ai.ToolCall{
						{ID: "call-bad", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo fine; exit 3"}`}},
						{ID: "call-ok", Type: "function", Function: ai.ToolCallFunction{Name: "bash", Arguments: `{"command":"echo failed"}`}},
					},
				}}},
				Usage: &ai.Usage{PromptTokens: 20, CompletionTokens: 7, TotalTokens: 27},
			}, nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.SessionFile = path })
	failedLive := map[string]bool{}
	for _, event := range collect(t, mustSubmit(t, agent, "run both")) {
		if event.Kind == EventToolFailed {
			failedLive[event.CallID] = true
		}
	}
	if !failedLive["call-bad"] || failedLive["call-ok"] {
		t.Fatalf("live EventToolFailed by call id: %v, want only call-bad", failedLive)
	}
	check := func(where string, entries []DisplayEntry) {
		t.Helper()
		for id, want := range map[string]bool{"call-bad": true, "call-ok": false} {
			if got := toolEntryFor(t, entries, id).Failed; got != want {
				t.Fatalf("%s: %s Failed=%v, want %v", where, id, got, want)
			}
		}
	}
	check("live transcript", agent.Transcript())
	if err := agent.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	check("reopened", reopen(t, path).Transcript())
	check("detached reading", ReadTranscript(path).Entries)
}

// AN OLDER JOURNAL WITH NO `failed` LINE REPLAYS AS NOT FAILED, even when the
// output reads like an error: no heuristic invents the mark.
func TestAJournalWithNoFailedLinesReplaysAsNotFailed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	writeLines(t, path,
		`{"type":"session","version":1,"id":"old","cwd":"/tmp","model":"test","timestamp":"2026-01-01T00:00:00Z"}`,
		`{"type":"message","role":"user","content":"run it","timestamp":"2026-01-01T00:00:01Z"}`,
		`{"type":"message","role":"assistant","toolCalls":[{"id":"call-1","type":"function","function":{"name":"bash","arguments":"{\"command\":\"false\"}"}}],"timestamp":"2026-01-01T00:00:02Z"}`,
		`{"type":"message","role":"tool","toolCallId":"call-1","content":"error: exit status 1","timestamp":"2026-01-01T00:00:03Z"}`,
		`{"type":"message","role":"assistant","content":"done","timestamp":"2026-01-01T00:00:04Z"}`,
	)
	if toolEntryFor(t, ReadTranscript(path).Entries, "call-1").Failed {
		t.Fatal("an older journal invented Failed from the output text")
	}
}
