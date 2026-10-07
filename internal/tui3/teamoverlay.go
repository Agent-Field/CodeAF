package tui3

import (
	tea "charm.land/bubbletea/v2"
)

type teamOverlayView struct {
	key      string
	viewport tabViewport
}

// Each overlay remembers its own selection and strip window. Drafts and chat
// reading positions already belong to the conversation's keeper entry.
type teamOverlayViews struct {
	id       string
	views    map[string]teamOverlayView
	members  []string
	pending  string
	deferred bool
}

func (a *app) teamViewSet(id string) {
	if a.teamViews.views == nil {
		a.teamViews.views = map[string]teamOverlayView{}
	}
	if a.teamViews.id != id {
		if !a.teamViews.deferred {
			a.teamViews.views[a.teamViews.id] = teamOverlayView{key: a.frontTabKey(), viewport: a.tabView}
		}
		a.tabView = a.teamViews.views[id].viewport
		a.teamViews.members = nil
		a.teamViews.pending = ""
	}
	a.teamViews.id = id
	a.teamViews.deferred = false
	a.wall.activeID = id
	a.teamsSelectionFromView(id)
	a.chatTabBar = tabBar{}
	a.touch()
}

// Membership changes do not select a new member. Only the disappearance of
// the selected membership moves focus, to the closest surviving position.
func (a *app) teamOverlaySync() tea.Cmd {
	if a.wall.on || a.pageShowing() || a.startingChat() {
		return nil
	}
	if a.teamViews.deferred {
		return a.teamActivate(a.teamViews.id)
	}
	t, ok := a.teamActive()
	if !ok || t.Closed() {
		if a.teamViews.id != "" {
			a.teamViewSet("")
		}
		return nil
	}
	front := a.frontTabKey()
	if a.teamOverlayHolds(t, front) {
		a.teamViews.pending = ""
		a.teamViews.members = a.teamViews.members[:0]
		for _, tab := range a.teamStripTabs(a.tabList()) {
			if tab.key != "" {
				a.teamViews.members = append(a.teamViews.members, tab.key)
			}
		}
		return nil
	}
	if a.teamViews.pending != "" {
		return nil
	}
	at := 0
	for i, key := range a.teamViews.members {
		if key == front {
			at = i
			break
		}
	}
	all := a.teamStripTabs(a.tabList())
	tabs := make([]chatTab, 0, len(all))
	for _, tab := range all {
		if tab.key != "" {
			tabs = append(tabs, tab)
		}
	}
	if len(t.Members) == 0 || len(tabs) == 0 {
		a.teamViewSet("")
		return nil
	}
	target := tabs[min(at, len(tabs)-1)]
	if target.key == "" {
		return nil
	}
	a.teamViews.pending = target.key
	cmd := a.tabGo(target)
	if a.frontTabKey() != target.key {
		a.teamViewSet("")
	}
	return cmd
}

// Root membership shown in Chats is its current managers, matching the overview.
func (a *app) teamOverlayHolds(t team, key string) bool {
	if !t.Root {
		return teamHolds(t, key)
	}
	for _, member := range a.teamsCrewMembers(t) {
		if member.Key == key {
			return true
		}
	}
	return false
}

// Both surfaces name the same selected team. Clearing maps to the All teams
// overview without enabling the optional global manager overlay.
func (a *app) teamsSelectionFromView(id string) {
	selected := id
	if selected == "" {
		selected = teamsAllRow
	}
	if a.tp.sel != selected {
		a.teamsScrollSelection(selected)
		a.tp.sel = selected
		a.tp.expand, a.tp.answering = "", ""
		a.tp.cur = teamsRef{act: teamsActSelect, id: selected}
	}
}

// Choosing the overview enables All teams in Chats only when its optional
// global manager can actually be opened. Closed teams remain history only.
func (a *app) teamsViewFromSelection(id string) {
	a.teamsScrollSelection(id)
	view := ""
	if id == teamsAllRow {
		if root, ok := a.teamsRoot(); ok && root.Manager != "" && !a.teamsManagerMissing(root) {
			view = root.ID
		}
	} else if t, ok := a.teamByID(id); ok && !t.Closed() {
		if !t.Root || t.Manager != "" && !a.teamsManagerMissing(t) {
			view = id
		}
	}
	deferred := a.teamViews.deferred || view != a.teamViews.id
	a.teamViewSet(view)
	// The overview names the overlay now; Chats restores its conversation on return.
	a.teamViews.deferred = deferred
	// Retained history has no Chats overlay but keeps its selected record.
	a.tp.sel = id
}
