// Package automation is the work codeaf does on a clock: a reminder at six, the
// weekly update every Monday at nine, a watch on CI that speaks when it goes
// red. the implementation is the design; this package is the
// object, the store, the schedule and the clock. What a run actually DOES —
// a look, a judgment, a piece of unattended work — is the session's business
// and arrives through [Runner].
//
// ── THE LAWS ──
//
//   - THREE KINDS, ONE MECHANISM. A reminder, scheduled work and a watch are
//     all an [Automation]: a schedule, an optional look, and an action. A watch
//     is a schedule whose action waits on a look; a reminder is a schedule whose
//     action is a fixed line. Nothing else wakes.
//
//   - NOTHING RUNS WHILE CODEAF IS CLOSED. The clock lives inside codeaf and
//     runs only while at least one window is open ([Presence]). There is no
//     operating-system timer, no daemon and nothing to install. What fell due
//     while codeaf was closed catches up ONCE when it opens, marked late.
//
//   - ONE CLOCK AT A TIME. Every window's process may hold the clock, and an
//     operating-system file lock decides which one does ([Clock]). The lock dies
//     with its process, so a crash never leaves the machine without a clock.
//
//   - THE STORE IS THE TRUTH. Every run is a row before it starts and the same
//     row when it ends. A window learns what happened by reading the store, never
//     by being the process that happened to run it, so news reaches every window
//     whichever process held the clock.
//
//   - AN OUTCOME IS A FACT, NEVER A GUESS FROM PROSE. Work says how it went by
//     reporting it ([Report]); a run that ends without a report, runs out of
//     time, hits its cap or loses its provider is `incomplete` with the reason.
//     The words are the task-state words:
//     done, stopped, incomplete, your call — never "failed".
//
//   - NOTHING IS SAVED UNTIL THE PERSON SAYS YES. [Store.Create] is called after
//     a card was answered or a typed command was confirmed; nothing arms itself.
package automation

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// The limits one run gets when nobody named any. They are shown on every card
// and edited there, never applied silently.
const (
	DefaultTime = 30 * time.Minute
	DefaultUSD  = 5.0
)

// Kind is what an automation is, derived from what it holds: a look makes a
// watch, a fixed line makes a reminder, and a brief makes scheduled work.
type Kind string

const (
	KindReminder Kind = "reminder"
	KindWork     Kind = "work"
	KindWatch    Kind = "watch"
)

// Status is where an automation is in its life.
type Status string

const (
	// StatusActive wakes on its schedule.
	StatusActive Status = "active"
	// StatusPaused keeps everything and wakes for nothing until it is resumed.
	StatusPaused Status = "paused"
	// StatusFinished is a one-time automation that has run, or a once-watch
	// that has spoken. It stays in the list with its history.
	StatusFinished Status = "finished"
	// StatusProposed is a standing order an agent proposed and the person has
	// not ratified. NOTHING RUNS BEFORE RATIFY: it is in the list and wakes for
	// nothing until [Store.Ratify].
	StatusProposed Status = "proposed"
	// StatusStopped is one the person stopped for good. It stays in the list
	// with its history and wakes for nothing; it is not resumed.
	StatusStopped Status = "stopped"
)

// Automation is one thing codeaf does on a clock. The top half is what the
// person agreed to; [Automation.Next] and [Automation.Seen] are the clock's
// own present, rewritten as it works.
type Automation struct {
	ID string `json:"id"`
	// Title is the short name a row leads with.
	Title string `json:"title"`
	// Words is the person's own sentence when it was said in a conversation,
	// kept verbatim as provenance. A typed automation has none.
	Words    string   `json:"words,omitempty"`
	Schedule Schedule `json:"schedule"`
	// Look is set for a watch and only for a watch.
	Look   *Look  `json:"look,omitempty"`
	Action Action `json:"action"`
	// Workspace is where the automation runs: a project's root, or the
	// person's home for one that belongs to no project.
	Workspace string `json:"workspace"`
	// Worktree runs work in a separate git worktree whose branch is kept for
	// review, rather than in the live checkout.
	Worktree bool   `json:"worktree,omitempty"`
	Limits   Limits `json:"limits"`
	Origin   Origin `json:"origin,omitempty"`

	Status  Status    `json:"status"`
	Created time.Time `json:"created"`
	Updated time.Time `json:"updated"`
	// Revision counts the person's changes. The clock's own writes are guarded
	// by it, so an edit made while a run was in flight is never written over.
	Revision int64 `json:"revision"`

	// Next is when it next wakes; zero when it never will again.
	Next time.Time `json:"next,omitempty"`
	// Seen is a watch's last decided judgment, "yes" or "no", and empty before
	// its first. A watch speaks on the change from anything else to "yes".
	Seen string `json:"seen,omitempty"`
	// Memo is what a watch's look keeps between looks — a file watch's listing,
	// so the next look can say what changed. It is the look's own business and
	// opaque to everything else.
	Memo string `json:"memo,omitempty"`

	// Trust is how far the person lets it run alone (trust.go): every run
	// asked first, earning trust over Approved runs, or trusted. Empty is
	// trusted, which every automation saved before the ladder existed is.
	Trust    Trust `json:"trust,omitempty"`
	Approved int   `json:"approved,omitempty"`
	// Granted is the run the person last said go to, which the clock then
	// starts without asking again.
	Granted int64 `json:"granted,omitempty"`
	// AnswersIn is where its results are said: the chat it was made in, the
	// crew, everywhere, or nowhere but its own row. Empty is the chat.
	AnswersIn Scope `json:"answersIn,omitempty"`
	// Crew is the crew it belongs to, and Wont what it is told never to do.
	Crew string `json:"crew,omitempty"`
	Wont string `json:"wont,omitempty"`

	// Last is the most recent run that ended, filled in by [Store.List] and
	// [Store.Get] from the runs table so a list can say what each one last came
	// to without asking once per row. IT IS NEVER STORED IN THE DOCUMENT: the
	// runs table is the one record of a run.
	Last *Run `json:"last,omitempty"`
}

// Look is what a watch looks at, and the sentence its judgment is held to.
// Exactly one of Command, Files or Tool is set.
type Look struct {
	// Command is a shell command run in the workspace.
	Command string `json:"command,omitempty"`
	// Files is a glob, relative to the workspace, whose files are read.
	Files string `json:"files,omitempty"`
	// Tool and Args are one call to a tool on the belt — a connected
	// account's, most often.
	Tool string          `json:"tool,omitempty"`
	Args json.RawMessage `json:"args,omitempty"`
	// Service is the connected account a Tool belongs to, when it is one: its
	// tools are only on the belt once the service is put to use, so the look
	// puts it to use first.
	Service string `json:"service,omitempty"`
	// Condition is what the model is asked of every look: "the latest run on
	// main failed", "an invoice from Hetzner arrived".
	Condition string `json:"condition"`
	// Once finishes the watch after the first time it speaks.
	Once bool `json:"once,omitempty"`
}

// Action is what happens when an automation wakes, or when its watch speaks.
// Exactly one of Say or Do is set.
type Action struct {
	// Say is a fixed line delivered as it is. No model is called.
	Say string `json:"say,omitempty"`
	// Do is a brief carried out as unattended work.
	Do string `json:"do,omitempty"`
}

// Limits bound one run.
type Limits struct {
	// Time is the most one run may take. Zero is [DefaultTime].
	Time time.Duration `json:"time,omitempty"`
	// USD is the most one run may spend, a watch's judgment included. Zero is
	// [DefaultUSD].
	USD float64 `json:"usd,omitempty"`
}

// Effective is the limits with defaults applied — the figures a card quotes and
// a run is held to.
func (l Limits) Effective() Limits {
	if l.Time <= 0 {
		l.Time = DefaultTime
	}
	if l.USD <= 0 {
		l.USD = DefaultUSD
	}
	return l
}

// OriginTeam is creation-team provenance for one automation.
//
// On [Origin.Team]:
//   - nil — legacy UNKNOWN (saved before this field existed); never backfilled
//   - non-nil with Known false — forbidden on write; readers treat as UNKNOWN
//   - Known true and ID empty — explicit NONE (ordinary conversation turn)
//   - Known true and ID set — the exact team id stamped from an accepted
//     crew-message turn and validated at creation
//
// THIS IS NOT [Automation.Crew]. Crew is belonging; Origin.Team is where the
// automation was made. A create path never writes Crew from this fact.
type OriginTeam struct {
	Known bool   `json:"known"`
	ID    string `json:"id,omitempty"`
}

// Origin is the conversation an automation was made in: where its one line
// goes when a run ends, and the door "why did this happen?" opens.
type Origin struct {
	SessionID  string `json:"sessionId,omitempty"`
	Transcript string `json:"transcript,omitempty"`
	// Team is creation-team provenance. See [OriginTeam]. Change and reopen
	// keep whatever was stored; they never retarget from current membership.
	Team *OriginTeam `json:"team,omitempty"`
}

// Kind is what this automation is.
func (a Automation) Kind() Kind {
	switch {
	case a.Look != nil:
		return KindWatch
	case strings.TrimSpace(a.Action.Say) != "":
		return KindReminder
	default:
		return KindWork
	}
}

// Validate is the whole admission law in one place.
func (a Automation) Validate() error {
	if strings.TrimSpace(a.Title) == "" {
		return errors.New("an automation needs a title")
	}
	if strings.TrimSpace(a.Workspace) == "" {
		return errors.New("an automation needs a workspace")
	}
	if err := a.Schedule.Validate(); err != nil {
		return err
	}
	say, do := strings.TrimSpace(a.Action.Say), strings.TrimSpace(a.Action.Do)
	switch {
	case say == "" && do == "":
		return errors.New("an automation needs something to say or something to do")
	case say != "" && do != "":
		return errors.New("an automation says a line or does work, not both")
	}
	if a.Worktree && do == "" {
		return errors.New("only work can run in a separate worktree")
	}
	if look := a.Look; look != nil {
		set := 0
		for _, field := range []string{look.Command, look.Files, look.Tool} {
			if strings.TrimSpace(field) != "" {
				set++
			}
		}
		if set != 1 {
			return errors.New("a watch looks at exactly one of a command, files or a tool")
		}
		if strings.TrimSpace(look.Condition) == "" {
			return errors.New("a watch needs the condition it is looking for")
		}
		if !a.Schedule.Repeats() {
			return errors.New("a watch needs a rhythm to look on")
		}
		if len(look.Args) > 0 && !json.Valid(look.Args) {
			return errors.New("a watch's tool arguments are not valid JSON")
		}
	}
	if a.Limits.Time < 0 || a.Limits.USD < 0 {
		return errors.New("a limit cannot be negative")
	}
	switch a.Status {
	case "", StatusActive, StatusPaused, StatusFinished, StatusProposed, StatusStopped:
	default:
		return fmt.Errorf("unknown status %q", a.Status)
	}
	switch a.Trust {
	case "", TrustAsksFirst, TrustEarning, TrustTrusted:
	default:
		return fmt.Errorf("unknown trust %q", a.Trust)
	}
	switch a.AnswersIn {
	case "", ScopeChat, ScopeCrew, ScopeEverywhere, ScopeNotHere:
	default:
		return fmt.Errorf("unknown place to answer in %q", a.AnswersIn)
	}
	return nil
}

// ── runs ────────────────────────────────────────────────────────────────────

// Phase is where a run is: waiting for a slot, in hand, or over.
type Phase string

const (
	PhaseQueued  Phase = "queued"
	PhaseRunning Phase = "running"
	PhaseOver    Phase = "over"
	// PhaseAsking is a run waiting for the person's go before it starts,
	// because its automation is not yet trusted to run alone (trust.go).
	PhaseAsking Phase = "asking"
)

// Outcome is what a run came to. The words a person reads are [Outcome.Word].
type Outcome string

const (
	// OutcomeDone is work that reported it finished, a reminder said, or a
	// watch whose condition turned true.
	OutcomeDone Outcome = "done"
	// OutcomeQuiet is a watch that looked and found its condition not met, or
	// still met since it last spoke. Nothing is delivered for it.
	OutcomeQuiet Outcome = "quiet"
	// OutcomeYourCall is a run that stopped on something only the person can
	// allow or answer.
	OutcomeYourCall Outcome = "your-call"
	// OutcomeIncomplete is a run that ended without finishing; Line says why.
	OutcomeIncomplete Outcome = "incomplete"
	// OutcomeStopped is a run the person stopped, or one codeaf closing ended.
	OutcomeStopped Outcome = "stopped"
	// OutcomeUnchecked is a watch whose look or judgment could not be made.
	// It changes nothing about what the watch has seen.
	OutcomeUnchecked Outcome = "unchecked"
	// OutcomeSkipped is a run the person was asked about and passed over, or
	// said was not right.
	OutcomeSkipped Outcome = "skipped"
)

// Word is the outcome as a person reads it.
func (o Outcome) Word() string {
	switch o {
	case OutcomeDone:
		return "done"
	case OutcomeQuiet:
		return "nothing new"
	case OutcomeYourCall:
		return "your call"
	case OutcomeIncomplete:
		return "incomplete"
	case OutcomeStopped:
		return "stopped"
	case OutcomeUnchecked:
		return "couldn't check"
	case OutcomeSkipped:
		return "skipped"
	}
	return ""
}

// Delivered reports whether a run with this outcome is news: it gets a line in
// its conversation and a notification. A quiet look is the one that is not —
// QUIET IS THE DESIGN, and a watch that checked faithfully all day says so on
// its row, not in a stream of "nothing new".
func (o Outcome) Delivered() bool { return o != OutcomeQuiet && o != OutcomeSkipped && o != "" }

// Why says what woke a run.
type Why string

const (
	// WhyOnTime is a slot taken when it fell due.
	WhyOnTime Why = "on time"
	// WhyLate is a slot taken after it fell due — codeaf was closed or the
	// machine was asleep. However many slots were missed, one run catches up.
	WhyLate Why = "late"
	// WhyNow is a run somebody asked for outside the schedule.
	WhyNow Why = "now"
)

// LateAfter is how far behind its slot a run may start and still be on time.
// The clock looks every few seconds, so anything past this was really missed.
const LateAfter = 2 * time.Minute

// Run is one waking of one automation.
type Run struct {
	ID           int64  `json:"id"`
	AutomationID string `json:"automationId"`
	// Due is the slot this run was for; for a run asked for now, the moment
	// it was asked.
	Due time.Time `json:"due"`
	// Asked is when the run began to wait on the person, which for one the
	// clock parked late is long after its slot. It is zero for a run that
	// never asked, and for one an older build parked (store_asked.go).
	Asked    time.Time `json:"asked,omitzero"`
	Why      Why       `json:"why"`
	Phase    Phase     `json:"phase"`
	Started  time.Time `json:"started,omitempty"`
	Finished time.Time `json:"finished,omitempty"`
	Outcome  Outcome   `json:"outcome,omitempty"`
	// Line is the one sentence a person reads for this run: what it said, what
	// it found, or why it stopped.
	Line string `json:"line,omitempty"`
	// Detail is the longer account — the work's report, what a look saw.
	Detail string  `json:"detail,omitempty"`
	USD    float64 `json:"usd,omitempty"`
	// Transcript is the folder of the session a piece of work ran in.
	Transcript string `json:"transcript,omitempty"`
}

// News reports whether this run is something to tell a person: over, with an
// outcome worth a line, and not one codeaf closing cut off — that one is
// paused, and the run that picks up after it is the one that is told.
func (r Run) News() bool { return r.Phase == PhaseOver && r.Outcome.Delivered() && !r.Closed() }

// Closed reports whether codeaf closing cut this run off. Such a run is paused
// rather than over: nobody is told about it, and its automation runs again,
// once, when a window next opens ([Store.Finish]).
func (r Run) Closed() bool { return r.Outcome == OutcomeStopped && r.Line == LineClosed }

// Late is how far behind its slot the run started, and zero when it was on
// time or asked for now.
func (r Run) Late() time.Duration {
	if r.Why != WhyLate || r.Started.IsZero() {
		return 0
	}
	return r.Started.Sub(r.Due)
}
