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
//     with path-segment, abbreviation and typo-aware folder ranking. Input that
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
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// folderRows is how many rows of DIRECTORIES this browser wants at once. The
// owner asked for a spacious sheet rather than the model picker's twelve, and
// eighteen is what a full-height terminal gives back after the status line, the
// box and one row of conversation — [app.overlayHeight] clamps it down on
// anything smaller, and the browser follows whatever it is actually given
// ([folderPick.page]).
const folderRows = 18

// folderPaneFloorRows is the least the sheet is drawn at while a preview is
// beside the list. Twelve rows is a readable amount of a file's source and a
// picture with a shape to it; under that the pane is a strip.
const folderPaneFloorRows = 12

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
	// not there. The same call in the walk's own box write put `/t/b/t/alpha/`
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

// folderListing is ONE readdir's answer: the subdirectories, the files under
// them, or the reason there are none.
//
// THE ERROR IS CARRIED AND NEVER FLATTENED TO AN EMPTY LIST. A directory nobody
// may read and a directory with nothing in it are two different facts about a
// person's disk, and a browser that drew them the same way would answer "is
// anything in there?" with a confident lie. [folderCols] draws them apart.
//
// THE DIRECTORIES LEAD AND THE FILES FOLLOW, which is Finder's order and the
// reference screenshot's: what you can walk into first, what you can only look
// at second. folderfiles.go holds the reader and the row arithmetic; the fields
// are apart rather than one slice of tagged rows so that every question this
// browser already asked about a directory still reads the field it always read.
type folderListing struct {
	names []string
	// files are the ordinary files in the same directory, after the
	// subdirectories (folderfiles.go).
	files []folderFile
	err   error
	// cut marks a directory whose rows were bounded at [folderRowsCap]. It is
	// SAID on the foot rather than hidden, because a person looking for a name
	// that is not on screen needs to know whether it is absent or merely beyond
	// the bound.
	cut bool
	// done marks an answer that has arrived, so an empty list that IS the answer
	// is told apart from a level nobody has read yet.
	done bool
}

// folderPick is the browser's whole state. The zero value is closed.
type folderPick struct {
	open bool

	// The ranker folds candidates once and reuses its scratch on each query.
	all    []folderCand
	ranker folderRanker
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
	kids map[string]folderListing
	// asking is which directories are being read right now, so a cursor held
	// down a column forks one readdir per level and not one per keypress.
	asking map[string]bool
	// gen stamps every read this OPENING asked for. An answer carrying another
	// opening's stamp is dropped: the picker that asked for it is gone, and
	// filing it against the one that is up would be a column of somebody else's
	// directory (folderplace.go's [folderKidsMsg]).
	gen int

	// held is which of these directories THE CONVERSATION IS ALREADY ABOUT, so
	// the action row can say the truth about the row under the cursor: a folder
	// already attached is one to take off, not one to add again.
	//
	// It is a snapshot filled by [app.markFolderHeld] on the open and refreshed
	// when one comes off, rather than a call through the door on every paint —
	// the row is rebuilt on every keystroke that moves the cursor.
	held map[string]bool

	// forTarget is this sheet opened FROM HOME, about the conversation home is
	// ABOUT TO OPEN rather than about the one this window is holding
	// (homedraft.go's target). It changes three things and nothing else: the
	// verb on the action row, what a confirmed folder does (it pins
	// [homeTarget.where] instead of being referred to a conversation), and where
	// `esc` lands — back on home, which is where the sheet was opened from.
	//
	// A FOLDER IS NEVER `held` ON THIS SHEET. `held` means "the conversation is
	// already about this", and the conversation this sheet is about does not
	// exist yet — so every folder on it is one that can be chosen, and the row
	// never offers to remove one ([app.openTargetFolderPick] leaves the map
	// empty for exactly that reason).
	forTarget bool

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
	// win is where the SHEET landed on the screen — the modal frame's own
	// rectangle and the cell [folderGeom] counts from (contextmodal.go). The two
	// are apart because they answer two different questions: geom is the layout
	// of the browser, and win is where that layout was put.
	win contextWin

	// marks are the things a person has CHOSEN and not yet confirmed, in the
	// order they chose them — the deliberate multiselect the owner asked for
	// (folderact.go). Browsing, focusing and previewing put nothing in here;
	// only alt+m and a press on a tray cell do.
	marks []folderMark
	// trayHot is which mark cell the pointer is over, or -1. It is answered
	// where the pointer is resolved ([app.folderHoverColumn]) and read where the
	// row is painted, so what lights is what a press takes off (hover.go's law).
	trayHot int
	// paneHot is which BODY ROW of the preview pane the pointer is over, or -1.
	// It is answered in the same place trayHot is and read in the same paint, and
	// it exists because the preview's folder listing is a set of rows a press now
	// acts on — a column that lit nothing while every one of its rows was a
	// target was the dead pane this wave was opened to fix (folderpane.go).
	paneHot int
	// say is one sentence this component wants the door to note — the mark cap's
	// refusal, and nothing else so far. The component draws no notes of its own:
	// a conversation's lines belong to the conversation, and a modal list that
	// wrote into it directly would be a second door onto the transcript.
	say string

	// The preview pane: where it is, how far it has been scrolled, and the three
	// pieces contextpreview.go asks a pane to hold (folderpane.go).
	//
	// previews is the seam — it owns the generation counter, the cancellation of
	// the read nobody wants any more, and the cache. preview is the answer being
	// drawn, and canvas is the one-entry memo in front of the painter so a sheet
	// redrawn on a tick does not lex the same forty rows of source again.
	pane     folderPane
	paneTop  int
	paneLeft int
	previews previewPump
	preview  filePreview
	canvas   previewCanvas

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
	here folderListing
	// up are the PARENT's subdirectories, drawn dim beside them so a person can
	// see where they are standing.
	up folderListing
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
	// tray is the mark tray's index within the overlay, or -1 where nothing is
	// marked or the frame had no room (folderpane.go).
	tray int
	// trayCells are the marks' cells on that row, left to right, so a press
	// takes off the one it is over.
	trayCells []hudSpan
	// crumbs are the breadcrumb's segments, left to right.
	crumbs []folderCrumb
	// up, here and pane are the three columns' cells. pane is the preview, in
	// the place the old column of children used to hold (folderpane.go).
	up, here, pane hudSpan
	// upFrom is which of the PARENT's rows the ancestry column's first row drew.
	//
	// IT IS THE ANCESTRY COLUMN'S OWN WINDOW AND NOT THE MIDDLE COLUMN'S. It used
	// to be derived from the middle column's cursor — the parent row for the
	// directory you are standing in was drawn beside the row the cursor was on —
	// which meant that with the cursor at the top of a folder, every sibling ABOVE
	// that folder was off the top of the column and the ancestry showed one name.
	// The owner's reference draws the parent as what it is: a list, with the one
	// you came out of marked in it.
	upFrom int
	// paneDir is the directory the preview pane is LISTING, absolute, or "" when
	// the pane is showing something that is not a folder. It is the raw path the
	// preview was read from and never a name off the screen.
	paneDir string
	// paneFrom is which entry of that listing the pane's first body row drew, and
	// paneBody how many rows it drew. Together they are the map from a pointer's
	// row to the entry it is over — written by the same function that draws those
	// rows ([folderPick.paneRows]), so the two cannot disagree.
	paneFrom, paneBody int
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
	// THE PREVIEW PUMP SURVIVES THE OPENING AND ITS CACHE WITH IT. Reopening the
	// browser on the folder you were just in is the common next gesture, and the
	// identity in every cache key is what makes a held preview safe to reuse
	// (contextpreview.go's [previewPump.close] states this). Its read in flight
	// is cancelled first, because that one belongs to the sheet that has gone.
	f.previews.close()
	pump := f.previews
	*f = folderPick{
		open:     true,
		all:      candidates,
		tilde:    tilde,
		facts:    map[string][]string{},
		kids:     map[string]folderListing{},
		asking:   map[string]bool{},
		gen:      gen,
		previews: pump,
		trayHot:  -1,
		paneHot:  -1,
	}
	f.geom.action = -1
	f.geom.tray = -1
	rankees := make([]folderRankee, len(candidates))
	for i, cand := range candidates {
		rankees[i] = folderRankee{Show: cand.show, Layer: int(cand.layer), Rank: cand.rank, Freq: cand.freq}
	}
	f.ranker.load(rankees)
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
//
// AND SO DOES THE PREVIEW PUMP, for [folderPick.start]'s reason: its own
// [previewPump.close] cancels the read in flight and forgets the selection while
// KEEPING the cache, which is what makes reopening on the same folder free.
//
// THE MARKS DO NOT SURVIVE, and that is the whole of esc's promise: nothing a
// person chose in a sheet they then abandoned reaches the conversation, the
// message, or the next opening of this list.
func (f *folderPick) close() {
	f.previews.close()
	*f = folderPick{gen: f.gen + 1, previews: f.previews}
}

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
// Folder search ranks path segments, abbreviations and small spelling slips,
// then preserves source priority and prior use within each match class.
func (f *folderPick) rank() {
	// WHICH SURFACE IS UP FOLLOWS THE BOX, WITH ONE EXCEPTION THAT IS THE WHOLE
	// OF THIS WAVE'S REPAIR TO IT: AN EMPTY BOX CHANGES NOTHING.
	//
	// The box used to hold the current directory while the columns were up
	// ([folderPick.writeBack], as it was), so the mode really did follow the box
	// alone. It cost the search. Somebody who opened the chooser and typed `sibl`
	// — the ordinary thing to do — appended four characters to a path and got a
	// browse of `~/code/worksibl`, and the only way to reach the ranked list of
	// remembered places was to notice the path was in the box and clear it first.
	//
	// So the box is a SEARCH box and the breadcrumb is where you are. Typing a
	// word ranks; typing a path browses it; clearing the box puts you back where
	// you were standing rather than throwing the columns away. Walking clears the
	// box ([folderPick.browseHold]), which is what makes the two agree without
	// either of them being a claim about the other.
	if typed := strings.TrimSpace(f.filter.String()); typed == "" {
		// AN EMPTY BOX MEANS "WHEREVER THE COLUMNS ARE", and that is true whether
		// the columns are what is on screen or what a search is standing in front
		// of: clearing the box after a search puts a person back where they were
		// browsing rather than leaving them on a list of everything. `f.cols.dir`
		// is set the moment the sheet opens anywhere ([app.openContextPick]), so
		// the only state with no columns behind it is a sheet that has never been
		// pointed at a directory at all.
		if f.cols.dir != "" {
			f.browsing = true
			return
		}
	} else {
		f.browsing = folderPathish(typed)
		if f.browsing {
			return
		}
	}
	f.hits = f.ranker.rank(f.filter.String())
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
		was := f.cols.cursor
		f.cols.cursor = moveCursor(f.cols.cursor, delta, f.cols.here.rows())
		f.cols.top = listTop(f.cols.cursor, f.cols.top, f.cols.here.rows(), f.page())
		if f.cols.cursor != was {
			// A NEW THING UNDER THE CURSOR IS A NEW PREVIEW FROM ITS TOP. Carrying
			// the last file's scroll onto the next one would open a short file
			// showing nothing at all (folderpane.go's [folderPick.paneRest]).
			f.paneRest()
		}
		return
	}
	f.cursor = moveCursor(f.cursor, delta, len(f.hits))
	f.follow(f.page())
}

func (f *folderPick) follow(height int) {
	f.top = listTop(f.cursor, f.top, len(f.hits), height)
}

// here is the thing the cursor is on — the one the action row would take — and
// false where there is none. It is what every decision on this surface reads
// before it acts, and folderact.go's [folderPick.hereKind] is the same answer
// with the extra fact of WHICH KIND it is.
//
// A ROW MAY NOW BE A FILE. It used to be a directory always, which is why every
// caller that needs to know says so through hereKind rather than guessing from
// the path's shape — a directory called `notes.md` is a perfectly ordinary thing
// to have on a disk.
func (f *folderPick) here() (string, bool) {
	path, _, ok := f.hereKind()
	return path, ok
}

// ── the columns ─────────────────────────────────────────────────────────────

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
	f.cols.upAt = f.cols.up.rowAt(filepath.Base(f.cols.dir))
}

// seatCursor puts the cursor on the name the columns are waiting for, if that
// name has arrived, and follows it with the window either way.
func (f *folderPick) seatCursor() {
	if f.cols.keep != "" {
		if at := f.cols.here.rowAt(f.cols.keep); at >= 0 {
			f.cols.cursor, f.cols.keep = at, ""
		}
	}
	f.cols.cursor = moveCursor(f.cols.cursor, 0, f.cols.here.rows())
	f.cols.top = listTop(f.cols.cursor, f.cols.top, f.cols.here.rows(), f.page())
}

// read is one level out of the cache, and the zero [folderListing] — not done, no
// error, no names — for a level nobody has read yet.
func (f *folderPick) read(dir string) folderListing {
	if f.kids == nil || dir == "" {
		return folderListing{}
	}
	return f.kids[dir]
}

// took files one readdir's answer and re-seats the columns if the answer was
// about a level they are drawing. It reports whether anything on screen changed.
func (f *folderPick) took(dir string, read folderListing) bool {
	if !f.open {
		return false
	}
	if f.kids == nil {
		f.kids = map[string]folderListing{}
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
	f.kids, f.asking = map[string]folderListing{}, map[string]bool{}
	f.cols.here, f.cols.up, f.cols.upAt = folderListing{}, folderListing{}, -1
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
	for at := 0; at < f.cols.here.rows(); at++ {
		if strings.HasPrefix(strings.ToLower(f.cols.here.rowName(at)), lower) {
			f.cols.cursor = at
			f.cols.top = listTop(at, f.cols.top, f.cols.here.rows(), f.page())
			f.paneRest()
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
//
// A FILE IS NOT SOMEWHERE TO GO. `→` on one does nothing rather than opening
// anything: this browser reads a file into the pane beside the list and never
// runs it, and a key that sometimes navigated and sometimes opened a document
// would be the ambiguous gesture the owner asked us not to build.
func (f *folderPick) descend() bool {
	if !f.browsing || !f.cols.here.isDir(f.cols.cursor) {
		return false
	}
	f.browseAt(filepath.Join(f.cols.dir, f.cols.here.rowName(f.cols.cursor)), "")
	f.browseHold()
	f.paneRest()
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
	f.browseHold()
	f.paneRest()
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
	f.browseHold()
	f.paneRest()
}

// browseHold is what every walk does to the box: it EMPTIES it, and keeps the
// columns up.
//
// It used to write the directory back into the box instead, on the reasoning
// that what is typed and what is shown must never be two different claims about
// where a person is. The reasoning was right and the box was the wrong place to
// answer it: the sheet now says where you are in two places that cannot be typed
// over — the breadcrumb above the columns and the location on the sheet's own
// head rule (contextmodal.go) — which leaves the box free to be what a person
// reaches for it as, a search.
//
// EMPTYING IT IS WHAT KEEPS THE TWO IN STEP. [folderPick.browseSync] follows the
// box on every keystroke, so a box still holding the path you walked away from
// would drag the columns back the moment somebody typed; and an empty box means
// "wherever the columns are", which is exactly what [folderPick.rank] then does
// with it.
func (f *folderPick) browseHold() {
	f.filter.setText("")
	f.browsing = true
}

// complete is `tab`: the highlighted row's name is written into the box, whole,
// with a separator after it — which is a `→` a person can see the result of
// before they commit to it.
// A FILE IS COMPLETED WITHOUT A SEPARATOR AFTER IT, because a separator would
// turn the box into a claim that the file is a directory — and the very next
// keystroke re-resolves the box, so the columns would jump to the file's own
// parent and lose the row a person had just landed on.
func (f *folderPick) complete() bool {
	at := f.cols.cursor
	if !f.browsing || at < 0 || at >= f.cols.here.rows() {
		return false
	}
	shown := tildePath(filepath.Join(f.cols.dir, f.cols.here.rowName(at)), f.tilde)
	if f.cols.here.isDir(at) {
		shown += "/"
	}
	f.filter.setText(shown)
	return true
}

// wanted is every directory this browser needs read and has not got: the level
// under the cursor and its parent. It is what the door turns into commands
// ([app.askFolderKids]) and it is deliberately TWO PATHS AT MOST — a browser
// that read ahead down a tree would be the recursive scan this design refuses.
//
// IT USED TO BE THREE. The children of the highlighted row were read here for
// the old third column, which meant one extra readdir every time the cursor
// moved a row. That region is the preview pane now (folderpane.go), and
// contextpreview.go reads it under its own bound, its own cancellation and its
// own cache — so the same fact reaches the screen for one read rather than two.
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
		// A SHEET WITH A PREVIEW ON IT ASKS FOR THE WHOLE ALLOWANCE, because the
		// pane is what the extra rows are for: a four-row preview of a
		// six-hundred-line file is a pane nobody can read, and the owner asked
		// for the reference screenshot's spacious hierarchy rather than a strip.
		// A sheet with the preview off follows its own rows, so a folder with
		// three things in it is still three rows and not eighteen.
		rows := f.cols.here.rows()
		if folderDivide(width, f.pane).pane > 0 {
			// A SHEET WITH A PREVIEW ON IT HAS A FLOOR AND NOT A FIXED HEIGHT. It
			// used to ask for the whole allowance whatever was in the folder, so a
			// directory with four things in it drew four names and fourteen blank
			// rows inside a frame — which reads as a broken sheet rather than as a
			// spacious one, and is what the owner met. The floor is what the PREVIEW
			// needs to be worth reading; past it the middle column's own length is
			// the honest height.
			//
			// IT IS THE MIDDLE COLUMN'S LENGTH AND NEVER THE PREVIEW'S, deliberately:
			// the preview changes as the cursor moves and a sheet that resized under
			// a person walking a list would be unusable. `alt+o` is how a long file
			// gets the whole sheet.
			rows = max(rows, folderPaneFloorRows)
		}
		return min(max(rows, 1), folderRows) + f.chromeRows()
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

// chromeRows is what the browse draws beside the rows: the breadcrumb above,
// the action row below, and the mark tray between them when anything has been
// chosen (folderpane.go).
func (f *folderPick) chromeRows() int {
	if len(f.marks) > 0 {
		return folderChromeRows + 1
	}
	return folderChromeRows
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
func (f *folderPick) rows(width, n int, pal palette, st *tokens.Styler, hover int, col string) []string {
	if n <= 0 {
		return nil
	}
	trayCells := f.geom.trayCells[:0]
	f.geom = folderGeom{action: -1, tray: -1, trayCells: trayCells}
	// THE ACTION ROW IS THE FIRST ROW GIVEN UP AND THE LAST ROW DRAWN, because a
	// frame with one row to give must spend it on the directories: a sheet
	// showing only the way to add something, with no way to see what would be
	// added, is not a browser. Its own row number is known before it is painted,
	// so the pointer's band lands on it.
	// The filter contains the current path while browsing, so its placeholder
	// cannot teach the preview controls. Reserve a persistent legend above the
	// action, giving it up only when the terminal cannot hold a useful list.
	wantLegend := n > 4
	if wantLegend {
		n--
	}
	wantAction := n > 1
	if wantAction {
		n--
	}
	// THE MARK TRAY IS GIVEN UP NEXT, and only ever drawn when something is on
	// it. It says how many things the confirm is carrying, so it is worth a row
	// exactly when the action row's sentence would otherwise be a count with
	// nothing behind it (folderpane.go).
	wantTray := len(f.marks) > 0 && n > 2
	if wantTray {
		n--
	}
	var body []string
	switch {
	case f.browsing:
		body = f.columnRows(width, n, pal, st, hover, col)
	case len(f.hits) == 0:
		body = []string{pal.dim(folderPad + folderNoMatchWord)}
		f.geom.body = 1
	default:
		body = f.listRows(width, n, pal, hover)
	}
	out := body
	if wantTray {
		hot := -1
		if col == folderColTray {
			hot = f.trayHot
		}
		f.geom.tray = len(out)
		out = append(out, f.trayRow(width, pal, hot))
	}
	if !wantAction {
		return out
	}
	if wantLegend {
		out = append(out, pal.dim(folderPad+f.controlLegend(width-folderPadCells)))
	}
	f.geom.action = len(out)
	return append(out, f.actionRow(width, pal, hover))
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

// columnRows draws the breadcrumb and the successive columns: the ancestry dim
// on the left, where you are in the middle, and the PREVIEW of the thing under
// the cursor on the right. No borders and no rules between them — the columns
// are told apart by the gaps and by the ink, which is what every other block on
// this surface does and what keeps the reference screenshot's hierarchy without
// borrowing its chrome.
func (f *folderPick) columnRows(width, n int, pal palette, st *tokens.Styler, hover int, col string) []string {
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
	div := folderDivide(width, f.pane)
	f.geom.up = hudSpan{from: folderPadCells, to: folderPadCells + div.up}
	f.geom.here = hudSpan{from: f.geom.up.to + folderGapFor(div.up)}
	f.geom.here.to = f.geom.here.from + div.here
	f.geom.pane = hudSpan{from: f.geom.here.to + folderGapFor(div.pane)}
	f.geom.pane.to = f.geom.pane.from + div.pane
	if div.here < 1 {
		// THE PREVIEW ALONE. There is no list to lay out, so the pane starts at
		// the one margin every row of this sheet shares.
		f.geom.pane = hudSpan{from: folderPadCells, to: folderPadCells + div.pane}
	}
	f.cols.top = listTop(f.cols.cursor, f.cols.top, f.cols.here.rows(), n)
	// The ancestry column's own window, seated on the folder we are standing in
	// (folderGeom.upFrom says why it is its own). It is computed here rather than
	// in [folderPick.upText] because the press reads it back — one window, written
	// once, for the draw and the pointer both.
	f.geom.upFrom = listTop(f.cols.upAt, 0, f.cols.up.rows(), n)
	// WHICH ROW OF THE PANE THE POINTER IS ON, in the pane's own numbering, and
	// only while the pointer is actually over that column. It is read back from
	// where the pointer was RESOLVED rather than recomputed here, which is exactly
	// how the mark tray's own hot cell works and for the same reason: one answer
	// to "what is the pointer over", written once.
	paneHot := -1
	if col == folderColPane {
		paneHot = f.paneHot
	}
	pane := f.paneRows(pal, st, width, n, paneHot)
	for row := 0; row < n; row++ {
		line := folderPad
		if div.up > 0 {
			line += folderCell(f.upText(row, div.up, pal), div.up) + " "
		}
		if div.here > 0 {
			hovered := col == folderColHere && hover == row+f.geom.head
			line += folderCell(f.hereText(row, div.here, pal, hovered), div.here)
			if div.pane > 0 {
				line += " "
			}
		}
		if row < len(pane) {
			line += pane[row]
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
	// Keep the leaf and as many whole ancestors as fit. Charge the cut marker
	// only for a shortened trail; its four cells do not replace a three-cell
	// separator for free. Very small rows give their cells to the leaf instead.
	pad := min(max(width, 0), folderPadCells)
	room := max(0, width-pad)
	line, cells := folderPad[:pad], pad
	f.geom.crumbs = f.geom.crumbs[:0]
	if len(trail) == 0 || room == 0 {
		return line
	}
	cutCells := ansi.StringWidth(folderCrumbCut)
	from := len(trail) - 1
	used := ansi.StringWidth(trail[from].name)
	for from > 0 {
		next := used + folderCrumbGapCells + ansi.StringWidth(trail[from-1].name)
		cost := next
		if from > 1 {
			cost += cutCells
		}
		if cost > room {
			break
		}
		from--
		used = next
	}
	if from > 0 && room > cutCells {
		line, cells = line+pal.dim(folderCrumbCut), cells+cutCells
	}
	for at := from; at < len(trail); at++ {
		crumb := trail[at]
		if at > from {
			line, cells = line+pal.dim(folderCrumbGap), cells+folderCrumbGapCells
		}
		name := fit(crumb.name, width-cells)
		if name == "" {
			continue
		}
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
	at := row + f.geom.upFrom
	if at < 0 || at >= f.cols.up.rows() {
		return ""
	}
	name := f.cols.up.rowName(at)
	if f.cols.up.isDir(at) {
		name += "/"
	}
	name = fit(name, room)
	if at == f.cols.upAt {
		return pal.muted(name)
	}
	return pal.dim(name)
}

// hereText is one row of the middle column: the lead, then the name, then the
// size against the right edge for a file.
//
// THE LEAD IS FOUR CELLS AND IT CARRIES TWO SEPARATE FACTS, because focus and
// selection have to be told apart at a glance [steering-02 §5]. The first cell
// is the CURSOR — `›` in accent, the same lead every other list down here uses
// ([overlayLead]) — and the third is the MARK, so a row can be both and a person
// can see that it is. The row under the cursor is ink and bold behind that lead,
// so it stays the brightest thing on the sheet on a monochrome terminal too; the
// row under the POINTER carries the same band every list on this surface gives it
// (hover.go's law).
//
// A DIRECTORY WEARS A TRAILING SLASH AND THE BODY INK; A FILE WEARS THE QUIETER
// ONE AND ITS SIZE. Two tiers is the whole colour scheme here — a filename
// coloured by its type is a legend nobody was given [steering-02 §5] — and it is
// deliberately the same two tiers the preview pane's own folder listing uses
// ([previewNameAndSize]), so the sheet reads as one thing.
func (f *folderPick) hereText(row, room int, pal palette, hovered bool) string {
	return folderCellBand(f.hereInk(row, room, pal), room, pal,
		row+f.cols.top == f.cols.cursor, hovered)
}

// folderCellBand puts the ladder's own grounds under a row of the middle
// column — and under THAT COLUMN'S CELLS AND NOTHING WIDER.
//
// THE THREE STATES ARE THREE DIFFERENT THINGS AND THEY ARE TOLD APART. The row
// the keyboard is on wears the SELECTED step, which is ungated on the linear
// tier because "which row am I on" is a fact for every reader (styles.go); the
// row the POINTER is on wears the quieter cursor step, and only where it is not
// already the selected one, so hover never speaks over selection. What a person
// has CHOSEN is neither of these — it is the persistent mark in the lead, and it
// survives moving off the row, which a ground cannot.
//
// AND THE BAND IS THE COLUMN'S WIDTH, NEVER THE TERMINAL'S. A stripe across the
// whole frame would light the ancestry and the preview beside a row that has
// nothing to do with either [owner review 2026-09-08 §1].
func folderCellBand(painted string, room int, pal palette, cursor, hovered bool) string {
	cell := folderCell(painted, room)
	switch {
	case cursor:
		return pal.selected(cell, room)
	case hovered:
		return pal.cursor(cell, room)
	}
	return cell
}

// hereInk is one row of the middle column's INK, before any ground goes under
// it.
func (f *folderPick) hereInk(row, room int, pal palette) string {
	at := row + f.cols.top
	if at < 0 || at >= f.cols.here.rows() {
		if row == 0 && f.cols.here.rows() == 0 {
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
	dir := f.cols.here.isDir(at)
	name := f.cols.here.rowName(at)
	// THE MARK IS LOOKED UP ONLY WHERE THERE ARE MARKS. This runs once per row of
	// every frame the sheet is drawn on, and [filepath.Join] allocates — an empty
	// tray, which is nearly every sheet there is, must not pay for eighteen of
	// them (PERF.md's rule about work on the paint path).
	chosen := len(f.marks) > 0 && f.marked(filepath.Join(f.cols.dir, name))
	if dir {
		name += "/"
	}
	size := ""
	if file, ok := f.cols.here.rowFile(at); ok {
		size = folderSizeWord(file)
	}
	lead := "  "
	if at == f.cols.cursor {
		lead = pal.accent("› ")
	}
	if chosen {
		lead += pal.accent(folderMarkGlyph(pal) + " ")
	} else {
		lead += "  "
	}
	// The third pair reinforces the file type with a small shape and colour cue.
	lead += folderTypeMark(pal, name, dir)
	return lead + folderNameAndSize(pal, name, size, dir, at == f.cols.cursor, room-folderLeadCells)
}

// folderLeadCells is what the lead above costs: the cursor mark, the choice
// mark and the type mark, each with its own space.
const folderLeadCells = 6

// folderNameAndSize lays one row out: the name, then whatever space is left,
// then the size against the right edge — the reference screenshot's own
// alignment, and the one that makes a column of sizes readable at a glance.
//
// The SIZE is given up before the name is, because the name is what a person
// came to read; and a size is drawn only where one was obtained
// ([folderSizeWord]) [design-law §EMPTINESS].
func folderNameAndSize(pal palette, name, size string, dir, cursor bool, room int) string {
	ink := pal.ink
	switch {
	case cursor:
		ink = func(s string) string { return pal.bold(pal.ink(s)) }
	case dir:
		ink = pal.muted
	}
	if size == "" || room < folderSizeAt {
		return ink(fit(name, room))
	}
	gap := room - ansi.StringWidth(size) - 1
	if gap < folderSizeFloor {
		return ink(fit(name, room))
	}
	fitted, used := fitWidth(name, gap)
	return ink(fitted) + strings.Repeat(" ", room-used-ansi.StringWidth(size)) + pal.dim(size)
}

// The two widths a size turns on: the narrowest column that carries one at all,
// and the least name left over once it has. Under either, the name takes
// everything — a row showing `12.4 KB` beside `main…` is a row that gave up the
// only thing on it a person was looking for.
const (
	folderSizeAt    = 28
	folderSizeFloor = 12
)

// folderStateWord is what a column with no names in it says about itself — and
// there are five different things it can be saying, four of which are reasons
// and one of which is silence.
//
// THE READ THAT FAILED AND THE READ THAT FOUND NOTHING ARE TOLD APART BY THE
// ERROR AND NEVER BY THE COUNT. That is the defect this replaced: `folderKids`
// dropped `os.ReadDir`'s error and answered nil, so a directory somebody may not
// open drew `nothing below here` — a confident lie about their own disk.
func folderStateWord(read folderListing) string {
	switch {
	case !read.done:
		return ""
	case read.err == nil:
		return folderLeafWord
	case os.IsPermission(read.err):
		return folderClosedWord
	case os.IsNotExist(read.err):
		return folderMissingWord
	case errors.Is(read.err, syscall.ENOTDIR):
		return folderNotDirWord
	}
	return folderUnreadableWord
}

// The five things a column with no rows can be saying. They are constants
// because the manual quotes each of them exactly as it is spelled here.
//
// THE FOUR REFUSALS ARE THE SEARCH LANE'S OWN SPELLING (its `folderReadWord`,
// reports/search.md), deliberately: that lane's read helpers replace the readdir
// under this file after integration, and two lanes shipping two sentences for
// the same disk error would be a wording change nobody decided on. The constants
// are named apart from its own so the two files can sit in one package until the
// swap happens.
const (
	// folderLeafWord is a directory that WAS read and has nothing under it.
	folderLeafWord = "nothing below here"
	// folderClosedWord is a directory this machine will not let this program
	// read. It is NEVER the word above.
	folderClosedWord = "this folder cannot be read · permission denied"
	// folderUnreadableWord is every other way a readdir fails.
	folderUnreadableWord = "this folder cannot be read"
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
	// SEVERAL THINGS CHOSEN IS ONE SENTENCE AND NOT A PATH, because there is no
	// single path to draw and a row naming only the last one would be the sheet
	// misreporting what enter is about to do (folderact.go).
	if len(f.marks) > 0 {
		painted := pal.dim(folderTakeWord) + pal.ink(fit(f.markWord(), max(room-ansi.StringWidth(folderTakeWord), 1)))
		if hover >= 0 && hover == f.geom.actionAt() {
			painted = pal.cursor(painted, 0)
		}
		return folderPad + painted
	}
	path, dir, ok := f.hereKind()
	if !ok {
		return pal.dim(folderPad + fit(f.hint(), room))
	}
	shown := tildePath(path, f.tilde)
	lead := f.actionWord(path, dir)
	// The facts ride the right end of the same row, and are dropped WHOLE rather
	// than cut: half a branch name is a branch nobody has.
	facts := strings.Join(f.factsFor(path), " · ")
	keep := room - ansi.StringWidth(lead)
	if facts != "" {
		if room := keep - ansi.StringWidth(facts) - folderFactsGap; room >= folderNameFloor {
			keep = room
		} else {
			facts = ""
		}
	}
	shown = fit(shown, max(keep, 1))
	painted := pal.dim(lead) + pal.ink(shown)
	if hover >= 0 && hover == f.geom.actionAt() {
		painted = pal.cursor(painted, 0)
	}
	cells := ansi.StringWidth(lead) + ansi.StringWidth(shown)
	line := folderPad + painted
	if facts != "" {
		line += strings.Repeat(" ", max(room-cells-ansi.StringWidth(facts), folderFactsGap)) + pal.dim(facts)
	}
	return line
}

// actionAt is where the action row was drawn on the LAST paint, which is what a
// hover index is compared against while the current one is still being built.
func (g folderGeom) actionAt() int { return g.action }

// actionWord is the verb the action row is offering, and it is the whole of how
// this sheet stays honest about a folder that is already attached.
//
// ONE ROW, ONE ACTION, WHATEVER STATE THE FOLDER IS IN. A row that always said
// `add` on a folder the conversation already holds would be offering something
// that is already true — and it would leave TAKING ONE OFF as a gesture you can
// only make with a mouse, on a tray cell above the box, which is the one thing
// docs/DESIGN-LANGUAGE.md refuses: the keyboard stays first-class and every
// chord keeps a visible, clickable, self-teaching door beside it. Here the door
// and the key are the same row.
// A FILE HAS ITS OWN TWO VERBS, and they are not the folder's. Adding a folder
// is a lasting fact about the conversation; attaching a file is cargo on the
// NEXT MESSAGE — two different things happening to two different objects, and
// one word for both would have been the surface hiding the difference that
// matters most here (folderact.go's header).
func (f *folderPick) actionWord(path string, dir bool) string {
	switch {
	case dir && f.forTarget:
		// THE SHEET OPENED FROM HOME IS ABOUT A CONVERSATION THAT DOES NOT EXIST
		// YET, and `add this folder` would be a promise about the one behind home
		// — the invisible effect this wave exists to end. The row says what enter
		// actually does, in the words home's own rule says it in (homedraft.go).
		return folderTargetWord
	case dir && f.held[path]:
		return folderDropWord
	case dir:
		return folderAddWord
	case isImagePath(path):
		return folderPictureWord
	}
	return folderFileWord
}

// holds reports whether the folder under the cursor is one the conversation is
// already about — which is what decides both what the row says and what pressing
// it does ([app.folderConfirm]). A FILE NEVER HOLDS: the tray above the box is
// where a file's own removal lives (attach.go).
func (f *folderPick) holds() (string, bool) {
	path, dir, ok := f.hereKind()
	if !ok || !dir {
		return "", false
	}
	return path, f.held[path]
}

// The four verbs the action row offers. They are constants because the manual
// quotes every one of them exactly as it is spelled here.
const (
	folderAddWord     = "add this folder · "
	folderDropWord    = "remove this folder · "
	folderFileWord    = "attach this file · "
	folderPictureWord = "attach this picture · "
	// folderTargetWord is the verb on the sheet home opened: this folder is
	// where the NEXT conversation opens, and nothing behind home is touched.
	folderTargetWord = "open the next conversation in · "
	// folderTakeWord leads the row once several things are chosen, where there
	// is no one path to draw after it.
	folderTakeWord = "enter · "
)

// markWord is what the action row says about a set of marks: each kind with its
// OWN verb, because adding a folder and attaching a file are two different
// things and a single count would have hidden which was about to happen.
//
// Nothing is said about a kind with none of it ([design-law §EMPTINESS]).
func (f *folderPick) markWord() string {
	folders, files := 0, 0
	for _, mark := range f.marks {
		if mark.dir {
			if f.held[mark.path] {
				// Already this conversation's, and a mixed confirm leaves it alone
				// rather than taking it off ([folderPick.takes] says why).
				continue
			}
			folders++
			continue
		}
		files++
	}
	var parts []string
	switch {
	case folders > 0 && f.forTarget:
		// ONE FOLDER IS ALL A TARGET CAN BE ([folderPick.mark] keeps it to one),
		// so this says the act rather than a count nobody needs.
		parts = append(parts, folderTargetMarkWord)
	case folders > 0:
		parts = append(parts, "add "+itoa(folders)+plural(" folder", folders))
	}
	if files > 0 {
		parts = append(parts, "attach "+itoa(files)+plural(" file", files))
	}
	if len(parts) == 0 {
		return folderHeldOnlyWord
	}
	return strings.Join(parts, " · ")
}

// folderHeldOnlyWord is a set of marks with nothing left in it to do — every
// folder chosen is one the conversation already holds. Saying so is the honest
// answer; a row reading `add 0 folders` would be arithmetic in a person's face.
const folderHeldOnlyWord = "these folders are already here"

// folderTargetMarkWord is the marked-folder clause on the sheet home opened.
const folderTargetMarkWord = "open the next conversation there"

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
	folderBrowseHintWord = "←→ walk · alt+m chooses · alt+p hides the preview · ctrl+u searches"
)

// folderCell pads one painted cell to its column's width, measuring through the
// escape sequences rather than around them.
func folderCell(painted string, room int) string {
	if gap := room - ansi.StringWidth(painted); gap > 0 {
		return painted + strings.Repeat(" ", gap)
	}
	return painted
}

// The two widths the division in folderpane.go turns on: where an ancestry
// column starts being affordable, and the floor under a name. Twenty-four cells
// is a name a person recognizes; below that the middle column takes everything
// rather than showing three cut columns.
const (
	folderWideAt    = 82
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
	// THE PANE KEYS ARE READ FIRST AND THEY ARE READ WHATEVER SURFACE IS UP, so
	// a person who turned the preview off in the columns does not find it back on
	// when a filter puts the list up. `say` is how the one key with something to
	// tell gets it said (folderact.go's cap).
	switch msg.String() {
	case folderPaneKey:
		// OFF AND BESIDE, one key, and the wide state is the OTHER key's — a
		// three-way cycle on one key would make "hide it" a gesture you have to
		// press twice to be sure of.
		if f.pane == folderPaneOff {
			f.pane = folderPaneBeside
		} else {
			f.pane = folderPaneOff
		}
		f.paneRest()
		return true
	case folderWideKey:
		if f.pane == folderPaneWide {
			f.pane = folderPaneBeside
		} else {
			f.pane = folderPaneWide
		}
		f.paneRest()
		return true
	case folderMarkKey:
		f.say = f.mark()
		return true
	case "shift+up":
		f.paneStep(-1)
		return true
	case "shift+down":
		f.paneStep(1)
		return true
	case "shift+left":
		f.paneSlide(-folderSlideStep)
		return true
	case "shift+right":
		f.paneSlide(folderSlideStep)
		return true
	}
	// AND WITH THE PREVIEW ALONE ON THE SHEET THE PLAIN ARROWS ARE ITS OWN, for
	// folderpane.go's stated reason: the pane the keys act on is the pane that is
	// drawn, so there is no focus to lose track of.
	if f.browsing && f.pane == folderPaneWide {
		switch msg.String() {
		case "up":
			f.paneStep(-1)
			return true
		case "down":
			f.paneStep(1)
			return true
		case "pgup":
			f.paneStep(-f.page())
			return true
		case "pgdown":
			f.paneStep(f.page())
			return true
		case "left":
			f.paneSlide(-folderSlideStep)
			return true
		case "right":
			f.paneSlide(folderSlideStep)
			return true
		}
	}
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
	rowSay("search folders and files"), rowSay("↑↓"), rowSay("→ opens"),
	rowSay("enter adds"), rowSay("esc"),
}

// folderBrowseHintFields is what the SEARCH BOX says while the columns are up:
// what typing in it does, and the two chords the foot row does not carry.
//
// IT IS FOUR CLAUSES AND IT USED TO BE TEN. The two lines together — this
// placeholder and [folderPick.controlLegend] under the columns — name every
// chord the sheet owns exactly once, which is docs/DESIGN-LANGUAGE.md's rule
// that a chord keeps a visible door beside it; naming them all in BOTH places
// was the wall of shortcuts the owner's 2026-09-08 review asked us to stop
// drawing. The fields go from the RIGHT, whole, on a frame too narrow for all of
// them (rowfit.go), so the line never draws a key spelled `alt+…`.
var folderBrowseHintFields = []rowField{
	rowSay("search folders and files"), rowSay("or type a path"),
	rowSay("alt+p preview"), rowSay("alt+h hidden"),
}

// folderWideHintFields is the legend with the preview alone on the sheet, where
// the arrows are the pane's own and there is no list to walk (folderpane.go).
var folderWideHintFields = []rowField{
	rowSay("↑↓ scroll"), rowSay("←→ slide"), rowSay("alt+o back"), rowSay("esc"),
}

// folderHintAt is whichever of the two lines belongs to what is on screen, in
// the cells the box actually has.
func (f *folderPick) folderHintAt(room int) string {
	switch {
	case f.browsing && f.pane == folderPaneWide:
		return rowTail(folderWideHintFields, room)
	case f.browsing:
		return rowTail(folderBrowseHintFields, room)
	}
	return rowTail(folderHintFields, room)
}

// controlLegend keeps cancel and the narrow preview door ahead of optional hints.
func (f *folderPick) controlLegend(room int) string {
	if !f.browsing {
		return rowTail([]rowField{rowSay("esc cancel"), rowSay("→ open"), rowSay("enter add")}, room)
	}
	if f.pane == folderPaneWide {
		return rowTail([]rowField{rowSay("esc cancel"), rowSay("alt+o back"), rowSay("↑↓ scroll"), rowSay("←→ slide")}, room)
	}
	// FIVE CLAUSES AND NOT EIGHT. This row used to name every chord the sheet
	// owns, which made the one line under the columns a wall of shortcuts that
	// read as noise and taught nothing [owner review 2026-09-08 §5]. What is left
	// is the four things a person actually reaches for and the way out; the rest
	// live in the box's own placeholder, which is on screen whenever the box is
	// empty — which is now its resting state — and in the manual.
	// THE PREVIEW'S DOOR IS ALWAYS NAMED AND THE WORD FOR IT IS CONTEXTUAL. On a
	// narrow frame the pane has been given up for the names (folderpane.go) and
	// `alt+o` is the only way to read a file at all; on a wide one it is the way to
	// give the file the whole sheet. Either way it is a door a person cannot guess,
	// and docs/DESIGN-LANGUAGE.md's rule is that every chord keeps a visible one
	// beside it — so it is one clause here rather than a clause that comes and
	// goes as the terminal is dragged.
	door := "alt+o preview"
	if f.paneWide(room) {
		door = "alt+o wide"
	}
	return rowTail([]rowField{
		rowSay("esc cancel"), rowSay("enter add"), rowSay(door),
		rowSay("←→ walk"), rowSay("alt+m choose"), rowSay("ctrl+u search"),
	}, room)
}
