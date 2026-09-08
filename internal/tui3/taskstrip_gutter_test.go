package tui3

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/charmbracelet/x/ansi"
)

func TestTaskStripGutterMovesItsPaintAndPointerTargetsTogether(t *testing.T) {
	a, _, _ := roomApp(t)
	for i := 8; i < 13; i++ {
		a.taskUpdate(update(uint64(i), "Investigate parser case "+itoa(i), session.TaskRunning, session.TaskNotice{}))
	}
	for _, width := range []int{80, 62, 99, 62, 80} {
		a.width = width
		a.touch()
		rows := a.stripRows(width)
		lead := textGutterCols(width)
		if len(rows) != 2 || rows[1] != "" || !strings.HasPrefix(plain(rows[0]), strings.Repeat(" ", lead+1)) {
			t.Fatalf("width %d: strip lost its gutter or separating blank: %q", width, rows)
		}
		if ansi.StringWidth(rows[0]) > width || len(a.stripSpans) == 0 || a.stripSpans[0].span.from != lead {
			t.Fatalf("width %d: strip overflows or shifts on repeat layout", width)
		}
		if hot, ok := a.stripHoverAt(lead-1, a.headHeight()); ok || hot.kind != hoverNothing {
			t.Fatal("the reading gutter advertises a task")
		}
		if _, took := a.stripPress(lead-1, a.headHeight()); !took || a.roomOpen() {
			t.Fatal("a gutter click fell through or opened a task")
		}
		for _, chip := range a.stripSpans {
			hot, ok := a.stripHoverAt(chip.span.from, a.headHeight())
			if !ok || hot.kind != hoverStrip || hot.id != chip.id || chip.span.to > width {
				t.Fatal("a painted chip and its pointer target disagree")
			}
		}
		if !a.stripMore.pressable() || !strings.HasPrefix(ansi.Cut(plain(rows[0]), a.stripMore.from, a.stripMore.to), "+") {
			t.Fatal("the overflow target did not move with its painted count")
		}
	}
	chip := a.stripSpans[0]
	if _, took := a.stripPress(chip.span.from, a.headHeight()); !took || !a.roomOpen() || a.room.id != chip.id {
		t.Fatal("the inset chip no longer opens its own task")
	}
}

func TestTaskStripGutterMovesTheHarnessAndLeavesThePhoneDoorAlone(t *testing.T) {
	a := newTestApp(&runningAgent{fakeAgent: &fakeAgent{}, name: "triage-flake"})
	a.width, a.height = 80, 30
	rows := a.stripRows(a.width)
	if len(rows) != 2 || a.stripHarn.from != textGutterCols(a.width) || !strings.Contains(ansi.Cut(plain(rows[0]), a.stripHarn.from, a.stripHarn.to), "triage-flake") {
		t.Fatal("the harness chip's text and hit map did not receive the gutter together")
	}
	if hot, ok := a.stripHoverAt(a.stripHarn.from, a.headHeight()); !ok || hot.kind != hoverStripHarness {
		t.Fatal("the inset harness lost its pointer target")
	}
	phone, _, _ := taskApp(t)
	phone.width = 44
	phone.taskUpdate(update(7, "Check parsing", session.TaskRunning, session.TaskNotice{}))
	rows = phone.stripRows(phone.width)
	if len(rows) != 2 || strings.HasPrefix(plain(rows[0]), " ") || len(phone.stripSpans) != 0 || phone.stripMore.pressable() || phone.stripHarn.pressable() {
		t.Fatal("the phone's whole-row task door acquired desktop padding or chip targets")
	}
}
