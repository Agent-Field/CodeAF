package standing

// completion_finish_test.go is the disk-fault proof for the completed-firing
// settlement: a task whose post-run item write is lost is completed from its own
// run record instead of being read as ambiguous (D1), and a say recovered from
// the ledger restores the cost, outcome and history that firing actually
// produced instead of a zero and a fresh clock (D2). Each test drives a REAL
// filesystem fault at the moment of the lost write; no production knob exists.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

// rootFlipTaskRunner runs a task for real and, while the run is in progress,
// makes the store root read-only, so the item write that would record the firing
// fails exactly where a crash between the run and that write would leave it. The
// pre-run marker write and the append-only ledger line both land before it.
type rootFlipTaskRunner struct {
	*fakeRunner
	root  string
	armed bool
}

func (r *rootFlipTaskRunner) Run(ctx context.Context, item Item, runDir, evidence string) (Outcome, error) {
	if r.armed {
		if err := os.Chmod(r.root, 0o500); err != nil {
			return Outcome{}, err
		}
	}
	return r.fakeRunner.Run(ctx, item, runDir, evidence)
}

// billOnDeliverRunner carries a billed line out and then makes the store root
// read-only, so the write that would clear the intent and retire a fulfilled
// one-shot fails after the ledger line landed.
type billOnDeliverRunner struct {
	fakeRunner
	root   string
	armed  bool
	billed float64
	ids    []string
	calls  int
}

func (r *billOnDeliverRunner) Deliver(_ context.Context, _ Item, pending Pending) (Outcome, error) {
	r.calls++
	r.ids = append(r.ids, pending.ID)
	if r.armed {
		if err := os.Chmod(r.root, 0o500); err != nil {
			return Outcome{}, err
		}
	}
	return Outcome{Kind: "said", USD: r.billed}, nil
}

// A COMPLETED ONE-SHOT TASK WHOSE POST-RUN WRITE IS LOST RETIRES FROM ITS RUN
// RECORD AND IS NEVER RUN AGAIN. The pre-run marker and the ledger line are the
// durable evidence; the restart books the firing from that record, clears the
// marker, retires the one-shot and never writes the false "waiting for you" line
// for a task that already finished.
func TestALostPostRunTaskWriteSettlesFromItsRunRecordAndRetiresOnce(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	// The day's ledger FILE must exist before the root is momentarily read-only,
	// so the firing's own append-only line can still land.
	if err := store.Append(Entry{At: now, ItemID: "seed", Kind: entryCheck}); err != nil {
		t.Fatal(err)
	}
	runner := &rootFlipTaskRunner{
		fakeRunner: &fakeRunner{evidence: `{"ready": true}`, outcome: Outcome{Kind: "landed", USD: 0.30}},
		root:       store.Root(),
	}
	item := oneShotProbe("when ready becomes true, fix the migration once")
	item.Does = Action{Kind: ActionTask, Brief: "fix the migration"}
	made, err := store.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	yes, _ := countingSentinel(VerdictYes)

	// PASS ONE: the task runs, the write that would record it fails.
	runner.armed = true
	mustTick(t, probeTicker(store, runner, yes, now))
	runner.armed = false
	if err := os.Chmod(store.Root(), 0o700); err != nil {
		t.Fatal(err)
	}
	if len(runner.runDirs) != 1 {
		t.Fatalf("the task ran %d times on the first pass, wanted one", len(runner.runDirs))
	}
	lost, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lost.TaskInflight == nil || lost.Status != StatusActive || lost.SpentUSD != 0 {
		t.Fatalf("the lost post-run write left inflight=%v status=%q spent=%v, wanted the marker up on an active, unbooked item",
			lost.TaskInflight != nil, lost.Status, lost.SpentUSD)
	}
	// THE RUN IDENTITY IS NATIVE DURABLE EVIDENCE of the completed firing.
	entry, recorded, err := store.recordedFiring(made.ID, "", lost.TaskInflight.RunDir, lost.TaskInflight.Started)
	if err != nil || !recorded {
		t.Fatalf("the completed task firing is not on the ledger: recorded=%v err=%v", recorded, err)
	}
	if entry.Outcome != "landed" || entry.USD != 0.30 {
		t.Fatalf("the run record is %+v, wanted the landed outcome and its cost", entry)
	}

	// RESTART, ON A WRITABLE ROOT: the completion is settled from the record.
	second := mustTick(t, probeTicker(store, runner, yes, now.Add(6*time.Minute)))
	if second.Fired != 1 {
		t.Fatalf("the restart did not settle the completed task: %+v", second)
	}
	if len(runner.runDirs) != 1 {
		t.Fatalf("the task was replayed: %d runs", len(runner.runDirs))
	}
	settled, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.TaskInflight != nil || settled.Status != StatusRetired || settled.RetiredWhy != "fired" {
		t.Fatalf("a settled one-shot task is inflight=%v status=%q because=%q, wanted cleared and retired because fired",
			settled.TaskInflight != nil, settled.Status, settled.RetiredWhy)
	}
	if strings.Contains(settled.NeedsPerson, "waiting") {
		t.Fatalf("a completed task reported a false waiting line: %q", settled.NeedsPerson)
	}
	if settled.Runs != 1 || settled.SpentUSD != 0.30 || settled.LastOutcome != "landed" {
		t.Fatalf("the settled firing booked runs=%d spent=%v outcome=%q, wanted 1, 0.30 and landed",
			settled.Runs, settled.SpentUSD, settled.LastOutcome)
	}
	if !settled.LastFired.Equal(now) {
		t.Fatalf("the settlement invented a moment: last fired %s, wanted the firing's own %s", settled.LastFired, now)
	}
	if len(settled.Previous) != 1 || !strings.Contains(settled.Previous[0], "landed") {
		t.Fatalf("the settled firing did not restore its history: %v", settled.Previous)
	}
	// EXACTLY ONE LEDGER LINE FOR THE ONE FIRING: a settlement adds none.
	today, err := store.Today(made.ID, now.Add(6*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if today.Fired != 1 || today.USD != 0.30 {
		t.Fatalf("the ledger says %+v, wanted one firing and one charge", today)
	}

	// A LATER PASS LOOKS AT NOTHING: a retired one-shot is done.
	third := mustTick(t, probeTicker(store, runner, yes, now.Add(12*time.Minute)))
	if third.Fired != 0 || third.Examined != 0 {
		t.Fatalf("a settled one-shot was looked at again: %+v", third)
	}
	if len(runner.runDirs) != 1 {
		t.Fatalf("the task ran again after its retirement: %d runs", len(runner.runDirs))
	}
}

// A RECURRING TASK WHOSE POST-RUN WRITE WAS LOST IS SETTLED ONCE AND THEN STAYS
// QUIET. The item keeps running, the firing's cost and outcome are restored, the
// marker comes down, and the unchanged reading does not run the task a second
// time.
func TestARecurringTaskWhosePostRunWriteWasLostIsNotRunAgain(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	if err := store.Append(Entry{At: now, ItemID: "seed", Kind: entryCheck}); err != nil {
		t.Fatal(err)
	}
	runner := &rootFlipTaskRunner{
		fakeRunner: &fakeRunner{evidence: `{"ready": true}`, outcome: Outcome{Kind: "landed", USD: 0.10}},
		root:       store.Root(),
	}
	item := newProbe("when ready is true, fix the warnings")
	item.Does = Action{Kind: ActionTask, Brief: "fix the warnings"}
	made, err := store.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	yes, _ := countingSentinel(VerdictYes)

	runner.armed = true
	mustTick(t, probeTicker(store, runner, yes, now))
	runner.armed = false
	if err := os.Chmod(store.Root(), 0o700); err != nil {
		t.Fatal(err)
	}
	lost, _ := store.Get(made.ID)
	if lost.TaskInflight == nil || lost.Status != StatusActive {
		t.Fatalf("the lost write left the recurring task inflight=%v status=%q", lost.TaskInflight != nil, lost.Status)
	}

	second := mustTick(t, probeTicker(store, runner, yes, now.Add(6*time.Minute)))
	if second.Fired != 1 {
		t.Fatalf("the replay did not settle the completed recurring task: %+v", second)
	}
	settled, _ := store.Get(made.ID)
	if settled.TaskInflight != nil || settled.Status != StatusActive {
		t.Fatalf("the recurring task is inflight=%v status=%q, wanted the marker down and still active",
			settled.TaskInflight != nil, settled.Status)
	}
	if settled.Runs != 1 || settled.SpentUSD != 0.10 || settled.LastOutcome != "landed" {
		t.Fatalf("the settled firing booked runs=%d spent=%v outcome=%q, wanted 1, 0.10 and landed",
			settled.Runs, settled.SpentUSD, settled.LastOutcome)
	}
	if strings.Contains(settled.NeedsPerson, "waiting") {
		t.Fatalf("a completed recurring task reported a false waiting line: %q", settled.NeedsPerson)
	}

	// THE UNCHANGED READING DOES NOT RUN IT AGAIN.
	third := mustTick(t, probeTicker(store, runner, yes, now.Add(12*time.Minute)))
	if third.Fired != 0 || third.Examined != 1 {
		t.Fatalf("the settled recurring task was looked at again or fired: %+v", third)
	}
	if len(runner.runDirs) != 1 {
		t.Fatalf("the recurring task ran again: %d runs", len(runner.runDirs))
	}
	today, err := store.Today(made.ID, now.Add(12*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if today.Fired != 1 || today.USD != 0.10 {
		t.Fatalf("the ledger says %+v, wanted one firing and one charge", today)
	}
}

// A TASK WITH NO RECORD FOR ITS OWN RUN STAYS FOR THE PERSON AND IS NEVER
// REPLAYED. A ledger line for a DIFFERENT run is not evidence of this attempt's
// outcome, so the marker stays up and the person is told.
func TestATaskWithNoRecordForItsRunStillWaitsForThePerson(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: `{"ready": true}`, runErr: errors.New("the edits are ambiguous")}
	item := oneShotProbe("when ready becomes true, fix the migration")
	item.Does = Action{Kind: ActionTask, Brief: "fix the migration"}
	made, err := store.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	yes, _ := countingSentinel(VerdictYes)

	mustTick(t, probeTicker(store, runner, yes, now))
	stuck, _ := store.Get(made.ID)
	if stuck.TaskInflight == nil || !strings.Contains(stuck.NeedsPerson, "waiting") {
		t.Fatalf("a failed task was left inflight=%v needs=%q, wanted the marker up and the person told",
			stuck.TaskInflight != nil, stuck.NeedsPerson)
	}
	// A RECORD OF SOME OTHER RUN IS NOT A RECORD OF THIS ONE.
	if err := store.Append(Entry{At: now, ItemID: made.ID, Kind: string(ActionTask), Run: stuck.TaskInflight.RunDir + "-other", USD: 0.9, Outcome: "landed"}); err != nil {
		t.Fatal(err)
	}

	second := mustTick(t, probeTicker(store, runner, yes, now.Add(6*time.Minute)))
	if second.Fired != 0 {
		t.Fatalf("a task with no record for its own run was settled: %+v", second)
	}
	if len(runner.runDirs) != 1 {
		t.Fatalf("the ambiguous task was replayed: %d runs", len(runner.runDirs))
	}
	after, _ := store.Get(made.ID)
	if after.TaskInflight == nil || !strings.Contains(after.NeedsPerson, "waiting") {
		t.Fatalf("an ambiguous task lost its marker or its line: inflight=%v needs=%q",
			after.TaskInflight != nil, after.NeedsPerson)
	}
}

// A SAY SETTLED FROM THE LEDGER RESTORES ITS COST, OUTCOME AND HISTORY FROM THE
// RECORD, AT THE RECORD'S OWN MOMENT, AND CHARGES THE DAY ONCE.
func TestASaySettledFromTheLedgerRestoresItsCostOutcomeAndHistory(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	if err := store.Append(Entry{At: now, ItemID: "seed", Kind: entryCheck}); err != nil {
		t.Fatal(err)
	}
	runner := &billOnDeliverRunner{
		fakeRunner: fakeRunner{evidence: `{"ready": true}`},
		root:       store.Root(),
		billed:     0.42,
	}
	made, err := store.Create(oneShotProbe("notify me once when ready becomes true"))
	if err != nil {
		t.Fatal(err)
	}
	yes, _ := countingSentinel(VerdictYes)

	// PASS ONE: the line is delivered and billed; the retire-save fails.
	runner.armed = true
	if pass := mustTick(t, probeTicker(store, runner, yes, now)); pass.Fired != 1 {
		t.Fatalf("the one-shot first true did not deliver once: %+v", pass)
	}
	runner.armed = false
	if err := os.Chmod(store.Root(), 0o700); err != nil {
		t.Fatal(err)
	}
	lost, _ := store.Get(made.ID)
	if lost.Status != StatusActive || len(lost.Pending) != 1 || lost.SpentUSD != 0 || lost.LastOutcome != "" {
		t.Fatalf("the lost retire-save left status=%q pending=%d spent=%v outcome=%q, wanted active with one intent and no card figure",
			lost.Status, len(lost.Pending), lost.SpentUSD, lost.LastOutcome)
	}
	// The firing IS on the ledger, with what it came to.
	entry, recorded, err := store.recordedFiring(made.ID, lost.Pending[0].ID, "", lost.Pending[0].At)
	if err != nil || !recorded {
		t.Fatalf("the delivered firing is not on the ledger: recorded=%v err=%v", recorded, err)
	}
	if entry.Outcome != "said" || entry.USD != 0.42 || entry.Line == "" {
		t.Fatalf("the firing record is %+v, wanted the said outcome, its cost and its line", entry)
	}

	// RESTART: the settlement restores the firing from that record.
	second := mustTick(t, probeTicker(store, runner, yes, now.Add(6*time.Minute)))
	if second.Fired != 1 || second.Said != 1 {
		t.Fatalf("the restart did not settle the fulfilled one-shot: %+v", second)
	}
	if runner.calls != 1 {
		t.Fatalf("the restart delivered a line that had already reached the person: calls=%d", runner.calls)
	}
	settled, _ := store.Get(made.ID)
	if settled.Status != StatusRetired || len(settled.Pending) != 0 {
		t.Fatalf("a settled one-shot is %q with %d pending, wanted retired and clean", settled.Status, len(settled.Pending))
	}
	if settled.SpentUSD != 0.42 || settled.LastOutcome != "said" {
		t.Fatalf("the settled say booked spent=%v outcome=%q, wanted the record's 0.42 and said", settled.SpentUSD, settled.LastOutcome)
	}
	if !settled.LastFired.Equal(now) {
		t.Fatalf("the settlement invented a moment: last fired %s, wanted the firing's own %s", settled.LastFired, now)
	}
	if len(settled.Previous) != 1 || !strings.Contains(settled.Previous[0], "said") {
		t.Fatalf("the settled say did not restore its history: %v", settled.Previous)
	}
	// NO DUPLICATED LEDGER LINE.
	today, err := store.Today(made.ID, now.Add(6*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if today.Fired != 1 || today.USD != 0.42 {
		t.Fatalf("the ledger says %+v, wanted one firing and one charge", today)
	}
}

// recordedFiring reads a task's own run identity back out of the ledger, and
// nothing else does.
func TestRecordedFiringReadsATaskRunIdentityFromTheLedger(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	if err := store.Append(Entry{At: now, ItemID: "aaaaaaaaaaaaaaaa", Kind: string(ActionTask), Run: "runs/0001", USD: 0.2, Outcome: "landed", Line: "the files you are watching changed"}); err != nil {
		t.Fatal(err)
	}

	entry, found, err := store.recordedFiring("aaaaaaaaaaaaaaaa", "", "runs/0001", now)
	if err != nil || !found {
		t.Fatalf("a recorded task firing was not found: found=%v err=%v", found, err)
	}
	if entry.Outcome != "landed" || entry.USD != 0.2 || entry.Line != "the files you are watching changed" {
		t.Fatalf("the record read back as %+v", entry)
	}
	for _, probe := range []struct{ item, pending, run string }{
		{"aaaaaaaaaaaaaaaa", "", "runs/0002"}, // a different run
		{"bbbbbbbbbbbbbbbb", "", "runs/0001"}, // a different item
		{"aaaaaaaaaaaaaaaa", "p-other", ""},   // a different delivery identity
		{"", "", "runs/0001"},                 // no item
		{"aaaaaaaaaaaaaaaa", "", ""},          // no identity at all
	} {
		if _, got, err := store.recordedFiring(probe.item, probe.pending, probe.run, now); err != nil || got {
			t.Fatalf("recordedFiring(%q,%q,%q) = %v,%v, wanted false,nil", probe.item, probe.pending, probe.run, got, err)
		}
	}
}

// A RECOVERED TASK THAT STOPPED ON A QUESTION IS NOT A CLEAN SUCCESS. A task
// whose run reaches needs-you with no error is a REACHED outcome, and its
// post-run write can be lost just like any other; the recovery must restore the
// question, put the clean-run streak back to nothing, restore the run folder and
// never report the false "waiting for you" line or run the task again.
func TestARecoveredTaskThatStoppedOnAQuestionKeepsItsQuestion(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	if err := store.Append(Entry{At: now, ItemID: "seed", Kind: entryCheck}); err != nil {
		t.Fatal(err)
	}
	const question = "may I delete the old index?"
	runner := &rootFlipTaskRunner{
		fakeRunner: &fakeRunner{
			evidence: `{"ready": true}`,
			outcome:  Outcome{Kind: OutcomeNeedsYou, NeedsPerson: question, USD: 0.05},
		},
		root: store.Root(),
	}
	item := newProbe("when ready is true, tidy the index")
	item.Does = Action{Kind: ActionTask, Brief: "tidy the index"}
	item.CleanRuns = 2 // a streak the question must break, not extend
	made, err := store.Create(item)
	if err != nil {
		t.Fatal(err)
	}
	yes, _ := countingSentinel(VerdictYes)

	runner.armed = true
	mustTick(t, probeTicker(store, runner, yes, now))
	runner.armed = false
	if err := os.Chmod(store.Root(), 0o700); err != nil {
		t.Fatal(err)
	}
	lost, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if lost.TaskInflight == nil {
		t.Fatal("the lost post-run write did not leave the in-flight marker up")
	}
	runDir := lost.TaskInflight.RunDir

	second := mustTick(t, probeTicker(store, runner, yes, now.Add(6*time.Minute)))
	if second.Fired != 1 {
		t.Fatalf("the restart did not settle the completed task: %+v", second)
	}
	if len(runner.runDirs) != 1 {
		t.Fatalf("the task was replayed: %d runs", len(runner.runDirs))
	}
	settled, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.TaskInflight != nil {
		t.Fatalf("the recovered task kept its in-flight marker: %+v", settled.TaskInflight)
	}
	if settled.NeedsPerson != question {
		t.Fatalf("the recovered firing left needs=%q, wanted the question %q it actually asked", settled.NeedsPerson, question)
	}
	if strings.Contains(settled.NeedsPerson, "waiting") {
		t.Fatalf("the recovered task reported the false waiting line: %q", settled.NeedsPerson)
	}
	if settled.CleanRuns != 0 {
		t.Fatalf("a firing that stopped on a question left clean runs at %d, wanted nothing", settled.CleanRuns)
	}
	if settled.LastOutcome != OutcomeNeedsYou || settled.Runs != 1 || settled.SpentUSD != 0.05 {
		t.Fatalf("the recovered firing is outcome=%q runs=%d spent=%v, wanted needs-you, 1 and 0.05",
			settled.LastOutcome, settled.Runs, settled.SpentUSD)
	}
	if settled.LastRun != runDir {
		t.Fatalf("the recovered firing lost its run folder: %q, wanted %q", settled.LastRun, runDir)
	}
}
