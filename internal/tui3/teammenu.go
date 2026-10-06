package tui3

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// The strip's switcher chooses only a team overlay. None restores ordinary
// Chats; selecting a team restores its last conversation and strip position.
// Management belongs to Teams, so this menu cannot change a membership or manager.
// It is modal: arrows choose a row, enter activates it, and escape or a press
// outside closes the menu without changing the view.

// teamMenu is the switcher's state: whether it is up, the row the keyboard is
// on, the row the pointer is on, and where the last frame drew it and its rows,
// in frame cells.
type teamMenu struct {
	on     bool
	cursor int
	top    int
	hover  wallHitRef
	card   wallRect
	hits   []wallHit
}

// The switcher's rows that are not a team. A team's row is a wallHitPopRow
// with arg wallPopTeam and the team's id.
const teamMenuNone = -10 // None selects ordinary Chats without a team overlay.

// teamMenuRow is one row of the switcher, as the painter and the keys both
// read it.
type teamMenuRow struct {
	code int
	id   string
	rule bool
	// depth is how far a team's row is indented: a sub-team stands under the
	// team it is in, as it does on the teams page's rail.
	depth int
}

// The optional global manager has its own overlay, distinct from the grid.
func (a *app) teamMenuRows() []teamMenuRow {
	rows := []teamMenuRow{{code: teamMenuNone}}
	if root, ok := a.teamsRoot(); ok && root.Manager != "" && !a.teamsManagerMissing(root) {
		rows = append(rows, teamMenuRow{code: wallPopTeam, id: root.ID})
	}
	for _, r := range a.teamsOpenTree() {
		rows = append(rows, teamMenuRow{code: wallPopTeam, id: r.id, depth: r.depth})
	}
	return rows
}

// teamMenuPicks is the rows the keyboard can land on, the rule left out.
func teamMenuPicks(rows []teamMenuRow) []teamMenuRow {
	out := rows[:0:0]
	for _, r := range rows {
		if !r.rule {
			out = append(out, r)
		}
	}
	return out
}

// openTeamMenu puts the switcher up with the keyboard on the team that is
// shown, or on None.
func (a *app) openTeamMenu() {
	a.teamsEnsure()
	a.teamMenu = teamMenu{on: true}
	picks := teamMenuPicks(a.teamMenuRows())
	for i, r := range picks {
		if (r.code == wallPopTeam && r.id == a.wall.activeID) || (r.code == teamMenuNone && a.wall.activeID == "") {
			a.teamMenu.cursor = i
			break
		}
	}
	a.touch()
}

func (a *app) closeTeamMenu() {
	a.teamMenu = teamMenu{}
	a.touch()
}

// teamMenuFront is the conversation in front as a member: its strip tab when
// the strip has one, and otherwise what this window knows of it.
func (a *app) teamMenuFront() chatTab {
	key := a.frontTabKey()
	for _, tab := range a.tabList() {
		if tab.key == key {
			return tab
		}
	}
	return chatTab{key: key, file: a.file, where: a.workspace}
}

// teamMenuDo is one row chosen, by the keyboard or the pointer.
func (a *app) teamMenuDo(r teamMenuRow) tea.Cmd {
	if r.code != wallPopTeam && r.code != teamMenuNone {
		return nil
	}
	a.closeTeamMenu()
	if a.wall.on {
		a.closeWall()
	}
	return a.teamActivate(r.id)
}

// Team creation has its own member picker over the originating Teams page.
func (a *app) teamMenuNewTeam() tea.Cmd                { return a.teamMenuNewTeamIn("") }
func (a *app) teamMenuNewTeamIn(parent string) tea.Cmd { return a.teamCreateOpen(parent) }

// teamMenuKey is a key while the switcher is up: the arrows walk its rows,
// enter takes one, esc puts it away, and nothing else leaks to what is under
// it.
func (a *app) teamMenuKey(msg tea.KeyPressMsg) tea.Cmd {
	picks := teamMenuPicks(a.teamMenuRows())
	m := &a.teamMenu
	switch msg.String() {
	case "esc":
		a.closeTeamMenu()
	case "up", "k":
		m.cursor = max(m.cursor-1, 0)
		a.touch()
	case "down", "j":
		m.cursor = min(m.cursor+1, len(picks)-1)
		a.touch()
	case "enter", "space":
		width, height := a.size()
		if len(a.teamMenuCard(width, height).rows) == 0 {
			return nil
		}
		if m.cursor >= 0 && m.cursor < len(picks) {
			return a.teamMenuDo(picks[m.cursor])
		}
	}
	return nil
}

// teamMenuHitAt is the switcher's row under the pointer on the last frame.
func (a *app) teamMenuHitAt(x, y int) (wallHit, bool) {
	for _, hit := range a.teamMenu.hits {
		if x >= hit.x0 && x < hit.x1 && y >= hit.y0 && y < hit.y1 {
			return hit, true
		}
	}
	return wallHit{}, false
}

// teamMenuPress answers a left press while the switcher is up. A press on a
// row takes it; one on the card between rows does nothing; one anywhere else
// puts the switcher away and does nothing else, the chip included, so the
// chip that opened it also closes it.
func (a *app) teamMenuPress(x, y int) tea.Cmd {
	if hit, ok := a.tabAt(x, y); ok && hit.kind == tabTeamClear {
		return a.teamMenuDo(teamMenuRow{code: teamMenuNone})
	}
	if hit, ok := a.teamMenuHitAt(x, y); ok {
		for _, r := range a.teamMenuRows() {
			if !r.rule && r.code == hit.arg && r.id == hit.id {
				return a.teamMenuDo(r)
			}
		}
		return nil
	}
	if !a.teamMenu.card.holds(x, y) {
		a.closeTeamMenu()
	}
	return nil
}

// teamMenuMotion lights the row under the pointer.
func (a *app) teamMenuMotion(x, y int) {
	// The chip remains clickable above the modal, so its name and clear mark
	// must keep their independent hover feedback while the menu is open.
	hot := hoverAt{}
	if hit, ok := a.tabAt(x, y); ok && (hit.kind == tabTeam || hit.kind == tabTeamClear) {
		hot = hoverAt{kind: hoverTab, index: hit.span.from}
	}
	if hot != a.hot {
		a.hot = hot
		a.touch()
	}
	hit, _ := a.teamMenuHitAt(x, y)
	if ref := hit.ref(); ref != a.teamMenu.hover {
		a.teamMenu.hover = ref
		a.touch()
	}
}

// ── DRAWING IT ──────────────────────────────────────────────────────────────

// teamMenuOver lays the switcher over a finished frame, under the chip, and
// writes down where its rows landed. With the switcher down it hands the frame
// back as it was given.
func (a *app) teamMenuOver(frame string) string {
	if !a.teamMenu.on {
		return frame
	}
	width, height := a.size()
	card := a.teamMenuCard(width, height)
	a.teamMenu.hits = card.hits
	if len(card.rows) == 0 {
		a.teamMenu.card = wallRect{}
		return frame
	}
	a.teamMenu.card = wallRect{card.x, card.y, card.x + card.w, card.y + len(card.rows)}
	rows := strings.Split(frame, "\n")
	for len(rows) < height {
		rows = append(rows, "")
	}
	for dy, cr := range card.rows {
		if y := card.y + dy; y >= 0 && y < len(rows) {
			rows[y] = wallSplice(rows[y], cr, card.x, width)
		}
	}
	return strings.Join(rows[:height], "\n")
}

// teamMenuCard is the switcher as a card, hung from the chip's first cell on
// the row under the strip, kept a cell inside the frame's sides, and not drawn
// with a scrolling row window when the hierarchy is longer than the frame.
func (a *app) teamMenuCard(width, height int) wallCard {
	pal := a.pal
	g := wallGlyphsFor(pal.ascii)
	on, off := "◉", pal.glyph(tokens.GEmptyCell)
	if pal.ascii {
		on, off = "*", "o"
	}
	rows := a.teamMenuRows()
	picks := teamMenuPicks(rows)
	lit := func(r teamMenuRow) bool {
		if a.teamMenu.hover == (wallHitRef{kind: wallHitPopRow, arg: r.code, id: r.id}) {
			return true
		}
		c := a.teamMenu.cursor
		return c >= 0 && c < len(picks) && picks[c].code == r.code && picks[c].id == r.id
	}

	// The counts are of open conversations, as the wall's are.
	open := map[string]bool{}
	for _, tab := range a.tabList() {
		if !tab.start && !tab.work {
			open[tab.key] = true
		}
	}
	type line struct {
		row         teamMenuRow
		left, right string
		leftW       int
	}
	var lines []line
	for _, r := range rows {
		ln := line{row: r}
		switch r.code {
		case wallPopTeam:
			t, _ := a.teamByID(r.id)
			radio := off
			if t.ID == a.wall.activeID {
				radio = on
			}
			name := t.Name
			if ansi.StringWidth(name) > wallChipCap {
				name = ansi.Truncate(name, wallChipCap, g.more)
			}
			n := 0
			for _, m := range a.teamsCrewMembers(t) {
				if open[m.Key] {
					n++
				}
			}
			indent := strings.Repeat("  ", min(r.depth, 4))
			ln.left = pal.ink(radio) + " " + indent + a.tabTeamDot(t) + " " + pal.ink(name)
			ln.leftW = ansi.StringWidth(radio) + 3 + len(indent) + ansi.StringWidth(name)
			ln.right = strconv.Itoa(n)
		case teamMenuNone:
			radio := off
			if a.wall.activeID == "" {
				radio = on
			}
			ln.left = pal.ink(radio) + "   " + pal.ink("None")
			ln.leftW = ansi.StringWidth(radio) + 3 + len("None")
			ln.right = strconv.Itoa(len(open))
		}
		lines = append(lines, ln)
	}
	// The inner width fits the widest row with its count a cell from the
	// border, and never less than the brief's own drawing.
	inner := 24
	for _, ln := range lines {
		if ln.row.rule {
			continue
		}
		w := ln.leftW
		if ln.right != "" {
			w += 2 + len(ln.right) + 1
		}
		inner = max(inner, w)
	}
	const padX = 1
	inner = min(inner, width-2-2-2*padX)
	if inner < 16 {
		return wallCard{}
	}
	w := inner + 2 + 2*padX
	top := tabStripRow + 1
	footer := teamFooter(pal, inner, teamHint("up/down", "move"), teamHint("enter", "choose"), teamHint("esc", "cancel"))
	capacity := height - top - 2 - len(footer)
	if w > width-2 || capacity < 1 {
		return wallCard{}
	}
	m := &a.teamMenu
	m.cursor = min(max(m.cursor, 0), len(lines)-1)
	m.top = min(max(m.top, 0), max(len(lines)-capacity, 0))
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+capacity {
		m.top = m.cursor - capacity + 1
	}
	end := min(m.top+capacity, len(lines))
	title := "Teams"
	if end-m.top < len(lines) {
		title += " " + a.teamsDot() + " " + strconv.Itoa(m.top+1) + "-" + strconv.Itoa(end) + " of " + strconv.Itoa(len(lines))
	}
	x := min(max(a.wall.chip.from, 1), width-1-w)
	var cardLines []wallCardLine
	for _, ln := range lines[m.top:end] {
		if ln.row.rule {
			cardLines = append(cardLines, wallCardLine{rule: true})
			continue
		}
		leftRoom := inner
		if ln.right != "" {
			leftRoom -= len(ln.right) + 3
		}
		s := ansi.Truncate(ln.left, leftRoom, g.more)
		ln.leftW = ansi.StringWidth(s)
		if ln.right != "" {
			s += strings.Repeat(" ", max(inner-ln.leftW-len(ln.right)-1, 1)) + pal.dim(ln.right) + " "
		}
		cardLines = append(cardLines, wallCardLine{
			s:    wallPopRowPaint(pal, s, inner, lit(ln.row)),
			hits: []wallHit{{x0: 0, y0: 0, x1: inner, y1: 1, kind: wallHitPopRow, arg: ln.row.code, id: ln.row.id}},
		})
	}
	cardLines = append(cardLines, footer...)
	return wallCardBuild(pal, ansi.Truncate(title, w-5, g.more), cardLines, x, top, w, padX, 0)
}
