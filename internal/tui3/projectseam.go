package tui3

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// seamProjectWord is the conversation's project as every row that names it
// spells it: the workspace under `~`, with the machine in front over a
// connection. One function, because the seam at phone width and the keys
// row everywhere else must agree on the word.
func (a *app) seamProjectWord() string {
	return a.hostedPath(a.placeWord(tildePath(a.workspace, a.tilde)))
}

// seamProjectRight adds the project at the right edge after any telemetry —
// AT THE PHONE TIER ONLY, since 2026-09-22, where the seam is the keys row;
// everywhere else the project is on the keys row under the box (footswap.go's
// [app.hintRow]). The controls and telemetry keep their space; paths truncate
// at the right, and a field without room for its root and ellipsis
// disappears altogether. The span covers the displayed path alone, relative
// to the right label.
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

// seamProjectPress is a press on the conversation's project, wherever this
// frame drew it — the keys row, or the seam at phone width
// ([app.hintRowKind]) — and it opens the folder chooser, which is what the
// word is a door onto: the same sheet `/folder` opens.
func (a *app) seamProjectPress(x, y int) (tea.Cmd, bool) {
	if a.copy.on || a.pick.open || a.roomOpen() || !a.seamProjectSpan.holds(x) {
		return nil, false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != a.hintRowKind() {
		return nil, false
	}
	return a.openFolderPick(""), true
}

// THERE IS NO CROSS ON A CONVERSATION'S TIP. `tipClosePress` stood here for
// one build on 2026-09-22, while the tip had a row of its own over the rule;
// the tip is the keys row's lowest rung again (render.go's [app.footHint]) and
// the keys row has never had one. Home's row keeps its cross (home.go's
// [app.homePress]).

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
	if a.placeHasDraft() && !a.composer.open && !a.target.pick.open {
		switch {
		case a.targetRow > 0 && y == a.targetRow && a.targetModelSpan.holds(x):
			next = hoverStatusModel
		case a.footRow > 0 && y == a.footRow && a.targetFolderSpan.holds(x):
			// The path is on the keys row now (hometip.go).
			next = hoverSeamProject
		}
	}
	if next != a.targetHover {
		a.targetHover = next
		a.touch()
	}
}
