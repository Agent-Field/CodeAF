package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
)

// A stopped conversation did not fail its rule. Read its history through the
// same solo service and app wire as Carry on, including a new engine's read.
func TestFactoryRoundOutcomesAfterCarryOn(t *testing.T) {
	for _, tc := range []struct {
		name, step, stop, code, result string
		restart, rejected              bool
	}{
		{name: "step stop", step: "write", stop: "factory.job_step_stop", result: "stopped"},
		{name: "job stop", step: "write", stop: "factory.job_stop", result: "stopped"},
		{name: "runner lost across restart", step: "proof", code: factory.CodeMachineLost, result: "interrupted", restart: true},
		{name: "real failure", step: "write", code: factory.CodeToolError, result: "failed", rejected: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newLocalSetup(t)
			status := s.start()
			floor := s.backend.local.host.floor
			opened := make(chan *factoryrun.FakeConversation, 1)
			maker := &factoryrun.FakeMaker{Script: func(ctx context.Context, c *factoryrun.FakeConversation) {
				opened <- c
				<-ctx.Done()
			}}
			if tc.rejected {
				maker.Fail = errors.New("tool failed to start")
			}
			runner := factoryrun.New(factoryrun.Options{Store: floor, Exec: map[factory.StageKind]factoryrun.Executor{factory.StageChat: factoryrun.NewChatExecutor(maker)}})
			item, err := floor.Create(factory.Item{Tier: factory.TierOwner, Kind: factory.KindIssue, Repo: "acme/api", Num: 17, Title: "Keep the work", State: factory.StateNew, Cap: 10,
				Stages: []factory.Stage{{Name: tc.step, Kind: factory.StageChat, On: true, Until: factory.UntilDone, Max: 2}}})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				_ = runner.Stop(item.ID)
				eventually(t, "runner cleanup", func() bool { return !runner.Running(item.ID) })
			})
			if err := runner.Launch(item.ID); err != nil {
				t.Fatal(err)
			}
			wire := s.factoryWire
			read := func() api.Job {
				var job api.Job
				wire.must("factory.job", map[string]any{"factory": status.Factory, "job": item.ID}, &job)
				return job
			}
			act := func(method string) {
				job := read()
				params := map[string]any{"factory": status.Factory, "job": item.ID, "expect": map[string]any{"version": job.Version}}
				if method != "factory.job_stop" {
					params["step"] = tc.step
				}
				wire.must(method, params, &appwire.FactoryJobActed{})
			}
			if tc.rejected {
				eventually(t, "the executor's failure", func() bool { return read().Failure != nil })
			} else {
				select {
				case <-opened:
				case <-time.After(5 * time.Second):
					t.Fatal("fake model never started")
				}
				eventually(t, "the running step chat", func() bool {
					got, e := floor.Get(item.ID)
					return e == nil && got.Stream != nil && got.Stream.Phases[0].Chat != ""
				})
				if tc.stop != "" {
					act(tc.stop)
				}
				if err := runner.Stop(item.ID); err != nil {
					t.Fatal(err)
				}
			}
			// Wait for the actual runner to release its round before the service
			// rewinds it. No real model or native worker executes the next round.
			eventually(t, "the runner releases the round", func() bool { return !runner.Running(item.ID) })
			if tc.restart {
				// A lost runner's typed report is the persisted restart boundary.
				// Its wording deliberately says nothing about interruption.
				if err := floor.Update(item.ID, func(it *factory.Item) error {
					it.State = factory.StateRunning
					failure := factory.NewFailure(tc.step, tc.code, "Work was in progress", "", factory.FailureStopped, 1, time.Now())
					return factoryrun.ApplyReport(it, factory.RunReport{Stages: []factory.StageReport{{Name: tc.step, State: factory.PhaseFailed, Round: 1, Failure: &failure}}}, time.Now())
				}); err != nil {
					t.Fatal(err)
				}
				s.backend.Close()
				wire, _ = reopened(t, s.backend.factoryHub())
			}
			legacy := factory.RoundLine{Round: 1, Chat: "earlier-chat", Summary: "Stopped by you", Rejected: true, At: time.Now().Add(-time.Hour)}
			if err := floor.Update(item.ID, func(it *factory.Item) error {
				it.Stream.Phases = append([]factory.Phase{{Name: "earlier", State: factory.PhaseDone, Round: 1, Rounds: []factory.RoundLine{legacy}}}, it.Stream.Phases...)
				it.Stream.Cur = 1
				it.Stages = append([]factory.Stage{{Name: "earlier", Kind: factory.StageChat, On: true}}, it.Stages...)
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before := read()
			if len(before.Steps) != 2 || len(before.Steps[1].Rounds) != 0 {
				t.Fatalf("before retry: %+v", before.Steps)
			}
			wantCode := tc.code
			if tc.stop != "" {
				wantCode = factory.CodeStopped
			}
			if before.Failure == nil || before.Failure.Code != wantCode {
				t.Fatalf("failure before retry = %+v; want code %q", before.Failure, wantCode)
			}
			act("factory.job_step_rerun")
			got := read()
			step := got.Steps[1]
			if len(step.Rounds) != 1 {
				t.Fatalf("rounds = %+v", step.Rounds)
			}
			line := step.Rounds[0]
			if line.Round != 1 || line.Result != tc.result || line.Rejected != tc.rejected {
				t.Errorf("round 1: result=%q rejected=%t; want result=%q rejected=%t", line.Result, line.Rejected, tc.result, tc.rejected)
			}
			if step.Round != 2 || step.RoundLimit != 3 {
				t.Errorf("retry limits: round=%d limit=%d; want 2, 3", step.Round, step.RoundLimit)
			}
			if old := got.Steps[0].Rounds; len(old) != 1 || !old[0].Rejected || old[0].Result != "" || old[0].Summary != legacy.Summary {
				t.Errorf("legacy history changed: %+v", old)
			}
			// Reading again must retain the saved outcome, not reclassify old lines.
			again := read()
			if len(again.Steps[1].Rounds) != 1 || again.Steps[1].Rounds[0] != line {
				t.Errorf("history changed on reread: %+v", again.Steps[1].Rounds)
			}
		})
	}
}
