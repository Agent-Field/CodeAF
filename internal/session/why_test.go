package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// Why is a derivation, not a request: the same turn always reads back the same
// sentence, and every figure in it comes from the journal. This is the whole
// shape — the instruction quoted, then what the tools actually did, with the
// counts taken from the calls themselves.
func TestWhyNamesTheFilesAndTheCounts(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("e1", "edit",
				`{"path":"loop.go","edits":[{"oldText":"alpha","newText":"ALPHA\nEXTRA"},{"oldText":"bravo","newText":"BRAVO"}]}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("b1", "bash", `{"command":"echo built"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("w1", "write", `{"path":"out.txt","content":"result\n"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("done"), nil
		},
	}}
	agent, workspace := newTestAgent(t, completer, nil)
	if err := os.WriteFile(filepath.Join(workspace, "loop.go"), []byte("alpha\nbravo\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	collect(t, mustSubmit(t, agent, "make the loop faster"))

	why := agent.Why()
	if !strings.HasPrefix(why, "> make the loop faster\n") {
		t.Fatalf("Why did not quote the instruction first:\n%s", why)
	}
	// +3 −2: one replacement traded one line for two, the other one for one.
	// The arithmetic is over the call's own arguments, so it is the same answer
	// after the file has moved on.
	for _, want := range []string{
		"edited loop.go (+3 −2 via 2 replacements)",
		"ran echo built (ok)",
		"wrote out.txt",
	} {
		if !strings.Contains(why, want) {
			t.Fatalf("Why is missing %q:\n%s", want, why)
		}
	}
	// Order is the order the work happened in, not the order the tools are
	// listed anywhere.
	if edited, ran := strings.Index(why, "edited"), strings.Index(why, "ran"); edited > ran {
		t.Fatalf("Why reordered the turn:\n%s", why)
	}

	// Offline and deterministic: nothing was asked of the provider, and the
	// second reading is the first one.
	if got := completer.requests(); got != 4 {
		t.Fatalf("requests = %d, want the turn's 4 — Why must not ask the model", got)
	}
	if second := agent.Why(); second != why {
		t.Fatalf("Why is not deterministic:\n%s\n---\n%s", why, second)
	}
}

// A command that failed is reported as failed. The verdict is read from pi's
// own status footer, which is the only record the transcript keeps of how a
// call went.
func TestWhyReportsAFailedCommand(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("b1", "bash", `{"command":"exit 3"}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("that did not work"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	collect(t, mustSubmit(t, agent, "build it"))

	if why := agent.Why(); !strings.Contains(why, "ran exit 3 (failed)") {
		t.Fatalf("Why = %q, want the failed command reported as failed", why)
	}
}

// An edit that never applied is not an edit. Claiming one — with a diffstat
// computed from arguments that were refused — is exactly the fluent
// reconstruction this call exists to avoid.
func TestWhyDoesNotClaimAnEditThatFailed(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("e1", "edit",
				`{"path":"missing.go","edits":[{"oldText":"alpha","newText":"beta"}]}`), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the file is not there"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)

	collect(t, mustSubmit(t, agent, "rename alpha"))

	why := agent.Why()
	if !strings.Contains(why, "tried to edit missing.go (failed)") {
		t.Fatalf("Why = %q, want the failed edit reported as attempted", why)
	}
	if strings.Contains(why, "+1") {
		t.Fatalf("Why reported a diffstat for an edit that never applied: %q", why)
	}
}

func TestWhyOnATurnThatRanNoTools(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("the planner walks the graph"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, nil)
	collect(t, mustSubmit(t, agent, "how does the planner work?"))

	why := agent.Why()
	if !strings.Contains(why, "> how does the planner work?") {
		t.Fatalf("Why did not quote the question:\n%s", why)
	}
	if !strings.Contains(why, "No tools ran") {
		t.Fatalf("Why = %q, want it to say the turn changed nothing", why)
	}
}

func TestWhyBeforeAnythingHappened(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	if why := agent.Why(); !strings.Contains(why, "Nothing to explain yet") {
		t.Fatalf("Why on a fresh session = %q", why)
	}
}

// ── compaction focus ────────────────────────────────────────────────────────

// `/compact keep the API decisions` used to be a person telling the summarizer
// which part of a lossy summary had to survive. There is no summarizer now
// (loop.go), so the focus reaches nothing — and the pass has to run ANYWAY, and
// has to make no provider call while it does. A person who typed the old form is
// owed the compaction they asked for, not an error about a machine that used to
// exist.
func TestCompactWithFocusStillCompactsAndCallsNoModel(t *testing.T) {
	completer := &scriptedCompleter{}
	agent := compactableAgent(t, completer)

	if err := agent.CompactWithFocus(context.Background(), "keep the API decisions and the failing test"); err != nil {
		t.Fatalf("CompactWithFocus: %v", err)
	}
	if got := completer.requests(); got != 0 {
		t.Fatalf("requests = %d, want a compaction that costs nothing", got)
	}

	// And the session's own system message is untouched: the focus had nowhere
	// to leak to, and the next turn is byte-for-byte what it was.
	agent.mu.Lock()
	system := messageText(agent.messages[0])
	agent.mu.Unlock()
	if system != "SYSTEM" {
		t.Fatalf("the focus leaked into the session prompt: %q", system)
	}
}

// compactableAgent is a session whose transcript is already over its window: a
// tiny window, one long ASSISTANT message for the fold to take, and one short
// one to keep as the tail.
//
// The assistant role is the point. A person's words are never folded (loop.go's
// [Agent.foldLocked]), so a transcript of one long user message has nothing a
// compaction may touch and reports ErrNothingToCompact — correctly.
func compactableAgent(t *testing.T, completer Completer) *Agent {
	t.Helper()
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.ContextWindow = 200
	})
	agent.mu.Lock()
	agent.messages = append(agent.messages,
		textMessage("user", "get on with it"),
		textMessage("assistant", strings.Repeat("context that will not fit. ", 40)),
		textMessage("assistant", "short reply"))
	agent.mu.Unlock()
	return agent
}
