package blocks

import (
	"strings"
	"testing"
	"time"
)

// Freeze-at-commit: once a cell commits, the wall clock stops mattering to it.
func TestTimeCellFreezesAtCommit(t *testing.T) {
	cell := NewTimeCell(base)
	cell.Sample(base.Add(4*time.Minute + 12*time.Second))
	if got := cell.String(); got != "4m" {
		t.Fatalf("elapsed %q, want 4m", got)
	}
	cell.Freeze()
	cell.Sample(base.Add(3 * time.Hour))
	if got := cell.String(); got != "4m" {
		t.Fatalf("a committed cell drifted to %q", got)
	}
	if !cell.Frozen() {
		t.Fatal("Frozen reports false after Freeze")
	}
	if got := cell.Elapsed(); got != 4*time.Minute+12*time.Second {
		t.Fatalf("frozen elapsed %v", got)
	}
}

func TestFormatElapsedSwitchesUnitsWithoutJitter(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{999 * time.Millisecond, "0s"},
		{59 * time.Second, "59s"},
		{time.Minute, "1m"},
		{59*time.Minute + 59*time.Second, "59m"},
		{time.Hour, "1h00"},
		{time.Hour + 2*time.Minute, "1h02"},
		{23*time.Hour + 59*time.Minute, "23h59"},
		{-time.Second, "0s"},
	}
	for _, c := range cases {
		if got := FormatElapsed(c.d); got != c.want {
			t.Fatalf("FormatElapsed(%v) = %q, want %q", c.d, got, c.want)
		}
	}
	for _, c := range cases {
		if got := Width(PadLeft(FormatElapsed(c.d), ElapsedCellWidth)); got != ElapsedCellWidth {
			t.Fatalf("%v renders %d cells, not the reserved %d", c.d, got, ElapsedCellWidth)
		}
	}
}

// The corollary of 8.1.2: committed bytes never drift. A finalized block
// re-rendered an hour later — at a new width, even — still says what it said
// when it committed.
func TestCommittedBytesNeverDrift(t *testing.T) {
	tr := New(60, 10)
	tr.Strict = false

	turn := NewText("turn", Header{Glyph: "◐", Title: "aforge"})
	turn.StartClock(base)
	turn.Write("the answer, such as it was")
	tr.Append(turn)

	// While live, the elapsed meta ages.
	for _, at := range []time.Duration{time.Second, 30 * time.Second, 90 * time.Second} {
		turn.Elapsed().Sample(base.Add(at))
		turn.Mutate(func(b *TextBlock) { b.Head.Meta = []string{b.Elapsed().String()} })
		tr.Frame(base.Add(at))
	}
	if !strings.Contains(rowsOf(tr.Frame(base.Add(90*time.Second))), "1m") {
		t.Fatal("a live elapsed did not age")
	}

	turn.Finalize(EndCompleted)
	committed := rowsOf(tr.Frame(base.Add(90 * time.Second)))

	// An hour of frames later, and a resize, and the bytes are the same.
	for _, at := range []time.Duration{2 * time.Minute, 10 * time.Minute, time.Hour} {
		turn.Elapsed().Sample(base.Add(at))
		if got := rowsOf(tr.Frame(base.Add(at))); got != committed {
			t.Fatalf("committed bytes drifted after %v:\n got %q\nwant %q", at, got, committed)
		}
	}
	if !strings.Contains(committed, "1m") {
		t.Fatalf("the committed row lost its frozen elapsed:\n%s", committed)
	}

	// A resize re-renders the block from scratch; the frozen cell still says
	// what it said when it committed.
	tr.SetSize(40, 10)
	if got := rowsOf(tr.Frame(base.Add(2 * time.Hour))); !strings.Contains(got, "1m") {
		t.Fatalf("a resize re-derived the elapsed from the wall clock:\n%s", got)
	}
}

func TestTimeCellCachesItsText(t *testing.T) {
	cell := NewTimeCell(base)
	cell.Sample(base.Add(12 * time.Second))
	first := cell.String()
	if got := allocs(func() { _ = cell.String() }); got != 0 {
		t.Fatalf("a repeated read of an unchanged cell allocated %.0f times", got)
	}
	cell.Sample(base.Add(13 * time.Second))
	if cell.String() == first {
		t.Fatal("the cell cached past a real change")
	}
}
