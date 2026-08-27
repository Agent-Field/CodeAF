package remote

import (
	"context"
	"reflect"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestTaskCallOutlivesTheEngineShaper(t *testing.T) {
	if taskCallDeadline <= session.TaskShapeWindow {
		t.Fatalf("task call gives up after %s before the %s shaper can finish", taskCallDeadline, session.TaskShapeWindow)
	}
}

func TestTaskDoorsRunOnTheEngineAgent(t *testing.T) {
	far := &fakeAgent{}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	id, title, err := loop.Client.Agent().StartTask(context.Background(), "fix it")
	if err != nil || id != 17 || title != "far task" {
		t.Fatalf("start = %d %q %v", id, title, err)
	}
	run, name, err := loop.Client.Agent().StartPlannerRun(context.Background(), "plan it", "two parts")
	if err != nil || run != "run-8" || name != "far plan" {
		t.Fatalf("planner = %q %q %v", run, name, err)
	}
	wide, parts, why := loop.Client.Agent().JudgeDecomposable(context.Background(), "size it")
	if !wide || !reflect.DeepEqual(parts, []string{"one", "two"}) || why != "independent" {
		t.Fatalf("judge = %v %v %q", wide, parts, why)
	}
	if !reflect.DeepEqual(far.tasks, []string{"fix it", "judge:size it"}) || !reflect.DeepEqual(far.planners, []string{"plan it|two parts"}) {
		t.Fatalf("far calls = %v %v", far.tasks, far.planners)
	}
}

func TestTaskDoorRefusesWhenTheEngineDoesNotCarryIt(t *testing.T) {
	base := &tasklessAgent{WrappedAgent: &fakeAgent{}}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: base, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	if _, _, err := loop.Client.Agent().StartTask(context.Background(), "fix it"); err == nil {
		t.Fatal("a taskless engine accepted Task.Start")
	}
}

// tasklessAgent hides the optional task methods while retaining the ordinary conversation.
type tasklessAgent struct{ WrappedAgent }
