package session

// question.go is ONE OBJECT FOR EVERY DECISION THIS ENGINE HANDS TO A PERSON.
//
// Before this file there were thirteen of them. The approval gate, the connect
// offer, the harness offer, the harness design, the sub-harness proposal, a
// running sub-harness's own question, the standing card, the task proposal, a
// landed task's `your call`, the merge conflict, the fuel gate, the stuck-turn
// recovery and the auto-settle take-back each minted their own id, banked their
// own wait, emitted their own event and were answered through their own door —
// and the September 2026 questions audit counted twenty-one distinct question
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
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/orchestrate"
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

// askKinds is every shape a question can be, in the order the ladder climbs
// them. It is the list a schema offers an asker ([askSchemaJSON] writes its enum
// from it), so a kind this package can read is a kind the asker is offered and
// nothing else is.
var askKinds = []AskKind{AskPermission, AskChoice, AskJudgement, AskClarification, AskConfirmation, AskLanding, AskAssumption, AskRatify}

// Waits reports whether a question of this shape STOPS ANYTHING. Every kind
// does but one: a ratify says something reversible was already done and offers
// the chance to unwind it, so the asker carries on the moment it is shown and
// the person answers it — or does not — in their own time.
//
// IT IS READ AT BOTH ENDS AND IS THE ONE READING. The lane that raises the
// question parks a call on it only where something waits (tools_ask.go), the
// presence file counts only those as this session being stopped on somebody
// (taskpresence.go's [Agent.waitingOnPerson]), and a surface counts the same
// ones in the line that says how many questions are open — a `? 1 question`
// beside a row that settled itself is a person told they are needed when they
// are not.
func (k AskKind) Waits() bool { return k != AskRatify }

// Waiting reports whether ANYTHING IS STOPPED on this question — the one
// reading of "is somebody being waited for", and the only one the waiting desk,
// the presence file and the open-question cap are allowed to take.
//
// IT IS TWO TERMS AND BOTH ARE NEEDED. [AskKind.Waits] is the SHAPE: a ratify
// waits on nobody by definition, whatever else it carries, and a ratify that
// carried a blocking of its own would otherwise be back on the waiting desk —
// which is the defect this exists for. [Blocking.Blocks] is the FACT: what the
// lane that raised it says is actually paused, the turn or a task.
//
// The defect, measured on 2026-09-11: a standing ratify and a bash approval
// standing at once, and home said `waiting on you` with the RATIFY's line on it
// — because the desk answered with its oldest row and nothing asked whether
// that row was waiting for anything. Pressing the key answered the ratify.
func (q Question) Waiting() bool { return q.Ask.Waits() && q.Blocking.Blocks() }

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

// questionForms is every form, smallest first, which is the order the schema
// offers them in and the order a surface may promote through.
var questionForms = []QuestionForm{FormLine, FormCard, FormRoom, FormSheet}

// AskerKind is WHO is asking, which is the attribution a surface draws dim
// beside the head. It is never machinery vocabulary: a person reads "the model
// asks", "codeaf asks", or the task's own name.
type AskerKind string

const (
	// AskerModel is the model, through the `ask` tool (lane E2 owns that door).
	AskerModel AskerKind = "model"
	// AskerEngine is codeaf itself: the approval gate, the fuel gate, a merge
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
// one. The name is EMPTY for the engine and the model, because "codeaf asks"
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
	// SubjectOrder is one standing order waiting to be agreed to, named by
	// [SubjectRef.ID]. It is the card the transcript is already drawing — the
	// person's own sentence, when it wakes, what it costs and how far it reaches
	// — so a surface that finds it says the question's reason once rather than
	// under both.
	//
	// IT IS `order` AND NOT `standing` because [SubjectStanding] is already
	// taken, by the spend ledger, for the thing money was spent inside
	// (usage_spend.go). Two names for two ideas.
	SubjectOrder SubjectKind = "order"
	// SubjectAccount is one of the person's connected accounts, named by
	// [SubjectRef.Ref] and read out in [SubjectRef.Name].
	SubjectAccount SubjectKind = "account"
)

// subjectKinds is every kind of subject a question can be about, and SubjectNone
// is not among them: a question about nothing in particular says so by leaving
// the subject out.
var subjectKinds = []SubjectKind{SubjectCall, SubjectNode, SubjectPage, SubjectRun, SubjectOrder, SubjectAccount}

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

// blankKinds is every kind of blank, in the order the schema offers them.
var blankKinds = []BlankKind{BlankText, BlankPath, BlankNumber, BlankChoice, BlankTime}

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

// inputKinds is every shape of extra input, in the order the schema offers
// them. InputNone is not among them: it is the zero value and an asker says it
// by leaving input out.
var inputKinds = []InputKind{InputText, InputBlanks, InputChecklist, InputPairs, InputDial}

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
	// Secret says the answer is a CREDENTIAL and is never drawn back. A surface
	// that honours it masks the box a character at a time and shows how many
	// characters arrived, which is what a person pasting a key needs to know and
	// the whole of what they need to know. It is on the question rather than
	// decided by each surface because the asker is the only thing that knows a
	// key from a folder name, and a box drawn in the clear once is a secret on
	// somebody's screen.
	Secret bool `json:"secret,omitempty"`
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

// confidences is every value [Confidence] takes, in the order the schema offers
// them. It is the whole of what a surface can draw
// (internal/tui3's questionConfidenceWord).
var confidences = []Confidence{ConfidenceSure, ConfidenceFairly, ConfidenceUnsure}

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

// UnmarshalJSON lets a pick be written as nothing but its key. A model asked for
// `"pick":{"key":"1"}` has sent `"pick":1` and `"pick":"1"` (deepseek-v4-flash,
// 2026-09-10), and both say exactly one thing: the first answer. A type that
// decodes itself decides its own grammar (toolargs.go), so the bare forms are
// read here; the object form goes back through the one decoder, so that what
// is inside it is read, and refused, by the same rules and in the same words as
// every other argument on the belt.
func (p *Pick) UnmarshalJSON(raw []byte) error {
	text := strings.TrimSpace(string(raw))
	if text == "" || text == "null" {
		return nil
	}
	if text[0] != '{' {
		var key json.RawMessage = raw
		if text[0] != '"' {
			// A number or a truth: its own spelling is the key.
			key = json.RawMessage(strconv.Quote(text))
		}
		var bare string
		if err := json.Unmarshal(key, &bare); err != nil {
			return &toolArgumentError{field: "pick", repair: `pick takes an object: send {"pick":{"key":"1"}}`}
		}
		*p = Pick{Key: strings.TrimSpace(bare)}
		return nil
	}
	// pickFields is Pick without this method, so the object form decodes
	// without recursing into it.
	type pickFields Pick
	fixed, err := coerceArgument(raw, reflect.TypeOf(pickFields{}), "pick")
	if err != nil {
		return err
	}
	var fields pickFields
	if err := json.Unmarshal(fixed, &fields); err != nil {
		return err
	}
	*p = Pick(fields)
	return nil
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

// stakesKinds is the three, cheapest first.
var stakesKinds = []Stakes{StakesReversible, StakesCostly, StakesIrreversible}

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

// questionOutlivesTurn reports whether the turn that raised a question may end
// without it.
//
// IT IS KEYED ON WHAT THE QUESTION IS AND NEVER ON ITS KIND. Two things let a
// question outlive its turn and there is no third: the asker said it would read
// the answer whenever it came ([Question.Later]), or something that outlives the
// turn is itself waiting on it — a task, which goes on standing there after the
// conversation has moved on. Everything else is retired with the turn that
// raised it, which is what keeps unanswered ratifies and questions somebody
// asked back on from piling up in [Agent.OpenQuestions], on the presence desk
// and against [QuestionCap].
//
// IT IS NOT [Question.Waiting], and the two are spelled apart on purpose
// because they are two different questions. That one asks whether anything is
// STOPPED on this question right now, and the waiting desk reads it; this one
// asks whether the question may still stand once the turn is over, and only the
// sweep at the end of a turn reads it. A question nothing waits on may perfectly
// well outlive its turn — that is exactly what a non-blocking `ask` is for.
func questionOutlivesTurn(q Question) bool {
	return q.Later || len(q.Blocking.Tasks) > 0
}

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

// answerScopes is every lifetime an answer can have, narrowest first.
var answerScopes = []AnswerScope{ScopeOnce, ScopeTask, ScopeProject, ScopeAlways}

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
	// DecidedByWindow is ANOTHER WINDOW ON THIS CONVERSATION. It is stamped by
	// the surface that LEARNS of an answer rather than by the one that gave it
	// — the giver knows perfectly well it was a person, and the value is there
	// so the second window's receipt does not say `you` about a key somebody
	// pressed on a different screen (docs/design/questions/DESIGN.md's FIRST
	// ANSWER WINS).
	DecidedByWindow DecidedBy = "window"
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
	// ClarificationDepth orders prerequisites ahead of the question they explain.
	ClarificationDepth int `json:"clarificationDepth,omitempty"`

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
	// Batch is WHICH STEP OF A TURN RAISED IT, and it is the one thing on this
	// object that is about the question's NEIGHBOURS rather than about itself: a
	// model that calls three tools at once can put three questions on somebody's
	// screen in the same instant, and those are one thing to answer rather than
	// three ([Agent.stepToken] mints it, and the lanes raised from inside a tool
	// batch — the approval gate and the model's own `ask` — are the ones that
	// carry it). Empty is a question raised outside any step, which is every
	// landing and every fuel gate: those have no neighbours to group with.
	Batch string `json:"batch,omitempty"`
	// Later says THE ASKER IS NOT WAITING FOR THIS and will read the answer
	// whenever it comes, so the turn that raised it may end without it. It is
	// the model's own word about its own question — the `ask` tool sets it when
	// the call says nothing is blocked on the turn — and it is what keeps such a
	// question off the sweep that retires everything a turn leaves behind
	// ([questionOutlivesTurn]).
	//
	// A RATIFY DOES NOT CARRY IT, and that is not an oversight: a ratify says
	// something reversible was already done and is answered in the person's own
	// time, but it belongs to the turn that did the thing, and one still
	// standing when that turn ends is a question about work nobody is doing.
	Later bool `json:"later,omitempty"`
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
		"a question offers at most %d answers, or %d when several may be ticked at once: set input.kind to checklist and keep the answers in options, make the extras their own question, or consolidate them",
		questionOptionCap, questionChecklistCap)
	errQuestionChecklistWithoutOptions = errors.New(
		"a checklist ticks its answers, so they go in options with a key and a label each, and blanks are a form rather than a list: move the items into options, or ask for blanks instead")
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
// errQuestionUnlabelledOption is the refusal for an answer with nothing on it a
// person can read. The key is never the problem — an asker's keys are
// renumbered to digits on the way in (askDigitKeys) — but a row with no label is
// a row the person is asked to choose blind, and the asker is the only one who
// knows what it was for. Answers are counted from one, the way the keys read.
func errQuestionUnlabelledOption(nth int) error {
	return fmt.Errorf(
		"every answer needs a label a person can read, and answer %d has none: write one, or drop that answer",
		nth)
}

func errQuestionUnknownPick(key string) error {
	return fmt.Errorf(
		"the pick names %q, which is not one of the answers this question offers: pick one of them, or add it to the list",
		key)
}

// errQuestionStillOpen is the refusal for a question that is already standing
// unanswered. It names the question rather than merely refusing, because what
// the asker has to do about it is nothing: the answer reaches it as a message
// when the person gives one (tools_ask.go's [Agent.answerAsk]).
func errQuestionStillOpen(q Question) error {
	return fmt.Errorf("already asked and still open: %s — their answer will reach you as a message",
		strings.TrimSpace(q.Head))
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
	case q.Input.Kind == InputChecklist && len(q.Options) < 2:
		// The one shape a model writes wrong more than any other: the items
		// of a checklist under input.blanks with nothing in options, because
		// the schema's word for a list of things to fill in is right there.
		// The refusal names the move rather than making it, per the header.
		return errQuestionChecklistWithoutOptions
	case q.Ask.needsOptions() && len(q.Options) < 2:
		return errQuestionTooFewOptions
	}
	for at, option := range q.Options {
		if strings.TrimSpace(option.Label) == "" {
			return errQuestionUnlabelledOption(at + 1)
		}
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
	// Was is what this decision replaced, in the words it was read under, and it
	// is filled on a CHANGED decision alone ([Answer.Revises]).
	//
	// THE LINE HAS TO SAY IT OR THE RECORD READS AS A CONTRADICTION. Two lines
	// with the same head and different answers is exactly what a person changing
	// their mind leaves behind, and a reader — the model, the mark reader, the
	// person three days later — cannot tell that from the program having asked
	// the same thing twice and got two answers. Measured on the Spark,
	// 2026-09-11: a side-call read the record, found "Hello" and a file saying
	// "Hola", and set about "fixing" the file.
	Was []string `json:"was,omitempty"`
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
	clauses := r.LineClauses()
	parts := make([]string, 0, len(clauses))
	for _, clause := range clauses {
		parts = append(parts, clause.Text)
	}
	return strings.Join(parts, DecisionSep)
}

// DecisionSep joins the clauses of a record's line, and it is the separator
// every telemetry row on every surface uses.
const DecisionSep = " · "

// DecisionClause is one segment of [DecisionRecord.Line], with what it is worth
// beside it.
type DecisionClause struct {
	// Text is the segment as it reads, with no separator on either end.
	Text string
	// GiveUp is the order a row too narrow for the whole line surrenders its
	// clauses in — the HIGHEST number goes first, and zero is never given up.
	//
	// IT IS HERE RATHER THAN IN THE SURFACE THAT DOES THE GIVING UP, because a
	// clause and what it is worth are one fact about the record. A surface that
	// ranked them itself would be a second opinion about which half of a
	// decision matters, kept in a file that never sees the other half.
	GiveUp int
}

// LineClauses is [DecisionRecord.Line] before it is joined.
//
// A NARROW ROW GIVES UP A WHOLE CLAUSE AND NEVER CUTS THE LINE FROM THE RIGHT.
// Cutting is what a receipt did before this existed, and the tail is where
// everything a person cannot infer lives: at a hundred columns a long `with:`
// clause took `· you · 14:02 · c change` off the end with it, so the one line
// left behind by an answer stopped saying who gave it, when, or that it could
// still be changed. The rank says what is actually worth keeping:
//
//   - the head and what was picked are the record itself and are never given
//     up — a row with no room for them is cut rather than emptied;
//   - WHO DECIDED is never given up either. It is the one thing on the line
//     nobody can work out for themselves, and it is what keeps a receipt from
//     reading as something this person did: `another window` and `codeaf, on
//     your settings` are the whole reason the field exists;
//   - `cannot change` stays for the same kind of reason — it is a LIMIT rather
//     than a detail, and a row that dropped it would read as a decision
//     somebody could still walk back;
//   - the change said beside the pick goes first, because it is the one clause
//     the transcript and `decisions.jsonl` both still carry in full;
//   - then the time, which is the only clause on the line a person can usually
//     get from where the row is sitting.
func (r DecisionRecord) LineClauses() []DecisionClause {
	clauses := []DecisionClause{{Text: strings.TrimSpace(r.Head) + " → " + r.Words()}}
	if change := strings.TrimSpace(r.Change); change != "" {
		clauses = append(clauses, DecisionClause{Text: "with: " + change, GiveUp: 2})
	}
	if len(r.Was) > 0 {
		// IT IS NEVER GIVEN UP. A changed decision that dropped the clause
		// saying so is the contradiction this field exists to prevent.
		clauses = append(clauses, DecisionClause{Text: "changed from " + strings.Join(r.Was, ", ")})
	}
	if by := strings.TrimSpace(string(r.By)); by != "" {
		clauses = append(clauses, DecisionClause{Text: decidedByWord(r.By)})
	}
	if !r.At.IsZero() {
		clauses = append(clauses, DecisionClause{Text: r.At.Format("15:04"), GiveUp: 1})
	}
	if !r.Reversible() {
		clauses = append(clauses, DecisionClause{Text: "cannot change"})
	}
	return clauses
}

// decidedByWord is who decided, in the words a person would use rather than the
// value's own spelling. `person` is drawn as `you` because the record is read by
// the person who gave it, and nobody calls themselves the person.
func decidedByWord(by DecidedBy) string {
	switch by {
	case DecidedByPerson:
		return "you"
	case DecidedByDial:
		return "codeaf, on your settings"
	case DecidedByRecord:
		return "an earlier decision"
	case DecidedByAsker:
		return "done and not objected to"
	case DecidedByWindow:
		return "another window"
	}
	return string(by)
}

// decisionsSectionMost is how many decisions the section may list. It is
// [standingWorldMost]'s figure for [standingWorldMost]'s reason — a block
// somebody pays for on every request of every turn is a preamble and not an
// archive — and it is read from there rather than typed again.
//
// THE CAP IS ON THE RENDERING AND NEVER ON THE RECORD. [Question.Check] is
// asked against [Agent.Decisions], which stays whole: a gate that stopped
// recognising a decision because eight newer ones were made would ask a person
// something they had already settled, which is the one failure the record exists
// to prevent.
const decisionsSectionMost = standingWorldMost

// DecisionsSection is the record as the model's context carries it: a heading
// and one line per decision, oldest first, bounded to the newest
// [decisionsSectionMost] with a line saying how many older ones the file still
// holds.
//
// THE NEWEST ARE THE ONES KEPT, which is the opposite of what the standing
// section does and is right for the same reason that one leads with the
// longest-standing: a standing order is a house rule that gets more binding with
// age, and a decision is an answer to a question that came up — the one given
// this morning is the one still shaping the work, and the one from eleven
// questions ago is history. The older ones are named rather than dropped
// silently, because a model that cannot see them can still go and read them.
//
// It answers with "" for a session that has decided nothing, and a caller adds
// NOTHING for an empty section — a heading over no lines is the emptiness law
// broken in the one place it costs tokens as well as clarity.
func DecisionsSection(records []DecisionRecord) string {
	if len(records) == 0 {
		return ""
	}
	older := 0
	if len(records) > decisionsSectionMost {
		older = len(records) - decisionsSectionMost
		records = records[older:]
	}
	lines := make([]string, 0, len(records)+2)
	lines = append(lines, "the record")
	for _, record := range records {
		lines = append(lines, "- "+record.Line())
	}
	if older > 0 {
		lines = append(lines, fmt.Sprintf("- and %d older, in %s", older, decisionsName))
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
	if _, err := file.Write(line); err != nil {
		_ = file.Close()
		return
	}
	_ = file.Close()
	// AND THE SYSTEM PROMPT IS NOT REWRITTEN HERE, which is the whole of what
	// answering costs now. message[0] sits in front of every message there is,
	// so one changed byte in it re-prices the whole conversation at the uncached
	// rate on the very next request (memory.go's [Agent.refreshSystemLocked]) —
	// and every `allow once` came through this line. What the model reads about
	// this decision is the answer's own result, which is in front of it either
	// way; what the record is FOR is the gate, which reads the file
	// ([Question.Check]). The section message[0] carries is a snapshot taken
	// whenever that message is rebuilt for some other reason, and it is rendered
	// here, off the lock, so the rebuild never reads a file (memory.go's
	// [Agent.takeRecord]).
	a.takeRecord()
}

// SecretAnswer reports whether what a person types into this question is a
// CREDENTIAL rather than a sentence — an API key, a token, the half of an
// address that authorises it.
//
// IT IS THE ONE READING OF [InputShape.Secret], and far more hangs on it than
// how a box is drawn: [withoutSecretWords] takes a secret's words off every
// copy of the answer that leaves the lane that asked for them, so a key never
// reaches the record, the questions lane, a `--host` frame or the model's
// prompt. A surface asks the same question to mask its box.
func (q Question) SecretAnswer() bool { return q.Input.Secret }

// withoutSecretWords is one answer as EVERYTHING BUT THE LANE THAT ASKED FOR IT
// may see it: the pick kept, every word the person typed gone.
//
// A SECRET IS SPENT ON THE LANE AND NOWHERE ELSE. By the time this runs the
// lane has already been handed the real answer ([Agent.applyToLane]) — connect
// has the key and has stored it where keys are stored — and what is left is
// enough to say that a key arrived, when, and who gave it, which is the whole
// of what a record is for. What is dropped is the key itself.
//
// IT IS ENFORCED IN ONE PLACE BECAUSE THE READERS ARE THE PROBLEM. An answer
// leaving [Agent.ResolveQuestion] takes four roads at once — the decision file
// on disk, the questions lane every window and every `--host` frame reads, the
// preference [Agent.rememberOverride] would write, and message[0]'s own
// snapshot of the record, which is the model's next request — and a rule kept
// at four doors is a rule that is kept at three. Observed on 2026-09-11: an
// OpenRouter key typed into the connect box was written to decisions.jsonl,
// rendered into the system prompt as `with: sk-or-…` and sent to the provider.
//
// EVERY TYPED FIELD GOES, not only the one the lane reads. A person pasting a
// key has no idea which box the engine will read it out of, and a key in the
// wrong field is still a key on disk.
func withoutSecretWords(q Question, answer Answer) Answer {
	if !q.SecretAnswer() {
		return answer
	}
	answer.Change = ""
	answer.Reframe = ""
	answer.Why = ""
	answer.Comments = nil
	answer.Blanks = nil
	answer.AskedBack = nil
	return answer
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

// stepToken names the step of the turn now running: one token shared by every
// question the SAME tool batch raises, and a different one after the next
// request goes out.
//
// IT IS THE TURN'S OWN COUNT AND NOT A NEW CLOCK. [episode.decisionBegins]
// advances it at the one moment a step begins — immediately before a request is
// sent, where the transcript horizon is already stamped — so a question raised
// while that request's tools run wears the number of the request that asked for
// them. A conversation with no turn running carries whatever the last step was,
// which is why only the lanes raised from INSIDE a batch wear it.
func (a *Agent) stepToken() string {
	return "step:" + strconv.FormatUint(a.stepSeq.Load(), 10)
}

// questionToken is how one question is keyed in [Agent.questionWords]: the lane
// and the lane's own token, joined. The lane is part of the key because two
// lanes mint ids from two counters and both start at 1.
func questionToken(kind QuestionKind, token string) string {
	return string(kind) + ":" + token
}

// questionAsked names the question one event on a TURN'S STREAM put in front of
// somebody, and false for an event that asks nothing.
//
// IT EXISTS FOR THE REPLAY AND FOR NOTHING ELSE ([eventHub.attach]). A turn's
// backlog is every event that turn has sent, kept verbatim so a surface arriving
// mid-turn reads the work from its first line — and a card is the one kind of
// event in it that is not a report of something that happened but a QUESTION
// about something that has not. A decision somebody already made must not be
// replayed as one still waiting for them, so the replay asks this which question
// a card was, and [Agent.askingLocked] whether that question is still open.
//
// THE KEYS ARE THE LANES' OWN AND ARE NEVER GUESSED. Each case names the field
// its lane's question is keyed by — consent's id, the proposal's node id, the
// standing item's id inside its card, the harness offer's id, connect's string —
// and each is the same token the lane's builder further down this file gives the
// question it banks, so the two cannot mean different things. A kind missing
// from this list replays exactly as it always did, which is the safe half of
// being wrong about one.
func questionAsked(event Event) (string, bool) {
	switch event.Kind {
	case EventConsentRequest:
		if event.ID == 0 {
			return "", false
		}
		return questionToken(QuestionConsent, strconv.FormatUint(event.ID, 10)), true

	case EventTaskProposal:
		if event.Task == nil || event.Task.ID == 0 {
			return "", false
		}
		if event.Task.Decided != nil || event.Task.Withdrawn != "" {
			// A CARD THAT STATES ITS OWN OUTCOME IS NOT ASKING ANYTHING. The
			// proposal is the one lane that restates its card when it is settled
			// ([taskWait.decided], [taskWait.withdraw]), and that restatement is
			// what the replay carries in the open card's place: the assignment,
			// with the answer under it.
			return "", false
		}
		return questionToken(QuestionTask, strconv.FormatUint(event.Task.ID, 10)), true

	case EventStandingProposal:
		if event.Standing == nil || event.Standing.ID == 0 {
			return "", false
		}
		return questionToken(QuestionStanding, strconv.FormatUint(event.Standing.ID, 10)), true

	case EventHarnessOffer:
		if event.ID == 0 {
			return "", false
		}
		return questionToken(QuestionHarness, strconv.FormatUint(event.ID, 10)), true

	case EventConnectAsk:
		id := strings.TrimSpace(event.ConnectID)
		if id == "" {
			return "", false
		}
		return questionToken(QuestionConnect, id), true
	}
	return "", false
}

// stillAskedLocked is every question this session is still waiting on somebody for,
// as the keys [questionAsked] names a card by. The caller holds a.mu.
//
// IT IS THE BANKED WORDS AND NOT A SECOND LIST, which is the whole reason it can
// be read here: [Agent.rememberQuestion] puts a question on that book as it is
// raised and [Agent.claimQuestionLocked] takes it off the moment it is answered
// or withdrawn, under this same lock — so "still on the book" is exactly "still
// a question", with no lock of anybody else's to take and no reconciliation to
// get wrong. [Agent.OpenQuestions] walks the lanes themselves and takes three
// locks doing it, which is the reading a surface asks for and not one an attach
// may make while it holds this one.
func (a *Agent) stillAskedLocked() map[string]bool {
	if len(a.questionWords) == 0 {
		return nil
	}
	asking := make(map[string]bool, len(a.questionWords))
	for key := range a.questionWords {
		asking[key] = true
	}
	return asking
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

// putBackQuestion banks the words of a question whose answer was REFUSED, which
// is the one road back onto the book that is not a raise: the question was
// claimed a moment ago by the answer this door would not take, nothing was
// decided, and the question is still on screen and still has to be answerable.
// It says nothing on the lane, because nothing happened that a surface did not
// already know ([Agent.raiseQuestion] is the door for a question that is new).
func (a *Agent) putBackQuestion(q Question) { _ = a.rememberQuestion(q) }

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
	return a.claimQuestionLocked(kind, token, keep)
}

// claimQuestionLocked is that claim with a.mu ALREADY HELD, for a road that has
// to take the words off the book in the same locked section as something else.
//
// THE ONLY SUCH ROAD IS A QUESTION THE PERSON TALKED PAST (steerquestion.go),
// and it needs this for a reason worth stating: closing the `ask` channel makes
// that lane runnable at once, and the lane's own defer withdraws whatever is
// still standing with the sentence it uses when a turn simply moved on
// ([questionGoneReason]). Two roads racing for one entry is a coin flip over
// what the person reads about their own act. Claiming both the wait and the
// words under one lock makes the loser provably find nothing, which is the rule
// askwait.go already keeps for the channels.
func (a *Agent) claimQuestionLocked(kind QuestionKind, token string, keep bool) (Question, bool) {
	key := questionToken(kind, token)
	q, said := a.questionWords[key]
	if said && !keep {
		delete(a.questionWords, key)
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
	if a.questionParent != nil {
		a.questionParent(event)
	}
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
	for _, event := range a.discussionEvents {
		stream.send(event)
	}
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

// raiseQuestion is THE ONE DOOR EVERY LANE RAISES A QUESTION THROUGH, and it is
// the whole of a question's life on the questions lane: raised here, then
// answered through [Agent.ResolveQuestion] or withdrawn through the func this
// hands back — never anything else, and never one without the other.
//
// IT EXISTS BECAUSE FIVE LANES HAD HALF OF IT. The model's `ask`, consent, a
// task proposal, a standing card and a landing banked their words and spoke on
// this lane; connect, the harness offer and design, a sub-harness intake card, a
// running sub-harness's own question and an adaptive run at its fuel gate only
// sent their own turn event. A window that learned one of those from the
// replay a lane opens with had nothing that would ever close it — no answered,
// no withdrawn — so it went on saying somebody was needed after the question
// was settled, in every window but the one that answered it. A lane that goes
// through here cannot be raised without the words that make the answer and the
// withdrawal speak.
//
// THE ORDER IS THE LAW, and it lives here so no lane has to keep it. The words
// are banked FIRST, because a surface that has the lane's own event can answer
// before the next line runs and an answer that finds no words is an answer
// nobody records or announces. The lane's own announcement goes SECOND (announce,
// nil for a lane that has none) — the tool row, the consent request, the card —
// because a question attaches to a row a surface has already drawn. The
// question goes out on its own lane LAST.
//
// letGo is the lane's to call when it stops waiting, and calling it for a
// question somebody answered does nothing: the answer claimed the words first
// ([Agent.claimQuestion]), and letting go of a question nobody answered is
// withdrawing it.
func (a *Agent) raiseQuestion(q Question, announce func()) (letGo func()) {
	if q.Asked.IsZero() {
		q.Asked = time.Now()
	}
	letGo = a.rememberQuestion(q)
	if announce != nil {
		announce()
	}
	a.emitQuestion(EventQuestion, q, nil)
	// AND EVERY OTHER WINDOW LEARNS AT ONCE. Home, the switcher and a second
	// terminal read the presence file, which is otherwise rewritten on its
	// heartbeat; a question somebody is needed for is the one fact worth not
	// waiting a beat to say.
	a.nudgePresence()
	return letGo
}

// AskQuestion is the door an asker inside this engine puts a question through:
// the gate, then [Agent.raiseQuestion] with the question's row on the presence
// desk beside it. It answers the func that takes both back down, and a refusal
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
	// THE DESK ROW GOES UP WITH THE QUESTION, NOT ONLY THE WORD BOOK.
	//
	// [Agent.rememberQuestion] alone banks what [Agent.OpenQuestions] reads,
	// which is enough for the window holding this conversation and nothing at
	// all for anybody else: home, another window and the `--host` link all read
	// the PRESENCE file ([Agent.presenceAskingQuestion]). A question that only
	// reached the word book was a question you could answer in the one place you
	// were already standing — which is the opposite of what a question object is
	// for. Observed: three `ask` calls waiting and home drawing the conversation
	// as `working`.
	return a.presenceAskingWhole(q, nil), nil
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
	// AND A QUESTION THAT IS STILL ON SOMEBODY'S SCREEN IS NOT ASKED TWICE. It
	// is [decidedAlready]'s twin one step earlier: that one refuses a question a
	// record has answered, this one refuses a question nobody has answered YET —
	// which a lane could not raise before questions began outliving the call that
	// asked them (tools_ask.go's [askOpen]) and can now. The refusal says where
	// the answer will arrive, because the asker's next move is to carry on.
	head := questionFold(q.Head)
	open := 0
	for _, other := range a.OpenQuestions() {
		if other.Token() == q.Token() && other.Kind == q.Kind {
			continue
		}
		if head != "" && questionFold(other.Head) == head && other.Subject == q.Subject {
			return errQuestionStillOpen(other)
		}
		// AND THE CAP COUNTS WHAT IS WAITING, on the same one reading the
		// waiting desk takes ([Question.Waiting]). The thing it protects is a
		// person's attention on one piece of work that has STOPPED; a ratify
		// beside it stopped nothing, and holding a slot for one would refuse the
		// question that actually needs answering.
		if q.Subject.Kind != SubjectNone && other.Subject == q.Subject && other.Waiting() {
			open++
		}
	}
	if q.Subject.Kind == SubjectNone {
		return nil
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
	// THE LOOK AND THE TAKING ARE ONE HOLD, so "withdrawn exactly once" is true
	// rather than nearly true: two callers that both read a standing question
	// before either deleted it would both go on to announce the withdrawal, and
	// every window would draw the sentence twice.
	q, said := a.claimQuestion(kind, token, false)
	if !said {
		return
	}
	a.sayWithdrawn(q, reason)
}

// sayWithdrawn is the second half of a withdrawal: the sentence, said to every
// surface drawing the row. It is separate from the claim because a road that
// claimed the words under a lock it was already holding still owes the saying
// once that lock is down (steerquestion.go), and there must not be two spellings
// of what a withdrawal looks like.
func (a *Agent) sayWithdrawn(q Question, reason string) {
	q.Withdrawn = &Withdrawal{
		Reason: strings.TrimSpace(reason),
		By:     q.Asker.Kind,
		At:     time.Now(),
	}
	a.emitQuestion(EventQuestionWithdrawn, q, nil)
	// AND EVERY OTHER WINDOW LEARNS AT ONCE, for the reason the raise does
	// ([Agent.raiseQuestion]): a question that is no longer being asked must not
	// go on saying somebody is needed for a beat of the heartbeat.
	a.nudgePresence()
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
	case QuestionConsent:
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
	// The model's own question and everything else: the turn that raised it
	// has ended — interrupted, or finished around it — which is the one way a
	// question with no subject of its own stops being asked. It is the consent
	// line's sentence because it is the consent line's fact, and a row that
	// said `no longer needed · it is no longer needed` said nothing twice.
	return "the turn moved on without it"
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
	if child, original, ok := a.discussionAnswer(answer); ok {
		return child.ResolveQuestion(original)
	}
	if answer.Clarify {
		return a.clarifyQuestion(answer)
	}
	if len(a.discussionQuestions()) > 0 {
		return errors.New("answer the clarification's question first")
	}
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
	// A REVISION IS THE OTHER THING AN ANSWER CAN BE, and it takes its own road
	// for one reason: every lane below is a resolver handing an answer to
	// somebody WAITING for it, and a question that has been settled has nobody
	// waiting. [Agent.reviseLane] applies it and answers what was asked, so the
	// record and the announcement below are made exactly as they are for a first
	// answer — a changed decision is a decision, and it is kept and said the
	// same way.
	if answer.Revises {
		q, prior, err := a.reviseLane(answer)
		if err != nil {
			return err
		}
		record := decisionRecordOf(q, answer)
		if prior != nil {
			record.Was = decisionRecordOf(q, *prior).Labels
		}
		a.recordDecision(record)
		a.emitQuestion(EventQuestionAnswered, q, &answer)
		return nil
	}
	// THE WORDS ARE CLAIMED BEFORE THE LANE IS TOUCHED. The lane is about to
	// return and let go of this question, and letting go of one nobody answered
	// is withdrawing it ([Agent.rememberQuestion]) — so an answer that had not
	// taken the entry first would be raced by its own withdrawal.
	q, said := a.claimQuestion(answer.Kind, answerToken(answer), !resolvesQuestion(answer))
	// A LANDING ANSWER STILL OWES THE EVENT WHEN NOTHING WAS BANKED. Before
	// publishLandingQuestion started banking, and for an answer that reaches
	// this door from a restored graph whose raise was only replayed, claim
	// answers false — and without an EventQuestionAnswered a `--host` surface
	// that never closed the question itself keeps drawing it open. Capture the
	// words before apply settles the node out of PendingDecisions.
	landingSaid := !said && resolvesQuestion(answer) &&
		(answer.Kind == QuestionLanding || answer.Kind == QuestionConflict)
	if landingSaid {
		q = a.questionForLandingAnswer(answer)
	}
	if err := a.applyToLane(answer); err != nil {
		if errors.Is(err, errAnswerSettled) {
			// THE LOSER OF A RACE RECORDS NOTHING AND SAYS NOTHING. Somebody
			// else's answer to this question got there first — the clock and a
			// person can both reach this door in the same instant — and it has
			// already been recorded, announced and given to the model. Writing a
			// second record here is how the record and the windows came to show
			// one answer while the model had been told the other. The question is
			// NOT put back: it really is answered.
			return nil
		}
		// NOTHING WAS DECIDED, SO NOTHING IS FORGOTTEN. The question is still a
		// question and still has to be drawn.
		if said && resolvesQuestion(answer) {
			a.putBackQuestion(q)
		}
		return err
	}
	// AND A SECRET'S WORDS GO NO FURTHER THAN THE LANE THAT ASKED FOR THEM,
	// which has just had them on the line above. Everything below this point
	// writes, keeps or announces the answer, and none of it may carry a key
	// ([withoutSecretWords] says why it is enforced here and not at each
	// reader).
	answer = withoutSecretWords(q, answer)
	// AN ANSWER THAT DID NOT END THE QUESTION ENDS HERE. A steer on a landed
	// task, a change asked of a design, a question asked back on the model's own
	// `ask`: all three send WORDS and leave the question standing
	// ([AnswerResolves]), so there is no decision to record and nothing to
	// announce as answered — a lane event saying so would close the question in
	// every window but this one, over a decision nobody has made yet.
	if !resolvesQuestion(answer) {
		return nil
	}
	// THE RECORD IS WRITTEN FROM THE QUESTION AND THE ANSWER TOGETHER, and it is
	// written after the lane took it: a decision recorded for work that was never
	// resolved is a record that refuses the next question for no reason.
	//
	// A LANDING ANSWERED WITH NOTHING BANKED IS RECORDED TOO, when the record
	// can be read back. Its words are synthesized
	// ([Agent.questionForLandingAnswer]) with the head and subject the next
	// raise consults ([decidedAlready] in [Agent.landingQuestion]); the one
	// road that synthesizes no head — the pending decision already gone, the
	// restore race — writes nothing, because a record whose question cannot
	// be read back is a line nobody can act on and a gate that never matches.
	// The answered event still goes out: the window holding the card closes on
	// the event, not on the record. Other lanes banked nothing and leave no
	// record for the same law.
	if said || (landingSaid && strings.TrimSpace(q.Head) != "") {
		a.recordDecision(decisionRecordOf(q, answer))
		// THE COMPLETION CHECK READS THIS, NOT THE TOOL RESULT. A digest clips
		// results, and "nothing was set up" then reads as work still owed. The
		// person's own answer is what closes that gap (checkpoint.go).
		a.rememberPersonCardAnswer(q, answer)
	}
	if said {
		a.rememberOverride(q, answer)
	}
	if said || landingSaid {
		a.emitQuestion(EventQuestionAnswered, q, &answer)
	}
	// AND EVERY OTHER WINDOW LEARNS THE SESSION IS NO LONGER STOPPED, on the
	// beat the answer lands rather than on the presence heartbeat's next tick:
	// home, the switcher and a second terminal all read that file, and a `needs
	// you` that stands for another five seconds after a key was pressed is the
	// oldest complaint about this lane ([Agent.raiseQuestion] says the same for
	// the raise).
	a.nudgePresence()
	return nil
}

// rememberOverride keeps the person's own reason for going against the pick, as
// a durable preference rather than a note on one decision. The existing memory
// door keeps it forgettable.
//
// IT RUNS BESIDE THE ANSWER AND NEVER IN FRONT OF IT. [Agent.RememberScoped]
// weighs the new line against what is already kept, which is a model call
// (memory.go's applyCandidate) — and this door is the one a person's keystroke
// is waiting on, over a wire with a deadline on it. A preference written a
// moment later is worth exactly what one written now is; a key that did not
// land for as long as a reflex call takes is not.
func (a *Agent) rememberOverride(q Question, answer Answer) {
	why := strings.TrimSpace(answer.Why)
	if why == "" || q.Pick == nil || answer.FirstKey() == q.Pick.Key {
		return
	}
	scope := memoryScopeForAnswer(answer.Scope)
	// AND IT IS A TRACKED BACKGROUND PASS AND NOT A BARE `go`, on the one
	// mechanism every background memory pass in this package uses
	// ([Agent.startMemoryJob]): the work is registered, so [Agent.Close] waits
	// for it rather than leaving a reflex call writing into a store the session
	// has finished with, and it is refused outright once the session is closing.
	if !a.startMemoryJob() {
		return
	}
	go func() {
		defer a.memoryJobs.Done()
		_, _ = a.RememberScoped("prefers "+why, scope)
	}()
}

// questionForLandingAnswer is the words a landing answer is ABOUT, captured
// before apply settles the node. Prefer the pending decision's own shape; fall
// back to the lane the answer named so the event still carries a token a
// surface can match.
func (a *Agent) questionForLandingAnswer(answer Answer) Question {
	for _, pending := range a.PendingDecisions() {
		if pending.Notice.ID != answer.ID {
			continue
		}
		q := a.landingQuestion(pending)
		if answer.Kind != "" {
			q.Kind = answer.Kind
		}
		return q
	}
	return Question{ID: answer.ID, Kind: answer.Kind, Ask: AskLanding}
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
func resolvesQuestion(answer Answer) bool { return AnswerResolves(answer) }

// AnswerResolves reports whether this answer ENDS the question it was given to,
// or leaves it open for another one.
//
// IT IS EXPORTED BECAUSE THE SURFACE HAS TO ASK THE SAME QUESTION. A block that
// took every answer as the end of a question would clear the rows, write the
// receipt and stop counting the question — while the engine, correctly, left the
// lane waiting — so the person would be looking at a decision that was still
// theirs to make with nothing on screen to make it with. One reading, at both
// ends (tui3's [app.answerQuestion] calls this before it closes anything).
//
// Two answers do it, and both for the same reason: what they send is WORDS, and
// words are how you ask for something different rather than how you settle.
//
//   - `tell it` on a landed task steers the work and leaves the `your call`
//     exactly where it was (docs/design/task-states/DESIGN.md).
//   - `change it` on a finished design hands the page back to the designer, which
//     rewrites it and puts it in front of you again ([HarnessChangeKey]).
func AnswerResolves(answer Answer) bool {
	if answer.Clarify {
		return false
	}
	switch answer.Kind {
	case QuestionAsk:
		// AND ASKING BACK IS NOT ANSWERING. The third answer to the model's own
		// question is a question: "what do you mean by the second one?" sends the
		// words and leaves the decision where it was, which is what the block and
		// the room both draw while they go on holding it (tools_ask.go's
		// [Agent.answerAsk] does the engine's half).
		return !onlyAsksBack(answer)
	case QuestionLanding:
		return answer.FirstKey() != LandingTellKey
	case QuestionHarness:
		// THE SHAPE IS PART OF THE TEST. `2` is `not now` on an OFFER, which
		// ends the question outright, and `change it` on a DESIGN, which does
		// not — one digit, two questions, told apart by the shape the asker gave
		// them ([HarnessOptions]).
		return answer.Ask != AskJudgement || answer.FirstKey() != HarnessChangeKey
	}
	return true
}

// onlyAsksBack reports whether all this answer carries is words FOR the asker:
// something asked back, with no key picked and nothing said beside a pick. An
// answer that asks back AND decides — the room's own compose step sends both —
// is an answer, and the exchange rides along in the record.
func onlyAsksBack(answer Answer) bool {
	if len(answer.AskedBack) == 0 {
		return false
	}
	return len(answer.Keys()) == 0 && strings.TrimSpace(answer.Words()) == ""
}

// applyToLane hands one answer to the resolver that owns it. Every arm here is
// a call to a function that already existed and is already what a surface
// calls; nothing in this switch decides anything for itself.
func (a *Agent) applyToLane(answer Answer) error {
	key := answer.FirstKey()
	words := answer.Words()
	switch answer.Kind {
	case QuestionAsk:
		// THE MODEL'S OWN LANE DELIVERS RATHER THAN RELEASES (asklane.go): the
		// answer reaches the call parked on it where there is one, and the
		// conversation's own queue where there is not. The delivery cannot block
		// and the claim is the ownership, so the handover is one locked step
		// rather than the read-unlock-send it was.
		return a.answerAsk(answer)
	case QuestionConsent, QuestionTask, QuestionStanding:
		if answer.Kind == QuestionStanding && key == "" && words != "" {
			// A STANDING CARD ANSWERED IN WORDS IS A CORRECTION, and it is not a
			// yes. "make it 2pm" says the arrangement is nearly right and names
			// what is wrong with it; the model re-proposes on those words
			// ([StandingAnswer.Change]), so nothing stands until the corrected
			// card is agreed to. It is the answer the card's own `change when or
			// where` chip asked for before the block drew this lane, and the
			// block asks for it the way every other question does — `c`, then the
			// box (tui3's questionkeys.go).
			a.ResolveStanding(answer.ID, StandingAnswer{Change: words})
			return nil
		}
		if answer.Kind == QuestionTask && key == "" && words != "" {
			// A PROPOSAL ANSWERED IN WORDS IS APPROVED, AND THE WORDS ARE THE
			// REDIRECT. It is the one lane on this door where a sentence is a
			// whole answer rather than a note beside one: the most valuable
			// thing a person can do with a groomed piece of work is CORRECT
			// it, and correcting it is saying yes to the corrected version.
			// The words are appended to the brief by the runner
			// ([TaskAnswer.Redirect]), so what travels is verbatim.
			//
			// IT IS NOT A HIDDEN DIALECT. The surface that raised this door
			// used to read a bare "no" typed into the box as a decline and
			// anything longer as a redirect, which meant one of the two
			// answers was reachable by a word nothing on screen had named.
			// The answers are on the row with their keys; the box is words.
			a.ResolveTask(answer.ID, TaskAnswer{
				Approved: true, Redirect: words, Model: answer.Blanks[TaskModelBlank],
			})
			return nil
		}
		// The three lanes answers.go already mapped, through the mapping it
		// already wrote: [AnswerFromKey] says what a key MEANS, and a key the
		// kind does not take is applied to nothing.
		action, ok := AnswerFromKey(answer.Kind, key)
		if !ok {
			return errAnswerEmpty
		}
		switch action.Kind {
		case QuestionConsent:
			a.ResolveConsentRemember(answer.ID, action.Allow, ConsentScopeOf(action, answer))
		case QuestionTask:
			// AND THE HOLE IN THE SENTENCE IS PART OF THE YES. A proposal whose
			// shortlist the harness could not settle carries one choice blank
			// ([TaskModelShape]); what a person left in it travels on the answer
			// they gave, so `start it` starts it on the model the card was
			// showing them. [TaskAnswer.Model] states what an empty one means and
			// what happens to a name outside the shortlist — both are the leading
			// option, which is what the card was showing.
			task := action.Task
			task.Model = answer.Blanks[TaskModelBlank]
			a.ResolveTask(answer.ID, task)
		case QuestionStanding:
			a.ResolveStanding(answer.ID, action.Standing)
		}
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
		if !AnswerResolves(answer) {
			// `change it` ON A DESIGN TOUCHES NOTHING. The page is still waiting,
			// the lane is still holding it, and what actually happens next is a
			// sentence said to the design's own thread — which is a turn and not
			// an answer ([HarnessChangeKey]).
			return nil
		}
		a.ResolveHarness(answer.ID, key == HarnessSaveKey, strings.TrimSpace(answer.Comments[HarnessModelNote]))
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

// HarnessModelNote is the key a harness answer carries the model under, in
// [Answer.Comments]. It is a comment rather than a field because it is one
// lane's own extra and every other lane would carry it empty
// ([Event.Model] is where the question offered it).
//
// IT IS EXPORTED BECAUSE THE SURFACE FILLS IT. The model the OFFER SHOWED is
// what the person read before they pressed a key, so it travels back with the
// answer rather than being looked up again on the far side — where the lane may
// by then be holding something else ([Agent.ResolveHarness] takes it).
const HarnessModelNote = "model"

// AnswerBanked is the key a widening answer carries under, in [Answer.Comments],
// when the SURFACE has already written the permission down somewhere the person
// can find and change it — the shape of a shell command, in the words they
// picked out of it.
//
// IT IS WHAT KEEPS A NARROW YES FROM WIDENING ITSELF. The session memo this
// engine writes for a [ConsentToolSession] answer is keyed by the tool's NAME
// alone, so on `bash` it means every command for the rest of the conversation —
// and a person who read `git status*` and pressed a key must not buy silence for
// `rm -rf`. When the surface has banked a rule the answer is a [ConsentRule]
// instead, which is the scope that tells this engine to write nothing beside it
// (consent.go's askAnswer says the same from the other end).
//
// It is a comment rather than a field for [HarnessModelNote]'s reason: it is
// one lane's own extra, and every other lane would carry it empty.
const AnswerBanked = "banked"

// ConsentScopeOf is how far one consent answer actually reaches.
//
// It is the key's own scope ([AnswerFromKey]) in every case but one: a widening
// yes whose rule the surface has already written down is a [ConsentRule], and
// [AnswerBanked] is where that fact rides.
//
// It is exported for the same reason [AnswerFromKey] is: anything that applies
// an answer to this lane without going through [Agent.ResolveQuestion] — a
// stand-in, a link that resolves on the far side — has to reach the one mapping
// rather than write a second.
func ConsentScopeOf(action AnswerAction, answer Answer) ConsentScope {
	if action.Allow && action.Scope == ConsentToolSession &&
		strings.TrimSpace(answer.Comments[AnswerBanked]) != "" {
		return ConsentRule
	}
	return action.Scope
}

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
	// A CONFLICT'S YES IS NOT AN ACCEPT, and this is the one arm where the two
	// lanes part. `[a] resolve it` on a branch that would not fasten spends the
	// merge round or the carry ([Agent.ResolveConflict]); accepting would offer
	// the same branch to the same ground and be refused in the same words, which
	// is what it did (docs/design/task-states/DESIGN.md's conflict row).
	if answer.Kind == QuestionConflict && key == LandingYesKey {
		return a.ResolveConflict(answer.ID)
	}
	switch key {
	case LandingYesKey:
		return a.ResolveUnverified(answer.ID, TaskAccept, words)
	case LandingNoKey:
		return a.ResolveUnverified(answer.ID, TaskRefute, words)
	case LandingAgainKey:
		return a.ResolveUnverified(answer.ID, TaskReaudit, words)
	case LandingDecideKey:
		// A SECOND PRESS IS THE SAME ANSWER AND NOT A REFUSAL. `let codeaf decide
		// this one` pressed twice used to hand the model the same decision twice,
		// in two identical lines, because the row a person is looking at was drawn
		// before the hand-over reached it. The engine says the question is already
		// in its hands ([ErrTaskHandedOver]), and what this door owes for that is
		// the answer standing: the state is exactly the one that was asked for.
		// EVERY OTHER REFUSAL IS STILL ONE.
		if err := a.HandUnverifiedToModel(answer.ID); err != nil && !errors.Is(err, ErrTaskHandedOver) {
			return err
		}
		return nil
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
	open := a.discussionQuestions()

	a.mu.Lock()
	modelAsks := a.asked.openLocked()
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
		Reason:   ConsentFallbackReason,
		Options:  AnswerOptions(QuestionConsent),
		Stakes:   StakesCostly,
		Blocking: Blocking{Turn: true},
	})
}

// ConsentFallbackReason is why the gate is asking, in the one sentence that is
// true of every question on this lane whatever the policy matched. The policy's
// own phrasing is better and rides on the banked question; this is what is left
// when there is none.
//
// It is exported because a SURFACE builds the same question out of the same
// request event (tui3's [app.consentQuestion]) and the two are keyed by one
// token — so a sentence spelled twice would be two questions replacing each
// other on screen while somebody read one of them.
const ConsentFallbackReason = "it will not run this without your word"

// connectQuestion is a connect offer as a question. An account that needs a
// typed answer is a question with a box rather than a pick, because a bare yes
// to one of those is read as a decline (connect.go) and a chip that means no
// while reading yes is worse than no chip.
func (a *Agent) connectQuestion(id string, ask connectAsk) Question {
	return a.said(QuestionConnect, id, ConnectQuestion(id, ask.name, ask.needsKey, "", ask.secret))
}

// ConnectQuestion is that object, and it is ONE BUILDER for the two roads it
// arrives by: this lane's own [Agent.OpenQuestions], and the surface, which has
// the event a moment before the questions lane reaches it and builds the same
// question from it (tui3's connect.go). The block keys a question by its lane
// and its token, so two builders would be two questions replacing each other on
// screen while somebody read one of them.
//
// The surface knows three things this engine does not — the word a person owns
// the account by, the service's own sentence over the box, and whether what it
// wants is a secret or the part of an address — so they are arguments with
// honest defaults rather than facts invented here.
//
// AN ACCOUNT THAT NEEDS A TYPED ANSWER IS A QUESTION WITH A BOX RATHER THAN A
// PICK, because a bare yes to one of those is read as a decline (connect.go) and
// an answer that means no while reading yes is worse than no answer.
func ConnectQuestion(id, name string, needsKey bool, keyAsk string, secret bool) Question {
	head := ConnectAskHead
	if name = strings.TrimSpace(name); name != "" {
		head = "connect your " + name + " account?"
	}
	built := Question{
		Ref:      id,
		Kind:     QuestionConnect,
		Ask:      AskPermission,
		Form:     FormCard,
		Asker:    Asker{Kind: AskerEngine},
		Head:     head,
		Reason:   ConnectAskReason,
		Subject:  SubjectRef{Kind: SubjectAccount, Ref: id},
		Options:  AnswerOptions(QuestionConnect),
		Stakes:   StakesReversible,
		Blocking: Blocking{Turn: true},
		Scope:    []AnswerScope{ScopeOnce},
	}
	if needsKey {
		// AND THE WAY OUT SURVIVES THE BOX. A question whose only answer is words
		// has no way to say no, and the turn is waiting: `esc` is *later* on this
		// block and decides nothing, so the decline has to be an answer a person
		// can see. The engine reads a bare pick on a key question as a no
		// ([Agent.ResolveConnect] says why a yes cannot be one), so this is the
		// one option it keeps.
		built.Options = []AnswerOption{{Key: "2", Label: "not now", Safe: true}}
		built.Ask = AskClarification
		if keyAsk = strings.TrimSpace(keyAsk); keyAsk == "" {
			keyAsk = ConnectKeyPrompt
		}
		built.Input = InputShape{Kind: InputText, Prompt: keyAsk, Secret: secret}
	}
	return built
}

// The three sentences a connect offer is spelled with. They are constants
// because the surface builds the same question and the two must not drift.
const (
	// ConnectAskHead is the head where the session named the account only by an
	// id nobody would recognize, which is the one case there is no better word
	// for.
	ConnectAskHead = "connect your account?"
	// ConnectAskReason is why it is being asked now.
	ConnectAskReason = "the turn asked for something only that account can answer"
	// ConnectKeyPrompt is the line over the box where the service said nothing
	// of its own about what it wants.
	ConnectKeyPrompt = "the key, or the part of the address it is missing"
)

// harnessQuestion is a sub-harness offer, or a written design waiting to be
// approved, as a question.
func (a *Agent) harnessQuestion(id uint64, card Event) Question {
	return a.said(QuestionHarness, strconv.FormatUint(id, 10), HarnessQuestion(id, card))
}

// HarnessQuestion is that object, built from the card the lane already holds:
// Text is the harness's name and Hint its one sentence.
//
// IT IS ONE BUILDER AND NOT TWO. Every other lane in this file has a twin on the
// surface — a window has the event before the questions lane reaches it and
// raises the question from that, so the two must be one sentence
// ([TaskProposalLead] states the cost of a drift). This lane is the first to be
// written the honest way round: the surface calls THIS, so there is nothing to
// drift.
func HarnessQuestion(id uint64, card Event) Question {
	head, name, reason := strings.TrimSpace(card.Text), strings.TrimSpace(card.Text), harnessOfferReason(card)
	if head == "" {
		head = "run a saved program for this?"
	}
	ask, form, stakes := AskPermission, FormLine, StakesReversible
	// AN OFFER STOPS THE TURN, because the turn asked whether to run the program
	// and cannot go on until it is told.
	blocking := Blocking{Turn: true}
	// THE FINISHED PAGE IS THE TEST, AND NOT THE EVENT'S NAME. A design done
	// event is the only one of this lane's events that carries a written program,
	// and the surface refuses one without it (tui3's askHarnessDesign) — so the
	// page itself is the fact both roads agree on, where a kind is a field a
	// caller building the object by hand can forget.
	if card.Harness != nil {
		// A DESIGN IS A JUDGEMENT AND NOT A PERMISSION: the page is written,
		// and what is being asked is whether it is right — which is a thing
		// nothing but a person ever answers.
		//
		// AND IT COSTS SOMETHING TO ANSWER WRONG. Declining an offer loses
		// nothing; dropping a finished page loses the minutes of model work that
		// wrote it and there is no way back to it, which is [StakesCostly] and
		// not the reversible an offer wears.
		ask, form, stakes = AskJudgement, FormCard, StakesCostly
		// AND THE QUESTION IS ABOUT THE PAGE, so it is the PAGE that is named.
		// `card.Text` on this event is the goal the design was asked for, which
		// is what the designer was told rather than what it wrote — and a head
		// naming the request over a card showing the result would be two subjects
		// on one decision.
		name = strings.TrimSpace(card.Harness.Id.Name)
		head = harnessDesignLead + name
		if desc := strings.TrimSpace(card.Harness.Id.Desc); desc != "" {
			reason = desc
		}
		// AND A DESIGN STOPS ITS OWN NODE AND NOT THE CONVERSATION. The page was
		// written by a task and lands in the transcript as ordinary scrollable
		// content; the conversation carried on while it was being written and
		// carries on now. What waits is the design's own thread.
		//
		// IT MATTERS MORE THAN A LABEL. A question that stops the turn OWNS THE
		// BOX (tui3's [questionOwnsBox]), so `enter` over a half-typed sentence
		// would send it as this question's answer — and an answer to a design
		// with no key on it resolves the lane, which is the DROP. That is the
		// exact destruction `change it` was rewritten to stop doing, arriving by
		// a second road.
		blocking = Blocking{}
		if goal := strings.TrimSpace(card.Text); goal != "" {
			blocking.Tasks = []string{goal}
		}
	} else {
		head = harnessOfferLead + strconv.Quote(head) + "?"
	}
	return Question{
		ID:      id,
		Kind:    QuestionHarness,
		Ask:     ask,
		Form:    form,
		Asker:   Asker{Kind: AskerEngine},
		Head:    head,
		Reason:  reason,
		Subject: SubjectRef{Kind: SubjectPage, Name: name},
		// THE ANSWERS ARE THE SHAPE'S AND NOT THE KIND'S. This lane asks two
		// questions with two different rows ([HarnessOptions] says why), and the
		// shape decided above is the one fact that tells them apart.
		Options:  HarnessOptions(ask),
		Stakes:   stakes,
		Blocking: blocking,
	}
}

// harnessOfferLead opens the sentence an offer asks with, and the harness's own
// name closes it. It is a constant so the row, the presence file and this object
// cannot become three accounts of one offer.
const harnessOfferLead = "run harness "

// harnessDesignLead opens the sentence a FINISHED PAGE asks with, and the name
// the designer gave it closes the line. It is the designer's own act stated
// plainly — a program has been written, and the three answers under it are what
// can be done about that — where the offer above asks for permission to run one
// that already exists.
const harnessDesignLead = "wrote a program: "

// harnessOfferReason is the dim line under an offer: where the turn's own words
// chose what it would run on, and what the saved program says it does.
//
// THE MODEL COMES FIRST SO THAT IT OUTLIVES THE DESCRIPTION, which is the row's
// own ranking kept through the move onto the block. A surface cuts a reason it
// has no room for from the END, so the order IS the ranking: the description is
// context for a name a person can already read, and the model is the one thing on
// the offer that says this run would not be the ordinary one — "run this
// harness?" is a different question when the answer costs what opus costs.
//
// Where the turn named a model this install does not have, the session's own note
// stands in its place. It is never a refusal: the question is the same question
// either way, and yes still runs the harness.
func harnessOfferReason(card Event) string {
	parts := make([]string, 0, 2)
	switch {
	case strings.TrimSpace(card.Model) != "":
		parts = append(parts, "model: "+strings.TrimSpace(card.Model))
	case strings.TrimSpace(card.ModelNote) != "":
		parts = append(parts, strings.TrimSpace(card.ModelNote))
	}
	if hint := strings.TrimSpace(card.Hint); hint != "" {
		parts = append(parts, hint)
	}
	return strings.Join(parts, " · ")
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
		Reason:  StandingAskReason,
		Subject: SubjectRef{Kind: SubjectOrder, ID: id},
		Options: AnswerOptions(QuestionStanding),
		Stakes:  StakesReversible,
		// AND THE TURN IS STOPPED ON IT, which this question did not say for a
		// long time and which is simply true: [Agent.askStanding] parks the
		// `stand` call on the answer and the card carries no clock at all, by
		// law (standing_contract.go), so the conversation waits for as long as
		// the card stands. A question that said otherwise was a window drawing
		// `idle` over work that had stopped, and — once the waiting desk began
		// reading [Question.Waiting] — a standing card that was not on the desk
		// at all.
		Blocking: Blocking{Turn: true},
		Scope:    []AnswerScope{ScopeOnce, ScopeAlways},
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
		Head:     TaskProposalLead + strings.TrimSpace(notice.Title),
		Reason:   strings.TrimSpace(notice.Summary),
		Subject:  SubjectRef{Kind: SubjectNode, ID: id, Name: strings.TrimSpace(notice.Title)},
		Options:  AnswerOptions(QuestionTask),
		Stakes:   StakesCostly,
		Blocking: Blocking{Turn: true},
		Deadline: notice.Deadline,
		Input:    TaskModelShape(notice),
	}
	if !notice.Deadline.IsZero() {
		built.Pick = &Pick{Key: "1", Reason: TaskProposalPickReason, Confidence: ConfidenceFairly}
		built.Policy = Policy{Kind: PolicyRecommendThenAuto, After: time.Until(notice.Deadline)}
	}
	return a.said(QuestionTask, token, built)
}

// TaskModelBlank is the label of the hole a proposal carries when the harness
// could not settle which model the work runs on, and it is the key the answer
// carries the chosen one back under ([Answer.Blanks]).
//
// IT IS ONE NAME READ AT BOTH ENDS. The card fills that map by the blank's own
// label (tui3's questioninput.go does the filling) and [Agent.applyToLane] reads
// [TaskAnswer.Model] straight back out of it, so a label spelled twice would be
// a choice somebody made and nothing acted on.
const TaskModelBlank = "model"

// TaskModelPrompt is the sentence the hole sits in, with `{model}` where the
// hole goes — so what a person reads is `run it on [ anthropic/claude-opus-5 ▾ ]`
// rather than a form with a field name over it.
const TaskModelPrompt = "run it on {" + TaskModelBlank + "}"

// TaskModelShape is the small form a proposal carries when — and only when — the
// harness raised a shortlist it could not choose within
// ([TaskNotice.ModelOptions]).
//
// ONE OPTION IS NOT A CHOICE, so an ordinary proposal carries no shape at all and
// the card draws no hole: a row offering the one model the work was already going
// to run on is a question that has answered itself. That is the same bound the
// row of model chips this replaced kept, said once instead of in the renderer.
//
// IT IS EXPORTED BECAUSE THE SURFACE BUILDS THE SAME QUESTION, for the reason
// [TaskProposalLead] states: two builders that drifted would put two questions on
// screen about one proposal.
func TaskModelShape(notice TaskNotice) InputShape {
	if len(notice.ModelOptions) < 2 {
		return InputShape{}
	}
	return InputShape{
		Kind:   InputBlanks,
		Prompt: TaskModelPrompt,
		Blanks: []Blank{{
			Label:   TaskModelBlank,
			Kind:    BlankChoice,
			Choices: append([]string(nil), notice.ModelOptions...),
			// THE DEFAULT IS AN ANSWER ALREADY GIVEN ([Blank] says so). The
			// leading option is the closest match, it is what the card shows, and
			// it is what the clock settles on — so a person who changes nothing
			// has confirmed the model the work was always going to run on.
			Default: strings.TrimSpace(notice.Model),
		}},
	}
}

// TaskProposalLead opens the sentence a task proposal asks with, and it is
// task.go's own lead repeated here so the card, the presence file and this
// object cannot become three accounts of one proposal.
//
// IT IS EXPORTED BECAUSE THE SURFACE BUILDS THE SAME QUESTION. A window that
// draws the proposal has the notice before the questions lane reaches it and
// raises the question from that, so the two objects must be one sentence — the
// block keys a question by its lane and its id, and two builders that drifted
// would put two questions on screen about one proposal.
const TaskProposalLead = "wants to start a task: "

// TaskProposalPickReason is why the clock recommends starting it, in the words
// the recommendation is made in. It is exported for [TaskProposalLead]'s
// reason.
const TaskProposalPickReason = "it starts on its own unless you say otherwise"

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
	q := Question{
		ID:      notice.ID,
		Kind:    kind,
		Ask:     AskLanding,
		Form:    landingForm(status.Ask),
		Asker:   Asker{Kind: AskerTask, Name: strings.TrimSpace(notice.Title)},
		Head:    strings.TrimSpace(notice.Title),
		Reason:  landingReason(status.Ask),
		Subject: SubjectRef{Kind: SubjectNode, ID: notice.ID, Name: strings.TrimSpace(notice.Title)},
		Options: landingOptions(status.Ask),
		// ACCEPTING BRINGS A BRANCH HOME AND REFUSING KEEPS ONE. Neither is free
		// and neither is beyond taking back, which is exactly `costly`.
		Stakes: StakesCostly,
		// AND NOTHING IS WAITING ON IT IN THE CONVERSATION. The work has already
		// finished; what is waiting is the decision about whether it holds, and
		// a person may leave it as long as they like.
		Blocking: Blocking{},
		// AND WHO MAY ANSWER IT IS READ OFF THE ASK'S OWNER AND NOWHERE ELSE.
		// task-states already keeps one holder for a landing — [TaskAsk.Owner],
		// which the node carries, the notice publishes and the checkpoint now
		// survives (task_store.go's [taskRecord.Decider]) — so this dresses that
		// one fact as the policy this object spells it in rather than minting a
		// second holder to disagree with it.
		Policy: landingPolicy(status.Ask.Owner),
	}
	// AND A QUESTION SOMEBODY ANSWERED CARRIES THAT ANSWER'S FATE. The raise
	// owes the record the same consultation the replay owes it (this file's
	// [questionAsked]): an answered landing re-asked with the same words reads
	// as though the earlier answers were ignored (#1077). The stamp leads
	// because it is the new information; the ask's own reason stays after it
	// because what became of the answer IS why it is asking again. A
	// resolution still in flight says so instead — the person is not being
	// asked twice, they are being told their answer is still working. The
	// consultation is the gate's own helper on the gate's terms
	// ([decidedAlready] over [Agent.Decisions]), and it costs what the gate's
	// own check costs: one record read per derived landing question, no cache
	// and so no second source of truth. Banked words are an unanswered
	// question and carry no fate — [Agent.said] returns them untouched.
	if record, found := decidedAlready(a.Decisions(), q); found {
		stamp := landingAnsweredStamp(record)
		switch {
		case strings.TrimSpace(notice.Settling) != "":
			q.Reason = stamp + " · still working on it"
		case strings.TrimSpace(q.Reason) == "":
			// A LANDING WITH NOTHING TO EXPLAIN CARRIES THE STAMP ALONE — a
			// plain `your call` has no ask reason, and a separator after it
			// would dangle.
			q.Reason = stamp
		default:
			q.Reason = stamp + " · " + q.Reason
		}
	}
	return a.said(kind, token, q)
}

// landingForm is which shape a landing asks to be drawn in, and it is decided
// by HOW MUCH EVIDENCE THIS PARTICULAR LANDING CARRIES rather than by the kind.
//
// THE TASK-STATES ROW IS UNCHANGED (docs/design/questions/DESIGN.md's defaults
// table says exactly that beside this kind): `[a] <yes> · [n] <no> · [s] tell
// it`, one row, the three columns in the one order — which is the line form,
// because the card form spends a row per answer and never composes that row.
// The landing's head, its facts and its reason are already drawn by the card
// this surface lands in the transcript (internal/tui3's taskdone.go), so a
// second head and a second reason above the box would be the two-renderings
// defect rather than more evidence.
//
// ONE ROAD PROMOTES, and it is the road with something to say that no verb can
// carry: a landing held by the person's own uncommitted copies, whose `[a]`
// MOVES FILES OF THEIRS (task_status.go's [taskAskGroundConsequence]). A
// consequence is drawn beside its answer on the card and nowhere on a row, and
// forms promote and never demote — so the lane that knows the evidence is here
// asks for the card exactly where the evidence exists.
func landingForm(ask TaskAsk) QuestionForm {
	if strings.TrimSpace(ask.Consequence) != "" {
		return FormCard
	}
	return FormLine
}

// landingReason is the row's own sentence, plus WHO IS DECIDING where that is
// not the person.
//
// THE CARD MUST SAY SO ON THE REASON LINE (docs/design/task-states/DESIGN.md).
// Under `task.settle = auto`, and after somebody hands one card over, the model
// is reading the work and will spend a verb on it — and a row that said nothing
// about that is a person answering a question somebody else is already
// answering. THE ANSWERS STAY DRAWN: the floor hands an unanswered question back
// at the end of the turn anyway, and a card with a sentence and no handle is the
// exact shape #767 was filed about. Answering it IS taking it back.
func landingReason(ask TaskAsk) string {
	reason := strings.TrimSpace(ask.Reason)
	if ask.Owner != TaskAskOwnerModel {
		return reason
	}
	if reason == "" {
		return LandingDecidingWord
	}
	return reason + " · " + LandingDecidingWord
}

// LandingDecidingWord is that clause, and it is a WHOLE CLAUSE rather than a
// word: a row reading `nobody could check it · auto` would have told a person
// the name of a setting instead of who is deciding. It is exported for the one
// surface that must recognize it (internal/tui3's
// [app.questionReasonIsNews]): the done card already says who is deciding, so
// the question repeating the clause alone beside it is a duplication, not
// news.
const LandingDecidingWord = "codeaf is deciding"

// landingPolicy is [TaskAsk.Owner] as a [Policy], and it is the whole of this
// wave's composition with the auto-settle floor.
//
// ONE HOLDER, TWO VOCABULARIES. `task.settle = auto` and `[d] let codeaf decide
// this one` both write the model onto the node, and that mark is what the card,
// the roster and the floor all read; a landing question is DERIVED from the same
// mark, so a person asking "who is deciding this" gets one answer whichever
// surface they ask.
//
// AND THERE IS NO CLOCK ON THIS ROAD. [PolicyRecommendThenAuto] is the timed
// shape and it belongs to the `ask` tool's own assumptions, which mint a deadline
// and run the one timer this program has (tools_ask.go). The floor is not a
// timer: it is the end of a turn, and a landing the model was handed comes back
// when that turn ends however long or short it was — so this says `decide` with
// no [Policy.After], and a second timer is never started for it.
func landingPolicy(owner TaskAskOwner) Policy {
	if owner == TaskAskOwnerModel {
		return Policy{Kind: PolicyDecide}
	}
	return Policy{Kind: PolicyAsk}
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
			// AND WHAT IT WILL DO, WHERE THE VERB DOES NOT SAY IT. One road carries
			// a consequence — a landing held by the person's own untracked copies,
			// whose `resolve it` moves files of theirs (task_status.go's
			// [taskAskGroundConsequence]) — and every other ask leaves it empty,
			// which is the emptiness law and draws no row.
			options[at].Consequence = strings.TrimSpace(ask.Consequence)
		case LandingNoKey:
			if word := strings.TrimSpace(ask.No); word != "" {
				options[at].Label = word
			}
		}
	}
	return options
}
