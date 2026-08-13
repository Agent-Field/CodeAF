package rail

import "strings"

// Pane binds a [Model] and a [View] to the shell's pane seam — the one method
// internal/tui2 asks of anything that owns bytes:
//
//	Render(width, height int) string
//
// It is declared structurally rather than by importing the shell, so this
// package stays a leaf and the integration lane writes one line:
//
//	shell.SetPane(layer, &rail.Pane{Model: m, View: v})
//
// Key handling, invalidation and the composer binding stay with the shell. This
// type owns no policy; it joins lines.
type Pane struct {
	// Model is the scope model. A nil Model renders nothing.
	Model *Model
	// View is the renderer. A nil View is built on first use with no colour,
	// which keeps a zero-valued Pane usable in a test.
	View *View
	// Mode is which rendering to draw. [ModeAuto] — the zero value — picks the
	// rail or the full-pane list by width (Part 9.12).
	Mode Mode
	// Slim draws the collapsed rail's handle instead of the map ([View.Handle]).
	// The pane still owns the same model, so expanding costs a flag and not a
	// rebuild — and the model keeps its cursor, its scope stack and its place
	// across a collapse, which is why the handle is a rendering here rather than
	// a second pane the shell swaps in.
	Slim bool
}

// Render implements the shell's pane contract: at most height lines, each at
// most width cells, and a pure function of the model and the size.
func (p *Pane) Render(width, height int) string {
	if p == nil || p.Model == nil {
		return ""
	}
	if p.View == nil {
		p.View = NewView(nil)
	}
	var lines []string
	if p.Slim {
		lines = p.View.Handle(p.Model.Scope().Unseen(), width, height)
	} else {
		lines = p.View.Render(p.Model, p.Mode, width, height)
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n")
}
