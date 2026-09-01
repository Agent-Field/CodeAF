package plan

import (
	"os"
	"strings"
	"testing"
)

// The sizing pass is the one place capacity is judged, and its prompt and
// schema are pinned byte for byte. The goldens were captured before a second
// worker was ever added to this tree and they are what the pass emits again: a
// stray space, a re-worded sentence or a re-ordered schema key fails here rather
// than in a month of differently-shaped plans.
func TestBaselineSizingPromptAndSchemaAreByteIdentical(t *testing.T) {
	want, err := os.ReadFile("testdata/size_prompt_baseline.golden")
	if err != nil {
		t.Fatal(err)
	}
	if got := sizePromptWith(Anchors()); got != string(want) {
		t.Fatalf("the sizing prompt drifted from its pinned bytes:\n%s", diffLine(got, string(want)))
	}
	schema, err := os.ReadFile("testdata/size_schema_baseline.golden")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(sizeSchema); got != string(schema) {
		t.Fatalf("the sizing schema drifted from its pinned bytes:\n%s", diffLine(got, string(schema)))
	}
}

// The two predicates answer two different questions about one column, and the
// difference is load-bearing: "linear" is the worker written down, and empty is
// a column nobody wrote. Only the second may be filled in from elsewhere.
func TestTheWorkerNamedAndTheColumnUnwrittenAreDifferentFacts(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		generalist bool
		chosen     bool
	}{
		{name: LinearSubharness, generalist: true, chosen: true},
		{name: " linear ", generalist: true, chosen: true},
		{name: "", generalist: false, chosen: false},
		{name: "   ", generalist: false, chosen: false},
		{name: "a-worker-this-build-does-not-have", generalist: false, chosen: true},
	} {
		if got := GeneralistSubharness(testCase.name); got != testCase.generalist {
			t.Errorf("GeneralistSubharness(%q) = %v", testCase.name, got)
		}
		if got := SubharnessChosen(testCase.name); got != testCase.chosen {
			t.Errorf("SubharnessChosen(%q) = %v", testCase.name, got)
		}
	}
}

// The column survives the file the graph is persisted to and the splice that
// admits a subtree, whatever it says. A graph written by an older build names
// what it named, and it must come back out of the file naming it — that is what
// lets the surface reading it say once that this build runs the work anyway.
func TestSubharnessSurvivesJSONAndSplice(t *testing.T) {
	const stored = "a-worker-this-build-does-not-have"
	graph := &Graph{Goal: "g", Nodes: []Node{
		{ID: 1, Kind: KindWork, Stage: 1, Title: "t", State: StatePending, Subharness: stored},
	}, NextID: 2}
	encoded, err := graph.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"subharness": "`+stored+`"`) {
		t.Fatalf("subharness is not in the persisted graph:\n%s", encoded)
	}
	loaded, err := Load(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Node(1).Subharness != stored {
		t.Fatalf("subharness lost on load: %+v", *loaded.Node(1))
	}

	sub := &Graph{Nodes: []Node{
		{ID: 1, Kind: KindWork, Stage: 1, Title: "child", State: StatePending, Subharness: stored},
		{ID: 2, Kind: KindSynthesis, Stage: 1, Title: "sink", State: StatePending, Needs: []int{1}},
	}, NextID: 3}
	if err := loaded.Splice(1, sub); err != nil {
		t.Fatalf("splice: %v", err)
	}
	found := false
	for _, node := range loaded.Nodes {
		if node.Parent == 1 && node.Subharness == stored {
			found = true
		}
	}
	if !found {
		t.Fatal("no spliced child carried its subharness")
	}
}

func diffLine(got, want string) string {
	gotLines, wantLines := strings.Split(got, "\n"), strings.Split(want, "\n")
	for index := 0; index < len(gotLines) && index < len(wantLines); index++ {
		if gotLines[index] != wantLines[index] {
			return "line " + itoa(index+1) + ":\n  got:  " + gotLines[index] + "\n  want: " + wantLines[index]
		}
	}
	return "got " + itoa(len(gotLines)) + " lines, want " + itoa(len(wantLines))
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits []byte
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
