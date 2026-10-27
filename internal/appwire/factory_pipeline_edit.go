package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// ── EDITING A REPOSITORY'S PIPELINE FILE: READ IT, PREVIEW AN EDIT ──────────
//
// Feature factory.pipeline_edit announces two methods that come before
// factory.pipeline_pr and WRITE NOTHING:
//
//   - factory.pipeline_source reads the repository's raw .codeaf/factory.yaml as
//     the DEFAULT BRANCH holds it, pinned to one commit, with the caller's own
//     GitHub credential. It is the only honest base for an edit: never the host's
//     checkout, never the merged read factory.pipeline gives, never the personal
//     file. A file that is missing, does not read cleanly or is too large says so
//     and refuses editing; defaults are never offered as though they were the file.
//   - factory.pipeline_preview applies STRUCTURED edits to that base and answers
//     the exact canonical text the pull request would commit. The edits are
//     set_step {name, prompt?, until?, max?, when?, effort?, fanout?},
//     set_budget {value} and, behind feature factory.pipeline_edit_structure,
//     add_step {pipeline, name, after, prompt, until?, max?, when?, effort?,
//     fanout?}, remove_step {pipeline, name} and move_step {pipeline, name,
//     after}; nothing else (never kind: a step becomes a check, gate or post only
//     in the file itself). They are the only edits the file format can hold
//     today. A caller never builds YAML, and the preview is computed by the same
//     parser and encoder the pull request route uses, so what is shown is what is
//     committed, comments dropped and layout rewritten as that encoder does.
//
// Behind feature factory.pipeline_edit_rules the two methods also edit the
// file's OWN lists and a file that is not there yet:
//
//   - the source carries rules, ask_first and never, the lines the repository's
//     file itself holds (never the factory's house rules), for the states file
//     and missing;
//   - a missing file is editable: the source describes the shipped default
//     pipeline the repository runs without one (text "", nothing in_file), and
//     a preview with base_text "" applies the edits to those defaults and
//     answers the whole file the pull request creates;
//   - set_list {list, lines} replaces one of rules, ask_first and never whole,
//     and the preview answers lists, every list whose lines changed.
//
// An engine that does not announce the word answers exactly as before: no
// lists, a missing file not editable, and set_list or an empty base_text
// refused as bad_params.
//
// NEITHER METHOD OPENS A PULL REQUEST, CREATES A BRANCH OR WRITES A FILE. The
// next step is factory.pipeline_pr with the preview's text and the source's
// base_sha as expected_base_sha.
//
// A preview that changes nothing says so (changed false) and answers the base
// text byte for byte: reformatting alone is not a change and offers nothing to
// write.

// FeatureFactoryPipelineEdit is announced in hello when the engine serves
// factory.pipeline_source and factory.pipeline_preview.
const FeatureFactoryPipelineEdit = "factory.pipeline_edit"

// FeatureFactoryPipelineEditRuntime is announced beside FeatureFactoryPipelineEdit
// when this engine decodes when, effort and fanout in a preview AND refuses to
// preview them for a factory that did not attest them. It says nothing about any
// remote: that is the source's runtime_fields, read per file.
const FeatureFactoryPipelineEditRuntime = "factory.pipeline_edit_runtime"

// FeatureFactoryPipelineEditStructure is announced beside FeatureFactoryPipelineEdit
// when this engine previews add_step, remove_step and move_step. It says nothing
// about any remote: the three edits write only pipeline lists and step
// definitions, keys every YAML-reading factory already parses. An editor that
// does not announce it refuses the three verbs as bad_params, as before they
// existed.
const FeatureFactoryPipelineEditStructure = "factory.pipeline_edit_structure"

// FeatureFactoryPipelineEditRules is announced beside FeatureFactoryPipelineEdit
// when this engine carries the file's own rules, ask_first and never on the
// source, previews set_list, and edits a missing file from the shipped
// defaults (an editable missing source, and a preview of base_text ""). It says
// nothing about any remote: rules and start were keys every YAML-reading
// factory parsed before this, and the pull request route already creates a
// file the default branch does not have.
const FeatureFactoryPipelineEditRules = "factory.pipeline_edit_rules"

const (
	// factoryPipelineSourceBudget covers the three GitHub reads of the route,
	// whose own limit is sixty seconds.
	factoryPipelineSourceBudget = 75 * time.Second

	// PipelinePreviewTextMax is the most base text a preview takes: the pull
	// request route's own cap on the file.
	PipelinePreviewTextMax = pipeline.EditTextMax
	// PipelinePreviewEditsMax is the most edits one preview takes.
	PipelinePreviewEditsMax = pipeline.EditOpsMax
)

// The states of a read file, and the verbs of an edit.
const (
	PipelineSourceFile     = api.PipelineEditFile
	PipelineSourceMissing  = api.PipelineEditMissing
	PipelineSourceInvalid  = api.PipelineEditInvalid
	PipelineSourceTooLarge = api.PipelineEditTooLarge

	PipelineEditSetStep    = pipeline.OpSetStep
	PipelineEditSetBudget  = pipeline.OpSetBudget
	PipelineEditAddStep    = pipeline.OpAddStep
	PipelineEditRemoveStep = pipeline.OpRemoveStep
	PipelineEditMoveStep   = pipeline.OpMoveStep
	PipelineEditSetList    = pipeline.OpSetList

	PipelineListRules    = pipeline.ListRules
	PipelineListAskFirst = pipeline.ListAskFirst
	PipelineListNever    = pipeline.ListNever
)

// FactoryPipelineSourceParams names the repository whose file is read.
type FactoryPipelineSourceParams struct {
	Factory string `json:"factory"`
	Repo    string `json:"repo"`
}

// FactoryPipelineSource is the pinned file. It repeats the connection and the
// repository that were asked for. BaseSHA is the default-branch commit the file
// was read at, to be passed back to the preview and to factory.pipeline_pr.
// Text is the raw file for the states file and invalid. Editable is true for
// state file, and only then is the rest, the parse of Text, present.
//
// Behind feature factory.pipeline_edit_rules, Editable is true for state missing
// too: Text is "", the rest describes the shipped default pipeline the
// repository runs without a file (budget_in_file false, every pipeline and step
// in_file false), and the preview takes base_text "" for it. Rules, AskFirst
// and Never are then present, always arrays (`[]` when empty), for the states
// file and missing: the lines the repository's file itself holds under rules,
// start.ask_first and start.never, never the factory's house rules, and empty
// for a missing file. Without the feature they are absent.
type FactoryPipelineSource struct {
	Factory       string `json:"factory"`
	Repo          string `json:"repo"`
	Path          string `json:"path"`
	DefaultBranch string `json:"default_branch"`
	BaseSHA       string `json:"base_sha"`
	CanPush       bool   `json:"can_push"`
	Archived      bool   `json:"archived"` // always present; an archived repository refuses a pull request whatever can_push says
	// RuntimeFields is true only when the factory itself said so in this read:
	// its parser reads a step's kind, when, effort and fanout and its runner
	// obeys them. Always present. The steps carry those words only then.
	RuntimeFields bool     `json:"runtime_fields"`
	State         string   `json:"state" enum:"file,missing,invalid,too_large"`
	Editable      bool     `json:"editable"`
	Text          string   `json:"text,omitempty"`
	Problems      []string `json:"problems,omitempty"`

	DefaultBudget float64                `json:"default_budget,omitempty"`
	BudgetInFile  bool                   `json:"budget_in_file,omitempty"`
	Pipelines     []api.PipelineEditKind `json:"pipelines,omitempty"`
	Steps         []api.PipelineEditStep `json:"steps,omitempty"`
	Fixed         []string               `json:"fixed,omitempty"`
	Rules         *[]string              `json:"rules,omitempty"`
	AskFirst      *[]string              `json:"ask_first,omitempty"`
	Never         *[]string              `json:"never,omitempty"`
}

// FactoryPipelineEdit is one structured edit. set_step names a step and at
// least one of prompt, until, max, when, effort and fanout; set_budget names
// only value. A field the verb does not take is bad_params, and there is no kind
// field at all: it is rejected as unknown. An omitted field is not sent: until ""
// is a request to clear it, which the file format may refuse, and when, effort or
// fanout "" clears that word back to the default.
//
// Pipeline and After belong to add_step, remove_step and move_step alone, and
// are bad_params on the other two. Pipeline is the kind whose list changes
// (issue, pr, ci or another kind the file lists). After is the step the named
// one goes after in that list, "" for first; add_step and move_step require it,
// so a position is always said and never assumed. add_step also requires a
// prompt; there is no kind on it either, so an added step is a chat.
//
// List and Lines belong to set_list alone (feature factory.pipeline_edit_rules),
// and are bad_params on every other verb. List is rules, ask_first or never;
// Lines is required and is every line the list holds after the edit, in order,
// [] to clear it. Each line is trimmed, one line of at most 300 characters, and
// is in the list once (ignoring case for rules and ask_first, exactly for
// never); a list holds at most 40 lines and is set once per preview.
type FactoryPipelineEdit struct {
	Op       string   `json:"op" enum:"set_step,set_budget,add_step,remove_step,move_step,set_list"`
	Name     string   `json:"name,omitempty"`
	Pipeline string   `json:"pipeline,omitempty"`
	After    *string  `json:"after,omitempty"`
	Prompt   *string  `json:"prompt,omitempty"`
	Until    *string  `json:"until,omitempty"`
	Max      *int     `json:"max,omitempty"`
	When     *string  `json:"when,omitempty"`
	Effort   *string  `json:"effort,omitempty"`
	Fanout   *string  `json:"fanout,omitempty"`
	Value    *float64 `json:"value,omitempty"`
	List     string   `json:"list,omitempty" enum:"rules,ask_first,never"`
	Lines    []string `json:"lines,omitempty"`
}

// FactoryPipelinePreviewParams is the file as factory.pipeline_source gave it
// (BaseText, BaseSHA) and the edits to apply. The engine reads nothing: the
// preview is a function of these params. BaseText is required; "" is a
// repository with no file, and is taken only behind feature
// factory.pipeline_edit_rules.
type FactoryPipelinePreviewParams struct {
	Factory  string                `json:"factory"`
	Repo     string                `json:"repo"`
	BaseSHA  string                `json:"base_sha"`
	BaseText string                `json:"base_text"`
	Edits    []FactoryPipelineEdit `json:"edits"`
}

// FactoryPipelineStepValues are what a step says. When, effort and fanout are
// omitted when empty, so a preview of a legacy file is byte for byte what it was
// before they existed.
type FactoryPipelineStepValues struct {
	Prompt string `json:"prompt"`
	Until  string `json:"until"`
	Max    int    `json:"max"`
	When   string `json:"when,omitempty"`
	Effort string `json:"effort,omitempty"`
	Fanout string `json:"fanout,omitempty"`
}

// FactoryPipelineStepChange is one step an edit really changed; Fields says
// which of prompt, until, max, when, effort and fanout differ, never kind.
type FactoryPipelineStepChange struct {
	Name   string                    `json:"name"`
	Fields []string                  `json:"fields"`
	Before FactoryPipelineStepValues `json:"before"`
	After  FactoryPipelineStepValues `json:"after"`
}

// FactoryPipelineBudgetChange is the default budget before and after.
type FactoryPipelineBudgetChange struct {
	Before float64 `json:"before"`
	After  float64 `json:"after"`
}

// FactoryPipelineOrderChange is one pipeline whose list changed: every step it
// listed before the edit and every step it lists after, in order.
type FactoryPipelineOrderChange struct {
	Pipeline string   `json:"pipeline"`
	Before   []string `json:"before"`
	After    []string `json:"after"`
}

// FactoryPipelineCount is how many steps one pipeline lists after the edit.
type FactoryPipelineCount struct {
	Pipeline string `json:"pipeline"`
	Steps    int    `json:"steps"`
}

// FactoryPipelineGateChange is one person (approve, or a step whose kind is
// gate) whose run-up the edit changed: Before and After are the steps that run
// before that person answers, gates excluded, in order. It is reported whenever
// that SET of steps changes, which is what runs unattended before the person;
// the edit is not refused for it.
type FactoryPipelineGateChange struct {
	Pipeline string   `json:"pipeline"`
	Gate     string   `json:"gate"`
	Before   []string `json:"before"`
	After    []string `json:"after"`
}

// FactoryPipelineStructure is the structural half of a preview, present only
// when an edit was add_step, remove_step or move_step. Order is every pipeline
// whose list changed, Added every step definition the file did not hold before,
// Counts every pipeline's length after the edit, Gates every person whose
// run-up changed. All four are always arrays, empty when nothing applies. There
// is no loop count: the file has no loop.
type FactoryPipelineStructure struct {
	Order  []FactoryPipelineOrderChange `json:"order"`
	Added  []string                     `json:"added"`
	Counts []FactoryPipelineCount       `json:"counts"`
	Gates  []FactoryPipelineGateChange  `json:"gates"`
}

// FactoryPipelineListChange is one list whose lines the edit changed: every
// line it held before and every line it holds after, in order.
type FactoryPipelineListChange struct {
	List   string   `json:"list" enum:"rules,ask_first,never"`
	Before []string `json:"before"`
	After  []string `json:"after"`
}

// FactoryPipelinePreview is the exact outcome of an edit, without writing it.
//
// Changed compares the edit with the base as the parser reads it, so a file that
// is only laid out differently from the encoder's is NOT changed, and Text is
// then the base, byte for byte: there is nothing to write. When Changed, Text is
// the canonical file the pull request commits. FormatRewritten says those bytes
// differ from the base beyond the edit (the factory's own layout, keys the file
// left out written in); CommentsDropped says the base holds a YAML comment that
// the committed file does not. Both are false when not Changed. Structure is
// absent unless an edit was structural, so a preview of field edits is byte for
// byte what it was before the structural edits existed.
//
// Lists is present whenever an edit was set_list, and absent otherwise: every
// list whose lines changed, in the order rules, ask_first, never, `[]` when
// none did. For base_text "" (no file yet) a changed Text is the whole file the
// pull request creates, and FormatRewritten and CommentsDropped are false: there
// was no layout and no comment to lose. An unchanged one is "", as the base was.
type FactoryPipelinePreview struct {
	Factory         string                       `json:"factory"`
	Repo            string                       `json:"repo"`
	BaseSHA         string                       `json:"base_sha"`
	Path            string                       `json:"path"`
	Text            string                       `json:"text"`
	Changed         bool                         `json:"changed"`
	FormatRewritten bool                         `json:"format_rewritten"`
	CommentsDropped bool                         `json:"comments_dropped"`
	Steps           []FactoryPipelineStepChange  `json:"steps"`
	Budget          *FactoryPipelineBudgetChange `json:"budget,omitempty"`
	Structure       *FactoryPipelineStructure    `json:"structure,omitempty"`
	Lists           *[]FactoryPipelineListChange `json:"lists,omitempty"`
}

// FactoryPipelineEditRefusal is the Detail of a preview refused as bad_params.
// Code says why, and Op is the position of the edit that caused it, absent when
// the base or the whole request did.
type FactoryPipelineEditRefusal struct {
	Factory string `json:"factory"`
	Repo    string `json:"repo"`
	Code    string `json:"code" enum:"base_invalid,bad_op,locked,unknown_step,bad_value,too_large,not_representable"`
	Op      *int   `json:"op,omitempty"`
}

// FactoryPipelineEditor is the engine's half of factory.pipeline_edit.
// FactoryPipelineSource makes at most three GETs through the factory and no
// write. FactoryPipelinePreview is pure: it asks only that the caller still
// holds the connection. An edit it refuses is an *Error with code bad_params
// whose Detail is a [FactoryPipelineEditRefusal].
type FactoryPipelineEditor interface {
	FactoryPipelineSource(ctx context.Context, p FactoryPipelineSourceParams) (FactoryPipelineSource, error)
	FactoryPipelinePreview(ctx context.Context, p FactoryPipelinePreviewParams) (FactoryPipelinePreview, error)
}

// FactoryPipelineRuntimeGuard is implemented ONLY by an editor that decodes when,
// effort and fanout AND refuses to preview them for a file its factory did not
// attest. The method set of [FactoryPipelineEditor] cannot say that, so an
// editor that lacks this marker never announces the runtime feature.
type FactoryPipelineRuntimeGuard interface {
	PipelineRuntimeGuarded()
}

// FactoryPipelineStructureEditor is implemented ONLY by an editor whose preview
// applies add_step, remove_step and move_step. Without it the three verbs are
// refused before the engine is asked, and the feature is not announced.
type FactoryPipelineStructureEditor interface {
	PipelineStructureEdits()
}

// FactoryPipelineRulesEditor is implemented ONLY by an editor whose source
// carries the file's own lists and describes a missing file from the shipped
// defaults, and whose preview applies set_list and takes an empty base.
// Without it set_list and base_text "" are refused before the engine is asked,
// the lists are not on a source, a missing file is not editable, and the
// feature is not announced.
type FactoryPipelineRulesEditor interface {
	PipelineRulesEdits()
}

func withFactoryPipelineEditFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryPipelineEditor); ok {
		hello = withFeature(hello, FeatureFactoryPipelineEdit)
		if _, guarded := e.(FactoryPipelineRuntimeGuard); guarded {
			hello = withFeature(hello, FeatureFactoryPipelineEditRuntime)
		}
		if _, structural := e.(FactoryPipelineStructureEditor); structural {
			hello = withFeature(hello, FeatureFactoryPipelineEditStructure)
		}
		if _, rules := e.(FactoryPipelineRulesEditor); rules {
			hello = withFeature(hello, FeatureFactoryPipelineEditRules)
		}
	}
	return hello
}

// PipelineEditOps converts wire edits to the pipeline package's, unchanged.
// Kind is left nil on purpose: a step's kind is a permission change that no
// preview shows, so the wire has no way to send one.
func PipelineEditOps(edits []FactoryPipelineEdit) []pipeline.EditOp {
	ops := make([]pipeline.EditOp, len(edits))
	for i, e := range edits {
		ops[i] = pipeline.EditOp{Op: e.Op, Name: e.Name, Pipeline: e.Pipeline, After: e.After, Prompt: e.Prompt, Until: e.Until, Max: e.Max,
			When: e.When, Effort: e.Effort, Fanout: e.Fanout, Value: e.Value, List: e.List, Lines: e.Lines}
	}
	return ops
}

// checkPipelineRepo trims the connection and repository as factory.pipeline_pr
// does.
func checkPipelineRepo(factory, repo string) (string, string, error) {
	id, err := factoryID(strings.TrimSpace(factory))
	if err != nil {
		return "", "", err
	}
	owner, name, ok := strings.Cut(strings.TrimSpace(repo), "/")
	if !ok || !repoOwnerPattern.MatchString(owner) || !repoNamePattern.MatchString(name) ||
		owner == "." || owner == ".." || name == "." || name == ".." {
		return "", "", Errorf(CodeBadParams, "repo must be owner/name")
	}
	return id, owner + "/" + name, nil
}

// CheckPipelineSource bounds the read without asking anyone.
func CheckPipelineSource(p FactoryPipelineSourceParams) (FactoryPipelineSourceParams, error) {
	var err error
	p.Factory, p.Repo, err = checkPipelineRepo(p.Factory, p.Repo)
	return p, err
}

// CheckPipelinePreview bounds the preview without asking anyone: the base, the
// pinned commit, and every edit on its own (verb, fields, name, value bounds).
// Whether a step exists or is locked depends on the base and is the engine's to
// say. BaseText and the edits' texts are forwarded exactly. A BaseText of ""
// (no file yet) passes here; whether the engine takes one is the method's to
// say ([checkRulesServed]).
func CheckPipelinePreview(p FactoryPipelinePreviewParams) (FactoryPipelinePreviewParams, error) {
	var err error
	if p.Factory, p.Repo, err = checkPipelineRepo(p.Factory, p.Repo); err != nil {
		return p, err
	}
	p.BaseSHA = strings.ToLower(strings.TrimSpace(p.BaseSHA))
	if !ValidPipelinePRCommit(p.BaseSHA) {
		return p, Errorf(CodeBadParams, "base_sha must be a 40-character commit SHA")
	}
	switch {
	case p.BaseText != "" && strings.TrimSpace(p.BaseText) == "":
		return p, Errorf(CodeBadParams, "base_text is the file as read, or \"\" when there is none")
	case len(p.BaseText) > PipelinePreviewTextMax:
		return p, Errorf(CodeBadParams, "base_text is at most %d bytes", PipelinePreviewTextMax)
	case !utf8.ValidString(p.BaseText):
		return p, Errorf(CodeBadParams, "base_text must be valid UTF-8")
	}
	if len(p.Edits) == 0 || len(p.Edits) > PipelinePreviewEditsMax {
		return p, Errorf(CodeBadParams, "edits is from 1 to %d edits", PipelinePreviewEditsMax)
	}
	for i, op := range PipelineEditOps(p.Edits) {
		if _, bad := pipeline.CheckEdit(i, op); bad != nil {
			return p, Errorf(CodeBadParams, "edit %d: %s", i, bad.Why)
		}
	}
	return p, nil
}

func init() {
	methods["factory.pipeline_source"] = pipelineSourceMethod("factory.pipeline_source")
	methods["factory.pipeline_preview"] = pipelinePreviewMethod("factory.pipeline_preview")
	for _, name := range []string{"factory.pipeline_source", "factory.pipeline_preview"} {
		factoryPipelineEditMethods[name] = methods[name]
	}
}

func pipelineEditor(e Engine, name string) (FactoryPipelineEditor, error) {
	ed, ok := e.(FactoryPipelineEditor)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return ed, nil
}

func pipelineSourceMethod(name string) method {
	return shaped[FactoryPipelineSourceParams, FactoryPipelineSource](func(e Engine, raw json.RawMessage) (*FactoryPipelineSource, error) {
		ed, err := pipelineEditor(e, name)
		if err != nil {
			return nil, err
		}
		if !json.Valid(raw) {
			return nil, Errorf(CodeBadParams, "%s: the params are not one JSON value", name)
		}
		var p FactoryPipelineSourceParams
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		if p, err = CheckPipelineSource(p); err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), factoryPipelineSourceBudget)
		defer cancel()
		out, err := ed.FactoryPipelineSource(ctx, p)
		if err != nil {
			return nil, err
		}
		out = shapeRulesSource(e, out)
		return &out, nil
	})
}

func pipelinePreviewMethod(name string) method {
	return shaped[FactoryPipelinePreviewParams, FactoryPipelinePreview](func(e Engine, raw json.RawMessage) (*FactoryPipelinePreview, error) {
		ed, err := pipelineEditor(e, name)
		if err != nil {
			return nil, err
		}
		if !json.Valid(raw) {
			return nil, Errorf(CodeBadParams, "%s: the params are not one JSON value", name)
		}
		var p FactoryPipelinePreviewParams
		if err := decodeStrict(name, raw, &p); err != nil {
			return nil, err
		}
		// base_text "" is a request about a repository with no file, so it
		// has to be said: an omitted base is not that request.
		var keys map[string]json.RawMessage
		if json.Unmarshal(raw, &keys) != nil || keys["base_text"] == nil || string(keys["base_text"]) == "null" {
			return nil, Errorf(CodeBadParams, "base_text is required")
		}
		if p, err = CheckPipelinePreview(p); err != nil {
			return nil, err
		}
		if err := checkStructureServed(e, p.Edits); err != nil {
			return nil, err
		}
		if err := checkRulesServed(e, p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := ed.FactoryPipelinePreview(ctx, p)
		if err != nil {
			return nil, err
		}
		if out.Steps == nil {
			out.Steps = []FactoryPipelineStepChange{}
		}
		// lists is on the answer exactly when a set_list was asked for, so an
		// older preview answers byte for byte what it always did.
		switch {
		case !anySetList(p.Edits):
			out.Lists = nil
		case out.Lists == nil || *out.Lists == nil:
			out.Lists = &[]FactoryPipelineListChange{}
		}
		return &out, nil
	})
}

// checkStructureServed refuses a structural edit to an editor that has not said
// it previews one, the same bad_params an unknown verb always was.
func checkStructureServed(e Engine, edits []FactoryPipelineEdit) error {
	if _, ok := e.(FactoryPipelineStructureEditor); ok {
		return nil
	}
	for i, edit := range edits {
		switch edit.Op {
		case PipelineEditAddStep, PipelineEditRemoveStep, PipelineEditMoveStep:
			return Errorf(CodeBadParams, "edit %d: this engine does not add, remove or move a step", i)
		}
	}
	return nil
}

// checkRulesServed refuses a set_list, or a base of "" (no file yet), to an
// editor that has not said it previews one: the same bad_params an unknown verb
// and an empty base always were.
func checkRulesServed(e Engine, p FactoryPipelinePreviewParams) error {
	if _, ok := e.(FactoryPipelineRulesEditor); ok {
		return nil
	}
	if p.BaseText == "" {
		return Errorf(CodeBadParams, "base_text is required")
	}
	for i, edit := range p.Edits {
		if edit.Op == PipelineEditSetList {
			return Errorf(CodeBadParams, "edit %d: this engine does not set a list", i)
		}
	}
	return nil
}

func anySetList(edits []FactoryPipelineEdit) bool {
	for _, edit := range edits {
		if edit.Op == PipelineEditSetList {
			return true
		}
	}
	return false
}

// shapeRulesSource holds a source to its feature. Without the rules feature it
// carries no lists and a missing file is not editable, as before the feature;
// with it, a file or a missing source always carries all three lists, `[]`
// when empty, and no other state carries any.
func shapeRulesSource(e Engine, out FactoryPipelineSource) FactoryPipelineSource {
	_, rules := e.(FactoryPipelineRulesEditor)
	if !rules || (out.State != PipelineSourceFile && out.State != PipelineSourceMissing) {
		out.Rules, out.AskFirst, out.Never = nil, nil, nil
		if !rules && out.State == PipelineSourceMissing && out.Editable {
			out.Editable = false
			out.DefaultBudget, out.BudgetInFile, out.Pipelines, out.Steps, out.Fixed = 0, false, nil, nil, nil
		}
		return out
	}
	for _, list := range []**[]string{&out.Rules, &out.AskFirst, &out.Never} {
		if *list == nil || **list == nil {
			*list = &[]string{}
		}
	}
	return out
}

// factoryPipelineEditMethods is the table of the two methods, for the tests
// that walk it.
var factoryPipelineEditMethods = map[string]method{}
