package session

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestStageFinalityUsesTheToolArgumentDecoder(t *testing.T) {
	a := stageAgent(t, &fakeStageDoor{})
	for _, args := range []string{
		`{"done":true,"findings":0.0}`,
		`{"done":true,"claims":"[{\"text\":\"checked\",\"ok\":true,\"evidence\":\"receipt\"}]"}`,
	} {
		r, invalid := parseStageResult(json.RawMessage(args))
		if invalid != "" || r.Checkpoint {
			t.Fatalf("accepted final report lost intent: %+v %s", r, invalid)
		}
		calls := []ai.ToolCall{{Function: ai.ToolCallFunction{Name: stageResultToolName, Arguments: args}}}
		if !a.stageBatchFinal(calls, []toolResult{{}}) {
			t.Fatalf("accepted report did not end turn: %s", args)
		}
	}
	checkpoint, invalid := parseStageResult(json.RawMessage(`{"done":true,"final":false}`))
	if invalid != "" || !checkpoint.Checkpoint {
		t.Fatal("explicit checkpoint became final")
	}
	raw, _ := json.Marshal(checkpoint)
	if strings.Contains(string(raw), "Checkpoint") {
		t.Fatal("runtime checkpoint leaked into saved result")
	}
	calls := []ai.ToolCall{{Function: ai.ToolCallFunction{Name: stageResultToolName}}, {Function: ai.ToolCallFunction{Name: stageResultToolName}}}
	if a.stageReportBatchFault(calls) == "" {
		t.Fatal("parallel lifecycle reports accepted")
	}
}

func TestStageOwnedChecksRemainLiveUntilTheirEndingIsDelivered(t *testing.T) {
	a := stageAgent(t, &fakeStageDoor{})
	a.jobs = &jobRegistry{stage: true}
	j := &job{stageOwned: true, kind: jobKindBash, done: make(chan struct{}), started: time.Now(), command: "go test ./..."}
	a.jobs.jobs = []*job{j}
	if !a.StageStillGoing() || a.SettleStage() || len(a.StageChecks()) != 1 {
		t.Fatal("required check was not live")
	}
	close(j.done)
	if !a.StageStillGoing() {
		t.Fatal("process exit preceded ending delivery but stage settled")
	}
	j.mu.Lock()
	j.stageDelivered = true
	j.mu.Unlock()
	if a.StageStillGoing() || !a.SettleStage() {
		t.Fatal("delivered check kept stage alive")
	}
	a.mu.Lock()
	fenced := a.stageSettled
	woke := a.wakeLocked()
	a.mu.Unlock()
	if !fenced || woke {
		t.Fatal("settled stage allowed an unobserved self-wake")
	}
	a.ReopenStage()
	a.mu.Lock()
	fenced = a.stageSettled
	a.mu.Unlock()
	if fenced {
		t.Fatal("runner continuation could not reopen stage")
	}
	a.jobs.jobs = []*job{{kind: jobKindWatch, done: make(chan struct{})}, {kind: jobKindBash, command: "dev-server", done: make(chan struct{})}}
	if a.StageStillGoing() {
		t.Fatal("auxiliary server or watch held stage completion")
	}
}

func TestStageParkWaitsForPromotedChecksWithoutProviderPolling(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "release")
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nprintf 'started\\n'\nwhile [ ! -f %q ]; do sleep 0.01; done\nprintf 'PASS\\n'\n", release)
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	var second atomic.Bool
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("run", "bash", `{"command":"go test ./..."}`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			second.Store(true)
			if _, err := os.Stat(release); err != nil {
				t.Error("model was polled before required check finished")
			}
			return toolResponse("final", "stage_result", `{"done":true}`), nil
		},
	}}
	a := stageTestAgent(t, completer, &fakeStageDoor{})
	a.config.BashBackgroundAfterSeconds = 1
	a.config.bashAfter = func(_ time.Duration, fire func()) *time.Timer { return time.AfterFunc(0, fire) }
	a.ArmStageJobPark(time.Minute)
	events, err := a.Submit(context.Background(), "run the check")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0600) })
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	promoted := false
	for !promoted {
		select {
		case e, ok := <-events:
			if !ok {
				t.Fatal("turn ended before promotion")
			}
			promoted = strings.Contains(e.Output, BashPromotedLead)
		case <-deadline.C:
			t.Fatal("check did not promote")
		}
	}
	if second.Load() {
		t.Fatal("provider request preceded check release")
	}
	if !a.StageHasChecks() {
		t.Fatal("promoted check has no durable stage ownership")
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	collect(t, events)
	if !second.Load() {
		t.Fatal("check completion did not resume the parked turn")
	}
}

func TestSteeringAStageReleasesItsParkWithoutDetachingTheCheck(t *testing.T) {
	root := t.TempDir()
	release := filepath.Join(root, "release")
	bin := filepath.Join(root, "bin")
	if err := os.Mkdir(bin, 0700); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf("#!/bin/sh\nwhile [ ! -f %q ]; do sleep 0.01; done\necho PASS\n", release)
	if err := os.WriteFile(filepath.Join(bin, "go"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	sawSteer := make(chan struct{})
	completer := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("check", "bash", `{"command":"go test ./..."}`), nil
		},
		func(_ context.Context, messages []ai.Message) (*ai.Response, error) {
			var found bool
			for _, m := range messages {
				if m.Role == "user" && strings.Contains(messageContentText(m), "inspect the failure context") {
					found = true
				}
			}
			if !found {
				t.Error("park resumed without delivering the person's instruction")
			}
			close(sawSteer)
			return textResponse("Instruction received; the check continues."), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("final", "stage_result", `{"done":true}`), nil
		},
	}}
	a := stageTestAgent(t, completer, &fakeStageDoor{})
	a.config.BashBackgroundAfterSeconds = 1
	a.config.bashAfter = func(_ time.Duration, fire func()) *time.Timer { return time.AfterFunc(0, fire) }
	a.ArmStageJobPark(time.Minute)
	wakes, stop := a.WatchWakes()
	defer stop()
	events, err := a.Submit(context.Background(), "run check")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.WriteFile(release, nil, 0600) })
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case e := <-events:
			if strings.Contains(e.Output, BashPromotedLead) {
				goto promoted
			}
		case <-deadline.C:
			t.Fatal("check did not promote")
		}
	}
promoted:
	steered, err := a.Steer("inspect the failure context")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range steered {
		}
	}()
	select {
	case <-sawSteer:
	case <-deadline.C:
		t.Fatal("person's steer remained behind a parked check")
	}
	collect(t, events)
	if !a.StageHasChecks() || !a.StageStillGoing() {
		t.Fatal("steering detached a required check")
	}
	if err := os.WriteFile(release, nil, 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case w := <-wakes:
		collect(t, w)
	case <-deadline.C:
		t.Fatal("check completion failed to wake the stage")
	}
}

type withdrawingStageDoor struct {
	fakeStageDoor
	rejected bool
}

func (d *withdrawingStageDoor) RejectReport() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.rejected = true
	if len(d.reports) > 0 {
		d.reports[len(d.reports)-1].Checkpoint = true
	}
}
func TestMalformedStageReportWithdrawsEarlierCompletion(t *testing.T) {
	door := &withdrawingStageDoor{}
	a := stageAgent(t, door)
	tool := a.stageResultTool()
	if _, bad := callStageTool(t, tool, `{"done":true}`); bad {
		t.Fatal("valid report refused")
	}
	if _, bad := callStageTool(t, tool, `{"claims":[]}`); !bad || !door.rejected {
		t.Fatal("malformed newer report left previous completion accepted")
	}
	if len(door.reports) != 1 || !door.reports[0].Done {
		t.Fatal("prior evidence discarded")
	}
}

func TestNewStageTurnRequiresItsOwnFinalAssessment(t *testing.T) {
	door := &withdrawingStageDoor{}
	if err := door.Report(context.Background(), factory.StageResult{Done: true, Output: "old completion"}); err != nil {
		t.Fatal(err)
	}
	model := &scriptedCompleter{steps: []step{
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return textResponse("More work ended without a new final assessment."), nil
		},
		func(context.Context, []ai.Message) (*ai.Response, error) {
			return toolResponse("new-final", "stage_result", `{"done":true,"final":true,"output":"new completion"}`), nil
		},
	}}
	a := stageTestAgent(t, model, door)
	first, err := a.Submit(context.Background(), "wake after prior completion")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, first)
	door.mu.Lock()
	staleWithdrawn := len(door.reports) == 1 && door.reports[0].Checkpoint
	door.mu.Unlock()
	if !staleWithdrawn {
		t.Fatal("new stage work reused a prior terminal assessment")
	}
	second, err := a.Submit(context.Background(), "finish the current assessment")
	if err != nil {
		t.Fatal(err)
	}
	collect(t, second)
	door.mu.Lock()
	defer door.mu.Unlock()
	last := door.reports[len(door.reports)-1]
	if len(door.reports) != 2 || last.Checkpoint || !last.Done || last.Output != "new completion" {
		t.Fatalf("new valid final assessment was invalidated by its own turn: %+v", last)
	}
}
