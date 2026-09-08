package tui3

// THE FOLDER BROWSER: /folder, and the ONE component for choosing a directory
// anywhere in this product.
//
// It is the palette gesture again (palette.go) — a filter box in the input
// line's place, a list under it, ↑↓ to move, esc to leave everything exactly as
// it was — with three properties that are the whole design:
//
//   - IT OPENS FROM MEMORY. Nothing here walks the filesystem when the list
//     opens. The rows are laid down in layers that were already known: the
//     directories this conversation has been referred to, every project home
//     knows about, and the repositories under `~` from an index built in the
//     background and refreshed off the interaction path. Ranking a few hundred
//     candidates a person has actually been in beats crawling a disk, and it
//     beats it on the axis that matters — the first frame after `/folder` is a
//     list and never a spinner.
//   - TYPING FILTERS; ANYTHING ELSE BROWSES. Free words narrow the candidates
//     with the same scorer the `@` completion uses ([pathScore]). Input that
//     LOOKS like a path — `/`, `~/`, `./`, `../` — morphs this same surface
//     into COLUMNS, and so does opening a row of the list: `→`, or a click.
//     THAT IS WHY A SEARCH NEVER HAS TO BE RETYPED — the row a filter found is
//     the place the columns open on.
//   - THE COLUMNS ARE SUCCESSIVE AND THEY ARE FINDER'S. The parent on the left,
//     the directory you are standing in beside it, and THE CHILDREN OF THE ROW
//     UNDER THE CURSOR on the right — so a walk down a tree is a walk to the
//     right, and the next level is on screen before you have asked for it. The
//     path above them is a breadcrumb, and every segment of it is a place you
//     can click back to.
//
// WHAT THE MACHINE KNOWS about the highlighted directory — `repository · main ·
// clean`, `folder · 214 files`, `AGENTS.md` — is the dim tail of the action row
// at the foot, one line, beside the one thing this surface exists to do: ADD
// THIS FOLDER. Navigating and choosing are two different acts and they have two
// different gestures, which is the whole reason that row is drawn.
//
// EVERY DIRECTORY READ IS ASKED FOR AS A COMMAND AND DRAWN FROM A CACHE. A
// keystroke may not wait for a disk any more than it may wait for git
// (homeband_repo.go states that law and this list obeys it): the level the
// cursor moved onto is read off the loop, stamped with the opening that asked
// for it, and an answer that arrives after the picker closed or reopened is
// dropped rather than filed against whatever is under the cursor now.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// folderRows is how many rows of DIRECTORIES this browser wants at once. The
// owner asked for a spacious sheet rather than the model picker's twelve, and
// eighteen is what a full-height terminal gives back after the status line, the
// box and one row of conversation — [app.overlayHeight] clamps it down on
// anything smaller, and the browser follows whatever it is actually given
// ([folderPick.page]).
const folderRows = 18

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
	// folderReferred are the directories this conversation is already about —
	// the session's own `Places`, read through the one door that has them
	// (folderplace.go's [app.referredPlaces]).
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
	// path is absolute and cleaned, and is what the add action hands over.
	path string
	// show is the path as a person reads it — `~` for the home directory and
	// EVERY OTHER SEGMENT LEFT ALONE ([tildePath]) — and is BOTH what is drawn
	// and what the filter scores against, because a person typing "code/ag" is
	// typing what they can see.
	//
	// IT USED TO BE [shortPath], WHICH IS THE LEGEND SPELLING and spends the
	// ancestors down to initials: `~/code/aforge-v2/internal` drew as
	// `~/c/a/internal`, so the rows were unreadable AND unsearchable — a filter
	// on "code" matched nothing, because the letters it was scoring against were
	// not there. The same call in [folderPick.writeBack] put `/t/b/t/alpha/`
	// into the box after a walk, which is not a path any resolve can find.
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

// folderRead is ONE readdir's answer: the subdirectory names, or the reason
// there are none.
//
// THE ERROR IS CARRIED AND NEVER FLATTENED TO AN EMPTY LIST. A directory nobody
// may read and a directory with nothing in it are two different facts about a
// person's disk, and a browser that drew them the same way would answer "is
// anything in there?" with a confident lie. [folderCols] draws them apart.
type folderRead struct {
	names []string
	err   error
	// done marks an answer that has arrived, so an empty list that IS the answer
	// is told apart from a level nobody has read yet.
	done bool
}

// folderPick is the browser's whole state. The zero value is closed.
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
	// kids is the THIRD COLUMN's cache — the children of directories the cursor
	// has rested on — keyed by absolute path and filled off the loop. It is the
	// same bargain the facts make: the keystroke asks, the paint draws whatever
	// has come back, and a level still in flight draws nothing rather than
	// stalling the frame.
	kids map[string]folderRead
	// asking is which directories are being read right now, so a cursor held
	// down a column forks one readdir per level and not one per keypress.
	asking map[string]bool
	// gen stamps every read this OPENING asked for. An answer carrying another
	// opening's stamp is dropped: the picker that asked for it is gone, and
	// filing it against the one that is up would be a column of somebody else's
	// directory (folderplace.go's [folderKidsMsg]).
	gen int

	// hidden reveals the dot-directories and the names the `@` walk prunes.
	// It is off by default and it is a person's own act — alt+h, or a name
	// beginning with a dot typed into the box, which is what somebody reaching
	// for `.config` is already doing.
	hidden bool

	// tilde is this machine's home directory, for the `~` spelling. It is a
	// snapshot like everything else on this struct: a modal list cannot outlive
	// the answer to "where is home".
	tilde string

	// geom is WHERE THE LAST PAINT PUT THINGS, so a click resolves against what
	// is on the screen rather than against a second layout that could disagree
	// with it (attach.go's [app.chipTrayTarget] states this law for the tray).
	geom folderGeom

	filter editor
}

// folderCols is the browse state: one readdir per level, and nothing deep.
//
// THE COLUMNS ARE PARENT, HERE, AND THE CHILDREN OF THE ROW UNDER THE CURSOR.
// That is Finder's arrangement and it is what the owner asked for: a directory
// tree is walked by moving right, and the level you are about to walk into is
// already drawn when you get there. The parent stays on the left so that where
// you are standing is visible without reading the path.
type folderCols struct {
	// dir is the directory the middle column lists, absolute and cleaned.
	dir string
	// here are dir's own subdirectories, by name, in readdir order with the dot
	// directories and [skipDirs] pruned unless [folderPick.hidden] is on.
	here folderRead
	// up are the PARENT's subdirectories, drawn dim beside them so a person can
	// see where they are standing.
	up folderRead
	// cursor indexes here.names, and top is the first row drawn.
	cursor int
	top    int
	// upAt is which of the parent's rows is the directory we are standing in,
	// or -1 when the parent could not be read.
	upAt int
	// keep is the child the cursor is FOR — the directory somebody just walked
	// out of, or the row a toggle should not lose — held until the level it
	// belongs to has actually been read.
	//
	// IT IS A NAME AND NOT AN INDEX, and it outlives the readdir on purpose:
	// walking out of a folder whose parent is not in the cache used to land the
	// cursor at the top of that parent, because the level arrived a frame after
	// the only code that knew which row to look for had finished.
	keep string
}

// folderCrumb is one segment of the path above the columns: the name drawn, the
// directory it stands for, and the cells it was drawn in.
type folderCrumb struct {
	name string
	path string
	span hudSpan
}

// folderGeom is what the last paint laid down. Everything the pointer asks is
// answered from here, because what lights and what a press acts on must be the
// same thing (hover.go's law).
type folderGeom struct {
	// head is how many rows sit above the body — the breadcrumb, so one while
	// browsing and none in the list.
	head int
	// body is how many body rows were drawn.
	body int
	// action is the action row's index within the overlay, or -1 where the
	// frame was too short to draw one.
	action int
	// crumbs are the breadcrumb's segments, left to right.
	crumbs []folderCrumb
	// up, here and kids are the three columns' cells.
	up, here, kids hudSpan
	// owner maps a LIST row back to the candidate drawn on it. At phone width a
	// row is two lines, so the two are not the same number ([overlayFill.done]).
	owner []int
}

// folderPathish reports whether what has been typed is a PATH rather than a
// query — which is one of the two things that decide whether this surface is a
// list or a set of columns, the other being whether somebody opened a row.
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
	gen := f.gen + 1
	*f = folderPick{
		open:   true,
		all:    candidates,
		tilde:  tilde,
		facts:  map[string][]string{},
		kids:   map[string]folderRead{},
		asking: map[string]bool{},
		gen:    gen,
	}
	f.geom.action = -1
	f.lower = make([]string, len(candidates))
	for i, cand := range candidates {
		f.lower[i] = strings.ToLower(cand.show)
	}
	f.score = make([]int, len(candidates))
	f.rank()
}

// close puts the browser away and forgets the filter, the columns and the
// caches. The next /folder opens on the whole list, which is the only thing a
// person can predict; a picker that remembered last week's query would open onto
// a list with no explanation.
//
// THE GENERATION SURVIVES, because it is the one field whose whole job is to
// outlive the state it stamped: a readdir still in flight answers with the
// number it was asked under, and the next opening's number has to differ from it.
func (f *folderPick) close() { *f = folderPick{gen: f.gen + 1} }

// page is how many rows the cursor moves through for one page key, and the
// window the cursor is followed within. It is what the LAST PAINT actually
// drew — a browser given eight rows on a short terminal pages by eight — and
// falls back on its own want before the first paint.
func (f *folderPick) page() int {
	if f.geom.body > 0 {
		return f.geom.body
	}
	return folderRows
}

// rank re-filters the candidates against the filter box, or — when what is
// typed is a path — leaves the list alone, because the columns are what is
// being drawn and the list is not.
//
// The scorer is [pathScore], the `@` completion's own, so a person who has
// learned that "tui3" finds internal/tui3 in one list finds ~/code/aforge-v2 in
// this one by the same rule. Within a score the LAYER decides, then the layer's
// own order — which is what keeps a directory this conversation touched above a
// project three levels away that happens to score the same.
//
// THE SCORING IS DELIBERATELY LEFT WHERE IT IS. The search lane is building the
// ranking this list will end up using in its own files; this function is the
// ONE place a call to it replaces, and nothing else in this file reads a score
// (folderplace.go's report names the seam).
func (f *folderPick) rank() {
	// WHICH SURFACE IS UP FOLLOWS THE BOX AND NOTHING ELSE, opening a row
	// included: [folderPick.openAt] writes that row's path into the box, and an
	// absolute path is always pathish — so the mode is still ONE question with
	// one answer, and editing the path back down to a word puts the list back.
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
	f.follow(f.page())
}

// less is the order INSIDE a score: the layer first, then frecency, then the
// order the layer's own source handed the candidate over in.
//
// FRECENCY IS RECENCY × FREQUENCY and it is computed where the picks are kept
// (folderplace.go's [folderFrecency]) — the zoxide model, and the reason a week
// of use turns `/folder` and the add action into the answer rather than the
// beginning of a search.
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
		f.cols.cursor = moveCursor(f.cols.cursor, delta, len(f.cols.here.names))
		f.cols.top = listTop(f.cols.cursor, f.cols.top, len(f.cols.here.names), f.page())
		return
	}
	f.cursor = moveCursor(f.cursor, delta, len(f.hits))
	f.follow(f.page())
}

func (f *folderPick) follow(height int) {
	f.top = listTop(f.cursor, f.top, len(f.hits), height)
}

// here is the directory the cursor is on — the one the add action would take —
// and false where there is none. It is what every decision on this surface reads
// before it acts.
func (f *folderPick) here() (string, bool) {
	if !f.open {
		return "", false
	}
	if f.browsing {
		if f.cols.cursor >= 0 && f.cols.cursor < len(f.cols.here.names) {
			return filepath.Join(f.cols.dir, f.cols.here.names[f.cols.cursor]), true
		}
		// A DIRECTORY WITH NO SUBDIRECTORIES IS STILL A CHOICE. Somebody who has
		// walked into a leaf has arrived; refusing the add there would make the
		// deepest folder on a machine the one folder this browser cannot pick.
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
// what a person is allowed to see — unless `hidden` is on, and then EVERYTHING
// the readdir returned that is a directory is shown, `.git` and `node_modules`
// included. One word, one rule: a person who asked for the hidden folders asked
// for all of them.
//
// A DIRECTORY THAT CANNOT BE READ ANSWERS WITH THE REASON, never with an empty
// list. Permission denied is not "there is nothing in there", and a browser that
// said it was would be lying about somebody's own disk.
func folderKids(dir string, hidden bool) folderRead {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return folderRead{err: err, done: true}
	}
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() {
			continue
		}
		if !hidden && (skipDirs[name] || strings.HasPrefix(name, ".")) {
			continue
		}
		out = append(out, name)
	}
	return folderRead{names: out, done: true}
}

// browseAt points the columns at one directory. It reads NOTHING: the two
// levels come out of the cache, and whatever is missing is asked for by the
// caller's command ([app.askFolderKids]). keep is the child to put the cursor on
// — the directory somebody just walked out of — and is empty when arriving from
// anywhere else.
func (f *folderPick) browseAt(dir, keep string) {
	dir = filepath.Clean(dir)
	f.cols = folderCols{dir: dir, here: f.read(dir), upAt: -1, keep: keep}
	if parent := filepath.Dir(dir); parent != dir {
		f.cols.up = f.read(parent)
		f.markUp()
	}
	f.seatCursor()
}

// markUp finds the row of the parent column that is the directory we are
// standing in, and -1 where the parent has not been read or does not hold it.
func (f *folderPick) markUp() {
	f.cols.upAt = -1
	for at, name := range f.cols.up.names {
		if name == filepath.Base(f.cols.dir) {
			f.cols.upAt = at
			break
		}
	}
}

// seatCursor puts the cursor on the name the columns are waiting for, if that
// name has arrived, and follows it with the window either way.
func (f *folderPick) seatCursor() {
	if f.cols.keep != "" {
		for at, name := range f.cols.here.names {
			if name == f.cols.keep {
				f.cols.cursor, f.cols.keep = at, ""
				break
			}
		}
	}
	f.cols.cursor = moveCursor(f.cols.cursor, 0, len(f.cols.here.names))
	f.cols.top = listTop(f.cols.cursor, f.cols.top, len(f.cols.here.names), f.page())
}

// read is one level out of the cache, and the zero [folderRead] — not done, no
// error, no names — for a level nobody has read yet.
func (f *folderPick) read(dir string) folderRead {
	if f.kids == nil || dir == "" {
		return folderRead{}
	}
	return f.kids[dir]
}

// took files one readdir's answer and re-seats the columns if the answer was
// about a level they are drawing. It reports whether anything on screen changed.
func (f *folderPick) took(dir string, read folderRead) bool {
	if !f.open {
		return false
	}
	if f.kids == nil {
		f.kids = map[string]folderRead{}
	}
	f.kids[dir] = read
	delete(f.asking, dir)
	if !f.browsing {
		return true
	}
	// THE CURSOR SURVIVES A LEVEL ARRIVING UNDER IT. browseAt would reset it to
	// the top, and a person who had already pressed ↓ three times while the disk
	// was answering would watch the surface take those presses back.
	switch dir {
	case f.cols.dir:
		f.cols.here = read
		f.seatCursor()
	case filepath.Dir(f.cols.dir):
		f.cols.up = read
		f.markUp()
	}
	return true
}

// forget drops every cached level and every fact. It is what the hidden toggle
// runs through: the cache holds ANSWERS TO A DIFFERENT QUESTION once the rule
// about which names count has changed, and a column half-filled from before the
// toggle would be a directory that gained three folders and kept none of them.
func (f *folderPick) forget() {
	f.kids, f.asking = map[string]folderRead{}, map[string]bool{}
	f.cols.here, f.cols.up, f.cols.upAt = folderRead{}, folderRead{}, -1
	f.cols.top = 0
}

// browseSync keeps the columns pointed at whatever the typed path names. It is
// called after every edit while a path is being typed, and it re-seats a level
// only when the level has CHANGED — which is what keeps a person holding
// backspace from asking for one readdir per keystroke.
//
// The directory a path names is the path itself when it ends in a separator,
// and its parent otherwise: `~/code/af` is somebody part way through a name in
// `~/code`, and listing `~/code` is what lets the next keystroke narrow it.
func (f *folderPick) browseSync(resolve func(string) string) {
	typed := strings.TrimSpace(f.filter.String())
	if typed == "" {
		return
	}
	dir, leaf := typed, ""
	if !strings.HasSuffix(typed, "/") {
		dir, leaf = pathHead(typed)
	}
	// A NAME BEGINNING WITH A DOT IS SOMEBODY REACHING FOR A HIDDEN FOLDER, and
	// a browser that answered `.con` with nothing would be one that refused to
	// admit `.config` exists. It is the shell's own rule and it is why the
	// toggle is rarely needed.
	if strings.HasPrefix(leaf, ".") && !f.hidden {
		f.hidden = true
		f.forget()
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
	for at, name := range f.cols.here.names {
		if strings.HasPrefix(strings.ToLower(name), lower) {
			f.cols.cursor = at
			f.cols.top = listTop(at, f.cols.top, len(f.cols.here.names), f.page())
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

// descend is `→` in the columns: walk into the directory under the cursor. It
// answers false when there is nowhere to go, so the key can fall through to the
// filter box's own right.
func (f *folderPick) descend() bool {
	if !f.browsing || f.cols.cursor < 0 || f.cols.cursor >= len(f.cols.here.names) {
		return false
	}
	f.browseAt(filepath.Join(f.cols.dir, f.cols.here.names[f.cols.cursor]), "")
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

// openAt is HOW A SEARCH RESULT BECOMES A PLACE TO BROWSE, and it is the whole
// answer to "the path is on the screen and I have to type it again". A row of
// the list, a segment of the breadcrumb and a name in the third column all come
// through here: the columns move to that directory and the box is rewritten to
// match, so nothing is ever retyped.
func (f *folderPick) openAt(dir, keep string) {
	f.browsing = true
	f.browseAt(dir, keep)
	f.writeBack()
}

// writeBack puts the directory the columns are now on back into the filter box,
// so what is typed and what is shown are never two different claims about where
// a person is.
//
// IT IS [tildePath] AND IT HAS TO BE. What goes in the box is read back by
// [app.resolvePath] on the very next keystroke, so it must be a path that
// resolves: `~` is the one abbreviation that survives that, and every other
// segment is left exactly as it is. Written with the legend's spelling it put
// `/t/b/t/alpha/` in the box after a walk — which drew as a claim about where
// you were standing and named nowhere at all.
func (f *folderPick) writeBack() {
	f.filter.setText(tildePath(f.cols.dir, f.tilde) + "/")
	f.browsing = true
}

// complete is `tab`: the highlighted row's name is written into the box, whole,
// with a separator after it — which is a `→` a person can see the result of
// before they commit to it.
func (f *folderPick) complete() bool {
	if !f.browsing || f.cols.cursor < 0 || f.cols.cursor >= len(f.cols.here.names) {
		return false
	}
	f.filter.setText(tildePath(filepath.Join(f.cols.dir, f.cols.here.names[f.cols.cursor]), f.tilde) + "/")
	return true
}

// wanted is every directory this browser needs read and has not got: the level
// under the cursor, its parent, and the children of the highlighted row. It is
// what the door turns into commands ([app.askFolderKids]) and it is deliberately
// THREE PATHS AT MOST — a browser that read ahead down a tree would be the
// recursive scan this design refuses.
func (f *folderPick) wanted() []string {
	if !f.open || !f.browsing {
		return nil
	}
	var out []string
	want := func(dir string) {
		if dir == "" || f.asking[dir] {
			return
		}
		if read, ok := f.kids[dir]; ok && read.done {
			return
		}
		for _, already := range out {
			if already == dir {
				return
			}
		}
		out = append(out, dir)
	}
	want(f.cols.dir)
	if parent := filepath.Dir(f.cols.dir); parent != f.cols.dir {
		want(parent)
	}
	// The third column is the children of the row under the cursor, and it is
	// asked for LAST: the two levels a person can already see are worth more
	// than the one they are about to.
	if path, ok := f.here(); ok && path != f.cols.dir {
		want(path)
	}
	return out
}

// ── what the frame draws ────────────────────────────────────────────────────

// height is how many rows this overlay wants, not counting the filter box —
// the box sits in the input line's place and costs the frame nothing.
func (f *folderPick) height(width int) int {
	switch {
	case !f.open:
		return 0
	case f.browsing:
		// The breadcrumb, the tallest column, and the action row.
		rows := max(len(f.cols.here.names), len(f.read(f.hereForFacts()).names))
		return min(max(rows, 1), folderRows) + folderChromeRows
	case len(f.hits) == 0:
		// A filter that matches nothing has to say so where the list was, and
		// the way out is still on the action row under it.
		return 1 + 1
	}
	body := overlayWindow(width, f.top, len(f.hits), folderRows, func(at int) string {
		return f.note(at, width)
	})
	return body + 1
}

// folderChromeRows is what the browse draws beside the directories: the
// breadcrumb above and the action row below.
const folderChromeRows = 2

// hereForFacts is the path the action row is about, and "" when there is none.
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

// rows draws exactly n rows: the candidate list, or the columns — and under
// either of them the action row, which is the one thing on this surface that
// ADDS a folder rather than moving around one.
func (f *folderPick) rows(width, n int, pal palette, hover int) []string {
	if n <= 0 {
		return nil
	}
	f.geom = folderGeom{action: -1}
	// THE ACTION ROW IS THE FIRST ROW GIVEN UP AND THE LAST ROW DRAWN, because a
	// frame with one row to give must spend it on the directories: a sheet
	// showing only the way to add something, with no way to see what would be
	// added, is not a browser. Its own row number is known before it is painted,
	// so the pointer's band lands on it.
	wantAction := n > 1
	if wantAction {
		n--
	}
	var body []string
	switch {
	case f.browsing:
		body = f.columnRows(width, n, pal, hover)
	case len(f.hits) == 0:
		body = []string{pal.dim(folderPad + folderNoMatchWord)}
		f.geom.body = 1
	default:
		body = f.listRows(width, n, pal, hover)
	}
	if !wantAction {
		return body
	}
	f.geom.action = len(body)
	return append(body, f.actionRow(width, pal, hover))
}

// listRows is the candidate list, drawn through the fill every list on this
// surface draws through, with the row-to-candidate map kept for the pointer.
func (f *folderPick) listRows(width, n int, pal palette, hover int) []string {
	f.follow(overlayItems(n, width))
	fill := newOverlayFill(width, n, pal, hover)
	for at := f.top; at < len(f.hits) && fill.room(); at++ {
		if !fill.add(at, f.all[f.hits[at]].show, f.note(at, width), at == f.cursor, false) {
			break
		}
	}
	lines, owner := fill.done()
	f.geom.body, f.geom.owner = len(lines), owner
	return lines
}

// folderNoMatchWord is what a filter that matched nothing says where the list
// was. It names the way out, because the way out of this one is not another
// keystroke of the same kind — it is typing a path.
const folderNoMatchWord = "no folder matches · type a path to browse"

// columnRows draws the breadcrumb and the three successive columns: the parent
// dim on the left, where you are in the middle, and the children of the row
// under the cursor on the right. No borders and no rules between them — the
// columns are told apart by the gaps and by the ink, which is what every other
// block on this surface does.
func (f *folderPick) columnRows(width, n int, pal palette, hover int) []string {
	out := make([]string, 0, n)
	// THE BREADCRUMB IS THE FIRST ROW AND IS WORTH ONE ROW OF THE COLUMNS,
	// because it is the only thing on the sheet that says where you are in one
	// glance and the only thing whose every segment is a place to go back to.
	// It is dropped on a frame with two rows or fewer, where the directories
	// themselves are the last thing left worth drawing.
	if n > 2 {
		out = append(out, f.crumbRow(width, pal))
		f.geom.head, n = 1, n-1
	}
	up, here, kids := folderColumns(width)
	f.geom.up = hudSpan{from: folderPadCells, to: folderPadCells + up}
	f.geom.here = hudSpan{from: f.geom.up.to + folderGapFor(up), to: 0}
	f.geom.here.to = f.geom.here.from + here
	f.geom.kids = hudSpan{from: f.geom.here.to + folderGapFor(kids), to: 0}
	f.geom.kids.to = f.geom.kids.from + kids
	f.cols.top = listTop(f.cols.cursor, f.cols.top, len(f.cols.here.names), n)
	child := f.read(f.hereForFacts())
	for row := 0; row < n; row++ {
		line := folderPad
		if up > 0 {
			line += folderCell(f.upText(row, up, pal), up) + " "
		}
		line += folderCell(f.hereText(row, here, pal, hover == row+f.geom.head), here)
		if kids > 0 {
			line += " " + folderNameCell(child, row, kids, pal)
		}
		out = append(out, strings.TrimRight(line, " "))
	}
	f.geom.body = n
	return out
}

// folderPad is the inset every row of this sheet shares, so the breadcrumb, the
// columns and the action row stand in one left margin.
const folderPad = "  "

// folderPadCells is that inset, in cells.
const folderPadCells = len(folderPad)

// folderGapFor is the one cell between two drawn columns, and none where the
// column on the right was not drawn at all.
func folderGapFor(width int) int {
	if width > 0 {
		return 1
	}
	return 0
}

// crumbRow is the path above the columns, one segment per level, with the
// directory being listed in ink and its ancestors dim. Every segment's cells are
// recorded, because every segment is a place a click can go back to.
func (f *folderPick) crumbRow(width int, pal palette) string {
	trail := folderTrail(f.cols.dir, f.tilde)
	// THE TAIL IS WHAT SURVIVES A NARROW FRAME. `~ › code › … › tui3` keeps the
	// two facts a person needs — the root they are under and where they are
	// standing — and dropping from the left is what every breadcrumb on this
	// surface does (roomcrumbs.go).
	room := width - folderPadCells
	line, cells := folderPad, folderPadCells
	f.geom.crumbs = f.geom.crumbs[:0]
	from, used := len(trail), 0
	for from > 0 {
		used += ansi.StringWidth(trail[from-1].name) + folderCrumbGapCells
		if used > room {
			break
		}
		from--
	}
	if from >= len(trail) && len(trail) > 0 {
		from = len(trail) - 1
	}
	if from > 0 {
		line, cells = line+pal.dim(folderCrumbCut), cells+ansi.StringWidth(folderCrumbCut)
	}
	for at := from; at < len(trail); at++ {
		crumb := trail[at]
		if at > from {
			line, cells = line+pal.dim(folderCrumbGap), cells+folderCrumbGapCells
		}
		name := fit(crumb.name, room)
		crumb.span = hudSpan{from: cells, to: cells + ansi.StringWidth(name)}
		if at == len(trail)-1 {
			line += pal.bold(pal.ink(name))
		} else {
			line += pal.dim(name)
		}
		cells = crumb.span.to
		f.geom.crumbs = append(f.geom.crumbs, crumb)
	}
	return line
}

// The breadcrumb's punctuation. `›` is the surface's own step mark and the cut
// is what stands in for the levels a narrow frame had no room for.
const (
	folderCrumbGap = " › "
	folderCrumbCut = "… › "
	// Both are measured in CELLS and not in bytes: `›` and `…` are three bytes
	// each and one cell each, and a breadcrumb laid out with len() would record
	// spans two cells to the right of the names a person is clicking on.
	folderCrumbGapCells = 3
)

// folderTrail is the path as clickable segments, root first. The topmost is
// spelled `~` under a person's home directory and `/` above it, which is how
// they read it everywhere else on this surface.
func folderTrail(dir, tilde string) []folderCrumb {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil
	}
	var out []folderCrumb
	for {
		name := filepath.Base(dir)
		if tilde != "" && dir == tilde {
			name = "~"
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			name = dir
		}
		out = append(out, folderCrumb{name: name, path: dir})
		if parent == dir || (tilde != "" && dir == tilde) {
			break
		}
		dir = parent
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// upText is one row of the parent column: dim, and one tier brighter on the
// directory we are standing inside, which is the whole reason the column is
// drawn at all.
func (f *folderPick) upText(row, room int, pal palette) string {
	if f.cols.up.err != nil || f.cols.upAt < 0 {
		return ""
	}
	at := row + f.cols.top - f.cols.cursor + f.cols.upAt
	if at < 0 || at >= len(f.cols.up.names) {
		return ""
	}
	name := fit(f.cols.up.names[at], room)
	if at == f.cols.upAt {
		return pal.muted(name)
	}
	return pal.dim(name)
}

// hereText is one row of the middle column: the lead, then the name. The row
// under the cursor is ink and bold behind the lead every other list on this
// surface uses ([overlayLead]), so it stays the brightest thing on a monochrome
// terminal too; the row under the POINTER carries the same band every list on
// this surface gives it (hover.go's law).
func (f *folderPick) hereText(row, room int, pal palette, hovered bool) string {
	at := row + f.cols.top
	if at < 0 || at >= len(f.cols.here.names) {
		if row == 0 && len(f.cols.here.names) == 0 {
			// A LEVEL WITH NOTHING TO SHOW SAYS WHY, WHERE ITS ROWS WOULD BE.
			// This is not the emptiness law being broken: the law is about facts
			// nobody established, and "there is nothing below here", "you may not
			// read this" and "still reading" are three facts this browser DID
			// establish, on the one screen where a person is about to press `→`
			// and wonder why it did nothing.
			return pal.dim(fit(folderStateWord(f.cols.here), room))
		}
		return ""
	}
	name := fit(f.cols.here.names[at], room-2)
	if at == f.cols.cursor {
		return pal.accent("› ") + pal.bold(pal.ink(name))
	}
	if hovered {
		return "  " + pal.cursor(pal.dim(name), 0)
	}
	return "  " + pal.dim(name)
}

// folderNameCell is one row of the THIRD column — the children of the row under
// the cursor — dim throughout, because it is what is over there rather than what
// is being chosen.
func folderNameCell(read folderRead, row, room int, pal palette) string {
	if row < len(read.names) {
		return pal.dim(fit(read.names[row], room))
	}
	if row == 0 {
		return pal.dim(fit(folderStateWord(read), room))
	}
	return ""
}

// folderStateWord is what a column with no names in it says about itself, and
// there are three different things it can be saying.
func folderStateWord(read folderRead) string {
	switch {
	case !read.done:
		return ""
	case read.err != nil && os.IsPermission(read.err):
		return folderClosedWord
	case read.err != nil:
		return folderUnreadableWord
	}
	return folderLeafWord
}

// The three things a column with no rows can be saying. They are constants
// because the manual quotes each of them exactly as it is spelled here.
const (
	// folderLeafWord is a directory with no subdirectories under it.
	folderLeafWord = "nothing below here"
	// folderClosedWord is a directory this machine will not let this program
	// read. It is NEVER the word above: a folder somebody may not open is not a
	// folder with nothing in it.
	folderClosedWord = "you cannot read this folder"
	// folderUnreadableWord is every other way a readdir fails.
	folderUnreadableWord = "this folder could not be read"
)

// actionRow is the one row on this sheet that ADDS a folder, with what the
// machine knows about that folder dim at the right end of it.
//
// IT IS A ROW AND NOT A LEGEND because navigating and choosing are two different
// acts, and the owner asked for the second to be its own thing rather than a
// meaning `enter` happens to carry. A person who has never pressed `enter` here
// can read what it would do and click it.
func (f *folderPick) actionRow(width int, pal palette, hover int) string {
	room := width - folderPadCells - 1
	if room < 1 {
		return ""
	}
	path, ok := f.here()
	if !ok {
		return pal.dim(folderPad + fit(f.hint(), room))
	}
	shown := tildePath(path, f.tilde)
	// The facts ride the right end of the same row, and are dropped WHOLE rather
	// than cut: half a branch name is a branch nobody has.
	facts := strings.Join(f.factsFor(path), " · ")
	keep := room - ansi.StringWidth(folderAddWord)
	if facts != "" {
		if room := keep - ansi.StringWidth(facts) - folderFactsGap; room >= folderNameFloor {
			keep = room
		} else {
			facts = ""
		}
	}
	shown = fit(shown, max(keep, 1))
	painted := pal.dim(folderAddWord) + pal.ink(shown)
	if hover >= 0 && hover == f.geom.actionAt() {
		painted = pal.cursor(painted, 0)
	}
	cells := ansi.StringWidth(folderAddWord) + ansi.StringWidth(shown)
	line := folderPad + painted
	if facts != "" {
		line += strings.Repeat(" ", max(room-cells-ansi.StringWidth(facts), folderFactsGap)) + pal.dim(facts)
	}
	return line
}

// actionAt is where the action row was drawn on the LAST paint, which is what a
// hover index is compared against while the current one is still being built.
func (g folderGeom) actionAt() int { return g.action }

// folderAddWord leads the action row. It is the verb the owner asked for and it
// is spelled the same way in the manual.
const folderAddWord = "add this folder · "

// folderFactsGap is the least clear space between the action and the facts
// beside it.
const folderFactsGap = 2

// hint is what the action row says when there is nothing under the cursor to
// add — which is a filter matching nothing, and an empty machine.
func (f *folderPick) hint() string {
	if f.browsing {
		return folderBrowseHintWord
	}
	return folderListHintWord
}

// The two sentences the action row falls back on.
const (
	folderListHintWord   = "type a path to browse · → opens the folder under the cursor"
	folderBrowseHintWord = "←→ walk · alt+h shows hidden folders"
)

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
// the parent is context and the children are a look ahead, so on a narrowing
// frame the parent goes first and the children go second, and the names never
// get cut to make room for either. A column that would leave the names
// unreadable is a column not worth drawing.
func folderColumns(width int) (up, here, kids int) {
	room := width - folderPadCells - 1
	if room < 1 {
		return 0, max(width, 1), 0
	}
	if room >= folderWideAt {
		up = min(room/5, 24)
	}
	if room >= folderKidsAt {
		kids = min(room/3, 32)
	}
	gaps := folderGapFor(up) + folderGapFor(kids)
	here = room - up - kids - gaps
	if here < folderNameFloor && kids > 0 {
		here, kids, gaps = here+kids+1, 0, gaps-1
	}
	if here < folderNameFloor && up > 0 {
		here, up = here+up+1, 0
	}
	return up, max(here, 1), kids
}

// The three widths the division above turns on: where a parent column starts
// being affordable, where the children do, and the floor under a directory name.
// Twenty-four cells is a name a person recognizes; below that the middle column
// takes everything rather than showing three cut columns.
const (
	folderWideAt    = 82
	folderKidsAt    = 50
	folderNameFloor = 24
)

// navigate is every key this browser owns that is not a decision: the walk, the
// scroll, the hidden toggle and the filter box. enter and esc are left to the
// door that opened it ([app.folderKey]), for the reason [picker.navigate]
// leaves them there.
//
// It answers whether the COLUMNS ALREADY AGREE WITH THE BOX, so the door knows
// whether to re-seat a level after the key ([app.folderKey]). `→` and `←` move
// both at once and answer true; everything else — a letter, a backspace, a tab
// — moves the box and lets the columns follow it.
//
// THE WALK KEYS ARE READ BEFORE THE SHARED MAP because in the columns ←/→ are
// how a person moves between levels, and there they mean that from anywhere in
// the box: a browse whose ← only worked with the caret at the start would be a
// gesture that stops working the moment somebody types.
func (f *folderPick) navigate(msg tea.KeyPressMsg) bool {
	switch msg.String() {
	case "tab":
		// TAB MEANS NOTHING IN A BOX YOU TYPE INTO, which is what makes it free
		// for the fold in the model picker and free for the completion here.
		// Left to the shared key map it would insert a literal tab into a filter
		// — a character no path has and no list matches.
		f.complete()
		return false
	case folderHiddenKey:
		// THE CURSOR STAYS ON THE FOLDER IT WAS ON. Revealing the hidden ones
		// inserts rows above and below it, and a toggle that threw a person back
		// to the top of a level they had scrolled down would be a toggle nobody
		// presses twice.
		keep := ""
		if f.browsing && f.cols.cursor >= 0 && f.cols.cursor < len(f.cols.here.names) {
			keep = f.cols.here.names[f.cols.cursor]
		}
		f.hidden = !f.hidden
		f.forget()
		if f.browsing {
			f.browseAt(f.cols.dir, keep)
		}
		return true
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
	} else if msg.String() == "right" && f.filter.cursor == len(f.filter.value) {
		// `→` AT THE END OF THE BOX OPENS THE ROW UNDER THE CURSOR, which is what
		// makes a search result something you can walk into rather than a path to
		// retype. With the caret anywhere else it is still the box's own right —
		// somebody editing a filter in the middle of it has not asked to leave.
		if path, ok := f.here(); ok {
			f.openAt(path, "")
			return true
		}
	}
	listNavigate(msg, &f.filter, f.move, f.rank, f.page())
	return false
}

// folderHiddenKey reveals the dot-directories and the names the `@` walk prunes.
// It is `alt+h` and not `ctrl+h`, which a great many terminals still send as
// backspace — a toggle that silently ate a character of the filter would be
// worse than no toggle at all.
const folderHiddenKey = "alt+h"

// folderHintFields is the placeholder in the empty filter box, as the ranked
// fields it is made of — the keys go from the right, whole, on a frame too
// narrow for all of them (rowfit.go), so the box never draws a key spelled
// `es…`.
var folderHintFields = []rowField{
	rowSay("filter"), rowSay("↑↓"), rowSay("→ opens"),
	rowSay("enter adds"), rowSay("esc"),
}

// folderBrowseHintFields is the same line while the columns are up, where the
// keys mean something else entirely and saying so is the only honest legend.
var folderBrowseHintFields = []rowField{
	rowSay("←→ walk"), rowSay("↑↓"), rowSay("tab completes"),
	rowSay("alt+h hidden"), rowSay("enter adds this folder"), rowSay("esc"),
}

// folderHintAt is whichever of the two lines belongs to what is on screen, in
// the cells the box actually has.
func (f *folderPick) folderHintAt(room int) string {
	if f.browsing {
		return rowTail(folderBrowseHintFields, room)
	}
	return rowTail(folderHintFields, room)
}
