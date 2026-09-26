package remote

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

type slowTaskStartAgent struct {
	*railAgent
	accepted chan struct{}
	release  chan struct{}
	starts   int
}

func (a *slowTaskStartAgent) StartTask(context.Context, string, bool) (uint64, string, string, error) {
	a.mu.Lock()
	a.starts++
	a.mu.Unlock()
	a.land(taskEvent(7, "accepted work", session.TaskRunning))
	close(a.accepted)
	<-a.release
	return 7, "accepted work", "", nil
}

func (a *slowTaskStartAgent) StartTaskEffort(ctx context.Context, brief string, solo bool, _ string) (uint64, string, string, error) {
	return a.StartTask(ctx, brief, solo)
}

func (a *slowTaskStartAgent) StartDelegate(ctx context.Context, _ string, brief string) (uint64, string, string, error) {
	return a.StartTask(ctx, brief, true)
}

func TestALateTaskStartKeepsOneAcceptedTaskOnTheHostedRail(t *testing.T) {
	for _, entry := range []struct {
		name  string
		start func(*Agent, context.Context) (uint64, string, string, error)
	}{
		{"ordinary", func(a *Agent, ctx context.Context) (uint64, string, string, error) {
			return a.StartTask(ctx, "accepted work", true)
		}},
		{"effort", func(a *Agent, ctx context.Context) (uint64, string, string, error) {
			return a.StartTaskEffort(ctx, "accepted work", true, "best")
		}},
		{"delegate", func(a *Agent, ctx context.Context) (uint64, string, string, error) {
			return a.StartDelegate(ctx, "builder", "accepted work")
		}},
	} {
		t.Run(entry.name, func(t *testing.T) {
			far := &slowTaskStartAgent{
				railAgent: &railAgent{fakeAgent: &fakeAgent{}},
				accepted:  make(chan struct{}), release: make(chan struct{}),
			}
			var releaseOnce sync.Once
			defer func() { releaseOnce.Do(func() { close(far.release) }) }()
			loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
				return &Engine{Agent: far, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = loop.Close() })
			lane, stop := loop.Client.Agent().WatchTaskUpdates()
			t.Cleanup(stop)
			waitFor(t, "the task lane to open", func() bool { return far.opened() == 1 })

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			t.Cleanup(cancel)
			result := make(chan error, 1)
			go func() {
				_, _, _, err := entry.start(loop.Client.Agent(), ctx)
				result <- err
			}()
			select {
			case <-far.accepted:
			case <-ctx.Done():
				t.Fatal("the engine did not accept the task before the deadline")
			}
			if err := <-result; !errors.Is(err, session.ErrSendUnanswered) {
				t.Fatalf("accepted start without its receipt = %v, want an uncertain result", err)
			}
			row := nextTask(t, lane)
			if row.Task == nil || row.Task.ID != 7 || row.Task.State != session.TaskRunning {
				t.Fatalf("accepted task disappeared after its receipt timed out: %+v", row)
			}
			releaseOnce.Do(func() { close(far.release) })
			if _, err := loop.Client.Ping(); err != nil {
				t.Fatalf("the late receipt broke the connection: %v", err)
			}
			far.mu.Lock()
			defer far.mu.Unlock()
			if far.starts != 1 || len(far.roster) != 1 {
				t.Fatalf("start attempts = %d, tasks = %d, want one of each", far.starts, len(far.roster))
			}
		})
	}
}

func TestAnEngineRefusalToStartATaskIsStillDefinite(t *testing.T) {
	client, engine := newEngine(t)
	engine.fails[MethodTaskStart] = "the planner did not answer in time"
	_, _, _, err := client.Agent().StartTask(context.Background(), "work", true)
	if err == nil || errors.Is(err, session.ErrSendUnanswered) {
		t.Fatalf("engine refusal = %v, want a definite failure", err)
	}
}
