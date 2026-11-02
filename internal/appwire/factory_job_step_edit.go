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
// no more: reword a step's ask, set its thinking, switch steps on or off, add a
// step with only a name, kind, ask, until and effort (and a check its max),
// optionally after another step, remove a step from this job, and move a step
// after another (or first). It cannot change a step's kind or its max, when,
// fanout or proof, and cannot name a path. A conversation step's tries
// (1 to 3) are in the language only where the engine announces
// [FeatureFactoryJobStepEditMax], and a step's model only where it announces
// [FeatureFactoryJobStepEditModel]. A part the language lacks is
// `bad_params` (a field this wire does not carry is refused, not ignored).
// Which steps may be removed or moved (never proof, a fixed step, a step a
// policy names or one that has begun; an approval only by its person) is the
// factory's to say, as for every other part.
//
// AN EDIT CAN REQUEUE A RUNNING JOB. When the factory applies an edit whose
// `lines` are not empty while a worker holds the job, the running step goes
// back to the queue and that worker is cut off. The app says so before it sends.

// FeatureFactoryJobStepEdit is announced in hello when the engine serves
// factory.job_step_edits_read and factory.job_step_edit.
const FeatureFactoryJobStepEdit = "factory.job_step_edit"

// FeatureFactoryJobStepEditChecks is announced beside FeatureFactoryJobStepEdit
// when this engine carries a check (kind check, with a max) in an added step.
// The factory still decides: it takes one only from the job's person.
const FeatureFactoryJobStepEditChecks = "factory.job_step_edit_checks"

// FeatureFactoryJobStepEditStructure is announced beside FeatureFactoryJobStepEdit
// when this engine carries `remove` and `move`. An app sends neither to an
// engine that does not announce it: an older engine refuses them as unknown
// fields, and the factory behind it would too.
const FeatureFactoryJobStepEditStructure = "factory.job_step_edit_structure"

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
	// Kind is empty or chat (a conversation), gate (a step the person
	// approves) or check (Ask is a shell command run in the job's checkout;
	// its exit code is the evidence). Any other kind is refused by the factory,
	// which also refuses a check from anyone but the job's person.
	Kind   string `json:"kind,omitempty" enum:"chat,gate,check"`
	Ask    string `json:"ask,omitempty"`
	Until  string `json:"until,omitempty"`
	Effort string `json:"effort,omitempty" enum:"cheap,strong"`
	// Max is a check's rounds, 1 to 9; any other step takes none.
	Max int `json:"max,omitempty"`
}

// FactoryStepAddition is one step to add, optionally after a named step.
type FactoryStepAddition struct {
	Step  FactoryNewStep `json:"step"`
	After string         `json:"after,omitempty"`
}

// FactoryStepMove is one step to move. After is required: the step it now goes
// after, or "" for first. A move with no `after` is refused, never read as first.
type FactoryStepMove struct {
	Step  string  `json:"step"`
	After *string `json:"after"`
}

// FactoryStepEdit is the whole edit language of one request. Every name is a
// step of the job as the person read it.
type FactoryStepEdit struct {
	Add []FactoryStepAddition `json:"add,omitempty"`
	// Ask maps a step name to its new ask.
	Ask map[string]string `json:"ask,omitempty"`
	// Thinking maps a step name to cheap or strong; "" returns it to the usual.
	Thinking map[string]string `json:"thinking,omitempty"`
	// Max maps a conversation step's name to how many tries it gets for this
	// job, 1 to 3. Sent only to an engine that announces
	// [FeatureFactoryJobStepEditMax].
	Max map[string]int `json:"max,omitempty"`
	// Model maps a step name to the model it runs on for this job, one of
	// models.list's ids; "" gives it back to the job's model. Sent only to an
	// engine that announces [FeatureFactoryJobStepEditModel].
	Model map[string]string `json:"model,omitempty"`
	On    []string          `json:"on,omitempty"`
	Skip  []string          `json:"skip,omitempty"`
	// Remove takes steps out of this job; skip leaves them in, off.
	Remove []string          `json:"remove,omitempty"`
	Move   []FactoryStepMove `json:"move,omitempty"`
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
		hello = withFeature(hello, FeatureFactoryJobStepEditChecks)
		hello = withFeature(hello, FeatureFactoryJobStepEditStructure)
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
	if len(ed.Add) > factory.StageMost || len(ed.Ask) > factory.StageMost || len(ed.Thinking) > factory.StageMost || len(ed.Max) > factory.StageMost || len(ed.Model) > factory.StageMost {
		return Errorf(CodeBadParams, "an edit changes at most %d steps in one part", factory.StageMost)
	}
	if ed.On, err = stepRefs("on", ed.On); err != nil {
		return err
	}
	if ed.Skip, err = stepRefs("skip", ed.Skip); err != nil {
		return err
	}
	if ed.Remove, err = stepRefs("remove", ed.Remove); err != nil {
		return err
	}
	if len(ed.Move) > factory.StageMost {
		return Errorf(CodeBadParams, "move names at most %d steps", factory.StageMost)
	}
	for i := range ed.Move {
		mv := &ed.Move[i]
		if mv.Step, err = stepRef("move", mv.Step); err != nil {
			return err
		}
		if mv.After == nil {
			return Errorf(CodeBadParams, "a moved step names the step it goes after, or \"\" for first")
		}
		after := strings.TrimSpace(*mv.After)
		if after != "" {
			if after, err = stepRef("after", after); err != nil {
				return err
			}
		}
		mv.After = &after
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
	if ed.Max, err = stepTries(ed.Max); err != nil {
		return err
	}
	if ed.Model, err = stepModels(ed.Model); err != nil {
		return err
	}
	for i := range ed.Add {
		if err := checkNewStep(&ed.Add[i]); err != nil {
			return err
		}
	}
	if len(ed.Add) == 0 && len(ed.Ask) == 0 && len(ed.Thinking) == 0 && len(ed.Max) == 0 && len(ed.Model) == 0 && len(ed.On) == 0 && len(ed.Skip) == 0 && len(ed.Remove) == 0 && len(ed.Move) == 0 {
		return Errorf(CodeBadParams, "the edit changes nothing: name a step to add, reword, set thinking for, switch on, skip, remove or move")
	}
	return nil
}

// stepTries trims each named step and refuses a count outside 1 to
// [factory.StepTriesMost]. Which steps may take one is the factory's to say.
func stepTries(in map[string]int) (map[string]int, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]int, len(in))
	for name, tries := range in {
		name, err := stepRef("max", name)
		if err != nil {
			return nil, err
		}
		if tries < 1 || tries > factory.StepTriesMost {
			return nil, Errorf(CodeBadParams, "%s", factory.ErrStepTries.Error())
		}
		if _, duplicate := out[name]; duplicate {
			return nil, Errorf(CodeBadParams, "%s is named twice", name)
		}
		out[name] = tries
	}
	return out, nil
}

// stepModels trims each named step and its model id. Which models exist is the
// engine's model list's to say (the engine checks it before it sends); here a
// model is only refused for not being shaped like one id.
func stepModels(in map[string]string) (map[string]string, error) {
	out := make(map[string]string, len(in))
	for name, model := range in {
		name, err := stepRef("model", name)
		if err != nil {
			return nil, err
		}
		model = strings.TrimSpace(model)
		if !factory.ModelID(model) {
			return nil, Errorf(CodeBadParams, "%s", factory.ErrModelID.Error())
		}
		if _, duplicate := out[name]; duplicate {
			return nil, Errorf(CodeBadParams, "%s is named twice", name)
		}
		out[name] = model
	}
	return nilIfEmpty(out), nil
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
	case "", string(factory.StageChat), string(factory.StageGate), string(factory.StageCheck):
	default:
		return Errorf(CodeBadParams, "a step added to a job is a conversation (chat), an approval (gate) or a check")
	}
	if st.Kind != string(factory.StageCheck) && st.Max != 0 {
		return Errorf(CodeBadParams, "only a check says its rounds")
	}
	if st.Max < 0 || st.Max > factory.CheckRoundsMost {
		return Errorf(CodeBadParams, "a check runs from 1 to %d rounds", factory.CheckRoundsMost)
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
