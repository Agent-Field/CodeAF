package session

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

func flashDecision() crewroute.Decision {
	d := crewroute.Decision{Mode: crewroute.ModeAuto}
	for _, seat := range crewroute.Seats {
		d.Crew = append(d.Crew, crewroute.Pick{Seat: seat, Model: "flash", Send: "flash", Provider: "openrouter", Kind: crewroute.Metered, EstUSD: 0.01,
			Price: &crewroute.RoutePrice{Prompt: 1e-7, Completion: 4e-7}})
	}
	return d
}

func statuses(v TaskRoutingView) map[crewroute.Seat]string {
	out := map[crewroute.Seat]string{}
	for _, r := range v.Roles {
		out[r.Seat] = r.Status
	}
	return out
}

// ONE MODEL IN EVERY ROLE, THE WORKER ALONE HAS RUN: only the worker reads as
// used — through the real seat call, marked by the role that made it — the
// checker is not run yet while the task runs and not used once it settled
// without running, and a later checker call marks the checker alone. The
// facts survive a checkpoint and a restart.
func TestASharedModelMarksOnlyTheRoleThatRan(t *testing.T) {
	stub := newCrewStub(t, http.StatusOK, map[string]int{})
	stub.cost = 0.002
	agent, _ := crewStubAgent(t, stub)
	agent.config.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) { return flashDecision(), nil }
	crew, err := agent.routeTaskCrew(t.Context(), 1, "fix the parser", "it crashes")
	if err != nil {
		t.Fatal(err)
	}
	run := &beltRun{row: 1, crew: crew}
	call := func(seat crewroute.Seat) {
		t.Helper()
		ctx := context.WithValue(t.Context(), crewSeatContextKey{}, seat)
		if _, err := (crewSeatCompleter{agent: agent, run: run}).CompleteWithMessages(ctx, []ai.Message{textMessage("user", "go")}, ai.WithModel("flash")); err != nil {
			t.Fatalf("%s call: %v (asked %v)", seat, err, stub.models())
		}
	}
	call(crewroute.Worker)
	got := statuses(taskRoutingOf("1", crew.routingRecord(20), false, 20))
	want := map[crewroute.Seat]string{crewroute.Worker: TaskRoleUsed, crewroute.Planner: TaskRoleNotRunYet, crewroute.Checker: TaskRoleNotRunYet}
	for seat, w := range want {
		if got[seat] != w {
			t.Fatalf("worker only: %s is %s, want %s", seat, got[seat], w)
		}
	}
	if got := statuses(taskRoutingOf("1", crew.routingRecord(20), true, 20)); got[crewroute.Checker] != TaskRoleNotUsed {
		t.Fatalf("settled without a checker call: %s", got[crewroute.Checker])
	}
	call(crewroute.Checker)
	view := taskRoutingOf("1", crew.routingRecord(20), false, 20)
	got = statuses(view)
	if got[crewroute.Checker] != TaskRoleUsed || got[crewroute.Planner] != TaskRoleNotRunYet {
		t.Fatalf("after the checker: %v", got)
	}
	if view.Roles[1].SpentUSD != 0 || !view.Roles[1].SpentKnown || view.Roles[0].SpentUSD <= 0 || view.Roles[2].SpentUSD <= 0 {
		t.Fatalf("role spend %+v", view.Roles)
	}
	if p := view.Roles[0].Price; p == nil || p.Prompt != 1e-7 {
		t.Fatalf("the admitted tariff was not kept: %+v", p)
	}

	// Checkpoint and restart keep the per-role facts.
	record := crew.record()
	record.Routed = true
	back, err := (&Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir()}}).restoreTaskCrew(1, record)
	if err != nil {
		t.Fatal(err)
	}
	got = statuses(taskRoutingOf("1", back.routingRecord(20), true, 20))
	if got[crewroute.Worker] != TaskRoleUsed || got[crewroute.Checker] != TaskRoleUsed || got[crewroute.Planner] != TaskRoleNotUsed {
		t.Fatalf("after reload: %v", got)
	}
}

// AN OLDER CHECKPOINT KEPT NO PER-ROLE FACTS: a model-wide start never makes
// a role used, a role with nothing recorded is unknown — not unused, and not a
// proven $0 — and a role spend it did record is shown as recorded.
func TestAnOlderCheckpointInventsNoRoleFacts(t *testing.T) {
	var legacy TaskCrewRecord
	raw := `{"version":1,"routed":true,"Decision":{"Crew":[{"Seat":"worker","Model":"flash","Send":"flash"},{"Seat":"planner","Model":"flash","Send":"flash"},{"Seat":"checker","Model":"flash","Send":"flash"}]},
		"Original":{"worker":"flash","planner":"flash","checker":"flash"},"Started":{"flash":true},
		"Guard":{"TaskSpent":0.03,"SeatSpent":{"worker":0.03}}}`
	if err := json.Unmarshal([]byte(raw), &legacy); err != nil {
		t.Fatal(err)
	}
	view := taskRoutingOf("1", &legacy, true, 20)
	if view.RoleFacts {
		t.Fatal("an older checkpoint claimed per-role facts")
	}
	got := statuses(view)
	if got[crewroute.Worker] != TaskRoleUsed || got[crewroute.Planner] != TaskRoleUnknown || got[crewroute.Checker] != TaskRoleUnknown {
		t.Fatalf("legacy roles %v", got)
	}
	if view.Roles[1].SpentKnown || !view.Roles[0].SpentKnown {
		t.Fatalf("legacy spend certainty %+v", view.Roles)
	}
	if view.Roles[0].Price != nil {
		t.Fatal("an older pick was given a tariff")
	}
}

// One task's read finds its row where it is kept: its cost does not grow with
// every other task in the conversation.
func TestOneTaskReadDoesNotCopyTheWholeGraph(t *testing.T) {
	read := func(rows int) float64 {
		a, _ := newTestAgent(t, &recoveryModelRecorder{}, nil)
		g := a.graph()
		g.mu.Lock()
		if g.runs == nil {
			g.runs = map[uint64][]TaskNotice{}
		}
		for i := 1; i <= rows; i++ {
			id := uint64(i)
			g.runRuns = append(g.runRuns, id)
			g.runs[id] = []TaskNotice{{ID: id, Title: "t", State: TaskDone, Model: "flash"}}
		}
		g.mu.Unlock()
		last := uint64(rows)
		return testing.AllocsPerRun(20, func() {
			if _, ok := a.appRunRow(last); !ok {
				t.Fatal("row not found")
			}
		})
	}
	small, large := read(10), read(4000)
	if large > small || large > 2 {
		t.Fatalf("a row lookup allocates %v with 4000 tasks, %v with 10", large, small)
	}
}

// Change models… is the engine's own preview, by the same policy as the
// change: Stronger names the worker for Auto this once and shows the model
// and tariff it would move to; Best names all three and shows the cap it
// would meet; neither moves anything.
func TestTheOptionsPreviewIsTheEnginesOwnAndMovesNothing(t *testing.T) {
	var asks []config.CrewAsk
	a := routerAgent(t, func(ask config.CrewAsk) (crewroute.Decision, error) {
		asks = append(asks, ask)
		d := liveDecision()
		for i := range d.Crew {
			if ask.AutoSeats[d.Crew[i].Seat] {
				d.Crew[i] = crewroute.Pick{Seat: d.Crew[i].Seat, Model: "sonnet", Send: "sonnet", Provider: "openrouter", Kind: crewroute.Metered, EstUSD: 1.0,
					Price: &crewroute.RoutePrice{Prompt: 3e-6, Completion: 15e-6}}
			}
		}
		return d, nil
	})
	crew := liveCrew(2)
	stronger := a.livePreview(crew, TaskRoutingStronger)
	if stronger.Refusal != nil || len(stronger.Moves) != 1 || stronger.Moves[0].To != "sonnet" || stronger.Moves[0].From != "glm-flash" || stronger.Moves[0].ToPrice.Prompt != 3e-6 {
		t.Fatalf("stronger preview %+v", stronger)
	}
	if !asks[0].AutoSeats[crewroute.Worker] || asks[0].AutoSeats[crewroute.Checker] || asks[0].Sends[crewroute.Checker] != "ds-flash" {
		t.Fatalf("stronger asked %+v", asks[0])
	}
	best := a.livePreview(crew, TaskRoutingBest)
	if !best.OverCap || best.EstimateUSD == nil || *best.EstimateUSD != 3 || best.CapUSD != 2 || len(best.Moves) != 3 {
		t.Fatalf("best preview %+v", best)
	}
	if len(asks[1].AutoSeats) != 3 {
		t.Fatalf("best asked %+v", asks[1].AutoSeats)
	}
	if crew.changeCount != 0 || crew.sendForSeat(crewroute.Worker, "glm-flash") != "glm-flash" {
		t.Fatal("a preview moved the crew")
	}
	// Back to Auto names no role for Auto: the saved policy comes back.
	crew.taskPins = map[crewroute.Seat]bool{crewroute.Checker: true}
	if _, err := a.actOnLiveCrew(1, crew, TaskRoutingAction{Kind: TaskRoutingAuto}); err != nil {
		t.Fatal(err)
	}
	if last := asks[len(asks)-1]; len(last.AutoSeats) != 0 {
		t.Fatalf("back to auto asked %+v", last.AutoSeats)
	}
}
