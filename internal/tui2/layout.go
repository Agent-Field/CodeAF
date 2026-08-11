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
// and the clicks without any pane knowing a dialog exists. The dialog's chrome
// sits one plane under its body: it is drawn before the panel and clicked after
// it, which is what makes the boundary part of the dialog rather than a hole in
// the modal discipline.
const (
	zBase         = 0
	zDialogChrome = 9
	zOverlay      = 10
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
		// The lens is what a dialog may float over: the main pane's body, and
		// nothing else. See overlayRects.
		lens := image.Rect(0, 0, mainW, bodyH)
		chrome, panel, floating := overlayRects(w, h, lens, m)
		if floating {
			l.Slots = append(l.Slots, slot{ID: LayerDialogChrome, Rect: chrome, Z: zDialogChrome})
		}
		l.Slots = append(l.Slots, slot{ID: LayerOverlay, Rect: panel, Z: zOverlay})
	}
	return l
}

// Dialog anatomy and siting.
//
// WHERE A DIALOG MAY FLOAT. It floats over the LENS and never over the persistent
// chrome. "The rail is the stable element; the main pane is the lens" (5.15) is
// the sentence this implements: 5.15 opens by refusing the full-screen task room
// precisely because the rail must persist, 10.3.15 makes it a law that "our rail
// must always carry every live thing — no silent lanes", and 8.3 refuses a
// fullscreen roster on wide terminals for the same reason. A dialog that covered
// the rail would hide running work behind a help sheet; one that covered the
// composer's place line would hide where the next answer lands (5.19). So the
// float region is the main pane's body rect, and the panel is centred inside
// THAT rather than inside the frame.
//
// (The defect this replaces: centring on the full frame put an 80-column panel
// at cols 20–99 of a 120-column terminal while the rail held 92–119, so eight
// columns of every rail row were overwritten mid-word.)
//
// WHEN IT STOPS FLOATING. Under either fullscreen door (10.4.17's "forced
// fullscreen below a stated size threshold", numbered by 10.5.24 and pinned in
// tokens at 72×20) there is nothing left to float over, so the dialog takes the
// whole frame — both axes, matching consentui.ForcedFullscreen. The same answer
// covers a lens too small to hold a panel.
//
// WHAT ITS BOUNDARY IS. Not a border. That is a decision the doc already made
// three times: 5.21's anti-catalog refuses "nested box-drawing frames", 5.13
// spends the structure budget on "whitespace not boxes; hairline rules only at
// room boundaries", and 7.1 refuses another harness's box style outright. A
// floating dialog IS a room boundary, so it gets exactly the two things the doc
// allows — a one-cell margin of its own ground on every side, and a hairline
// rule along the top and bottom edge of that margin. The margin is a real slot
// (LayerDialogChrome) rather than a reservation inside the panel, because a pane
// is given its WHOLE rectangle and the shell reserves nothing inside it (pane.go).
const (
	// dialogMargin is the whitespace ring the chrome slot owns, in cells. One
	// cell is enough: the ring is opaque, so it separates the panel's text from
	// the transcript's by a full column on every side.
	dialogMargin = 1
	// dialogMinWidth and dialogMinHeight are the smallest OUTER panel worth
	// floating, margin included. Below them the lens has no room for a dialog
	// and a dialog with no room is a fullscreen dialog.
	dialogMinWidth  = 40
	dialogMinHeight = 8
)

// overlayRects sites one dialog. It returns the chrome rectangle (the panel plus
// its margin), the panel rectangle the overlay pane is given, and whether the
// dialog is floating at all — a fullscreen dialog has no margin to draw, because
// the frame's own edge is already its boundary.
//
// It is a pure function of two integers, the lens and the metrics table, so
// every dialog shape this surface can take is reachable from a test.
func overlayRects(w, h int, lens image.Rectangle, m Metrics) (chrome, panel image.Rectangle, floating bool) {
	full := image.Rect(0, 0, w, h)
	if w < m.DialogFullscreenBelowWidth || h < m.DialogFullscreenBelowHeight {
		return full, full, false
	}
	lens = lens.Intersect(full)
	if lens.Dx() < dialogMinWidth || lens.Dy() < dialogMinHeight {
		return full, full, false
	}
	cw := min(lens.Dx(), max(dialogMinWidth, lens.Dx()*2/3))
	ch := min(lens.Dy(), max(dialogMinHeight, lens.Dy()*2/3))
	x := lens.Min.X + (lens.Dx()-cw)/2
	y := lens.Min.Y + (lens.Dy()-ch)/2
	chrome = image.Rect(x, y, x+cw, y+ch)
	panel = chrome.Inset(dialogMargin)
	if panel.Empty() {
		return full, full, false
	}
	return chrome, panel, true
}
