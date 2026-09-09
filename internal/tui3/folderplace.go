package tui3

// THE ROADS ONTO A FOLDER, AND THE ONE SEAM THEY ALL COME OUT OF.
//
// folderpick.go is the component; this file is everything the app does around
// it — where the candidates come from, what the third column is told, where the
// picks are kept, and what happens when somebody finally presses enter.
//
// ── THE SEAM ────────────────────────────────────────────────────────────────
//
// Every road that ends in "this directory" ends in [app.referPlace] with a
// [chosenPlace]: the picker's enter, `/attach` handed a folder, and — when the
// forming card's ground row lands — its `g`. There is ONE such method on
// purpose. Lane P2 is what makes a chosen place PERSIST (a `Places` list on the
// conversation's meta, feeding the ground ladder's SAID rung); it replaces the
// body of that one method and does not have to go and find the doors again.
// What P1 does there is real and not a placeholder: it writes the pick down for
// frecency, so the next `/folder` opens with the answer on the first row, and
// it says one dim line.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/reltime"
)

// placeDoor is WHICH ROAD a directory came in by. It is carried rather than
// inferred because the roads are told apart by what they may say afterwards —
// the picker has already shown the person what they picked and needs no
// sentence, `/attach` was typed blind and does.
type placeDoor uint8

const (
	// placeFromPicker is /folder's enter.
	placeFromPicker placeDoor = iota
	// placeFromAttach is `/attach <dir>`, which used to refuse.
	placeFromAttach
)

// chosenPlace is one directory, chosen. It is the whole of what a door hands
// over, and it is a struct rather than a string so that a field added by a
// later lane — the mode a person said out loud, the evidence rung it came from
// — does not have to change five call sites to arrive.
type chosenPlace struct {
	// Path is the directory: absolute, cleaned, and stat'd as a directory on
	// THIS machine at the moment it was chosen. Nothing downstream re-checks it,
	// and nothing downstream should.
	Path string
	// Door is the road it came in by.
	Door placeDoor
}

// referPlace is THE SEAM. See this file's header.
//
// IT REACHES THE CONVERSATION BEFORE IT SAYS ANYTHING, and that is the whole
// change this wave made to it. It used to assert the door and, where the
// assertion failed, say `folder · <path>` anyway — so a build whose engine could
// not remember a place printed the same green sentence as one that could, and a
// person who chose a folder, closed the terminal and came back found the
// conversation had never heard of it (the session observed doing exactly that
// had no `Places` on its meta at all). A capability that cannot work is ABSENT,
// not broken: with no door the picker never opens ([app.canReferPlace]), and if
// one is somehow reached anyway the refusal is what gets said.
func (a *app) referPlace(chosen chosenPlace) {
	path := strings.TrimSpace(chosen.Path)
	if path == "" {
		return
	}
	door, ok := a.placeDoor()
	if !ok {
		a.note(folderNoDoorWord)
		a.touch()
		return
	}
	// ReferPlace snaps the path to its repository root and stamps the
	// conversation's meta, so the next task about this folder finds it at the
	// SAID rung and nobody is asked. The refusal IS the line when it comes, and
	// no pick is kept for a place the conversation did not gain.
	ref, err := door.ReferPlace(path, session.PlaceSaid)
	if err != nil {
		a.noteFacts(err.Error())
		a.touch()
		return
	}
	chose := path
	path = ref.Path
	a.placeChosen = path
	a.keepFolderPick(path)
	shown := tildePath(path, a.tilde)
	// THE PATH IS THE WHOLE OF THIS LINE (payload.go). `folder ·` is a label a
	// person already knows they asked for; the path is the one thing here they
	// cannot see anywhere else at this moment, so it steps to ink and the label
	// stays dim — which is exactly how /workspace and /model say their answers.
	//
	// AND WHERE THE TWO DIFFER, THE SENTENCE SAYS SO. A directory inside a
	// repository comes back as the repository, because work is cut from a
	// repository and not from a folder inside one — and a surface that quietly
	// printed the root a person had not chosen would be misreporting the scope
	// they actually picked.
	// The engine's own word for what was pointed at is preferred over ours
	// ([placeScope]); ours is what is left when it has nothing to say.
	if scope := placeScope(ref); scope != "" {
		chose = scope
	}
	line := folderChoseWord + shown
	if chose != path {
		line += folderInsideWord + tildePath(chose, a.tilde)
	}
	a.noteFacts(line, shown)
	a.touch()
}

// folderChoseWord leads the one line a chosen folder says.
const folderChoseWord = "folder · "

// folderInsideWord is the clause added when the folder somebody picked sits
// inside a repository and the repository is what the conversation gained.
const folderInsideWord = " · the repository holding "

// folderNoDoorWord is every road onto a folder, on a conversation that cannot
// hold one. It says what would be missing rather than naming a method: the
// point of choosing a folder is that the next request knows about it, and a
// conversation that cannot remember one would gain nothing at all.
const folderNoDoorWord = "this conversation cannot be given a folder · it has no way to remember one, so nothing would reach the next request"

// placeDoor is the slice of the agent that remembers what the conversation is
// about, and whether this build has one at all.
func (a *app) placeDoor() (placeReferrer, bool) {
	door, ok := a.agent.(placeReferrer)
	return door, ok
}

// placeCapable is the engine's OWN STATEMENT about whether it can keep the
// folders a conversation is about, made at the door and re-read after /new,
// /resume and a reconnect (internal/remote's `Welcome.Folders`).
//
// IT EXISTS BECAUSE A TYPE ASSERTION CANNOT ANSWER THE QUESTION. The ordinary
// local launch goes through the wire too, and `*remote.Agent` carries
// ReferPlace, Places and RemovePlace whatever is on the far end of the pipe —
// so [placeReferrer] is satisfied by every connection there has ever been,
// including one whose engine has never heard of a folder. Asserting the methods
// proves the CLIENT has them; only the far side can say whether they do
// anything.
type placeCapable interface{ KeepsFolders() bool }

// canReferPlace is what every road onto a folder asks BEFORE it offers one: the
// methods, and then — where anything is willing to say — the answer to whether
// they will do anything.
//
// An agent that makes no such statement is believed, because that is what the
// in-process session is: it has the methods and it is the engine, so there is
// nobody else to ask.
func (a *app) canReferPlace() bool {
	if _, ok := a.placeDoor(); !ok {
		return false
	}
	if says, ok := a.agent.(placeCapable); ok {
		return says.KeepsFolders()
	}
	return true
}

// ── the command ─────────────────────────────────────────────────────────────

// folderRemoteWord is /folder over a connection. The folders this process can
// read are the laptop's and the conversation is on the other machine, so every
// row this list could draw would be somewhere the work cannot go — which is the
// same fault [app.composerDestinations] already refuses to commit. The design's
// own ruling is that a far place is a wire door of its own and belongs to a
// later wave; until then this says so in one sentence rather than offering a
// list that lies.
const folderRemoteWord = "choosing a folder is not available over --host yet — the folders here are this machine's, not the ones the conversation is on."

// THERE IS NO LONGER A REFUSAL FOR AN EMPTY MACHINE. `folderEmptyWord` said
// `nothing to offer yet · type a path after /folder` on a profile whose
// remembered places were empty — a fresh install, which is the one person least
// able to type the path — and it was reached because the sheet opened on a LIST
// of remembered places rather than on the tree. It opens on the tree now
// ([app.contextStart]), and a machine with nothing remembered has a home
// directory like every other machine, so there is nothing left to refuse.

// openFolderPick is /folder: the ONE context browser, opened with FOLDER
// INTENT. openContextPick is the same sheet opened by a bare /attach, with file
// intent. See [app.openContextPick] for what the intent does and does not
// decide.
func (a *app) openFolderPick(query string) tea.Cmd {
	return a.openContextPick(query, true)
}

// openContextPick is the browser, opened FROM MEMORY. The only work on this path
// is ranking candidates that were already known; everything that touches a disk
// — the picks and the index of repositories under `~`, the facts about the row
// the cursor lands on, the levels the columns draw and the preview beside them —
// is asked for as a command and arrives later.
//
// ── WHAT THE INTENT DECIDES, AND WHAT IT DOES NOT ──────────────────────────
//
// ONE SHEET SHOWS BOTH KINDS WHICHEVER DOOR OPENED IT, which is the owner's
// requirement in as many words: a person deciding what this conversation is
// about is looking at a repository and a log inside it at the same time, and two
// browsers would have made them choose the door before they had seen the thing.
// So the intent decides exactly one thing — WHETHER A FOLDER DOOR IS REQUIRED
// TO OPEN AT ALL:
//
//   - /folder is a request to give the conversation a directory, and a
//     conversation that cannot hold one is told so instead of being handed a
//     browser it cannot choose from ([app.canReferPlace]).
//   - a bare /attach is a request to put a file on the next message, which
//     needs no folder door whatever. The sheet opens; a folder row on it then
//     refuses with the same sentence when it is confirmed (folderact.go).
func (a *app) openContextPick(query string, folders bool) tea.Cmd {
	if a.hosted() {
		a.note(folderRemoteWord)
		return nil
	}
	// AND THE CAPABILITY IS ASKED FOR BEFORE THE LIST IS BUILT. `--host` is not
	// the only way to reach a conversation this program is not itself running:
	// a LOCAL engine reached down the same wire has an empty far hostname, so
	// [app.hosted] is false for it and the picker used to open, rank a hundred
	// directories and print `folder · …` at a session that could not hold one.
	// A browser you cannot choose from is not offered at all.
	if folders && !a.canReferPlace() {
		a.note(folderNoDoorWord)
		return nil
	}
	// Home has its own composer. Reveal the conversation's browser without
	// changing the unsent draft suspended underneath that page.
	a.closeHome()
	a.folder.start(a.folderCandidates(), a.tilde)
	a.markFolderHeld()
	if query = strings.TrimSpace(query); query != "" {
		// A QUERY SEEDS THE SHEET AND DECIDES NOTHING ELSE. A path opens the
		// columns on it and a word puts the ranked list up, which is
		// [folderPick.rank]'s one question asked once.
		a.folder.filter.setText(query)
		a.folder.rank()
		if a.folder.browsing {
			a.folder.browseSync(a.resolvePath)
		}
	} else if root := a.contextStart(); root != "" {
		// BOTH DOORS OPEN BROWSING, AT THE SAME DIRECTORY, AND THIS IS THIS
		// WAVE'S OWN REPAIR.
		//
		// They used to open differently and the owner met the difference: `/attach`
		// browsed the columns immediately and `/folder` showed a flat list of
		// remembered PLACES, so one command answered "what is here" and the other
		// answered "where have you been" — two surfaces behind one sheet. On a
		// machine with nothing remembered `/folder` refused outright rather than
		// showing a person the folder they were standing in.
		//
		// The list is not gone: it is what the box shows the moment there is a word
		// in it rather than a path, and ctrl+u clears the path to reach it. So the
		// remembered places, the projects and the index are still one keystroke
		// away — they are simply no longer the first thing a person meets instead
		// of their own tree.
		a.folder.openAt(root, "")
	}
	a.touch()
	return tea.Batch(a.askFolderStore(), a.folderWork())
}

// contextStart is WHERE THE CHOOSER OPENS, and it is one answer for both doors.
//
// The ladder is what a person most likely means: the folder this conversation is
// already about, then the directory this window is working in, then home. Every
// rung is somewhere they have already been — the sheet never opens on `/`, which
// is a place nobody was and every walk away from it is four keystrokes.
//
// NOTHING HERE STATS ANYTHING. A directory that has been moved since it was
// remembered opens a column that says it cannot be read, which is the honest
// answer and costs one row; a stat on a disconnected mount is exactly the wait
// this surface must never take on the way to its first frame.
func (a *app) contextStart() string {
	for _, path := range a.referredPlaces() {
		if strings.TrimSpace(path) != "" {
			return path
		}
	}
	if root := a.pathRoot(); root != "" {
		return root
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}

// folderKey routes one keypress while the picker owns the keyboard. The input
// box is suspended for the duration — its draft is untouched and comes back
// whole on esc or enter.
//
// ESC CHANGES NOTHING, which is the palette's law and this list keeps it: the
// draft, the workspace and the conversation are exactly as they were, and the
// only thing that happened is that a list was up for a moment.
func (a *app) folderKey(msg tea.KeyPressMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		a.folder.close()
		a.touch()
		return nil

	case "enter":
		return a.folderConfirm()

	default:
		browsed := a.folder.navigate(msg)
		if a.folder.browsing && !browsed {
			a.folder.browseSync(a.resolvePath)
		}
		// THE COMPONENT'S ONE SENTENCE IS SAID BY THE DOOR, for the reason
		// [folderPick.say] gives: a conversation's lines belong to the
		// conversation, and a modal list that wrote into it directly would be a
		// second door onto the transcript.
		if say := a.folder.say; say != "" {
			a.folder.say = ""
			a.note(say)
		}
		a.touch()
		return a.folderWork()
	}
}

// markFolderHeld tells the open sheet which of its rows the conversation is
// already about, so the action row says the truth about the one under the
// cursor. It is called where the answer can have changed — the open, and a
// folder coming off — and nowhere in a paint.
func (a *app) markFolderHeld() {
	if !a.folder.open {
		return
	}
	held := map[string]bool{}
	for _, ref := range a.attachedPlaces() {
		held[folderScopePath(ref)] = true
	}
	a.folder.held = held
}

// folderWork is everything the browser wants read after a gesture: the facts
// about the row it has stopped on, the levels its columns are drawing, and the
// PREVIEW of the thing under the cursor. All three go off the loop, and all
// three come back through a cache keyed by path.
func (a *app) folderWork() tea.Cmd {
	return tea.Batch(a.askFolderFacts(), a.askFolderKids(), a.askFolderPreview())
}

// askFolderPreview is the preview pane's one call, and it is
// contextpreview.go's own seam doing the work (reports/preview.md's recipe).
//
// EVERY MOVE ASKS, INCLUDING ONE THAT LANDS BACK ON A FILE ALREADY READ. The
// pump cancels the read nobody wants any more, stamps this one with its own
// generation, and validates the cached identity INSIDE the reader — so a cache
// hit costs a goroutine and no disk wait on the loop, and a file rewritten under
// the cursor is re-read rather than drawn from a stale entry. Nothing here stats
// anything: a stat on a disconnected mount is exactly the wait this surface must
// never take (coordinator pass 11).
//
// THE PANE IS EMPTIED THE MOMENT THE CURSOR MOVES. Holding the last file's
// preview until the next one arrives would draw one file's source under another
// file's name for a frame, which is the one thing a preview may not do.
func (a *app) askFolderPreview() tea.Cmd {
	if !a.folder.open || !a.folder.browsing {
		a.folder.previews.stop()
		a.folder.preview = filePreview{}
		return nil
	}
	path, _, ok := a.folder.hereKind()
	if !ok {
		a.folder.previews.stop()
		a.folder.preview = filePreview{}
		return nil
	}
	// A HOSTED SESSION'S PATH IS RESOLVED THROUGH THE DOOR THAT ALREADY HAS THE
	// MIRROR (imagepreview.go's [app.readPathFor]), so a preview reads the file
	// this process can actually open. `/folder` refuses over --host today
	// ([folderRemoteWord]) so this is the local answer in every reachable case,
	// and it is written this way so the remote door does not have to find it.
	read, ok := a.readPathFor(path, true)
	if !ok {
		a.folder.previews.stop()
		a.folder.preview = filePreview{}
		return nil
	}
	pv, cmd := a.folder.previews.show(previewRequest{
		Path:     read,
		Pictures: a.pal.paintsPictures(),
		Hidden:   a.folder.hidden,
	})
	a.folder.preview = pv
	return cmd
}

// tookFolderPreview files a preview, or drops it.
//
// `!ok` IS THE ORDINARY CASE AND NOT AN ERROR: the person moved on, the terminal
// was re-measured, or the hidden key was pressed, and the answer is about a
// moment that has passed (contextpreview.go's [previewPump.took]).
func (a *app) tookFolderPreview(msg previewLoadedMsg) {
	if !a.folder.open {
		return
	}
	pv, ok := a.folder.previews.took(msg)
	if !ok {
		return
	}
	a.folder.preview = pv
	a.touch()
}

// folderGoneWord is the add action on a row whose directory is no longer there.
const folderGoneWord = "no such folder · "

// ── the pointer ─────────────────────────────────────────────────────────────

// folderRowPress resolves a press on ONE ROW OF THE SHEET, in the sheet's own
// coordinates: row counted from [folderPick.rows]' first row and x counted from
// its first cell. Where that row landed on the screen is the modal's business
// and only the modal's ([contextWin], contextmodal.go) — this function is the
// browser's own layout answering about itself, which is what keeps one hit map.
//
// THE GESTURES ARE FINDER'S, and they are two and not one. A click on a NAME
// walks — into the list row's folder, along the breadcrumb, out to the parent —
// or moves the cursor onto it, because that is what clicking a name in a column
// of names has meant for forty years. A click on the ACTION ROW is the only
// thing on this sheet that commits, and a click on a MARK CELL takes that mark
// off. Nothing here both moves and commits, and NOTHING HERE ATTACHES BY
// ITSELF.
//
// A SECOND PRESS ON THE ROW YOU ARE ALREADY ON OPENS IT, when it is a folder.
// That is how the pointer walks INTO a tree: the first press selects and the
// second opens, which is the gesture every file browser has taught, without this
// surface having to measure the gap between two clicks.
//
// AND A PRESS IN THE PREVIEW COLUMN MOVES THE SELECTION THERE, when the preview
// is a folder listing. That column used to consume presses and do nothing at
// all — a list of directory names, drawn exactly like the column beside it,
// answering to no pointer — which is the defect this wave exists for. The rule
// is the SAME rule the middle column keeps, one column to the right: the press
// selects, and a second press on what is now the cursor row opens it. So the
// scheme is one scheme in both columns and there is no double-click timing
// anywhere on this sheet.
func (a *app) folderRowPress(x, row int) (tea.Cmd, bool) {
	if !a.folder.open {
		return nil, false
	}
	geom := a.folder.geom
	if row == geom.action {
		return a.folderConfirm(), true
	}
	if row == geom.tray {
		if a.folder.trayPress(x) {
			a.touch()
		}
		return nil, true
	}
	if !a.folder.browsing {
		// A LIST ROW OPENS FOR BROWSING, which is the whole answer to "the path
		// I searched for is on the screen and I have to type it out again".
		at := row
		if row >= 0 && row < len(geom.owner) {
			at = geom.owner[row]
		}
		if at < 0 || at >= len(a.folder.hits) {
			return nil, true
		}
		a.folder.cursor = at
		a.folder.openAt(a.folder.all[a.folder.hits[at]].path, "")
		a.touch()
		return a.folderWork(), true
	}
	if geom.head > 0 && row == 0 {
		for _, crumb := range geom.crumbs {
			if crumb.span.holds(x) {
				a.folder.openAt(crumb.path, filepath.Base(a.folder.cols.dir))
				a.touch()
				return a.folderWork(), true
			}
		}
		return nil, true
	}
	at := row - geom.head
	if at < 0 || at >= geom.body {
		return nil, true
	}
	switch {
	case geom.up.holds(x):
		// The ancestry column: the row pressed is counted from the parent column's
		// own window, exactly as it was drawn ([folderGeom.upFrom]). Only a
		// DIRECTORY over there is somewhere to go — a file in the parent is drawn
		// for context and pressing it would leave the columns nowhere.
		want := at + geom.upFrom
		up := a.folder.cols.up
		if a.folder.cols.upAt < 0 || !up.isDir(want) {
			return nil, true
		}
		a.folder.openAt(filepath.Dir(a.folder.cols.dir), up.rowName(want))
		a.touch()
		return a.folderWork(), true
	case geom.pane.holds(x) && a.folder.pane != folderPaneOff:
		// THE PREVIEW COLUMN, when what it is previewing is a FOLDER. The row
		// pressed is the entry drawn on it, and the entry's RAW name is what is
		// joined onto the previewed directory — never the drawn one, which has been
		// through [drawableLine] and is a label rather than a path
		// ([previewEntry.Raw] states this at length).
		//
		// A file's source, a picture and every refusal are read-only over here and
		// take no press: they have no rows to select.
		name, ok := a.folder.paneEntry(at)
		if !ok {
			return nil, true
		}
		a.folder.openAt(a.folder.geom.paneDir, name)
		a.touch()
		return a.folderWork(), true
	}
	// The middle column, and every column-less frame: the press moves the cursor,
	// or — on the row the cursor is already on — walks into it (this function's
	// header states that gesture).
	want := at + a.folder.cols.top
	if want < 0 || want >= a.folder.cols.here.rows() {
		return nil, true
	}
	if want == a.folder.cols.cursor && a.folder.cols.here.isDir(want) {
		a.folder.descend()
		a.touch()
		return a.folderWork(), true
	}
	a.folder.cols.cursor = want
	a.folder.paneRest()
	a.touch()
	return a.folderWork(), true
}

// folderHoverColumn resolves a pointer on the sheet to WHICH COLUMN it is over,
// and reports whether the sheet is drawing columns at all.
//
// It exists because what lights has to be what a press acts on (hover.go's law),
// and on this sheet that is a question about x: the three columns walk out, move
// the cursor and walk in. The action row and a candidate row are whole-row
// targets and answer [folderColRow], which is the same answer every other list
// down here gives.
func (a *app) folderHoverColumn(x, row int) (string, bool) {
	if !a.folder.open {
		return "", false
	}
	geom := a.folder.geom
	if row == geom.tray {
		// WHICH CELL, and not merely which row: every cell up there takes a
		// different thing off the confirm, so a band across the row would offer to
		// unchoose the file beside the one a person is aiming at (hover.go's law,
		// and attach.go's own tray keeps it the same way).
		a.folder.trayHot = -1
		for at, span := range geom.trayCells {
			if span.holds(x) {
				a.folder.trayHot = at
				break
			}
		}
		return folderColTray, true
	}
	a.folder.trayHot = -1
	a.folder.paneHot = -1
	if !a.folder.browsing || row == geom.action {
		return folderColRow, true
	}
	if geom.head > 0 && row == 0 {
		return folderColCrumb, true
	}
	switch {
	case geom.up.holds(x):
		return folderColUp, true
	case geom.pane.holds(x):
		// WHICH ROW OF THE PANE, and not merely which column, wherever that row is
		// a thing a press acts on. The pane keeps answering for its own column
		// either way — the wheel scrolls a file's source over there and a source
		// has no rows to light — so the column is the answer and the row is the
		// extra fact (hover.go's law: what lights is what a press acts on).
		if at := row - geom.head; at >= 0 && at < geom.paneBody {
			a.folder.paneHot = at
		}
		return folderColPane, true
	}
	return folderColHere, true
}

// The six things the pointer can be over on this sheet. They are the hover's
// own alphabet and nothing else reads them.
const (
	folderColRow   = "row"
	folderColCrumb = "crumb"
	folderColUp    = "up"
	folderColHere  = "here"
	folderColPane  = "pane"
	folderColTray  = "tray"
)

// ── the candidates, in layers ───────────────────────────────────────────────

// folderCandidates is the layered set, best first, deduplicated by path with
// the FIRST layer that names a directory keeping it. Nothing here touches the
// filesystem: every source is something this surface already holds.
func (a *app) folderCandidates() []folderCand {
	var out []folderCand
	seen := map[string]bool{}
	add := func(layer folderLayer, paths []string) {
		for _, path := range paths {
			path = strings.TrimSpace(path)
			if path == "" || !filepath.IsAbs(path) {
				continue
			}
			path = filepath.Clean(path)
			if seen[path] {
				continue
			}
			seen[path] = true
			out = append(out, folderCand{
				path:  path,
				show:  tildePath(path, a.tilde),
				layer: layer,
				rank:  len(out),
				freq:  folderFrecency(a.folderStore.Picks[path], a.now()),
			})
		}
	}
	add(folderReferred, a.referredPlaces())
	add(folderTouched, a.touchedRoots())
	add(folderProject, a.composerDestinations())
	add(folderIndexed, a.folderStore.Roots)
	return out
}

// placeReferrer is the slice of the agent that remembers what the conversation
// is about — session.Agent's accrual door (internal/session/places.go). It is
// asserted rather than added to [Agent], because a scripted test agent and a
// hosted connection honestly lack it, and a capability that cannot work is
// absent, not broken.
//
// THIS IS THE INTERFACE THE WIRE HAS TO MEET, spelled here so there is one
// place to read it: a connection that carries `ReferPlace` and `Places` with
// these exact signatures gets the whole folder surface, and one that does not
// gets [folderNoDoorWord] instead of a picker that lies.
type placeReferrer interface {
	ReferPlace(path string, arrival session.PlaceArrival) (session.PlaceRef, error)
	Places() []session.PlaceRef
}

// placeRemover is the OTHER half of the same door, asserted separately and
// deliberately so.
//
// The two arrive on their own timetables — the local agent had the first long
// before anything could take a folder back off — and a surface that demanded
// both at once would have withdrawn a working picker to wait for a button. So
// the indicator draws whatever the conversation is about either way, and offers
// to take a folder off ONLY where there is something behind the offer
// (folderchip.go).
type placeRemover interface {
	RemovePlace(path string) error
}

// referredPlaces are the directories this conversation is already ABOUT, most
// recently referred first — the session's own answer, read off the meta the
// accrual door stamps. This is the one function on this side that reads it,
// exactly as the hook promised when it answered nothing.
func (a *app) referredPlaces() []string {
	door, ok := a.placeDoor()
	if !ok {
		return nil
	}
	refs := door.Places()
	if len(refs) == 0 {
		return nil
	}
	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		paths = append(paths, folderScopePath(ref))
	}
	return paths
}

// touchedRoots are the directories this conversation's own tool calls named,
// MOST RECENT FIRST — the recency weighting the design asks for, said as an
// order rather than as a number, because a list a person reads down is already
// ordered and a score printed beside a folder would be machinery in their face.
//
// IT READS WHAT THIS SURFACE ALREADY DREW and never the disk. Every entry here
// is a tool call whose target names a path ([targetField]), resolved the way a
// person means it ([app.resolvePath]); a call pointed at a file contributes the
// directory holding it, and `ls` contributes the path itself. NOTHING IS
// STAT'D, which is what makes this affordable on the open: a guess that names
// somewhere that is not there costs one dim row and is caught by the stat enter
// does.
func (a *app) touchedRoots() []string {
	var out []string
	seen := map[string]bool{}
	for at := len(a.entries) - 1; at >= 0; at-- {
		e := &a.entries[at]
		if targetField[e.tool] != "path" {
			continue
		}
		target := strings.TrimSpace(argString(argsOf(e.detail.Args), "path"))
		if target == "" {
			continue
		}
		dir := a.resolvePath(target)
		// `ls` is pointed AT a directory and everything else at a file inside
		// one. Reading the tool rather than the string is what keeps this from
		// guessing about a path with a dot in its name.
		if e.tool != "ls" {
			dir = filepath.Dir(dir)
		}
		dir = filepath.Clean(dir)
		if dir == "" || dir == "." || dir == string(filepath.Separator) || seen[dir] {
			continue
		}
		seen[dir] = true
		out = append(out, dir)
		if len(out) >= folderTouchedCap {
			break
		}
	}
	return out
}

// folderTouchedCap bounds the touched layer. Twelve is one screenful of this
// list, and a conversation that has read four hundred files has not been about
// four hundred places — the tail of that walk is noise sitting on top of the
// projects a person actually chooses between.
const folderTouchedCap = 12

// ── what the machine knows about one directory ──────────────────────────────

// folderFactsMsg is one directory's facts, coming BACK. It carries the path it
// is about because several may be in flight — a cursor swept down a list asks
// about every row it rests on — and an answer filed against whichever row
// happened to be under the cursor when it arrived would be a branch name from
// another project (homeband_repo.go's [homeRepoMsg] states this law).
type folderFactsMsg struct {
	path  string
	lines []string
}

// tookFolderFacts files that answer.
func (a *app) tookFolderFacts(msg folderFactsMsg) {
	delete(a.folderAsking, msg.path)
	if a.folder.facts == nil {
		a.folder.facts = map[string][]string{}
	}
	a.folder.facts[msg.path] = msg.lines
	a.touch()
}

// askFolderFacts asks about the row the cursor is on, and only that one. It is
// a tea.Cmd and not a syscall for homeband_repo.go's stated reason — A
// KEYSTROKE MAY NOT WAIT FOR git — and one ask per path is in flight at a time,
// so holding `↓` down a list does not fork a git per row.
func (a *app) askFolderFacts() tea.Cmd {
	path := a.folder.hereForFacts()
	if path == "" {
		return nil
	}
	if _, known := a.folder.facts[path]; known {
		return nil
	}
	if a.folderAsking == nil {
		a.folderAsking = map[string]bool{}
	}
	if a.folderAsking[path] {
		return nil
	}
	a.folderAsking[path] = true
	return func() tea.Msg { return folderFactsMsg{path: path, lines: folderFactsOf(path)} }
}

// ── the levels the columns draw ─────────────────────────────────────────────

// folderKidsMsg is ONE directory's subdirectories, coming BACK.
//
// It carries the path it is about AND the opening that asked for it. The path
// is what files it against the right level — several are in flight whenever a
// cursor sweeps a column — and the generation is the cancellation this design
// asks for: a readdir of a slow network mount answers long after somebody
// pressed esc, and filing it into the picker they opened afterwards would put
// one directory's names in another directory's column.
type folderKidsMsg struct {
	path string
	read folderListing
	gen  int
}

// tookFolderKids files that answer, or drops it.
func (a *app) tookFolderKids(msg folderKidsMsg) tea.Cmd {
	if msg.gen != a.folder.gen {
		return nil
	}
	if !a.folder.took(msg.path, msg.read) {
		return nil
	}
	a.touch()
	// A LEVEL ARRIVING CAN MAKE THE NEXT ONE WORTH ASKING FOR — the parent of
	// the level that just landed is not known until the level is, and neither is
	// the row the cursor seats onto, which is what the preview is about. That is
	// the whole of the read-ahead: one step, on an answer, and never a walk.
	return tea.Batch(a.askFolderKids(), a.askFolderFacts(), a.askFolderPreview())
}

// askFolderKids reads the levels the columns want and have not got — at most
// three, and never more than one command per directory at a time.
//
// EVERY READDIR ON THIS SURFACE GOES THROUGH HERE. A keystroke may not wait for
// a disk: a home directory on a network mount, a folder with forty thousand
// entries in it, an unresponsive automount are all things `os.ReadDir` can sit
// on for seconds, and the browser has to keep drawing and keep taking keys
// while it does.
func (a *app) askFolderKids() tea.Cmd {
	wanted := a.folder.wanted()
	if len(wanted) == 0 {
		return nil
	}
	if a.folder.asking == nil {
		a.folder.asking = map[string]bool{}
	}
	gen, hidden := a.folder.gen, a.folder.hidden
	cmds := make([]tea.Cmd, 0, len(wanted))
	for _, dir := range wanted {
		a.folder.asking[dir] = true
		cmds = append(cmds, func() tea.Msg {
			return folderKidsMsg{path: dir, read: folderEntries(dir, hidden), gen: gen}
		})
	}
	return tea.Batch(cmds...)
}

// The three facts, and the words they are said in. They are constants because
// each is quoted in the manual exactly as it is spelled here.
// folderChangedWord leads a file's modification time on the action row. It is a
// constant because the manual quotes it exactly as it is spelled here.
const folderChangedWord = "changed "

const (
	folderRepoWord  = "repository"
	folderPlainWord = "folder"
	folderCleanWord = "clean"
	folderDirtyWord = "dirty"
	folderAgentWord = "AGENTS.md"
)

// folderAgentsFile is the file whose PRESENCE is the third fact. It is named
// once because the manual quotes it and the check reads it.
const folderAgentsFile = "AGENTS.md"

// folderFactsOf is what the machine knows about one directory, one line per
// fact, cheap stats only. It runs off the loop.
//
// THE EMPTINESS LAW RUNS THROUGH IT. A repository whose head has no name says
// `repository` and stops; a folder with nothing in it says `folder` and stops;
// a directory with no AGENTS.md says nothing about AGENTS.md. Nowhere does this
// draw a zero, a blank or an "unknown".
func folderFactsOf(dir string) []string {
	info, err := os.Stat(dir)
	if err != nil {
		return nil
	}
	if !info.IsDir() {
		// A FILE'S FACTS ARE THE TWO A CHOOSER CAN ANSWER: how big it is, and when
		// it last changed. They ride the same right end of the action row a
		// directory's do [owner review 2026-09-08 §4: metadata answers the SELECTED
		// item rather than cluttering every row], and neither is drawn where the
		// disk had nothing to say — a zero-byte file says its size and a file with
		// no modification time says nothing at all [design-law §EMPTINESS].
		//
		// IT IS THE MODIFICATION TIME AND IT IS NAMED AS ONE. It is not "last
		// opened": atime is a lie on every filesystem mounted `relatime`, and
		// aforge keeps no record of opening a file it merely previewed.
		var lines []string
		if size := byteWord(int(info.Size())); size != "" {
			lines = append(lines, size)
		}
		if when := reltime.Short(info.ModTime(), time.Now()); when != "" {
			lines = append(lines, folderChangedWord+when)
		}
		return lines
	}
	var lines []string
	if branch, dirty, ok := folderRepoOf(dir); ok {
		line := folderRepoWord
		if branch != "" {
			line += " · " + branch
		}
		if dirty {
			line += " · " + folderDirtyWord
		} else {
			line += " · " + folderCleanWord
		}
		lines = append(lines, line)
	} else {
		line := folderPlainWord
		if files := folderFileCount(dir); files > 0 {
			line += " · " + strconv.Itoa(files) + plural(" file", files)
		}
		lines = append(lines, line)
	}
	if _, err := os.Stat(filepath.Join(dir, folderAgentsFile)); err == nil {
		lines = append(lines, folderAgentWord)
	}
	return lines
}

// folderRepoOf is the head one directory is on and whether its tree is dirty,
// and false for a directory that is not a repository at all.
//
// It goes through [homeGitStatus] — the seam home's own repository band uses —
// so a test can pin a branch without a repository on disk, and so there is ONE
// git invocation in this package rather than two that could drift about which
// flags they pass.
func folderRepoOf(dir string) (string, bool, bool) {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return "", false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	raw, err := homeGitStatus(ctx, dir)
	if err != nil {
		// The `.git` is there and git would not answer. It is still a
		// repository, and saying so with nothing after it is more honest than
		// calling it a folder.
		return "", false, true
	}
	line, branch := parseHomeRepo(string(raw))
	return branch, strings.Contains(line, folderDirtyWord), true
}

// folderFileCount is how many things are directly under a directory, hidden
// entries excluded — ONE readdir, nothing deep. It is the cheap stat the design
// asks for and deliberately not a recursive count: a number that took a minute
// to reach would be a number nobody waited for.
func folderFileCount(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), ".") {
			count++
		}
	}
	return count
}

// ── what is kept, and the index under ~ ─────────────────────────────────────

// folderStoreName is the file under the aforge state root, moved wholesale by
// AFORGE_HOME the way every other file aforge writes is.
var folderStoreName = []string{"v3", "folders.json"}

func folderStorePath() string { return home.Join(folderStoreName...) }

// folderStore is what this picker remembers between launches: how often each
// directory was chosen and when, and the repositories under `~` as the last
// scan found them.
//
// IT IS ONE FILE AND IT IS SMALL. The picks are a few dozen paths and two
// numbers each; the roots are a list of directory names. Both are a cache of
// answers and neither is configuration — a machine that loses this file loses
// the first row's head start and nothing else, which is why every failure to
// read it is answered with an empty store rather than with a complaint.
type folderStore struct {
	Picks map[string]folderPickCount `json:"picks,omitempty"`
	Roots []string                   `json:"roots,omitempty"`
	// Scanned is when the roots were walked. It is what stops the walk from
	// running on every launch, and what makes it run again eventually.
	Scanned time.Time `json:"scanned,omitempty"`
}

// folderPickCount is one directory's history on this list: how many times it
// was chosen, and when it last was.
type folderPickCount struct {
	N  int       `json:"n"`
	At time.Time `json:"at"`
}

// folderFrecency is recency × frequency — the zoxide model, and the reason a
// week of use makes `/folder` `enter` the answer rather than the start of a
// search. A directory nobody has chosen scores zero and falls back on its
// layer's own order, which is exactly right on the first day.
//
// The four bands are hours, a day, a week, and older. They are wide on purpose:
// a curve fine enough to reorder the top of the list between two presses of a
// key would be a list that moves under somebody's hand.
func folderFrecency(pick folderPickCount, now time.Time) float64 {
	if pick.N <= 0 || pick.At.IsZero() {
		return 0
	}
	weight := 0.25
	switch age := now.Sub(pick.At); {
	case age < time.Hour:
		weight = 4
	case age < 24*time.Hour:
		weight = 2
	case age < 7*24*time.Hour:
		weight = 0.5
	}
	return float64(pick.N) * weight
}

// keepFolderPick writes one choice down: in memory at once, so the very next
// `/folder` is already ordered by it, and on disk off the loop.
func (a *app) keepFolderPick(path string) {
	if a.folderStore.Picks == nil {
		a.folderStore.Picks = map[string]folderPickCount{}
	}
	pick := a.folderStore.Picks[path]
	pick.N, pick.At = pick.N+1, a.now()
	a.folderStore.Picks[path] = pick
	store := a.folderStore
	// The write is fired and not waited on, and its error is dropped — which is
	// honest rather than lazy: nothing on screen claims the pick was saved. The
	// line says `folder · <path>`, which is true of this conversation whatever
	// the disk did.
	go func() { _ = writeFolderStore(store) }()
}

// folderStoreMsg carries the store back to the loop, refreshed if it was stale.
type folderStoreMsg struct{ store folderStore }

// tookFolderStore files it, and re-ranks an open list against it — the picks
// arriving are what turn "the order the sources handed these over" into "the
// order you actually use them".
func (a *app) tookFolderStore(msg folderStoreMsg) tea.Cmd {
	a.folderStore = msg.store
	a.folderStoreRead = true
	if !a.folder.open {
		return nil
	}
	// THE FILTER, THE CURSOR AND EVERYTHING ALREADY READ SURVIVE. Somebody who
	// typed three characters while the store was in flight has not stopped
	// typing, and a list that reset itself under them would be the background
	// reaching into their hands. The caches survive for a sharper version of the
	// same reason: they are answers about directories, and nothing about a
	// directory changed because a file of pick counts arrived — re-asking for
	// them would flush three columns a person is looking at and redraw them a
	// frame later.
	//
	// AND SO DO THE MARKS AND THE PANE, which is this wave's own repair to this
	// function. [folderPick.start] builds a FRESH sheet, so everything not named
	// here is thrown away — and the things a person had CHOSEN, along with which
	// state the preview pane was in and how far they had scrolled it, were being
	// silently discarded by a file of pick counts landing a second after the
	// sheet opened. The one thing that may not survive a re-`start` is the
	// preview itself, because start cancels the read in flight; the ask is
	// therefore returned as a command and made again.
	filter, cursor := a.folder.filter, a.folder.cursor
	facts, kids, asking := a.folder.facts, a.folder.kids, a.folder.asking
	hidden, gen, cols := a.folder.hidden, a.folder.gen, a.folder.cols
	marks, pane := a.folder.marks, a.folder.pane
	paneTop, paneLeft := a.folder.paneTop, a.folder.paneLeft
	a.folder.start(a.folderCandidates(), a.tilde)
	a.folder.filter, a.folder.facts = filter, facts
	a.folder.kids, a.folder.asking, a.folder.hidden = kids, asking, hidden
	a.folder.marks, a.folder.pane = marks, pane
	a.folder.paneTop, a.folder.paneLeft = paneTop, paneLeft
	a.markFolderHeld()
	// The COLUMNS are kept whole and not re-seated: which level they are on and
	// which row of it the cursor is on are facts about where a person has walked
	// to, and a store arriving is not news about either.
	a.folder.cols = cols
	// AND SO DOES THE GENERATION, because the reads still in flight were stamped
	// with it: bumping it here would drop every column this picker had already
	// asked for and leave the sheet blank until the next keystroke asked again.
	a.folder.gen = gen
	a.folder.rank()
	a.folder.cursor = moveCursor(cursor, 0, len(a.folder.hits))
	if a.folder.browsing {
		a.folder.browseSync(a.resolvePath)
	}
	a.touch()
	// THE PREVIEW IS ASKED FOR AGAIN, and this is the whole of the repair: the
	// re-`start` above cancelled the read in flight, so a sheet whose store landed
	// a second after it opened sat with an empty pane until the person happened to
	// press a key. Caught in a real terminal capture, not in a unit test — which is
	// why the sheet is photographed as well as asserted.
	return a.askFolderPreview()
}

// askFolderStore reads the store and, when the index is stale, walks for
// repositories under `~` and writes the answer back — ALL OF IT OFF THE
// INTERACTION PATH. It runs at most once per surface: the picker opens on
// whatever is in memory, and this is what puts something there.
func (a *app) askFolderStore() tea.Cmd {
	if a.folderStoreRead || a.hosted() {
		return nil
	}
	// The flag is set before the command runs rather than when it answers, so a
	// person who opens and closes the picker three times in a second does not
	// start three walks of their home directory.
	a.folderStoreRead = true
	now := a.now()
	return func() tea.Msg {
		store := readFolderStore()
		if now.Sub(store.Scanned) > folderScanTTL {
			store.Roots, store.Scanned = scanFolderRoots(), now
			_ = writeFolderStore(store)
		}
		return folderStoreMsg{store: store}
	}
}

// folderScanTTL is how long the index of repositories under `~` is believed. A
// day is the honest figure: people clone a repository a few times a week and
// the layer under this one — the projects home already knows — catches every
// directory aforge has actually been opened in the moment it is opened there.
const folderScanTTL = 24 * time.Hour

// scanFolderRoots discovers both projects and ordinary folders within the
// shared index bounds. All of this work runs in the background command.
func scanFolderRoots() []string {
	base, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return folderIndexPaths(folderIndexWalk(context.Background(), folderIndexDefaults(base)))
}

// depth is how many levels below base a path sits.
func depth(base, path string) int {
	rel, err := filepath.Rel(base, path)
	if err != nil || rel == "." {
		return 0
	}
	return strings.Count(rel, string(filepath.Separator)) + 1
}

// readFolderStore reads the file, and answers an EMPTY STORE for every way that
// can fail — no file, no home directory, a half-written file. A picker with no
// store falls back on its layers' own order; a picker that reported a parse
// error would be answering "which folder" with a filesystem complaint
// (models.go's [CachedModels] states this rule for the model cache).
func readFolderStore() folderStore {
	raw, err := os.ReadFile(folderStorePath())
	if err != nil {
		return folderStore{}
	}
	var store folderStore
	if json.Unmarshal(raw, &store) != nil {
		return folderStore{}
	}
	return store
}

// writeFolderStore replaces the file, through a temporary so a process that
// dies mid-write leaves the previous store readable rather than half a JSON
// document — [WriteModelCache]'s pattern, for its reason.
func writeFolderStore(store folderStore) error {
	path := folderStorePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(store)
	if err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
