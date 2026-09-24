package tui3

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// ── THE STRIP'S TEAM SWITCHER ───────────────────────────────────────────────
//
// The chip at the strip's left end, ` ● harbor ▾ ` while a team narrows the
// strip, is the switcher: a press opens a small menu hung under it, on every
// page the strip is drawn on, the conversations view included.
//
//	╭─ Teams ──────────────────╮
//	│ ◉ ● harbor            3  │
//	│ ○ ● orbit             5  │
//	│ ○   All              12  │
//	│ ──────────────────────── │
//	│ + Add this conversation  │
//	│ ◆ Make manager           │
//	│ + New team…              │
//	│   Team settings…         │
//	╰──────────────────────────╯
//
// Choosing a team narrows the strip as the wall's segments do, and switches the
// conversation in front only when it is not a member ([app.teamActivate]).
// The rows under the rule act on the conversation in front: into or out of the
// team that is shown, a new team starting with it, or the shown team's settings,
// the last two on the wall, where teams are edited. `Make manager` makes a
// member the team's manager, and on the manager it reads `Remove manager`
// (teammanager.go).
//
// WITH NO TEAM SHOWN THE CHIP IS STILL THERE, AS A QUIET ` Teams ▾ `, whenever
// there is a team to switch to. The strip is the one control on every page,
// and a switcher that appeared only once a team was already chosen could not be
// used to choose the first; with no team at all there is nothing to switch to
// and the chip takes no cells.
//
// It is modal as every menu is: while it is up it has the keyboard (↑ ↓ enter
// esc), and a press anywhere off it puts it away and does nothing else. It is
// drawn from memory alone; the teams were loaded on an opening (teamsEnsure).

// teamMenu is the switcher's state: whether it is up, the row the keyboard is
// on, the row the pointer is on, and where the last frame drew it and its rows,
// in frame cells.
type teamMenu struct {
	on     bool
	cursor int
	hover  wallHitRef
	card   wallRect
	hits   []wallHit
}

// The switcher's rows that are not a team. A team's row is a wallHitPopRow
// with arg wallPopTeam and the team's id.
const (
	teamMenuAll      = -10 // All
	teamMenuToggle   = -11 // + Add this conversation, or − Remove it
	teamMenuNew      = -12 // + New team…
	teamMenuSettings = -13 // Team settings…
	teamMenuManager  = -14 // ◆ Make manager, or Remove manager
	teamMenuClosed   = -15 // Closed · N, folded, which opens the teams page
)

// teamMenuRow is one row of the switcher, as the painter and the keys both
// read it.
type teamMenuRow struct {
	code int
	id   string
	rule bool
}

// teamMenuRows is the switcher's rows in order: each team, All, a rule, then
// the acts on the conversation in front. Add and settings are offered only
// while a team is shown, and Add only for a conversation that can be a member.
func (a *app) teamMenuRows() []teamMenuRow {
	var rows []teamMenuRow
	for _, t := range a.wallTeams() {
		rows = append(rows, teamMenuRow{code: wallPopTeam, id: t.ID})
	}
	rows = append(rows, teamMenuRow{code: teamMenuAll})
	// THE CLOSED TEAMS ARE ONE FOLDED ROW (ruling c-9), never a team on the
	// switcher: a press opens the teams page with its Closed fold open.
	if len(a.teamsClosed()) > 0 {
		rows = append(rows, teamMenuRow{code: teamMenuClosed})
	}
	rows = append(rows, teamMenuRow{rule: true})
	_, shown := a.teamActive()
	if shown && a.frontTabKey() != "" {
		rows = append(rows, teamMenuRow{code: teamMenuToggle})
		if t, _ := a.teamActive(); teamHolds(t, a.frontTabKey()) {
			rows = append(rows, teamMenuRow{code: teamMenuManager})
		}
	}
	rows = append(rows, teamMenuRow{code: teamMenuNew})
	if shown {
		rows = append(rows, teamMenuRow{code: teamMenuSettings})
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
// shown, or on All.
func (a *app) openTeamMenu() {
	a.teamsEnsure()
	a.teamMenu = teamMenu{on: true}
	picks := teamMenuPicks(a.teamMenuRows())
	for i, r := range picks {
		if (r.code == wallPopTeam && r.id == a.wall.activeID) || (r.code == teamMenuAll && a.wall.activeID == "") {
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
	switch r.code {
	case wallPopTeam, teamMenuAll:
		a.closeTeamMenu()
		if a.wall.on {
			// On the wall the choice is the wall's own segment's, which keeps
			// each team's place and never switches the conversation in front.
			a.wallSetTeam(r.id)
			return nil
		}
		return a.teamActivate(r.id)
	case teamMenuToggle:
		// The menu stays up, so the row's word is seen to flip.
		t, ok := a.teamActive()
		if !ok {
			return nil
		}
		if err := a.teamToggleMember(t.ID, a.teamMenuFront()); err != nil {
			a.note("the team is changed for this window, but " + err.Error())
		}
		a.touch()
		return nil
	case teamMenuManager:
		// The menu stays up, so the row's word is seen to flip.
		t, ok := a.teamActive()
		if !ok {
			return nil
		}
		if err := a.teamToggleManager(t.ID, a.teamMenuFront()); err != nil {
			a.note("the manager is changed for this window, but " + err.Error())
		}
		a.touch()
		return nil
	case teamMenuNew:
		a.closeTeamMenu()
		return a.teamMenuNewTeam()
	case teamMenuClosed:
		a.closeTeamMenu()
		a.tp.closedOpen = true
		return a.showPage(pageTeams)
	case teamMenuSettings:
		a.closeTeamMenu()
		// The team's card, over whatever is drawn (teamsheet.go): its name,
		// its colour, the settings it overrides and its close.
		return a.teamSheetOpen(a.wall.activeID, teamSheetSettings)
	}
	return nil
}

// teamMenuNewTeam opens the wall with the new-team card, the conversation in
// front already picked. A team shown that it is not a member of would hide its
// tile, so the wall widens to All first: the card makes a team of the tiles it
// can see.
func (a *app) teamMenuNewTeam() tea.Cmd {
	var open tea.Cmd
	if !a.wall.on {
		open = a.openWall()
	}
	front := a.frontTabKey()
	if t, ok := a.teamActive(); ok && !teamHolds(t, front) {
		a.wallSetTeam("")
	}
	tiles := a.wallShown(a.now())
	a.wall.marked = map[string]bool{}
	for i, tile := range tiles {
		if tile.tab.key == front {
			a.wall.marked[front] = true
			a.wallMove(i, len(tiles))
		}
	}
	return tea.Batch(open, a.wallStartNaming(tiles))
}

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
// on a frame too small to hold it whole.
func (a *app) teamMenuCard(width, height int) wallCard {
	pal := a.pal
	g := wallGlyphsFor(pal.ascii)
	k := wallKeysFor(pal.ascii)
	on, off := "◉", "○"
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
	front := a.frontTabKey()
	shown, _ := a.teamActive()
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
			for _, m := range t.Members {
				if open[m.Key] {
					n++
				}
			}
			ln.left = pal.ink(radio) + " " + a.tabTeamDot(t) + " " + pal.ink(name)
			ln.leftW = ansi.StringWidth(radio) + 3 + ansi.StringWidth(name)
			ln.right = strconv.Itoa(n)
		case teamMenuAll:
			radio := off
			if a.wall.activeID == "" {
				radio = on
			}
			ln.left = pal.ink(radio) + "   " + pal.ink("All")
			ln.leftW = ansi.StringWidth(radio) + 3 + 3
			ln.right = strconv.Itoa(len(open))
		case teamMenuClosed:
			word := "Closed " + a.teamsDot() + " " + strconv.Itoa(len(a.teamsClosed())) + " " + a.linearMark("▸", ">")
			ln.left = strings.Repeat(" ", ansi.StringWidth(off)+3) + pal.dim(word)
			ln.leftW = ansi.StringWidth(off) + 3 + ansi.StringWidth(word)
		case teamMenuToggle:
			word := "+ Add this conversation"
			if teamHolds(shown, front) {
				word = "− Remove this conversation"
				if pal.ascii {
					word = "- Remove this conversation"
				}
			}
			ln.left, ln.leftW = pal.ink(word), ansi.StringWidth(word)
		case teamMenuManager:
			word := a.teamManagerMenuWord(shown, front)
			ln.left, ln.leftW = pal.ink(word), ansi.StringWidth(word)
			if a.teamsOff() {
				ln.left = pal.dim(word)
			}
		case teamMenuNew:
			word := "+ New team" + k.more
			ln.left, ln.leftW = pal.ink(word), ansi.StringWidth(word)
		case teamMenuSettings:
			word := "  Team settings" + k.more
			ln.left, ln.leftW = pal.ink(word), ansi.StringWidth(word)
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
	w := inner + 2 + 2*padX
	h := len(lines) + 2
	top := placeTabRow + 1
	if w > width-2 || top+h > height {
		return wallCard{}
	}
	x := min(max(a.wall.chip.from, 1), width-1-w)
	var cardLines []wallCardLine
	for _, ln := range lines {
		if ln.row.rule {
			cardLines = append(cardLines, wallCardLine{rule: true})
			continue
		}
		s := ln.left
		if ln.right != "" {
			s += strings.Repeat(" ", max(inner-ln.leftW-len(ln.right)-1, 1)) + pal.dim(ln.right) + " "
		}
		cardLines = append(cardLines, wallCardLine{
			s:    wallPopRowPaint(pal, s, inner, lit(ln.row)),
			hits: []wallHit{{x0: 0, y0: 0, x1: inner, y1: 1, kind: wallHitPopRow, arg: ln.row.code, id: ln.row.id}},
		})
	}
	return wallCardBuild(pal, "Teams", cardLines, x, top, w, padX, 0)
}
