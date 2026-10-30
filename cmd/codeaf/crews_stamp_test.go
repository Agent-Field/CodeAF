package main

import (
	"os"
	"testing"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// THE CREWS WATCHERS SEE TWO WRITES IN ONE CLOCK TICK. On a filesystem that
// keeps modification times in whole seconds, two writes in one second leave
// teams.db's stat as it was: WAL mode does not grow the file between
// checkpoints. The app's crew poll and the work graph's crew read follow the
// store's own count of its writes ([teamstore.Stamp]), which moves anyway.
func TestTheCrewsWatchersSeeTwoWritesInOneClockTick(t *testing.T) {
	backend, _, profile, _ := closedCrewRig(t)
	rename := func(name string) {
		t.Helper()
		if err := teamstore.Update(profile, func(f *teamstore.File) error {
			f.Teams[teamstore.Index(f.Teams, "0d0d0d0d0d0d")].Name = name
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	rename("dock one")
	path := teamstore.Path(profile)
	was, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	run := &appCrewRun{cursors: map[string]string{}}
	inputs := backend.crewInputs(run)
	var graph appWorkGraph
	if _, _, err := graph.crewFile(profile, "0d0d0d0d0d0d"); err != nil {
		t.Fatal(err)
	}

	rename("dock two")
	// The coarse clock: the second write's time is the first's.
	if err := os.Chtimes(path, was.ModTime(), was.ModTime()); err != nil {
		t.Fatal(err)
	}
	if now, err := os.Stat(path); err != nil || now.Size() != was.Size() || !now.ModTime().Equal(was.ModTime()) {
		t.Fatalf("the rig did not leave teams.db's stat as it was: %v", err)
	}
	if backend.crewInputs(run) == inputs {
		t.Fatal("the crew poll missed a write its clock tick hid")
	}
	file, _, err := graph.crewFile(profile, "0d0d0d0d0d0d")
	if err != nil {
		t.Fatal(err)
	}
	if crew, _ := file.Team("0d0d0d0d0d0d"); crew.Name != "dock two" {
		t.Fatalf("the work graph read the crew as %q", crew.Name)
	}
}
