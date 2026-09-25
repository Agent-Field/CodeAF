package run

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/session"
)

// crewCallProbe is a provider that answers nothing and counts what it was asked.
type crewCallProbe struct{ calls int }

func (p *crewCallProbe) CompleteWithMessages(context.Context, []ai.Message, ...ai.Option) (*ai.Response, error) {
	p.calls++
	return &ai.Response{}, nil
}

// The completer installed by CrewFactory marks each call before the guard
// sees it. Calls on a shared model reach the worker, while the checker's
// estimate crosses its own line before its first provider call.
func TestCrewFactoryCarriesTheRoleSeatToTheSpendGuard(t *testing.T) {
	dir := t.TempDir()
	profile := t.TempDir()
	rows, _ := json.Marshal(map[string]string{
		config.KeyTierWorkerModel: "vendor/shared",
		config.KeyTierHighModel:   "vendor/shared",
	})
	if err := os.WriteFile(config.BudgetConfigPath(profile), rows, 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := plandb.Open(filepath.Join(dir, "plan.db"), "seat-test", "root", "Root", "check the seat")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.AddMany([]plandb.TaskSpec{
		{ID: "leaf", Title: "Work", ParentID: store.RootID()},
		{ID: "review", Title: "Review", Role: plandb.RoleCheck},
	}); err != nil {
		t.Fatal(err)
	}
	guard := &session.SpendGuard{
		Price:         func(string) (float64, float64, float64, bool) { return 0, 1e-5, 0, true },
		SeatCeilings:  map[crewroute.Seat]float64{crewroute.Checker: 0.01},
		CeilingAction: "checker ceiling $%.2f",
	}
	probes := []*crewCallProbe{}
	factory := CrewFactory(store, dir, profile, Seats{}, func(model string) session.Completer {
		p := &crewCallProbe{}
		probes = append(probes, p)
		return guard.Wrap(model, p)
	})
	call := func(id string) error {
		worker := factory(*store.Task(id)).(*BashWorker)
		_, err := worker.completer.CompleteWithMessages(t.Context(), []ai.Message{{Role: "user"}})
		return err
	}
	if err := call("leaf"); err != nil || len(probes) != 1 || probes[0].calls != 1 {
		t.Fatalf("worker's shared-model call: %v, probes %+v", err, probes)
	}
	err = call("review")
	var stopped session.ErrSpendStopped
	if !errors.As(err, &stopped) || len(probes) != 2 || probes[1].calls != 0 {
		t.Fatalf("checker crossed its line: %v, probes %+v", err, probes)
	}
}
