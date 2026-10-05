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
	"hash"
	"io"
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
	// SETTLE FIRST. Authorized deliveries that were never acknowledged are done
	// before any fresh look and before the rails, so a quiet item, an advanced
	// due moment or a spent daily rail can never strand one.
	t.settle(ctx, &pass, items)
	for i := range items {
		item := &items[i]
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
			var acct *accountingError
			if errors.As(err, &acct) {
				// The firing succeeded; only its accounting failed. Do not
				// relabel its outcome as a check failure.
				_ = t.Store.Log(item.ID, "the accounting for this firing could not be written: "+oneLine(acct.Error()))
			} else {
				t.noteFailure(*item, err)
			}
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
//
// IT TAKES THE ITEM BY POINTER because a firing writes twice — the delivery
// intent, then the result — and the revision that guards those writes has to
// travel with the item, not with a copy the caller kept.
func (t *Ticker) one(ctx context.Context, pass *Pass, item *Item) error {
	now := t.clock()

	// RAIL ONE: it ran out of time.
	if deadline, has := expiryOf(*item); has && !now.Before(deadline) {
		item.Status = StatusRetired
		item.RetiredWhy = "expired"
		item.LastChecked = now
		item.LastCheckLine = "its time ran out"
		pass.Skipped++
		pass.Notes = append(pass.Notes, shorten(item.Words, 60)+": its time ran out")
		_ = t.Store.Log(item.ID, "its time ran out — no longer watching")
		if err := t.Store.saveActive(item); err != nil && !errors.Is(err, errConsentChanged) {
			return err
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
		return t.quiet(*item, now, "it has already run today as often as you allowed")
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
			return t.quiet(*item, now, "today's spending limit is reached")
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

	// AN UNRESOLVED DELIVERY OR TASK ATTEMPT IS NOT A REASON TO FIRE AGAIN.
	// [Ticker.settle] already tried the delivery at the top of the pass; if it
	// is still here the last attempt failed, and firing a second line would be a
	// duplicate intent for the same authorization. A task whose in-flight marker
	// is up must never run twice, so it is surfaced as needing the person.
	if len(item.Pending) > 0 {
		pass.Skipped++
		return nil
	}
	if item.TaskInflight != nil {
		if item.NeedsPerson == "" {
			item.NeedsPerson = oneLine("a task is waiting for you before it can run again")
			if err := t.Store.saveActive(item); err != nil && !errors.Is(err, errConsentChanged) {
				return err
			}
		}
		pass.Skipped++
		return nil
	}

	found, err := t.look(ctx, item, now)
	if err != nil {
		return err
	}

	// CONSENT IS CHECKED BEFORE THE DELIVERY BEGINS. A probe may have taken
	// minutes and the person may have acted in them; the item read at the top of
	// the walk is stale, and a firing must not begin past their newer act. The
	// revision is the guard, not the status: a pause and a resume both leave the
	// status active. The save below is guarded again ([Store.saveActive]), and a
	// pause that lands DURING the delivery is not claimed as a cancelled one — it
	// only refuses to record the result.
	if current, err := t.Store.Get(item.ID); err != nil || current.Status != StatusActive || current.Revision != item.Revision {
		return nil
	}

	switch found.state {
	case stateAsleep:
		// Nothing was looked at, so nothing is written. An item that says it
		// was checked when it was not is the one dishonesty this design has no
		// tolerance for.
		return nil
	case stateUndecided:
		// It was looked at but nobody could decide. NOTHING IS WRITTEN — not
		// the check, not the next-due, not a negative — so the item stays due
		// and the next pass faces the same question. An unknown must not consume
		// the opportunity the person is still waiting on.
		pass.Checked++
		return nil
	case stateQuiet:
		pass.Checked++
		return t.quiet(*item, now, found.line)
	}
	pass.Checked++
	return t.fire(ctx, pass, item, now, found)
}

// state is what one look at the world came to. The names are spelled out
// because the pass reads them beside a method called quiet and a method called
// look, and a reader should never have to work out which is which.
type state int

const (
	stateAsleep    state = iota // its time has not come; nothing was looked at
	stateUndecided              // looked, but nobody could decide; nothing is written
	stateQuiet                  // it was looked at and the world had nothing to say
	stateReady                  // it is time, or the world changed, or the sentinel said yes
)

// sighting is one look's answer: what state it left the item in, the one plain
// sentence a person would read for it, and whatever evidence a firing should
// carry with it.
type sighting struct {
	state    state
	line     string
	evidence string
}

// look decides whether an item wants to fire, and updates the parts of the item
// that a look changes whatever it decides: the next moment, and the fingerprint.
func (t *Ticker) look(ctx context.Context, item *Item, now time.Time) (sighting, error) {
	found := sighting{}
	switch item.When.Kind {
	case WhenAt:
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
		digest, listing, truncated, err := fingerprint(item.Workspace, item.When.Glob)
		if err != nil {
			return sighting{}, err
		}
		first := item.Fingerprint == ""
		changed := !first && digest != item.Fingerprint
		item.Fingerprint = digest
		switch {
		case first:
			// THE FIRST READING IS THE BASELINE AND IS SILENT. An item that was
			// armed ([Store.Arm]) carries its reading already and never reaches
			// here; one that was not keeps the old, silent first look exactly.
			//
			// A WATCH THAT COULD NOT BE ARMED CARRIES A VISIBLE FLAG
			// ([NeedsBaselineLead], written by the ratifier). The first look
			// that reads everything it matches ESTABLISHES the baseline the arm
			// could not, so the flag comes down here rather than staying on the
			// item as a stale complaint — and a look that was still truncated
			// keeps it, because no baseline has been set even now.
			if IsBaselineLine(item.NeedsPerson) && !truncated {
				item.NeedsPerson = ""
			}
			return sighting{state: stateQuiet, line: "nothing has changed yet"}, nil
		case !changed:
			if truncated {
				// A bounded view cannot certify that nothing changed past the
				// bound. It says only what it could read.
				return sighting{state: stateQuiet, line: "nothing has changed in what I could read"}, nil
			}
			return sighting{state: stateQuiet, line: "nothing has changed"}, nil
		}
		found = sighting{state: stateReady, line: "the files you are watching changed", evidence: listing}

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
		evidence = clipTail(evidence, ProbeClip)
		verdict, line, err := t.judge(ctx, item, now, evidence)
		if err != nil {
			return sighting{}, err
		}
		if verdict == VerdictUnknown {
			// NOBODY COULD DECIDE. Nothing is written, so the item stays due and
			// the next pass faces the same question: an unknown must not consume
			// the opportunity the person is waiting on, and it must never read as
			// an established no.
			return sighting{state: stateUndecided, line: line}, nil
		}
		if verdict != VerdictYes {
			return sighting{state: stateQuiet, line: line}, nil
		}
		return sighting{state: stateReady, line: line, evidence: evidence}, nil

	default:
		return sighting{}, errors.New("standing: an unknown kind of watch: " + string(item.When.Kind))
	}

	// A hint on a kind that does not need judgment asks for it anyway: "every
	// weekday at 8, IF there is anything worth saying". The sentinel is given the
	// evidence the look gathered — the changed-file listing for a file watch, and
	// nothing for a kind that has none — so it is never asked to judge a change
	// it was told about but cannot see.
	if item.When.Hint != "" {
		verdict, line, err := t.judge(ctx, item, now, found.evidence)
		if err != nil {
			return sighting{}, err
		}
		if verdict == VerdictUnknown {
			// An undecided hint writes nothing at all, so the item stays due and
			// is judged again rather than counted as a no it never was.
			return sighting{state: stateUndecided, line: line}, nil
		}
		if verdict != VerdictYes {
			return sighting{state: stateQuiet, line: line}, nil
		}
		found.line = line
	}
	return found, nil
}

// judge is the one cheap call, with the item's last few judgments riding along
// so that a thing already said is not said again every five minutes.
//
// IT ANSWERS THREE WAYS, NOT TWO. [VerdictYes] fires; [VerdictNo] is a decided
// negative; [VerdictUnknown] — a refusal, a timeout, a provider that could not
// answer, or a sentinel that merely errored — is neither, and is never written
// into the item's history as a no.
//
// A JUDGMENT IS BILLED WHETHER OR NOT IT SAYS YES. It is the auxiliary line the
// card promised, and a watch that looks a hundred times to fire once has spent
// a hundred looks' worth of the day's money.
func (t *Ticker) judge(ctx context.Context, item *Item, now time.Time, evidence string) (Verdict, string, error) {
	if t.SentinelVerdict == nil && t.Sentinel == nil {
		return VerdictUnknown, "", errors.New("there is nothing in this build to judge with")
	}
	judgment := Judgment{Item: *item, Evidence: evidence, Previous: item.Previous}
	var (
		verdict Verdict
		line    string
		usd     float64
		err     error
	)
	if t.SentinelVerdict != nil {
		verdict, line, usd, err = t.SentinelVerdict(ctx, judgment)
	} else {
		var yes bool
		yes, line, usd, err = t.Sentinel(ctx, judgment)
		switch {
		case err != nil:
			verdict = VerdictUnknown
		case yes:
			verdict = VerdictYes
		default:
			verdict = VerdictNo
		}
	}
	if err != nil {
		// A REFUSAL, A TIMEOUT OR A PROVIDER THAT COULD NOT ANSWER REACHES HERE.
		// It is not a no and it is not this item's failure: the opportunity a
		// person is still waiting on stays open, and no negative is written into
		// [Item.Previous]. WHAT THE CALL COST IS STILL CHARGED WHEN THE CALLER
		// COULD NAME IT — a refusal that was billed is still a bill — and a
		// ledger that could not be written is a storage error that propagates.
		//
		// AND THE ITEM'S OWN LIFETIME FIGURE IS PERSISTED HERE, because the
		// undecided path writes NOTHING else on the item. [Store.NoteSpend] adds
		// only the cost, under the item's lock: no NextDue is set, no fingerprint
		// touched, no question written and the status unchanged, so the
		// opportunity the person is waiting on is not consumed. A failure to
		// record it is returned rather than swallowed, so the pass counts it.
		if usd > 0 {
			if lerr := t.Store.Append(Entry{At: now, ItemID: item.ID, Kind: entryCheck, USD: usd}); lerr != nil {
				return VerdictUnknown, oneLine(line), lerr
			}
			if serr := t.Store.NoteSpend(item.ID, usd); serr != nil {
				return VerdictUnknown, oneLine(line), serr
			}
			item.SpentUSD += usd
		}
		if strings.TrimSpace(line) == "" {
			line = "I could not tell"
		}
		return VerdictUnknown, oneLine(line), nil
	}
	if usd > 0 {
		item.SpentUSD += usd
		if err := t.Store.Append(Entry{At: now, ItemID: item.ID, Kind: entryCheck, USD: usd}); err != nil {
			return verdict, oneLine(line), err
		}
	}
	return verdict, oneLine(line), nil
}

// quiet is the whole of a check that found nothing: the item remembers it
// looked, and NOTHING ELSE IS WRITTEN ANYWHERE.
//
// IT SAVES GUARDED. The check ran against a copy read at the top of the walk;
// if the person paused or stopped the item while it was being looked at, their
// act wins and this write is refused rather than undoing it.
func (t *Ticker) quiet(item Item, now time.Time, line string) error {
	item.LastChecked = now
	item.LastCheckLine = oneLine(line)
	if err := t.Store.saveActive(&item); err != nil {
		if errors.Is(err, errConsentChanged) {
			return nil
		}
		return err
	}
	return nil
}

// fire is the firing and everything it leaves behind.
//
// THE ORDER IS THE CONTRACT. For [ActionSay] the intent — a [Pending] record
// with an identity — is written to the item BEFORE the line is carried out, and
// cleared only when the delivery is acknowledged. A crash between the two leaves
// the intent on disk, and a later pass settles it BY IDENTITY ([Ticker.settle])
// whatever the item's prerequisite now says. The identity is what an inbox
// dedups on: a retry is at-least-once against a Runner with no [Deliverer] and
// deduped by identity against one that has it. This package makes NO exactly-
// once promise about anything; it makes the retry carry the same identity and
// never mint a second intent for the same firing.
//
// A TASK IS THE OPPOSITE. It edits the world and no identity makes a second run
// safe, so a durable in-flight marker goes down BEFORE [Runner.Run], and a task
// whose marker is still up is never run again — it is reconciled as
// [Item.NeedsPerson], not replayed.
func (t *Ticker) fire(ctx context.Context, pass *Pass, item *Item, now time.Time, found sighting) error {
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
		text := strings.ReplaceAll(item.Does.Say, "{{evidence}}", found.evidence)
		pending, known := item.pendingSay(text)
		if !known {
			pending = Pending{ID: newID(), Kind: ActionSay, Text: text, At: now}
			if !item.addPending(pending) {
				// BACKPRESSURE, NOT SILENT EVICTION: a cap that dropped the
				// oldest undelivered line would be this package discarding the
				// person's own authorized work. It stops and says so instead.
				item.NeedsPerson = oneLine("there are already too many undelivered lines waiting")
				_ = t.Store.saveActive(item)
				return errors.New("standing: too many undelivered lines are waiting; not delivering another")
			}
		}
		// THE INTENT IS ON DISK BEFORE THE LINE IS CARRIED OUT. If this write
		// fails, nothing is delivered: an external notification must never
		// precede the durable state that lets a later pass reconcile it.
		if err := t.Store.saveActive(item); err != nil {
			item.dropPending(pending.ID)
			if errors.Is(err, errConsentChanged) {
				return nil
			}
			return fmt.Errorf("could not record the delivery before making it: %w", err)
		}
		outcome, err = t.deliver(ctx, *item, pending)
		if err != nil {
			// The line may or may not have landed. The intent STAYS, with a
			// counted attempt, so a later pass settles it by identity rather
			// than guessing; the error propagates so the pass counts it.
			item.bumpPending(pending.ID)
			if pendingAttempts(item, pending.ID) >= PendingGiveUp {
				item.NeedsPerson = oneLine("a line could not be delivered and is still waiting")
			}
			_ = t.Store.saveActive(item)
			return err
		}
		item.dropPending(pending.ID)
	case ActionTask:
		runDir, err = t.Store.newRunDir(item.ID)
		if err != nil {
			return err
		}
		// THE MARKER GOES DOWN BEFORE THE TASK RUNS. A crash after this line, or
		// an error below, leaves it on disk and a later pass refuses to replay.
		attempts := 1
		if item.TaskInflight != nil {
			attempts = item.TaskInflight.Attempts + 1
		}
		item.TaskInflight = &TaskInflight{RunDir: runDir, Started: now, Attempts: attempts}
		if err := t.Store.saveActive(item); err != nil {
			item.TaskInflight = nil
			if errors.Is(err, errConsentChanged) {
				return nil
			}
			return fmt.Errorf("could not record the task before starting it: %w", err)
		}
		outcome, err = t.Runner.Run(ctx, *item, runDir, found.evidence)
	default:
		return errors.New("standing: an unknown kind of action: " + string(item.Does.Kind))
	}
	if err != nil {
		if item.Does.Kind == ActionTask {
			// A TASK FAILED WITH ITS IN-FLIGHT MARKER STILL UP. It may already
			// have made external edits, and nothing here can make a second run
			// idempotent, so it is left FOR THE PERSON: the marker stays and
			// [Ticker.one] refuses to run the task again while it is there. No
			// exactly-once promise is made about what the task did.
			item.NeedsPerson = oneLine("a task could not be finished and is waiting for you: " + err.Error())
			if saveErr := t.Store.saveActive(item); saveErr != nil && !errors.Is(saveErr, errConsentChanged) {
				return errors.Join(err, saveErr)
			}
		}
		return err
	}
	if item.Does.Kind == ActionTask {
		item.TaskInflight = nil // the attempt reached an outcome
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
	}
	ledgerErr := t.Store.Append(Entry{At: now, ItemID: item.ID, Kind: string(item.Does.Kind), USD: outcome.USD, Run: runDir})
	logErr := t.Store.Log(item.ID, firingLine(found, outcome))
	// THE SAVE IS GUARDED, and a refusal is the person's act winning rather than
	// this pass failing: the delivery already happened and cannot be undone, but
	// a paused or retired item must not be written over.
	saveErr := t.Store.saveActive(item)
	if saveErr != nil && !errors.Is(saveErr, errConsentChanged) {
		return errors.Join(ledgerErr, logErr, saveErr)
	}
	// THE DELIVERY SUCCEEDED. A ledger or log failure is ACCOUNTING, not the
	// check, and must not be relabelled as one: [Tick] counts it and says so
	// without overwriting the outcome the firing just recorded.
	if ledgerErr != nil || logErr != nil {
		return &accountingError{err: errors.Join(ledgerErr, logErr)}
	}
	return nil
}

// accountingError says a firing succeeded but its ledger or log line could not
// be written. [Tick] counts it and logs it, but does NOT replace the item's
// outcome with a check failure the way [Ticker.noteFailure] would — the delivery
// happened, and saying "could not check" about it would be a lie.
type accountingError struct{ err error }

func (e *accountingError) Error() string { return e.err.Error() }
func (e *accountingError) Unwrap() error { return e.err }

// deliver carries out one pending line, by identity when the Runner can dedup
// ([Deliverer]) and through the legacy [Runner.Say] otherwise.
func (t *Ticker) deliver(ctx context.Context, item Item, pending Pending) (Outcome, error) {
	if deliverer, ok := t.Runner.(Deliverer); ok {
		return deliverer.Deliver(ctx, item, pending)
	}
	return t.Runner.Say(ctx, item, pending.Text)
}

// pendingAttempts is how many passes have failed to settle one delivery intent.
func pendingAttempts(item *Item, id string) int {
	for i := range item.Pending {
		if item.Pending[i].ID == id {
			return item.Pending[i].Attempts
		}
	}
	return 0
}

// settle resolves durable delivery intents INDEPENDENTLY of any fresh look, at
// the top of a pass and before the rails. A pending line is authorized work: it
// is delivered by identity even if the item's file stopped changing, its
// NextDue moved on, or its daily rail is spent. The SAME identity is reused, so
// a retry mints no new intent, runs nothing new and adds no second ledger
// charge for the same authorization.
//
// An item that is no longer active (paused, retired or expired) cannot be
// settled through its own walk — [Ticker.Tick] skips it — so its unresolved
// line is surfaced as [Item.NeedsPerson] instead of being silently forgotten.
func (t *Ticker) settle(ctx context.Context, pass *Pass, items []Item) {
	if t.Runner == nil {
		return
	}
	for i := range items {
		item := &items[i]
		if len(item.Pending) == 0 {
			continue
		}
		if item.Status != StatusActive {
			if err := t.Store.markPendingOrphan(item.ID, "an authorized line is still waiting and this item is no longer running"); err != nil {
				pass.Errors++
				pass.Notes = append(pass.Notes, "could not mark a waiting line: "+oneLine(err.Error()))
			} else {
				pass.Notes = append(pass.Notes, shorten(item.Words, 60)+": an authorized line is still waiting")
			}
			continue
		}
		t.settleItem(ctx, pass, item)
	}
}

// settleItem carries out the pending lines of one active item, completing the
// firing bookkeeping the original attempt never reached, and settling each under
// its own stable identity.
func (t *Ticker) settleItem(ctx context.Context, pass *Pass, item *Item) {
	if _, err := t.Store.Get(item.ID); err != nil {
		return // gone or unreadable; nothing can be settled safely
	}
	for len(item.Pending) > 0 {
		pending := item.Pending[0]
		outcome, err := t.deliver(ctx, *item, pending)
		if err != nil {
			item.bumpPending(pending.ID)
			if pendingAttempts(item, pending.ID) >= PendingGiveUp {
				item.NeedsPerson = oneLine("a line could not be delivered and is still waiting")
			}
			if saveErr := t.Store.saveActive(item); saveErr != nil && !errors.Is(saveErr, errConsentChanged) {
				pass.Errors++
				pass.Notes = append(pass.Notes, shorten(item.Words, 60)+": a waiting line could not be recorded: "+oneLine(saveErr.Error()))
				return
			}
			pass.Errors++
			pass.Notes = append(pass.Notes, shorten(item.Words, 60)+": a waiting line could not be delivered")
			return
		}
		// SETTLED: this is the tail of a firing that never got to record itself.
		item.dropPending(pending.ID)
		item.Runs++
		item.LastFired = t.clock()
		item.LastChecked = item.LastFired
		item.LastCheckLine = oneLine("delivered a line that was waiting")
		item.LastOutcome = outcome.Kind
		item.SpentUSD += outcome.USD
		item.Previous = remember(item.Previous, pending.Text, outcome)
		if item.When.Kind == WhenAt {
			item.Status = StatusRetired
			item.RetiredWhy = "fired"
		}
		ledgerErr := t.Store.Append(Entry{At: item.LastFired, ItemID: item.ID, Kind: string(pending.Kind), USD: outcome.USD})
		logErr := t.Store.Log(item.ID, "delivered a line that was waiting")
		if saveErr := t.Store.saveActive(item); saveErr != nil && !errors.Is(saveErr, errConsentChanged) {
			pass.Errors++
			pass.Notes = append(pass.Notes, shorten(item.Words, 60)+": a delivered line could not be recorded: "+oneLine(saveErr.Error()))
			return
		}
		pass.Fired++
		if pending.Kind == ActionSay {
			pass.Said++
		}
		if ledgerErr != nil || logErr != nil {
			pass.Errors++
			pass.Notes = append(pass.Notes, "the accounting for a waiting line could not be written: "+oneLine(errors.Join(ledgerErr, logErr).Error()))
		}
	}
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
func (t *Ticker) noteFailure(item Item, failure error) {
	now := t.clock()
	item.LastChecked = now
	item.LastCheckLine = "could not check: " + shorten(oneLine(failure.Error()), 200)
	if err := t.Store.saveActive(&item); err != nil && !errors.Is(err, errConsentChanged) {
		_ = t.Store.Log(item.ID, "could not write the item's failure: "+oneLine(err.Error()))
	}
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

// The bounds on one WhenFile reading. THEY ARE THE WHOLE POINT: an unbounded
// scan of a home directory is a hang, and a fingerprint that varies with how
// much of the world it happened to read is worse than none. Past any of these a
// reading is TRUNCATED, which is reported and never passed off as "unchanged".
const (
	fingerprintMaxFiles = 4096
	fingerprintMaxBytes = 8 << 20
	fingerprintPerFile  = 256 << 10
	fingerprintBudget   = 2 * time.Second
)

// fingerprint is a WhenFile's reading of the world: the names, sizes and bounded
// CONTENTS of everything the glob matches, hashed. The listing beside it is what
// a firing is told, since a hash is evidence of nothing to a model or a person.
//
// CONTENT, NOT MTIME. A bare touch changes no content and is not a change; an
// edit that keeps a file's size and its mtime identical changes the content and
// IS one. A file that was renamed changes its name and is one. A symlink is read
// as its target, never followed, so a link whose target changed is a change and
// a link cannot walk the scan out of the workspace.
//
// IT ANSWERS truncated AND SAYS WHY NOWHERE ELSE MATTERS: a name with no file
// (vanished between the glob and the stat), a file it could not open, a file
// larger than the per-file cap, more files than the file cap, more bytes than
// the byte cap, or a scan that ran out of its time budget. A truncated reading
// hashes to a different digest than a complete one, so arming ([Store.Arm])
// refuses it and a watch never certifies "unchanged" on a partial look.
func fingerprint(workspace, glob string) (string, string, bool, error) {
	pattern := glob
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(workspace, pattern)
	}
	matches, truncated, err := boundedGlob(pattern, fingerprintMaxFiles)
	if err != nil {
		return "", "", false, fmt.Errorf("standing: cannot read the pattern %q: %w", glob, err)
	}
	sum := sha256.New()
	listing := &strings.Builder{}
	deadline := time.Now().Add(fingerprintBudget)
	budget := int64(fingerprintMaxBytes)
	for _, match := range matches {
		if time.Now().After(deadline) {
			truncated = true
			break
		}
		info, err := os.Lstat(match)
		if err != nil {
			// It was listed and then was not there: an UNSTABLE listing is
			// unknown, not "unchanged". Marking it truncated stops the reading
			// from certifying anything about a moment it could not trust.
			truncated = true
			continue
		}
		name := match
		if relative, err := filepath.Rel(workspace, match); err == nil {
			name = relative
		}
		kind := "file"
		size := info.Size()
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			kind = "symlink"
			if target, err := os.Readlink(match); err == nil {
				size = int64(len(target))
			} else {
				truncated = true
			}
		case info.IsDir():
			kind = "dir"
		}
		fmt.Fprintf(sum, "%s\x00%s\x00%d\x00", name, kind, size)
		switch kind {
		case "file":
			if budget <= 0 {
				truncated = true
				break
			}
			limit := int64(fingerprintPerFile)
			if budget < limit {
				limit = budget
			}
			read, fileTruncated, readErr := hashFile(sum, match, limit)
			budget -= read
			if readErr != nil || fileTruncated {
				truncated = true
			}
			// The bytes we read disagree with the size we stat'd: the file moved
			// under the scan. That instability is unknown, never "unchanged".
			if readErr == nil && !fileTruncated && info.Size() <= limit && read != info.Size() {
				truncated = true
			}
		case "symlink":
			if target, err := os.Readlink(match); err == nil {
				if budget > 0 {
					sum.Write([]byte(target))
					budget -= int64(len(target))
				} else {
					truncated = true
				}
			}
		}
		if listing.Len() < ProbeClip {
			fmt.Fprintf(listing, "%s  %d bytes\n", name, size)
		}
	}
	hexdigest := hex.EncodeToString(sum.Sum(nil))
	if truncated {
		hexdigest = "t" + hexdigest
	}
	return hexdigest, listing.String(), truncated, nil
}

// globMaxEntries bounds how many directory entries one listing step may read,
// so a directory with a million names cannot make the scan read them all just
// to find the four the pattern matches.
const globMaxEntries = 100000

// boundedGlob expands a glob pattern while LISTING BOUNDED and COLLECTING
// BOUNDED: filepath.Glob materialises and sorts every match before any cap is
// applied, which is the very allocation the caps exist to prevent. It walks the
// pattern one path segment at a time, listing each directory once, so a
// metacharacter in ANY segment — "services/*/go.mod" as readily as "*.sql" —
// is expanded under the same entry bound. It stops collecting at max and
// reports truncation, which the caller reads as "unknown", never as "nothing
// matched".
//
// IT NEVER HANDS A PATTERN TO filepath.Glob. An earlier version fell back to
// Glob when the pattern's directory contained a metacharacter, which is exactly
// the unbounded allocation the bound exists to prevent; a pattern this walk
// cannot expand answers truncation rather than a lie about an empty set. A
// literal path segment that does not exist simply contributes nothing, which is
// the same "no match" an old reader got.
func boundedGlob(pattern string, max int) (matches []string, truncated bool, err error) {
	volume := filepath.VolumeName(pattern)
	rest := strings.TrimPrefix(pattern, volume)
	root := volume
	if strings.HasPrefix(rest, string(filepath.Separator)) {
		root += string(filepath.Separator)
		rest = strings.TrimPrefix(rest, string(filepath.Separator))
	}
	if root == "" {
		root = "."
	}
	bases := []string{root}
	scanned := 0
	for _, segment := range strings.Split(rest, string(filepath.Separator)) {
		if segment == "" || segment == "." {
			continue
		}
		var next []string
		meta := strings.ContainsAny(segment, "*?[")
		for _, base := range bases {
			if !meta {
				candidate := filepath.Join(base, segment)
				if _, statErr := os.Lstat(candidate); statErr == nil {
					next = append(next, candidate)
				}
				continue
			}
			entries, readErr := os.ReadDir(base)
			if readErr != nil {
				// A base that is gone or is not a directory contributes nothing,
				// the way filepath.Glob's silent skip did; any other failure is real.
				if info, statErr := os.Stat(base); statErr != nil || !info.IsDir() {
					continue
				}
				return nil, false, readErr
			}
			for _, entry := range entries {
				scanned++
				if scanned > globMaxEntries {
					truncated = true
					break
				}
				ok, matchErr := filepath.Match(segment, entry.Name())
				if matchErr != nil {
					return nil, false, matchErr
				}
				if ok {
					next = append(next, filepath.Join(base, entry.Name()))
				}
			}
			if len(next) >= max {
				truncated = true
				break
			}
		}
		if len(next) > max {
			next = next[:max]
			truncated = true
		}
		bases = next
		if len(bases) == 0 || (truncated && scanned > globMaxEntries) {
			break
		}
	}
	sort.Strings(bases)
	if len(bases) > max {
		return bases[:max], true, nil
	}
	return bases, truncated, nil
}

// hashFile writes up to limit bytes of path into dst and answers how many it
// wrote and whether the file was longer than the limit. It reads in fixed chunks
// rather than slurping the file, so a pathologically large match cannot allocate
// its size just to be hashed.
func hashFile(dst hash.Hash, path string, limit int64) (read int64, truncated bool, err error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, false, err
	}
	defer file.Close()
	buf := make([]byte, 32*1024)
	for read < limit {
		want := int64(len(buf))
		if remaining := limit - read; remaining < want {
			want = remaining
		}
		n, rerr := file.Read(buf[:want])
		if n > 0 {
			dst.Write(buf[:n])
			read += int64(n)
		}
		if rerr == io.EOF {
			return read, false, nil
		}
		if rerr != nil {
			return read, false, rerr
		}
	}
	// We stopped on the limit, not the end. One more byte settles which.
	var probe [1]byte
	if n, _ := file.Read(probe[:]); n > 0 {
		truncated = true
	}
	return read, truncated, nil
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
