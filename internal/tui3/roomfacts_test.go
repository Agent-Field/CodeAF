package tui3

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestRoomFactsGroupOutcomeActivityAndSetupOnOneRow(t *testing.T) {
	a := headRoom(t)
	a.roomNode().model = "z-ai/glm-5.3-flash"
	line := plain(a.roomFactsLine(160))
	if strings.Contains(line, "\n") || ansi.StringWidth(line) != 160 {
		t.Fatal("metadata changed its row budget")
	}
	for _, word := range []string{"working", "3m", "2 tool calls", "bash", "z-ai/glm-5.3-flash", "$0.42", "Stop"} {
		if strings.Count(line, word) != 1 {
			t.Fatalf("metadata lost or repeated %q: %q", word, line)
		}
	}
	state, clock := strings.Index(line, "working"), strings.Index(line, "3m")
	calls, model, cost := strings.Index(line, "2 tool calls"), strings.Index(line, "z-ai/"), strings.Index(line, "$0.42")
	if !(state < clock && clock < calls && calls < model && model < cost) {
		t.Fatalf("groups have no reading hierarchy: %q", line)
	}
	if !strings.Contains(line[state+len("working"):clock], "   ") || !strings.Contains(line[calls:model], "─") {
		t.Fatalf("groups lack separation: %q", line)
	}
	if got := ansi.Cut(line, a.roomStop.from, a.roomStop.to); got != "Stop" {
		t.Fatalf("Stop target moved: %q", got)
	}
}

func TestRoomFactsCompactFallbackFitsAndClearsHiddenStopTargets(t *testing.T) {
	a := headRoom(t)
	a.roomNode().model = "provider/日本語の長いモデル名-for-a-complicated-task"
	for _, width := range []int{160, 80, 60, 40, 24, 12, 4} {
		line := plain(a.roomFactsLine(width))
		if strings.Contains(line, "\n") || ansi.StringWidth(line) > width {
			t.Fatalf("metadata overflow at %d: %q", width, line)
		}
		if a.roomStop.pressable() {
			if a.roomStop.to > width || ansi.Cut(line, a.roomStop.from, a.roomStop.to) != "Stop" {
				t.Fatalf("stale Stop target at %d: %+v %q", width, a.roomStop, line)
			}
		}
	}
}

func TestRoomFactsDoNotInventEmptyMetadataGroups(t *testing.T) {
	a, _, _ := roomApp(t)
	a.openRoom(7, "Fix the nil-map crash")
	line := plain(a.roomFactsLine(160))
	for _, unwanted := range []string{"$0", "0 calls", "0 tool calls", " ·  · "} {
		if strings.Contains(line, unwanted) {
			t.Fatalf("empty metadata became %q", line)
		}
	}
}

func TestBreadcrumbReservesBackWhileKeepingCurrentTaskWhole(t *testing.T) {
	a := crumbApp(t)
	a.width, a.height = 80, 40
	a.title = "A long recognizable conversation"
	line := plain(a.roomTrailRow(a.width))
	if !strings.Contains(line, a.roomHereWord()) {
		t.Fatalf("Back displaced current task: %q", line)
	}
	if !a.roomBackSpan.pressable() || !strings.Contains(line, roomBackWord) {
		t.Fatalf("long path crowded out Back: %q", line)
	}
	for x := a.roomBackSpan.from; x < a.roomBackSpan.to; x++ {
		if !a.roomBackAt(x, a.roomHeadRow()) {
			t.Fatal("Back padding lost its target")
		}
	}
}
