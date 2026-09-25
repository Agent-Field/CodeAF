package session

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// A TASK HANDED OFF IN A CONVERSATION RUNS ON THE CREW'S OWN WORKER (#312).
//
// The engine never reads a profile: the surface hands it a [Config.RolesSource]
// and a task's model climbs the ladder through that (taskmodel.go's
// [Agent.defaultTaskModel]). This is the ENGINE half of the acceptance, driven
// through the same call the chat door builds its map with (cmd/codeaf's
// v3Crew): the worker row as internal/config resolves it — a pin, or the
// router's pick — and the model a task admitted right now would run on. No
// row is ever answered with a model this build chose for everybody.
func TestATaskRunsOnTheCrewsWorker(t *testing.T) {
	pinned := "vendor/pinned-small-work"
	for _, test := range []struct {
		name  string
		rows  map[string]string
		model string
	}{
		{
			name:  "a pinned worker row is what the task runs on",
			rows:  map[string]string{config.KeyTierLowModel: pinned, config.KeyTierWorkerModel: "vendor/my-own-worker"},
			model: "vendor/my-own-worker",
		},
		{
			// Cleared means "follow the conversation", and this is the surface
			// that HAS one — so the task rides the model the person is talking
			// to, exactly as it did before any of this.
			name:  "a worker row cleared on purpose follows the conversation",
			rows:  map[string]string{config.KeyTierLowModel: pinned, config.KeyTierWorkerModel: ""},
			model: "test/model",
		},
		{
			// A profile that has said nothing has an AUTO worker: the router
			// picks it per task, and with nothing to pick from — no catalog in
			// this test — the task rides the conversation's model, never a
			// model this build chose for everybody.
			name:  "a profile that has said nothing never runs a build-chosen worker",
			rows:  map[string]string{},
			model: "test/model",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
				c.RolesSource = profileTiers(t, test.rows)
			})
			if got := agent.resolveTaskModel("").model; got != test.model {
				t.Fatalf("a task admitted now would run on %q, want %q", got, test.model)
			}
		})
	}
}

// profileTiers is the five classes read off a profile of a stated vintage, the
// way the chat door reads them (cmd/codeaf's v3Crew.snapshot): a value the role
// ladder holds, and "not held" for a row that follows the conversation.
func profileTiers(t *testing.T, rows map[string]string) func(string) (string, bool) {
	t.Helper()
	dir := t.TempDir()
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.BudgetConfigPath(dir), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	values := map[string]string{
		roles.TierKey(roles.TierReflex):     config.TierModelAt(dir, config.ModelTierReflex),
		roles.TierKey(roles.TierMastermind): config.TierModelAt(dir, config.ModelTierMastermind),
		roles.TierKey(roles.TierWorker):     config.TierModelAt(dir, config.ModelTierWorker),
		roles.TierKey(roles.TierLow):        config.TierModelAt(dir, config.ModelTierLow),
		roles.TierKey(roles.TierHigh):       config.TierModelAt(dir, config.ModelTierHigh),
	}
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok && value != ""
	}
}
