package session

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/cellstore"
)

func TestIdleRelease_C2_RunningQueuedAndSteeringWorkRetainOwnership(t *testing.T) {
	entered, finish := make(chan struct{}), make(chan struct{})
	watch := &cellstore.SealWatch{}
	watch.Waiting()
	a, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			close(entered)
			select {
			case <-finish:
				return textResponse("first answer"), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
	}}, func(cfg *Config) { cfg.Seals = watch })
	waitFor(t, "initial saved session", watch.IdleReady)
	turn, err := a.Submit(t.Context(), "work until released")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("model did not start")
	}
	if watch.IdleReady() {
		t.Fatal("running turn released ownership")
	}
	queued, err := a.FollowUp("queued work")
	if err != nil {
		t.Fatal(err)
	}
	steered, err := a.Submit(t.Context(), "steering work")
	if err != nil {
		t.Fatal(err)
	}
	if watch.IdleReady() {
		t.Fatal("queued or steering work released ownership")
	}
	close(finish)
	collect(t, turn)
	collect(t, queued)
	collect(t, steered)
	waitFor(t, "all queued work to settle", watch.IdleReady)
}

func TestIdleRelease_C2_BackgroundJobRetainsOwnership(t *testing.T) {
	watch := &cellstore.SealWatch{}
	watch.Waiting()
	a, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) { cfg.Seals = watch })
	id := startJob(t, a, "sleep 30")
	if watch.IdleReady() {
		t.Fatal("running background job released ownership")
	}
	runTool(t, a, "jobs", fmt.Sprintf(`{"op":"kill","id":%d}`, id))
}

func TestIdleRelease_C2_BackgroundTaskRetainsOwnership(t *testing.T) {
	watch := &cellstore.SealWatch{}
	watch.Waiting()
	entered := make(chan struct{}, 8)
	a, _ := newTestAgent(t, &scriptedCompleter{steps: []step{
		func(ctx context.Context, _ []ai.Message) (*ai.Response, error) {
			entered <- struct{}{}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}}, func(cfg *Config) {
		cfg.Seals = watch
		cfg.Workspace = newTestRepo(t)
		cfg.TaskAudit = false
	})
	if _, _, _, err := a.StartTask(t.Context(), "read the workspace and report", true); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("background task did not reach its model")
	}
	if watch.IdleReady() {
		t.Fatal("running task released ownership")
	}
}

func TestQAHandoffWaitsForTheJobExitNoteDelivery(t *testing.T) {
	for _, tc := range []struct {
		name   string
		settle func(*jobRegistry, *job)
	}{
		{"process", func(r *jobRegistry, job *job) { r.settleExit(job, 0) }},
		{"worker", func(r *jobRegistry, job *job) { r.finish(job, 0, "background worker finished") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			watch := &cellstore.SealWatch{}
			watch.Waiting()
			a, _ := newTestAgent(t, &scriptedCompleter{}, func(cfg *Config) { cfg.Seals = watch })
			entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
			old := a.jobs.notify
			a.jobs.notify = func(text string) { close(entered); <-release; old(text) }
			job := settlementJob(t, a.jobs)
			job.owed = true
			go func() { defer close(done); tc.settle(a.jobs, job) }()
			defer func() { close(release); settlementWait(t, done, "exit-note delivery") }()
			settlementWait(t, entered, "exit note to reach delivery")
			select {
			case <-job.done:
			default:
				t.Fatal("job is still running")
			}
			if a.idleForHandoff() {
				t.Fatal("handoff became ready between job exit and its promised note")
			}
		})
	}
}

func TestQAHandoffAllowsQueuedProgressFromAStoppedWatch(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	a.enqueueWatchNote("build", "build watch: a new line", false)
	if len(ambientQueue(a)) != 1 {
		t.Fatal("watch progress was not queued")
	}
	waitFor(t, "handoff after watch progress", a.idleForHandoff)
}
