package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/cell"
)

// Contract C3: while workspace files are still arriving in the background, the
// model can answer in words immediately, and any tool that reads or writes the
// workspace or runs a command in it waits until the files land and then runs
// against the complete tree.
func TestC3WorkspaceToolWaitsForArrivingFilesWhileWordsAnswerImmediately(t *testing.T) {
	var toolEntered, toolRan atomic.Bool
	completer := &scriptedCompleter{
		steps: []step{
			// Turn 1: model answers in words while files are still arriving.
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return textResponse("answering in words while workspace files arrive"), nil
			},
			// Turn 2: model calls read on arrived.txt, which must wait for arrival.
			func(context.Context, []ai.Message) (*ai.Response, error) {
				toolEntered.Store(true)
				return toolResponse("call_read_1", "read", `{"path":"arrived.txt"}`), nil
			},
			func(_ context.Context, msgs []ai.Message) (*ai.Response, error) {
				toolRan.Store(true)
				last := msgs[len(msgs)-1]
				return textResponse("read saw: " + strings.TrimSpace(messageText(last))), nil
			},
		},
	}

	agent, workspace := newTestAgent(t, completer, func(cfg *Config) {
		cfg.ApprovalPosture = PostureAllow
	})
	defer agent.Close()

	arrival := cell.BeginWorkspaceArrival(workspace, workspace, func() time.Duration { return 5 * time.Second }, nil)
	defer arrival.Complete()

	// Words-only turn finishes immediately while the arrival gate is still open.
	events, err := agent.Submit(t.Context(), "can you answer in words?")
	if err != nil {
		t.Fatalf("words turn failed: %v", err)
	}
	var wordsOut string
	for ev := range events {
		if ev.Kind == EventTextDelta {
			wordsOut += ev.Text
		}
	}
	if wordsOut == "" {
		wordsOut = messageText(lastMessage(agent))
	}
	if !strings.Contains(wordsOut, "answering in words while workspace files arrive") {
		t.Fatalf("words turn output = %q", wordsOut)
	}

	// Tool turn starts and blocks waiting for the workspace files to arrive.
	toolEvents, err := agent.Submit(t.Context(), "read arrived.txt")
	if err != nil {
		t.Fatalf("tool turn submit failed: %v", err)
	}
	done := make(chan string, 1)
	go func() {
		var out string
		for ev := range toolEvents {
			if ev.Kind == EventTextDelta {
				out += ev.Text
			}
		}
		if out == "" {
			out = messageText(lastMessage(agent))
		}
		done <- out
	}()

	deadline := time.Now().Add(2 * time.Second)
	for !toolEntered.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if !toolEntered.Load() {
		t.Fatal("model never issued tool call")
	}
	time.Sleep(25 * time.Millisecond)
	if toolRan.Load() {
		t.Fatal("workspace tool ran before workspace files finished arriving")
	}

	// Write the arriving workspace file and mark the workspace arrival complete.
	if err := os.WriteFile(filepath.Join(workspace, "arrived.txt"), []byte("complete workspace content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	arrival.Complete()

	select {
	case out := <-done:
		if !strings.Contains(out, "complete workspace content") {
			t.Fatalf("tool turn output = %q, want complete workspace content", out)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("waiting workspace tool did not resume after files landed")
	}
}

// Contract C3 and C4: when the background workspace fetch fails, a workspace
// tool keeps waiting up to the stall bound, reports why in plain words, and
// never runs on a partial tree.
func TestC3WorkspaceToolWaitsOnFailedBackgroundFetchAndReportsWhyNeverRunningOnPartialTree(t *testing.T) {
	var executedOnPartialTree atomic.Int32
	completer := &scriptedCompleter{
		steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call_read_fail", "read", `{"path":"partial.txt"}`), nil
			},
			func(_ context.Context, msgs []ai.Message) (*ai.Response, error) {
				last := msgs[len(msgs)-1]
				return textResponse("tool reported: " + strings.TrimSpace(messageText(last))), nil
			},
		},
	}

	agent, workspace := newTestAgent(t, completer, func(cfg *Config) {
		cfg.ApprovalPosture = PostureAllow
	})
	defer agent.Close()

	for i := range agent.tools {
		if agent.tools[i].Name == "read" {
			orig := agent.tools[i].Execute
			agent.tools[i].Execute = func(ctx context.Context, args json.RawMessage) (string, bool, error) {
				executedOnPartialTree.Add(1)
				return orig(ctx, args)
			}
		}
	}

	arrival := cell.BeginWorkspaceArrival(workspace, workspace, func() time.Duration { return 40 * time.Millisecond }, nil)
	defer arrival.Complete()
	arrival.Fail(errors.New("sync could not be reached"))

	events, err := agent.Submit(t.Context(), "read partial.txt")
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	var toolErrText, finalText string
	for ev := range events {
		if ev.Kind == EventToolFailed || ev.Kind == EventToolEnd {
			toolErrText += ev.Output
		}
		if ev.Kind == EventTextDelta {
			finalText += ev.Text
		}
	}
	if finalText == "" {
		finalText = messageText(lastMessage(agent))
	}
	if executedOnPartialTree.Load() != 0 {
		t.Fatalf("tool executed %d times on a partial workspace tree; want 0", executedOnPartialTree.Load())
	}
	combined := toolErrText + "\n" + finalText
	if !strings.Contains(combined, "workspace files could not arrive") || !strings.Contains(combined, "sync could not be reached") {
		t.Fatalf("expected plain-words failure reason, got:\n%s", combined)
	}
}
