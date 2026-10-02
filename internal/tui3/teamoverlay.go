package tui3

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type teamOverlayView struct {
	key      string
	viewport tabViewport
}

// Each overlay remembers its own selection and strip window. Drafts and chat
// reading positions already belong to the conversation's keeper entry.
type teamOverlayViews struct {
	id      string
	views   map[string]teamOverlayView
	members []string
	badges  []teamBadge
	pending string
	hover   string
}

type teamBadge struct {
	id   string
	span hudSpan
}

func (a *app) teamViewSet(id string) {
	if a.teamViews.views == nil {
		a.teamViews.views = map[string]teamOverlayView{}
	}
	if a.teamViews.id != id {
		a.teamViews.views[a.teamViews.id] = teamOverlayView{key: a.frontTabKey(), viewport: a.tabView}
		a.tabView = a.teamViews.views[id].viewport
		a.teamViews.members = nil
		a.teamViews.pending = ""
	}
	a.teamViews.id = id
	a.wall.activeID = id
	a.chatTabBar = tabBar{}
	a.touch()
}

// Membership changes do not select a new member. Only the disappearance of
// the selected membership moves focus, to the closest surviving position.
func (a *app) teamOverlaySync() tea.Cmd {
	if a.wall.on || a.pageShowing() || a.startingChat() {
		return nil
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

// Membership badges occupy the existing air row under the strip. They change
// the view without changing the engine's reporting roles or ongoing work.
func (a *app) teamBadgesRow(width int) string {
	a.teamViews.badges = nil
	if a.wall.on || a.startingChat() {
		return ""
	}
	front := a.frontTabKey()
	var memberships []team
	for _, t := range a.wall.teams {
		if !t.Closed() && a.teamOverlayHolds(t, front) {
			memberships = append(memberships, t)
		}
	}
	line := " "
	paint := func(id, word string) {
		x := ansi.StringWidth(line)
		a.teamViews.badges = append(a.teamViews.badges, teamBadge{id: id, span: hudSpan{from: x, to: x + ansi.StringWidth(word)}})
		if a.teamViews.hover == id {
			word = a.tabHoverPaint(word)
		} else if id == a.wall.activeID && id != "" {
			word = a.pal.selected(a.pal.muted(word), 0)
		} else {
			word = a.pal.underline(a.pal.dim(word))
		}
		line += word + " "
	}
	for i, t := range memberships {
		room := width - ansi.StringWidth(line)
		reserve := 0
		if i < len(memberships)-1 {
			reserve = ansi.StringWidth(fmt.Sprintf(" +%d teams ", len(memberships)-i-1)) + 1
		}
		if room-reserve < 6 {
			word := fmt.Sprintf(" +%d teams ", len(memberships)-i)
			if ansi.StringWidth(word) <= room {
				paint("more", word)
			}
			break
		}
		word := " " + ansi.Truncate(t.Name, room-reserve-2, "…") + " "
		paint(t.ID, word)
	}
	return strings.TrimRight(line, " ")
}

func (a *app) teamBadgePress(x, y int) (tea.Cmd, bool) {
	if y != chatHeadRows-1 || a.pageShowing() || a.wall.on || a.startingChat() {
		return nil, false
	}
	for _, badge := range a.teamViews.badges {
		if x >= badge.span.from && x < badge.span.to {
			if badge.id == "more" {
				a.openTeamMenu()
				return nil, true
			}
			a.teamViewSet(badge.id)
			return nil, true
		}
	}
	return nil, false
}

func (a *app) teamBadgeMotion(x, y int) {
	hot := ""
	if y == chatHeadRows-1 && !a.pageShowing() && !a.wall.on {
		for _, b := range a.teamViews.badges {
			if x >= b.span.from && x < b.span.to {
				hot = b.id
				break
			}
		}
	}
	if a.teamViews.hover != hot {
		a.teamViews.hover = hot
		a.touch()
	}
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
