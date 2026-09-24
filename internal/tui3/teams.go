package tui3

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	teamstore "github.com/Agent-Field/codeaf/internal/teams"
)

// ── TEAMS: NAMED SETS OF CONVERSATIONS, KEPT ON THEIR OWN FILE ─────────────
//
// A team is a set of conversations a person marked on the wall and named.
// Activating one narrows the tab strip to its members; leaving it widens the
// strip again. Neither ever ends work: the strip's own law (chattabs.go) is
// that the × closes a view, never work, and a team is only a view of the
// strip. A member whose tab was closed is still a member, and it carries the
// file and the workspace it needs to be opened again.
//
// A TEAM IS KNOWN BY ITS ID, NEVER BY ITS PLACE OR ITS NAME. The id is random
// and minted once; the active team, each team's remembered place on the wall,
// an open popover and every pointer target name a team by it, so deleting or
// reordering one never moves any of those onto another. The name is the
// person's and may change.
//
// THE STORE IS internal/teams. The file, the ids, the tree, the migration from
// the first build's spaces.json, handles, the manager and the lock two writers
// share all live there, because the team tools a model calls (internal/session)
// write the same file. What is here is the interface's side: which tabs make a
// team, how a team is named on the card, what the strip shows.
//
// EVERY EDIT HERE IS THE STORE'S READ-MODIFY-WRITE ([app.teamEdit], over
// [teamstore.Update]). The change is made to what this window loaded, so the
// person sees it at once and keeps it even when the disk refuses, and then made
// again, under the lock, to the file as it is on disk now; what that wrote is
// what the window holds afterwards. A manager, a handle or a member another
// process wrote since the opening is therefore kept, where saving the whole
// loaded list would have put the old list back over it.
//
// THE FRAME NEVER READS IT (framedisk_law_test.go). The file is read once, by
// [app.teamsEnsure], on an opening (the wall, an alt+digit, the strip chip);
// every function a frame calls reads only what that loaded.

// teamsFile is the file's name inside the profile directory, and
// teamsLegacyFile the first build's.
const (
	teamsFile       = teamstore.FileName
	teamsLegacyFile = teamstore.LegacyFileName
)

// teamNameCells is the widest a suggested name may be, in cells.
const teamNameCells = 16

// teamsPath is where the sets live ([teamstore.Path]); an empty profile
// directory is the ordinary launch and resolves to this process's profile.
func teamsPath(profileDir string) string { return teamstore.Path(profileDir) }

// loadTeams reads the sets from profileDir, repaired and coloured around the
// reserved hues ([teamstore.LoadHued]). A missing file is no teams and no
// error; an unreadable one is an error and is left as it was.
func loadTeams(profileDir string, reserved []float64) ([]team, error) {
	f, err := teamstore.LoadHued(profileDir, reserved)
	if err != nil {
		return nil, err
	}
	return f.Teams, nil
}

// saveTeams writes the sets to profileDir as the whole file
// ([teamstore.Save]). THE INTERFACE NO LONGER CALLS IT: a whole-list save puts
// back what this window loaded over whatever another process wrote since, and
// every edit goes through [app.teamEdit] instead. It stays for the tests, which
// write a file as a fixture.
func saveTeams(profileDir string, s []team) error { return teamstore.Save(profileDir, s) }

// newTeamID is a fresh random id ([teamstore.NewID]).
func newTeamID() string { return teamstore.NewID() }

// teamIndex is where the team with id sits in teams, -1 when it is not there.
func teamIndex(teams []team, id string) int { return teamstore.Index(teams, id) }

// ── THE TREE ────────────────────────────────────────────────────────────────

// teamTree is the loaded sets as the store's file, for its tree walks. It
// shares the slice, so a change through it is a change to the loaded sets.
func (a *app) teamTree() *teamstore.File { return &teamstore.File{Teams: a.wall.teams} }

// teamByID is the team with id. Frame-safe: memory only.
func (a *app) teamByID(id string) (team, bool) { return a.teamTree().Team(id) }

// teamChildren is every team whose parent is id, in stored order; id "" is
// the top level.
func (a *app) teamChildren(id string) []team { return a.teamTree().Children(id) }

// teamAncestors is id's parent, its parent's parent, and so on to the top,
// nearest first.
func (a *app) teamAncestors(id string) []team { return a.teamTree().Ancestors(id) }

// teamSetParent puts team id under parent, or at the top level for "". The
// parent must exist and may not be the team or anything under it. It is the
// tree's one door and nothing in the interface opens it yet.
func (a *app) teamSetParent(id, parent string) error {
	return a.teamEdit(func(f *teamstore.File) error { return f.SetParent(id, parent) })
}

// ── MEMBERS AND NAMES ───────────────────────────────────────────────────────

// teamFromTabs makes a team of the given tabs. The start tab and the work
// tab are pages of this window rather than conversations, and a tab with no
// key is nothing that could be found again, so none of those become members.
// A key already taken is taken once.
func teamFromTabs(name string, tabs []chatTab, now time.Time) team {
	t := team{Name: name, Made: now}
	for _, tab := range tabs {
		if tab.start || tab.work || tab.key == "" || teamHolds(t, tab.key) {
			continue
		}
		t.Members = append(t.Members, teamMember{Key: tab.key, File: tab.file, Where: tab.where, Word: tab.word})
	}
	return t
}

// teamHolds reports whether key is one of t's members.
func teamHolds(t team, key string) bool { return t.Holds(key) }

// teamSuggestName is a short name for tabs from their own words. Conversations
// that all sit in one workspace are most likely that project's, so its folder
// name is the suggestion; otherwise the first conversation's first word is.
// It is lowercased and cut to [teamNameCells] so it fits the strip's corner.
func teamSuggestName(tabs []chatTab) string {
	if name := teamFolder(tabs); name != "" {
		return name
	}
	first := ""
	for _, tab := range tabs {
		if tab.start || tab.work || tab.key == "" {
			continue
		}
		if words := strings.Fields(tab.word); len(words) > 0 {
			first = words[0]
			break
		}
	}
	return ansi.Truncate(strings.ToLower(first), teamNameCells, "")
}

// teamFolder is the folder name every conversation in tabs shares, lowercased
// and cut to [teamNameCells], or "" when they do not all sit in one.
func teamFolder(tabs []chatTab) string {
	where, shared := "", true
	for _, tab := range tabs {
		if tab.start || tab.work || tab.key == "" {
			continue
		}
		w := filepath.Clean(tab.where)
		if tab.where == "" {
			shared = false
		} else if where == "" {
			where = w
		} else if w != where {
			shared = false
		}
	}
	if !shared || where == "" {
		return ""
	}
	base := filepath.Base(where)
	if base == "/" || base == "." {
		return ""
	}
	return ansi.Truncate(strings.ToLower(base), teamNameCells, "")
}

// teamWords is the short list a new team's name is drawn from when its
// conversations share no folder: plain, pleasant, easy to say and to type.
var teamWords = []string{"harbor", "orbit", "lumen", "atlas", "ember", "quartz", "ridge", "tide", "cedar", "delta", "north", "prism"}

// teamFreshName is the name the new-team card starts with. Conversations
// that share a folder get the folder's name; otherwise it is a word from
// [teamWords]. It is never a name a team already has, compared without case,
// because [app.teamMake] would read that as editing the team that has it.
// not is the name being shuffled away from, and it also turns the folder off:
// a person asking for another name has seen that one. pick is the dice, so a
// test can load them.
func teamFreshName(tabs []chatTab, taken []string, not string, pick func(int) int) string {
	used := func(name string) bool {
		if strings.EqualFold(name, not) {
			return true
		}
		for _, t := range taken {
			if strings.EqualFold(t, name) {
				return true
			}
		}
		return false
	}
	if not == "" {
		if name := teamFolder(tabs); name != "" && !used(name) {
			return name
		}
	}
	var free []string
	for _, w := range teamWords {
		if !used(w) {
			free = append(free, w)
		}
	}
	if len(free) > 0 {
		return free[pick(len(free))]
	}
	// Every word is taken: the words again, numbered.
	base := teamWords[pick(len(teamWords))]
	for n := 2; ; n++ {
		if name := base + strconv.Itoa(n); !used(name) {
			return name
		}
	}
}

// teamTabs is t as strip tabs, in the order the person stored them. A member
// this window still has a tab for is THAT tab, so its here, held and signal
// are the strip's own and a press attaches rather than opens; a member whose
// tab was closed is rebuilt from what the team kept, which is enough for
// [app.tabGo] to open it again.
func teamTabs(t team, live []chatTab) []chatTab {
	out := make([]chatTab, 0, len(t.Members))
	for _, m := range t.Members {
		tab, found := chatTab{}, false
		for _, l := range live {
			if l.key == m.Key {
				tab, found = l, true
				break
			}
		}
		if !found {
			// A member closed before it had a name is not drawn, as the strip
			// draws no nameless tab ([app.tabList]); it is still a member.
			if strings.TrimSpace(m.Word) == "" {
				continue
			}
			tab = chatTab{key: m.Key, file: m.File, where: m.Where, word: m.Word, full: m.Word}
		}
		out = append(out, tab)
	}
	return out
}

// ── THE APP'S SETS ──────────────────────────────────────────────────────────

// teamsEnsure loads the sets the first time anything needs them. It reads
// disk, so it is called on an opening and never from a frame.
//
// AN UNREADABLE FILE IS MOVED ASIDE, NOT OVERWRITTEN. Starting empty is the
// only way the person can go on making teams, but the next save would then
// replace a file that may hold every set they made; renamed to
// teams.json.unreadable-<nanos> it survives for a person to recover.
func (a *app) teamsEnsure() {
	if a.wall.loaded {
		return
	}
	a.wall.loaded = true
	a.wall.activeID = ""
	teams, err := loadTeams(a.profileDir, teamReservedHues(a.pal))
	if err != nil {
		_, _ = teamstore.SetAside(a.profileDir)
		teams = nil
	}
	a.wall.teams = teams
}

// teamEdit makes one change to the teams, and it is the only way the
// interface writes them.
//
// THE CHANGE IS MADE TWICE, AND THAT IS THE POINT. First to what this window
// holds, so the strip and the wall show it on this frame and keep it when the
// disk refuses (the error says the disk did not take it, and the change stays
// "for this window"). Then again inside [teamstore.Update], to the file as it
// is on disk under the lock, so a manager, a handle, a member or a whole team
// another process wrote since this window loaded is kept rather than replaced.
// What that second pass wrote, tidied and coloured, is what the window holds
// afterwards. So change must depend only on the file it is handed and on
// values its caller chose beforehand: an id minted inside it would be two ids.
//
// Each member this window has an open tab for takes the tab's current name on
// the way ([app.teamRefreshWords]), which is how a conversation that joined
// before it had a title gets one, and with it a handle ([teamstore.DeriveHandle]
// through the store's tidy).
//
// It reads and writes the disk, so it is called from an update and never from a
// frame (framedisk_law_test.go).
func (a *app) teamEdit(change func(f *teamstore.File) error) error {
	a.teamsEnsure()
	mine := &teamstore.File{Version: teamstore.Version, Teams: teamsClone(a.wall.teams)}
	if err := change(mine); err != nil {
		return err
	}
	a.teamRefreshWords(mine.Teams)
	a.wall.teams = mine.Teams
	// A Traffic read already out may carry the file from before this write;
	// counting the write keeps it from being put back (teamtraffic.go).
	a.traffic.edits++
	reserved := teamReservedHues(a.pal)
	var wrote *teamstore.File
	err := teamstore.Update(a.profileDir, func(f *teamstore.File) error {
		if err := change(f); err != nil {
			return err
		}
		a.teamRefreshWords(f.Teams)
		f.Colour(reserved)
		// The store tidies the list in place after this returns and before it
		// writes (ids, handles, the manager), so the pointer kept here reads
		// what was written once Update is done.
		wrote = f
		return nil
	})
	if err != nil {
		return err
	}
	if wrote != nil {
		a.teamAdopt(teamsClone(wrote.Teams))
	}
	return nil
}

// teamAdopt makes teams what this window holds: a list that came off the disk,
// newer than the one loaded. The active team is cleared when it is gone, and
// nothing else moves, because everything else names a team by its id.
//
// A time a change put on a team came from the clock and carries its monotonic
// reading, which the same time read back from the file does not; it is dropped
// here, so a team is the same value whichever way it reached this window.
func (a *app) teamAdopt(teams []team) {
	for i := range teams {
		teams[i].Made = teams[i].Made.Round(0)
	}
	a.wall.teams = teams
	if a.wall.activeID != "" && teamIndex(teams, a.wall.activeID) < 0 {
		a.wall.activeID = ""
	}
}

// teamRefreshWords gives each member of teams that this window has an open tab
// for the tab's current name. A member keeps the name it has when its tab has
// none, or is a page rather than a conversation.
func (a *app) teamRefreshWords(teams []team) {
	for i := range teams {
		for j, m := range teams[i].Members {
			for _, tab := range a.chatTabs {
				if tab.key == m.Key && strings.TrimSpace(tab.word) != "" && !tab.start && !tab.work {
					teams[i].Members[j].Word = tab.word
					break
				}
			}
		}
	}
}

// teamMake keeps tabs as a team called name and returns its id. A name
// already used, compared without case, is the same team with new members, so
// marking a second time is how a team is edited. The set is kept in memory
// even when the save fails, and the error says the disk did not take it.
func (a *app) teamMake(name string, tabs []chatTab) (string, error) {
	a.teamsEnsure()
	return a.teamMakeHued(name, tabs, nextTeamHue(a.teamHues(""), teamReservedHues(a.pal)))
}

// teamHues is every team's colour but the one with id skip ("" skips none).
func (a *app) teamHues(skip string) []teamHueSpec {
	out := make([]teamHueSpec, 0, len(a.wall.teams))
	for _, t := range a.wall.teams {
		if skip == "" || t.ID != skip {
			out = append(out, t.HueSpec())
		}
	}
	return out
}

// teamMakeHued is [app.teamMake] with the colour chosen: the new-team card
// offers several and the person may take any. A team remade under a name it
// already has keeps its id, its place in the tree and its colour.
func (a *app) teamMakeHued(name string, tabs []chatTab, hue teamHueSpec) (string, error) {
	a.teamsEnsure()
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("a team needs a name")
	}
	t := teamFromTabs(name, tabs, time.Now())
	if len(t.Members) == 0 {
		return "", errors.New("a team needs at least one conversation")
	}
	// The id is minted here, once, and the change below only uses it: the
	// change is made twice ([app.teamEdit]) and must name the same team both
	// times.
	fresh := newTeamID()
	id := fresh
	err := a.teamEdit(func(f *teamstore.File) error {
		at := teamNamed(f.Teams, name)
		if at < 0 {
			made := t.Clone()
			made.ID = fresh
			made.SetHue(hue)
			f.Teams = append(f.Teams, made)
			id = fresh
			return nil
		}
		// A team remade under its name keeps what its members already had (a
		// handle above all) and keeps its manager while the manager is still
		// one of them; the store's tidy clears a manager that is not.
		old := &f.Teams[at]
		members := make([]teamMember, 0, len(t.Members))
		for _, m := range t.Members {
			if kept, ok := old.Member(m.Key); ok {
				kept.File, kept.Where = m.File, m.Where
				if strings.TrimSpace(m.Word) != "" {
					kept.Word = m.Word
				}
				m = kept
			}
			members = append(members, m)
		}
		old.Name, old.Members = name, members
		id = old.ID
		return nil
	})
	return id, err
}

// teamAt is the index of team id for a change, or an error naming it.
func (a *app) teamAt(id string) (int, error) {
	a.teamsEnsure()
	if i := teamIndex(a.wall.teams, id); i >= 0 {
		return i, nil
	}
	return -1, fmt.Errorf("no team %s", id)
}

// teamToggleMember puts tab into team id, or takes it out if it is there.
func (a *app) teamToggleMember(id string, tab chatTab) error {
	i, err := a.teamAt(id)
	if err != nil {
		return err
	}
	if teamHolds(a.wall.teams[i], tab.key) {
		return a.teamRemove(id, []string{tab.key})
	}
	return a.teamAdd(id, []chatTab{tab})
}

// teamRecolor gives team id another colour.
func (a *app) teamRecolor(id string, hue teamHueSpec) error {
	i, err := a.teamAt(id)
	if err != nil {
		return err
	}
	id = a.wall.teams[i].ID
	return a.teamEdit(func(f *teamstore.File) error {
		j := teamIndex(f.Teams, id)
		if j < 0 {
			return fmt.Errorf("no team %s", id)
		}
		f.Teams[j].SetHue(hue)
		return nil
	})
}

// teamRename gives team id another name. A name another team has, compared
// without case, is refused: two teams one name would be one team to
// [app.teamMake]. This and the new-team card are the only doors that name a
// team; nothing renames one on its own.
func (a *app) teamRename(id, name string) error {
	i, err := a.teamAt(id)
	if err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("a team needs a name")
	}
	for j, t := range a.wall.teams {
		if j != i && strings.EqualFold(t.Name, name) {
			return fmt.Errorf("there is already a team called %s", t.Name)
		}
	}
	id = a.wall.teams[i].ID
	return a.teamEdit(func(f *teamstore.File) error {
		j := teamIndex(f.Teams, id)
		if j < 0 {
			return fmt.Errorf("no team %s", id)
		}
		f.Teams[j].Name = name
		return nil
	})
}

// teamsOf is the id of every team holding key, in order. Frame-safe: memory
// only.
func (a *app) teamsOf(key string) []string {
	if !a.wall.loaded || key == "" {
		return nil
	}
	var out []string
	for _, t := range a.wall.teams {
		if teamHolds(t, key) {
			out = append(out, t.ID)
		}
	}
	return out
}

// teamAdd puts tabs into team id beside the members it already has, in the
// order given, each key once. It saves as [app.teamMake] does: the set is kept
// in memory even when the disk refuses it.
func (a *app) teamAdd(id string, tabs []chatTab) error {
	i, err := a.teamAt(id)
	if err != nil {
		return err
	}
	id = a.wall.teams[i].ID
	members := teamFromTabs("", tabs, time.Time{}).Members
	return a.teamEdit(func(f *teamstore.File) error {
		for _, m := range members {
			// A member joins with a handle when it has a title
			// ([teamstore.File.AddMember]); one already there is left as it is.
			if err := f.AddMember(id, m); err != nil {
				return err
			}
		}
		return nil
	})
}

// teamRemove takes the conversations with the given keys out of team id. The
// conversations are untouched, and so is the team, even when it is left
// holding nothing: a team emptied is still a name a person gave.
func (a *app) teamRemove(id string, keys []string) error {
	i, err := a.teamAt(id)
	if err != nil {
		return err
	}
	id = a.wall.teams[i].ID
	return a.teamEdit(func(f *teamstore.File) error {
		for _, k := range keys {
			// A manager taken out of its team is no longer its manager.
			if err := f.RemoveMember(id, k); err != nil {
				return err
			}
		}
		return nil
	})
}

// teamActivate narrows the strip to team id, or widens it to every tab for
// "" (or an id that is gone). It changes the view and nothing else. If the
// conversation in front is not a member, the strip would be narrowed away
// from the page the person is on, so it steps to the first member instead and
// returns that switch.
func (a *app) teamActivate(id string) tea.Cmd {
	a.teamsEnsure()
	t, ok := a.teamByID(id)
	if !ok {
		a.wall.activeID = ""
		return nil
	}
	a.wall.activeID = id
	if len(t.Members) == 0 || teamHolds(t, a.frontTabKey()) {
		return nil
	}
	tabs := teamTabs(t, a.tabList())
	return a.tabGo(tabs[0])
}

// teamActive is the team the strip is narrowed to. It reads only memory and
// is safe from a frame; before the first load no team is active.
func (a *app) teamActive() (team, bool) {
	if !a.wall.loaded {
		return team{}, false
	}
	return a.teamByID(a.wall.activeID)
}

// teamNames is every team's name in order. Frame-safe: memory only.
func (a *app) teamNames() []string {
	if !a.wall.loaded {
		return nil
	}
	names := make([]string, len(a.wall.teams))
	for i, t := range a.wall.teams {
		names[i] = t.Name
	}
	return names
}

// teamDelete forgets team id. Its conversations are untouched; only the name
// for the set goes. A team under it moves up to its parent, so the tree
// keeps every other team, and the active team is cleared only if it was this
// one: nothing else names a team by its place, so nothing else moves.
func (a *app) teamDelete(id string) error {
	i, err := a.teamAt(id)
	if err != nil {
		return err
	}
	id = a.wall.teams[i].ID
	if a.wall.activeID == id {
		a.wall.activeID = ""
	}
	delete(a.wall.places, id)
	return a.teamEdit(func(f *teamstore.File) error {
		teamDrop(f, id)
		return nil
	})
}

// teamDrop takes team id out of f and moves every team under it up to its
// parent. A team that is not there is nothing to drop.
func teamDrop(f *teamstore.File, id string) {
	j := teamIndex(f.Teams, id)
	if j < 0 {
		return
	}
	parent := f.Teams[j].Parent
	f.Teams = append(f.Teams[:j:j], f.Teams[j+1:]...)
	for k := range f.Teams {
		if f.Teams[k].Parent == id {
			f.Teams[k].Parent = parent
		}
	}
}

// teamJoinFront puts the conversation this window has just started, now in
// front, into the team the strip is narrowed to, so a new conversation opened
// while looking at a team is one of it. It is called from the two doors that
// mint a fresh conversation, /new and its road from the strip's + and the start
// page ([app.renewRefusing]) and a path typed on home ([app.startBeside]), at
// the moment the conversation first has its key. It is never called from a
// switch: moving to a conversation that already exists changes no team.
func (a *app) teamJoinFront() {
	a.teamJoinNew(chatTab{key: a.convKey(a.file), file: a.file, where: a.workspace})
}

// teamJoinNew is [app.teamJoinFront] for any tab. With no team active it does
// nothing, and it never loads the file, because a team can only be active once
// it has been loaded.
func (a *app) teamJoinNew(tab chatTab) {
	t, ok := a.teamActive()
	if !ok || tab.key == "" || teamHolds(t, tab.key) {
		return
	}
	if err := a.teamAdd(t.ID, []chatTab{tab}); err != nil {
		a.note("the conversation is in " + t.Name + " for this window, but " + err.Error())
	}
}

// teamStripTabs is what the strip draws given the tabs it would draw with no
// team. With a team active it is that team's members, plus the tab in front
// when it is not one of them: THE TAB YOU ARE ON NEVER VANISHES, because a
// strip that does not show where you are cannot show you the way back. With no
// team active the tabs come back unchanged. Frame-safe: memory only.
func (a *app) teamStripTabs(tabs []chatTab) []chatTab {
	t, ok := a.teamActive()
	if !ok {
		return tabs
	}
	out := a.teamStripManager(t, teamTabs(t, tabs))
	for _, tab := range tabs {
		if tab.here && !teamHolds(t, tab.key) {
			out = append(out, tab)
			break
		}
	}
	return out
}
