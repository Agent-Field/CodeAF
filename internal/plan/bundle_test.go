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
