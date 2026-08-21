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
			t.noteFailure(item, err)
		}
	}
	if err := t.Store.appendWake(pass); err != nil {
		pass.Errors++
		pass.Notes = append(pass.Notes, "could not write the wake log: "+oneLine(err.Error()))
	}
	return pass, nil
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
func (t *Ticker) one(ctx context.Context, pass *Pass, item Item) error {
	now := t.clock()

	// RAIL ONE: it ran out of time.
	if deadline, has := expiryOf(item); has && !now.Before(deadline) {
		item.Status = StatusRetired
		item.RetiredWhy = "expired"
		item.LastChecked = now
		item.LastCheckLine = "its time ran out"
		pass.Skipped++
		pass.Notes = append(pass.Notes, shorten(item.Words, 60)+": its time ran out")
		_ = t.Store.Log(item.ID, "its time ran out — no longer watching")
		return t.Store.Save(item)
	}

	// RAIL TWO: it has already run today as often as the person allowed.
	mine, err := t.Store.Today(item.ID, now)
	if err != nil {
		return err
	}
	if item.Rails.MaxPerDay > 0 && mine.Fired >= item.Rails.MaxPerDay {
		pass.Skipped++
		return t.quiet(item, now, "it has already run today as often as you allowed")
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
			return t.quiet(item, now, "today's spending limit is reached")
		}
	}

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
		return t.quiet(item, now, found.line)
	}
	pass.Checked++
	return t.fire(ctx, pass, item, now, found)
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
		digest, listing, err := fingerprint(item.Workspace, item.When.Glob)
		if err != nil {
			return sighting{}, err
		}
		first := item.Fingerprint == ""
		changed := !first && digest != item.Fingerprint
		item.Fingerprint = digest
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
func (t *Ticker) quiet(item Item, now time.Time, line string) error {
	item.LastChecked = now
	item.LastCheckLine = oneLine(line)
	return t.Store.Save(item)
}

// fire is the firing and everything it leaves behind.
func (t *Ticker) fire(ctx context.Context, pass *Pass, item Item, now time.Time, found sighting) error {
	if t.Runner == nil {
		return errors.New("there is nothing in this build to run it with")
	}
	var (
		outcome Outcome
		runDir  string
		err     error
	)
	switch item.Does.Kind {
	case ActionSay:
		outcome, err = t.Runner.Say(ctx, item, strings.ReplaceAll(item.Does.Say, "{{evidence}}", found.evidence))
	case ActionTask:
		runDir, err = t.Store.newRunDir(item.ID)
		if err != nil {
			return err
		}
		outcome, err = t.Runner.Run(ctx, item, runDir, found.evidence)
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
	if outcome.Kind == "needs-you" && item.NeedsPerson == "" {
		item.NeedsPerson = oneLine(outcome.Text)
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
	return errors.Join(ledgerErr, logErr, t.Store.Save(item))
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
	_ = t.Store.Save(item)
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
func fingerprint(workspace, glob string) (string, string, error) {
	pattern := glob
	if !filepath.IsAbs(pattern) {
		pattern = filepath.Join(workspace, pattern)
	}
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", "", fmt.Errorf("standing: cannot read the pattern %q: %w", glob, err)
	}
	sort.Strings(matches)
	digest := sha256.New()
	listing := &strings.Builder{}
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
		if listing.Len() < ProbeClip {
			fmt.Fprintf(listing, "%s  %d bytes  %s\n", name, info.Size(), info.ModTime().Format(time.RFC3339))
		}
	}
	return hex.EncodeToString(digest.Sum(nil)), listing.String(), nil
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
		" errors=" + strconv.Itoa(pass.Errors) + "\n"
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
