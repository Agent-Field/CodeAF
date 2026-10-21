package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/consent"
	tools "github.com/Agent-Field/codeaf/internal/exec"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/resident"
	"github.com/Agent-Field/codeaf/internal/store"
)

// C18 requires real resident work and housekeeping to run shell tools without
// the package's host-seat fallback masking a missing production context.
func TestC18ResidentRootsRunToolsWithoutTestFallback(t *testing.T) {
	if os.Getenv("CODEAF_TEST_NO_HOST_SEAT") != "1" {
		child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestC18ResidentRootsRunToolsWithoutTestFallback$", "-test.v")
		child.Env = append(os.Environ(), "CODEAF_TEST_NO_HOST_SEAT=1")
		if out, err := child.CombinedOutput(); err != nil {
			t.Fatalf("resident without fallback: %v\n%s", err, out)
		}
		return
	}
	t.Setenv("CODEAF_CELLS", "0")
	t.Setenv("CODEAF_RTK", "off")
	workspace := t.TempDir()
	if _, err := executor.For(context.Background()).In(workspace).Exec(t.Context(), executor.ExecRequest{Argv: []string{"true"}}, nil); !errors.Is(err, executor.ErrNoSeat) {
		t.Fatalf("the test fallback is still active: %v", err)
	}
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	space, err := tools.NewWorkspace(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{{ID: "shell", Brief: "run shell", Stage: 1}}}, store.Provenance{Origin: store.OriginUser, Intent: "run shell"}); err != nil {
		t.Fatal(err)
	}
	if _, err := graph.RequestCommand(store.Command{SessionID: "resident", Kind: store.CommandSplice, Instruction: "check housekeeping", Deliberate: true}); err != nil {
		t.Fatal(err)
	}
	done := make(chan string, 2)
	runTool := func(ctx context.Context, label string) {
		tool := tools.NewToolbox(space, label, nil)
		result := tool.Execute(ctx, "sh", `{"cmd":"printf resident-seat"}`)
		if result.IsError || !strings.Contains(result.Content, "resident-seat") {
			done <- fmt.Sprintf("%s: %+v", label, result)
			return
		}
		done <- ""
	}
	runner := resident.NewRunner(graph, func(ctx context.Context, _ store.Node) (resident.ExecResult, error) {
		runTool(ctx, "work")
		return resident.ExecResult{Summary: "done"}, nil
	}, "resident", 1)
	reconciler := resident.New(graph, func(ctx context.Context, _, _ string) (resident.Compiled, error) {
		runTool(ctx, "housekeeping")
		return resident.Compiled{}, errors.New("the fixture needs no new plan")
	}, nil)
	brain := &chatBrain{window: &chatWindow{graph: graph}, session: "resident", runner: runner, reconciler: reconciler, consent: consent.NewDesk(graph)}
	brain.start()
	defer brain.stop()
	for range 2 {
		select {
		case result := <-done:
			if result != "" {
				t.Error(result)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("resident did not reach both real tool paths")
		}
	}
}
