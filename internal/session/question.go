package session

// question.go is ONE OBJECT FOR EVERY DECISION THIS ENGINE HANDS TO A PERSON.
//
// Before this file there were thirteen of them. The approval gate, the connect
// offer, the harness offer, the harness design, the sub-harness proposal, a
// running sub-harness's own question, the standing card, the task proposal, a
// landed task's `your call`, the merge conflict, the fuel gate, the stuck-turn
// recovery and the auto-settle take-back each minted their own id, banked their
// own wait, emitted their own event and were answered through their own door —
// and `ideation/questions-audit.md` counted twenty-one distinct question
// mechanisms across fourteen answer surfaces, three of which could draw an
// answer row at all. Two of those doors had no caller anywhere in the product.
//
// A person does not have thirteen kinds of decision. They have one: somebody is
// asking me something, here is what it is about, here is what I may say. So
// this file states that object once — [Question] — and every lane above is
// expressed in it.
//
// ── WHAT THIS FILE DOES NOT DO ──
//
// IT ADDS NO SECOND STORE. [Agent.OpenQuestions] is DERIVED, every time, from
// the waits the lanes already keep: the consent map, the connect asks, the
// harness asks, the standing answers, the task proposals, the sub-harness
// offers and questions, the orchestrator's pause and the task graph's own
// [PendingDecision] registry. A list of open questions kept beside those would
// be the second place a question could be open, and the two would disagree the
// first hour a lane learned to close one on a road that forgot to tell this
// file (pending.go states the same law about its own registry, in its own
// words).
//
// AND IT CHANGES NOTHING A PERSON SEES. Every resolver still resolves exactly
// what it resolved; every event a surface draws today is still emitted, in the
// same order, with the same fields. What is new is a second, wider description
// of the same moment, which the surfaces take up one lane at a time.
//
// ── THE TWO WORDS FOR "KIND", AND WHY THERE ARE TWO ──
//
// [QuestionKind] (answers.go) is the LANE: which part of the engine is stopped,
// and therefore which resolver an answer is applied through. It was already
// spelled, already on disk in presence files, already read by home and by the
// `--host` link, and this file extends its list rather than renaming it.
//
// [AskKind] is the SHAPE OF THE DECISION: is this a permission, a choice, a
// judgement, a clarification, a confirmation, a landing, an assumption, a
// ratification. It is what decides how a question is drawn, what may answer it
// without a person, and what a safe answer is.
//
// They are orthogonal on purpose. Two lanes can raise the same shape — the
// approval gate and the sub-harness proposal are both permissions — and one
// lane can raise two shapes, which is exactly what the consent lane does when
// recovery.go borrows it to ask about a TURN instead of a tool. A single enum
// would have had to pick one of those facts to be, and the surfaces need both.
//
// ── THE LADDER, AND WHY THE GATE REFUSES RATHER THAN REPAIRS ──
//
// docs/design/questions/DESIGN.md states the ladder an asker climbs before it
// puts a question to anybody: read the record, assume and say so, act then
// ratify, show outcomes, offer structured input, and only then ask. [Question.Check]
// is where the last rung is defended, and it REFUSES rather than filling
// anything in: a question with no reason is a question the asker had not
// finished thinking about, and a gate that supplied the missing sentence would
// be writing the reason a person then reads as the asker's.
//
// Every refusal it makes ends in something the asker can DO — "decide, or state
// the assumption" — because the model on the other side of it has to be able to
// act on the refusal without asking a second time.

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/orchestrate"
)

// ── the shape of the decision ───────────────────────────────────────────────

// AskKind is what SHAPE of decision is being handed over. It decides how a
// question is drawn, what a safe answer is, and whether anything but a person
// may answer it (docs/design/questions/DESIGN.md's table of kinds and their
// defaults).
//
// It is a string and not an iota for the reason [QuestionKind] is: these values
// travel in presence files and answer files that a build of a different age
// reads, and a number whose meaning moved when somebody inserted a constant is
// the one bug a wire format must not have.
type AskKind string

const (
	// AskPermission is "may this happen": the approval gate, a connect offer, a
	// harness offer, a sub-harness proposal. Its safe answer is to skip it, and
	// it may run on a clock only where the stakes are reversible.
	AskPermission AskKind = "permission"
	// AskChoice is "which of these": several answers the asker has already
	// thought through, usually with a pick among them.
	AskChoice AskKind = "choice"
	// AskJudgement is "is this good enough" — a landed task's work, a design's
	// shape. Nothing but a person ever answers one; its safe answer is to keep
	// what is already there.
	AskJudgement AskKind = "judgement"
	// AskClarification is "I could not tell what you meant". Free text is the
	// FIRST door on this kind rather than the last, because the whole content
	// of the answer is words the asker did not have.
	AskClarification AskKind = "clarification"
	// AskConfirmation is "this is about to happen and it cannot be taken back".
	// It never runs on a clock, its safe answer is the one that changes nothing,
	// and the destructive answer never shares a key with a routine one.
	AskConfirmation AskKind = "confirmation"
	// AskLanding is a landed task's `your call` — work that finished and that
	// nobody could check (docs/design/task-states/DESIGN.md's third tier). Its
	// answers keep that design's keys exactly: `a` accept, `n` do not, `s` tell
	// it something.
	AskLanding AskKind = "landing"
	// AskAssumption is the second rung of the ladder drawn as a question: the
	// asker states what it is taking for granted and goes on unless somebody
	// strikes one. Everything on it stands until it is struck.
	AskAssumption AskKind = "assumption"
	// AskRatify is the third rung: something reversible was DONE, and this is
	// the chance to unwind it. Nothing waits on the answer, which is what makes
	// it the cheapest question on the ladder.
	AskRatify AskKind = "ratify"
)

// needsOptions reports whether a kind is meaningless without at least two
// answers written down. A clarification's answer is words the asker did not
// have, an assumption's is "all of these stand", and a ratification's is "it is
// already done" — none of the three has a list for a gate to count.
func (k AskKind) needsOptions() bool {
	switch k {
	case AskClarification, AskAssumption, AskRatify:
		return false
	}
	return true
}

// QuestionForm is how big the drawing is: THE EVIDENCE SETS THE SIZE. A
// surface may promote a form — draw a card where a line was asked for, because
// there is room — and may never demote below what the evidence needs, because
// folding a diff into one row is showing somebody less than they are deciding
// on.
type QuestionForm string

const (
	// FormLine is one row and one answers row, pinned above the message box.
	FormLine QuestionForm = "line"
	// FormCard is a head, a reason, one row per answer, and an answers row.
	FormCard QuestionForm = "card"
	// FormRoom is a page over the conversation: answers as sections with their
	// bodies and blocks, comparison, comments, and a foot that composes the
	// answer.
	FormRoom QuestionForm = "room"
	// FormSheet is many questions from one step or many hands, grouped and
	// answered together.
	FormSheet QuestionForm = "sheet"
)

// AskerKind is WHO is asking, which is the attribution a surface draws dim
// beside the head. It is never machinery vocabulary: a person reads "the model
// asks", "aforge asks", or the task's own name.
type AskerKind string

const (
	// AskerModel is the model, through the `ask` tool (lane E2 owns that door).
	AskerModel AskerKind = "model"
	// AskerEngine is aforge itself: the approval gate, the fuel gate, a merge
	// conflict — questions no model chose to ask.
	AskerEngine AskerKind = "engine"
	// AskerTask is one task node, and [Asker.Name] is the task's title.
	AskerTask AskerKind = "task"
	// AskerSurface is a window's own confirmation — stopping a run, closing a
	// tab — raised by the surface rather than by the engine.
	AskerSurface AskerKind = "surface"
	// AskerWindow is another window on this machine, and [Asker.Name] is the
	// machine or window it came from.
	AskerWindow AskerKind = "window"
)

// Asker is who put the question, and the name that goes with it where there is
// one. The name is EMPTY for the engine and the model, because "aforge asks"
// and "the model asks" are already whole sentences and a name after them would
// be a second attribution of one asker.
type Asker struct {
	Kind AskerKind `json:"kind,omitempty"`
	Name string    `json:"name,omitempty"`
}

// SubjectKind says what sort of thing a question is ABOUT, so a surface can
// find the row it already drew for it.
type SubjectKind string

const (
	// SubjectNone is a question about nothing already on screen. The head and
	// the attached blocks are the whole of what a person has to read.
	SubjectNone SubjectKind = ""
	// SubjectCall is one tool call, named by [SubjectRef.CallID].
	SubjectCall SubjectKind = "call"
	// SubjectNode is one task node, named by [SubjectRef.ID].
	SubjectNode SubjectKind = "task"
	// SubjectPage is a written page — a harness design — named by
	// [SubjectRef.Name].
	SubjectPage SubjectKind = "page"
	// SubjectRun is an adaptive run, named by [SubjectRef.Ref].
	SubjectRun SubjectKind = "run"
	// SubjectAccount is one of the person's connected accounts, named by
	// [SubjectRef.Ref] and read out in [SubjectRef.Name].
	SubjectAccount SubjectKind = "account"
)

// SubjectRef names the row a question is about, AND THE ROW IS DRAWN ONCE.
//
// That bound is consent.go's own law repeated here for every lane: two
// renderings of one call is how a person ends up approving something other than
// what they read. A question points AT the row a surface has already put on
// screen; it does not carry a second copy of it.
type SubjectRef struct {
	Kind SubjectKind `json:"kind,omitempty"`
	// ID is the numeric token where the subject has one — a task node's id.
	ID uint64 `json:"id,omitempty"`
	// CallID is the tool call's own id, as EventToolBegin and
	// EventConsentRequest both carry it.
	CallID string `json:"callId,omitempty"`
	// Ref is the string token where the subject's id is a string — a connect
	// account, an adaptive run.
	Ref string `json:"ref,omitempty"`
	// Name is the word a person reads for it, and never an id spelled out.
	Name string `json:"name,omitempty"`
}

// ── the evidence ────────────────────────────────────────────────────────────

// BlockKind is one sort of evidence an asker may attach to a question or to one
// of its answers. THE EVIDENCE SETS THE SIZE OF THE DRAWING: a question with a
// diff on it is not a question that fits on a line.
type BlockKind string

const (
	// BlockText is prose. Body is the whole of it.
	BlockText BlockKind = "text"
	// BlockDiagram is a drawing the asker made, already rendered to lines.
	BlockDiagram BlockKind = "diagram"
	// BlockTable is Rows, the first of which is the header.
	BlockTable BlockKind = "table"
	// BlockDiff is a unified diff in Body, drawn with the diff glyphs.
	BlockDiff BlockKind = "diff"
	// BlockImage is a picture at Path, which is a path this machine can open.
	BlockImage BlockKind = "image"
	// BlockLayout is a rendering of a surface the answer would produce.
	BlockLayout BlockKind = "layout"
)

// Block is one piece of evidence. Only the field its kind names is filled; the
// rest are empty, and a reader that does not know a kind draws Title and
// nothing else rather than guessing at a body it cannot read.
type Block struct {
	Kind BlockKind `json:"kind"`
	// Title is one short line above it, and "" for a block that speaks for
	// itself (the emptiness law: nothing is drawn for an empty title).
	Title string `json:"title,omitempty"`
	// Body is the text, the diagram's lines, or the diff.
	Body string `json:"body,omitempty"`
	// Rows is a table, header first.
	Rows [][]string `json:"rows,omitempty"`
	// Path is where a picture is, for BlockImage.
	Path string `json:"path,omitempty"`
}

// ── the shape of the answer the question will take ──────────────────────────

// InputKind is what a person types, toggles or drags to answer, BESIDE the
// answers the asker wrote down. Free text is always available and is never the
// only door (the ladder's last two rungs).
type InputKind string

const (
	// InputNone is a question answered entirely by picking one of its answers.
	InputNone InputKind = ""
	// InputText is one free-text box.
	InputText InputKind = "text"
	// InputBlanks is a small form: [InputShape.Blanks], each with its own kind
	// and default.
	InputBlanks InputKind = "blanks"
	// InputChecklist is several answers at once rather than one, which is the
	// one kind whose answer list may run to eight.
	InputChecklist InputKind = "checklist"
	// InputPairs is this-or-this, once per row.
	InputPairs InputKind = "pairs"
	// InputDial is a number on a range, drawn as a dial and answered with the
	// arrows — and never drawn at all on the screen-reader tier, which gets a
	// number instead.
	InputDial InputKind = "dial"
)

// BlankKind is what one blank in a small form holds, so a surface can offer the
// right completion for it rather than a bare box.
type BlankKind string

const (
	// BlankText is words.
	BlankText BlankKind = "text"
	// BlankPath is a path on this machine, which a surface may complete.
	BlankPath BlankKind = "path"
	// BlankNumber is a number.
	BlankNumber BlankKind = "number"
	// BlankChoice is one of [Blank.Choices].
	BlankChoice BlankKind = "choice"
	// BlankTime is a moment or a duration, in the words a person would say.
	BlankTime BlankKind = "time"
)

// Blank is one field of a small form: what it is called, what goes in it, and
// what it already holds. THE DEFAULT IS AN ANSWER ALREADY GIVEN — a person who
// changes nothing has answered the question, which is the whole reason a form
// beats a free-text box on the ladder.
type Blank struct {
	// Label is the field's name in the asker's own words.
	Label string `json:"label"`
	// Kind is what goes in it.
	Kind BlankKind `json:"kind,omitempty"`
	// Default is what it holds before anybody types, and "" is an honestly
	// empty field rather than a placeholder to be invented.
	Default string `json:"default,omitempty"`
	// Choices are the words a BlankChoice offers, and are empty on every other
	// kind.
	Choices []string `json:"choices,omitempty"`
}

// Dial is a number on a range. Labels name the ends and any marked points along
// it, so a person reads words rather than a bare number.
type Dial struct {
	Min     float64  `json:"min"`
	Max     float64  `json:"max"`
	Default float64  `json:"default"`
	Labels  []string `json:"labels,omitempty"`
}

// InputShape is what a person may give BESIDES a pick. Its zero value is
// InputNone, which is the ordinary case: most questions are answered by
// pressing one of the keys the asker wrote down.
type InputShape struct {
	Kind InputKind `json:"kind,omitempty"`
	// Blanks are the fields of an InputBlanks form, in the order they are
	// drawn and tabbed through.
	Blanks []Blank `json:"blanks,omitempty"`
	// Dial is the range of an InputDial, and nil on every other kind.
	Dial *Dial `json:"dial,omitempty"`
	// Prompt is the one line above a free-text box, and "" draws nothing above
	// it at all.
	Prompt string `json:"prompt,omitempty"`
}

// ── the asker's own pick ────────────────────────────────────────────────────

// Confidence is how sure the asker is of its own pick, in three words a person
// would use. It is deliberately coarse: a percentage is a number nobody can
// check, and a person deciding whether to read further wants to know whether
// the asker is guessing, not how much.
type Confidence string

const (
	// ConfidenceSure is "I would do this".
	ConfidenceSure Confidence = "sure"
	// ConfidenceFairly is "I lean this way".
	ConfidenceFairly Confidence = "fairly"
	// ConfidenceUnsure is "I genuinely do not know", which is the one value
	// that says the question was worth asking.
	ConfidenceUnsure Confidence = "unsure"
)

// Pick is the asker's own answer to its own question, and it is a POINTER on
// [Question] so that "I have no pick" is spelled once. A question with no pick
// draws no `enter →` line at all, because there is nothing for enter to take
// (the emptiness law).
type Pick struct {
	// Key names one of the question's own answers, and the gate refuses a pick
	// that names a key nobody offered.
	Key string `json:"key"`
	// Reason is one dim sentence: why this one. It is the half a person
	// actually reads before pressing enter.
	Reason string `json:"reason,omitempty"`
	// Confidence is how sure the asker is.
	Confidence Confidence `json:"confidence,omitempty"`
	// WouldChange is WHAT WOULD CHANGE THE ASKER'S MIND — "if the file is
	// generated, the other answer" — and it is the most useful line on a card,
	// because it tells a person which fact they hold that the asker does not.
	WouldChange string `json:"wouldChange,omitempty"`
}

// ── what is at stake, and what may answer without a person ──────────────────

// Stakes is what an answer costs if it turns out wrong. It, and not the kind,
// is what decides whether anything may answer on a clock.
type Stakes string

const (
	// StakesReversible is work that can be undone with nothing lost but time.
	StakesReversible Stakes = "reversible"
	// StakesCostly is work that can be undone but not cheaply — money spent,
	// an hour of a run.
	StakesCostly Stakes = "costly"
	// StakesIrreversible is work that cannot be taken back: something sent,
	// something deleted, something published. IT NEVER RUNS ON A CLOCK AND
	// NOTHING EVER ANSWERS IT BUT A PERSON, and [Question.Check] refuses a
	// question that says otherwise.
	StakesIrreversible Stakes = "irreversible"
)

// PolicyKind is what may answer a question without a person present.
type PolicyKind string

const (
	// PolicyAsk waits. It is the zero value, and it is the floor every other
	// value has to be raised above deliberately.
	PolicyAsk PolicyKind = "ask"
	// PolicyRecommendThenAuto shows the pick, waits [Policy.After], and then
	// takes the pick itself, recording DecidedByDial. It is meaningless
	// without a pick and refused on irreversible stakes.
	PolicyRecommendThenAuto PolicyKind = "recommend-then-auto"
	// PolicyDecide takes the pick at once and says so. It is how a person turns
	// a whole kind of question off, and it is never a default.
	PolicyDecide PolicyKind = "decide"
)

// Policy is what may answer this question by itself, and after how long.
//
// ITS ZERO VALUE WAITS, which is the whole safety of this type: a lane that
// forgets to fill it in gets the behaviour every lane has today.
type Policy struct {
	Kind PolicyKind `json:"kind,omitempty"`
	// After is how long a PolicyRecommendThenAuto waits before it takes the
	// pick. It is zero on every other kind.
	After time.Duration `json:"after,omitempty"`
}

// Blocking is WHAT IS PAUSED ON THIS QUESTION, which is a different fact from
// how urgent it is — and the one a person actually wants: a question nothing
// waits on is a question they may leave.
type Blocking struct {
	// Turn says the conversation's own turn is stopped on it.
	Turn bool `json:"turn,omitempty"`
	// Tasks names the work that is stopped on it, by title. It is empty when
	// nothing is, and a surface draws nothing at all rather than "0 tasks".
	Tasks []string `json:"tasks,omitempty"`
}

// Blocks reports whether anything at all is waiting on this question.
func (b Blocking) Blocks() bool { return b.Turn || len(b.Tasks) > 0 }

// AnswerScope is HOW LONG an answer lasts, and it is the person's to choose
// among the scopes the question offered.
//
// IT IS NOT [ConsentScope], and the two are spelled apart on purpose: that one
// is the approval gate's own vocabulary for how a tool memo is banked, and it
// has a third value ("rule") that is about where the answer is written rather
// than how long it lives. This one is the question object's, and every lane
// speaks it.
type AnswerScope string

const (
	// ScopeOnce answers this question and nothing else. It is the zero value.
	ScopeOnce AnswerScope = "once"
	// ScopeTask answers every question of this shape for the rest of this piece
	// of work.
	ScopeTask AnswerScope = "task"
	// ScopeProject answers it for this project.
	ScopeProject AnswerScope = "project"
	// ScopeAlways answers it everywhere, from now on. It is only ever OFFERED,
	// never assumed, and a row answered by one says so with a way to change it.
	ScopeAlways AnswerScope = "always"
)

// DecidedBy is WHO answered, and it is the field that makes a decision record
// worth keeping: a person reading the record months later wants to know whether
// they said this or whether something said it for them.
type DecidedBy string

const (
	// DecidedByPerson is somebody pressing a key. It is the ordinary answer.
	DecidedByPerson DecidedBy = "person"
	// DecidedByDial is a policy taking the asker's own pick because nobody was
	// there.
	DecidedByDial DecidedBy = "dial"
	// DecidedByRecord is an earlier decision answering this one.
	DecidedByRecord DecidedBy = "record"
	// DecidedByAsker is the asker answering itself, which happens on the
	// ratify rung: the work was already done and nobody objected.
	DecidedByAsker DecidedBy = "asker"
)

// Withdrawal is why a question stopped being a question, and who took it back.
//
// A QUESTION IS NEVER SIMPLY GONE. The subject settled, the plan changed,
// another answer made it moot — whatever it was, the person who saw it on their
// screen is owed one dim sentence saying so, or the count they were watching
// drops for no reason they can see.
type Withdrawal struct {
	// Reason is that sentence, in the asker's own words.
	Reason string `json:"reason"`
	// By is who withdrew it, in the same vocabulary [Asker] uses.
	By AskerKind `json:"by,omitempty"`
	// At is when.
	At time.Time `json:"at,omitzero"`
}

// Exchange is one round of asking back: a person's question about one of the
// answers, and the asker's reply. IT IS BOUNDED AT ONE PER ANSWER by design —
// a question that turns into a conversation is a conversation, and the box
// below is already open for one.
type Exchange struct {
	// Option names the answer this was about, and "" is a question about the
	// question itself.
	Option string `json:"option,omitempty"`
	// Asked is the person's words; Replied is the asker's.
	Asked   string    `json:"asked"`
	Replied string    `json:"replied,omitempty"`
	At      time.Time `json:"at,omitzero"`
}

// ── the question ────────────────────────────────────────────────────────────

// QuestionCap is how many questions may stand open against ONE piece of work at
// a time, and it is spelled here and nowhere else.
//
// Past it the asker is refused and told to consolidate: several questions about
// one task are a sheet, which a person answers in one sitting, and not a queue
// they meet one at a time over an afternoon. The number is small because the
// thing it bounds is somebody's attention rather than any resource this program
// holds — three open decisions about one piece of work is already a piece of
// work that has stopped.
const QuestionCap = 3

// questionOptionCap and questionChecklistCap bound how many answers one
// question may write down.
//
// FOUR IS WHAT SOMEBODY CAN HOLD IN THEIR HEAD while reading one card, and a
// fifth answer is almost always two questions that have not been separated yet.
// A checklist is the exception and gets eight, because its answers are not
// alternatives — nobody is choosing BETWEEN them, they are ticking the ones
// that apply, and the reading cost of the eighth is the reading cost of the
// second.
const (
	questionOptionCap    = 4
	questionChecklistCap = 8
)

// Question is a decision handed to a person with its evidence attached.
//
// It is ONE object for every lane in this engine (see the file header), and
// every field on it is either the asker's own account of the decision or the
// engine's account of what is waiting on it. Nothing here is a rendering: the
// forms are internal/tui3's, and two of them drawing the same value differently
// is a surface question rather than a contract one.
type Question struct {
	// ID is the token an answer names, and it is THE SAME NUMBER the lane's own
	// resolver already takes — [Event.ID] for a consent request, the node's id
	// for a proposal, [StandingNotice.ID] for a standing card. A question does
	// not mint an id of its own, because a second id for one decision is a
	// second thing an answer could name and get wrong.
	ID uint64 `json:"id"`
	// Ref is that token where the lane's is a STRING rather than a number — a
	// connect account, an adaptive run. Exactly one of ID and Ref is set.
	Ref string `json:"ref,omitempty"`
	// Kind is the LANE: which part of the engine is stopped, and therefore
	// which resolver [Agent.ResolveQuestion] applies the answer through.
	Kind QuestionKind `json:"kind"`
	// Ask is the SHAPE of the decision (see [AskKind] and the file header on
	// why these are two fields and not one).
	Ask AskKind `json:"ask"`
	// Form is the smallest drawing the evidence allows. A surface may promote
	// it and may never demote it.
	Form QuestionForm `json:"form,omitempty"`
	// Asker is who is asking, for the dim attribution beside the head.
	Asker Asker `json:"asker,omitzero"`
	// Head is the question in one sentence, in the asker's own words and in a
	// person's vocabulary — never "approval", "gate", "prompt" or "modal".
	Head string `json:"head"`
	// Reason is WHY NOW, in one dim sentence: the policy's own phrasing, the
	// task's reason, what changed. It is the half a person acts on, and
	// [Question.Check] refuses a question without one.
	Reason string `json:"reason,omitempty"`
	// Subject is the row a surface has already drawn for this, which is the row
	// the question attaches to. The question never carries a second copy of it.
	Subject SubjectRef `json:"subject,omitzero"`
	// Options are the answers the asker wrote down, in the order chips are
	// drawn. THEY ARE THE WRITER'S ACCOUNT OF WHAT IT WILL ACCEPT — a surface
	// draws these and never a list of its own, so it can never offer a key the
	// engine would drop (taskpresence.go's law about the presence file's
	// options, which is the same law one layer up).
	Options []AnswerOption `json:"options,omitempty"`
	// Input is what a person may give besides a pick.
	Input InputShape `json:"input,omitzero"`
	// Pick is the asker's own answer, or nil where it genuinely has none.
	Pick *Pick `json:"pick,omitempty"`
	// Stakes is what a wrong answer costs, and it is what decides whether a
	// clock is allowed at all.
	Stakes Stakes `json:"stakes"`
	// Policy is what may answer this without a person.
	Policy Policy `json:"policy,omitzero"`
	// Blocking is what is paused on it. Its zero value means NOTHING is, which
	// is the honest reading for a ratification and for most landings.
	Blocking Blocking `json:"blocking,omitzero"`
	// Scope are the lifetimes an answer may carry, in the order they are
	// offered. Empty means the answer is [ScopeOnce] and nothing wider was ever
	// on the table.
	Scope []AnswerScope `json:"scope,omitempty"`
	// Attach is the evidence at the head — what a person reads before the
	// answers. It is what sets [Question.Form].
	Attach []Block `json:"attach,omitempty"`
	// Asked is when it was put.
	Asked time.Time `json:"asked,omitzero"`
	// Deadline is when a clock takes the question, and it is ZERO ON EVERY
	// QUESTION THAT HAS NO CLOCK — which is all of them but the task proposal
	// today. A WAIT THAT ENDED IS NOT A NO (consent.go): what a deadline does is
	// written by the lane, and for the proposal lane it APPROVES.
	Deadline time.Time `json:"deadline,omitzero"`
	// Withdrawn is set when the question stopped being one, and nil while it
	// stands.
	Withdrawn *Withdrawal `json:"withdrawn,omitempty"`
}

// Token is the question's id as one string, whichever of the two the lane uses.
// It is what a record is keyed by and what a surface names in a log line; it is
// never drawn for a person.
func (q Question) Token() string {
	if strings.TrimSpace(q.Ref) != "" {
		return strings.TrimSpace(q.Ref)
	}
	return strconv.FormatUint(q.ID, 10)
}

// Option is the answer this question offered under one key, and false for a key
// it did not offer. A surface deciding whether a keypress is an answer asks
// THIS rather than [AnswerOptions], for the reason [PresenceQuestion.Label]
// gives: the writer's list is narrower than the kind's whenever the answers
// depend on what is being asked.
func (q Question) Option(key string) (AnswerOption, bool) {
	key = strings.TrimSpace(key)
	for _, option := range q.Options {
		if option.Key == key {
			return option, true
		}
	}
	return AnswerOption{}, false
}

// Open reports whether this question is still one: it has not been withdrawn.
func (q Question) Open() bool { return q.Withdrawn == nil }

// ── the gate ────────────────────────────────────────────────────────────────

// The refusals [Question.Check] makes. EVERY ONE OF THEM ENDS IN SOMETHING THE
// ASKER CAN DO, because the reader on the other side is a model that has to act
// on the refusal without asking again — and "invalid question" is a sentence it
// can only retry.
var (
	errQuestionNoHead = errors.New(
		"a question needs a head: one sentence, in your own words, saying what is being decided — decide, or state the assumption")
	errQuestionNoReason = errors.New(
		"a question needs a reason: one sentence saying why it is being asked now — decide, or state the assumption")
	errQuestionNoStakes = errors.New(
		"a question needs its stakes: reversible, costly or irreversible — decide, or state the assumption")
	errQuestionTooFewOptions = errors.New(
		"a question of this kind needs at least two answers written down — decide, or state the assumption")
	errQuestionTooManyOptions = fmt.Errorf(
		"a question offers at most %d answers (%d on a checklist): make the extras their own question, or consolidate them",
		questionOptionCap, questionChecklistCap)
	errQuestionClockOnIrreversible = errors.New(
		"an irreversible question never runs on a clock and nothing answers it but a person: drop the clock, or lower the stakes if it can in fact be taken back")
	errQuestionAutoOnIrreversible = errors.New(
		"an irreversible question is never answered by a policy: ask it, and wait")
	errQuestionAutoWithoutPick = errors.New(
		"a question that may answer itself needs a pick to take: name the answer you would give, and why")
)

// errQuestionUnknownPick is the refusal for a pick naming an answer nobody
// offered. It names the key, because the asker's own list is right in front of
// it and the fix is one word.
func errQuestionUnknownPick(key string) error {
	return fmt.Errorf(
		"the pick names %q, which is not one of the answers this question offers: pick one of them, or add it to the list",
		key)
}

// errQuestionDecided is the refusal for a question a record already answers. It
// reads back the decision rather than merely refusing, because the asker's next
// move is to ACT on that decision and it needs to know what it was.
func errQuestionDecided(record DecisionRecord) error {
	return fmt.Errorf("already decided: %s", record.Line())
}

// Check is THE QUESTION GATE: the last rung of the ladder, defended.
//
// It refuses rather than repairs (see the file header), and it consults the
// decision record: a question whose head matches an answer already given about
// the same subject is refused with what was decided, so the asker acts on that
// answer instead of asking a person to give it twice. Passing nil records is
// legal and means the record was not consulted — every caller inside this
// package passes [Agent.Decisions].
//
// It checks the SHAPE of one question and never how many are open; that bound
// is [QuestionCap] and it belongs to whoever is holding the set (see
// [Agent.checkQuestion]).
func (q Question) Check(records []DecisionRecord) error {
	if strings.TrimSpace(q.Head) == "" {
		return errQuestionNoHead
	}
	if strings.TrimSpace(q.Reason) == "" {
		return errQuestionNoReason
	}
	switch q.Stakes {
	case StakesReversible, StakesCostly, StakesIrreversible:
	default:
		return errQuestionNoStakes
	}
	cap := questionOptionCap
	if q.Input.Kind == InputChecklist {
		cap = questionChecklistCap
	}
	switch {
	case len(q.Options) > cap:
		return errQuestionTooManyOptions
	case q.Ask.needsOptions() && len(q.Options) < 2:
		return errQuestionTooFewOptions
	}
	if q.Pick != nil {
		if _, ok := q.Option(q.Pick.Key); !ok {
			return errQuestionUnknownPick(q.Pick.Key)
		}
	}
	if q.Stakes == StakesIrreversible {
		if !q.Deadline.IsZero() {
			return errQuestionClockOnIrreversible
		}
		if q.Policy.Kind == PolicyRecommendThenAuto || q.Policy.Kind == PolicyDecide {
			return errQuestionAutoOnIrreversible
		}
	}
	if q.Policy.Kind == PolicyRecommendThenAuto && q.Pick == nil && q.Ask != AskAssumption {
		return errQuestionAutoWithoutPick
	}
	if record, found := decidedAlready(records, q); found {
		return errQuestionDecided(record)
	}
	return nil
}

// decidedAlready finds a record that already answers this question: the same
// head, about the same subject.
//
// THE SUBJECT IS PART OF THE MATCH AND NOT AN AFTERTHOUGHT. "May I overwrite
// this file?" is one question about one path and a different question about
// another, and a record keyed on the head alone would answer for every file a
// session ever touched. A question with no subject at all matches only records
// that had none either.
func decidedAlready(records []DecisionRecord, q Question) (DecisionRecord, bool) {
	head := questionFold(q.Head)
	if head == "" {
		return DecisionRecord{}, false
	}
	for at := len(records) - 1; at >= 0; at-- {
		record := records[at]
		if questionFold(record.Head) != head {
			continue
		}
		if record.Subject != q.Subject {
			continue
		}
		return record, true
	}
	return DecisionRecord{}, false
}

// questionFold is how two heads are compared: case and surrounding space are
// not part of what a question MEANS. Nothing cleverer is attempted — a record
// match is a refusal, and a fuzzy one would refuse questions nobody had
// answered.
func questionFold(head string) string {
	return strings.ToLower(strings.Join(strings.Fields(head), " "))
}

// ── the record ──────────────────────────────────────────────────────────────

// decisionsName is the file, inside one session's folder. It sits beside
// answers.jsonl and presence.json and is spelled here for their reason: it is
// not part of what a session KEEPS, it is what a session has DECIDED, and the
// two are different enough to be different files.
const decisionsName = "decisions.jsonl"

// DecisionsPath is the record for one session folder.
func DecisionsPath(sessionDir string) string {
	return filepath.Join(strings.TrimSpace(sessionDir), decisionsName)
}

// DecisionRecord is one decision, kept.
//
// IT IS NOT [Decision] AND IT IS NOT [PendingDecision], and all three are
// spelled apart on purpose: [Decision] is the principal's answer about what a
// session should do next, [PendingDecision] is a question still waiting on
// somebody, and this is a question that has been answered and will not be asked
// again. pending.go draws the same distinction between the first two in its own
// words.
//
// THE RECORD IS THE FIRST RUNG OF THE LADDER. Before an asker may put anything
// to a person, it reads this: a question a record already answers is refused
// with what was decided ([Question.Check]), which is the difference between a
// program that learns what somebody wants and one that asks them every morning.
type DecisionRecord struct {
	// ID and Ref name the question that was answered, exactly as [Question] did.
	ID  uint64 `json:"id"`
	Ref string `json:"ref,omitempty"`
	// Kind is the lane and Ask is the shape, both as the question carried them.
	Kind QuestionKind `json:"kind,omitempty"`
	Ask  AskKind      `json:"ask,omitempty"`
	// Head is the question's own sentence, kept verbatim, because it is what a
	// later question is matched against and what a person reads in the record.
	Head string `json:"head"`
	// Subject is what it was about, and it is part of the match: the same
	// question about two files is two decisions ([decidedAlready] says why).
	Subject SubjectRef `json:"subject,omitzero"`
	// Picked are the answers given, in the words they were given under —
	// [Words] renders them.
	Picked []string `json:"picked,omitempty"`
	// Labels are those answers as a person read them, kept beside the keys
	// because a key is meaningless a month later and the question that gave it
	// a meaning is gone.
	Labels []string `json:"labels,omitempty"`
	// Change is what the person said BESIDE the pick — "2, but keep the sqlite
	// file as the source of truth" — and it is the half of an answer that a key
	// can never carry.
	Change string `json:"change,omitempty"`
	// By is who decided; Stakes says whether it can be taken back; Scope says
	// how long it lasts.
	By     DecidedBy   `json:"by,omitempty"`
	Stakes Stakes      `json:"stakes,omitempty"`
	Scope  AnswerScope `json:"scope,omitempty"`
	// Why is the person's own reason where they gave one, and it is what a
	// preference is later written from. Empty is the ordinary case and nothing
	// is drawn for it.
	Why string `json:"why,omitempty"`
	// At is when.
	At time.Time `json:"at"`
}

// Reversible reports whether this decision can still be taken back. A record
// that says otherwise reads `cannot change` rather than offering a key that
// would fail.
func (r DecisionRecord) Reversible() bool { return r.Stakes != StakesIrreversible }

// Words is what was picked, as a person read it: the labels where the record
// kept them, and the bare keys where it did not.
func (r DecisionRecord) Words() string {
	if len(r.Labels) > 0 {
		return strings.Join(r.Labels, ", ")
	}
	return strings.Join(r.Picked, ", ")
}

// Line is one decision on one line, and it is the whole rendering this package
// does of a record: head, what was picked, what was said with it, who decided,
// when, and whether it can be taken back.
//
// IT IS ONE LINE BECAUSE IT IS READ IN BULK. The model carries the whole record
// in its context before it asks anything ([DecisionsSection]), and a person
// reads it as a list under a question. A rendering that ran to a paragraph
// would be a record nobody could hold in their head, which is the same as no
// record at all.
//
// THE EMPTINESS LAW APPLIES TO EVERY SEGMENT. No change said, no `with:`; no
// reason, no reason; an unknown decider, no attribution at all.
func (r DecisionRecord) Line() string {
	parts := []string{strings.TrimSpace(r.Head) + " → " + r.Words()}
	if change := strings.TrimSpace(r.Change); change != "" {
		parts = append(parts, "with: "+change)
	}
	if by := strings.TrimSpace(string(r.By)); by != "" {
		parts = append(parts, decidedByWord(r.By))
	}
	if !r.At.IsZero() {
		parts = append(parts, r.At.Format("15:04"))
	}
	if !r.Reversible() {
		parts = append(parts, "cannot change")
	}
	return strings.Join(parts, " · ")
}

// decidedByWord is who decided, in the words a person would use rather than the
// value's own spelling. `person` is drawn as `you` because the record is read by
// the person who gave it, and nobody calls themselves the person.
func decidedByWord(by DecidedBy) string {
	switch by {
	case DecidedByPerson:
		return "you"
	case DecidedByDial:
		return "aforge, on your settings"
	case DecidedByRecord:
		return "an earlier decision"
	case DecidedByAsker:
		return "done and not objected to"
	}
	return string(by)
}

// DecisionsSection is the record as the model's context carries it: a heading
// and one line per decision, oldest first.
//
// It answers with "" for a session that has decided nothing, and a caller adds
// NOTHING for an empty section — a heading over no lines is the emptiness law
// broken in the one place it costs tokens as well as clarity.
func DecisionsSection(records []DecisionRecord) string {
	if len(records) == 0 {
		return ""
	}
	lines := make([]string, 0, len(records)+1)
	lines = append(lines, "the record")
	for _, record := range records {
		lines = append(lines, "- "+record.Line())
	}
	return strings.Join(lines, "\n")
}

// ReadDecisions reads one session's record, oldest first. A folder with no
// record is an empty slice and no error, which is every session that has not
// yet decided anything.
func ReadDecisions(sessionDir string) ([]DecisionRecord, error) {
	if strings.TrimSpace(sessionDir) == "" {
		return nil, nil
	}
	file, err := os.Open(DecisionsPath(sessionDir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()
	var records []DecisionRecord
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}
		var record DecisionRecord
		if json.Unmarshal(raw, &record) != nil {
			// A line nothing can read is a decision nothing can apply, and
			// there is nobody left to tell (answers.go drops a bad line for
			// the same reason).
			continue
		}
		records = append(records, record)
	}
	if err := scanner.Err(); err != nil {
		return records, err
	}
	return records, nil
}

// Decisions is this session's record, oldest first, and it is what
// [Question.Check] is asked against.
//
// A SESSION WITH NO FOLDER HAS NO RECORD, and answers nothing rather than
// keeping one in memory: a decision that survives only as long as the process
// is not a record, it is a cache, and a gate built on one would refuse a
// question in one window and allow it in the next.
func (a *Agent) Decisions() []DecisionRecord {
	records, _ := ReadDecisions(a.config.Place.Dir)
	return records
}

// recordDecision appends one answered question to the record and says so.
//
// EVERY FAILURE IS SILENCE, on answers.go's terms exactly: a session must not
// stall or say anything because a directory would not answer, and the answer
// has already been applied by the time this runs. What is lost is the record of
// it, which is worth strictly less than the answer.
func (a *Agent) recordDecision(record DecisionRecord) {
	dir := strings.TrimSpace(a.config.Place.Dir)
	if dir == "" {
		return
	}
	line, err := json.Marshal(record)
	if err != nil {
		return
	}
	line = append(line, '\n')
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	file, err := os.OpenFile(DecisionsPath(dir), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	if _, err := file.Write(line); err == nil {
		_ = file.Close()
		a.mu.Lock()
		a.refreshSystemLocked()
		a.mu.Unlock()
		return
	}
	_ = file.Close()
}

// decisionRecordOf is the record one answered question leaves behind. The question
// supplies what was asked and the answer supplies what was said, which is why
// both are needed: an answer alone carries a key and no sentence, and a month
// later a key is not a decision anybody can read.
func decisionRecordOf(q Question, answer Answer) DecisionRecord {
	picked := answer.Keys()
	labels := make([]string, 0, len(picked))
	for _, key := range picked {
		if option, ok := q.Option(key); ok && strings.TrimSpace(option.Label) != "" {
			labels = append(labels, option.Label)
		}
	}
	at := answer.At
	if at.IsZero() {
		at = time.Now()
	}
	return DecisionRecord{
		ID: q.ID, Ref: q.Ref, Kind: q.Kind, Ask: q.Ask,
		Head: q.Head, Subject: q.Subject,
		Picked: picked, Labels: labels, Change: strings.TrimSpace(answer.Change),
		By: answer.DecidedBy, Stakes: q.Stakes, Scope: answer.Scope,
		Why: strings.TrimSpace(answer.Why), At: at,
	}
}

// ── raising, withdrawing, and the words a question said ─────────────────────

// questionToken is how one question is keyed in [Agent.questionWords]: the lane
// and the lane's own token, joined. The lane is part of the key because two
// lanes mint ids from two counters and both start at 1.
func questionToken(kind QuestionKind, token string) string {
	return string(kind) + ":" + token
}

// rememberQuestion banks the words of a question this session has just put, and
// answers the func that forgets them.
//
// IT IS [Agent.presenceAsking]'S SHAPE, ONE LAYER WIDER, and it is meant to be
// used the same way: called by the lane that raises the question, its return
// deferred beside the lane's own cleanup, so the words go away in the same
// breath the wait does. What presence banks is the one line another window may
// answer from; what this banks is the whole object every surface will draw.
func (a *Agent) rememberQuestion(q Question) func() {
	key := questionToken(q.Kind, q.Token())
	a.mu.Lock()
	if a.questionWords == nil {
		a.questionWords = make(map[string]Question, 1)
	}
	a.questionWords[key] = q
	a.mu.Unlock()
	// AND LETTING GO OF A QUESTION NOBODY ANSWERED IS WITHDRAWING IT. This is
	// where withdrawal actually happens in the ordinary case, and it is why no
	// lane has to remember to do it: the lane's own defer runs when its wait ends
	// — the turn was interrupted, the clock started the work, the run was stopped
	// — and an entry still standing here at that moment is a question that never
	// got an answer. [Agent.ResolveQuestion] takes its entry off FIRST, so an
	// answered question is already gone by the time the lane lets go and nothing
	// is said about it.
	return func() { a.WithdrawQuestion(q.Kind, q.Token(), questionGoneReason(q)) }
}

// claimQuestion takes one question's words OFF the book and answers them, or
// false where nothing was banked.
//
// IT IS A CLAIM AND NOT A LOOK, and that is what keeps an answered question from
// being withdrawn behind its own answer: the lane that raised it is about to
// return and run the defer that withdraws whatever is still standing, so the
// answer has to have taken the entry away before it gets there.
//
// keep says the answer does NOT end the question — a steer never resolves a task
// by itself — and leaves the entry exactly where it was.
func (a *Agent) claimQuestion(kind QuestionKind, token string, keep bool) (Question, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	q, said := a.questionWords[questionToken(kind, token)]
	if said && !keep {
		delete(a.questionWords, questionToken(kind, token))
	}
	return q, said
}

// questionSaid is what one question said, or false where the lane that raised
// it banked nothing.
func (a *Agent) questionSaid(kind QuestionKind, token string) (Question, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	q, said := a.questionWords[questionToken(kind, token)]
	return q, said
}

// emitQuestion puts one question in front of whoever is watching.
//
// IT SPEAKS ON ITS OWN LANE AND NEVER ON THE TURN'S ([Agent.WatchQuestions]),
// and that is a decision rather than an omission. A turn's stream is a strict
// sequence a caller reads to its close — text, tool rows, the lane's own
// question event, the turn's end — and a second description of a moment
// threaded into it is an event every existing reader has to step over to find
// the one it was waiting for. The surfaces that draw questions hold the
// questions lane; the surfaces that do not are exactly what they were.
//
// AND IT IS STILL EMITTED AFTER THE ROW IT IS ABOUT. Each lane sends its own
// event first and this second, in that order, so a surface holding both lanes
// has already been handed the row by the time the question reaches it — which
// is the whole content of that ordering law.
func (a *Agent) emitQuestion(kind EventKind, q Question, answer *Answer) {
	event := Event{Kind: kind, ID: q.ID, Question: &q, Answer: answer}
	a.mu.Lock()
	watchers := make([]*eventStream, len(a.questionWatchers))
	copy(watchers, a.questionWatchers)
	a.mu.Unlock()
	for _, watcher := range watchers {
		watcher.send(event)
	}
}

// WatchQuestions is a standing subscription to every question this session
// raises, withdraws and has answered, for the whole life of the session rather
// than one turn. stop is never nil and calling it twice is calling it once.
//
// IT IS A LANE OF ITS OWN AND NOT THE TASK LANE, and that is deliberate rather
// than tidy. [Agent.WatchTaskUpdates] is the ROSTER's lane: a surface holding it
// reads a strict sequence of task rows — the roster replayed on open, then one
// notice per move — and a question threaded into that sequence is an event that
// lane's readers have to skip past to find the row they were waiting for. They
// are two different subscriptions because they are two different things: what
// the work is doing, and what somebody is being asked.
//
// It exists because most questions outlive the turn that raised them or never
// had one. A landed task waits on somebody's word with no turn running at all,
// a question is withdrawn on the presence heartbeat, and an answer left in
// another window arrives on that same beat — none of those has a hub to speak
// on, and a surface that only read turn streams would never hear them.
func (a *Agent) WatchQuestions() (<-chan Event, func()) {
	stream := newEventStream()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		stream.close()
		return stream.out, func() {}
	}
	a.questionWatchers = append(a.questionWatchers, stream)
	a.mu.Unlock()
	// AND WHAT IS ALREADY OPEN GOES OUT FIRST, to every new lane. A surface
	// opens this with no questions on screen — a conversation resumed from its
	// checkpoint, one switched back to behind home, a window attached over
	// --host — and everything standing was raised on lanes that closed with the
	// surface that held them. Replaying them is what makes the questions on
	// screen rebuildable from the engine's own record, and a surface that
	// watched all along re-hears what it already drew, which is drawing it once.
	for _, open := range a.OpenQuestions() {
		stream.send(Event{Kind: EventQuestion, ID: open.ID, Question: &open})
	}
	var once sync.Once
	return stream.out, func() {
		once.Do(func() {
			a.mu.Lock()
			a.questionWatchers = dropWatcher(a.questionWatchers, stream)
			a.mu.Unlock()
			stream.leave()
		})
	}
}

// AskQuestion is the door an asker inside this engine puts a question through:
// the gate, then the event, then the words banked for every surface that will
// draw it. It answers the func that takes the words back down, and a refusal
// answers a func that does nothing — a question that did not pass the gate was
// never raised and has nothing to retire.
//
// THE GATE IS ASKED AGAINST THE RECORD, which is the first rung of the ladder
// defended in the one place every asker passes through. A question a decision
// already answers never reaches a person; the asker is handed the decision
// instead.
func (a *Agent) AskQuestion(q Question) (func(), error) {
	if q.Asked.IsZero() {
		q.Asked = time.Now()
	}
	if err := a.checkQuestion(q); err != nil {
		return func() {}, err
	}
	forget := a.rememberQuestion(q)
	a.emitQuestion(EventQuestion, q, nil)
	return forget, nil
}

// checkQuestion is [Question.Check] plus the one bound that belongs to the SET
// rather than to any single question: [QuestionCap] open questions against one
// piece of work.
//
// The cap is counted against the SUBJECT and not against the session, because
// the thing it protects is a person's attention on one piece of work. Three
// questions about three different tasks is three tasks that need something;
// three questions about one is a task that has stopped, and the answer is a
// sheet.
func (a *Agent) checkQuestion(q Question) error {
	if err := q.Check(a.Decisions()); err != nil {
		return err
	}
	if q.Subject.Kind == SubjectNone {
		return nil
	}
	open := 0
	for _, other := range a.OpenQuestions() {
		if other.Subject == q.Subject && other.Token() != q.Token() {
			open++
		}
	}
	if open >= QuestionCap {
		return fmt.Errorf(
			"%d questions already stand open against this work, which is the most there may be: answer them, or put what is left into one question with several parts",
			open)
	}
	return nil
}

// WithdrawQuestion takes one question back with a reason, and says so.
//
// It is safe to call for a question nobody remembers — a second withdrawal, a
// lane that never banked its words — and does nothing then, on
// [Agent.ResolveConsent]'s terms: the thing is already gone, and there is
// nobody left to tell.
func (a *Agent) WithdrawQuestion(kind QuestionKind, token, reason string) {
	q, said := a.questionSaid(kind, token)
	if !said {
		return
	}
	a.mu.Lock()
	delete(a.questionWords, questionToken(kind, token))
	a.mu.Unlock()
	q.Withdrawn = &Withdrawal{
		Reason: strings.TrimSpace(reason),
		By:     q.Asker.Kind,
		At:     time.Now(),
	}
	a.emitQuestion(EventQuestionWithdrawn, q, nil)
}

// sweepQuestions withdraws every question whose lane has stopped waiting on it
// and that nothing took back on its way out.
//
// IT IS THE RECONCILER AND NOT THE ORDINARY PATH. Withdrawal ordinarily happens
// the moment a lane lets go of its question ([Agent.rememberQuestion]), which is
// exact and immediate. This is the beat that catches what that misses: a lane
// that banked words and was killed before its defer could run, and a lane added
// later that has not learned to let go properly.
//
// IT COMPARES THE WORDS AGAINST THE WAITS. [Agent.OpenQuestions] walks the
// lanes; anything this session said out loud and no lane is waiting on any more
// is a question whose subject went away — a consent the turn's interrupt took,
// a proposal the clock approved, a task that settled — and the person looking at
// it is owed the sentence saying so.
//
// It runs on the presence heartbeat beside [Agent.drainAnswers], which is the
// natural place for it and not merely a convenient one: that beat is the one
// that already reconciles what this session is asking against what the world
// outside it believes.
func (a *Agent) sweepQuestions() {
	open := make(map[string]bool)
	for _, q := range a.OpenQuestions() {
		open[questionToken(q.Kind, q.Token())] = true
	}
	a.mu.Lock()
	var gone []Question
	for key, q := range a.questionWords {
		if !open[key] {
			gone = append(gone, q)
		}
	}
	a.mu.Unlock()
	sort.Slice(gone, func(i, j int) bool { return gone[i].Asked.Before(gone[j].Asked) })
	for _, q := range gone {
		a.WithdrawQuestion(q.Kind, q.Token(), questionGoneReason(q))
	}
}

// questionGoneReason is the sentence a withdrawn question retires with when
// nothing more specific was said: what happened, in the words of the lane it
// came from. It never says "cancelled", "expired" or "timed out" — those are
// machinery words for something a person experiences as the thing no longer
// needing them.
func questionGoneReason(q Question) string {
	switch q.Kind {
	case QuestionConsent, QuestionRecovery:
		return "the turn moved on without it"
	case QuestionTask:
		if !q.Deadline.IsZero() {
			return "it started on its own, as the card said it would"
		}
		return "the work is no longer waiting on it"
	case QuestionLanding, QuestionConflict:
		return "the work settled"
	case QuestionFuel:
		return "the run is no longer at its gate"
	}
	return "it is no longer needed"
}

// ── the one door ────────────────────────────────────────────────────────────

// The two refusals [Agent.ResolveQuestion] makes, and both are about the CALLER
// rather than about any lane: an answer naming a lane this door does not take,
// and an answer that named no answer at all on a question that has some.
var (
	errAnswerUnknownLane = errors.New(
		"session: nothing in this conversation asks that kind of question")
	errAnswerEmpty = errors.New(
		"session: that answer names nothing — pick one of the answers the question offered, or say what you want instead")
)

// ResolveQuestion is THE ONE DOOR every answer in this engine goes through.
//
// It is how answers.go's first law — AN ANSWER IS APPLIED THROUGH THE SAME
// RESOLVER A SURFACE USES — stays literally true across eleven lanes instead
// of the three it covered. This function does not decide anything: it reads
// which lane the answer names and hands it to that lane's own resolver, which
// is exactly what the card in a window calls, what home's chip row reaches
// through answers.jsonl, and what the `--host` link replays. There is no second
// place that knows what "yes" does.
//
// A LATE ANSWER IS IGNORED AND NOTHING SAYS SO (answers.go's second law). Every
// resolver below already drops an id nobody is waiting on, so this adds no
// staleness rule of its own; it hands the id over and lets the one rule that
// exists apply. That is why an answer to a question that has already been
// answered comes back nil rather than as an error — it is not a failure, it is
// a second click.
//
// AND IT REACHES THE THREE LANES NOTHING COULD REACH. The audit found
// [Agent.ResolveConflict], [Agent.TakeBackDecision] and [Agent.AnswerSubharness]
// with a door apiece and no caller anywhere in the product: work that stopped
// on a question no surface in this program could draw, let alone answer. They
// are on this door now, so a surface has one thing to call.
func (a *Agent) ResolveQuestion(answer Answer) error {
	if answer.At.IsZero() {
		answer.At = time.Now()
	}
	if answer.DecidedBy == "" {
		answer.DecidedBy = DecidedByPerson
	}
	if len(answer.Picked) == 0 {
		if key := strings.TrimSpace(answer.Key); key != "" {
			answer.Picked = []string{key}
		}
	}
	if strings.TrimSpace(answer.Key) == "" {
		answer.Key = answer.FirstKey()
	}
	// THE WORDS ARE CLAIMED BEFORE THE LANE IS TOUCHED. The lane is about to
	// return and let go of this question, and letting go of one nobody answered
	// is withdrawing it ([Agent.rememberQuestion]) — so an answer that had not
	// taken the entry first would be raced by its own withdrawal.
	q, said := a.claimQuestion(answer.Kind, answerToken(answer), !resolvesQuestion(answer))
	if err := a.applyToLane(answer); err != nil {
		// NOTHING WAS DECIDED, SO NOTHING IS FORGOTTEN. The question is still a
		// question and still has to be drawn.
		if said && resolvesQuestion(answer) {
			a.rememberQuestion(q)
		}
		return err
	}
	// THE RECORD IS WRITTEN FROM THE QUESTION AND THE ANSWER TOGETHER, and it is
	// written after the lane took it: a decision recorded for work that was never
	// resolved is a record that refuses the next question for no reason. A lane
	// that banked no words leaves no record — there is no head to keep, and a
	// record whose question cannot be read back is a line nobody can act on.
	if said {
		a.recordDecision(decisionRecordOf(q, answer))
		if strings.TrimSpace(answer.Why) != "" && q.Pick != nil && answer.FirstKey() != q.Pick.Key {
			// An explained override is a durable preference, not merely a note on
			// this decision. The existing memory door keeps it forgettable.
			_, _ = a.RememberScoped("prefers "+strings.TrimSpace(answer.Why), memoryScopeForAnswer(answer.Scope))
		}
		a.emitQuestion(EventQuestionAnswered, q, &answer)
	}
	return nil
}

// answerToken is the answer's own id as one string, matching [Question.Token].
func answerToken(answer Answer) string {
	if ref := strings.TrimSpace(answer.Ref); ref != "" {
		return ref
	}
	return strconv.FormatUint(answer.ID, 10)
}

// resolvesQuestion reports whether this answer ENDS the question it was given
// to.
//
// A STEER NEVER RESOLVES A TASK BY ITSELF (docs/design/task-states/DESIGN.md),
// and that is the whole of what this function is for: `tell it` sends words to
// the work and the question stays exactly where it was, because "looks good"
// typed on a card must not silently become accept.
func resolvesQuestion(answer Answer) bool {
	if answer.Kind == QuestionLanding && answer.FirstKey() == LandingTellKey {
		return false
	}
	return true
}

// applyToLane hands one answer to the resolver that owns it. Every arm here is
// a call to a function that already existed and is already what a surface
// calls; nothing in this switch decides anything for itself.
func (a *Agent) applyToLane(answer Answer) error {
	key := answer.FirstKey()
	words := answer.Words()
	switch answer.Kind {
	case QuestionAsk:
		a.mu.Lock()
		wait := a.askWaits[answer.ID]
		if wait != nil {
			delete(a.askWaits, answer.ID)
		}
		a.mu.Unlock()
		if wait == nil {
			return nil
		}
		wait <- answer
		return nil
	case QuestionConsent, QuestionTask, QuestionStanding:
		// The three lanes answers.go already mapped, through the mapping it
		// already wrote: [AnswerFromKey] says what a key MEANS, and a key the
		// kind does not take is applied to nothing.
		action, ok := AnswerFromKey(answer.Kind, key)
		if !ok {
			return errAnswerEmpty
		}
		switch action.Kind {
		case QuestionConsent:
			a.ResolveConsentRemember(answer.ID, action.Allow, action.Scope)
		case QuestionTask:
			a.ResolveTask(answer.ID, action.Task)
		case QuestionStanding:
			a.ResolveStanding(answer.ID, action.Standing)
		}
		return nil
	case QuestionRecovery:
		// The stuck-turn question borrows the consent lane's wait and has three
		// answers where consent has two, so it is answered through its own
		// resolver with the choice the option key names (recovery.go).
		a.ResolveRecovery(answer.ID, RecoveryChoice(key))
		return nil
	case QuestionConnect:
		// A YES TO A QUESTION THAT WANTED A TYPED ANSWER IS NOT AN ANSWER
		// (connect.go). Words are the key or the missing half of an address and
		// go through the typed door; a bare pick goes through the other one,
		// and the lane itself reads a bare yes to a key question as a decline.
		if words != "" {
			a.ResolveConnectKey(answer.Ref, words)
			return nil
		}
		a.ResolveConnect(answer.Ref, key == "1")
		return nil
	case QuestionHarness:
		a.ResolveHarness(answer.ID, key == "1", strings.TrimSpace(answer.Comments[questionModelNote]))
		return nil
	case QuestionSubharness:
		a.ResolveSubharness(answer.ID, key == "1", nil)
		return nil
	case QuestionSubharnessAsk:
		// A RUNNING SUB-HARNESS IS ANSWERED IN WORDS, not with a key: its
		// question is its own and this engine never wrote answers for it. Taking
		// the work over is the third answer and is not a stop (subharness_env.go).
		a.AnswerSubharness(answer.ID, words, answer.TakingOver)
		return nil
	case QuestionLanding, QuestionConflict:
		return a.applyLanding(answer, key, words)
	case QuestionFuel:
		_, err := a.ResolveOrchestrate(answer.Ref, fuelAnswer(key, words))
		return err
	}
	return errAnswerUnknownLane
}

// questionModelNote is the key a harness answer carries the model under, in
// [Answer.Comments]. It is a comment rather than a field because it is one
// lane's own extra and every other lane would carry it empty
// ([Event.Model] is where the question offered it).
const questionModelNote = "model"

// applyLanding answers a landed task's `your call`, and it is the one arm of
// this door with more than two outcomes — because a landed task is the one
// question in this engine that a person can accept, refuse, look at again, hand
// over, take back, or simply talk to.
//
// THE THREE KEYS ON THE ROW ARE TASK-STATES' OWN and are never re-spelled here
// (answers.go's [LandingYesKey] and its neighbours). The two that are not on
// the row — look again, and take it back — are reachable through this door for
// the audit's reason: [Agent.TakeBackDecision] had no caller anywhere, so a
// decision the model settled could not be undone by anybody.
func (a *Agent) applyLanding(answer Answer, key, words string) error {
	switch key {
	case LandingYesKey:
		return a.ResolveUnverified(answer.ID, TaskAccept, words)
	case LandingNoKey:
		return a.ResolveUnverified(answer.ID, TaskRefute, words)
	case LandingAgainKey:
		return a.ResolveUnverified(answer.ID, TaskReaudit, words)
	case LandingDecideKey:
		return a.HandUnverifiedToModel(answer.ID)
	case LandingTakeBackKey:
		return a.TakeBackDecision(answer.ID)
	case LandingTellKey:
		// AND THE QUESTION STAYS OPEN. [resolvesQuestion] says why: words to the
		// work are a steer, and a steer never resolves a task by itself.
		if words == "" {
			return errAnswerEmpty
		}
		_, err := a.SteerTask(answer.ID, words)
		return err
	}
	if answer.Kind == QuestionConflict {
		// A CONFLICT HAS ONE ANSWER, and it is the one this engine can act on:
		// bring the branch home over the person's. Refusing it is leaving it
		// alone, which needs no door at all.
		return a.ResolveConflict(answer.ID)
	}
	return errAnswerEmpty
}

// fuelAnswer turns a fuel-gate key into the word [Agent.ResolveOrchestrate]
// takes. The top-up carries the amount the person named, and a top-up with no
// amount is the run's own default rather than nothing — the gate reads a bare
// `topup:` exactly that way.
func fuelAnswer(key, words string) string {
	switch key {
	case "1":
		return "topup:" + strings.TrimSpace(words)
	case "2":
		return orchestrate.GateFinish
	case "3":
		return orchestrate.GateStop
	}
	return strings.TrimSpace(words)
}

// ── what is open right now ──────────────────────────────────────────────────

// OpenQuestions is every decision this session is waiting on somebody for,
// oldest first, each as one [Question].
//
// IT IS DERIVED AND THERE IS NO SECOND STORE (the file header, and pending.go's
// own law one layer down). This walks the WAITS the lanes already keep — the
// consent map, the connect asks, the harness asks, the standing answers, the
// task proposals, the sub-harness offers and questions, the orchestrator's
// pause and the graph's unverified nodes — and asks [Agent.questionWords] only
// what each of them SAID. A list kept beside those would be a second place a
// question could be open, and the two would disagree the first hour a lane
// learned to close one on a road that forgot to tell this file.
//
// IT IS WHERE `NEEDS SOMEBODY` IS COUNTED FROM, and that is the audit's fourth
// finding closed: a landed task's `your call` lived in a registry of its own
// that [Agent.waitingOnPerson] never folded in, so a task sitting on somebody's
// decision left home, the switcher and the tab signal all saying there was
// nothing to do.
//
// THE LOCKS ARE TAKEN ONE AT A TIME AND NEVER NESTED, which is taskpresence.go's
// standing rule about anything holding a lock of its own: the question lanes are
// the agent's, the graph is its own, and a run's pause is the orchestrator's.
// Holding a.mu across another lock is holding the lock Interrupt has to be able
// to take.
func (a *Agent) OpenQuestions() []Question {
	var open []Question

	a.mu.Lock()
	modelAsks := make([]uint64, 0, len(a.askWaits))
	for id := range a.askWaits {
		modelAsks = append(modelAsks, id)
	}
	consent := make([]uint64, 0, len(a.consent))
	for id := range a.consent {
		consent = append(consent, id)
	}
	connects := make(map[string]connectAsk, len(a.connectAsks))
	for id, ask := range a.connectAsks {
		connects[id] = ask
	}
	harnesses := make(map[uint64]Event, len(a.harnessAsks))
	for id, ask := range a.harnessAsks {
		harnesses[id] = ask.card
	}
	standings := make([]uint64, 0, len(a.standingAnswers))
	for id := range a.standingAnswers {
		standings = append(standings, id)
	}
	proposals := make(map[uint64]TaskNotice, len(a.taskAnswers))
	for id, proposal := range a.taskAnswers {
		if proposal != nil {
			proposals[id] = proposal.notice
		}
	}
	offers := make(map[uint64]Event, len(a.subharnessOffers))
	for id, offer := range a.subharnessOffers {
		if offer != nil {
			offers[id] = offer.card
		}
	}
	askers := make(map[uint64]subharnessQuestion, len(a.subharnessAsks))
	for id, ask := range a.subharnessAsks {
		if ask != nil {
			askers[id] = *ask
		}
	}
	// THE RUNS TRAVEL WITH THEIR IDS because the id is the map's key and is on
	// nothing the run itself holds — and the id is what an answer to a fuel gate
	// names ([Agent.ResolveOrchestrate] takes it).
	runs := make(map[string]*orchestration, len(a.orchestrations))
	for id, live := range a.orchestrations {
		runs[id] = live
	}
	a.mu.Unlock()
	for _, id := range modelAsks {
		if q, ok := a.questionSaid(QuestionAsk, strconv.FormatUint(id, 10)); ok {
			open = append(open, q)
		}
	}

	for _, id := range consent {
		open = append(open, a.consentQuestion(id))
	}
	for id, ask := range connects {
		open = append(open, a.connectQuestion(id, ask))
	}
	for id, card := range harnesses {
		open = append(open, a.harnessQuestion(id, card))
	}
	for _, id := range standings {
		open = append(open, a.standingQuestion(id))
	}
	for id, notice := range proposals {
		open = append(open, a.proposalQuestion(id, notice))
	}
	for id, card := range offers {
		open = append(open, a.subharnessOfferQuestion(id, card))
	}
	for id, ask := range askers {
		open = append(open, a.subharnessAskQuestion(id, ask))
	}
	for id, live := range runs {
		if snap := live.run.Snapshot(); snap.Paused {
			open = append(open, a.fuelQuestion(id, snap.Fuel.Gauge()))
		}
	}
	for _, pending := range a.PendingDecisions() {
		open = append(open, a.landingQuestion(pending))
	}

	// OLDEST FIRST, because that is the one being answered next — and because a
	// map has no order at all, so two reads of one unchanged session would
	// otherwise hand a surface two different lists (tools_subharness.go's
	// [Agent.standingSubharnessCardsLocked] sorts its own for the same reason).
	sort.SliceStable(open, func(i, j int) bool {
		if !open[i].Asked.Equal(open[j].Asked) {
			return open[i].Asked.Before(open[j].Asked)
		}
		return open[i].Token() < open[j].Token()
	})
	return open
}

// said is the words a lane banked for one question, or the built fallback where
// it banked none. It is how every builder below starts: a lane that has learned
// to describe its own question wins, and a lane that has not yet is still
// described honestly from what it holds.
func (a *Agent) said(kind QuestionKind, token string, fallback Question) Question {
	if q, ok := a.questionSaid(kind, token); ok {
		return q
	}
	return fallback
}

// consentQuestion is the approval gate as a question.
//
// THE WORDS COME FROM THE GATE ITSELF where it banked them, because the gate is
// the only thing that knows the tool, the rule the policy matched and the gloss
// of the call. Where it did not — the stuck-turn question borrows this lane to
// ask about a TURN (recovery.go) — the fallback says only what is true: this
// session is waiting on somebody about something it has already drawn.
func (a *Agent) consentQuestion(id uint64) Question {
	token := strconv.FormatUint(id, 10)
	return a.said(QuestionConsent, token, Question{
		ID:       id,
		Kind:     QuestionConsent,
		Ask:      AskPermission,
		Form:     FormLine,
		Asker:    Asker{Kind: AskerEngine},
		Head:     a.presenceAsk().Text,
		Reason:   consentFallbackReason,
		Options:  AnswerOptions(QuestionConsent),
		Stakes:   StakesCostly,
		Blocking: Blocking{Turn: true},
	})
}

// consentFallbackReason is why the gate is asking, in the one sentence that is
// true of every question on this lane whatever the policy matched. The policy's
// own phrasing is better and rides on the banked question; this is what is left
// when there is none.
const consentFallbackReason = "it will not run this without your word"

// connectQuestion is a connect offer as a question. An account that needs a
// typed answer is a question with a box rather than a pick, because a bare yes
// to one of those is read as a decline (connect.go) and a chip that means no
// while reading yes is worse than no chip.
func (a *Agent) connectQuestion(id string, ask connectAsk) Question {
	built := Question{
		Ref:      id,
		Kind:     QuestionConnect,
		Ask:      AskPermission,
		Form:     FormLine,
		Asker:    Asker{Kind: AskerEngine},
		Head:     "connect your account?",
		Reason:   "the turn asked for something only that account can answer",
		Subject:  SubjectRef{Kind: SubjectAccount, Ref: id},
		Options:  AnswerOptions(QuestionConnect),
		Stakes:   StakesReversible,
		Blocking: Blocking{Turn: true},
		Scope:    []AnswerScope{ScopeOnce},
	}
	if ask.needsKey {
		built.Options = nil
		built.Ask = AskClarification
		built.Input = InputShape{Kind: InputText, Prompt: "the key, or the part of the address it is missing"}
	}
	return a.said(QuestionConnect, id, built)
}

// harnessQuestion is a sub-harness offer, or a written design waiting to be
// approved, as a question. The card the lane already holds is where its words
// come from: Text is the harness's name and Hint its one sentence.
func (a *Agent) harnessQuestion(id uint64, card Event) Question {
	token := strconv.FormatUint(id, 10)
	head := strings.TrimSpace(card.Text)
	if head == "" {
		head = "run a saved program for this?"
	}
	ask, form := AskPermission, FormLine
	if card.Kind == EventHarnessDesignDone {
		// A DESIGN IS A JUDGEMENT AND NOT A PERMISSION: the page is written,
		// and what is being asked is whether it is right — which is a thing
		// nothing but a person ever answers.
		ask, form = AskJudgement, FormCard
	}
	return a.said(QuestionHarness, token, Question{
		ID:       id,
		Kind:     QuestionHarness,
		Ask:      ask,
		Form:     form,
		Asker:    Asker{Kind: AskerEngine},
		Head:     head,
		Reason:   strings.TrimSpace(card.Hint),
		Subject:  SubjectRef{Kind: SubjectPage, Name: strings.TrimSpace(card.Text)},
		Options:  AnswerOptions(QuestionHarness),
		Stakes:   StakesReversible,
		Blocking: Blocking{Turn: true},
	})
}

// standingQuestion is a standing card as a question. Its answers are the ONE
// item's and not the kind's ([StandingOptions]) wherever the lane banked them —
// a one-off reminder offers no `just once`, and a list that said otherwise
// would be a chip the session drops.
func (a *Agent) standingQuestion(id uint64) Question {
	token := strconv.FormatUint(id, 10)
	return a.said(QuestionStanding, token, Question{
		ID:      id,
		Kind:    QuestionStanding,
		Ask:     AskChoice,
		Form:    FormCard,
		Asker:   Asker{Kind: AskerEngine},
		Head:    a.presenceAsk().Text,
		Reason:  "nothing stands until you say so",
		Options: AnswerOptions(QuestionStanding),
		Stakes:  StakesReversible,
		Scope:   []AnswerScope{ScopeOnce, ScopeAlways},
	})
}

// proposalQuestion is a task proposal as a question.
//
// IT IS THE ONE QUESTION IN THIS ENGINE WITH A CLOCK, and the clock APPROVES:
// the card is the person's chance to redirect, never a gate the work waits on
// forever (session.go's EventTaskProposal says the same in its own words). So
// the policy is written down as what it is — show the pick, and take it after
// the deadline — rather than left for a surface to infer from a bare time.
func (a *Agent) proposalQuestion(id uint64, notice TaskNotice) Question {
	token := strconv.FormatUint(id, 10)
	built := Question{
		ID:       id,
		Kind:     QuestionTask,
		Ask:      AskPermission,
		Form:     FormCard,
		Asker:    Asker{Kind: AskerModel},
		Head:     taskProposalLead + strings.TrimSpace(notice.Title),
		Reason:   strings.TrimSpace(notice.Summary),
		Subject:  SubjectRef{Kind: SubjectNode, ID: id, Name: strings.TrimSpace(notice.Title)},
		Options:  AnswerOptions(QuestionTask),
		Stakes:   StakesCostly,
		Blocking: Blocking{Turn: true},
		Deadline: notice.Deadline,
	}
	if !notice.Deadline.IsZero() {
		built.Pick = &Pick{Key: "1", Reason: "it starts on its own unless you say otherwise", Confidence: ConfidenceFairly}
		built.Policy = Policy{Kind: PolicyRecommendThenAuto, After: time.Until(notice.Deadline)}
	}
	return a.said(QuestionTask, token, built)
}

// taskProposalLead opens the sentence a task proposal asks with, and it is
// task.go's own lead repeated here so the card, the presence file and this
// object cannot become three accounts of one proposal.
const taskProposalLead = "wants to start a task: "

// subharnessOfferQuestion is an intake card chat raised for a saved program.
func (a *Agent) subharnessOfferQuestion(id uint64, card Event) Question {
	token := strconv.FormatUint(id, 10)
	name := strings.TrimSpace(card.Text)
	if card.Subharness != nil && strings.TrimSpace(card.Subharness.Manifest.Name) != "" {
		name = strings.TrimSpace(card.Subharness.Manifest.Name)
	}
	return a.said(QuestionSubharness, token, Question{
		ID:       id,
		Kind:     QuestionSubharness,
		Ask:      AskPermission,
		Form:     FormCard,
		Asker:    Asker{Kind: AskerEngine},
		Head:     subharnessOfferLine + name,
		Reason:   strings.TrimSpace(card.Hint),
		Subject:  SubjectRef{Kind: SubjectPage, Name: name},
		Options:  AnswerOptions(QuestionSubharness),
		Stakes:   StakesReversible,
		Blocking: Blocking{Turn: true},
	})
}

// subharnessAskQuestion is a RUNNING sub-harness's own question — the lane the
// audit found with a resolver and nothing anywhere that drew it. It has no
// answers written down because the run wrote none: what it wants is words.
func (a *Agent) subharnessAskQuestion(id uint64, ask subharnessQuestion) Question {
	token := strconv.FormatUint(id, 10)
	return a.said(QuestionSubharnessAsk, token, Question{
		ID:       id,
		Kind:     QuestionSubharnessAsk,
		Ask:      AskClarification,
		Form:     FormCard,
		Asker:    Asker{Kind: AskerTask, Name: strings.TrimSpace(ask.name)},
		Head:     strings.TrimSpace(ask.question),
		Reason:   "the work has stopped here until you answer",
		Subject:  SubjectRef{Kind: SubjectNode, ID: id, Name: strings.TrimSpace(ask.name)},
		Input:    InputShape{Kind: InputText},
		Stakes:   StakesReversible,
		Blocking: Blocking{Tasks: []string{strings.TrimSpace(ask.name)}},
		Asked:    ask.asked,
	})
}

// fuelQuestion is an adaptive run standing at its fuel gate. The gauge is the
// run's own sentence, repeated exactly as [Agent.waitingOnPerson] repeats it,
// so a person reading it on home and a person reading it on the run's page read
// the same line.
func (a *Agent) fuelQuestion(id, gauge string) Question {
	id = strings.TrimSpace(id)
	return a.said(QuestionFuel, id, Question{
		Ref:     id,
		Kind:    QuestionFuel,
		Ask:     AskChoice,
		Form:    FormCard,
		Asker:   Asker{Kind: AskerEngine},
		Head:    fuelGateLine,
		Reason:  gauge,
		Subject: SubjectRef{Kind: SubjectRun, Ref: id},
		Options: AnswerOptions(QuestionFuel),
		Stakes:  StakesCostly,
		// NOTHING IN THE CONVERSATION IS WAITING ON IT. The run is, and the run
		// is what the answer is about — a surface that said the turn was blocked
		// here would be telling somebody they cannot type.
		Blocking: Blocking{Tasks: []string{id}},
	})
}

// landingQuestion is a landed task's `your call` as a question, and it is built
// OVER [TaskAsk] rather than beside it.
//
// THE ASK TABLE IS TASK-STATES' AND IS READ, NEVER RESTATED. task_status.go
// decides which of the six shapes a your-call row is asking, its reason
// sentence, and the two words its answers wear; this reads that reading and
// dresses it as a question. A second table here would be a second answer to
// "what is this row asking", and the two would drift the day one of them
// learned a seventh shape.
func (a *Agent) landingQuestion(pending PendingDecision) Question {
	notice := pending.Notice
	status := ProjectTask(notice.StatusFacts())
	kind := QuestionLanding
	if status.Ask.Kind == TaskAskConflict {
		kind = QuestionConflict
	}
	token := strconv.FormatUint(notice.ID, 10)
	return a.said(kind, token, Question{
		ID:      notice.ID,
		Kind:    kind,
		Ask:     AskLanding,
		Form:    FormCard,
		Asker:   Asker{Kind: AskerTask, Name: strings.TrimSpace(notice.Title)},
		Head:    strings.TrimSpace(notice.Title),
		Reason:  strings.TrimSpace(status.Ask.Reason),
		Subject: SubjectRef{Kind: SubjectNode, ID: notice.ID, Name: strings.TrimSpace(notice.Title)},
		Options: landingOptions(status.Ask),
		// ACCEPTING BRINGS A BRANCH HOME AND REFUSING KEEPS ONE. Neither is free
		// and neither is beyond taking back, which is exactly `costly`.
		Stakes: StakesCostly,
		// AND NOTHING IS WAITING ON IT IN THE CONVERSATION. The work has already
		// finished; what is waiting is the decision about whether it holds, and
		// a person may leave it as long as they like.
		Blocking: Blocking{},
	})
}

// landingOptions dresses one [TaskAsk] as the three answers the row draws:
// `[a] <yes> · [n] <no> · [s] tell it`, always the same three columns in the
// same order with the same keys (docs/design/task-states/DESIGN.md).
//
// THE WORDS ARE THE ASK'S OWN and are never invented here — a row whose yes
// reads `bring it home` on the card and `accept` on home would be two names for
// one answer, which is answers.go's law about `just once` in a different lane.
// A row that carried no words falls back to the kind's ([AnswerOptions]).
func landingOptions(ask TaskAsk) []AnswerOption {
	options := AnswerOptions(QuestionLanding)
	for at := range options {
		switch options[at].Key {
		case LandingYesKey:
			if word := strings.TrimSpace(ask.Yes); word != "" {
				options[at].Label = word
			}
		case LandingNoKey:
			if word := strings.TrimSpace(ask.No); word != "" {
				options[at].Label = word
			}
		}
	}
	return options
}
