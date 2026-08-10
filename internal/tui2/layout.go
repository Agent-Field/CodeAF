package tui2

import "image"

// The layout solver is a pure function: a terminal size and a mode go in, a
// table of rectangles comes out. Nothing it produces is stored anywhere else,
// nothing reads a rectangle it did not get from here, and it does not touch
// the shell — which means every shape this surface can take is reachable from
// a test with two integers, including the shapes a terminal only reaches while
// someone is dragging its corner.
//
// The order it gives columns away is the order of what a surface stops being
// useful without. The status line goes last, because it is the line that
// explains the rest; the composer goes second to last, because a chat you
// cannot type into is not a chat; the transcript absorbs whatever is left; and
// the rail — a map, and the only pane whose absence has a defined fallback
// (5.15) — is the first to hand its columns back.

// z planes. Panes share the base plane because they tile rather than overlap;
// the overlay plane sits above all of them so a dialog takes both the pixels
// and the clicks without any pane knowing a dialog exists.
const (
	zBase    = 0
	zOverlay = 10
)

// slot is one pane's allotment. Rect is absolute, in cells, with Min at the
// top-left; it is the only geometry in the surface.
type slot struct {
	ID   LayerID
	Rect image.Rectangle
	Z    int
}

// layout is a solved frame. Slots are emitted in ascending Z.
type layout struct {
	Width, Height int

	// Narrow reports that the rail did not fit as a column, so scope must be
	// reached as a full pane instead. It is a fact about this frame, not a
	// preference: linear mode and a 60-column terminal both produce it.
	Narrow bool

	// Linear reports the accessible single-column, no-motion rendering
	// (10.1.5). Same product, less motion — so it is a rendering mode here,
	// not a different layout algorithm with its own bugs.
	Linear bool

	Slots []slot
}

// mode is the shell state the solver is allowed to see. Keeping it this small
// is deliberate: a solver that could read the whole shell would grow branches
// that only reproduce with a live session behind them.
type mode struct {
	// Linear selects the accessible rendering.
	Linear bool
	// ScopeOpen asks for the scope map. In a wide frame the rail is always
	// drawn and this changes nothing (4.3: persistent, not toggle-hidden); in
	// a narrow frame it is what swaps the main pane to the scope list.
	ScopeOpen bool
	// OverlayOpen asks for the overlay plane. Wave 3 fills it.
	OverlayOpen bool
}

// solve lays out one frame. It allocates only the slot slice, and callers that
// care reuse the previous layout's slice through solveInto.
func solve(w, h int, m Metrics, md mode) layout {
	return solveInto(nil, w, h, m, md)
}

// solveInto is solve with a caller-owned slice. The shell passes its previous
// slots back in, so a resize storm rebuilds the table in place rather than
// handing the collector a frame's worth of garbage per event.
func solveInto(slots []slot, w, h int, m Metrics, md mode) layout {
	m = m.sane()
	l := layout{Width: w, Height: h, Linear: md.Linear, Slots: slots[:0]}
	if w <= 0 || h <= 0 {
		// A terminal that reports nothing gets nothing drawn. Every consumer
		// downstream is written to survive an empty slot table, so this is a
		// real state rather than an error.
		l.Narrow = true
		return l
	}

	// Rows, from the bottom up.
	rows := h
	statusH := min(m.StatusHeight, rows)
	rows -= statusH
	composerH := min(m.ComposerHeight, rows)
	rows -= composerH
	bodyH := rows

	// Columns. The rail takes its width only if what remains is still a
	// usable transcript; a rail that leaves forty columns of conversation has
	// cost more than it showed.
	railW := 0
	if !md.Linear && w >= m.RailBreakpoint && m.RailWidth > 0 && bodyH+composerH > 0 {
		if w-m.RailWidth >= m.MinMainWidth {
			railW = m.RailWidth
		}
	}
	// Narrow is a fact about this frame rather than about its width: a frame
	// one row tall has no column to give the rail either, and scope has to be
	// reachable the narrow way in both cases.
	l.Narrow = railW == 0
	mainW := w - railW

	// In a narrow frame the scope map is not a column, it is the pane: the
	// same rows, the same keys, the same selection, drawn where the transcript
	// was (5.15). The transcript yields rather than shrinking, because two
	// half-panes in sixty columns is the shape the doc rejects.
	mainID := LayerTranscript
	if l.Narrow && md.ScopeOpen {
		mainID = LayerRail
	}

	if bodyH > 0 && mainW > 0 {
		l.Slots = append(l.Slots, slot{ID: mainID, Rect: image.Rect(0, 0, mainW, bodyH), Z: zBase})
	}
	if railW > 0 {
		// The rail stands beside the transcript and the composer both — it is
		// the stable element and the main column is the lens, so it does not
		// stop at the composer's top edge.
		l.Slots = append(l.Slots, slot{ID: LayerRail, Rect: image.Rect(mainW, 0, w, bodyH+composerH), Z: zBase})
	}
	if composerH > 0 && mainW > 0 {
		l.Slots = append(l.Slots, slot{ID: LayerComposer, Rect: image.Rect(0, bodyH, mainW, bodyH+composerH), Z: zBase})
	}
	if statusH > 0 {
		l.Slots = append(l.Slots, slot{ID: LayerStatus, Rect: image.Rect(0, h-statusH, w, h), Z: zBase})
	}
	if md.OverlayOpen {
		l.Slots = append(l.Slots, slot{ID: LayerOverlay, Rect: overlayRect(w, h, m), Z: zOverlay})
	}
	return l
}

// overlayRect centers a dialog, or gives it the whole frame under the
// fullscreen breakpoint (10.5.24). Wave 3 decides what goes inside it; the
// geometry is settled here so the overlay plane is a real, testable region
// from the first wave rather than a Wave 3 discovery.
func overlayRect(w, h int, m Metrics) image.Rectangle {
	if w < m.DialogFullscreenBelow {
		return image.Rect(0, 0, w, h)
	}
	dw := min(w-4, max(40, w*2/3))
	dh := min(h-2, max(6, h*2/3))
	if dw <= 0 || dh <= 0 {
		return image.Rect(0, 0, w, h)
	}
	x := (w - dw) / 2
	y := (h - dh) / 2
	return image.Rect(x, y, x+dw, y+dh)
}
