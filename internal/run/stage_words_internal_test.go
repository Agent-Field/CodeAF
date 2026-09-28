package run

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// A PROGRAM'S STAGE IS SHOWN IN THE WORD IT GAVE A PERSON. senior-dev's page
// read `agent-runtime` for the whole of its work, which is its machinery's name
// for a model turn; the row now reads the program's own word for the phase, a
// stage it gave no word keeps the word already shown, and a program that gave
// no words at all is shown its stages as it spelled them.
func TestAProgramsStageIsShownInTheWordItGaveAPerson(t *testing.T) {
	live := func(program delegate.Delegate, stages ...[2]string) string {
		t.Helper()
		store, err := plandb.Open(filepath.Join(t.TempDir(), "plandb.db"), "p", "root", "root", "root")
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		sink := &delegateSink{worker: &DelegateWorker{store: store, program: program}, taskID: "root", name: program.Name}
		for _, stage := range stages {
			sink.Stage(delegate.StageRecord{Stage: stage[0], Status: stage[1]})
		}
		return store.LiveSteps()["root"].Command
	}
	worded := delegate.Delegate{Name: "senior-dev", StageWords: map[string]string{"implement": "working"}}
	if got := live(worded, [2]string{"implement", "running"}); got != "senior-dev: working" {
		t.Fatalf("a worded stage reads %q, want the program's word and no status", got)
	}
	if got := live(worded, [2]string{"implement", "running"}, [2]string{"agent-runtime", "configured"}); got != "senior-dev: working" {
		t.Fatalf("a stage with no word reads %q, want the word already shown to stand", got)
	}
	if got := live(delegate.Delegate{Name: "fake"}, [2]string{"implement", "running"}); got != "fake: implement · running" {
		t.Fatalf("a program with no words reads %q, want its own stage and status", got)
	}
}

// THE LIVE STEP FOLLOWS THE STEP OF THE PROGRAM'S PROCESS, AND EVERY RECORD IS
// KEPT. Each stage, step and ending is written to the task's action log the
// moment it arrives, stamped with codeaf's own clock; the row reads the word the
// program's own reader gives the step a record served, falls back to the stage
// words until a record has named one, and keeps the step's word through a stage
// that names none.
func TestTheLiveStepFollowsTheProgramsStepAndEveryRecordIsKept(t *testing.T) {
	store, err := plandb.Open(filepath.Join(t.TempDir(), "plandb.db"), "p", "root", "root", "root")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	program := delegate.Delegate{
		Name: "senior-dev", StageWords: map[string]string{"intake": "reading the brief", "implement": "working"},
		Present: func() delegate.ActionReader {
			return func(action delegate.Action) (delegate.Shown, bool) {
				if action.Kind == delegate.ActionStep {
					return delegate.Shown{Step: action.Step, Text: action.Command}, true
				}
				return delegate.Shown{Text: action.Stage}, true
			}
		},
	}
	taskDir := t.TempDir()
	sink := &delegateSink{worker: &DelegateWorker{store: store, program: program}, taskID: "root", taskDir: taskDir, storeDir: t.TempDir(), name: program.Name}
	live := func() string { return store.LiveSteps()["root"].Command }

	before := time.Now()
	sink.Stage(delegate.StageRecord{Stage: "intake", Status: "captured"})
	if got := live(); got != "senior-dev: reading the brief" {
		t.Fatalf("before any step the row reads %q, want the stage's word", got)
	}
	exit := 1
	sink.Step(delegate.StepRecord{Command: "bash: go test ./...", Tool: "bash", Step: "explore", Exit: &exit})
	if got := live(); got != "senior-dev: explore" {
		t.Fatalf("after a step the row reads %q, want the step's word", got)
	}
	sink.Stage(delegate.StageRecord{Stage: "implement", Status: "running"})
	if got := live(); got != "senior-dev: explore" {
		t.Fatalf("a stage that names no step moved the row to %q", got)
	}
	sink.Terminal(delegate.Terminal{Status: delegate.StatusPass, Message: "done"})

	actions, err := delegate.ReadActions(taskDir, 0)
	if err != nil || len(actions) != 4 {
		t.Fatalf("the action log holds %+v (%v), want the four records", actions, err)
	}
	kinds := []string{actions[0].Kind, actions[1].Kind, actions[2].Kind, actions[3].Kind}
	if strings.Join(kinds, ",") != "stage,step,stage,end" || actions[1].Exit == nil || *actions[1].Exit != 1 || actions[3].Message != "done" {
		t.Fatalf("the action log = %+v", actions)
	}
	for i, action := range actions {
		if action.At.Before(before) || (i > 0 && action.At.Before(actions[i-1].At)) {
			t.Fatalf("action %d was stamped %v, want codeaf's own clock, in order", i, action.At)
		}
	}
}
