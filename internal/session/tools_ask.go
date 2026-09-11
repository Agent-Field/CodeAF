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

	"github.com/Agent-Field/aforge-v2/internal/exec/bare"
)

const askDescription = "Ask only after the decision ladder is exhausted. Give a reason, stakes, structured answers before free text, your pick and what would change it. Returns the person's whole answer."

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
const askSchemaTemplate = `{"type":"object","properties":{"head":{"type":"string","description":"Decision in one sentence"},"kind":{"type":"string","enum":ASKKINDS,"description":"assumption options stand unless struck; ratify waits on nothing"},"form":{"type":"string","enum":FORMS,"description":"Smallest form evidence needs"},"reason":{"type":"string","description":"Why ask now after cheaper rungs"},"subject":{"type":"object","description":"Existing subject","properties":{"kind":{"type":"string","enum":SUBJECTS},"id":{"type":"integer","description":"Task id"},"callId":{"type":"string"},"ref":{"type":"string","description":"String id"},"name":{"type":"string"}}},"options":{"type":"array","description":"Answers, or assumptions that stand","items":{"type":"object","properties":{"key":{"type":"string","description":"A single digit, 1 upward. Anything else is renumbered and given back as you wrote it"},"label":{"type":"string","description":"Short label"},"body":{"type":"string","description":"Meaning"},"consequence":{"type":"string","description":"What happens"},"safe":{"type":"boolean","description":"Changes nothing"},"widening":{"type":"boolean","description":"Grants more"},"blocks":{"type":"array","description":"Evidence","items":BLOCK},"dimensions":{"type":"object","description":"Value per axis","additionalProperties":{"type":"string"}}},"required":["key","label"]}},"input":{"type":"object","description":"checklist and pairs use the options; blanks and dial carry their shape here","properties":{"kind":{"type":"string","enum":INPUTS},"blanks":{"type":"array","description":"Named fields, kind blanks","items":{"type":"object","properties":{"label":{"type":"string"},"kind":{"type":"string","enum":BLANKS},"default":{"type":"string","description":"Prefilled"},"choices":{"type":"array","description":"kind choice","items":{"type":"string"}}},"required":["label"]}},"dial":{"type":"object","description":"Number on a range, kind dial","properties":{"min":{"type":"number"},"max":{"type":"number"},"default":{"type":"number"},"labels":{"type":"array","description":"Words for the ends","items":{"type":"string"}}},"required":["min","max"]},"prompt":{"type":"string","description":"One line over the box"},"secret":{"type":"boolean","description":"A credential, never drawn back"}}},"pick":{"type":"object","description":"Recommended answer","properties":{"key":{"type":"string","description":"Option key"},"reason":{"type":"string"},"confidence":{"type":"string","enum":CONFIDENCE},"wouldChange":{"type":"string","description":"What would change it"}},"required":["key"]},"stakes":{"type":"string","enum":STAKES,"description":"Cost if wrong"},"blocking":{"type":"object","description":"What waits","properties":{"turn":{"type":"boolean"},"tasks":{"type":"array","description":"Task names","items":{"type":"string"}}}},"scope":{"type":"array","description":"Allowed lifetimes","items":{"type":"string","enum":SCOPES}},"attach":{"type":"array","description":"Evidence before answers","items":BLOCK}},"required":["head","kind","reason","stakes"],"additionalProperties":false}`

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
	Blocking Blocking       `json:"blocking"`
	Scope    []AnswerScope  `json:"scope"`
	Attach   []Block        `json:"attach"`
}

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
	q := Question{ID: id, Kind: QuestionAsk, Ask: in.Kind, Form: in.Form, Asker: Asker{Kind: AskerModel}, Head: in.Head, Reason: in.Reason, Subject: in.Subject, Options: in.Options, Input: in.Input, Pick: in.Pick, Stakes: in.Stakes, Blocking: in.Blocking, Scope: in.Scope, Attach: in.Attach,
		// AND WHICH STEP ASKED IT. A model may call `ask` three times in one
		// batch, and those three questions are one moment to a person
		// (question.go's [Question.Batch]).
		Batch: a.stepToken()}
	// AND WHETHER THE ASKER IS WAITING FOR IT AT ALL. A model that says nothing
	// is blocked on the turn has said it will read the answer whenever it comes,
	// which is the one thing that lets a question stand after the turn that
	// raised it ends ([questionOutlivesTurn]). The [AskKind.Waits] term keeps a
	// ratify out of it: a ratify names something this turn already did, and one
	// still on screen after the turn is a question about work nobody is doing.
	q.Later = q.Ask.Waits() && !in.Blocking.Turn
	// AND `blocking.turn` IS DERIVED AND NEVER THE MODEL'S CLAIM. What stops the
	// turn is whether this call actually parks, which is the kind and nothing
	// else ([AskKind.Waits]) — so a model that writes `blocking: {turn: true}`
	// on a ratify cannot put a row on the waiting desk about a turn that is
	// carrying on, and a model that writes nothing at all cannot make home draw
	// `idle` over work that has stopped. The model's own word about waiting is
	// kept, once, in [Question.Later] above; nothing downstream may read this
	// field as an argument the asker supplied.
	//
	// The tasks half is the asker's, because only the asker knows what work of
	// its own is waiting and this engine cannot derive that from anything.
	q.Blocking = Blocking{Turn: q.Ask.Waits(), Tasks: in.Blocking.Tasks}
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
	if !a.config.Interactive && q.Policy.Kind == PolicyAsk {
		if q.Pick == nil {
			return "your call: " + strings.TrimSpace(q.Head) + " (nobody to ask)", false, nil
		}
		q.Policy = Policy{Kind: PolicyDecide}
	}
	if q.Policy.Kind == PolicyDecide {
		answer := defaultAnswer(q, "")
		answer.From = headlessAnswerLine(q, answer)
		a.recordDecision(decisionRecordOf(q, answer))
		// AND THE LINE LEAVES THE ENGINE. docs/design/questions/DESIGN.md's
		// HEADLESS law is that the policy applies AND IS PRINTED — a run that
		// took a default silently is a run whose decision nobody can find
		// afterwards. The engine does not print; the answer carries the sentence
		// in [Answer.From] and this is what puts it where a door with nobody at
		// it can read it (cmd/aforge's --once). Nothing was ever asked here, so
		// a surface hearing this about a question it never drew does nothing
		// with it, which is what [app.foldOthersAnswer] already does.
		a.emitQuestion(EventQuestionAnswered, q, &answer)
		return marshalAnswer(askInTheirKeys(answer, q, theirs))
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
	if wait == nil {
		// NOTHING WAITS ON A RATIFY ([AskKind.Waits]), so the call comes straight
		// back and the question stands on its own. The turn goes on; if the
		// person strikes what was done, the answer reaches this conversation as a
		// message ([Agent.answerAsk]).
		return askShownLead + strings.TrimSpace(q.Head), false, nil
	}
	var clock <-chan time.Time
	if q.Policy.Kind == PolicyRecommendThenAuto {
		timer := time.NewTimer(q.Policy.After)
		defer timer.Stop()
		clock = timer.C
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
		return marshalAnswer(askInTheirKeys(answer, q, theirs))
	case <-clock:
		answer := defaultAnswer(q, "")
		_ = a.ResolveQuestion(answer)
		letGo()
		return marshalAnswer(askInTheirKeys(answer, q, theirs))
	case <-ctx.Done():
		a.mu.Lock()
		a.asked.letGoLocked(id)
		a.mu.Unlock()
		letGo()
		return "", false, ctx.Err()
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
	// The delivery cannot block and the entry is the ownership, so this is one
	// call under one lock rather than the read-unlock-send it was (askwait.go).
	// An answer to an ask that has already ended finds nothing parked and is
	// nothing to do (answers.go's second law: a late answer is ignored and
	// nothing says so).
	a.mu.Lock()
	open, said := a.asked.answerLocked(answer.ID, answer)
	a.mu.Unlock()
	if !said {
		return nil
	}
	if open.wait != nil {
		// The lane read it, and the lane owns the rest: it lets go of the
		// question and returns the answer to the model itself.
		return nil
	}
	if open.letGo != nil {
		// THE QUESTION OUTLIVED ITS CALL. The words were claimed by the answer
		// before this ran, so what this takes down is the presence row and
		// nothing else — the withdrawal it would otherwise send finds the
		// question already answered.
		open.letGo()
	}
	a.tellAskAnswer(open, answer)
	return nil
}

// tellAskAnswer puts the answer to a question that outlived its call in front of
// the model, in the same JSON the tool call would have returned.
//
// IT IS OWED A TURN WHEN IT ASKS FOR SOMETHING. A person who struck an
// assumption, picked against what stands or typed a sentence has said something
// the model has to act on, so the note wakes the conversation the way a landed
// task's report does. An answer that changes nothing — the safe answer on a
// ratify, or a bare acknowledgement — is context for the next request and
// nothing more, so it rides the ambient lane and starts no turn: waking a model
// to be told that what it already did is fine is the session talking to itself.
func (a *Agent) tellAskAnswer(open *askOpen, answer Answer) {
	body, _, err := marshalAnswer(askInTheirKeys(answer, open.q, open.theirs))
	if err != nil {
		return
	}
	note := askAnsweredLead + strings.TrimSpace(open.q.Head) + "\n" + body
	if answerChangesNothing(open.q, answer) {
		a.enqueueAmbientNote(note)
		return
	}
	a.enqueueSteering(note)
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
	// askAnsweredLead opens the message carrying an answer to a question that
	// outlived the call that asked it.
	askAnsweredLead = "The person answered the question you left open — "
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
