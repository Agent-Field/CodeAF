package resident

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// inkWorkspace lays out the shape of the run this whole file is about: a
// project whose request names one thing, and a repository root a stuck model
// can write scratch into.
func inkWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, path := range []string{
		"src/grid.ts", "src/box.ts", "src/index.ts",
		"test/grid.test.ts", "package.json",
	} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("//\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func inkJob(t *testing.T, root string) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-2", Brief: "Fix the Grid layout in src/grid.ts so Box children lay out correctly."},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1",
		Intent: "Fix the Grid layout in src/grid.ts so Box children lay out correctly."}); err != nil {
		t.Fatal(err)
	}
	node, _, _ := graph.Node("task-2")
	forgetJobWorkspaces()
	t.Cleanup(forgetJobWorkspaces)
	RememberJobWorkspace(graph, node, root)
	return graph
}

func scratchRun(t *testing.T, root string, names ...string) []string {
	t.Helper()
	paths := make([]string, 0, len(names))
	for _, name := range names {
		full := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("//\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, full)
	}
	return paths
}

// The ink s10 reading. Every file the stuck rounds wrote is a file, and not one
// of them is the work: the request names `src/grid.ts`, and what the rounds
// produced was a repository root full of debug scripts.
func TestScratchFilesBesideTheChangeAreNotProgress(t *testing.T) {
	root := inkWorkspace(t)
	graph := inkJob(t, root)

	wrote := scratchRun(t, root,
		"debug-grid.ts", "debug-grid10.ts", "debug-grid11.ts",
		"debug-yoga.ts", "debug-test2.tsx")
	change := MeasureRound(graph, "task-2", root, wrote)
	if !change.Measured {
		t.Fatal("the workspace was named and the tree was read; this is a measurement")
	}
	if change.Moved() {
		t.Fatalf("scratch beside the change counted as progress: %v", change.Relevant)
	}
	if len(change.Scratch) != len(wrote) {
		t.Fatalf("scratch = %v, want all five journaled", change.Scratch)
	}

	// And the same reading says yes to the work itself, and to a check.
	real := scratchRun(t, root, "src/grid.ts", "test/box.test.ts", "debug-grid.ts")
	change = MeasureRound(graph, "task-2", root, real)
	if len(change.Relevant) != 2 || len(change.Scratch) != 1 {
		t.Fatalf("relevant=%v scratch=%v, want the source and the check in and the debug file out",
			change.Relevant, change.Scratch)
	}
}

// A job whose request this workspace cannot place narrows by nothing, and every
// changed file counts — the same fail-safe verify takes on an empty focus.
func TestAJobThatNamedNothingCountsEveryChange(t *testing.T) {
	root := inkWorkspace(t)
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer graph.Close()
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-2", Brief: "make it better please"},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: "make it better please"}); err != nil {
		t.Fatal(err)
	}
	change := MeasureRound(graph, "task-2", root, scratchRun(t, root, "debug-grid.ts"))
	if !change.Moved() {
		t.Fatal("a request that named nothing must not narrow anything away")
	}
}

// The whole of ink s10 in one sequence. Round one is a real change and is
// bought; round two writes only scratch and is STILL bought, because the first
// fruitless round is never refused; round three is refused, with the wall
// unspent and the run free to settle and be judged.
func TestTheGovernorRefusesTheThirdRoundOfScratch(t *testing.T) {
	root := inkWorkspace(t)
	graph := inkJob(t, root)
	node, _, _ := graph.Node("task-2")

	round := func(lineage string, number int, artifacts []string, gap string) GrowVerdict {
		t.Helper()
		verdict, err := growJob(context.Background(), graph, nil, GrowRequest{
			JobRoot: "task-2", Node: node, Lineage: lineage, Reason: GrowOverrun,
			Round: number, Measured: true, Artifacts: artifacts, Remainder: RemainderDigest(gap),
		})
		if err != nil {
			t.Fatal(err)
		}
		if verdict.Allow {
			admitGrowth(graph, GrowRequest{
				JobRoot: "task-2", Node: node, Lineage: lineage, Reason: GrowOverrun,
				Measured: true, Artifacts: artifacts, Remainder: RemainderDigest(gap),
			}, verdict, 2)
		}
		return verdict
	}

	if verdict := round("task-2", 1, scratchRun(t, root, "src/grid.ts"), "the grid is wrong"); !verdict.Allow {
		t.Fatalf("round one moved the work and was refused: %+v", verdict)
	}
	if verdict := round("task-2", 2, scratchRun(t, root, "debug-grid.ts"), "the grid is still wrong"); !verdict.Allow {
		t.Fatalf("the FIRST fruitless round must never be refused: %+v", verdict)
	}
	verdict := round("task-2", 3, scratchRun(t, root, "debug-grid10.ts", "debug-yoga.ts"), "the grid is wrong yet again")
	if verdict.Allow || verdict.Cause != CauseStandstill {
		t.Fatalf("round three = %+v, want a standstill refusal", verdict)
	}

	// And the journal says what the rounds actually wrote, so the refusal can
	// be read afterwards without a transcript.
	rounds, err := graph.JobGrowthRounds("task-2")
	if err != nil {
		t.Fatal(err)
	}
	last := rounds[len(rounds)-1]
	if last.Scratch != 2 || len(last.Wrote) != 2 {
		t.Fatalf("the refusal names nothing it refused: %+v", last)
	}
	standing, ok := GovernorStanding(graph, "task-2")
	if !ok || !strings.Contains(standing, "no relevant progress in 2 rounds") ||
		!strings.Contains(standing, "last change: src/grid.ts") {
		t.Fatalf("closing line = %q ok=%t", standing, ok)
	}
}

// happy-dom s10: one lineage was refused for a standstill and its SIBLING
// bought a revision round and ran the rest of the wall. A standstill is a fact
// about the job.
func TestOneLineageStandstillBindsTheWholeJob(t *testing.T) {
	root := inkWorkspace(t)
	graph := inkJob(t, root)
	node, _, _ := graph.Node("task-2")

	ask := func(lineage, reason string, artifacts []string) GrowVerdict {
		t.Helper()
		request := GrowRequest{JobRoot: "task-2", Node: node, Lineage: lineage,
			Reason: reason, Round: 1, Measured: true, Artifacts: artifacts}
		verdict, err := growJob(context.Background(), graph, nil, request)
		if err != nil {
			t.Fatal(err)
		}
		if verdict.Allow {
			admitGrowth(graph, request, verdict, 2)
		}
		return verdict
	}

	if verdict := ask("task-2-x1", GrowOverrun, scratchRun(t, root, "lib/PropertySymbol.d.ts")); !verdict.Allow {
		t.Fatalf("the first fruitless round is never refused: %+v", verdict)
	}
	if verdict := ask("task-2-x1", GrowOverrun, scratchRun(t, root, "lib/PropertySymbol.js")); verdict.Cause != CauseStandstill {
		t.Fatalf("the lineage's second fruitless round = %+v, want a standstill", verdict)
	}
	sibling := ask("task-2-x2", GrowRevision, scratchRun(t, root, "lib/PropertySymbol.d.ts.map"))
	if sibling.Allow {
		t.Fatal("a sibling lineage bought a round after the job had stopped moving")
	}
	if sibling.Cause != CauseStandstill {
		t.Fatalf("the sibling was refused for %q, want the job's own standstill", sibling.Cause)
	}
}

// A round the wall cannot hold is not bought, so the job settles while there is
// still time for a verdict on it — the defect that left two 5400-second runs
// with no gate at all.
func TestAGateIsCutBeforeTheWall(t *testing.T) {
	root := inkWorkspace(t)
	graph := inkJob(t, root)
	node, _, _ := graph.Node("task-2")

	// Two admitted rounds, journaled far enough apart that the job's own pace
	// is longer than the wall it has left.
	for _, name := range []string{"src/grid.ts", "src/box.ts"} {
		request := GrowRequest{JobRoot: "task-2", Node: node, Lineage: "task-2",
			Reason: GrowOverrun, Round: 1, Measured: true,
			Artifacts: scratchRun(t, root, name)}
		admitGrowth(graph, request, GrowVerdict{Allow: true, Round: 1}, 2)
		time.Sleep(60 * time.Millisecond)
	}

	// A wall with room to spare buys the round.
	roomy, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	verdict, err := growJob(roomy, graph, nil, GrowRequest{
		JobRoot: "task-2", Node: node, Lineage: "task-2", Reason: GrowOverrun,
		Round: 3, Measured: true, Artifacts: scratchRun(t, root, "src/index.ts")})
	if err != nil || !verdict.Allow {
		t.Fatalf("a job with an hour left was refused: %+v %v", verdict, err)
	}

	// A wall shorter than the job's own round does not.
	tight, cancelTight := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancelTight()
	verdict, err = growJob(tight, graph, nil, GrowRequest{
		JobRoot: "task-2", Node: node, Lineage: "task-2", Reason: GrowOverrun,
		Round: 3, Measured: true, Artifacts: scratchRun(t, root, "src/index.ts")})
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Allow || verdict.Cause != CauseOutOfWall {
		t.Fatalf("round bought with no wall left to finish it: %+v", verdict)
	}
	if standing, ok := GovernorStanding(graph, "task-2"); !ok || !strings.Contains(standing, "no time left") {
		t.Fatalf("closing line = %q ok=%t", standing, ok)
	}
}

// An exhaustion resumed in place spends a body of work, and the ledger says so.
func TestAResumedLeafIsAJournaledRound(t *testing.T) {
	root := inkWorkspace(t)
	graph := inkJob(t, root)
	node, _, _ := graph.Node("task-2")

	NoteResumedRound(graph, node, scratchRun(t, root, "debug-grid.ts"))
	NoteResumedRound(graph, node, scratchRun(t, root, "debug-yoga.ts"))
	rounds, err := graph.JobGrowthRounds("task-2")
	if err != nil {
		t.Fatal(err)
	}
	if len(rounds) != 2 {
		t.Fatalf("resumes journaled = %d, want 2", len(rounds))
	}
	for _, round := range rounds {
		if round.Reason != GrowResume || !round.Allowed || !round.Measured || round.Produced != 0 {
			t.Fatalf("resume row = %+v, want an admitted measured round that moved nothing", round)
		}
	}
	// They do not spend the round cap — the job is no bigger — but they are
	// exactly what the standstill rule was missing.
	if next := growthRound(rounds, "task-2"); next != 1 {
		t.Fatalf("resumes consumed the round cap: next round = %d", next)
	}
	verdict, err := growJob(context.Background(), graph, nil, GrowRequest{
		JobRoot: "task-2", Node: node, Lineage: "task-2", Reason: GrowOverrun,
		Measured: true, Artifacts: scratchRun(t, root, "debug-grid11.ts")})
	if err != nil {
		t.Fatal(err)
	}
	if verdict.Allow || verdict.Cause != CauseStandstill {
		t.Fatalf("eight resumes of one lineage stayed free: %+v", verdict)
	}
}

// A round that moved no file but closed a finding has moved the work.
func TestAClosedFindingIsProgressTheTreeCannotShow(t *testing.T) {
	rounds := []store.JobGrowthRound{{JobGrowth: store.JobGrowth{
		Lineage: "task-2", Allowed: true, Measured: true, Produced: 0, Red: 3,
	}}}
	moved := store.JobGrowth{Lineage: "task-2", Allowed: true, Measured: true, Produced: 0, Red: 1}
	if fruitless(rounds, len(rounds), moved) {
		t.Fatal("a round that took two failing checks off the board is not fruitless")
	}
	stuck := store.JobGrowth{Lineage: "task-2", Allowed: true, Measured: true, Produced: 0, Red: 3}
	if !fruitless(rounds, len(rounds), stuck) {
		t.Fatal("a round that moved neither a file nor a finding is fruitless")
	}
	// And the symbol-level half of the same shortfall.
	rounds[0].Lost, moved.Red, moved.Lost = 8, 3, 0
	if fruitless(rounds, len(rounds), moved) {
		t.Fatal("a round that put back eight deleted public names is not fruitless")
	}
}

// ── a file the request names ─────────────────────────────────────────────────

// namedJob is a job whose request is whatever the caller wants it to say, in
// the workspace the caller lays out. It is inkJob with the brief opened up,
// because the reading under test is exactly the reading of that sentence.
func namedJob(t *testing.T, root, id, brief string) *store.Store {
	t.Helper()
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: id, Brief: brief},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: brief}); err != nil {
		t.Fatal(err)
	}
	node, _, _ := graph.Node(id)
	forgetJobWorkspaces()
	t.Cleanup(forgetJobWorkspaces)
	RememberJobWorkspace(graph, node, root)
	return graph
}

// namedWorkspace is the shape of the run #403 is about: the file the request is
// written around sits at the top of the workspace, beside the project's own
// furniture, and the request names something under a directory as well — so the
// focus is not empty and the narrowing is live.
func namedWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, path := range []string{"notes.md", "src/grid.ts", "package.json", "test/grid.test.ts"} {
		full := filepath.Join(root, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The #403 reading. The request is written around `notes.md`, the resolution of
// its other name reaches `src/grid.ts` and not the file at the top, and the
// round that edited the file the whole job is about used to be journaled as
// `produced: 0, scratch: 1`.
func TestAFileTheRequestNamesIsNeverScratch(t *testing.T) {
	root := namedWorkspace(t)
	graph := namedJob(t, root, "task-2",
		"Read src/grid.ts first, then make three edits to notes.md in sequence.")

	change := MeasureRound(graph, "task-2", root, scratchRun(t, root, "notes.md"))
	if !change.Moved() || len(change.Scratch) != 0 {
		t.Fatalf("relevant=%v scratch=%v, want the file the request names counted",
			change.Relevant, change.Scratch)
	}

	// And the narrowing is still narrowing: a file at the same top of the same
	// workspace that nothing named is scratch, which is the property a focus
	// entry sitting at the root must not quietly buy for everything beside it.
	change = MeasureRound(graph, "task-2", root, scratchRun(t, root, "debug-notes.ts"))
	if change.Moved() || len(change.Scratch) != 1 {
		t.Fatalf("relevant=%v scratch=%v, want the unnamed file left as scratch",
			change.Relevant, change.Scratch)
	}
}

// A name the workspace does not hold buys nothing. The focus is what it was
// before the request's own spelling was read, and so is every measurement made
// against it.
func TestARequestNamingAFileThatIsNotThereChangesNothing(t *testing.T) {
	root := namedWorkspace(t)
	absent := namedJob(t, root, "task-2",
		"Fix the layout in src/grid.ts, and keep missing.md up to date.")
	forgetJobWorkspaces()
	plain := namedJob(t, root, "task-3", "Fix the layout in src/grid.ts.")

	wrote := scratchRun(t, root, "src/box.ts", "notes.md", "debug-grid.ts")
	before := MeasureRound(plain, "task-3", root, wrote)
	after := MeasureRound(absent, "task-2", root, wrote)
	if strings.Join(after.Relevant, " ") != strings.Join(before.Relevant, " ") ||
		strings.Join(after.Scratch, " ") != strings.Join(before.Scratch, " ") {
		t.Fatalf("a name the tree does not hold moved the reading: %+v, want %+v", after, before)
	}
	if len(after.Scratch) == 0 {
		t.Fatalf("this fixture is meant to narrow something away: %+v", after)
	}
}

// The governor's own path, end to end: three rounds that each change the one
// file the request names are three rounds of progress, and the standstill
// sentence is never reached. Every reader of the round's measurement is
// checked here, because they all read the row this writes.
func TestRoundsThatChangeTheNamedFileAreNeverAStandstill(t *testing.T) {
	root := namedWorkspace(t)
	graph := namedJob(t, root, "task-2",
		"Read src/grid.ts first, then make three edits to notes.md in sequence.")
	node, _, _ := graph.Node("task-2")

	for round := 1; round <= 3; round++ {
		request := GrowRequest{JobRoot: "task-2", Node: node, Lineage: "task-2",
			Reason: GrowOverrun, Round: round, Measured: true,
			Artifacts: scratchRun(t, root, "notes.md"),
			// A fresh remainder each time: the fixed-point rule is a different
			// governor and the one under test here is the standstill.
			Remainder: RemainderDigest(fmt.Sprintf("section %d of the notes is still short", round))}
		verdict, err := growJob(context.Background(), graph, nil, request)
		if err != nil {
			t.Fatal(err)
		}
		if !verdict.Allow || verdict.Cause == CauseStandstill {
			t.Fatalf("round %d changed the file the request names and was refused: %+v", round, verdict)
		}
		admitGrowth(graph, request, verdict, 2)
	}

	// The first reader: the journal, which is what revision.productiveRounds
	// asks whether a round left anything in the world — a zero there un-spends
	// nothing and leaves every citation of that round spent.
	rounds, err := graph.JobGrowthRounds("task-2")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rounds {
		if !row.Allowed {
			continue
		}
		if !row.Measured || row.Produced < 1 || row.Scratch != 0 {
			t.Fatalf("journaled round = %+v, want produced ≥ 1 and nothing as scratch", row)
		}
	}

	// The second: the closing line a person reads, which names the last change
	// the job made through lastRelevantChange.
	standing, ok := GovernorStanding(graph, "task-2")
	if ok && strings.Contains(standing, "no relevant progress") {
		t.Fatalf("the standstill sentence was posted while the named file changed: %q", standing)
	}
	if line := lastRelevantChange(rounds, len(rounds)-1); line != "notes.md" {
		t.Fatalf("last change = %q, want the file the request names", line)
	}
}

// A ROUND THAT CHANGED A FILE THE REQUEST ITSELF NAMES IS A ROUND THAT MOVED THE
// WORK, and no reading of the world may call it a standstill. The standstill
// sentence is now an ending — the job stops gating, repairing and resuming on it
// — so a round misread as fruitless does not cost a wasted gate any more, it
// costs the work.
func TestAChangeToAFileTheRequestNamesIsNeverAStandstill(t *testing.T) {
	root := inkWorkspace(t)
	graph, err := store.Open(filepath.Join(t.TempDir(), "graph.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { graph.Close() })
	asked := "Read notes/RULINGS.md in full, then write the three lanes into report.md."
	if err := graph.Splice(store.RootID, store.Subtree{Nodes: []store.NodeSpec{
		{ID: "task-2", Brief: asked},
	}}, store.Provenance{Origin: store.OriginUser, SessionID: "s1", Intent: asked}); err != nil {
		t.Fatal(err)
	}
	node, _, _ := graph.Node("task-2")
	forgetJobWorkspaces()
	t.Cleanup(forgetJobWorkspaces)
	RememberJobWorkspace(graph, node, root)

	// Two rounds that really did write only scratch, so the job is one round
	// away from the standstill and the third round is the one being weighed.
	for _, name := range []string{"debug-one.ts", "debug-two.ts"} {
		request := GrowRequest{JobRoot: "task-2", Node: node, Lineage: "task-2",
			Reason: GrowOverrun, Round: 1, Measured: true, Artifacts: scratchRun(t, root, name)}
		admitGrowth(graph, request, GrowVerdict{Allow: true, Round: 1}, 1)
	}
	wrote := scratchRun(t, root, "report.md")
	if change := MeasureRound(graph, "task-2", root, wrote); !change.Moved() {
		t.Fatalf("the file the request names was read as scratch: %+v", change)
	}
	verdict, err := growJob(context.Background(), graph, nil, GrowRequest{
		JobRoot: "task-2", Node: node, Lineage: "task-2", Reason: GrowOverrun,
		Round: 3, Measured: true, Artifacts: wrote})
	if err != nil {
		t.Fatal(err)
	}
	if _, stopped := GrowthStopped(verdict.Cause); stopped {
		t.Fatalf("a round that wrote the file the request names was refused: %+v", verdict)
	}
}
