package appwire

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// ── THE FACTORY'S PIPELINE LIBRARY ──────────────────────────────────────────
//
// factory.library reads, factory.library_set replaces and
// factory.library_preview edits (without writing) the factory's pipeline
// library: named pipelines and the step definitions they use, kept by THE
// FACTORY for every repository it works on, not by one Mac and not in any
// repository (internal/factory/pipeline, library.go). Its text is the same
// YAML as a repository's .codeaf/factory.yaml, `pipeline:` and `steps:` and
// nothing else, read by pipeline.Parse and written by pipeline.Encode.
//
// A job's pipeline name resolves in one order, whichever door names it
// (factory.job_start {pipeline}, a routing rule's then.pipeline, the job's own
// kind): the repository's file as the factory runs it (its own copy when it
// keeps one, else main's), then the library, then the built-in steps. A
// repository pipeline of the same name overrides the library's. factory.pipeline
// lists every choice with where it came from (FactoryPipeline.Pipeline.Pipelines).
//
// factory.library_set PARAMS ARE {factory, text, expect}. text is required
// ("" for an empty library) and is checked here as the factory checks it;
// expect.version, from 1, is the version of the factory.library read the
// person made, and a library written since is [CodeStale] and nothing is
// written. ONLY THE OPERATOR MAY WRITE IT, because a library pipeline runs on
// every repository whose jobs pick it; everyone signed in may read it, and
// can.change says whether this caller may write. A WRITE IS SENT ONCE; an
// outcome the engine cannot confirm is [CodeUncertain].
//
// factory.library_preview {factory, base_text, edits} applies the same
// structured edits as factory.pipeline_preview to a library ("" is an empty
// one) and answers the exact text factory.library_set would keep. It takes
// only the verbs that touch `pipeline:` and `steps:` (pipeline.LibraryOp) and
// reads nothing.

// FeatureFactoryLibrary is announced in hello when the engine serves
// factory.library, factory.library_set and factory.library_preview, and
// factory.pipeline lists every pipeline a job can run.
const FeatureFactoryLibrary = "factory.library"

// FactoryLibraryChoice is one pipeline: its name, its steps in order, where it
// came from (repo, factory_copy, library or built_in) and every lower layer it
// overrides.
type FactoryLibraryChoice = api.PipelineChoice

// FactoryLibrary is the library as the factory keeps it: Text ("" when
// empty), Version (from 1, the expect.version a write names), every pipeline
// it names, whether the caller may change it, and who wrote it last and when.
type FactoryLibrary struct {
	Factory   string                 `json:"factory"`
	Text      string                 `json:"text"`
	Version   int                    `json:"version"`
	Pipelines []FactoryLibraryChoice `json:"pipelines"`
	Can       FactoryLibraryCan      `json:"can"`
	factory.RulesChange
}

// FactoryLibraryCan is what the caller may do: Change is factory.library_set.
type FactoryLibraryCan struct {
	Change bool `json:"change"`
}

// FactoryLibrarySetParams replaces the library. Text is a pointer so that an
// omitted text is refused instead of read as an empty library.
type FactoryLibrarySetParams struct {
	Factory string                     `json:"factory"`
	Text    *string                    `json:"text"`
	Expect  *FactoryPipelineCopyExpect `json:"expect"`
}

// FactoryLibraryPreviewParams is the library as read (BaseText, required, ""
// for an empty one) and the edits to apply.
type FactoryLibraryPreviewParams struct {
	Factory  string                `json:"factory"`
	BaseText *string               `json:"base_text"`
	Edits    []FactoryPipelineEdit `json:"edits"`
}

// FactoryLibraryPreview is the exact library the edits make, without writing
// it: Text is what factory.library_set would keep ("" for an empty library,
// and the base byte for byte when nothing changed), Steps every step whose
// words changed and Structure, present when an edit was structural, the
// pipelines' order and every pipeline made, renamed or removed.
type FactoryLibraryPreview struct {
	Factory   string                      `json:"factory"`
	Text      string                      `json:"text"`
	Changed   bool                        `json:"changed"`
	Steps     []FactoryPipelineStepChange `json:"steps"`
	Structure *FactoryPipelineStructure   `json:"structure,omitempty"`
}

// FactoryLibraryKeeper is the engine's half of the three methods. The read
// and the write each make one request to the factory and never send it twice;
// the preview makes none.
type FactoryLibraryKeeper interface {
	FactoryLibrary(ctx context.Context, id string) (FactoryLibrary, error)
	FactoryLibrarySet(ctx context.Context, p FactoryLibrarySetParams) (FactoryLibrary, error)
	FactoryLibraryPreview(ctx context.Context, p FactoryLibraryPreviewParams) (FactoryLibraryPreview, error)
}

func withFactoryLibraryFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryLibraryKeeper); ok {
		hello = withFeature(hello, FeatureFactoryLibrary)
	}
	return hello
}

// CheckLibrarySet bounds a write without asking anyone, and answers it with
// text as the factory will keep it.
func CheckLibrarySet(p FactoryLibrarySetParams) (FactoryLibrarySetParams, error) {
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return p, err
	}
	if p.Text == nil {
		return p, Errorf(CodeBadParams, `text is required; send "" for an empty library`)
	}
	if err := checkCopyExpect(p.Expect); err != nil {
		return p, Errorf(CodeBadParams, "expect.version is required: send the version of the library the person read")
	}
	data, refused := pipeline.CheckLibrary(*p.Text)
	if refused != "" {
		return p, Errorf(CodeBadParams, "%s; nothing was saved", refused)
	}
	text := string(data)
	p.Text = &text
	return p, nil
}

// CheckLibraryPreview bounds a preview without asking anyone.
func CheckLibraryPreview(p FactoryLibraryPreviewParams) (FactoryLibraryPreviewParams, error) {
	var err error
	if p.Factory, err = factoryID(strings.TrimSpace(p.Factory)); err != nil {
		return p, err
	}
	switch {
	case p.BaseText == nil:
		return p, Errorf(CodeBadParams, `base_text is required; send "" for an empty library`)
	case len(*p.BaseText) > pipeline.LibraryTextMax:
		return p, Errorf(CodeBadParams, "base_text is at most %d bytes", pipeline.LibraryTextMax)
	case !utf8.ValidString(*p.BaseText):
		return p, Errorf(CodeBadParams, "base_text must be valid UTF-8")
	case len(p.Edits) == 0 || len(p.Edits) > PipelinePreviewEditsMax:
		return p, Errorf(CodeBadParams, "edits is from 1 to %d edits", PipelinePreviewEditsMax)
	}
	for i, op := range PipelineEditOps(p.Edits) {
		if !pipeline.LibraryOp(op.Op) {
			return p, Errorf(CodeBadParams, "edit %d: %s changes a repository's file; the library keeps pipelines and steps only", i, op.Op)
		}
		if _, bad := pipeline.CheckEdit(i, op); bad != nil {
			return p, Errorf(CodeBadParams, "edit %d: %s", i, bad.Why)
		}
	}
	return p, nil
}

// LibraryPreview is the preview itself, a pure function of its params: the
// engine's half calls it once the caller is known to hold the connection.
func LibraryPreview(p FactoryLibraryPreviewParams) (FactoryLibraryPreview, error) {
	ed, err := pipeline.LibraryEdited(*p.BaseText, PipelineEditOps(p.Edits))
	if err != nil {
		var bad *pipeline.EditError
		if !errors.As(err, &bad) {
			return FactoryLibraryPreview{}, Errorf(CodeEngine, "the edit could not be previewed")
		}
		return FactoryLibraryPreview{}, PipelineEditRefusal(p.Factory, "", bad)
	}
	out := FactoryLibraryPreview{Factory: p.Factory, Text: ed.Text, Changed: ed.Changed, Steps: []FactoryPipelineStepChange{}}
	for _, c := range ed.Steps {
		out.Steps = append(out.Steps, FactoryPipelineStepChange{Name: c.Name, Fields: c.Fields,
			Before: PipelineStepValues(c.Before), After: PipelineStepValues(c.After)})
	}
	out.Structure = PipelineStructureOf(ed)
	return out, nil
}

func init() {
	methods["factory.library"] = typed(func(e Engine, p FactoryRef) (*FactoryLibrary, error) {
		k, err := libraryKeeper(e, "factory.library")
		if err != nil {
			return nil, err
		}
		id, err := factoryID(p.Factory)
		if err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := k.FactoryLibrary(ctx, id)
		if err != nil {
			return nil, err
		}
		if out.Pipelines == nil {
			out.Pipelines = []FactoryLibraryChoice{}
		}
		return &out, nil
	})
	const set = "factory.library_set"
	methods[set] = shaped[FactoryLibrarySetParams, FactoryLibrary](func(e Engine, raw json.RawMessage) (*FactoryLibrary, error) {
		k, err := libraryKeeper(e, set)
		if err != nil {
			return nil, err
		}
		var p FactoryLibrarySetParams
		if err := decodeStrict(set, raw, &p); err != nil {
			return nil, err
		}
		if p, err = CheckLibrarySet(p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := k.FactoryLibrarySet(ctx, p)
		if err != nil {
			return nil, err
		}
		if out.Pipelines == nil {
			out.Pipelines = []FactoryLibraryChoice{}
		}
		return &out, nil
	})
	const preview = "factory.library_preview"
	methods[preview] = shaped[FactoryLibraryPreviewParams, FactoryLibraryPreview](func(e Engine, raw json.RawMessage) (*FactoryLibraryPreview, error) {
		k, err := libraryKeeper(e, preview)
		if err != nil {
			return nil, err
		}
		var p FactoryLibraryPreviewParams
		if err := decodeStrict(preview, raw, &p); err != nil {
			return nil, err
		}
		if p, err = CheckLibraryPreview(p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := k.FactoryLibraryPreview(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}

func libraryKeeper(e Engine, name string) (FactoryLibraryKeeper, error) {
	k, ok := e.(FactoryLibraryKeeper)
	if _, isFactory := e.(Factory); !ok || !isFactory {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return k, nil
}
