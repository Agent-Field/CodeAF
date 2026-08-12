package store

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The property this file locks is the one 13.11 filed and could not draw: a
// tree level can be asked what it cost. The arithmetic matters, but the two
// laws that matter more are that a level nobody billed says so rather than
// saying zero, and that a job filed away keeps every part it ever had.

func receiptStore(t *testing.T, name string) *Store {
	t.Helper()
	return openTestStore(t, filepath.Join(t.TempDir(), name+".db"))
}

// runNode claims and starts a node, leaving it running.
func runNode(t *testing.T, graph *Store, id string) Claim {
	t.Helper()
	claim := mustClaim(t, graph, id, "worker")
	if err := graph.Start(claim); err != nil {
		t.Fatalf("start %q: %v", id, err)
	}
	return claim
}

// finish takes a node all the way through the real lifecycle, because the
// clocks and statuses this read is about are written by it and not by hand.
func finish(t *testing.T, graph *Store, id string, ending Status) {
	t.Helper()
	claim := runNode(t, graph, id)
	var err error
	switch ending {
	case Done:
		err = graph.Complete(claim, "settled")
	case Failed:
		err = graph.Fail(claim, "did not work")
	default:
		t.Fatalf("finish %q: %s is not an ending", id, ending)
	}
	if err != nil {
		t.Fatalf("finish %q as %s: %v", id, ending, err)
	}
}

// clock rewrites one node's start and finish stamps. The lifecycle stamps them
// with the wall clock, which is right for the machine and useless for asserting
// that a wall is a wall: these tests need to know what the answer should be. A
// zero time clears the column, which is how a node that never started or never
// recorded an ending is spelled.
func clock(t *testing.T, graph *Store, id string, started, finished time.Time) {
	t.Helper()
	stamp := func(at time.Time) any {
		if at.IsZero() {
			return nil
		}
		return formatTime(at)
	}
	if _, err := graph.db.Exec(`UPDATE nodes SET started_at = ?, finished_at = ? WHERE id = ?`,
		stamp(started), stamp(finished), id); err != nil {
		t.Fatalf("clock %q: %v", id, err)
	}
}

func mustReceipt(t *testing.T, ledger SubtreeLedger, id string) NodeReceipt {
	t.Helper()
	receipt, found := ledger.Receipt(id)
	if !found {
		t.Fatalf("no receipt for %q; the ledger holds %v", id, ledgerIDs(ledger))
	}
	return receipt
}

func mustRollup(t *testing.T, ledger SubtreeLedger, id string) SubtreeRollup {
	t.Helper()
	roll, found := ledger.Rollup(id)
	if !found {
		t.Fatalf("no rollup for %q; the ledger holds %v", id, ledgerIDs(ledger))
	}
	return roll
}

func ledgerIDs(ledger SubtreeLedger) []string {
	ids := make([]string, 0, len(ledger.Receipts))
	for _, receipt := range ledger.Receipts {
		ids = append(ids, receipt.NodeID)
	}
	return ids
}

// plantSettledJob is the multi-part job most of these tests read: a root that
// paid for its own judging, one part that worked, one that failed across two
// attempts, one that produced nothing measurable, and a worker one level down.
func plantSettledJob(t *testing.T, graph *Store) *Store {
	t.Helper()
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "the whole errand", Stage: 3},
		{ID: "alpha", Parent: "job", Brief: "the part that worked", Stage: 2},
		{ID: "alpha-w", Parent: "alpha", Brief: "the worker under it", Stage: 1},
		{ID: "beta", Parent: "job", Brief: "the part that did not", Stage: 2},
		{ID: "gamma", Parent: "job", Brief: "the part nobody billed", Stage: 2},
	}}, Provenance{Origin: OriginUser, SessionID: "room", Intent: "the whole errand"}); err != nil {
		t.Fatal(err)
	}
	finish(t, graph, "alpha-w", Done)
	finish(t, graph, "alpha", Done)
	finish(t, graph, "beta", Failed)
	finish(t, graph, "gamma", Done)
	finish(t, graph, "job", Done)

	// The worker's own tool loop, billed once on the way out — which is where
	// every leaf's money comes from.
	bill(t, graph, NodeUsage{NodeID: "alpha-w", PromptTokens: 40000, CompletionTokens: 800, Cost: 0.30})
	// The part that failed twice: two rows, one node.
	bill(t, graph, NodeUsage{NodeID: "beta", PromptTokens: 5000, CompletionTokens: 100, Cost: 0.04})
	bill(t, graph, NodeUsage{NodeID: "beta", PromptTokens: 6000, CompletionTokens: 120, Cost: 0.06})
	// The root's own judging call, billed against the root through WithSpendNode.
	bill(t, graph, NodeUsage{NodeID: "job", PromptTokens: 2000, CompletionTokens: 50, Cost: 0.02})
	// gamma and alpha are deliberately unbilled: a step that only supervised,
	// and a part whose run recorded nothing.
	return graph
}

func TestEveryPartOfAJobHasItsOwnReceiptAndTheRootHasTheirSum(t *testing.T) {
	graph := plantSettledJob(t, receiptStore(t, "parts"))

	ledger, err := graph.SubtreeReceipts("job")
	if err != nil {
		t.Fatal(err)
	}
	if got := ledgerIDs(ledger); len(got) != 5 {
		t.Fatalf("ledger = %v; want the root and its four descendants", got)
	}

	worker := mustReceipt(t, ledger, "alpha-w")
	if !worker.Billed() || worker.Runs != 1 || worker.Cost != 0.30 {
		t.Fatalf("worker receipt = %+v; want one run at $0.30", worker)
	}
	if worker.PromptTokens != 40000 || worker.CompletionTokens != 800 {
		t.Fatalf("worker tokens = %+v; want the row's own", worker)
	}
	if worker.Parent != "alpha" || worker.Status != Done {
		t.Fatalf("worker receipt = %+v; want alpha's child, done", worker)
	}

	// A node that ran twice contributes both of its runs, and neither of them
	// climbs into a sibling.
	failed := mustReceipt(t, ledger, "beta")
	if failed.Runs != 2 || failed.Cost < 0.0999 || failed.Cost > 0.1001 {
		t.Fatalf("twice-run receipt = %+v; want two runs at $0.10", failed)
	}

	// A parent's own line is its own line: the money a judge spent on the root
	// is the root's, and is NOT the subtree's total.
	root := mustReceipt(t, ledger, "job")
	if root.Runs != 1 || root.Cost != 0.02 {
		t.Fatalf("root receipt = %+v; want only its own judging call", root)
	}

	// The rollup is where the subtree's money is: every run once, no child's
	// spend counted twice for its parent.
	roll := mustRollup(t, ledger, "job")
	if roll.Nodes != 5 || roll.Runs != 4 {
		t.Fatalf("rollup = %+v; want five nodes and four runs", roll)
	}
	if roll.Cost < 0.4199 || roll.Cost > 0.4201 {
		t.Fatalf("rollup cost = %v; want $0.42", roll.Cost)
	}
	if roll.PromptTokens != 53000 || roll.CompletionTokens != 1070 {
		t.Fatalf("rollup tokens = %+v; want every row's, once", roll)
	}

	// And the store's one-figure door answers the same thing, because it is the
	// same read: a card and the room it opens must not disagree.
	direct, found, err := graph.SubtreeRollup("job")
	if err != nil || !found {
		t.Fatalf("SubtreeRollup = found %v, err %v", found, err)
	}
	if direct != roll {
		t.Fatalf("SubtreeRollup = %+v; want the ledger's own %+v", direct, roll)
	}
}

func TestAPartNobodyBilledHasNoCostRatherThanAZeroOne(t *testing.T) {
	graph := plantSettledJob(t, receiptStore(t, "absent"))

	ledger, err := graph.SubtreeReceipts("job")
	if err != nil {
		t.Fatal(err)
	}

	// gamma ran and settled. Nothing was ever billed against it, so it has no
	// money at all — 8.2.20's — and not $0.00.
	unmeasured := mustReceipt(t, ledger, "gamma")
	if unmeasured.Billed() {
		t.Fatalf("an unbilled part reports Billed; want absence, got %+v", unmeasured)
	}
	if unmeasured.Status != Done {
		t.Fatalf("gamma = %s; want a part that finished without costing anything measurable", unmeasured.Status)
	}

	// A step whose whole branch is unbilled rolls up absent too — the absence
	// survives the aggregation rather than becoming a zero at the first sum.
	if roll := mustRollup(t, ledger, "gamma"); roll.Billed() || roll.Cost != 0 {
		t.Fatalf("rollup of an unbilled branch = %+v; want absence", roll)
	}

	// And a run that genuinely cost nothing is the other answer, all the way
	// through: measured, and free.
	bill(t, graph, NodeUsage{NodeID: "gamma", PromptTokens: 30, CompletionTokens: 4})
	ledger, err = graph.SubtreeReceipts("job")
	if err != nil {
		t.Fatal(err)
	}
	free := mustReceipt(t, ledger, "gamma")
	if !free.Billed() || free.Cost != 0 {
		t.Fatalf("a free run = %+v; want Billed with zero cost", free)
	}
	if roll := mustRollup(t, ledger, "gamma"); !roll.Billed() || roll.Cost != 0 {
		t.Fatalf("rollup of a free run = %+v; want Billed with zero cost", roll)
	}

	// A root the graph has never heard of has no rollup at all, which a caller
	// must not draw as an empty job.
	if _, found, err := graph.SubtreeRollup("no-such-job"); err != nil || found {
		t.Fatalf("SubtreeRollup of an unknown root = found %v, err %v; want absent", found, err)
	}
}

func TestASubtreesWallIsTheTimeItOccupiedAndNotTheSumOfItsParts(t *testing.T) {
	graph := receiptStore(t, "wall")
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "four workers side by side", Stage: 2},
		{ID: "one", Parent: "job", Brief: "first", Stage: 1},
		{ID: "two", Parent: "job", Brief: "second", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "room", Intent: "side by side"}); err != nil {
		t.Fatal(err)
	}
	finish(t, graph, "one", Done)
	finish(t, graph, "two", Done)
	finish(t, graph, "job", Done)

	base := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	// Two ten-minute workers, overlapping. The job took twenty-two minutes of
	// wall; the sum of its parts is twenty, and that number is a lie about a
	// parallel job.
	clock(t, graph, "job", base, base.Add(22*time.Minute))
	clock(t, graph, "one", base.Add(1*time.Minute), base.Add(11*time.Minute))
	clock(t, graph, "two", base.Add(11*time.Minute), base.Add(21*time.Minute))

	ledger, err := graph.SubtreeReceipts("job")
	if err != nil {
		t.Fatal(err)
	}
	now := base.Add(time.Hour)

	elapsed, known := mustReceipt(t, ledger, "one").Elapsed(now)
	if !known || elapsed != 10*time.Minute {
		t.Fatalf("one part's clock = %v (known %v); want 10m", elapsed, known)
	}
	roll := mustRollup(t, ledger, "job")
	wall, known := roll.Elapsed(now)
	if !known || wall != 22*time.Minute {
		t.Fatalf("subtree wall = %v (known %v); want the 22m it occupied, never the 20m its parts add to",
			wall, known)
	}
	if !roll.Started.Equal(base) || !roll.Settled.Equal(base.Add(22*time.Minute)) {
		t.Fatalf("wall ends = %v .. %v; want the earliest start and the latest settle", roll.Started, roll.Settled)
	}
	if roll.Live {
		t.Fatalf("a settled job reports Live; want a closed wall, got %+v", roll)
	}
}

func TestWorkThatHasNotStartedHasNoClockAndWorkStillRunningIsMeasuredAgainstNow(t *testing.T) {
	graph := receiptStore(t, "live")
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "still going", Stage: 2},
		{ID: "settled", Parent: "job", Brief: "the part that landed", Stage: 1},
		{ID: "live", Parent: "job", Brief: "the part still at it", Stage: 1},
		{ID: "queued", Parent: "job", Brief: "the part that has not begun", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "room", Intent: "still going"}); err != nil {
		t.Fatal(err)
	}
	finish(t, graph, "settled", Done)
	runNode(t, graph, "live")
	runNode(t, graph, "job")

	base := time.Date(2026, 8, 11, 9, 0, 0, 0, time.UTC)
	clock(t, graph, "job", base, time.Time{})
	clock(t, graph, "settled", base.Add(time.Minute), base.Add(6*time.Minute))
	clock(t, graph, "live", base.Add(2*time.Minute), time.Time{})

	ledger, err := graph.SubtreeReceipts("job")
	if err != nil {
		t.Fatal(err)
	}
	now := base.Add(30 * time.Minute)

	// Nothing has been asked of the queued part yet, so there is nothing to
	// measure. A zero duration would draw as "0s", which is a claim.
	queued := mustReceipt(t, ledger, "queued")
	if _, known := queued.Elapsed(now); known {
		t.Fatalf("a queued part reports a clock; want absence, got %+v", queued)
	}
	if queued.Open() {
		t.Fatalf("a queued part reports Open; want work that has not started, got %+v", queued)
	}

	// The live part's clock runs to now, and its MONEY does not exist: a leaf
	// is billed once on the way out, so a running leaf reports absence rather
	// than a figure this read invented for it.
	live := mustReceipt(t, ledger, "live")
	elapsed, known := live.Elapsed(now)
	if !known || elapsed != 28*time.Minute {
		t.Fatalf("a live part's clock = %v (known %v); want 28m against now", elapsed, known)
	}
	if live.Billed() {
		t.Fatalf("a live part reports Billed; want cost-so-far absent, got %+v", live)
	}

	// But money billed against a live node by somebody else — a judge, a
	// sentinel, an earlier attempt — is real and is shown. Cost-so-far is a
	// floor, never a forecast.
	bill(t, graph, NodeUsage{NodeID: "live", PromptTokens: 900, Cost: 0.07})
	ledger, err = graph.SubtreeReceipts("job")
	if err != nil {
		t.Fatal(err)
	}
	if live = mustReceipt(t, ledger, "live"); !live.Billed() || live.Cost != 0.07 {
		t.Fatalf("a live part with a journaled run = %+v; want $0.07 so far", live)
	}

	roll := mustRollup(t, ledger, "job")
	if !roll.Live {
		t.Fatalf("a job with a running part = %+v; want Live", roll)
	}
	wall, known := roll.Elapsed(now)
	if !known || wall != 30*time.Minute {
		t.Fatalf("an open wall = %v (known %v); want the 30m since it opened", wall, known)
	}
	if !roll.Settled.IsZero() && roll.Live && wall != now.Sub(roll.Started) {
		t.Fatalf("an open wall was measured to a settle stamp; want it measured to now")
	}
}

func TestACollapsedParentCountsWhatIsRunningDoneFailedAndQueuedBeneathIt(t *testing.T) {
	graph := receiptStore(t, "census")
	if err := graph.Splice(RootID, Subtree{Nodes: []NodeSpec{
		{ID: "job", Brief: "a job in every state at once", Stage: 2},
		{ID: "won", Parent: "job", Brief: "done", Stage: 1},
		{ID: "lost", Parent: "job", Brief: "failed", Stage: 1},
		{ID: "going", Parent: "job", Brief: "running", Stage: 1},
		{ID: "waiting", Parent: "job", Brief: "pending", Stage: 1},
		{ID: "picked", Parent: "job", Brief: "claimed but not moved", Stage: 1},
	}}, Provenance{Origin: OriginUser, SessionID: "room", Intent: "every state"}); err != nil {
		t.Fatal(err)
	}
	finish(t, graph, "won", Done)
	finish(t, graph, "lost", Failed)
	runNode(t, graph, "going")
	mustClaim(t, graph, "picked", "worker")
	runNode(t, graph, "job")

	roll, found, err := graph.SubtreeRollup("job")
	if err != nil || !found {
		t.Fatalf("SubtreeRollup = found %v, err %v", found, err)
	}
	want := StateCounts{Queued: 2, Running: 2, Done: 1, Failed: 1}
	if roll.States != want {
		t.Fatalf("census = %+v; want %+v — a claim is a worker picking work up and not work moving",
			roll.States, want)
	}
	if roll.States.Total() != roll.Nodes {
		t.Fatalf("census totals %d over %d nodes; every member must be counted exactly once",
			roll.States.Total(), roll.Nodes)
	}
	if roll.States.Settled() != 2 {
		t.Fatalf("settled = %d; want the done one and the failed one", roll.States.Settled())
	}
}

func TestARollupCanBeTakenAtAnyLevelAndALeafRollsUpToItself(t *testing.T) {
	graph := plantSettledJob(t, receiptStore(t, "levels"))

	ledger, err := graph.SubtreeReceipts("job")
	if err != nil {
		t.Fatal(err)
	}

	// A step in the middle: its own line says nothing was billed to it, and its
	// branch says its worker spent $0.30. Both are true, and a tree draws the
	// second when the step is collapsed and the first when it is open.
	step := mustReceipt(t, ledger, "alpha")
	if step.Billed() {
		t.Fatalf("a supervising step = %+v; want no money of its own", step)
	}
	branch := mustRollup(t, ledger, "alpha")
	if branch.Nodes != 2 || branch.Runs != 1 || branch.Cost != 0.30 {
		t.Fatalf("branch rollup = %+v; want the step and its worker at $0.30", branch)
	}

	// A leaf's rollup is its own receipt, deliberately and not as a special
	// case: a row and the collapsed form of that row may never disagree.
	leaf := mustReceipt(t, ledger, "alpha-w")
	leafRoll := mustRollup(t, ledger, "alpha-w")
	if leafRoll.Nodes != 1 || leafRoll.Runs != leaf.Runs || leafRoll.Cost != leaf.Cost {
		t.Fatalf("leaf rollup = %+v; want its own receipt %+v", leafRoll, leaf)
	}

	// A rollup never climbs out of the tree it was given: rolling up a step
	// cannot reach its parent's money or its siblings'.
	if branch.Cost >= mustRollup(t, ledger, "job").Cost {
		t.Fatalf("branch rollup %v is not less than the job's; a rollup climbed upward", branch.Cost)
	}

	// And a node outside this subtree has no rollup here at all.
	if _, found := ledger.Rollup(RootID); found {
		t.Fatalf("the spine rolled up inside a job's ledger; want the read bounded by its root")
	}
}

// TestFoldingAJobDoesNotHideItsPartsFromItsReceipts is 257800f's property
// applied to money. Folding is what happens to every job shortly after it
// settles, so a receipt read that honoured folding would go blank on precisely
// the jobs a reader scrolls back to — and it would go blank silently, which is
// the failure mode that report described: `$0.16 · 4 workers` becoming `atomic`.
func TestFoldingAJobDoesNotHideItsPartsFromItsReceipts(t *testing.T) {
	graph := plantSettledJob(t, receiptStore(t, "folded"))

	before, err := graph.SubtreeReceipts("job")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Fold("job", "the errand, in one line", nil); err != nil {
		t.Fatal(err)
	}

	// The active view now speaks for the whole job with one row. That is right
	// for a home rail and is exactly what this read must not do.
	active, err := graph.ActiveNodes()
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range active {
		if node.ID == "alpha-w" {
			t.Fatalf("the fold left a member in the active view; the fixture is not testing what it claims")
		}
	}

	after, err := graph.SubtreeReceipts("job")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ledgerIDs(after), ledgerIDs(before); len(got) != len(want) {
		t.Fatalf("after folding the ledger holds %v; want the same %v", got, want)
	}
	for index, receipt := range after.Receipts {
		was := before.Receipts[index]
		if receipt.NodeID != was.NodeID || receipt.Runs != was.Runs || receipt.Cost != was.Cost {
			t.Fatalf("folding changed %q's receipt: %+v then %+v", was.NodeID, was, receipt)
		}
		if !receipt.Folded {
			t.Fatalf("%q reads unfolded after its job was folded; want history reported, not hidden", receipt.NodeID)
		}
	}
	rolledBefore := mustRollup(t, before, "job")
	rolledAfter := mustRollup(t, after, "job")
	if rolledBefore.Cost != rolledAfter.Cost || rolledBefore.Nodes != rolledAfter.Nodes {
		t.Fatalf("folding changed the bill: %+v then %+v", rolledBefore, rolledAfter)
	}
}

func TestReceiptsSurviveRebuild(t *testing.T) {
	graph := plantSettledJob(t, receiptStore(t, "rebuild"))

	before, _, err := graph.SubtreeRollup("job")
	if err != nil {
		t.Fatal(err)
	}
	if err := graph.Rebuild(); err != nil {
		t.Fatal(err)
	}
	after, _, err := graph.SubtreeRollup("job")
	if err != nil {
		t.Fatal(err)
	}
	// The clocks are journal-derived and replay identically; compare the money
	// and the census, which are what a rebuild could plausibly lose.
	if before.Runs != after.Runs || before.Cost != after.Cost || before.States != after.States {
		t.Fatalf("rebuild changed the receipts: %+v then %+v", before, after)
	}
}

// TestSubtreeReceiptsReadOnlyTheSubtree is 12.1.6's perf ledger applied to a
// read a task room makes on entry, stated as a plan rather than a stopwatch.
// subtreeSpend's comment names the exact failure: written as a plain join,
// SQLite reads the whole usage table and builds a throwaway index over the
// subtree instead. The CROSS JOINs are what stop it, and this test exists so a
// later tidy-up of either cannot quietly turn a per-room read into a scan of
// every run this machine has ever recorded.
func TestSubtreeReceiptsReadOnlyTheSubtree(t *testing.T) {
	graph := plantSettledJob(t, receiptStore(t, "plans"))

	rows, err := graph.db.Query("EXPLAIN QUERY PLAN "+subtreeReceiptsQuery, "job")
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	defer rows.Close()
	var lines []string
	for rows.Next() {
		var id, parent, notUsed int
		var detail string
		if err := rows.Scan(&id, &parent, &notUsed, &detail); err != nil {
			t.Fatalf("explain: %v", err)
		}
		lines = append(lines, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("explain: %v", err)
	}
	plan := strings.Join(lines, "\n")

	if strings.Contains(plan, "SCAN usage") {
		t.Fatalf("the receipts read scans every run ever recorded:\n%s", plan)
	}
	if strings.Contains(plan, "SCAN nodes") {
		t.Fatalf("the receipts read scans the whole node table:\n%s", plan)
	}
	if !strings.Contains(plan, "usage_node") {
		t.Fatalf("spend does not enter usage by its node index:\n%s", plan)
	}
}
