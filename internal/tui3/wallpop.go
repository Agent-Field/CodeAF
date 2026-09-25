package tui3

import (
	tea "charm.land/bubbletea/v2"
)

// ── THE WALL'S POPOVERS, WIRED ──────────────────────────────────────────────
//
// Two small cards hang off the control that opened them (wallbar.go draws
// them): which teams a conversation is in, and one team's settings. While
// one is up it has the keyboard, and a press anywhere off it puts it away and
// does nothing else, as a menu's does.
//
// A CONVERSATION MAY BE IN ANY NUMBER OF TEAMS. The teams popover is a list
// of boxes, one per team, and a box pressed is saved at once: there is no
// apply, because a grouping is cheap to change and cheap to change back.

// wallPopDone is the settings popover's Done: the name kept, the popover put
// away, what enter does. It is a row code beside wallcontract.go's, and is
// kept here because this file is the only one that answers it.
const wallPopDone = -5

// wallLocal is a hit's cells as an anchor in the painter's own rows, which
// start under the head.
func (a *app) wallLocal(hit wallHit) wallPop {
	return wallPop{x: hit.x0, y0: hit.y0 - a.wall.headRows, y1: hit.y1 - a.wall.headRows}
}

// wallAnchor is where the last frame drew the target of this kind and arg, as
// an anchor; a popover opened from the keyboard hangs where a press would
// have opened it. With no such target it hangs from the tile itself.
func (a *app) wallAnchor(kind wallHitKind, arg int) wallPop {
	var fallback *wallHit
	for i, hit := range a.wall.hits {
		if hit.kind == kind && hit.arg == arg {
			return a.wallLocal(hit)
		}
		if fallback == nil && hit.kind == wallHitTile && hit.arg == arg {
			fallback = &a.wall.hits[i]
		}
	}
	if fallback != nil {
		return a.wallLocal(*fallback)
	}
	return wallPop{x: 2, y0: 2, y1: 3}
}

// wallAnchorTeam is [app.wallAnchor] for a target on team id: where the last
// frame drew it, or a corner of the grid when it was not drawn.
func (a *app) wallAnchorTeam(kind wallHitKind, id string) wallPop {
	for _, hit := range a.wall.hits {
		if hit.kind == kind && hit.id == id {
			return a.wallLocal(hit)
		}
	}
	return wallPop{x: 2, y0: 2, y1: 3}
}

// wallOpenMembers puts up the teams popover for the conversations with the
// given keys.
func (a *app) wallOpenMembers(keys []string, at wallPop) {
	if len(keys) == 0 {
		return
	}
	a.teamsEnsure()
	at.kind = wallPopMembers
	at.targets = keys
	a.wall.pop = at
	a.wall.filterOn = false
}

// wallOpenSettings puts up team id's settings: its name ready to edit, and
// the colour it has first among the others it could take.
func (a *app) wallOpenSettings(id string, at wallPop) {
	t, ok := a.teamByID(id)
	if !ok {
		return
	}
	at.kind = wallPopSettings
	at.team = id
	at.name = t.Name
	at.choices = append([]teamHueSpec{t.HueSpec()},
		teamHueChoices(a.teamHues(id), teamReservedHues(a.pal), wallSwatchCount-1)...)
	at.choice = 0
	a.wall.pop = at
	a.wall.filterOn = false
}

// wallTabsFor is the conversations with the given keys, as the strip knows
// them; a key the wall is not showing (a filter hides it) is looked up on the
// strip's whole list.
func (a *app) wallTabsFor(keys []string, tiles []wallTile) []chatTab {
	var out []chatTab
	for _, key := range keys {
		found := false
		for _, tile := range tiles {
			if tile.tab.key == key {
				out, found = append(out, tile.tab), true
				break
			}
		}
		if found {
			continue
		}
		for _, tab := range a.tabList() {
			if tab.key == key {
				out = append(out, tab)
				break
			}
		}
	}
	return out
}

// wallToggleTeam is a box in the teams popover pressed: the targets all go
// into team id, unless they are all in it already, in which case they all
// come out. A mixed box fills first, which is what a checkbox does.
func (a *app) wallToggleTeam(id string, tiles []wallTile) {
	t, ok := a.teamByID(id)
	if !ok {
		return
	}
	tabs := a.wallTabsFor(a.wall.pop.targets, tiles)
	all := len(tabs) > 0
	for _, tab := range tabs {
		if !teamHolds(t, tab.key) {
			all = false
		}
	}
	var err error
	if all {
		keys := make([]string, 0, len(tabs))
		for _, tab := range tabs {
			keys = append(keys, tab.key)
		}
		err = a.teamRemove(id, keys)
	} else {
		err = a.teamAdd(id, tabs)
	}
	if err != nil {
		a.note("the team is changed for this window, but " + err.Error())
	}
}

// wallPopManagerRow is the teams popover's manager row: offered when the
// popover is about one conversation and that conversation is in the team shown,
// as Make manager, or as Remove manager when it is that team's manager already.
// Frame-safe: memory only.
func (a *app) wallPopManagerRow() int {
	p := a.wall.pop
	if p.kind != wallPopMembers || len(p.targets) != 1 {
		return 0
	}
	t, ok := a.teamActive()
	if !ok || !teamHolds(t, p.targets[0]) {
		return 0
	}
	if t.Manager == p.targets[0] {
		return wallManagerRemove
	}
	return wallManagerMake
}

// wallPopToggleManager is the popover's manager row pressed. The popover stays
// up, so the row's word is seen to flip.
func (a *app) wallPopToggleManager(tiles []wallTile) {
	if a.wallPopManagerRow() == 0 {
		return
	}
	t, _ := a.teamActive()
	tabs := a.wallTabsFor(a.wall.pop.targets, tiles)
	if len(tabs) == 0 {
		return
	}
	if err := a.teamToggleManager(t.ID, tabs[0]); err != nil {
		a.note("the manager is changed for this window, but " + err.Error())
	}
}

// wallPopNewTeam is + New team… in the teams popover: the new-team card,
// with the popover's conversations as the ones picked.
func (a *app) wallPopNewTeam(tiles []wallTile) tea.Cmd {
	keys := a.wall.pop.targets
	a.wall.pop = wallPop{}
	a.wall.marked = map[string]bool{}
	for _, key := range keys {
		a.wall.marked[key] = true
	}
	return a.wallStartNaming(tiles)
}

// wallRecolor takes colour j of the settings popover's choices, at once.
func (a *app) wallRecolor(j int) {
	p := &a.wall.pop
	if j < 0 || j >= len(p.choices) {
		return
	}
	p.choice = j
	if err := a.teamRecolor(p.team, p.choices[j]); err != nil {
		a.note("the colour is kept for this window, but " + err.Error())
	}
}

// wallRenameFromPop saves the settings popover's name if it changed.
func (a *app) wallRenameFromPop() {
	p := a.wall.pop
	if t, ok := a.teamByID(p.team); !ok || p.name == t.Name {
		return
	}
	if err := a.teamRename(p.team, p.name); err != nil {
		a.note(err.Error())
	}
}

// wallPopPress is a press on a popover's own row or swatch.
func (a *app) wallPopPress(hit wallHit, tiles []wallTile) tea.Cmd {
	p := &a.wall.pop
	switch {
	case hit.kind == wallHitSwatch && p.kind == wallPopSettings:
		a.wallRecolor(hit.arg)
	case p.kind == wallPopMembers && hit.arg == wallPopNew:
		return a.wallPopNewTeam(tiles)
	case p.kind == wallPopMembers && hit.arg == wallPopManager:
		p.cursor = len(a.wallTeams()) + 1
		a.wallPopToggleManager(tiles)
	case p.kind == wallPopMembers:
		if i := teamIndex(a.wallTeams(), hit.id); i >= 0 {
			p.cursor = i
		}
		a.wallToggleTeam(hit.id, tiles)
	case hit.arg == wallPopDelete:
		p.confirm = true
	case hit.arg == wallPopKeep:
		p.confirm = false
	case hit.arg == wallPopConfirm:
		a.wallDeleteTeam(p.team)
	case hit.arg == wallPopDone:
		a.wallRenameFromPop()
		a.wall.pop = wallPop{}
	}
	return nil
}

// wallPopKey is a key while a popover is up. The teams popover walks its rows
// with the arrows and presses one with space or enter; the settings popover
// takes typing into the name, the arrows through the colours, and enter to
// keep the name. esc puts either away.
func (a *app) wallPopKey(msg tea.KeyPressMsg, tiles []wallTile) tea.Cmd {
	p := &a.wall.pop
	key := msg.String()
	if key == "esc" {
		if p.confirm {
			p.confirm = false
			return nil
		}
		a.wall.pop = wallPop{}
		return nil
	}
	switch p.kind {
	case wallPopMembers:
		last := len(a.wallTeams()) // the + New team row
		end := last
		if a.wallPopManagerRow() != 0 {
			end = last + 1 // the manager row under it
		}
		switch key {
		case "up", "k":
			p.cursor = max(p.cursor-1, 0)
		case "down", "j":
			p.cursor = min(p.cursor+1, end)
		case "space", "enter":
			if p.cursor > last {
				a.wallPopToggleManager(tiles)
				return nil
			}
			if p.cursor >= last {
				return a.wallPopNewTeam(tiles)
			}
			if p.cursor >= 0 {
				a.wallToggleTeam(a.wallTeams()[p.cursor].ID, tiles)
			}
		}
	case wallPopSettings:
		switch key {
		case "enter":
			a.wallRenameFromPop()
			a.wall.pop = wallPop{}
		case "backspace":
			p.name = dropLastRune(p.name)
		case "left", "right":
			if c := len(p.choices); c > 0 {
				step := 1
				if key == "left" {
					step = c - 1
				}
				a.wallRecolor((p.choice + step) % c)
			}
		default:
			if t := msg.Key().Text; t != "" {
				p.name += t
			}
		}
	}
	return nil
}
