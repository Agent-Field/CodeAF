package tui3

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestReopenSharedRemoteTabKeepsRefusalForRetryFromHome(t *testing.T) {
	a, engine, handle := sharedSurface(t)
	a.width, a.height = 120, 40
	a.draftFile = filepath.Join(t.TempDir(), "draft.txt")
	a.input.setText("draft belonging to A")
	tab := rememberUnheldTab(a, "/srv/app/b.jsonl", "/srv/app", "Remote B")
	a.tabShutKey(tab.key)
	_ = a.showPage(pageHome)
	original := a.resume
	attempts := 0
	a.resume = func(file string) (Agent, error) {
		attempts++
		if file != tab.file {
			t.Fatalf("reopen requested %q instead of the stored remote address", file)
		}
		if attempts == 1 {
			return nil, errors.New("remote connection is unavailable")
		}
		return original(file)
	}
	drive(t, a, reopenPress())
	if attempts != 1 || len(a.closedTabs) != 1 || !a.tabShut[tab.key] || !a.at(pageHome) || a.file != "/srv/app/a.jsonl" {
		t.Fatal("failed remote reopen was filtered locally, consumed history or changed selection")
	}
	if !strings.Contains(plain(frame(a)), "remote connection is unavailable") {
		t.Fatal("Home did not show why reopening failed")
	}
	drive(t, a, reopenPress())
	if attempts != 2 || len(a.closedTabs) != 0 || a.tabShut[tab.key] || a.at(pageHome) || a.file != tab.file || engine.at != tab.file {
		t.Fatal("repeating the chord did not retry and reopen the same remote tab")
	}
	if handle.closes != 0 || len(engine.shut) != 0 || len(engine.stopped) != 0 || len(engine.ended) != 1 {
		t.Fatal("reopening stopped the remote handle beyond its normal session swap")
	}
	a.resume = original
	cmd, refusal := a.openBeside("/srv/app", "/srv/app/a.jsonl")
	if refusal != "" {
		t.Fatal(refusal)
	}
	drain(t, a, cmd)
	if a.input.String() != "draft belonging to A" {
		t.Fatal("failed/retried reopening lost the outgoing conversation's draft")
	}
}

func TestReopenSkipsTheLastClosedTabAfterEscapeAlreadyReturnedToIt(t *testing.T) {
	a := reopenApp(t)
	var closed []string
	for i := 0; i < 3; i++ {
		closed = append(closed, a.file)
		drive(t, a, key(closeTabChord))
	}
	if !a.at(pageHome) {
		t.Fatal("closing the final tab did not go Home")
	}
	drive(t, a, key("esc"))
	if a.pageShowing() || a.file != closed[2] {
		t.Fatal("Escape did not return to the same underlying conversation")
	}
	drive(t, a, reopenPress())
	if a.file != closed[1] || a.tabShut[a.convKey(closed[2])] {
		t.Fatal("reopen spent a key on the already-visible tab instead of the prior closure")
	}
}
