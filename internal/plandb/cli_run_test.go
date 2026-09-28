package plandb

import (
	"path/filepath"
	"testing"
)

// A WORKER'S `plandb` WRITES ONLY ITS OWN RUN'S STORE. The worker is bound to
// its store by path (PLANDB_DB) and to its run by the run's root (RunEnv). A
// store at that path whose root is another run's is the store a later request
// left there, and the CLI refuses it and writes nothing: a worker once filed
// four children and ten `done`s into the run beside its own.
func TestPlandbCliRefusesAnotherRunsStore(t *testing.T) {
	db := filepath.Join(t.TempDir(), "plandb.db")
	other, err := Open(db, "the other run", "8", "the other run", "")
	if err != nil {
		t.Fatal(err)
	}
	before := len(other.Tasks())
	_ = other.Close()

	h := cliNewHarness(t)
	t.Setenv("PLANDB_DB", db)
	t.Setenv(RunEnv, "1")
	code := h.run("add", "not this run's work", "--as", "hijack")
	cliWantError(t, h, code, "another run")
	reread, err := Open(db, "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := len(reread.Tasks()); got != before {
		t.Fatalf("the other run's store went from %d tasks to %d", before, got)
	}
	_ = reread.Close()

	// AND THE RUN'S OWN STORE IS WRITTEN AS EVER.
	t.Setenv(RunEnv, "8")
	cliWantCode(t, h.run("add", "this run's work", "--as", "own"), 0)
}
