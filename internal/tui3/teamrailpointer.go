package tui3

import (
	tea "charm.land/bubbletea/v2"
)

// ── THE TRAFFIC'S KEYS AND DOORS (teamrail.go says what it is) ─────────────

// trafficToggle lays one message out in full under a thread card, or folds it
// again. It moves no focus and changes nothing but what the cards draw.
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

// trafficKeyPress takes two keys on the conversation: [trafficKey] shows or
// hides the side column (sidecol.go's [app.sideToggle]), and [teamManagerKey]
// goes to the team's manager. Every overlay and page that owns the keyboard is
// read before it and keeps these keys.
func (a *app) trafficKeyPress(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	key := msg.String()
	if key != trafficKey && key != teamManagerKey {
		return nil, false
	}
	if a.asking() || a.at(pageSettings) || a.at(pageTasks) || a.at(pageHome) || a.pick.open ||
		a.copy.on || a.welcome.open || a.menu.open || a.comp.open || a.effPick.open || a.wall.on {
		return nil, false
	}
	if key == trafficKey {
		return nil, a.sideToggle()
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

// trafficHint is the composer's placeholder in a managed team: the person's
// words go to the manager when it is in front and to the member when one is,
// and the box says which. "" elsewhere. Frame-safe, allocation free for every
// conversation that is not in a managed team.
func (a *app) trafficHint() string {
	if a.teamsOff() || !a.wall.loaded || len(a.wall.teams) == 0 {
		return ""
	}
	// On the teams page the box says which team as well as who.
	if words := a.teamsComposerWord(); words != "" {
		return words
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
