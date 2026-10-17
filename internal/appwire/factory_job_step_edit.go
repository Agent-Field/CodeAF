package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// ── PER-JOB STEP EDITS ON AN ATTACHED FACTORY ───────────────────────────────
//
// factory.job_step_edits_read and factory.job_step_edit carry the factory's
// per-JOB step edit route (GET and POST /v1/jobs/{id}/step-edits). They change
// the steps of ONE job that is not finished; they never read or write the
// repository's .codeaf/factory.yaml. That file, and the recipe every FUTURE job
// starts from, belong to factory.pipeline_*.
//
// THE FACTORY DECIDES; THIS WIRE ONLY CARRIES. Who may edit (the job's person),
// which steps have already run, whether the job is finished or publishing, the
// recipe's rules and the nine-step cap are the factory's, read from its source
// and answered whole. Provenance is the caller the factory's HTTP route
// authenticates; no field of these params can name another author.
//
// A WRITE CARRIES THE VERSION THE PERSON READ. `expect.version` is required here
// even though the factory's route treats `expect` as optional: an edit with no
// version would land on steps the person never saw. The version comes only from
// a successful factory.job_step_edits_read (or the `version` of an earlier
// edit's answer); a read that finds no version (a factory without a store)
// answers 0, and a write cannot carry 0. A stale version is [CodeStale] and the
// edit is not sent again.
//
// THE FEATURE IS A CARRIER, NOT A PERMISSION. Announcing
// [FeatureFactoryJobStepEdit] says only that this engine can carry the two
// methods. Whether THIS job can be edited by THIS person is the job's own
// `controls.edit_steps` AND a successful versioned read; even then the factory
// can still refuse the write (for example when it has no worker desk).
//
// WHAT AN EDIT CAN SAY is deliberately the factory's bounded edit language and
// no more: reword a step's ask, set its thinking, switch steps on or off, and
// add a step with only a name, kind, ask, until and effort, optionally after
// another step. It cannot reorder or remove a step, cannot change a step's kind
// or its max, when, fanout, proof or model, and cannot name a path. A part the
// language lacks is `bad_params` (a field this wire does not carry is refused,
// not ignored).
//
// AN EDIT CAN REQUEUE A RUNNING JOB. When the factory applies an edit whose
// `lines` are not empty while a worker holds the job, the running step goes
// back to the queue and that worker is cut off. The app says so before it sends.

// FeatureFactoryJobStepEdit is announced in hello when the engine serves
// factory.job_step_edits_read and factory.job_step_edit.
const FeatureFactoryJobStepEdit = "factory.job_step_edit"

// FactoryJobStepEditsParams names the job whose steps are read.
type FactoryJobStepEditsParams struct {
	Factory string `json:"factory"`
	Job     int    `json:"job"`
}

// FactoryNewStep is the part of a step a person may give a step they add. It is
// narrower than api.StepDef on purpose: strict decoding refuses every other
// field of a step.
type FactoryNewStep struct {
	Name string `json:"name,omitempty"`
	// Kind is empty or chat (a conversation), or gate (a step the person
	// approves). Any other kind is refused by the factory.
	Kind   string `json:"kind,omitempty" enum:"chat,gate"`
	Ask    string `json:"ask,omitempty"`
	Until  string `json:"until,omitempty"`
	Effort string `json:"effort,omitempty" enum:"cheap,strong"`
}

// FactoryStepAddition is one step to add, optionally after a named step.
type FactoryStepAddition struct {
	Step  FactoryNewStep `json:"step"`
	After string         `json:"after,omitempty"`
}

// FactoryStepEdit is the whole edit language of one request. Every name is a
// step of the job as the person read it.
type FactoryStepEdit struct {
	Add []FactoryStepAddition `json:"add,omitempty"`
	// Ask maps a step name to its new ask.
	Ask map[string]string `json:"ask,omitempty"`
	// Thinking maps a step name to cheap or strong; "" returns it to the usual.
	Thinking map[string]string `json:"thinking,omitempty"`
	On       []string          `json:"on,omitempty"`
	Skip     []string          `json:"skip,omitempty"`
	// Why is one line, recorded with the edit.
	Why string `json:"why,omitempty"`
}

// FactoryJobStepEditParams is one edit of one job, made from one read.
type FactoryJobStepEditParams struct {
	Factory string          `json:"factory"`
	Job     int             `json:"job"`
	Expect  *api.Expect     `json:"expect"`
	Edit    FactoryStepEdit `json:"edit"`
}

// FactoryJobStepEdits is the job's steps as the factory's step-edit route reads
// them. Version is the token the next write carries; 0 means the factory gave
// none and no write is possible. Lines are the factory's account of an edit and
// are empty on a read.
type FactoryJobStepEdits struct {
	Factory api.Factory `json:"factory"`
	Job     int         `json:"job"`
	Version int         `json:"version"`
	Steps   []api.Step  `json:"steps"`
	Lines   []string    `json:"lines"`
}

// FactoryJobStepEdited is the answer to an edit the factory accepted: the same
// facts as a read, after the edit, plus the job as it reads now. Current is one
// fresh read the engine makes after the factory said yes; it is absent when that
// read failed, which says nothing about the edit. Empty Lines mean the factory
// changed nothing. The next write carries Version, or Current.Version when the
// person has read the job since.
type FactoryJobStepEdited struct {
	Factory api.Factory `json:"factory"`
	Job     int         `json:"job"`
	Version int         `json:"version"`
	Steps   []api.Step  `json:"steps"`
	Lines   []string    `json:"lines"`
	Current *api.Job    `json:"current,omitempty"`
}

// FactoryJobStepEditor is the engine's half of the two methods. It is optional
// on top of [Factory] and independent of [FactoryActor]: an engine without it
// refuses both as unknown_method and does not announce the feature. The write
// makes at most one request to the factory and never sends it twice; an outcome
// it cannot confirm is [CodeUncertain].
type FactoryJobStepEditor interface {
	FactoryJobStepEditsRead(ctx context.Context, p FactoryJobStepEditsParams) (FactoryJobStepEdits, error)
	FactoryJobStepEdit(ctx context.Context, p FactoryJobStepEditParams) (FactoryJobStepEdited, error)
}

func withFactoryJobStepEditFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryJobStepEditor); ok {
		hello = withFeature(hello, FeatureFactoryJobStepEdit)
	}
	return hello
}

func factoryStepEditorOf(e Engine, name string) (FactoryJobStepEditor, error) {
	s, ok := e.(FactoryJobStepEditor)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return s, nil
}

// factoryStepEditMethods is the table of the two methods, for the tests that
// walk it.
var factoryStepEditMethods = map[string]method{}

func init() {
	for name, m := range map[string]method{
		"factory.job_step_edits_read": shaped[FactoryJobStepEditsParams, FactoryJobStepEdits](readStepEdits),
		"factory.job_step_edit":       shaped[FactoryJobStepEditParams, FactoryJobStepEdited](writeStepEdit),
	} {
		methods[name] = m
		factoryStepEditMethods[name] = m
	}
}

func readStepEdits(e Engine, raw json.RawMessage) (*FactoryJobStepEdits, error) {
	s, err := factoryStepEditorOf(e, "factory.job_step_edits_read")
	if err != nil {
		return nil, err
	}
	var p FactoryJobStepEditsParams
	if err := decodeStrict("factory.job_step_edits_read", raw, &p); err != nil {
		return nil, err
	}
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return nil, err
	}
	if p.Job < 1 {
		return nil, Errorf(CodeBadParams, "job must be a whole number from 1")
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := s.FactoryJobStepEditsRead(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func writeStepEdit(e Engine, raw json.RawMessage) (*FactoryJobStepEdited, error) {
	s, err := factoryStepEditorOf(e, "factory.job_step_edit")
	if err != nil {
		return nil, err
	}
	var p FactoryJobStepEditParams
	if err := decodeStrict("factory.job_step_edit", raw, &p); err != nil {
		return nil, err
	}
	if err := checkJob(&p.Factory, p.Job, p.Expect); err != nil {
		return nil, err
	}
	if err := checkStepEdit(&p.Edit); err != nil {
		return nil, err
	}
	ctx, cancel := factoryCall()
	defer cancel()
	out, err := s.FactoryJobStepEdit(ctx, p)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// stepWords is a line of text of at most limit characters, whitespace folded as
// the factory folds it.
func stepWords(field, value string, limit int) (string, error) {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) > limit {
		return "", Errorf(CodeBadParams, "%s is at most %d characters", field, limit)
	}
	return value, nil
}

// stepRef is a step name the edit refers to: it is checked only for being a
// name that can be one, because which steps the job has is the factory's to say.
func stepRef(field, name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > MaxFactoryStepName || strings.ContainsFunc(name, func(r rune) bool { return r < ' ' || r == 0x7f }) {
		return "", Errorf(CodeBadParams, "%s must name a step of the job", field)
	}
	return name, nil
}

func stepRefs(field string, names []string) ([]string, error) {
	if len(names) > factory.StageMost {
		return nil, Errorf(CodeBadParams, "%s names at most %d steps", field, factory.StageMost)
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		n, err := stepRef(field, n)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, nil
}

// checkStepEdit trims the edit and refuses what the factory's edit language
// cannot say. The bounds are the factory's own ([factory.StageMost],
// [factory.AskMost], [factory.StageWord], [factory.EffortWords]); nothing here
// is tighter, and the factory still decides everything else.
func checkStepEdit(ed *FactoryStepEdit) error {
	var err error
	if ed.Why, err = stepWords("why", ed.Why, MaxFactoryJobWords); err != nil {
		return err
	}
	if len(ed.Add) > factory.StageMost || len(ed.Ask) > factory.StageMost || len(ed.Thinking) > factory.StageMost {
		return Errorf(CodeBadParams, "an edit changes at most %d steps in one part", factory.StageMost)
	}
	if ed.On, err = stepRefs("on", ed.On); err != nil {
		return err
	}
	if ed.Skip, err = stepRefs("skip", ed.Skip); err != nil {
		return err
	}
	asks := make(map[string]string, len(ed.Ask))
	for name, ask := range ed.Ask {
		if name, err = stepRef("ask", name); err != nil {
			return err
		}
		if ask, err = stepWords("ask", ask, factory.AskMost); err != nil {
			return err
		}
		if ask == "" {
			return Errorf(CodeBadParams, "a step needs an ask; %s has none", name)
		}
		if _, duplicate := asks[name]; duplicate {
			return Errorf(CodeBadParams, "%s is named twice", name)
		}
		asks[name] = ask
	}
	ed.Ask = nilIfEmpty(asks)
	thinking := make(map[string]string, len(ed.Thinking))
	for name, word := range ed.Thinking {
		if name, err = stepRef("thinking", name); err != nil {
			return err
		}
		word = strings.ToLower(strings.TrimSpace(word))
		if word != "" && word != factory.EffortWords[0] && word != factory.EffortWords[1] {
			return Errorf(CodeBadParams, "thinking is cheap, strong, or nothing")
		}
		if _, duplicate := thinking[name]; duplicate {
			return Errorf(CodeBadParams, "%s is named twice", name)
		}
		thinking[name] = word
	}
	ed.Thinking = nilIfEmpty(thinking)
	for i := range ed.Add {
		if err := checkNewStep(&ed.Add[i]); err != nil {
			return err
		}
	}
	if len(ed.Add) == 0 && len(ed.Ask) == 0 && len(ed.Thinking) == 0 && len(ed.On) == 0 && len(ed.Skip) == 0 {
		return Errorf(CodeBadParams, "the edit changes nothing: name a step to add, reword, set thinking for, switch on or skip")
	}
	return nil
}

func nilIfEmpty(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}

func checkNewStep(a *FactoryStepAddition) error {
	var err error
	st := &a.Step
	if a.After != "" {
		if a.After, err = stepRef("after", a.After); err != nil {
			return err
		}
	}
	st.Kind = strings.TrimSpace(st.Kind)
	if st.Name = strings.TrimSpace(st.Name); st.Name != "" {
		if _, err = stepRef("name", st.Name); err != nil {
			return err
		}
		if st.Kind != string(factory.StageGate) {
			if _, err = factory.StageWord(st.Name); err != nil {
				return Errorf(CodeBadParams, "%s", err.Error())
			}
		}
	}
	switch st.Kind = strings.TrimSpace(st.Kind); st.Kind {
	case "", string(factory.StageChat), string(factory.StageGate):
	default:
		return Errorf(CodeBadParams, "a step added to a job is a conversation (chat) or an approval (gate)")
	}
	if st.Ask, err = stepWords("ask", st.Ask, factory.AskMost); err != nil {
		return err
	}
	if st.Until, err = stepWords("until", st.Until, MaxFactoryJobWords); err != nil {
		return err
	}
	switch st.Effort = strings.ToLower(strings.TrimSpace(st.Effort)); st.Effort {
	case "", factory.EffortWords[0], factory.EffortWords[1]:
	default:
		return Errorf(CodeBadParams, "thinking is cheap, strong, or nothing")
	}
	if st.Name == "" && st.Ask == "" && st.Kind != string(factory.StageGate) {
		return Errorf(CodeBadParams, "an added step needs a name or an ask")
	}
	return nil
}
