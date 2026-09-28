package tui3

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/fuzzy"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/skills"
	store "github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// THE SKILL PICKER: /skill with a space after it, and the shelf is a list you
// can put skills in front of this conversation from.
//
// Until this existed the only way to reach a skill was to hope the model chose
// it by itself. The session side of that already existed — the four doors on
// internal/session's skillattach.go — and this is the surface over them.
//
//	/skill            the whole shelf, the attached ones at the top
//	/skill <query>    the picker: filter, ↑↓, enter toggles one on or off
//	<the request>     enter sends it with the attached skills in front of it
//
// It follows the harness picker (harnesspick.go) everywhere the two errands
// agree — synced off the draft rather than modal, the shared fuzzy scoring,
// the overlay grammar — with ONE difference: the choice is a SET. Enter on a
// row toggles that skill on or off and LEAVES THE LIST OPEN, so three skills
// are three keystrokes and not three commands. Escape closes, and the
// attachment stays: it is held by the session, not by the draft, so it is
// still on three messages later, and the tray's chip above the box is where a
// person can see that it is.

// skillPickListLimit is how far into the shelf the picker reads. It is the
// catalog's own scan bound (internal/session's skillcatalog.go) rather than a
// second number, because the shelf the picker shows and the shelf the model
// is offered are the same shelf.
const skillPickListLimit = 50

// skillChipCap is how much of a name the tray's chip may spend
// (harnesspick.go's [harnessChipCap], for its reason).
const skillChipCap = 24

// skillFolderWord is the folder row's own word: one extra row at the bottom of
// the list whenever the query looks like a path, offering to attach the skill
// that lives in that folder.
const skillFolderWord = "attach the skill in"

// skillFolderMissing is the one plain line a folder with no SKILL.md is
// refused by.
func skillFolderMissing(path string) string {
	return "no SKILL.md in " + path
}

// skillFromShelf, skillFromProject and skillFromUser are the dim words saying
// where a row came from — the shelf the distiller keeps, or a folder in place
// under this project or the home directory.
const (
	skillFromShelf   = "shelf"
	skillFromProject = skills.ScopeProject
	skillFromUser    = skills.ScopeUser

	// skillNoShelfWarning is the dim tail every folder row carries when this
	// conversation has no skill shelf at all. The picker reads the DISK, so it
	// lists a person's skills whether or not this session can use one, and
	// attaching is inert with no store to resolve a name against (skillturn.go's
	// turnSkills). A list of rows that do nothing when chosen, with nothing
	// saying why, is the control present and failing rather than absent, which
	// is the thing this codebase does not do.
	//
	// IT NO LONGER BLAMES MEMORY. It said "memory is off" once, and it said so
	// on every machine: it looked for the shelf through the live door's memory
	// seam, which never read skills, so the check failed with memory on too.
	// The shelf is now asked of the session itself ([skillShelf]), and memory
	// off has a shelf of its own, so the one case left is a conversation whose
	// door built none, or a far engine too old to be asked.
	skillNoShelfWarning = "this conversation has no skill shelf, so this cannot be attached"
)

// skillHomeDir is where discovery looks beside the workspace: the same login
// home the launch's import pass reads (internal/home's Login, which follows
// CODEAF_HOME), so the list a person picks from and the shelf a choice
// resolves against are read out of the same folders. It is a door rather than
// a call so the suite can point it at a temporary home.
var skillHomeDir = func() string {
	dir, err := home.Login()
	if err != nil {
		return ""
	}
	return dir
}

// skillDiscover is the picker's one road to the disk scan. It is a variable so
// a test can hold a scan in flight and prove the list, and every gesture
// after it, carries on without it; nothing else ever replaces it.
var skillDiscover = skills.Discover

// skillPickRow is one skill as this list draws it: the name, the one-line
// description, the dim word for where it came from, a Warning the folder
// carries, and whether the skill is attached to this conversation.
type skillPickRow struct {
	name    string
	desc    string
	from    string
	warning string
	on      bool
}

// note is the row's dim tail: what the skill is for, where it came from, and
// what is wrong with it when something is — dim rather than hidden, because
// "why is my skill not working" deserves an answer on the row itself.
func (r skillPickRow) note() string {
	parts := make([]string, 0, 3)
	if desc := strings.TrimSpace(r.desc); desc != "" {
		parts = append(parts, desc)
	}
	if r.from != "" {
		parts = append(parts, r.from)
	}
	if warning := strings.TrimSpace(r.warning); warning != "" {
		parts = append(parts, warning)
	}
	return strings.Join(parts, " · ")
}

// skillPick is the picker's whole state. The zero value is closed.
type skillPick struct {
	open bool
	// rows are the shelf as it was when the list opened, attached first. It
	// is resolved on the keystroke and not held from boot, for the harness
	// picker's reason: another window may have installed a skill a minute ago.
	rows []skillPickRow
	// fields are each row's own words for the matcher (harnesspick.go).
	fields [][]string
	// score is per-row scratch, reused across keystrokes.
	score []int
	// hits are indexes into rows, in rank order.
	hits []int
	// folder is the query when it looks like a path, and the folder row is
	// drawn after the hits whenever it is set.
	folder string
	// query is what has been typed after the command, folded once.
	query string

	cursor int
	top    int
	// owner maps each screen line back to the row that drew it.
	owner []int
}

func (p *skillPick) close() { *p = skillPick{} }

// count is how many rows the list has, the folder row included when the query
// is a path: it is a row like any other as far as the cursor is concerned.
func (p *skillPick) count() int {
	n := len(p.hits)
	if p.folder != "" {
		n++
	}
	return n
}

// onFolder reports whether the cursor is on the folder row.
func (p *skillPick) onFolder() bool {
	return p.folder != "" && p.cursor == len(p.hits)
}

// at resolves a row index to the skill it draws, and false for the folder row.
func (p *skillPick) at(index int) (skillPickRow, bool) {
	if index < 0 || index >= len(p.hits) {
		return skillPickRow{}, false
	}
	return p.rows[p.hits[index]], true
}

// choice is the skill under the cursor, and false on the folder row or when
// the filter matched nothing.
func (p *skillPick) choice() (skillPickRow, bool) { return p.at(p.cursor) }

// start opens the list over rows already in attachment-then-scope order.
func (p *skillPick) start(rows []skillPickRow, query string) {
	*p = skillPick{open: true, rows: rows}
	p.fields = make([][]string, len(rows))
	for i, row := range rows {
		p.fields[i] = []string{row.name, row.desc}
	}
	p.score = make([]int, len(rows))
	p.rank(query)
}

// rank narrows the list to the query on the shared fuzzy scoring
// (harnesspick.go's [harnessPick.rank] states the law), and decides whether
// the query looks like a path — it starts with "/", "./" or "~" — in which
// case the folder row is offered at the bottom whatever the filter matched.
func (p *skillPick) rank(query string) {
	p.query = query
	p.folder = ""
	if skillPathLike(query) {
		p.folder = strings.TrimSpace(query)
	}
	terms := strings.Fields(strings.ToLower(query))
	ft := fuzzyTerms(terms)
	p.hits = p.hits[:0]
	for i := range p.rows {
		if len(terms) == 0 {
			p.hits = append(p.hits, i)
			continue
		}
		total, matched := fuzzy.ScoreFields(p.fields[i], ft)
		if !matched {
			continue
		}
		p.score[i] = total
		p.hits = append(p.hits, i)
	}
	if len(terms) > 0 {
		sort.SliceStable(p.hits, func(a, b int) bool { return p.score[p.hits[a]] > p.score[p.hits[b]] })
	}
	// A changed query is a changed list (palette.go says it first).
	p.cursor, p.top = 0, 0
}

func (p *skillPick) move(delta int) {
	p.cursor = moveCursor(p.cursor, delta, p.count())
	p.follow(harnessPickRows)
}

func (p *skillPick) follow(height int) {
	p.top = listTop(p.cursor, p.top, p.count(), height)
}

// label is a row's own half: the on mark and the name. An attached row carries
// the vocabulary's filled cell and an unattached one its empty circle, so the
// mark is the state and not a second spelling of the name. The folder row
// carries an arrow instead, because it is a door and not a thing.
func (p *skillPick) label(index int, pal palette) string {
	row, ok := p.at(index)
	if !ok {
		if pal.ascii || pal.linear {
			return skillFolderWord + " " + fit(p.folder, 32) + " ->"
		}
		return skillFolderWord + " " + fit(p.folder, 32) + " →"
	}
	if row.on {
		return pal.glyph(tokens.GDoneCell) + " " + row.name
	}
	return pal.dim(pal.glyph(tokens.GQueued)) + " " + row.name
}

// note is the row's dim tail, and the folder's path for the folder row.
func (p *skillPick) note(index int) string {
	row, ok := p.at(index)
	if !ok {
		return p.folder
	}
	return row.note()
}

// height is how many lines the overlay wants.
func (p *skillPick) height(width int) int {
	if !p.open {
		return 0
	}
	return overlayWindow(width, p.top, p.count(), harnessPickRows, p.note)
}

func (p *skillPick) draw(width, n int, pal palette, hover int) []string {
	if n <= 0 || !p.open {
		return nil
	}
	p.follow(overlayItems(n, width))
	fill := newOverlayFill(width, n, pal, hover)
	for at := p.top; at < p.count() && fill.room(); at++ {
		if !fill.add(at, p.label(at, pal), p.note(at), at == p.cursor, false) {
			break
		}
	}
	lines, owner := fill.done()
	p.owner = owner
	return lines
}

// skillPathLike reports whether a query is a path a folder might be named by.
func skillPathLike(query string) bool {
	return strings.HasPrefix(query, "/") || strings.HasPrefix(query, "./") || strings.HasPrefix(query, "~")
}

// ── the app's side: opening it off the draft ────────────────────────────────

// skillPickWords are the commands that open this list — the /skill row's own
// name and aliases (commands.go), on harnesspick.go's terms.
func skillPickWords() []string {
	words := []string{"skill"}
	for _, c := range commands {
		if c.name == "skill" {
			words = append(words, c.alias...)
		}
	}
	return words
}

// skillPickQuery reads the draft as this list reads it: the text after
// "/skill ", on [harnessPickQuery]'s terms — the space is the door.
func skillPickQuery(line string) (string, bool) {
	if !strings.HasPrefix(line, "/") || strings.Contains(line, "\n") {
		return "", false
	}
	word, rest, spaced := strings.Cut(line[1:], " ")
	if !spaced {
		return "", false
	}
	word = strings.ToLower(word)
	for _, other := range skillPickWords() {
		if word == other {
			return rest, true
		}
	}
	return "", false
}

// syncSkillPick opens, narrows or closes the picker from what is in the draft,
// and reports whether it is up. It is called from [app.syncLists] beside the
// harness picker, and the two can never be open together: a draft is one line.
//
// THE LIST OPENS ON THE KEYSTROKE AND THE SHELF ARRIVES AFTER IT. The folders
// on disk and the attachment the facts already carry are drawn at once; the
// session's shelf is a door, asked off the update loop ([app.readSkillShelf]),
// and the list is redrawn from its answer with the cursor where it was.
func (a *app) syncSkillPick() (bool, tea.Cmd) {
	query, ok := skillPickQuery(a.input.String())
	if !ok {
		a.skillPick.close()
		return false, nil
	}
	if !a.skillPick.open {
		a.skillPick.start(a.skillPickList(), query)
		return true, tea.Batch(a.readSkillShelf(), a.readSkillDisk())
	}
	if query != a.skillPick.query {
		a.skillPick.rank(query)
	}
	return true, nil
}

// skillShelfReading is the session's shelf as the last read of it answered:
// the active skills, and whether there was a shelf to read at all.
type skillShelfReading struct {
	rows     []shelfSkillRow
	readable bool
}

// readSkillShelf asks the session for its shelf off the update loop and
// redraws the open list from the answer. It is asked IN the door line, not
// beside it: the list opened because somebody typed, and a toggle pressed a
// moment later must reach the session after this read, not race it.
func (a *app) readSkillShelf() tea.Cmd {
	shelf, ok := a.agent.(skillShelf)
	if !ok {
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		facts, err := shelf.SkillFacts(store.FactActive, skillPickListLimit)
		reading := &skillShelfReading{readable: err == nil}
		for _, fact := range facts {
			reading.rows = append(reading.rows, shelfSkillRow{name: fact.SkillName(), desc: strings.TrimSpace(fact.Body)})
		}
		return func(here bool) tea.Cmd {
			if !here {
				return nil
			}
			a.skillShelfSeen = reading
			if a.skillPick.open {
				a.restartSkillPick()
				a.touch()
			}
			return nil
		}
	})
}

// readSkillDisk scans the skill folders under the workspace and the home
// directory off the update loop and redraws the open list from the answer.
//
// IT IS ASKED BESIDE THE LINE, NOT IN IT ([app.besideLine]). A scan is a read
// nobody pressed for in the line's sense: nothing a person does next depends on
// it having finished, and it is the one read here that can be slow for reasons
// nobody chose — a skill folder linked to the root of a large repository, a
// home on a network disk. In the ordered line it would stand in front of every
// attach and every message after it. So the list opens at once on what is
// already known (the shelf, and the last scan this app saw), and the folders
// arrive when the scan answers.
func (a *app) readSkillDisk() tea.Cmd {
	workspace, homeDir := a.workspace, skillHomeDir()
	return a.besideLine(func() func(bool) tea.Cmd {
		found, _ := skillDiscover(skills.Options{ProjectDir: workspace, HomeDir: homeDir})
		return func(here bool) tea.Cmd {
			if !here {
				return nil
			}
			a.skillDiskSeen = found
			if a.skillPick.open {
				a.restartSkillPick()
				a.touch()
			}
			return nil
		}
	})
}

// restartSkillPick rebuilds the open list from what is known now, keeping the
// query and, where it still points at a row, the cursor.
func (a *app) restartSkillPick() {
	cursor, query := a.skillPick.cursor, a.skillPick.query
	a.skillPick.start(a.skillPickList(), query)
	if cursor < a.skillPick.count() {
		a.skillPick.cursor = cursor
	}
}

// skillPickList resolves the shelf into rows: the attached ones first, in
// attachment order, then the rest by scope — project before user — and by
// name. Two sources are merged and deduplicated by name: the active shelf the
// store keeps, and the folders internal/skills discovers in place.
func (a *app) skillPickList() []skillPickRow {
	attached := a.attachedSkillNames()
	// WHETHER A CHOICE ON THIS LIST CAN DO ANYTHING. The shelf is the store and
	// the rows below come off the disk, so the two can disagree, and they do in
	// a conversation whose door built no shelf.
	shelf, shelfReadable := a.shelfSkillFacts()
	rows := make([]skillPickRow, 0, 16)
	seen := make(map[string]bool, 16)
	// THE ATTACHED ONES FIRST, in the order the session holds them. Attachment
	// order is the conflict rule the workers read, so it is the order the list
	// opens on too.
	for _, name := range attached {
		rows = append(rows, skillPickRow{name: name, on: true})
		seen[strings.ToLower(name)] = true
	}
	var rest []skillPickRow
	for _, skill := range shelf {
		name := skill.name
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		rest = append(rest, skillPickRow{name: name, desc: skill.desc, from: skillFromShelf, on: attachedHas(attached, name)})
	}
	for _, skill := range a.skillDiskSeen {
		if skill.Shadowed || skill.Name == "" || seen[strings.ToLower(skill.Name)] {
			continue
		}
		seen[strings.ToLower(skill.Name)] = true
		from := skillFromUser
		if skill.Scope == skills.ScopeProject {
			from = skillFromProject
		}
		warning := skill.Warning
		if !shelfReadable {
			// BOTH, AND THE FOLDER'S FIRST. The two warnings answer different
			// questions: one is what is wrong with this skill, the other is
			// what is wrong with the machine, and a row that dropped the first
			// to make room for the second would hide a fault that outlives the
			// setting.
			warning = strings.TrimSpace(strings.Join([]string{warning, skillNoShelfWarning}, " · "))
			warning = strings.TrimPrefix(warning, "· ")
		}
		rest = append(rest, skillPickRow{name: skill.Name, desc: skill.Description, from: from, warning: warning, on: attachedHas(attached, nameOf(skill))})
	}
	// PROJECT BEFORE USER, and the name as the tie-break: scope is a claim
	// about where a skill lives and the name is the only order left inside a
	// scope.
	sort.SliceStable(rest, func(i, j int) bool {
		if rest[i].from != rest[j].from {
			return rest[i].from == skillFromProject
		}
		return rest[i].name < rest[j].name
	})
	return append(rows, rest...)
}

// attachedSkillNames is what the session holds, or nothing on a session
// without the door — the seam is asserted rather than added to [Agent], on
// [harnessRunner]'s terms.
func (a *app) attachedSkillNames() []string {
	door, ok := a.skillDoor()
	if !ok {
		return nil
	}
	return door.AttachedSkills()
}

// attachedHas is the case-insensitive membership test the shelf names are
// compared by (internal/session's containsSkillName is the same law).
func attachedHas(names []string, name string) bool {
	for _, held := range names {
		if strings.EqualFold(held, name) {
			return true
		}
	}
	return false
}

// shelfSkillRow is one active shelf fact as the list reads it.
type shelfSkillRow struct {
	name string
	desc string
}

// shelfSkillFacts is the active shelf as the SESSION last answered it, newest
// first as the store returns it, and whether there is a shelf at all. A read
// that failed is no shelf: the rows the disk gives are still listed, and each
// says it cannot be attached. A shelf not yet answered is not a missing one —
// no row is marked until the session has said so.
func (a *app) shelfSkillFacts() ([]shelfSkillRow, bool) {
	if _, ok := a.agent.(skillShelf); !ok {
		return nil, false
	}
	if a.skillShelfSeen == nil {
		return nil, true
	}
	return a.skillShelfSeen.rows, a.skillShelfSeen.readable
}

// skillShelf is the session's own reading of its shelf, asserted on the agent
// the surface holds (internal/session's Agent.SkillFacts, and the same door
// across the wire on a hosted conversation).
//
// IT IS ASKED OF THE AGENT AND NOT OF A STORE THE SURFACE HOLDS, because the
// shelf is the session's: the store it reads is chosen by the door — the
// memory database, or with memory off a shelf built from the skill folders
// alone — and a surface that read some store of its own would be a second
// answer to "which skills can this conversation use". It was one, once: it
// read the memory seam, which the live door wraps without any reading of
// skills, so every row said memory was off on every machine. An error is no
// shelf, and so is an agent without the door.
type skillShelf interface {
	SkillFacts(status string, limit int) ([]store.Fact, error)
}

// nameOf keeps the discovered Skill's own name in one place.
func nameOf(skill skills.Skill) string { return skill.Name }

// ── the keys ────────────────────────────────────────────────────────────────

// skillPickKey routes one keypress while the picker is up, on the harness
// picker's terms: only the keys that move and commit, everything else falls
// through to the editor.
func (a *app) skillPickKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "up", "ctrl+p":
		a.skillPick.move(-1)
		a.touch()
		return nil, true
	case "down", "ctrl+n":
		a.skillPick.move(1)
		a.touch()
		return nil, true
	case "pgup":
		a.skillPick.move(-harnessPickRows)
		a.touch()
		return nil, true
	case "pgdown":
		a.skillPick.move(harnessPickRows)
		a.touch()
		return nil, true
	case "esc":
		a.skillPick.close()
		a.touch()
		return nil, true
	case "enter":
		return a.skillToggled(), true
	}
	return nil, false
}

// skillToggled is enter on the list: the skill under the cursor goes on or
// comes off, and THE LIST STAYS OPEN — the choice is a set, and a list that
// closed after one toggle would make three skills three commands again. The
// cursor stays where it is, because the row a person just answered is the one
// they can answer again to undo.
func (a *app) skillToggled() tea.Cmd {
	if !a.skillPick.open {
		return nil
	}
	door, ok := a.skillDoor()
	if !ok {
		// A capability that cannot work is absent rather than broken
		// (harnesspick.go's law).
		a.skillPick.close()
		a.note(skillUnavailableWord)
		a.touch()
		return nil
	}
	if a.skillPick.onFolder() {
		return a.skillFolderAttached(door)
	}
	row, ok := a.skillPick.choice()
	if !ok {
		// Nothing matched. The line is still a line and enter is still
		// enter: it falls through to the editor and sends what was typed.
		return nil
	}
	// THE MARK MOVES ON THE KEYSTROKE AND THE DOOR IS ASKED OFF THE LOOP. The
	// row turns over at once because this window knows what it just asked for;
	// the session's answer then re-marks every row from the set it holds.
	name, on := row.name, row.on
	a.markSkillRow(name, !on)
	a.touch()
	return a.offLoop(func() func(bool) tea.Cmd {
		if on {
			door.DetachSkill(name)
		} else {
			door.AttachSkills(name)
		}
		return a.skillsMoved
	})
}

// skillsMoved is the fold every attachment door hands back: the rows are
// re-marked from the set the session now holds.
func (a *app) skillsMoved(here bool) tea.Cmd {
	if !here {
		return nil
	}
	a.remarkSkillRows()
	a.touch()
	return nil
}

// markSkillRow turns one row's mark over without asking anybody.
func (a *app) markSkillRow(name string, on bool) {
	for i := range a.skillPick.rows {
		if strings.EqualFold(a.skillPick.rows[i].name, name) {
			a.skillPick.rows[i].on = on
		}
	}
}

// remarkSkillRows rewrites the on marks against the session after a toggle,
// without reordering the list under the cursor.
func (a *app) remarkSkillRows() {
	held := a.attachedSkillNames()
	for i := range a.skillPick.rows {
		a.skillPick.rows[i].on = attachedHas(held, a.skillPick.rows[i].name)
	}
}

// skillFolderAttached is enter on the folder row: the skill in the folder the
// query named goes on, named the way discovery named it when the picker's last
// scan found that folder and read from its own SKILL.md otherwise, and a folder
// with no SKILL.md — or one whose SKILL.md is not an ordinary file — is refused
// in one plain line. Nothing is copied anywhere — attachment is by name, and
// the folder stays where it is.
//
// NO DISK SCAN RUNS HERE. The name is read on the update loop because the
// attach is a person's gesture and belongs in the ordered line exactly where
// they pressed it: a name resolved off the loop would put this attach behind
// whatever they pressed next, and attachment order is the conflict rule the
// workers read. What makes that safe is the size of the read. The folder's
// scan already ran when the picker opened ([app.readSkillDisk]), beside the
// line; what is left is at most one SKILL.md, read through the skills
// package's opener, which never opens a pipe or a device and never reads past
// the skill cap.
func (a *app) skillFolderAttached(door skillAttacher) tea.Cmd {
	folder := expandSkillPath(a.skillPick.folder)
	if folder == "" {
		return nil
	}
	name := ""
	for _, skill := range a.skillDiskSeen {
		if samePath(skill.Dir, folder) {
			name = skill.Name
			break
		}
	}
	if name == "" {
		read, err := readSkillName(folder)
		if err != nil {
			a.note(err.Error())
			a.touch()
			return nil
		}
		name = read
	}
	// The list is rebuilt rather than patched once the session answers, so the
	// skill just attached is on it at the top where the attached ones open.
	return a.offLoop(func() func(bool) tea.Cmd {
		door.AttachSkills(name)
		return func(here bool) tea.Cmd {
			if here && a.skillPick.open {
				a.skillPick.start(a.skillPickList(), a.skillPick.query)
				a.touch()
			}
			return nil
		}
	})
}

// expandSkillPath turns a typed path into one the file system answers to:
// "~" and "~/" become the home directory, and everything else is left as
// written for filepath.Abs to settle.
func expandSkillPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(path, "~"), "/"))
	}
	return path
}

// samePath compares two folder paths without demanding that either be
// absolute, because a typed relative path and a discovered absolute one name
// the same folder.
func samePath(one, other string) bool {
	if one == "" || other == "" {
		return false
	}
	a, aerr := filepath.Abs(one)
	b, berr := filepath.Abs(other)
	if aerr != nil || berr != nil {
		return one == other
	}
	return a == b
}

// readSkillName reads one folder's own SKILL.md frontmatter for its name, the
// way internal/skills reads it. A folder without a SKILL.md, or with one that
// names no skill, is an error whose text is the one plain line the row
// refuses by.
func readSkillName(folder string) (string, error) {
	data, err := skills.ReadRegularHead(filepath.Join(folder, "SKILL.md"), skills.MaxSkillFileBytes)
	if err != nil {
		if errors.Is(err, skills.ErrNotRegular) {
			return "", errors.New("SKILL.md is not a regular file in " + folder)
		}
		return "", errors.New(skillFolderMissing(folder))
	}
	name := skillFrontmatterName(string(data))
	if name == "" {
		return "", errors.New("the SKILL.md in " + folder + " names no skill")
	}
	return name, nil
}

// skillFrontmatterName pulls the `name` field out of a SKILL.md's frontmatter
// block, leniently: the folder was pointed at by a person, and a name with an
// odd edge is still the name they meant.
func skillFrontmatterName(text string) string {
	normalized := strings.ReplaceAll(text, "\r\n", "\n")
	if !strings.HasPrefix(normalized, "---\n") {
		return ""
	}
	rest := normalized[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return ""
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		if name, ok := strings.CutPrefix(line, "name:"); ok {
			return strings.Trim(strings.TrimSpace(name), `"'`)
		}
	}
	return ""
}

// skillPickPress resolves a click on one of the picker's rows, on the harness
// picker's terms: it is not modal, and a press anywhere else falls through.
func (a *app) skillPickPress(y int) (tea.Cmd, bool) {
	if !a.skillPick.open {
		return nil, false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeOverlay {
		return nil, false
	}
	at := -1
	if mark.index >= 0 && mark.index < len(a.skillPick.owner) {
		at = a.skillPick.owner[mark.index]
	}
	if at < 0 {
		return nil, false
	}
	a.skillPick.cursor = at
	return a.skillToggled(), true
}

// ── the chip ────────────────────────────────────────────────────────────────

// skillAttacher is the narrow slice of the session this surface needs — the
// four doors on internal/session's skillattach.go — asserted on the agent
// rather than added to [Agent], on [harnessRunner]'s terms.
type skillAttacher interface {
	AttachSkills(names ...string) []string
	DetachSkill(name string) bool
	AttachedSkills() []string
	ClearAttachedSkills() int
}

// skillDoor is the attachment doors of the session under this surface, and
// false when it has none. A hosted conversation's agent ALWAYS has the
// methods (internal/remote's skills.go), so the assertion alone cannot tell a
// far engine with the doors from one built before them; such an agent also
// says which it is, and one that says no is treated as having no doors at
// all — the picker's own sentence rather than choices that go nowhere.
func (a *app) skillDoor() (skillAttacher, bool) {
	door, ok := a.agent.(skillAttacher)
	if !ok {
		return nil, false
	}
	if far, asks := a.agent.(interface{ SkillsSupported() bool }); asks && !far.SkillsSupported() {
		return nil, false
	}
	return door, true
}

// skillTrayCells is the skill chip's cells on the row above the box: the name
// of the one skill attached, or "N skills" for more than one, with the ✕ that
// takes every one back off. Nothing at all when none is attached.
//
// THE ATTACHMENT STAYS AFTER THE MESSAGE IS SENT. A skill is attached to the
// conversation and not to one request, which is why the chip is how a person
// knows it is still on three messages later; the ✕ — one gesture — is how it
// comes off, and the manual page says so.
func (a *app) skillTrayCells() []string {
	if _, ok := a.skillDoor(); !ok {
		return nil
	}
	held := a.attachedSkillNames()
	if len(held) == 0 {
		return nil
	}
	drop := glyphChipDrop
	if a.pal.ascii || a.pal.linear {
		drop = "x"
	}
	word := fit(held[0], skillChipCap)
	if len(held) > 1 {
		word = strconv.Itoa(len(held)) + " skills"
	}
	return []string{skillChipMark(a.pal) + " " + word + " " + drop}
}

// skillChipMark is the tray's glyph for attached skills — the vocabulary's
// filled cell, the same mark the picker's rows carry for a skill that is on.
func skillChipMark(pal palette) string {
	return pal.glyph(tokens.GDoneCell)
}

// dropSkillChip takes every attached skill back off, off the update loop, and
// answers nil when there was nothing on to take off. It is the ✕ on the chip.
func (a *app) dropSkillChip() tea.Cmd {
	door, ok := a.skillDoor()
	if !ok || len(a.attachedSkillNames()) == 0 {
		return nil
	}
	return a.offLoop(func() func(bool) tea.Cmd {
		door.ClearAttachedSkills()
		return a.skillsMoved
	})
}

// skillUnavailableWord is what the surface says when the session under it
// cannot carry attached skills.
const skillUnavailableWord = "this conversation cannot carry attached skills"
