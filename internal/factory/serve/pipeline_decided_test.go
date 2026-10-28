package serve_test

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// decidedFactory is a v2 factory whose pipeline reads are those of a host
// that keeps copies (Config.JobPipeline over pipeline.JobFile): main's file
// asks first about main/ with a $3 budget, and the factory's copy, while it
// keeps one, asks about copy/ with $7.
type decidedFactory struct {
	*v2Jobs
	mu   sync.Mutex
	kept *factory.PipelineCopy
}

func newDecidedFactory(t *testing.T) *decidedFactory {
	t.Helper()
	f := &decidedFactory{v2Jobs: newV2Jobs(t)}
	f.kept = &factory.PipelineCopy{Repo: "acme/api", Text: "default_budget: 7\nstart:\n  ask_first: [touch copy/]\n", By: "test-owner", At: time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC)}
	main := func(t *testing.T) pipeline.File {
		p, err := pipeline.Parse("default_budget: 3\nstart:\n  ask_first: [touch main/]\n")
		if err != nil {
			t.Fatal(err)
		}
		p.From = factory.FromMainBranch
		return p
	}(t)
	read := func(withCopy bool) (pipeline.File, error) {
		f.mu.Lock()
		kept := f.kept.Clone()
		f.mu.Unlock()
		if withCopy && kept != nil {
			return pipeline.CopyFile(*kept)
		}
		return main, nil
	}
	f.h.Close()
	f.cfg.Pipeline = func(context.Context, string, string) (pipeline.File, error) { return read(true) }
	f.cfg.JobPipeline = func(_ context.Context, it factory.Item, _ string) (pipeline.File, error) {
		return pipeline.JobFile(it, "", read)
	}
	f.h = serve.New(f.seam, f.cfg)
	return f
}

func (f *decidedFactory) stopCopy() { f.mu.Lock(); f.kept = nil; f.mu.Unlock() }

// A JOB'S PIPELINE FILE IS DECIDED AT ITS FIRST START AND KEPT: the start
// takes the copy the factory keeps, with its rules and its budget; a later
// start, after the copy was stopped, still runs it; and a job decided on main's
// file before the copy was saved starts under main's rules and budget.
func TestAStartDecidesTheJobsPipelineFileOnce(t *testing.T) {
	f := newDecidedFactory(t)
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam"})
	onMain := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam", Num: 2, PipelineDecided: true})
	f.post(t, it, "run", "", 200)
	cur, _ := f.st.Get(it.ID)
	if !cur.PipelineCopy.Same(f.kept) || !cur.PipelineDecided || cur.Cap != 7 || !reflect.DeepEqual(cur.StartRules.AskFirst, []string{"touch copy/"}) {
		t.Fatalf("first start: copy %+v cap %v rules %+v", cur.PipelineCopy, cur.Cap, cur.StartRules)
	}
	f.post(t, onMain, "run", "", 200)
	cur, _ = f.st.Get(onMain.ID)
	if cur.PipelineCopy != nil || cur.Cap != 3 || !reflect.DeepEqual(cur.StartRules.AskFirst, []string{"touch main/"}) {
		t.Fatalf("a job decided on main: copy %+v cap %v rules %+v", cur.PipelineCopy, cur.Cap, cur.StartRules)
	}

	// The copy stopped; the job, stopped and started again, keeps it.
	saved := f.kept.Clone()
	f.stopCopy()
	if err := f.st.Update(it.ID, func(it *factory.Item) error {
		it.State, it.Stream, it.Cap, it.StartRules = factory.StateNew, nil, 0, nil
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	f.post(t, it, "run", "", 200)
	cur, _ = f.st.Get(it.ID)
	if !cur.PipelineCopy.Same(saved) || cur.Cap != 7 || !reflect.DeepEqual(cur.StartRules.AskFirst, []string{"touch copy/"}) {
		t.Fatalf("a later start: copy %+v cap %v rules %+v", cur.PipelineCopy, cur.Cap, cur.StartRules)
	}
}

// BRING BACK FORGETS THE FILE A JOB WAS DECIDED ON, with its steps: its steps
// are the recipe's as the factory reads it now, and its next start decides
// afresh.
func TestBringBackForgetsTheDecidedPipelineFile(t *testing.T) {
	f := newDecidedFactory(t)
	saved := f.kept.Clone()
	it := f.create(t, factory.Item{Num: 61, Origin: factory.OriginForge, State: factory.StateDismissed, DoneHow: "dropped", DoneAt: now, Assignee: "sam",
		PipelineCopy: saved, PipelineDecided: true})
	f.stopCopy()
	f.post(t, it, "restore", "", 200)
	cur, _ := f.st.Get(it.ID)
	if cur.PipelineCopy != nil || cur.PipelineDecided {
		t.Fatalf("brought back still decided: %+v", cur.PipelineCopy)
	}
	f.post(t, it, "run", "", 200)
	cur, _ = f.st.Get(it.ID)
	if cur.PipelineCopy != nil || !cur.PipelineDecided || cur.Cap != 3 {
		t.Fatalf("its next start: copy %+v decided %v cap %v", cur.PipelineCopy, cur.PipelineDecided, cur.Cap)
	}
}
