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
//     set_step {name, prompt?, until?, max?} and set_budget {value}, nothing
//     else; they are the only edits the file format can hold today. A caller
//     never builds YAML, and the preview is computed by the same parser and
//     encoder the pull request route uses, so what is shown is what is committed.
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

	PipelineEditSetStep   = pipeline.OpSetStep
	PipelineEditSetBudget = pipeline.OpSetBudget
)

// FactoryPipelineSourceParams names the repository whose file is read.
type FactoryPipelineSourceParams struct {
	Factory string `json:"factory"`
	Repo    string `json:"repo"`
}

// FactoryPipelineSource is the pinned file. It repeats the connection and the
// repository that were asked for. BaseSHA is the default-branch commit the file
// was read at, to be passed back to the preview and to factory.pipeline_pr.
// Text is the raw file for the states file and invalid. Editable is true only
// for state file, and only then is the rest, the parse of Text, present.
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
}

// FactoryPipelineEdit is one structured edit. set_step names a step and at
// least one of prompt, until and max; set_budget names only value. A field the
// verb does not take is bad_params. An omitted field is not sent: until "" is a
// request to clear it, which the file format may refuse.
type FactoryPipelineEdit struct {
	Op     string   `json:"op" enum:"set_step,set_budget"`
	Name   string   `json:"name,omitempty"`
	Prompt *string  `json:"prompt,omitempty"`
	Until  *string  `json:"until,omitempty"`
	Max    *int     `json:"max,omitempty"`
	Value  *float64 `json:"value,omitempty"`
}

// FactoryPipelinePreviewParams is the file as factory.pipeline_source gave it
// (BaseText, BaseSHA) and the edits to apply. The engine reads nothing: the
// preview is a function of these params.
type FactoryPipelinePreviewParams struct {
	Factory  string                `json:"factory"`
	Repo     string                `json:"repo"`
	BaseSHA  string                `json:"base_sha"`
	BaseText string                `json:"base_text"`
	Edits    []FactoryPipelineEdit `json:"edits"`
}

// FactoryPipelineStepValues are the three things a step says.
type FactoryPipelineStepValues struct {
	Prompt string `json:"prompt"`
	Until  string `json:"until"`
	Max    int    `json:"max"`
}

// FactoryPipelineStepChange is one step an edit really changed; Fields says
// which of prompt, until and max differ.
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

// FactoryPipelinePreview is the exact outcome of an edit, without writing it.
//
// Changed compares the edit with the base as the parser reads it, so a file that
// is only laid out differently from the encoder's is NOT changed, and Text is
// then the base, byte for byte: there is nothing to write. When Changed, Text is
// the canonical file the pull request commits. FormatRewritten says those bytes
// differ from the base beyond the edit (the factory's own layout, keys the file
// left out written in); CommentsDropped says the base holds a YAML comment that
// the committed file does not. Both are false when not Changed.
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

func withFactoryPipelineEditFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryPipelineEditor); ok {
		hello = withFeature(hello, FeatureFactoryPipelineEdit)
	}
	return hello
}

// PipelineEditOps converts wire edits to the pipeline package's, unchanged.
func PipelineEditOps(edits []FactoryPipelineEdit) []pipeline.EditOp {
	ops := make([]pipeline.EditOp, len(edits))
	for i, e := range edits {
		ops[i] = pipeline.EditOp{Op: e.Op, Name: e.Name, Prompt: e.Prompt, Until: e.Until, Max: e.Max, Value: e.Value}
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
// say. BaseText and the edits' texts are forwarded exactly.
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
	case strings.TrimSpace(p.BaseText) == "":
		return p, Errorf(CodeBadParams, "base_text is required")
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
		if p, err = CheckPipelinePreview(p); err != nil {
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
		return &out, nil
	})
}

// factoryPipelineEditMethods is the table of the two methods, for the tests
// that walk it.
var factoryPipelineEditMethods = map[string]method{}
