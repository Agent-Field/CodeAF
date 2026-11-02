package session

import (
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// A SESSION SEES TWO TEAM WRITES IN ONE CLOCK TICK. Its team watch asks
// whether the teams moved by the store's own count of its writes, not by
// teams.db's stat, which a coarse clock and WAL mode can leave unchanged.
func TestTheTeamsStampSeesTwoWritesInOneClockTick(t *testing.T) {
	profile := t.TempDir()
	rename := func(name string) {
		t.Helper()
		if err := teams.Update(profile, func(f *teams.File) error {
			if len(f.Teams) == 0 {
				f.Teams = []teams.Team{{ID: "aaaaaaaaaaaa"}}
			}
			f.Teams[0].Name = name
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	fresh := func() fileStamp {
		teamsStampMemo.mu.Lock()
		teamsStampMemo.at = nil
		teamsStampMemo.mu.Unlock()
		return sharedTeamsStamp(profile)
	}
	if got := fresh(); got.present {
		t.Fatalf("no store reads as present: %+v", got)
	}
	rename("one")
	path := teams.Path(profile)
	was, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	before := fresh()
	if !before.present {
		t.Fatal("the store reads as missing")
	}
	rename("two")
	if err := os.Chtimes(path, was.ModTime(), was.ModTime()); err != nil {
		t.Fatal(err)
	}
	if now, err := os.Stat(path); err != nil || now.Size() != was.Size() {
		t.Fatalf("the rig changed teams.db's size: %v", err)
	}
	if fresh() == before {
		t.Fatal("two writes in one clock tick read as none")
	}
}
