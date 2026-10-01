package preflight

import (
	"github.com/Agent-Field/codeaf/internal/inventory"
	"reflect"
	"testing"
)

func TestChangedNamesReadsPathsAndRenames(t *testing.T) {
	out := []byte(" M a.go\x00?? b.txt\x00R  new.go\x00old.go\x00")
	if got, want := changedNames(out), []string{"a.go", "b.txt", "new.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestUncommittedOfNoRepositoryIsNone(t *testing.T) {
	if got := Uncommitted(t.TempDir()); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestCompareCarriesTheRecordedTestRun(t *testing.T) {
	run := &inventory.TestRun{Command: "go test ./...", Passed: true}
	if got := Compare("", inventory.Inventory{Tests: run}, here{}, Report{}).Tests; got != run {
		t.Fatalf("got %v", got)
	}
	if Compare("", inventory.Inventory{}, here{}, Report{}).Tests != nil {
		t.Fatal("a run was invented")
	}
}
