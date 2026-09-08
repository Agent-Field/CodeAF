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
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/session"
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
	shown := shortPath(path, a.tilde, 0)
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
	line := folderChoseWord + shown
	if chose != path {
		line += folderInsideWord + shortPath(chose, a.tilde, 0)
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

// canReferPlace is what every road onto a folder asks BEFORE it offers one.
func (a *app) canReferPlace() bool {
	_, ok := a.placeDoor()
	return ok
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

// folderEmptyWord is /folder on a machine with nothing to offer yet: no project
// home knows about, nothing touched, and an index that has not answered. It
// names the way through rather than apologising, because typing a path is the
// answer and always was.
const folderEmptyWord = "nothing to offer yet · type a path after /folder, or use the picker's box"

// openFolderPick is /folder: the picker, opened FROM MEMORY. The only work on
// this path is ranking candidates that were already known; the two things that
// touch a disk — the picks and the index of repositories under `~`, and the
// facts about the row the cursor lands on — are asked for as commands and
// arrive later.
func (a *app) openFolderPick(query string) tea.Cmd {
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
	if !a.canReferPlace() {
		a.note(folderNoDoorWord)
		return nil
	}
	candidates := a.folderCandidates()
	if len(candidates) == 0 && strings.TrimSpace(query) == "" {
		a.note(folderEmptyWord)
		return nil
	}
	a.folder.start(candidates, a.tilde)
	if query = strings.TrimSpace(query); query != "" {
		a.folder.filter.setText(query)
		a.folder.rank()
		if a.folder.browsing {
			a.folder.browseSync(a.resolvePath)
		}
	}
	a.touch()
	return tea.Batch(a.askFolderStore(), a.askFolderFacts(), a.askFolderKids())
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
		a.addFolderUnderCursor()
		return nil

	default:
		browsed := a.folder.navigate(msg)
		if a.folder.browsing && !browsed {
			a.folder.browseSync(a.resolvePath)
		}
		a.touch()
		return a.folderWork()
	}
}

// addFolderUnderCursor is THE ADD ACTION — `enter`, and the click on the row
// that says so. It is one function because the key and the row must not be able
// to drift into meaning two different things.
func (a *app) addFolderUnderCursor() {
	path, ok := a.folder.here()
	if !ok {
		a.folder.close()
		a.touch()
		return
	}
	// THE ONE STAT ON THIS SURFACE, AND IT IS ON A KEYSTROKE. The candidate
	// layers are built from what was said and remembered rather than from a
	// walk, so a row can name a directory that has since been moved or
	// deleted — and handing that path on as a place would be this surface
	// passing its own staleness downstream. A keystroke may wait for one
	// stat; nothing else here waits for anything.
	if info, err := os.Stat(path); err != nil || !info.IsDir() {
		a.note(folderGoneWord + shortPath(path, a.tilde, 0))
		a.touch()
		return
	}
	a.folder.close()
	a.referPlace(chosenPlace{Path: path, Door: placeFromPicker})
}

// folderWork is everything the browser wants read after a gesture: the facts
// about the row it has stopped on, and the levels its columns are drawing. Both
// go off the loop, and both come back through a cache keyed by path.
func (a *app) folderWork() tea.Cmd {
	return tea.Batch(a.askFolderFacts(), a.askFolderKids())
}

// folderGoneWord is the add action on a row whose directory is no longer there.
const folderGoneWord = "no such folder · "

// ── the pointer ─────────────────────────────────────────────────────────────

// folderPress resolves a click on the browser, and reports whether it took one.
//
// IT IS NOT MODAL TO THE POINTER, which is [app.harnessPickPress]'s own bargain
// and it is kept for that reason: this sheet hangs over a draft somebody is
// still writing, so a press anywhere else is a press on whatever is there rather
// than a way of closing a list they did not ask to close.
//
// THE GESTURES ARE FINDER'S, and they are two and not one. A click on a NAME
// walks — into the list row's folder, along the breadcrumb, out to the parent,
// or on into the column of children — because that is what clicking a folder in
// a column of folders has meant for forty years. A click on the ACTION ROW adds
// the folder the cursor is on. Nothing on this sheet both moves and commits.
func (a *app) folderPress(x, y int) (tea.Cmd, bool) {
	if !a.folder.open {
		return nil, false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeOverlay {
		return nil, false
	}
	row, geom := mark.index, a.folder.geom
	if row == geom.action {
		a.addFolderUnderCursor()
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
		// The parent column: the row pressed is counted from the one we are
		// standing in, exactly as it was drawn ([folderPick.upText]).
		want := at + a.folder.cols.top - a.folder.cols.cursor + a.folder.cols.upAt
		names := a.folder.cols.up.names
		if a.folder.cols.upAt < 0 || want < 0 || want >= len(names) {
			return nil, true
		}
		a.folder.openAt(filepath.Dir(a.folder.cols.dir), names[want])
		a.touch()
		return a.folderWork(), true
	case geom.kids.holds(x):
		// The children column: walking in and landing on the name pressed is one
		// act, which is what makes a click over there a step down the tree.
		child := a.folder.read(a.folder.hereForFacts())
		if at >= len(child.names) {
			return nil, true
		}
		path, ok := a.folder.here()
		if !ok {
			return nil, true
		}
		a.folder.openAt(path, child.names[at])
		a.touch()
		return a.folderWork(), true
	}
	// The middle column, and every column-less frame: the press moves the cursor
	// and nothing else, so the third column fills with what is under it.
	want := at + a.folder.cols.top
	if want < 0 || want >= len(a.folder.cols.here.names) {
		return nil, true
	}
	a.folder.cols.cursor = want
	a.touch()
	return a.folderWork(), true
}

// folderWheel turns the wheel over the browser into a walk of whichever surface
// is up, and reports whether it took the gesture. The window follows the cursor
// here rather than an offset of its own, so scrolling and selecting are one act
// — which is what the other cursor-followed lists on this surface do.
func (a *app) folderWheel(y, delta int) (tea.Cmd, bool) {
	if !a.folder.open || delta == 0 {
		return nil, false
	}
	// OVER ITS OWN ROWS AND NOWHERE ELSE, for the reason the press keeps: the
	// sheet is not modal to the pointer, and a wheel turned over the transcript
	// belongs to the transcript.
	if mark, ok := a.chromeAt(y); !ok || mark.kind != chromeOverlay {
		return nil, false
	}
	a.folder.move(delta)
	a.touch()
	return a.folderWork(), true
}

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
				show:  shortPath(path, a.tilde, 0),
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
		paths = append(paths, ref.Path)
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
	read folderRead
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
	// A LEVEL ARRIVING CAN MAKE THE NEXT ONE WORTH ASKING FOR — the children of
	// the row the cursor landed on are not known until the row is. That is the
	// whole of the read-ahead: one step, on an answer, and never a walk.
	return tea.Batch(a.askFolderKids(), a.askFolderFacts())
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
			return folderKidsMsg{path: dir, read: folderKids(dir, hidden), gen: gen}
		})
	}
	return tea.Batch(cmds...)
}

// The three facts, and the words they are said in. They are constants because
// each is quoted in the manual exactly as it is spelled here.
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
	if err != nil || !info.IsDir() {
		return nil
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
func (a *app) tookFolderStore(msg folderStoreMsg) {
	a.folderStore = msg.store
	a.folderStoreRead = true
	if !a.folder.open {
		return
	}
	// THE FILTER, THE CURSOR AND EVERYTHING ALREADY READ SURVIVE. Somebody who
	// typed three characters while the store was in flight has not stopped
	// typing, and a list that reset itself under them would be the background
	// reaching into their hands. The caches survive for a sharper version of the
	// same reason: they are answers about directories, and nothing about a
	// directory changed because a file of pick counts arrived — re-asking for
	// them would flush three columns a person is looking at and redraw them a
	// frame later.
	filter, cursor := a.folder.filter, a.folder.cursor
	facts, kids, asking := a.folder.facts, a.folder.kids, a.folder.asking
	hidden, gen, cols := a.folder.hidden, a.folder.gen, a.folder.cols
	a.folder.start(a.folderCandidates(), a.tilde)
	a.folder.filter, a.folder.facts = filter, facts
	a.folder.kids, a.folder.asking, a.folder.hidden = kids, asking, hidden
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

// The two bounds on the walk. It runs in the background and it still may not be
// a crawl of somebody's whole disk: six levels reaches ~/code/work/client/repo
// without reaching a node_modules nobody asked about, and two thousand roots is
// more repositories than any list can rank usefully.
const (
	folderScanDepth = 6
	folderScanCap   = 2000
)

// scanFolderRoots walks `~` for repositories, newest-modified first.
//
// A DIRECTORY WITH A `.git` IN IT IS A ROOT AND IS NOT DESCENDED INTO. That is
// what keeps the walk small — a repository's own subdirectories are not other
// repositories, and the ones that are (a submodule, a vendored checkout) are
// reached by typing a path, which is what typing a path is for.
func scanFolderRoots() []string {
	base, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	type found struct {
		path string
		at   time.Time
	}
	var roots []found
	_ = filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || !entry.IsDir() {
			// An unreadable directory is skipped, not fatal: an index that
			// refused to exist because of one permission is worth less than an
			// index with one directory missing from it (walkFiles' own law).
			return nil
		}
		name := entry.Name()
		if path != base && (skipDirs[name] || strings.HasPrefix(name, ".")) {
			return fs.SkipDir
		}
		if len(roots) >= folderScanCap {
			return fs.SkipAll
		}
		if depth(base, path) > folderScanDepth {
			return fs.SkipDir
		}
		if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
			return nil
		}
		at := time.Time{}
		if info, err := entry.Info(); err == nil {
			at = info.ModTime()
		}
		roots = append(roots, found{path: path, at: at})
		return fs.SkipDir
	})
	// NEWEST FIRST, because the order a source hands its candidates over in is
	// what decides ties inside a layer, and "the repository I touched most
	// recently" is a better guess than "the one alphabetically first" every
	// single time.
	sort.SliceStable(roots, func(i, j int) bool { return roots[i].at.After(roots[j].at) })
	out := make([]string, 0, len(roots))
	for _, root := range roots {
		out = append(out, root.path)
	}
	return out
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
