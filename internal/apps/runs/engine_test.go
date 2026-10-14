package runs

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
)

func TestFollowUpWaitsThenSendsOnApproval(t *testing.T) {
	h := newHarness(t)
	state, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "follow-up"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != bridge.RunWaiting || state.Cost.Cap != 0.5 || !state.Fake || state.Trigger != "manual" {
		t.Fatalf("state = %+v", state)
	}
	send, ok := findStep(state.Steps, "send")
	if !ok || send.Status != bridge.RunWaiting || send.Approval == "" {
		t.Fatalf("send = %+v", send)
	}
	draft, ok := findStep(state.Steps, "draft")
	if !ok || !draft.AI {
		t.Fatalf("draft = %+v", draft)
	}
	h.model.mu.Lock()
	gotModel := h.model.last.Model
	gotPrompt := ""
	if len(h.model.last.Messages) > 0 {
		gotPrompt = h.model.last.Messages[0].Text
	}
	h.model.mu.Unlock()
	if gotModel != "deepseek/deepseek-v4.1-flash" || !strings.Contains(gotPrompt, "Expansion · support team") {
		t.Fatalf("model %q prompt %q", gotModel, gotPrompt)
	}
	list, err := h.inbox.List(h.ctx, h.scope(), bridge.ListApprovalsParams{})
	if err != nil || len(list.Approvals) != 1 || list.Approvals[0].Title != "Approval needed: send email" {
		t.Fatalf("approvals = %+v err %v", list, err)
	}
	if _, err := h.inbox.Decide(h.ctx, h.scope(), bridge.DecideApprovalParams{ApprovalID: send.Approval, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	done, err := h.runs.Get(h.ctx, h.scope(), bridge.GetRunParams{RunID: state.ID})
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != bridge.RunDone {
		t.Fatalf("after yes = %+v", done)
	}
	h.eng.mu.Lock()
	var sawSend bool
	for _, call := range h.eng.calls {
		if call.Tool == "gmail.send" && call.Args["subject"] == "Hi" {
			sawSend = true
		}
	}
	h.eng.mu.Unlock()
	if !sawSend {
		t.Fatal("gmail.send was not recorded")
	}
	row, err := h.recs.Get(h.ctx, h.scope(), bridge.GetRecordParams{Type: "deal", ID: "dl_umbrella"})
	if err != nil || row.Fields["last_touch"] == "" {
		t.Fatalf("last_touch = %+v err %v", row.Fields, err)
	}
}

func TestBudgetRefusesBeforeStart(t *testing.T) {
	h := newHarness(t)
	h.bundle.App.Budget.Month = 0.40
	_, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "follow-up"})
	var typed *bridge.Error
	if !errors.As(err, &typed) || typed.Code != bridge.CodeBudget || typed.Message != "up to $0.50" {
		t.Fatalf("err = %v", err)
	}
}

func TestAIOverCapNeedsYou(t *testing.T) {
	h := newHarness(t)
	h.model.cost = 0.25
	h.bundle.Runs["spend"] = &spec.Run{
		Run: "spend", Budget: &spec.RunBudget{PerRun: 0.10},
		Steps: []*spec.Step{{ID: "ask", Agent: "CRM agent", Prompt: "Hi", Output: map[string]any{"type": "object"}}},
	}
	state, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "spend"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != bridge.RunNeedsYou || state.Error != "up to $0.10" || state.Cost.Spent != 0.25 {
		t.Fatalf("state = %+v", state)
	}
}

func TestDeclinedApprovalStopsBeforeSend(t *testing.T) {
	h := newHarness(t)
	state, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "follow-up"})
	if err != nil {
		t.Fatal(err)
	}
	send, _ := findStep(state.Steps, "send")
	if _, err := h.inbox.Decide(h.ctx, h.scope(), bridge.DecideApprovalParams{ApprovalID: send.Approval, Answer: "no"}); err != nil {
		t.Fatal(err)
	}
	got, _ := h.runs.Get(h.ctx, h.scope(), bridge.GetRunParams{RunID: state.ID})
	if got.Status != bridge.RunStopped {
		t.Fatalf("status = %s", got.Status)
	}
	h.eng.mu.Lock()
	defer h.eng.mu.Unlock()
	for _, call := range h.eng.calls {
		if call.Tool == "gmail.send" {
			t.Fatal("send ran after a no")
		}
	}
}

func TestOutboundWithoutGateStillAsks(t *testing.T) {
	h := newHarness(t)
	h.bundle.Runs["ping"] = &spec.Run{Run: "ping", Steps: []*spec.Step{{
		ID: "send", Tool: "gmail.send", Args: map[string]any{"subject": "Hi"},
	}}}
	state, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "ping"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != bridge.RunWaiting {
		t.Fatalf("status = %s", state.Status)
	}
	list, err := h.inbox.List(h.ctx, h.scope(), bridge.ListApprovalsParams{})
	if err != nil || len(list.Approvals) != 1 || list.Approvals[0].Title != "Approval needed: send email" {
		t.Fatalf("approvals = %+v %v", list, err)
	}
}

func TestSearchDoesNotAsk(t *testing.T) {
	h := newHarness(t)
	h.bundle.Runs["read"] = &spec.Run{Run: "read", Steps: []*spec.Step{{
		ID: "find", Tool: "gmail.read", Args: map[string]any{"q": "newer_than:30d"},
	}}}
	state, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "read"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != bridge.RunDone {
		t.Fatalf("status = %s err %s", state.Status, state.Error)
	}
	if _, err := h.tools.Call(h.ctx, h.scope(), "gmail.send", map[string]any{}); err == nil {
		t.Fatal("send without approval")
	}
}

func TestRetryThenSuccess(t *testing.T) {
	h := newHarness(t)
	h.model.errs = []error{errors.New("temporary"), errors.New("temporary")}
	h.bundle.Runs["flaky"] = &spec.Run{Run: "flaky", Steps: []*spec.Step{{
		ID: "ask", Agent: "CRM agent", Prompt: "Hi",
		Retry: &spec.Retry{Max: 2, Backoff: "1s"},
	}}}
	state, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "flaky"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != bridge.RunDone || h.model.calls != 3 {
		t.Fatalf("status %s calls %d", state.Status, h.model.calls)
	}
}

func TestOutOfRounds(t *testing.T) {
	h := newHarness(t)
	h.bundle.Runs["spin"] = &spec.Run{Run: "spin", Steps: []*spec.Step{{
		ID: "again", Loop: &spec.Loop{Until: "false", Max: 2},
		Steps: []*spec.Step{{ID: "noop", If: "false", Agent: "CRM agent", Prompt: "x"}},
	}}}
	state, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "spin"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != bridge.RunNeedsYou || state.Error != "out of rounds" {
		t.Fatalf("state = %+v", state)
	}
}

func TestWaitResumesWhenDue(t *testing.T) {
	h := newHarness(t)
	h.bundle.Runs["pause"] = &spec.Run{Run: "pause", Steps: []*spec.Step{{
		ID: "pause", Wait: &spec.Wait{For: "1h"},
	}}}
	state, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "pause"})
	if err != nil || state.Status != bridge.RunWaiting {
		t.Fatalf("state %+v err %v", state, err)
	}
	h.clock.set(h.clock.get().Add(2 * time.Hour))
	h.eng.drive(h.ctx, "crm", state.ID)
	got, err := h.runs.Get(h.ctx, h.scope(), bridge.GetRunParams{RunID: state.ID})
	if err != nil || got.Status != bridge.RunDone {
		t.Fatalf("after wait %+v err %v", got, err)
	}
}

func TestScheduleFiresOncePerMinute(t *testing.T) {
	h := newHarness(t)
	h.bundle.App.Triggers = append(h.bundle.App.Triggers, spec.Trigger{
		ID: "nightly", On: spec.TriggerSchedule, Run: "pause", Cron: "* * * * *",
	})
	h.bundle.Runs["pause"] = &spec.Run{Run: "pause", Steps: []*spec.Step{{ID: "pause", Wait: &spec.Wait{For: "1h"}}}}
	h.eng.pump(h.ctx)
	h.eng.pump(h.ctx)
	list, err := h.runs.List(h.ctx, h.scope(), bridge.ListRunsParams{Run: "pause"})
	if err != nil || len(list.Runs) != 1 || list.Runs[0].Trigger != "nightly" {
		t.Fatalf("runs = %+v err %v", list, err)
	}
	h.clock.set(h.clock.get().Add(time.Minute))
	h.eng.pump(h.ctx)
	list, _ = h.runs.List(h.ctx, h.scope(), bridge.ListRunsParams{Run: "pause"})
	if len(list.Runs) != 2 {
		t.Fatalf("after a minute = %d", len(list.Runs))
	}
}

func TestRecordEventOnTheHubStartsTheRun(t *testing.T) {
	h := newHarness(t)
	h.eng.mu.Lock()
	h.eng.started = make(chan string, 1)
	h.eng.mu.Unlock()
	if _, err := h.recs.Update(h.ctx, h.scope(), bridge.UpdateRecordParams{
		Type: "deal", ID: "dl_hooli", Set: map[string]any{"stage": "Proposal"},
	}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.eng.started:
	case <-time.After(time.Second):
		t.Fatal("record event did not start a run")
	}
}

func TestRecordChangeStartsTheRun(t *testing.T) {
	h := newHarness(t)
	row, err := h.recs.Get(h.ctx, h.scope(), bridge.GetRecordParams{Type: "deal", ID: "dl_hooli"})
	if err != nil {
		t.Fatal(err)
	}
	row.Fields["stage"] = "Proposal"
	row.Version++
	h.eng.handleRecord(h.ctx, "crm", bridge.RecordDiff{
		Op: bridge.DiffUpsert, Type: "deal", ID: row.ID, Record: &row, By: svc.Owner,
	})
	list, err := h.runs.List(h.ctx, h.scope(), bridge.ListRunsParams{For: "dl_hooli"})
	if err != nil || len(list.Runs) != 1 || list.Runs[0].Trigger != "proposal-sent" {
		t.Fatalf("runs = %+v err %v", list, err)
	}
}

func TestRestartResumesAWaitingRun(t *testing.T) {
	h := newHarness(t)
	state, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "follow-up"})
	if err != nil {
		t.Fatal(err)
	}
	send, _ := findStep(state.Steps, "send")
	h.reopen(t)
	got, err := h.runs.Get(h.ctx, h.scope(), bridge.GetRunParams{RunID: state.ID})
	if err != nil || got.Status != bridge.RunWaiting {
		t.Fatalf("reloaded %+v err %v", got, err)
	}
	if _, err := h.inbox.Decide(h.ctx, h.scope(), bridge.DecideApprovalParams{ApprovalID: send.Approval, Answer: "yes"}); err != nil {
		t.Fatal(err)
	}
	done, err := h.runs.Get(h.ctx, h.scope(), bridge.GetRunParams{RunID: state.ID})
	if err != nil || done.Status != bridge.RunDone {
		t.Fatalf("resumed %+v err %v", done, err)
	}
}

func TestShellIsRefused(t *testing.T) {
	h := newHarness(t)
	h.bundle.Runs["sh"] = &spec.Run{Run: "sh", Steps: []*spec.Step{{
		ID: "test", Workspace: "repo", Shell: "go test",
	}}}
	state, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "sh"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != bridge.RunFailed || !strings.Contains(state.Error, "shell") {
		t.Fatalf("state = %+v", state)
	}
}

func TestOnErrorContinue(t *testing.T) {
	h := newHarness(t)
	h.model.errs = []error{errors.New("nope")}
	h.bundle.Runs["keep"] = &spec.Run{Run: "keep", Steps: []*spec.Step{
		{ID: "ask", Agent: "CRM agent", Prompt: "Hi", OnError: "continue"},
		{ID: "find", Tool: "gmail.read"},
	}}
	state, err := h.runs.Start(h.ctx, h.scope(), bridge.StartRunParams{Run: "keep"})
	if err != nil {
		t.Fatal(err)
	}
	if state.Status != bridge.RunDone {
		t.Fatalf("status %s err %s", state.Status, state.Error)
	}
	ask, ok := findStep(state.Steps, "ask")
	if !ok || ask.Status != bridge.RunFailed {
		t.Fatalf("ask = %+v", ask)
	}
}
