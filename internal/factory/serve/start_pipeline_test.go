package serve_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

const docsPipelineFile = `pipeline:
  issue: [plan, write, proof]
  docs: [write, proof]
steps:
  plan:
    prompt: plan it
  write:
    prompt: write the docs
  proof:
    prompt: prove it
`

// startWithDocs is a v2Jobs factory whose repository file lists an issue and a
// docs pipeline.
func startWithDocs(t *testing.T) *v2Jobs {
	t.Helper()
	f := newV2Jobs(t)
	f.cfg.Pipeline = func(context.Context, string, string) (pipeline.File, error) { return pipeline.Parse(docsPipelineFile) }
	f.h.Close()
	f.h = serve.New(f.seam, f.cfg)
	return f
}

func stageNames(stages []factory.Stage) string {
	names := make([]string, len(stages))
	for i, st := range stages {
		names[i] = st.Name
	}
	return strings.Join(names, ",")
}

// A START MAY NAME THE PIPELINE THE JOB RUNS: one of the kinds the repository's
// file lists, decided in the start's one write.
func TestStartNowRunsTheNamedPipeline(t *testing.T) {
	f := startWithDocs(t)
	it := f.create(t, factory.Item{State: factory.StateNew})
	f.post(t, it, "start_now", `{"pipeline":"docs"}`, 200)
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := stageNames(saved.Stages); got != "write,proof" {
		t.Fatalf("stages %s", got)
	}
}

func TestStartNowRefusesAPipelineItCannotRunBeforeWriting(t *testing.T) {
	f := startWithDocs(t)
	for body, status := range map[string]int{
		`{"pipeline":"migration"}`: 422,
		`{"pipeline":"pr"}`:        422,
		`{"pipeline":"Docs!"}`:     422,
	} {
		it := f.create(t, factory.Item{State: factory.StateNew})
		f.post(t, it, "start_now", body, status)
		saved, err := f.st.Get(it.ID)
		if err != nil {
			t.Fatal(err)
		}
		if saved.Marked || saved.Starter != "" {
			t.Fatalf("%s: a refused start wrote the job: %+v", body, saved)
		}
	}
	// A job that has run keeps the steps it ran.
	ran := f.create(t, factory.Item{State: factory.StateNew, Stream: &factory.Stream{Phases: []factory.Phase{{Name: "plan", Round: 1, State: factory.PhaseDone}}}})
	f.post(t, ran, "start_now", `{"pipeline":"docs"}`, 409)
	// Only start_now names a pipeline.
	other := f.create(t, factory.Item{State: factory.StateNew})
	f.post(t, other, "dismiss", `{"pipeline":"docs"}`, 422)
}

// startWithLibrary is a v2Jobs factory whose repository file is repo and whose
// store keeps library as its pipeline library, read the way every host reads
// it: the repository's file with the library beside it.
func startWithLibrary(t *testing.T, repo, library string) *v2Jobs {
	t.Helper()
	f := newV2Jobs(t)
	data, refused := pipeline.CheckLibrary(library)
	if refused != "" {
		t.Fatal(refused)
	}
	if _, err := f.st.SetPipelineLibrary(string(data), factory.RulesChange{}, 1); err != nil {
		t.Fatal(err)
	}
	f.cfg.Pipeline = func(context.Context, string, string) (pipeline.File, error) {
		file, err := pipeline.Parse(repo)
		if err != nil {
			return file, err
		}
		kept, err := f.st.PipelineLibrary()
		if err != nil {
			return file, err
		}
		lib, err := pipeline.ParseLibrary(kept.Text)
		return pipeline.WithLibrary(file, lib), err
	}
	f.h.Close()
	f.h = serve.New(f.seam, f.cfg)
	return f
}

const handbookLibrary = `pipeline:
  handbook: [write, hb-check, proof]
  docs: [write, proof]
steps:
  write:
    prompt: write it the library's way
  hb-check:
    prompt: read the handbook back
`

// A START MAY PICK A PIPELINE ONLY THE FACTORY'S LIBRARY HAS, and the job runs
// the library's steps in the library's words.
func TestStartNowRunsALibraryOnlyPipeline(t *testing.T) {
	f := startWithLibrary(t, docsPipelineFile, handbookLibrary)
	it := f.create(t, factory.Item{State: factory.StateNew})
	f.post(t, it, "start_now", `{"pipeline":"handbook"}`, 200)
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := stageNames(saved.Stages); got != "write,hbcheck,proof" {
		t.Fatalf("stages %s", got)
	}
	if saved.Stages[0].Ask != "write it the library's way" {
		t.Fatalf("the library pipeline ran %q", saved.Stages[0].Ask)
	}
}

// A REPOSITORY PIPELINE OF THE SAME NAME WINS over the library's.
func TestStartNowRunsTheRepositorysPipelineOverTheLibrarysOfTheSameName(t *testing.T) {
	f := startWithLibrary(t, docsPipelineFile, handbookLibrary)
	it := f.create(t, factory.Item{State: factory.StateNew})
	f.post(t, it, "start_now", `{"pipeline":"docs"}`, 200)
	saved, err := f.st.Get(it.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := stageNames(saved.Stages); got != "write,proof" || saved.Stages[0].Ask != "write the docs" {
		t.Fatalf("stages %s, write %q", got, saved.Stages[0].Ask)
	}
	// A name no layer has keeps its refusal.
	other := f.create(t, factory.Item{State: factory.StateNew})
	f.post(t, other, "start_now", `{"pipeline":"migration"}`, 422)
}
