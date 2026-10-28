package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"time"
	"unicode/utf8"
)

// ── USING EDITED STEPS ON THIS FACTORY, WITHOUT A PULL REQUEST ──────────────
//
// factory.pipeline_copy_set saves, and factory.pipeline_copy_clear stops, a
// factory's OWN COPY of one repository's .codeaf/factory.yaml
// (factory.PipelineCopy): the very file a pull request of the same edit would
// write, kept in the factory's store, so the jobs THAT FACTORY starts
// afterwards run it at once. Nothing is pushed and no pull request is opened;
// factory.pipeline_pr stays the way to share the steps with the repository.
// The owner decided this on 2026-10-28 (factory.RecipeSource records who reads
// what, and the precedence: the factory's copy, else main's file, else the
// built-in steps). The copy clears itself once main's file says the same.
//
// THE PARAMS ARE {factory, repo, text, expect} and {factory, repo, expect}.
// expect.version is REQUIRED on both, from 1: the copy_version of the
// factory.pipeline_source read the person made (or the version of an earlier
// answer). A copy saved or stopped since is [CodeStale] and nothing is
// written. text is checked here as factory.pipeline_pr checks it, and the
// factory checks the whole file exactly as its pull request route does.
//
// ONLY THE FACTORY'S OPERATOR MAY SAVE OR STOP IT, because it changes what
// every job on the repository runs. The factory says who may on each
// factory.pipeline_source read (can.use_on_factory), and an app offers the
// action only then. A caller who is not is refused in words before anything
// is sent. On a solo factory the one person is its operator.
//
// A WRITE IS SENT ONCE, never retried. Only a refusal carrying the factory's
// phase marker proves nothing was written; any other failure is
// [CodeUncertain], and factory.pipeline_source says what the factory keeps.
//
// Behind the same feature, factory.pipeline_source says what the factory runs
// (FactoryPipelineSource.FactoryCopy, CopyVersion, Can).

// FeatureFactoryPipelineCopy is announced in hello when the engine serves
// factory.pipeline_copy_set and factory.pipeline_copy_clear, and reads the
// factory's copy on factory.pipeline_source.
const FeatureFactoryPipelineCopy = "factory.pipeline_copy"

// How main's file compares with the factory's copy ([FactoryPipelineCopy]).
const (
	PipelineCopyMainMissing = "missing"
	PipelineCopyMainDiffers = "differs"
	PipelineCopyMainSame    = "same"
)

// FactoryPipelineCopy is the factory's own copy of a repository's file as a
// read says it: who saved it (a handle) and when, and how main's file compares.
// Main is missing (main has no file), differs (main's file says something
// else) or same: main's file now says the same and the copy cleared itself on
// this very read, so main's file is what runs from now on.
//
// On a refused read ([FactoryPipelineSourceRefusal]) Main is absent when main's
// file was not read.
type FactoryPipelineCopy struct {
	By   string    `json:"by"`
	At   time.Time `json:"at"`
	Main string    `json:"main,omitempty" enum:"missing,differs,same"`
}

// FactoryPipelineSourceRefusal is the Detail of a factory.pipeline_source
// refusal (any code) when the factory keeps its own copy of the repository's
// steps: the file cannot be shown, but the copy still runs, so the app can
// still offer Stop using this copy. FactoryCopy says who saved it and when, and
// Main only when main's file was read; CopyVersion is the expect.version
// factory.pipeline_copy_clear names; Can says whether this caller may stop it,
// and an app offers Stop only when it says so. A refusal without this Detail
// says nothing of a copy.
type FactoryPipelineSourceRefusal struct {
	Factory     string               `json:"factory"`
	Repo        string               `json:"repo"`
	FactoryCopy *FactoryPipelineCopy `json:"factory_copy"`
	CopyVersion int                  `json:"copy_version"`
	Can         *FactoryPipelineCan  `json:"can,omitempty"`
}

// FactoryPipelineCan is what the caller may do with a repository's pipeline
// on this factory. UseOnFactory is saving and stopping the factory's copy.
type FactoryPipelineCan struct {
	UseOnFactory bool `json:"use_on_factory"`
}

// FactoryPipelineCopyExpect is the copy's version as the person read it.
type FactoryPipelineCopyExpect struct {
	Version int `json:"version"`
}

// FactoryPipelineCopySetParams saves text as the factory's copy for repo.
type FactoryPipelineCopySetParams struct {
	Factory string                     `json:"factory"`
	Repo    string                     `json:"repo"`
	Text    string                     `json:"text"`
	Expect  *FactoryPipelineCopyExpect `json:"expect"`
}

// FactoryPipelineCopyClearParams stops the factory's copy for repo.
type FactoryPipelineCopyClearParams struct {
	Factory string                     `json:"factory"`
	Repo    string                     `json:"repo"`
	Expect  *FactoryPipelineCopyExpect `json:"expect"`
}

// FactoryPipelineCopyDone is the factory's answer: the copy's version now, and
// the copy as it stands (who saved it and when), absent once it was stopped.
// Its Main is not said here (the write reads nothing of main):
// factory.pipeline_source says it.
type FactoryPipelineCopyDone struct {
	Factory     string               `json:"factory"`
	Repo        string               `json:"repo"`
	Version     int                  `json:"version"`
	FactoryCopy *FactoryPipelineCopy `json:"factory_copy,omitempty"`
}

// FactoryPipelineCopier is the engine's half of the two methods. Each makes at
// most one request to the factory and never sends it twice.
type FactoryPipelineCopier interface {
	FactoryPipelineCopySet(ctx context.Context, p FactoryPipelineCopySetParams) (FactoryPipelineCopyDone, error)
	FactoryPipelineCopyClear(ctx context.Context, p FactoryPipelineCopyClearParams) (FactoryPipelineCopyDone, error)
}

func withFactoryPipelineCopyFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	_, copier := e.(FactoryPipelineCopier)
	_, editor := e.(FactoryPipelineEditor)
	if copier && editor {
		hello = withFeature(hello, FeatureFactoryPipelineCopy)
	}
	return hello
}

// checkCopyExpect requires the version the person read.
func checkCopyExpect(e *FactoryPipelineCopyExpect) error {
	if e == nil || e.Version < 1 {
		return Errorf(CodeBadParams, "expect.version is required: send the copy_version the person read")
	}
	return nil
}

// CheckPipelineCopySet bounds a save without asking anyone: the connection,
// the repository, the version read, and text as factory.pipeline_pr bounds it.
func CheckPipelineCopySet(p FactoryPipelineCopySetParams) (FactoryPipelineCopySetParams, error) {
	var err error
	if p.Factory, p.Repo, err = checkPipelineRepo(p.Factory, p.Repo); err != nil {
		return p, err
	}
	switch {
	case strings.TrimSpace(p.Text) == "":
		return p, Errorf(CodeBadParams, "text is required")
	case len(p.Text) > PipelinePRTextMax:
		return p, Errorf(CodeBadParams, "text is at most %d bytes", PipelinePRTextMax)
	case !utf8.ValidString(p.Text):
		return p, Errorf(CodeBadParams, "text must be valid UTF-8")
	}
	return p, checkCopyExpect(p.Expect)
}

// CheckPipelineCopyClear bounds a stop without asking anyone.
func CheckPipelineCopyClear(p FactoryPipelineCopyClearParams) (FactoryPipelineCopyClearParams, error) {
	var err error
	if p.Factory, p.Repo, err = checkPipelineRepo(p.Factory, p.Repo); err != nil {
		return p, err
	}
	return p, checkCopyExpect(p.Expect)
}

func init() {
	const set = "factory.pipeline_copy_set"
	methods[set] = shaped[FactoryPipelineCopySetParams, FactoryPipelineCopyDone](func(e Engine, raw json.RawMessage) (*FactoryPipelineCopyDone, error) {
		c, err := pipelineCopier(e, set)
		if err != nil {
			return nil, err
		}
		var p FactoryPipelineCopySetParams
		if err := decodeStrict(set, raw, &p); err != nil {
			return nil, err
		}
		if p, err = CheckPipelineCopySet(p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := c.FactoryPipelineCopySet(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
	const clear = "factory.pipeline_copy_clear"
	methods[clear] = shaped[FactoryPipelineCopyClearParams, FactoryPipelineCopyDone](func(e Engine, raw json.RawMessage) (*FactoryPipelineCopyDone, error) {
		c, err := pipelineCopier(e, clear)
		if err != nil {
			return nil, err
		}
		var p FactoryPipelineCopyClearParams
		if err := decodeStrict(clear, raw, &p); err != nil {
			return nil, err
		}
		if p, err = CheckPipelineCopyClear(p); err != nil {
			return nil, err
		}
		ctx, cancel := factoryCall()
		defer cancel()
		out, err := c.FactoryPipelineCopyClear(ctx, p)
		if err != nil {
			return nil, err
		}
		return &out, nil
	})
}

func pipelineCopier(e Engine, name string) (FactoryPipelineCopier, error) {
	c, ok := e.(FactoryPipelineCopier)
	_, isFactory := e.(Factory)
	_, editor := e.(FactoryPipelineEditor)
	if !ok || !isFactory || !editor {
		return nil, Errorf(CodeUnknownMethod, "this engine does not serve %s", name)
	}
	return c, nil
}
