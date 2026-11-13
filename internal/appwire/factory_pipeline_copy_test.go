package appwire

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// pipelineCopyEngine is a pipeline-editing factory engine that also keeps the
// factory's copy, and keeps what each call was handed.
type pipelineCopyEngine struct {
	pipelineEditEngine
	sets   []FactoryPipelineCopySetParams
	clears []FactoryPipelineCopyClearParams
}

func (r *pipelineCopyEngine) FactoryPipelineCopySet(_ context.Context, p FactoryPipelineCopySetParams) (FactoryPipelineCopyDone, error) {
	r.sets = append(r.sets, p)
	return FactoryPipelineCopyDone{Factory: p.Factory, Repo: p.Repo, Version: p.Expect.Version + 1,
		FactoryCopy: &FactoryPipelineCopy{By: "test-owner", At: time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)}}, nil
}

func (r *pipelineCopyEngine) FactoryPipelineCopyClear(_ context.Context, p FactoryPipelineCopyClearParams) (FactoryPipelineCopyDone, error) {
	r.clears = append(r.clears, p)
	return FactoryPipelineCopyDone{Factory: p.Factory, Repo: p.Repo, Version: p.Expect.Version + 1}, nil
}

// THE FEATURE IS ITS OWN, AND ONLY AN ENGINE THAT ALSO READS THE SOURCE
// ANNOUNCES IT: the copy is saved from an edit of that source.
func TestFactoryPipelineCopyIsAnnouncedOnlyBesideTheEditor(t *testing.T) {
	raw := json.RawMessage(`{"factory":"f","repo":"a/b","text":"pipeline: {}\n","expect":{"version":1}}`)
	for _, e := range []Engine{&fakeEngine{}, &factoryEngine{}, &pipelineEditEngine{}, &pipelinePREngine{}} {
		if _, err := methods["factory.pipeline_copy_set"].call(e, raw); wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%T: %v", e, err)
		}
		if got := withFactoryPipelineCopyFeature(Hello{}, e); len(got.Features) != 0 {
			t.Errorf("%T announced %v", e, got.Features)
		}
	}
	got := withFactoryPipelineCopyFeature(Hello{}, &pipelineCopyEngine{})
	if len(got.Features) != 1 || got.Features[0] != FeatureFactoryPipelineCopy {
		t.Fatalf("announced %v", got.Features)
	}
}

// BOTH WRITES NAME THE VERSION READ, AND A SAVE CARRIES A FILE THE PULL
// REQUEST WOULD TAKE: anything else, or any other field, is bad_params and the
// engine is not asked; good params are passed on once, trimmed.
func TestFactoryPipelineCopyParamsAreStrict(t *testing.T) {
	e := &pipelineCopyEngine{}
	for name, raw := range map[string]string{
		"no expect":        `{"factory":"f","repo":"a/b","text":"pipeline: {}\n"}`,
		"version zero":     `{"factory":"f","repo":"a/b","text":"pipeline: {}\n","expect":{"version":0}}`,
		"no text":          `{"factory":"f","repo":"a/b","text":"  ","expect":{"version":1}}`,
		"text too large":   `{"factory":"f","repo":"a/b","text":"` + strings.Repeat("a", PipelinePRTextMax+1) + `","expect":{"version":1}}`,
		"not owner/name":   `{"factory":"f","repo":"ab","text":"pipeline: {}\n","expect":{"version":1}}`,
		"no factory":       `{"repo":"a/b","text":"pipeline: {}\n","expect":{"version":1}}`,
		"an unknown field": `{"factory":"f","repo":"a/b","text":"pipeline: {}\n","expect":{"version":1},"by":"someone"}`,
	} {
		if _, err := methods["factory.pipeline_copy_set"].call(e, json.RawMessage(raw)); wireCode(t, err) != CodeBadParams {
			t.Errorf("%s: %v", name, err)
		}
	}
	for name, raw := range map[string]string{
		"no expect": `{"factory":"f","repo":"a/b"}`,
		"text":      `{"factory":"f","repo":"a/b","text":"x","expect":{"version":2}}`,
	} {
		if _, err := methods["factory.pipeline_copy_clear"].call(e, json.RawMessage(raw)); wireCode(t, err) != CodeBadParams {
			t.Errorf("clear %s: %v", name, err)
		}
	}
	if len(e.sets) != 0 || len(e.clears) != 0 {
		t.Fatalf("the engine was asked: %v %v", e.sets, e.clears)
	}
	out, err := methods["factory.pipeline_copy_set"].call(e, json.RawMessage(`{"factory":" f ","repo":" a/b ","text":"pipeline: {}\n","expect":{"version":3}}`))
	if err != nil || len(e.sets) != 1 || e.sets[0].Factory != "f" || e.sets[0].Repo != "a/b" || e.sets[0].Expect.Version != 3 {
		t.Fatalf("%v %+v", err, e.sets)
	}
	if done := out.(*FactoryPipelineCopyDone); done.Version != 4 || done.FactoryCopy == nil || done.FactoryCopy.By != "test-owner" {
		t.Fatalf("%+v", done)
	}
	if _, err := methods["factory.pipeline_copy_clear"].call(e, json.RawMessage(`{"factory":"f","repo":"a/b","expect":{"version":4}}`)); err != nil || len(e.clears) != 1 {
		t.Fatalf("%v %+v", err, e.clears)
	}
}
