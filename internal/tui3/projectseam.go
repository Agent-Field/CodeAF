package tui3

import "github.com/charmbracelet/x/ansi"

// seamProjectRight adds the project at the right edge after any telemetry.
// The controls and telemetry keep their space; paths truncate at the right,
// and a field without room for its root and ellipsis disappears altogether.
// The span covers the displayed path alone, relative to the right label.
func seamProjectRight(left, right, path string, width int) (string, hudSpan) {
	if path == "" {
		return right, hudSpan{}
	}
	prefix := targetProjectLead
	if right != "" {
		prefix = groupGapRun + prefix
	}
	room := legendRoomFor(width, ansi.StringWidth(right)) - ansi.StringWidth(left) - 3 - ansi.StringWidth(targetProjectLead)
	if room < 3 {
		return right, hudSpan{}
	}
	path = fit(path, room)
	from := ansi.StringWidth(right) + ansi.StringWidth(prefix)
	return right + prefix + path, hudSpan{from: from, to: from + ansi.StringWidth(path)}
}

// paintSeamProject underlines only the path under the pointer, preserving the
// dim project label and the telemetry's own paint when they share the right.
func (a *app) paintSeamProject(text string, span hudSpan, hovered bool) string {
	return paintSpan(text, span, a.pal.dim, func(path string) string {
		return a.pal.underline(a.pal.dim(path))
	}, hovered)
}

// seamModelPaint keeps the current model bold and bright even while underlined.
func seamModelPaint(pal palette, text string, hovered bool) string {
	text = pal.seamModel(text)
	if hovered {
		text = pal.underline(text)
	}
	return text
}

// hoverDraftSeam uses the same spans as a press. It also clears the highlight
// when the pointer leaves home or crosses onto the tab bar or another field.
func (a *app) hoverDraftSeam(x, y int) {
	next := hoverNothing
	if a.placeHasDraft() && !a.composer.open && !a.target.pick.open && a.targetRow > 0 && y == a.targetRow {
		switch {
		case a.targetModelSpan.holds(x):
			next = hoverStatusModel
		case a.targetFolderSpan.holds(x):
			next = hoverSeamProject
		}
	}
	if next != a.targetHover {
		a.targetHover = next
		a.touch()
	}
}
