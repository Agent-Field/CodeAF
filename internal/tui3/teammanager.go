package tui3

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── THE MANAGER: ONE CONVERSATION THE TEAM IS RUN FROM ─────────────────────
//
// A team may have one manager, recorded in the store as a member key
// ([teamstore.Team.Manager]). The person's words go to the manager, and the
// manager's to the members, through the Traffic log (teamtraffic.go). What is
// here is how the interface shows it and how a person makes one.
//
// THE MANAGER'S PLACE IS THE FIRST TAB. While a team narrows the strip, its
// first position is the manager: the manager's own tab, `◆ harbor`, named after
// the team rather than after its title, because it speaks for the team. Until
// the team has one, the place is a quiet `+ Manager`, a word button that costs
// nothing: no conversation exists behind it until it is pressed, and pressing it
// starts one, in the team's folder, as the manager ([app.teamManagerStart]).
//
// A MANAGER IS ALSO MADE FROM A MEMBER, and unmade. The strip's team switcher
// (teammenu.go) and a tile's Teams popover (wallpop.go) offer `Make manager` on a
// member of the team shown, and `Remove manager` on the manager, which leaves it
// an ordinary member.
//
// ON THE WALL the manager's tile is pinned first while its team is shown, and
// wears the same `◆` before its name.
//
// Everything a frame calls here reads memory only (framedisk_law_test.go); the
// writes are [app.teamEdit]'s.

// teamManagerGlyph is the manager's mark, and its ASCII spelling.
const (
	teamManagerGlyph      = "◆"
	teamManagerGlyphASCII = "*"
)

// teamManagerSlotWord is the manager's place while the team has none.
const teamManagerSlotWord = "+ Manager"

// teamManagerMark is the manager's mark in this terminal's glyphs.
func (a *app) teamManagerMark() string {
	if a.pal.ascii {
		return teamManagerGlyphASCII
	}
	return a.linearMark(teamManagerGlyph, teamManagerGlyphASCII)
}

// teamManaged is the team whose manager is key: the team shown when it is one,
// and otherwise the first in stored order. Frame-safe: memory only, and it
// allocates nothing, because the composer asks it on every frame.
func (a *app) teamManaged(key string) (team, bool) {
	if !a.wall.loaded || key == "" {
		return team{}, false
	}
	if t, ok := a.teamActive(); ok && t.Manager == key {
		return t, true
	}
	for _, t := range a.wall.teams {
		if t.Manager == key {
			return t, true
		}
	}
	return team{}, false
}

// teamFrontManaged is the team the conversation in front manages.
func (a *app) teamFrontManaged() (team, bool) {
	if !a.wall.loaded || len(a.wall.teams) == 0 {
		return team{}, false
	}
	return a.teamManaged(a.frontTabKey())
}

// teamMakeManager makes the conversation tab team id's manager. It joins the
// team first when it is not a member, with its title, file and folder, and
// then takes the team's one manager's place, so a manager there before goes
// back to being an ordinary member.
func (a *app) teamMakeManager(id string, tab chatTab) error {
	if tab.key == "" || tab.start || tab.work {
		return nil
	}
	if _, err := a.teamAt(id); err != nil {
		return err
	}
	m := teamFromTabs("", []chatTab{tab}, a.now()).Members
	return a.teamEdit(func(f *teamstore.File) error {
		if len(m) > 0 {
			if err := f.AddMember(id, m[0]); err != nil {
				return err
			}
		}
		return f.SetManager(id, tab.key)
	})
}

// teamClearManager leaves team id without a manager. The conversation stays in
// the team as an ordinary member, and nothing about it is ended.
func (a *app) teamClearManager(id string) error {
	if _, err := a.teamAt(id); err != nil {
		return err
	}
	return a.teamEdit(func(f *teamstore.File) error { return f.ClearManager(id) })
}

// teamToggleManager is `Make manager` or `Remove manager` on tab in team id,
// whichever it is.
func (a *app) teamToggleManager(id string, tab chatTab) error {
	t, ok := a.teamByID(id)
	if !ok {
		return nil
	}
	if t.Manager != "" && t.Manager == tab.key {
		return a.teamClearManager(id)
	}
	return a.teamMakeManager(id, tab)
}

// teamWhere is the folder a new conversation for t opens in: its manager's,
// else the one its members share, else this window's.
func (a *app) teamWhere(t team) string {
	if m, ok := t.Member(t.Manager); ok && strings.TrimSpace(m.Where) != "" {
		return m.Where
	}
	where := ""
	for _, m := range t.Members {
		w := strings.TrimSpace(m.Where)
		if w == "" {
			continue
		}
		if where == "" {
			where = filepath.Clean(w)
		} else if filepath.Clean(w) != where {
			where = ""
			break
		}
	}
	if where != "" {
		return where
	}
	return a.workspace
}

// teamStartIn opens a fresh conversation for team t, in its folder, and puts it
// in front, with the conversation that was there going on running behind. It
// says why when it could not.
func (a *app) teamStartIn(t team) (tea.Cmd, string) {
	where := a.teamWhere(t)
	if a.start != nil && strings.TrimSpace(where) != "" && where != a.workspace {
		return a.startBeside(where)
	}
	if a.start != nil {
		return a.startBeside(a.workspace)
	}
	cmd, ok := a.renew()
	if !ok {
		return nil, newUnavailableWord
	}
	return cmd, ""
}

// teamManagerStart is `+ Manager` pressed: a new conversation, in the team's
// folder, made the team's manager. It is the one press that makes a manager out
// of nothing, and the conversation it opens is in front for the person's first
// words to it.
func (a *app) teamManagerStart() tea.Cmd {
	t, ok := a.teamActive()
	if !ok || t.Manager != "" {
		return nil
	}
	cmd, refusal := a.teamStartIn(t)
	if refusal != "" {
		a.note(refusal)
		return nil
	}
	tab := chatTab{key: a.convKey(a.file), file: a.file, where: a.workspace}
	if err := a.teamMakeManager(t.ID, tab); err != nil {
		a.note("the manager is set for this window, but " + err.Error())
	}
	a.touch()
	return cmd
}

// teamStripManager is t's members as the strip draws them, with the manager's
// place first: the manager's own tab named `◆ <team>`, or `+ Manager` while
// the team has none. tabs is [teamTabs]'s answer for t, and it is changed in
// place and returned. Frame-safe: memory only.
func (a *app) teamStripManager(t team, tabs []chatTab) []chatTab {
	if t.Manager == "" {
		slot := chatTab{word: teamManagerSlotWord, full: teamManagerSlotWord, slot: true}
		return append([]chatTab{slot}, tabs...)
	}
	word := a.teamManagerMark() + " " + t.Name
	for i, tab := range tabs {
		if tab.key != t.Manager {
			continue
		}
		tab.word, tab.full = word, word
		copy(tabs[1:i+1], tabs[:i])
		tabs[0] = tab
		return tabs
	}
	// THE MANAGER IS DRAWN BEFORE IT HAS A TITLE. The strip draws no nameless
	// tab ([app.tabList]), but the manager's tab is named for the team, so a
	// manager just started, or closed before its first answer, still has its
	// place: the conversation in front as itself, and a closed one from what
	// the team kept, which is what [app.tabGo] needs to open it again.
	m, _ := t.Member(t.Manager)
	here := t.Manager == a.frontTabKey()
	if !here && m.File == "" {
		return tabs
	}
	tab := chatTab{key: t.Manager, file: m.File, where: m.Where, word: word, full: word, here: here}
	if here {
		tab.file, tab.where = a.file, a.workspace
	}
	return append([]chatTab{tab}, tabs...)
}
