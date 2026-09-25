package session

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
)

// A TASK RUNS ON THE MODEL SOMEBODY NAMED FOR IT. The ladder the manual and
// the settings row teach — a model named in the ask, then the `task model`
// row, then the crew's worker — reached the proposal card and the receipt,
// but never the router: the run was seated on the routed worker whatever was
// named. A named model and the task model row now reach the router as a
// one-task pin on the worker, and a task that names nothing is routed.
func TestATaskRunsOnTheModelNamedForIt(t *testing.T) {
	var asked []config.CrewAsk
	route := func(ask config.CrewAsk) (crewroute.Decision, error) {
		asked = append(asked, ask)
		return crewroute.Decision{}, nil
	}
	agent := &Agent{config: Config{Workspace: t.TempDir(), ProfileDir: t.TempDir(), RouteCrew: route}, model: "somelab/the-chat-model"}

	if _, err := agent.routeTaskCrew(t.Context(), 1, "fix the parser", ""); err != nil {
		t.Fatal(err)
	}
	if got := asked[0].Sends[crewroute.Worker]; got != "" {
		t.Fatalf("a task that named nothing pinned its worker to %q", got)
	}
	if _, err := agent.routeTaskCrew(withCrewWish(t.Context(), crewWish{worker: "vendor/named"}), 2, "fix the parser", ""); err != nil {
		t.Fatal(err)
	}
	if got := asked[1].Sends[crewroute.Worker]; got != "vendor/named" {
		t.Fatalf("a hand-off that named vendor/named routed its worker as %q", got)
	}
	agent.mu.Lock()
	agent.config.TaskModel = "vendor/task-row"
	agent.mu.Unlock()
	if _, err := agent.routeTaskCrew(t.Context(), 3, "fix the parser", ""); err != nil {
		t.Fatal(err)
	}
	if got := asked[2].Sends[crewroute.Worker]; got != "vendor/task-row" {
		t.Fatalf("the task model row routed the worker as %q", got)
	}
	if _, err := agent.routeTaskCrew(withCrewWish(t.Context(), crewWish{worker: "vendor/named"}), 4, "fix the parser", ""); err != nil {
		t.Fatal(err)
	}
	if got := asked[3].Sends[crewroute.Worker]; got != "vendor/named" {
		t.Fatalf("a named model lost to the task model row: %q", got)
	}
}
