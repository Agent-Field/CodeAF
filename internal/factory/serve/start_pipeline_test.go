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
	ran := f.create(t, factory.Item{State: factory.StateNew, Stream: &factory.Stream{}})
	f.post(t, ran, "start_now", `{"pipeline":"docs"}`, 409)
	// Only start_now names a pipeline.
	other := f.create(t, factory.Item{State: factory.StateNew})
	f.post(t, other, "dismiss", `{"pipeline":"docs"}`, 422)
}
