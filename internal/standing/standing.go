// Package standing is the ambient side of v3: the things a conversation leaves
// behind that keep working after the window is closed — a reminder, a watch, a
// rule, an overnight job — and the small, honest machinery that wakes them.
//
// THIS FILE IS THE CONTRACT. It was written by hand before any lane started, and
// every lane codes against it: the core lane fills in the store and the ticker,
// the session lane supplies the sentinel and the runner and arms the belt tool,
// the home lane draws items, the errand lane hosts the exchange at home. A field
// or a method added here mid-build is a change every lane has to hear about, so
// the shape is deliberately small and deliberately complete.
//
// ── THE LAWS ──
//
//   - ONE OBJECT, MANY SHAPES. A reminder, a routine, a watch, a rule, an
//     overnight job and a self-proposed follow-up are all an [Item]: the person's
//     words, what wakes it ([When]), what it does ([Action]), and what bounds it
//     ([Rails]). The product's variety is in two fields, not in six mechanisms.
//
//   - THE MECHANISMS ARE CLOSED; THE CONDITION IS OPEN. There are five ways an
//     item can be woken ([WhenKind]) and that list does not grow per feature. But
//     [WhenProbe] is general: the model writes the probe — any shell command, or
//     any tool on the belt with any arguments, including a tool a connected
//     account brought — and a cheap yes/no judgment ([Sentinel]) reads its output
//     against the person's words. "Is CI red", "did Priya reply", "is the cert
//     under 14 days" are all probes; none of them is a kind.
//
//   - FILES, NOT A DATABASE. One JSON document per item, written temp+rename under
//     a per-item flock; an append-only daily ledger for the rails; one folder per
//     run. docs/AMBIENT.md Part 4 has the numbers. Every path is answered by this
//     package and nowhere else, so an index could be added behind it later
//     without a caller changing.
//
//   - NOTHING STANDS UNTIL THE PERSON SAYS YES. [Store.Create] is only ever
//     called after a ratification card was answered yes (a StandingProposal in
//     internal/session). There is no path that arms an item silently.
//
//   - UNATTENDED MEANS WHAT WAS ALREADY ALLOWED. A firing runs under the person's
//     banked approval rules with nobody to ask; anything that would have asked
//     stops the run as "needs your look". This paragraph used to finish "there
//     are no probation counters: the rules are the tenure", and the second half
//     of that is no longer true: [Item.CleanRuns] counts clean firings in a row,
//     and the rope column reads it ([RopeWord]). The first half still is, and it
//     is the part that mattered — the counter changes WHAT A PERSON IS TOLD
//     about an item, never what a firing is allowed to do. What is allowed is
//     the banked rules and only ever the banked rules; nothing here widens with
//     a count.
//
//   - QUIET IS THE DESIGN. A check that found nothing rewrites LastChecked and
//     LastCheckLine in the item and writes NO line anywhere else. A run that
//     delivered nothing is reaped after [RunKeep].
//
//   - STATUS IS DERIVED, NEVER ASSERTED. Whether the OS timer is installed is
//     whether its definition file still matches byte for byte what this build
//     would write; last wake and next due come from the wake log and the fixed
//     cadence. Nothing shells out to ask.
//
// ── WHERE THE BODIES ARE ──
//
// This file is the shape. The work is in store.go (the documents, the item log,
// the locks), ledger.go (the daily lines the rails are summed from), inbox.go
// (news for a window that is not open), every.go (the rhythm), tick.go (the
// pass) and watch.go (the operating system's timer).
//
// Every name a caller holds is declared here, with ONE exception the contract
// could not carry: [Watch] is an interface with no way to make one, so watch.go
// adds NewWatch and the WatchOptions it takes. Nothing else outside this file
// is reachable.
package standing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Schema is the document version every [Item] carries. Bump it when a field
// changes meaning; a reader that meets a newer schema than it knows skips the
// document and says so in the pass.
const Schema = 1

// Interval is how often a pass runs, whether a window runs it or the OS timer
// does. It is the cadence the ratification card quotes for "checked every …".
const Interval = 5 * time.Minute

// TickWindow bounds ONE pass, wherever the pass is run from. It is generous for
// a pass that found nothing (a stat per item) and short enough that a wedged
// probe cannot hold the store's lock against every other window on the machine.
//
// IT IS ONE NUMBER BECAUSE IT IS ONE QUESTION. A window's own goroutine and
// `codeaf tick` each used to name their own 120 seconds, and [Store.Running]
// needs a third reading of the same figure — how long a pass may last is how
// long a marker may be believed. Three copies of a ceiling is three chances for
// one of them to move.
const TickWindow = 120 * time.Second

// RunKeep is how long a run that delivered nothing is kept before the sweep
// reaps it. A run that delivered something — a note, a task landing, a
// needs-your-look — is kept like any session.
const RunKeep = 7 * 24 * time.Hour

// Previous is how many of an item's last sentinel judgments ride in the next
// judgment's prompt. It is what stops a declined firing being proposed again
// every wake forever: the sentinel sees what it said last time and what came
// of it.
const Previous = 5

// ── the item ────────────────────────────────────────────────────────────────

// Status is where an item is in its life. There is no "proposed": a proposal is
// a card in a conversation, and only a yes makes an item.
type Status string

const (
	StatusActive  Status = "active"
	StatusPaused  Status = "paused"
	StatusRetired Status = "retired"
)

// WhenKind is one of the six shapes of an item — five ways to be woken, and
// one that never wakes ([WhenHold]). The list is closed.
type WhenKind string

const (
	// WhenAt fires once, at a moment, then retires. A reminder is this.
	WhenAt WhenKind = "at"
	// WhenEvery fires on a rhythm — a cron line or an interval — forever.
	WhenEvery WhenKind = "every"
	// WhenFile fires when files matching a glob change (a fingerprint of
	// names, sizes and mtimes, as v1's file watch did).
	WhenFile WhenKind = "file"
	// WhenIdle fires when the machine has been quiet — no window busy, no
	// task running anywhere — for [When.IdleFor]. "Learn this later" is this.
	WhenIdle WhenKind = "idle"
	// WhenProbe fires when a probe's output, judged by the sentinel against
	// the person's words, says yes. Anything the belt can do is a probe.
	WhenProbe WhenKind = "probe"
	// WhenHold never wakes. A rule, "always use tabs here", "never touch the
	// public API", has no moment, no rhythm and no probe: its whole work is
	// done at birth, riding into the world of every conversation and task it
	// reaches (docs/STANDING-ORDERS.md, the birth seam). The pass walks past
	// it; it cannot fire, so it cannot spend, so it alone needs no rails and
	// no action.
	WhenHold WhenKind = "hold"
)

// CardKind is what a proposal card calls this item. It is derived from the
// when, because that is the fact a person can check: a moment is a reminder,
// a rhythm is a repeating check, a condition is a watch, and a hold is a rule.
type CardKind string

const (
	// CardReminder is one moment. Doing it now is not a smaller version of it.
	CardReminder CardKind = "reminder"
	// CardCheck repeats on a cadence.
	CardCheck CardKind = "check"
	// CardWatch waits on a condition or an event.
	CardWatch CardKind = "watch"
	// CardRule is kept, and never wakes.
	CardRule CardKind = "rule"
)

// CardKindOf reports which card this item is. A when this build does not know
// is read as a watch: it is something to look for, and the card says so.
func (it Item) CardKindOf() CardKind {
	switch it.When.Kind {
	case WhenAt:
		return CardReminder
	case WhenEvery:
		return CardCheck
	case WhenHold:
		return CardRule
	default:
		return CardWatch
	}
}

// shortWordsRunes is how much of a cadence a button may carry. Past it the
// label eats the row and the other answers disappear.
const shortWordsRunes = 32

// ShortWords is the cadence a button can carry. A long sentence is cut at a
// word, because a label that fills the row leaves no room for the other answers.
func (w When) ShortWords() string {
	words := strings.TrimSpace(w.Words)
	if words == "" || utf8.RuneCountInString(words) <= shortWordsRunes {
		return words
	}
	runes := []rune(words)
	cut := shortWordsRunes
	for cut > 0 && runes[cut-1] != ' ' {
		cut--
	}
	if cut == 0 {
		cut = shortWordsRunes
	}
	return strings.TrimSpace(string(runes[:cut])) + "..."
}

// When is what wakes an item. Exactly the fields its Kind names are read; the
// rest are left empty and never consulted. Words are always kept: they are the
// person's own cadence or condition, and every surface speaks them back rather
// than the spec.
type When struct {
	Kind  WhenKind `json:"kind"`
	Words string   `json:"words,omitempty"`
	// At is the one moment of a WhenAt.
	At time.Time `json:"at,omitempty"`
	// Every is a WhenEvery's rhythm: a five-field cron line ("0 9 * * 1") or
	// a Go duration ("20m", "2h"). [ParseEvery] is the one reader of it.
	Every string `json:"every,omitempty"`
	// Glob is a WhenFile's pattern, relative to the item's workspace.
	Glob string `json:"glob,omitempty"`
	// IdleFor is how quiet the machine must have been for a WhenIdle.
	IdleFor time.Duration `json:"idleFor,omitempty"`
	// Probe is a WhenProbe's look at the world, taken every ProbeEvery.
	Probe      Probe         `json:"probe,omitempty"`
	ProbeEvery time.Duration `json:"probeEvery,omitempty"`
	// Hint tells the sentinel what a yes looks like, in the model's words at
	// proposal time: "yes when any run on main shows conclusion=failure".
	Hint string `json:"hint,omitempty"`
}

// Probe is one look at the world: a shell command in the workspace, OR a belt
// tool with arguments. Exactly one is set. Output is what the sentinel reads,
// clipped to [ProbeClip] bytes from the tail.
type Probe struct {
	Command string          `json:"command,omitempty"`
	Tool    string          `json:"tool,omitempty"`
	Args    json.RawMessage `json:"args,omitempty"`
}

// ProbeClip bounds what one probe may put in front of the sentinel.
const ProbeClip = 8 * 1024

// ActionKind is what a firing does.
type ActionKind string

const (
	// ActionSay delivers one line to the person — into the conversation that
	// asked, and onto home. A reminder, "CI is red", "Priya replied".
	ActionSay ActionKind = "say"
	// ActionTask runs work: the brief is carried out in the item's workspace by
	// a fresh unattended session of its own, bounded by the item's rails, and
	// what it came to is written down with a cost row.
	//
	// IT IS A SESSION AND NOT A WORKTREE, which is the runner's own statement of
	// itself (internal/session's standing_run.go) and worth saying here because
	// this constant used to promise otherwise. A firing is one turn in the
	// project the person pointed it at — it has no branch, no landing to accept,
	// and no hands to give work to.
	ActionTask ActionKind = "task"
)

// Action is what a firing does. Say is read for ActionSay; Brief, Acceptance,
// Model, Effort and MaxSteps for ActionTask. Either kind may template the
// probe's evidence into its text with {{evidence}}.
type Action struct {
	Kind       ActionKind `json:"kind"`
	Say        string     `json:"say,omitempty"`
	Brief      string     `json:"brief,omitempty"`
	Acceptance string     `json:"acceptance,omitempty"`
	Model      string     `json:"model,omitempty"`

	// Effort is the rung on the effort ladder this item's firings and its checks
	// ask for (internal/effort), and empty on almost every item.
	//
	// EMPTY IS NOT THE CHEAPEST RUNG, IT IS "NOBODY SAID". An item that says
	// nothing runs at the standing role's own floor — low — because a firing is
	// unattended and repeats forever, and an install dialled deep must not turn
	// every check on the machine into a deep pass. This field is how the one
	// item that genuinely needs thinking says so once and gets it every time.
	Effort string `json:"effort,omitempty"`

	MaxSteps int `json:"maxSteps,omitempty"`
}

// DefaultPerRunUSD is what ONE FIRING of a standing item may spend when nobody
// named a figure — the probe, the sentinel's judgment and the work itself.
//
// IT IS THE ONE PLACE THIS NUMBER LIVES. It was written out three times once —
// here in the store that creates a charter, in the proposal that quotes a price
// to the person, and in the belt tool's own schema — and three copies of one
// fact is the drift this codebase has a law against. Every reader resolves it
// from this constant.
//
// Fifteen cents was the old figure and it was a rail rather than a backstop: a
// cheap look plus a small model's answer and nothing else, so the first
// standing order anybody wrote that did real work stopped halfway through its
// first firing. Five dollars is a whole piece of work on a good model, which is
// what a person who says "keep an eye on this" is actually asking for. The
// protection that matters is still the machine-wide daily rail plus the
// max-per-day count, not this.
const DefaultPerRunUSD = 5.0

// Rails bound an item. MaxPerDay is mandatory by construction: [Store.Create]
// refuses an item that may fire zero times a day, which is an item that would
// never fire at all. They stay quiet on the proposal card unless the person
// named money themselves, because the ordinary promise is the machine-wide
// daily allowance.
type Rails struct {
	// PerRunUSD is the most one firing may spend, probe and sentinel included.
	//
	// ZERO IS NO LIMIT, and it always was at the place that enforces it —
	// internal/session's standing_run.go has only ever stopped a firing when
	// `PerRunUSD > 0` — so a person who wants a standing order bounded by
	// nothing but the daily rail writes 0 here and gets exactly that.
	// [Item.Validate] used to refuse that number, which made the enforcement
	// site's own contract unreachable.
	PerRunUSD float64 `json:"perRunUsd"`
	// MaxPerDay is how many times it may fire in one local day.
	MaxPerDay int `json:"maxPerDay"`
	// Expires retires the item at that moment. Zero is never. A WhenAt item
	// expires a day after its moment whatever this says.
	Expires time.Time `json:"expires,omitempty"`
}

// Origin is where an item was asked for. It is provenance and it is the door
// home opens: "why did I get this?" opens the conversation that made it.
type Origin struct {
	// SessionID and Transcript name the conversation the card was answered in.
	SessionID  string `json:"sessionId,omitempty"`
	Transcript string `json:"transcript,omitempty"`
	// Exchange is set instead when the item was made from home's own box: the
	// short exchange that produced it is kept under the item's folder
	// ([Store.ExchangeDir]) and not as a project session. Promoting it to a
	// conversation moves the folder and fills SessionID.
	Exchange string `json:"exchange,omitempty"`
	// TaskID is set when a finishing task proposed the item itself.
	TaskID int `json:"taskId,omitempty"`
	// TurnIDs are the turns of the origin session that were about making or
	// changing this item. Home uses them to tell a conversation that was only
	// ever about this item from one that merely contains it.
	TurnIDs []string `json:"turnIds,omitempty"`
}

// ── altitude: how far an item reaches ───────────────────────────────────────

// Altitude is an item's reach — which work it governs and which surfaces list
// it. It is decided on the ratification card and it never drifts afterward;
// widening it is a new card. docs/STANDING-ORDERS.md is the design.
type Altitude string

const (
	// AltitudeConversation governs one conversation and dies with it. It
	// requires Origin.SessionID: a reach with no place to reach is an error.
	AltitudeConversation Altitude = "conversation"
	// AltitudeProject governs every conversation and every task in one
	// workspace. IT IS WHAT THE ZERO VALUE MEANS: every item made before
	// altitudes were spelled was workspace-scoped, so an empty altitude reads
	// as this and nothing migrates.
	AltitudeProject Altitude = "project"
	// AltitudeMachine governs everything the person does on this machine. It
	// keeps the existing convention that a machine-wide item's workspace is
	// the person's home.
	AltitudeMachine Altitude = "machine"
)

// Brief is the working half of an item: a short title for rows too narrow for
// a sentence, and the compiled prompt the machinery follows. The person's
// Words are NEVER rewritten — they are the reason the item exists, and every
// surface that opens the item shows both halves, words first. An empty Brief
// reads as the Words themselves. Editing a brief down (narrower, gentler) is
// free; editing it up (more reach, more action) is a new ratification card —
// the session lane enforces that law, not this package.
type Brief struct {
	Title  string `json:"title,omitempty"`
	Prompt string `json:"prompt,omitempty"`
}

// Exception is one place an item deliberately does not reach: a workspace, or
// a single conversation. Exactly one field is set. Exceptions are made by the
// person — from the place ("not here"), or from the item's own record pointing
// at a place — and never by the machinery. Both gestures write the same fact.
type Exception struct {
	Workspace string    `json:"workspace,omitempty"`
	SessionID string    `json:"sessionId,omitempty"`
	At        time.Time `json:"at"`
}

// Item is one standing thing. The top half is what the person agreed to and
// never changes without another card; the bottom half is the item's own
// present, rewritten on every check.
type Item struct {
	Schema int    `json:"schema"`
	ID     string `json:"id"`
	// Words are the person's verbatim sentence. Permanent anchor; every
	// surface leads with it.
	Words string `json:"words"`
	// Workspace is the REAL project root the item belongs to — the resolved git
	// root, an owned work/ directory, or the person's home for a machine-wide
	// item. It is what home groups by and where a task firing runs.
	Workspace string `json:"workspace"`
	Origin    Origin `json:"origin"`
	When      When   `json:"when"`
	Does      Action `json:"does"`
	Rails     Rails  `json:"rails"`
	// Altitude is the item's reach (see [Altitude]); empty reads as project.
	Altitude Altitude `json:"altitude,omitempty"`
	// Brief is the working title and compiled prompt; empty reads as Words.
	Brief Brief `json:"brief,omitempty"`
	// Grant is one sentence of what acting on this item may do without asking,
	// quoted on the card that ratified it. Empty means say-only, which is what
	// every item made before grants were spelled could do.
	Grant string `json:"grant,omitempty"`
	// Exceptions are the places this item deliberately does not reach.
	Exceptions []Exception `json:"exceptions,omitempty"`

	Status  Status    `json:"status"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	// RetiredWhy says why a retired item retired: "fired", "expired",
	// "stopped by you", or the sentence the last failure left.
	RetiredWhy string `json:"retiredWhy,omitempty"`

	// The quiet half. LastChecked and LastCheckLine are what let a watch that
	// checked faithfully for thirty mornings and found nothing read differently
	// from one that never ran.
	LastChecked   time.Time `json:"lastChecked,omitempty"`
	LastCheckLine string    `json:"lastCheckLine,omitempty"`
	NextDue       time.Time `json:"nextDue,omitempty"`
	// Fingerprint is a WhenFile's last reading.
	Fingerprint string `json:"fingerprint,omitempty"`
	// Previous are the last [Previous] sentinel lines, newest first, each with
	// what came of it.
	Previous []string `json:"previous,omitempty"`

	// The ledger half, kept on the item for the card; the daily ledger is the
	// truth for the rails.
	Runs        int       `json:"runs"`
	LastFired   time.Time `json:"lastFired,omitempty"`
	LastOutcome string    `json:"lastOutcome,omitempty"`
	LastRun     string    `json:"lastRun,omitempty"`
	SpentUSD    float64   `json:"spentUsd"`
	// NeedsPerson is set while the latest run is stopped waiting on the person,
	// with the one line it is stopped on. Home sorts on it.
	//
	// IT CARRIES TWO DIFFERENT THINGS, and a reader has to know which. One is a
	// QUESTION the firing put to the person, in its own words. The other is the
	// line written when a call was refused for want of somebody to allow it,
	// which opens with [NeedsPermissionLead].
	//
	// The pass writes this field whole on the next firing. The PERSON can put
	// down the permission line by changing the item ([Item.ClearNeedsPerson]),
	// and can never lose a question that way. With only the pass, an item that
	// could not fire again — one that had spent its allowance for the day — kept
	// a row on home saying it needed somebody for as long as that stayed true,
	// with no act of theirs able to put it down.
	NeedsPerson string `json:"needsPerson,omitempty"`
	// CleanRuns is HOW MANY FIRINGS IN A ROW CAME BACK CLEAN — fired with
	// nothing waiting for the person and no failure. It is the count the rope
	// column's middle rung is drawn from ([RopeWord]), and a firing that stops
	// on a question or fails puts it back to nothing.
	//
	// IT IS RECORDED AND NOT DERIVED, and that is a deliberate loss of
	// elegance. Everything else this struct keeps about a firing is the LATEST
	// one — LastOutcome is overwritten every time — so a streak simply is not in
	// the record. The one field that looks like it might do is [Item.Previous],
	// which holds the last few sentinel judgments with `" → " + outcome.Kind`
	// glued on the end; reading a streak out of that would mean splitting
	// model-authored prose on an arrow, and it would silently tie a product
	// threshold to [Previous], whose size exists to bound a PROMPT. Somebody
	// trimming that prompt by two lines would quietly make trust unreachable.
	// So the count is kept at the one site that records a firing
	// ([Ticker.fire]), which is the same place [Item.Runs] is kept.
	//
	// AN ITEM WRITTEN BEFORE THIS FIELD EXISTED DECODES AS ZERO and starts
	// earning trust again. That is the conservative direction and the only
	// honest one: nothing on disk says those firings were clean, and a column
	// that assumed they were would be granting rope nobody measured.
	CleanRuns int `json:"cleanRuns,omitempty"`
}

// Validate is what [Store.Create] and [Store.Save] refuse on. It is the whole
// admission law in one place: words, a workspace, a kind with its fields, an
// action with its text, and rails that are not zero.
func (it Item) Validate() error {
	switch {
	case it.Words == "":
		return errors.New("an item needs the person's words")
	case it.Workspace == "":
		return errors.New("an item needs a workspace")
	}
	// A HOLD CANNOT SPEND, SO IT ALONE CARRIES NO RAILS AND NO ACTION. A waking
	// kind still needs to be able to fire at all; what it may spend when it does
	// is allowed to be unbounded, because 0 there is the person's own "no limit"
	// and the firing site already reads it that way.
	if it.Spends() {
		switch {
		case it.Rails.PerRunUSD < 0:
			return errors.New("a per-run budget cannot be negative")
		case it.Rails.MaxPerDay <= 0:
			return errors.New("an item needs a max per day")
		}
	}
	switch it.Altitude {
	case "", AltitudeProject, AltitudeMachine:
	case AltitudeConversation:
		if it.Origin.SessionID == "" {
			return errors.New("a conversation item needs its conversation")
		}
	default:
		return errors.New("unknown altitude: " + string(it.Altitude))
	}
	for _, ex := range it.Exceptions {
		if (ex.Workspace == "") == (ex.SessionID == "") {
			return errors.New("an exception is exactly one of a workspace or a conversation")
		}
	}
	switch it.When.Kind {
	case WhenAt:
		if it.When.At.IsZero() {
			return errors.New("an at item needs its moment")
		}
	case WhenEvery:
		if _, err := ParseEvery(it.When.Every); err != nil {
			return err
		}
	case WhenFile:
		if it.When.Glob == "" {
			return errors.New("a file item needs a glob")
		}
	case WhenIdle:
		if it.When.IdleFor <= 0 {
			return errors.New("an idle item needs how long")
		}
	case WhenProbe:
		if (it.When.Probe.Command == "") == (it.When.Probe.Tool == "") {
			return errors.New("a probe is exactly one of a command or a tool")
		}
	case WhenHold:
		// Nothing wakes it, so nothing about waking can be wrong.
	default:
		return errors.New("unknown when: " + string(it.When.Kind))
	}
	switch it.Does.Kind {
	case ActionSay:
		if it.Does.Say == "" {
			return errors.New("a say item needs what to say")
		}
	case ActionTask:
		if it.Does.Brief == "" {
			return errors.New("a task item needs a brief")
		}
	case "":
		if it.Spends() {
			return errors.New("unknown action: " + string(it.Does.Kind))
		}
	default:
		return errors.New("unknown action: " + string(it.Does.Kind))
	}
	return nil
}

// Spends answers whether anything about this item can ever cost money, and it
// is the ONE PLACE that question is decided.
//
// EVERY WAKING KIND CAN. A probe runs, the sentinel judges it, a firing works —
// all three are billed, which is why [Item.Validate] refuses one of them with a
// zero budget. A HOLD CANNOT: nothing wakes it, so nothing about it is ever
// bought. That is why it alone may carry no rails, and it is why the
// ratification card draws no cost band for a rule — THE EMPTINESS LAW IS THE
// OTHER HALF OF THE SENTENCE, and a figure nobody can spend is a figure no
// surface may print.
func (it Item) Spends() bool { return it.When.Kind != WhenHold }

// Level is the altitude with the zero value resolved to its meaning.
func (it Item) Level() Altitude {
	if it.Altitude == "" {
		return AltitudeProject
	}
	return it.Altitude
}

// Title is what a row too narrow for a sentence leads with: the brief's title,
// or the words themselves when nobody wrote one.
func (it Item) Title() string {
	if it.Brief.Title != "" {
		return it.Brief.Title
	}
	return it.Words
}

// Prompt is the instruction the machinery follows: the compiled brief, or the
// person's words themselves when nobody compiled one.
func (it Item) Prompt() string {
	if it.Brief.Prompt != "" {
		return it.Brief.Prompt
	}
	return it.Words
}

// Reaches answers whether this item's altitude covers the given place, WITH
// EXCEPTIONS IGNORED. It exists because a page drawing its dim "not here"
// lines asks exactly "would this have applied but for the person keeping it
// out" — and before it was in the contract, the one caller answered that by
// copying the item and clearing its exceptions, which is the contract's own
// arithmetic written a second time. Callers pass what they know; an empty
// sessionID is a place with no conversation (a task's worktree, a firing).
func (it Item) Reaches(workspace, sessionID string) bool {
	switch it.Level() {
	case AltitudeMachine:
		return true
	case AltitudeProject:
		return workspace != "" && it.Workspace == workspace
	case AltitudeConversation:
		return sessionID != "" && it.Origin.SessionID == sessionID
	}
	return false
}

// AppliesTo answers whether this item governs the given place: its reach,
// minus the places the person kept it out of. An exception beats every
// altitude.
func (it Item) AppliesTo(workspace, sessionID string) bool {
	return !it.ExceptedFrom(workspace, sessionID) && it.Reaches(workspace, sessionID)
}

// ExceptedFrom answers whether the person excepted this item from the place.
func (it Item) ExceptedFrom(workspace, sessionID string) bool {
	for _, ex := range it.Exceptions {
		if ex.Workspace != "" && ex.Workspace == workspace {
			return true
		}
		if ex.SessionID != "" && ex.SessionID == sessionID {
			return true
		}
	}
	return false
}

// NeedsPermissionLead opens the one line a firing leaves when it stopped
// because a call needed permission and nobody was there to give it. It is
// declared here, beside the field, because two packages must agree on it: the
// session writes it and [IsPermissionLine] recognises it.
const NeedsPermissionLead = "stopped: it needed your ok to run "

// permissionRefusals are the sentences this program writes for a call that
// needed a person and had none, matched by the PROPERTY each one states rather
// than by its exact words. Three doors write such a sentence and they do not
// share a spelling: the turn inside a task says `— nobody to ask`, the turn
// with no resolver says `no resolver is attached`, and the door that runs this
// program underneath another one says `nobody is here to ask`. Holding the
// three literals would mean a fourth door, or a reworded third, silently
// stopped counting as a permission stop — which is exactly how the spelling on
// disk today came to be unrecognised.
// THE ABSENCE ALONE IS NOT ENOUGH, and the refusal word is what makes the
// first shape safe. This field's other tenant is a QUESTION the firing put to
// the person in its own words, which is model prose and can say anything: "there
// is nobody on call, who do you want me to ask" carries the absence and is a
// question. Reading it as a permission stop would put it down the moment the
// person paused the item, losing the one thing the field exists to carry. Both
// doors that state the absence also say they refused, so requiring that costs
// nothing.
var permissionRefusals = [][]string{
	{"nobody", "to ask", "refus"},
	{"no resolver is attached"},
}

// IsPermissionLine reports whether a line on an item is about a permission the
// firing could not get, rather than a QUESTION it put to the person. It is the
// ONE predicate for that, asked by the store when a person changes an item and
// by the surface when it decides which door a row takes, so a line cannot be a
// permission in one place and a question in another.
//
// IT KNOWS THE OLD SPELLING AS WELL AS THE NEW ONE, and that is not tidiness.
// Builds before this one put the engine's own refusal on the item verbatim, and
// those items are on disk now: a watch stuck for days carries `refused in a
// task: default — nobody to ask` and will carry it until it fires again, which
// an item that has spent its allowance for the day cannot do. A predicate that
// knew only the new lead would leave every row that provoked this exactly as it
// was, which is the one outcome that would make the change pointless.
func IsPermissionLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" {
		return false
	}
	if strings.HasPrefix(line, NeedsPermissionLead) {
		return true
	}
	lower := strings.ToLower(line)
	for _, said := range permissionRefusals {
		if saysAllOf(lower, said) {
			return true
		}
	}
	return false
}

// saysAllOf reports whether the line carries every part of one of the shapes in
// [permissionRefusals]. The parts are looked for anywhere and in any order,
// which is what lets one shape cover both `nobody to ask` and `nobody is here
// to ask` without either spelling being written down twice.
func saysAllOf(lower string, parts []string) bool {
	for _, part := range parts {
		if !strings.Contains(lower, part) {
			return false
		}
	}
	return true
}

// ClearNeedsPerson puts down the line about a permission a firing could not
// get, on the person's own act of changing the item. The acts are the ones this
// build has: pausing it, stopping it, and letting it go again. What a run before
// that could not be allowed to do is no longer news about what this item will
// do next.
//
// IT LEAVES A QUESTION ALONE, and that is the whole of why it reads the line
// before clearing it. This field carries two different things. One is a
// QUESTION the firing actually put to the person, in its own words, which is
// theirs to answer and which nothing may throw away behind their back — pausing
// a watch is not answering it. The other is the line this build writes when a
// call was refused for want of somebody to allow it, which goes stale the
// moment the item changes. Only the second is put down.
//
// IT IS A CHANGE AND NOT A LOOK. Opening an item and closing it again leaves
// the row exactly as it was, because nothing about the item moved and the
// reason it stopped is still true.
func (it Item) ClearNeedsPerson() Item {
	if !IsPermissionLine(it.NeedsPerson) {
		return it
	}
	it.NeedsPerson = ""
	return it
}

// Glyph is the one character a row leads with, decided here so every surface
// agrees: ▲ needs you, ● a pass has it in its hands right now — checking it or
// firing it — ◦ waiting for its time, ∙ paused or retired. Running is the
// store's knowledge and not the item's, so it is passed ([Store.Running]).
func (it Item) Glyph(running bool) string {
	switch {
	case it.NeedsPerson != "":
		return "▲"
	case running:
		return "●"
	case it.Status != StatusActive:
		return "∙"
	}
	return "◦"
}

// ── firing now: the one fact that is not in the document ────────────────────

// RunningMark is what a pass leaves behind while it has one item in its hands:
// which process is doing it, since when, and which half of a pass it is in. It
// is the answer [Store.Running] gives and the whole of what running.go writes.
//
// IT IS A CLAIM ABOUT NOW AND IT IS ALWAYS DOUBTED. A process that was killed
// mid-firing leaves its marker behind, so every reader treats a dead pid or an
// age past [TickWindow] as no marker at all (running.go's markLive).
type RunningMark struct {
	PID   int       `json:"pid"`
	Since time.Time `json:"since"`
	// What is [RunningChecking] or [RunningFiring]. A surface says it in those
	// words — "checking now", "firing now" — so it is the person's vocabulary
	// and not a state name.
	What string `json:"what"`
}

// The two things a pass can be doing to one item, and the whole of what a
// marker's What may say. Checking is the look — a probe, a fingerprint, the
// sentinel's yes-or-no; firing is the work that follows a yes.
const (
	RunningChecking = "checking"
	RunningFiring   = "firing"
)

// RunningFile is the marker's name inside the item's own folder
// ([Store.RunningPath]).
const RunningFile = "running"

// ── the store ───────────────────────────────────────────────────────────────

// Root is where everything standing lives: <codeaf home>/v3/standing.
// Callers pass it in rather than this package reading internal/home, so a
// test's store is a temp dir and nothing else.
type Store struct {
	root string
	// clock is the store's now. It exists so a test can stamp documents from a
	// held clock; nothing outside this package sets it and it is nil in every
	// real build, which reads as time.Now.
	clock func() time.Time
}

// Open answers the store at root, creating the directory. It holds no handles.
func Open(root string) (*Store, error) {
	if root == "" {
		return nil, errors.New("standing: an empty root")
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

// Root is the directory the store was opened on.
func (s *Store) Root() string { return s.root }

// ItemPath is <root>/<id>.json.
func (s *Store) ItemPath(id string) string { return filepath.Join(s.root, id+".json") }

// ItemDir is <root>/<id>/ — the item's own folder: runs/, exchange/, log.
func (s *Store) ItemDir(id string) string { return filepath.Join(s.root, id) }

// RunsDir is where an item's firings live, one session folder each, numbered.
// They live here and NOT under v3/projects so home never scans them.
func (s *Store) RunsDir(id string) string { return filepath.Join(s.ItemDir(id), "runs") }

// ExchangeDir is where a home-made item's origin exchange is kept.
func (s *Store) ExchangeDir(id string) string { return filepath.Join(s.ItemDir(id), "exchange") }

// ExchangesRoot is where an errand said at home keeps its folder BEFORE
// anything stands: <root>/exchanges/<session id>/. Home's own `ask here` makes
// one there so that home never lists it, the session lane reads it to know that
// a conversation IS an errand, and the sweep reaps the ones that came to
// nothing after [RunKeep].
//
// It takes the root rather than hanging off the store because two of those
// three callers hold a path and not a store, and opening one to ask a question
// about a directory would create the directory.
func ExchangesRoot(root string) string { return filepath.Join(root, "exchanges") }

// LogPath is the item's own one-line-per-event log: checks that found
// something, firings, pauses. Never a line per quiet check.
func (s *Store) LogPath(id string) string { return filepath.Join(s.ItemDir(id), "log") }

// LedgerPath is the append-only daily ledger the rails are summed from.
func (s *Store) LedgerPath(day time.Time) string {
	return filepath.Join(s.root, "ledger-"+day.Format("2006-01-02")+".jsonl")
}

// LockPath is the flock one ticker at a time holds. A window takes it for the
// length of a pass; `codeaf tick` refuses when it is held.
func (s *Store) LockPath() string { return filepath.Join(s.root, "tick.lock") }

// WakeLogPath is where every pass writes one line, and where "last wake" is
// read from.
func (s *Store) WakeLogPath() string { return filepath.Join(s.root, "wake.log") }

// ErrNotFound is Get's answer for an id that is not here.
var ErrNotFound = errors.New("standing: no such item")

// ── the ledger ──────────────────────────────────────────────────────────────

// Entry is one line of the daily ledger: one firing, or one sentinel call, with
// what it cost. The rails are sums over today's lines.
type Entry struct {
	At     time.Time `json:"at"`
	ItemID string    `json:"item"`
	// Kind is "check" (a probe + sentinel), "say", or "task".
	Kind string  `json:"kind"`
	USD  float64 `json:"usd"`
	// Run is the run folder, for a firing.
	Run string `json:"run,omitempty"`
}

// Spend is what today's ledger says, for one item or for all.
type Spend struct {
	USD   float64
	Fired int
}

// ── the inbox: how news reaches a conversation that is not open ─────────────

// Note is one line of news for a conversation: a firing's delivery, a
// needs-your-look, a failure. It is appended to <session dir>/inbox.jsonl
// when the conversation's window is not open, and drained into one "while you
// were away" fold the next time it is.
type Note struct {
	At     time.Time `json:"at"`
	ItemID string    `json:"item"`
	Words  string    `json:"words"`
	// Kind is "said", "landed", "needs-you", or "failed".
	Kind string `json:"kind"`
	Text string `json:"text"`
	// Run is the run folder a person can open for the whole story.
	Run string `json:"run,omitempty"`
}

// InboxPath is the inbox inside a session folder.
func InboxPath(sessionDir string) string { return filepath.Join(sessionDir, "inbox.jsonl") }

// ── the pass ────────────────────────────────────────────────────────────────

// Judgment is what the sentinel is asked: the person's words, the hint, the
// evidence a probe gathered, and what the sentinel said the last few times.
type Judgment struct {
	Item     Item
	Evidence string
	Previous []string
}

// Sentinel is one cheap yes/no call. The line is kept on the item and in the
// log; it is read by the person, so it is one plain sentence.
type Sentinel func(ctx context.Context, judgment Judgment) (yes bool, line string, usd float64, err error)

// Outcome is what a run came to.
type Outcome struct {
	// Kind is "said", "landed", "needs-you", "failed", or [OutcomeNothing].
	Kind string
	Text string
	USD  float64
	// NeedsPerson is the one line the run stopped on, when Kind is needs-you.
	NeedsPerson string
}

// OutcomeNothing is the [Outcome.Kind] of a run that delivered nothing at all:
// no line, no landing, nothing waiting for the person. It is the ONE outcome
// whose run folder the sweep may reap after [RunKeep], so it is a constant
// rather than a word spelled twice in two packages.
const OutcomeNothing = "nothing"

// The two outcomes that mean a firing did NOT come back clean, spelled once
// here because three places now test for them — the clause a card reads
// ([outcomeClause]), the recorder ([Ticker.fire]) and the trust counter that
// recorder keeps. They were string literals in each, which is a word spelled
// three times and therefore a word that will drift.
const (
	// OutcomeNeedsYou is a firing that stopped on a question for the person.
	OutcomeNeedsYou = "needs-you"
	// OutcomeFailed is a firing that could not finish.
	OutcomeFailed = "failed"
)

// CameTo is the one-word file a firing leaves in its run folder saying what
// that run came to — the same word as [Outcome.Kind]. The item's own
// LastOutcome is overwritten by the next firing, so without this nothing on
// disk would say which of a hundred run folders delivered anything.
const CameTo = "came-to"

// RunCameToNothing answers whether a run folder's own marker says the run
// delivered nothing, which is the whole of the sweep's licence over it.
//
// EVERYTHING ELSE ANSWERS FALSE: a run that said something, landed something or
// is waiting for the person; a marker that cannot be read; and a run with no
// marker at all. A folder that cannot say what it came to is a folder nobody
// may remove.
func RunCameToNothing(runDir string) bool {
	raw, err := os.ReadFile(filepath.Join(runDir, CameTo))
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(raw)) == OutcomeNothing
}

// Runner is supplied by the session lane. It is how a firing touches the
// world: a probe run in the item's workspace, a line delivered, a task run
// headless under the person's banked rules in a fresh session folder at
// runDir.
type Runner interface {
	// Probe runs the item's probe and answers its output, clipped.
	Probe(ctx context.Context, item Item) (string, error)
	// Say delivers one line: into the origin conversation if it is open in
	// this process, else into its inbox, and always onto the item.
	Say(ctx context.Context, item Item, text string) (Outcome, error)
	// Run runs the item's task brief in a fresh headless session at runDir,
	// with the evidence available to the brief, bounded by the item's rails.
	Run(ctx context.Context, item Item, runDir, evidence string) (Outcome, error)
}

// Idle answers whether the machine is quiet enough for a WhenIdle: no live
// presence file says busy, and the last person activity anywhere is older
// than the given duration. The session lane supplies it from the world reader.
type Idle func(for_ time.Duration) bool

// Tidied is what one consolidation pass over what is remembered came to
// (internal/session's memory_consolidate.go): how many lines were merged into
// one clearer line, how many were retired in favour of one that replaced them,
// and what the single call cost.
//
// It is declared HERE rather than in the session lane because the two things a
// pass owes the person about it — the money on the day's rail and the line in
// the wake log — are both this package's business.
type Tidied struct {
	Merged     int
	Superseded int
	USD        float64
}

// Changed is how many remembered lines the pass actually moved. Zero is a pass
// that read fifty lines and decided every one of them was already right, which
// is the ordinary answer.
func (t Tidied) Changed() int { return t.Merged + t.Superseded }

// Line is the pass's own account of a tidy, THE EMPTINESS LAW APPLIED: a part
// that is zero is absent rather than printed as a zero, and a pass that changed
// nothing and spent nothing is no line at all.
func (t Tidied) Line() string {
	parts := make([]string, 0, 3)
	if t.Merged > 0 {
		parts = append(parts, strconv.Itoa(t.Merged)+" merged")
	}
	if t.Superseded > 0 {
		parts = append(parts, strconv.Itoa(t.Superseded)+" superseded")
	}
	if t.USD > 0 {
		parts = append(parts, fmt.Sprintf("$%.3f", t.USD))
	}
	if len(parts) == 0 {
		return ""
	}
	return "consolidated · " + strings.Join(parts, " · ")
}

// Tidy is the one piece of work in a pass that nobody armed: the call that
// reads what is remembered and answers with the duplicates merged and the
// replaced lines retired. The session lane supplies it, and a NIL Tidy is the
// whole of "memory is off on this machine" — a capability that cannot work is
// absent rather than present and refusing.
type Tidy func(ctx context.Context) (Tidied, error)

// Pass is what one tick decided, for the wake log and for /status.
type Pass struct {
	At       time.Time
	Examined int
	Checked  int
	Fired    int
	Said     int
	NeedsYou int
	Skipped  int
	Errors   int
	// Tidied is how many remembered lines the consolidation pass moved, which
	// is zero on all but a handful of passes a day (see [Tidy]).
	Tidied int
	// Notes are one sentence per thing worth saying, for the log.
	Notes []string
}

// Ticker runs passes. One is built per process that may tick — a window, or
// `codeaf tick` — and [Ticker.Tick] is what both call.
type Ticker struct {
	Store    *Store
	Sentinel Sentinel
	Runner   Runner
	Idle     Idle
	// Tidy is the consolidation pass over what is remembered, run once at the
	// end of a pass and only when the session lane supplied one.
	Tidy Tidy
	// DailyRailUSD is the ceiling on everything standing spends in one day,
	// from settings. Zero is no rail, which the card says out loud.
	DailyRailUSD float64
	// Now is the clock, injectable for tests.
	Now func() time.Time
}

// ErrHeld is Tick's answer when another process holds the lock.
var ErrHeld = errors.New("standing: another codeaf is ticking")

// ── the rhythm ──────────────────────────────────────────────────────────────

// ParseEvery, in every.go, is the one reader of a WhenEvery's Every: a
// five-field cron line or a Go duration, in, and a function from a moment to
// the next moment, out. Nothing else in the product parses a cadence.

// ── keeping watch with no window open ───────────────────────────────────────

// WatchStatus is what /status prints, derived and never asserted.
type WatchStatus struct {
	// Installed means the OS timer's definition on disk matches byte for byte
	// what this build writes.
	Installed bool
	LastWake  time.Time
	NextDue   time.Time
}

// Watch is the OS timer: a launchd agent or a systemd user timer running
// `codeaf tick` every [Interval]. The core lane builds it on internal/watchdog's
// shape with its own unit names, so it can coexist with v1's.
type Watch interface {
	Install(ctx context.Context) error
	Uninstall(ctx context.Context) error
	Status() (WatchStatus, error)
}
