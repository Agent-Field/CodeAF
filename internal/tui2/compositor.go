package tui2

import (
	"image"

	"charm.land/lipgloss/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// The compositor is the one place in the v2 surface that knows where anything
// is. Every pane is a layer with an id, a rectangle and a z, and both the
// drawing and the mouse hit test walk the same array — so a pane's clickable
// region cannot drift from its drawn region, because there is no second copy
// of the geometry to drift. That is the whole point of the exercise: the old
// surface carried 31 hand-maintained bounds structs and 45 sites asking one of
// them whether it contained a point, and every one of those was a chance for
// the two truths to disagree.
//
// Lip Gloss v2 ships a Layer/Compositor pair that does most of this, and we
// do not use it, for two reasons that both matter here:
//
//  1. A Layer's bounds are derived from its CONTENT. A pane holding three
//     rows of text inside a twenty-row region would be clickable in three
//     rows. A pane's hit region must be the region it was GIVEN, whether or
//     not it filled it — the empty half of a half-full rail is still the rail.
//  2. Its Compositor re-flattens the layer tree (and rebuilds an id map) on
//     every change, and a Layer's content can only be set by constructing a
//     new Layer. That is an allocation per pane per frame on the hot path,
//     for a layout that changes only when the terminal is resized.
//
// So we keep the shape and drop the churn: the layer table is rebuilt only
// when the layout changes, per-frame work is assigning a string to each
// layer's body, and the cell buffer underneath is Lip Gloss v2's Canvas —
// resized on resize, cleared and redrawn per frame, never reallocated. What
// reaches the terminal is decided by Bubble Tea v2's diffing renderer, which
// is the metric that matters: bytes over the wire, not frames per second.

// LayerID names a region of the surface. It is dense and small on purpose:
// the shell keeps one pane slot per id in a fixed array, so binding a pane
// costs an index rather than a map.
type LayerID uint8

// The layer vocabulary. Declaration order is not z order — the layout assigns
// that — but it is the order the layout emits slots in, which keeps the
// compositor's insertion sort at zero work in the ordinary case.
const (
	// LayerNone is the absence of a layer: what a hit test returns when the
	// point landed on no pane at all.
	LayerNone LayerID = iota
	// LayerTranscript is the conversation: user turns, streamed head replies,
	// collapsed tool rows, settled cards at their birth position (4.3).
	LayerTranscript
	// LayerRail is the scope map (5.15). Wide terminals draw it as a column
	// beside the transcript; narrow ones draw the same rows, with the same
	// keys and the same selection, as a full pane. Same id either way — which
	// is what makes the scope model width-independent rather than a rail
	// feature with a narrow fallback.
	LayerRail
	// LayerComposer is the input: draft, attachments, the meta strip carrying
	// this turn's cost and context (10.5.23).
	LayerComposer
	// LayerStatus is the contextual footer: a registry of columns that drop
	// lowest-priority-first rather than wrapping (10.5.22).
	LayerStatus
	// LayerOverlay is the single overlay plane Wave 3 fills with dialogs,
	// the palette and the ? surfaces. It exists here so the z discipline is
	// exercised by the skeleton instead of invented later.
	LayerOverlay

	// numLayers bounds the shell's pane array. Keep it last.
	numLayers
)

// String names the layer for the status line and for test failures.
func (id LayerID) String() string {
	switch id {
	case LayerTranscript:
		return "transcript"
	case LayerRail:
		return "rail"
	case LayerComposer:
		return "composer"
	case LayerStatus:
		return "status"
	case LayerOverlay:
		return "overlay"
	default:
		return "none"
	}
}

// layer is one entry in the compositor's table. body is held by value so the
// table is one contiguous allocation; only its Text field moves per frame.
type layer struct {
	id   LayerID
	rect image.Rectangle
	z    int
	body uv.StyledString
}

// compositor holds the cell buffer and the layer table. The zero value is
// usable and renders nothing, which is the correct behaviour before the first
// window size arrives.
type compositor struct {
	canvas *lipgloss.Canvas
	layers []layer
	w, h   int
}

// setLayout rebuilds the layer table from a solved layout. It reuses the
// existing backing array, so after the first few resizes this allocates
// nothing; append's zero value also clears each layer's carried-over text,
// which is what stops a pane that vanished at this width from leaving its
// last frame behind.
func (c *compositor) setLayout(l layout) {
	c.resize(l.Width, l.Height)
	c.layers = c.layers[:0]
	for _, slot := range l.Slots {
		c.layers = append(c.layers, layer{id: slot.ID, rect: slot.Rect, z: slot.Z})
	}
	// The layout emits slots in ascending z, so this is a scan and no swaps.
	// It is here anyway because "the caller sorted it" is an invariant that
	// survives exactly until someone adds a slot in the wrong place, and an
	// insertion sort over half a dozen elements costs less than the comment
	// explaining why we trusted the caller.
	for i := 1; i < len(c.layers); i++ {
		for j := i; j > 0 && c.layers[j-1].z > c.layers[j].z; j-- {
			c.layers[j-1], c.layers[j] = c.layers[j], c.layers[j-1]
		}
	}
}

// resize sizes the cell buffer. A zero or negative dimension leaves the canvas
// absent rather than allocating a degenerate one: a terminal reporting 0x0 (or
// a multiplexer mid-reattach) must produce an empty frame, not a crash.
func (c *compositor) resize(w, h int) {
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	c.w, c.h = w, h
	if w == 0 || h == 0 {
		return
	}
	if c.canvas == nil {
		c.canvas = lipgloss.NewCanvas(w, h)
		return
	}
	if c.canvas.Width() != w || c.canvas.Height() != h {
		c.canvas.Resize(w, h)
	}
}

// setContent hands a layer the string it will draw. The layer keeps its
// rectangle regardless of what the string contains — short content does not
// shrink a pane, long content does not grow one — so content and geometry stay
// independent, which is the property that makes hit testing trustworthy.
func (c *compositor) setContent(id LayerID, text string) {
	for i := range c.layers {
		if c.layers[i].id == id {
			c.layers[i].body.Text = text
			return
		}
	}
}

// rect returns a layer's rectangle, and whether that layer is in this layout
// at all. Panes ask before they render; nothing else needs geometry.
func (c *compositor) rect(id LayerID) (image.Rectangle, bool) {
	for i := range c.layers {
		if c.layers[i].id == id {
			return c.layers[i].rect, true
		}
	}
	return image.Rectangle{}, false
}

// render draws every layer in z order onto the canvas and returns the frame.
// Each layer clears its own rectangle first (StyledString.Draw does), and each
// line is truncated rather than wrapped: a pane is given a box and may not
// grow out of it, ever. The returned string is the frame — that allocation is
// the product, and it is the only one on this path.
func (c *compositor) render() string {
	if c.w <= 0 || c.h <= 0 || c.canvas == nil {
		return ""
	}
	c.canvas.Clear()
	for i := range c.layers {
		l := &c.layers[i]
		if l.rect.Empty() {
			continue
		}
		l.body.Draw(c.canvas, l.rect)
	}
	return c.canvas.Render()
}

// hit answers which pane owns a cell, and where in that pane the cell is. The
// scan runs from the top of the z order down, so an overlay takes the click
// before the pane it covers — the modal discipline falls out of the table
// rather than being asserted by every caller.
//
// The second return is pane-local, which is the part that matters downstream:
// a pane handling a click never sees an absolute coordinate, so it can never
// subtract the wrong origin.
func (c *compositor) hit(x, y int) (LayerID, image.Point, bool) {
	pt := image.Pt(x, y)
	for i := len(c.layers) - 1; i >= 0; i-- {
		l := &c.layers[i]
		if pt.In(l.rect) {
			return l.id, pt.Sub(l.rect.Min), true
		}
	}
	return LayerNone, image.Point{}, false
}
