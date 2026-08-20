package tui3

import (
	"path/filepath"
	"sort"
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
// shows. Six is the depth at which a list still answers "what has this been
// doing" rather than becoming the project's whole history, which is what the
// task index itself is for.
const homeTaskRows = 6

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
const homeMinDetail = 76

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
	homeFootWord = "type start something new · @ find · enter open"
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
	// homeQuiet is the collapsed tail of a project — "…2 more, quiet since
	// Tue". It is not a cursor stop either: it is a fact about what is not
	// being shown, not a thing to open.
	homeQuiet
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
	// when nobody has been in them.
	quiet int
	since time.Time
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

	// box is the one line at the foot, and it is TWO things depending on what
	// is in it: a message being started, or — with a leading `@` — a search
	// over the left column. One box rather than two, because a person's fingers
	// do not choose a mode before they choose a word.
	box editor

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
	a.closeLists()
	a.dismissWelcome()
	a.home = homeView{
		open:   true,
		world:  session.ReadWorld(a.placesRoot()),
		bucket: homeBucketOf(a.file),
		hover:  -1,
		last:   map[string]session.Summary{},
	}
	a.home.build()
	a.touch()
	return homeTick()
}

func (a *app) closeHome() {
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
	// [homeView.build] is the one that keeps the cursor on its conversation, so
	// this is a rescan and a rebuild and nothing else.
	a.home.build()
	a.touch()
}

// build turns the world into lines, applying the filter when one is typed.
func (h *homeView) build() {
	previous := h.focused()
	h.lines = h.lines[:0]
	if query, searching := h.query(); searching {
		h.buildFound(query)
	} else {
		h.buildWorld()
	}
	// THE CURSOR FOLLOWS THE CONVERSATION AND NOT THE LINE NUMBER. A filter
	// typed one letter at a time, and a rescan that re-sorts around work
	// starting, both rebuild this list under a cursor — and a cursor that held
	// its position would land on whatever happened to sort into row seven. So it
	// goes to the top of the new list, and comes back to the row it was on if
	// that row is still in it.
	h.cursor, h.top = h.clamp(0), 0
	if previous.Transcript != "" {
		h.point(previous.Transcript)
	}
}

// buildWorld is the ordinary column: projects as headings, their conversations
// under them, the quiet tail of each collapsed.
func (h *homeView) buildWorld() {
	for _, project := range h.world.Projects {
		if len(h.lines) > 0 {
			h.lines = append(h.lines, homeLine{kind: homeBlank})
		}
		h.lines = append(h.lines, homeLine{kind: homeHeading, project: project.Name, dir: project.Dir})
		shown, quiet, since := homeSplit(project.Sessions)
		for _, row := range shown {
			h.lines = append(h.lines, homeLine{
				kind: homeSession, project: project.Name, dir: project.Dir, row: row,
			})
		}
		if quiet > 0 {
			h.lines = append(h.lines, homeLine{
				kind: homeQuiet, project: project.Name, dir: project.Dir, quiet: quiet, since: since,
			})
		}
	}
}

// buildFound is the column under a filter: every conversation on the machine,
// ranked, with no headings and no collapse. A search is a flat answer — the
// project is on the row itself, because a heading over one hit is a heading
// that says less than the row under it.
func (h *homeView) buildFound(query string) {
	type hit struct {
		line  homeLine
		score int
	}
	var hits []hit
	tokens := strings.Fields(strings.ToLower(query))
	for _, project := range h.world.Projects {
		for _, row := range project.Sessions {
			text := strings.ToLower(homeName(row) + " " + project.Name)
			total, matched := 0, true
			for _, token := range tokens {
				score, ok := tokenScore(text, token)
				if !ok {
					matched = false
					break
				}
				total += score
			}
			if !matched {
				continue
			}
			hits = append(hits, hit{
				line:  homeLine{kind: homeSession, project: project.Name, dir: project.Dir, row: row},
				score: total,
			})
		}
	}
	// Lower is better, and ties keep the world's own order — which is triage
	// first and recency under it, so an empty-ish filter reads as the column
	// did before anything was typed.
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score < hits[j].score })
	for _, found := range hits {
		h.lines = append(h.lines, found.line)
	}
}

// homeSplit decides what a project shows and what it whispers: everything with
// work running or work left unfinished, then enough of the rest to reach
// [homeShown], and the remainder counted with the newest of their stamps.
func homeSplit(rows []session.SessionRow) (shown []session.SessionRow, quiet int, since time.Time) {
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
	return shown, quiet, since
}

// query is what is in the box after a leading `@`, and false when the box is
// not a search at all.
func (h *homeView) query() (string, bool) {
	text := h.box.String()
	if !strings.HasPrefix(text, "@") {
		return "", false
	}
	return strings.TrimSpace(text[1:]), true
}

// focused is the conversation under the cursor, and the zero row when the
// cursor is not on one.
func (h *homeView) focused() session.SessionRow {
	if h.cursor < 0 || h.cursor >= len(h.lines) || h.lines[h.cursor].kind != homeSession {
		return session.SessionRow{}
	}
	return h.lines[h.cursor].row
}

// focusedLine is [homeView.focused] with the project around it.
func (h *homeView) focusedLine() (homeLine, bool) {
	if h.cursor < 0 || h.cursor >= len(h.lines) || h.lines[h.cursor].kind != homeSession {
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
		if h.lines[i].kind == homeSession {
			return i
		}
	}
	for i := at; i >= 0; i-- {
		if h.lines[i].kind == homeSession {
			return i
		}
	}
	return at
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
		for next >= 0 && next < len(h.lines) && h.lines[next].kind != homeSession {
			next += step
		}
		if next < 0 || next >= len(h.lines) {
			break
		}
		at = next
	}
	h.cursor = at
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
	case "left", "ctrl+b":
		h.box.left()
		return nil
	case "right", "ctrl+f":
		h.box.right()
		return nil

	default:
		// TYPING IS THE WHOLE CEREMONY. Any printable key starts a message, or —
		// with a leading `@` — a search, and nothing had to be opened first.
		if text := msg.Key().Text; text != "" {
			h.box.insert(text)
			h.build()
		}
		return nil
	}
}

// homeEnter is the one decision this surface makes, and it makes a different
// one depending on what is in the box.
func (a *app) homeEnter() tea.Cmd {
	h := &a.home
	// A message typed into the box outranks the cursor: the person wrote a
	// sentence, and enter after a sentence has one meaning everywhere else on
	// this surface.
	if text := strings.TrimSpace(h.box.String()); text != "" && !strings.HasPrefix(text, "@") {
		return a.homeStart(text)
	}
	line, ok := h.focusedLine()
	if !ok {
		return nil
	}
	switch {
	case line.row.Transcript == a.file:
		// The conversation this window is already in. Reopening it would drop
		// the lock, replay the journal and land exactly here — the resume
		// picker's words, for the same second of work (resume.go).
		a.closeHome()
		a.note("already here · " + homeName(line.row))
		return nil
	case !a.homeOpens(line):
		h.msg = homeElsewhereWord + " · " + homeWhere(line)
		return nil
	}
	chosen := Session{
		Title: line.row.Title,
		File:  line.row.Transcript,
		At:    line.row.At,
	}
	a.closeHome()
	// THE SAME DOOR THE RESUME PICKER WALKS THROUGH, not a second one: closing
	// the agent, opening the chosen journal, replaying it and re-subscribing the
	// standing lanes is one arrangement, and two of them would be two things to
	// keep in step (welcome.go's [app.resumeSession]).
	return a.resumeSession(chosen)
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
	if at < 0 || at >= len(a.home.lines) || a.home.lines[at].kind != homeSession {
		return nil
	}
	if a.home.cursor == at {
		return a.homeEnter()
	}
	a.home.cursor = at
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
		if at := hits[y]; at >= 0 && at < len(a.home.lines) && a.home.lines[at].kind == homeSession {
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
		add(" "+pal.bad(fit(a.home.msg, width-2)), -1)
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
	return left, width - left - 2
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
			pad := left - ansi.StringWidth(text)
			if pad < 0 {
				pad = 0
			}
			text += strings.Repeat(" ", pad+2) + detail[i]
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
		if _, searching := h.query(); searching {
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
		return "  " + pal.dim(fit(homeQuietWord(line, h.world.Read), width-2))
	}
	label := homeGlyph(line.row, pal.ascii) + " " + homeName(line.row)
	note := homeNote(line.row, h.world.Read)
	if _, searching := h.query(); searching && line.project != "" {
		// A flat answer has no heading over it, so the project — and whether
		// this window can open it — rides on the row.
		note = line.project
		if !a.homeOpens(line) {
			note += " · " + homeElsewhereWord
		}
	}
	return overlayRow(label, note, at == h.cursor, line.row.Transcript == a.file, at == h.hover, width, pal)
}

// homeQuietWord is the collapsed tail's one line. The age is the newest of the
// conversations it stands for, so "quiet since" is a fact about the whole group
// rather than about whichever one sorted last.
func homeQuietWord(line homeLine, now time.Time) string {
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
func homeNote(row session.SessionRow, now time.Time) string {
	var parts []string
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

// homeDetail is the right column: the focused conversation, whole.
func (a *app) homeDetail(width, room int, pal palette) []string {
	line, ok := a.home.focusedLine()
	if !ok {
		return nil
	}
	row := line.row
	lines := []string{pal.ink(fit(homeName(row), width))}

	place := line.project
	if path := strings.TrimSpace(row.ProjectDir); path != "" && path != place {
		place += " · " + path
	}
	lines = append(lines, pal.dim(fit(place, width)))
	if word := a.homeHolding(row); word != "" {
		lines = append(lines, pal.dim(fit(word, width)))
	}
	// THE QUESTION IT IS STOPPED ON IS THE ONE THING ON THIS PANE THAT IS NOT
	// DIM. Everything else here is a fact about what happened; this is a thing
	// somebody has to do, and it is the whole reason the row sorted to the top
	// of its project. It draws nothing at all when the conversation gave no
	// words for what it is waiting on (the emptiness law, and
	// [session.SessionPresence.Reason]'s own instruction).
	if reason := row.Reason(); reason != "" {
		lines = append(lines, "")
		for _, wrapped := range wrap(reason, width) {
			lines = append(lines, pal.ink(wrapped))
		}
	}

	if len(row.Tasks.Rows) > 0 {
		lines = append(lines, "")
		shown := row.Tasks.Rows
		if len(shown) > homeTaskRows {
			shown = shown[:homeTaskRows]
		}
		for _, entry := range shown {
			lines = append(lines, homeTaskLine(entry, row, a.home.world.Read, width, pal))
		}
	}
	// Spend only when there is spend to name — see [homeNote].
	if row.Tasks.Spend > 0 {
		lines = append(lines, "", pal.dim(fit("spent "+dollars(row.Tasks.Spend), width)))
	}
	if last := a.homeLast(row); last != "" {
		lines = append(lines, "")
		for _, wrapped := range wrap(last, width) {
			lines = append(lines, pal.dim(wrapped))
		}
	}
	if len(lines) > room {
		lines = lines[:room]
	}
	return lines
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
		word = "open in another window"
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
	if query, searching := a.home.query(); searching {
		if query == "" {
			return "type to search every conversation · enter open · esc clear"
		}
		return "enter open · esc clear"
	}
	if !a.home.box.empty() {
		return "enter starts a new conversation here and sends this · esc clear"
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
