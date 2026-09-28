package tui3

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// A finished conversation's first wall reading uses the conversation's own
// last write, so opening the wall does not claim that old work happened now.
func TestWallFirstReadShowsConversationFileActivity(t *testing.T) {
	now := time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)
	file := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(file, []byte("saved conversation\n"), 0600); err != nil {
		t.Fatal(err)
	}
	last := now.Add(-5 * time.Minute)
	if err := os.Chtimes(file, last, last); err != nil {
		t.Fatal(err)
	}
	agent := &fakeAgent{model: "m", past: []session.DisplayEntry{{Role: "user", Text: "hello"}, {Role: "assistant", Text: "done"}}}
	a := newTestApp(agent)
	a.file, a.workspace, a.title = file, filepath.Dir(file), "A finished conversation"
	a.width, a.height = 120, 40
	a.clock = func() time.Time { return now }
	_ = a.openWall()
	drive(t, a, runCmd(a.wallReadCmd(a.frontTabKey()))...)
	if got := wallTileRowsFor(t, a, a.frontTabKey()); !strings.Contains(got, "updated 5m ago") {
		t.Fatalf("the first idle tile did not show its last activity:\n%s", got)
	}
	now = now.Add(2 * time.Minute)
	if got := wallTileRowsFor(t, a, a.frontTabKey()); !strings.Contains(got, "updated 7m ago") {
		t.Fatalf("the idle tile did not age with the wall clock:\n%s", got)
	}
}
