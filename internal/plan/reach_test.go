package plan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/ctxbudget"
)

// workspaceNaming writes a workspace holding one file of the given size and
// returns the directory. It is the whole fixture for the bare-name half of the
// measurement: a name with no scope around it is stat'ed and never opened, so
// what is IN the file has never mattered there.
func workspaceNaming(t *testing.T, name string, size int) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, name), make([]byte, size), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// registerWorkspace is fixture A of issue #480, built rather than checked in: a
// register of three regional blocks of 1,160 records each under one file
// header, and the conventions document the brief says to read first. It is the
// shape a wide brief divides into lanes over, and the shape the measurement
// used to charge every lane the whole of.
func registerWorkspace(t *testing.T) (dir string, register int) {
	t.Helper()
	dir = t.TempDir()
	var file strings.Builder
	file.WriteString("# Regional inventory register\n")
	for _, block := range []struct{ region, tag string }{{"North", "N"}, {"South", "S"}, {"East", "E"}} {
		fmt.Fprintf(&file, "## %s\n", block.region)
		for record := 1; record <= 1160; record++ {
			fmt.Fprintf(&file, "%s-%04d | pallet crate %03d | %3d | %02d/%02d/20%02d\n",
				block.tag, record, record%97, record%999+1, record%28+1, record%12+1, record%7+20)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "register.txt"), []byte(file.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	var conventions strings.Builder
	conventions.WriteString("# Register conventions\n")
	for ruling := 1; ruling <= 4; ruling++ {
		fmt.Fprintf(&conventions, "## Ruling %d\n", ruling)
		for line := 0; line < 30; line++ {
			conventions.WriteString("The quota is settled and the manifest is not up for redesign; " +
				"a clerk records the berth, the depot and the season in that order.\n")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "CONVENTIONS.md"), []byte(conventions.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, file.Len()
}

// handbookWorkspace is fixture B: one file of thirty chapters, whose heading
// lines are a couple of kilobytes of the eighty-five the file weighs.
func handbookWorkspace(t *testing.T) (dir string, handbook int) {
	t.Helper()
	dir = t.TempDir()
	var file strings.Builder
	file.WriteString("# Handbook\n")
	for chapter := 1; chapter <= 30; chapter++ {
		fmt.Fprintf(&file, "## chapter %d: the %d harbour tariff\n", chapter, chapter)
		for line := 0; line < 12; line++ {
			fmt.Fprintf(&file, "The vessel is berthed against the quota, and the manifest is filed with "+
				"the clerk of sector %d before the cargo leaves the basin for the depot at the relay.\n", chapter)
		}
		file.WriteString("\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "HANDBOOK.md"), []byte(file.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir, file.Len()
}

// laneGraph is the correct division of fixture A: one node per lane, each
// owning one block of the register and nothing else, with the conventions
// document beside the first of them exactly as the brief hands it over.
func laneGraph(dir string) *Graph {
	graph := &Graph{Goal: "three lanes over register.txt", NextID: 1, Workspace: dir,
		Stages: []Stage{{Title: "Lanes", Summary: "one lane per block"}}}
	graph.Add(Node{Kind: KindWork, Stage: 1, Title: "North-date-rewrite",
		Summary: "rewrite the recorded dates of the North block",
		Sources: []string{"register.txt: header lines, North block heading, 1,160 North records",
			"CONVENTIONS.md: rulings on date format and scope"}})
	graph.Add(Node{Kind: KindWork, Stage: 1, Title: "South-sort",
		Summary: "sort and renumber the South block",
		Sources: []string{"register.txt: South block heading, 1,160 South records"}})
	graph.Add(Node{Kind: KindWork, Stage: 1, Title: "East-total",
		Summary: "flag and total the East block",
		Sources: []string{"register.txt: East block heading, 1,160 East records"}})
	return graph
}

// The reach is the worker's own window and not a second opinion about it. A
// number two packages both need is a number that drifts unless one of them
// owns it, and the leaf loop that spends this window sizes itself from the same
// call.
func TestTheReachIsTheWindowTheWorkerItselfWillGet(t *testing.T) {
	for _, window := range []int{0, 8_000, 200_000, 1_000_000} {
		if got, want := ReachFor("/tmp", window).Bytes, ctxbudget.ObservationBytes(window); got != want {
			t.Fatalf("a %d-token model reaches %d bytes in the planner and %d in the worker", window, got, want)
		}
	}
	// No workspace measures nothing, whatever the window says.
	if measurement := ReachFor("", 200_000).Measure("read notes.md"); measurement.Taken() || measurement.Line() != "" {
		t.Fatalf("a run with no workspace measured something: %+v", measurement)
	}
}

// The measurement in the words it reaches a reader in. Both figures, their
// ratio, and no instruction. The name here stands bare, which is the reading
// that has not changed: a sentence that names a file and says nothing about
// which part of it names all of it.
func TestTheMeasurementSaysBothFiguresAndTheirRatio(t *testing.T) {
	dir := workspaceNaming(t, "corpus.txt", 269_000)
	measurement := ReachFor(dir, 0).Measure("rewrite corpus.txt in three lanes")
	if !measurement.Exceeds() {
		t.Fatalf("a 269 KB file did not exceed a %d-byte reach", measurement.Reach)
	}
	want := "MEASURED — the material this goal names by name is 1 file, 262.7 KB in all. " +
		"One worker holds 32.0 KB of material at a time, so what is named is 8.2 times what one worker can hold."
	if got := measurement.Line(); got != want {
		t.Fatalf("the measurement reads\n  %q\nwant\n  %q", got, want)
	}

	// Material that fits says so, in the same shape. The line is a measurement
	// and not an alarm: telling a pass the material fits is what stops it
	// inventing pressure that is not there.
	small := ReachFor(workspaceNaming(t, "notes.md", 4_096), 0).Measure("summarise notes.md")
	if small.Exceeds() {
		t.Fatal("4 KB exceeded a 32 KB reach")
	}
	if got := small.Line(); !strings.HasSuffix(got, "fits inside one worker.") {
		t.Fatalf("material within reach reads %q", got)
	}
}

// A goal that names nothing is not a small goal; it is a goal nothing was
// measured about. It renders nothing and moves no verdict — a guess about
// unweighed material is the exact thing this replaces.
func TestAGoalThatNamesNothingMeasuresNothingAndChangesNothing(t *testing.T) {
	dir := workspaceNaming(t, "corpus.txt", 269_000)
	reach := ReachFor(dir, 0)
	for _, goal := range []string{
		"write up what the team decided",
		"read the whole repository and improve it",
		// A name that is not there, a path that climbs out of the workspace, and
		// an address that is not a file at all: none of them is material.
		"open missing.txt, ../corpus.txt and https://example.com/corpus.txt",
	} {
		measurement := reach.Measure(goal)
		if measurement.Taken() || measurement.Exceeds() || measurement.Line() != "" {
			t.Fatalf("%q was measured as %+v", goal, measurement)
		}
	}

	// Nothing measured, nothing corrected: the sizing pass's verdict stands.
	graph := &Graph{Goal: "write it up", NextID: 1, Workspace: dir}
	graph.Add(Node{Kind: KindWork, Stage: 1, Title: "Write", Summary: "write it up",
		Size: SizeAtomic, Sources: []string{"the team's decision"}})
	correctBeyondReach(graph)
	if graph.Nodes[0].Size != SizeAtomic || graph.Nodes[0].Undivided != "" {
		t.Fatalf("a node naming nothing was corrected to %q/%q", graph.Nodes[0].Size, graph.Nodes[0].Undivided)
	}
}

// THE LAW, on the case that made it: what is measured is the material the node
// will read, not the file its words mention.
//
// The node is one lane of a correct division and its own sources say so — the
// North block heading and the 1,160 records under it. The measurement used to
// read the word `register.txt` out of that sentence, stat the whole file, add
// the whole of the conventions beside it and report `2 files, 165.4 KB in all …
// 5.2 times what one worker can hold`, which then overruled the sizer and
// refused the lane.
func TestALaneIsMeasuredByTheBlockItScopesAndNotByTheWholeFile(t *testing.T) {
	dir, register := registerWorkspace(t)
	reach := ReachFor(dir, 0)
	node := laneGraph(dir).Node(1)
	measurement := reach.Measure(node.Sources...)

	// One block of three, and the conventions — scoped in words no arithmetic
	// reaches — weigh nothing rather than a guess.
	if measurement.Files != 1 {
		t.Fatalf("the lane measured %d files, want the one it scopes: %+v", measurement.Files, measurement)
	}
	if measurement.Bytes >= register/2 || measurement.Bytes <= register/4 {
		t.Fatalf("a lane over one block of three measured %d bytes of a %d-byte file", measurement.Bytes, register)
	}

	// And the sizer's verdict stands. Three lanes each scoping a region of the
	// same file is a division, and there is nothing here to correct: expansion
	// cannot divide a lane over one block any further, so the correction only
	// converts a good plan into a refusal.
	graph := laneGraph(dir)
	if _, err := sizeApply(graph, []sizeResult{{verdicts: []sizeVerdict{
		{Node: 1, Size: "atomic"}, {Node: 2, Size: "atomic"}, {Node: 3, Size: "atomic"},
	}}}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{1, 2, 3} {
		if got := graph.Node(id).Size; got != SizeAtomic {
			t.Fatalf("lane %d was corrected to %q", id, got)
		}
		if got := graph.Node(id).Undivided; got != "" {
			t.Fatalf("lane %d was refused: %q", id, got)
		}
	}
}

// The one reading of where a file name stops and its scope starts. A name
// followed by a colon, a dash or a bracket is scoped by the words after it; a
// name in a sentence is a name in a sentence, and the sentence is not a scope —
// "rewrite corpus.txt in three lanes" names all of corpus.txt, and a reader
// that took "in three lanes" for a scope would measure nothing anywhere.
func TestAScopeIsMarkedAndASentenceIsNot(t *testing.T) {
	dir, register := registerWorkspace(t)
	reach := ReachFor(dir, 0)
	for _, source := range []string{
		"register.txt: lines 2-1161",
		"register.txt — lines 2-1161",
		"register.txt - lines 2-1161",
		"register.txt (lines 2-1161)",
		"register.txt [lines 2-1161]",
	} {
		measurement := reach.Measure(source)
		if !measurement.Taken() {
			t.Fatalf("%q scoped nothing", source)
		}
		if measurement.Bytes >= register/2 || measurement.Bytes <= register/4 {
			t.Fatalf("%q measured %d bytes of a %d-byte file", source, measurement.Bytes, register)
		}
	}
	for _, source := range []string{
		"rewrite register.txt in three lanes",
		"register.txt",
	} {
		if got := reach.Measure(source).Bytes; got != register {
			t.Fatalf("%q measured %d bytes, want the whole %d-byte file", source, got, register)
		}
	}
}

// A heading set is material too, and it is the heading lines and not the file
// they head. The reading that stamped this node was `1 file, 84.8 KB in all …
// 2.7 times what one worker can hold` for perhaps two kilobytes of headings.
func TestAHeadingSetIsMeasuredAsItsHeadingLinesAndNotAsTheFile(t *testing.T) {
	dir, handbook := handbookWorkspace(t)
	measurement := ReachFor(dir, 0).Measure("HANDBOOK.md — all 30 `## chapter N: …` heading lines")
	if !measurement.Taken() {
		t.Fatalf("thirty named heading lines measured nothing: %+v", measurement)
	}
	if measurement.Exceeds() {
		t.Fatalf("thirty heading lines of a %d-byte file measured %d bytes, past a %d-byte reach",
			handbook, measurement.Bytes, measurement.Reach)
	}
	// And it is the heading lines themselves and not the file's mean line
	// thirty times over: the mean is five times too generous about a chapter
	// heading, and a number reported for material the pass did not locate is
	// the guessing this replaced.
	headings := 0
	for _, line := range strings.SplitAfter(readFile(t, dir, "HANDBOOK.md"), "\n") {
		if strings.HasPrefix(line, "## chapter ") {
			headings += len(line)
		}
	}
	if headings == 0 {
		t.Fatal("the fixture wrote no chapter headings")
	}
	if measurement.Bytes > 2*headings {
		t.Fatalf("thirty heading lines weighing %d bytes measured %d, of a %d-byte file",
			headings, measurement.Bytes, handbook)
	}
}

// readFile is the fixture read back, for a test that checks the measurement
// against the material rather than against the measurement's own arithmetic.
func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// The other half of the law, and the guarantee from issue #384 kept whole: a
// node that names the register and does not say which part of it names all of
// it, and one worker cannot hold all of it.
func TestAWholeFileNamedBareIsStillCorrectedAndStillJournalsTheRefusal(t *testing.T) {
	dir, register := registerWorkspace(t)
	graph := &Graph{Goal: "three lanes over one file", NextID: 1, Workspace: dir}
	graph.Add(Node{Kind: KindWork, Stage: 1, Title: "All three lanes",
		Summary: "work all three lanes", Sources: []string{"register.txt"}})
	graph.Add(Node{Kind: KindWork, Stage: 1, Title: "Read the notes",
		Summary: "read the short notes", Sources: []string{"missing.txt"}})

	if got := ReachFor(dir, 0).Measure("register.txt").Bytes; got != register {
		t.Fatalf("a bare name measured %d bytes of a %d-byte file", got, register)
	}
	if _, err := sizeApply(graph, []sizeResult{{verdicts: []sizeVerdict{
		{Node: 1, Size: "atomic", Parts: []string{"block A", "block B"}},
		{Node: 2, Size: "atomic"},
	}}}); err != nil {
		t.Fatal(err)
	}
	if got := graph.Node(1).Size; got != SizeOversized {
		t.Fatalf("a node naming the whole register against a 32 KB reach was sized %q", got)
	}
	if got := graph.Node(1).Undivided; got != RefusalBeyondReach {
		t.Fatalf("the correction journaled %q, want %q", got, RefusalBeyondReach)
	}
	if got := graph.Node(2).Size; got != SizeAtomic {
		t.Fatalf("a node naming nothing measurable was corrected to %q", got)
	}
}

// One bare name rides in free if the exemption above is written loosely: a node
// sourcing the whole register beside a scoped line or two of a file its sibling
// also scopes is not a lane of a division, and the whole register is exactly
// what the correction exists to catch.
func TestABareNameBesideASharedScopeIsStillCorrected(t *testing.T) {
	dir, _ := registerWorkspace(t)
	graph := laneGraph(dir)
	// Two nodes scope the conventions between them, so the file is shared — and
	// the first of them also names the whole register, with nothing scoping it.
	graph.Node(1).Sources = []string{"register.txt", "CONVENTIONS.md: lines 2-4"}
	graph.Node(2).Sources = []string{"CONVENTIONS.md: lines 5-9"}
	if _, err := sizeApply(graph, []sizeResult{{verdicts: []sizeVerdict{
		{Node: 1, Size: "atomic"}, {Node: 2, Size: "atomic"}, {Node: 3, Size: "atomic"},
	}}}); err != nil {
		t.Fatal(err)
	}
	if got := graph.Node(1).Size; got != SizeOversized {
		t.Fatalf("a node naming the whole register beside a shared scope was sized %q", got)
	}
	if got := graph.Node(1).Undivided; got != RefusalBeyondReach {
		t.Fatalf("the correction journaled %q, want %q", got, RefusalBeyondReach)
	}
	// Its sibling scopes a handful of lines and is nowhere near one worker's
	// window, so nothing about it changed.
	if got := graph.Node(2).Size; got != SizeAtomic || graph.Node(2).Undivided != "" {
		t.Fatalf("the sibling was corrected to %q/%q", got, graph.Node(2).Undivided)
	}
}

// A scope in words no arithmetic reaches is not measured at all, and the sizer
// keeps the node. That is this pass's standing rule for anything it cannot
// weigh: an over-estimate is still a guess, and a guess is the thing the
// measurement replaced.
func TestAScopeTheMeasureCannotResolveIsLeftToTheSizer(t *testing.T) {
	dir, _ := registerWorkspace(t)
	if measurement := ReachFor(dir, 0).Measure("CONVENTIONS.md: rulings on date format and scope"); measurement.Taken() {
		t.Fatalf("an unresolvable scope was weighed as %+v", measurement)
	}
	graph := &Graph{Goal: "settle the conventions", NextID: 1, Workspace: dir}
	graph.Add(Node{Kind: KindWork, Stage: 1, Title: "Rulings", Summary: "apply the settled rulings",
		Sources: []string{"CONVENTIONS.md: rulings on date format and scope"}})
	if _, err := sizeApply(graph, []sizeResult{{verdicts: []sizeVerdict{
		{Node: 1, Size: "atomic"},
	}}}); err != nil {
		t.Fatal(err)
	}
	if got := graph.Node(1).Size; got != SizeAtomic || graph.Node(1).Undivided != "" {
		t.Fatalf("an unmeasured node was corrected to %q/%q", got, graph.Node(1).Undivided)
	}
}

// The split question reads the same measurement, so a lane over one file whose
// share fits is not refused for the size of the file. A lane over one file
// cannot name two simultaneous pieces, so the refusal used to stand and the
// node was handed over whole — the exact state the measurement was added to
// prevent.
func TestASplitIsNotRefusedBeyondReachWhenTheLaneShareFits(t *testing.T) {
	dir, _ := handbookWorkspace(t)
	options := Options{MaxDepth: 2, Workspace: dir}
	lane := &Node{Kind: KindWork, Size: SizeAtomic, Title: "Heading rewrite",
		Summary: "rewrite the chapter headings",
		Sources: []string{"HANDBOOK.md — all 30 `## chapter N: …` heading lines"}}
	// Nothing was named to divide it into, so it is still handed over whole —
	// but for the ordinary reason and not for the size of a file it reads
	// thirty lines of.
	if verdict := JudgeSplit(lane, options); verdict.Divide || verdict.Reason != RefusalUnnamed {
		t.Fatalf("a lane whose share fits answered %+v", verdict)
	}
	// And where the lane did name its pieces, the question is settled on its
	// merits: the share fits, so the null hypothesis holds.
	lane.Parts = []string{"headings 1-15", "headings 16-30"}
	if verdict := JudgeSplit(lane, options); verdict.Divide || verdict.Reason != RefusalWithinReach {
		t.Fatalf("a lane whose share fits answered %+v", verdict)
	}

	// And the arithmetic still discharges the burden where the node really does
	// name more than one worker holds.
	whole := workspaceNaming(t, "corpus.txt", 269_000)
	beyond := &Node{Kind: KindWork, Size: SizeAtomic, Parts: []string{"block A", "block B"},
		Sources: []string{"corpus.txt"}}
	if got := JudgeSplit(beyond, Options{MaxDepth: 2, Workspace: whole}); !got.Divide {
		t.Fatalf("the measurement did not discharge the null hypothesis: %+v", got)
	}
	// Nothing could be named to divide it into, so it is handed over whole —
	// and the journal says which of the two refusals it was, because the two
	// call for different repairs.
	unnamed := &Node{Kind: KindWork, Size: SizeAtomic, Sources: []string{"corpus.txt"}}
	if got := JudgeSplit(unnamed, Options{MaxDepth: 2, Workspace: whole}); got.Divide || got.Reason != RefusalBeyondReach {
		t.Fatalf("an undividable node beyond reach was refused with %+v", got)
	}
	// The rollback: with no workspace every branch answers as it always did.
	if got := JudgeSplit(beyond, Options{MaxDepth: 2}); got.Divide || got.Reason != RefusalWithinReach {
		t.Fatalf("an unmeasured atomic node answered %+v", got)
	}
}

// The acceptance's first line, driven through the whole sizing pass: the three
// lanes plan as they do, the ruler calls each of them atomic, and none of them
// comes out of the pass left whole with a refusal on it.
func TestTheThreeLaneDivisionSurvivesTheSizingPass(t *testing.T) {
	dir, _ := registerWorkspace(t)
	graph := laneGraph(dir)
	client := &stubClient{reply: func(system, _ string) string {
		if !strings.Contains(system, "You judge whether each node is the right size") {
			return ""
		}
		return `{"sizes":[{"node":1,"size":"atomic"},{"node":2,"size":"atomic"},{"node":3,"size":"atomic"}]}`
	}}
	if _, err := SizeNodes(context.Background(), client, graph); err != nil {
		t.Fatal(err)
	}
	for _, node := range graph.Nodes {
		if node.Size != SizeAtomic || node.Undivided != "" {
			t.Fatalf("lane %d came out of the sizing pass %q/%q", node.ID, node.Size, node.Undivided)
		}
	}
}

// The measurement is one bounded pass and it says so: a goal that mentions a
// hundred dotted words costs a bounded number of syscalls, and the same words
// twice cost one.
func TestTheMeasurementIsOneBoundedPass(t *testing.T) {
	dir := workspaceNaming(t, "corpus.txt", 1_000)
	var words []string
	for index := 0; index < 400; index++ {
		words = append(words, fmt.Sprintf("thing%d.txt", index))
	}
	words = append(words, "corpus.txt", "corpus.txt")
	measurement := ReachFor(dir, 0).Measure(strings.Join(words, " "))
	if measurement.Taken() {
		t.Fatalf("the pass walked past its candidate ceiling and found %+v", measurement)
	}
	// Named twice, counted once.
	if got := ReachFor(dir, 0).Measure("read corpus.txt, then corpus.txt again"); got.Files != 1 || got.Bytes != 1_000 {
		t.Fatalf("the same file was counted %d times: %+v", got.Files, got)
	}
}
