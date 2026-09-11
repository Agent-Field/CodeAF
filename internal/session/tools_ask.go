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
const askSchemaTemplate = `{"type":"object","properties":{"head":{"type":"string","description":"Decision in one sentence"},"kind":{"type":"string","enum":["permission","choice","judgement","clarification","confirmation","landing","assumption","ratify"],"description":"assumption options stand unless struck"},"form":{"type":"string","enum":["line","card","room","sheet"],"description":"Smallest form evidence needs"},"reason":{"type":"string","description":"Why ask now after cheaper rungs"},"subject":{"type":"object","description":"Existing subject","properties":{"kind":{"type":"string","enum":["call","task","page","run","account"]},"id":{"type":"integer","description":"Task id"},"callId":{"type":"string"},"ref":{"type":"string","description":"String id"},"name":{"type":"string"}}},"options":{"type":"array","description":"Answers, or assumptions standing by default","items":{"type":"object","properties":{"key":{"type":"string","description":"A single digit, 1 upward in order. Anything else is renumbered and given back as you wrote it"},"label":{"type":"string","description":"Short label"},"body":{"type":"string","description":"Meaning"},"consequence":{"type":"string","description":"What happens"},"safe":{"type":"boolean","description":"Changes nothing"},"widening":{"type":"boolean","description":"Grants more"},"blocks":{"type":"array","description":"Evidence","items":BLOCK},"dimensions":{"type":"object","description":"Comparison axis values, one short text per axis","additionalProperties":{"type":"string"}}},"required":["key","label"]}},"input":{"type":"object","description":"Extra input. checklist ticks up to 8 of the options, which stay in options; blanks, pairs and dial carry their own shape here","properties":{"kind":{"type":"string","enum":["text","blanks","checklist","pairs","dial"]},"blanks":{"type":"array","description":"Named typed fields, kind blanks only"},"pairs":{"type":"array","description":"Paired choices"},"dial":{"type":"object","description":"Numeric range and default"}}},"pick":{"type":"object","description":"Recommended answer","properties":{"key":{"type":"string","description":"Option key"},"reason":{"type":"string"},"confidence":{"type":"string","enum":["low","medium","high"]},"wouldChange":{"type":"string","description":"What changes the pick"}},"required":["key"]},"stakes":{"type":"string","enum":["reversible","costly","irreversible"],"description":"Cost if wrong"},"blocking":{"type":"object","description":"What waits","properties":{"turn":{"type":"boolean"},"tasks":{"type":"array","description":"Task names","items":{"type":"string"}}}},"scope":{"type":"array","description":"Allowed lifetimes","items":{"type":"string","enum":["once","task","project","always"]}},"attach":{"type":"array","description":"Evidence before answers","items":BLOCK}},"required":["head","kind","reason","stakes"],"additionalProperties":false}`

// askSchemaJSON is the wire schema: the template with the block written out in
// full at both places it stands.
var askSchemaJSON = strings.NewReplacer("BLOCK", askBlockSchemaJSON).Replace(askSchemaTemplate)

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
	q := Question{ID: id, Kind: QuestionAsk, Ask: in.Kind, Form: in.Form, Asker: Asker{Kind: AskerModel}, Head: in.Head, Reason: in.Reason, Subject: in.Subject, Options: in.Options, Input: in.Input, Pick: in.Pick, Stakes: in.Stakes, Blocking: in.Blocking, Scope: in.Scope, Attach: in.Attach}
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
	wait := make(chan Answer, 1)
	a.mu.Lock()
	if a.askWaits == nil {
		a.askWaits = make(map[uint64]chan Answer)
	}
	a.askWaits[id] = wait
	a.mu.Unlock()
	forget, err := a.AskQuestion(q)
	if err != nil {
		a.mu.Lock()
		delete(a.askWaits, id)
		a.mu.Unlock()
		return askRefusedLead + err.Error(), false, nil
	}
	defer forget()
	var clock <-chan time.Time
	if q.Policy.Kind == PolicyRecommendThenAuto {
		timer := time.NewTimer(q.Policy.After)
		defer timer.Stop()
		clock = timer.C
	}
	select {
	case answer := <-wait:
		return marshalAnswer(askInTheirKeys(answer, q, theirs))
	case <-clock:
		answer := defaultAnswer(q, "")
		_ = a.ResolveQuestion(answer)
		return marshalAnswer(askInTheirKeys(answer, q, theirs))
	case <-ctx.Done():
		a.mu.Lock()
		delete(a.askWaits, id)
		a.mu.Unlock()
		return "", false, ctx.Err()
	}
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
