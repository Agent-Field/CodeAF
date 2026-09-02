package remote

import (
	"context"
	"os"
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

	id, title, _, err := loop.Client.Agent().StartTask(context.Background(), "fix it")
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

func TestRunningTaskRoomReadsSteersAndStopsByEngineID(t *testing.T) {
	journal := t.TempDir() + "/task.jsonl"
	if err := os.WriteFile(journal, []byte("far journal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	far := &fakeAgent{taskJournal: journal}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, TaskRecord: func(uri string, tail int) (session.TaskRecord, error) {
			if uri != "file://"+journal || tail != session.TaskJournalTail {
				t.Fatalf("room read %q tail %d", uri, tail)
			}
			return session.TaskRecord{Journal: []byte("far journal\n"), Kept: true}, nil
		}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })
	record, err := loop.Client.Agent().TaskRoom(17, session.TaskJournalTail)
	if err != nil || string(record.Journal) != "far journal\n" {
		t.Fatalf("room = %q, %v", record.Journal, err)
	}
	waiting, err := loop.Client.Agent().SteerTask(17, "check the lock")
	if err != nil || !waiting {
		t.Fatalf("steer = %v, %v", waiting, err)
	}
	line, err := loop.Client.Agent().Cancel("task:17")
	if err != nil || line != "stopping task 17" {
		t.Fatalf("stop = %q, %v", line, err)
	}
	if !reflect.DeepEqual(far.steered, []string{"17:check the lock"}) || !reflect.DeepEqual(far.cancelled, []string{"task:17"}) {
		t.Fatalf("engine calls = %v, %v", far.steered, far.cancelled)
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
	if _, _, _, err := loop.Client.Agent().StartTask(context.Background(), "fix it"); err == nil {
		t.Fatal("a taskless engine accepted Task.Start")
	}
}

// tasklessAgent hides the optional task methods while retaining the ordinary conversation.
type tasklessAgent struct{ WrappedAgent }
