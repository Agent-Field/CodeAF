package plan

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// briefJournalClient scripts a build that writes briefs. Every planning pass
// the build runs is answered by passClient; the brief pass — the one system
// prompt passClient does not handle — is answered here, with a per-node
// instruction and a criterion, so the journal hook can be exercised against a
// real store without a network.
type briefJournalClient struct{}

func (c *briefJournalClient) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var system, target string
	for _, m := range messages {
		text := textOf(m)
		if m.Role == "system" {
			system = text
			continue
		}
		// The brief call's last user message is the per-node target; it opens
		// with "Write the instruction for node N, ...". Earlier user messages
		// are the shared catalog, so only the last one is the target.
		target = text
	}
	if system == briefPrompt || system == briefWithCriterion {
		var id int
		fmt.Sscanf(target, "Write the instruction for node %d,", &id)
		return response(fmt.Sprintf(`{"instruction":"Do node %d and hand back its result.",`+
			`"done":{"produces":["the result of node %d"],`+
			`"conditions":[{"kind":"run","check":"the command for node %d runs","expect":"it reports success"}]}}`,
			id, id, id)), nil
	}
	return (&passClient{}).CompleteWithMessages(ctx, messages, options...)
}

// TestBriefsAreJournaledPerNode is the falsifiability test for this wave:
// after a plan build with briefs, the store holds one node_briefed event per
// briefed node, each carrying the exact sufficiency sentence the brief pass
// wrote. Before this, the sentence lived only as a field inside the single
// plan_graph blob, so a run's stopping condition was answerable only by
// re-reading the whole plan and finding the node inside it.
func TestBriefsAreJournaledPerNode(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer db.Close()

	const prefix = "job"
	// The id law the cmd uses (resident.PlanStoreIDs): the bare prefix for the
	// deliverable sink, "<prefix>-n<id>" otherwise. This test lives in package
	// plan, which resident imports, so the law is spelled here rather than
	// imported — and the spellings it produces are the ones the store reads.
	storeID := func(graph *Graph, nodeID int) string {
		if nodeID == graph.deliverableSink() {
			return prefix
		}
		return fmt.Sprintf("%s-n%d", prefix, nodeID)
	}
	var journalled int
	journal := func(graph *Graph, nodeID int, brief store.NodeBrief) {
		if err := db.RecordNodeBrief(storeID(graph, nodeID), brief); err != nil {
			t.Errorf("journal brief for %s: %v", storeID(graph, nodeID), err)
			return
		}
		journalled++
	}

	graph, err := Build(context.Background(), &briefJournalClient{},
		"review the pull request and deliver REVIEW.md", Options{
			Ensemble:     EnsembleNever,
			SpineSamples: 1,
			NodeBudget:   20,
			Briefs:       true,
			Journal:      journal,
		})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	leaves := graph.writtenLeaves()
	if len(leaves) == 0 {
		t.Fatal("the build produced no briefed leaves")
	}
	if journalled != len(leaves) {
		t.Fatalf("journalled %d node_briefed events, want one per briefed node (%d)", journalled, len(leaves))
	}
	for _, id := range leaves {
		node := graph.Node(id)
		if node == nil {
			t.Fatalf("node %d vanished from the built graph", id)
		}
		sid := storeID(graph, id)
		got, ok, err := db.BriefFor(sid)
		if err != nil {
			t.Fatalf("BriefFor %s: %v", sid, err)
		}
		if !ok {
			t.Fatalf("no node_briefed event for %s", sid)
		}
		if got.Node != id {
			t.Errorf("%s: payload node = %d, want %d", sid, got.Node, id)
		}
		if got.Brief != node.Brief {
			t.Errorf("%s: brief = %q, want %q", sid, got.Brief, node.Brief)
		}
		// The sufficiency sentence is the whole point: it must be the exact
		// rendered criterion, falsifiable from the artifact alone.
		if want := node.Spec.Done.Sentence(); got.Criterion != want {
			t.Errorf("%s: criterion = %q, want %q", sid, got.Criterion, want)
		}
		if got.Subharness != node.Subharness {
			t.Errorf("%s: subharness = %q, want %q", sid, got.Subharness, node.Subharness)
		}
	}
}
