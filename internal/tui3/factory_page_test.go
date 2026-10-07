package tui3

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// factoryTestNow is the fixture's pinned clock, so every frame below is the
// same frame.
var factoryTestNow = time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)

// factoryPlaceLab is the factory place over the still fixture, opened through
// the router and with its first read folded in, on a frame shorter than the
// rail so the window has to follow the cursor.
func factoryPlaceLab(t *testing.T) *app {
	t.Helper()
	a := placeApp(t)
	a.factory = factory.FixtureSeam(factoryTestNow)
	a.width, a.height = 120, 20
	if cmd := a.showPage(pageFactory); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
	if !a.at(pageFactory) {
		t.Fatal("the factory place did not open")
	}
	if !a.fp.loaded {
		t.Fatal("the factory place opened and never folded its first read in")
	}
	return a
}

// factoryFrameText is the whole frame, plain.
func factoryFrameText(a *app) string {
	f, _, _ := a.frame()
	return plain(f)
}

// WITH NOTHING CONNECTED THE PAGE IS ONE DIM LINE, and no key is offered that
// would need a door.
func TestFactoryWithNoSeamSaysWhatArrivesHere(t *testing.T) {
	a := placeApp(t)
	if cmd := a.showPage(pageFactory); cmd != nil {
		drive(t, a, runCmd(cmd)...)
	}
	if !a.at(pageFactory) {
		t.Fatal("/factory's place did not open without a seam")
	}
	text := strings.Join(strings.Fields(factoryFrameText(a)), " ")
	if !strings.Contains(text, "nothing connected yet · the factory floor arrives here") {
		t.Fatalf("the unconnected page does not say what arrives here:\n%s", factoryFrameText(a))
	}
	if got := (placeFactory{}).hint(a); got != "esc back" {
		t.Fatalf("the unconnected page offers %q", got)
	}
}

// THE FIXTURE DRAWS EVERY GROUP IN ORDER, the cursor's item in the pane.
func TestFactoryDrawsTheFixtureByGroupWithThePane(t *testing.T) {
	a := factoryPlaceLab(t)
	a.height = 40
	text := factoryFrameText(a)
	// The headings are read off the rail's own column, left of the separator.
	var headings []string
	for _, line := range strings.Split(text, "\n") {
		cut := strings.Index(line, "│")
		if cut < 0 {
			continue
		}
		switch word := strings.TrimSpace(line[:cut]); word {
		case "needs you", "streams", "new", "landed", "shipped":
			headings = append(headings, word)
		}
	}
	if got := strings.Join(headings, ","); got != "needs you,streams,new,landed,shipped" {
		t.Fatalf("the rail's groups are %q:\n%s", got, text)
	}
	for _, want := range []string{"#1538", "budget caps per task", "plan is ready · go, or change it?", "[a] answer in words"} {
		if !strings.Contains(text, want) {
			t.Fatalf("the frame is missing %q:\n%s", want, text)
		}
	}
	// AND ↓ MOVES THE PANE WITH THE CURSOR.
	drive(t, a, key("down"))
	if text := factoryFrameText(a); !strings.Contains(text, "the filter is read before the tree exists") {
		t.Fatalf("↓ did not bring the next item into the pane:\n%s", text)
	}
}

// THE RAIL RULE: no rail under the floor, and a bounded rail above it.
func TestFactoryRailColumns(t *testing.T) {
	for _, c := range []struct{ width, want int }{
		{44, 0}, {71, 0}, {72, 24}, {100, 30}, {120, 36}, {200, 40},
	} {
		if got := factoryRailCols(c.width); got != c.want {
			t.Errorf("factoryRailCols(%d) = %d, want %d", c.width, got, c.want)
		}
	}
}

// THE BODY IS EXACTLY THE ROOM IT WAS GIVEN, every row exactly the width.
func TestFactoryBodyFillsTheRoomExactly(t *testing.T) {
	a := factoryPlaceLab(t)
	for _, width := range []int{44, 60, 72, 80, 120, 200} {
		for _, room := range []int{1, 5, 12, 30} {
			rows := a.factoryBody(width, room)
			if len(rows) != room {
				t.Fatalf("at %d×%d the body drew %d rows", width, room, len(rows))
			}
			for i, r := range rows {
				if got := ansi.StringWidth(r.text); got != width {
					t.Fatalf("at %d×%d row %d is %d cells: %q", width, room, i, got, ansi.Strip(r.text))
				}
			}
		}
	}
}

// A FAILED RE-READ KEEPS THE FLOOR IT HAD and says so on the note line.
func TestFactoryKeepsTheLastFloorWhenAReadFails(t *testing.T) {
	a := factoryPlaceLab(t)
	a.factory.Load = func() (factory.Snapshot, error) { return factory.Snapshot{}, errors.New("disk gone") }
	drive(t, a, runCmd(a.factoryRead())...)
	if len(a.fp.snap.Items) == 0 {
		t.Fatal("a failed read dropped the floor")
	}
	if note := strings.Join((placeFactory{}).note(a, 120), ""); !strings.Contains(ansi.Strip(note), "disk gone") {
		t.Fatalf("the note does not say the read failed: %q", note)
	}
}
