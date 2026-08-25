package standing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// fakeRunner is the session lane, stood in for: it remembers what it was asked
// to do and answers whatever the test told it to.
type fakeRunner struct {
	evidence  string
	probeErr  error
	said      []string
	runDirs   []string
	outcome   Outcome
	sayErr    error
	runErr    error
	probeSeen int
}

func (f *fakeRunner) Probe(_ context.Context, _ Item) (string, error) {
	f.probeSeen++
	return f.evidence, f.probeErr
}

func (f *fakeRunner) Say(_ context.Context, _ Item, text string) (Outcome, error) {
	f.said = append(f.said, text)
	if f.sayErr != nil {
		return Outcome{}, f.sayErr
	}
	outcome := f.outcome
	if outcome.Kind == "" {
		outcome.Kind = "said"
	}
	return outcome, nil
}

func (f *fakeRunner) Run(_ context.Context, _ Item, runDir, _ string) (Outcome, error) {
	f.runDirs = append(f.runDirs, runDir)
	if f.runErr != nil {
		return Outcome{}, f.runErr
	}
	outcome := f.outcome
	if outcome.Kind == "" {
		outcome.Kind = "landed"
	}
	return outcome, nil
}

// answers is a sentinel that says what a test told it to, in order.
func answers(replies ...bool) (Sentinel, *int) {
	asked := 0
	return func(_ context.Context, judgment Judgment) (bool, string, float64, error) {
		reply := false
		if asked < len(replies) {
			reply = replies[asked]
		}
		asked++
		if reply {
			return true, "the world says yes", 0, nil
		}
		return false, "nothing worth telling you", 0, nil
	}, &asked
}

func newTicker(store *Store, runner Runner, now time.Time) *Ticker {
	return &Ticker{Store: store, Runner: runner, Now: held(now)}
}

func mustTick(t *testing.T, ticker *Ticker) Pass {
	t.Helper()
	pass, err := ticker.Tick(context.Background())
	if err != nil {
		t.Fatalf("tick: %v", err)
	}
	return pass
}

func TestTickFiresAMomentOnceAndRetiresIt(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{}
	made, err := store.Create(reminder("remind me at 6 to leave", now.Add(time.Hour)))
	if err != nil {
		t.Fatal(err)
	}

	// Before the moment nothing happens and NOTHING IS WRITTEN: a reminder
	// waiting for six o'clock has not checked anything.
	early := mustTick(t, newTicker(store, runner, now))
	if early.Examined != 1 || early.Fired != 0 || early.Checked != 0 {
		t.Fatalf("the early pass is %+v", early)
	}
	if len(runner.said) != 0 {
		t.Fatalf("it spoke before its time: %v", runner.said)
	}

	after := now.Add(time.Hour)
	store.clock = held(after)
	pass := mustTick(t, newTicker(store, runner, after))
	if pass.Fired != 1 || pass.Said != 1 {
		t.Fatalf("the pass is %+v, wanted one firing", pass)
	}
	if len(runner.said) != 1 || runner.said[0] != "leave now" {
		t.Fatalf("what it said was %v", runner.said)
	}

	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Status != StatusRetired || back.RetiredWhy != "fired" {
		t.Fatalf("a reminder that fired is %q because %q, wanted retired because fired", back.Status, back.RetiredWhy)
	}
	if back.Runs != 1 || !back.LastFired.Equal(after) || back.LastOutcome != "said" {
		t.Fatalf("the item does not remember firing: %+v", back)
	}
	if len(back.Previous) != 1 || !strings.Contains(back.Previous[0], "said") {
		t.Fatalf("the previous judgments are %v", back.Previous)
	}

	spend, err := store.Today(made.ID, after)
	if err != nil {
		t.Fatal(err)
	}
	if spend.Fired != 1 {
		t.Fatalf("the ledger counted %d firings", spend.Fired)
	}
	if _, err := os.Stat(store.LogPath(made.ID)); err != nil {
		t.Fatalf("a firing left no line in the item's log: %v", err)
	}

	// A retired item is not walked again.
	later := after.Add(time.Hour)
	store.clock = held(later)
	third := mustTick(t, newTicker(store, runner, later))
	if third.Examined != 0 || len(runner.said) != 1 {
		t.Fatalf("a retired reminder fired again: %+v %v", third, runner.said)
	}
}

func TestTickAdvancesARhythm(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{}

	rhythm := reminder("every 30 minutes, tell me the time", time.Time{})
	rhythm.When = When{Kind: WhenEvery, Words: "every 30 minutes", Every: "30m"}
	made, err := store.Create(rhythm)
	if err != nil {
		t.Fatal(err)
	}
	if !made.NextDue.Equal(now.Add(30 * time.Minute)) {
		t.Fatalf("the first moment is %s", made.NextDue)
	}

	// Five minutes later it is not due, and it stays silent.
	soon := now.Add(5 * time.Minute)
	store.clock = held(soon)
	if pass := mustTick(t, newTicker(store, runner, soon)); pass.Fired != 0 {
		t.Fatalf("a rhythm fired before its time: %+v", pass)
	}

	due := now.Add(31 * time.Minute)
	store.clock = held(due)
	pass := mustTick(t, newTicker(store, runner, due))
	if pass.Fired != 1 || pass.Said != 1 {
		t.Fatalf("the pass is %+v", pass)
	}
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Status != StatusActive {
		t.Fatalf("a rhythm retired after firing: %q", back.Status)
	}
	if !back.NextDue.Equal(due.Add(30 * time.Minute)) {
		t.Fatalf("the next moment is %s, wanted %s", back.NextDue, due.Add(30*time.Minute))
	}
	if back.Runs != 1 {
		t.Fatalf("it has run %d times", back.Runs)
	}
}

func TestTickIsSilentOnAFileWatchesFirstReading(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{}
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "schema.sql"), []byte("create table one;"), 0o600); err != nil {
		t.Fatal(err)
	}

	watch := reminder("re-run the suite when schema.sql changes", time.Time{})
	watch.Workspace = workspace
	watch.When = When{Kind: WhenFile, Words: "when schema.sql changes", Glob: "*.sql"}
	made, err := store.Create(watch)
	if err != nil {
		t.Fatal(err)
	}

	// THE FIRST READING IS THE BASELINE AND IS SILENT.
	first := mustTick(t, newTicker(store, runner, now))
	if first.Fired != 0 || first.Checked != 1 {
		t.Fatalf("the first reading is %+v, wanted a quiet check", first)
	}
	if len(runner.said) != 0 {
		t.Fatalf("the baseline reading spoke: %v", runner.said)
	}
	base, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if base.Fingerprint == "" {
		t.Fatal("the baseline reading was not kept")
	}
	if base.LastCheckLine != "nothing has changed yet" {
		t.Fatalf("the baseline says %q", base.LastCheckLine)
	}
	if _, err := os.Stat(store.LogPath(made.ID)); !os.IsNotExist(err) {
		t.Fatalf("a quiet check wrote a log line: %v", err)
	}

	// Unchanged is still quiet.
	quietMoment := now.Add(5 * time.Minute)
	store.clock = held(quietMoment)
	if pass := mustTick(t, newTicker(store, runner, quietMoment)); pass.Fired != 0 {
		t.Fatalf("an unchanged file fired: %+v", pass)
	}

	changed := now.Add(10 * time.Minute)
	if err := os.WriteFile(filepath.Join(workspace, "schema.sql"), []byte("create table one; create table two;"), 0o600); err != nil {
		t.Fatal(err)
	}
	store.clock = held(changed)
	pass := mustTick(t, newTicker(store, runner, changed))
	if pass.Fired != 1 {
		t.Fatalf("a changed file did not fire: %+v", pass)
	}
	after, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Fingerprint == base.Fingerprint {
		t.Fatal("the reading was not kept after the firing")
	}
	if after.LastCheckLine != "the files you are watching changed" {
		t.Fatalf("it says %q", after.LastCheckLine)
	}

	// And having fired, it settles: the same file does not fire twice.
	settled := now.Add(15 * time.Minute)
	store.clock = held(settled)
	if pass := mustTick(t, newTicker(store, runner, settled)); pass.Fired != 0 {
		t.Fatalf("the same change fired twice: %+v", pass)
	}
}

func TestTickJudgesAProbeAndOnlySpendsOnAYes(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: "conclusion=success"}
	sentinel, asked := answers(false, true)

	watch := reminder("tell me when CI on main goes red", time.Time{})
	watch.When = When{Kind: WhenProbe, Words: "when CI goes red", Probe: Probe{Command: "gh run list"}, ProbeEvery: 10 * time.Minute}
	watch.Does = Action{Kind: ActionSay, Say: "CI is red: {{evidence}}"}
	made, err := store.Create(watch)
	if err != nil {
		t.Fatal(err)
	}

	ticker := newTicker(store, runner, now)
	ticker.Sentinel = sentinel
	pass := mustTick(t, ticker)
	if pass.Checked != 1 || pass.Fired != 0 {
		t.Fatalf("a no is %+v, wanted one quiet check", pass)
	}
	if *asked != 1 || runner.probeSeen != 1 {
		t.Fatalf("the probe ran %d times and the sentinel was asked %d times", runner.probeSeen, *asked)
	}
	quietItem, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !quietItem.LastChecked.Equal(now) {
		t.Fatalf("the check was not remembered: %s", quietItem.LastChecked)
	}
	if quietItem.LastCheckLine != "nothing worth telling you" {
		t.Fatalf("the check line is %q", quietItem.LastCheckLine)
	}
	if !quietItem.NextDue.Equal(now.Add(10 * time.Minute)) {
		t.Fatalf("the next look is %s", quietItem.NextDue)
	}
	if quietItem.Runs != 0 || len(quietItem.Previous) != 0 {
		t.Fatalf("a no left a mark on the item: %+v", quietItem)
	}
	if spend, _ := store.Today("", now); spend.Fired != 0 || spend.USD != 0 {
		t.Fatalf("a free no wrote a ledger line: %+v", spend)
	}
	if _, err := os.Stat(store.LogPath(made.ID)); !os.IsNotExist(err) {
		t.Fatalf("a no wrote a log line: %v", err)
	}

	// It is not looked at again until its own cadence says so.
	soon := now.Add(2 * time.Minute)
	store.clock = held(soon)
	soonTicker := newTicker(store, runner, soon)
	soonTicker.Sentinel = sentinel
	if pass := mustTick(t, soonTicker); pass.Checked != 0 {
		t.Fatalf("the probe was run early: %+v", pass)
	}
	if runner.probeSeen != 1 {
		t.Fatalf("the probe ran %d times", runner.probeSeen)
	}

	yes := now.Add(10 * time.Minute)
	runner.evidence = "conclusion=failure"
	runner.outcome = Outcome{Kind: "said", Text: "CI is red", USD: 0.02}
	store.clock = held(yes)
	yesTicker := newTicker(store, runner, yes)
	yesTicker.Sentinel = sentinel
	pass = mustTick(t, yesTicker)
	if pass.Fired != 1 || pass.Said != 1 {
		t.Fatalf("a yes is %+v", pass)
	}
	if len(runner.said) != 1 || runner.said[0] != "CI is red: conclusion=failure" {
		t.Fatalf("the evidence did not reach what it said: %v", runner.said)
	}
	fired, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fired.Runs != 1 || fired.SpentUSD < 0.019 || fired.SpentUSD > 0.021 {
		t.Fatalf("the item does not remember the firing: %+v", fired)
	}
	if len(fired.Previous) != 1 || !strings.HasPrefix(fired.Previous[0], "the world says yes") {
		t.Fatalf("the previous judgments are %v", fired.Previous)
	}
	if !fired.NextDue.Equal(yes.Add(10 * time.Minute)) {
		t.Fatalf("the next look is %s", fired.NextDue)
	}
	spend, err := store.Today(made.ID, yes)
	if err != nil {
		t.Fatal(err)
	}
	if spend.Fired != 1 || spend.USD < 0.019 || spend.USD > 0.021 {
		t.Fatalf("the ledger says %+v", spend)
	}
	raw, err := os.ReadFile(store.LogPath(made.ID))
	if err != nil {
		t.Fatalf("a firing left no log line: %v", err)
	}
	if !strings.Contains(string(raw), "the world says yes") {
		t.Fatalf("the log line is %q", string(raw))
	}
}

func TestTickAsksTheSentinelWhenAnItemCarriesAHint(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{}
	sentinel, asked := answers(false, true)

	routine := reminder("every weekday at 8, tell me what needs a reply", time.Time{})
	routine.When = When{Kind: WhenEvery, Every: "5m", Hint: "yes when there is anything worth saying"}
	made, err := store.Create(routine)
	if err != nil {
		t.Fatal(err)
	}

	due := now.Add(6 * time.Minute)
	store.clock = held(due)
	ticker := newTicker(store, runner, due)
	ticker.Sentinel = sentinel
	pass := mustTick(t, ticker)
	if pass.Fired != 0 || pass.Checked != 1 {
		t.Fatalf("a hint that said no still fired: %+v", pass)
	}
	if runner.probeSeen != 0 {
		t.Fatalf("a hint on a rhythm ran a probe %d times", runner.probeSeen)
	}
	if *asked != 1 {
		t.Fatalf("the sentinel was asked %d times", *asked)
	}
	quietItem, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if quietItem.LastCheckLine != "nothing worth telling you" {
		t.Fatalf("the check line is %q", quietItem.LastCheckLine)
	}
	if !quietItem.NextDue.After(due) {
		t.Fatalf("a hint that said no did not move the rhythm on: %s", quietItem.NextDue)
	}

	later := quietItem.NextDue.Add(time.Minute)
	store.clock = held(later)
	ticker = newTicker(store, runner, later)
	ticker.Sentinel = sentinel
	if pass := mustTick(t, ticker); pass.Fired != 1 {
		t.Fatalf("a hint that said yes did not fire: %+v", pass)
	}
}

func TestTickRunsATaskInItsOwnNumberedFolder(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{outcome: Outcome{Kind: "needs-you", Text: "the migration stopped", NeedsPerson: "it wants to drop a column", USD: 0.5}}

	nightly := reminder("every night, try the migration", time.Time{})
	nightly.When = When{Kind: WhenEvery, Every: "1h"}
	nightly.Does = Action{Kind: ActionTask, Brief: "try the migration", Acceptance: "tests pass", MaxSteps: 40}
	made, err := store.Create(nightly)
	if err != nil {
		t.Fatal(err)
	}

	due := now.Add(61 * time.Minute)
	store.clock = held(due)
	pass := mustTick(t, newTicker(store, runner, due))
	if pass.Fired != 1 || pass.NeedsYou != 1 || pass.Said != 0 {
		t.Fatalf("the pass is %+v", pass)
	}
	if len(runner.runDirs) != 1 {
		t.Fatalf("the run folders are %v", runner.runDirs)
	}
	if want := filepath.Join(store.RunsDir(made.ID), "0001"); runner.runDirs[0] != want {
		t.Fatalf("the run folder is %q, wanted %q", runner.runDirs[0], want)
	}
	if info, err := os.Stat(runner.runDirs[0]); err != nil || !info.IsDir() {
		t.Fatalf("the run folder was not made: %v", err)
	}
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.NeedsPerson != "it wants to drop a column" {
		t.Fatalf("the item does not say what it needs: %q", back.NeedsPerson)
	}
	if back.Glyph(false) != "▲" {
		t.Fatalf("the glyph is %q, wanted the one that means it needs you", back.Glyph(false))
	}
	if back.LastRun != runner.runDirs[0] {
		t.Fatalf("the item does not name its run: %q", back.LastRun)
	}

	// A second firing takes the next number.
	again := due.Add(61 * time.Minute)
	store.clock = held(again)
	if pass := mustTick(t, newTicker(store, runner, again)); pass.Fired != 1 {
		t.Fatalf("the second firing is %+v", pass)
	}
	if want := filepath.Join(store.RunsDir(made.ID), "0002"); runner.runDirs[1] != want {
		t.Fatalf("the second run folder is %q, wanted %q", runner.runDirs[1], want)
	}
}

func TestTickStopsAtTheDailyCount(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{}

	rhythm := reminder("every minute, say hello", time.Time{})
	rhythm.When = When{Kind: WhenEvery, Every: "1m"}
	rhythm.Rails = Rails{PerRunUSD: 0.01, MaxPerDay: 2}
	made, err := store.Create(rhythm)
	if err != nil {
		t.Fatal(err)
	}

	for minute := 1; minute <= 5; minute++ {
		moment := now.Add(time.Duration(minute) * time.Minute)
		store.clock = held(moment)
		if _, err := newTicker(store, runner, moment).Tick(context.Background()); err != nil {
			t.Fatalf("tick: %v", err)
		}
	}
	if len(runner.said) != 2 {
		t.Fatalf("it spoke %d times, wanted the two the rails allow", len(runner.said))
	}
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.LastCheckLine != "it has already run today as often as you allowed" {
		t.Fatalf("the check line is %q", back.LastCheckLine)
	}
	if back.Status != StatusActive {
		t.Fatalf("reaching the daily count retired it: %q", back.Status)
	}

	// Tomorrow it runs again: the ledger is the day's, not the item's life.
	tomorrow := now.Add(25 * time.Hour)
	store.clock = held(tomorrow)
	if pass := mustTick(t, newTicker(store, runner, tomorrow)); pass.Fired != 1 {
		t.Fatalf("tomorrow is %+v", pass)
	}
}

func TestTickStopsAtTheDaysSpending(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{}

	rhythm := reminder("every minute, say hello", time.Time{})
	rhythm.When = When{Kind: WhenEvery, Every: "1m"}
	made, err := store.Create(rhythm)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Append(Entry{At: now, ItemID: "somebody-else", Kind: string(ActionTask), USD: 4.90}); err != nil {
		t.Fatal(err)
	}

	due := now.Add(2 * time.Minute)
	store.clock = held(due)
	ticker := newTicker(store, runner, due)
	ticker.DailyRailUSD = 5
	pass := mustTick(t, ticker)
	if pass.Fired != 1 || pass.Skipped != 0 {
		t.Fatalf("under the rail the pass is %+v, wanted it to run", pass)
	}

	if err := store.Append(Entry{At: now, ItemID: "somebody-else", Kind: string(ActionTask), USD: 0.20}); err != nil {
		t.Fatal(err)
	}
	over := now.Add(3 * time.Minute)
	store.clock = held(over)
	ticker = newTicker(store, runner, over)
	ticker.DailyRailUSD = 5
	pass = mustTick(t, ticker)
	if pass.Fired != 0 || pass.Skipped != 1 {
		t.Fatalf("over the rail the pass is %+v", pass)
	}
	if len(pass.Notes) == 0 || !strings.Contains(pass.Notes[0], "spending limit") {
		t.Fatalf("the pass does not say why it stopped: %v", pass.Notes)
	}
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.LastCheckLine != "today's spending limit is reached" {
		t.Fatalf("the check line is %q", back.LastCheckLine)
	}
	if len(runner.said) != 1 {
		t.Fatalf("it spoke %d times, wanted only the one under the rail", len(runner.said))
	}
}

func TestTickRetiresWhatRanOutOfTime(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{}

	// A rhythm with an expiry the person set.
	rhythm := reminder("every minute until Friday", time.Time{})
	rhythm.When = When{Kind: WhenEvery, Every: "1m"}
	rhythm.Rails = Rails{PerRunUSD: 0.01, MaxPerDay: 10, Expires: now.Add(time.Hour)}
	timed, err := store.Create(rhythm)
	if err != nil {
		t.Fatal(err)
	}
	// A reminder nothing woke up to deliver. It expires a day after its moment
	// whatever the rails say.
	missed, err := store.Create(reminder("remind me at 6", now.Add(-25*time.Hour)))
	if err != nil {
		t.Fatal(err)
	}

	pass := mustTick(t, newTicker(store, runner, now))
	if pass.Fired != 0 {
		t.Fatalf("something fired: %+v", pass)
	}
	stale, err := store.Get(missed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Status != StatusRetired || stale.RetiredWhy != "expired" {
		t.Fatalf("the missed reminder is %q because %q", stale.Status, stale.RetiredWhy)
	}
	if len(runner.said) != 0 {
		t.Fatalf("a reminder a day late was still delivered: %v", runner.said)
	}

	after := now.Add(2 * time.Hour)
	store.clock = held(after)
	if _, err := newTicker(store, runner, after).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	over, err := store.Get(timed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if over.Status != StatusRetired || over.RetiredWhy != "expired" {
		t.Fatalf("the expired rhythm is %q because %q", over.Status, over.RetiredWhy)
	}
}

func TestTickRefusesWhenAnotherAforgeHoldsTheLock(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)

	other, err := os.OpenFile(store.LockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if err := unix.Flock(int(other.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatalf("cannot hold the lock: %v", err)
	}

	if _, err := newTicker(store, &fakeRunner{}, now).Tick(context.Background()); !errors.Is(err, ErrHeld) {
		t.Fatalf("a held lock answered %v, wanted ErrHeld", err)
	}
	if _, err := os.Stat(store.WakeLogPath()); !os.IsNotExist(err) {
		t.Fatalf("a refused pass wrote a wake line: %v", err)
	}

	// Released, the next pass runs.
	if err := unix.Flock(int(other.Fd()), unix.LOCK_UN); err != nil {
		t.Fatal(err)
	}
	if _, err := newTicker(store, &fakeRunner{}, now).Tick(context.Background()); err != nil {
		t.Fatalf("the lock was not released: %v", err)
	}
}

func TestTickWritesOneWakeLineAPass(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{}
	if _, err := store.Create(reminder("remind me now", now)); err != nil {
		t.Fatal(err)
	}

	mustTick(t, newTicker(store, runner, now))
	next := now.Add(5 * time.Minute)
	store.clock = held(next)
	mustTick(t, newTicker(store, runner, next))

	raw, err := os.ReadFile(store.WakeLogPath())
	if err != nil {
		t.Fatalf("read the wake log: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(raw), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("the wake log has %d lines: %q", len(lines), string(raw))
	}
	if !strings.HasPrefix(lines[0], now.Format(time.RFC3339)) {
		t.Fatalf("a wake line does not lead with when: %q", lines[0])
	}
	if !strings.Contains(lines[0], "examined=1") || !strings.Contains(lines[0], "fired=1") {
		t.Fatalf("a wake line does not carry the counts: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], next.Format(time.RFC3339)) {
		t.Fatalf("the second wake line is %q", lines[1])
	}
}

func TestTickCountsOneItemsFailureAndWalksOn(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{sayErr: errors.New("the conversation is gone")}

	broken, err := store.Create(reminder("the one that fails", now))
	if err != nil {
		t.Fatal(err)
	}
	store.clock = held(now.Add(time.Second))
	if _, err := store.Create(reminder("the one that works", now)); err != nil {
		t.Fatal(err)
	}
	store.clock = held(now)

	ticker := newTicker(store, runner, now)
	pass, err := ticker.Tick(context.Background())
	if err != nil {
		t.Fatalf("one item's failure ended the pass: %v", err)
	}
	if pass.Examined != 2 || pass.Errors != 2 {
		t.Fatalf("the pass is %+v, wanted both items tried and both counted", pass)
	}
	hurt, err := store.Get(broken.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hurt.LastCheckLine, "could not check:") {
		t.Fatalf("the item does not say what went wrong: %q", hurt.LastCheckLine)
	}
	if hurt.Status != StatusActive {
		t.Fatalf("a failure retired the item: %q", hurt.Status)
	}
	if _, err := os.Stat(store.WakeLogPath()); err != nil {
		t.Fatalf("a pass with failures wrote no wake line: %v", err)
	}
}

func TestTickFiresNothingWithNoRunner(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	if _, err := store.Create(reminder("remind me now", now)); err != nil {
		t.Fatal(err)
	}
	// This is exactly the ticker `aforge tick` builds before the session lane
	// lands: no runner at all, and a sentinel that always says no.
	ticker := &Ticker{Store: store, Now: held(now), Sentinel: func(context.Context, Judgment) (bool, string, float64, error) {
		return false, "no runner in this build", 0, nil
	}}
	pass, err := ticker.Tick(context.Background())
	if err != nil {
		t.Fatalf("a build with no runner cannot even walk: %v", err)
	}
	if pass.Examined != 1 || pass.Fired != 0 || pass.Errors != 1 {
		t.Fatalf("the pass is %+v", pass)
	}
	if _, err := os.Stat(store.WakeLogPath()); err != nil {
		t.Fatalf("the wake log was not written: %v", err)
	}
}

func TestTickStopsWhenTheTimeIsUp(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{}
	for count := 0; count < 3; count++ {
		store.clock = held(now.Add(time.Duration(count) * time.Second))
		if _, err := store.Create(reminder("remind me now", now)); err != nil {
			t.Fatal(err)
		}
	}
	store.clock = held(now)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := newTicker(store, runner, now).Tick(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("a pass with no time answered %v", err)
	}
	if len(runner.said) != 0 {
		t.Fatalf("a cancelled pass still fired: %v", runner.said)
	}
}

func TestIdleFiresOnlyWhenTheMachineIsQuiet(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{}

	idle := reminder("when the machine is quiet, learn this", time.Time{})
	idle.When = When{Kind: WhenIdle, IdleFor: 30 * time.Minute}
	made, err := store.Create(idle)
	if err != nil {
		t.Fatal(err)
	}

	// A build that cannot tell whether the machine is quiet never says it is.
	if pass := mustTick(t, newTicker(store, runner, now)); pass.Fired != 0 || pass.Checked != 0 {
		t.Fatalf("with no way to look the pass is %+v", pass)
	}

	busy := newTicker(store, runner, now)
	busy.Idle = func(time.Duration) bool { return false }
	if pass := mustTick(t, busy); pass.Fired != 0 || pass.Checked != 1 {
		t.Fatalf("a busy machine is %+v", pass)
	}
	if back, _ := store.Get(made.ID); back.LastCheckLine != "the machine has not been quiet long enough" {
		t.Fatalf("the check line is %q", back.LastCheckLine)
	}

	var asked time.Duration
	free := newTicker(store, runner, now)
	free.Idle = func(quiet time.Duration) bool { asked = quiet; return true }
	if pass := mustTick(t, free); pass.Fired != 1 {
		t.Fatalf("a quiet machine is %+v", pass)
	}
	if asked != 30*time.Minute {
		t.Fatalf("it asked about %s of quiet, wanted the item's own", asked)
	}
}

func TestASentinelIsBilledEvenWhenItSaysNo(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: "conclusion=success"}

	watch := reminder("tell me when CI goes red", time.Time{})
	watch.When = When{Kind: WhenProbe, Probe: Probe{Command: "gh run list"}, ProbeEvery: time.Hour}
	made, err := store.Create(watch)
	if err != nil {
		t.Fatal(err)
	}
	ticker := newTicker(store, runner, now)
	ticker.Sentinel = func(context.Context, Judgment) (bool, string, float64, error) {
		return false, "CI is green", 0.001, nil
	}
	if pass := mustTick(t, ticker); pass.Fired != 0 {
		t.Fatalf("the pass is %+v", pass)
	}
	spend, err := store.Today(made.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	if spend.USD < 0.0009 || spend.USD > 0.0011 {
		t.Fatalf("the look cost %v in the ledger, wanted a tenth of a cent", spend.USD)
	}
	if spend.Fired != 0 {
		t.Fatalf("a look was counted as a firing: %+v", spend)
	}
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.SpentUSD < 0.0009 || back.SpentUSD > 0.0011 {
		t.Fatalf("the item does not carry what its looks cost: %v", back.SpentUSD)
	}
}

func TestPreviousJudgmentsAreCappedAtTheConstant(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{}

	rhythm := reminder("every minute, say hello", time.Time{})
	rhythm.When = When{Kind: WhenEvery, Every: "1m"}
	rhythm.Rails = Rails{PerRunUSD: 0.01, MaxPerDay: 100}
	made, err := store.Create(rhythm)
	if err != nil {
		t.Fatal(err)
	}
	for minute := 1; minute <= Previous+3; minute++ {
		moment := now.Add(time.Duration(minute) * time.Minute)
		store.clock = held(moment)
		if _, err := newTicker(store, runner, moment).Tick(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Runs != Previous+3 {
		t.Fatalf("it ran %d times", back.Runs)
	}
	if len(back.Previous) != Previous {
		t.Fatalf("it remembers %d judgments, wanted %d", len(back.Previous), Previous)
	}
}

// EVERY RUN SAYS WHAT IT CAME TO, IN THE RUN'S OWN FOLDER. The item's
// LastOutcome is overwritten by the next firing, so the marker is the only
// thing on disk that can tell a folder full of runs apart — and it is what the
// sweep reads before it removes one ([RunCameToNothing]).
func TestTickWritesWhatEachRunCameTo(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	for _, probe := range []struct {
		kind   string
		reaped bool
	}{
		{"landed", false},
		{"needs-you", false},
		{OutcomeNothing, true},
	} {
		store := openStore(t, now)
		runner := &fakeRunner{outcome: Outcome{Kind: probe.kind, Text: "what it came to"}}
		nightly := reminder("tonight run the full suite", now)
		nightly.Does = Action{Kind: ActionTask, Brief: "run it"}
		made, err := store.Create(nightly)
		if err != nil {
			t.Fatal(err)
		}

		mustTick(t, newTicker(store, runner, now))

		if len(runner.runDirs) != 1 {
			t.Fatalf("%s: %d runs", probe.kind, len(runner.runDirs))
		}
		runDir := runner.runDirs[0]
		raw, err := os.ReadFile(filepath.Join(runDir, CameTo))
		if err != nil {
			t.Fatalf("%s: the run left no marker: %v", probe.kind, err)
		}
		if strings.TrimSpace(string(raw)) != probe.kind {
			t.Fatalf("%s: the marker says %q", probe.kind, raw)
		}
		if got := RunCameToNothing(runDir); got != probe.reaped {
			t.Fatalf("%s: RunCameToNothing = %v", probe.kind, got)
		}
		back, err := store.Get(made.ID)
		if err != nil || back.LastRun != runDir {
			t.Fatalf("%s: the item does not name its run: %+v %v", probe.kind, back, err)
		}
		// AND A RUN THAT CAME TO NOTHING STILL RAN. The folder is the only thing
		// the sweep may reap; the item's own account of itself and the day's
		// ledger are what say it happened at all, and the ledger is money — it is
		// never reaped, whatever the run came to.
		if len(back.Previous) != 1 || !strings.Contains(back.Previous[0], probe.kind) {
			t.Fatalf("%s: the item's previous line is %v", probe.kind, back.Previous)
		}
		spend, err := store.Today(made.ID, now)
		if err != nil || spend.Fired != 1 {
			t.Fatalf("%s: the ledger counted %d firings (%v)", probe.kind, spend.Fired, err)
		}
	}
}

// A RUN WITH NO MARKER IS A RUN NOBODY MAY REMOVE. That is every run written
// before the marker existed, and every run whose disk was full when it ended.
func TestRunCameToNothingIsFalseWithoutAMarker(t *testing.T) {
	dir := t.TempDir()
	if RunCameToNothing(dir) {
		t.Fatal("a run folder with no marker claims it delivered nothing")
	}
	if err := os.WriteFile(filepath.Join(dir, CameTo), []byte("landed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if RunCameToNothing(dir) {
		t.Fatal("a run that landed claims it delivered nothing")
	}
	if err := os.WriteFile(filepath.Join(dir, CameTo), []byte(OutcomeNothing+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !RunCameToNothing(dir) {
		t.Fatal("a run that delivered nothing was not readable as one")
	}
}

// ── a rule that never wakes ─────────────────────────────────────────────────

// holding is a rule as the card stands one up: the person's sentence, a
// workspace, and nothing else at all. No moment, no rhythm, no probe, no action
// and no rails — a hold cannot fire, so it cannot spend, so there is nothing to
// bound.
func holding(words string) Item {
	return Item{
		Words:     words,
		Workspace: "/tmp/project",
		When:      When{Kind: WhenHold},
	}
}

// THE PASS WALKS PAST A HOLD AND LEAVES NO TRACE OF HAVING LOOKED.
//
// Its whole work was done at birth — it rides into the world of every
// conversation and every task it reaches — so a pass has nothing to do to it and
// says nothing about it. Every assertion here is about something NOT happening,
// which is the only way to state quiet: no probe, no judgment, no ledger line, no
// log line, no running marker, and a next moment that stays empty for the rest of
// its life.
func TestTickWalksPastAHoldAndWritesNothing(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{evidence: "anything at all"}
	made, err := store.Create(holding("always run the tests before you say you are done"))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if !made.NextDue.IsZero() {
		t.Fatalf("a rule was given a next moment: %s", made.NextDue)
	}

	ticker := newTicker(store, runner, now)
	ticker.Sentinel = func(context.Context, Judgment) (bool, string, float64, error) {
		t.Fatal("a rule was put in front of the sentinel")
		return false, "", 0, nil
	}
	// Twice, an hour apart: a hold is walked past on the pass that meets it and on
	// every pass after it, and neither one may leave a first reading behind the way
	// a file watch's baseline does.
	for _, at := range []time.Time{now, now.Add(time.Hour)} {
		store.clock = held(at)
		ticker.Now = held(at)
		pass := mustTick(t, ticker)
		if pass.Examined != 1 {
			t.Fatalf("the pass examined %d items, wanted the one rule", pass.Examined)
		}
		if pass.Checked != 0 || pass.Fired != 0 || pass.Skipped != 0 || pass.Errors != 0 || len(pass.Notes) != 0 {
			t.Fatalf("a rule made the pass say something: %+v", pass)
		}
	}
	if runner.probeSeen != 0 || len(runner.said) != 0 || len(runner.runDirs) != 0 {
		t.Fatalf("a rule reached the runner: %+v", runner)
	}

	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !back.NextDue.IsZero() {
		t.Fatalf("the rule was given a next moment: %s", back.NextDue)
	}
	if !back.LastChecked.IsZero() || back.LastCheckLine != "" {
		t.Fatalf("the rule claims it was checked: %s %q", back.LastChecked, back.LastCheckLine)
	}
	if back.Runs != 0 || back.SpentUSD != 0 || back.Status != StatusActive {
		t.Fatalf("the rule moved: %+v", back)
	}
	if spend, err := store.Today("", now); err != nil || spend.Fired != 0 || spend.USD != 0 {
		t.Fatalf("a rule wrote a ledger line: %+v (%v)", spend, err)
	}
	if _, err := os.Stat(store.LogPath(made.ID)); !os.IsNotExist(err) {
		t.Fatalf("a rule wrote a log line: %v", err)
	}
	if _, err := os.Stat(store.RunningPath(made.ID)); !os.IsNotExist(err) {
		t.Fatalf("a rule was marked as being worked on: %v", err)
	}
}

// AND A RULE THE PERSON GAVE AN END TO STILL REACHES IT. The walk-past is asked
// after the expiry and not before, because "never touch the public API until the
// release lands" is a rule with a last day, and a pass that skipped it entirely
// would hold it forever.
func TestTickRetiresAHoldThatRanOutOfTime(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	rule := holding("never touch the public API until the release lands")
	rule.Rails.Expires = now.Add(time.Hour)
	made, err := store.Create(rule)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	after := now.Add(2 * time.Hour)
	store.clock = held(after)
	pass := mustTick(t, newTicker(store, &fakeRunner{}, after))
	if pass.Skipped != 1 {
		t.Fatalf("the expired rule was not retired: %+v", pass)
	}
	back, err := store.Get(made.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Status != StatusRetired || back.RetiredWhy != "expired" {
		t.Fatalf("the rule is %q because %q", back.Status, back.RetiredWhy)
	}
}

// THE TRUST COUNTER IS KEPT WHERE A FIRING IS RECORDED, AND A QUESTION BREAKS
// IT. This is the arithmetic the rope column's middle rung is drawn from
// ([RopeWord]), and it is asserted against real firings through the real
// recorder because a streak is not recoverable from the record afterwards —
// nothing else on the item remembers what the firing before last came to.
func TestFiringKeepsTheCountOfCleanRunsInARow(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	store := openStore(t, now)
	runner := &fakeRunner{outcome: Outcome{Kind: "said", Text: "CI is red", USD: 0.01}}

	watch := reminder("every hour, tell me if CI went red", time.Time{})
	watch.When = When{Kind: WhenEvery, Every: "1h"}
	watch.Grant = "tell me without asking"
	// The rail is raised because this test needs eight firings and the default
	// fixture allows three a day. The rail is not what is under test here.
	watch.Rails = Rails{PerRunUSD: 0.05, MaxPerDay: 50}
	made, err := store.Create(watch)
	if err != nil {
		t.Fatal(err)
	}

	// fireOnce walks the clock past the next due moment and takes one pass, so
	// each call is one real firing through the one recorder.
	at := now
	fireOnce := func() Item {
		t.Helper()
		at = at.Add(61 * time.Minute)
		store.clock = held(at)
		if pass := mustTick(t, newTicker(store, runner, at)); pass.Fired != 1 {
			t.Fatalf("the pass fired %d times: %+v", pass.Fired, pass)
		}
		item, err := store.Get(made.ID)
		if err != nil {
			t.Fatal(err)
		}
		return item
	}

	for want := 1; want <= TrustAfter; want++ {
		item := fireOnce()
		if item.CleanRuns != want {
			t.Fatalf("after %d clean firings the count is %d", want, item.CleanRuns)
		}
		rope := RopeWord(item)
		if want < TrustAfter && rope == RopeTrusted {
			t.Fatalf("%d clean firings already read as %q", want, rope)
		}
		if want == TrustAfter && rope != RopeTrusted {
			t.Fatalf("%d clean firings read as %q, want %q", want, rope, RopeTrusted)
		}
	}

	// AND ONE FIRING THAT STOPS ON A QUESTION PUTS IT BACK TO NOTHING, so the
	// column starts counting again from where somebody has to start watching
	// again — five clean mornings do not buy an item past the one that asked.
	runner.outcome = Outcome{Kind: OutcomeNeedsYou, Text: "it wants to drop a column",
		NeedsPerson: "should it drop the column?", USD: 0.02}
	item := fireOnce()
	if item.CleanRuns != 0 {
		t.Fatalf("a firing that needed somebody left the count at %d", item.CleanRuns)
	}
	if got := RopeWord(item); got != "earning trust 0/5" {
		t.Fatalf("after a question the rope reads as %q", got)
	}

	// A FAILURE BREAKS IT THE SAME WAY, and it is a separate arm because a
	// runner may report a failure with nothing waiting for the person at all —
	// so the outcome kind has to be read as well as [Item.NeedsPerson].
	runner.outcome = Outcome{Kind: "said", Text: "CI is red", USD: 0.01}
	if item = fireOnce(); item.CleanRuns != 1 {
		t.Fatalf("the count did not start again: %d", item.CleanRuns)
	}
	runner.outcome = Outcome{Kind: OutcomeFailed, Text: "the probe could not reach the host"}
	if item = fireOnce(); item.CleanRuns != 0 {
		t.Fatalf("a failed firing left the count at %d", item.CleanRuns)
	}
}
