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
// A CONVERSATION MAY BE IN ANY NUMBER OF SPACES. The teams popover is a list
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

// wallOpenSettings puts up team i's settings: its name ready to edit, and
// the colour it has first among the others it could take.
func (a *app) wallOpenSettings(i int, at wallPop) {
	if i < 0 || i >= len(a.wall.teams) {
		return
	}
	at.kind = wallPopSettings
	at.team = i
	at.name = a.wall.teams[i].Name
	at.choices = append([]teamHueSpec{a.wall.teams[i].hueSpec()},
		teamHueChoices(a.teamHues(i), teamReservedHues(a.pal), wallSwatchCount-1)...)
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
// into team i, unless they are all in it already, in which case they all
// come out. A mixed box fills first, which is what a checkbox does.
func (a *app) wallToggleTeam(i int, tiles []wallTile) {
	if i < 0 || i >= len(a.wall.teams) {
		return
	}
	tabs := a.wallTabsFor(a.wall.pop.targets, tiles)
	all := len(tabs) > 0
	for _, tab := range tabs {
		if !teamHolds(a.wall.teams[i], tab.key) {
			all = false
		}
	}
	var err error
	if all {
		keys := make([]string, 0, len(tabs))
		for _, tab := range tabs {
			keys = append(keys, tab.key)
		}
		err = a.teamRemove(i, keys)
	} else {
		err = a.teamAdd(i, tabs)
	}
	if err != nil {
		a.note("the team is changed for this window, but " + err.Error())
	}
}

// wallPopNewTeam is + New team… in the teams popover: the new-team card,
// with the popover's conversations as the ones picked.
func (a *app) wallPopNewTeam(tiles []wallTile) {
	keys := a.wall.pop.targets
	a.wall.pop = wallPop{}
	a.wall.marked = map[string]bool{}
	for _, key := range keys {
		a.wall.marked[key] = true
	}
	a.wallStartNaming(tiles)
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
	if p.team < 0 || p.team >= len(a.wall.teams) || p.name == a.wall.teams[p.team].Name {
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
		a.wallPopNewTeam(tiles)
	case p.kind == wallPopMembers:
		p.cursor = hit.arg
		a.wallToggleTeam(hit.arg, tiles)
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
// with the arrows and presses one with team or enter; the settings popover
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
		last := len(a.wall.teams) // the + New team row
		switch key {
		case "up", "k":
			p.cursor = max(p.cursor-1, 0)
		case "down", "j":
			p.cursor = min(p.cursor+1, last)
		case "space", "enter":
			if p.cursor >= last {
				a.wallPopNewTeam(tiles)
				return nil
			}
			a.wallToggleTeam(p.cursor, tiles)
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
