package tui3

import (
	"strings"
	"testing"
)

// ── ctrl+b ON HOME ───────────────────────────────────────────────────────────
//
// The tip `ctrl+b freezes the screen so you can read and copy from it` draws
// over home's box since the two lists merged, and until 2026-09-22 the key on
// home was the emacs `left`: a tip teaching a key that did nothing where it was
// read. Home freezes now the way a room does (copymode.go's [app.freezeHome]),
// and esc gives it back exactly as it was.

func TestCtrlBFreezesHomeAndEscGivesItBack(t *testing.T) {
	a := placeApp(t)
	before := placeFrameText(a)
	drive(t, a, key("ctrl+b"))
	if !a.copy.on {
		t.Fatal("ctrl+b on home did not freeze it")
	}
	if !a.at(pageHome) {
		t.Fatal("freezing home left home")
	}
	if !a.notices.retired("copy-mode") {
		t.Fatal("freezing home did not retire the tip that teaches the key")
	}
	frozen := placeFrameText(a)
	if !strings.Contains(frozen, copyKeysWord) {
		t.Fatalf("the keys row does not name the reader's keys:\n%s", frozen)
	}
	if got := a.noticeHomeHint(); got != "" {
		t.Fatalf("a tip drew over a frozen home: %q", got)
	}
	// THE SNAPSHOT IS THE SCREEN. Every row it holds was on the frame the
	// moment before the key, and the frame now draws those rows.
	if len(a.copy.text) == 0 {
		t.Fatal("the snapshot is empty")
	}
	for _, row := range a.copy.text {
		if row = strings.TrimSpace(row); row != "" && !strings.Contains(before, row) {
			t.Fatalf("the snapshot holds a row the screen did not: %q", row)
		}
	}
	// THE READER'S KEYS MOVE THE CURSOR, AND NOTHING REACHES THE BOX.
	drive(t, a, key("home"))
	if a.copy.at != 0 {
		t.Fatalf("home did not park the cursor on the first row: %d", a.copy.at)
	}
	drive(t, a, key("down"))
	if want := min(1, len(a.copy.rows)-1); a.copy.at != want {
		t.Fatalf("down moved the cursor to %d, want %d", a.copy.at, want)
	}
	drive(t, a, key("x"))
	if !a.home.box.empty() {
		t.Fatalf("a letter reached the box through a frozen home: %q", a.home.box.String())
	}
	// AND ESC IS THE WAY BACK, onto home, with the box as it was.
	drive(t, a, key("esc"))
	if a.copy.on {
		t.Fatal("esc did not thaw home")
	}
	if !a.at(pageHome) {
		t.Fatal("esc from a frozen home left home")
	}
	if !a.home.box.empty() {
		t.Fatal("thawing home put something in the box")
	}
	if strings.Contains(placeFrameText(a), copyKeysWord) {
		t.Fatal("the keys row still names the reader's keys after esc")
	}
}

// A second press of the chord leaves, as it does in a conversation, and a
// frozen home is not frozen twice.
func TestCtrlBOnAFrozenHomeLeaves(t *testing.T) {
	a := placeApp(t)
	placeFrameText(a)
	drive(t, a, key("ctrl+b"))
	if !a.copy.on {
		t.Fatal("ctrl+b on home did not freeze it")
	}
	drive(t, a, key("ctrl+b"))
	if a.copy.on {
		t.Fatal("a second ctrl+b did not leave copy mode")
	}
	if !a.at(pageHome) {
		t.Fatal("leaving copy mode left home")
	}
}
