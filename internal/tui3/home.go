package tui3

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// HOME: /home — everything this machine has worked on, in one place.
//
// Every other surface here is a reading of ONE conversation in ONE directory.
// That is the whole of what a person is fighting when they say "do I need
// another terminal?": aforge made the folder you are standing in the identity
// of the screen, so moving between projects meant moving windows, and there was
// nowhere at all that showed the work as a person holds it — everything, at
// once, ordered by what wants them.
//
// This is that place, and it is a GLANCE YOU TAKE rather than somewhere you
// live. It does nothing on its own: no notifications, no charts, no telemetry.
// You open it, you see where things stand, and you leave — into a conversation,
// into a new one, or back into the one you came from with esc.
//
// Three jobs and no fourth (docs/home-design.md):
//
//   - TRIAGE. The left column is every project as a dim heading with its
//     conversations under it, ordered by what is happening rather than by what
//     is newest ([session.World] does that ordering). A conversation stopped on
//     a question wears `▲` and sits at the top of its project, which is the
//     single most valuable row this screen can draw — it is the one thing that
//     costs a keystroke to unblock and can otherwise sit unnoticed for a day.
//     Quiet rows past the first few collapse to one dim line, because density
//     here is omission and never compression.
//   - RECALL. `@` turns the same column into a search over every conversation
//     on the machine, ranked by [tokenScore] — the same ladder the model picker
//     and the resume picker rank with, so three characters find a chat from
//     last week the way they find a model.
//   - THE DOOR. Typing anything else is the start of a new conversation: the
//     words go in the box at the foot, and enter opens a fresh session in this
//     project and sends them. No picker, no ceremony, no structure declared
//     before there is anything to declare it about.
//
// WHAT IT WILL NOT DO YET, said plainly because a surface that quietly does
// nothing is worse than one that says why: enter opens a conversation of THE
// PROJECT THIS WINDOW IS IN. A row from another project draws with a dim
// `elsewhere` and enter on it says where to go instead. Opening one would mean
// moving this window's workspace, and the workspace is what the approval gate,
// the crew, the spend rail and the harnesses were all resolved from at launch —
// carrying the session over without carrying those is a window running under
// another project's permissions, which is the one failure this is not worth.
// The design doc sequences cross-project open as its own piece of work; this
// slice shows the world honestly and moves inside it.

// homeShown is how many conversations a project draws before the rest collapse
// into one line. Four is what the reference layout holds under a heading and it
// is roughly what a person scans without reading: past it a section stops being
// a shape on the page and becomes a list.
//
// IT IS A FLOOR AND NOT A CEILING. Every row with work running or work left
// unfinished is drawn whatever the count says — those are the rows this screen
// exists for — and the collapse takes only the quiet ones underneath them.
const homeShown = 4

// homeTaskRows is how much of the focused conversation's work the detail column
// shows. FOUR AND NOT MORE, because each one now costs two lines: the row and
// the sentence saying what it came to. Four tasks with their outcomes answer
// "what has this been doing" better than twelve bare labels, and the project's
// whole history is what the index itself is for.
const homeTaskRows = 4

// homeEvery is how long between readings of the disk. Three seconds is slow
// enough that the walk is free and fast enough that a task landing in another
// window shows up while you are still looking at the screen.
//
// IT IS ITS OWN CLOCK AND NOT THE PAINT CLOCK. The frame clock turns at thirty
// frames a second because something on screen is moving (app.go's [app.paint]),
// and home is a still page — running it at that rate to re-read a directory
// twice a minute would be this surface spending a core on a screen whose whole
// character is that it does nothing. So one tick, three seconds apart, re-armed
// only while home is open. There is no watcher either: the read is a directory
// walk and a lock asked as a question (session's world.go).
const homeEvery = 3 * time.Second

// homeTickMsg is that clock's beat.
type homeTickMsg struct{}

// homeTick schedules the next reading.
func homeTick() tea.Cmd {
	return tea.Tick(homeEvery, func(time.Time) tea.Msg { return homeTickMsg{} })
}

// homeBeat is the beat, arriving. A beat that finds home closed re-arms
// nothing, which is how the clock stops.
func (a *app) homeBeat() tea.Cmd {
	if !a.home.open {
		return nil
	}
	a.refreshHome()
	return homeTick()
}

// homeMinDetail is the width below which the detail column is not drawn at all.
// Two columns at thirty cells each is two truncated columns; under this the
// left column takes the frame and the detail is one screen away, which is the
// honest answer at that width.
const homeMinDetail = 80

// homeGutter is the empty space between the two columns, and it is the ONLY
// thing that separates them.
//
// THIS SURFACE DRAWS NO BORDERS (internal/tui is the north star, and it has
// none), so the separation has to be made of nothing — which means there has to
// be enough of it. Two columns of gap read as a wide word space and the two
// panes ran together into one ragged column; four is unmistakably a gutter. It
// works only because the left column is padded to its full width on every row,
// so the right pane's edge is a straight vertical line at a fixed x that the
// eye can find without looking for it.
const homeGutter = 4

// homeDetailFloor is the narrowest the detail column is worth drawing. Under it
// the left list takes the whole frame: a preview squeezed into thirty cells is
// two truncated columns, and between an index somebody can read and a preview
// nobody can, the index wins.
const homeDetailFloor = 34

// The glyphs a conversation wears in the left column. They say what is
// HAPPENING and nothing else — there is no state here that a person has to be
// taught, only "wants you", "moving", "stopped mid-way" and "at rest".
//
// THE TRIANGLE IS THE ONLY ONE THAT POINTS AT ANYTHING. The other three are
// round and read as weather; a conversation stopped on a question is the one
// row on this screen that is asking for a hand, and it gets the one shape that
// looks like it is asking.
const (
	homeAskGlyph   = "▲"
	homeLiveGlyph  = "●"
	homeStuckGlyph = "◌"
	homeIdleGlyph  = "○"

	homeAskASCII   = "!"
	homeLiveASCII  = "*"
	homeStuckASCII = "o"
	homeIdleASCII  = "-"
)

// The sentences this surface says. Each is quoted in the manual exactly as it
// is spelled here.
const (
	// homeFootWord is the three verbs, and it stays three. A footer that grew a
	// key for everything this screen can do would be the cockpit this is
	// deliberately not (docs/home-design.md).
	homeFootWord = "type to search or start something new · ↑↓ pick · enter open"
	// homeEmptyWord is a machine that has not held a conversation yet.
	homeEmptyWord = "nothing here yet — say something and this fills up"
	// homeNoMatchWord is a filter that matched nothing.
	homeNoMatchWord = "no conversation matches"
	// homeRemoteWord is the refusal over --host: the projects under
	// ~/.aforge/v3 are THIS machine's, and the session is on another one.
	homeRemoteWord = "home shows this machine's projects, and this session is on another"
	// homeElsewhereWord marks a row this window cannot open, and is also what
	// enter on one says, with the project's path after it.
	homeElsewhereWord = "elsewhere"
	// homeStartWord is the action row's label, with what was typed quoted after
	// it. "conversation" and not "chat" because that is what this surface calls
	// one everywhere else it names one — /new closes a session and starts a
	// fresh one, and the manual has said "conversation" since before home
	// existed.
	homeStartWord = "start a new conversation"
	// homeStartGlyph marks it. A plain `+` on purpose: it is the one row on the
	// column that is not a thing that exists yet, and every other glyph here is
	// a state something is in.
	homeStartGlyph = "+"
	// homeHeldWord is what the detail column says about a conversation another
	// window is holding, and homeHeldShort is the same fact in the width the
	// left column has for it. Two spellings of ONE thing, and the short one
	// exists for a reason a person can see: the list column is forty-six cells
	// wide and a row that spent twenty-two of them on this would have nothing
	// left for the name it is about.
	homeHeldWord  = "open in another window"
	homeHeldShort = "another window"
)

// homeRowKind is what one line of the left column is.
type homeRowKind uint8

const (
	// homeHeading is a project's name. It is not a cursor stop: there is
	// nothing to do to a project, and a cursor that had to be walked past every
	// heading would double the keystrokes between two conversations.
	homeHeading homeRowKind = iota
	// homeSession is one conversation, and the only thing enter means anything
	// on.
	homeSession
	// homeQuiet is a project's tail — "…2 more, quiet since Tue" folded, and
	// the same line holding it open when it is not. IT IS A CURSOR STOP AND A
	// DOOR: enter or → opens the project, ← folds it again, and a click does
	// the same. A line that says work is being hidden and cannot be asked to
	// stop hiding it is a dead end somebody hits and gives up at.
	homeQuiet
	// homeAction is "start a new conversation", drawn only while something is
	// typed and always at the very top. It is a cursor stop and it is where the
	// cursor RESTS by default, which is what keeps type-and-enter meaning
	// exactly what it meant before the box could also search.
	homeAction
	// homeBlank is the empty line between projects.
	homeBlank
)

// homeLine is one drawn line of the left column, resolved against the world
// once when the rows are built. Holding the row rather than an index into the
// world is what lets a rescan replace the world underneath without a stale
// index reaching into it.
type homeLine struct {
	kind homeRowKind
	// project is the heading's text, and the name carried on a session row so
	// that a filtered list — which has no headings — still says where a hit
	// came from.
	project string
	// dir is the project's bucket directory, which is how a row answers whether
	// THIS window can open it.
	dir string
	// row is the conversation, for [homeSession].
	row session.SessionRow
	// quiet is how many conversations the collapsed line stands for, and since
	// when nobody has been in them. folded says the line is hiding them right
	// now; an expanded project keeps the line as the way back.
	quiet  int
	since  time.Time
	folded bool
}

// homeView is the whole surface's state. The zero value is closed, which is
// what every surface starts as — and closing is assigning the zero value, so
// there is no field that can be left behind from the last time it was up.
type homeView struct {
	open bool

	// world is the reading the rows were built from, replaced whole on every
	// rescan.
	world session.World
	// lines is the left column in draw order; cursor indexes it and never rests
	// on a heading, a blank or a collapsed tail.
	lines  []homeLine
	cursor int
	top    int
	// hover is the line the pointer is over, or -1.
	hover int

	// box is the one line at the foot, and it is TWO THINGS AT ONCE rather than
	// two things by turns. What is typed there is a new conversation waiting to
	// be sent AND a live query over every project on the machine, both of them
	// true of the same characters at the same moment. There is no prefix, no
	// mode and nothing to switch: a person's fingers should not have to choose
	// what a word is for before they have finished typing it.
	box editor
	// picked says the person walked off the action row onto a match. It is what
	// keeps the two readings of the box from fighting: while it is false the
	// cursor sits on "start a new conversation" through every keystroke, so
	// type-and-enter starts a chat exactly as it always did; one ↓ sets it, and
	// then the list is being chosen from.
	picked bool
	// expanded is the projects somebody opened by hand, by bucket directory.
	// It outlives a rescan and a query, because folding is a thing a person did
	// and not a thing the data said.
	expanded map[string]bool

	// bucket is the project directory THIS window is in, which is what decides
	// whether enter can open a row (see this file's header).
	bucket string
	// last caches the tail of a conversation's journal by transcript path.
	// Reading one is a scan of the file ([session.Peek]) and the cursor moves
	// on every arrow key, so the second look at a row is free.
	last map[string]session.Summary

	// msg is the last refusal, in this surface's own words.
	msg string
}

// ── opening, closing, and the rescan ────────────────────────────────────────

// openHome is /home.
//
// It reads the world HERE rather than holding one from boot, for the reason the
// resume picker resolves its list on the keystroke: a screen opened an hour
// into a conversation must show the work that has happened in the next terminal
// since, and the walk is a directory read per project and a lock asked as a
// question per conversation.
// It returns its own clock, because home is the one screen here that changes
// with nothing arriving, and a surface that opened without starting one would be
// a photograph.
func (a *app) openHome() tea.Cmd {
	if a.hosted() {
		// A capability that cannot work is absent, not broken: over --host the
		// state root under this process belongs to the wrong machine, and a
		// screen full of the laptop's projects while the session runs on the
		// server would be a lie drawn confidently.
		a.note(homeRemoteWord)
		return nil
	}
	// THE OTHER FULLSCREEN PAGES STAND DOWN — the settings panel and the task
	// page both ([app.standDownFullscreen] states the law). It happens BEFORE the
	// screen below is built, because standing down closes home too and a call the
	// other way round would sweep away the view this line is about to make.
	a.standDownFullscreen()
	a.closeLists()
	a.dismissWelcome()
	a.home = homeView{
		open:     true,
		world:    session.ReadWorld(a.placesRoot()),
		bucket:   homeBucketOf(a.file),
		hover:    -1,
		last:     map[string]session.Summary{},
		expanded: map[string]bool{},
	}
	a.home.build()
	a.home.point(a.file)
	a.touch()
	return homeTick()
}

// ── the landing ─────────────────────────────────────────────────────────────

// landHome decides, once, whether home is the FIRST THING a launch shows.
//
// A person opening aforge is not usually opening a conversation — they are
// opening the machine, and the conversation is a guess the door made for them
// out of which directory they happened to be standing in. So the first frame is
// this screen, with the conversation the door picked loaded and waiting
// underneath it: esc, or the first character of a message, drops straight into
// it exactly as if home had never been there. Nothing about which session opens
// is changed by any of this — the door had already chosen before the surface
// existed.
//
// THREE THINGS HAVE TO BE TRUE, and each of them is a way of saying that a
// person is being greeted rather than obeyed:
//
//  1. THE DOOR ASKED FOR IT ([Options.Landing]). A `--once` run, a headless
//     frame, a test, anything over `--host` — none of them set it, so none of
//     them can be greeted by accident. And a launch that NAMED a conversation
//     (`--session <path>`, `aforge resume`) does not set it either: somebody who
//     said which one means that one.
//  2. NOTHING ELSE IS ALREADY GREETING THEM. `aforge resume` opens on its
//     picker; a surface that put a second full-screen greeting behind the first
//     would be two answers to one keystroke.
//  3. THERE IS SOMEWHERE ELSE TO GO. This is the emptiness law applied to a
//     whole surface rather than to a number: a machine whose only conversation
//     is the one this launch just opened has NOTHING home could tell anybody —
//     it would be a dashboard of one row, and the row is the screen behind it.
//     A first run therefore goes straight to the chat, and gets the welcome box
//     it always got. Home arrives the day it has an answer.
//
// It is not a setting. Whether a person is greeted is a property of what the
// machine holds and of how they launched, and both of those change by
// themselves; a switch would be a third answer that has to be kept in step with
// two facts that are already true.
func (a *app) landHome() {
	if a.hosted() || a.resume == nil {
		return
	}
	// ONE READ ANSWERS BOTH QUESTIONS. Whether to greet somebody now, and
	// whether the door is worth advertising at the foot of the conversation
	// afterwards ([app.homeWorth]), are the same fact about the same machine —
	// and the second is asked on every frame, which is exactly as many times as
	// a directory walk must not happen.
	world := session.ReadWorld(a.placesRoot())
	a.homeWorth = worldHasElsewhere(world, a.file)
	if !a.landing || a.pickSession || !a.homeWorth {
		return
	}
	a.home = homeView{
		open:     true,
		world:    world,
		bucket:   homeBucketOf(a.file),
		hover:    -1,
		last:     map[string]session.Summary{},
		expanded: map[string]bool{},
	}
	a.home.build()
	// THE CURSOR OPENS ON THE CONVERSATION THIS WINDOW IS IN, which is the
	// resume picker's law and it matters more here: enter is a confirm key, and
	// a screen that greeted somebody with the cursor on a stranger's row would
	// make the cheapest keystroke on it the wrong one. Landed on the row you
	// were already in, enter and esc mean the same calm thing — go on with what
	// I was doing (resume.go's [roster.start] holds the original of this).
	a.home.point(a.file)
	// AND THE WELCOME BOX RETIRES WITHOUT EVER DRAWING. Its right column is the
	// four most recent conversations in this directory, and home's left column
	// is every conversation in every project — the same rows and more, under a
	// heading that says which project each belongs to. Two greeters is one too
	// many, and between a box that lists four and a screen that lists them all
	// there is nothing to weigh up.
	//
	// It is RETIRED and not merely hidden ([welcome.spent]), so that esc out of
	// home lands on the ordinary prompt rather than on a box popping up behind
	// the screen that just closed. A machine where home does not land is
	// untouched by this: the box greets a first run exactly as it always has.
	a.welcome = welcome{spent: true}
}

// worldHasElsewhere reports whether this machine holds a conversation OTHER than
// the one a launch just opened.
//
// It is the third condition of [app.landHome] and it is deliberately a fact
// rather than a count. "More than one session" and "more than one project" are
// both thresholds somebody would have to defend; this is the question home
// actually answers on a launch — is there anywhere else to go — and a machine
// that answers no has no use for the screen.
//
// A launch with no session file at all (memory-only, a surface with no door
// onto the disk) compares against nothing, so any conversation on the machine
// counts as somewhere else.
func worldHasElsewhere(world session.World, here string) bool {
	for _, project := range world.Projects {
		for _, row := range project.Sessions {
			if row.Transcript != here {
				return true
			}
		}
	}
	return false
}

func (a *app) closeHome() {
	// The world in hand on the way out is the freshest reading there will be
	// until home opens again, so the door's advertisement is trued up here
	// rather than left as it was at boot.
	if len(a.home.world.Projects) > 0 {
		a.homeWorth = worldHasElsewhere(a.home.world, a.file)
	}
	a.home = homeView{}
	a.touch()
}

// placesRoot is where the projects live. The field is the test's door and
// nothing else sets it: a surface that took the root from its options would be
// a second answer to a question internal/session already owns.
func (a *app) placesRoot() string {
	if root := strings.TrimSpace(a.homeRoot); root != "" {
		return root
	}
	return session.PlacesRoot()
}

// refreshHome is the slow tick: the same walk again, with the cursor kept on
// the conversation it was on rather than on the line number it was on.
//
// A LIST THAT REORDERS UNDER A CURSOR HAS MOVED THE CURSOR. Triage order is a
// function of what is running, so a task landing in another window genuinely
// re-sorts the column — and a cursor that stayed at line seven would land the
// person on somebody else's conversation between two glances.
func (a *app) refreshHome() {
	if !a.home.open {
		return
	}
	a.home.world = session.ReadWorld(a.placesRoot())
	a.homeWorth = worldHasElsewhere(a.home.world, a.file)
	// [homeView.build] is the one that keeps the cursor on its conversation, so
	// this is a rescan and a rebuild and nothing else.
	a.home.build()
	a.touch()
}

// build turns the world into lines, applying the filter when one is typed.
func (h *homeView) build() {
	previous := h.focused()
	// An empty box is not a choice anybody has made yet, so the next character
	// typed starts on the action row again.
	if !h.searching() {
		h.picked = false
	}
	h.lines = h.lines[:0]
	h.buildWorld()
	// THE CURSOR FOLLOWS THE CONVERSATION AND NOT THE LINE NUMBER. A query typed
	// one letter at a time, and a rescan that re-sorts around work starting,
	// both rebuild this list under a cursor — and a cursor that held its
	// position would land on whatever happened to sort into row seven. So it
	// goes to the top of the new list, and comes back to the row it was on if
	// that row is still in it.
	//
	// THE ACTION ROW IS THE EXCEPTION AND IT IS THE WHOLE POINT. While something
	// is typed, the cursor rests on "start a new conversation" unless the person
	// walked off it — so type-and-enter still starts a chat, exactly as it did
	// before this box could also search (see [homeAction]).
	h.cursor, h.top = h.clamp(0), 0
	if h.searching() {
		h.picked = h.picked && h.pointable(previous.Transcript)
		if !h.picked {
			return
		}
	}
	if previous.Transcript != "" {
		h.point(previous.Transcript)
	}
}

// buildWorld is the column: projects as dim headings with their conversations
// under them, filtered and ranked by whatever is in the box.
func (h *homeView) buildWorld() {
	query := h.query()
	if query != "" {
		// The action row leads, always, and is what the cursor opens on.
		h.lines = append(h.lines, homeLine{kind: homeAction})
	}
	type ranked struct {
		project session.Project
		rows    []session.SessionRow
		score   int
	}
	var found []ranked
	for _, project := range h.world.Projects {
		hit := ranked{project: project}
		for _, row := range project.Sessions {
			score, ok := homeRank(row, project, query, h.world.Read)
			if !ok {
				continue
			}
			hit.rows = append(hit.rows, row)
			if score > hit.score {
				hit.score = score
			}
		}
		if len(hit.rows) == 0 {
			continue
		}
		if query != "" {
			// Inside a project the best match leads. With nothing typed the rows
			// keep the world's own triage order, which is what the screen is for
			// when nobody is searching (session's sortSessions).
			rows := hit.rows
			sort.SliceStable(rows, func(i, j int) bool {
				a, _ := homeRank(rows[i], project, query, h.world.Read)
				b, _ := homeRank(rows[j], project, query, h.world.Read)
				return a > b
			})
		}
		found = append(found, hit)
	}
	if query != "" {
		// And the project holding the best row leads, so the thing somebody is
		// hunting is near the top of the screen rather than under four headings.
		sort.SliceStable(found, func(i, j int) bool { return found[i].score > found[j].score })
	}
	for _, hit := range found {
		if len(h.lines) > 0 {
			h.lines = append(h.lines, homeLine{kind: homeBlank})
		}
		h.lines = append(h.lines, homeLine{
			kind: homeHeading, project: hit.project.Name, dir: hit.project.Dir,
		})
		shown, quiet, since := h.split(hit.project, hit.rows, query)
		for _, row := range shown {
			h.lines = append(h.lines, homeLine{
				kind: homeSession, project: hit.project.Name, dir: hit.project.Dir, row: row,
			})
		}
		// A SEARCH HAS NO TAIL LINE. Everything that matched is on screen, so
		// there is nothing being hidden to offer to show — and a fold control
		// over a list nobody folded would be a control that does nothing.
		if quiet > 0 && query == "" {
			h.lines = append(h.lines, homeLine{
				kind: homeQuiet, project: hit.project.Name, dir: hit.project.Dir,
				quiet: quiet, since: since, folded: !h.expanded[hit.project.Dir],
			})
		}
	}
}

// split decides what a project shows and what it whispers: everything with work
// running or work left unfinished, then enough of the rest to reach
// [homeShown], and the remainder counted with the newest of their stamps.
//
// TWO THINGS OPEN IT ALL THE WAY. A project somebody expanded by hand stays
// expanded ([homeView.expanded]), and — the one that matters — A SEARCH IS
// NEVER COLLAPSED. A filter that could not see what it hides would be a filter
// lying about the machine: somebody typing three letters and getting
// "…13 more, quiet since 10h" has been told the thing they asked for might be
// behind a line they cannot open, which is worse than no search at all.
func (h *homeView) split(project session.Project, rows []session.SessionRow, query string) (shown []session.SessionRow, quiet int, since time.Time) {
	for i, row := range rows {
		busy := row.Tasks.Running > 0 || row.Tasks.Incomplete > 0
		if busy || i < homeShown {
			shown = append(shown, row)
			continue
		}
		quiet++
		if row.At.After(since) {
			since = row.At
		}
	}
	// THE COUNT IS THE SAME EITHER WAY, and only whether the rows are drawn
	// changes. An opened project still has to say how many it opened, because
	// that line is the way back: a fold with no label is a fold nobody can find
	// again.
	if query != "" || h.expanded[project.Dir] {
		return rows, quiet, since
	}
	return shown, quiet, since
}

// query is what is in the box, folded for matching. It is the SAME text the
// action row would send as a new conversation: one box, read two ways, and
// never a mode (see this file's header).
func (h *homeView) query() string {
	return strings.ToLower(strings.TrimSpace(h.box.String()))
}

// searching reports whether anything is typed at all.
func (h *homeView) searching() bool { return h.query() != "" }

// pointable reports whether a transcript is still a row on this list.
func (h *homeView) pointable(transcript string) bool {
	if transcript == "" {
		return false
	}
	for _, line := range h.lines {
		if line.kind == homeSession && line.row.Transcript == transcript {
			return true
		}
	}
	return false
}

// ── the ranking ─────────────────────────────────────────────────────────────

// THERE ARE NO EMBEDDINGS HERE AND THERE ARE NOT GOING TO BE.
//
// Home's search is lexical, local and instant: a few hundred rows already in
// memory, matched with the one ladder this program has ([session.MatchQuality]),
// answered inside a keystroke with nothing loaded, no model called and no index
// to keep in step with the disk. That is the right trade for the question it is
// asked, which is "get me back to the thing I half remember the name of".
//
// The semantic tail is served twice over, and neither answer is this function's:
//
//   - THE ACTION ROW NEVER GOES AWAY. A query that matches nothing at all still
//     offers to start a conversation with it, so the worst case of a lexical
//     miss is that the words somebody typed become the first message of a chat
//     rather than a dead end — which is very often what they wanted anyway.
//   - THE MODEL OWNS THE DEEP SEARCH. The `tasks` tool reads the whole project
//     record and the chat can be asked in sentences. Home is the fast layer and
//     the conversation is the thoughtful one, and putting a second, worse
//     semantic search on the fast layer would blur which is which.
//
// Score is HIGHEST WINS, and it is three things added together.
const (
	// The FIELD weights: which text the query landed in. They multiply the rung
	// so that a strong match in a weak field cannot beat a weak match in a
	// strong one by more than the gap between them — a name is what a person
	// remembers, and an outcome sentence is where they end up when they cannot.
	homeFieldName    = 10
	homeFieldProject = 6
	homeFieldTask    = 5
	homeFieldOutcome = 3

	// The STATE boosts and the recency bonus, added once per row. Their sizes
	// relative to each other and to a rung are the whole of the ranking's
	// character, and the band they sit in is deliberate:
	//
	//	a rung at the weakest field   200 × 3  = 600
	//	needs somebody                           400
	//	work running                             200
	//	work left unfinished                      60
	//	the whole recency range                  120
	//
	// SO: A BETTER MATCH ALWAYS WINS, and among matches of the SAME quality the
	// row that wants somebody always wins — the needs-you boost is larger than
	// the entire recency range, so no amount of "but the other one is newer"
	// can push a waiting conversation below a cold one it ties with. Recency
	// then orders what is left, which is what it is for: separating equals, not
	// overruling a better answer.
	homeBoostNeedsYou   = 400
	homeBoostRunning    = 200
	homeBoostIncomplete = 60

	homeRecencyBoost = 120
	homeRecencySpan  = 30 * 24 * time.Hour
)

// homeRank scores one conversation against the box, and reports false for one
// the query does not reach at all. An empty query matches everything at zero,
// which is what leaves the world in its own order.
func homeRank(row session.SessionRow, project session.Project, query string, now time.Time) (int, bool) {
	if query == "" {
		return 0, true
	}
	name := strings.ToLower(homeName(row))
	place := strings.ToLower(project.Name)
	total := 0
	// EVERY WORD MUST LAND SOMEWHERE, which is the roster's rule and the reason
	// a second word narrows instead of widening. Where each lands is its own
	// business: "auth flaky" may match the name with one word and a task outcome
	// with the other, and that row is a better answer than either alone.
	for _, token := range strings.Fields(query) {
		best := 0
		if rung, ok := session.MatchQuality(name, token); ok {
			best = max(best, rung*homeFieldName)
		}
		if rung, ok := session.MatchQuality(place, token); ok {
			best = max(best, rung*homeFieldProject)
		}
		for _, entry := range row.Tasks.Rows {
			if best >= session.MatchWord*homeFieldTask {
				// Nothing left in this field can beat what we already have, and
				// a project's index runs to two thousand rows.
				break
			}
			if rung, ok := session.MatchQuality(strings.ToLower(homeTaskText(entry)), token); ok {
				best = max(best, rung*homeFieldTask)
			}
			if entry.Outcome == "" {
				continue
			}
			if rung, ok := session.MatchQuality(strings.ToLower(entry.Outcome), token); ok {
				best = max(best, rung*homeFieldOutcome)
			}
		}
		if best == 0 {
			return 0, false
		}
		total += best
	}
	return total + homeState(row) + homeRecency(row.At, now), true
}

// homeTaskText is what a task is searched by: the label a row shows, falling
// back to the title it was groomed from.
func homeTaskText(entry session.TaskIndexEntry) string {
	if entry.Label != "" {
		return entry.Label
	}
	return entry.Title
}

// homeState is what a row's own situation is worth, added once per row.
func homeState(row session.SessionRow) int {
	switch {
	case row.NeedsPerson():
		return homeBoostNeedsYou
	case row.Tasks.Running > 0:
		return homeBoostRunning
	case row.Tasks.Incomplete > 0:
		return homeBoostIncomplete
	}
	return 0
}

// homeRecency decays [homeRecencyBoost] to nothing over [homeRecencySpan],
// straight-line. Nothing subtler is warranted: this is a tie-breaker between
// matches of the same quality, and a curve would be a shape nobody could read
// off the screen.
func homeRecency(at, now time.Time) int {
	if at.IsZero() || now.IsZero() {
		return 0
	}
	old := now.Sub(at)
	if old <= 0 {
		return homeRecencyBoost
	}
	if old >= homeRecencySpan {
		return 0
	}
	return int(int64(homeRecencyBoost) * (int64(homeRecencySpan) - int64(old)) / int64(homeRecencySpan))
}

// focused is the conversation under the cursor, and the zero row when the
// cursor is not on one.
func (h *homeView) focused() session.SessionRow {
	if h.cursor < 0 || h.cursor >= len(h.lines) || h.lines[h.cursor].kind != homeSession {
		return session.SessionRow{}
	}
	return h.lines[h.cursor].row
}

// focusedLine is WHATEVER the cursor is on — a conversation, a project's folded
// tail, or the action row. [homeView.focused] is the narrower question and
// answers a zero row for the other two, which is what keeps the detail column
// from drawing a conversation nobody is pointing at.
func (h *homeView) focusedLine() (homeLine, bool) {
	if h.cursor < 0 || h.cursor >= len(h.lines) || !h.lines[h.cursor].stop() {
		return homeLine{}, false
	}
	return h.lines[h.cursor], true
}

// point puts the cursor on the row holding a transcript, and leaves it where it
// is when that conversation is not on the list any more.
func (h *homeView) point(transcript string) {
	for at, line := range h.lines {
		if line.kind == homeSession && line.row.Transcript == transcript {
			h.cursor = at
			return
		}
	}
}

// clamp walks from a line number to the nearest one a cursor may rest on,
// searching forward and then back — so a cursor landing on a heading after a
// rebuild slides onto the conversation under it rather than off the list.
func (h *homeView) clamp(at int) int {
	if len(h.lines) == 0 {
		return 0
	}
	if at < 0 {
		at = 0
	}
	if at >= len(h.lines) {
		at = len(h.lines) - 1
	}
	for i := at; i < len(h.lines); i++ {
		if h.lines[i].stop() {
			return i
		}
	}
	for i := at; i >= 0; i-- {
		if h.lines[i].stop() {
			return i
		}
	}
	return at
}

// stop reports whether the cursor may rest on this line. A heading names a
// project and a blank separates two, and neither is a thing to do anything to;
// everything else on the column answers enter.
func (l homeLine) stop() bool {
	switch l.kind {
	case homeSession, homeQuiet, homeAction:
		return true
	}
	return false
}

// move walks by whole conversations, stepping over headings, blanks and
// collapsed tails as though they were not there. It clamps at both ends rather
// than wrapping, for the reason every list here does ([moveCursor]).
func (h *homeView) move(delta int) {
	if delta == 0 || len(h.lines) == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step, delta = -1, -delta
	}
	at := h.cursor
	for ; delta > 0; delta-- {
		next := at + step
		for next >= 0 && next < len(h.lines) && !h.lines[next].stop() {
			next += step
		}
		if next < 0 || next >= len(h.lines) {
			break
		}
		at = next
	}
	h.cursor = at
	// WALKING OFF THE ACTION ROW IS THE DECISION. Until it is made the box is a
	// message being written; after it, the person is picking from the list and
	// the cursor stays where they put it through every further keystroke.
	if h.cursor >= 0 && h.cursor < len(h.lines) && h.lines[h.cursor].kind != homeAction {
		h.picked = true
	}
}

// ── the keyboard ────────────────────────────────────────────────────────────

// homeKey routes one keypress while home is up. It is modal for the reason the
// settings panel is: home takes the whole frame, so there is nothing underneath
// for a key to mean anything to. Only ctrl+c is read before it (input.go),
// because leaving is never modal.
func (a *app) homeKey(msg tea.KeyPressMsg) tea.Cmd {
	if !a.home.open {
		return nil
	}
	h := &a.home
	defer a.touch()
	h.msg = ""
	switch msg.String() {
	case "esc":
		// ONE LAYER AT A TIME, the settings panel's rule: a box with something
		// in it is cleared first, and the second esc leaves. A person who typed
		// a search and meant to keep looking must not be thrown back into the
		// conversation for pressing the key that means "undo that".
		if !h.box.empty() {
			h.box.reset()
			h.build()
			return nil
		}
		a.closeHome()
		return nil

	case "up", "ctrl+p":
		h.move(-1)
		return nil
	case "down", "ctrl+n":
		h.move(1)
		return nil
	case "pgup":
		h.move(-homeShown)
		return nil
	case "pgdown":
		h.move(homeShown)
		return nil

	case "enter":
		return a.homeEnter()

	case "backspace":
		h.box.deleteBackward()
		h.build()
		return nil
	case "ctrl+u":
		h.box.reset()
		h.build()
		return nil
	case "ctrl+w":
		h.box.deleteWord()
		h.build()
		return nil
	case "right":
		// THE ARROWS ARE THE FOLD'S, the same way they are in the task column:
		// → opens what is closed, ← closes what is open. On the tail line that
		// is the project; on a conversation inside an opened project, ← folds
		// the project back, which is how somebody gets out of a list they
		// opened without walking to the bottom of it.
		if line, ok := h.focusedLine(); ok && line.kind == homeQuiet {
			h.fold(line.dir, true)
			return nil
		}
		h.box.right()
		return nil
	case "left":
		if line, ok := h.focusedLine(); ok && (line.kind == homeQuiet || line.kind == homeSession) && h.expanded[line.dir] {
			h.fold(line.dir, false)
			return nil
		}
		h.box.left()
		return nil
	case "ctrl+b":
		h.box.left()
		return nil
	case "ctrl+f":
		h.box.right()
		return nil

	default:
		// TYPING IS THE WHOLE CEREMONY, and it does both jobs at once: the
		// characters are a message being written AND a query over every project
		// on the machine. Nothing had to be opened, and nothing has to be
		// chosen between (see [homeView.box]).
		if text := msg.Key().Text; text != "" {
			h.box.insert(text)
			h.build()
		}
		return nil
	}
}

// fold opens or closes one project, and leaves the cursor on the line that did
// it so the gesture can be reversed without moving.
func (h *homeView) fold(dir string, open bool) {
	if h.expanded == nil {
		h.expanded = map[string]bool{}
	}
	if open {
		h.expanded[dir] = true
	} else {
		delete(h.expanded, dir)
	}
	held := h.cursor
	h.rebuild()
	// The tail line of the project just toggled, which is where the person is
	// standing. It has moved — an opened project put its rows above it — so it
	// is found again rather than counted to.
	for at, line := range h.lines {
		if line.kind == homeQuiet && line.dir == dir {
			h.cursor = at
			h.picked = true
			return
		}
	}
	h.cursor = h.clamp(held)
}

// rebuild is [homeView.build] with the cursor left alone, for the callers that
// are moving it themselves.
func (h *homeView) rebuild() {
	h.lines = h.lines[:0]
	h.buildWorld()
	h.cursor = h.clamp(h.cursor)
}

// homeEnter is the one decision this surface makes, and it makes a different
// one depending on what is in the box.
func (a *app) homeEnter() tea.Cmd {
	h := &a.home
	line, ok := h.focusedLine()
	if !ok {
		return nil
	}
	switch line.kind {
	case homeAction:
		// The row the cursor rests on while something is typed, which is what
		// makes type-and-enter mean today what it meant yesterday.
		return a.homeStart(strings.TrimSpace(h.box.String()))
	case homeQuiet:
		h.fold(line.dir, line.folded)
		return nil
	}
	switch {
	case line.row.Transcript == a.file:
		// The conversation this window is already in, and it is already loaded
		// underneath this screen — so enter simply steps into it. Reopening it
		// would drop the lock, replay the journal and land exactly here, for a
		// second of work and nothing to show (resume.go says the same of its
		// own marked row).
		//
		// IT SAYS NOTHING. The picker notes `already here` because it stays open
		// and owes an explanation for a keystroke that did nothing; home CLOSES,
		// and closing into the conversation somebody just confirmed is the thing
		// happening rather than the absence of one. A note here would be the
		// surface narrating a door it just walked through.
		a.closeHome()
		return nil
	case !a.homeOpens(line):
		h.msg = homeElsewhereWord + " · " + homeWhere(line)
		return nil
	case a.homeHeldNow(line.row):
		// THE DOOR ANNOUNCES ITSELF LOCKED RATHER THAN SLAMMING. Home read the
		// same flock the open would take, seconds ago and again just now, so it
		// KNOWS. The resume picker reports this failure after the fact because
		// it genuinely cannot know beforehand; home can, and a screen that
		// offers a door it has already established goes nowhere is a screen that
		// wastes a keystroke and a second of somebody's attention on a raw error.
		h.msg = sessionBusyWord
		return nil
	}
	chosen := Session{
		Title: line.row.Title,
		File:  line.row.Transcript,
		At:    line.row.At,
	}
	// THE SAME DOOR THE RESUME PICKER WALKS THROUGH, not a second one: opening
	// the chosen journal, replaying it, closing the old agent and re-subscribing
	// the standing lanes is one arrangement, and two of them would be two things
	// to keep in step (welcome.go's [app.openSession]).
	//
	// HOME TAKES THE REFUSAL ITSELF rather than letting it be said in the
	// conversation. The check above closes the window where a lock can appear
	// down to the microseconds between the flock probe and the open — but not to
	// nothing, so this is the same sentence again for the same fact, in the same
	// place, and home stays open around it. A refusal on this screen belongs to
	// this screen: notes stack in a transcript, and pressing enter twice on a
	// locked row is exactly how somebody would find that out.
	cmd, refusal := a.openSession(chosen)
	if refusal != "" {
		h.msg = refusal
		return nil
	}
	a.closeHome()
	return cmd
}

// homeStart is the door: a fresh conversation in this project, carrying the
// sentence that opened it.
//
// It is /new and then a submit, in that order and with nothing invented in
// between — [app.renew] swaps the agent synchronously and hands back the lanes
// the new conversation owes itself, so the submit below is talking to the new
// agent and not to the one that just closed.
func (a *app) homeStart(text string) tea.Cmd {
	if a.fresh == nil {
		a.home.msg = newUnavailableWord
		return nil
	}
	a.closeHome()
	renewed := a.renew()
	return tea.Batch(renewed, a.submit(text))
}

// homeHeld reports whether another window is holding this conversation, from
// THE LAST SCAN. It is what the drawing asks.
//
// A LABEL MAY BE A FEW SECONDS OLD; AN ACTION MAY NOT. This is read for every
// row of every frame — and a frame is drawn on every keystroke and every mouse
// movement — so it must not touch the disk: twenty rows times a pointer moving
// across them is thousands of opens a second to re-learn something the scan
// already knows and refreshes every few seconds ([homeEvery]). The keystroke
// that actually opens a row asks the disk instead ([app.homeHeldNow]).
//
// The conversation THIS window is in is never held against it: we are the ones
// holding it, and stepping into it is what enter already does there.
func (a *app) homeHeld(row session.SessionRow) bool {
	if row.Transcript == "" || row.Transcript == a.file {
		return false
	}
	return row.Open || row.Live
}

// homeHeldNow is the same question asked of the disk, for the one moment it is
// worth a syscall: somebody has pressed enter on the row.
//
// It closes the window between the last scan and this keystroke, which is where
// the lock in the report actually appeared — a session opened in another
// terminal after home had already drawn its row as available. What it cannot
// close is the microseconds between this answer and the open that follows it,
// and [app.homeEnter] carries the same sentence for that case rather than
// pretending the race is gone.
func (a *app) homeHeldNow(row session.SessionRow) bool {
	if row.Transcript == "" || row.Transcript == a.file {
		return false
	}
	return session.InUse(row.Transcript) || a.homeHeld(row)
}

// homeOpens reports whether THIS window can open a row. See this file's header
// for why the answer is "only its own project's" in this slice.
func (a *app) homeOpens(line homeLine) bool {
	if a.resume == nil {
		return false
	}
	return a.home.bucket != "" && filepath.Clean(line.dir) == a.home.bucket
}

// homeWhere is where a person has to be to open a row, in the words they would
// type: the project's own path, and its bucket name when nothing recorded one.
func homeWhere(line homeLine) string {
	if path := strings.TrimSpace(line.row.ProjectDir); path != "" {
		return path
	}
	return line.project
}

// homeBucketOf is the project directory a transcript belongs to. A session
// folder's journal is one level inside the bucket, and a legacy flat journal
// sits in the bucket itself — the same climb [session.TaskIndexPath] makes, and
// for the same reason.
func homeBucketOf(transcript string) string {
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return ""
	}
	dir := filepath.Dir(transcript)
	if filepath.Base(transcript) == "transcript.jsonl" {
		return filepath.Clean(filepath.Dir(dir))
	}
	return filepath.Clean(dir)
}

// ── the door from inside a conversation ─────────────────────────────────────

// homeDoorWord is the dim advertisement at the foot of an idle conversation,
// and it is written in the hint slot's own grammar — the key, then the noun,
// exactly as `ctrl+g tasks` is (render.go's [app.hintWord]).
const homeDoorWord = "space space home"

// homeGesture is TWO SPACES TYPED INTO AN EMPTY BOX, and it is the way back to
// home from inside a conversation.
//
// WHY A GESTURE AND NOT A KEY. Every ctrl+letter is taken. `esc` was the
// obvious candidate and is not available: on an idle conversation it already
// arms rewind (the hint slot says `esc again to rewind`) and it already sends a
// message parked against a turn that has ended, and a third meaning on one key
// in that state is how a surface becomes unpredictable. What was left is a
// gesture, and a leading run of spaces in an empty message is the one keystroke
// on this surface that is reliably NOTHING: a message that begins with two
// spaces is a message nobody meant to send that way.
//
// THE INTERMEDIATE SPACE IS REAL, AND THAT IS THE POINT. The first space types
// itself, plainly, the way every other character does — there is no pending
// state, no timer, and no ghost. The SECOND one, arriving to find a box holding
// exactly one space, takes both away and opens home. So somebody who genuinely
// wanted a leading space types it and carries on: space then `x` leaves ` x`,
// untouched, because the gesture only ever fires on a space and only ever when
// a single space is all there is.
//
// PASTED TEXT CANNOT FIRE IT. A bracketed paste arrives as its own message and
// never reaches this router at all, and a paste whose brackets leak is absorbed
// key by key into the bracket's buffer above it (app.go's [app.pasteKey]) —
// so two spaces at the start of pasted text are two characters, not a door. The
// one hole is a terminal that does not speak bracketed paste at all, where a
// paste IS a stream of keystrokes and there is nothing anywhere in this program
// that can tell it from typing.
//
// A RUNNING TURN IS NO OBSTACLE. Home takes the frame the way the settings
// panel does, and the settings panel does not disturb a turn: the stream events
// are their own messages and land whatever is drawn over them (app.go's
// Update). The turn goes on underneath and is still there when esc comes back.
func (a *app) homeGesture(msg tea.KeyPressMsg) bool {
	if msg.Key().Text != " " || !a.homeDoorOpen() {
		return false
	}
	return len(a.input.value) == 1 && a.input.value[0] == ' '
}

// homeDoorOpen reports whether home is reachable AND worth going to from this
// conversation. It is the gesture's guard and the advertisement's condition,
// which is deliberate: a door that is drawn is a door that works, and one that
// would open on nothing is neither drawn nor bound.
func (a *app) homeDoorOpen() bool {
	return a.homeWorth && a.resume != nil && !a.hosted() && !a.home.open
}

// homeDoorShowing reports whether the foot of the conversation should advertise
// it: the door is open, and the box is EMPTY. It vanishes on the first
// character typed, because it is a door and not chrome — the space it takes is
// the hint slot's, which the frame already has (render.go's [app.legendRight]).
func (a *app) homeDoorShowing() bool {
	return a.homeDoorOpen() && a.input.empty() && !a.copy.on && !a.rew.on
}

// homeDoorPress is a click on that advertisement.
func (a *app) homeDoorPress(x, y int) (tea.Cmd, bool) {
	if !a.homeDoorShowing() || !a.homeDoor.holds(x) {
		return nil, false
	}
	mark, ok := a.chromeAt(y)
	if !ok || mark.kind != chromeLegend {
		return nil, false
	}
	return a.openHome(), true
}

// ── the pointer ─────────────────────────────────────────────────────────────

// homePress is a click in the left column: the first puts the cursor on a row,
// the second opens it. It is the settings panel's two-step and for its reason —
// a single click that switched conversations would make a mis-aimed pointer
// close somebody's session.
func (a *app) homePress(x, y int) tea.Cmd {
	if !a.home.open {
		return nil
	}
	width, height := a.size()
	_, hits, _, _ := a.homeFrame(width, height)
	if y < 0 || y >= len(hits) {
		return nil
	}
	at := hits[y]
	if at < 0 || at >= len(a.home.lines) || !a.home.lines[at].stop() {
		return nil
	}
	// A FOLDED TAIL OPENS ON ONE CLICK. The two-step below is there so a
	// mis-aimed pointer cannot switch somebody's conversation; unfolding a
	// project costs nothing and undoes itself, so making a person click it
	// twice would be ceremony guarding against no risk.
	if a.home.lines[at].kind == homeQuiet {
		a.home.cursor = at
		a.home.picked = true
		a.home.fold(a.home.lines[at].dir, a.home.lines[at].folded)
		a.touch()
		return nil
	}
	if a.home.cursor == at {
		return a.homeEnter()
	}
	a.home.cursor = at
	if a.home.lines[at].kind != homeAction {
		a.home.picked = true
	}
	a.touch()
	return nil
}

// homeHover records which line the pointer is over, repainting only when the
// answer changed.
func (a *app) homeHover(y int) {
	if !a.home.open {
		return
	}
	width, height := a.size()
	_, hits, _, _ := a.homeFrame(width, height)
	was := a.home.hover
	a.home.hover = -1
	if y >= 0 && y < len(hits) {
		if at := hits[y]; at >= 0 && at < len(a.home.lines) && a.home.lines[at].stop() {
			a.home.hover = at
		}
	}
	if a.home.hover != was {
		a.touch()
	}
}

// ── the drawing ─────────────────────────────────────────────────────────────

// homeFrame is the whole screen while home is open: exactly height rows, which
// line of the column each of them answers to the pointer (-1 for none), and
// where the caret sits.
//
// It is ONE function for the reason [app.sheetFrame] is: the press and the
// hover both index what this returned, so a click cannot land on a row the
// draw did not put there.
func (a *app) homeFrame(width, height int) ([]string, []int, int, int) {
	pal := a.pal
	var lines []string
	var hits []int
	add := func(text string, hit int) {
		lines = append(lines, text)
		hits = append(hits, hit)
	}

	head := " " + pal.bold(pal.ink("home"))
	if escape := pal.dim("esc close"); ansi.StringWidth(head)+ansi.StringWidth(escape)+2 <= width {
		gap := width - ansi.StringWidth(head) - ansi.StringWidth(escape) - 1
		head += strings.Repeat(" ", gap) + escape
	}
	add(head, -1)
	add("", -1)
	add(pal.dim(rule(width)), -1)
	add("", -1)

	const foot = 3
	room := height - len(lines) - foot
	if room < 1 {
		room = 1
	}

	left, right := homeColumns(width)
	a.home.top = listTop(a.home.cursor, a.home.top, len(a.home.lines), room)
	body := a.homeBody(left, right, room, pal)
	for _, drawn := range body {
		add(drawn.text, drawn.hit)
	}

	add(pal.dim(rule(width)), -1)
	caretX, caretY := 0, 0
	if a.home.box.empty() {
		add(" "+pal.dim(fit(homeFootWord, width-2)), -1)
	} else {
		text := a.home.box.String()
		add(" "+pal.accent("› ")+pal.ink(fit(text, width-4)), -1)
		caretX, caretY = 3+ansi.StringWidth(text), len(lines)-1
		if caretX > width-1 {
			caretX = width - 1
		}
	}
	if a.home.msg != "" {
		// DIM, AND NOT THE FAULT COLOUR. Every refusal this screen has is a fact
		// about a door — that conversation is open somewhere, that project is
		// not this one — and none of them is anybody's mistake. It also replaces
		// rather than stacks, being one field: pressing enter twice on a locked
		// row says the same thing once, where a note in the conversation would
		// have said it twice.
		add(" "+pal.dim(fit(a.home.msg, width-2)), -1)
	} else {
		add(" "+pal.dim(fit(a.homeHint(), width-2)), -1)
	}

	// A frame too short for the whole thing keeps its head and its last rows:
	// the same clamp the settings panel takes, so a tiny terminal shows a
	// truncated screen rather than a screen scrolled off the top.
	if len(lines) > height {
		keep := lines[:1]
		keepHits := hits[:1]
		lines = append(keep, lines[len(lines)-(height-1):]...)
		hits = append(keepHits, hits[len(hits)-(height-1):]...)
	}
	for len(lines) < height {
		add("", -1)
	}
	return lines, hits, caretX, caretY
}

// homeDrawn is one screen line and the column line it belongs to.
type homeDrawn struct {
	text string
	hit  int
}

// homeColumns splits the frame: the list on the left and the focused
// conversation on the right, with the detail dropped entirely on a frame too
// narrow to hold two readable columns ([homeMinDetail]).
func homeColumns(width int) (left, right int) {
	if width < homeMinDetail {
		return width, 0
	}
	left = width / 2
	if left > 46 {
		left = 46
	}
	if left < 30 {
		left = 30
	}
	right = width - left - homeGutter
	if right < homeDetailFloor {
		return width, 0
	}
	return left, right
}

// homeBody draws the two columns side by side, room rows tall.
func (a *app) homeBody(left, right, room int, pal palette) []homeDrawn {
	// A COLUMN WITH NOTHING IN IT HAS NOTHING TO SIT BESIDE. An empty machine and
	// a filter that matched nothing both draw one sentence, and a sentence
	// clipped to half the frame so that an empty second column could keep its
	// share is the layout winning an argument with the only words on screen.
	if len(a.home.lines) == 0 {
		left, right = left+right+2, 0
	}
	column := a.homeList(left, room, pal)
	var detail []string
	if right > 0 {
		detail = a.homeDetail(right, room, pal)
	}
	drawn := make([]homeDrawn, 0, room)
	for i := 0; i < room; i++ {
		text, hit := "", -1
		if i < len(column) {
			text, hit = column[i].text, column[i].hit
		}
		if right > 0 && i < len(detail) && detail[i] != "" {
			// THE GUTTER IS PADDED ON EVERY ROW, whether or not the left column
			// had anything to put there. That is what makes the right pane's
			// edge a straight line instead of a ragged one that moves with the
			// length of whatever conversation name happened to land beside it.
			pad := left - ansi.StringWidth(text)
			if pad < 0 {
				pad = 0
			}
			text += strings.Repeat(" ", pad+homeGutter) + detail[i]
		}
		drawn = append(drawn, homeDrawn{text: text, hit: hit})
	}
	return drawn
}

// homeList is the left column: the window of lines the cursor is inside.
func (a *app) homeList(width, room int, pal palette) []homeDrawn {
	h := &a.home
	if len(h.lines) == 0 {
		word := homeEmptyWord
		if h.searching() {
			word = homeNoMatchWord
		}
		return []homeDrawn{{text: "  " + pal.dim(fit(word, width-2)), hit: -1}}
	}
	drawn := make([]homeDrawn, 0, room)
	for at := h.top; at < len(h.lines) && len(drawn) < room; at++ {
		drawn = append(drawn, homeDrawn{text: a.homeLine(h.lines[at], at, width, pal), hit: at})
	}
	return drawn
}

// homeLine draws one line of the left column.
func (a *app) homeLine(line homeLine, at, width int, pal palette) string {
	h := &a.home
	switch line.kind {
	case homeBlank:
		return ""
	case homeHeading:
		// WHICH PROJECT THIS WINDOW CAN OPEN IS A FACT ABOUT THE PROJECT, so it
		// is said once, on the heading, and not again on every row under it.
		// Marking each row would put the same word down twelve times and take
		// the width the rollup and the age are on — which is to say it would
		// spend the whole column saying what this screen cannot do.
		word := line.project
		if !a.homeOpens(line) {
			word += " · " + homeElsewhereWord
		}
		return "  " + pal.dim(fit(word, width-2))
	case homeQuiet:
		// THE SAME FOLD MARK THE TASK COLUMN USES (task.go's [glyphShut] and
		// [glyphOpen]), because it is the same gesture over the same kind of
		// thing: a line that stands for rows you cannot see, and an arrow that
		// says which way it goes.
		mark := glyphOpen
		if line.folded {
			mark = glyphShut
		}
		if pal.ascii {
			mark = ">"
			if !line.folded {
				mark = glyphOpenASCII
			}
		}
		return overlayRow(mark+" "+homeQuietWord(line, h.world.Read), "", at == h.cursor, false, at == h.hover, width, pal)
	case homeAction:
		// It carries the words back at the person, cut to fit. The box at the
		// foot holds them too, but the box is where you are typing and this is
		// what enter will DO with it — and on a screen where enter has two
		// possible meanings, the one it currently has must be legible without
		// looking away from the list.
		label := homeStartWord
		if text := strings.TrimSpace(h.box.String()); text != "" {
			label += ": " + strconv.Quote(text)
		}
		return overlayRow(homeStartGlyph+" "+label, "", at == h.cursor, false, at == h.hover, width, pal)
	}
	label := homeGlyph(line.row, pal.ascii) + " " + homeName(line.row)
	note := homeNote(line.row, a.homeHeld(line.row), h.world.Read)
	// THE LEFT COLUMN IS AN INDEX AND STAYS CALM. Every row is dim except the
	// one the cursor is on, which takes the band and the ink — the same
	// treatment the detail column's title takes across the gutter, so the two
	// read as one thing rather than as two lists.
	//
	// The one exception is a row that wants somebody. `waiting on you` is
	// brought up out of the dim, because a screen whose whole job is triage
	// cannot render its most urgent fact in the same grey as an age.
	return overlayRowTinted(label, note, homeNoteInk(line.row, a.homeHeld(line.row)),
		at == h.cursor, line.row.Transcript == a.file, at == h.hover, width, pal)
}

// homeNoteInk is how a row's trailing fact is painted. It answers nil for every
// row that has nothing urgent to say, which is [paintNote]'s way of asking for
// the ordinary rule.
func homeNoteInk(row session.SessionRow, held bool) noteInk {
	if held || !row.NeedsPerson() {
		// A locked row keeps the ordinary dim. It is a fact about a door, not a
		// thing anybody has to do, and shouting it would put the loudest ink on
		// this screen on the one row that cannot be acted on.
		return nil
	}
	return func(pal palette, note string, selected bool) string {
		if selected {
			return pal.ink(note)
		}
		return pal.accent(note)
	}
}

// homeQuietWord is the collapsed tail's one line. The age is the newest of the
// conversations it stands for, so "quiet since" is a fact about the whole group
// rather than about whichever one sorted last.
func homeQuietWord(line homeLine, now time.Time) string {
	if !line.folded {
		// Open, and the line is now the way back. It says how many it is
		// holding open rather than how long they have been quiet: the ages are
		// on the rows themselves, right above it.
		return "…" + itoa(line.quiet) + " fewer"
	}
	word := "…" + itoa(line.quiet) + " more"
	if age := sinceAt(line.since, now); age != "" {
		word += ", quiet since " + age
	}
	return word
}

// homeNote is a conversation's dim tail: what it has going on, then how long
// since somebody spoke in it.
//
// THE EMPTINESS LAW IS THE WHOLE OF THE ARITHMETIC HERE. A conversation with no
// tasks says nothing about tasks; one that spent nothing says nothing about
// spending. A row reading "0 tasks · $0.00 · now" is four facts of which three
// are the absence of a fact.
func homeNote(row session.SessionRow, held bool, now time.Time) string {
	var parts []string
	// A DOOR THAT IS LOCKED SAYS SO BEFORE IT IS TRIED — but it says so in the
	// rung BELOW the states, and that ordering is a fact about what the states
	// already mean rather than a compromise over width.
	//
	// `waiting on you` and `N running` are read off a presence file that only a
	// LIVE session writes (session's taskpresence.go). A row wearing either of
	// them is therefore already saying a window has it; adding "and another
	// window has it" would be the same fact twice, in the width the name needed.
	// What those words cannot cover is the case in the report — a conversation
	// somebody left sitting idle in another terminal, holding its lock and
	// claiming nothing — and that is exactly the row this rung catches.
	switch {
	case row.NeedsPerson():
		// THE CONVERSATION'S OWN WORD, not a second one meaning the same thing.
		// `waiting on you` is what the presence file says (taskpresence.go's
		// [session.PresenceWaiting]) and what the task column already says of a
		// card that is holding; a third spelling here would be a third thing to
		// keep in step.
		parts = append(parts, string(session.PresenceWaiting))
	case row.Tasks.Running > 0:
		parts = append(parts, itoa(row.Tasks.Running)+" running")
	case row.Tasks.Incomplete > 0:
		parts = append(parts, itoa(row.Tasks.Incomplete)+" incomplete")
	case held:
		parts = append(parts, homeHeldShort)
	case row.Tasks.Total() > 0:
		parts = append(parts, itoa(row.Tasks.Total())+plural(" task", row.Tasks.Total()))
	}
	if age := sinceAt(row.At, now); age != "" {
		parts = append(parts, age)
	}
	return strings.Join(parts, " · ")
}

// homeGlyph is what a conversation's state looks like: wanting somebody,
// moving, stopped mid-way, or at rest.
//
// The order is [session.sortSessions]'s order, and it has to be: the glyph and
// the row's position are one claim made twice, and a row sorted to the top of
// its project under a glyph that says "at rest" is the screen arguing with
// itself.
func homeGlyph(row session.SessionRow, ascii bool) string {
	switch {
	case row.NeedsPerson():
		if ascii {
			return homeAskASCII
		}
		return homeAskGlyph
	case row.Tasks.Running > 0:
		if ascii {
			return homeLiveASCII
		}
		return homeLiveGlyph
	case row.Tasks.Incomplete > 0:
		if ascii {
			return homeStuckASCII
		}
		return homeStuckGlyph
	}
	if ascii {
		return homeIdleASCII
	}
	return homeIdleGlyph
}

// homeName is what a conversation is CALLED, through the one ladder this
// surface has for the question ([humanName], resume.go): the title it gave
// itself, the first words somebody said, then the file it lives in.
func homeName(row session.SessionRow) string {
	return humanName(Session{Title: row.Title, File: row.Transcript})
}

// homeDetail is the right column, and it is a PREVIEW CARD rather than a second
// list.
//
// The left column is an index — a hundred things, each one line, calm. This is
// the one thing the cursor is on, and it has to read as a different KIND of
// object or the eye runs the two together into one confusing column. It gets
// that from three things and no border (this surface draws none):
//
//   - A TITLE THAT IS THE BRIGHTEST TEXT ON THE SCREEN, matching the ink and
//     weight of the focused row across the gutter. That pairing is the bridge:
//     the eye leaves the highlighted row on the left and arrives at the same
//     treatment on the right, and the two read as one thing.
//   - BANDS SEPARATED BY BLANK LINES, in one fixed order — who it is, what it
//     is doing, what it has done, what was last said, and the dim arithmetic
//     underneath. Whitespace where a lesser surface would put rules.
//   - A FLOOR ON WHAT SURVIVES. A short frame drops bands FROM THE BOTTOM, so
//     the facts go first and the title never goes at all. A pane that truncated
//     its own title would be a preview that cannot say what it is previewing.
func (a *app) homeDetail(width, room int, pal palette) []string {
	line, ok := a.home.focusedLine()
	if !ok || line.kind != homeSession {
		// The action row and a folded tail are not things with a detail; the
		// column stays empty rather than keeping the last conversation's up,
		// which would be the pane answering for a row nobody is on.
		return nil
	}
	row := line.row
	now := a.home.world.Read

	// The bands, in order, each already painted. The first is the title and is
	// never dropped; the rest go from the bottom up as the frame shortens.
	bands := [][]string{{pal.bold(pal.ink(fit(homeName(row), width)))}}

	place := line.project
	if path := strings.TrimSpace(row.ProjectDir); path != "" && path != place {
		place += " · " + path
	}
	bands = append(bands, []string{pal.dim(fit(place, width))})

	// STATE IS THE LOUDEST CONTENT LINE, because it is the only band that is
	// about right now. A conversation stopped on a question says so here and
	// then says what it is stopped on, in ink.
	var state []string
	if word := a.homeHolding(row); word != "" {
		ink := pal.dim
		if row.NeedsPerson() {
			ink = pal.accent
		}
		state = append(state, ink(fit(word, width)))
	}
	if reason := row.Reason(); reason != "" {
		for _, wrapped := range wrap(reason, width) {
			state = append(state, pal.ink(wrapped))
		}
	}
	bands = append(bands, state)

	// THE WORK, WITH WHAT IT CAME TO. The outcome sentence is the most
	// informative text this program holds about a finished task and nothing has
	// ever drawn it; a row that says "done" and nothing else makes a person open
	// the conversation to find out what "done" meant.
	var work []string
	shown := row.Tasks.Rows
	if len(shown) > homeTaskRows {
		shown = shown[:homeTaskRows]
	}
	for _, entry := range shown {
		work = append(work, homeTaskLine(entry, row, now, width, pal))
		if outcome := strings.TrimSpace(entry.Outcome); outcome != "" && width > homeOutcomeIndent+16 {
			work = append(work, strings.Repeat(" ", homeOutcomeIndent)+
				pal.dim(fit(outcome, width-homeOutcomeIndent)))
		}
	}
	bands = append(bands, work)

	if last := a.homeLast(row); last != "" {
		var said []string
		for _, wrapped := range wrap(last, width) {
			said = append(said, pal.dim(wrapped))
		}
		bands = append(bands, said)
	}

	if facts := homeFacts(row, now); facts != "" {
		bands = append(bands, []string{pal.dim(fit(facts, width))})
	}

	return homeBands(bands, room)
}

// homeOutcomeIndent is where an outcome sentence hangs under the task it
// belongs to — far enough in to read as a continuation rather than as another
// task.
const homeOutcomeIndent = 2

// homeBands assembles the card, dropping whole bands from the bottom until it
// fits and putting one blank line between the ones that survive.
//
// IT DROPS AND NEVER TRUNCATES. Half a band is a band that lies about how much
// there was; a band that is not there is simply a fact this frame had no room
// for, and the frame is one keystroke from being taller.
func homeBands(bands [][]string, room int) []string {
	// The title is bands[0] and is not up for negotiation.
	for len(bands) > 1 {
		if homeBandLines(bands) <= room {
			break
		}
		bands = bands[:len(bands)-1]
	}
	var out []string
	for _, band := range bands {
		if len(band) == 0 {
			continue
		}
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, band...)
	}
	if len(out) > room {
		out = out[:room]
	}
	return out
}

// homeBandLines is how many screen lines a set of bands takes, blanks included.
func homeBandLines(bands [][]string) int {
	total, drawn := 0, 0
	for _, band := range bands {
		if len(band) == 0 {
			continue
		}
		if drawn > 0 {
			total++
		}
		total += len(band)
		drawn++
	}
	return total
}

// homeFacts is the dim arithmetic under the card: what this conversation has
// spent, what it weighed, and when it was last touched.
//
// EVERY FACT IS OMITTED WHEN IT IS NOT ONE. The emptiness law is at its most
// literal on a line like this — a footer reading "$0.00 · 0 tok · last active"
// is three absences dressed as three facts — so each part appears only when
// there is something to say, and a footer with nothing to say is not drawn.
func homeFacts(row session.SessionRow, now time.Time) string {
	var parts []string
	if row.Tasks.Spend > 0 {
		parts = append(parts, "spent "+dollars(row.Tasks.Spend))
	}
	if row.Tasks.Tokens > 0 {
		parts = append(parts, tokenWord(row.Tasks.Tokens)+" tokens")
	}
	// The later of "somebody spoke" and "work landed": both are this
	// conversation being active, and the footer is asked when, not how.
	touched := row.At
	if row.Tasks.Newest.After(touched) {
		touched = row.Tasks.Newest
	}
	if age := sinceAt(touched, now); age != "" {
		parts = append(parts, "last active "+age)
	}
	return strings.Join(parts, " · ")
}

// homeHolding says whether a window has this conversation open right now and
// what it is doing, and says nothing at all when nobody has it.
//
// TWO FACTS, AND THE SECOND IS THE CONVERSATION'S OWN. That a window holds the
// journal is the kernel's answer, taken as a lock asked as a question; what it
// is DOING is the conversation saying so in its presence file, believed only
// while it keeps saying it (session's world.go). A window open under a build
// too old to say gets the first half and no second, which is exactly as much as
// is known about it.
func (a *app) homeHolding(row session.SessionRow) string {
	word := ""
	switch {
	case row.Transcript == a.file:
		word = "open here"
	case row.Open || row.Live:
		word = homeHeldWord
	default:
		return ""
	}
	// `idle` is the ordinary state of an open conversation and adding it would
	// put a word on every row that carries no news (the emptiness law applied to
	// a state rather than to a number).
	if doing := row.Doing(); doing != "" && doing != string(session.PresenceIdle) {
		word += " · " + doing
	}
	return word
}

// homeTaskLine is one piece of work in the detail column: what it came to, what
// it was, and when.
func homeTaskLine(entry session.TaskIndexEntry, row session.SessionRow, now time.Time, width int, pal palette) string {
	word := homeTaskWord(entry, row)
	age := sinceAt(entry.EndedAt, now)
	label := entry.Label
	if label == "" {
		label = entry.Title
	}
	room := width - ansi.StringWidth(word) - 1
	if age != "" {
		room -= ansi.StringWidth(age) + 1
	}
	if room < 8 {
		return pal.dim(fit(word+" "+label, width))
	}
	label = fit(label, room)
	line := pal.dim(word) + " " + pal.muted(label)
	if age != "" {
		gap := width - ansi.StringWidth(word) - 1 - ansi.StringWidth(label) - ansi.StringWidth(age)
		if gap < 1 {
			gap = 1
		}
		line += strings.Repeat(" ", gap) + pal.dim(age)
	}
	return line
}

// homeTaskWord is what one row of the index is called on screen, and it says
// `running` only where the count in [session.TaskRollup] said so — the two are
// the same judgement and it is made once, in session's world.go, not twice.
//
// A task takes its row when it starts and the index is append-only, so a
// machine that lost power leaves rows saying `running` for as long as the file
// exists. What settles it is the conversation itself: a live one names the nodes
// it has out, and a row it does not name is work that was under way when the
// window went — `incomplete`, the same word the interrupted-task outcome uses,
// and not a claim that something is happening.
func homeTaskWord(entry session.TaskIndexEntry, row session.SessionRow) string {
	switch {
	case entry.Live() && row.Runs(entry):
		return "running"
	case entry.Live():
		return "incomplete"
	case entry.Status == string(session.TaskFailed):
		return "failed"
	case entry.Status == string(session.TaskUnverified):
		return "needs your look"
	}
	return "done"
}

// homeLast is the last thing said in a conversation, read once per conversation
// and remembered.
//
// The read is a forward scan of the journal ([session.Peek]) with no lock and
// no replay, which is cheap enough on the keystroke that moves the cursor and
// far too expensive on every frame — hence the cache, which lives and dies with
// the screen.
func (a *app) homeLast(row session.SessionRow) string {
	if a.home.last == nil {
		a.home.last = map[string]session.Summary{}
	}
	summary, read := a.home.last[row.Transcript]
	if !read {
		summary, _ = session.Peek(row.Transcript)
		a.home.last[row.Transcript] = summary
	}
	return strings.TrimSpace(summary.Last)
}

// homeHint is the line under the foot: what the keyboard does, and what the box
// will do with what is in it.
func (a *app) homeHint() string {
	line, _ := a.home.focusedLine()
	switch {
	case line.kind == homeAction:
		// The two readings of the box, both said, because both are true of what
		// is on screen right now: enter sends it, ↓ walks into what it found.
		return "enter starts a new conversation and sends this · ↓ pick a match · esc clear"
	case line.kind == homeQuiet && line.folded:
		return "enter or → show them · esc close"
	case line.kind == homeQuiet:
		return "enter or ← fold them away · esc close"
	case a.home.searching():
		return "enter open · ↑ back to starting a new conversation · esc clear"
	}
	return "↑↓ move · enter open · esc close"
}

// ── the small arithmetic ────────────────────────────────────────────────────

// sinceAt is [since] measured from a reading's own instant rather than from
// now, so every age on one screen was taken at the same moment.
func sinceAt(at, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	if now.IsZero() {
		return since(at)
	}
	d := now.Sub(at)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m"
	case d < 24*time.Hour:
		return itoa(int(d/time.Hour)) + "h"
	case d < 30*24*time.Hour:
		return itoa(int(d/(24*time.Hour))) + "d"
	default:
		return at.Format("2 Jan")
	}
}
