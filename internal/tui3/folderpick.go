package tui3

// THE FOLDER PICKER: /folder, and the ONE component for choosing a directory
// anywhere in this product.
//
// It is the palette gesture again (palette.go) — a filter box in the input
// line's place, a short list under it, ↑↓ to move, enter to take, esc to leave
// everything exactly as it was — with two properties that are the whole design:
//
//   - IT OPENS FROM MEMORY. Nothing here walks the filesystem when the list
//     opens. The rows are laid down in layers that were already known: the
//     directories this conversation has touched, every project home knows
//     about, and the repositories under `~` from an index built in the
//     background and refreshed off the interaction path. Ranking a few hundred
//     candidates a person has actually been in beats crawling a disk, and it
//     beats it on the axis that matters — the first frame after `/folder` is a
//     list and never a spinner.
//   - TYPING FILTERS; TYPING A PATH BROWSES. Free words narrow the candidates
//     with the same scorer the `@` completion uses ([pathScore]). Input that
//     LOOKS like a path — `/`, `~/`, `./`, `../` — morphs this same surface
//     into columns: the parent beside where you are, ←/→ to walk, ↑/↓ to move,
//     tab to complete, enter to take the row you are on. Directories only, and
//     the dot-directories are pruned by the very [skipDirs] law the `@` walk
//     keeps.
//
// THE THIRD COLUMN IS WHAT THE MACHINE ALREADY KNOWS about the directory under
// the cursor — `repository · main · clean`, `folder · 214 files`, `AGENTS.md` —
// dim, one line per fact, and the emptiness law throughout: a fact nobody has
// established draws nothing rather than a blank or a zero. That is the moment
// this picker exists for: you are not picking a string, you are picking a place
// the program has already understood. The facts are ASKED FOR on the keystroke
// that moves the cursor and DRAWN from a cache (folderplace.go), because a
// keystroke may not wait for git — homeband_repo.go states that law and this
// list obeys it rather than restating it.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// folderRows is how many rows this list takes at once. It is the model
// picker's twelve for the model picker's reason: it is a screenful a person
// reads down without scrolling, and it is the same block of the frame whichever
// list happens to be open.
const folderRows = 12

// folderLayer is WHICH SOURCE a candidate came from, and the order of the
// constants is the order the rows are laid down in — best first.
//
// The layers are a ladder and not a sort key with the sources mixed together:
// a directory this conversation has been reading all afternoon outranks a
// project on the far side of the disk however often either was picked, because
// the question `/folder` answers is "the one I am thinking of", and what a
// person is thinking of is nearly always what they were just doing.
type folderLayer uint8

const (
	// folderReferred are the directories this conversation is already about.
	// It is EMPTY IN THIS BUILD and it is named here rather than left out
	// because the whole point of the layer ladder is that a source is added by
	// filling one hook: [app.referredPlaces] is that hook, and lane P2 —
	// which is what puts `Places` on the conversation's meta — fills it.
	folderReferred folderLayer = iota
	// folderTouched are the roots this conversation's own tool calls named,
	// most recent first. It is read off the entries this surface has already
	// drawn (folderplace.go's [app.touchedRoots]) and never re-derived.
	folderTouched
	// folderProject is every project home knows about — the alt+w source
	// ([app.composerDestinations]), which already leads with where this window
	// stands.
	folderProject
	// folderIndexed are the repositories under `~`, from the background index.
	folderIndexed
)

// folderCand is one directory on offer.
type folderCand struct {
	// path is absolute and cleaned, and is what enter hands over.
	path string
	// show is the path as a person reads it — `~` for the home directory —
	// and is BOTH what is drawn and what the filter scores against, because a
	// person typing "code/ag" is typing what they can see.
	show  string
	layer folderLayer
	// rank is the position this candidate held in its own source's order, kept
	// so that two candidates nobody has ever picked still come out in the order
	// their source meant.
	rank int
	// freq is the frecency of this path — recency × frequency of prior picks
	// (folderplace.go's [folderFrecency]) — and zero for a directory nobody has
	// chosen from this list yet.
	freq float64
}

// folderPick is the picker's whole state. The zero value is closed.
type folderPick struct {
	open bool

	// all is the layered candidate set as it stood when the list opened, and
	// lower the same `show` strings folded once: filtering is per keystroke over
	// every row, and folding a few hundred paths on each of them is the one
	// allocation this path cannot afford to repeat.
	all   []folderCand
	lower []string
	// score is per-candidate scratch, indexed as all is, reused across
	// keystrokes.
	score []int
	// hits are indexes into all, in rank order — the rows actually on offer.
	hits   []int
	cursor int
	top    int

	// cols is the browse state, and is live only while [folderPick.browsing].
	cols     folderCols
	browsing bool

	// facts is what the machine knows about a directory, keyed by absolute
	// path, as the answers came back (folderplace.go). A path with no entry has
	// not been asked about yet and draws nothing, which is the emptiness law
	// and not a blank.
	facts map[string][]string

	// tilde is this machine's home directory, for the `~` spelling. It is a
	// snapshot like everything else on this struct: a modal list cannot outlive
	// the answer to "where is home".
	tilde string

	filter editor
}

// folderCols is the browse state: one readdir per level, and nothing deep.
//
// THE COLUMNS ARE PARENT, HERE, AND WHAT IS KNOWN. Finder's third column is the
// highlighted row's children, and this one is what the machine knows about the
// highlighted row instead — because a list of names a person has not asked for
// is inventory, and the design's own ruling is consequences and never inventory.
// Walking into a directory is one `→` away and shows those children as the
// middle column, which is the same information one keypress later and in the
// place a person was already looking.
type folderCols struct {
	// dir is the directory the middle column lists, absolute and cleaned.
	dir string
	// here are dir's own subdirectories, by name, in readdir order with the dot
	// directories and [skipDirs] pruned.
	here []string
	// up are the PARENT's subdirectories, by name, drawn dim beside them so a
	// person can see where they are standing.
	up []string
	// cursor indexes here, and top is the first row drawn.
	cursor int
	top    int
	// upAt is which of the parent's rows is the directory we are standing in,
	// or -1 when the parent could not be read.
	upAt int
}

// folderPathish reports whether what has been typed is a PATH rather than a
// query — which is the one thing that decides whether this surface is a list or
// a set of columns.
//
// The four leads are the four ways a person spells "I know where it is": the
// root, home, here, and up. A bare word is never a path, even when a directory
// of that name exists: `agentfield` is what somebody types when they want the
// list to find it for them, and turning it into a browse of `./agentfield`
// would be the surface deciding they meant something more specific than they
// said.
func folderPathish(query string) bool {
	query = strings.TrimSpace(query)
	switch {
	case query == "":
		return false
	case query == "~", query == ".", query == "..":
		return true
	case strings.HasPrefix(query, "/"), strings.HasPrefix(query, "~/"),
		strings.HasPrefix(query, "./"), strings.HasPrefix(query, "../"):
		return true
	}
	return false
}

// start opens the picker over a layered candidate set. It never touches the
// disk: everything here was resolved before the list opened.
func (f *folderPick) start(candidates []folderCand, tilde string) {
	*f = folderPick{open: true, all: candidates, tilde: tilde, facts: map[string][]string{}}
	f.lower = make([]string, len(candidates))
	for i, cand := range candidates {
		f.lower[i] = strings.ToLower(cand.show)
	}
	f.score = make([]int, len(candidates))
	f.rank()
}

// close puts the picker away and forgets the filter, the columns and the facts.
// The next /folder opens on the whole list, which is the only thing a person
// can predict; a picker that remembered last week's query would open onto a
// list with no explanation.
func (f *folderPick) close() { *f = folderPick{} }

// rank re-filters the candidates against the filter box, or — when what is
// typed is a path — leaves the list alone, because the columns are what is
// being drawn and the list is not.
//
// The scorer is [pathScore], the `@` completion's own, so a person who has
// learned that "tui3" finds internal/tui3 in one list finds ~/code/aforge-v2 in
// this one by the same rule. Within a score the LAYER decides, then the layer's
// own order — which is what keeps a directory this conversation touched above a
// project three levels away that happens to score the same.
func (f *folderPick) rank() {
	f.browsing = folderPathish(f.filter.String())
	if f.browsing {
		return
	}
	needle := strings.ToLower(strings.TrimSpace(f.filter.String()))
	f.hits = f.hits[:0]
	for i := range f.all {
		// AN EMPTY BOX SCORES NOTHING, AND THAT IS THE PRODUCT. [pathScore]
		// answers a bare query with the path's own LENGTH — which is the right
		// tiebreak for a completion list ranking files under one directory, and
		// exactly wrong here: it would sort a person's projects by how deep in
		// the disk they happen to sit, and bury the layers and the frecency
		// under an accident of spelling. The empty-query view is the whole
		// point of this list, so every row scores the same and [folderPick.less]
		// alone decides it.
		score := 0
		if needle != "" {
			hit, ok := pathScore(f.lower[i], needle)
			if !ok {
				continue
			}
			score = hit
		}
		f.score[i] = score
		f.hits = append(f.hits, i)
	}
	sort.SliceStable(f.hits, func(a, b int) bool {
		ai, bi := f.hits[a], f.hits[b]
		if f.score[ai] != f.score[bi] {
			return f.score[ai] < f.score[bi]
		}
		return f.less(ai, bi)
	})
	// A changed query is a changed list, and a cursor left at row nine of the
	// old one points at nothing anybody chose.
	f.cursor, f.top = 0, 0
	f.follow(folderRows)
}

// less is the order INSIDE a score: the layer first, then frecency, then the
// order the layer's own source handed the candidate over in.
//
// FRECENCY IS RECENCY × FREQUENCY and it is computed where the picks are kept
// (folderplace.go's [folderFrecency]) — the zoxide model, and the reason a week
// of use turns `/folder` `enter` into the answer rather than the beginning of a
// search.
func (f *folderPick) less(a, b int) bool {
	if f.all[a].layer != f.all[b].layer {
		return f.all[a].layer < f.all[b].layer
	}
	if f.all[a].freq != f.all[b].freq {
		return f.all[a].freq > f.all[b].freq
	}
	return f.all[a].rank < f.all[b].rank
}

// move walks whichever of the two surfaces is up, clamping at both ends rather
// than wrapping: a list that wraps makes "hold ↓ until it stops" an infinite
// gesture.
func (f *folderPick) move(delta int) {
	if f.browsing {
		f.cols.cursor = moveCursor(f.cols.cursor, delta, len(f.cols.here))
		f.cols.top = listTop(f.cols.cursor, f.cols.top, len(f.cols.here), folderRows)
		return
	}
	f.cursor = moveCursor(f.cursor, delta, len(f.hits))
	f.follow(folderRows)
}

func (f *folderPick) follow(height int) {
	f.top = listTop(f.cursor, f.top, len(f.hits), height)
}

// here is the directory the cursor is on — the one enter would take — and false
// where there is none. It is what every decision on this list reads before it
// acts.
func (f *folderPick) here() (string, bool) {
	if !f.open {
		return "", false
	}
	if f.browsing {
		if f.cols.cursor >= 0 && f.cols.cursor < len(f.cols.here) {
			return filepath.Join(f.cols.dir, f.cols.here[f.cols.cursor]), true
		}
		// A DIRECTORY WITH NO SUBDIRECTORIES IS STILL A CHOICE. Somebody who has
		// walked into a leaf has arrived; refusing enter there would make the
		// deepest folder on a machine the one folder this picker cannot pick.
		if f.cols.dir != "" {
			return f.cols.dir, true
		}
		return "", false
	}
	if f.cursor < 0 || f.cursor >= len(f.hits) {
		return "", false
	}
	return f.all[f.hits[f.cursor]].path, true
}

// ── the columns ─────────────────────────────────────────────────────────────

// folderKids is one directory's subdirectories, by name, in readdir order —
// ONE readdir and nothing deep. Dot-directories and [skipDirs] are pruned by
// the very rule the `@` walk keeps, so the two surfaces cannot disagree about
// what a person is allowed to see.
//
// A directory that cannot be read answers nothing at all, which is the honest
// answer: a permission is not an error a browser should stop on.
func folderKids(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() || skipDirs[name] || strings.HasPrefix(name, ".") {
			continue
		}
		out = append(out, name)
	}
	return out
}

// browseAt points the columns at one directory, reading that level and its
// parent and no more. keep is the child to put the cursor on — the directory
// somebody just walked out of — and is empty when arriving from anywhere else.
func (f *folderPick) browseAt(dir, keep string) {
	dir = filepath.Clean(dir)
	f.cols = folderCols{dir: dir, here: folderKids(dir), upAt: -1}
	if parent := filepath.Dir(dir); parent != dir {
		f.cols.up = folderKids(parent)
		for at, name := range f.cols.up {
			if name == filepath.Base(dir) {
				f.cols.upAt = at
				break
			}
		}
	}
	for at, name := range f.cols.here {
		if keep != "" && name == keep {
			f.cols.cursor = at
			break
		}
	}
	f.cols.top = listTop(f.cols.cursor, 0, len(f.cols.here), folderRows)
}

// browseSync keeps the columns pointed at whatever the typed path names. It is
// called after every edit while a path is being typed, and it reads a level
// only when the level has CHANGED — which is what keeps a person holding
// backspace from doing one readdir per keystroke.
//
// The directory a path names is the path itself when it ends in a separator,
// and its parent otherwise: `~/code/af` is somebody part way through a name in
// `~/code`, and listing `~/code` is what lets the next keystroke narrow it.
func (f *folderPick) browseSync(resolve func(string) string) {
	typed := strings.TrimSpace(f.filter.String())
	dir, leaf := typed, ""
	if !strings.HasSuffix(typed, "/") {
		dir, leaf = pathHead(typed)
	}
	want := filepath.Clean(resolve(dir))
	if want != f.cols.dir {
		f.browseAt(want, "")
	}
	// AND THE CURSOR FOLLOWS THE HALF-TYPED NAME. A person typing `~/code/ag`
	// means the row that starts with those letters, and a column whose cursor
	// stayed at the top while they typed would be a column that ignores them.
	if leaf == "" {
		return
	}
	lower := strings.ToLower(leaf)
	for at, name := range f.cols.here {
		if strings.HasPrefix(strings.ToLower(name), lower) {
			f.cols.cursor = at
			f.cols.top = listTop(at, f.cols.top, len(f.cols.here), folderRows)
			return
		}
	}
}

// pathHead splits a typed path into the directory part and the half-typed name
// after the last separator. A path with no separator at all is all name, under
// the directory the caller resolves "" to.
func pathHead(typed string) (string, string) {
	cut := strings.LastIndex(typed, "/")
	if cut < 0 {
		return typed, ""
	}
	head := typed[:cut]
	if head == "" {
		head = "/"
	}
	return head, typed[cut+1:]
}

// descend is `→`: walk into the directory under the cursor. It answers false
// when there is nowhere to go, so the key can fall through to the filter box's
// own right.
func (f *folderPick) descend() bool {
	if !f.browsing || f.cols.cursor < 0 || f.cols.cursor >= len(f.cols.here) {
		return false
	}
	f.browseAt(filepath.Join(f.cols.dir, f.cols.here[f.cols.cursor]), "")
	f.writeBack()
	return true
}

// ascend is `←`: walk out to the parent, with the cursor left on the directory
// just left. It answers false at the root, where there is nothing above.
func (f *folderPick) ascend() bool {
	if !f.browsing || f.cols.dir == "" {
		return false
	}
	parent := filepath.Dir(f.cols.dir)
	if parent == f.cols.dir {
		return false
	}
	f.browseAt(parent, filepath.Base(f.cols.dir))
	f.writeBack()
	return true
}

// writeBack puts the directory the columns are now on back into the filter box,
// so what is typed and what is shown are never two different claims about where
// a person is. It keeps the `~` spelling, because that is what they typed.
func (f *folderPick) writeBack() {
	f.filter.setText(shortPath(f.cols.dir, f.tilde, 0) + "/")
	f.browsing = true
}

// complete is `tab`: the highlighted row's name is written into the box, whole,
// with a separator after it — which is a `→` a person can see the result of
// before they commit to it.
func (f *folderPick) complete() bool {
	if !f.browsing || f.cols.cursor < 0 || f.cols.cursor >= len(f.cols.here) {
		return false
	}
	f.filter.setText(shortPath(filepath.Join(f.cols.dir, f.cols.here[f.cols.cursor]), f.tilde, 0) + "/")
	return true
}

// ── what the frame draws ────────────────────────────────────────────────────

// height is how many rows this overlay wants, not counting the filter box —
// the box sits in the input line's place and costs the frame nothing.
func (f *folderPick) height(width int) int {
	switch {
	case !f.open:
		return 0
	case f.browsing:
		rows := max(len(f.cols.here), len(f.factsFor(f.hereForFacts())))
		return min(max(rows, 1), folderRows)
	case len(f.hits) == 0:
		// A filter that matches nothing has to say so where the list was.
		return 1
	}
	return overlayWindow(width, f.top, len(f.hits), folderRows, func(at int) string {
		return f.note(at, width)
	})
}

// hereForFacts is the path the third column is about, and "" when there is
// none.
func (f *folderPick) hereForFacts() string {
	path, ok := f.here()
	if !ok {
		return ""
	}
	return path
}

// factsFor is what the machine knows about one directory, as the last answer
// left it. A path nobody has asked about yet answers nothing — the emptiness
// law, and never a placeholder line saying so.
func (f *folderPick) factsFor(path string) []string {
	if path == "" || f.facts == nil {
		return nil
	}
	return f.facts[path]
}

// note is one LIST row's dim tail. It carries the facts on the row under the
// cursor and nothing anywhere else: one line about the place a person has
// stopped on is an answer, and the same line under every row is a wall.
//
// The facts are joined on this surface's own separator rather than stacked,
// because in list mode the tail is a row's tail — the grammar every other list
// here speaks. Stacked, one per line, is what the COLUMNS draw.
func (f *folderPick) note(at, width int) string {
	if at != f.cursor || at < 0 || at >= len(f.hits) {
		return ""
	}
	lines := f.factsFor(f.all[f.hits[at]].path)
	if len(lines) == 0 {
		return ""
	}
	return fit(strings.Join(lines, " · "), width/2)
}

// rows draws exactly n rows: the candidate list, or the columns.
func (f *folderPick) rows(width, n int, pal palette, hover int) []string {
	if n <= 0 {
		return nil
	}
	if f.browsing {
		return f.columnRows(width, n, pal)
	}
	if len(f.hits) == 0 {
		return []string{pal.dim("  " + folderNoMatchWord)}
	}
	f.follow(overlayItems(n, width))
	fill := newOverlayFill(width, n, pal, hover)
	for at := f.top; at < len(f.hits) && fill.room(); at++ {
		if !fill.add(at, f.all[f.hits[at]].show, f.note(at, width), at == f.cursor, false) {
			break
		}
	}
	lines, _ := fill.done()
	return lines
}

// folderNoMatchWord is what a filter that matched nothing says where the list
// was. It names the way out, because the way out of this one is not another
// keystroke of the same kind — it is typing a path.
const folderNoMatchWord = "no folder matches · type a path to browse"

// columnRows draws the browse: the parent dim on the left, where you are in the
// middle, and what is known on the right. No borders and no rules between them
// — the columns are told apart by the gaps and by the ink, which is what every
// other block on this surface does.
func (f *folderPick) columnRows(width, n int, pal palette) []string {
	up, here, facts := folderColumns(width)
	lines := f.factsFor(f.hereForFacts())
	f.cols.top = listTop(f.cols.cursor, f.cols.top, len(f.cols.here), n)
	out := make([]string, 0, n)
	for row := 0; row < n; row++ {
		line := ""
		if up > 0 {
			line += folderCell(f.upText(row, up, pal), up) + " "
		}
		line += folderCell(f.hereText(row, here, pal), here)
		if facts > 0 {
			fact := ""
			if row < len(lines) {
				fact = pal.dim(fit(lines[row], facts))
			}
			line += " " + fact
		}
		out = append(out, strings.TrimRight(line, " "))
	}
	return out
}

// upText is one row of the parent column: dim, and one tier brighter on the
// directory we are standing inside, which is the whole reason the column is
// drawn at all.
func (f *folderPick) upText(row, room int, pal palette) string {
	at := row + f.cols.top - f.cols.cursor + f.cols.upAt
	if f.cols.upAt < 0 || at < 0 || at >= len(f.cols.up) {
		return ""
	}
	name := fit(f.cols.up[at], room)
	if at == f.cols.upAt {
		return pal.muted(name)
	}
	return pal.dim(name)
}

// hereText is one row of the middle column: the lead, then the name. The row
// under the cursor is ink and bold behind the lead every other list on this
// surface uses ([overlayLead]), so it stays the brightest thing on a monochrome
// terminal too.
func (f *folderPick) hereText(row, room int, pal palette) string {
	at := row + f.cols.top
	if at < 0 || at >= len(f.cols.here) {
		if row == 0 && len(f.cols.here) == 0 {
			// A DIRECTORY WITH NOTHING UNDER IT SAYS SO WHERE ITS ROWS WOULD BE.
			// This is not the emptiness law being broken: the law is about facts
			// nobody established, and "there is nothing below here" is a fact this
			// readdir did establish, on the one screen where a person is about to
			// press `→` and wonder why it did nothing.
			return pal.dim(fit(folderLeafWord, room))
		}
		return ""
	}
	name := fit(f.cols.here[at], room-2)
	if at == f.cols.cursor {
		return pal.accent("› ") + pal.bold(pal.ink(name))
	}
	return "  " + pal.dim(name)
}

// folderLeafWord is what the middle column says about a directory with no
// subdirectories under it.
const folderLeafWord = "nothing below here"

// folderCell pads one painted cell to its column's width, measuring through the
// escape sequences rather than around them.
func folderCell(painted string, room int) string {
	if gap := room - ansi.StringWidth(painted); gap > 0 {
		return painted + strings.Repeat(" ", gap)
	}
	return painted
}

// folderColumns is how the frame's width is divided between the three columns.
//
// THE MIDDLE COLUMN IS THE ONE THAT SURVIVES. It is where a person's hands are;
// the parent is context and the facts are a bonus, so on a narrowing frame the
// parent goes first and the facts go second, and the names never get cut to
// make room for either. A column that would leave the names unreadable is a
// column not worth drawing.
func folderColumns(width int) (up, here, facts int) {
	room := width - 2
	if room < 1 {
		return 0, max(width, 1), 0
	}
	if room >= folderWideAt {
		up = min(room/5, 24)
	}
	if room >= folderFactsAt {
		facts = min(room/3, 34)
	}
	gaps := 0
	if up > 0 {
		gaps++
	}
	if facts > 0 {
		gaps++
	}
	here = room - up - facts - gaps
	if here < folderNameFloor && facts > 0 {
		here, facts, gaps = here+facts+1, 0, gaps-1
	}
	if here < folderNameFloor && up > 0 {
		here, up = here+up+1, 0
	}
	return up, max(here, 1), facts
}

// The three widths the division above turns on: where a parent column starts
// being affordable, where the facts do, and the floor under a directory name.
// Twenty-four cells is a name a person recognizes; below that the middle column
// takes everything rather than showing three cut columns.
const (
	folderWideAt    = 82
	folderFactsAt   = 50
	folderNameFloor = 24
)

// navigate is every key this list owns that is not a decision: the walk, the
// scroll and the filter box. enter and esc are left to the door that opened it
// ([app.folderKey]), for the reason [picker.navigate] leaves them there.
//
// It answers whether the COLUMNS ALREADY AGREE WITH THE BOX, so the door knows
// whether to re-read a level after the key ([app.folderKey]). `→` and `←` move
// both at once and answer true; everything else — a letter, a backspace, a tab
// — moves the box and lets the columns follow it.
//
// THE WALK KEYS ARE READ BEFORE THE SHARED MAP because in the columns ←/→ are
// how a person moves between levels, and there they mean that from anywhere in
// the box: a browse whose ← only worked with the caret at the start would be a
// gesture that stops working the moment somebody types.
func (f *folderPick) navigate(msg tea.KeyPressMsg) bool {
	if msg.String() == "tab" {
		// TAB MEANS NOTHING IN A BOX YOU TYPE INTO, which is what makes it free
		// for the fold in the model picker and free for the completion here.
		// Left to the shared key map it would insert a literal tab into a filter
		// — a character no path has and no list matches.
		f.complete()
		return false
	}
	if f.browsing {
		switch msg.String() {
		case "right":
			if f.descend() {
				return true
			}
		case "left":
			if f.ascend() {
				return true
			}
		}
	}
	listNavigate(msg, &f.filter, f.move, f.rank, folderRows)
	return false
}

// folderHintFields is the placeholder in the empty filter box, as the ranked
// fields it is made of — the keys go from the right, whole, on a frame too
// narrow for all of them (rowfit.go), so the box never draws a key spelled
// `es…`.
var folderHintFields = []rowField{
	rowSay("filter"), rowSay("↑↓"), rowSay("type a path to browse"),
	rowSay("enter"), rowSay("esc"),
}

// folderBrowseHintFields is the same line while a path is being typed, where
// the keys mean something else entirely and saying so is the only honest
// legend.
var folderBrowseHintFields = []rowField{
	rowSay("←→ walk"), rowSay("↑↓"), rowSay("tab completes"),
	rowSay("enter takes this folder"), rowSay("esc"),
}

// folderHintAt is whichever of the two lines belongs to what is on screen, in
// the cells the box actually has.
func (f *folderPick) folderHintAt(room int) string {
	if f.browsing {
		return rowTail(folderBrowseHintFields, room)
	}
	return rowTail(folderHintFields, room)
}
