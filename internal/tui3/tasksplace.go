package tui3

// THE TASKS PLACE IS ONE READING, AND EVERYTHING ON THE FRAME IS DRAWN FROM IT.
//
// The place answers "what has this machine run" — every project, every
// conversation, this window's own work included. Four authorities know part of
// that answer and no single one of them knows all of it:
//
//   - THE WORLD SCAN knows every project's index file, and it has already
//     settled whether a live-looking row is really still running.
//   - THIS PROJECT'S INDEX, held in memory and merged with this session's live
//     graph, is fresher than the file for the project the window is sitting in.
//   - THIS SESSION'S GRAPH is the only authority for work this window started
//     and has not landed: an ordinary task writes NO row into the project's
//     index until it finishes (internal/session's taskelsewhere.go says why).
//   - THE OTHER WINDOWS on this project are the only authority for THEIR
//     unlanded work, for the same reason.
//
// So the reading takes all four and DEDUPLICATES ON (SessionID, ID) — the pair
// internal/session states is what identifies one row — with the freshest
// authority winning. One piece of work is drawn once, however many of the four
// know about it.
//
// Everything below the reading is pure: it never touches disk and never reads a
// clock. The snapshot is taken when the place opens (and re-grouped, from the
// CACHED world, on a time-window key), so a resize, a cursor move or a frame can
// never walk a directory or find a different `now` halfway through one screen.

import (
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
	"github.com/charmbracelet/x/ansi"
)

type tasksSection int

const (
	tasksNeeds tasksSection = iota
	tasksRunning
	tasksToday
	tasksEarlier
)

// tasksItem is one piece of work as this place files it: the row itself, the
// conversation it came out of, and the two judgements the reading is not allowed
// to make twice — which section it belongs under, and whether it is happening.
type tasksItem struct {
	entry session.TaskIndexEntry
	// row is the conversation the work came out of, and it is a LABEL: it says
	// where to file the row on screen and it is never asked whether the work is
	// running. That answer travels on [tasksItem.runs] instead, from whichever
	// authority produced this row.
	row     session.SessionRow
	section tasksSection
	// runs is the ONE liveness answer for this row. It is decided once, by the
	// authority the row came from, and never recomputed — a screen that asked
	// twice is a screen that can say `running` on a row it filed under `earlier`.
	runs bool
	// away marks one piece of work ANOTHER window on this project has out at
	// this instant. It has no row in any index file — nothing lands one until
	// the work finishes — so the other window's own presence is the only place
	// it can be read from, and window is what that window is CALLED.
	away   bool
	window string
}

// pick reports whether the CURSOR may stand on this row.
//
// ANOTHER WINDOW'S WORK IS READ AND NOT PRESSED. The two doors this surface has
// onto a piece of work are a ROOM, which is a live lane onto a node in THIS
// session's graph, and the record CARD, which is minted out of a landed row.
// Work running in another window has neither — no node here to open, and nothing
// landed to point at — so the cursor steps over it rather than promising a door
// that does not exist ([app.taskSheetAwayRows] states the same law).
func (i tasksItem) pick() bool { return !i.away }

// tasksMine is what the window drawing this page knows that no file does yet.
//
// IT IS PLAIN DATA AND NOT A DOOR BACK TO THE SURFACE. Every row arrives with
// its liveness already settled by the one ladder that owns that judgement
// ([app.recordRuns], asked once in place_tasks.go), because a reading that could
// ask the app a question would be a reading that could ask it at paint time.
type tasksMine struct {
	// row is this conversation, as a row of the world — what a row of this
	// project's index is labelled with when the world scan has never seen the
	// conversation that ran it.
	row session.SessionRow
	// rows is this project's whole index with this session's live graph merged
	// over the top, each already judged.
	rows []tasksMineRow
	// away is what the other windows on this project have out right now.
	away []session.ElsewhereTask
}

// tasksMineRow is one of those rows: the work, and whether it is happening.
type tasksMineRow struct {
	entry session.TaskIndexEntry
	runs  bool
}

// tasksReading is everything drawing and routing need from one world reading.
//
// seen is deliberately retained even though state grouping does not use it: it
// is the look stamp paired with this snapshot, and a later place adapter must
// not need to reach back to disk to preserve that boundary.
type tasksReading struct {
	items []tasksItem
	win   session.UsageWindow
	seen  time.Time
	now   time.Time
}

// tasksKey is what identifies ONE piece of work across every authority: the
// conversation that ran it and the id inside that conversation. internal/session
// states the pair on [session.TaskIndexEntry.ID] — ids restart with every
// conversation, so an id alone names a different task in every one of them.
type tasksKey struct{ session, id string }

func tasksKeyOf(entry session.TaskIndexEntry) tasksKey {
	return tasksKey{session: strings.TrimSpace(entry.SessionID), id: strings.TrimSpace(entry.ID)}
}

// readTasks walks every authority once and files what it finds under the
// question a person acts on next.
//
// THE AUTHORITIES ARE APPLIED WEAKEST FIRST and each overwrites what the one
// before it said about the same work, because that is what "freshest wins"
// means in one pass: the file, then this project's in-memory index and live
// graph, then the other windows — which are reading a presence file written
// seconds ago and are the only authority for work that has not landed.
func readTasks(world session.World, mine tasksMine, win session.UsageWindow, seen, now time.Time) tasksReading {
	r := tasksReading{win: win.Normalized(), seen: seen, now: now}
	// order keeps the pass stable: a map alone would re-order the page on every
	// frame it was rebuilt, and the sections below are drawn in the order the
	// rows arrived within each one.
	var order []tasksKey
	held := map[tasksKey]tasksItem{}
	put := func(key tasksKey, item tasksItem) {
		if _, seen := held[key]; !seen {
			order = append(order, key)
		}
		held[key] = item
	}

	for _, project := range world.Projects {
		for _, row := range project.Sessions {
			for _, entry := range row.Tasks.Rows {
				// The world scan has already judged this row against the
				// conversation that wrote it (world.go's first law), so its
				// answer travels with it.
				put(tasksKeyOf(entry), tasksItem{entry: entry, row: row, runs: row.Runs(entry)})
			}
		}
	}
	for _, row := range mine.rows {
		put(tasksKeyOf(row.entry), tasksItem{
			entry: row.entry, row: tasksRowFor(world, mine, row.entry), runs: row.runs,
		})
	}
	for _, task := range mine.away {
		// A TASK NOTHING NAMED IS LEFT OFF. A row with no words on it says
		// nothing a person can act on, which is the same refusal
		// [session.recordTaskIndexEntry] makes about writing one.
		title := strings.TrimSpace(task.Task.Title)
		if title == "" {
			continue
		}
		entry := session.TaskIndexEntry{
			ID: task.Task.ID, Label: title, Title: title,
			Status: task.Task.State, SessionID: task.SessionID,
		}
		put(tasksKeyOf(entry), tasksItem{entry: entry, runs: true, away: true, window: task.Session})
	}

	sections := [4][]tasksItem{}
	for _, key := range order {
		item := held[key]
		if !r.win.Holds(tasksEntryAt(item.entry, now)) {
			continue
		}
		item.section = tasksSectionOf(item, now)
		sections[item.section] = append(sections[item.section], item)
	}
	for _, section := range sections {
		r.items = append(r.items, section...)
	}
	return r
}

// tasksRowFor is the conversation to LABEL one of this project's rows with: the
// world's own row for the session that ran it where the scan found one, and this
// conversation otherwise — which is the honest answer for work this window
// started and no file has heard about yet.
func tasksRowFor(world session.World, mine tasksMine, entry session.TaskIndexEntry) session.SessionRow {
	if id := strings.TrimSpace(entry.SessionID); id != "" {
		for _, project := range world.Projects {
			for _, row := range project.Sessions {
				if row.ID == id {
					return row
				}
			}
		}
	}
	return mine.row
}

// tasksSectionOf files one piece of work under the question a person acts on
// next, which is the whole ordering of this place.
func tasksSectionOf(item tasksItem, now time.Time) tasksSection {
	switch {
	case item.entry.Status == string(session.TaskUnverified) || (item.row.NeedsPerson() && item.runs):
		return tasksNeeds
	case item.runs:
		return tasksRunning
	case tasksLandedToday(item.entry, now):
		return tasksToday
	}
	return tasksEarlier
}

func tasksLandedToday(entry session.TaskIndexEntry, now time.Time) bool {
	if entry.Status != string(session.TaskDone) && entry.Status != string(session.TaskFailed) {
		return false
	}
	if entry.EndedAt.IsZero() {
		return false
	}
	y, m, d := now.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	return !entry.EndedAt.Before(start) && entry.EndedAt.Before(start.AddDate(0, 0, 1))
}

// ── the layout ──────────────────────────────────────────────────────────────

// THE PAINT AND THE POINTER READ ONE LAYOUT. They used to be two walks of the
// same rows counting the same headings, and two answers to "which line is the
// third earlier row on" is exactly how a click opens the wrong task.

type tasksLineKind uint8

const (
	// tasksLineWord is a line of the page's own prose: the header sentence, or
	// a section's word. It answers to nothing.
	tasksLineWord tasksLineKind = iota
	// tasksLineAir is the blank line between two sections. GROUPS ARE SEPARATED
	// BY WHITESPACE AND NEVER BY A DIVIDER on this surface.
	tasksLineAir
	// tasksLineTask is one piece of work.
	tasksLineTask
	// tasksLineTail is the second line of a phone card — the same row continued
	// (taskphone.go), never a row of its own.
	tasksLineTail
)

type tasksLine struct {
	kind tasksLineKind
	// text is the words on a prose line.
	text string
	// item is the work a task line and its tail are about.
	item tasksItem
	// owner is the line index of the task this line belongs to, and -1 on prose
	// and air. It is what the cursor stands on, what a press resolves to, and
	// what stops a card's second line reading as a second row.
	owner int
}

// tasksBareLead is the two cells in front of every row of work. On the row a
// person is on they carry the mark in the accent — `›` where the keyboard is and
// `·` where the pointer is — which is what every other list on this surface
// leads with ([overlayLead]); the lead is chosen by the frame and passed in,
// because the reading does not know where anybody is standing.
const tasksBareLead = "  "

// lay is the one walk of the reading: what line the page draws, in order, and
// which of them a person can act on.
func (r tasksReading) lay(width int) []tasksLine {
	if width <= 0 || len(r.items) == 0 {
		return nil
	}
	lines := make([]tasksLine, 0, len(r.items)+8)
	add := func(kind tasksLineKind, text string) {
		lines = append(lines, tasksLine{kind: kind, text: text, owner: -1})
	}
	add(tasksLineWord, r.head())
	phone := layoutTier(width) == tierPhone
	for _, section := range []tasksSection{tasksNeeds, tasksRunning, tasksToday, tasksEarlier} {
		items := r.section(section)
		// A SECTION WITH NOTHING IN IT IS NOT DRAWN AT ALL, filter or no filter.
		// It is the emptiness law: a `running` word with a blank under it says
		// the query found something and lost it.
		if len(items) == 0 {
			continue
		}
		add(tasksLineAir, "")
		add(tasksLineWord, tasksSectionWord(section))
		for _, item := range items {
			at := len(lines)
			lines = append(lines, tasksLine{kind: tasksLineTask, item: item, owner: at})
			// phone lane: a row becomes a two-line card a thumb goes into
			// (taskphone.go), and the second line belongs to the first.
			if phone && tasksCardTail(item, r.now) != "" {
				lines = append(lines, tasksLine{kind: tasksLineTail, item: item, owner: at})
			}
		}
	}
	return lines
}

// rows is the whole page painted with no cursor anywhere on it, which is what a
// reader of the reading alone sees.
func (r tasksReading) rows(width int, pal palette) []string {
	lines := r.lay(width)
	out := make([]string, len(lines))
	for i := range lines {
		out[i] = r.paint(lines, i, width, pal, tasksBareLead)
	}
	return out
}

// paint draws ONE line of the layout behind the lead the frame chose. Every line
// it returns is at most width cells, at every width.
func (r tasksReading) paint(lines []tasksLine, i, width int, pal palette, lead string) string {
	if i < 0 || i >= len(lines) || width <= 0 {
		return ""
	}
	line := lines[i]
	room := width - ansi.StringWidth(lead)
	if room < 1 {
		room = 1
	}
	switch line.kind {
	case tasksLineAir:
		return ""
	case tasksLineWord:
		if i == 0 {
			return pal.muted(fit(line.text, width))
		}
		return pal.dim(fit(line.text, width))
	case tasksLineTail:
		indent := strings.Repeat(" ", taskSheetPhoneIndent)
		tail := room - taskSheetPhoneIndent
		if tail < 1 {
			tail = 1
		}
		return lead + indent + pal.dim(fit(tasksCardTail(line.item, r.now), tail))
	}
	if layoutTier(width) == tierPhone {
		return lead + tasksCardHead(line.item, room, pal)
	}
	return lead + tasksRow(line.item, room, r.now, pal)
}

// at is the work drawn on one painted line, and whether the cursor may stand
// there. Prose, air and another window's rows are deliberately holes.
func (r tasksReading) at(lines []tasksLine, i int) (tasksItem, bool) {
	if i < 0 || i >= len(lines) || lines[i].kind != tasksLineTask {
		return tasksItem{}, false
	}
	if !lines[i].item.pick() {
		return tasksItem{}, false
	}
	return lines[i].item, true
}

// head is the sentence the page opens on: what this place is, how much of it
// there is, how far back it reaches and what it cost.
//
// THE WINDOW EDGE IS SAID HERE AND NOWHERE ELSE. It used to be repeated on a
// fold at the foot of every section, which is one number in two places and the
// drift the one-source-of-truth law exists to stop.
func (r tasksReading) head() string {
	word := fmt.Sprintf("work aforge ran on its own. %d", len(r.items))
	if start := tasksWindowStart(r.win); start != "" {
		word += " since " + start
	}
	var cost float64
	for _, item := range r.items {
		cost += item.entry.Cost
	}
	if cost > 0 {
		word += ", " + dollars(cost) + " of it"
	}
	return word + "."
}

func tasksWindowStart(win session.UsageWindow) string {
	win = win.Normalized()
	if win.From.IsZero() {
		return ""
	}
	return strings.ToLower(win.From.Format("Jan 2"))
}

func (r tasksReading) section(want tasksSection) []tasksItem {
	items := make([]tasksItem, 0)
	for _, item := range r.items {
		if item.section == want {
			items = append(items, item)
		}
	}
	return items
}

// tasksTally is what the place says it is holding, in the same words its
// sections are headed with.
//
// THE EMPTINESS LAW HOLDS HERE TOO. A section with nothing in it is not counted
// as zero — it is not mentioned — and the line is empty when the page is.
func (r tasksReading) tally() string {
	var segs []string
	for _, section := range []tasksSection{tasksNeeds, tasksRunning, tasksToday, tasksEarlier} {
		if n := len(r.section(section)); n > 0 {
			segs = append(segs, itoa(n)+" "+tasksSectionWord(section))
		}
	}
	return strings.Join(segs, railSep)
}

func tasksSectionWord(section tasksSection) string {
	switch section {
	case tasksNeeds:
		return "needs your look"
	case tasksRunning:
		return taskSheetNowHead
	case tasksToday:
		return "done today"
	default:
		return taskSheetPastHead
	}
}

// ── one row of work ─────────────────────────────────────────────────────────

// tasksLabel is what a row is CALLED, which is the label the index carries and
// the title behind it when nothing shortened one.
func tasksLabel(entry session.TaskIndexEntry) string {
	if label := strings.TrimSpace(entry.Label); label != "" {
		return label
	}
	return strings.TrimSpace(entry.Title)
}

// tasksRow is one piece of work on a wide frame. The label is the elastic
// column; every other cell either earns its room or disappears in the stated
// order, so no painted row can cross the terminal edge.
func tasksRow(item tasksItem, width int, now time.Time, pal palette) string {
	entry := item.entry
	glyph, glyphInk := tasksGlyph(item, pal)
	lead := glyphInk(glyph) + " "
	source := strings.TrimSpace(item.row.Title)
	if source == "" {
		source = strings.TrimSpace(item.row.Project)
	}
	parts := []tasksCell{
		{text: source, ink: pal.dim, drop: tasksDropSource},
		{text: tasksMiddle(entry), ink: pal.dim, drop: tasksDropMiddle},
		{text: session.TaskKindWord(entry.Kind), ink: pal.dim, drop: tasksDropKind},
	}
	if entry.Cost > 0 {
		parts = append(parts, tasksCell{text: dollars(entry.Cost), ink: placeMoneyInk(pal), drop: tasksDropCost})
	}
	// THE NOTE AND THE AGE ARE NEVER DROPPED. The note is the only place a row
	// says WHERE the work is or that a claim of running has nobody behind it,
	// and both are facts no other cell repeats.
	if note := tasksNote(item); note != "" {
		parts = append(parts, tasksCell{text: note, ink: pal.dim, drop: tasksDropNote})
	}
	parts = append(parts, tasksCell{text: sinceAt(tasksEntryAt(entry, now), now), ink: pal.dim, drop: tasksDropAge})
	if width < 80 {
		parts = tasksOmit(parts, tasksDropMiddle)
	}
	for tasksFixedWidth(parts)+ansi.StringWidth(lead)+8 > width {
		before := len(parts)
		for _, drop := range []tasksDrop{tasksDropMiddle, tasksDropCost, tasksDropSource, tasksDropKind} {
			parts = tasksOmit(parts, drop)
			if len(parts) < before {
				break
			}
		}
		if len(parts) == before {
			break
		}
	}
	fixed := tasksFixedWidth(parts)
	labelRoom := width - ansi.StringWidth(lead) - fixed
	if labelRoom < 0 {
		labelRoom = 0
	}
	label := fit(tasksLabel(entry), labelRoom)
	left := lead + tasksLabelInk(item, pal)(label)
	used := ansi.StringWidth(left)
	pad := width - used - fixed
	if pad < 0 {
		pad = 0
	}
	var b strings.Builder
	b.WriteString(left)
	b.WriteString(strings.Repeat(" ", pad))
	for _, part := range parts {
		if part.text != "" {
			b.WriteByte(' ')
			b.WriteString(part.ink(part.text))
		}
	}
	return fit(b.String(), width)
}

// tasksCardHead is the first line of a phone card: the state and the name, in
// the same hue the wide row paints them.
func tasksCardHead(item tasksItem, width int, pal palette) string {
	glyph, glyphInk := tasksGlyph(item, pal)
	lead := glyphInk(glyph) + " "
	room := width - ansi.StringWidth(lead)
	if room < 1 {
		room = 1
	}
	return lead + tasksLabelInk(item, pal)(fit(tasksLabel(item.entry), room))
}

// tasksCardTail is the second line of a phone card: what the work came to, where
// it is happening, and how long ago — each dropped when it has nothing behind
// it, so a card with nothing to add stays one line.
func tasksCardTail(item tasksItem, now time.Time) string {
	var segs []string
	if middle := tasksMiddle(item.entry); middle != "" {
		segs = append(segs, middle)
	}
	if note := tasksNote(item); note != "" {
		segs = append(segs, note)
	}
	if age := sinceAt(tasksEntryAt(item.entry, now), now); age != "" {
		segs = append(segs, age)
	}
	return strings.Join(segs, railSep)
}

// tasksLabelInk is THE HUE AS THE CLAIM: dulled means this is the record, and a
// row that is genuinely running is not the record. The ink is the one a running
// node's title wears in the column ([app.railTitle]), so a person who has
// watched work run recognizes it here without learning a second signal.
func tasksLabelInk(item tasksItem, pal palette) func(string) string {
	if item.runs {
		return pal.ink
	}
	return pal.muted
}

// tasksNote is what one row says about WHERE the work is, or about a claim of
// running with nobody behind it.
//
// THE RECORD IS A FILE AND THE FILE CANNOT CORRECT ITSELF (taskview.go states
// the law). A row takes the word `running` when the work starts and nothing
// rewrites it, so a window that was killed leaves rows claiming a present that
// ended hours ago. What is drawn is [taskRecordStoppedWord] — not a judgement
// about the work, only the fact that the window went.
func tasksNote(item tasksItem) string {
	if item.away {
		return taskAwayNote(item.window)
	}
	if item.entry.Live() && !item.runs {
		return taskRecordStoppedWord
	}
	return ""
}

type tasksDrop int

const (
	tasksDropMiddle tasksDrop = iota
	tasksDropCost
	tasksDropSource
	tasksDropKind
	tasksDropNote
	tasksDropAge
)

type tasksCell struct {
	text string
	ink  func(string) string
	drop tasksDrop
}

func tasksOmit(parts []tasksCell, drop tasksDrop) []tasksCell {
	for i, part := range parts {
		if part.drop == drop && part.text != "" {
			return append(parts[:i:i], parts[i+1:]...)
		}
	}
	return parts
}

func tasksFixedWidth(parts []tasksCell) int {
	width := 0
	for _, part := range parts {
		if part.text != "" {
			width += 1 + ansi.StringWidth(part.text)
		}
	}
	return width
}

// tasksEntryAt is WHEN a row is, for the time window and for the age on it: the
// moment it landed, and NOW for work that is still going or that nothing dated.
// An undated row is filed at the moment of the reading rather than dropped — a
// window cannot judge a row with no stamp on it, and dropping it would take work
// that stopped off every surface this program has.
func tasksEntryAt(entry session.TaskIndexEntry, now time.Time) time.Time {
	if entry.Live() || entry.EndedAt.IsZero() {
		return now
	}
	return entry.EndedAt
}

func tasksMiddle(entry session.TaskIndexEntry) string {
	if activity := strings.TrimSpace(entry.Activity); activity != "" {
		return activity
	}
	if entry.Status == string(session.TaskFailed) && strings.TrimSpace(entry.Outcome) != "" {
		return "gave up, said why"
	}
	var parts []string
	if entry.FilesChanged > 0 {
		parts = append(parts, itoa(entry.FilesChanged)+plural(" file", entry.FilesChanged))
	}
	if outcome := strings.TrimSpace(entry.Outcome); outcome != "" {
		parts = append(parts, outcome)
	}
	return strings.Join(parts, " · ")
}

func tasksGlyph(item tasksItem, pal palette) (string, func(string) string) {
	if item.section == tasksNeeds {
		return tokens.GlyphNeedsHuman, pal.warn
	}
	// A ROW NOTHING IS RUNNING DOES NOT WEAR THE RUNNING GLYPH. [glyphIdle] takes
	// its place — the dot this surface already spends on a call that was still
	// going when its turn ended — because a frozen spinner would claim the work
	// is alive and a dot claims nothing.
	if item.entry.Live() && !item.runs {
		if pal.ascii {
			return glyphIdleASCII, pal.dim
		}
		return glyphIdle, pal.dim
	}
	switch item.entry.Status {
	case string(session.TaskRunning):
		return tokens.GlyphWorking, pal.live
	case string(session.TaskQueued):
		return tokens.GlyphQueued, pal.dim
	case string(session.TaskDone):
		return tokens.GlyphSettled, pal.muted
	case string(session.TaskFailed):
		return tokens.GlyphFailed, pal.bad
	default:
		return tokens.GlyphQueued, pal.dim
	}
}

// step keeps the four time keys in one grammar shared with spend.
func (r tasksReading) step(win session.UsageWindow, key string) session.UsageWindow {
	switch key {
	case "shift+left":
		return win.Step(-1)
	case "shift+right":
		return win.Step(1)
	case "shift+up":
		return win.Coarser()
	case "shift+down":
		return win.Finer()
	default:
		return win
	}
}

// tasksTeach spends an empty page on explaining the place rather than drawing
// headings for lists that do not exist.
//
// IT IS THE ONLY PLACE THE PROSE MAY APPEAR, and it is reached only from the tab
// bar: `ctrl+.` and /history REFUSE to raise a page with nothing on it
// ([app.openTaskSheet]), so the teaching is what a person who walked in with
// `tab` finds rather than what a person who asked for their history is answered
// with.
func tasksTeach(pal palette) []string {
	return []string{
		pal.dim("tasks is the history of work this machine has run."),
		pal.dim("it lists work aforge ran on its own, across every project."),
		pal.dim("enter opens a task's room when there is one here."),
	}
}
