package plan

import (
	"strings"
	"testing"
)

// The whole bundle argument in one shape check: declared-independent requests
// share stage one with no edges between them, and the only thing after them is
// the synthesis that reads them all. The layout is geometry, not judgment — a
// measured spine run restated the independence rule and then chained the
// requests anyway, which is why no model is consulted here.
func TestABundleSharesOneStageAndNothingChains(t *testing.T) {
	parts := []string{
		"Fix the failing test in the fixtures repository and say what was wrong.",
		"Compute March revenue from orders.csv and name the top product.",
		"  ", // a blank part is refused structurally, never planned around
		"Draft the sponsorship decline email per the brief.",
	}
	graph := Bundle("Deliver three independent results.", parts)

	var workers, sinks int
	for _, node := range graph.Nodes {
		switch node.Kind {
		case KindWork:
			workers++
			if node.Stage != 1 {
				t.Errorf("part %q sits in stage %d, want 1", node.Title, node.Stage)
			}
			if len(node.Needs) != 0 {
				t.Errorf("part %q was given needs %v — a bundle has no cross-part edges", node.Title, node.Needs)
			}
			if node.Brief == "" || node.Summary == "" {
				t.Errorf("part %q is missing its own words", node.Title)
			}
		case KindSynthesis:
			sinks++
			if node.Stage != 2 {
				t.Errorf("the synthesis sits in stage %d, want 2", node.Stage)
			}
			if len(node.Needs) != 3 {
				t.Errorf("the synthesis reads %d parts, want 3", len(node.Needs))
			}
			if !strings.Contains(node.Brief, "in the order the person asked") {
				t.Errorf("the merge brief lost its ordering law: %q", node.Brief)
			}
			if !strings.Contains(node.Brief, "reconciling them is YOUR work") {
				t.Errorf("the merge brief lost its reconciliation law: %q", node.Brief)
			}
		}
	}
	if workers != 3 || sinks != 1 {
		t.Fatalf("bundle shape = %d workers, %d sinks; want 3 and 1", workers, sinks)
	}
	if sink := graph.deliverableSink(); sink == 0 {
		t.Fatal("the bundle has no deliverable owner for the contract and gate to hold")
	}
}

// The rail row this defect was reported from: twelve leaves of one enumerated
// bundle, every part opening on the same fifty-character instruction, the only
// word that told them apart sitting past the cut. Twelve identical rows.
//
// The property is not "the names are short" — the old code satisfied that and
// was useless. It is that a name still picks its part out from the ones beside
// it, which is what a rail is for.
func TestTwelveSiblingsSharingAStemGetTwelveDistinctNames(t *testing.T) {
	const stem = "Write a one-paragraph technical profile of the vector database "
	if len(stem) < 50 {
		t.Fatalf("the shared stem is %d characters; this test is not exercising the collision", len(stem))
	}
	databases := []string{
		"Milvus", "Weaviate", "Qdrant", "Pinecone", "Chroma", "pgvector",
		"Vespa", "Elasticsearch", "Redis", "LanceDB", "Marqo", "FAISS",
	}
	parts := make([]string, 0, len(databases))
	for _, database := range databases {
		parts = append(parts, stem+database+", covering its storage engine, its index types and what it is best at.")
	}

	graph := Bundle("Profile twelve vector databases.", parts)
	seen := make(map[string]string, len(databases))
	var workers int
	for _, node := range graph.Nodes {
		if node.Kind != KindWork {
			continue
		}
		workers++
		if first, clash := seen[node.Title]; clash {
			t.Fatalf("two rail rows read the same: %q\n  first:  %s\n  second: %s", node.Title, first, node.Summary)
		}
		seen[node.Title] = node.Summary
		if width := len([]rune(node.Title)); width > railWidth {
			t.Errorf("the rail row %q is %d characters wide, over the %d it has", node.Title, width, railWidth)
		}
	}
	if workers != len(databases) {
		t.Fatalf("the bundle laid %d parts, want %d", workers, len(databases))
	}
	// And the discriminating word is what survived — the name is worth nothing
	// otherwise, however distinct it is.
	for _, database := range databases {
		var found bool
		for title := range seen {
			if strings.Contains(title, database) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("no rail row names %s; the clip kept the stem and dropped the difference", database)
		}
	}
}

// A part that stands alone keeps its own opening words. The sibling-aware clip
// is a repair for a collision, not a new house style, so nothing changes for
// the ordinary bundle of unrelated asks.
func TestPartsThatDoNotCollideAreStillNamedByTheirOpeningWords(t *testing.T) {
	parts := []string{
		"Compute March revenue from orders.csv and name the top product by margin, then say why.",
		"Draft the sponsorship decline email per the brief, warm but final.",
	}
	names := clipTitles(parts)
	for index, name := range names {
		if !strings.HasPrefix(parts[index], name) {
			t.Errorf("part %d was renamed to %q rather than clipped from %q", index, name, parts[index])
		}
	}
}

// Two parts that really are the same words cannot be told apart by any clip, and
// the rail still may not show one row twice.
func TestIdenticalPartsStillGetOneRowEach(t *testing.T) {
	names := clipTitles([]string{"Summarise the report.", "Summarise the report."})
	if names[0] == names[1] {
		t.Fatalf("identical parts collapsed to one rail row: %q", names[0])
	}
}

// The probe shape, at the layer where the order is still recordable.
//
// A compile call declared four independent parts for "research three countries,
// then assemble a comparison brief": three researchers and, as a fourth peer,
// the assembler that reads all three. Bundle laid the four side by side with no
// edges — because that is what a bundle IS — and the assembler was claimable
// from the first tick. It ran beside its own inputs, invented the country
// section it was supposed to read, and shipped a brief with one of the three
// countries missing for good.
//
// Sequence is the check on the declaration the whole cheap route rests on.
func TestSequencePutsTheAssemblerBehindTheRequestsItWorksOver(t *testing.T) {
	graph := Bundle("Compare how three countries measure road distance.", []string{
		"Research and write the section for the first country, France.",
		"Research and write the section for the second country, Germany.",
		"Research and write the section for the third country, Australia.",
		"Assemble the three country sections into a comparison brief.",
	})
	client := &stubClient{reply: func(system, _ string) string {
		if !strings.Contains(system, "which of these requests must wait") {
			t.Errorf("an unexpected pass was called with: %s", system)
			return "{}"
		}
		return `{"waits":[{"request":1,"after":[]},{"request":2,"after":[]},
		         {"request":3,"after":[]},{"request":4,"after":[1,2,3]}]}`
	}}

	if _, err := Sequence(t.Context(), client, graph); err != nil {
		t.Fatalf("sequence: %v", err)
	}

	assembler := graph.Node(4)
	if assembler == nil {
		t.Fatal("the assembler left the graph")
	}
	for _, researcher := range []int{1, 2, 3} {
		if !contains(assembler.Needs, researcher) {
			t.Fatalf("the assembler waits for %v, not for %d — it can start beside its own input",
				assembler.Needs, researcher)
		}
		if node := graph.Node(researcher); len(node.Needs) != 0 {
			t.Errorf("researcher %d was chained behind %v; the three stand alone", researcher, node.Needs)
		}
	}
	// The synthesis still reads every part. It is the delivery, and nothing this
	// pass answers may narrow what it waits for.
	sink := graph.Node(5)
	if sink == nil || len(sink.Needs) != 4 {
		t.Fatalf("the synthesis reads %v, want all four parts", sink.Needs)
	}
}

// The ordinary bundle is the one this pass must not damage: unrelated asks stay
// unchained, and the person waits for the longest of them rather than the sum.
func TestSequenceLeavesIndependentRequestsUnchained(t *testing.T) {
	graph := Bundle("Three unrelated things.", []string{
		"Fix the failing test in the fixtures repository.",
		"Compute March revenue from orders.csv.",
		"Draft the sponsorship decline email.",
	})
	client := &stubClient{reply: func(string, string) string {
		return `{"waits":[{"request":1,"after":[]},{"request":2,"after":[]},{"request":3,"after":[]}]}`
	}}
	if _, err := Sequence(t.Context(), client, graph); err != nil {
		t.Fatalf("sequence: %v", err)
	}
	for _, id := range []int{1, 2, 3} {
		if node := graph.Node(id); len(node.Needs) != 0 {
			t.Errorf("part %d was chained behind %v", id, node.Needs)
		}
	}
}

// Two answers that must not take the job down with them: a mutual wait, which
// would be an unsplicable cycle, and ids the model invented. Both have to leave
// a graph that still runs.
func TestSequenceSurvivesACycleAndInventedIDs(t *testing.T) {
	graph := Bundle("Two things.", []string{"First thing.", "Second thing."})
	client := &stubClient{reply: func(string, string) string {
		return `{"waits":[{"request":1,"after":[2]},{"request":2,"after":[1]},{"request":9,"after":[41]}]}`
	}}
	if _, err := Sequence(t.Context(), client, graph); err != nil {
		t.Fatalf("sequence: %v", err)
	}
	if graph.hasCycle() {
		t.Fatal("a mutual wait was wired as a cycle; the splice would refuse the whole job")
	}
	if graph.Node(9) != nil || graph.Node(41) != nil {
		t.Fatal("an invented id became a node")
	}
}
