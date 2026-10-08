package tui3

import (
	"path/filepath"
	"sort"
	"time"
)

// Latest message means conversation activity, including assistant replies,
// rather than file modification or a team's settings/creation date. Old and
// hosted records without message timestamps use their known last user time.
func (a *app) teamsConversationAt(m teamMember) time.Time {
	at := a.tp.previews[m.Key].messageAt
	if at.IsZero() {
		at = a.tp.world[filepath.Clean(m.File)].At
	}
	if m.Key == a.frontTabKey() {
		for i := len(a.entries) - 1; i >= 0; i-- {
			e := a.entries[i]
			switch e.kind {
			case entryUser, entryAssistant, entryTeam, entrySteer, entryNote:
				if e.steer != nil && e.steer.at.After(at) {
					at = e.steer.at
				}
				if e.began.After(at) {
					at = e.began
				}
				if e.ended.After(at) {
					at = e.ended
				}
			}
		}
	}
	return at
}

// Each ancestor inherits its descendants' latest conversation message, so a
// recent subteam remains grouped with its parent in both navigation surfaces.
func (a *app) teamsRecentTimes() map[string]time.Time {
	children := map[string][]string{}
	own := map[string]time.Time{}
	for _, t := range a.wall.teams {
		children[t.Parent] = append(children[t.Parent], t.ID)
		for _, m := range append(append([]teamMember{}, t.Members...), t.FormerMembers...) {
			if at := a.teamsConversationAt(m); at.After(own[t.ID]) {
				own[t.ID] = at
			}
		}
	}
	out, visiting := map[string]time.Time{}, map[string]bool{}
	var visit func(string) time.Time
	visit = func(id string) time.Time {
		if at, ok := out[id]; ok {
			return at
		}
		if visiting[id] {
			return own[id]
		}
		visiting[id] = true
		at := own[id]
		for _, child := range children[id] {
			if next := visit(child); next.After(at) {
				at = next
			}
		}
		out[id] = at
		delete(visiting, id)
		return at
	}
	for _, t := range a.wall.teams {
		visit(t.ID)
	}
	return out
}

// Equal or unknown message times retain store order. Merely moving a team,
// opening a tab, changing a model or editing its title does not promote it.
func (a *app) teamsSortRecent(teams []team) {
	if len(teams) < 2 {
		return
	}
	latest := a.tp.recent
	if latest == nil {
		latest = a.teamsRecentTimes()
	}
	sort.SliceStable(teams, func(i, j int) bool { return latest[teams[i].ID].After(latest[teams[j].ID]) })
}
