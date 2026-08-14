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
	defer ForgetSubharnesses()
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
	defer ForgetSubharnesses()
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

// A specialist named for a node the baseline ruler already sized atomic is
// declined: the specialist's pipeline is a fixed cost the node's size does not
// repay. The decline target is the cheap whole-taker (bare), not the
// generalist — one sitting pays no pipeline, and bare is the cheaper
// whole-taker for it. Borderline and oversized are exactly the cases the
// specialist is for, and those stand.
func TestSizeApplyDeclinesSpecialistForAtomicNodes(t *testing.T) {
	defer ForgetSubharnesses()
	UseSubharness(Subharness{Name: "swe", Purpose: "coding"}, "ruler")
	UseSubharness(Subharness{Name: BareSubharness, Purpose: "the cheap whole-taker"}, "bare ruler")

	graph := &Graph{Nodes: []Node{
		{ID: 1, Kind: KindWork, Stage: 1},
		{ID: 2, Kind: KindWork, Stage: 1},
		{ID: 3, Kind: KindWork, Stage: 1},
	}, NextID: 4}
	_, err := sizeApply(graph, []sizeResult{{verdicts: []sizeVerdict{
		{Node: 1, Size: "atomic", Subharness: "swe"},
		{Node: 2, Size: "borderline", Subharness: "swe"},
		{Node: 3, Size: "oversized", Subharness: "swe", Parts: []string{"one", "two"}},
	}}})
	if err != nil {
		t.Fatalf("sizeApply: %v", err)
	}
	// Atomic: the specialist is declined to the cheap whole-taker.
	if got := graph.Node(1); got.Subharness != BareSubharness || got.Size != SizeAtomic {
		t.Fatalf("atomic node = %+v, want bare at atomic", *got)
	}
	// Borderline and oversized: the specialist keeps the node, forced atomic.
	for _, id := range []int{2, 3} {
		if got := graph.Node(id); got.Subharness != "swe" || got.Size != SizeAtomic || len(got.Parts) != 0 {
			t.Fatalf("node %d = %+v, want swe/atomic with no parts", id, *got)
		}
	}
}

// The generalist is a verdict, not a silence. The model answers "no
// specialist" by returning the empty string, and while that answer was stored
// as the empty string the graph also uses for "nobody judged this node", every
// downstream reader that fills a blank in — admission's inheritance above all —
// read a deliberate generalist as an unanswered question. So the answer is
// written down by name.
func TestGeneralistVerdictIsRecordedByName(t *testing.T) {
	defer ForgetSubharnesses()
	UseSubharness(Subharness{Name: "swe", Purpose: "coding"}, "ruler")
	UseSubharness(Subharness{Name: BareSubharness, Purpose: "the cheap whole-taker"}, "bare ruler")

	verdicts := []sizeVerdict{
		{Node: 1, Size: "atomic"},
		{Node: 2, Size: "atomic", Subharness: "swe"},
		{Node: 3, Size: "borderline", Subharness: "reviewer"},
	}
	recordGeneralist(verdicts)
	for _, want := range []struct {
		index int
		name  string
	}{{0, LinearSubharness}, {1, "swe"}, {2, LinearSubharness}} {
		if got := verdicts[want.index].Subharness; got != want.name {
			t.Fatalf("verdict %d = %q, want %q", want.index+1, got, want.name)
		}
	}

	graph := &Graph{Nodes: []Node{
		{ID: 1, Kind: KindWork, Stage: 1},
		{ID: 2, Kind: KindWork, Stage: 1},
		{ID: 3, Kind: KindWork, Stage: 1},
	}, NextID: 4}
	if _, err := sizeApply(graph, []sizeResult{{verdicts: verdicts}}); err != nil {
		t.Fatalf("sizeApply: %v", err)
	}
	// The generalist on atomic work with no inputs defaults to bare — the cheap
	// whole-taker for one-sitting work. The size stands (atomic) because the
	// node was judged against the baseline ruler and that is the ruler it was
	// judged against.
	if node := graph.Node(1); node.Subharness != BareSubharness || node.Size != SizeAtomic {
		t.Fatalf("node 1 = %+v, want bare named and atomic", *node)
	}
	// Node 2 was named for a specialist while sized atomic — the pipeline is a
	// fixed cost an atomic node never repays, so the cheap whole-taker takes it.
	if node := graph.Node(2); node.Subharness != BareSubharness || node.Size != SizeAtomic {
		t.Fatalf("node 2 = %+v, want bare named and atomic", *node)
	}
	// Node 3 was the generalist on borderline work — not one-sitting, so the
	// generalist keeps it. The size stands and so do the parts.
	if node := graph.Node(3); node.Subharness != LinearSubharness || node.Size != SizeBorderline {
		t.Fatalf("node 3 = %+v, want the generalist named and borderline left alone", *node)
	}

	// The two predicates answer different questions, and the difference is the
	// whole fix: KnownSubharness says "is this a registered specialist", and
	// says no to both the generalist and to nothing at all.
	for _, testCase := range []struct {
		name        string
		known       bool
		generalist  bool
		chosenByAny bool
	}{
		{name: "swe", known: true, chosenByAny: true},
		{name: LinearSubharness, generalist: true, chosenByAny: true},
		{name: " linear ", generalist: true, chosenByAny: true},
		{name: "", chosenByAny: false},
		{name: "reviewer", chosenByAny: true},
	} {
		if got := KnownSubharness(testCase.name); got != testCase.known {
			t.Fatalf("KnownSubharness(%q) = %v", testCase.name, got)
		}
		if got := GeneralistSubharness(testCase.name); got != testCase.generalist {
			t.Fatalf("GeneralistSubharness(%q) = %v", testCase.name, got)
		}
		if got := SubharnessChosen(testCase.name); got != testCase.chosenByAny {
			t.Fatalf("SubharnessChosen(%q) = %v", testCase.name, got)
		}
	}
}

// TestSizeApplyRoutesByThreeTiers exercises every arm of the three-tier routing
// policy in one pass: the pipeline specialist keeps non-atomic work and is
// declined to bare on atomic work; bare keeps atomic work and is declined to
// linear on non-atomic work; the generalist defaults to bare for standalone
// atomic work and to linear for everything else. Nodes with inputs (Needs) and
// synthesis nodes are excluded from bare.
func TestSizeApplyRoutesByThreeTiers(t *testing.T) {
	defer ForgetSubharnesses()
	UseSubharness(Subharness{Name: "swe", Purpose: "coding"}, "ruler")
	UseSubharness(Subharness{Name: BareSubharness, Purpose: "the cheap whole-taker"}, "bare ruler")

	graph := &Graph{Nodes: []Node{
		{ID: 1, Kind: KindWork, Stage: 1},                  // swe, non-atomic → swe
		{ID: 2, Kind: KindWork, Stage: 1},                  // swe, atomic → bare
		{ID: 3, Kind: KindWork, Stage: 1},                  // bare, atomic → bare
		{ID: 4, Kind: KindWork, Stage: 1},                  // bare, non-atomic → linear
		{ID: 5, Kind: KindWork, Stage: 1},                  // generalist, atomic, no Needs → bare
		{ID: 6, Kind: KindWork, Stage: 1, Needs: []int{5}}, // generalist, atomic, Needs → linear
		{ID: 7, Kind: KindWork, Stage: 1},                  // generalist, borderline, no Needs → linear
		{ID: 8, Kind: KindSynthesis, Stage: 1},             // synthesis → skipped, untouched
	}, NextID: 9}
	_, err := sizeApply(graph, []sizeResult{{verdicts: []sizeVerdict{
		{Node: 1, Size: "oversized", Subharness: "swe", Parts: []string{"one", "two"}},
		{Node: 2, Size: "atomic", Subharness: "swe"},
		{Node: 3, Size: "atomic", Subharness: BareSubharness},
		{Node: 4, Size: "oversized", Subharness: BareSubharness, Parts: []string{"one", "two"}},
		{Node: 5, Size: "atomic", Subharness: LinearSubharness},
		{Node: 6, Size: "atomic", Subharness: LinearSubharness},
		{Node: 7, Size: "borderline", Subharness: LinearSubharness},
		{Node: 8, Size: "atomic", Subharness: LinearSubharness},
	}}})
	if err != nil {
		t.Fatalf("sizeApply: %v", err)
	}
	for _, want := range []struct {
		id   int
		sub  string
		size Size
	}{
		{1, "swe", SizeAtomic},                // swe keeps non-atomic, forced atomic
		{2, BareSubharness, SizeAtomic},       // swe declined to bare
		{3, BareSubharness, SizeAtomic},       // bare keeps atomic
		{4, LinearSubharness, SizeOversized},  // bare declined to linear, size stands
		{5, BareSubharness, SizeAtomic},       // generalist default → bare
		{6, LinearSubharness, SizeAtomic},     // generalist + Needs → linear
		{7, LinearSubharness, SizeBorderline}, // generalist borderline → linear
		{8, "", SizeUnknown},                  // synthesis skipped, untouched
	} {
		got := graph.Node(want.id)
		if got.Subharness != want.sub || got.Size != want.size {
			t.Errorf("node %d = {Subharness: %q, Size: %q}, want {%q, %q}",
				want.id, got.Subharness, got.Size, want.sub, want.size)
		}
	}
}

// TestSizeApplyDefaultsSkippedNodesByThreeTiers covers the unknown-size arm: a
// node the model skipped is treated as atomic, and the same three-tier default
// applies — standalone work (no Needs) routes to bare, work with inputs keeps
// the generalist, and synthesis nodes are left untouched.
func TestSizeApplyDefaultsSkippedNodesByThreeTiers(t *testing.T) {
	defer ForgetSubharnesses()
	UseSubharness(Subharness{Name: "swe", Purpose: "coding"}, "ruler")
	UseSubharness(Subharness{Name: BareSubharness, Purpose: "the cheap whole-taker"}, "bare ruler")

	// No verdicts at all: every node was skipped.
	graph := &Graph{Nodes: []Node{
		{ID: 1, Kind: KindWork, Stage: 1},                  // no Needs → bare
		{ID: 2, Kind: KindWork, Stage: 1, Needs: []int{1}}, // has Needs → generalist
		{ID: 3, Kind: KindSynthesis, Stage: 1},             // not work → untouched
	}, NextID: 4}
	if _, err := sizeApply(graph, nil); err != nil {
		t.Fatalf("sizeApply: %v", err)
	}
	if n := graph.Node(1); n.Subharness != BareSubharness || n.Size != SizeAtomic {
		t.Fatalf("skipped no-needs node = %+v, want bare/atomic", *n)
	}
	if n := graph.Node(2); n.Subharness != "" || n.Size != SizeAtomic {
		t.Fatalf("skipped needs node = %+v, want empty subharness/atomic", *n)
	}
	if n := graph.Node(3); n.Subharness != "" || n.Size != SizeUnknown {
		t.Fatalf("skipped synthesis node = %+v, want untouched", *n)
	}
}

// TestSizeApplyDegradesToLinearWithoutBare is the additive law: bare is the
// decline target only when it is registered. A process without bare — one that
// has not installed the cheap whole-taker — declines specialists and defaults
// one-sitting work to the generalist, exactly as the system ran before bare
// existed.
func TestSizeApplyDegradesToLinearWithoutBare(t *testing.T) {
	defer ForgetSubharnesses()
	UseSubharness(Subharness{Name: "swe", Purpose: "coding"}, "ruler")

	graph := &Graph{Nodes: []Node{
		{ID: 1, Kind: KindWork, Stage: 1}, // swe, atomic → linear (bare absent)
		{ID: 2, Kind: KindWork, Stage: 1}, // generalist, atomic, no Needs → linear
	}, NextID: 3}
	_, err := sizeApply(graph, []sizeResult{{verdicts: []sizeVerdict{
		{Node: 1, Size: "atomic", Subharness: "swe"},
		{Node: 2, Size: "atomic", Subharness: LinearSubharness},
	}}})
	if err != nil {
		t.Fatalf("sizeApply: %v", err)
	}
	if got := graph.Node(1); got.Subharness != LinearSubharness || got.Size != SizeAtomic {
		t.Fatalf("swe+atomic without bare = %+v, want linear/atomic", *got)
	}
	if got := graph.Node(2); got.Subharness != LinearSubharness || got.Size != SizeAtomic {
		t.Fatalf("generalist+atomic without bare = %+v, want linear/atomic", *got)
	}
}

func TestAnchorsAreKeptPerSubharness(t *testing.T) {
	defer ForgetSubharnesses()
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
