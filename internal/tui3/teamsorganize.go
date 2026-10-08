package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// The existing suggestion dialog belongs to Teams. Its store and proposal
// logic remain shared; opening it no longer changes the Chats grid or overlay.
func (a *app) teamsOrganizeOver(frame string) string {
	a.tp.orgHits, a.tp.orgRect = nil, wallRect{}
	if !a.at(pageTeams) || a.wall.on || !a.wall.org.on {
		return frame
	}
	v := wallView{org: a.wallOrganizeFrame(nil, a.tabList()), hover: a.wall.hover}
	card := wallOrgCard(a.pal, wallGlyphsFor(a.pal.ascii), v, a.width, a.height)
	a.tp.orgHits = card.hits
	a.wall.org.top = card.top
	if len(card.rows) == 0 {
		return frame
	}
	a.tp.orgRect = wallRect{card.x, card.y, card.x + card.w, card.y + len(card.rows)}
	rows := strings.Split(frame, "\n")
	for len(rows) < a.height {
		rows = append(rows, "")
	}
	for dy, row := range card.rows {
		if y := card.y + dy; y >= 0 && y < len(rows) {
			rows[y] = wallSplice(rows[y], row, card.x, a.width)
		}
	}
	return strings.Join(rows[:a.height], "\n")
}

// The modal owns input even when the terminal is too small to draw it. Hidden
// Apply targets must never accept a positive confirmation.
func (a *app) teamsOrganizeRoute(msg tea.Msg) (tea.Cmd, bool) {
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		if m.String() == "ctrl+c" {
			return nil, false
		}
		if a.tp.orgRect.w() == 0 && m.String() != "esc" && m.String() != "q" {
			return nil, true
		}
		cmd := a.wallOrganizeKey(m.String())
		a.touch()
		return cmd, true
	case tea.MouseClickMsg:
		if m.Button != tea.MouseLeft {
			return nil, true
		}
		for _, h := range a.tp.orgHits {
			if m.X >= h.x0 && m.X < h.x1 && m.Y >= h.y0 && m.Y < h.y1 {
				return a.wallOrganizePress(h), true
			}
		}
		return nil, true
	case tea.MouseWheelMsg:
		if m.Button == tea.MouseWheelDown {
			a.wallOrganizeKey("down")
		}
		if m.Button == tea.MouseWheelUp {
			a.wallOrganizeKey("up")
		}
		a.touch()
		return nil, true
	case tea.MouseMotionMsg:
		a.wall.hover = wallHitRef{}
		for _, h := range a.tp.orgHits {
			if m.X >= h.x0 && m.X < h.x1 && m.Y >= h.y0 && m.Y < h.y1 {
				a.wall.hover = wallHitRef{kind: h.kind, arg: h.arg, id: h.id}
				break
			}
		}
		a.touch()
		return nil, true
	case tea.MouseReleaseMsg:
		return nil, true
	}
	return nil, false
}
