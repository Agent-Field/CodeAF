package tui3

import (
	tea "charm.land/bubbletea/v2"
)

// ── THE RAIL UNDER THE HAND AND THE KEYS (teamrail.go says what it is) ─────

// trafficEdgeAt reports whether the pointer is on the rail's edge: put away on
// a wide frame, or the whole of it on a narrow one.
func (a *app) trafficEdgeAt(x, y int) bool {
	if a.trafficWidth() != trafficGripCols {
		return false
	}
	width, _ := a.size()
	top := a.bodyTop()
	return top >= 0 && y >= top && y < top+a.viewHeight() && x >= width-trafficGripCols && x < width
}

// trafficRowAt is the rail's body row under the pointer, measured from the
// body's top, and whether the pointer is over the column or the card at all.
func (a *app) trafficRowAt(x, y int) (int, bool) {
	d := a.traffic.drawn
	switch d.mode {
	case trafficColumn:
		if !a.trafficHoldsRail() {
			return -1, false
		}
	case trafficCard:
		if !a.trafficOverShowing() {
			return -1, false
		}
	default:
		return -1, false
	}
	top := a.bodyTop()
	rel := y - top
	if top < 0 || x < d.x0 || x >= d.x1 || rel < d.y0 || rel >= d.y1 {
		return -1, false
	}
	return rel, true
}

// trafficDoorAt is the door under column x on body row rel of the last frame's
// rail, and its index on the row, -1 for none.
func (d trafficDrawn) trafficDoorAt(rel, x int) (trafficDoor, int) {
	if rel < 0 || rel >= len(d.doors) {
		return trafficDoor{}, -1
	}
	for i, door := range d.doors[rel] {
		if door.span.holds(x) {
			return door, i
		}
	}
	return trafficDoor{}, -1
}

// trafficHoverAt is the hover the rail answers with: a handle or a message's
// words under the pointer. Anywhere else on the rail answers with nothing, so
// it does not light: it is not a door.
func (a *app) trafficHoverAt(x, y int) (hoverAt, bool) {
	if rel, ok := a.trafficRowAt(x, y); ok {
		d := a.traffic.drawn
		switch {
		case d.mode == trafficCard && rel == d.closeY && d.close.holds(x):
			return hoverAt{kind: hoverTrafficClose}, true
		case d.mode == trafficColumn && rel == d.hideY && d.hide.holds(x):
			return hoverAt{kind: hoverTrafficHide}, true
		case d.mode == trafficColumn && rel == d.hideY && d.tab.holds(x):
			return hoverAt{kind: hoverTrafficTab}, true
		}
		if _, i := d.trafficDoorAt(rel, x); i >= 0 {
			return hoverAt{kind: hoverTraffic, index: rel, entry: i}, true
		}
		return hoverAt{}, true
	}
	if a.trafficEdgeAt(x, y) {
		return hoverAt{kind: hoverTrafficGrip}, true
	}
	return hoverAt{}, false
}

// trafficPress answers a press on the rail and reports whether it took it. A
// handle goes to its member, and puts the card away; a message's words are
// laid out in full or folded again; `hide` puts the column away; `Close esc`
// puts the card away; the edge brings the column back, or on a narrow frame
// lays the card over the body or takes it off. The rail's other cells are
// furniture and take the press to do nothing.
func (a *app) trafficPress(x, y int) (tea.Cmd, bool) {
	if rel, ok := a.trafficRowAt(x, y); ok {
		d := a.traffic.drawn
		switch {
		case d.mode == trafficCard && rel == d.closeY && d.close.holds(x):
			a.trafficShow(false)
			return nil, true
		case d.mode == trafficColumn && rel == d.hideY && d.hide.holds(x):
			a.trafficShow(false)
			return nil, true
		case d.mode == trafficColumn && rel == d.hideY && d.tab.holds(x):
			a.trafficTasksShow(true)
			return nil, true
		}
		door, i := d.trafficDoorAt(rel, x)
		switch {
		case i < 0:
		case door.member != "":
			a.traffic.over = false
			return a.trafficGo(door.member), true
		case door.expand != "":
			a.trafficToggle(door.expand)
		}
		return nil, true
	}
	if a.trafficEdgeAt(x, y) {
		a.trafficShow(!a.trafficShowing())
		return nil, true
	}
	return nil, false
}

// trafficToggle lays one message out in full, or folds it again. It moves no
// focus and changes nothing but what the rail and the thread cards draw.
func (a *app) trafficToggle(key string) {
	if a.traffic.open == nil {
		a.traffic.open = map[string]bool{}
	}
	if a.traffic.open[key] {
		delete(a.traffic.open, key)
	} else {
		a.traffic.open[key] = true
	}
	a.traffic.opened++
	a.touch()
}

// trafficShowing reports whether the Traffic is in front of the person: the
// column on a wide frame, the card on a narrow one.
func (a *app) trafficShowing() bool {
	if a.trafficFits() {
		return !a.traffic.hidden
	}
	return a.traffic.over
}

// trafficShow shows the Traffic or puts it away, in whichever shape this frame
// has for it. The column's answer is kept for the window; the card is only
// ever laid over for as long as the person is reading it.
func (a *app) trafficShow(on bool) {
	if a.trafficFits() {
		a.traffic.hidden = !on
	} else {
		a.traffic.over = on
	}
	a.dropHover()
	a.touch()
}

// trafficGo switches to member key: its tab when the strip has one, and
// otherwise what the team kept of it, which is enough to open it again.
func (a *app) trafficGo(key string) tea.Cmd {
	if key == "" || key == a.frontTabKey() {
		return nil
	}
	for _, tab := range a.tabList() {
		if tab.key == key {
			return a.tabGo(tab)
		}
	}
	if held := a.behind[key]; held != nil {
		cmd, _ := a.bringForward(held.conv.SessionFile)
		return cmd
	}
	for _, t := range a.wall.teams {
		if m, ok := t.Member(key); ok && m.File != "" {
			return a.tabGo(chatTab{key: m.Key, file: m.File, where: m.Where, word: m.Word, full: m.Word})
		}
	}
	return nil
}

// trafficKeyPress takes the rail's keys on the conversation: `esc` while the
// card is over the body, [trafficKey] to show or hide the Traffic, and
// [teamManagerKey] to go to the team's manager. Every overlay and page that
// owns the keyboard is read before it and keeps these keys.
func (a *app) trafficKeyPress(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	if key != "esc" && key != trafficKey && key != teamManagerKey {
		return nil, false
	}
	if a.asking() || a.at(pageSettings) || a.at(pageTasks) || a.at(pageHome) || a.pick.open ||
		a.copy.on || a.welcome.open || a.menu.open || a.comp.open || a.effPick.open || a.wall.on {
		return nil, false
	}
	switch key {
	case "esc":
		if !a.trafficOverShowing() {
			return nil, false
		}
		a.trafficShow(false)
		return nil, true
	case trafficKey:
		if !a.trafficOn() {
			return nil, false
		}
		a.trafficShow(!a.trafficShowing())
		return nil, true
	}
	t, ok := a.teamOfFront()
	if !ok || t.Manager == "" {
		return nil, false
	}
	if a.teamsOff() {
		a.note(teamHostedWord)
		return nil, true
	}
	return a.trafficGo(t.Manager), true
}

// teamOfFront is the team the conversation in front belongs to and is run
// from: the team shown when it holds it, else the first managed team that
// does, else the team shown. Frame-safe: memory only.
func (a *app) teamOfFront() (team, bool) {
	if !a.wall.loaded {
		return team{}, false
	}
	front := a.frontTabKey()
	shown, showing := a.teamActive()
	if showing && teamHolds(shown, front) {
		return shown, true
	}
	for _, t := range a.wall.teams {
		if t.Manager != "" && teamHolds(t, front) {
			return t, true
		}
	}
	return shown, showing
}

// trafficHoverWords is what the hint line says with the pointer on the rail,
// "" anywhere else.
func (a *app) trafficHoverWords() string {
	d := a.traffic.drawn
	switch a.hot.kind {
	case hoverTraffic:
		if rel := a.hot.index; rel >= 0 && rel < len(d.doors) && a.hot.entry >= 0 && a.hot.entry < len(d.doors[rel]) {
			return d.doors[rel][a.hot.entry].hint
		}
	case hoverTrafficHide:
		return "Hide the traffic" + hintSegment + trafficKey
	case hoverTrafficTab:
		if i := d.hideY; i >= 0 && i < len(d.hints) {
			return d.hints[i]
		}
	case hoverTrafficClose:
		if d.closeY >= 0 && d.closeY < len(d.hints) {
			return d.hints[d.closeY]
		}
	case hoverTrafficGrip:
		words := "Show the team's traffic" + hintSegment + trafficKey
		if a.trafficOverShowing() {
			words = "Close the traffic" + hintSegment + "esc"
		} else if t, ok := a.teamFrontManaged(); ok {
			if n := a.trafficUnseen(t); n > 0 {
				words += hintSegment + itoa(n) + " new"
			}
		}
		return words
	}
	return ""
}

// trafficHint is the composer's placeholder in a managed team: the person's
// words go to the manager when it is in front and to the member when one is,
// and the box says which. "" elsewhere. Frame-safe, allocation free for every
// conversation that is not in a managed team.
func (a *app) trafficHint() string {
	if a.teamsOff() || !a.wall.loaded || len(a.wall.teams) == 0 {
		return ""
	}
	if _, ok := a.teamFrontManaged(); ok {
		return "to " + a.teamManagerMark() + " manager"
	}
	front := a.frontTabKey()
	for _, t := range a.wall.teams {
		if t.Manager == "" {
			continue
		}
		if m, ok := t.Member(front); ok && m.Handle != "" {
			return "to @" + m.Handle
		}
	}
	return ""
}
