package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/agentfield"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

func TestFactoryJobsChangedRefreshesPersonGatesAndCompletion(t *testing.T) {
	s := newLocalSetup(t)
	s.backend.emit = s.server
	status := s.start()
	host := s.backend.local.host
	translated, err := agentfield.Translate(agentfield.Meta{Source: "generic_bearer"}, json.RawMessage(`{"external_id":"gate-1","title":"Answer a request","repo":"acme/api"}`), factory.TierOwner)
	if err != nil {
		t.Fatal(err)
	}
	filed, err := inbox.Add(context.Background(), host.floor, *translated.File, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := host.floor.Update(filed.ID, func(it *factory.Item) error {
		it.Assignee = "sam"
		it.Stages = []factory.Stage{{Name: "approve", Kind: factory.StageGate, Pass: "person", On: true}, {Name: "answer", Kind: factory.StageAgentField, Target: "support.answer", On: true}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	host.jobFiled(filed.ID)
	var initial api.Queue
	s.must("factory.queue", map[string]any{"factory": status.Factory}, &initial)
	runner := factoryrun.New(factoryrun.Options{Store: host.floor, Exec: map[factory.StageKind]factoryrun.Executor{
		factory.StageAgentField: factoryrun.ExecutorFunc(func(context.Context, factoryrun.Job) (factory.StageResult, error) {
			return factory.StageResult{Done: true, Output: "The answer is ready."}, nil
		}),
	}})
	t.Cleanup(func() { runner.Withdraw(filed.ID) })
	if err := runner.Launch(filed.ID); err != nil {
		t.Fatal(err)
	}
	waitQueue := func(state string) api.Queue {
		t.Helper()
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		for {
			select {
			case line := <-s.answers:
				var frame struct {
					Event string                     `json:"event"`
					Data  appwire.FactoryJobsChanged `json:"data"`
				}
				if json.Unmarshal(line, &frame) != nil || frame.Event != appwire.EventFactoryJobsChanged || frame.Data.Job != filed.ID {
					continue
				}
				if frame.Data.Factory != appLocalID {
					t.Fatalf("notification names %q", frame.Data.Factory)
				}
				queue, err := s.backend.FactoryQueue(context.Background(), status.Factory)
				if err != nil {
					t.Fatal(err)
				}
				for _, j := range queue.Jobs {
					if j.ID == filed.ID && j.State == state {
						return queue
					}
				}
			case <-timer.C:
				t.Fatalf("no jobs_changed refresh exposed the job as %s", state)
			}
		}
	}
	waiting := waitQueue(api.StateNeedsYou)
	if len(waiting.NeedsYou) == 0 {
		t.Fatal("gate refresh omitted the Needs you list")
	}
	if err := runner.Answer(filed.ID, true, "go"); err != nil {
		t.Fatal(err)
	}
	waitQueue(api.StateDone)
	count := func() int {
		s.mu.Lock()
		defer s.mu.Unlock()
		n := 0
		for _, line := range s.lines {
			var frame struct {
				Event string `json:"event"`
			}
			_ = json.Unmarshal([]byte(line), &frame)
			if frame.Event == appwire.EventFactoryJobsChanged {
				n++
			}
		}
		return n
	}
	s.must("factory.queue", map[string]any{"factory": status.Factory}, &initial)
	before := count()
	if err := host.floor.Update(filed.ID, func(*factory.Item) error { return nil }); err != nil {
		t.Fatal(err)
	}
	s.must("factory.queue", map[string]any{"factory": status.Factory}, &initial)
	if count() != before {
		t.Fatal("unchanged lifecycle fields emitted another jobs_changed event")
	}
}
