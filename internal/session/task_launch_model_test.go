package session

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/teams"
)

func TestProposalNamedModelReachesRunWithoutCrewRouter(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("done")
	registerBeltRunEngine(t, double)
	agent := batchAgent(t, t.TempDir())
	var args taskArguments
	if err := json.Unmarshal(beltProposalArgs("Read the parser", "report received"), &args); err != nil {
		t.Fatal(err)
	}
	args.Model = "moonshotai/kimi-k3"
	body, _ := json.Marshal(args)
	answer, failed, err := approveBeltProposal(t, agent, body)
	if err != nil || failed {
		t.Fatalf("proposal: %s, %v", answer, err)
	}
	<-double.entered
	double.mu.Lock()
	got := double.spec.WorkModel
	double.mu.Unlock()
	endBeltRun(t, agent, double)
	if got != args.Model {
		t.Fatalf("worker model = %q, want %q", got, args.Model)
	}
}

func TestTeamStartCarriesRequestedModel(t *testing.T) {
	fixture := newTeamFixture(t, true)
	agent := teamAgent(t, fixture, fixture.manager, nil, func(c *Config) {
		c.TaskModels = func() []string { return []string{"moonshotai/kimi-k3", "deepseek/deepseek-v4.1-flash"} }
	})
	answer, failed, err := agent.teamStartTool(t.Context(), json.RawMessage(`{"handle":"scout","brief":"Report ready","model":"kimi-k3"}`))
	if failed || err != nil {
		t.Fatalf("team_start: %s, %v", answer, err)
	}
	rows, err := teams.ReadTraffic(fixture.profile, fixture.teamID, "", 100)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(rows)
	if !strings.Contains(string(raw), `"model":"moonshotai/kimi-k3"`) {
		t.Fatalf("start lost requested model: %s", raw)
	}
}

func TestJoinedProposalRetainsItsOwnModel(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("done")
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	agent := batchAgent(t, dir)
	if _, _, _, err := agent.StartTask(t.Context(), "read the seed", false); err != nil {
		t.Fatal(err)
	}
	var args taskArguments
	_ = json.Unmarshal(beltProposalArgs("Read separately", "report ready"), &args)
	args.Model = "moonshotai/kimi-k3"
	body, _ := json.Marshal(args)
	answer, failed, err := approveBeltProposal(t, agent, body)
	if err != nil || failed {
		t.Fatalf("proposal: %s %v", answer, err)
	}
	store := beltRunStoreAt(t, dir)
	defer store.Close()
	var recorded string
	for _, task := range store.Tasks() {
		if task.Title == args.Title {
			raw, _ := json.Marshal(task)
			recorded = string(raw)
		}
	}
	endBeltRun(t, agent, double)
	if !strings.Contains(recorded, `"model":"moonshotai/kimi-k3"`) {
		t.Fatalf("joined task lost model: %s", recorded)
	}
}
