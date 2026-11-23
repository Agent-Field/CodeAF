package session

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
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

// Round 5b (2): a crashed owner in another process must not stall workspace
// tools; a waiter in this process ignores and removes a marker whose owner PID
// is dead instead of waiting out the 2-minute stall bound.
func TestC3CrashedArrivalOwnerDoesNotStallWorkspaceTools(t *testing.T) {
	if cellRoot := os.Getenv("CODEAF_TEST_C3_CRASH_CELL"); cellRoot != "" {
		workDir := os.Getenv("CODEAF_TEST_C3_CRASH_WORK")
		_ = cell.BeginWorkspaceArrival(cellRoot, workDir, nil, nil)
		os.Exit(0)
	}

	completer := &scriptedCompleter{
		steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call_read_crash", "read", `{"path":"survived.txt"}`), nil
			},
			func(_ context.Context, msgs []ai.Message) (*ai.Response, error) {
				last := msgs[len(msgs)-1]
				return textResponse("read saw: " + strings.TrimSpace(messageText(last))), nil
			},
		},
	}

	cellRoot := filepath.Join(t.TempDir(), "v3", "cells", "c3-crash-cell")
	if err := os.MkdirAll(filepath.Join(cellRoot, cell.StateDir), 0o755); err != nil {
		t.Fatal(err)
	}

	agent, workspace := newTestAgent(t, completer, func(cfg *Config) {
		cfg.ApprovalPosture = PostureAllow
		cfg.Place = Place{Dir: cellRoot}
	})
	defer agent.Close()

	if err := os.WriteFile(filepath.Join(workspace, "survived.txt"), []byte("tool ran after owner crash\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestC3CrashedArrivalOwnerDoesNotStallWorkspaceTools$")
	cmd.Env = append(os.Environ(),
		"CODEAF_TEST_C3_CRASH_CELL="+cellRoot,
		"CODEAF_TEST_C3_CRASH_WORK="+workspace,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child process failed: %v\n%s", err, out)
	}

	marker := cell.ArrivalMarkerPath(cellRoot)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected crashed child to leave arrival marker at %s: %v", marker, err)
	}

	start := time.Now()
	events, err := agent.Submit(t.Context(), "read survived.txt")
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	var finalText string
	for ev := range events {
		if ev.Kind == EventTextDelta {
			finalText += ev.Text
		}
	}
	if finalText == "" {
		finalText = messageText(lastMessage(agent))
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("workspace tool stalled for %s after owner crash; expected immediate execution", elapsed)
	}
	if !strings.Contains(finalText, "tool ran after owner crash") {
		t.Fatalf("unexpected tool output: %q", finalText)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("expected dead owner's marker %s to be removed, stat err = %v", marker, err)
	}
}

// Fail closed: the process that was bringing a chat's files fails, then goes
// away (the person closed its window). The workspace is a partial tree and no
// retry is coming, so a tool in another process must never run on it, and the
// person is told in plain words what happened and what to do. Before this the
// dead owner's marker was removed and the tool ran on the partial tree.
func TestC3WorkspaceToolNeverRunsAfterTheOwnerOfAFailedArrivalDied(t *testing.T) {
	if cellRoot := os.Getenv("CODEAF_TEST_C3_FAILED_CELL"); cellRoot != "" {
		workDir := os.Getenv("CODEAF_TEST_C3_FAILED_WORK")
		a := cell.BeginWorkspaceArrival(cellRoot, workDir, nil, nil)
		a.Fail(errors.New("sync could not be reached"))
		os.Exit(0)
	}

	var executedOnPartialTree atomic.Int32
	completer := &scriptedCompleter{
		steps: []step{
			func(context.Context, []ai.Message) (*ai.Response, error) {
				return toolResponse("call_read_abandoned", "read", `{"path":"partial.txt"}`), nil
			},
			func(_ context.Context, msgs []ai.Message) (*ai.Response, error) {
				last := msgs[len(msgs)-1]
				return textResponse("tool reported: " + strings.TrimSpace(messageText(last))), nil
			},
		},
	}
	cellRoot := filepath.Join(t.TempDir(), "v3", "cells", "c3-abandoned-cell")
	if err := os.MkdirAll(filepath.Join(cellRoot, cell.StateDir), 0o755); err != nil {
		t.Fatal(err)
	}
	agent, workspace := newTestAgent(t, completer, func(cfg *Config) {
		cfg.ApprovalPosture = PostureAllow
		cfg.Place = Place{Dir: cellRoot}
	})
	defer agent.Close()
	if err := os.WriteFile(filepath.Join(workspace, "partial.txt"), []byte("half a tree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := range agent.tools {
		if agent.tools[i].Name == "read" {
			orig := agent.tools[i].Execute
			agent.tools[i].Execute = func(ctx context.Context, args json.RawMessage) (string, bool, error) {
				executedOnPartialTree.Add(1)
				return orig(ctx, args)
			}
		}
	}

	cmd := exec.Command(os.Args[0], "-test.run=^TestC3WorkspaceToolNeverRunsAfterTheOwnerOfAFailedArrivalDied$")
	cmd.Env = append(os.Environ(),
		"CODEAF_TEST_C3_FAILED_CELL="+cellRoot,
		"CODEAF_TEST_C3_FAILED_WORK="+workspace,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child process failed: %v\n%s", err, out)
	}
	marker := cell.ArrivalMarkerPath(cellRoot)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected the failed arrival's marker at %s: %v", marker, err)
	}

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
		t.Fatalf("tool executed %d times on a partial workspace tree after its fetch failed and its owner left; want 0", executedOnPartialTree.Load())
	}
	combined := toolErrText + "\n" + finalText
	for _, want := range []string{"workspace files could not arrive", "sync could not be reached", "move the chat here again"} {
		if !strings.Contains(combined, want) {
			t.Fatalf("expected %q in what the person is told, got:\n%s", want, combined)
		}
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the failure must outlive its owner: %v", err)
	}
}
