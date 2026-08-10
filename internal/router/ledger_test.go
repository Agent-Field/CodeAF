package router

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// TestLedgerPersistsAndReloads is the continual-learning claim in one test.
// Nothing about routing improves across runs unless what one process measured is
// on disk when the next one starts.
func TestLedgerPersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	first, err := LoadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		first.Observe("a/one", provider.ClassPlanSpine, 0, provider.VerdictVerifiedSuccess)
	}
	first.Alias("~a/one-latest", "a/one-0731")
	if err := first.Save(); err != nil {
		t.Fatal(err)
	}

	second, err := LoadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	rating, count := second.Rating("a/one", provider.ClassPlanSpine, 0)
	if count != 5 || rating <= 0 {
		t.Fatalf("reloaded rating = %.3f over %d observations, want the five that were recorded", rating, count)
	}
	if got := second.Resolve("~a/one-latest"); got != "a/one-0731" {
		t.Fatalf("Resolve = %q, want the snapshot recorded by the first process", got)
	}
}

// TestLedgerIgnoresUngradedVerdicts is the taxonomy doing its job. A transport
// failure and an answer nobody checked are both real events and neither is
// evidence about a model.
func TestLedgerIgnoresUngradedVerdicts(t *testing.T) {
	ledger, err := LoadLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, verdict := range []provider.Verdict{provider.VerdictProviderFailure, provider.VerdictUnverifiedSuccess} {
		ledger.Observe("a/one", provider.ClassExecLeaf, 0, verdict)
	}
	if _, count := ledger.Rating("a/one", provider.ClassExecLeaf, 0); count != 0 {
		t.Fatalf("ungraded verdicts produced %d observations", count)
	}
	for _, verdict := range []provider.Verdict{provider.VerdictBudgetStop, provider.VerdictTurnCap, provider.VerdictEmptyResponse} {
		ledger.Observe("a/one", provider.ClassExecLeaf, 0, verdict)
	}
	rating, count := ledger.Rating("a/one", provider.ClassExecLeaf, 0)
	if count != 3 || rating >= 0 {
		t.Fatalf("rating = %.3f over %d, want three negatives from the leaf failures", rating, count)
	}
}

// TestRatingsStayFiniteUnderOneSidedEvidence is the regularization guard, and it
// is here because the unregularized version of this fit genuinely broke: a model
// that passed every item it was shown ran to +42 logits, made the information
// matrix singular and collapsed separation reliability to zero.
func TestRatingsStayFiniteUnderOneSidedEvidence(t *testing.T) {
	ledger, err := LoadLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for range 2000 {
		ledger.Observe("perfect/model", provider.ClassPlanSpine, 0, provider.VerdictVerifiedSuccess)
		ledger.Observe("hopeless/model", provider.ClassPlanSpine, 0, provider.VerdictFormatFailure)
	}
	high, _ := ledger.Rating("perfect/model", provider.ClassPlanSpine, 0)
	low, _ := ledger.Rating("hopeless/model", provider.ClassPlanSpine, 0)
	if high > ratingBound || low < -ratingBound {
		t.Fatalf("ratings escaped their bounds: %.3f and %.3f", high, low)
	}
	if high <= low {
		t.Fatalf("ratings did not separate: %.3f against %.3f", high, low)
	}
	// A single observation may never swing a rating: that is what stops one
	// unlucky call from rewriting what a hundred calls established.
	before, _ := ledger.Rating("perfect/model", provider.ClassPlanSpine, 0)
	ledger.Observe("perfect/model", provider.ClassPlanSpine, 0, provider.VerdictFormatFailure)
	after, _ := ledger.Rating("perfect/model", provider.ClassPlanSpine, 0)
	if before-after > maxStep {
		t.Fatalf("one observation moved a rating by %.3f, over the %.2f clamp", before-after, maxStep)
	}
}

// TestColdStartUsesTheRoleHintAndNeverThePrice restates the finding this whole
// package is built to respect. Price does not predict ability: measured over the
// router lab's panel the correlation was 0.46 at n = 7, and the second-cheapest
// model had the second-highest ability while the second-dearest sat sixth of
// seven.
func TestColdStartUsesTheRoleHintAndNeverThePrice(t *testing.T) {
	if coldStart("top") <= coldStart("base") {
		t.Fatal("an operator's role hint is not being read")
	}
	if coldStart("") != 0 {
		t.Fatal("an unhinted model must start diffuse, not somewhere")
	}
	ledger, err := LoadLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if rating, count := ledger.Rating("unseen/model", provider.ClassPlanSpine, 1.5); rating != 1.5 || count != 0 {
		t.Fatalf("unseen model = %.2f over %d, want the prior and no evidence", rating, count)
	}
}

// TestConcurrentWritesDoNotCorruptTheLedger covers the case that actually
// happens: several aforge processes running at once, all doing read-modify-write
// on one small file. Without the lock the last writer out wins and everyone
// else's evidence disappears; without the atomic rename a reader finds half a
// file.
func TestConcurrentWritesDoNotCorruptTheLedger(t *testing.T) {
	dir := t.TempDir()
	const writers, each = 8, 10

	var group sync.WaitGroup
	for writer := range writers {
		group.Add(1)
		go func(writer int) {
			defer group.Done()
			// A separate Ledger per goroutine is the point: they stand in for
			// separate processes, so the in-process mutex is not what is under
			// test here — the file lock is.
			ledger, err := LoadLedger(dir)
			if err != nil {
				t.Error(err)
				return
			}
			for range each {
				ledger.Observe("shared/model", provider.ClassExecLeaf, 0, provider.VerdictVerifiedSuccess)
			}
			if err := ledger.Save(); err != nil {
				t.Error(err)
			}
		}(writer)
	}
	group.Wait()

	data, err := os.ReadFile(filepath.Join(dir, "router-ledger.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file ledgerFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("the ledger is not valid JSON after concurrent writes: %v\n%s", err, data)
	}
	final, err := LoadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, count := final.Rating("shared/model", provider.ClassExecLeaf, 0)
	if count != writers*each {
		t.Fatalf("recorded %d observations, want all %d — the merge lost evidence", count, writers*each)
	}
}

// TestPanelParsesBothForms covers the whole configuration surface. The
// comma-separated list is the one people type and it has to work with nothing
// else set up; the file is for a panel worth keeping.
func TestPanelParsesBothForms(t *testing.T) {
	empty, err := LoadPanel("  ")
	if err != nil || len(empty.Models) != 0 {
		t.Fatalf("an unset AFORGE_MODELS must be the kill switch, got %+v %v", empty, err)
	}

	list, err := LoadPanel(" a/one , b/two ,, c/three ")
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Models) != 3 || list.Models[0].Slug != "a/one" || list.Models[2].Slug != "c/three" {
		t.Fatalf("list panel = %+v", list.Models)
	}

	path := filepath.Join(t.TempDir(), "models.json")
	body := `{"max_output_price":3.0,"models":[
	  {"slug":"a/one","role":"base"},
	  {"slug":"b/two","role":"top","price":2.4}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	file, err := LoadPanel(path)
	if err != nil {
		t.Fatal(err)
	}
	if file.MaxOutputPrice != 3.0 || len(file.Models) != 2 || file.Models[1].Role != "top" || file.Models[1].Price != 2.4 {
		t.Fatalf("file panel = %+v", file)
	}
	if _, err := LoadPanel(filepath.Join(t.TempDir(), "panel.yaml")); err == nil {
		t.Fatal("a YAML path must be refused with an explanation, not parsed as a slug")
	}

	// The tilde is the trap. OpenRouter's floating-alias prefix opens a slug and
	// a home directory opens a path, and the harness's own default model is one
	// of the first kind — so a single-model panel naming it must not be read as
	// a filename.
	alias, err := LoadPanel("~deepseek/deepseek-v4-flash-latest")
	if err != nil {
		t.Fatalf("a floating-alias slug was read as a path: %v", err)
	}
	if len(alias.Models) != 1 || alias.Models[0].Slug != "~deepseek/deepseek-v4-flash-latest" {
		t.Fatalf("alias panel = %+v", alias.Models)
	}
	if !looksLikePath("~/.aforge/models.json") {
		t.Fatal("a home-directory path was read as a slug")
	}
}

// TestPanelRefusesAModelOverTheCap is the sanity cap doing what it is for. A
// typo in a slug that resolves to a frontier model should be found at startup,
// not on the invoice.
func TestPanelRefusesAModelOverTheCap(t *testing.T) {
	panel := Panel{MaxOutputPrice: 2.0, Models: []Spec{{Slug: "a/one", Price: 15.0}}}
	if _, err := New(panel, provider.Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}, t.TempDir()); err == nil {
		t.Fatal("a model over the cap was accepted")
	}
}

// TestAppendingAnEventRacesCleanlyWithClose is the locking story, and it is a
// -race test rather than an assertion test: a closed *os.File returns an error
// instead of panicking, so the unsynchronised read of the handle was invisible
// to every other means of noticing.
//
// It is a real sequence, not a contrived one. A leaf's settled verdict is
// appended from whichever goroutine reported it, which may be after the run has
// begun shutting down — so Append and Close genuinely overlap.
func TestAppendingAnEventRacesCleanlyWithClose(t *testing.T) {
	events, err := OpenEvents(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var writers sync.WaitGroup
	for range 8 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for range 50 {
				events.Append(Event{Class: string(provider.ClassExecLeaf), Model: "a/one",
					Verdict: provider.VerdictUnverifiedSuccess, Final: true})
			}
		}()
	}
	if err := events.Close(); err != nil {
		t.Fatal(err)
	}
	writers.Wait()
	// And closing twice is not an error, because the shutdown path joins several
	// closers and must not turn a second call into a failed run.
	if err := events.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestABudgetStopMovesARatingLessThanAWrongAnswer is defect 1's second half.
//
// Both verdicts are graded and both are negative, but they are not the same
// claim. A reply that parsed and was wrong is the model failing at the work; a
// leaf that exhausted its budget may be the model wandering, or it may be a leaf
// that was three nodes' worth of work — and BASELINE.md measured a byte-identical
// brief drawing anywhere from 5 to 26 nodes, so sizing dominates what a leaf
// costs. Arm B let five of the second kind outvote a prior; a quarter of a step
// is what stops that arithmetic from working.
func TestABudgetStopMovesARatingLessThanAWrongAnswer(t *testing.T) {
	ledger, err := LoadLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ledger.Observe("a/one", provider.ClassExecLeaf, 0, provider.VerdictBudgetStop)
	ledger.Observe("b/two", provider.ClassExecLeaf, 0, provider.VerdictSemanticFailure)

	stopped, _ := ledger.Rating("a/one", provider.ClassExecLeaf, 0)
	wrong, _ := ledger.Rating("b/two", provider.ClassExecLeaf, 0)
	if stopped >= 0 || wrong >= 0 {
		t.Fatalf("budget stop = %.4f, wrong answer = %.4f, want both negative", stopped, wrong)
	}
	if stopped <= wrong {
		t.Fatalf("budget stop moved the rating to %.4f and a wrong answer to %.4f — "+
			"a sizing failure must not count for as much as an ability one", stopped, wrong)
	}
	// A quarter, not an arbitrary fraction: four oversized leaves say about as
	// much about a model as one wrong answer does.
	if ratio := stopped / wrong; ratio < 0.2 || ratio > 0.3 {
		t.Fatalf("a budget stop moved %.3f of a wrong answer's step, want about a quarter", ratio)
	}
}

// TestBudgetStopsStillCountTowardsTheGate keeps the two brakes independent. The
// weight decides how far an observation may move a rating; the count decides how
// much the router has looked at. A down-weighted outcome is still something it
// saw, and conflating the two would hold the gate shut forever on a model that
// only ever runs out of budget.
func TestBudgetStopsStillCountTowardsTheGate(t *testing.T) {
	ledger, err := LoadLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for range MinGraded {
		ledger.Observe("a/one", provider.ClassExecLeaf, 0, provider.VerdictBudgetStop)
	}
	if _, count := ledger.Rating("a/one", provider.ClassExecLeaf, 0); count != MinGraded {
		t.Fatalf("count = %d after %d budget stops, want every one counted", count, MinGraded)
	}
}

// holdLock takes the ledger's lock file the way another aforge process would,
// so a flush has to wait out its retries.
func holdLock(t *testing.T, dir string) string {
	t.Helper()
	lock := filepath.Join(dir, "router-ledger.json.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(lock) })
	return lock
}

// The contention this fixes. flush takes a file lock with retries, re-reads,
// re-parses, re-encodes and renames — on the order of a second when another
// process holds the lock — and it used to do all of that with the mutex that
// ranking reads through held. Routing one call reads the ledger once per rung,
// so every rung of every call queued behind the disk.
func TestAFlushBlockedOnTheFileLockDoesNotBlockRanking(t *testing.T) {
	dir := t.TempDir()
	ledger, err := LoadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	holdLock(t, dir)

	flushed := make(chan struct{})
	go func() {
		defer close(flushed)
		ledger.Observe("a/one", provider.ClassPlanSpine, 0, provider.VerdictVerifiedSuccess)
	}()

	// The observation is in memory before the file work starts, so the reading
	// side both sees it and is not made to wait for it.
	deadline := time.Now().Add(2 * time.Second)
	for {
		started := time.Now()
		_, count := ledger.Rating("a/one", provider.ClassPlanSpine, 0)
		if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
			t.Fatalf("a rating read waited %s on a flush", elapsed)
		}
		if count == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the observation never reached the map")
		}
		time.Sleep(time.Millisecond)
	}

	// Reads keep working for as long as the flush is stuck.
	for range 20 {
		started := time.Now()
		ledger.Read(provider.ClassPlanSpine, []Query{{Slug: "a/one"}, {Slug: "b/two"}})
		if elapsed := time.Since(started); elapsed > 250*time.Millisecond {
			t.Fatalf("a panel read waited %s on a flush", elapsed)
		}
	}
	<-flushed

	// And the evidence is not lost by the flush that could not land: it is
	// queued, and the next successful flush carries it to the file.
	os.Remove(filepath.Join(dir, "router-ledger.json.lock"))
	if err := ledger.Save(); err != nil {
		t.Fatal(err)
	}
	reloaded, err := LoadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, count := reloaded.Rating("a/one", provider.ClassPlanSpine, 0); count != 1 {
		t.Fatalf("a flush that lost its lock lost the observation: count = %d", count)
	}
}

// Read is what ranking uses, and every rung in one ordering has to come from
// one instant — including the alias indirection, which used to be a second
// acquisition per rung.
func TestReadRatesAWholePanelThroughItsAliases(t *testing.T) {
	ledger, err := LoadLedger(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ledger.Alias("~vendor/model-latest", "vendor/model-0731")
	for range MinGraded {
		ledger.Observe("vendor/model-0731", provider.ClassPlanSpine, 0, provider.VerdictVerifiedSuccess)
	}
	readings := ledger.Read(provider.ClassPlanSpine, []Query{
		{Slug: "~vendor/model-latest"},
		{Slug: "unmeasured/model", Prior: 1.5},
	})
	if readings[0].Count != MinGraded || readings[0].Rating <= 0 {
		t.Fatalf("aliased reading = %+v, want the snapshot's record", readings[0])
	}
	if readings[1].Count != 0 || readings[1].Rating != 1.5 {
		t.Fatalf("unmeasured reading = %+v, want the cold-start prior", readings[1])
	}
}

// A closed ledger stops waiting. Verdicts are reported by whichever goroutine
// settled them, which may be after the run has begun shutting down, and a late
// observation must not hold the process open for a lock another process has.
func TestAClosedLedgerAbandonsTheLockWait(t *testing.T) {
	dir := t.TempDir()
	ledger, err := LoadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	holdLock(t, dir)

	settled := make(chan time.Duration, 1)
	go func() {
		started := time.Now()
		ledger.Observe("late/model", provider.ClassPlanSpine, 0, provider.VerdictVerifiedSuccess)
		settled <- time.Since(started)
	}()
	select {
	case elapsed := <-settled:
		// The full retry ladder is forty waits of 25ms; a closed ledger takes
		// none of them.
		if elapsed > 300*time.Millisecond {
			t.Fatalf("a late observation waited %s on a closed ledger", elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a late observation blocked a closed ledger")
	}
	// Closing twice is what a replaced router and an exiting process both do,
	// and it must not panic on an already-closed channel.
	_ = ledger.Close()
}

// A plan build grades its spine in one burst — eight to twelve observations,
// then one per leaf and one per judge — and each of them used to take the lock
// file, re-read, re-parse, re-encode and rename, serialised process-wide. The
// burst rides one write now, and the file it leaves is the same file.
func TestABurstOfObservationsCostsOneWrite(t *testing.T) {
	dir := t.TempDir()
	debounced, err := LoadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	for range 40 {
		debounced.Observe("a/one", provider.ClassPlanSpine, 0, provider.VerdictVerifiedSuccess)
		debounced.Observe("b/two", provider.ClassExecLeaf, 0, provider.VerdictBudgetStop)
	}
	debounced.Alias("~a/one-latest", "a/one-0731")
	if err := debounced.Save(); err != nil {
		t.Fatal(err)
	}
	if writes := debounced.writes.Load(); writes > 2 {
		t.Fatalf("81 observations took %d writes of the ledger file", writes)
	}

	// The same evidence, in the same order, written one observation at a time:
	// the debounce must not have changed a byte of what ends up on disk.
	eager := t.TempDir()
	oneByOne, err := LoadLedger(eager)
	if err != nil {
		t.Fatal(err)
	}
	for range 40 {
		oneByOne.Observe("a/one", provider.ClassPlanSpine, 0, provider.VerdictVerifiedSuccess)
		if err := oneByOne.Save(); err != nil {
			t.Fatal(err)
		}
		oneByOne.Observe("b/two", provider.ClassExecLeaf, 0, provider.VerdictBudgetStop)
		if err := oneByOne.Save(); err != nil {
			t.Fatal(err)
		}
	}
	oneByOne.Alias("~a/one-latest", "a/one-0731")
	if err := oneByOne.Save(); err != nil {
		t.Fatal(err)
	}

	batched, individual := readLedgerFile(t, dir), readLedgerFile(t, eager)
	if !bytes.Equal(stripUpdated(t, batched), stripUpdated(t, individual)) {
		t.Fatalf("the debounced ledger is not the eager one:\n%s\n\n%s", batched, individual)
	}
}

// Crash durability is the window and no more: an observation nobody saves
// reaches the file within it, on its own.
func TestAnObservationReachesTheFileWithoutASave(t *testing.T) {
	dir := t.TempDir()
	ledger, err := LoadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	ledger.Observe("a/one", provider.ClassPlanSpine, 0, provider.VerdictVerifiedSuccess)

	deadline := time.Now().Add(4 * ledgerDebounce)
	for {
		reloaded, err := LoadLedger(dir)
		if err != nil {
			t.Fatal(err)
		}
		if _, count := reloaded.Rating("a/one", provider.ClassPlanSpine, 0); count == 1 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("an observation never reached the file inside %s", 4*ledgerDebounce)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Save and Close do not wait out the window. The end of a run is where the
// evidence has to be on disk, not a second later.
func TestSaveLandsTheQueueImmediately(t *testing.T) {
	dir := t.TempDir()
	ledger, err := LoadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	ledger.Observe("a/one", provider.ClassPlanSpine, 0, provider.VerdictVerifiedSuccess)
	started := time.Now()
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > ledgerDebounce/2 {
		t.Fatalf("closing waited %s for the debounce window", elapsed)
	}
	reloaded, err := LoadLedger(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, count := reloaded.Rating("a/one", provider.ClassPlanSpine, 0); count != 1 {
		t.Fatal("closing left the observation in memory")
	}
}

func readLedgerFile(t *testing.T, dir string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "router-ledger.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// stripUpdated removes the one field that is a wall-clock reading rather than a
// measurement, so two runs of the same evidence can be compared byte for byte.
func stripUpdated(t *testing.T, data []byte) []byte {
	t.Helper()
	var file ledgerFile
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("the ledger is not valid JSON: %v", err)
	}
	for index := range file.Entries {
		file.Entries[index].Updated = time.Time{}
	}
	encoded, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return encoded
}
