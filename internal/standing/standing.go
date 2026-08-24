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
//     stops the run as "needs your look". There are no probation counters: the
//     rules are the tenure.
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
// `aforge tick` each used to name their own 120 seconds, and [Store.Running]
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
	// WhenHold never wakes. A rule — "always use tabs here", "never touch the
	// public API" — has no moment, no rhythm and no probe: its whole work is
	// done at birth, riding into the world of every conversation and task it
	// reaches (docs/STANDING-ORDERS.md, the birth seam). The pass walks past
	// it; it cannot fire, so it cannot spend, so it alone needs no rails and
	// no action.
	WhenHold WhenKind = "hold"
)

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
// Model and MaxSteps for ActionTask. Either kind may template the probe's
// evidence into its text with {{evidence}}.
type Action struct {
	Kind       ActionKind `json:"kind"`
	Say        string     `json:"say,omitempty"`
	Brief      string     `json:"brief,omitempty"`
	Acceptance string     `json:"acceptance,omitempty"`
	Model      string     `json:"model,omitempty"`
	MaxSteps   int        `json:"maxSteps,omitempty"`
}

// Rails bound an item. They are mandatory by construction: [Store.Create]
// refuses an item whose PerRunUSD or MaxPerDay is zero. They stay quiet on the
// proposal card unless the person named money themselves, because the ordinary
// promise is the machine-wide daily allowance.
type Rails struct {
	// PerRunUSD is the most one firing may spend, probe and sentinel included.
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
	NeedsPerson string `json:"needsPerson,omitempty"`
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
	// A HOLD CANNOT SPEND, SO IT ALONE CARRIES NO RAILS AND NO ACTION. Every
	// waking kind still refuses a zero budget by construction.
	if it.Spends() {
		switch {
		case it.Rails.PerRunUSD <= 0:
			return errors.New("an item needs a per-run budget")
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

// Root is where everything standing lives: <aforge home>/v3/standing.
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
// length of a pass; `aforge tick` refuses when it is held.
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
// `aforge tick` — and [Ticker.Tick] is what both call.
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
var ErrHeld = errors.New("standing: another aforge is ticking")

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
// `aforge tick` every [Interval]. The core lane builds it on internal/watchdog's
// shape with its own unit names, so it can coexist with v1's.
type Watch interface {
	Install(ctx context.Context) error
	Uninstall(ctx context.Context) error
	Status() (WatchStatus, error)
}
