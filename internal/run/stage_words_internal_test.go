package run

import (
	"path/filepath"
	"testing"

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
