package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/resident"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui"
)

// The two capabilities the task page asks a commander for. Both are optional
// interfaces, so a missing method is a silently dead key rather than a build
// failure — which is exactly what this stops.
var (
	_ tui.Restarter   = (*chatCommander)(nil)
	_ tui.SurgeryGate = (*chatCommander)(nil)
)

// The whole journey the restart was built for and never actually had: the user
// stops a worker mid-turn, what it had already written survives the landing,
// and the retry the restart splices is handed those words instead of "(not
// run): cancelled by user" — which is what it used to read, while the
// half-finished file sat on disk beside it.
func TestCancellingAWorkerLeavesThePartialForTheRestartToRead(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "cancel-restart.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()

	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "write the report", Title: "Report"},
		{ID: "job-n1", Parent: "job", Brief: "draft the findings", Title: "Draft"},
	}}, store.Provenance{
		Origin: store.OriginUser, SessionID: "journey", Intent: "write the report",
	}); err != nil {
		t.Fatal(err)
	}

	partial := "wrote sections one and two to /tmp/report.md; section three never started"
	// The cancel arrives while the worker is mid-turn, which is the only moment
	// the partial exists: the executor observes it at its next boundary and
	// hands back what it had.
	runner := resident.NewRunner(graph, func(_ context.Context, node store.Node) (resident.ExecResult, error) {
		if err := graph.RequestNodeCancel(node.ID, store.UserCancelReason); err != nil {
			return resident.ExecResult{}, err
		}
		return resident.ExecResult{Summary: partial}, nil
	}, "journey-runner", 1)
	if _, err := runner.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	runner.Wait()

	cancelled, found, err := graph.Node("job-n1")
	if err != nil || !found {
		t.Fatalf("leaf after cancel: found=%v err=%v", found, err)
	}
	if cancelled.Status != store.Cancelled {
		t.Fatalf("cancelled leaf = %+v", cancelled)
	}
	if cancelled.Summary != partial {
		t.Fatalf("the partial did not survive the landing: %q", cancelled.Summary)
	}

	// Restart, through the same durable command a sentence would journal.
	commander := &chatCommander{store: graph, sessionID: "journey"}
	if err := commander.Restart("job-n1"); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	reconciler := resident.New(graph, nil, nil)
	if err := reconciler.Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	nodes, err := graph.Nodes()
	if err != nil {
		t.Fatal(err)
	}
	retry := ""
	for _, node := range nodes {
		if node.Provenance.RetryOf == "job-n1" {
			retry = node.ID
		}
	}
	if retry == "" {
		t.Fatalf("the restart spliced no retry: %+v", nodes)
	}
	inputs, err := graph.DependencyInputs(retry, 4096)
	if err != nil {
		t.Fatal(err)
	}
	carried := ""
	for _, input := range inputs {
		if input.NodeID == "job-n1" {
			carried = input.Digest
		}
	}
	if carried == "" {
		t.Fatalf("the retry was not wired to the attempt it replaces: %+v", inputs)
	}
	if !strings.Contains(carried, "cancelled midway") || !strings.Contains(carried, "sections one and two") {
		t.Fatalf("the retry was handed nothing to read: %q", carried)
	}
	if strings.Contains(carried, "(not run)") {
		t.Fatalf("a worker that got halfway is described as never having run: %q", carried)
	}
}

// One mouth: the key path's restart is the same typed command, on the same
// journal, that "restart that" produces.
func TestTheCommanderRestartJournalsTheDurableRestartCommand(t *testing.T) {
	graph, err := store.Open(filepath.Join(t.TempDir(), "restart-command.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "job", Brief: "ship it", Title: "Ship"},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "keys", Intent: "ship it"}); err != nil {
		t.Fatal(err)
	}
	if err := graph.CancelPendingWithPartial("job", store.UserCancelReason, "half of it"); err != nil {
		t.Fatal(err)
	}
	commander := &chatCommander{store: graph, sessionID: "keys"}
	if err := commander.Restart("job"); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	commands, err := graph.PendingCommands(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(commands) != 1 || commands[0].Kind != store.CommandRestart || commands[0].Target != "job" {
		t.Fatalf("journalled commands = %+v", commands)
	}
	if commands[0].SessionID != "keys" {
		t.Fatalf("the restart was journalled into session %q", commands[0].SessionID)
	}
}
