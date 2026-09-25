package plan

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/store"
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

// skillsBriefClient scripts a build whose brief instructions carry fixed cue
// words, so retrieval has something deterministic to match against. Every pass
// but the brief call goes to passClient, exactly as briefJournalClient does.
type skillsBriefClient struct{ instruction string }

func (c *skillsBriefClient) CompleteWithMessages(ctx context.Context, messages []ai.Message, options ...ai.Option) (*ai.Response, error) {
	var system, target string
	for _, m := range messages {
		text := textOf(m)
		if m.Role == "system" {
			system = text
			continue
		}
		target = text
	}
	if system == briefPrompt || system == briefWithCriterion {
		var id int
		fmt.Sscanf(target, "Write the instruction for node %d,", &id)
		return response(fmt.Sprintf(`{"instruction":"`+fmt.Sprintf(c.instruction, id)+`",`+
			`"done":{"produces":["the sorted list of node %d"],`+
			`"conditions":[{"kind":"run","check":"the sort for node %d runs","expect":"it reports success"}]}}`,
			id, id)), nil
	}
	return (&passClient{}).CompleteWithMessages(ctx, messages, options...)
}

// shelfFixture is a two-skill shelf: one whose doc line shares two words with
// every brief this client writes, and one that shares only a stop word.
func shelfFixture() []store.Fact {
	return []store.Fact{
		{Artifact: "/skills/imgshrink", Body: "optimize images without losing quality"},
		{Artifact: "/skills/lint", Body: "gofmt vet and lint the tree"},
	}
}

// journalledBriefs runs a build with the shelf wired in and journals every
// node_briefed event into a real store, returning what the store holds by
// store id — the same id law cmd's briefJournal uses.
func journalledBriefs(t *testing.T, goal, instruction string, facts []store.Fact) (map[string]store.NodeBrief, *Graph, *store.Store) {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	const prefix = "job"
	storeID := func(graph *Graph, nodeID int) string {
		if nodeID == graph.deliverableSink() {
			return prefix
		}
		return fmt.Sprintf("%s-n%d", prefix, nodeID)
	}
	journal := func(graph *Graph, nodeID int, brief store.NodeBrief) {
		if err := db.RecordNodeBrief(storeID(graph, nodeID), brief); err != nil {
			t.Errorf("journal brief for %s: %v", storeID(graph, nodeID), err)
		}
	}

	graph, err := Build(context.Background(), &skillsBriefClient{instruction: instruction}, goal, Options{
		Ensemble:     EnsembleNever,
		SpineSamples: 1,
		NodeBudget:   20,
		Briefs:       true,
		Journal:      journal,
		Skills:       facts,
	})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}

	briefs := map[string]store.NodeBrief{}
	for _, id := range graph.writtenLeaves() {
		sid := storeID(graph, id)
		got, ok, err := db.BriefFor(sid)
		if err != nil {
			t.Fatalf("BriefFor %s: %v", sid, err)
		}
		if !ok {
			t.Fatalf("no node_briefed event for %s", sid)
		}
		briefs[sid] = got
	}
	if len(briefs) == 0 {
		t.Fatal("the build produced no briefed leaves")
	}
	return briefs, graph, db
}

// TestAProposalNamingASkillAttachesItFirst is the pinned half of the
// attachment: a goal that names a shelf skill attaches it, first in the order,
// on every briefed leaf — and the same order is what the journal holds.
func TestAProposalNamingASkillAttachesItFirst(t *testing.T) {
	briefs, graph, _ := journalledBriefs(t, "shrink the report images with imgshrink",
		"Sort the report images by hue and optimize the order for node %d.", shelfFixture())
	for sid, brief := range briefs {
		node := graph.Node(brief.Node)
		if node == nil {
			t.Fatalf("%s: node %d vanished from the built graph", sid, brief.Node)
		}
		if want := []string{"imgshrink"}; !reflect.DeepEqual(brief.Skills, want) {
			t.Errorf("%s: journalled skills = %q, want %q", sid, brief.Skills, want)
		}
		if !reflect.DeepEqual(node.Skills, brief.Skills) {
			t.Errorf("%s: node skills %q and journalled skills %q disagree", sid, node.Skills, brief.Skills)
		}
	}
}

// TestAProposalNamingNothingAttachesNothing is the zero half: a goal that
// names no skill, over a shelf, attaches nothing anywhere — the node brief
// carries no skills field and the worker prompt would render zero bytes.
func TestAProposalNamingNothingAttachesNothing(t *testing.T) {
	briefs, graph, _ := journalledBriefs(t, "review the pull request and deliver REVIEW.md",
		"Write the summary for node %d, list what was checked, and hand back the result.", shelfFixture())
	for sid, brief := range briefs {
		node := graph.Node(brief.Node)
		if node == nil {
			t.Fatalf("%s: node %d vanished from the built graph", sid, brief.Node)
		}
		if len(brief.Skills) != 0 || len(node.Skills) != 0 {
			t.Errorf("%s: journalled skills %q, node skills %q, want nothing attached",
				sid, brief.Skills, node.Skills)
		}
	}
}

// TestRetrievedCandidatesFollowPinned is the order half: a goal that names one
// skill and a brief whose territory cues another compose pinned first and the
// retrieved candidate behind it, in that order, in the journal.
func TestRetrievedCandidatesFollowPinned(t *testing.T) {
	briefs, graph, _ := journalledBriefs(t, "lint the tree with lint",
		"Sort the report images by hue and optimize the order for node %d.", shelfFixture())
	want := []string{"lint", "imgshrink"}
	for sid, brief := range briefs {
		node := graph.Node(brief.Node)
		if node == nil {
			t.Fatalf("%s: node %d vanished from the built graph", sid, brief.Node)
		}
		if !reflect.DeepEqual(brief.Skills, want) {
			t.Errorf("%s: journalled skills = %q, want %q", sid, brief.Skills, want)
		}
		if !reflect.DeepEqual(node.Skills, want) {
			t.Errorf("%s: node skills = %q, want %q", sid, node.Skills, want)
		}
	}
}
