package subharness

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSaveBumpsTheVersionOnEveryEdit(t *testing.T) {
	store := New(t.TempDir())
	harness := sample()

	first, err := store.Save(harness)
	if err != nil {
		t.Fatalf("first save: %v", err)
	}
	if first.Version != 1 {
		t.Fatalf("a first registration is v%d, want v1", first.Version)
	}

	harness.Description = "chase a flaky test and land the fix"
	second, err := store.Save(harness)
	if err != nil {
		t.Fatalf("second save: %v", err)
	}
	if second.Version != 2 {
		t.Fatalf("an edit is v%d, want v2", second.Version)
	}

	// AN IDENTICAL SAVE STILL BUMPS. The version records an approval, not a
	// diff: somebody looked at the card again and said yes again.
	third, err := store.Save(harness)
	if err != nil {
		t.Fatalf("third save: %v", err)
	}
	if third.Version != 3 {
		t.Fatalf("a re-registration is v%d, want v3", third.Version)
	}

	current, err := store.Load(harness.Name)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if current.Version != 3 {
		t.Fatalf("the entry points at v%d, want the newest", current.Version)
	}
	// And every version that was ever current is still openable by number,
	// which is what makes "run it at v1" a sentence with an answer.
	old, err := store.LoadVersion(harness.Name, 1)
	if err != nil {
		t.Fatalf("v1: %v", err)
	}
	if old.Version != 1 || old.Description != "chase a flaky test to a fix" {
		t.Fatalf("v1 is not what was registered: %+v", old)
	}
	if _, err := store.LoadVersion(harness.Name, 9); !errors.Is(err, ErrNoHarness) {
		t.Fatalf("a version nobody wrote answered: %v", err)
	}
}

func TestSaveRefusesToOverwriteWhatItCannotRead(t *testing.T) {
	root := t.TempDir()
	store := New(root)
	if err := os.WriteFile(store.Path("broken"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	harness := sample()
	harness.Name = "broken"
	if _, err := store.Save(harness); err == nil {
		t.Fatalf("a file that could not be read was overwritten anyway")
	}
}

func TestListSkipsWhatItCannotRead(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.Save(sample()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.Path("broken"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	rows, err := store.List()
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 || rows[0].Name != "triage-flake" {
		t.Fatalf("one broken entry took the list down: %+v", rows)
	}
}

func TestMissingHarnessIsASentinel(t *testing.T) {
	store := New(t.TempDir())
	if _, err := store.Load("nothing"); !errors.Is(err, ErrNoHarness) {
		t.Fatalf("a missing harness answered with %v", err)
	}
	rows, err := store.List()
	if err != nil || len(rows) != 0 {
		t.Fatalf("an empty registry is not empty: %v / %+v", err, rows)
	}
}

func TestRunsLandUnderTheHarnessAndComeBackNewestFirst(t *testing.T) {
	store := New(t.TempDir())
	saved, err := store.Save(sample())
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 8, 16, 14, 25, 30, 0, time.UTC)
	for at := 0; at < 3; at++ {
		run := Run{
			Harness: saved.Name, Version: saved.Version, Status: StatusOK,
			Started:  base.Add(time.Duration(at) * time.Minute),
			Finished: base.Add(time.Duration(at)*time.Minute + time.Second),
			Nodes:    []Trace{{ID: "start", Node: "start", Kind: KindTrigger, OK: true}},
		}
		path, err := store.SaveRun(run)
		if err != nil {
			t.Fatalf("save run: %v", err)
		}
		want := filepath.Join(store.Root(), saved.Name, "run")
		if filepath.Dir(path) != want {
			t.Fatalf("a run landed in %s, want %s", filepath.Dir(path), want)
		}
		if !strings.HasSuffix(path, ".json") {
			t.Fatalf("a run is not json: %s", path)
		}
	}
	paths, err := store.Runs(saved.Name)
	if err != nil || len(paths) != 3 {
		t.Fatalf("history = %d runs (%v)", len(paths), err)
	}
	newest, err := store.LoadRun(paths[0])
	if err != nil {
		t.Fatalf("load run: %v", err)
	}
	if !newest.Started.Equal(base.Add(2 * time.Minute)) {
		t.Fatalf("the history is not newest-first: %s", newest.Started)
	}
	last, ok := store.LastRun(saved.Name)
	if !ok || !last.Started.Equal(newest.Started) {
		t.Fatalf("LastRun disagrees with the history")
	}
}

func TestTwoRunsInOneSecondBothSurvive(t *testing.T) {
	store := New(t.TempDir())
	saved, _ := store.Save(sample())
	at := time.Date(2026, 8, 16, 14, 25, 30, 0, time.UTC)
	for i := 0; i < 2; i++ {
		if _, err := store.SaveRun(Run{Harness: saved.Name, Started: at, Status: StatusOK}); err != nil {
			t.Fatalf("save run %d: %v", i, err)
		}
	}
	paths, _ := store.Runs(saved.Name)
	if len(paths) != 2 {
		t.Fatalf("a second run in the same second overwrote the first: %v", paths)
	}
}

func TestRemoveKeepsTheHistory(t *testing.T) {
	store := New(t.TempDir())
	saved, _ := store.Save(sample())
	if _, err := store.SaveRun(Run{Harness: saved.Name, Started: time.Now(), Status: StatusOK}); err != nil {
		t.Fatal(err)
	}
	if err := store.Remove(saved.Name); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := store.Load(saved.Name); !errors.Is(err, ErrNoHarness) {
		t.Fatalf("the entry survived the retirement: %v", err)
	}
	if paths, _ := store.Runs(saved.Name); len(paths) != 1 {
		t.Fatalf("retiring a harness deleted the evidence: %v", paths)
	}
}

// TWO RUNS IN ONE SECOND STILL COME BACK IN ORDER. The collision suffix sorts
// before the dot in a plain string comparison, so a store that sorted names
// would call the FIRST of two runs a second apart the newest one — which is
// what "last run" means everywhere it is shown.
func TestRunsInsideOneSecondAreOrderedByTheirSuffix(t *testing.T) {
	store := New(t.TempDir())
	saved, _ := store.Save(sample())
	at := time.Date(2026, 8, 16, 14, 25, 30, 0, time.UTC)
	for _, status := range []Status{StatusOK, StatusDeclined} {
		if _, err := store.SaveRun(Run{Harness: saved.Name, Started: at, Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	last, ok := store.LastRun(saved.Name)
	if !ok || last.Status != StatusDeclined {
		t.Fatalf("the newest run is %q, want the second one written", last.Status)
	}
}
