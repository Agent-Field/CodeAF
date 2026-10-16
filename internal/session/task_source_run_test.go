package session

// THE RUN ROAD CAPTURES ITS SOURCE ON THE REAL ROAD. The seam tests in
// task_source_test.go prove each piece; this one drives the whole of it — `/task`
// through [Agent.StartTask], [Agent.startTaskRun], [Agent.startKnownTaskRun] and a
// run engine that really starts — with the local run double standing in for the
// engine and a scripted completer for the model, so nothing leaves the machine, and reads the source back from the row the app
// lists, the plan row the run's store holds, and the checkpoint.

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
)

func TestARunStartedByTaskCarriesItsDirectiveOnTheRealRunRoad(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("the run fixed the nil map")
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	completer := &scriptedCompleter{steps: []step{finalText("the run fixed the nil map")}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.SessionFile = filepath.Join(dir, placeTranscript)
		config.AskConsent = false
	})
	heardAs(agent, "an unrelated earlier message")
	id, _, _, err := agent.StartTask(context.Background(), "fix the nil map crash", false)
	if err != nil || id == 0 {
		t.Fatalf("StartTask = %d, %v", id, err)
	}
	defer endBeltRun(t, agent, double)
	if _, ok := agent.appRunRow(id); !ok {
		t.Fatalf("the run road published no run row for %d", id)
	}
	source := agent.appRunViewSource(t, id)
	if source == nil || source.Kind != TaskSourceDirective || source.Text != "fix the nil map crash" {
		t.Fatalf("the run row's source = %+v, want the /task directive", source)
	}
	plan := planRowFor(agent.PlanTasks(), planStoreID(strconv.FormatUint(id, 10)))
	if plan == nil || plan.Source == nil || plan.Source.ID != source.ID {
		t.Fatalf("the run's plan root carries %+v, want the run row's block %q", plan, source.ID)
	}
	kept := false
	for _, run := range readCheckpoint(t, taskCheckpointPath(agent.config.SessionFile)).Runs {
		if run.ID == id && run.Source != nil && run.Source.ID == source.ID {
			kept = true
		}
	}
	if !kept {
		t.Fatal("the run row's source is not in the checkpoint")
	}
	beltRunWaitFor(t, "the run engine starting", func() bool {
		double.mu.Lock()
		defer double.mu.Unlock()
		return double.ran
	})
}
