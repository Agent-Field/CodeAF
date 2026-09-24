package tui3

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
)

// ── SPACES: NAMED SETS OF CONVERSATIONS, KEPT ON THEIR OWN FILE ────────────
//
// A team is a set of conversations a person marked on the wall and named.
// Activating one narrows the tab strip to its members; leaving it widens the
// strip again. Neither ever ends work: the strip's own law (chattabs.go) is
// that the × closes a view, never work, and a team is only a view of the
// strip. A member whose tab was closed is still a member, and it carries the
// file and the workspace it needs to be opened again.
//
// THE SETS LIVE IN <profile>/teams.json AND NEVER IN config.json. config.json
// is a flat dotted map of settings, and a nested list written there would read
// back as unset with no error and trip the unread-key notice besides. A file of
// its own has its own shape and its own failure.
//
// THE FRAME NEVER READS IT (framedisk_law_test.go). The file is read once, by
// [app.teamsEnsure], on an opening (the wall, an alt+digit); every function a
// frame calls reads only what that loaded.

// teamsFile is the file's name inside the profile directory.
const teamsFile = "teams.json"

// teamNameCells is the widest a suggested name may be, in cells.
const teamNameCells = 16

// teamsDisk is the file's whole shape. It is an object rather than a bare
// list so that a field added later is an addition, not a new format.
type teamsDisk struct {
	Teams []team `json:"teams"`
}

// teamsPath is where the sets live. AN EMPTY PROFILE DIRECTORY IS THE
// ORDINARY LAUNCH, not the absence of a profile: CODEAF_PROFILE_DIR is almost
// never exported, and the empty string resolves to this process's own profile
// in the state root, as every other file a profile keeps does
// (emptyprofile_test.go states the law; [config.ProfilePath] is the one
// resolution).
func teamsPath(profileDir string) string {
	return config.ProfilePath(profileDir, teamsFile)
}

// loadTeams reads the sets from profileDir. A missing file is no teams and
// no error, because a person who never made one has no file. A file that is there but
// unreadable is an ERROR and is left exactly as it was: what to do with it is
// the caller's decision, and silently treating it as empty would let the next
// save overwrite every set the person made.
func loadTeams(profileDir string) ([]team, error) {
	teams, _, err := loadTeamsHued(profileDir)
	return teams, err
}

// loadTeamsHued is [loadTeams] that also says, team by team, whether the
// file gave it a colour. A file from before colours has none, and a hue of 0
// is a real hue, so absence is read off the file and not off the value.
func loadTeamsHued(profileDir string) ([]team, []bool, error) {
	raw, err := os.ReadFile(teamsPath(profileDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var disk teamsDisk
	if err := json.Unmarshal(raw, &disk); err != nil {
		return nil, nil, fmt.Errorf("teams: %s is unreadable: %w", teamsFile, err)
	}
	var probe struct {
		Teams []struct {
			Hue *float64 `json:"hue"`
		} `json:"teams"`
	}
	_ = json.Unmarshal(raw, &probe)
	hued := make([]bool, len(disk.Teams))
	for i := range hued {
		hued[i] = i < len(probe.Teams) && probe.Teams[i].Hue != nil
	}
	return disk.Teams, hued, nil
}

// teamsHueLegacy gives every team the file left without a colour one from
// the generator, in file order, around the ones that have theirs. It depends
// on nothing but the file and the ramp, so the same file is coloured the same
// way on every load.
func teamsHueLegacy(teams []team, hued []bool, reserved []float64) {
	var used []teamHueSpec
	for i, sp := range teams {
		if i < len(hued) && hued[i] {
			used = append(used, sp.hueSpec())
		}
	}
	for i := range teams {
		if i < len(hued) && hued[i] {
			continue
		}
		next := nextTeamHue(used, reserved)
		next.Tier = teamTierFor(i)
		teams[i].Hue, teams[i].Tier = next.Hue, next.Tier
		used = append(used, next)
	}
}

// saveTeams writes the sets to profileDir, all at once or not at all: the
// bytes go to a temporary file beside the real one and are renamed over it, so
// a crash mid-write leaves the previous file whole rather than half a list.
func saveTeams(profileDir string, s []team) error {
	path := teamsPath(profileDir)
	dir := filepath.Dir(path)
	if s == nil {
		s = []team{}
	}
	raw, err := json.MarshalIndent(teamsDisk{Teams: s}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, teamsFile+".writing-*")
	if err != nil {
		return err
	}
	name := temp.Name()
	if _, err := temp.Write(raw); err != nil {
		_ = temp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// teamFromTabs makes a team of the given tabs. The start tab and the work
// tab are pages of this window rather than conversations, and a tab with no
// key is nothing that could be found again, so none of those become members.
// A key already taken is taken once.
func teamFromTabs(name string, tabs []chatTab, now time.Time) team {
	sp := team{Name: name, Made: now}
	for _, tab := range tabs {
		if tab.start || tab.work || tab.key == "" || teamHolds(sp, tab.key) {
			continue
		}
		sp.Members = append(sp.Members, teamMember{Key: tab.key, File: tab.file, Where: tab.where, Word: tab.word})
	}
	return sp
}

// teamHolds reports whether key is one of sp's members.
func teamHolds(sp team, key string) bool {
	for _, m := range sp.Members {
		if m.Key == key {
			return true
		}
	}
	return false
}

// teamSuggestName is the name the naming prompt starts with. Conversations
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

// teamTabs is sp as strip tabs, in the order the person stored them. A member
// this window still has a tab for is THAT tab, so its here, held and signal
// are the strip's own and a press attaches rather than opens; a member whose
// tab was closed is rebuilt from what the team kept, which is enough for
// [app.tabGo] to open it again.
func teamTabs(sp team, live []chatTab) []chatTab {
	out := make([]chatTab, 0, len(sp.Members))
	for _, m := range sp.Members {
		tab, found := chatTab{}, false
		for _, t := range live {
			if t.key == m.Key {
				tab, found = t, true
				break
			}
		}
		if !found {
			tab = chatTab{key: m.Key, file: m.File, where: m.Where, word: m.Word, full: m.Word}
		}
		out = append(out, tab)
	}
	return out
}

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
	a.wall.active = -1
	teams, hued, err := loadTeamsHued(a.profileDir)
	if err != nil {
		path := teamsPath(a.profileDir)
		_ = os.Rename(path, path+".unreadable-"+strconv.FormatInt(time.Now().UnixNano(), 10))
		teams = nil
	}
	teamsHueLegacy(teams, hued, teamReservedHues(a.pal))
	a.wall.teams = teams
}

// teamMake keeps tabs as a team called name and returns its index. A name
// already used, compared without case, is the same team with new members, so
// marking a second time is how a team is edited. The set is kept in memory
// even when the save fails, and the error says the disk did not take it.
func (a *app) teamMake(name string, tabs []chatTab) (int, error) {
	a.teamsEnsure()
	return a.teamMakeHued(name, tabs, nextTeamHue(a.teamHues(-1), teamReservedHues(a.pal)))
}

// teamHues is every team's colour but team skip's (-1 skips none).
func (a *app) teamHues(skip int) []teamHueSpec {
	out := make([]teamHueSpec, 0, len(a.wall.teams))
	for i, sp := range a.wall.teams {
		if i != skip {
			out = append(out, sp.hueSpec())
		}
	}
	return out
}

// teamMakeHued is [app.teamMake] with the colour chosen: the new-team card
// offers several and the person may take any. A team remade under a name it
// already has keeps the colour it had.
func (a *app) teamMakeHued(name string, tabs []chatTab, hue teamHueSpec) (int, error) {
	a.teamsEnsure()
	name = strings.TrimSpace(name)
	if name == "" {
		return -1, errors.New("a team needs a name")
	}
	sp := teamFromTabs(name, tabs, time.Now())
	if len(sp.Members) == 0 {
		return -1, errors.New("a team needs at least one conversation")
	}
	sp.Hue, sp.Tier = hue.Hue, hue.Tier
	at := -1
	for i, old := range a.wall.teams {
		if strings.EqualFold(old.Name, name) {
			at = i
			break
		}
	}
	if at < 0 {
		a.wall.teams = append(a.wall.teams, sp)
		at = len(a.wall.teams) - 1
	} else {
		sp.Hue, sp.Tier = a.wall.teams[at].Hue, a.wall.teams[at].Tier
		a.wall.teams[at] = sp
	}
	return at, saveTeams(a.profileDir, a.wall.teams)
}

// teamToggleMember puts tab into team i, or takes it out if it is there.
func (a *app) teamToggleMember(i int, tab chatTab) error {
	a.teamsEnsure()
	if i < 0 || i >= len(a.wall.teams) {
		return fmt.Errorf("no team %d", i)
	}
	if teamHolds(a.wall.teams[i], tab.key) {
		return a.teamRemove(i, []string{tab.key})
	}
	return a.teamAdd(i, []chatTab{tab})
}

// teamRecolor gives team i another colour.
func (a *app) teamRecolor(i int, hue teamHueSpec) error {
	a.teamsEnsure()
	if i < 0 || i >= len(a.wall.teams) {
		return fmt.Errorf("no team %d", i)
	}
	a.wall.teams[i].Hue, a.wall.teams[i].Tier = hue.Hue, hue.Tier
	return saveTeams(a.profileDir, a.wall.teams)
}

// teamRename gives team i another name. A name another team has, compared
// without case, is refused: two teams one name would be one team to
// [app.teamMake].
func (a *app) teamRename(i int, name string) error {
	a.teamsEnsure()
	if i < 0 || i >= len(a.wall.teams) {
		return fmt.Errorf("no team %d", i)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("a team needs a name")
	}
	for j, sp := range a.wall.teams {
		if j != i && strings.EqualFold(sp.Name, name) {
			return fmt.Errorf("there is already a team called %s", sp.Name)
		}
	}
	a.wall.teams[i].Name = name
	return saveTeams(a.profileDir, a.wall.teams)
}

// teamsOf is the index of every team holding key, in order. Frame-safe:
// memory only.
func (a *app) teamsOf(key string) []int {
	if !a.wall.loaded || key == "" {
		return nil
	}
	var out []int
	for i, sp := range a.wall.teams {
		if teamHolds(sp, key) {
			out = append(out, i)
		}
	}
	return out
}

// teamAdd puts tabs into team i beside the members it already has, in the
// order given, each key once. It saves as [app.teamMake] does: the set is kept
// in memory even when the disk refuses it.
func (a *app) teamAdd(i int, tabs []chatTab) error {
	a.teamsEnsure()
	if i < 0 || i >= len(a.wall.teams) {
		return fmt.Errorf("no team %d", i)
	}
	sp := a.wall.teams[i]
	grown := sp
	grown.Members = append([]teamMember(nil), sp.Members...)
	for _, m := range teamFromTabs(sp.Name, tabs, sp.Made).Members {
		if !teamHolds(grown, m.Key) {
			grown.Members = append(grown.Members, m)
		}
	}
	a.wall.teams[i] = grown
	return saveTeams(a.profileDir, a.wall.teams)
}

// teamRemove takes the conversations with the given keys out of team i. The
// conversations are untouched, and so is the team, even when it is left
// holding nothing: a team emptied is still a name a person gave.
func (a *app) teamRemove(i int, keys []string) error {
	a.teamsEnsure()
	if i < 0 || i >= len(a.wall.teams) {
		return fmt.Errorf("no team %d", i)
	}
	gone := make(map[string]bool, len(keys))
	for _, k := range keys {
		gone[k] = true
	}
	sp := a.wall.teams[i]
	kept := make([]teamMember, 0, len(sp.Members))
	for _, m := range sp.Members {
		if !gone[m.Key] {
			kept = append(kept, m)
		}
	}
	sp.Members = kept
	a.wall.teams[i] = sp
	return saveTeams(a.profileDir, a.wall.teams)
}

// teamActivate narrows the strip to team i, or widens it to every tab for
// i < 0 (or an index that is gone). It changes the view and nothing else. If
// the conversation in front is not a member, the strip would be narrowed away
// from the page the person is on, so it steps to the first member instead and
// returns that switch.
func (a *app) teamActivate(i int) tea.Cmd {
	a.teamsEnsure()
	if i < 0 || i >= len(a.wall.teams) {
		a.wall.active = -1
		return nil
	}
	a.wall.active = i
	sp := a.wall.teams[i]
	if len(sp.Members) == 0 || teamHolds(sp, a.frontTabKey()) {
		return nil
	}
	tabs := teamTabs(sp, a.tabList())
	return a.tabGo(tabs[0])
}

// teamActive is the team the strip is narrowed to. It reads only memory and
// is safe from a frame; before the first load no team is active, whatever
// the zero value of the index says.
func (a *app) teamActive() (team, bool) {
	if !a.wall.loaded || a.wall.active < 0 || a.wall.active >= len(a.wall.teams) {
		return team{}, false
	}
	return a.wall.teams[a.wall.active], true
}

// teamNames is every team's name in order. Frame-safe: memory only.
func (a *app) teamNames() []string {
	if !a.wall.loaded {
		return nil
	}
	names := make([]string, len(a.wall.teams))
	for i, sp := range a.wall.teams {
		names[i] = sp.Name
	}
	return names
}

// teamDelete forgets team i. Its conversations are untouched; only the name
// for the set goes. The active index follows the team it pointed at, and is
// cleared if that was the one deleted.
func (a *app) teamDelete(i int) error {
	a.teamsEnsure()
	if i < 0 || i >= len(a.wall.teams) {
		return fmt.Errorf("no team %d", i)
	}
	a.wall.teams = append(a.wall.teams[:i], a.wall.teams[i+1:]...)
	switch {
	case a.wall.active == i:
		a.wall.active = -1
	case a.wall.active > i:
		a.wall.active--
	}
	return saveTeams(a.profileDir, a.wall.teams)
}

// teamStripTabs is what the strip draws given the tabs it would draw with no
// team. With a team active it is that team's members, plus the tab in front
// when it is not one of them: THE TAB YOU ARE ON NEVER VANISHES, because a
// strip that does not show where you are cannot show you the way back. With no
// team active the tabs come back unchanged. Frame-safe: memory only.
func (a *app) teamStripTabs(tabs []chatTab) []chatTab {
	sp, ok := a.teamActive()
	if !ok {
		return tabs
	}
	out := teamTabs(sp, tabs)
	for _, tab := range tabs {
		if tab.here && !teamHolds(sp, tab.key) {
			out = append(out, tab)
			break
		}
	}
	return out
}
