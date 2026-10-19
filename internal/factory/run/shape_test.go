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
	"github.com/Agent-Field/codeaf/internal/factory/store"
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

// A LAUNCH GOES STRAIGHT TO THE FIRST STAGE (the owner's run of 2026-10-19,
// where a manager turn before the plan stopped on a card nobody answered and
// the plan never ran): the manager is given no turn, the plan starting is the
// first thing its chat is told, the stages run as the repository's recipe
// has them, the log says no shaping line, and what the person typed before the
// run still reaches the notes.
func TestALaunchGoesStraightToTheFirstStage(t *testing.T) {
	talk := newFakeTalk()
	rec := &ranRecorder{}
	var mu sync.Mutex
	turns := 0
	var before []string
	var g *rig
	exec := ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
		if job.Stage.Name == "plan" {
			mu.Lock()
			before = talk.lines(job.Item.Talk)
			mu.Unlock()
		}
		return rec.exec().Run(ctx, job)
	})
	g, _ = managed(t, map[factory.StageKind]Executor{factory.StageChat: exec}, talk, time.Hour)
	g.r.opts.ShapeNow = func(context.Context, factory.Item, bool) (factory.RunEdit, string, error) {
		mu.Lock()
		turns++
		mu.Unlock()
		return shapeEdit(), "", nil
	}
	g.r.opts.Inbox = func(context.Context, factory.Item, factory.Asked) (InboxReply, error) {
		mu.Lock()
		turns++
		mu.Unlock()
		return InboxReply{}, nil
	}
	id := g.add("fix the ledger", chat("plan"), chat("write"), chat("review"))
	talk.typed("manager-"+strconv.Itoa(id), "keep the old API")
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	mu.Lock()
	defer mu.Unlock()
	if turns != 0 {
		t.Fatalf("the manager was given %d runner turns, want none", turns)
	}
	if got := rec.got(); got != "plan=do plan, write=do write, review=do review" {
		t.Fatalf("ran %s", got)
	}
	if len(before) != 1 || before[0] != "plan started" {
		t.Fatalf("before the plan ran the manager's chat was told %q, want only that the plan started", before)
	}
	for _, line := range []string{sayShapeStands, sayShapeNoReply, sayShapeBusy, sayShapeAway} {
		if logHas(it, line) {
			t.Fatalf("the log says %q: %+v", line, it.Stream.Log)
		}
	}
	if shapedByManager(it) {
		t.Fatalf("the item reads as shaped by the manager: %q", it.Adapted)
	}
	if len(it.Notes) != 1 || it.Notes[0] != "keep the old API" {
		t.Fatalf("notes %q", it.Notes)
	}
}

// A STAGE THE MANAGER'S PEN CHANGED BEFORE THE RUN is what the run runs: the
// person talked first, and the manager's own `factory_run` there set it.
func TestALaunchRunsWhatTheManagerSetBeforeIt(t *testing.T) {
	rec := &ranRecorder{}
	g := newRig(t, map[factory.StageKind]Executor{factory.StageChat: rec.exec()}, nil)
	id := g.add("fix the ledger", chat("plan"), chat("write"))
	if err := g.st.Update(id, func(it *factory.Item) error {
		_, err := ApplyShape(it, shapeEdit(), factory.DefaultRecipe(), false, time.Now())
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	g.waitState(id, factory.StateLanded)
	if got := rec.got(); got != "plan=do plan, write=change only the ledger, arch=say whether the shape holds" {
		t.Fatalf("ran %s", got)
	}
}

// MID-RUN WORDS ARE HANDLED ONCE: the person typed them to the manager, whose
// own turn answers them, so the runner takes them as the steer and gives the
// manager NO second turn on them (the owner's run of 2026-10-09 answered
// `how's the plan coming along?` twice). A stage the manager's own pen changed
// meanwhile is what runs.
func TestMidRunWordsAreASteerAndNoSecondManagerTurn(t *testing.T) {
	talk := newFakeTalk()
	var mu sync.Mutex
	turns := 0
	var g *rig
	var reviewAsk string
	exec := ExecutorFunc(func(ctx context.Context, job Job) (factory.StageResult, error) {
		switch job.Stage.Name {
		case "plan":
			talk.typed(job.Item.Talk, "how is the plan coming along?")
			// The manager's own turn on those words changes review through
			// its pen, as the floor's edit door does.
			_ = g.st.Update(job.Item.ID, func(it *factory.Item) error {
				if i := factory.StageIndex(it.Stages, "review"); i >= 0 {
					it.Stages[i].Ask = "read it for security"
				}
				return nil
			})
			deadline := time.Now().Add(10 * time.Second)
			for time.Now().Before(deadline) {
				if it, _ := g.st.Get(job.Item.ID); logHas(it, "steer: how is the plan coming along?") {
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
	g.r.opts.ShapeNow = func(context.Context, factory.Item, bool) (factory.RunEdit, string, error) {
		mu.Lock()
		turns++
		mu.Unlock()
		return factory.RunEdit{}, "", nil
	}
	id := g.add("fix the ledger", chat("plan"), chat("review"))
	if err := g.r.Launch(id); err != nil {
		t.Fatal(err)
	}
	it := g.waitState(id, factory.StateLanded)
	mu.Lock()
	defer mu.Unlock()
	if turns != 0 {
		t.Fatalf("the manager was given %d runner turns, want none", turns)
	}
	if reviewAsk != "read it for security" {
		t.Fatalf("review ran with %q", reviewAsk)
	}
	if !logHas(it, "steer: how is the plan coming along?") {
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

// APPLYSHAPE IS THE ONE APPLICATION, shared by a worker's `shape steps` and the
// floor's Shape door: on an item with no run it changes the stages, records the line
// and makes no stream; on an item in a run it also compiles the phases not yet
// started and says the line on the log. An empty edit changes nothing.
func TestApplyShapeIsTheOneApplication(t *testing.T) {
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

// ShapeAsked is the turn a lease signal asks of the worker holding the job.
func TestShapeAsked(t *testing.T) {
	write := factory.Stage{Name: "write", Kind: factory.StageChat, Ask: "do write", On: true}
	t.Run("applies the edit", func(t *testing.T) {
		st, it := shapeItem(t, factory.Item{Title: "fix the ledger", State: factory.StateNew, Stages: []factory.Stage{write}})
		called := 0
		r := New(Options{Store: st, ShapeNow: func(context.Context, factory.Item, bool) (factory.RunEdit, string, error) {
			called++
			return shapeEdit(), "", nil
		}})
		line, err := r.ShapeAsked(context.Background(), it.ID)
		if err != nil || line != shapedLine || called != 1 {
			t.Fatalf("line %q err %v calls %d", line, err, called)
		}
		got, err := st.Get(it.ID)
		if err != nil || !shapedByManager(got) {
			t.Fatalf("not shaped: %v %+v", err, got.Stages)
		}
	})
	t.Run("keeps the recipe", func(t *testing.T) {
		st, it := shapeItem(t, factory.Item{Title: "fix the ledger", State: factory.StateNew, Stages: []factory.Stage{write}})
		r := New(Options{Store: st, ShapeNow: func(context.Context, factory.Item, bool) (factory.RunEdit, string, error) {
			return factory.RunEdit{}, "", nil
		}})
		line, err := r.ShapeAsked(context.Background(), it.ID)
		got, _ := st.Get(it.ID)
		if err != nil || line != SayShapeStands || len(got.Adapted) != 1 || got.Adapted[0] != KeptRecipe {
			t.Fatalf("line %q err %v adapted %v", line, err, got.Adapted)
		}
	})
	t.Run("already shaped", func(t *testing.T) {
		st, it := shapeItem(t, factory.Item{Title: "fix the ledger", State: factory.StateNew, Stages: []factory.Stage{write}, Adapted: []string{KeptRecipe}})
		r := New(Options{Store: st, ShapeNow: func(context.Context, factory.Item, bool) (factory.RunEdit, string, error) {
			t.Fatal("shaped again")
			return factory.RunEdit{}, "", nil
		}})
		line, err := r.ShapeAsked(context.Background(), it.ID)
		if err != nil || line != SayShapedAlready {
			t.Fatalf("line %q err %v", line, err)
		}
	})
	t.Run("run over", func(t *testing.T) {
		st, it := shapeItem(t, factory.Item{Title: "fix the ledger", State: factory.StateLanded, Num: 12, Stages: []factory.Stage{write}})
		r := New(Options{Store: st, ShapeNow: func(context.Context, factory.Item, bool) (factory.RunEdit, string, error) {
			t.Fatal("shaped a landed item")
			return factory.RunEdit{}, "", nil
		}})
		_, err := r.ShapeAsked(context.Background(), it.ID)
		if !errors.Is(err, ErrNothingToShape) {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("no turn", func(t *testing.T) {
		st, it := shapeItem(t, factory.Item{Title: "fix the ledger", State: factory.StateNew, Stages: []factory.Stage{write}})
		line, err := New(Options{Store: st}).ShapeAsked(context.Background(), it.ID)
		if err == nil || line != sayShapeNoReply {
			t.Fatalf("line %q err %v", line, err)
		}
	})
	t.Run("already out", func(t *testing.T) {
		st, it := shapeItem(t, factory.Item{Title: "fix the ledger", State: factory.StateNew, Stages: []factory.Stage{write}})
		started := make(chan struct{})
		release := make(chan struct{})
		r := New(Options{Store: st, ShapeNow: func(context.Context, factory.Item, bool) (factory.RunEdit, string, error) {
			close(started)
			<-release
			return shapeEdit(), "", nil
		}})
		var first string
		var firstErr error
		done := make(chan struct{})
		go func() {
			first, firstErr = r.ShapeAsked(context.Background(), it.ID)
			close(done)
		}()
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatal("the first turn never started")
		}
		line, err := r.ShapeAsked(context.Background(), it.ID)
		close(release)
		<-done
		if err != nil || line != factory.ShapeAlready || firstErr != nil || first != shapedLine {
			t.Fatalf("second %q %v; first %q %v", line, err, first, firstErr)
		}
	})
}

// A `shape steps` TURN THAT FAILS, TAKES TOO LONG OR ANSWERS NOTHING keeps the
// recipe, and the line says which.
func TestShapeAskedFallsBackToTheRecipe(t *testing.T) {
	write := factory.Stage{Name: "write", Kind: factory.StageChat, Ask: "do write", On: true}
	for _, c := range []struct {
		name  string
		shape func(context.Context, factory.Item, bool) (factory.RunEdit, string, error)
		line  string
	}{
		{"error", func(context.Context, factory.Item, bool) (factory.RunEdit, string, error) {
			return factory.RunEdit{}, "", errors.New("the conversation is held by another window")
		}, sayShapeNoReply},
		{"timeout", func(context.Context, factory.Item, bool) (factory.RunEdit, string, error) {
			time.Sleep(2 * time.Second) // a turn that never looks at its ctx
			return shapeEdit(), "", nil
		}, sayShapeNoReply},
		{"nothing", func(context.Context, factory.Item, bool) (factory.RunEdit, string, error) {
			return factory.RunEdit{}, "the recipe fits", nil
		}, sayShapeStands},
		{"busy", func(context.Context, factory.Item, bool) (factory.RunEdit, string, error) {
			return factory.RunEdit{}, "", fmt.Errorf("asked twice: %w", ErrManagerBusy)
		}, sayShapeBusy},
		{"away", func(context.Context, factory.Item, bool) (factory.RunEdit, string, error) {
			return factory.RunEdit{}, "", fmt.Errorf("held: %w", ErrManagerAway)
		}, sayShapeAway},
	} {
		t.Run(c.name, func(t *testing.T) {
			st, it := shapeItem(t, factory.Item{Title: "fix the ledger", State: factory.StateNew, Stages: []factory.Stage{write}})
			r := New(Options{Store: st, ShapeNow: c.shape, ShapeWait: 50 * time.Millisecond})
			line, _ := r.ShapeAsked(context.Background(), it.ID)
			if line != c.line {
				t.Fatalf("line %q, want %q", line, c.line)
			}
			got, _ := st.Get(it.ID)
			if got.Stages[0].Ask != "do write" {
				t.Fatalf("the stages changed: %+v", got.Stages)
			}
		})
	}
}

func shapeItem(t *testing.T, it factory.Item) (*store.Store, factory.Item) {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	made, err := st.Create(it)
	if err != nil {
		t.Fatal(err)
	}
	return st, made
}
