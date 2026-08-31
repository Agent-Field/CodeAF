package tui3

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// THE INDEX IS ONE FILE FOR THE WHOLE MACHINE, SO IT IS READ ONCE FOR THE WHOLE
// SCREEN.
//
// This is a PERFORMANCE law and it is stated as one because the cost of getting
// it wrong is invisible in every other test: the band drew the right rows the
// whole time it was re-parsing a megabyte of JSON on every arrival and every
// third second. What went wrong was the SHAPE of the cache — keyed by the row,
// over a reading that is global — which multiplied one file read by the number
// of rows a person walks past. Measured on a real machine with a 900KB index:
// 10ms of every keystroke, against 0.16ms for the whole rest of the frame.
//
// So: one reading on `open`, none at all while walking the list, and another
// only when the file has actually changed.
func TestTheDeliverablesIndexIsReadOnceForTheWholeScreen(t *testing.T) {
	lab := newHomeLab(t)
	now := time.Now()
	alpha, beta := lab.workspace("alpha"), lab.workspace("beta")
	mine := lab.session("-alpha", "aaaa000000000001", "alpha chat", alpha, now)
	for i := 0; i < 5; i++ {
		lab.session("-beta", "bbbb00000000000"+strconv.Itoa(i+1),
			"beta chat "+strconv.Itoa(i), beta, now.Add(-time.Duration(i+1)*time.Hour))
	}
	a := lab.app(mine)

	// A REAL INDEX, WRITTEN THE ONLY WAY ONE IS EVER WRITTEN.
	dir := t.TempDir()
	index := filepath.Join(dir, session.ArtifactsIndexName)
	made := func(id, name string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
		session.RecordArtifact(index, session.Artifact{
			Path: path, Session: id, Title: name, Created: now.Add(-time.Hour),
		})
	}
	made("aaaa000000000001", "alpha.md")
	made("bbbb000000000001", "beta.md")
	a.artifacts = index

	reads := 0
	real := readArtifactIndex
	readArtifactIndex = func(path string) []session.Artifact {
		reads++
		return real(path)
	}
	t.Cleanup(func() { readArtifactIndex = real })

	a.width, a.height = homeCardMin, 40
	a.openHome()
	if reads != 1 {
		t.Fatalf("opening home read the index %d times, want exactly one reading", reads)
	}

	// WALKING THE LIST READS NOTHING. Every row is a card and every card asks
	// this band, so this is the whole claim: the arrival is a map lookup.
	for i := 0; i < 10; i++ {
		drive(t, a, key("down"))
		a.homeFrame(a.width, a.height)
	}
	if reads != 1 {
		t.Fatalf("walking ten rows read the index %d times, want the one reading open took", reads)
	}

	// AND NEITHER DOES SITTING STILL. Home's beat asks for the reading and gets
	// one os.Stat, because nothing has written a file since.
	a.refreshHome()
	a.refreshHome()
	if reads != 1 {
		t.Fatalf("two beats over an unchanged index read it %d times", reads)
	}

	// AND A FILE MADE SINCE ARRIVES ON THE NEXT BEAT, which is what the stat is
	// FOR: the cheapness may not cost a person the file they just made.
	made("aaaa000000000001", "second.md")
	a.refreshHome()
	if reads != 2 {
		t.Fatalf("a beat after a file was written read the index %d times, want a second reading", reads)
	}
	a.home.point(mine)
	card := strings.Join(workCard(t, a), "\n")
	if !strings.Contains(card, "second.md") {
		t.Fatalf("the file made since the last reading is not on the card:\n%s", card)
	}
	// AND THE ROWS ARE STILL FILED UNDER THE CONVERSATION THAT MADE THEM.
	if strings.Contains(card, "beta.md") {
		t.Fatalf("another conversation's file landed on this card:\n%s", card)
	}
}
