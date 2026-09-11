package tui3

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestCompactStepElapsedTimeStartsAfterTenSeconds(t *testing.T) {
	a := liveStepsApp(t)
	base := liveStepRowsOf(t, a, 100)
	for _, tc := range []struct {
		age  time.Duration
		want string
	}{{9999 * time.Millisecond, ""}, {10 * time.Second, "10s"}, {11 * time.Second, "11s"}, {61 * time.Second, "1m 1s"}} {
		a.clock = func() time.Time { return liveStepsBase.Add(7*time.Second + tc.age) }
		a.touch()
		out := liveStepRowsOf(t, a, 100)
		if len(out) != liveStepRows {
			t.Fatalf("elapsed time changed the row budget: %d", len(out))
		}
		for i, r := range out {
			want := plain(base[i].text)
			if i == len(out)-1 && tc.want != "" {
				want += "  " + tc.want
			}
			if plain(r.text) != want {
				t.Fatalf("age %s: got %q, want %q", tc.age, plain(r.text), want)
			}
		}
	}
}

func TestCompactStepTimerKeepsLayoutAndCaptionIdentity(t *testing.T) {
	for _, width := range []int{8, 9, 20, 34, 80} {
		a := liveStepsApp(t)
		before := liveStepRowsOf(t, a, width)
		a.clock = func() time.Time { return liveStepsBase.Add(19 * time.Second) }
		a.touch()
		after := liveStepRowsOf(t, a, width)
		if len(before) != len(after) {
			t.Fatalf("width %d: timer reflowed the caption", width)
		}
		for i := range after {
			shown := plain(after[i].text)
			if strings.TrimSuffix(shown, "  12s") != plain(before[i].text) {
				t.Fatalf("width %d: timer changed words or icon: %q -> %q", width, plain(before[i].text), shown)
			}
			if ansi.StringWidth(after[i].text) > width-workIndentCols(width) {
				t.Fatalf("width %d overflowed: %q", width, shown)
			}
		}
	}
	if compactStepAge(time.Time{}, liveStepsBase) != "" || compactStepAge(liveStepsBase.Add(time.Minute), liveStepsBase) != "" {
		t.Fatal("an unknown or future start invented elapsed time")
	}
}

func TestCompactStepTimerUsesTheBatchStartAcrossRenaming(t *testing.T) {
	a := liveStepsApp(t)
	a.clock = func() time.Time { return liveStepsBase.Add(19 * time.Second) }
	a.entries[len(a.entries)-2].text = "starting the local server"
	shown := liveStepRowsOf(t, a, 100)
	if !strings.HasSuffix(plain(shown[len(shown)-1].text), "starting the local server  12s") {
		t.Fatalf("renaming reset the step clock: %q", plain(shown[len(shown)-1].text))
	}
	a.entries[len(a.entries)-1].began = a.now()
	shown = liveStepRowsOf(t, a, 100)
	if strings.HasSuffix(plain(shown[len(shown)-1].text), "12s") {
		t.Fatal("a new batch retained the previous step's elapsed time")
	}
}

func TestCompactStepTimerBelongsToTheRoomBatch(t *testing.T) {
	a := roomCompactApp(t)
	a.turnBegan = liveStepsBase.Add(-time.Hour)
	a.clock = func() time.Time { return liveStepsBase.Add(14 * time.Second) }
	a.touch()
	page := roomText(a)
	if !strings.Contains(page, "  10s") {
		t.Fatalf("the room did not time its own batch:\n%s", page)
	}
	a.room.entries[len(a.room.entries)-1].status = toolOK
	a.room.entries[len(a.room.entries)-1].ended = a.now()
	a.room.dirty = true
	a.touch()
	if strings.Contains(roomText(a), "  10s") {
		t.Fatal("a completed room batch kept its live timer")
	}
}
