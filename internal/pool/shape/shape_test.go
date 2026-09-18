package shape

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/crewpick"
)

// Rows sum per task and seat; rows with no seat word, no task, an unpicked
// seat or a torn line are skipped; tasks come back in id order.
func TestTasksSumsEachSeatPerTaskAndSkipsWhatIsNotEvidence(t *testing.T) {
	ledger := strings.Join([]string{
		`{"at":"2026-09-17T14:05:32Z","model":"a/w","seat":"worker","task":"t2","in":100,"out":10}`,
		`{"at":"2026-09-17T14:05:33Z","model":"a/w","seat":"worker","task":"t2","in":50,"out":5}`,
		`{"at":"2026-09-17T14:05:34Z","model":"b/h","seat":"high","task":"t2","in":30,"out":3}`,
		`{"at":"2026-09-17T14:05:35Z","model":"c/r","seat":"reflex","task":"t2","in":9,"out":9}`,
		`{"at":"2026-09-17T14:05:36Z","model":"a/w","task":"t2","in":999,"out":999}`,
		`{"at":"2026-09-17T14:05:37Z","model":"a/w","seat":"worker","in":999,"out":999}`,
		`{"at":"2026-09-17T14:05:38Z","model":"a/w","seat":"worker","task":"t1","in":7,"out":1}`,
		`{"at":"2026-09-17T14:05:39Z","model":"a/w","seat":"worker","task":"t1","in":0,"out":0}`,
		`{"torn`,
	}, "\n")
	path := filepath.Join(t.TempDir(), "usage.jsonl")
	if err := os.WriteFile(path, []byte(ledger), 0o600); err != nil {
		t.Fatal(err)
	}
	tasks, err := Tasks(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []crewpick.TaskUsage{
		{crewpick.Worker: {In: 7, Out: 1}},
		{crewpick.Worker: {In: 150, Out: 15}, crewpick.High: {In: 30, Out: 3}},
	}
	if len(tasks) != len(want) {
		t.Fatalf("read %d tasks, want %d: %v", len(tasks), len(want), tasks)
	}
	for i := range want {
		if len(tasks[i]) != len(want[i]) {
			t.Fatalf("task %d = %v, want %v", i, tasks[i], want[i])
		}
		for seat, tokens := range want[i] {
			if tasks[i][seat] != tokens {
				t.Fatalf("task %d seat %d = %+v, want %+v", i, seat, tasks[i][seat], tokens)
			}
		}
	}
}

// A ledger that does not exist is no tasks and no error.
func TestAMissingLedgerTeachesNothing(t *testing.T) {
	tasks, err := Tasks(filepath.Join(t.TempDir(), "absent.jsonl"))
	if err != nil || tasks != nil {
		t.Fatalf("got %v, %v; want nil, nil", tasks, err)
	}
}
