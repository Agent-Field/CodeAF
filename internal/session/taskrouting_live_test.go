package session

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// A ROLE HAS STARTED WHEN ITS CALL LEAVES, not when the provider answers:
// with the worker's first call held open on a provider shared by every role,
// the worker reads used and the checker on the same model not run yet; then
// the checker's call starts and only the checker moves.
func TestARoleStartsWhenItsCallLeaves(t *testing.T) {
	arrived, release := make(chan struct{}, 4), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !answersChatOnly(w, r) {
			return
		}
		arrived <- struct{}{}
		<-release
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"model": "flash", "choices": []any{map[string]any{
			"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": "done"}}}})
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		defer func() { _ = recover() }()
		close(release)
	})
	agent, _ := crewStubAgent(t, &crewStub{Server: srv})
	agent.config.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) { return flashDecision(), nil }
	crew, err := agent.routeTaskCrew(t.Context(), 1, "fix the parser", "it crashes")
	if err != nil {
		t.Fatal(err)
	}
	run := &beltRun{row: 1, crew: crew}
	done := make(chan error, 2)
	call := func(seat crewroute.Seat) {
		ctx := context.WithValue(context.Background(), crewSeatContextKey{}, seat)
		_, err := (crewSeatCompleter{agent: agent, run: run}).CompleteWithMessages(ctx, []ai.Message{textMessage("user", "go")}, ai.WithModel("flash"))
		done <- err
	}
	go call(crewroute.Worker)
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker's call never reached the provider")
	}
	got := statuses(taskRoutingOf("1", crew.routingRecord(20), false, 20))
	if got[crewroute.Worker] != TaskRoleUsed || got[crewroute.Checker] != TaskRoleNotRunYet || got[crewroute.Planner] != TaskRoleNotRunYet {
		t.Fatalf("worker call in flight: %v", got)
	}
	go call(crewroute.Checker)
	select {
	case <-arrived:
	case <-time.After(10 * time.Second):
		t.Fatal("the checker's call never reached the provider")
	}
	got = statuses(taskRoutingOf("1", crew.routingRecord(20), false, 20))
	if got[crewroute.Checker] != TaskRoleUsed || got[crewroute.Planner] != TaskRoleNotRunYet {
		t.Fatalf("checker call in flight: %v", got)
	}
	close(release)
	for range 2 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
}

// A call the task cap refuses before it leaves never starts its role.
func TestARefusedCallDoesNotStartItsRole(t *testing.T) {
	stub := newCrewStub(t, http.StatusOK, map[string]int{})
	agent, _ := crewStubAgent(t, stub)
	agent.config.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) { return flashDecision(), nil }
	crew, err := agent.routeTaskCrew(t.Context(), 1, "fix the parser", "it crashes")
	if err != nil {
		t.Fatal(err)
	}
	crew.guard.TaskCap = 0.01
	crew.guard.Task = &SpendTask{spent: 0.02}
	ctx := context.WithValue(t.Context(), crewSeatContextKey{}, crewroute.Checker)
	_, err = (crewSeatCompleter{agent: agent, run: &beltRun{row: 1, crew: crew}}).CompleteWithMessages(ctx, []ai.Message{textMessage("user", "go")}, ai.WithModel("flash"))
	var stopped crewStopped
	if !errors.As(err, &stopped) {
		t.Fatalf("the cap did not stop the call: %v", err)
	}
	if got := statuses(taskRoutingOf("1", crew.routingRecord(20), false, 20)); got[crewroute.Checker] != TaskRoleNotRunYet {
		t.Fatalf("a refused call started the checker: %v", got)
	}
	if len(stub.models()) != 0 {
		t.Fatalf("a refused call reached the provider: %v", stub.models())
	}
}

// HELPERS ARE IN THE TASK'S TALLY ONCE: a helper call is booked on the task
// tally it shares with the roles, and its breakout is never added again —
// known cost, unknown cost and free alike, before and after a reload.
func TestHelperSpendIsCountedOnce(t *testing.T) {
	a := &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir()}, model: "chat/model"}
	crew := liveCrew(0)
	crew.guard.after(seatCtx(crewroute.Worker), "glm-flash", usage(usd(0.05)), 0)
	helper := a.helperGuard(crew)
	known := usage(usd(0.03))
	helper.after(context.Background(), "helper/model", known, 0)
	crew.addHelperSpend(known)
	helper.after(context.Background(), "helper/unpriced", &ai.Response{}, 0)
	crew.addHelperSpend(&ai.Response{})
	check := func(label string, view TaskRoutingView) {
		t.Helper()
		if !view.SpentKnown || view.SpentUSD < 0.0799 || view.SpentUSD > 0.0801 || view.HelperUSD != 0.03 || view.Calls != 3 || view.UnknownCalls != 1 {
			t.Fatalf("%s: spent %v (known %v) helper %v calls %d unknown %d", label, view.SpentUSD, view.SpentKnown, view.HelperUSD, view.Calls, view.UnknownCalls)
		}
	}
	check("live", taskRoutingOf("1", crew.routingRecord(20), false, 20))
	record := crew.record()
	record.Routed = true
	check("checkpoint", taskRoutingOf("1", record, false, 20))
	back, err := a.restoreTaskCrew(1, record)
	if err != nil {
		t.Fatal(err)
	}
	check("reload", taskRoutingOf("1", back.routingRecord(20), false, 20))
	// A record that kept no task tally does not claim a spend.
	record.Guard = nil
	if view := taskRoutingOf("1", record, true, 20); view.SpentKnown {
		t.Fatalf("no tally, yet spent %v is claimed", view.SpentUSD)
	}
}

// A role's first booking racing a read: every read sees the task figure and
// exactly the roles it includes.
func TestABoundedReadSeesANewRoleWithItsBooking(t *testing.T) {
	for range 50 {
		crew := liveCrew(0)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, seat := range crewroute.Seats {
				crew.guard.after(seatCtx(seat), "m", usage(usd(0.25)), 0)
			}
		}()
		for i := 0; i < 20; i++ {
			g := crew.routingRecord(20).Guard
			sum := 0.0
			for _, v := range g.SeatSpent {
				sum += v
			}
			if sum != g.TaskSpent {
				t.Fatalf("task %v but roles sum to %v", g.TaskSpent, sum)
			}
		}
		wg.Wait()
	}
}
