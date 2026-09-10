package standing

// tick.go is the pass: the two hundred lines that are the whole of the ambient
// side's behaviour. Everything else in this package is storage for it.
//
// ONE PASS IS: take the lock or go away, walk the active items, decide which
// ones the world has something to say about, spend within the rails, and write
// one line saying it happened. It is called every five minutes by whichever of
// a live window or the operating system's timer gets there first, and the two
// run exactly the same code.
//
// THE ORDER OF THE RAILS IS THE ORDER OF THE CONSEQUENCES. Expiry first, since
// a dead item should not be looked at, let alone paid for. Then the item's own
// daily count, then the whole day's spending — both BEFORE the probe, because a
// probe costs money and an item that could not fire if it wanted to has no
// business buying evidence.
//
// NOTHING HERE BLOCKS ON A PERSON, and one item's bad day never ends the pass:
// a failure is counted, noted, written onto that item, and the walk goes on.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Tick runs one pass: take the lock (or decline), walk every active item, wake
// the due ones, judge, fire within the rails, write the wake log, release. It
// never blocks on a person and never runs past ctx.
func (t *Ticker) Tick(ctx context.Context) (Pass, error) {
	if t == nil || t.Store == nil {
		return Pass{}, errors.New("standing: a pass with no store")
	}
	if err := ctx.Err(); err != nil {
		return Pass{}, err
	}
	release, err := t.Store.takeTickLock()
	if err != nil {
		return Pass{}, err
	}
	defer release()

	pass := Pass{At: t.clock()}
	items, err := t.Store.List()
	if err != nil {
		return pass, err
	}
	for _, item := range items {
		if ctx.Err() != nil {
			// A pass that ran out of time stops where it is. The items it did
			// not reach are simply due again in five minutes; there is no state
			// to unwind, which is the whole reason the pass is written this way.
			pass.Notes = append(pass.Notes, "the pass ran out of time")
			break
		}
		if item.Status != StatusActive {
			continue
		}
		pass.Examined++
		if err := t.one(ctx, &pass, item); err != nil {
			pass.Errors++
			pass.Notes = append(pass.Notes, shorten(item.Words, 60)+": "+oneLine(err.Error()))
		}
	}
	// THE TIDY GOES LAST AND IS NOT AN ITEM. Everything the person actually
	// armed is walked first, because a pass that ran out of time owes them their
	// own reminders before it owes them a tidier brain.
	t.tidy(ctx, &pass)
	if err := t.Store.appendWake(pass); err != nil {
		pass.Errors++
		pass.Notes = append(pass.Notes, "could not write the wake log: "+oneLine(err.Error()))
	}
	return pass, nil
}

// tidy runs the consolidation pass over what is remembered, once, at the end of
// a pass (internal/session's memory_consolidate.go owns the call itself).
//
// IT RUNS HERE BECAUSE THIS IS THE MACHINE'S ONE ELECTED IDLE PASS. The lock is
// already held, exactly one process in the world is inside it, and a second
// timer for off-path memory work would be a second thing to install, a second
// thing to hold a lock for and a second thing to explain to somebody reading
// /status.
//
// IT SPENDS UNDER THE SAME DAILY RAIL AS EVERY FIRING, and the rail is read
// here rather than inside the pass for RAIL THREE's reason: what the day has
// spent is the ticker's question, and a second reader of the ledger is where
// the two would come to disagree.
//
// A NIL Tidy IS MEMORY OFF and is silent — no note, no error, nothing in the
// wake log. A capability that cannot work is absent, not broken.
func (t *Ticker) tidy(ctx context.Context, pass *Pass) {
	if t.Tidy == nil || ctx.Err() != nil {
		return
	}
	if t.DailyRailUSD > 0 {
		all, err := t.Store.Today("", t.clock())
		if err != nil || all.USD >= t.DailyRailUSD {
			return
		}
	}
	tidied, err := t.Tidy(ctx)
	if err != nil {
		pass.Errors++
		pass.Notes = append(pass.Notes, "tidying what is remembered: "+oneLine(err.Error()))
		return
	}
	// THE MONEY IS WRITTEN EVEN WHEN NOTHING MOVED. A call that read fifty lines
	// and decided every one of them was already right was still billed, and a
	// rail told only about the passes that changed something is a rail quoting a
	// figure that is too small.
	if tidied.USD > 0 {
		if err := t.Store.Append(Entry{At: t.clock(), Kind: entryTidy, USD: tidied.USD}); err != nil {
			pass.Notes = append(pass.Notes, "could not write the ledger line: "+oneLine(err.Error()))
		}
	}
	pass.Tidied += tidied.Changed()
	if line := tidied.Line(); line != "" {
		pass.Notes = append(pass.Notes, line)
	}
}

// clock is the ticker's own now, taken once per item so that everything one
// item writes agrees with itself.
func (t *Ticker) clock() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// one is a single item's whole pass: the rails, the look, and the firing.
func (t *Ticker) one(ctx context.Context, pass *Pass, item Item) (failure error) {
	before := item
	ownedByFire := false
	now := t.clock()
	// Keep the observed spend and schedule changes on failure too. The outer
	// list's stale copy cannot describe what this check actually consumed.
	defer func() {
		if failure != nil && !ownedByFire {
			t.noteFailure(before, item, failure)
		}
	}()
	admitted, err := t.Store.currentAdmission(before)
	if err != nil {
		return err
	}
	if !admitted {
		return nil
	}

	// RAIL ONE: it ran out of time.
	if deadline, has := expiryOf(item); has && !now.Before(deadline) {
		item.Status = StatusRetired
		item.RetiredWhy = "expired"
		item.LastChecked = now
		item.LastCheckLine = "its time ran out"
		if err := t.Store.recordRuntime(before, item); err != nil {
			return err
		}
		current, err := t.Store.Get(item.ID)
		if err != nil {
			return err
		}
		if current.Status == StatusRetired && current.RetiredWhy == "expired" {
			pass.Skipped++
			pass.Notes = append(pass.Notes, shorten(item.Words, 60)+": its time ran out")
			_ = t.Store.Log(item.ID, "its time ran out — no longer watching")
		}
		return nil
	}

	// AND A HOLD IS WALKED PAST IN SILENCE. It has no moment, no rhythm and no
	// probe, so there is nothing about it that could be due; its whole work was
	// done at birth, riding into the world of every conversation and every task it
	// reaches (internal/session/standing_world.go). Nothing is looked at, so
	// NOTHING IS WRITTEN: no marker goes up, no probe runs, no judgment is bought,
	// no ledger line, no log line, and NextDue stays empty for the rest of its
	// life. It is not counted as skipped either — the rails held nothing back,
	// there was simply nothing here to wake. It is asked AFTER the expiry above
	// because a rule the person gave an end to still has to reach that end.
	if item.When.Kind == WhenHold {
		return nil
	}

	// RAIL TWO: it has already run today as often as the person allowed.
	mine, err := t.Store.Today(item.ID, now)
	if err != nil {
		return err
	}
	if item.Rails.MaxPerDay > 0 && mine.Fired >= item.Rails.MaxPerDay {
		pass.Skipped++
		return t.quiet(before, item, now, "it has already run today as often as you allowed")
	}

	// RAIL THREE: everything standing has spent what the day allows.
	if t.DailyRailUSD > 0 {
		all, err := t.Store.Today("", now)
		if err != nil {
			return err
		}
		if all.USD >= t.DailyRailUSD {
			pass.Skipped++
			pass.Notes = append(pass.Notes, "today's spending limit is reached; nothing standing runs again until tomorrow")
			return t.quiet(before, item, now, "today's spending limit is reached")
		}
	}

	// THE MARKER GOES UP BEFORE THE LOOK AND COMES DOWN WHEN THIS ITEM'S PASS
	// ENDS, whatever the pass came to — a firing, a quiet check, an error, or
	// nothing at all. It is raised HERE and not at the top of the method because
	// the three rails above are a decision not to look: an item that was skipped
	// for its budget was never in anybody's hands, and saying otherwise would be
	// the same dishonesty as writing LastChecked for a check that never happened.
	//
	// The defer is the only thing that takes it down in this process, so every
	// road out of the walk below — including a panic climbing through — leaves
	// the item unmarked (running.go says what happens to a marker whose process
	// never got that far).
	t.Store.markRunning(item.ID, RunningChecking)
	defer t.Store.clearRunning(item.ID)

	found, err := t.look(ctx, &item, now)
	if err != nil {
		return err
	}
	switch found.state {
	case stateAsleep:
		// Nothing was looked at, so nothing is written. An item that says it
		// was checked when it was not is the one dishonesty this design has no
		// tolerance for.
		return nil
	case stateQuiet:
		pass.Checked++
		return t.quiet(before, item, now, found.line)
	}
	pass.Checked++
	admitted, err = t.Store.currentAdmission(before)
	if err != nil {
		return err
	}
	if !admitted {
		return t.quiet(before, item, now, "the item changed while it was being checked")
	}
	ownedByFire = true
	return t.fire(ctx, pass, before, item, now, found)
}

// state is what one look at the world came to. The names are spelled out
// because the pass reads them beside a method called quiet and a method called
// look, and a reader should never have to work out which is which.
type state int

const (
	stateAsleep state = iota // its time has not come; nothing was looked at
	stateQuiet               // it was looked at and the world had nothing to say
	stateReady               // it is time, or the world changed, or the sentinel said yes
)

// sighting is one look's answer: what state it left the item in, the one plain
// sentence a person would read for it, and whatever evidence a firing should
// carry with it.
type sighting struct {
	state    state
	line     string
	evidence string
	// changes are the files a watch saw change, and changesUnknown says the
	// previous reading could not be read — never the same as "none changed".
	changes        []Change
	changesUnknown bool
	// since is the reading the change list was measured from.
	since string
}

// look decides whether an item wants to fire, and updates the parts of the item
// that a look changes whatever it decides: the next moment, and the fingerprint.
func (t *Ticker) look(ctx context.Context, item *Item, now time.Time) (sighting, error) {
	found := sighting{}
	switch item.When.Kind {
	case WhenAt:
		// A one-shot that finished while paused is still consumed when resumed.
		// Preserve the pause, without delivering that same moment twice.
		if !item.LastFired.IsZero() && !item.LastFired.Before(item.When.At) {
			return sighting{state: stateAsleep}, nil
		}
		if now.Before(item.When.At) {
			return sighting{state: stateAsleep}, nil
		}
		found = sighting{state: stateReady, line: "it was the time you asked for"}

	case WhenEvery:
		next, err := ParseEvery(item.When.Every)
		if err != nil {
			return sighting{}, err
		}
		if item.NextDue.IsZero() {
			item.NextDue = next(now)
			return sighting{state: stateQuiet, line: "waiting for its time"}, nil
		}
		if now.Before(item.NextDue) {
			return sighting{state: stateAsleep}, nil
		}
		// THE RHYTHM MOVES ON BECAUSE ITS TIME CAME, not because it fired. A
		// routine whose hint says "nothing worth saying this morning" must wait
		// for tomorrow morning like any other; advancing this only on a firing
		// would leave it due, and therefore judged, every five minutes until it
		// finally said yes.
		item.NextDue = next(now)
		found = sighting{state: stateReady, line: "it was the time you asked for"}

	case WhenFile:
		digest, listing, files, err := fingerprint(item.Workspace, item.When.Glob)
		if err != nil {
			return sighting{}, err
		}
		previous := item.Fingerprint
		first := previous == ""
		changed := !first && digest != previous
		item.Fingerprint = digest
		since := previous
		if item.Does.Kind == ActionTask {
			since = t.Store.unreportedSince(item.ID, previous)
		}
		t.Store.keepReading(item.ID, digest, files, previous, since)
		switch {
		case first:
			// THE FIRST READING IS THE BASELINE AND IS SILENT. Everything on
			// disk looks new to a watch that has never looked, and telling a
			// person their whole repository just changed would be the last time
			// they trusted one of these.
			return sighting{state: stateQuiet, line: "nothing has changed yet"}, nil
		case !changed:
			return sighting{state: stateQuiet, line: "nothing has changed"}, nil
		}
		changes, readErr := t.Store.changesSince(item.ID, since, files)
		found = sighting{
			state: stateReady, line: "the files you are watching changed",
			evidence: changesText(changes, readErr != nil, since != previous) + "\nALL MATCHING FILES:\n" + listing,
			changes:  changes, changesUnknown: readErr != nil, since: since,
		}

	case WhenIdle:
		if t.Idle == nil {
			// Nobody in this process can say whether the machine is quiet, so
			// it never is. A capability that cannot work is absent.
			return sighting{state: stateAsleep}, nil
		}
		if !t.Idle(item.When.IdleFor) {
			return sighting{state: stateQuiet, line: "the machine has not been quiet long enough"}, nil
		}
		found = sighting{state: stateReady, line: "the machine has been quiet"}

	case WhenProbe:
		every := item.When.ProbeEvery
		if every <= 0 {
			every = Interval
		}
		if !item.NextDue.IsZero() && now.Before(item.NextDue) {
			return sighting{state: stateAsleep}, nil
		}
		if t.Runner == nil {
			return sighting{}, errors.New("there is nothing in this build to look with")
		}
		evidence, err := t.Runner.Probe(ctx, *item)
		item.NextDue = now.Add(every)
		if err != nil {
			return sighting{}, err
		}
		admitted, admissionErr := t.Store.currentAdmission(*item)
		if admissionErr != nil {
			return sighting{}, admissionErr
		}
		if !admitted {
			return sighting{state: stateQuiet, line: "the item changed while it was being checked"}, nil
		}
		evidence = clipTail(evidence, ProbeClip)
		yes, line, err := t.judge(ctx, item, now, evidence)
		if err != nil {
			return sighting{}, err
		}
		if !yes {
			return sighting{state: stateQuiet, line: line}, nil
		}
		return sighting{state: stateReady, line: line, evidence: evidence}, nil

	default:
		return sighting{}, errors.New("standing: an unknown kind of watch: " + string(item.When.Kind))
	}

	// A hint on a kind that does not need judgment asks for it anyway: "every
	// weekday at 8, IF there is anything worth saying". The sentinel is given no
	// evidence, because there is none — only the person's words and the hint.
	if item.When.Hint != "" {
		yes, line, err := t.judge(ctx, item, now, "")
		if err != nil {
			return sighting{}, err
		}
		if !yes {
			return sighting{state: stateQuiet, line: line}, nil
		}
		found.line = line
	}
	return found, nil
}

// judge is the one cheap yes/no call, with the item's last few judgments riding
// along so that a thing already said is not said again every five minutes.
//
// A JUDGMENT IS BILLED WHETHER OR NOT IT SAYS YES. It is the auxiliary line the
// card promised, and a watch that looks a hundred times to fire once has spent
// a hundred looks' worth of the day's money.
func (t *Ticker) judge(ctx context.Context, item *Item, now time.Time, evidence string) (bool, string, error) {
	if t.Sentinel == nil {
		return false, "", errors.New("there is nothing in this build to judge with")
	}
	yes, line, usd, err := t.Sentinel(ctx, Judgment{Item: *item, Evidence: evidence, Previous: item.Previous})
	if err != nil {
		return false, "", err
	}
	if usd > 0 {
		item.SpentUSD += usd
		if err := t.Store.Append(Entry{At: now, ItemID: item.ID, Kind: entryCheck, USD: usd}); err != nil {
			return false, "", err
		}
	}
	return yes, line, nil
}

// quiet is the whole of a check that found nothing: the item remembers it
// looked, and NOTHING ELSE IS WRITTEN ANYWHERE.
func (t *Ticker) quiet(before, item Item, now time.Time, line string) error {
	item.LastChecked = now
	item.LastCheckLine = oneLine(line)
	return t.Store.recordRuntime(before, item)
}

// fire is the firing and everything it leaves behind.
func (t *Ticker) fire(ctx context.Context, pass *Pass, before, item Item, now time.Time, found sighting) (failure error) {
	recorded := false
	defer func() {
		if failure != nil && !recorded {
			t.noteFailure(before, item, failure)
		}
	}()
	if t.Runner == nil {
		return errors.New("there is nothing in this build to run it with")
	}
	// The look said yes, so the marker stops saying "checking" and starts saying
	// "firing". [Ticker.one] raised it and [Ticker.one] takes it down; this is
	// the same marker changing its mind, not a second one.
	t.Store.markRunning(item.ID, RunningFiring)
	var (
		outcome Outcome
		runDir  string
		err     error
	)
	switch item.Does.Kind {
	case ActionSay:
		outcome, err = t.Runner.Say(ctx, item, strings.ReplaceAll(item.Does.Say, "{{evidence}}", found.evidence))
	case ActionTask:
		key := occurrenceKey(before, now)
		// AN OCCURRENCE THAT ALREADY FINISHED IS RECORDED, NOT RUN AGAIN. Its
		// record says it ended; only the item's document never heard, because
		// the process stopped between the two writes. Running it again would
		// publish and deliver the same occurrence twice (occurrence.go).
		if done, finished := t.Store.finishedOccurrence(item.ID, key); finished {
			recorded = true
			pass.Notes = append(pass.Notes, shorten(item.Words, 60)+": recorded an occurrence that had finished before its process stopped")
			return t.recoverFinished(before, item, done)
		}
		runDir, err = t.Store.newRunDir(item.ID)
		if err != nil {
			return err
		}
		// THE CAUSE IS WRITTEN BEFORE THE WORK STARTS, and a firing whose cause
		// cannot be written does not start: a run folder that cannot say what
		// woke it is the question this record exists to answer (occurrence.go).
		occurrence := t.admit(before, item, now, found, key, runDir)
		if err := WriteOccurrence(runDir, occurrence); err != nil {
			return fmt.Errorf("could not record this occurrence: %w", err)
		}
		outcome, err = t.Runner.Run(ctx, item, runDir, found.evidence)
		t.finishOccurrence(runDir, occurrence, outcome, err)
	default:
		return errors.New("standing: an unknown kind of action: " + string(item.Does.Kind))
	}
	if err != nil {
		return err
	}

	item.Runs++
	item.LastFired = now
	item.LastChecked = now
	item.LastCheckLine = oneLine(found.line)
	item.LastOutcome = outcome.Kind
	item.SpentUSD += outcome.USD
	item.NeedsPerson = outcome.NeedsPerson
	if outcome.Kind == OutcomeNeedsYou && item.NeedsPerson == "" {
		item.NeedsPerson = oneLine(outcome.Text)
	}
	// THE TRUST COUNTER IS KEPT HERE BECAUSE THIS IS WHERE A FIRING IS RECORDED,
	// beside [Item.Runs] and for the same reason: it is the one place in the
	// program that knows a firing happened and how it came back. A streak is not
	// recoverable from the record afterwards — LastOutcome is overwritten by the
	// next firing — which [Item.CleanRuns] states at length.
	//
	// A QUESTION OR A FAILURE PUTS IT BACK TO NOTHING. Trust is a run of clean
	// firings and not a tally of them: an item that needed somebody last night
	// is an item somebody has to watch again, whatever it did the fortnight
	// before. NeedsPerson is read rather than the outcome kind alone, because a
	// runner may hand back a question on an outcome of any kind and the field is
	// where that question actually lands.
	if item.NeedsPerson != "" || outcome.Kind == OutcomeFailed {
		item.CleanRuns = 0
	} else {
		item.CleanRuns++
	}
	if runDir != "" {
		item.LastRun = runDir
		writeCameTo(runDir, outcome.Kind)
	}
	item.Previous = remember(item.Previous, found.line, outcome)

	if item.When.Kind == WhenAt {
		// A reminder is the smallest of these: it fires once and it retires.
		item.Status = StatusRetired
		item.RetiredWhy = "fired"
	}

	pass.Fired++
	if item.Does.Kind == ActionSay {
		pass.Said++
	}
	if item.NeedsPerson != "" {
		pass.NeedsYou++
		pass.Notes = append(pass.Notes, shorten(item.Words, 60)+": "+oneLine(shorten(item.NeedsPerson, 240)))
	} else if outcome.Kind == OutcomeFailed {
		// A RUN THAT DID NOT FINISH IS SAID, by name and in its own words, so
		// whoever ran the pass by hand learns it from the pass rather than from
		// a report that silently stayed as it was.
		pass.Failed++
		pass.Notes = append(pass.Notes, shorten(item.Words, 60)+": "+oneLine(shorten(outcome.Text, 240)))
	}
	ledgerErr := t.Store.Append(Entry{At: now, ItemID: item.ID, Kind: string(item.Does.Kind), USD: outcome.USD, Run: runDir})
	logErr := t.Store.Log(item.ID, firingLine(found, outcome))
	runtimeErr := t.Store.recordRuntime(before, item)
	recorded = runtimeErr == nil
	return errors.Join(ledgerErr, logErr, runtimeErr)
}

// admit is the occurrence record for one firing, before it runs: which item
// state it was admitted from, which version of the instructions it runs on,
// what woke it, and which interrupted attempts of the same occurrence it
// supersedes.
func (t *Ticker) admit(before, item Item, now time.Time, found sighting, key, runDir string) Occurrence {
	id := item.ID + "/" + filepath.Base(runDir)
	supersedes, attempts := t.Store.interruptedAttempts(item.ID, key, id)
	occurrence := Occurrence{
		ID: id, ItemID: item.ID, Key: key,
		Spec: before.SpecRevision, Revision: before.Revision,
		Words: item.Words, Brief: item.Does.Brief, Report: item.Does.Report,
		Trigger: item.When, Because: oneLine(found.line),
		Changes: found.changes, ChangesUnknown: found.changesUnknown, Since: found.since,
		PreviousRun: before.LastRun, PreviousFired: before.LastFired,
		Admitted: now, PID: os.Getpid(),
		Attempt: attempts + 1, Supersedes: supersedes,
		Phase: PhaseAdmitted,
	}
	switch item.When.Kind {
	case WhenEvery:
		occurrence.Due = before.NextDue
	case WhenFile:
		occurrence.Reading = item.Fingerprint
	}
	return occurrence
}

// finishOccurrence completes the record with what the run came to. It keeps a
// publication the runner already wrote into the record ([Outcome.Published]),
// so a receipt made before a failure is not erased by the failure.
func (t *Ticker) finishOccurrence(runDir string, admitted Occurrence, outcome Outcome, failure error) {
	record := admitted
	if current, err := ReadOccurrence(runDir); err == nil {
		record = current
	}
	record.Phase = PhaseFinished
	record.Finished = t.clock()
	record.Outcome = outcome.Kind
	record.OutcomeText = outcome.Text
	record.USD = outcome.USD
	if outcome.Published != nil {
		record.Published = outcome.Published
	}
	record.Withheld = outcome.Withheld
	if failure != nil {
		record.Outcome = OutcomeFailed
		record.Error = oneLine(failure.Error())
	}
	_ = WriteOccurrence(runDir, record)
}

// recoverFinished records an occurrence whose run finished before the item's
// document said so: the firing is counted once, the watch moves to the reading
// that occurrence reported on, and nothing is run, published or delivered.
func (t *Ticker) recoverFinished(before, item Item, done Occurrence) error {
	if done.Phase == PhaseAdmitted {
		// ITS REPORT WAS PUBLISHED AND ITS PROCESS STOPPED BEFORE THE REST WAS
		// WRITTEN ([Store.finishedOccurrence]). It landed — the receipt is the
		// proof — and the record says so now, with what cannot be known: the
		// note is delivered after the report and may or may not have gone, and
		// the run's cost was known only to that process.
		done.Phase = PhaseFinished
		done.Finished = t.clock()
		done.Outcome = "landed"
		done.OutcomeText = "report published to " + done.Published.Path + "; its process stopped before the rest was recorded, so whether its note was delivered, and what it cost, is not known"
		_ = WriteOccurrence(done.RunDir, done)
	}
	// The firing is dated when it was admitted, as [Ticker.fire] dates one.
	at := done.Admitted
	if at.IsZero() {
		at = done.Finished
	}
	item.Runs++
	item.LastFired = at
	item.LastChecked = t.clock()
	item.LastCheckLine = "recorded an occurrence that had finished before its process stopped"
	item.LastOutcome = done.Outcome
	item.LastRun = done.RunDir
	if item.When.Kind == WhenFile && done.Reading != "" {
		// THE WATCH MOVES TO WHAT THAT RUN SAW, NOT TO WHAT THIS LOOK SAW. A
		// change made after that run began has not been reported yet, and the
		// next pass has to find it.
		item.Fingerprint = done.Reading
	}
	writeCameTo(done.RunDir, done.Outcome)
	logErr := t.Store.Log(item.ID, "recorded "+filepath.Base(done.RunDir)+" — it finished before its process stopped; not run again")
	return errors.Join(logErr, t.Store.recordRuntime(before, item))
}

// writeCameTo leaves [CameTo] in the run folder: one word saying what this run
// delivered, which is what lets the sweep tell a run that came to nothing from
// one worth keeping.
//
// IT IS BEST EFFORT AND SAYS NOTHING WHEN IT FAILS, and the failure is safe in
// the one direction that matters: a run with no marker is a run the sweep never
// touches, so a full disk costs a folder that lives forever rather than one
// that is removed on a guess.
func writeCameTo(runDir, kind string) {
	kind = strings.TrimSpace(kind)
	if runDir == "" || kind == "" {
		return
	}
	_ = os.WriteFile(filepath.Join(runDir, CameTo), []byte(kind+"\n"), 0o600)
}

// remember keeps the last few judgments with what came of each, newest first.
// It is what stops a firing the person has already seen being proposed again on
// every wake for the rest of the week.
func remember(previous []string, line string, outcome Outcome) []string {
	said := oneLine(line)
	if said == "" {
		said = "it was time"
	}
	if outcome.Kind != "" {
		said += " → " + outcome.Kind
	}
	kept := append([]string{said}, previous...)
	if len(kept) > Previous {
		kept = kept[:Previous]
	}
	return kept
}

func firingLine(found sighting, outcome Outcome) string {
	line := oneLine(found.line)
	if outcome.Kind != "" {
		line += " — " + outcome.Kind
	}
	if text := oneLine(outcome.Text); text != "" {
		line += ": " + shorten(text, 200)
	}
	return line
}

// noteFailure writes a failure onto the item so the card can say what went
// wrong, rather than showing a watch that silently stopped working weeks ago.
func (t *Ticker) noteFailure(before, item Item, failure error) {
	now := t.clock()
	item.LastChecked = now
	item.LastCheckLine = "could not check: " + shorten(oneLine(failure.Error()), 200)
	_ = t.Store.recordRuntime(before, item)
	_ = t.Store.Log(item.ID, item.LastCheckLine)
}

// expiryOf answers when an item stops being watched. A WhenAt item expires a
// day after its moment whatever the rails say, because a reminder for six
// o'clock that nothing woke up to deliver is not still worth delivering on
// Thursday.
func expiryOf(item Item) (time.Time, bool) {
	deadline := item.Rails.Expires
	if item.When.Kind == WhenAt {
		ceiling := item.When.At.Add(24 * time.Hour)
		if deadline.IsZero() || ceiling.Before(deadline) {
			deadline = ceiling
		}
	}
	if deadline.IsZero() {
		return time.Time{}, false
	}
	return deadline, true
}

// fingerprint is a WhenFile's reading of the world: the names, sizes and
// modification times of everything the glob matches, hashed. The listing beside
// it is what the firing is told, since a hash is evidence of nothing to a model
// or to a person.
func fingerprint(workspace, glob string) (string, string, map[string]fileEntry, error) {
	pattern := glob
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(workspace, pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", "", nil, fmt.Errorf("standing: cannot read the pattern %q: %w", glob, err)
	}
	sort.Strings(matches)
	digest := sha256.New()
	listing := &strings.Builder{}
	files := make(map[string]fileEntry, len(matches))
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil {
			// A file that vanished between the glob and the stat is a change
			// like any other; the next reading will not have it either.
			continue
		}
		name := match
		if relative, err := filepath.Rel(workspace, match); err == nil {
			name = relative
		}
		fmt.Fprintf(digest, "%s|%d|%d\n", name, info.Size(), info.ModTime().UnixNano())
		files[name] = fileEntry{Size: info.Size(), MTime: info.ModTime().UnixNano()}
		if listing.Len() < ProbeClip {
			fmt.Fprintf(listing, "%s  %d bytes  %s\n", name, info.Size(), info.ModTime().Format(time.RFC3339))
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), listing.String(), files, nil
}

// appendWake writes the one line per pass that "last wake" and "next check" are
// derived from. It is the only proof, anywhere, that the machine is awake.
func (s *Store) appendWake(pass Pass) error {
	if err := os.MkdirAll(s.root, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(s.WakeLogPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	line := pass.At.Format(time.RFC3339) +
		" examined=" + strconv.Itoa(pass.Examined) +
		" checked=" + strconv.Itoa(pass.Checked) +
		" fired=" + strconv.Itoa(pass.Fired) +
		" said=" + strconv.Itoa(pass.Said) +
		" needs=" + strconv.Itoa(pass.NeedsYou) +
		" skipped=" + strconv.Itoa(pass.Skipped) +
		" errors=" + strconv.Itoa(pass.Errors) +
		" tidied=" + strconv.Itoa(pass.Tidied) + "\n"
	if _, err := file.WriteString(line); err != nil {
		return err
	}
	return file.Close()
}

// clipTail keeps the end of what a probe said, because the end is where a
// command puts what went wrong.
func clipTail(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[len(text)-limit:]
}

func shorten(text string, limit int) string {
	text = oneLine(text)
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "…"
}
