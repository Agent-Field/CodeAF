package run

import (
	"context"
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

// shapeEdit is an edit the manager's own `factory_run` might make.
func shapeEdit() factory.RunEdit {
	return factory.RunEdit{
		Ask: map[string]string{"write": "change only the ledger"},
		Add: []factory.Added{{Stage: factory.Stage{Name: "arch", Kind: factory.StageChat, Ask: "say whether the shape holds", On: true}, After: "write"}},
		Why: "the ledger is the risk",
	}
}

// A LAUNCH GOES STRAIGHT TO THE FIRST STAGE (the owner's run of 2026-10-19,
// where a manager turn before the plan stopped on a card nobody answered and
// the plan never ran): the manager is given no turn, the plan starting is the
// first thing its chat is told, the stages run as the repository's recipe
// has them, nothing on the item says the manager changed them, and what the
// person typed before the run still reaches the notes.
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
	for _, l := range it.Adapted {
		if strings.HasPrefix(l, factory.ByManager+" ") {
			t.Fatalf("the item reads as changed by the manager: %q", it.Adapted)
		}
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
		next, _, err := factory.Edit(*it, shapeEdit(), factory.DefaultRecipe(), factory.EditBeforeRun)
		*it = next
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
// the same message twice). A stage the manager's own pen changed
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
			talk.typed(job.Item.Talk, "Use the existing helper")
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
				if it, _ := g.st.Get(job.Item.ID); logHas(it, "steer: Use the existing helper") {
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
	g.r.opts.Inbox = func(context.Context, factory.Item, factory.Asked) (InboxReply, error) {
		mu.Lock()
		turns++
		mu.Unlock()
		return InboxReply{}, nil
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
	if !logHas(it, "steer: Use the existing helper") {
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
