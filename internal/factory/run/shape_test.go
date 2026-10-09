package run

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// ranRecorder is a chat executor that keeps each stage it ran with its ask.
type ranRecorder struct {
	mu  sync.Mutex
	ran []string
}

func (r *ranRecorder) exec() Executor {
	return ExecutorFunc(func(_ context.Context, job Job) (factory.StageResult, error) {
		r.mu.Lock()
		r.ran = append(r.ran, job.Stage.Name+"="+job.Stage.Ask)
		r.mu.Unlock()
		return done(""), nil
	})
}

func (r *ranRecorder) got() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.ran, ", ")
}

// shapeEdit is the manager's edit the fake turns answer.
func shapeEdit() factory.RunEdit {
	return factory.RunEdit{
		Ask: map[string]string{"write": "change only the ledger"},
		Add: []factory.Added{{Stage: factory.Stage{Name: "arch", Kind: factory.StageChat, Ask: "say whether the shape holds", On: true}, After: "write"}},
		Why: "the ledger is the risk",
	}
}

const shapedLine = "manager set write: change only the ledger · added arch after write · why: the ledger is the risk"

// LAUNCH CALLS SHAPE BEFORE THE FIRST STAGE, with what the person typed before
// the run, applies the edit, and says the line on the log and to the manager.
func TestLaunchShapesTheRunBeforeTheFirstStage(t *testing.T) {
	talk := newFakeTalk()
	rec := &ranRecorder{}
	var mu sync.Mutex
	var calls []string
	g, _ := managed(t, map[factory.StageKind]Executor{factory.StageChat: rec.exec()}, talk, time.Hour)
	g.r.opts.Shape = func(_ context.Context, it factory.Item, said []string) (factory.RunEdit, string, error) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, strings.Join(said, "|")+" ran:"+rec.got())
		return shapeEdit(), "set write and added arch", nil
	}
	id := g.add("fix the ledger", chat("plan"), chat("write"), chat("review"))
	talk.typed("manager-"+strconv.Itoa(id), "keep the old API")
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	mu.Lock()
	if len(calls) != 1 || calls[0] != "keep the old API ran:" {
		t.Fatalf("shape was called %v", calls)
	}
	mu.Unlock()
	if got := rec.got(); got != "plan=do plan, write=change only the ledger, arch=say whether the shape holds, review=do review" {
		t.Fatalf("ran %s", got)
	}
	if !logHas(it, shapedLine) {
		t.Fatalf("the log does not say %q: %+v", shapedLine, it.Stream.Log)
	}
	talk.waitSaid(t, "manager-"+strconv.Itoa(id), shapedLine)
	if !shapedByManager(it) {
		t.Fatal("the item does not read as shaped by the manager")
	}
}

// SHAPING NEVER STOPS THE RUN: the person reads the shaped stages at the first
// approve step, here before any stage, and continue runs them.
func TestShapedStagesAreReadAtTheFirstApproveStep(t *testing.T) {
	rec := &ranRecorder{}
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: rec.exec()}, func(o *Options) {
		o.Shape = func(context.Context, factory.Item, []string) (factory.RunEdit, string, error) {
			return shapeEdit(), "", nil
		}
	})
	id := g.add("fix the ledger", factory.Stage{Name: factory.ApproveName, Kind: factory.StageGate}, chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.asked(id, factory.ApproveQuestion(""))
	if it.QKind != factory.QKindApprove || rec.got() != "" || !logHas(it, shapedLine) {
		t.Fatalf("kind %q; ran before the yes: %s; log %+v", it.QKind, rec.got(), it.Stream.Log)
	}
	if err := g.r.Answer(id, true, ""); err != nil {
		t.Fatal(err)
	}
	g.waitState(id, factory.StateLanded)
	if got := rec.got(); got != "write=change only the ledger, arch=say whether the shape holds" {
		t.Fatalf("ran %s", got)
	}
}

// AN ITEM THE MANAGER ALREADY SHAPED is not shaped again: the person talked
// first, and that is this run's shape.
func TestShapingIsSkippedWhenTheManagerShapedFirst(t *testing.T) {
	called := false
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: (&ranRecorder{}).exec()}, func(o *Options) {
		o.Shape = func(context.Context, factory.Item, []string) (factory.RunEdit, string, error) {
			called = true
			return shapeEdit(), "", nil
		}
	})
	id := g.add("fix the ledger", chat("write"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		it.Adapted = []string{"manager set write: as the person said"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	if called || logHas(it, sayShapeStands) || logHas(it, sayShapeNoReply) {
		t.Fatalf("shaped again: called %v, log %+v", called, it.Stream.Log)
	}
}

// A TURN THAT FAILS, TAKES TOO LONG OR ANSWERS NOTHING runs the recipe, and the
// log says which.
func TestShapingFallsBackToTheRecipe(t *testing.T) {
	for _, c := range []struct {
		name  string
		shape func(context.Context, factory.Item, []string) (factory.RunEdit, string, error)
		line  string
	}{
		{"error", func(context.Context, factory.Item, []string) (factory.RunEdit, string, error) {
			return factory.RunEdit{}, "", errors.New("the conversation is held by another window")
		}, sayShapeNoReply},
		{"timeout", func(context.Context, factory.Item, []string) (factory.RunEdit, string, error) {
			time.Sleep(2 * time.Second) // a turn that never looks at its ctx
			return shapeEdit(), "", nil
		}, sayShapeNoReply},
		{"nothing", func(context.Context, factory.Item, []string) (factory.RunEdit, string, error) {
			return factory.RunEdit{}, "the recipe fits", nil
		}, sayShapeStands},
		{"busy", func(context.Context, factory.Item, []string) (factory.RunEdit, string, error) {
			return factory.RunEdit{}, "", fmt.Errorf("asked twice: %w", ErrManagerBusy)
		}, sayShapeBusy},
		{"away", func(context.Context, factory.Item, []string) (factory.RunEdit, string, error) {
			return factory.RunEdit{}, "", fmt.Errorf("held: %w", ErrManagerAway)
		}, sayShapeAway},
	} {
		t.Run(c.name, func(t *testing.T) {
			rec := &ranRecorder{}
			g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: rec.exec()}, func(o *Options) {
				o.Shape = c.shape
				o.ShapeWait = 50 * time.Millisecond
			})
			id := g.add("fix the ledger", chat("plan"), chat("write"))
			if err := g.r.Launch(id); err != nil {
				t.Fatal(err)
			}
			it := g.waitState(id, factory.StateLanded)
			if !logHas(it, c.line) {
				t.Fatalf("the log does not say %q: %+v", c.line, it.Stream.Log)
			}
			if got := rec.got(); got != "plan=do plan, write=do write" {
				t.Fatalf("ran %s", got)
			}
		})
	}
}

// THE PERSON'S WORDS REACH THE NOTES WHATEVER THE SHAPING TURN DID: a manager
// another window holds is said by name, and the brief still carries the words.
func TestTheWordsReachTheNotesWhenTheManagerIsAway(t *testing.T) {
	talk := newFakeTalk()
	rec := &ranRecorder{}
	g, _ := managed(t, map[factory.StageKind]Executor{factory.StageChat: rec.exec()}, talk, time.Hour)
	g.r.opts.Shape = func(context.Context, factory.Item, []string) (factory.RunEdit, string, error) {
		return factory.RunEdit{}, "", ErrManagerAway
	}
	id := g.add("fix the ledger", chat("write"))
	talk.typed("manager-"+strconv.Itoa(id), "keep the fix tiny and prove it at the CLI")
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	if !logHas(it, sayShapeAway) {
		t.Fatalf("the log does not say %q: %+v", sayShapeAway, it.Stream.Log)
	}
	talk.waitSaid(t, "manager-"+strconv.Itoa(id), sayShapeAway)
	if len(it.Notes) != 1 || it.Notes[0] != "keep the fix tiny and prove it at the CLI" {
		t.Fatalf("notes %q", it.Notes)
	}
}

// MID-RUN WORDS GIVE THE MANAGER A TURN, and what it sets on a stage not yet
// started is what that stage runs.
func TestMidRunWordsGiveTheManagerATurnAndTheTailChanges(t *testing.T) {
	talk := newFakeTalk()
	var mu sync.Mutex
	var heard []string
	var g *rig
	var reviewAsk string
	exec := ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
		switch job.Stage.Name {
		case "plan":
			// The person speaks while plan runs; plan ends once the manager's
			// change to review is on the item.
			talk.typed(job.Item.Talk, "do a thorough review on security, code and architecture")
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				it, _ := g.st.Get(job.Item.ID)
				if i := factory.StageIndex(it.Stages, "review"); i >= 0 && it.Stages[i].Ask != "do review" {
					break
				}
				time.Sleep(3 * time.Millisecond)
			}
		case "review":
			mu.Lock()
			reviewAsk = job.Stage.Ask
			mu.Unlock()
		}
		return done(""), nil
	})
	g, _ = managed(t, map[factory.StageKind]Executor{factory.StageChat: exec}, talk, 5*time.Millisecond)
	g.r.opts.Reshape = func(_ context.Context, it factory.Item, said string) (factory.RunEdit, string, error) {
		mu.Lock()
		heard = append(heard, said)
		mu.Unlock()
		return factory.RunEdit{Ask: map[string]string{"review": "read it for security, code and architecture"}}, "", nil
	}
	id := g.add("fix the ledger", chat("plan"), chat("review"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	mu.Lock()
	defer mu.Unlock()
	if len(heard) != 1 || heard[0] != "do a thorough review on security, code and architecture" {
		t.Fatalf("the manager heard %v", heard)
	}
	if reviewAsk != "read it for security, code and architecture" {
		t.Fatalf("review ran with %q", reviewAsk)
	}
	if !logHas(it, "manager set review: read it for security, code and architecture") || !logHas(it, "steer: do a thorough review on security, code and architecture") {
		t.Fatalf("log %+v", it.Stream.Log)
	}
}

// THE TAIL IS RE-READ AT EACH ROUND: a stage switched on in the item's stages
// while an earlier one runs is run after it.
func TestTheTailIsReReadAtEachRound(t *testing.T) {
	rec := &ranRecorder{}
	var g *rig
	exec := ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
		if job.Stage.Name == "plan" {
			_ = g.st.Update(job.Item.ID, func(it *factory.Item) error {
				it.Stages = append(it.Stages, factory.Stage{Name: "arch", Kind: factory.StageChat, Ask: "check the shape", Until: "done", On: true})
				return nil
			})
		}
		return rec.exec().Run(ctx, job)
	})
	g = newRig(t, map[factory.StageKind]Executor{factory.StageChat: exec}, nil)
	id := g.add("fix it", chat("plan"), chat("write"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitState(id, factory.StateLanded)
	if got := rec.got(); got != "plan=do plan, write=do write, arch=check the shape" {
		t.Fatalf("ran %s", got)
	}
}

func TestEditLineSaysWhoOnce(t *testing.T) {
	if got := EditLine([]string{"manager set review: x", "manager added arch after review", "why: y"}); got != "manager set review: x · added arch after review · why: y" {
		t.Fatalf("line = %q", got)
	}
}

// APPLYSHAPE IS THE ONE APPLICATION, shared by the launch and the floor's
// Shape door: on an item with no run it changes the stages, records the line
// and makes no stream; on an item in a run it also compiles the phases not yet
// started and says the line on the log. An empty edit changes nothing.
func TestApplyShapeIsSharedWithTheLaunch(t *testing.T) {
	recipe := factory.DefaultRecipe()
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	fresh := factory.Item{ID: 1, Kind: factory.KindIssue, Stages: factory.CopyStages(recipe.For(factory.KindIssue))}
	before := factory.CopyStages(fresh.Stages)
	edit := shapeEdit()
	edit.Ask = map[string]string{"review": "read it for security"}
	edit.Add = nil

	if line, err := ApplyShape(&fresh, factory.RunEdit{}, recipe, false, at); err != nil || line != "" || len(fresh.Adapted) != 0 {
		t.Fatalf("an empty edit answered %q, %v and recorded %q", line, err, fresh.Adapted)
	}
	line, err := ApplyShape(&fresh, edit, recipe, false, at)
	if err != nil || line != "manager set review: read it for security · why: the ledger is the risk" {
		t.Fatalf("line %q, err %v", line, err)
	}
	if fresh.Stream != nil || !shapedByManager(fresh) {
		t.Fatalf("stream %v, record %q", fresh.Stream, fresh.Adapted)
	}
	if i := factory.StageIndex(fresh.Stages, "review"); fresh.Stages[i].Ask == before[i].Ask {
		t.Fatal("the edit did not reach the stages")
	}

	running := factory.Item{ID: 2, Kind: factory.KindIssue, Stages: factory.CopyStages(recipe.For(factory.KindIssue))}
	running.Stream = &factory.Stream{Started: at, Phases: loopPhases(running)}
	add := shapeEdit()
	add.Ask = nil
	add.Add = []factory.Added{{Stage: factory.Stage{Name: "arch", Kind: factory.StageChat, Ask: "say whether the shape holds", On: true}, After: "review"}}
	if _, err := ApplyShape(&running, add, recipe, false, at); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range running.Stream.Phases {
		found = found || p.Name == "arch"
	}
	if !found || len(running.Stream.Log) == 0 {
		t.Fatalf("phases %+v, log %+v", running.Stream.Phases, running.Stream.Log)
	}
}
