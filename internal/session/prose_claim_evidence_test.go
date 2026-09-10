package session

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// CRITICAL: finding quoted text cannot establish what the surrounding prose
// claims about it. Negated deletion must not invent a repair obligation.
func TestReportWordingCannotInventARepairObligation(t *testing.T) {
	repo := newTestRepo(t)
	tree, err := prepareTaskTree(Place{}, repo, "aaaabbbbccccdddd", 1, "prepare the release checklist")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(tree.dir, "notes.txt"), "release checklist\nAll entries are retained.\n")
	completer := &routedCompleter{audit: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("VERIFIED — the checklist retains its entries"), nil
		},
	}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.Workspace = repo })
	node := loneTestNode(t, "prepare the release checklist")
	node.graph.mu.Lock()
	node.spec.acceptance = "the release checklist is present and its entries are retained"
	node.graph.mu.Unlock()
	verdict := agent.auditNode(context.Background(), node, tree, []string{"notes.txt"},
		"Read `release checklist` (no entries were deleted).", io.Discard)
	if !verdict.answered || !verdict.verified {
		t.Fatalf("report wording invented a repair obligation: %s", verdict.report())
	}
	if completer.auditCalls() == 0 {
		t.Fatal("the report's meaning was decided without the checker")
	}
}
