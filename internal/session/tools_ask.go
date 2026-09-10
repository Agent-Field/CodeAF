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

const askSchemaJSON = `{"type":"object","properties":{"head":{"type":"string","description":"Decision in one sentence"},"kind":{"type":"string","enum":["permission","choice","judgement","clarification","confirmation","landing","assumption","ratify"],"description":"Decision kind; assumption options stand unless struck"},"form":{"type":"string","enum":["line","card","room","sheet"],"description":"Smallest form evidence needs"},"reason":{"type":"string","description":"Why ask now after cheaper rungs"},"subject":{"type":"object","description":"Existing subject","properties":{"kind":{"type":"string","enum":["call","task","page","run","account"],"description":"Subject kind"},"id":{"type":"integer","description":"Task id"},"callId":{"type":"string","description":"Call id"},"ref":{"type":"string","description":"String id"},"name":{"type":"string","description":"Visible name"}}},"options":{"type":"array","description":"Answers, or assumptions standing by default","items":{"type":"object","properties":{"key":{"type":"string","description":"Answer key"},"label":{"type":"string","description":"Short label"},"body":{"type":"string","description":"Meaning"},"consequence":{"type":"string","description":"What happens"},"safe":{"type":"boolean","description":"Changes nothing"},"widening":{"type":"boolean","description":"Grants more"},"blocks":{"type":"array","description":"Evidence","items":{"$ref":"#/$defs/block"}},"dimensions":{"type":"object","description":"Comparison axis values"}},"required":["key","label"]}},"input":{"type":"object","description":"Extra structured input","properties":{"kind":{"type":"string","enum":["text","blanks","checklist","pairs","dial"],"description":"Input kind"},"blanks":{"type":"array","description":"Named typed fields"},"pairs":{"type":"array","description":"Paired choices"},"dial":{"type":"object","description":"Numeric range and default"}}},"pick":{"type":"object","description":"Recommended answer","properties":{"key":{"type":"string","description":"Option key"},"reason":{"type":"string","description":"Why"},"confidence":{"type":"string","enum":["low","medium","high"],"description":"Certainty"},"wouldChange":{"type":"string","description":"What changes the pick"}},"required":["key"]},"stakes":{"type":"string","enum":["reversible","costly","irreversible"],"description":"Cost if wrong"},"blocking":{"type":"object","description":"What waits","properties":{"turn":{"type":"boolean","description":"Turn waits"},"tasks":{"type":"array","description":"Task names","items":{"type":"string"}}}},"scope":{"type":"array","description":"Allowed lifetimes","items":{"type":"string","enum":["once","task","project","always"]}},"attach":{"type":"array","description":"Evidence before answers","items":{"$ref":"#/$defs/block"}}},"required":["head","kind","reason","stakes"],"additionalProperties":false,"$defs":{"block":{"type":"object","description":"Evidence block","properties":{"kind":{"type":"string","enum":["text","diagram","table","diff","image","layout"],"description":"Block kind"},"title":{"type":"string","description":"Heading"},"body":{"type":"string","description":"Text or lines"},"rows":{"type":"array","description":"Table rows","items":{"type":"array","items":{"type":"string"}}},"path":{"type":"string","description":"Image path"}},"required":["kind"]}}}`

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

func (a *Agent) askTool() bare.Tool {
	return bare.Tool{Name: "ask", Description: askDescription, Schema: json.RawMessage(askSchemaJSON), Execute: a.executeAsk}
}

func (a *Agent) executeAsk(ctx context.Context, raw json.RawMessage) (string, bool, error) {
	var in askArguments
	if err := decodeToolArguments(raw, &in); err != nil {
		return "Invalid arguments: " + err.Error(), true, nil
	}
	id := a.askSeq.Add(1)
	q := Question{ID: id, Kind: QuestionAsk, Ask: in.Kind, Form: in.Form, Asker: Asker{Kind: AskerModel}, Head: in.Head, Reason: in.Reason, Subject: in.Subject, Options: in.Options, Input: in.Input, Pick: in.Pick, Stakes: in.Stakes, Blocking: in.Blocking, Scope: in.Scope, Attach: in.Attach}
	q.Policy = a.autonomyFor(q.Ask)
	if q.Stakes == StakesIrreversible {
		q.Policy = Policy{Kind: PolicyAsk}
	}
	if q.Policy.Kind == PolicyRecommendThenAuto {
		q.Deadline = time.Now().Add(q.Policy.After)
	}
	if err := q.Check(a.Decisions()); err != nil {
		return err.Error(), false, nil
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
		return marshalAnswer(answer)
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
		return err.Error(), false, nil
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
		return marshalAnswer(answer)
	case <-clock:
		answer := defaultAnswer(q, "")
		_ = a.ResolveQuestion(answer)
		return marshalAnswer(answer)
	case <-ctx.Done():
		a.mu.Lock()
		delete(a.askWaits, id)
		a.mu.Unlock()
		return "", false, ctx.Err()
	}
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
