package session

// The ask tool parks on the same question object every surface draws:
//
//   ? Which storage shape should this use?
//     I found two viable shapes and the record does not choose between them.
//     1 sqlite   2 jsonl
//
// There is no private model-only prompt. A headless caller either takes the
// declared default under policy or gets `your call`; it never waits on a key.

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

const askDescription = "Ask only after the decision ladder is exhausted. Give a reason, stakes, structured answers before free text, your pick and what would change it."

// askBlockSchemaJSON is the one evidence block, spelled once and spliced into
// askSchemaJSON at the two places a block may stand: under an answer and
// before the answers. It is spliced rather than referenced because a `$ref`
// into `$defs` is a shape no other tool on the belt uses, and the one tool that
// used it was the one tool deepseek-v4-flash could not call (2026-09-10: three
// calls with the answers list rendered as a string). A structural test
// (schema_test.go) keeps every belt schema free of references.
const askBlockSchemaJSON = `{"type":"object","properties":{"kind":{"type":"string","enum":["text","diagram","table","diff","image","layout"]},"title":{"type":"string"},"body":{"type":"string","description":"Text or lines"},"rows":{"type":"array","description":"Table rows","items":{"type":"array","items":{"type":"string"}}},"path":{"type":"string","description":"Image path"}},"required":["kind"]}`

// askSchemaTemplate is the schema with BLOCK where the evidence block goes.
//
// EVERY DESCRIPTION HERE IS A CONTRACT AND NOT A GLOSS (schemalaw_test.go states
// the law; docs/design/prompt-diet/BENCH.md §4a is the bill). `ask` is shelved on
// a full belt and PRE-ARMED on a lean one (promptprofile.go), which makes this
// the one schema a small model pays for on every request of every turn: it was
// four thousand two hundred and seventy-seven bytes, thirty percent of the whole
// lean tool block. It is smaller now, and no field left and no enum left. What
// left is prose that said the field's own name back at the model — `"Block kind"`
// on a field called `kind` with the kinds listed beside it, `"Subject kind"`,
// `"Turn waits"`, `"Certainty"` on a low/medium/high enum — plus the tail of the
// two long ones. A field whose name and enum already say what it is gets no
// description at all, which is the honest amount rather than a missing one.
//
// AND THE NUMBER ONLY EVER COMES DOWN, which is why this wave paid for
// `blocking.turn` — the one description on this belt that measurably changed
// what a model does (0 of 2 non-blocking asks with the contract on the verb, 2
// of 2 with it on the field) — out of the same schema rather than out of the
// budget. `"What waits"` on a field called `blocking` and `"Recommended answer"`
// on one called `pick` are the same habit as `"Turn waits"` was, both with their
// halves described beneath them; `"Prefilled"` on a blank's `default` is a third.
// The tail of `key` went for a different reason and a worse one: it told the
// model that anything but a digit would be "renumbered and given back as you
// wrote it", which is an instruction to comply followed by a promise that not
// complying is handled. The engine still renumbers (askDigitKeys); it just no
// longer says so to the one reader who should not be told.
//
// AND `askDescription` GAVE UP ITS LAST SENTENCE BECAUSE IT HAD STOPPED BEING
// TRUE. "Returns the person's whole answer" is what `ask` did when every call
// parked; a call with `blocking.turn` false returns with the question still
// standing and the answer arrives later as a message. Rather than write the
// exception twice, the verb says what to ask and `blocking.turn` says what comes
// back — which is where a model reading one field at a time will find it.
const askSchemaTemplate = `{"type":"object","properties":{"head":{"type":"string","description":"Decision in one sentence"},"kind":{"type":"string","enum":ASKKINDS,"description":"assumption options stand unless struck; ratify waits on nothing"},"form":{"type":"string","enum":FORMS,"description":"Smallest form evidence needs"},"reason":{"type":"string","description":"Why ask now after cheaper rungs"},"subject":{"type":"object","description":"Existing subject","properties":{"kind":{"type":"string","enum":SUBJECTS},"id":{"type":"integer","description":"Task id"},"callId":{"type":"string"},"ref":{"type":"string","description":"String id"},"name":{"type":"string"}}},"options":{"type":"array","description":"Answers, or assumptions that stand","items":{"type":"object","properties":{"key":{"type":"string","description":"A single digit, 1 upward"},"label":{"type":"string","description":"Short label"},"body":{"type":"string","description":"Meaning"},"consequence":{"type":"string","description":"What happens"},"safe":{"type":"boolean","description":"Changes nothing"},"widening":{"type":"boolean","description":"Grants more"},"blocks":{"type":"array","description":"Evidence","items":BLOCK},"dimensions":{"type":"object","description":"Value per axis","additionalProperties":{"type":"string"}}},"required":["key","label"]}},"input":{"type":"object","description":"checklist and pairs use the options; blanks and dial carry their shape here","properties":{"kind":{"type":"string","enum":INPUTS},"blanks":{"type":"array","description":"Named fields, kind blanks","items":{"type":"object","properties":{"label":{"type":"string"},"kind":{"type":"string","enum":BLANKS},"default":{"type":"string"},"choices":{"type":"array","description":"kind choice","items":{"type":"string"}}},"required":["label"]}},"dial":{"type":"object","description":"Number on a range, kind dial","properties":{"min":{"type":"number"},"max":{"type":"number"},"default":{"type":"number"},"labels":{"type":"array","description":"Words for the ends","items":{"type":"string"}}},"required":["min","max"]},"prompt":{"type":"string","description":"One line over the box"},"secret":{"type":"boolean","description":"A credential, never drawn back"}}},"pick":{"type":"object","properties":{"key":{"type":"string","description":"Option key"},"reason":{"type":"string"},"confidence":{"type":"string","enum":CONFIDENCE},"wouldChange":{"type":"string","description":"What would change it"}},"required":["key"]},"stakes":{"type":"string","enum":STAKES,"description":"Cost if wrong"},"blocking":{"type":"object","properties":{"turn":{"type":"boolean","description":"Default true stops your turn until they answer. false returns at once, keep working, their answer arrives as a message"},"tasks":{"type":"array","description":"Task names","items":{"type":"string"}}}},"scope":{"type":"array","description":"Allowed lifetimes","items":{"type":"string","enum":SCOPES}},"attach":{"type":"array","description":"Evidence before answers","items":BLOCK}},"required":["head","kind","reason","stakes"],"additionalProperties":false}`

// askSchemaJSON is the wire schema: the template with the block written out in
// full at both places it stands, and EVERY ENUM WRITTEN FROM THE CONSTANTS THE
// CODE READS.
//
// THE VOCABULARY IS ONE VOCABULARY. The confidence enum said low, medium, high
// while [Confidence] reads sure, fairly, unsure and the surface draws nothing
// for anything else, so every pick a model was careful about arrived with its
// confidence silently dropped. A schema typed out beside the constants is a
// second list waiting to disagree with the first; written from them, a value
// the code cannot read is a value the model is never offered.
var askSchemaJSON = strings.NewReplacer(
	"BLOCK", askBlockSchemaJSON,
	"ASKKINDS", jsonEnum(askKinds),
	"FORMS", jsonEnum(questionForms),
	"SUBJECTS", jsonEnum(subjectKinds),
	"INPUTS", jsonEnum(inputKinds),
	"BLANKS", jsonEnum(blankKinds),
	"CONFIDENCE", jsonEnum(confidences),
	"STAKES", jsonEnum(stakesKinds),
	"SCOPES", jsonEnum(answerScopes),
).Replace(askSchemaTemplate)

// jsonEnum is one vocabulary as a schema enum, in the order the constants are
// written. It takes any string-shaped kind, because every vocabulary in this
// package is one.
func jsonEnum[T ~string](values []T) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, strconv.Quote(string(value)))
	}
	return "[" + strings.Join(quoted, ",") + "]"
}

type askArguments struct {
	Head     string         `json:"head"`
	Kind     AskKind        `json:"kind"`
	Form     QuestionForm   `json:"form"`
	Reason   string         `json:"reason"`
	Subject  SubjectRef     `json:"subject"`
	Options  []AnswerOption `json:"options"`
	Input    InputShape     `json:"input"`
	Pick     *Pick          `json:"pick"`
	Stakes   Stakes         `json:"stakes"`
	Blocking askBlocking    `json:"blocking"`
	Scope    []AnswerScope  `json:"scope"`
	Attach   []Block        `json:"attach"`
}

// askBlocking is [Blocking] as the ASKER may write it, and the one difference is
// the pointer: it is how this decoder tells `turn: false` from a `blocking` the
// model did not write at all.
//
// THE ZERO VALUE WAITS, exactly as [Policy]'s does and for the same reason: a
// model that says nothing gets the behaviour every `ask` has always had, and
// carrying on while somebody decides is a thing the asker has to CLAIM it can
// do.
type askBlocking struct {
	Turn  *bool    `json:"turn"`
	Tasks []string `json:"tasks"`
}

// waits reads that field: true where the asker asked to wait or said nothing.
//
// IT IS THE ASKER'S WORD AND NOTHING ELSE. Whether the KIND waits is
// [AskKind.Waits]'s answer and is asked beside this one, so neither reading has
// to know the other's business: a ratify waits on nobody whatever the model
// writes here, and everything else waits unless the model says otherwise.
func (b askBlocking) waits() bool { return b.Turn == nil || *b.Turn }

// askRefusedLead opens every refusal `ask` hands back. IT SAYS THAT NOTHING WAS
// SHOWN, because a model reading a bare refusal has told the person "the form
// is presented above — check whichever boxes you like" over a screen with
// nothing on it (2026-09-10, deepseek-v4-flash). The lead is also the fact
// [askRefusedAndNotRetried] reads off the transcript, so the two are one
// string.
const askRefusedLead = "nothing was asked and the person saw no question: "

func (a *Agent) askTool() bare.Tool {
	return bare.Tool{Name: "ask", Description: askDescription, Schema: json.RawMessage(askSchemaJSON), Execute: a.executeAsk}
}

func (a *Agent) executeAsk(ctx context.Context, raw json.RawMessage) (string, bool, error) {
	var in askArguments
	if err := decodeToolArguments(raw, &in); err != nil {
		return "Invalid arguments: " + err.Error(), true, nil
	}
	// A PICK WITH NO KEY IS NO PICK. A model that writes `pick: {key: ""}` with
	// "their taste decides it" beside it has declined to recommend, which the
	// object spells as a nil pick; refusing it as a pick that names nothing
	// sends the model round again for a comma. Measured on 2026-09-10: a
	// well-formed checklist of eight was refused for exactly this.
	if in.Pick != nil && strings.TrimSpace(in.Pick.Key) == "" {
		in.Pick = nil
	}
	id := a.askSeq.Add(1)
	theirs := askDigitKeys(in.Options, in.Pick)
	// AND WHETHER THE ASKER IS WAITING FOR IT AT ALL. Two things decide it, and
	// both are the asker's: the KIND, because a ratify names something this turn
	// already did and nothing waits on one ([AskKind.Waits]), and the asker's own
	// `blocking.turn`, which is how a model says it has work in hand that does
	// not depend on the answer. Either of them is enough to let the call carry on.
	waits := in.Kind.Waits() && in.Blocking.waits()
	q := Question{ID: id, Kind: QuestionAsk, Ask: in.Kind, Form: in.Form, Asker: Asker{Kind: AskerModel}, Head: in.Head, Reason: in.Reason, Subject: in.Subject, Options: in.Options, Input: in.Input, Pick: in.Pick, Stakes: in.Stakes, Scope: in.Scope, Attach: in.Attach,
		// AND WHICH STEP ASKED IT. A model may call `ask` three times in one
		// batch, and those three questions are one moment to a person
		// (question.go's [Question.Batch]).
		Batch: a.stepToken()}
	// WHAT MAY OUTLIVE THE TURN IS THE ASKER'S OWN WORD, kept once
	// ([questionOutlivesTurn] reads it). The [AskKind.Waits] term keeps a ratify
	// out of it: a ratify names something this turn already did, and one still on
	// screen after the turn is a question about work nobody is doing.
	q.Later = q.Ask.Waits() && !in.Blocking.waits()
	// AND `blocking.turn` IS DERIVED AND NEVER THE MODEL'S CLAIM. What stops the
	// turn is whether this call actually parks — the two terms above — so a model
	// that writes `blocking: {turn: true}` on a ratify cannot put a row on the
	// waiting desk about a turn that is carrying on, and a model that writes
	// nothing at all cannot make home draw `idle` over work that has stopped.
	// Nothing downstream may read this field as an argument the asker supplied.
	//
	// The tasks half is the asker's, because only the asker knows what work of
	// its own is waiting and this engine cannot derive that from anything.
	q.Blocking = Blocking{Turn: waits, Tasks: in.Blocking.Tasks}
	q.Policy = a.autonomyFor(q.Ask)
	if q.Stakes == StakesIrreversible {
		q.Policy = Policy{Kind: PolicyAsk}
	}
	if q.Policy.Kind == PolicyRecommendThenAuto {
		q.Deadline = time.Now().Add(q.Policy.After)
	}
	if err := q.Check(a.Decisions()); err != nil {
		return askRefusedLead + err.Error(), false, nil
	}
	// A TEAM MEMBER'S CLARIFYING QUESTION GOES TO ITS MANAGER FIRST, as a
	// decision packet, when its team says questions go up (team_questions.go).
	// A conversation in no team pays a stat of the teams file here, and only
	// when it asks.
	if said, up := a.askUp(q); up {
		return said, false, nil
	}
	if !a.config.Interactive && q.Policy.Kind == PolicyAsk {
		if q.Pick == nil {
			return "your call: " + strings.TrimSpace(q.Head) + " (nobody to ask)", false, nil
		}
		q.Policy = Policy{Kind: PolicyDecide}
	}
	// AND A CARD UNDER --yolo TAKES ITS OWN DEFAULT. yolo says nobody is
	// watching, so a card that carries a default and would otherwise wait is a
	// run that hangs until something times it out. The posture is
	// [Config.Unattended] — yolo is approvals AND that flag, and
	// [Config.Interactive] is still true for it because a surface exists — so
	// this is deliberately not the headless branch above, which answers a card
	// with NO default by handing it back as `your call`.
	//
	// A CARD WITH NO DEFAULT IS UNTOUCHED: it has nothing to take, so it parks
	// exactly as it did before. An assumption card is not this road either — its
	// default is its options standing and it already carries the away-policy
	// clock, so it never reaches here as [PolicyAsk].
	if a.config.Unattended && q.Policy.Kind == PolicyAsk && q.Pick != nil {
		q.Policy = Policy{Kind: PolicyDecide}
	}
	if q.Policy.Kind == PolicyDecide {
		answer := defaultAnswer(q, "")
		answer.From = headlessAnswerLine(q, answer)
		a.recordDecision(decisionRecordOf(q, answer))
		// Nothing was asked, so there is nothing in the book and nothing to
		// deliver: the answer is this call's own result, rendered the one way
		// every answer is rendered ([askAnswerText]).
		// AND THE LINE LEAVES THE ENGINE. docs/design/questions/DESIGN.md's
		// HEADLESS law is that the policy applies AND IS PRINTED — a run that
		// took a default silently is a run whose decision nobody can find
		// afterwards. The engine does not print; the answer carries the sentence
		// in [Answer.From] and this is what puts it where a door with nobody at
		// it can read it (cmd/codeaf's --once). Nothing was ever asked here, so
		// a surface hearing this about a question it never drew does nothing
		// with it, which is what [app.foldOthersAnswer] already does.
		a.emitQuestion(EventQuestionAnswered, q, &answer)
		return askAnswerText(q, answer, theirs, nil), false, nil
	}
	open := &askOpen{q: q, theirs: theirs}
	a.mu.Lock()
	wait := a.asked.parkLocked(open)
	a.mu.Unlock()
	letGo, err := a.AskQuestion(q)
	if err != nil {
		a.mu.Lock()
		a.asked.letGoLocked(id)
		a.mu.Unlock()
		return askRefusedLead + err.Error(), false, nil
	}
	a.mu.Lock()
	open.letGo = letGo
	a.mu.Unlock()
	// THE CLOCK STARTS ONCE THE QUESTION IS ON SCREEN, and it belongs to the lane
	// rather than to this call ([Agent.startAskClock]): it has to go on running
	// for a question this call is not waiting for, which is the shape that had no
	// clock at all while the timer lived in the select below.
	a.startAskClock(open)
	if wait == nil {
		if !in.Blocking.waits() {
			// THE ASKER SAID ITS TURN DOES NOT WAIT. The question stays up, the
			// turn carries on around it, and the answer arrives as a message
			// ([Agent.deliverAskAnswer]).
			return askedText(q), false, nil
		}
		// NOTHING WAITS ON A RATIFY ([AskKind.Waits]), so the call comes straight
		// back and the question stands on its own. The turn goes on; if the
		// person strikes what was done, the answer reaches this conversation as a
		// message ([Agent.answerAsk]).
		return askShownLead + strings.TrimSpace(q.Head), false, nil
	}
	select {
	case answer, answered := <-wait:
		// A CLOSED WAIT IS THE PERSON TALKING PAST THE QUESTION, and it is the
		// one ending here that is neither an answer nor a cancellation: they
		// typed a sentence into the running turn instead of pressing a key, and
		// the splice let go of this question so the turn could read it
		// (steerquestion.go). Nothing was decided, so nothing is recorded as a
		// decision — the result says what happened and the words follow it.
		if !answered {
			// The splice claimed the words and said the withdrawal under the
			// lock it was already holding (steerquestion.go), so this only takes
			// the presence row down and finds nothing else to take.
			letGo()
			return askTalkedPast, false, nil
		}
		if !AnswerResolves(answer) {
			// THEY ASKED BACK, AND THE QUESTION IS STILL THEIRS. The call returns
			// so the model can answer them — a model parked in a tool cannot say
			// a word — and the question stays open on every screen it is drawn
			// on, with this book still holding it. Their answer, when it comes,
			// arrives as a message ([Agent.answerAsk]). The way down stays with
			// the question rather than being spent here, which is why this road
			// does NOT let go of it.
			return askedBackResult(q, answer)
		}
		letGo()
		// THE ONE RENDERING, so a model that waited reads exactly what a model
		// that carried on reads ([askAnswerText], asklane.go). There is no clock
		// case here any more: the clock belongs to the lane, because a question
		// nobody is parked on needs one too and this select is only reached when
		// somebody is.
		return askAnswerText(q, answer, theirs, nil), false, nil
	case <-ctx.Done():
		a.askLetGo(open, wait)
		letGo()
		return "", false, ctx.Err()
	}
}

// askLetGo is this call leaving a question it was parked on — the turn was
// interrupted, or it moved on without it.
//
// AN ANSWER THAT ARRIVED IN THE SAME INSTANT IS STILL AN ANSWER. It was recorded
// and announced by the door that took it, so throwing it away here would leave a
// decision in the record that the model was never told about; it becomes a
// message instead, which is what an answer with nobody parked already is. The
// answering road claimed the wait before it sent (askwait.go), so a settled
// entry here means the value is in the buffer and this is the only road left
// that can read it.
func (a *Agent) askLetGo(open *askOpen, wait <-chan Answer) {
	a.mu.Lock()
	settled := open.settled
	if settled == nil {
		a.asked.letGoLocked(open.q.ID)
	}
	a.mu.Unlock()
	if settled == nil {
		return
	}
	select {
	case answer := <-wait:
		a.deliverAskAnswer(answer, askAnswerText(open.q, answer, open.theirs, nil))
	default:
	}
}

// retireTurnQuestions withdraws every question of the model's own that the turn
// which raised it is not allowed to leave behind. It is the trigger a question
// that outlived its call never had, and the rule is one predicate rather than a
// list of kinds (askwait.go's [askedOfThePerson.retireLocked] says the whole of
// it).
//
// It runs at the end of a turn with a.mu DOWN, and is safe to run twice — a
// question already withdrawn is one [Agent.WithdrawQuestion] finds nothing for.
func (a *Agent) retireTurnQuestions() {
	a.mu.Lock()
	gone := a.asked.retireLocked()
	a.mu.Unlock()
	for _, open := range gone {
		if open.letGo != nil {
			// The lane's own way down: the presence row and the word book
			// together, which is what raised them (taskpresence.go's
			// [Agent.presenceAskingWhole]).
			open.letGo()
			continue
		}
		a.WithdrawQuestion(QuestionAsk, open.q.Token(), questionGoneReason(open.q))
	}
}

// answerAsk is the model lane's half of [Agent.ResolveQuestion]: it hands the
// answer to the call parked on the question, or — where the question outlived
// its call — to the model as a message.
func (a *Agent) answerAsk(answer Answer) error {
	if !AnswerResolves(answer) {
		// AN ASK-BACK TAKES THE CALL OFF THE QUESTION AND LEAVES THE QUESTION.
		// It is one question with two answers coming: the words now, the
		// decision later. The delivery cannot block and the entry is the
		// ownership, so this is one call under one lock (askwait.go).
		a.mu.Lock()
		delivered := a.asked.askedBackLocked(answer.ID, answer)
		open := a.asked.atLocked(answer.ID)
		a.mu.Unlock()
		if delivered || open == nil {
			return nil
		}
		// They asked back about a question that is already standing on its own,
		// so there is no call to come back. The words go to the model as words;
		// the question is untouched.
		a.enqueueSteering(askBackNote(open.q, answer))
		return nil
	}
	// THE SETTLE AND THE HANDOVER ARE ONE LOCKED STEP (askwait.go's
	// [askedOfThePerson.answerLocked]): the wait is claimed there before anything
	// is put on it, so a call letting go in the same instant and this road cannot
	// interleave into an answer that was recorded, announced, and handed to a
	// channel nobody will ever read.
	a.mu.Lock()
	prior := priorAnswerLocked(a.asked.atLocked(answer.ID), answer)
	open, wait, err := a.asked.answerLocked(answer.ID, answer)
	a.mu.Unlock()
	if err != nil {
		return err
	}
	if wait != nil {
		// The lane read it, and the lane owns the rest: it lets go of the
		// question and renders the answer to the model itself.
		return nil
	}
	text := askAnswerText(open.q, answer, open.theirs, prior)
	if !a.deliverAskAnswer(answer, text) {
		// THE CONVERSATION IS CLOSED AND NOBODY WILL EVER READ THIS. Undoing the
		// settle is what makes the refusal true: the question is still a
		// question, and the error stops [Agent.ResolveQuestion] recording a
		// decision whose answer the model was never told.
		a.mu.Lock()
		open.settled = prior
		a.mu.Unlock()
		return errAnswerClosed
	}
	if open.letGo != nil {
		// THE QUESTION OUTLIVED ITS CALL. The words were claimed by the answer
		// before this ran, so what this takes down is the presence row and
		// nothing else — the withdrawal it would otherwise send finds the
		// question already answered.
		open.letGo()
		a.mu.Lock()
		open.letGo = nil
		a.mu.Unlock()
	}
	return nil
}

// priorAnswerLocked is what this question was answered with BEFORE, for a
// revision that has to say what it replaced. It is nil for a first answer and
// for a change to something this book is not holding.
func priorAnswerLocked(open *askOpen, answer Answer) *Answer {
	if open == nil || !answer.Revises {
		return nil
	}
	return open.settled
}

// answerChangesNothing reports whether an answer asks for nothing to be done:
// the option the asker marked as changing nothing ([AnswerOption.Safe]), or an
// answer with no key and no words at all.
func answerChangesNothing(q Question, answer Answer) bool {
	if strings.TrimSpace(answer.Words()) != "" {
		return false
	}
	keys := answer.Keys()
	if len(keys) == 0 {
		return true
	}
	for _, key := range keys {
		option, ok := q.Option(key)
		if !ok || !option.Safe {
			return false
		}
	}
	return true
}

// The three sentences this lane says about a question that is not a tool call
// waiting for an answer. They are constants because each is read by a model and
// by the manual's own account of what `ask` returns.
const (
	// askShownLead opens what a ratify hands straight back: it was put in front
	// of the person and the turn carries on.
	askShownLead = "shown, and nothing waits on it — carry on. If they strike it you will be told: "
	// askedBackLead opens the result of a question the person asked back on.
	askedBackLead = "they asked you this before answering, and the question is still open on their screen — answer them in your reply and do not ask it again; their answer will arrive as a message: "
	// askBackNoteLead opens the message carrying words said about a question that
	// is still standing.
	askBackNoteLead = "The person said this about the question still open — "
)

// askedBackResult is what the call returns when the person asked back: their
// words, and what is true about the question they asked them about.
func askedBackResult(q Question, answer Answer) (string, bool, error) {
	return askedBackLead + strings.TrimSpace(q.Head) + "\n" + askedBackWords(answer), false, nil
}

// askBackNote is the same words as a message, for an ask-back about a question
// that is standing on its own.
func askBackNote(q Question, answer Answer) string {
	return askBackNoteLead + strings.TrimSpace(q.Head) + "\n" + askedBackWords(answer)
}

// askedBackWords is everything the person asked, one line each, in the order
// they said it.
func askedBackWords(answer Answer) string {
	lines := make([]string, 0, len(answer.AskedBack)+1)
	for _, exchange := range answer.AskedBack {
		if asked := strings.TrimSpace(exchange.Asked); asked != "" {
			lines = append(lines, asked)
		}
	}
	if words := strings.TrimSpace(answer.Words()); words != "" {
		lines = append(lines, words)
	}
	return strings.Join(lines, "\n")
}

// askDigitKeys makes every answer's key ONE DIGIT, in the order the asker gave
// them, and hands back what the asker had called each one.
//
// THE KEY GRAMMAR IS THE SURFACE'S AND NOT THE ASKER'S. A model writes keys like
// `landscape` and `still-life` because the schema said "key" and a word is what
// a model reaches for; a person then sees `[landscape] Landscapes & seascapes`
// on a row that has to hold eight of those, and there is no single key on the
// keyboard that presses it. Every other question on this surface is answered
// with `1`–`9` (questionkeys.go), so a question the model raises is too. The
// asker's own names are kept beside the digits and given back on the answer
// ([askInTheirKeys]), so the model reads its answer in the words it wrote and
// the person never sees them.
//
// Keys that already are single digits in order are left exactly as they came,
// so nothing moves under an asker that followed the grammar.
func askDigitKeys(options []AnswerOption, pick *Pick) map[string]string {
	ordered := true
	for i, option := range options {
		if strings.TrimSpace(option.Key) != itoaKey(i+1) {
			ordered = false
			break
		}
	}
	if ordered {
		return nil
	}
	theirs := make(map[string]string, len(options))
	for i := range options {
		digit := itoaKey(i + 1)
		theirs[digit] = strings.TrimSpace(options[i].Key)
		if pick != nil && strings.TrimSpace(pick.Key) == strings.TrimSpace(options[i].Key) {
			pick.Key = digit
		}
		options[i].Key = digit
	}
	return theirs
}

func itoaKey(n int) string { return fmt.Sprintf("%d", n) }

// askInTheirKeys is the answer as the asker reads it: every digit the person
// pressed translated back to the name the asker gave that answer, and the
// labels beside them, so the model needs no table of its own to know what
// `2` meant.
func askInTheirKeys(answer Answer, q Question, theirs map[string]string) Answer {
	name := func(key string) string {
		if own, ok := theirs[key]; ok && own != "" {
			return own
		}
		return key
	}
	if answer.Key != "" {
		answer.Key = name(answer.Key)
	}
	picked := make([]string, 0, len(answer.Picked))
	for _, key := range answer.Picked {
		picked = append(picked, name(key))
	}
	if len(picked) > 0 {
		answer.Picked = picked
	}
	for _, key := range answer.Keys() {
		for _, option := range q.Options {
			if name(strings.TrimSpace(option.Key)) == key {
				answer.Labels = append(answer.Labels, strings.TrimSpace(option.Label))
			}
		}
	}
	return answer
}

func defaultAnswer(q Question, from string) Answer {
	answer := Answer{Kind: QuestionAsk, ID: q.ID, Ask: q.Ask, DecidedBy: DecidedByDial, From: from, At: time.Now()}
	if q.Ask == AskAssumption {
		for _, option := range q.Options {
			answer.Picked = append(answer.Picked, option.Key)
		}
	} else if q.Pick != nil {
		answer.Key, answer.Picked = q.Pick.Key, []string{q.Pick.Key}
	}
	return answer
}

func headlessAnswerLine(q Question, answer Answer) string {
	picked := strings.Join(answer.Keys(), ",")
	return fmt.Sprintf("asked: %s → %s (default · nobody to ask)", strings.TrimSpace(q.Head), picked)
}

func marshalAnswer(answer Answer) (string, bool, error) {
	data, err := json.Marshal(answer)
	if err != nil {
		return "", true, fmt.Errorf("encode answer: %w", err)
	}
	return string(data), false, nil
}
