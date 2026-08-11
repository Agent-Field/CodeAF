package tui2

import (
	"image"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestCompositorDrawsLayersInZOrder(t *testing.T) {
	var c compositor
	c.setLayout(layout{
		Width: 10, Height: 3,
		Slots: []slot{
			{ID: LayerTranscript, Rect: image.Rect(0, 0, 10, 3), Z: zBase},
			{ID: LayerOverlay, Rect: image.Rect(2, 1, 8, 2), Z: zOverlay},
		},
	})
	c.setContent(LayerTranscript, strings.Repeat("aaaaaaaaaa\n", 3))
	c.setContent(LayerOverlay, "BBBBBB")

	got := c.render()
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("wanted 3 rows, got %d: %q", len(lines), got)
	}
	if lines[0] != "aaaaaaaaaa" {
		t.Fatalf("row 0 = %q", lines[0])
	}
	if lines[1] != "aaBBBBBBaa" {
		t.Fatalf("overlay did not land on top: %q", lines[1])
	}
}

func TestCompositorClipsInsteadOfGrowing(t *testing.T) {
	var c compositor
	c.setLayout(layout{
		Width: 6, Height: 2,
		Slots: []slot{{ID: LayerTranscript, Rect: image.Rect(0, 0, 6, 2), Z: zBase}},
	})
	// A pane that misbehaves in both axes must produce a clipped frame, never a
	// broken one — the shell trusts the contract and the compositor enforces it.
	c.setContent(LayerTranscript, "way too wide for six columns\nsecond\nthird\nfourth")
	got := c.render()
	for i, line := range strings.Split(got, "\n") {
		if len([]rune(line)) > 6 {
			t.Fatalf("row %d escaped its width: %q", i, line)
		}
	}
	if rows := strings.Count(got, "\n") + 1; rows != 2 {
		t.Fatalf("frame is %d rows, want 2", rows)
	}
}

// Content and geometry are independent: a pane that filled two rows of a
// twenty-row region is still clickable in all twenty. This is the property
// Lip Gloss v2's own Layer does not have, and the reason we keep our own table.
func TestCompositorHitUsesGeometryNotContent(t *testing.T) {
	var c compositor
	c.setLayout(layout{
		Width: 20, Height: 20,
		Slots: []slot{{ID: LayerRail, Rect: image.Rect(0, 0, 20, 20), Z: zBase}},
	})
	c.setContent(LayerRail, "two\nrows")

	id, local, ok := c.hit(19, 19)
	if !ok || id != LayerRail {
		t.Fatalf("hit at the empty corner = %v %v", id, ok)
	}
	if local != image.Pt(19, 19) {
		t.Fatalf("local point = %v", local)
	}
}

func TestCompositorHitReturnsPaneLocalCoordinates(t *testing.T) {
	var c compositor
	c.setLayout(layout{
		Width: 100, Height: 30,
		Slots: []slot{
			{ID: LayerTranscript, Rect: image.Rect(0, 0, 72, 26), Z: zBase},
			{ID: LayerRail, Rect: image.Rect(72, 0, 100, 29), Z: zBase},
			{ID: LayerStatus, Rect: image.Rect(0, 29, 100, 30), Z: zBase},
		},
	})

	id, local, ok := c.hit(75, 4)
	if !ok || id != LayerRail {
		t.Fatalf("rail hit = %v %v", id, ok)
	}
	if local != image.Pt(3, 4) {
		t.Fatalf("rail-local = %v, want (3,4)", local)
	}

	if id, _, _ := c.hit(0, 29); id != LayerStatus {
		t.Fatalf("status row hit = %v", id)
	}
	if id, _, ok := c.hit(200, 200); ok || id != LayerNone {
		t.Fatalf("off-screen hit = %v %v", id, ok)
	}
}

func TestCompositorHitPrefersTheTopPlane(t *testing.T) {
	var c compositor
	c.setLayout(layout{
		Width: 40, Height: 10,
		Slots: []slot{
			{ID: LayerTranscript, Rect: image.Rect(0, 0, 40, 10), Z: zBase},
			{ID: LayerOverlay, Rect: image.Rect(10, 2, 30, 8), Z: zOverlay},
		},
	})
	if id, _, _ := c.hit(15, 5); id != LayerOverlay {
		t.Fatalf("overlay did not take the click: %v", id)
	}
	if id, _, _ := c.hit(2, 5); id != LayerTranscript {
		t.Fatalf("outside the overlay should reach the transcript: %v", id)
	}
}

func TestCompositorSortsSlotsHandedToItOutOfOrder(t *testing.T) {
	var c compositor
	c.setLayout(layout{
		Width: 10, Height: 4,
		Slots: []slot{
			{ID: LayerOverlay, Rect: image.Rect(0, 0, 10, 2), Z: zOverlay},
			{ID: LayerTranscript, Rect: image.Rect(0, 0, 10, 4), Z: zBase},
		},
	})
	if c.layers[0].id != LayerTranscript || c.layers[1].id != LayerOverlay {
		t.Fatalf("layers not sorted by z: %v %v", c.layers[0].id, c.layers[1].id)
	}
}

func TestCompositorZeroSizeRendersNothing(t *testing.T) {
	var c compositor
	for _, size := range [][2]int{{0, 0}, {0, 10}, {10, 0}, {-1, -1}} {
		c.setLayout(layout{Width: size[0], Height: size[1]})
		if got := c.render(); got != "" {
			t.Fatalf("%v rendered %q", size, got)
		}
		if id, _, ok := c.hit(0, 0); ok || id != LayerNone {
			t.Fatalf("%v hit-tested to %v", size, id)
		}
	}
	// And it comes back afterwards, which is the reattach case.
	c.setLayout(layout{
		Width: 4, Height: 1,
		Slots: []slot{{ID: LayerStatus, Rect: image.Rect(0, 0, 4, 1)}},
	})
	c.setContent(LayerStatus, "ok")
	if got := c.render(); !strings.HasPrefix(got, "ok") {
		t.Fatalf("recovered frame = %q", got)
	}
}

func TestCompositorLayerThatVanishedLeavesNothingBehind(t *testing.T) {
	var c compositor
	c.setLayout(layout{
		Width: 8, Height: 2,
		Slots: []slot{
			{ID: LayerTranscript, Rect: image.Rect(0, 0, 8, 1)},
			{ID: LayerRail, Rect: image.Rect(0, 1, 8, 2)},
		},
	})
	c.setContent(LayerTranscript, "chat")
	c.setContent(LayerRail, "railrail")
	_ = c.render()

	// The rail leaves; the transcript takes both rows. The reused backing
	// array must not carry the rail's old bytes into the new frame.
	c.setLayout(layout{
		Width: 8, Height: 2,
		Slots: []slot{{ID: LayerTranscript, Rect: image.Rect(0, 0, 8, 2)}},
	})
	c.setContent(LayerTranscript, "chat")
	got := c.render()
	if strings.Contains(got, "rail") {
		t.Fatalf("ghost frame: %q", got)
	}
}

// The hot path is the part that runs on every frame and every mouse event.
// Layout and content assignment reuse their storage, and hit testing walks a
// slice — none of it may hand the collector anything.
func TestCompositorHotPathDoesNotAllocate(t *testing.T) {
	var c compositor
	l := solve(120, 40, DefaultMetrics(), mode{OverlayOpen: true})
	c.setLayout(l)

	if allocs := testingAllocs(func() {
		c.setLayout(l)
	}); allocs != 0 {
		t.Fatalf("setLayout allocated %v times per run", allocs)
	}

	if allocs := testingAllocs(func() {
		c.setContent(LayerTranscript, "hello")
		c.setContent(LayerRail, "rail")
		c.setContent(LayerStatus, "status")
	}); allocs != 0 {
		t.Fatalf("setContent allocated %v times per run", allocs)
	}

	if allocs := testingAllocs(func() {
		c.hit(60, 20)
		c.hit(200, 200)
		c.rect(LayerRail)
	}); allocs != 0 {
		t.Fatalf("hit testing allocated %v times per run", allocs)
	}
}

// The solver reuses the caller's slice, so a resize storm costs no garbage.
func TestSolveIntoReusesItsSlice(t *testing.T) {
	metrics := DefaultMetrics()
	l := solve(120, 40, metrics, mode{})
	if allocs := testingAllocs(func() {
		l = solveInto(l.Slots, 120, 40, metrics, mode{})
	}); allocs != 0 {
		t.Fatalf("solveInto allocated %v times per run", allocs)
	}
}

func testingAllocs(f func()) float64 {
	return testing.AllocsPerRun(200, f)
}

// A frame is a picture of every cell in the terminal, and a row that stops
// early says nothing about the cells past the cut — so whatever the LAST frame
// left there stays on screen. The screenshot harness caught that ghost three
// ways in one scrape of a task room: a rail row reading two different frames
// superimposed ("1 running Aforge spine ↦"), an "○ more" left over from the home
// scope under a task scope with no members, and a three-row gap where the
// previous frame's rows had been. All three read as rail-content bugs and none
// of them was one: the rail rendered two lines and meant two lines.
//
// The invariant, pinned at the compositor because that is where the contract
// with the terminal lives: every row of a frame is exactly as wide as the frame,
// and there are exactly as many rows as the frame is tall — whatever the panes
// did or did not fill.
func TestFrameStatesEveryCellSoAShorterPaneCannotGhost(t *testing.T) {
	var c compositor
	c.setLayout(layout{
		Width: 20, Height: 4,
		Slots: []slot{
			{ID: LayerTranscript, Rect: image.Rect(0, 0, 12, 4), Z: zBase},
			{ID: LayerRail, Rect: image.Rect(12, 0, 20, 4), Z: zBase},
		},
	})

	// A tall frame, then the same layout with a rail that got shorter and a
	// transcript that emptied — the exact transition the ghost appears in.
	c.setContent(LayerTranscript, "aaaa\nbbbb\ncccc\ndddd")
	c.setContent(LayerRail, "r0\nr1\nr2\nr3")
	_ = c.render()

	c.setContent(LayerTranscript, "")
	c.setContent(LayerRail, "r0")
	got := c.render()

	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("frame is %d rows, want 4: %q", len(lines), got)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != 20 {
			t.Fatalf("row %d is %d cells, want 20: %q", i, w, line)
		}
	}
	for i, line := range lines[1:] {
		if strings.TrimSpace(line) != "" {
			t.Fatalf("row %d kept the previous frame's content: %q", i+1, line)
		}
	}
	if strings.TrimSpace(lines[0]) != "r0" {
		t.Fatalf("row 0 = %q, want only the rail's one line", lines[0])
	}
}

// The padding must not cost bandwidth on a settled screen: a frame that already
// fills its width is returned untouched, and the shell's repaint gate means an
// unchanged frame is never rebuilt at all (TestShellFrameIsStableWhenNothingMoved).
func TestFramePaddingIsFreeWhenNothingIsShort(t *testing.T) {
	full := "abcde\nfghij"
	if got := padFrame(full, 5); got != full {
		t.Fatalf("a full-width frame was rewritten: %q", got)
	}
	if got := padFrame("ab\ncd", 0); got != "ab\ncd" {
		t.Fatalf("a zero-width frame was rewritten: %q", got)
	}
}
