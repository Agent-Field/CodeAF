package session

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/service"
	"github.com/Agent-Field/codeaf/internal/service/servicetest"
)

// repairWorld is keepWorld on a clock the test moves, so a repaired service's
// first check can be had without waiting ten seconds.
func repairWorld(t *testing.T) (*service.Supervisor, *servicetest.Runtime, *servicetest.Clock) {
	t.Helper()
	rt := servicetest.New()
	clock := servicetest.NewClock(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	supervisor, err := service.Open(service.Options{Dir: t.TempDir(), Runtime: rt, Clock: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(supervisor.Close)
	return supervisor, rt, clock
}

// conflictedRow keeps the web dev server onto a port another program holds,
// through a real card, and answers its failed row.
func conflictedRow(t *testing.T, agent *Agent, supervisor *service.Supervisor, rt *servicetest.Runtime) service.Row {
	t.Helper()
	rt.PortsUsed[5173] = true
	events, err := agent.Submit(context.Background(), "start the web dev server and keep it up")
	if err != nil {
		t.Fatal(err)
	}
	drainAnsweringKeep(t, agent, events, KeepAllowKey, nil)
	rows := supervisor.List(agent.keepOwner())
	if len(rows) != 1 || !rows[0].PortConflict || rows[0].State != service.StateFailed || !rows[0].EpisodeOpen {
		t.Fatalf("the port conflict is one failed row: %+v", rows)
	}
	return rows[0]
}

func cardsIn(events []Event) []*KeepNotice {
	var cards []*KeepNotice
	for _, event := range events {
		if event.Kind == EventKeepRunning && event.Keep != nil && event.Keep.Decided == "" {
			cards = append(cards, event.Keep)
		}
	}
	return cards
}

// TestAFailedServiceIsRepairedOnItsOwnRow is the agent fixing a port conflict
// the way a person would: the changed command is asked about afresh, and the
// yes runs it on the SAME row, under the original id, whose one episode closes
// when the repaired service first passes its check.
func TestAFailedServiceIsRepairedOnItsOwnRow(t *testing.T) {
	supervisor, rt, clock := repairWorld(t)
	var mu sync.Mutex
	var heard []service.Event
	supervisor.Subscribe(func(e service.Event) { mu.Lock(); heard = append(heard, e); mu.Unlock() })
	completer := &scriptedCompleter{steps: []step{
		keepCall("k1", "npm run dev", map[string]any{"name": "web dev server", "health": map[string]any{"port": 5173}}),
		finalText("port is taken"),
		keepCall("k2", "npm run dev -- --port 5174", map[string]any{"name": "web dev server", "health": map[string]any{"port": 5174}, "service_id": "s1"}),
		finalText("moved it"),
	}}
	agent := keepAgent(t, completer, supervisor, "api-docs")
	failed := conflictedRow(t, agent, supervisor, rt)

	events, _ := agent.Submit(context.Background(), "use another port")
	collected := drainAnsweringKeep(t, agent, events, KeepAllowKey, nil)
	cards := cardsIn(collected)
	if len(cards) != 1 || cards[0].Replaces != failed.ID || cards[0].Command != "npm run dev -- --port 5174" ||
		!strings.Contains(KeepHead(*cards[0]), "in place of "+failed.ID+" (web dev server)") {
		t.Fatalf("the changed command was not asked about afresh, naming what it replaces: %+v", cards)
	}
	rows := supervisor.List("api-docs")
	if len(rows) != 1 || rows[0].ID != failed.ID || !rows[0].Running || rows[0].Health.Port != 5174 || rows[0].Episode != failed.Episode {
		t.Fatalf("the repair is the one original row: %+v", rows)
	}
	if rt.Starts() != 1 {
		t.Fatalf("started %d", rt.Starts())
	}
	if text := toolResultText(t, agent, "k2"); !strings.Contains(text, "Replaced its command and kept it running as "+failed.ID) {
		t.Fatalf("the model was told %q", text)
	}

	clock.Advance(service.CheckEvery)
	deadline := time.Now().Add(5 * time.Second)
	for {
		row, _ := supervisor.Get("api-docs", failed.ID)
		if row.State == service.StateHealthy {
			if row.EpisodeOpen || row.Episode != failed.Episode || row.EpisodeResolvedAt == nil {
				t.Fatalf("the original episode did not close on the healthy row: %+v", row)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the repaired service never passed its check: %+v", row)
		}
		time.Sleep(time.Millisecond)
	}
	supervisor.Close()
	mu.Lock()
	defer mu.Unlock()
	for _, e := range heard {
		if e.Row.ID != failed.ID || e.Removed || (e.Row.Episode != "" && e.Row.Episode != failed.Episode) {
			t.Fatalf("a listener heard something other than the one row and its one episode: %+v", e)
		}
	}
}

// TestADeniedRepairLeavesTheFailedRowAsItWas: the deny is the ordinary job it
// always is, and the kept row, its yes and its episode are untouched.
func TestADeniedRepairLeavesTheFailedRowAsItWas(t *testing.T) {
	supervisor, rt, _ := repairWorld(t)
	completer := &scriptedCompleter{steps: []step{
		keepCall("k1", "npm run dev", map[string]any{"health": map[string]any{"port": 5173}}),
		finalText("port is taken"),
		keepCall("k2", "sleep 300", map[string]any{"health": map[string]any{"port": 5174}, "service_id": "s1"}),
		finalText("ok"),
	}}
	agent := keepAgent(t, completer, supervisor, "api-docs")
	failed := conflictedRow(t, agent, supervisor, rt)
	events, _ := agent.Submit(context.Background(), "use another port")
	collected := drainAnsweringKeep(t, agent, events, KeepDenyKey, nil)
	if len(cardsIn(collected)) != 1 {
		t.Fatal("the repair was not asked about")
	}
	rows := supervisor.List("api-docs")
	if len(rows) != 1 || rows[0].Command != failed.Command || rows[0].State != service.StateFailed || rows[0].Episode != failed.Episode || !rows[0].EpisodeOpen {
		t.Fatalf("a deny changed the kept row: %+v", rows)
	}
	if _, ok := supervisor.Approved(service.Spec{Command: "sleep 300", Dir: agent.config.Workspace, Health: service.Health{Port: 5174}}); ok {
		t.Fatal("a no was banked as a yes")
	}
	if text := toolResultText(t, agent, "k2"); !strings.Contains(text, "stops when this turn ends") || !strings.Contains(text, failed.ID+" is unchanged") {
		t.Fatalf("the model was told %q", text)
	}
}

// TestACancelledRepairChangesNothing: the turn ending under the card is no
// answer, and an unanswered card starts and changes nothing.
func TestACancelledRepairChangesNothing(t *testing.T) {
	supervisor, rt, _ := repairWorld(t)
	completer := &scriptedCompleter{steps: []step{
		keepCall("k1", "npm run dev", map[string]any{"health": map[string]any{"port": 5173}}),
		finalText("port is taken"),
		keepCall("k2", "npm run dev -- --port 5174", map[string]any{"health": map[string]any{"port": 5174}, "service_id": "s1"}),
		finalText("ok"),
	}}
	agent := keepAgent(t, completer, supervisor, "api-docs")
	failed := conflictedRow(t, agent, supervisor, rt)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, _ := agent.Submit(ctx, "use another port")
	deadline := time.After(10 * time.Second)
	for open := true; open; {
		select {
		case event, more := <-events:
			open = more
			if more && event.Kind == EventKeepRunning && event.Keep != nil && event.Keep.Decided == "" {
				cancel()
			}
		case <-deadline:
			t.Fatal("the turn never ended")
		}
	}
	rows := supervisor.List("api-docs")
	if len(rows) != 1 || rows[0].Command != failed.Command || rows[0].State != service.StateFailed || rt.Starts() != 0 {
		t.Fatalf("a cancelled card changed the kept row: %+v", rows)
	}
	if _, ok := supervisor.Approved(service.Spec{Command: "npm run dev -- --port 5174", Dir: agent.config.Workspace, Health: service.Health{Port: 5174}}); ok {
		t.Fatal("an unanswered card banked a yes")
	}
}

// TestARepairIsRefusedBeforeTheCard: another conversation's id, an unknown
// one, a service still running and a duplicate are each said to the model with
// no card raised, nothing started and nothing signalled.
func TestARepairIsRefusedBeforeTheCard(t *testing.T) {
	supervisor, rt, _ := repairWorld(t)
	foreign, err := supervisor.Keep("other-agent", service.Spec{Command: "make serve", Dir: t.TempDir(), Health: service.Health{Port: 8080}}, service.PolicyNever, service.Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		keep map[string]any
		want string
	}{
		{"foreign", map[string]any{"health": map[string]any{"port": 5174}, "service_id": foreign.ID}, "is not one of this conversation's"},
		{"unknown", map[string]any{"health": map[string]any{"port": 5174}, "service_id": "s99"}, "is not one of this conversation's"},
		{"live", map[string]any{"health": map[string]any{"port": 5174}, "service_id": "s2"}, "is still running"},
		{"duplicate", map[string]any{"health": map[string]any{"port": 5175}, "service_id": "s3"}, "already has that command"},
	}
	var steps []step
	for i, c := range cases {
		command := "npm run dev -- --port 5174"
		if c.name == "duplicate" {
			command = "npm start"
		}
		steps = append(steps, keepCall("r"+string(rune('0'+i)), command, c.keep), finalText("refused"))
	}
	agent := keepAgent(t, &scriptedCompleter{steps: steps}, supervisor, "api-docs")
	dir := agent.config.Workspace
	live, err := supervisor.Keep("api-docs", service.Spec{Command: "npm start", Dir: dir, Health: service.Health{Port: 3000}}, service.PolicyOnFailure, service.Provenance{})
	if err != nil || live.ID != "s2" || !live.Running {
		t.Fatalf("%+v %v", live, err)
	}
	stopped, err := supervisor.Keep("api-docs", service.Spec{Command: "npm run docs", Dir: dir, Health: service.Health{Port: 4000}}, service.PolicyOnFailure, service.Provenance{})
	if err != nil || stopped.ID != "s3" {
		t.Fatalf("%+v %v", stopped, err)
	}
	if _, err := supervisor.Stop("api-docs", stopped.ID); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		if row, _ := supervisor.Get("api-docs", stopped.ID); !row.Running && !row.Stopping {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("s3 never stopped")
		}
	}
	starts, running := rt.Starts(), rt.Running()
	for i, c := range cases {
		events, _ := agent.Submit(context.Background(), "fix it")
		collected := drainAnsweringKeep(t, agent, events, KeepAllowKey, nil)
		if cards := cardsIn(collected); len(cards) != 0 {
			t.Errorf("%s: a refused repair raised a card", c.name)
		}
		if text := toolResultText(t, agent, "r"+string(rune('0'+i))); !strings.Contains(text, c.want) || !strings.Contains(text, "Nothing was started or changed") {
			t.Errorf("%s: the model was told %q", c.name, text)
		}
	}
	if rt.Starts() != starts || rt.Running() != running {
		t.Fatal("a refused repair started or ended something")
	}
	if rows := supervisor.List("api-docs"); len(rows) != 2 || rows[1].Command != "npm run docs" {
		t.Fatalf("a refused repair changed a row: %+v", rows)
	}
	if row, _ := supervisor.Get("other-agent", foreign.ID); row.Command != "make serve" || !row.Running {
		t.Fatalf("another conversation's service was touched: %+v", row)
	}
}

// TestBashOffersServiceIDWithKeepRunning: the argument rides keep_running and
// is absent with it.
func TestBashOffersServiceIDWithKeepRunning(t *testing.T) {
	supervisor, _, _ := repairWorld(t)
	agent := keepAgent(t, &scriptedCompleter{}, supervisor, "api-docs")
	for _, tool := range agent.belt() {
		if tool.Name == "bash" && !strings.Contains(string(tool.Schema), `"service_id"`) {
			t.Fatal("bash's keep_running has no service_id")
		}
	}
}
