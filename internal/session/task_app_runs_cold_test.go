package session

import "testing"

// A RUN ROW'S PROGRAM, ENDING AND BRANCH REACH THE APP'S TASK FROM THE ROW ALONE,
// survive a reopen with the checkpoint, and are unknown for a row that never
// recorded them. A person's stop is the one ending the row's Stopped flag says,
// and an older failure with no typed ending is not reclassified.
func TestARunRowShowsItsColdProgramFactsAfterReopenAndNothingForALegacyRow(t *testing.T) {
	journal, checkpoint := journalIn(t)
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.SessionFile = journal })
	graph := agent.graph()
	one, two, three := graph.reserve(), graph.reserve(), graph.reserve()
	agent.publishRunRow(graph, TaskNotice{ID: one, Title: "senior-dev", State: TaskFailed, Program: "senior-dev", Branch: "task/fix", Stopped: true, Ending: TaskEndingStopped})
	agent.publishRunRow(graph, TaskNotice{ID: two, Title: "review", State: TaskFailed, Program: "review", Ending: TaskEndingProgram})
	agent.publishRunRow(graph, TaskNotice{ID: three, Title: "senior-dev: old", State: TaskFailed})

	check := func(a *Agent) {
		t.Helper()
		facts := func(id uint64) AppTaskView {
			row, ok := a.appRunRow(id)
			if !ok {
				t.Fatalf("no run row %d", id)
			}
			return a.appRunView(row, 0)
		}
		if v := facts(one); v.RunProgram != "senior-dev" || v.Ending != "stopped" || v.Branch != "task/fix" || v.State != "stopped" {
			t.Fatalf("stopped row = %+v", v)
		}
		if v := facts(two); v.RunProgram != "review" || v.Ending != "program" || v.Branch != "" || v.State != "failed" {
			t.Fatalf("typed row = %+v", v)
		}
		if v := facts(three); v.RunProgram != "" || v.Ending != "" || v.Branch != "" || v.State != "failed" {
			t.Fatalf("legacy row = %+v", v)
		}
	}
	check(agent)

	reopened, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) { config.InTask = true })
	reopened.graph().rehydrate(readCheckpoint(t, checkpoint), t.TempDir(), TaskSettleAsk)
	check(reopened)
}
