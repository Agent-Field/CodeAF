package exec

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/plan"
)

// inlineFixture is one settled producer with one file, and the consumer that
// declared it. Both surfaces route a fan-in through the same two facts — what
// the producer said, and what it left on disk — so the fixture is those two.
func inlineFixture(t *testing.T, body []byte, result string) (*Scheduler, *plan.Graph) {
	t.Helper()
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	space = space.WithScratch(t.TempDir())
	path, err := space.Resolve("01-vendors.md")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	graph := &plan.Graph{Goal: "pick a vendor", Nodes: []plan.Node{
		{
			ID: 1, Stage: 1, Kind: plan.KindWork, Title: "Measure", State: plan.StateDone,
			Result: result, Artifacts: []string{"01-vendors.md"},
		},
		{
			ID: 2, Stage: 2, Kind: plan.KindSynthesis, Title: "Choose", Brief: "choose one",
			State: plan.StatePending, Needs: []int{1},
		},
	}}
	return NewScheduler(NewRegistry(NewLinear(nil, space, nil, 1, 1, time.Minute)), space, 1), graph
}

// The headless asymmetry, stated as a test. The resident surface has carried the
// producer's FILES on the edge since the digest learned to read them back; this
// path carried the producer's one-sentence announcement and nothing else, under
// a header telling the consumer it already held the work — and then, because
// nothing here ever set Whole, invited it to go and read the files anyway. The
// consumer did. Nineteen turns of filesystem archaeology across four nodes of
// one job is what that invitation cost when it was measured.
func TestAHeadlessFanInCarriesTheProducersFileAndSaysItIsHoldingIt(t *testing.T) {
	scheduler, graph := inlineFixture(t,
		[]byte("Vendor A: 41ms.\nVendor B: 88ms.\n"),
		"The measurement is complete. The table is at 01-vendors.md")
	task := scheduler.taskFor(graph, &graph.Nodes[1])
	if len(task.Inputs) != 1 {
		t.Fatalf("inputs = %+v, want the one declared dependency", task.Inputs)
	}
	input := task.Inputs[0]
	if !strings.Contains(input.Result, "Vendor B: 88ms.") {
		t.Fatalf("the producer's file never reached the consumer:\n%s", input.Result)
	}
	if !input.Whole {
		t.Fatalf("the consumer holds the whole file and was not told so: %+v", input)
	}
	if len(input.Artifacts) != 1 || input.Artifacts[0] != "01-vendors.md" {
		t.Fatalf("the path was dropped, so nothing can be cited: %+v", input.Artifacts)
	}
	// And the brief says the true one of its two opposite sentences.
	linear := NewLinear(nil, workspace(t), nil, 1, 1, time.Minute)
	brief := linear.brief(task)
	if strings.Contains(brief, "read them if you need") {
		t.Fatalf("a consumer holding the contents was still sent to the files:\n%s", brief)
	}
	if !strings.Contains(brief, "you are holding their contents") {
		t.Fatalf("the brief never says the block is the material:\n%s", brief)
	}
}

// A file past the consumer's budget is named rather than inlined, and the claim
// is withdrawn with it. Half a table read as a whole table is worse than a path.
func TestAnOversizedProductIsNamedRatherThanClaimed(t *testing.T) {
	scheduler, graph := inlineFixture(t,
		[]byte(strings.Repeat("Vendor row with a long measured latency line.\n", 4000)),
		"The measurement is complete. The table is at 01-vendors.md")
	input := scheduler.taskFor(graph, &graph.Nodes[1]).Inputs[0]
	if input.Whole {
		t.Fatalf("a clipped read was handed over as the whole of the file:\n%s", input.Result)
	}
	if !strings.Contains(input.Result, "past the budget for it") {
		t.Fatalf("the clip was silent, which reads as a finished thought:\n%s", input.Result)
	}
	if len(input.Artifacts) != 1 {
		t.Fatalf("the path a consumer would have to open was dropped: %+v", input.Artifacts)
	}
}

// A binary artifact cannot be held at all. The refusal is structural — a NUL
// byte, not a name or an extension — and it takes the claim down with it: a
// consumer handed the first kilobyte of a PNG has been handed noise it will
// spend a turn making sense of.
func TestABinaryProductIsRefusedAndTheClaimWithdrawn(t *testing.T) {
	scheduler, graph := inlineFixture(t,
		[]byte{0x89, 'P', 'N', 'G', 0x00, 0x1a, 0x0a, 0x00, 0x00},
		"The chart is at 01-vendors.md")
	input := scheduler.taskFor(graph, &graph.Nodes[1]).Inputs[0]
	if input.Whole {
		t.Fatalf("a binary file was reported as text in the consumer's hands:\n%q", input.Result)
	}
	if strings.ContainsRune(input.Result, 0) {
		t.Fatalf("binary bytes reached the consumer's prompt:\n%q", input.Result)
	}
}

// A producer that answered in its final message and left no file is holding
// nothing back, and the consumer holds all of it. The old code could not say so
// — Whole was never set on this path at all — so even the cheapest fan-in in the
// system opened with an invitation to go and look for something.
func TestAProducerWithNoFilesStillArrivesWhole(t *testing.T) {
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	graph := &plan.Graph{Goal: "answer it", Nodes: []plan.Node{
		{ID: 1, Stage: 1, Kind: plan.KindWork, Title: "Count", State: plan.StateDone,
			Result: "there are four defects"},
		{ID: 2, Stage: 2, Kind: plan.KindSynthesis, Title: "Report", Brief: "report",
			State: plan.StatePending, Needs: []int{1}},
	}}
	scheduler := NewScheduler(NewRegistry(NewLinear(nil, space, nil, 1, 1, time.Minute)), space, 1)
	input := scheduler.taskFor(graph, &graph.Nodes[1]).Inputs[0]
	if !input.Whole {
		t.Fatalf("a producer whose whole product is its message did not arrive whole: %+v", input)
	}
}

// One file, two producers naming it, one consumer. It is read once. The pot is
// the consumer's and paying twice out of it for the same bytes is the fan-in
// paying for a coincidence in how two upstream leaves wrote their summaries.
func TestOneFileIsInlinedOnceHoweverManyProducersNameIt(t *testing.T) {
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	path, err := space.Resolve("00-spec.md")
	if err != nil {
		t.Fatal(err)
	}
	const body = "The specification says every latency is a p99."
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	graph := &plan.Graph{Goal: "review", Nodes: []plan.Node{
		{ID: 1, Stage: 1, Kind: plan.KindWork, Title: "A", State: plan.StateDone,
			Result: "see the spec", Artifacts: []string{"00-spec.md"}},
		{ID: 2, Stage: 1, Kind: plan.KindWork, Title: "B", State: plan.StateDone,
			Result: "see the spec too", Artifacts: []string{"00-spec.md"}},
		{ID: 3, Stage: 2, Kind: plan.KindSynthesis, Title: "Join", Brief: "join",
			State: plan.StatePending, Needs: []int{1, 2}},
	}}
	scheduler := NewScheduler(NewRegistry(NewLinear(nil, space, nil, 1, 1, time.Minute)), space, 1)
	task := scheduler.taskFor(graph, &graph.Nodes[2])
	if got := strings.Count(task.Inputs[0].Result+task.Inputs[1].Result, body); got != 1 {
		t.Fatalf("the shared spec was carried %d times, want once", got)
	}
	for index, input := range task.Inputs {
		if !input.Whole {
			t.Fatalf("input %d does not hold the spec the consumer is holding: %+v", index, input)
		}
	}
}

// THE SUFFICIENCY CLAIM, and the three facts it rests on.
//
// The prompt fenced the leaf inside its working directory and told it what it
// held; nothing ever told it the holding was COMPLETE. An agent with no such
// assurance does the only responsible thing, which is to go and look — and the
// looking was measured at five of eleven turns. The sentence is one sentence and
// it is said only where it is true.
func TestTheSufficiencySentenceIsSaidOnlyWhenItIsAFact(t *testing.T) {
	const claim = "This is the whole of what exists for this job"

	fresh := t.TempDir()
	space, err := NewWorkspace(fresh)
	if err != nil {
		t.Fatal(err)
	}
	space = space.WithScratch(t.TempDir())
	linear := NewLinear(nil, space, nil, 1, 1, time.Minute)

	// Zero dependencies in a directory the harness made and nobody has written
	// to: there is genuinely nothing here to find.
	if brief := linear.brief(Task{Brief: "write the note"}); !strings.Contains(brief, claim) {
		t.Fatalf("a leaf with nothing to discover was not told so:\n%s", brief)
	}

	// A dependency that arrived on a pointer is a dependency the leaf has to open.
	partial := linear.brief(Task{Brief: "assemble", Inputs: []Input{{
		Title: "n1", Result: "a summary of it", Artifacts: []string{"01-vendors.md"},
	}}})
	if strings.Contains(partial, claim) {
		t.Fatalf("a leaf holding only an account of its input was told it holds everything:\n%s", partial)
	}

	// An attachment is material the leaf holds only after it has gone and got it,
	// which is the exact act the sentence would be telling it not to do.
	attached := linear.brief(Task{Brief: "summarise it", DocumentPaths: []string{"contract.pdf"}})
	if strings.Contains(attached, claim) {
		t.Fatalf("a leaf with a document still to read was told there is nothing to read:\n%s", attached)
	}

	// A file nobody named is a file the leaf could discover, and the only way to
	// know one is there is to look.
	if err := os.WriteFile(filepath.Join(fresh, "main.go"), []byte("package main"), 0o644); err != nil {
		t.Fatal(err)
	}
	if brief := linear.brief(Task{Brief: "write the note"}); strings.Contains(brief, claim) {
		t.Fatalf("a leaf standing in somebody's project was told the project is not there:\n%s", brief)
	}

	// A sibling's deliverable that this leaf is already holding is not something
	// to discover — it is the input, and naming it is what makes the claim safe
	// in the case the whole fix is for.
	held := linear.brief(Task{Brief: "assemble", Inputs: []Input{{
		Title: "n1", Result: "package main", Artifacts: []string{"main.go"}, Whole: true,
	}}})
	if !strings.Contains(held, claim) {
		t.Fatalf("a fan-in holding every byte of its inputs was not told so:\n%s", held)
	}
}
