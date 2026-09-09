package remote

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

func TestTaskCallOutlivesTheEngineShaper(t *testing.T) {
	if taskCallDeadline <= session.TaskShapeWindow {
		t.Fatalf("task call gives up after %s before the %s shaper can finish", taskCallDeadline, session.TaskShapeWindow)
	}
}

func TestTaskDoorsRunOnTheEngineAgent(t *testing.T) {
	far := &fakeAgent{startNote: session.TaskShapeFallbackNote}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far, Workspace: "/srv/app", SessionFile: "/srv/app/j.jsonl"}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	// AND THE FALLBACK NOTE CROSSES THE WIRE. The whole reason [TaskStarted]
	// grew a Note is that a shaper cut on the ENGINE machine has to reach the
	// surface drawing the started row, so the receipt is checked for it here
	// rather than only where session hands it over.
	id, title, note, err := loop.Client.Agent().StartTask(context.Background(), "fix it")
	if err != nil || id != 17 || title != "far task" || note != session.TaskShapeFallbackNote {
		t.Fatalf("start = %d %q %q %v", id, title, note, err)
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
	// The engine behind this loop carries the OLDER door (a bool), so this is
	// also the assertion that a client keeps steering such an engine and is given
	// the delivery sentence for it rather than an empty receipt.
	receipt, err := loop.Client.Agent().SteerTask(17, "check the lock")
	if err != nil || !receipt.Waiting {
		t.Fatalf("steer = %+v, %v", receipt, err)
	}
	if receipt.Held || receipt.Landing != session.SteerDelivered(true) {
		t.Fatalf("steer receipt = %+v, want the older engine's delivery said in the engine's own words", receipt)
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

// receiptAgent is an engine that carries the WHOLE steer receipt: the door
// internal/session grew when a line said during a task's check stopped being a
// refusal and became something the engine keeps.
type receiptAgent struct {
	*fakeAgent
	receipt session.SteerReceipt
}

func (a *receiptAgent) SteerTask(id uint64, line string) (session.SteerReceipt, error) {
	a.steered = append(a.steered, fmt.Sprintf("%d:%s", id, line))
	return a.receipt, nil
}

// A LINE HELD ON A TASK'S RECORD SURVIVES THE WIRE. This is the fact a hosted
// room cannot infer and could not have been told any other way: an error would
// have arrived as bare text, and a bool would have arrived as "delivered" — both
// of them telling the person furthest from the work the one thing that is not
// true about their words.
func TestAHeldSteerCrossesTheWireAsAReceiptAndNotAsARefusal(t *testing.T) {
	far := &receiptAgent{
		fakeAgent: &fakeAgent{},
		receipt: session.SteerReceipt{
			Held:      true,
			Direction: 4,
			Landing:   "held on the task's record — it is being checked, and it cannot land as done without this",
		},
	}
	loop, err := Loopback(Hello{Version: Version}, Options{Boot: func(Hello) (*Engine, error) {
		return &Engine{Agent: far}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = loop.Close() })

	receipt, err := loop.Client.Agent().SteerTask(9, "CSV instead of JSON")
	if err != nil {
		t.Fatalf("a held line came back as an error: %v", err)
	}
	if !receipt.Held || receipt.Waiting {
		t.Fatalf("receipt = %+v, want it held rather than delivered", receipt)
	}
	if receipt.Direction != 4 {
		t.Fatalf("direction id = %d, want the engine's own receipt id: without it no worker can cite the line", receipt.Direction)
	}
	if !strings.Contains(receipt.Landing, "cannot land as done without this") {
		t.Fatalf("landing = %q, want the engine's own sentence carried whole", receipt.Landing)
	}
	if !reflect.DeepEqual(far.steered, []string{"9:CSV instead of JSON"}) {
		t.Fatalf("engine calls = %v", far.steered)
	}
}
