package tui2

import (
	"image"
	"testing"
)

// The solver is a pure function of two integers and a mode, so every shape the
// surface can take is reachable here — including the ones a person only
// produces by dragging a window corner through them.

func TestLayoutSlotsNeverOverlapOrEscape(t *testing.T) {
	metrics := DefaultMetrics()
	modes := []mode{
		{},
		{ScopeOpen: true},
		{Linear: true},
		{Linear: true, ScopeOpen: true},
		{OverlayOpen: true},
		{ScopeOpen: true, OverlayOpen: true},
	}
	for _, md := range modes {
		for w := 0; w <= 140; w++ {
			for h := 0; h <= 40; h++ {
				l := solve(w, h, metrics, md)
				frame := image.Rect(0, 0, w, h)
				lastZ := 0
				for i, slot := range l.Slots {
					if slot.Rect.Empty() {
						t.Fatalf("%dx%d %+v: empty slot %v", w, h, md, slot.ID)
					}
					if !slot.Rect.In(frame) {
						t.Fatalf("%dx%d %+v: %v at %v escapes the frame", w, h, md, slot.ID, slot.Rect)
					}
					if i > 0 && slot.Z < lastZ {
						t.Fatalf("%dx%d %+v: slots not emitted in ascending z", w, h, md)
					}
					lastZ = slot.Z
					for _, other := range l.Slots[i+1:] {
						if slot.Z != other.Z {
							continue
						}
						if slot.Rect.Overlaps(other.Rect) {
							t.Fatalf("%dx%d %+v: %v and %v overlap on the same plane",
								w, h, md, slot.ID, other.ID)
						}
					}
				}
			}
		}
	}
}

func TestLayoutStatusIsTheLastThingGivenUp(t *testing.T) {
	metrics := DefaultMetrics()
	// One row, one column: the smallest terminal anyone can produce. It gets
	// the line that explains the rest, and nothing else.
	l := solve(1, 1, metrics, mode{})
	if len(l.Slots) != 1 || l.Slots[0].ID != LayerStatus {
		t.Fatalf("1x1 wanted only a status line, got %v", l.Slots)
	}
	if got := l.Slots[0].Rect; got != image.Rect(0, 0, 1, 1) {
		t.Fatalf("1x1 status rect = %v", got)
	}

	// Two rows: status, then as much composer as fits. A chat you cannot type
	// into is not a chat.
	l = solve(20, 2, metrics, mode{})
	if len(l.Slots) != 2 || l.Slots[0].ID != LayerComposer || l.Slots[1].ID != LayerStatus {
		t.Fatalf("20x2 = %v", l.Slots)
	}
}

func TestLayoutZeroSizeDrawsNothing(t *testing.T) {
	for _, size := range [][2]int{{0, 0}, {0, 40}, {80, 0}, {-4, -4}} {
		l := solve(size[0], size[1], DefaultMetrics(), mode{})
		if len(l.Slots) != 0 {
			t.Fatalf("%v produced slots %v", size, l.Slots)
		}
		if !l.Narrow {
			t.Fatalf("%v should read as narrow", size)
		}
	}
}

func TestLayoutRailAppearsOnlyAtTheBreakpoint(t *testing.T) {
	metrics := DefaultMetrics()
	for w := 0; w < metrics.RailBreakpoint; w++ {
		l := solve(w, 30, metrics, mode{})
		if !l.Narrow {
			t.Fatalf("width %d claimed a rail below the breakpoint", w)
		}
		for _, slot := range l.Slots {
			if slot.ID == LayerRail {
				t.Fatalf("width %d drew a rail column", w)
			}
		}
	}
	l := solve(metrics.RailBreakpoint, 30, metrics, mode{})
	if l.Narrow {
		t.Fatalf("width %d should carry the rail", metrics.RailBreakpoint)
	}
	var rail, transcript image.Rectangle
	for _, slot := range l.Slots {
		switch slot.ID {
		case LayerRail:
			rail = slot.Rect
		case LayerTranscript:
			transcript = slot.Rect
		}
	}
	if rail.Dx() != metrics.RailWidth {
		t.Fatalf("rail width = %d, want %d", rail.Dx(), metrics.RailWidth)
	}
	if transcript.Dx() < metrics.MinMainWidth {
		t.Fatalf("transcript squeezed to %d, below MinMainWidth %d", transcript.Dx(), metrics.MinMainWidth)
	}
	// The rail stands beside the composer too — it is the stable element.
	if rail.Max.Y <= transcript.Max.Y {
		t.Fatalf("rail %v stops at the transcript's bottom %v", rail, transcript)
	}
}

// The scope model is width-independent (5.15): the same layer id, and
// therefore the same keys, the same selection and the same hit target, whether
// scope is a column or the whole pane.
func TestLayoutScopeIsTheSameLayerAtEveryWidth(t *testing.T) {
	metrics := DefaultMetrics()

	wide := solve(120, 30, metrics, mode{ScopeOpen: true})
	narrow := solve(70, 30, metrics, mode{ScopeOpen: true})

	find := func(l layout, id LayerID) (image.Rectangle, bool) {
		for _, slot := range l.Slots {
			if slot.ID == id {
				return slot.Rect, true
			}
		}
		return image.Rectangle{}, false
	}

	wideRail, ok := find(wide, LayerRail)
	if !ok {
		t.Fatal("wide frame lost the rail")
	}
	narrowRail, ok := find(narrow, LayerRail)
	if !ok {
		t.Fatal("narrow frame lost scope entirely")
	}
	if _, ok := find(narrow, LayerTranscript); ok {
		t.Fatal("narrow scope should take the main pane, not split it")
	}
	if wideRail.Min.X == 0 {
		t.Fatal("a wide rail should sit at the right edge, not the left")
	}
	if narrowRail.Min.X != 0 {
		t.Fatal("a narrow scope list should own the main column")
	}

	// Closed scope in a narrow frame gives the pane back to the transcript.
	closed := solve(70, 30, metrics, mode{})
	if _, ok := find(closed, LayerRail); ok {
		t.Fatal("narrow frame drew a rail with scope closed")
	}
	if _, ok := find(closed, LayerTranscript); !ok {
		t.Fatal("narrow frame with scope closed lost the transcript")
	}
}

func TestLayoutLinearNeverSplits(t *testing.T) {
	for w := 1; w <= 200; w++ {
		l := solve(w, 30, DefaultMetrics(), mode{Linear: true})
		if !l.Narrow {
			t.Fatalf("linear at width %d claimed a rail", w)
		}
		for _, slot := range l.Slots {
			if slot.ID == LayerRail {
				t.Fatalf("linear at width %d drew a rail column", w)
			}
			if slot.Rect.Min.X != 0 || slot.Rect.Max.X != w {
				t.Fatalf("linear at width %d: %v is not full width (%v)", w, slot.ID, slot.Rect)
			}
		}
	}
}

// A token table is data, and data arrives wrong. Nonsense must produce a
// smaller surface, never a panic or an escaped rectangle.
func TestLayoutSurvivesHostileMetrics(t *testing.T) {
	hostile := []Metrics{
		{},
		{RailBreakpoint: -10, RailWidth: -10, MinMainWidth: -10, ComposerHeight: -10, StatusHeight: -10},
		{RailBreakpoint: 1, RailWidth: 1 << 20, MinMainWidth: 0, ComposerHeight: 1 << 20, StatusHeight: 1 << 20},
		{RailBreakpoint: 1, RailWidth: 80, MinMainWidth: 0, ComposerHeight: 3, StatusHeight: 1},
	}
	for _, m := range hostile {
		for _, size := range [][2]int{{1, 1}, {80, 24}, {200, 60}} {
			l := solve(size[0], size[1], m, mode{ScopeOpen: true, OverlayOpen: true})
			frame := image.Rect(0, 0, size[0], size[1])
			for _, slot := range l.Slots {
				if !slot.Rect.In(frame) {
					t.Fatalf("metrics %+v at %v: %v escaped to %v", m, size, slot.ID, slot.Rect)
				}
			}
		}
	}
}

func TestLayoutOverlaySitsAboveEverything(t *testing.T) {
	l := solve(120, 30, DefaultMetrics(), mode{OverlayOpen: true})
	last := l.Slots[len(l.Slots)-1]
	if last.ID != LayerOverlay || last.Z != zOverlay {
		t.Fatalf("overlay is not the top plane: %v", l.Slots)
	}
	// Under the fullscreen breakpoint it stops being a panel.
	narrow := solve(60, 20, DefaultMetrics(), mode{OverlayOpen: true})
	for _, slot := range narrow.Slots {
		if slot.ID == LayerOverlay && slot.Rect != image.Rect(0, 0, 60, 20) {
			t.Fatalf("narrow overlay should be fullscreen, got %v", slot.Rect)
		}
	}
}

// TestLayoutOverlayFullscreensOnShortButWideFrames covers the height door:
// a frame wide enough for a floating dialog but too short for one has the
// same "nothing left to float over" problem a narrow frame has (10.5.24,
// consentui.ForcedFullscreen's both-axes semantics). Width alone must not be
// enough to keep the overlay a centered panel.
func TestLayoutOverlayFullscreensOnShortButWideFrames(t *testing.T) {
	m := DefaultMetrics()
	w, h := 120, m.DialogFullscreenBelowHeight-1 // wide, but one row under the height door
	l := solve(w, h, m, mode{OverlayOpen: true})
	found := false
	for _, slot := range l.Slots {
		if slot.ID != LayerOverlay {
			continue
		}
		found = true
		if slot.Rect != image.Rect(0, 0, w, h) {
			t.Fatalf("short-but-wide overlay should be fullscreen, got %v", slot.Rect)
		}
	}
	if !found {
		t.Fatalf("no overlay slot at %dx%d", w, h)
	}

	// Just above the height door, at the same generous width, it goes back to
	// being a centered panel rather than the whole frame.
	tall := solve(w, m.DialogFullscreenBelowHeight+10, m, mode{OverlayOpen: true})
	for _, slot := range tall.Slots {
		if slot.ID == LayerOverlay && slot.Rect == image.Rect(0, 0, w, m.DialogFullscreenBelowHeight+10) {
			t.Fatalf("overlay should be a centered panel once both doors clear, got %v", slot.Rect)
		}
	}
}

// find is the slot lookup the dialog tests share.
func find(l layout, id LayerID) (image.Rectangle, bool) {
	for _, sl := range l.Slots {
		if sl.ID == id {
			return sl.Rect, true
		}
	}
	return image.Rectangle{}, false
}

// A dialog floats over the LENS and never over the persistent chrome (5.15:
// "the rail is the stable element; the main pane is the lens"; 10.3.15: the rail
// must always carry every live thing). This is the regression that motivated the
// rule: at 120×32 the old solver centred an 80-column panel on the FULL frame,
// at columns 20–99, while the rail held 92–119 — eight columns of every rail row
// overwritten mid-word.
//
// Stated as an invariant rather than as one size: whenever the dialog is a
// floating panel, neither it nor its chrome may touch the rail, the composer or
// the status line, at any size or in any mode. The one exemption is the
// fullscreen door, which is checked to be exactly the frame instead.
func TestDialogNeverCoversTheRailOrTheComposer(t *testing.T) {
	tables := []Metrics{DefaultMetrics(), chatShapedMetrics()}
	modes := []mode{{OverlayOpen: true}, {OverlayOpen: true, ScopeOpen: true}}
	for _, m := range tables {
		for _, md := range modes {
			for w := 0; w <= 200; w++ {
				for h := 0; h <= 48; h++ {
					l := solve(w, h, m, md)
					panel, ok := find(l, LayerOverlay)
					if !ok {
						if w > 0 && h > 0 {
							t.Fatalf("%dx%d %+v: overlay asked for and not laid out", w, h, md)
						}
						continue
					}
					chrome, floating := find(l, LayerDialogChrome)
					if !floating {
						// The fullscreen door: nothing left to float over, so
						// the dialog is the frame and covering is the point.
						if panel != image.Rect(0, 0, w, h) {
							t.Fatalf("%dx%d %+v: a non-floating dialog must be the whole frame, got %v",
								w, h, md, panel)
						}
						continue
					}
					if !panel.In(chrome) {
						t.Fatalf("%dx%d %+v: panel %v escapes its chrome %v", w, h, md, panel, chrome)
					}
					for _, id := range []LayerID{LayerRail, LayerComposer, LayerStatus} {
						other, present := find(l, id)
						if !present {
							continue
						}
						// A narrow frame renders scope AS the main pane, so the
						// lens and the rail are the same rectangle there and a
						// dialog has nowhere else to be. The law is about the
						// rail as a COLUMN beside the transcript.
						if id == LayerRail && l.Narrow {
							continue
						}
						if chrome.Overlaps(other) {
							t.Fatalf("%dx%d %+v: dialog chrome %v overlaps %v %v",
								w, h, md, chrome, id, other)
						}
					}
				}
			}
		}
	}
}

// chatShapedMetrics is the table internal/tui2/chat actually passes (a four-row
// composer with the place line on its top edge, 5.19). The defect was reported
// against those numbers, so the invariant is checked against them and not only
// against the provisional defaults.
func chatShapedMetrics() Metrics {
	m := DefaultMetrics()
	m.ComposerHeight = 4
	return m
}

// The 120×32 and 80×24 frames the screenshot harness caught, named so a
// regression reads as the defect it is rather than as a bounds arithmetic
// puzzle.
func TestDialogAtTheTwoReportedSizes(t *testing.T) {
	m := chatShapedMetrics()
	for _, size := range [][2]int{{120, 32}, {80, 24}} {
		w, h := size[0], size[1]
		l := solve(w, h, m, mode{OverlayOpen: true, ScopeOpen: true})
		chrome, floating := find(l, LayerDialogChrome)
		if !floating {
			t.Fatalf("%dx%d is above both fullscreen doors and should float", w, h)
		}
		// The place line is the composer's top row (5.19). Nothing may cover it.
		composer, ok := find(l, LayerComposer)
		if !ok {
			t.Fatalf("%dx%d lost the composer", w, h)
		}
		if chrome.Max.Y > composer.Min.Y {
			t.Fatalf("%dx%d: dialog reaches row %d, the place line is row %d",
				w, h, chrome.Max.Y-1, composer.Min.Y)
		}
		if rail, ok := find(l, LayerRail); ok && !l.Narrow && chrome.Max.X > rail.Min.X {
			t.Fatalf("%dx%d: dialog reaches column %d, the rail starts at %d",
				w, h, chrome.Max.X-1, rail.Min.X)
		}
	}
}

// And the same two frames, as numbers rather than as invariants.
//
// THE DEFECT the numbers replace: two thirds on both axes gave 120×32 a 58×16
// panel — a width that truncated twelve of the palette's thirty catalog
// descriptions — and gave 80×24 ten content rows for a fifty-six row catalog.
// A list surface wants about three quarters of the width and more of the
// height, and the two axes need not share a fraction (dialogWidthPercent,
// dialogHeightPercent).
//
// Pinned as literal rectangles because a fraction is easy to change by accident
// and hard to notice: this test is what makes the next change to the shape a
// decision. Every number below is dialogWidthPercent/dialogHeightPercent of the
// lens, centred in it, with the panel inset by dialogMargin.
func TestDialogRectanglesAtTheTwoReportedSizes(t *testing.T) {
	cases := []struct {
		w, h          int
		lens          image.Rectangle
		chrome, panel image.Rectangle
	}{
		// 120×32: 27 body rows beside a 28-column rail, so the lens is 91×27.
		// 75% of 91 is 68 columns, 80% of 27 is 21 rows, centred at (11,3) —
		// a 66×19 panel where the old fraction gave 58×16.
		{120, 32,
			image.Rect(0, 0, 91, 27),
			image.Rect(11, 3, 79, 24),
			image.Rect(12, 4, 78, 23)},
		// 80×24: under the rail breakpoint, so the lens is the full 80×19.
		// 75% is 60 columns, 80% is 15 rows, centred at (10,2) — a 58×13 panel
		// where the old fraction gave 51×10.
		{80, 24,
			image.Rect(0, 0, 80, 19),
			image.Rect(10, 2, 70, 17),
			image.Rect(11, 3, 69, 16)},
	}
	for _, c := range cases {
		l := solve(c.w, c.h, chatShapedMetrics(), mode{OverlayOpen: true})
		chrome, floating := find(l, LayerDialogChrome)
		if !floating {
			t.Fatalf("%dx%d should float", c.w, c.h)
		}
		panel, ok := find(l, LayerOverlay)
		if !ok {
			t.Fatalf("%dx%d lost the panel", c.w, c.h)
		}
		if chrome != c.chrome {
			t.Errorf("%dx%d: chrome = %v (%dx%d), want %v (%dx%d)",
				c.w, c.h, chrome, chrome.Dx(), chrome.Dy(), c.chrome, c.chrome.Dx(), c.chrome.Dy())
		}
		if panel != c.panel {
			t.Errorf("%dx%d: panel = %v (%dx%d), want %v (%dx%d)",
				c.w, c.h, panel, panel.Dx(), panel.Dy(), c.panel, c.panel.Dx(), c.panel.Dy())
		}
		// The fractions are fractions OF THE LENS, and the panel is centred in
		// it: the margin left over is what keeps a dialog a dialog.
		if !chrome.In(c.lens) {
			t.Errorf("%dx%d: chrome %v escapes the lens %v", c.w, c.h, chrome, c.lens)
		}
		// Centred to within the odd column an integer halving cannot split,
		// which lands on the right the way every other rounding here does.
		if slop := (c.lens.Max.X - chrome.Max.X) - (chrome.Min.X - c.lens.Min.X); slop < 0 || slop > 1 {
			t.Errorf("%dx%d: chrome %v is not centred in the lens %v", c.w, c.h, chrome, c.lens)
		}
		if chrome.Dx() != c.lens.Dx()*dialogWidthPercent/100 {
			t.Errorf("%dx%d: chrome is %d of %d columns, want %d%%",
				c.w, c.h, chrome.Dx(), c.lens.Dx(), dialogWidthPercent)
		}
		if chrome.Dy() != c.lens.Dy()*dialogHeightPercent/100 {
			t.Errorf("%dx%d: chrome is %d of %d rows, want %d%%",
				c.w, c.h, chrome.Dy(), c.lens.Dy(), dialogHeightPercent)
		}
	}
}

// The margin is a real slot, not a reservation inside the panel: a pane is given
// its whole rectangle and the shell reserves nothing inside it (pane.go).
func TestDialogChromeIsAMarginAroundThePanel(t *testing.T) {
	l := solve(120, 32, chatShapedMetrics(), mode{OverlayOpen: true})
	chrome, ok := find(l, LayerDialogChrome)
	if !ok {
		t.Fatal("no chrome slot for a floating dialog")
	}
	panel, ok := find(l, LayerOverlay)
	if !ok {
		t.Fatal("no panel slot")
	}
	if got := chrome.Inset(dialogMargin); got != panel {
		t.Fatalf("panel = %v, want the chrome inset by %d = %v", panel, dialogMargin, got)
	}
	// Chrome under the panel: drawn first, clicked second.
	var chromeZ, panelZ int
	for _, sl := range l.Slots {
		switch sl.ID {
		case LayerDialogChrome:
			chromeZ = sl.Z
		case LayerOverlay:
			panelZ = sl.Z
		}
	}
	if !(zBase < chromeZ && chromeZ < panelZ) {
		t.Fatalf("planes out of order: base %d, chrome %d, panel %d", zBase, chromeZ, panelZ)
	}
}

// The fullscreen doors are two doors, not one, and neither of them draws a
// margin: a dialog that owns the frame is already bounded by the frame's edge
// (10.4.17, consentui.ForcedFullscreen's both-axes semantics).
func TestDialogFullscreenDoorsOnBothAxes(t *testing.T) {
	m := DefaultMetrics()
	cases := []struct {
		w, h       int
		fullscreen bool
		why        string
	}{
		{m.DialogFullscreenBelowWidth - 1, 40, true, "one column under the width door"},
		{200, m.DialogFullscreenBelowHeight - 1, true, "one row under the height door"},
		{m.DialogFullscreenBelowWidth - 1, m.DialogFullscreenBelowHeight - 1, true, "under both"},
		{m.DialogFullscreenBelowWidth, m.DialogFullscreenBelowHeight, false, "exactly on both doors"},
	}
	for _, c := range cases {
		l := solve(c.w, c.h, m, mode{OverlayOpen: true})
		panel, ok := find(l, LayerOverlay)
		if !ok {
			t.Fatalf("%s (%dx%d): no overlay", c.why, c.w, c.h)
		}
		_, floating := find(l, LayerDialogChrome)
		if floating == c.fullscreen {
			t.Fatalf("%s (%dx%d): floating = %v, want fullscreen = %v",
				c.why, c.w, c.h, floating, c.fullscreen)
		}
		if c.fullscreen && panel != image.Rect(0, 0, c.w, c.h) {
			t.Fatalf("%s (%dx%d): fullscreen panel = %v, want the whole frame",
				c.why, c.w, c.h, panel)
		}
	}
}

// THE DEFECT: the transcript and the rail were flush. A transcript line that
// ran the full width put its last character against the rail's first cell, and
// a selected rail row's band started in the cell after a word — two rooms with
// no wall between them. 5.13 separates rooms with whitespace, not with a drawn
// divider, so the wall is one column that belongs to neither pane.
//
// Pinned as an invariant over every size and mode: wherever a rail column and a
// main pane are both on screen, there is at least one column between them that
// no slot claims.
func TestTheRailAndTheLensNeverTouch(t *testing.T) {
	tables := []Metrics{DefaultMetrics(), chatShapedMetrics()}
	modes := []mode{{}, {ScopeOpen: true}, {OverlayOpen: true}, {ScopeOpen: true, OverlayOpen: true}}
	for _, m := range tables {
		for _, md := range modes {
			for w := 0; w <= 200; w++ {
				for h := 0; h <= 40; h++ {
					l := solve(w, h, m, md)
					if l.Narrow {
						continue
					}
					rail, ok := find(l, LayerRail)
					if !ok {
						continue
					}
					for _, id := range []LayerID{LayerTranscript, LayerComposer} {
						other, present := find(l, id)
						if !present {
							continue
						}
						if gap := rail.Min.X - other.Max.X; gap < railSeam {
							t.Fatalf("%dx%d %+v: %v ends at column %d and the rail starts at %d — gap %d, want at least %d",
								w, h, md, id, other.Max.X-1, rail.Min.X, gap, railSeam)
						}
					}
					// The rail keeps its whole width; the seam comes out of the
					// lens, which is the pane that can afford it.
					if rail.Dx() != m.RailWidth {
						t.Fatalf("%dx%d: rail is %d columns, want %d", w, h, rail.Dx(), m.RailWidth)
					}
					if rail.Max.X != w {
						t.Fatalf("%dx%d: rail does not reach the right edge: %v", w, h, rail)
					}
				}
			}
		}
	}
}

// The seam is ground, not a slot: nothing may claim it, or it stops being the
// whitespace 5.13 asked for and becomes a pane that happens to be blank.
func TestTheSeamBelongsToNobody(t *testing.T) {
	l := solve(120, 32, chatShapedMetrics(), mode{ScopeOpen: true})
	rail, ok := find(l, LayerRail)
	if !ok {
		t.Fatal("120x32 lost the rail")
	}
	// Only over the rows the rail occupies: the status line runs the full width
	// underneath everything, which is 10.5.22's footer and not a wall to knock
	// a hole in.
	seam := image.Rect(rail.Min.X-railSeam, rail.Min.Y, rail.Min.X, rail.Max.Y)
	for _, sl := range l.Slots {
		if sl.Rect.Overlaps(seam) {
			t.Fatalf("%v claimed the seam %v with %v", sl.ID, seam, sl.Rect)
		}
	}
}

// §6's `sidebar: hidden`, asked for by the surface rather than by the width: the
// rail leaves the frame and the columns go back to the LENS.
//
// A hidden rail that kept its rectangle would be the surface paying for a
// sidebar it decided not to draw, and the work page — which hides the rail
// precisely because it is already showing that list, fuller — would have gained
// nothing by hiding it.
func TestAHiddenRailGivesItsColumnsBackToTheLens(t *testing.T) {
	metrics := chatShapedMetrics()
	shown := solve(120, 32, metrics, mode{ScopeOpen: true})
	lens, ok := find(shown, LayerTranscript)
	if !ok {
		t.Fatal("120x32 lost the transcript")
	}

	hidden := solve(120, 32, metrics, mode{ScopeOpen: true, RailHidden: true})
	if _, still := find(hidden, LayerRail); still {
		t.Fatal("the rail kept its slot while hidden")
	}
	wide, ok := find(hidden, LayerTranscript)
	if !ok {
		t.Fatal("hiding the rail took the transcript with it")
	}
	if wide.Dx() != 120 {
		t.Fatalf("the lens is %d columns wide, want the whole frame (120)", wide.Dx())
	}
	if wide.Dx() <= lens.Dx() {
		t.Fatalf("hiding the rail did not widen the lens (%d, was %d)", wide.Dx(), lens.Dx())
	}
}

// A hidden rail never becomes the main pane. Narrow says the rail did not FIT,
// which is why scope is then reachable as a full pane; hidden says the surface
// asked for it not to be there, and falling back would draw the very duplicate
// the hiding prevented.
func TestAHiddenRailIsNeverTheNarrowFallback(t *testing.T) {
	l := solve(60, 24, chatShapedMetrics(), mode{ScopeOpen: true, RailHidden: true})
	if !l.Narrow {
		t.Fatal("a 60-column frame is not narrow")
	}
	if _, found := find(l, LayerRail); found {
		t.Fatal("a hidden rail took the main pane on a narrow frame")
	}
	if _, found := find(l, LayerTranscript); !found {
		t.Fatal("the narrow frame drew no lens at all")
	}
}
