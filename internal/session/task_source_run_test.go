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

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/crewroute"
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

func TestRoutingRerunKeepsItsRecordedSourceDuringALaterMessage(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(strconv.FormatBool(legacy), func(t *testing.T) {
			t.Setenv("CODEAF_TASK_BELT", "bash")
			double := newBeltRunDouble("finished")
			registerBeltRunEngine(t, double)
			agent, _ := newTestAgent(t, &scriptedCompleter{}, func(c *Config) {
				c.Workspace = newTestRepo(t)
				c.ProfileDir = t.TempDir()
				c.RouteCrew = func(config.CrewAsk) (crewroute.Decision, error) { return flashDecision(), nil }
			})
			heardAs(agent, "original task request")
			original := agent.bornSource()
			graph := agent.graph()
			graph.mu.Lock()
			source := cloneSource(original)
			if legacy {
				source = nil
			}
			graph.runs = make(map[uint64][]TaskNotice)
			graph.runs[40] = []TaskNotice{{ID: 40, Title: "prior", Brief: "fix the nil map", State: TaskDone, Source: source}}
			graph.mu.Unlock()
			heardAs(agent, "unrelated later request")
			id, err := agent.rerunWithRouting(context.Background(), TaskNotice{ID: 40, Title: "prior", Brief: "fix the nil map", State: TaskDone}, nil, TaskRoutingAction{Kind: TaskRoutingBest})
			if err != nil {
				t.Fatal(err)
			}
			defer endBeltRun(t, agent, double)
			got := agent.appRunViewSource(t, id)
			if legacy && got != nil {
				t.Fatalf("legacy source invented: %+v", got)
			}
			if !legacy && (got == nil || got.ID != original.ID) {
				t.Fatalf("rerun source=%+v want %+v", got, original)
			}
		})
	}
}
