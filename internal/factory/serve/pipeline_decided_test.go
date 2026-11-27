package serve_test

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

// Contract P3: The first start records why an issue uses the built-in steps,
// and a later start keeps the same single line.
func TestStartRecordsWhyAnIssueUsesBuiltInSteps(t *testing.T) {
	const missing = "This repository has no .codeaf/factory.yaml on its main branch, so this job runs the built-in steps."
	const omitted = ".codeaf/factory.yaml has no pipeline: issue: list, so this job runs the built-in steps."
	const custom = "pipeline:\n  docs: [write, proof]\n"
	const issue = "pipeline:\n  issue: [write, proof]\n"
	for _, tt := range []struct {
		name, yaml, copy, library, pick, want string
		kind                                  factory.Kind
	}{
		{name: "no file", want: missing},
		{name: "no issue list", yaml: custom, want: omitted},
		{name: "repository issue list", yaml: issue},
		{name: "copy issue list", copy: issue},
		{name: "copy without issue list", copy: custom, want: omitted},
		{name: "library issue list", yaml: custom, library: issue},
		{name: "library with no repository file", library: issue},
		{name: "pull request", kind: factory.KindPR},
		{name: "CI", kind: factory.KindCI},
		{name: "named pipeline", yaml: custom, pick: "docs"},
		{name: "routed backlog pipeline", yaml: custom},
		{name: "explicit built-in issue", pick: "issue", want: missing},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			file := pipeline.Defaults()
			file.Source, file.From = pipeline.SourceDefaults, factory.FromDefault
			var err error
			if tt.yaml != "" {
				file, err = pipeline.Parse(tt.yaml)
				if err != nil {
					t.Fatal(err)
				}
				file.Source, file.From = pipeline.SourceFile, factory.FromMainBranch
			}
			if tt.copy != "" {
				file, err = pipeline.CopyFile(factory.PipelineCopy{Repo: "acme/api", Text: tt.copy, At: now, By: "sam"})
				if err != nil {
					t.Fatal(err)
				}
			}
			if tt.library != "" {
				lib, err := pipeline.ParseLibrary(tt.library)
				if err != nil {
					t.Fatal(err)
				}
				file = pipeline.WithLibrary(file, lib)
			}
			f := newV2Jobs(t)
			f.cfg.Pipeline = func(context.Context, string, string) (pipeline.File, error) { return file, nil }
			f.h.Close()
			f.h = serve.New(f.seam, f.cfg)

			kind := tt.kind
			if kind == "" {
				kind = factory.KindIssue
			}
			job := factory.Item{Kind: kind, State: factory.StateNew, Assignee: "sam", Stages: file.Recipe().For(kind)}
			if tt.name == "routed backlog pipeline" {
				job.Route, job.Stages = "documentation", file.Named("docs")
			}
			it := f.create(t, job)
			if kind == factory.KindPR || kind == factory.KindCI {
				f.post(t, it, "run", "", 404)
			} else if tt.pick == "" {
				f.post(t, it, "run", "", 200)
			} else {
				f.post(t, it, "start_now", `{"pipeline":"`+tt.pick+`"}`, 200)
			}
			assertNotes := func() {
				t.Helper()
				got, err := f.st.Get(it.ID)
				if err != nil {
					t.Fatal(err)
				}
				var notes []string
				for _, ev := range got.Activity {
					if strings.Contains(ev.What, "this job runs the built-in steps") {
						notes = append(notes, ev.What)
					}
				}
				if tt.want == "" && len(notes) != 0 || tt.want != "" && (len(notes) != 1 || notes[0] != tt.want) {
					t.Errorf("fallback notes = %q, want %q once when present", notes, tt.want)
				}
			}
			assertNotes()
			if tt.want != "" {
				if err := f.st.Update(it.ID, func(cur *factory.Item) error {
					cur.State, cur.Stream = factory.StateNew, nil
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				f.post(t, it, "run", "", 200)
				assertNotes()
			}
		})
	}
}

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
	it := f.create(t, factory.Item{Tier: factory.TierOwner, Num: 61, Origin: factory.OriginForge, State: factory.StateDismissed, DoneHow: "dropped", DoneAt: now, Assignee: "sam",
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
