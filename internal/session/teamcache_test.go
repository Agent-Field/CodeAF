package session

import (
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// A JOURNAL WHOSE STAMP HAS NOT MOVED IS NOT READ AGAIN, and one that moved is.
func TestAMembersJournalIsReadOnlyWhenItMoved(t *testing.T) {
	fixture := newTeamFixture(t, true)
	path := fixture.web
	writeJournal(t, path,
		sessionEntry{Type: "message", Role: "user", Content: "go"},
		sessionEntry{Type: "pace"},
	)
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	var cache journalCache
	now := time.Now()
	if state, _, ok := cache.stateOf(path, now); !ok || state.State != teams.StateIdle {
		t.Fatalf("first read %+v", state)
	}

	// The same size and the same time: the cache must answer, not the file.
	raw, _ := os.ReadFile(path)
	swapped := []byte(string(raw))
	copy(swapped[len(swapped)-len(`"pace"`)-40:], []byte("X"))
	if err := os.WriteFile(path, swapped, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	if state, _, ok := cache.stateOf(path, now); !ok || state.State != teams.StateIdle {
		t.Fatalf("an unmoved journal was read again: %+v", state)
	}

	// A journal that moved is read again.
	writeJournal(t, path,
		sessionEntry{Type: "pace"},
		sessionEntry{Type: "message", Role: "user", Content: "next"},
	)
	if state, _, ok := cache.stateOf(path, time.Now()); !ok || state.State != teams.StateRunning {
		t.Fatalf("a moved journal was not read again: %+v", state)
	}
}

// THE CURSOR FILE IS WRITTEN WHEN A TEAM IS FIRST MET AND WHEN SOMETHING IS
// DELIVERED, never for lines addressed to somebody else.
func TestTheCursorIsWrittenOnlyWhenItMatters(t *testing.T) {
	fixture := newTeamFixture(t, true)
	web := teamAgent(t, fixture, fixture.web, nil, nil)
	web.teamBoundary()
	path := web.teamCursorFile()
	first, err := os.Stat(path)
	if err != nil {
		t.Fatalf("the first cursor was not written: %v", err)
	}
	if err := os.Chtimes(path, first.ModTime().Add(-time.Hour), first.ModTime().Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: "parser", Text: "not for web"})
	if news := web.teamBoundary(); news != "" {
		t.Fatalf("web was told %q", news)
	}
	if after, _ := os.Stat(path); !after.ModTime().Equal(first.ModTime().Add(-time.Hour)) {
		t.Fatal("the cursor was written for a line addressed to somebody else")
	}
	appendTraffic(t, fixture, teams.Entry{Kind: teams.KindNote, From: teams.FromManager, To: "web", Text: "for web"})
	if news := web.teamBoundary(); news == "" {
		t.Fatal("web was not told")
	}
	if after, _ := os.Stat(path); after.ModTime().Equal(first.ModTime().Add(-time.Hour)) {
		t.Fatal("the cursor was not written after a delivery")
	}
}
