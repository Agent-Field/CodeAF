package session

// answers.go is how an answer reaches a session that is stopped on a question
// in ANOTHER window.
//
// taskpresence.go carries the question outward: a live session blocked on a
// card writes what it is asking, with the answers it will take, into its
// presence file, and any other window may read it without opening a journal or
// taking a lock. This file is the return path, and it is deliberately the
// simplest thing that works — internal/standing's inbox.go in the other
// direction: ONE JSONL FILE in the session's own folder, appended by whoever
// answered, drained whole by the session itself on the heartbeat it already
// runs.
//
// ── THE THREE LAWS ──
//
//   - AN ANSWER IS APPLIED THROUGH THE SAME RESOLVER A SURFACE USES. There is
//     no second door into the approval gate, the task proposal or the standing
//     card: [Agent.applyAnswer] calls [Agent.ResolveConsentRemember],
//     [Agent.ResolveTask] and [Agent.ResolveStanding], which is exactly what the
//     card in the window calls. A lane of its own would be a second place that
//     decides what "yes" does, and the two would drift on the day one of them
//     learned something.
//
//   - A LATE ANSWER IS IGNORED, AND NOTHING SAYS SO. The resolvers already drop
//     an id nobody is waiting on — a question the clock approved, a card the
//     person answered in its own window a second earlier, a turn that was
//     interrupted — and an answer arriving through a file is late in exactly
//     those ways and no new ones. So this file adds no staleness rule of its
//     own; it hands the id over and lets the one rule that exists apply.
//
//   - THE KEY IS THE ANSWER'S NAME, AND THE MAPPING IS WRITTEN ONCE.
//     [AnswerOptions] says which keys a kind of question takes and what each of
//     them is called; [AnswerFromKey] says what one of them MEANS. Both live
//     here, in the engine, because the surface that draws the chips and the
//     session that applies the answer must agree about them completely — a
//     surface offering a key the session does not take is a chip that does
//     nothing, and a surface whose "2" means something else than the session's
//     is worse than either.
//
// ── THE FILE ──
//
//	<session dir>/answers.jsonl
//
// One JSON object per line: when it was given, which question it answers, and
// the key. JSONL and not JSON because two windows could answer two questions in
// the same instant and an append never loses one — the opposite of presence.json
// next to it, which has exactly one writer and one fact and is replaced whole.
//
// THE DRAIN RENAMES BEFORE IT READS, for standing's own reason: an answer
// delivered while the file is being read would otherwise be read and then
// deleted unapplied. Moving it aside first means a racing write starts a fresh
// file that the next beat finds.

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// The two refusals this file makes, and both are about the CALLER rather than
// about the disk: a key the question does not take, and a session with no
// folder to leave anything in. Everything that can go wrong with the file
// system is dropped in silence instead (see [Agent.drainAnswers]).
var (
	errUnknownAnswer   = errors.New("session: that question does not take that answer")
	errNoSessionFolder = errors.New("session: that conversation has no folder to answer into")
)

// answersName is the file, inside one session's folder. Like presenceName it is
// spelled here and nowhere else: an answers file is not part of what a session
// KEEPS, it is a doorstep other windows leave things on.
const answersName = "answers.jsonl"

// AnswersPath is that doorstep for one session folder.
func AnswersPath(sessionDir string) string {
	return filepath.Join(strings.TrimSpace(sessionDir), answersName)
}

// QuestionKind is which of the three lanes a question came from. The words are
// the ones the code already uses for them.
type QuestionKind string

const (
	// QuestionConsent is the approval gate: may this call run (consent.go).
	QuestionConsent QuestionKind = "consent"
	// QuestionTask is a task proposal: should this work go (task.go).
	QuestionTask QuestionKind = "task"
	// QuestionStanding is a standing card: should this be kept an eye on
	// (tools_standing.go).
	QuestionStanding QuestionKind = "standing"

	// ── the lanes below were added with question.go, and every one of them
	// already existed as a wait with a resolver; what they had never had was a
	// NAME another window could put on an answer. They are listed here rather
	// than beside their own lanes for this file's third law: the mapping from a
	// key to what it MEANS is written once, in one place, or a surface and a
	// session end up meaning two things by "2".

	// QuestionConnect is a connect offer: may one of the person's accounts be
	// connected, and where the account needs one, the key or address it is
	// missing (connect.go, [Agent.ResolveConnect] and
	// [Agent.ResolveConnectKey]).
	QuestionConnect QuestionKind = "connect"
	// QuestionHarness is a sub-harness offer or a written design waiting to be
	// approved (harness.go, [Agent.ResolveHarness]).
	QuestionHarness QuestionKind = "harness"
	// QuestionSubharness is an intake card chat raised for a saved program
	// (tools_subharness.go, [Agent.ResolveSubharness]).
	QuestionSubharness QuestionKind = "subharness"
	// QuestionSubharnessAsk is a RUNNING sub-harness putting its own question to
	// the person (subharness_env.go, [Agent.AnswerSubharness]). The audit found
	// this lane drawn by nothing at all: the run waited, and no surface in the
	// product had a door onto it.
	QuestionSubharnessAsk QuestionKind = "subharness-ask"
	// QuestionLanding is a landed task's `your call` — work that finished and
	// that nobody could check (task_audit.go, [Agent.ResolveUnverified],
	// [Agent.HandUnverifiedToModel] and [Agent.TakeBackDecision]).
	QuestionLanding QuestionKind = "landing"
	// QuestionConflict is a branch that would not fasten onto the person's
	// (task_merge_round.go, [Agent.ResolveConflict]). It is the lane the audit
	// found with a door and no card anywhere.
	QuestionConflict QuestionKind = "conflict"
	// QuestionFuel is an adaptive run standing at its fuel gate
	// (orchestrate.go, [Agent.ResolveOrchestrate]).
	QuestionFuel QuestionKind = "fuel"
	// QuestionRecovery is a turn caught going in circles, asking what to do
	// about ITSELF (recovery.go, [Agent.ResolveRecovery]). It borrows the
	// consent lane's wait and is deliberately answerable only in the window
	// that raised it, so it is never offered to another one.
	QuestionRecovery QuestionKind = "recovery"
	// QuestionAsk is the model's own question, raised through the ask tool.
	QuestionAsk QuestionKind = "ask"
)

// AnswerOption is one answer a question will take: the key that gives it and
// the word for it.
type AnswerOption struct {
	// Key is what a person presses. It is a digit on every kind, because the
	// surface that offers these is home, where the letters are already typing.
	Key string `json:"key"`
	// Label is the answer in the words the card uses for it.
	Label string `json:"label"`

	// ── the fields below arrived with question.go, and every one of them is
	// `omitempty`: a chip row that only ever read Key and Label reads exactly
	// what it always did, and a form with room for more finds more here.

	// Body is what this answer MEANS, in a sentence or two — what the room
	// draws under the word when the answer is opened. Empty is an answer whose
	// word says the whole of it.
	Body string `json:"body,omitempty"`
	// Consequence is what happens if this one is taken, in the future tense and
	// in one line: "the file is overwritten", "nothing runs". It is the line a
	// card draws beside the word, and it is the difference between choosing and
	// guessing.
	Consequence string `json:"consequence,omitempty"`
	// Safe marks THE ANSWER THAT CHANGES NOTHING. A confirmation starts its
	// cursor on it and a destructive answer never shares a key with it
	// (tabclose.go's law, stated once for every kind). At most one answer on a
	// question is safe; where none is, none is marked, because inventing one
	// would put a person's cursor on an act.
	Safe bool `json:"safe,omitempty"`
	// Widening marks an answer that grants MORE than the question asked about —
	// "always", "every command like this". It is drawn apart from the others so
	// that a person who meant "yes, this once" cannot land on it by muscle
	// memory.
	Widening bool `json:"widening,omitempty"`
	// Blocks are this answer's own evidence: the diff it would produce, the
	// layout it would draw, the rows it would write.
	Blocks []Block `json:"blocks,omitempty"`
	// Dimensions are the axes the asker wants these answers compared on —
	// "speed", "what it costs", "what it breaks" — keyed by the axis and valued
	// with this answer's reading of it. A question whose answers all carry the
	// same axes can be laid side by side; one whose answers do not is drawn as
	// a list, and no axis is ever invented to fill the table.
	Dimensions map[string]string `json:"dimensions,omitempty"`
}

// AnswerOptions is what one kind of question may be answered with, in the order
// the chips are drawn.
//
// THE KEYS ARE THE CARD'S OWN DIGITS WHERE THE CARD HAS DIGITS. The standing
// card is answered 1 yes, 2 change when or where, 3 just once in its own window
// (tui3's standing.go), and 1 and 3 mean the same here — the hand that learned
// them there is right here. `2 change when or where` is deliberately NOT on this
// list: it is a request for a text box, and there is no box on the row this is
// drawn beside.
//
// AND THE WORDS ARE THE CARD'S OWN TOO. A person reading `just once` on the card
// and `once, not standing` on home would be reading two names for one answer,
// and the second of them is written in this build's vocabulary rather than
// theirs — "standing" is a word they never said. One answer, one spelling,
// wherever it is drawn.
//
// AND `0 not set up` IS THE OUTRIGHT NO, ON EVERY SURFACE THAT DRAWS THE CARD.
// In the conversation the no was `esc` alone, which home does not have to give —
// esc there closes home — so a standing card met from home used to offer a yes,
// a once, and no way at all to say no; the only ways out were opening the window
// or leaving the question open. [StandingNoKey] is why the key is a `0` and not
// a fourth digit.
//
// THIS IS THE ANSWER FOR THE KIND AND NOT FOR AN ITEM. A one-off reminder's
// card offers no `3` at all, because doing that action "now" is meaningless —
// [StandingOptions] narrows this list to one item, and that is what a card and
// a presence file are actually built from.
//
// THE CONSENT KEYS ARE NEW AND THE ANSWERS ARE NOT. In its own window the gate
// is answered y / a / n; those letters cannot be borrowed here, because a letter
// on home is a character being typed. So the three answers keep their meaning
// and take digits, and the words beside them are the card's own.
func AnswerOptions(kind QuestionKind) []AnswerOption {
	switch kind {
	case QuestionConsent:
		return []AnswerOption{
			{Key: "1", Label: "allow once"},
			// `always` GRANTS MORE THAN THE QUESTION ASKED ABOUT — every later
			// call of that tool, for the rest of this session — so it is marked
			// widening and drawn apart from its neighbours (question.go's
			// [AnswerOption.Widening]). It is also the one answer a question
			// may not offer at all: the stuck-turn question borrows this lane
			// to ask about a TURN, where the memo would do nothing.
			{Key: "2", Label: "always", Widening: true},
			{Key: "3", Label: "deny", Safe: true},
		}
	case QuestionTask:
		return []AnswerOption{
			{Key: "1", Label: "yes"},
			{Key: "2", Label: "no"},
		}
	case QuestionStanding:
		return []AnswerOption{
			{Key: "1", Label: "yes"},
			{Key: StandingOnceKey, Label: "just once"},
			{Key: StandingNoKey, Label: "not set up"},
		}
	case QuestionConnect:
		return []AnswerOption{
			{Key: "1", Label: "connect"},
			{Key: "2", Label: "not now", Safe: true},
		}
	case QuestionHarness, QuestionSubharness:
		return []AnswerOption{
			{Key: "1", Label: "run it"},
			{Key: "2", Label: "not now", Safe: true},
		}
	case QuestionFuel:
		return []AnswerOption{
			{Key: "1", Label: "add more", Consequence: "the run carries on"},
			{Key: "2", Label: "finish on what is done", Consequence: "it writes up what it already has"},
			{Key: "3", Label: "stop", Consequence: "the run ends where it stands"},
		}
	case QuestionLanding, QuestionConflict:
		// THE LANDING KEYS ARE TASK-STATES' OWN, LETTER FOR LETTER
		// (docs/design/task-states/DESIGN.md): `[a] <yes> · [n] <no> · [s] tell
		// it`, always the same three columns in the same order. The words
		// beside them are the row's own — [TaskAsk] carries them, and
		// [landingOptions] is what fills them in for one node. This list is the
		// kind's shape and the fallback for a row that offered none.
		return []AnswerOption{
			{Key: LandingYesKey, Label: "accept"},
			{Key: LandingNoKey, Label: "not right", Safe: true},
			{Key: LandingTellKey, Label: "tell it"},
		}
	}
	return nil
}

// The keys a landed task's `your call` is answered with, and the two beside
// them that are not on the row.
//
// THEY ARE LETTERS AND NOT DIGITS, which is the one place this package parts
// company with [AnswerOptions]' rule that a key is a digit — because
// docs/design/task-states/DESIGN.md fixed `a`, `n` and `s` on that card before
// this file existed, and a hand that learned them there must find them here.
// Home draws them as chips, so the letters cost nothing there either.
const (
	// LandingYesKey accepts the work on the person's word.
	LandingYesKey = "a"
	// LandingNoKey says it does not hold.
	LandingNoKey = "n"
	// LandingTellKey sends words to the work and LEAVES THE QUESTION OPEN — a
	// steer never resolves a task by itself (docs/design/task-states/DESIGN.md).
	LandingTellKey = "s"
	// LandingAgainKey sends a fresh look at the same working copy.
	LandingAgainKey = "r"
	// LandingDecideKey hands this one decision to the model. It is the
	// one-time `let aforge decide this one`, and it is never a standing
	// setting.
	LandingDecideKey = "d"
	// LandingTakeBackKey takes back a decision that was settled without the
	// person. It is on a record rather than on a question, which is why it is
	// not among the three the row draws.
	LandingTakeBackKey = "u"
)

// StandingOnceKey is the digit "once, not standing" is answered with, on the
// card and on home alike. It is spelled once, here, because two surfaces and
// [StandingOptions] all have to agree about which chip is the one that may be
// missing.
const StandingOnceKey = "3"

// StandingNoKey is the digit that DECLINES a standing card outright: nothing is
// created, nothing is run, and the row settles as `not set up` — the answer
// [Agent.ResolveStanding] reads out of a zero [StandingAnswer].
//
// IT IS A `0` BECAUSE IT MUST NOT BE A DIGIT ANOTHER CHIP ALREADY OWNS, AND
// MUST NOT BE A LETTER. The card in a conversation numbers its chips by their
// position — 1 yes, 2 change when, 3 once (tui3's [taskModelKey]) — so a fourth
// answer taking `4` would move the moment a card drew one chip fewer, and the
// hand that learned the keys on a watch would decline a reminder. A letter is
// worse: on home the letters are already typing, which is the whole reason
// [AnswerOption.Key] is a digit on every kind. `0` is off the end of the chip
// numbering in both directions, is one keystroke, and is nowhere near `1`.
//
// AND IT IS A DRAWN CHIP EVERYWHERE, the conversation's own card included. It
// was a bare key there for a wave — `esc` had always been the no, and the `0`
// was named only in the hint slot under the message box — and a person meeting
// their first card said plainly that they could see no way to cancel. A gesture
// whose only documentation is documentation is the one trade
// docs/DESIGN-LANGUAGE.md refuses, so the decline is now a chip a person can see
// and click on all three surfaces, and `esc` goes on doing the same thing beside
// it in the one place there is an esc to spare.
const StandingNoKey = "0"

// StandingOnceIsAnAnswer reports whether "once, not standing" MEANS anything
// for one item, and it is the ONE PLACE that is decided.
//
// "Once" means: do the action NOW, as an ordinary turn, and leave nothing
// behind ([StandingAnswer.Once]). For a watch, a rule, a routine or overnight
// work that is a real answer — the person wants the thing done, not the
// arrangement. FOR A ONE-OFF REMINDER IT IS NOT AN ANSWER AT ALL: the whole
// content of "remind me at six" is the SIX, and doing it now says "time to
// leave" hours early or says nothing. A person met that chip after asking for a
// one-minute timer, pressed it because it was the only answer that was not a
// commitment, and was told the build could not hold a timer — which it can, and
// does, and had just offered to.
//
// So the card does not draw it there. Everywhere else it stays.
func StandingOnceIsAnAnswer(item standing.Item) bool {
	return !(item.When.Kind == standing.WhenAt && item.Does.Kind == standing.ActionSay)
}

// StandingOptions is [AnswerOptions](QuestionStanding) narrowed to ONE item:
// the answers this particular card offers, in the order chips are drawn.
//
// THE ONLY CHIP THAT IS EVER MISSING IS THE `once`. A yes and a no are answers
// to every standing card there is — "set it up" and "set nothing up" are what
// the question means — so [StandingNoKey] is on every row this returns, and a
// person who learned the decline on a watch finds it under the same key on a
// reminder.
//
// IT IS WHAT BOTH SURFACES DRAW FROM. The engine puts it on the card
// ([StandingNotice.Options]) and into the presence file another window answers
// through ([Agent.presenceAsking]), so the conversation's chip row, home's chip
// row and the keys the session will actually accept are one decision made once
// — a chip that does nothing is exactly what this file's third law forbids.
func StandingOptions(item standing.Item) []AnswerOption {
	options := AnswerOptions(QuestionStanding)
	if StandingOnceIsAnAnswer(item) {
		return options
	}
	kept := make([]AnswerOption, 0, len(options))
	for _, option := range options {
		if option.Key == StandingOnceKey {
			continue
		}
		kept = append(kept, option)
	}
	return kept
}

// AnswerLabel is the word for one key, and "" for a key that kind does not
// take. A surface says what it just did with it.
func AnswerLabel(kind QuestionKind, key string) string {
	for _, option := range AnswerOptions(kind) {
		if option.Key == key {
			return option.Label
		}
	}
	return ""
}

// AnswerAction is what one key MEANS: the answer, in the shape the lane's own
// resolver takes.
//
// It is one struct with three lanes' answers on it rather than three functions,
// because a caller with a key and a kind in its hand wants ONE call — and
// because the zero value of each field is already that lane's "no", so a
// mis-typed key can never come out as a yes.
type AnswerAction struct {
	// Kind is the lane this action belongs to, and the field a caller switches
	// on to know which of the three below to read.
	Kind QuestionKind
	// Allow and Scope are the consent lane's answer, for
	// [Agent.ResolveConsentRemember].
	Allow bool
	Scope ConsentScope
	// Task is the proposal lane's answer, for [Agent.ResolveTask].
	Task TaskAnswer
	// Standing is the standing lane's answer, for [Agent.ResolveStanding].
	Standing StandingAnswer
}

// AnswerFromKey is the whole mapping, and it is the one place it is written.
//
// A key the kind does not take answers false and is applied to nothing. That is
// the same conservative reading [Agent.ResolveConsentRemember] takes of an
// unknown scope, and for the same reason: a typo must never widen an approval,
// and a key that fell off a chip row must never be read as the answer next to
// it.
//
// WHAT "ALWAYS" IS, EXACTLY. It is [ConsentToolSession] — the same answer the
// window's `a` key sends the engine, which stops that session asking about that
// TOOL for the rest of its life. It is not [ConsentRule]: a banked rule is
// written from the command line the call actually carried, and a window
// answering somebody else's question has only the one line the session is
// stopped on. Writing a rule from that gloss would bank a standing approval for
// a command that was never run (tui3's [app.askCommand] states the same law).
// The floor holds either way — the shapes internal/approval always asks about
// are asked again whatever memo is standing.
func AnswerFromKey(kind QuestionKind, key string) (AnswerAction, bool) {
	key = strings.TrimSpace(key)
	if AnswerLabel(kind, key) == "" {
		return AnswerAction{}, false
	}
	action := AnswerAction{Kind: kind}
	switch kind {
	case QuestionConsent:
		switch key {
		case "1":
			action.Allow, action.Scope = true, ConsentOnce
		case "2":
			action.Allow, action.Scope = true, ConsentToolSession
		case "3":
			action.Allow, action.Scope = false, ConsentOnce
		}
	case QuestionTask:
		switch key {
		case "1":
			action.Task = TaskAnswer{Approved: true}
		case "2":
			action.Task = TaskAnswer{Approved: false}
		}
	case QuestionStanding:
		switch key {
		case "1":
			action.Standing = StandingAnswer{Approved: true}
		case StandingOnceKey:
			action.Standing = StandingAnswer{Once: true}
		case StandingNoKey:
			// THE DECLINE IS WRITTEN OUT RATHER THAN LEFT TO FALL THROUGH. It
			// is already the zero value — which is the safety this whole
			// function is built on — but a reader counting the answers a
			// standing card takes must find three arms here and not two, and
			// the day one of them learns something the line to change is
			// visible.
			action.Standing = StandingAnswer{}
		}
	}
	return action, true
}

// Answer is what somebody said to a question: one line of answers.jsonl, and
// the value every resolver in this engine is reached through
// ([Agent.ResolveQuestion]).
//
// IT GREW RATHER THAN BEING REPLACED, and the four fields it started with are
// still the four an older window writes: At, Kind, ID and Key. Everything below
// them is `omitempty`, so a build of any age reads a line written by a build of
// any other — [AnswerFromKey] still answers from a bare key alone, which is
// exactly what home has always sent.
//
// WHAT THE NEW FIELDS ARE FOR is the half of an answer a key could never
// carry: several picks rather than one, the sentence somebody added beside
// their pick, the blanks they filled, what they asked back, and — the one that
// makes the record worth keeping — WHO decided and how long it lasts.
type Answer struct {
	// At is when it was given. It orders a drain and is the only thing here a
	// reader could use to notice an answer that sat on the doorstep for a week
	// — nothing does, because a question that old is one nobody is waiting on
	// and the resolvers already drop it.
	At time.Time `json:"at"`
	// Kind and ID name the question, exactly as the presence file's
	// [PresenceQuestion] spelled them.
	Kind QuestionKind `json:"kind"`
	ID   uint64       `json:"id"`
	// Key is the answer, as [AnswerOptions] names it.
	Key string `json:"key"`
	// From is where it came from — "home" is the only writer today. It is here
	// so a later reader can tell an answer somebody gave on another screen from
	// one a machine gave, without guessing from a timestamp.
	From string `json:"from,omitempty"`

	// Ref names the question where its lane's token is a STRING rather than a
	// number — a connect account, an adaptive run. Exactly one of ID and Ref is
	// set, exactly as on [Question].
	Ref string `json:"ref,omitempty"`
	// Ask is the shape of the decision this answered ([AskKind]). It is
	// carried so a record can be read without the question beside it, and it is
	// empty on a line an older window wrote.
	Ask AskKind `json:"ask,omitempty"`
	// Picked are the answers given, by key. It is one key on most questions,
	// several on a checklist, and one per row on pairs. Key above is the FIRST
	// of these, kept filled so an older reader — and [Agent.applyAnswer]'s own
	// mapping — goes on working unchanged; [Answer.Keys] is how this package
	// reads either.
	Picked []string `json:"picked,omitempty"`
	// Change is what was said BESIDE the pick: "2, but keep the sqlite file as
	// the source of truth". It is the half of an answer that carries the
	// person's intent, and a lane that can take words does something with it —
	// the connect lane reads it as the key it asked for, the landing lane sends
	// it to the work as a steer, and the rest keep it in the record.
	Change string `json:"change,omitempty"`
	// Comments are what was said about ONE answer or ONE blank, keyed by its
	// key or its label. They are notes on the parts and never the answer
	// itself.
	Comments map[string]string `json:"comments,omitempty"`
	// AskedBack are the rounds of asking back, bounded at one per answer
	// ([Exchange] says why).
	AskedBack []Exchange `json:"askedBack,omitempty"`
	// Blanks are the fields of a small form, keyed by [Blank.Label].
	Blanks map[string]string `json:"blanks,omitempty"`
	// Dial is the number a dial was left on, and nil where there was no dial —
	// which is not the same as a dial left at zero.
	Dial *float64 `json:"dial,omitempty"`
	// Reframe is an answer that is not a pick at all: "the real question is…".
	// It resolves nothing by itself; it goes back to the asker.
	Reframe string `json:"reframe,omitempty"`
	// DecidedBy is who answered. It is the field that makes the record worth
	// keeping, and its zero value is empty rather than [DecidedByPerson] —
	// claiming a person pressed a key nobody pressed is the one thing a record
	// must never do.
	DecidedBy DecidedBy `json:"decidedBy,omitempty"`
	// Scope is how long this answer lasts, and it is only ever one the question
	// offered. Empty is [ScopeOnce].
	Scope AnswerScope `json:"scope,omitempty"`
	// Why is the person's own reason, asked for softly when they answered
	// against the pick. It is what a preference is later written from, and it
	// is empty far more often than not.
	Why string `json:"why,omitempty"`
	// TakingOver says a question a running sub-harness asked was answered by
	// the person taking the work over rather than by an answer to it. It is
	// meaningful on [QuestionSubharnessAsk] alone.
	TakingOver bool `json:"takingOver,omitempty"`
}

// Keys is what was picked, however the answer spelled it: [Answer.Picked] where
// it is filled, and the single [Answer.Key] where an older window wrote one.
//
// IT IS THE ONE READER OF BOTH, so no lane has to know which shape it was
// handed. An answer with neither is a real answer on the kinds that take words
// instead of keys — a clarification, a question a sub-harness asked — and comes
// back empty rather than as a guess.
func (a Answer) Keys() []string {
	if len(a.Picked) > 0 {
		return a.Picked
	}
	if key := strings.TrimSpace(a.Key); key != "" {
		return []string{key}
	}
	return nil
}

// FirstKey is the single key a two-or-three-answer question was answered with,
// and "" for an answer given in words.
func (a Answer) FirstKey() string {
	keys := a.Keys()
	if len(keys) == 0 {
		return ""
	}
	return strings.TrimSpace(keys[0])
}

// Words is everything the person typed, in one string: the change they said
// beside their pick, or the reframe where they gave one instead. It is what a
// lane that takes a typed answer is handed.
func (a Answer) Words() string {
	if change := strings.TrimSpace(a.Change); change != "" {
		return change
	}
	return strings.TrimSpace(a.Reframe)
}

// answerFromHome is what home writes in [Answer.From].
const answerFromHome = "home"

// WriteAnswer leaves one answer on a session's doorstep.
//
// It is the seam a surface is handed (tui3's Options.Answer), and it takes the
// session's FOLDER rather than an agent, because the whole point is that the
// session being answered is in another process. A key the kind does not take is
// refused here rather than written and dropped later — the surface that offered
// the chip is the one that can still say something about it.
func WriteAnswer(sessionDir string, kind QuestionKind, id uint64, key string) error {
	if _, ok := AnswerFromKey(kind, key); !ok {
		return errUnknownAnswer
	}
	return deliverAnswer(sessionDir, Answer{
		At:   time.Now(),
		Kind: kind,
		ID:   id,
		Key:  strings.TrimSpace(key),
		From: answerFromHome,
	})
}

// deliverAnswer appends one line, making the folder if it is not there. It is
// [standing.Deliver] with a different payload and the same shape.
func deliverAnswer(sessionDir string, answer Answer) error {
	dir := strings.TrimSpace(sessionDir)
	if dir == "" {
		return errNoSessionFolder
	}
	line, err := json.Marshal(answer)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(AnswersPath(dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	if _, err := file.Write(line); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// DrainAnswers reads and removes a session's answers, oldest first. A folder
// with nothing on its doorstep is an empty slice and no error, which is the
// ordinary case on every beat of every session that was never answered from
// anywhere.
func DrainAnswers(sessionDir string) ([]Answer, error) {
	path := AnswersPath(sessionDir)
	if strings.TrimSpace(sessionDir) == "" {
		return nil, nil
	}
	// THE RENAME IS THE READ'S OWN LOCK, and it is the whole of the concurrency
	// story here: whoever wins the rename owns those lines, and a write racing
	// it lands in a fresh file the next beat drains.
	staged := path + "." + NewSessionID() + ".draining"
	if err := os.Rename(path, staged); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer os.Remove(staged)
	file, err := os.Open(staged)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var answers []Answer
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var answer Answer
		if json.Unmarshal(raw, &answer) != nil {
			// A line nothing can read is a line nothing can apply. It is
			// dropped rather than reported: there is no question in it to be
			// answered and nobody left to tell.
			continue
		}
		answers = append(answers, answer)
	}
	if err := scanner.Err(); err != nil {
		return answers, err
	}
	sort.SliceStable(answers, func(a, b int) bool { return answers[a].At.Before(answers[b].At) })
	return answers, file.Close()
}

// ── the live session's side ─────────────────────────────────────────────────

// drainAnswers takes whatever is on this session's doorstep and applies it.
//
// It runs on the presence heartbeat (taskpresence.go's [presenceDesk.beat]),
// which is the natural place for it and not merely a convenient one: the same
// beat is what put the question on disk, the cadence a person waits after
// pressing a key is the cadence the question appeared at, and a session with no
// folder — a memory-only conversation, a task node — has no presence and
// therefore no doorstep either.
//
// EVERY FAILURE IS SILENCE, as every other write in that file is. A session
// must not stall or say anything because a directory would not answer; the
// answer is simply not applied, and the person's window still has the question
// on it.
func (a *Agent) drainAnswers() {
	dir := strings.TrimSpace(a.config.Place.Dir)
	if dir == "" {
		return
	}
	answers, err := DrainAnswers(dir)
	if err != nil && len(answers) == 0 {
		return
	}
	for _, answer := range answers {
		a.applyAnswer(answer)
	}
}

// applyAnswer hands one answer to the lane it belongs to, THROUGH THE SAME
// RESOLVER A SURFACE USES (the first law in this file's header). An id nobody is
// waiting on falls through those resolvers untouched, which is what makes a
// stale answer a no-op rather than a special case here.
func (a *Agent) applyAnswer(answer Answer) {
	// AND IT GOES THROUGH THE ONE DOOR, which is what keeps that law literally
	// true rather than nearly true. [Agent.ResolveQuestion] is the only thing
	// in this package that knows which resolver a lane's answer belongs to;
	// this file used to be a second, shorter copy of that knowledge, covering
	// three lanes of the eleven. An answer for a lane the door does not take
	// comes back with a refusal and is dropped here, because a file on a
	// doorstep has nobody left to tell.
	if answer.From == "" {
		answer.From = answerFromHome
	}
	_ = a.ResolveQuestion(answer)
}
