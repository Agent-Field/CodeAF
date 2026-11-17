package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── factory.job_answer: answer a step's question ────────────────────────────
//
// A step that asks a question while it works — one of this factory's own
// steps, or a codeaf harness step an AgentField run streams here — waits in
// Needs you with needs_you.question.kind "step", its options, the step's own
// pick and why. This is the person's answer to it: the option they chose,
// their own words, or both. It names the question it answers (question.id),
// so an answer to a question that has since changed is refused rather than
// given to the wrong one. An AgentField run's approval (kind
// agentfield_approval) is answered with Approve, Ask for changes and Reject,
// never here.

// FeatureFactoryJobAnswer is announced in hello when the engine serves
// factory.job_answer.
const FeatureFactoryJobAnswer = "factory.job_answer"

// JobActAnswer is factory.job_answer's action in FactoryJobActed.
const JobActAnswer = "answer"

// MaxFactoryAnswerOption is how long a chosen option may be, in characters.
const MaxFactoryAnswerOption = 300

// FactoryJobAnswerParams is one answer. QuestionID is needs_you.question.id;
// Option is one of the question's options, by its label; Words are the
// person's own. At least one of Option and Words is given; with both, the step
// reads "<option>: <words>".
type FactoryJobAnswerParams struct {
	Factory    string      `json:"factory"`
	Job        int         `json:"job"`
	QuestionID string      `json:"question_id"`
	Option     string      `json:"option,omitempty"`
	Words      string      `json:"words,omitempty"`
	Expect     *api.Expect `json:"expect"`
}

// FactoryJobAnswerer is the engine's half of factory.job_answer.
type FactoryJobAnswerer interface {
	FactoryJobAnswer(ctx context.Context, p FactoryJobAnswerParams) (FactoryJobActed, error)
}

func withFactoryJobAnswerFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryJobAnswerer); ok {
		hello = withFeature(hello, FeatureFactoryJobAnswer)
	}
	return hello
}

func init() {
	methods["factory.job_answer"] = shaped[FactoryJobAnswerParams, FactoryJobActed](jobAnswer)
}

func prepJobAnswer(p *FactoryJobAnswerParams) error {
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return err
	}
	p.QuestionID = strings.TrimSpace(p.QuestionID)
	if p.QuestionID == "" || len(p.QuestionID) > 256 {
		return Errorf(CodeBadParams, "question_id is required: send needs_you.question.id")
	}
	p.Option = strings.Join(strings.Fields(p.Option), " ")
	if utf8.RuneCountInString(p.Option) > MaxFactoryAnswerOption {
		return Errorf(CodeBadParams, "option is one of the question's options, at most %d characters", MaxFactoryAnswerOption)
	}
	var err error
	if p.Words, err = words("words", p.Words, MaxFactoryJobWords, false); err != nil {
		return err
	}
	if p.Option == "" && p.Words == "" {
		return Errorf(CodeBadParams, "an answer is an option, words, or both")
	}
	return nil
}

func jobAnswer(e Engine, raw json.RawMessage) (*FactoryJobActed, error) {
	const name = "factory.job_answer"
	a, ok := e.(FactoryJobAnswerer)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	var p FactoryJobAnswerParams
	if err := decodeStrict(name, raw, &p); err != nil {
		return nil, err
	}
	if err := prepJobAnswer(&p); err != nil {
		return nil, err
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := a.FactoryJobAnswer(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}
