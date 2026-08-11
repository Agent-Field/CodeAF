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
