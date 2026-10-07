package tui3

import (
	tea "charm.land/bubbletea/v2"
)

// The Teams overview owns its keyboard and pointer. Chat drafts and geometry
// are untouched until a person follows a member or interaction link.
func (a *app) teamsSync() tea.Cmd {
	if a.at(pageTeams) {
		a.teamsSettle()
		a.tp.focus = true
		a.tp.railW = teamsRailCols(a.width)
	}
	// A refresh can remove the last choice while the picker is open. Dismiss
	// it on the loop so an empty picker cannot retain keyboard ownership.
	if a.teamMenu.on && !a.teamMenuAvailable() {
		a.closeTeamMenu()
	}
	return a.teamOverlaySync()
}

// teamsNoticeRows is the pane's first rows: a move waiting on `Move` or
// `Cancel`, else the newest of a move and a close with its Undo, else nothing.
// One Undo at a time, so `u` and the button always mean the same thing.
func (a *app) teamsNoticeRows(d *teamsDraw, width, y int) []string {
	if p := a.tmove.pend; len(p.ids) > 0 && p.from == teamMoveFromPage {
		return a.teamsMoveRows(d, width, y)
	}
	if a.teamMoveUndoing() && a.tmove.undo.from == teamMoveFromPage {
		return a.teamsMoveRows(d, width, y)
	}
	return nil
}

// teamsUndoAny takes back only a move, never a disbanding.
func (a *app) teamsUndoAny() tea.Cmd { return a.teamMoveUndo() }

func (a *app) teamsRoute(msg tea.Msg) (tea.Cmd, bool) {
	if a.at(pageTeams) && !a.wall.on && a.wall.org.on {
		return a.teamsOrganizeRoute(msg)
	}
	if !a.at(pageTeams) || a.tsheet.on || a.teamMenu.on || a.wall.on || a.tmove.on || a.tcreate.on || a.tmembers.on || a.cdelete.on {
		return nil, false
	}
	switch m := msg.(type) {
	case tea.KeyPressMsg:
		return a.teamsRouteKey(m)
	case tea.MouseClickMsg:
		return a.teamsRouteMouse(m, m.Mouse())
	case tea.MouseReleaseMsg:
		return a.teamsRouteMouse(m, m.Mouse())
	case tea.MouseMotionMsg:
		return a.teamsRouteMouse(m, m.Mouse())
	case tea.MouseWheelMsg:
		return a.teamsRouteMouse(m, m.Mouse())
	}
	return nil, false
}

// teamsRouteKey gives overlays and pending gestures precedence over page keys.
func (a *app) teamsRouteKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	if key == "ctrl+c" {
		return nil, false
	}
	if key == "u" && a.teamMoveUndoing() && a.teamsHasKeys() {
		return a.teamsUndoAny(), true
	}
	// A DRAG IS DROPPED BY esc, and nothing happens (teamdrag.go).
	if key == "esc" && a.tdrag.press {
		a.teamDragCancel()
		return nil, true
	}
	// THE MEMBERS CARD has the keyboard while it is up (teamcrew.go).
	if a.tcrew.on {
		return a.teamCrewKey(msg), true
	}
	// A MOVE WAITING ON THE PERSON is answered by esc too.
	if key == "esc" && len(a.tmove.pend.ids) > 0 && a.tmove.pend.from == teamMoveFromPage {
		a.teamMoveCancel()
		return nil, true
	}
	return nil, false
}

// teamsRouteMouse is a pointer event on the page: the rail and the page's own
// rows answer while the shared router owns the head.
func (a *app) teamsRouteMouse(msg tea.Msg, m tea.Mouse) (tea.Cmd, bool) {
	if _, wheel := msg.(tea.MouseWheelMsg); wheel && !a.tcrew.on && !a.tdrag.press && m.Y >= placeHeadRows && m.Y < a.height-placeFootRowsFor(pageTeams, a.height) {
		delta := 0
		if m.Button == tea.MouseWheelDown {
			delta = 3
		}
		if m.Button == tea.MouseWheelUp {
			delta = -3
		}
		if delta != 0 {
			rail := a.tp.railW > 0 && m.X < a.tp.railW || a.tp.railW == 0 && m.Y < placeHeadRows+a.tp.paneTop
			if rail {
				a.teamsScrollRail(delta)
			} else {
				a.teamsScrollPane(delta)
			}
			return nil, true
		}
	}
	// THE MEMBERS CARD, AND A DRAG, TAKE THE POINTER FIRST (teamcrew.go,
	// teamdrag.go): a drag that started on the card goes on over the rail.
	if a.tdrag.press {
		switch msg.(type) {
		case tea.MouseMotionMsg:
			a.teamDragMotion(m.X, m.Y, m.Button == tea.MouseLeft)
			return nil, true
		case tea.MouseReleaseMsg:
			cmd, _ := a.teamDragRelease()
			return cmd, true
		case tea.MouseClickMsg:
			// A second press with the first never let go: the first is over.
			a.teamDragCancel()
		}
	}
	if a.tcrew.on {
		if cmd, took := a.teamCrewMouse(msg, m); took {
			return cmd, true
		}
		if _, click := msg.(tea.MouseClickMsg); click {
			return nil, true
		}
	}
	if m.Y < placeHeadRows {
		return nil, false
	}
	if t, ok := a.teamsTargetAt(m.X, m.Y); ok {
		switch msg.(type) {
		case tea.MouseClickMsg:
			if m.Button == tea.MouseLeft {
				a.tp.hot = t.ref()
				// A TEAM ROW OR A MEMBER CHIP MAY BE DRAGGED (teamdrag.go). A team
				// row selects on the press, as it always has; a member's door
				// waits for the release, so a drag from it never opens it.
				if a.teamDraggable(t) {
					member := t.act == teamsActMember
					id := t.id
					a.teamDragPress(m.X, m.Y, member, id, t.arg, t, member || t.pane)
					if member || t.pane {
						return nil, true
					}
				}
				return a.teamsDo(t), true
			}
		case tea.MouseMotionMsg:
			a.teamsHover(m.X, m.Y)
			return nil, true
		}
	}
	if _, isMotion := msg.(tea.MouseMotionMsg); isMotion && a.tp.hot != (teamsRef{}) {
		a.tp.hot = teamsRef{}
		a.touch()
	}
	if _, click := msg.(tea.MouseClickMsg); click {
		return nil, true
	}
	return nil, false
}
