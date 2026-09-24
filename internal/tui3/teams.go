package tui3

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/config"
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
// THE MODEL IS READY FOR TREES AND MANAGERS THAT ARE NOT BUILT YET. A team has
// at most one parent, which must exist, and a chain of parents never loops
// ([app.teamSetParent] refuses). Manager is reserved for a member whose word
// will outrank the team's other conversations; no code path sets it today, and
// a file that has one keeps it.
//
// THE SETS LIVE IN <profile>/teams.json AND NEVER IN config.json. config.json
// is a flat dotted map of settings, and a nested list written there would read
// back as unset with no error and trip the unread-key notice besides. A file of
// its own has its own shape and its own failure. The file of the first build,
// spaces.json, is read once when teams.json is not there yet, written out as
// teams.json and renamed to spaces.json.migrated, so no group a person made is
// lost on the way.
//
// THE FRAME NEVER READS IT (framedisk_law_test.go). The file is read once, by
// [app.teamsEnsure], on an opening (the wall, an alt+digit, the strip chip);
// every function a frame calls reads only what that loaded.

// teamsFile is the file's name inside the profile directory, and
// teamsLegacyFile the first build's.
const (
	teamsFile       = "teams.json"
	teamsLegacyFile = "spaces.json"
	teamsVersion    = 2
)

// teamNameCells is the widest a suggested name may be, in cells.
const teamNameCells = 16

// teamsDisk is the file's whole shape. It is an object rather than a bare
// list so that a field added later is an addition, not a new format. Legacy
// is the first build's list, read and never written.
type teamsDisk struct {
	Version int    `json:"version"`
	Teams   []team `json:"teams"`
	Legacy  []team `json:"spaces,omitempty"`
}

// teamKnownFields is every key [team] reads itself. Any other key a team
// carries on disk was written by a later build, and is kept as it was.
var teamKnownFields = map[string]bool{
	"id": true, "name": true, "parent": true, "members": true, "manager": true,
	"hue": true, "tier": true, "made": true,
}

// teamFields is team's stored shape without its methods, so the two
// methods below can use the ordinary encoding for the fields they know.
type teamFields struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Parent  string       `json:"parent"`
	Members []teamMember `json:"members"`
	Manager string       `json:"manager"`
	Hue     float64      `json:"hue"`
	Tier    int          `json:"tier"`
	Made    time.Time    `json:"made"`
}

// UnmarshalJSON reads a team, keeping every field it does not know in
// extra, and noting whether the file gave it a colour: a hue of 0 is a real
// hue, so absence is read off the file and not off the value.
func (t *team) UnmarshalJSON(raw []byte) error {
	var known teamFields
	if err := json.Unmarshal(raw, &known); err != nil {
		return err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		return err
	}
	*t = team{ID: known.ID, Name: known.Name, Parent: known.Parent, Members: known.Members,
		Manager: known.Manager, Hue: known.Hue, Tier: known.Tier, Made: known.Made}
	_, t.hued = all["hue"]
	for k, v := range all {
		if teamKnownFields[k] {
			continue
		}
		if t.extra == nil {
			t.extra = map[string]json.RawMessage{}
		}
		t.extra[k] = v
	}
	return nil
}

// MarshalJSON writes the known fields in their order, then any field a later
// build wrote, sorted, exactly as it was read.
func (t team) MarshalJSON() ([]byte, error) {
	raw, err := json.Marshal(teamFields{ID: t.ID, Name: t.Name, Parent: t.Parent, Members: t.Members,
		Manager: t.Manager, Hue: t.Hue, Tier: t.Tier, Made: t.Made})
	if err != nil || len(t.extra) == 0 {
		return raw, err
	}
	keys := make([]string, 0, len(t.extra))
	for k := range t.extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b bytes.Buffer
	b.Write(raw[:len(raw)-1])
	for _, k := range keys {
		name, _ := json.Marshal(k)
		b.WriteByte(',')
		b.Write(name)
		b.WriteByte(':')
		b.Write(t.extra[k])
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}

// newTeamID is a fresh random id: twelve hex digits, never derived from the
// name, so a rename is a rename and two teams once called the same are still
// two.
func newTeamID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand does not fail on the systems this runs on; the clock is
		// a unique enough stand-in if it ever does.
		return strconv.FormatInt(time.Now().UnixNano()&0xffffffffffff, 16)
	}
	return hex.EncodeToString(b[:])
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

func teamsLegacyPath(profileDir string) string {
	return config.ProfilePath(profileDir, teamsLegacyFile)
}

// loadTeams reads the sets from profileDir and puts them in order: every team
// has an id, a colour and a parent that exists, and no chain of parents loops.
// reserved is the hues the palette spends on meaning, which a team the file
// left uncoloured is kept out of.
//
// A missing file is no teams and no error, because a person who never made one
// has no file. A file that is there but unreadable is an ERROR and is left
// exactly as it was: what to do with it is the caller's decision, and silently
// treating it as empty would let the next save overwrite every set the person
// made.
//
// THE FIRST BUILD'S FILE IS MIGRATED HERE, once. With no teams.json and a
// spaces.json beside it, the old list is read, given ids and the colours it was
// drawn with, written as teams.json, and only then is spaces.json renamed to
// spaces.json.migrated. A write that fails leaves spaces.json where it was, and
// the next load tries again.
func loadTeams(profileDir string, reserved []float64) ([]team, error) {
	raw, err := os.ReadFile(teamsPath(profileDir))
	legacy := false
	if errors.Is(err, os.ErrNotExist) {
		raw, err = os.ReadFile(teamsLegacyPath(profileDir))
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		legacy = true
	}
	if err != nil {
		return nil, err
	}
	var disk teamsDisk
	if err := json.Unmarshal(raw, &disk); err != nil {
		name := teamsFile
		if legacy {
			name = teamsLegacyFile
		}
		return nil, fmt.Errorf("teams: %s is unreadable: %w", name, err)
	}
	teams := disk.Teams
	if legacy || (disk.Version < teamsVersion && len(teams) == 0) {
		teams = disk.Legacy
	}
	changed := teamsTidy(teams, reserved)
	if legacy {
		if err := saveTeams(profileDir, teams); err == nil {
			path := teamsLegacyPath(profileDir)
			_ = os.Rename(path, path+".migrated")
		}
	} else if changed || disk.Version < teamsVersion {
		_ = saveTeams(profileDir, teams)
	}
	return teams, nil
}

// teamsTidy puts a loaded list in order, in place, and reports whether it had
// to change anything: an id for every team (and a second one for an id used
// twice), a colour for every team the file left without one, and a parent
// that exists and does not lead back to the team itself.
func teamsTidy(teams []team, reserved []float64) bool {
	changed := false
	seen := map[string]bool{}
	for i := range teams {
		if teams[i].ID == "" || seen[teams[i].ID] {
			teams[i].ID = newTeamID()
			changed = true
		}
		seen[teams[i].ID] = true
	}
	for i := range teams {
		if p := teams[i].Parent; p != "" && (!seen[p] || p == teams[i].ID) {
			teams[i].Parent = ""
			changed = true
		}
	}
	// A loop in the file is cut where it is first met, at the team whose
	// parent leads back to it.
	for i := range teams {
		if teams[i].Parent != "" && teamParentLoops(teams, teams[i].ID, teams[i].Parent) {
			teams[i].Parent = ""
			changed = true
		}
	}
	for _, t := range teams {
		if !t.hued {
			changed = true
			break
		}
	}
	teamsHueLegacy(teams, reserved)
	return changed
}

// teamsHueLegacy gives every team the file left without a colour one from
// the generator, in file order, around the ones that have theirs. It depends
// on nothing but the file and the ramp, so the same file is coloured the same
// way on every load, and a migrated file keeps the colours it was drawn with.
func teamsHueLegacy(teams []team, reserved []float64) {
	var used []teamHueSpec
	for _, t := range teams {
		if t.hued {
			used = append(used, t.hueSpec())
		}
	}
	for i := range teams {
		if teams[i].hued {
			continue
		}
		next := nextTeamHue(used, reserved)
		next.Tier = teamTierFor(i)
		teams[i].Hue, teams[i].Tier, teams[i].hued = next.Hue, next.Tier, true
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
	raw, err := json.MarshalIndent(teamsDisk{Version: teamsVersion, Teams: s}, "", "  ")
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

// ── THE TREE ────────────────────────────────────────────────────────────────

// teamIndex is where the team with id sits in teams, -1 when it is not there.
func teamIndex(teams []team, id string) int {
	if id == "" {
		return -1
	}
	for i, t := range teams {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// teamParentLoops reports whether making parent the parent of id would close
// a loop: whether id is parent itself or one of parent's ancestors. The walk
// is bounded by the list, so a loop already in the list cannot hang it.
func teamParentLoops(teams []team, id, parent string) bool {
	for at, steps := parent, 0; at != "" && steps <= len(teams); steps++ {
		if at == id {
			return true
		}
		i := teamIndex(teams, at)
		if i < 0 {
			return false
		}
		at = teams[i].Parent
	}
	return false
}

// teamByID is the team with id. Frame-safe: memory only.
func (a *app) teamByID(id string) (team, bool) {
	if i := teamIndex(a.wall.teams, id); i >= 0 {
		return a.wall.teams[i], true
	}
	return team{}, false
}

// teamChildren is every team whose parent is id, in stored order; id "" is
// the top level.
func (a *app) teamChildren(id string) []team {
	var out []team
	for _, t := range a.wall.teams {
		if t.Parent == id {
			out = append(out, t)
		}
	}
	return out
}

// teamAncestors is id's parent, its parent's parent, and so on to the top,
// nearest first.
func (a *app) teamAncestors(id string) []team {
	var out []team
	t, ok := a.teamByID(id)
	for ok && t.Parent != "" && len(out) < len(a.wall.teams) {
		t, ok = a.teamByID(t.Parent)
		if ok {
			out = append(out, t)
		}
	}
	return out
}

// teamSetParent puts team id under parent, or at the top level for "". The
// parent must exist and may not be the team or anything under it. It is the
// tree's one door and nothing in the interface opens it yet.
func (a *app) teamSetParent(id, parent string) error {
	a.teamsEnsure()
	i := teamIndex(a.wall.teams, id)
	if i < 0 {
		return fmt.Errorf("no team %s", id)
	}
	if parent != "" {
		if teamIndex(a.wall.teams, parent) < 0 {
			return fmt.Errorf("no team %s", parent)
		}
		if teamParentLoops(a.wall.teams, id, parent) {
			return errors.New("a team cannot sit under itself")
		}
	}
	a.wall.teams[i].Parent = parent
	return saveTeams(a.profileDir, a.wall.teams)
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
func teamHolds(t team, key string) bool {
	for _, m := range t.Members {
		if m.Key == key {
			return true
		}
	}
	return false
}

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
		path := teamsPath(a.profileDir)
		if _, statErr := os.Stat(path); statErr != nil {
			path = teamsLegacyPath(a.profileDir)
		}
		_ = os.Rename(path, path+".unreadable-"+strconv.FormatInt(time.Now().UnixNano(), 10))
		teams = nil
	}
	a.wall.teams = teams
}

// teamSave writes the sets, and says what the disk refused in the words the
// caller hands it; the sets are kept in memory either way.
func (a *app) teamSave() error {
	return saveTeams(a.profileDir, a.wall.teams)
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
			out = append(out, t.hueSpec())
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
	at := -1
	for i, old := range a.wall.teams {
		if strings.EqualFold(old.Name, name) {
			at = i
			break
		}
	}
	if at < 0 {
		t.ID, t.Hue, t.Tier, t.hued = newTeamID(), hue.Hue, hue.Tier, true
		a.wall.teams = append(a.wall.teams, t)
		return t.ID, a.teamSave()
	}
	old := a.wall.teams[at]
	old.Name, old.Members = name, t.Members
	a.wall.teams[at] = old
	return old.ID, a.teamSave()
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
	a.wall.teams[i].Hue, a.wall.teams[i].Tier = hue.Hue, hue.Tier
	return a.teamSave()
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
	a.wall.teams[i].Name = name
	return a.teamSave()
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
	t := a.wall.teams[i]
	t.Members = append([]teamMember(nil), t.Members...)
	for _, m := range teamFromTabs(t.Name, tabs, t.Made).Members {
		if !teamHolds(t, m.Key) {
			t.Members = append(t.Members, m)
		}
	}
	a.wall.teams[i] = t
	return a.teamSave()
}

// teamRemove takes the conversations with the given keys out of team id. The
// conversations are untouched, and so is the team, even when it is left
// holding nothing: a team emptied is still a name a person gave.
func (a *app) teamRemove(id string, keys []string) error {
	i, err := a.teamAt(id)
	if err != nil {
		return err
	}
	gone := make(map[string]bool, len(keys))
	for _, k := range keys {
		gone[k] = true
	}
	t := a.wall.teams[i]
	kept := make([]teamMember, 0, len(t.Members))
	for _, m := range t.Members {
		if !gone[m.Key] {
			kept = append(kept, m)
		}
	}
	t.Members = kept
	a.wall.teams[i] = t
	return a.teamSave()
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
	parent := a.wall.teams[i].Parent
	a.wall.teams = append(a.wall.teams[:i], a.wall.teams[i+1:]...)
	for j := range a.wall.teams {
		if a.wall.teams[j].Parent == id {
			a.wall.teams[j].Parent = parent
		}
	}
	if a.wall.activeID == id {
		a.wall.activeID = ""
	}
	delete(a.wall.places, id)
	return a.teamSave()
}

// teamJoinNew puts a conversation this window has just started into the team
// the strip is narrowed to, so a new conversation opened while looking at a
// team is one of it. It is called from the doors that mint a fresh
// conversation and never from a switch: moving to a conversation that already
// exists changes no team. With no team active it does nothing, and it never
// loads the file, because a team can only be active once it has been loaded.
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
	out := teamTabs(t, tabs)
	for _, tab := range tabs {
		if tab.here && !teamHolds(t, tab.key) {
			out = append(out, tab)
			break
		}
	}
	return out
}
