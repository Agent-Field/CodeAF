package session

// The `stand` tool: how a sentence in a conversation becomes something that
// keeps working after the window is closed.
//
// A person says "remind me at six", "tell me when CI goes red", "every Monday
// draft the weekly update", "keep main green", "tonight run the full suite".
// Every one of those is an ordinary sentence and NONE of them is a command:
// there is no cron syntax to learn and no scheduler screen to open, so the
// whole recognition problem lands on the model, and this tool is the one verb
// it reaches for when it recognises one. docs/AMBIENT.md is the design; the
// object it makes is internal/standing's [standing.Item].
//
// ── THE LAWS THIS FILE APPLIES ──
//
//   - NOTHING STANDS UNTIL THE PERSON SAYS YES. The tool proposes; the card is
//     answered; only then is anything created. There is no argument, no phrasing
//     and no op that arms an item without a yes.
//
//   - NO CLOCK WHILE SOMEBODY IS THERE. The card is a thing a person reads:
//     their own sentence, when it would wake, what it would cost. Reading that
//     takes as long as it takes, and a card that ended itself halfway through
//     was a card the person watched expire rather than answered. So a WATCHED
//     session sends NO deadline at all (Notice.Deadline stays zero, and the
//     surface draws no meter for a zero) and the wait ends on exactly three
//     things: they answer, the turn is interrupted, or the session closes.
//
//   - AND SILENCE STILL ARMS NOTHING. This is where the standing card parts
//     company with propose_task's, whose silence is a yes (task.go). A task is
//     bounded work somebody is watching; a standing item spends money on its
//     own, forever, at times nobody chose. A turn that ended with the card
//     unanswered therefore leaves NOTHING behind, and the model is told exactly
//     that rather than a refusal nobody made.
//
//   - AN UNWATCHED SESSION CANNOT RATIFY ONE AT ALL. A --once run, a task node,
//     a firing's own headless session: none of them has anybody to answer, and a
//     card drawn into an empty room would be a card only a clock could ever
//     answer. So the tool refuses in plain words rather than proposing there.
//     (The door does not even fill Config.Standing for those, so in practice the
//     tool is absent — this is the belt-and-braces half of the same law.)
//
//   - THE OFFER TO KEEP CHECKING WITH NO WINDOW OPEN IS MADE ONCE, EVER, and
//     the yes is journaled BEFORE the host is touched. A marker file under the
//     store root is the whole memory of it: asked, what they said, when. An
//     install that half-worked must never be an install nobody remembers asking
//     about.
//
// The tool is ABSENT when Config.Standing is nil (tools.go), which is the
// absence law this codebase is built on: a verb with nothing behind it is worse
// than no verb, because a model told it can set up a reminder will plan a whole
// reply around one.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// The rails a proposal takes when the model names none. ONE POOL, NOT N KNOBS:
// the person's real protection is the machine-wide daily allowance plus the
// audit gate, so these per-item rails are a quiet backstop, not a negotiation.
const (
	// standDefaultPerRunUSD is what one firing may spend, probe and judgment
	// included. It is [standing.DefaultPerRunUSD] and not a figure of its own,
	// because the schema this file shows the model, the charter the store
	// creates and the price the proposal quotes have to be one number.
	standDefaultPerRunUSD = standing.DefaultPerRunUSD
	// standDefaultMaxPerDay is how many times an item may fire in a local
	// day.
	standDefaultMaxPerDay = 10
)

// standingPastGrace is how far behind the clock a named moment may be and still
// be taken as meant for now.
//
// A REMINDER CAN NEVER BE SET FOR A MOMENT THAT HAS PASSED, which is the law
// [standingAtMoment] enforces; this is the only slack in it. A model works its
// stamp out from the `Now` line and calls a beat later, so a moment landing a
// few seconds behind the clock is arithmetic that was right when it was done —
// firing that immediately is what the person asked for. Half a minute is wide
// enough for that and nowhere near wide enough to swallow a mistake: the defect
// this bound exists for was a stamp TWO HOURS behind.
const standingPastGrace = 30 * time.Second

// standingWatchOffer is the marker file that remembers the one-time NOTICE:
// background checks are on, said once, ever. It lives under the store root
// beside the items rather than in the profile, because it is a fact about THIS
// store — a machine whose standing folder was thrown away has been told
// nothing.
//
// The name is the one the file has always had, and it is left alone: renaming
// it would say the sentence again to everybody who already heard the question
// it used to remember.
const standingWatchOffer = "watch-offer.json"

// standingWatchAnswer is that marker's whole content. It is journaled BEFORE
// [standing.Watch.Install] is called, so a person whose launchd would not take
// the file is somebody this build knows it has already spoken to — rather than
// somebody it tells again tomorrow.
//
// Told separates the two eras. Asked and Answer are what the one-time QUESTION
// wrote — keep checking when no window is open? — and a marker without Told is
// somebody who answered it; Told says this build put the timer on and said so,
// which is a different thing to have happened to a person and worth being able
// to tell apart in a folder somebody is reading a year from now.
type standingWatchAnswer struct {
	Asked  bool      `json:"asked"`
	Answer bool      `json:"answer"`
	Told   bool      `json:"told"`
	At     time.Time `json:"at"`
}

// The one dim line the conversation says the first time anything ever stands,
// and its honest other half.
//
// THEY ARE THE WHOLE OF WHAT IS SAID ABOUT THIS, EVER. The timer is installed
// without asking, so the person is owed the fact and the switch in one line
// they cannot miss and never have to read twice — and the cadence in it is
// [standing.IntervalWords] rather than a typed figure, because a sentence that
// spelled out five minutes would be the second place that number lived.
var (
	standingBackgroundLine = "checks every " + standing.IntervalWords() +
		", window or not · background checks under /settings"
	standingBackgroundFailed = "could not install the background check · "
	standingBackgroundWhere  = " · background checks under /settings"
)

// standingBackgroundUpdate is the update word that line travels under. It is
// the ONE piece of news that is not about an item — the item is only what
// occasioned it — and internal/tui3 draws it as the bare sentence with no
// glyph and no name in front of it (standing.go's [standUpdateRow]).
const standingBackgroundUpdate = "background"

// standingStore is the slice of [standing.Store] this file uses, named as an
// interface so a test can watch what a ratified card writes. *standing.Store
// satisfies it; nothing else in this build does.
//
// It is DELIBERATELY NARROW. Every path, every lock and every file format is
// internal/standing's business and stays there; what a conversation needs is to
// make one, rewrite one, read one and list them.
type standingStore interface {
	Create(standing.Item) (standing.Item, error)
	Save(standing.Item) error
	Get(id string) (standing.Item, error)
	ForWorkspace(workspace string) ([]standing.Item, error)
	Root() string
	// ExchangeDir is where a home-made item's origin exchange ends up. It is
	// asked for rather than assembled here for the reason the rest of this
	// interface exists: every path under the store root is internal/standing's
	// business, and a second answer to one of them would be a second truth.
	ExchangeDir(id string) string
}

// standingItems answers which store this agent writes through: the test's fake
// when one was handed over, and the door's real one otherwise. A nil answer is
// a session with no ambient side at all, and the tool is not on its belt.
func (a *Agent) standingItems() standingStore {
	if a.config.standingItems != nil {
		return a.config.standingItems
	}
	if a.config.Standing == nil || a.config.Standing.Store == nil {
		return nil
	}
	return a.config.Standing.Store
}

// standingWatch is this machine's timer, or nil where there is none. It is the
// same object the first ratified item installs and the same one the settings
// row turns, said once here so those two can never come to hold different
// timers.
func (a *Agent) standingWatch() standing.Watch {
	if a.config.Standing == nil || a.config.Standing.Watch == nil {
		return nil
	}
	return a.config.Standing.Watch
}

// standDescription is what the model reads before it calls, and most of it is
// RECOGNITION rather than mechanics: the tool is useless unless the model
// notices that an ordinary sentence was a standing one, and nothing else in
// this build watches for those words.
//
// AND RECOGNITION IS A TEST, NOT A WORD LIST. This description used to hand the
// model a bag of trigger words — "whenever", "every", "from now on", "make sure"
// — and a bag of words is a matcher a model runs instead of thinking: it caught
// "make sure this website you're building is 3 pages" and proposed a standing
// order for an acceptance criterion, which is the worst failure this tool has,
// because a card the person did not want teaches them to distrust every card
// after it. So what it carries now is the reasoning — the discharge test, what
// anchors a sentence to today's work, what separates a waking kind from a hold,
// and what to do when the answer is genuinely unclear — with a handful of
// canonical examples to calibrate it and nothing to pattern-match on.
//
// AND IT IS WRITTEN FOR DENSITY, BECAUSE THIS STRING IS BILLED ON EVERY REQUEST
// OF EVERY TURN. The belt's schemas ride in front of each request the model
// makes — dozens of them in one task — so a paragraph here is paid dozens of
// times while this comment is free. So the reasoning stays and the repetition
// went: the last paragraph used to walk every argument a second time, and every
// one of those sentences is now said ONCE, in the schema field it governs.
var standDescription = "Set up something that keeps working after this window is closed — a reminder, a watch on the world, a rule, or work that runs overnight — and manage the ones that already stand. THE PERSON NEVER NAMES THIS TOOL; you recognise it from what their sentence IS.\n\n" +
	"THE DISCHARGE TEST decides it. Can this sentence be satisfied once and then forgotten? If it CAN, it is part of the work in front of you — an acceptance criterion, an instruction — and it does NOT stand, whatever words it is dressed in and even when it says \"make sure\": \"make sure this website you are building is 3 pages\" is discharged the moment the site has three pages. If it can NEVER be discharged — if work nobody has done yet could violate it tomorrow — it is standing: \"make sure the tests never break\".\n\n" +
	"ANCHORING. A sentence about the artifact under construction RIGHT NOW binds the current work, whatever verbs it uses, and what anchors it is the GRAMMAR: a demonstrative pointing at the thing in front of you (\"this website you're building\"), or a present tense about work already under way (\"what you're doing\"). An \"always\", a \"never\" or an \"ensure\" inside such a sentence is EMPHASIS ON THIS WORK — a quality bar for the thing being built is acceptance, and acceptance is never a card.\n\n" +
	"WAKING OR HOLDING. A standing sentence that names a moment, a rhythm or a condition gets the waking kind it names (\"remind me at 6\" is at, \"every Monday draft the update\" is every, \"tell me when CI goes red\" is probe, \"tonight run the suite\" is idle). One that names none of them — a rule, a convention, a preference — is when.kind hold.\n\n" +
	"UNSURE MEANS INSTRUCTION PLUS AN OFFER. When the discharge test is genuinely unclear, bind the sentence to the work in front of you AND offer the standing version in one line of prose at the end of your reply. NEVER a card on a guess: a card they did not want costs their trust in every card after it.\n\n" +
	"Doing a standing sentence once instead of proposing it answers a request they did not make: \"run the tests\" is work you do now, \"run the tests whenever I push\" is one of these. The `watch` tool is the near neighbour that is NOT this: a watch is a job inside this conversation and stops the moment the window closes, so anything that has to keep looking after they walk away belongs here and never there.\n\n" +
	"Nothing stands until the person says yes: the card waits for them with no clock on it, and a session nobody is watching cannot set one up at all. Money is not yours to negotiate — omit rails and cost_words unless they named a limit. op=list shows what already stands here; op=pause, op=resume and op=stop take an id or the person's own words, and stop is permanent. op=change is not yours to call — it is what the card answers when they want it different."

var standSchemaJSON = `{"type":"object","properties":{` +
	`"op":{"type":"string","enum":["propose","list","pause","resume","stop","change"],"description":"propose a new one, list what stands here, or pause, resume or stop one that does."},` +
	`"words":{"type":"string","description":"THE PERSON'S OWN SENTENCE, verbatim, never a paraphrase: every card, row and note leads with it. On pause, resume and stop it names an item instead of its id."},` +
	`"when":{"type":"object","description":"What wakes it. Only the fields this kind names are read.","properties":{` +
	`"kind":{"type":"string","enum":["at","every","file","idle","probe","hold"],"description":"at: once at a moment, then it retires. every: a rhythm. file: a glob changing. idle: the machine quiet a while. probe: a look at the world judged against the person's words. hold: NEVER WAKES and so can never spend — the kind for a rule, a convention or a preference, a sentence with no moment, rhythm or condition in it; it rides automatically into the world of every conversation and task it reaches, which is how it is kept."},` +
	`"at":{"type":"string","description":"The one moment of an at, a local RFC3339 stamp (\"2026-08-20T18:00:00+01:00\"). Work it out from the Now line in your instructions; NEVER shell out to read a clock. A moment ALREADY PASSED is refused, and the refusal says the time now — recompute from that, not from the Now line you already used. For a relative moment send in."},` +
	`"in":{"type":"string","description":"An at's moment as a distance from RIGHT NOW: a Go duration (\"2m\", \"1h30m\"). aforge resolves it at the instant you call and answers with the moment it landed on. Send at or in, never both."},` +
	`"every":{"type":"string","description":"An every's rhythm: a five-field cron line (\"0 9 * * 1\") or a Go duration of at least a minute (\"20m\", \"2h\")."},` +
	`"glob":{"type":"string","description":"A file watch's pattern, relative to the project."},` +
	`"idle_for":{"type":"string","description":"How quiet the machine must have been for an idle item: a Go duration (\"45m\")."},` +
	`"probe":{"type":"object","description":"One look at the world: EXACTLY ONE of a shell command or a belt tool with arguments.","properties":{` +
	`"command":{"type":"string","description":"A shell command run in the project, whose output the judgment reads."},` +
	`"tool":{"type":"string","description":"A tool on your belt to call instead, including one a connected account brought."},` +
	`"args":{"type":"object","description":"That tool's arguments."}` +
	`},"additionalProperties":false},` +
	`"probe_every":{"type":"string","description":"How often to take that look, a Go duration. Defaults to how often anything is checked."},` +
	`"hint":{"type":"string","description":"What a yes looks like, for the cheap judgment that reads the probe's output: \"yes when any run on main shows conclusion=failure\"."}` +
	`},"additionalProperties":false},` +
	`"does":{"type":"object","description":"What a firing does. Every waking kind needs one; a hold takes NONE, and sending one with a hold is refused.","properties":{` +
	`"kind":{"type":"string","enum":["say","task"],"description":"say delivers one line to the person: into this conversation when it is open, else whichever conversation of this project they are in, else waiting on home and in the next one they open. task runs a brief in its own session, with a worktree and a cost row, the way propose_task's work runs."},` +
	`"say":{"type":"string","description":"The line to deliver. {{evidence}} in it is replaced by what the probe found."},` +
	`"brief":{"type":"string","description":"THE WORK, self-contained as propose_task's brief is: nobody will be there to ask. {{evidence}} is replaced by what the probe found."},` +
	`"acceptance":{"type":"string","description":"How anybody checks the work is done."},` +
	`"model":{"type":"string","description":"Model for the work, only when the person named one."},` +
	`"max_steps":{"type":"integer","description":"Tool calls one firing's work may take (default ` + strconv.Itoa(standingRunSteps) + `)."}` +
	`},"additionalProperties":false},` +
	`"rails":{"type":"object","description":"Optional quiet backstops. Name money only when the person did; otherwise the card quotes the machine-wide daily allowance. A hold takes none — it never wakes, so it never spends. Only expires means anything on one.","properties":{` +
	`"per_run_usd":{"type":"number","description":"The most one firing may spend, judgment included. Send only when they named a per-run limit; otherwise it quietly defaults to ` + strconv.FormatFloat(standDefaultPerRunUSD, 'f', 2, 64) + `."},` +
	`"max_per_day":{"type":"integer","description":"Firings allowed in one local day. Send only when they named a count; otherwise it quietly defaults to ` + strconv.Itoa(standDefaultMaxPerDay) + `."},` +
	`"expires":{"type":"string","description":"Local RFC3339 stamp after which it retires. Omit for never. A stamp already gone is refused, as when.at is — and so is one at or before the item's OWN first firing, which would retire it before it ever ran: an end for a one-off has to be later than when.at to the SECOND, and a one-off needs none at all, since it retires the moment it fires."}` +
	`},"additionalProperties":false},` +
	`"when_words":{"type":"string","description":"The cadence said back plainly — \"Mondays at 9am\". The card quotes this and never the spec, so never cron."},` +
	`"cost_words":{"type":"string","description":"When the person named money, quote their limit in their words — \"at most a dollar a run\". Omit when they named none; aforge quotes the shared allowance."},` +
	`"guessed":{"type":"boolean","description":"True when YOU invented the cadence because they gave none. The card then asks rather than states."},` +
	`"altitude":{"type":"string","enum":["conversation","project","machine"],"description":"HOW FAR IT REACHES, and the card always names it. conversation: this chat alone, dying with it. project: every conversation and task here. machine: everything they do on this computer. THEIR OWN SCOPE WORDS CHOOSE IT — \"just this chat\" is conversation, \"everywhere\" and \"all my projects\" are machine. Omit it when they said nothing about scope: widening it on your own judgment decides on their behalf."},` +
	`"title":{"type":"string","description":"Three or four words for a row too narrow for their sentence — \"weekly update\". Their sentence still leads every screen."},` +
	`"grant":{"type":"string","description":"One sentence, in their words, for what acting on this may do without asking — \"open a pull request but never merge it\". Send it only when they said something like it; with none, it may only tell them things."},` +
	`"id":{"type":"string","description":"Which item pause, resume and stop are about. Their own words work too."}` +
	`},"required":["op"],"additionalProperties":false}`

// standArguments is the wire form.
type standArguments struct {
	Op        string `json:"op"`
	Words     string `json:"words"`
	WhenWords string `json:"when_words"`
	CostWords string `json:"cost_words"`
	Guessed   bool   `json:"guessed"`
	ID        string `json:"id"`
	Altitude  string `json:"altitude"`
	Title     string `json:"title"`
	Grant     string `json:"grant"`
	When      struct {
		Kind    string `json:"kind"`
		At      string `json:"at"`
		In      string `json:"in"`
		Every   string `json:"every"`
		Glob    string `json:"glob"`
		IdleFor string `json:"idle_for"`
		Probe   struct {
			Command string          `json:"command"`
			Tool    string          `json:"tool"`
			Args    json.RawMessage `json:"args"`
		} `json:"probe"`
		ProbeEvery string `json:"probe_every"`
		Hint       string `json:"hint"`
	} `json:"when"`
	Does struct {
		Kind       string `json:"kind"`
		Say        string `json:"say"`
		Brief      string `json:"brief"`
		Acceptance string `json:"acceptance"`
		Model      string `json:"model"`
		MaxSteps   int    `json:"max_steps"`
	} `json:"does"`
	Rails struct {
		PerRunUSD *float64 `json:"per_run_usd"`
		MaxPerDay *int     `json:"max_per_day"`
		Expires   string   `json:"expires"`
	} `json:"rails"`
}

// standingTools is the belt's ambient family — one tool, present only where
// there is a store behind it (tools.go).
func (a *Agent) standingTools() []bare.Tool {
	if a.standingItems() == nil {
		return nil
	}
	return []bare.Tool{{
		Name:        "stand",
		Description: standDescription,
		Schema:      json.RawMessage(standSchemaJSON),
		Execute:     a.standTool,
	}}
}

// standTool is the belt's entry point, and it is [Agent.standDispatch] with the
// CLOCK STAMPED ON THE ANSWER.
//
// EVERY RESULT OF THIS TOOL ENDS WITH THE TIME, whichever op it was and whether
// it worked. This is the one tool whose whole subject is WHEN, and it is called
// at the exact moment the model most needs the real clock rather than the
// minute its instructions opened with — a `Now` line is at most [clockRefresh]
// old (prompt.go), and a model that has just been told a moment has already
// passed has to recompute from something. So it is told, here, in the shape the
// prompt uses, on a line of its own.
func (a *Agent) standTool(ctx context.Context, args json.RawMessage) (string, bool, error) {
	out, failed, err := a.standDispatch(ctx, args)
	if err != nil {
		return out, failed, err
	}
	return standingWithNow(out, time.Now()), failed, nil
}

// standingWithNow is that line, appended.
func standingWithNow(out string, now time.Time) string {
	out = strings.TrimRight(out, "\n")
	if out != "" {
		out += "\n"
	}
	return out + "now: " + standingClock(now)
}

// standingClock is a moment as this tool and its refusals spell one: the local
// time to the minute and the numeric offset, which is exactly what a model
// needs to write an RFC3339 stamp back. It is the `Now` line's own shape minus
// the date (prompt.go's [nowLine]), because the date is carried in the one
// place a date matters — the refusal that says what day it now is.
func standingClock(moment time.Time) string { return moment.Format("15:04 -07:00") }

// standingPassed is the refusal a moment already gone earns, in the model's own
// grammar and carrying THE CURRENT TIME so the recomputation needs no second
// call. The tail is the field's own, because "send when.in instead" is advice
// about a reminder and not about an expiry.
func standingPassed(field string, moment, now time.Time, tail string) string {
	return "Invalid arguments: " + field + " " + standingClock(moment) +
		" has already passed — it is now " + standingClock(now) +
		" (" + now.Format("Monday 2006-01-02") + "). " + tail
}

// standingClockExact is [standingClock] TO THE SECOND, and it exists for one
// refusal only.
//
// The minute is the right grain everywhere else, because a person names minutes
// and a model writes them back. It is the wrong grain for [standingRetires],
// where the whole mistake can live inside one minute: a model that wrote
// `in 1 minute — 23:11` for the words and `23:11` for the end, against a moment
// the engine resolved to 23:11:11, would otherwise be told that 23:11 is not
// after 23:11 and have nothing to work with.
func standingClockExact(moment time.Time) string { return moment.Format("15:04:05 -07:00") }

// standingRetires is the refusal an expiry earns for standing BEFORE the thing
// it is supposed to outlive — the same law [standingPassed] states, applied to
// the item's own moment rather than to the clock.
//
// It is one sentence in that refusal's grammar: the field, what is wrong with
// it, and the way out. `firing` is what the item's first waking would be and
// `named` is how to say that in the model's own vocabulary, since `when.at` is
// a field it can go and edit while a rhythm's first firing is only a
// consequence of one.
func standingRetires(moment, firing time.Time, named, tail string) string {
	return "Invalid arguments: rails.expires " + standingClockExact(moment) +
		" is not after " + named + " " + standingClockExact(firing) +
		", so it would retire before it ever fired. " + tail
}

// standDispatch dispatches the six ops. Everything it can answer badly is an
// ordinary tool result rather than a Go error, the way every other tool on this
// belt answers: a card the model shaped wrongly is a card it can shape again.
func (a *Agent) standDispatch(ctx context.Context, args json.RawMessage) (string, bool, error) {
	var parsed standArguments
	if len(args) > 0 {
		if err := decodeToolArguments(args, &parsed); err != nil {
			return "Invalid arguments: " + err.Error(), true, nil
		}
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Op)) {
	case "propose":
		return a.standPropose(ctx, parsed)
	case "list":
		return a.standList()
	case "pause":
		return a.standSetStatus(parsed, standing.StatusPaused)
	case "resume":
		return a.standSetStatus(parsed, standing.StatusActive)
	case "stop":
		return a.standSetStatus(parsed, standing.StatusRetired)
	case "change":
		// The person's own word for "not like that". It is answered by the CARD
		// and never by a call, so a model that reached for it here is being told
		// where the door actually is rather than being failed.
		return "change is what the person's card answers, not an op you call. Propose it again with what they corrected.", true, nil
	case "":
		return "Invalid arguments: op is required — propose, list, pause, resume or stop", true, nil
	default:
		return "Invalid arguments: no op called " + strconv.Quote(parsed.Op) + " — propose, list, pause, resume or stop", true, nil
	}
}

// ── proposing ───────────────────────────────────────────────────────────────

// standPropose builds the item, puts the card in front of the person, and does
// exactly what they said.
func (a *Agent) standPropose(ctx context.Context, parsed standArguments) (string, bool, error) {
	store := a.standingItems()
	if store == nil {
		return "there is nothing here to set one up with", true, nil
	}
	// ONE READING OF THE CLOCK FOR THE WHOLE CALL. `when.in`, the refusal of a
	// moment that has passed and the refusal of an expiry that has passed all
	// measure against the SAME instant; two readings a microsecond apart would
	// be two answers to one question in a function whose whole subject is when.
	item, problem := a.standingItem(parsed, time.Now())
	if problem != "" {
		return problem, true, nil
	}
	// VALIDATED BEFORE ANYBODY IS ASKED. The admission law is [standing.Item]'s
	// own, and a card whose yes could only fail is worse than a refusal the
	// model can act on this turn.
	if err := item.Validate(); err != nil {
		return "Invalid arguments: " + err.Error(), true, nil
	}

	notice := StandingNotice{
		Item: item,
		// ONE SOURCE OF TRUTH FOR THE CADENCE. The card, the item and the line
		// this tool answers with all read [standing.When.Words], which is the
		// model's own when_words or — when it sent none and the moment was
		// worked out from a duration — the moment the engine landed on.
		WhenWords: item.When.Words,
		CostWords: a.standingCostWords(item, parsed),
		Guessed:   parsed.Guessed,
		// AND THE ENGINE SAYS WHICH ANSWERS THIS CARD HAS. Both surfaces draw
		// from this one list, so `once, not standing` is absent from a one-off
		// reminder's card everywhere at once (answers.go's [StandingOptions]).
		Options: StandingOptions(item),
	}
	answer, err := a.askStanding(ctx, &notice)
	if err != nil {
		switch {
		case errors.Is(err, errStandingUnwatched):
			return "nobody is here to say yes — this can only be set up in a conversation", true, nil
		case errors.Is(err, errStandingUnanswered):
			// THE TURN ENDED WITH THE CARD STILL UP, and it is said as exactly
			// that. "They said no" would be this tool putting a sentence in
			// somebody's mouth that they did not say, and "it declined on the
			// clock" would describe a clock this build does not run.
			return "the card was left unanswered — nothing was set up", false, nil
		}
		return "the card was never answered: the turn ended first", true, nil
	}
	switch {
	case answer.Once:
		// Nothing is created and nothing is scheduled. The person wanted the
		// action, not the arrangement, so the model does it here.
		return "do it once, now, as an ordinary turn — nothing stands. Nothing was set up.", false, nil
	case !answer.Approved:
		if correction := strings.TrimSpace(answer.Change); correction != "" {
			// AND THE CORRECTION MAY BE ABOUT ANY OF IT. The card's one change
			// door covers when it wakes and how far it reaches alike (tui3's
			// [standChangeWord]), so a result that named only the cadence would
			// have the model re-proposing the same reach it was just corrected
			// on.
			return "the person changed it: " + correction + "\nNothing stands yet. Propose it again with that — it may be about when it wakes, how far it reaches, or the words themselves.", false, nil
		}
		return "nothing was set up: the person said no.", false, nil
	}

	created, err := store.Create(item)
	if err != nil {
		// SAID PLAINLY AND NOT SWALLOWED. The person answered yes to a card, so
		// the one thing that must never happen is the conversation carrying on
		// as though something now stands.
		return "nothing was set up: " + err.Error(), true, nil
	}
	created = a.standingFileTheExchange(store, created)
	a.emitStandingUpdate("stood", created, "")
	// AND THE FIRST THING THAT EVER STANDS TURNS THE BACKGROUND CHECKS ON. It
	// is said to the person and not to the model: the line goes on the screen
	// as its own dim row, and the model's whole reply is still the one sentence
	// about what now stands ([standingRatifiedLine]).
	a.standingBackgroundOn(store, created)
	line := fmt.Sprintf("set up %s: %s", created.ID, created.Words)
	if when := strings.TrimSpace(notice.WhenWords); when != "" {
		line += "\nit wakes: " + when
	}
	line += "\n" + standingRatifiedLine
	return line, false, nil
}

// standingRatifiedLine is what a model is told the instant something stands,
// and it is the WHOLE of what it may say next.
//
// "Say in ONE line what now stands and what it costs" is what it used to say,
// and a real model read that as a rule about the good part of the answer: it
// replied "The person wants a reminder in 1 minute. This is a `stand` with
// op=propose, when.kind=at…" and then the sentence. A person sitting in the
// conversation got the machinery vocabulary this codebase bans in anything
// anybody reads — said by the model, where no surface could scrub it. So the
// instruction now states what the whole reply is rather than how long its best
// part should be.
const standingRatifiedLine = "They answered the card. Your WHOLE reply is one short line saying what now stands and what it costs: no preamble, no working out, no naming this tool or its arguments, and do not ask again."

// standingItem turns one call into the object internal/standing keeps. Every
// refusal it can make is in the person's grammar rather than the schema's,
// because the model is the only reader and it has to fix the call.
func (a *Agent) standingItem(parsed standArguments, now time.Time) (standing.Item, string) {
	words := strings.TrimSpace(parsed.Words)
	if words == "" {
		return standing.Item{}, "Invalid arguments: words is required — the person's own sentence, verbatim"
	}
	when, problem := standingWhen(parsed, now)
	if problem != "" {
		return standing.Item{}, problem
	}
	does, problem := standingDoes(parsed, when.Kind)
	if problem != "" {
		return standing.Item{}, problem
	}
	rails, problem := standingRails(parsed, when, now)
	if problem != "" {
		return standing.Item{}, problem
	}
	// THE PERSON'S CADENCE, SAID BACK, WINS OVER ANYTHING THE ENGINE WORKED
	// OUT. when_words is the model's plain-words reading of what they asked
	// for; the only time it is not the answer is when there is none, and then
	// whatever [standingWhen] echoed stands (a resolved `in`, or nothing).
	//
	// A HOLD HAS NO CADENCE TO SAY BACK. A rule is not due at any time, so a
	// `when ·` band under one would be the card reading a rhythm into the word
	// "always" — and every surface afterwards would quote it as the moment this
	// thing wakes up.
	if words := strings.TrimSpace(parsed.WhenWords); words != "" && when.Kind != standing.WhenHold {
		when.Words = words
	}
	return standing.Item{
		Schema:    standing.Schema,
		Words:     words,
		Workspace: a.standingWorkspace(),
		Origin:    a.standingOrigin(),
		When:      when,
		Does:      does,
		Rails:     rails,
		Altitude:  a.standingAltitude(parsed.Altitude),
		// THE BRIEF IS HALF WRITTEN IN THIS WAVE, and honestly so: a title for a
		// row too narrow for a sentence is something the model can write at
		// proposal time, and the compiled prompt is not — nothing follows one
		// yet, so nothing pretends to have one and [standing.Item.Prompt] reads
		// as the person's own words.
		Brief: standing.Brief{Title: strings.TrimSpace(parsed.Title)},
		Grant: strings.TrimSpace(parsed.Grant),
	}, ""
}

// standingAltitude is the reach the card will name.
//
// ALTITUDE IS DECIDED ON THE CARD AND NEVER GUESSED SILENTLY, so what the model
// sends is what stands, and a word that is not one of the three is refused by
// [standing.Item.Validate] before anybody is asked — there is no second list of
// them here.
//
// WHERE IT WAS SAID IS THE DEFAULT. A sentence said in a project is about that
// project, which is what every item made before altitudes were spelled already
// was; a sentence said in a conversation that belongs to no project at all is
// about the person, and this build already files those under their home
// directory ([Agent.standingWorkspace]) — so home IS the machine-wide
// convention, asked here rather than invented.
func (a *Agent) standingAltitude(raw string) standing.Altitude {
	if named := standing.Altitude(strings.ToLower(strings.TrimSpace(raw))); named != "" {
		return named
	}
	if house, err := os.UserHomeDir(); err == nil && strings.TrimSpace(house) != "" &&
		filepath.Clean(house) == filepath.Clean(a.standingWorkspace()) {
		return standing.AltitudeMachine
	}
	return standing.AltitudeProject
}

// standingKindWords is the closed list of shapes, said once, in the grammar the
// refusals use. It is ONE STRING because two spellings of a closed list is one
// of them forgetting the day a shape is added — which is exactly what happened
// when `hold` arrived and both refusals still offered five.
const standingKindWords = "at, every, file, idle, probe or hold"

func standingWhen(parsed standArguments, now time.Time) (standing.When, string) {
	when := standing.When{
		Kind: standing.WhenKind(strings.ToLower(strings.TrimSpace(parsed.When.Kind))),
		Hint: strings.TrimSpace(parsed.When.Hint),
	}
	switch when.Kind {
	case standing.WhenAt:
		moment, echo, problem := standingAtMoment(parsed.When.At, parsed.When.In, now)
		if problem != "" {
			return when, problem
		}
		when.At = moment
		// THE ECHO IS A FALLBACK AND NEVER AN OVERRIDE. [Agent.standingItem]
		// puts the model's own when_words over the top of this when it sent
		// any; what is left here is the case it sent none, where a card reading
		// "in 2 minutes — 06:54" is the difference between a person checking a
		// stamp and a person reading a sentence.
		when.Words = echo
	case standing.WhenEvery:
		when.Every = strings.TrimSpace(parsed.When.Every)
		if when.Every == "" {
			return when, "Invalid arguments: when.every is required for a rhythm — a cron line or a duration"
		}
	case standing.WhenFile:
		when.Glob = strings.TrimSpace(parsed.When.Glob)
	case standing.WhenIdle:
		idle, err := time.ParseDuration(strings.TrimSpace(parsed.When.IdleFor))
		if err != nil {
			return when, "Invalid arguments: when.idle_for is a duration like \"45m\""
		}
		when.IdleFor = idle
	case standing.WhenHold:
		// NOTHING WAKES IT, SO THERE IS NOTHING HERE TO GET WRONG. A hold's whole
		// content is the person's sentence and how far it reaches; the moment, the
		// rhythm, the glob and the probe are all fields about waking, and a rule
		// has no waking to describe.
	case standing.WhenProbe:
		when.Probe = standing.Probe{
			Command: strings.TrimSpace(parsed.When.Probe.Command),
			Tool:    strings.TrimSpace(parsed.When.Probe.Tool),
			Args:    parsed.When.Probe.Args,
		}
		// A LOOK WITH NO CADENCE IS TAKEN WHEN ANYTHING IS. The pass has one
		// rhythm of its own; an item that named none simply rides it, which is
		// the honest reading of "keep an eye on this" and not a guess.
		when.ProbeEvery = standing.Interval
		if every := strings.TrimSpace(parsed.When.ProbeEvery); every != "" {
			parsedEvery, err := time.ParseDuration(every)
			if err != nil {
				return when, "Invalid arguments: when.probe_every is a duration like \"10m\""
			}
			when.ProbeEvery = parsedEvery
		}
	case "":
		return when, "Invalid arguments: when.kind is required — " + standingKindWords
	default:
		return when, "Invalid arguments: no when called " + strconv.Quote(string(when.Kind)) + " — " + standingKindWords
	}
	return when, ""
}

// standingDoes is what a firing does, and it takes the kind that wakes it
// because ONE SHAPE HAS NO FIRING. A hold never wakes, so there is no moment for
// an action to be the content of; every other kind must say what it does.
func standingDoes(parsed standArguments, wakes standing.WhenKind) (standing.Action, string) {
	does := standing.Action{
		Kind:       standing.ActionKind(strings.ToLower(strings.TrimSpace(parsed.Does.Kind))),
		Say:        strings.TrimSpace(parsed.Does.Say),
		Brief:      strings.TrimSpace(parsed.Does.Brief),
		Acceptance: strings.TrimSpace(parsed.Does.Acceptance),
		Model:      strings.TrimSpace(parsed.Does.Model),
		MaxSteps:   parsed.Does.MaxSteps,
	}
	if wakes == standing.WhenHold {
		// AND AN ACTION SENT WITH A HOLD IS REFUSED RATHER THAN DROPPED. A model
		// that asked for a rule AND a line to say meant one of the two, and
		// standing something up with an action nothing will ever run would leave
		// the person holding a card whose promise cannot be kept.
		if does.Kind != "" {
			return standing.Action{}, "Invalid arguments: a hold does nothing — it holds. Leave does out, or give it a when that wakes."
		}
		return standing.Action{}, ""
	}
	switch does.Kind {
	case standing.ActionSay, standing.ActionTask:
	case "":
		return does, "Invalid arguments: does.kind is required — say or task"
	default:
		return does, "Invalid arguments: no action called " + strconv.Quote(string(does.Kind)) + " — say or task"
	}
	if does.MaxSteps < 0 {
		return does, "Invalid arguments: does.max_steps cannot be negative"
	}
	return does, ""
}

// standingRails fills what the model left out. THE DEFAULTS ARE THIS FILE'S
// CONSTANTS and never a second set of numbers: the schema quotes them and the
// item is created with them.
//
// A HOLD IS THE ONE SHAPE THAT GETS NONE OF THEM. It cannot spend
// ([standing.Item.Spends]), so a budget written onto it would be a number
// nothing ever reads and every surface would still have to decide not to print.
// An END is different and is kept: "never touch the public API until the release
// lands" is a rule with a last day, and the pass retires it on that day.
//
// IT TAKES THE WHOLE `when` AND NOT ONLY ITS KIND, because an end is a claim
// about the item's own life and cannot be judged without the moment that life
// starts at — see the second refusal below.
func standingRails(parsed standArguments, when standing.When, now time.Time) (standing.Rails, string) {
	rails := standing.Rails{}
	if when.Kind != standing.WhenHold {
		if parsed.Rails.PerRunUSD == nil {
			rails.PerRunUSD = standDefaultPerRunUSD
		} else {
			rails.PerRunUSD = *parsed.Rails.PerRunUSD
		}
		if parsed.Rails.MaxPerDay == nil {
			rails.MaxPerDay = standDefaultMaxPerDay
		} else {
			rails.MaxPerDay = *parsed.Rails.MaxPerDay
		}
	}
	if expires := strings.TrimSpace(parsed.Rails.Expires); expires != "" {
		moment, err := standingMoment(expires)
		if err != nil {
			return rails, "Invalid arguments: rails.expires " + err.Error()
		}
		// AND AN EXPIRY ALREADY GONE RETIRES THE ITEM BEFORE IT EVER FIRES, so
		// it is refused for [standingAtMoment]'s reason and with its wording: a
		// card answered yes that stood something up already dead is the worst
		// of both endings.
		if moment.Before(now.Add(-standingPastGrace)) {
			return rails, standingPassed("rails.expires", moment, now,
				"Work it out from that time, or leave it out for something that never expires.")
		}
		// AND AN EXPIRY STILL IN THE FUTURE DOES THE SAME DAMAGE WHEN IT STANDS
		// BEFORE THE ITEM'S OWN FIRST FIRING, which is the half of this law that
		// was missing (issue #188). The pass asks about the end FIRST, as rail
		// one of internal/standing/tick.go's [Ticker.one], before anything can
		// be due — so an end at or before the moment retires the item by every
		// road there is and no clock could ever have delivered it. A person who
		// answered yes to a card for a one-off reminder then waits for something
		// that was already dead when they said so, and the only trace is a line
		// in a log nobody reads: the worst of both endings again, and refused
		// for the same reason.
		//
		// The defect this pins is exact arithmetic and not a slip: the model
		// wrote `in 1 minute — 23:11` for the words and took `23:11` for the end
		// from the same words, while the engine resolved the moment to
		// 23:11:11 — eleven seconds later. So the refusal is spelled to the
		// SECOND, or it would read as a moment that is not after itself.
		if firing, named, has := standingFirstFiring(when, now); has && !moment.After(firing) {
			tail := "Put it after that moment, or leave it out for something that never expires."
			if when.Kind == standing.WhenAt {
				tail = "Put it after that moment, or leave it out — a one-off retires as it fires and needs no end at all."
			}
			return rails, standingRetires(moment, firing, named, tail)
		}
		rails.Expires = moment
	}
	return rails, ""
}

// standingFirstFiring is the earliest moment an item could ever wake, for the
// two shapes whose first waking is arithmetic rather than a fact about the
// world, together with the name the refusal should call it by.
//
// THE OTHER SHAPES HAVE NO ANSWER HERE AND ARE NOT GUESSED AT. A file watch, an
// idle watch and a probe wake when the world does something, which may be in a
// second or never, so there is no moment an end could be measured against and
// the only honest check on those is the one against the clock. A hold never
// wakes at all, and an end on one is the whole point of keeping it: "never
// touch the public API until the release lands" retires on the release day.
//
// A rhythm whose spec this cannot read answers nothing rather than a refusal of
// its own, because [standing.Item.Validate] is what rejects an unreadable
// rhythm and two refusals for one mistake would send the model to fix the wrong
// field.
func standingFirstFiring(when standing.When, now time.Time) (moment time.Time, named string, has bool) {
	switch when.Kind {
	case standing.WhenAt:
		if when.At.IsZero() {
			return time.Time{}, "", false
		}
		return when.At, "when.at", true
	case standing.WhenEvery:
		next, err := standing.ParseEvery(when.Every)
		if err != nil {
			return time.Time{}, "", false
		}
		// The pass reads a rhythm's first due the same way, from the clock at
		// the moment it first sees the item (tick.go's WhenEvery arm), so this
		// is that item's own first firing and not a second opinion about it.
		return next(now), "its first firing", true
	}
	return time.Time{}, "", false
}

// standingCostWords keeps person-named money word for word and otherwise says
// the one allowance that protects every standing item on this machine.
//
// A HOLD SAYS NOTHING ABOUT MONEY, which is the emptiness law reaching a whole
// band of the card: nothing wakes a rule, so nothing about it is ever bought,
// and quoting the day's allowance under one would be asking a person to weigh a
// figure that can never be drawn on ([standing.Item.Spends]).
func (a *Agent) standingCostWords(item standing.Item, parsed standArguments) string {
	if !item.Spends() {
		return ""
	}
	if parsed.Rails.PerRunUSD != nil || parsed.Rails.MaxPerDay != nil {
		return strings.TrimSpace(parsed.CostWords)
	}
	if a.config.Standing != nil && a.config.Standing.DailyRailUSD > 0 {
		return "shares the day's $" + strconv.FormatFloat(a.config.Standing.DailyRailUSD, 'f', 2, 64) + " allowance"
	}
	return "shares the day's allowance"
}

// standingAtMoment answers the one moment of an `at`, from either of the two
// ways a model may say it.
//
// A STAMP IS ONE ANSWER AND A DURATION IS THE OTHER, and there is never a third
// road out of this function: two answers to one question are refused rather
// than reconciled, because picking one of a disagreeing pair silently is how a
// reminder lands at the wrong hour and nobody can see why.
//
// The duration is resolved against the clock HERE, at the instant of the call,
// and not against the Now line the model was given at the top of the session
// (prompt.go's nowLine says why): a conversation that has been open for an hour
// still means two minutes from now when the person says "in two minutes".
func standingAtMoment(rawAt, rawIn string, now time.Time) (moment time.Time, echo, problem string) {
	rawAt, rawIn = strings.TrimSpace(rawAt), strings.TrimSpace(rawIn)
	switch {
	case rawAt != "" && rawIn != "":
		return time.Time{}, "", "Invalid arguments: when.at and when.in are two answers to one question — send the stamp or the duration, not both"
	case rawIn != "":
		span, err := time.ParseDuration(rawIn)
		if err != nil {
			return time.Time{}, "", "Invalid arguments: when.in is a duration like \"2m\", \"90s\" or \"1h30m\""
		}
		if span <= 0 {
			return time.Time{}, "", "Invalid arguments: when.in has to be a distance into the future"
		}
		landed := now.Add(span)
		return landed, "in " + standingSpanWords(span) + " — " + landed.Format("15:04"), ""
	}
	parsed, err := standingMoment(rawAt)
	if err != nil {
		return time.Time{}, "", "Invalid arguments: when.at " + err.Error()
	}
	// A REMINDER CAN NEVER BE SET FOR A MOMENT THAT HAS PASSED. The engine used
	// to take any stamp it could read, so a model whose `Now` line had gone
	// stale proposed 05:42 at 07:34 and this accepted it — a card the person
	// answered for a thing that could never fire. The refusal carries the
	// CURRENT time in the shape the prompt uses, because the model has to
	// recompute from something and a refusal that only says no costs another
	// call to find out what now is.
	if parsed.Before(now.Add(-standingPastGrace)) {
		return time.Time{}, "", standingPassed("when.at", parsed, now,
			`For a distance from now send when.in ("1m"); for a clock time compute it from now.`)
	}
	return parsed, "", ""
}

// standingSpanWords is a duration as somebody would say it out loud, which is
// what a card is read as. Go's own String() answers "1h30m0s", and a card that
// said that would be quoting a wire format at a person.
//
// It is deliberately coarse: whole hours and minutes down to a minute, seconds
// only under one minute, and no fractions anywhere. "in 1 hour 30 minutes" is
// the sentence; "in 1.5 hours" is arithmetic somebody has to check.
func standingSpanWords(span time.Duration) string {
	span = span.Round(time.Second)
	if span < time.Minute {
		return standingCountWords(int(span/time.Second), "second")
	}
	span = span.Round(time.Minute)
	hours, minutes := int(span/time.Hour), int(span%time.Hour/time.Minute)
	switch {
	case hours == 0:
		return standingCountWords(minutes, "minute")
	case minutes == 0:
		return standingCountWords(hours, "hour")
	}
	return standingCountWords(hours, "hour") + " " + standingCountWords(minutes, "minute")
}

// standingCountWords is "1 minute" and "2 minutes" — the plural nobody notices
// until it is wrong.
func standingCountWords(count int, unit string) string {
	if count == 1 {
		return "1 " + unit
	}
	return strconv.Itoa(count) + " " + unit + "s"
}

// standingMoment reads a stamp the way a model actually writes one: RFC3339
// first, then the two forms it reaches for when it forgets the offset. Both of
// those are read in the machine's own zone, which is the only zone a person
// saying "at six" could have meant.
func standingMoment(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, errors.New("is required — a local RFC3339 stamp")
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04"} {
		if moment, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return moment, nil
		}
	}
	return time.Time{}, errors.New("is not a moment this can read — write it as \"2026-08-20T18:00:00+01:00\"")
}

// standingWorkspace is the REAL project an item belongs to, and it is the same
// answer meta.json records ([Meta.Workspace]): the resolved root the launch
// settled on, never the subdirectory somebody happened to be standing in.
//
// AN OWNED SESSION HAS NO PROJECT, so its items are MACHINE-WIDE and belong to
// the person's home directory. "Remind me at six to leave" is not about a
// repository, and filing it under a work/ folder that the sweep may one day
// reap would be filing it where it cannot be found.
func (a *Agent) standingWorkspace() string {
	if a.config.Place.Owned {
		if dir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(dir) != "" {
			return dir
		}
	}
	if root := strings.TrimSpace(a.config.Place.Workspace); root != "" {
		return root
	}
	return strings.TrimSpace(a.config.Workspace)
}

// standingOrigin is the provenance every surface opens: the conversation that
// asked, its journal, and the turn it was asked on.
//
// THIS BUILD HAS NO TURN IDS. The journal is a flat list of messages and
// nothing numbers a turn, so the ordinal is the identity — the count of turns
// this conversation has completed, plus the one running now. It is stable for a
// session that is replayed and it is the honest answer available; a synthetic
// id minted here would be one nothing else in the build could match.
func (a *Agent) standingOrigin() standing.Origin {
	a.mu.Lock()
	turn := a.usage.Turns + 1
	id := a.sessionID()
	a.mu.Unlock()
	return standing.Origin{
		SessionID:  id,
		Transcript: strings.TrimSpace(a.config.SessionFile),
		TurnIDs:    []string{strconv.Itoa(turn)},
	}
}

// standingAskedFromHome answers whether THIS conversation is an errand said at
// home — home's `ask here` pane — rather than a project conversation.
//
// The only thing that says so is where the transcript sits: `ask here` mints
// its folder under <standing root>/exchanges/<session id>/ precisely so that
// home, which lists what is under v3/projects, can never list it (tui3's
// homeexchange.go). A session file anywhere else is an ordinary conversation.
func (a *Agent) standingAskedFromHome(store standingStore) bool {
	transcript := strings.TrimSpace(a.config.SessionFile)
	if store == nil || transcript == "" {
		return false
	}
	root := strings.TrimSpace(store.Root())
	if root == "" {
		return false
	}
	return filepath.Clean(filepath.Dir(filepath.Dir(transcript))) ==
		filepath.Clean(standing.ExchangesRoot(root))
}

// standingFileTheExchange points a home-made item's origin at where its
// exchange is ABOUT TO BE, and answers the item as it now reads.
//
// THE FOLDER MOVES AND THE ORIGIN NAMES WHERE IT LANDS. An errand's folder is
// made under exchanges/ and moved under the item the moment something stands
// ([standing.Store.ExchangeDir]) — the surface does the rename on the "stood"
// update this call is about to emit — so recording the folder it is leaving
// would be recording a path that stops existing one instant later, and "why did
// I get this reminder?" would open nothing.
//
// SessionID is untouched: the exchange's own id is still the identity of the
// conversation that asked, and it is what a live delivery is addressed to
// (standing_run.go).
//
// It is BEST EFFORT on the write. The item already stands — the person answered
// yes and Create wrote it — so a second write that failed costs the door home
// opens and never the thing itself; the alternative, failing here, would be a
// conversation saying nothing was set up when something was.
func (a *Agent) standingFileTheExchange(store standingStore, item standing.Item) standing.Item {
	if !a.standingAskedFromHome(store) {
		return item
	}
	filed := store.ExchangeDir(item.ID)
	item.Origin.Exchange = filed
	item.Origin.Transcript = filepath.Join(filed, placeTranscript)
	_ = store.Save(item)
	return item
}

// ── the card ────────────────────────────────────────────────────────────────

// The two endings that are NOT an answer, and they are errors rather than a
// declined [StandingAnswer] because all three are different news: a person said
// no, a person said nothing, or there was no person at all. A tool that spelled
// them the same way would have the model reporting a refusal nobody made.
var (
	errStandingUnwatched  = errors.New("session: nobody is watching this session")
	errStandingUnanswered = errors.New("session: the card went unanswered")
)

// askStanding emits one proposal and waits for the person — for as long as
// that takes.
//
// IT IS [Agent.askTask] WITH THE CLOCK TAKEN OFF, which is the file header's
// first law in one function:
//
//   - WATCHED: no deadline is sent and no timer is started. The card stands
//     until it is answered, the turn is interrupted, or the session closes.
//   - UNWATCHED: no card and no item, because there is nobody to answer one.
//
// The turn's context is what carries both of the endings that are not an
// answer: [Agent.Interrupt] and [Agent.Close] each cancel it, so one wait on
// ctx.Done covers a person who pressed esc and a window that went away.
func (a *Agent) askStanding(ctx context.Context, notice *StandingNotice) (StandingAnswer, error) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return StandingAnswer{}, errAgentClosed
	}
	hub := a.hub
	if !a.config.AskConsent || hub == nil {
		a.mu.Unlock()
		return StandingAnswer{}, errStandingUnwatched
	}
	a.standingSeq++
	id := a.standingSeq
	answers := make(chan StandingAnswer, 1)
	if a.standingAnswers == nil {
		a.standingAnswers = make(map[uint64]chan StandingAnswer, 1)
	}
	a.standingAnswers[id] = answers
	a.mu.Unlock()

	notice.ID = id
	// AND THE GOAL OWNER ANSWERS ITS OWN CARD, WHERE THERE IS ONE.
	//
	// "Nobody present means no" is this file's first law and it is the right law
	// for a session somebody walked away from. It is the WRONG law for a session
	// somebody deliberately left running with a budget: a [Steward] is not an
	// absent person, it is the stated owner of this goal (principal.go), and the
	// card it is shown is a card addressed to it. Left to the law above, the card
	// stands until the turn ends and the item is never created — so the one road
	// this build has for noticing that a unit of work has stalled is a road an
	// unattended run can never get onto.
	//
	// WHAT IT MAY SAY YES TO IS ALREADY BOUNDED, and by the same construction
	// that bounds every other yes: [standing.Item.Validate] has already refused
	// anything that spends without a per-run budget and a firing limit, the
	// ticker holds a daily rail over every item on the machine, and the ratified
	// item carries an expiry. This adds an answerer; it adds no capability.
	if steward := a.steward(); steward != nil {
		// The channel this call just registered is dropped again rather than
		// waited on: nothing is going to answer it, and a map entry nobody clears
		// is a card the session thinks is still up.
		a.forgetStanding(id)
		return StandingAnswer{Approved: true}, nil
	}
	// AND ANOTHER WINDOW LEARNS WHAT THIS ONE IS STOPPED ON (taskpresence.go).
	// The line is the PERSON'S OWN SENTENCE, which is the anchor every surface
	// leads this item with ([standing.Item.Words]) — the when and the cost are
	// the card's to show, in the window where there is room to read them.
	defer a.presenceAskingOptions(QuestionStanding, id,
		"wants to keep an eye on: "+strings.TrimSpace(notice.Item.Words), StandingOptions(notice.Item))()
	// SET TO ZERO AND NOT MERELY LEFT ZERO. The field is on the card's shape
	// and a caller could have filled it; this is the one place the law lives,
	// so it is applied here rather than trusted upstream.
	notice.Deadline = time.Time{}
	card := *notice
	hub.send(Event{Kind: EventStandingProposal, Tool: "stand", Standing: &card})

	select {
	case answer := <-answers:
		return answer, nil
	case <-ctx.Done():
		a.forgetStanding(id)
		// THE CARD IS FORGOTTEN AND NOTHING WAS CREATED. The person never
		// answered, so the model is told that and nothing else — a refusal
		// reported here would be a sentence nobody said.
		return StandingAnswer{}, errStandingUnanswered
	}
}

// forgetStanding drops a card nobody will answer, so a late resolve does not
// deliver into a channel with no reader.
func (a *Agent) forgetStanding(id uint64) {
	a.mu.Lock()
	delete(a.standingAnswers, id)
	a.mu.Unlock()
}

// emitStandingUpdate reports one item moving to whoever is watching the turn:
// it was just set up, paused, resumed, stopped. It is a REPORT AND NEVER A
// QUESTION (standing_contract.go), so a session with no hub simply says nothing.
//
// THE TURN'S HUB IS THE RIGHT LANE FOR THESE AND THE ONLY ONE. Every update
// this function carries is the direct consequence of a `stand` call the model
// just made, so there is by construction a turn running and somebody reading
// its stream — including home's `ask here` pane, whose only channel is the one
// Submit handed it (homeexchange.go). A firing is the opposite case and takes
// the opposite lane; see [Agent.emitStandingNews].
func (a *Agent) emitStandingUpdate(update string, item standing.Item, text string) {
	a.mu.Lock()
	hub := a.hub
	a.mu.Unlock()
	if hub == nil {
		return
	}
	hub.send(Event{Kind: EventStandingUpdate, Tool: "stand", Standing: &StandingNotice{
		Item:   item,
		Update: update,
		Text:   text,
	}})
}

// emitStandingNews reports one item FIRING into this conversation, so that the
// surface sitting on it draws the row the manual promises — `◦ <words> · said:
// <text>` — at the moment the news arrives (internal/tui3's standing.go).
//
// A FIRING ARRIVES BETWEEN TURNS, WHICH IS WHY IT IS NOT THE HUB'S. That is the
// whole of the defect this exists to close: [standingRunner.deliver] steered the
// line onto the queue and nothing else, so the transcript grew a note the model
// answered and THE SCREEN SHOWED NOTHING. There is no turn when an item fires —
// that is what ambient means — so [Agent.hub] is nil and a hub send would be a
// send into no lane at all.
//
// So the news takes the STANDING LANE ([Agent.TaskUpdates]), which is the
// subscription a surface holds for the whole life of the session for exactly
// this reason: a node's "done" and an item's firing both happen when no turn is
// running. It goes there and NOWHERE ELSE, even when a turn happens to be in
// flight — one line and never two (standing.go) is a law about the screen, and
// a surface reading both lanes would draw the same firing twice.
func (a *Agent) emitStandingNews(update string, item standing.Item, text string) {
	event := Event{Kind: EventStandingUpdate, Tool: "stand", Standing: &StandingNotice{
		Item:   item,
		Update: update,
		Text:   text,
	}}
	a.mu.Lock()
	watchers := make([]*eventStream, len(a.taskWatchers))
	copy(watchers, a.taskWatchers)
	a.mu.Unlock()
	for _, watcher := range watchers {
		watcher.send(event)
	}
}

// ── background checks, on by default, said once ─────────────────────────────

// standingBackgroundOn installs this machine's timer the first time anything
// ever stands, and says the one dim line about it.
//
// NOBODY IS ASKED, AND IT HAPPENS ONCE, EVER. There used to be a question here
// — keep checking when no window is open? — and it had one sensible answer:
// something you asked to happen every morning is something you asked to happen
// on the mornings you do not open a terminal. So the timer goes on, the person
// is told in one line where the question used to be, and the switch is a
// settings row from then on (internal/config's KeyStandingBackground).
//
// THE NOTICE IS JOURNALED BEFORE THE HOST IS TOUCHED. An install is a change to
// the person's machine; a marker written afterwards would be lost by exactly
// the failure that makes remembering worth doing, and they would be told all
// over again tomorrow.
func (a *Agent) standingBackgroundOn(store standingStore, item standing.Item) {
	if store == nil || a.config.Standing == nil || a.config.Standing.Watch == nil {
		return
	}
	if _, told := standingWatchAsked(store.Root()); told {
		return
	}
	// THE ROW OUTRANKS THE DEFAULT. Somebody who turned background checks off
	// before anything ever stood has answered this already, and installing a
	// timer over that answer would make the switch a suggestion.
	if !config.BackgroundChecksWantedAt(a.config.ProfileDir) {
		standingRememberWatch(store.Root(), false)
		return
	}
	standingRememberWatch(store.Root(), true)
	if err := a.config.Standing.Watch.Install(context.Background()); err != nil {
		// SAID HONESTLY AND NOT SWALLOWED. The person is about to walk away from
		// a machine they think is watching something for them.
		a.emitStandingUpdate(standingBackgroundUpdate, item,
			standingBackgroundFailed+oneLine(err.Error())+standingBackgroundWhere)
		return
	}
	a.emitStandingUpdate(standingBackgroundUpdate, item, standingBackgroundLine)
}

// oneLine flattens whatever the operating system said into the single row this
// surface has for it. launchctl's complaints arrive with newlines in them.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// BackgroundTold reports whether this store has already been told, once, that
// background checks are on.
//
// IT IS EXPORTED FOR ONE READER — the surface's /status line, which says WHY
// nothing is checking (internal/tui3's watchLine). Never told is a machine
// where nothing stands yet; told, with no timer installed, is a person who
// turned the row off or an install that did not take, and /status points at the
// row for both because the row reads `off` in both.
func BackgroundTold(root string) bool {
	_, told := standingWatchAsked(root)
	return told
}

// standingWatchAsked reads the marker. A missing or unreadable one is "nobody
// has been told", which is the safe direction: the worst case is one line said
// twice.
func standingWatchAsked(root string) (standingWatchAnswer, bool) {
	if strings.TrimSpace(root) == "" {
		return standingWatchAnswer{}, false
	}
	raw, err := os.ReadFile(filepath.Join(root, standingWatchOffer))
	if err != nil {
		return standingWatchAnswer{}, false
	}
	var marker standingWatchAnswer
	if json.Unmarshal(raw, &marker) != nil || !marker.Asked {
		return standingWatchAnswer{}, false
	}
	return marker, true
}

// standingRememberWatch journals the notice. A write that fails costs one
// repeated line and nothing else, so it is not reported.
func standingRememberWatch(root string, on bool) {
	if strings.TrimSpace(root) == "" {
		return
	}
	raw, err := json.Marshal(standingWatchAnswer{Asked: true, Answer: on, Told: true, At: time.Now()})
	if err != nil {
		return
	}
	if os.MkdirAll(root, 0o700) != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(root, standingWatchOffer), append(raw, '\n'), 0o600)
}

// ── listing, pausing, resuming, stopping ────────────────────────────────────

// standList answers what stands HERE: this project's items, and the
// machine-wide ones a person set up from nowhere in particular. Both belong in
// one list because both fire into this conversation's world.
func (a *Agent) standList() (string, bool, error) {
	items, err := a.standingHere()
	if err != nil {
		return "nothing could be read: " + err.Error(), true, nil
	}
	if len(items) == 0 {
		return "Nothing stands in this project yet.", false, nil
	}
	var out strings.Builder
	for at, item := range items {
		if at > 0 {
			out.WriteString("\n")
		}
		out.WriteString(standingRow(item))
	}
	return out.String(), false, nil
}

// standingHere is the project's items plus the machine-wide ones, newest first
// and never listed twice.
func (a *Agent) standingHere() ([]standing.Item, error) {
	store := a.standingItems()
	if store == nil {
		return nil, errors.New("there is nothing here to read")
	}
	roots := []string{a.standingWorkspace()}
	if dir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(dir) != "" && dir != roots[0] {
		roots = append(roots, dir)
	}
	seen := map[string]bool{}
	var items []standing.Item
	for _, root := range roots {
		found, err := store.ForWorkspace(root)
		if err != nil {
			return nil, err
		}
		for _, item := range found {
			if seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Created.After(items[j].Created) })
	return items, nil
}

// standingRow is one item as a person would read it out: the glyph every
// surface agrees on, their own words, when it wakes, and what has come of it.
//
// EVERY CLAUSE WITH NOTHING TO SAY IS DROPPED rather than written empty
// (the emptiness law): a row reading "· never checked · $0.00" states two
// things this build does not know as though it did.
func standingRow(item standing.Item) string {
	parts := []string{item.Glyph(false), item.Words}
	if when := strings.TrimSpace(item.When.Words); when != "" {
		parts = append(parts, when)
	}
	if item.Status != standing.StatusActive {
		parts = append(parts, string(item.Status))
	}
	row := strings.Join(parts, " · ") + "\n  " + item.ID
	var facts []string
	if !item.LastChecked.IsZero() {
		facts = append(facts, "checked "+TaskAgeWord(time.Since(item.LastChecked))+" ago")
	}
	if !item.LastFired.IsZero() {
		facts = append(facts, "fired "+TaskAgeWord(time.Since(item.LastFired))+" ago")
	}
	if item.SpentUSD > 0 {
		facts = append(facts, "$"+strconv.FormatFloat(item.SpentUSD, 'f', 2, 64))
	}
	if len(facts) > 0 {
		row += " · " + strings.Join(facts, " · ")
	}
	if line := strings.TrimSpace(item.LastCheckLine); line != "" {
		row += "\n  " + line
	}
	if needs := strings.TrimSpace(item.NeedsPerson); needs != "" {
		row += "\n  needs you: " + needs
	}
	return row + "\n"
}

// standSetStatus is pause, resume and stop: one lookup, one save, one report.
func (a *Agent) standSetStatus(parsed standArguments, status standing.Status) (string, bool, error) {
	store := a.standingItems()
	if store == nil {
		return "there is nothing here to read", true, nil
	}
	item, problem := a.standingNamed(parsed)
	if problem != "" {
		return problem, true, nil
	}
	if item.Status == status {
		switch status {
		case standing.StatusPaused:
			return item.Words + " is already paused.", false, nil
		case standing.StatusRetired:
			return item.Words + " is already stopped.", false, nil
		default:
			return item.Words + " is already running.", false, nil
		}
	}
	item, word, err := a.standingMove(store, item, status)
	if err != nil {
		return "nothing changed: " + err.Error(), true, nil
	}
	if status == standing.StatusRetired {
		return word + ": " + item.Words + "\nIt will not fire again. Setting it up afresh is a new card.", false, nil
	}
	return word + ": " + item.Words, false, nil
}

// standingMove is THE ONE PATH A STATUS CHANGE TAKES, whichever gesture asked
// for it: the model's `stand` op here, and the page's own stand-down and pause
// keys (standing_orders.go). It stamps the person's reason on a stop, writes,
// tells whoever is watching the turn, and answers the item as it now reads and
// the word the news travels under.
//
// IT IS ONE PATH BECAUSE THE REASON IS ONE FACT. [standing.Item.RetiredWhy]
// names its own spellings, and two doors writing "stopped by you" separately is
// two chances for one of them to say something else the day the wording moves.
func (a *Agent) standingMove(store standingStore, item standing.Item, status standing.Status) (standing.Item, string, error) {
	item.Status = status
	item.RetiredWhy = ""
	word := "resumed"
	switch status {
	case standing.StatusPaused:
		word = "paused"
	case standing.StatusRetired:
		word = "stopped"
		// The person's own reason, in the words [standing.Item] reserves for it.
		item.RetiredWhy = standingStoppedWhy
	}
	if err := store.Save(item); err != nil {
		return item, word, err
	}
	a.emitStandingUpdate(word, item, "")
	return item, word, nil
}

// standingStoppedWhy is what a stopped item's document records, and it is one
// of the cases [standing.Item.RetiredWhy] spells out.
const standingStoppedWhy = "stopped by you"

// standingNamed resolves an id OR the person's own words to one item.
//
// WORDS ARE HOW PEOPLE NAME THESE, and the id is how the machine does. Somebody
// says "stop the CI one", never "stop 7f3a", so a substring of their own
// sentence is a first-class handle here. An ambiguous one is not guessed at: it
// answers with the candidates, because pausing the wrong watch is a silence the
// person will not notice until it matters.
func (a *Agent) standingNamed(parsed standArguments) (standing.Item, string) {
	store := a.standingItems()
	handle := strings.TrimSpace(parsed.ID)
	if handle == "" {
		handle = strings.TrimSpace(parsed.Words)
	}
	if handle == "" {
		return standing.Item{}, "Invalid arguments: name it with id, or with the person's own words"
	}
	if item, err := store.Get(handle); err == nil && strings.TrimSpace(item.ID) != "" {
		return item, ""
	}
	items, err := a.standingHere()
	if err != nil {
		return standing.Item{}, "nothing could be read: " + err.Error()
	}
	needle := strings.ToLower(handle)
	var hits []standing.Item
	for _, item := range items {
		if strings.EqualFold(item.ID, handle) ||
			strings.Contains(strings.ToLower(item.Words), needle) ||
			strings.Contains(strings.ToLower(item.When.Words), needle) {
			hits = append(hits, item)
		}
	}
	switch len(hits) {
	case 0:
		return standing.Item{}, "nothing here matches " + strconv.Quote(handle) + ". Call stand with op list to see what stands."
	case 1:
		return hits[0], ""
	}
	var out strings.Builder
	out.WriteString(strconv.Quote(handle) + " matches more than one — say which:\n")
	for _, item := range hits {
		out.WriteString("  " + item.ID + " · " + item.Words + "\n")
	}
	return standing.Item{}, out.String()
}
