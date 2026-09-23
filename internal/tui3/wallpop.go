package tui3

import (
	tea "charm.land/bubbletea/v2"
)

// ── THE WALL'S POPOVERS, WIRED ──────────────────────────────────────────────
//
// Two small cards hang off the control that opened them (wallbar.go draws
// them): which spaces a conversation is in, and one space's settings. While
// one is up it has the keyboard, and a press anywhere off it puts it away and
// does nothing else, as a menu's does.
//
// A CONVERSATION MAY BE IN ANY NUMBER OF SPACES. The spaces popover is a list
// of boxes, one per space, and a box pressed is saved at once: there is no
// apply, because a grouping is cheap to change and cheap to change back.

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

// wallOpenMembers puts up the spaces popover for the conversations with the
// given keys.
func (a *app) wallOpenMembers(keys []string, at wallPop) {
	if len(keys) == 0 {
		return
	}
	a.spacesEnsure()
	at.kind = wallPopMembers
	at.targets = keys
	a.wall.pop = at
	a.wall.filterOn = false
}

// wallOpenSettings puts up space i's settings: its name ready to edit, and
// the colour it has first among the others it could take.
func (a *app) wallOpenSettings(i int, at wallPop) {
	if i < 0 || i >= len(a.wall.spaces) {
		return
	}
	at.kind = wallPopSettings
	at.space = i
	at.name = a.wall.spaces[i].Name
	at.choices = append([]spaceHueSpec{a.wall.spaces[i].hueSpec()},
		spaceHueChoices(a.spaceHues(i), spaceReservedHues(a.pal), wallSwatchCount-1)...)
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

// wallToggleSpace is a box in the spaces popover pressed: the targets all go
// into space i, unless they are all in it already, in which case they all
// come out. A mixed box fills first, which is what a checkbox does.
func (a *app) wallToggleSpace(i int, tiles []wallTile) {
	if i < 0 || i >= len(a.wall.spaces) {
		return
	}
	tabs := a.wallTabsFor(a.wall.pop.targets, tiles)
	all := len(tabs) > 0
	for _, tab := range tabs {
		if !spaceHolds(a.wall.spaces[i], tab.key) {
			all = false
		}
	}
	var err error
	if all {
		keys := make([]string, 0, len(tabs))
		for _, tab := range tabs {
			keys = append(keys, tab.key)
		}
		err = a.spaceRemove(i, keys)
	} else {
		err = a.spaceAdd(i, tabs)
	}
	if err != nil {
		a.note("the space is changed for this window, but " + err.Error())
	}
}

// wallPopNewSpace is + New space… in the spaces popover: the new-space card,
// with the popover's conversations as the ones picked.
func (a *app) wallPopNewSpace(tiles []wallTile) {
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
	if err := a.spaceRecolor(p.space, p.choices[j]); err != nil {
		a.note("the colour is kept for this window, but " + err.Error())
	}
}

// wallRenameFromPop saves the settings popover's name if it changed.
func (a *app) wallRenameFromPop() {
	p := a.wall.pop
	if p.space < 0 || p.space >= len(a.wall.spaces) || p.name == a.wall.spaces[p.space].Name {
		return
	}
	if err := a.spaceRename(p.space, p.name); err != nil {
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
		a.wallPopNewSpace(tiles)
	case p.kind == wallPopMembers:
		p.cursor = hit.arg
		a.wallToggleSpace(hit.arg, tiles)
	case hit.arg == wallPopDelete:
		p.confirm = true
	case hit.arg == wallPopKeep:
		p.confirm = false
	case hit.arg == wallPopConfirm:
		a.wallDeleteSpace(p.space)
	}
	return nil
}

// wallPopKey is a key while a popover is up. The spaces popover walks its rows
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
		last := len(a.wall.spaces) // the + New space row
		switch key {
		case "up", "k":
			p.cursor = max(p.cursor-1, 0)
		case "down", "j":
			p.cursor = min(p.cursor+1, last)
		case "space", "enter":
			if p.cursor >= last {
				a.wallPopNewSpace(tiles)
				return nil
			}
			a.wallToggleSpace(p.cursor, tiles)
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
