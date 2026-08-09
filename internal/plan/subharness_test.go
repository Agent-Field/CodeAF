package plan

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The first law: with only the baseline registered, the sizing pass is what it
// was before subharnesses existed — not equivalent, not compatible, the same
// bytes. The goldens were captured from the commit before this feature, so a
// stray space added to the prompt while adding a specialist section fails here
// rather than in a month of rerouted plans.
func TestBaselineSizingPromptAndSchemaAreByteIdentical(t *testing.T) {
	if got := len(Subharnesses()); got != 0 {
		t.Fatalf("a test registered a specialist and did not put it back: %d registered", got)
	}
	want, err := os.ReadFile("testdata/size_prompt_baseline.golden")
	if err != nil {
		t.Fatal(err)
	}
	if got := sizePromptWith(Anchors()); got != string(want) {
		t.Fatalf("sizing prompt drifted from its pre-subharness bytes:\n%s", diffLine(got, string(want)))
	}
	schema, err := os.ReadFile("testdata/size_schema_baseline.golden")
	if err != nil {
		t.Fatal(err)
	}
	if got := string(sizeSchemaFor(Subharnesses())); got != string(schema) {
		t.Fatalf("sizing schema drifted from its pre-subharness bytes:\n%s", diffLine(got, string(schema)))
	}
}

func TestRegisteredSpecialistReachesPromptAndSchema(t *testing.T) {
	defer forgetSubharnesses()
	UseSubharness(Subharness{Name: "swe", Purpose: "software engineering taken whole"}, "SWE RULER: three worked examples.")

	prompt := sizePromptWith(Anchors())
	for _, want := range []string{"swe — software engineering taken whole", "SWE RULER", "Also return subharness for every node"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("sizing prompt is missing %q", want)
		}
	}
	var schema struct {
		Properties struct {
			Sizes struct {
				Items struct {
					Properties map[string]struct {
						Enum []string `json:"enum"`
					} `json:"properties"`
					Required []string `json:"required"`
				} `json:"items"`
			} `json:"sizes"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(sizeSchemaFor(Subharnesses()), &schema); err != nil {
		t.Fatalf("schema does not parse: %v", err)
	}
	field, ok := schema.Properties.Sizes.Items.Properties["subharness"]
	if !ok {
		t.Fatal("schema has no subharness field")
	}
	if len(field.Enum) != 2 || field.Enum[0] != "" || field.Enum[1] != "swe" {
		t.Fatalf("subharness enum = %v, want [\"\" swe]", field.Enum)
	}
}

// A named specialist is the other half of the verdict: the node becomes atomic
// for it and stops decomposing. An unregistered name degrades to the baseline
// rather than failing — Registry.For's promise, made this far upstream.
func TestSizeApplyHonorsAndDegradesSubharnessVerdicts(t *testing.T) {
	defer forgetSubharnesses()
	UseSubharness(Subharness{Name: "swe", Purpose: "coding"}, "ruler")

	graph := &Graph{Nodes: []Node{
		{ID: 1, Kind: KindWork, Stage: 1},
		{ID: 2, Kind: KindWork, Stage: 1},
	}, NextID: 3}
	_, err := sizeApply(graph, []sizeResult{{verdicts: []sizeVerdict{
		{Node: 1, Size: "oversized", Parts: []string{"one part", "two part"}, Subharness: "swe"},
		{Node: 2, Size: "oversized", Parts: []string{"one part", "two part"}, Subharness: "reviewer"},
	}}})
	if err != nil {
		t.Fatalf("sizeApply: %v", err)
	}
	taken := graph.Node(1)
	if taken.Subharness != "swe" || taken.Size != SizeAtomic || len(taken.Parts) != 0 {
		t.Fatalf("node 1 = %+v, want swe/atomic with no parts", *taken)
	}
	unknown := graph.Node(2)
	if unknown.Subharness != "" || unknown.Size != SizeOversized || len(unknown.Parts) != 2 {
		t.Fatalf("node 2 = %+v, want the baseline verdict left untouched", *unknown)
	}
}

func TestAnchorsAreKeptPerSubharness(t *testing.T) {
	defer forgetSubharnesses()
	defer UseAnchors("")
	UseSubharness(Subharness{Name: "swe", Purpose: "coding"}, "the swe prior")

	if got := AnchorsFor("swe"); got != "the swe prior" {
		t.Fatalf("registered prior = %q", got)
	}
	UseAnchors("a measured linear ruler")
	UseAnchorsFor("swe", "a measured swe ruler")
	if got := Anchors(); got != "a measured linear ruler" {
		t.Fatalf("linear ruler = %q", got)
	}
	if got := AnchorsFor("swe"); got != "a measured swe ruler" {
		t.Fatalf("swe ruler = %q", got)
	}
	// An empty string restores that subharness's own prior and nobody else's.
	UseAnchorsFor("swe", "")
	if got := AnchorsFor("swe"); got != "the swe prior" {
		t.Fatalf("restored swe ruler = %q", got)
	}
	if got := Anchors(); got != "a measured linear ruler" {
		t.Fatalf("linear ruler moved when swe was restored: %q", got)
	}
	// The empty name is linear everywhere, and a stranger has no ruler at all.
	if AnchorsFor("") != Anchors() {
		t.Fatal("the empty subharness is not linear")
	}
	if got := AnchorsFor("nobody"); got != "" {
		t.Fatalf("an unregistered subharness has a ruler: %q", got)
	}
}

// The subharness has to survive the round trip through the file a graph is
// persisted to, and the splice that replaces a node with its own decomposition.
func TestSubharnessSurvivesJSONAndSplice(t *testing.T) {
	graph := &Graph{Goal: "g", Nodes: []Node{
		{ID: 1, Kind: KindWork, Stage: 1, Title: "t", State: StatePending, Subharness: "swe"},
	}, NextID: 2}
	encoded, err := graph.JSON()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"subharness": "swe"`) {
		t.Fatalf("subharness is not in the persisted graph:\n%s", encoded)
	}
	loaded, err := Load(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Node(1).Subharness != "swe" {
		t.Fatalf("subharness lost on load: %+v", *loaded.Node(1))
	}

	sub := &Graph{Nodes: []Node{
		{ID: 1, Kind: KindWork, Stage: 1, Title: "child", State: StatePending, Subharness: "swe"},
		{ID: 2, Kind: KindSynthesis, Stage: 1, Title: "sink", State: StatePending, Needs: []int{1}},
	}, NextID: 3}
	if err := loaded.Splice(1, sub); err != nil {
		t.Fatalf("splice: %v", err)
	}
	found := false
	for _, node := range loaded.Nodes {
		if node.Parent == 1 && node.Subharness == "swe" {
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
