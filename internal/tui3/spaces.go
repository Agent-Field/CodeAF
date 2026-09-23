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
// A space is a set of conversations a person marked on the wall and named.
// Activating one narrows the tab strip to its members; leaving it widens the
// strip again. Neither ever ends work: the strip's own law (chattabs.go) is
// that the × closes a view, never work, and a space is only a view of the
// strip. A member whose tab was closed is still a member, and it carries the
// file and the workspace it needs to be opened again.
//
// THE SETS LIVE IN <profile>/spaces.json AND NEVER IN config.json. config.json
// is a flat dotted map of settings, and a nested list written there would read
// back as unset with no error and trip the unread-key notice besides. A file of
// its own has its own shape and its own failure.
//
// THE FRAME NEVER READS IT (framedisk_law_test.go). The file is read once, by
// [app.spacesEnsure], on an opening (the wall, an alt+digit); every function a
// frame calls reads only what that loaded.

// spacesFile is the file's name inside the profile directory.
const spacesFile = "spaces.json"

// spaceNameCells is the widest a suggested name may be, in cells.
const spaceNameCells = 16

// spacesDisk is the file's whole shape. It is an object rather than a bare
// list so that a field added later is an addition, not a new format.
type spacesDisk struct {
	Spaces []space `json:"spaces"`
}

// spacesPath is where the sets live. AN EMPTY PROFILE DIRECTORY IS THE
// ORDINARY LAUNCH, not the absence of a profile: CODEAF_PROFILE_DIR is almost
// never exported, and the empty string resolves to this process's own profile
// in the state root, as every other file a profile keeps does
// (emptyprofile_test.go states the law; [config.ProfilePath] is the one
// resolution).
func spacesPath(profileDir string) string {
	return config.ProfilePath(profileDir, spacesFile)
}

// loadSpaces reads the sets from profileDir. A missing file is no spaces and
// no error, because a person who never made one has no file. A file that is there but
// unreadable is an ERROR and is left exactly as it was: what to do with it is
// the caller's decision, and silently treating it as empty would let the next
// save overwrite every set the person made.
func loadSpaces(profileDir string) ([]space, error) {
	spaces, _, err := loadSpacesHued(profileDir)
	return spaces, err
}

// loadSpacesHued is [loadSpaces] that also says, space by space, whether the
// file gave it a colour. A file from before colours has none, and a hue of 0
// is a real hue, so absence is read off the file and not off the value.
func loadSpacesHued(profileDir string) ([]space, []bool, error) {
	raw, err := os.ReadFile(spacesPath(profileDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var disk spacesDisk
	if err := json.Unmarshal(raw, &disk); err != nil {
		return nil, nil, fmt.Errorf("spaces: %s is unreadable: %w", spacesFile, err)
	}
	var probe struct {
		Spaces []struct {
			Hue *float64 `json:"hue"`
		} `json:"spaces"`
	}
	_ = json.Unmarshal(raw, &probe)
	hued := make([]bool, len(disk.Spaces))
	for i := range hued {
		hued[i] = i < len(probe.Spaces) && probe.Spaces[i].Hue != nil
	}
	return disk.Spaces, hued, nil
}

// spacesHueLegacy gives every space the file left without a colour one from
// the generator, in file order, around the ones that have theirs. It depends
// on nothing but the file and the ramp, so the same file is coloured the same
// way on every load.
func spacesHueLegacy(spaces []space, hued []bool, reserved []float64) {
	var used []spaceHueSpec
	for i, sp := range spaces {
		if i < len(hued) && hued[i] {
			used = append(used, sp.hueSpec())
		}
	}
	for i := range spaces {
		if i < len(hued) && hued[i] {
			continue
		}
		next := nextSpaceHue(used, reserved)
		next.Tier = spaceTierFor(i)
		spaces[i].Hue, spaces[i].Tier = next.Hue, next.Tier
		used = append(used, next)
	}
}

// saveSpaces writes the sets to profileDir, all at once or not at all: the
// bytes go to a temporary file beside the real one and are renamed over it, so
// a crash mid-write leaves the previous file whole rather than half a list.
func saveSpaces(profileDir string, s []space) error {
	path := spacesPath(profileDir)
	dir := filepath.Dir(path)
	if s == nil {
		s = []space{}
	}
	raw, err := json.MarshalIndent(spacesDisk{Spaces: s}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, spacesFile+".writing-*")
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

// spaceFromTabs makes a space of the given tabs. The start tab and the work
// tab are pages of this window rather than conversations, and a tab with no
// key is nothing that could be found again, so none of those become members.
// A key already taken is taken once.
func spaceFromTabs(name string, tabs []chatTab, now time.Time) space {
	sp := space{Name: name, Made: now}
	for _, tab := range tabs {
		if tab.start || tab.work || tab.key == "" || spaceHolds(sp, tab.key) {
			continue
		}
		sp.Members = append(sp.Members, spaceMember{Key: tab.key, File: tab.file, Where: tab.where, Word: tab.word})
	}
	return sp
}

// spaceHolds reports whether key is one of sp's members.
func spaceHolds(sp space, key string) bool {
	for _, m := range sp.Members {
		if m.Key == key {
			return true
		}
	}
	return false
}

// spaceSuggestName is the name the naming prompt starts with. Conversations
// that all sit in one workspace are most likely that project's, so its folder
// name is the suggestion; otherwise the first conversation's first word is.
// It is lowercased and cut to [spaceNameCells] so it fits the strip's corner.
func spaceSuggestName(tabs []chatTab) string {
	if name := spaceFolder(tabs); name != "" {
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
	return ansi.Truncate(strings.ToLower(first), spaceNameCells, "")
}

// spaceFolder is the folder name every conversation in tabs shares, lowercased
// and cut to [spaceNameCells], or "" when they do not all sit in one.
func spaceFolder(tabs []chatTab) string {
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
	return ansi.Truncate(strings.ToLower(base), spaceNameCells, "")
}

// spaceWords is the short list a new space's name is drawn from when its
// conversations share no folder: plain, pleasant, easy to say and to type.
var spaceWords = []string{"harbor", "orbit", "lumen", "atlas", "ember", "quartz", "ridge", "tide", "cedar", "delta", "north", "prism"}

// spaceFreshName is the name the new-space card starts with. Conversations
// that share a folder get the folder's name; otherwise it is a word from
// [spaceWords]. It is never a name a space already has, compared without case,
// because [app.spaceMake] would read that as editing the space that has it.
// not is the name being shuffled away from, and it also turns the folder off:
// a person asking for another name has seen that one. pick is the dice, so a
// test can load them.
func spaceFreshName(tabs []chatTab, taken []string, not string, pick func(int) int) string {
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
		if name := spaceFolder(tabs); name != "" && !used(name) {
			return name
		}
	}
	var free []string
	for _, w := range spaceWords {
		if !used(w) {
			free = append(free, w)
		}
	}
	if len(free) > 0 {
		return free[pick(len(free))]
	}
	// Every word is taken: the words again, numbered.
	base := spaceWords[pick(len(spaceWords))]
	for n := 2; ; n++ {
		if name := base + strconv.Itoa(n); !used(name) {
			return name
		}
	}
}

// spaceTabs is sp as strip tabs, in the order the person stored them. A member
// this window still has a tab for is THAT tab, so its here, held and signal
// are the strip's own and a press attaches rather than opens; a member whose
// tab was closed is rebuilt from what the space kept, which is enough for
// [app.tabGo] to open it again.
func spaceTabs(sp space, live []chatTab) []chatTab {
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

// spacesEnsure loads the sets the first time anything needs them. It reads
// disk, so it is called on an opening and never from a frame.
//
// AN UNREADABLE FILE IS MOVED ASIDE, NOT OVERWRITTEN. Starting empty is the
// only way the person can go on making spaces, but the next save would then
// replace a file that may hold every set they made; renamed to
// spaces.json.unreadable-<nanos> it survives for a person to recover.
func (a *app) spacesEnsure() {
	if a.wall.loaded {
		return
	}
	a.wall.loaded = true
	a.wall.active = -1
	spaces, hued, err := loadSpacesHued(a.profileDir)
	if err != nil {
		path := spacesPath(a.profileDir)
		_ = os.Rename(path, path+".unreadable-"+strconv.FormatInt(time.Now().UnixNano(), 10))
		spaces = nil
	}
	spacesHueLegacy(spaces, hued, spaceReservedHues(a.pal))
	a.wall.spaces = spaces
}

// spaceMake keeps tabs as a space called name and returns its index. A name
// already used, compared without case, is the same space with new members, so
// marking a second time is how a space is edited. The set is kept in memory
// even when the save fails, and the error says the disk did not take it.
func (a *app) spaceMake(name string, tabs []chatTab) (int, error) {
	a.spacesEnsure()
	return a.spaceMakeHued(name, tabs, nextSpaceHue(a.spaceHues(-1), spaceReservedHues(a.pal)))
}

// spaceHues is every space's colour but space skip's (-1 skips none).
func (a *app) spaceHues(skip int) []spaceHueSpec {
	out := make([]spaceHueSpec, 0, len(a.wall.spaces))
	for i, sp := range a.wall.spaces {
		if i != skip {
			out = append(out, sp.hueSpec())
		}
	}
	return out
}

// spaceMakeHued is [app.spaceMake] with the colour chosen: the new-space card
// offers several and the person may take any. A space remade under a name it
// already has keeps the colour it had.
func (a *app) spaceMakeHued(name string, tabs []chatTab, hue spaceHueSpec) (int, error) {
	a.spacesEnsure()
	name = strings.TrimSpace(name)
	if name == "" {
		return -1, errors.New("a space needs a name")
	}
	sp := spaceFromTabs(name, tabs, time.Now())
	if len(sp.Members) == 0 {
		return -1, errors.New("a space needs at least one conversation")
	}
	sp.Hue, sp.Tier = hue.Hue, hue.Tier
	at := -1
	for i, old := range a.wall.spaces {
		if strings.EqualFold(old.Name, name) {
			at = i
			break
		}
	}
	if at < 0 {
		a.wall.spaces = append(a.wall.spaces, sp)
		at = len(a.wall.spaces) - 1
	} else {
		sp.Hue, sp.Tier = a.wall.spaces[at].Hue, a.wall.spaces[at].Tier
		a.wall.spaces[at] = sp
	}
	return at, saveSpaces(a.profileDir, a.wall.spaces)
}

// spaceToggleMember puts tab into space i, or takes it out if it is there.
func (a *app) spaceToggleMember(i int, tab chatTab) error {
	a.spacesEnsure()
	if i < 0 || i >= len(a.wall.spaces) {
		return fmt.Errorf("no space %d", i)
	}
	if spaceHolds(a.wall.spaces[i], tab.key) {
		return a.spaceRemove(i, []string{tab.key})
	}
	return a.spaceAdd(i, []chatTab{tab})
}

// spaceRecolor gives space i another colour.
func (a *app) spaceRecolor(i int, hue spaceHueSpec) error {
	a.spacesEnsure()
	if i < 0 || i >= len(a.wall.spaces) {
		return fmt.Errorf("no space %d", i)
	}
	a.wall.spaces[i].Hue, a.wall.spaces[i].Tier = hue.Hue, hue.Tier
	return saveSpaces(a.profileDir, a.wall.spaces)
}

// spaceRename gives space i another name. A name another space has, compared
// without case, is refused: two spaces one name would be one space to
// [app.spaceMake].
func (a *app) spaceRename(i int, name string) error {
	a.spacesEnsure()
	if i < 0 || i >= len(a.wall.spaces) {
		return fmt.Errorf("no space %d", i)
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("a space needs a name")
	}
	for j, sp := range a.wall.spaces {
		if j != i && strings.EqualFold(sp.Name, name) {
			return fmt.Errorf("there is already a space called %s", sp.Name)
		}
	}
	a.wall.spaces[i].Name = name
	return saveSpaces(a.profileDir, a.wall.spaces)
}

// spacesOf is the index of every space holding key, in order. Frame-safe:
// memory only.
func (a *app) spacesOf(key string) []int {
	if !a.wall.loaded || key == "" {
		return nil
	}
	var out []int
	for i, sp := range a.wall.spaces {
		if spaceHolds(sp, key) {
			out = append(out, i)
		}
	}
	return out
}

// spaceAdd puts tabs into space i beside the members it already has, in the
// order given, each key once. It saves as [app.spaceMake] does: the set is kept
// in memory even when the disk refuses it.
func (a *app) spaceAdd(i int, tabs []chatTab) error {
	a.spacesEnsure()
	if i < 0 || i >= len(a.wall.spaces) {
		return fmt.Errorf("no space %d", i)
	}
	sp := a.wall.spaces[i]
	grown := sp
	grown.Members = append([]spaceMember(nil), sp.Members...)
	for _, m := range spaceFromTabs(sp.Name, tabs, sp.Made).Members {
		if !spaceHolds(grown, m.Key) {
			grown.Members = append(grown.Members, m)
		}
	}
	a.wall.spaces[i] = grown
	return saveSpaces(a.profileDir, a.wall.spaces)
}

// spaceRemove takes the conversations with the given keys out of space i. The
// conversations are untouched, and so is the space, even when it is left
// holding nothing: a space emptied is still a name a person gave.
func (a *app) spaceRemove(i int, keys []string) error {
	a.spacesEnsure()
	if i < 0 || i >= len(a.wall.spaces) {
		return fmt.Errorf("no space %d", i)
	}
	gone := make(map[string]bool, len(keys))
	for _, k := range keys {
		gone[k] = true
	}
	sp := a.wall.spaces[i]
	kept := make([]spaceMember, 0, len(sp.Members))
	for _, m := range sp.Members {
		if !gone[m.Key] {
			kept = append(kept, m)
		}
	}
	sp.Members = kept
	a.wall.spaces[i] = sp
	return saveSpaces(a.profileDir, a.wall.spaces)
}

// spaceActivate narrows the strip to space i, or widens it to every tab for
// i < 0 (or an index that is gone). It changes the view and nothing else. If
// the conversation in front is not a member, the strip would be narrowed away
// from the page the person is on, so it steps to the first member instead and
// returns that switch.
func (a *app) spaceActivate(i int) tea.Cmd {
	a.spacesEnsure()
	if i < 0 || i >= len(a.wall.spaces) {
		a.wall.active = -1
		return nil
	}
	a.wall.active = i
	sp := a.wall.spaces[i]
	if len(sp.Members) == 0 || spaceHolds(sp, a.frontTabKey()) {
		return nil
	}
	tabs := spaceTabs(sp, a.tabList())
	return a.tabGo(tabs[0])
}

// spaceActive is the space the strip is narrowed to. It reads only memory and
// is safe from a frame; before the first load no space is active, whatever
// the zero value of the index says.
func (a *app) spaceActive() (space, bool) {
	if !a.wall.loaded || a.wall.active < 0 || a.wall.active >= len(a.wall.spaces) {
		return space{}, false
	}
	return a.wall.spaces[a.wall.active], true
}

// spaceNames is every space's name in order. Frame-safe: memory only.
func (a *app) spaceNames() []string {
	if !a.wall.loaded {
		return nil
	}
	names := make([]string, len(a.wall.spaces))
	for i, sp := range a.wall.spaces {
		names[i] = sp.Name
	}
	return names
}

// spaceDelete forgets space i. Its conversations are untouched; only the name
// for the set goes. The active index follows the space it pointed at, and is
// cleared if that was the one deleted.
func (a *app) spaceDelete(i int) error {
	a.spacesEnsure()
	if i < 0 || i >= len(a.wall.spaces) {
		return fmt.Errorf("no space %d", i)
	}
	a.wall.spaces = append(a.wall.spaces[:i], a.wall.spaces[i+1:]...)
	switch {
	case a.wall.active == i:
		a.wall.active = -1
	case a.wall.active > i:
		a.wall.active--
	}
	return saveSpaces(a.profileDir, a.wall.spaces)
}

// spaceStripTabs is what the strip draws given the tabs it would draw with no
// space. With a space active it is that space's members, plus the tab in front
// when it is not one of them: THE TAB YOU ARE ON NEVER VANISHES, because a
// strip that does not show where you are cannot show you the way back. With no
// space active the tabs come back unchanged. Frame-safe: memory only.
func (a *app) spaceStripTabs(tabs []chatTab) []chatTab {
	sp, ok := a.spaceActive()
	if !ok {
		return tabs
	}
	out := spaceTabs(sp, tabs)
	for _, tab := range tabs {
		if tab.here && !spaceHolds(sp, tab.key) {
			out = append(out, tab)
			break
		}
	}
	return out
}
