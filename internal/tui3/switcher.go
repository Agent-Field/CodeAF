package tui3

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
)

// ── THE SWITCHER'S READING ──────────────────────────────────────────────────
//
// HOME IS A SWITCHER, NOT A DIRECTORY, AND THIS FILE IS THE WHOLE OF WHAT IT
// READS (SCREEN 1a). Data in — a world, the standing bands, where this window is
// standing, a look stamp and a clock — rows, stops and verbs out.
//
// IT IS PURE AND IT MUST STAY PURE. Nothing here takes an *app, starts a clock,
// opens a file or asks the disk anything: the facts are gathered on home's own
// three-second beat and handed in whole (docs/design/home-rethink/ARCHITECTURE.md
// states the three layers and which may know what). That is what lets the whole
// reading be tested with fixtures at sixty, eighty, a hundred and twenty and two
// hundred columns, and what keeps the place that draws it (place_home.go) down
// to a cursor, two view flags and a fold.

const switcherShown = 8

// switcherVerb is one thing the strip can offer for a row: the letter, the word
// it is spelled with, and — for the two verbs that ANSWER a question — the
// option key that answer has to be sent under.
//
// THE ANSWER KEY IS CARRIED AND NEVER DERIVED. A question's options are the ones
// that session offered ("1", "3", sometimes "2"), and a strip that recomputed
// them from the letter it drew would be answering a different question than the
// one on the row (homeband_answer.go's [app.answerKey] holds the same law for
// the digits).
type switcherVerb struct {
	key    rune
	word   string
	answer string
}

// switcherView is HOW this reading is shown, as opposed to what is in it: the
// three things `alt+g`, `alt+q` and the fold line change about one list of
// facts. They travel together because they are one question — what shape is this
// list in — and a reader that took three bools in a row would be a reader whose
// call sites are three unlabelled trues.
type switcherView struct {
	// grouped is `alt+g`: the flat ranked list becomes one block per project.
	grouped bool
	// hideQuiet is `alt+q`: nothing that is neither asking nor moving is drawn,
	// and the fold at the foot says so in one word.
	hideQuiet bool
	// all is the fold standing open — every row drawn, with no cap at all. It is
	// a door and not a setting ([homeQuiet] states the rule this inherits): a
	// line that says rows are being hidden and cannot be asked to stop hiding
	// them is a dead end somebody hits and gives up at.
	all bool
}

type switcherLedgerInput struct {
	learned int
	letGo   int
}

type switcherKind uint8

const (
	switcherConversation switcherKind = iota
	switcherStanding
	switcherLedger
	switcherFold
)

// switcherRow holds every kind of door the router can open. Zero fields are
// deliberately meaningful: a row never fabricates an address it was not given.
type switcherRow struct {
	kind     switcherKind
	session  session.SessionRow
	item     StandingItemView
	place    string
	project  string
	title    string
	note     string
	age      string
	at       time.Time
	needs    bool
	moving   bool
	paused   bool
	here     bool
	fold     bool
	foldWord string
	options  []session.AnswerOption
}

type switcherLine struct {
	row     *switcherRow
	heading string
	section bool
	blank   bool
}

type switcherReading struct {
	lines        []switcherLine
	chatCount    int
	hasAttention bool
	view         switcherView
	now          time.Time
	// hidden is how many rows the fold at the foot is standing for, and zero
	// when there is no fold. It is what the door needs to know whether opening
	// it would show anything.
	hidden int
}

// switcherHere is WHERE THIS WINDOW IS STANDING, and it is two addresses because
// two different questions are asked of it: `session` is the conversation on
// screen — the one row that wears `here` instead of an age — and `project` is
// the bucket it lives in, which is what puts a person's own project first when
// `alt+g` groups the list.
//
// THE CONVERSATION IS THE EXACT ANSWER AND THE PROJECT IS THE BROAD ONE. A
// window standing in a project with no conversation of its own has the second
// and not the first, and a reading that only had the project would have to guess
// which of its rows was `here` (it used to, and it guessed the first open one).
type switcherHere struct {
	session string
	project string
}

// readSwitcher uses the same attention rules as homeattention.go: NeedsPerson
// outranks everything; moving is Tasks.Running or a fresh PresenceWorking
// conversation, and a standing item moves only while view.Running. An item's
// own NeedsPerson likewise outranks its running marker.
func readSwitcher(world session.World, items map[string][]StandingItemView, here switcherHere, seen time.Time, now time.Time, view switcherView, ledger switcherLedgerInput) switcherReading {
	r := switcherReading{view: view, now: now}
	projectByDir := make(map[string]session.Project, len(world.Projects))
	var all []switcherRow
	for _, project := range world.Projects {
		projectByDir[filepath.Clean(project.Dir)] = project
		for _, row := range project.Sessions {
			if row.Archived {
				continue
			}
			r.chatCount++
			needs := row.NeedsPerson()
			moving := !needs && (row.Tasks.Running > 0 || row.Live && row.Presence.State == session.PresenceWorking)
			// EXACTLY THE ONE CONVERSATION THIS WINDOW IS HOLDING. A broader test
			// would put `here` on a row somebody would then press enter on and go
			// nowhere, which is the worst thing a word on a door can do.
			atHere := here.session != "" && ((row.Dir != "" && filepath.Clean(row.Dir) == filepath.Clean(here.session)) ||
				(row.ID != "" && row.ID == here.session))
			options := []session.AnswerOption(nil)
			if row.NeedsPerson() && strings.TrimSpace(row.Presence.Question.Text) != "" {
				options = append(options, row.Presence.Question.Options...)
			}
			all = append(all, switcherRow{
				kind: switcherConversation, session: row, project: project.Name,
				title: homeName(row), note: switcherConversationNote(row, seen), age: sinceAt(row.At, now),
				at: switcherSortAt(row), needs: needs, moving: moving, here: atHere,
				options: options,
			})
		}
		for _, view := range items[project.Dir] {
			if strings.TrimSpace(view.Item.NeedsPerson) == "" && !view.Running {
				continue
			}
			needs := strings.TrimSpace(view.Item.NeedsPerson) != ""
			all = append(all, switcherRow{
				kind: switcherStanding, item: view, project: project.Name,
				title: strings.TrimSpace(view.Item.Words), note: switcherStandingNote(view),
				age: sinceAt(switcherItemAt(view), now), at: switcherItemAt(view),
				needs: needs, moving: !needs && view.Running, paused: view.Item.Status == standing.StatusPaused,
			})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return switcherLess(all[i], all[j]) })
	for _, row := range all {
		if row.needs || row.moving {
			r.hasAttention = true
			break
		}
	}

	r.addLedger(items, world, seen, ledger)
	if view.grouped {
		r.addGrouped(all, here.project, projectByDir)
	} else {
		r.addFlat(all)
	}
	return r
}

// switcherCap is how many rows this reading draws before the rest go behind one
// door. It is [switcherShown] at rest and NO CAP AT ALL once the fold has been
// opened, which is the whole of what opening it means.
func (r switcherReading) cap(n int) int {
	if r.view.all {
		return n
	}
	return min(switcherShown, n)
}

func switcherSortAt(row session.SessionRow) time.Time {
	if row.NeedsPerson() {
		return attentionWaitedSince(row)
	}
	if row.Tasks.Running > 0 || row.Live && row.Presence.State == session.PresenceWorking {
		return attentionMovingSince(row)
	}
	return row.At
}

func switcherItemAt(view StandingItemView) time.Time {
	if strings.TrimSpace(view.Item.NeedsPerson) != "" {
		return view.Item.Updated
	}
	if view.Running {
		return view.Mark.Since
	}
	return view.Item.LastFired
}

func switcherLess(a, b switcherRow) bool {
	ra, rb := switcherRank(a), switcherRank(b)
	if ra != rb {
		return ra > rb
	}
	if ra == 3 { // A longer wait belongs first.
		return attentionOlder(a.at, b.at)
	}
	if ra == 2 { // More live work is the useful tie-break before recency.
		ba, bb := switcherBusy(a), switcherBusy(b)
		if ba != bb {
			return ba > bb
		}
	}
	return a.at.After(b.at)
}

func switcherRank(row switcherRow) int {
	if row.needs {
		return 3
	}
	if row.moving {
		return 2
	}
	return 1
}

func switcherBusy(row switcherRow) int {
	if row.kind == switcherConversation && row.session.Tasks.Running > 0 {
		return row.session.Tasks.Running
	}
	if row.moving {
		return 1
	}
	return 0
}

func switcherConversationNote(row session.SessionRow, seen time.Time) string {
	if row.NeedsPerson() {
		line := switcherFirstLine(row.Presence.Question.Text)
		if line == "" {
			line = switcherFirstLine(row.Reason())
		}
		if line == "" {
			return ""
		}
		if row.Presence.Question.Kind == session.QuestionConsent {
			return "wants to " + strings.TrimSpace(strings.TrimSuffix(line, "?"))
		}
		return "asks: " + line
	}
	if row.Tasks.Running > 0 {
		note := fmt.Sprintf("%d %s running", row.Tasks.Running, switcherPlural(row.Tasks.Running, "task", "tasks"))
		for _, entry := range row.Tasks.Rows {
			if row.Runs(entry) && strings.TrimSpace(entry.Activity) != "" {
				return note + " · " + switcherFirstLine(entry.Activity)
			}
		}
		return note
	}
	files := 0
	saved := false
	for _, entry := range row.Tasks.Rows {
		if !entry.EndedAt.After(seen) {
			continue
		}
		files += entry.FilesChanged
		if session.TaskKindWord(entry.Kind) == "saved shape" {
			saved = true
		}
	}
	if files > 0 {
		return fmt.Sprintf("%d files made", files)
	}
	if saved {
		return "ran a saved shape"
	}
	return ""
}

func switcherStandingNote(view StandingItemView) string {
	if need := switcherFirstLine(view.Item.NeedsPerson); need != "" {
		return "asks: " + need
	}
	if view.Running && strings.TrimSpace(view.Mark.What) != "" {
		return switcherFirstLine(view.Mark.What)
	}
	return ""
}

func switcherFirstLine(s string) string {
	s = strings.TrimSpace(s)
	if at := strings.IndexByte(s, '\n'); at >= 0 {
		s = s[:at]
	}
	return strings.TrimSpace(s)
}

func switcherPlural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

func (r *switcherReading) addLedger(items map[string][]StandingItemView, world session.World, seen time.Time, input switcherLedgerInput) {
	var events []switcherRow
	for _, views := range items {
		for _, view := range views {
			if !view.Item.LastFired.After(seen) {
				continue
			}
			line := standing.LastLookLine(view.Item, r.now)
			if line == "" {
				line = switcherFirstLine(view.Item.LastCheckLine)
			}
			if line != "" {
				events = append(events, switcherRow{kind: switcherLedger, item: view, title: line, place: "standing", at: view.Item.LastFired})
			}
		}
	}
	landed := 0
	for _, row := range world.Sessions() {
		for _, entry := range row.Tasks.Rows {
			if entry.EndedAt.After(seen) && entry.Status != string(session.TaskRunning) && entry.Status != string(session.TaskQueued) {
				landed++
			}
		}
	}
	if input.learned > 0 || input.letGo > 0 {
		parts := []string{}
		if input.learned > 0 {
			parts = append(parts, fmt.Sprintf("learned %d %s", input.learned, switcherPlural(input.learned, "thing", "things")))
		}
		if input.letGo > 0 {
			parts = append(parts, fmt.Sprintf("let go of %d", input.letGo))
		}
		events = append(events, switcherRow{kind: switcherLedger, title: strings.Join(parts, ", "), place: "memory", at: r.now})
	}
	if landed > 0 {
		events = append(events, switcherRow{kind: switcherLedger, title: fmt.Sprintf("%d tasks landed", landed), place: "tasks", at: r.now})
	}
	if len(events) == 0 {
		return
	}
	sort.SliceStable(events, func(i, j int) bool { return events[i].at.After(events[j].at) })
	age := sinceAt(seen, r.now)
	head := "since you left"
	if age != "" {
		head += " · " + age
	}
	r.addSectionLine(switcherLine{heading: head})
	for i := range events {
		row := events[i]
		r.lines = append(r.lines, switcherLine{row: &row})
	}
}

func (r *switcherReading) addFlat(all []switcherRow) {
	if r.hasAttention {
		r.addSectionLine(switcherLine{section: true})
	}
	r.addRowsAndFold(all)
}

func (r *switcherReading) addGrouped(all []switcherRow, bucket string, projects map[string]session.Project) {
	var active, quiet []switcherRow
	for _, row := range all {
		if row.needs || row.moving {
			active = append(active, row)
		} else {
			quiet = append(quiet, row)
		}
	}
	selected := append([]switcherRow(nil), active...)
	if !r.view.hideQuiet {
		selected = append(selected, quiet...)
	}
	capped := min(switcherShown, len(selected))
	selected = selected[:r.cap(len(selected))]
	byProject := map[string][]switcherRow{}
	for _, row := range selected {
		byProject[row.project] = append(byProject[row.project], row)
	}
	type group struct {
		name string
		at   time.Time
		here bool
	}
	var groups []group
	for name, rows := range byProject {
		g := group{name: name}
		for _, row := range rows {
			if row.at.After(g.at) {
				g.at = row.at
			}
			g.here = g.here || row.here
		}
		for dir, project := range projects {
			if bucket != "" && dir != "" && project.Name == name && filepath.Clean(dir) == filepath.Clean(bucket) {
				g.here = true
			}
		}
		groups = append(groups, g)
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].here != groups[j].here {
			return groups[i].here
		}
		return groups[i].at.After(groups[j].at)
	})
	if r.hasAttention {
		r.addSectionLine(switcherLine{section: true})
	}
	for _, group := range groups {
		r.addSectionLine(switcherLine{heading: group.name})
		for _, row := range byProject[group.name] {
			copy := row
			r.lines = append(r.lines, switcherLine{row: &copy})
		}
	}
	hidden := len(active) + len(quiet) - capped
	if hidden > 0 {
		clause := ""
		if capped >= len(active) {
			quietAt := capped - len(active)
			if r.view.hideQuiet {
				clause = "quiet"
			} else if quietAt < len(quiet) && !quiet[quietAt].at.IsZero() {
				clause = "quiet since " + strings.ToLower(quiet[quietAt].at.Format("Jan 2"))
			}
		}
		r.hidden = hidden
		r.addFold(foldLine(hidden, clause))
	}
}

func (r *switcherReading) addRowsAndFold(all []switcherRow) {
	eligible := all
	if r.view.hideQuiet {
		eligible = nil
		for _, row := range all {
			if row.needs || row.moving {
				eligible = append(eligible, row)
			}
		}
	}
	// WHAT THE FOLD STANDS FOR IS COUNTED AT THE CAP AND NEVER AT WHAT IS DRAWN.
	// An opened fold draws every row and still says how many rows it is the door
	// over, because it is the way back — a fold that vanished when it was opened
	// would leave the list with no way to become a summary again.
	capped := min(switcherShown, len(eligible))
	shown := r.cap(len(eligible))
	for _, row := range eligible[:shown] {
		copy := row
		r.lines = append(r.lines, switcherLine{row: &copy})
	}
	if more := len(all) - capped; more > 0 {
		clause := ""
		if capped < len(all) && !all[capped].needs && !all[capped].moving {
			if r.view.hideQuiet {
				clause = "quiet"
			} else if !all[capped].at.IsZero() {
				clause = "quiet since " + strings.ToLower(all[capped].at.Format("Jan 2"))
			}
		}
		r.hidden = more
		r.addFold(foldLine(more, clause))
	}
}

// addFold puts the one door over everything this reading is not drawing, and
// wears the mark that says which way it goes — `▸` while it is hiding rows,
// `▾` once it has been opened, the same two marks every other fold on this
// surface uses.
func (r *switcherReading) addFold(word string) {
	mark := tokens.GlyphCollapsed
	if r.view.all {
		mark = tokens.GlyphExpanded
	}
	row := switcherRow{kind: switcherFold, fold: true, foldWord: mark + strings.TrimPrefix(word, tokens.GlyphCollapsed)}
	r.lines = append(r.lines, switcherLine{row: &row})
}

// addSectionLine keeps headings on the shared one-blank rhythm while leaving
// the first block flush with the top of its reading.
func (r *switcherReading) addSectionLine(line switcherLine) {
	for len(r.lines) > 0 && r.lines[len(r.lines)-1].blank {
		r.lines = r.lines[:len(r.lines)-1]
	}
	if len(r.lines) > 0 {
		r.lines = append(r.lines, switcherLine{blank: true})
	}
	r.lines = append(r.lines, line)
}

func (r switcherReading) rows(width int, pal palette) []string {
	if width < 1 {
		return nil
	}
	out := make([]string, 0, len(r.lines))
	for _, line := range r.lines {
		out = append(out, r.paint(line, width, pal, switcherPaint{}))
	}
	return out
}

// switcherPaint is what the SURFACE knows about one line that the reading
// cannot: where the keyboard and the pointer are standing, which heading the
// cursor is under, and which single row on the page is allowed to animate.
//
// IT IS THREE VALUES AND NOT AN *app, which is what keeps this file pure. Each
// of them is a fact the drawing surface holds and the reading has no way to ask
// for (docs/design/home-rethink/ARCHITECTURE.md's three layers).
type switcherPaint struct {
	sel   bool
	hover bool
	// head is the ink a HEADING takes. The heading over the section the cursor is
	// standing in steps up one ink tier and wears no ground (homesection.go); nil
	// is the dim every other heading takes.
	head func(string) string
	// spin is THE ONE MOVING CELL ON THE PAGE, and "" on every other row. However
	// many things are running, exactly one row animates (homespinner.go), so the
	// moving mark gives way to the turning cell on that row alone.
	spin string
}

// paint is one line of the reading, with the band on the row the keyboard or the
// pointer is standing on.
//
// THE BAND IS THE WHOLE OF THE SELECTION AND THERE IS NO LEAD. This list has its
// state mark in the first cell of every row (SCREEN 1a), so two more cells spent
// on a `›` would push every name two columns right for a fact the ground already
// carries — which is the one device SCREEN 2a names for the cursor: "the band —
// where the cursor is — selection, and the subject goes bold inside it".
func (r switcherReading) paint(line switcherLine, width int, pal palette, p switcherPaint) string {
	if width < 1 {
		return ""
	}
	switch {
	case line.section:
		left := "what wants you first"
		if r.chatCount > 0 {
			left = fmt.Sprintf("%d chats · %s", r.chatCount, left)
		}
		right := switcherGroupWord
		if ansi.StringWidth(left)+ansi.StringWidth(right)+3 <= width && ansi.StringWidth(left)+ansi.StringWidth(right)+ansi.StringWidth(" · "+switcherQuietWord)+3 <= width {
			right += " · " + switcherQuietWord
		}
		leftInk := pal.dim
		if p.head != nil {
			leftInk = p.head
		}
		return switcherSides(width, left, right, leftInk, pal.dim)
	case line.heading != "":
		ink := pal.dim
		if p.head != nil {
			ink = p.head
		}
		return ink(fit(line.heading, width))
	case line.row != nil:
		return switcherPaintRow(*line.row, width, pal, r.view.grouped, p)
	}
	return ""
}

// switcherResumeWord is the undoing of a pause, and it is spelled here because
// nothing else on this surface offers it: an item's card and the standing place
// both pause and stop, and only a row that is ALREADY paused has a resume.
const switcherResumeWord = "resume it"

// The two views this list offers and the keys that reach them. They are quoted
// on the section line and in the manual from this one spelling.
const (
	switcherGroupWord = "alt+g group by project"
	switcherQuietWord = "alt+q hide the quiet ones"
)

func switcherPaintRow(row switcherRow, width int, pal palette, grouped bool, p switcherPaint) string {
	if row.fold {
		return switcherBand(pal.dim(fit(row.foldWord, width)), width, pal, p)
	}
	if row.kind == switcherLedger {
		// A LEDGER LINE IS A DOOR, so it takes the band like any other stop, and
		// the place it names sits out at the right margin where every row's tail
		// sits.
		return switcherBand(switcherSides(width, row.title, row.place, pal.ink, pal.dim), width, pal, p)
	}
	glyph, glyphInk := tokens.GlyphQueued, pal.dim
	if row.paused {
		glyph = tokens.GlyphPaused
	}
	if row.moving {
		// THE ONE MOVING CELL. A row that is the page's spinner turns; every other
		// live row holds the still mark, which is what makes a machine with twenty
		// things out cost the wire exactly what a machine with one costs
		// (homespinner.go).
		glyph, glyphInk = tokens.GlyphWorking, pal.accent
		if p.spin != "" {
			glyph = p.spin
		}
	}
	if row.needs {
		glyph, glyphInk = tokens.GlyphNeedsHuman, pal.warn
	}
	age := row.age
	project, note := row.project, row.note
	if grouped {
		project = ""
	}
	if row.here {
		age = homeHereWord
	}
	if width < 80 {
		note = ""
	}
	parts := []string{project, note, age}
	for switcherTailWidth(parts)+ansi.StringWidth(glyph)+2+8 > width {
		if parts[1] != "" {
			parts[1] = ""
			continue
		}
		if parts[0] != "" {
			parts[0] = ""
			continue
		}
		if parts[2] != "" {
			parts[2] = ""
			continue
		}
		break
	}
	tail := switcherTailWidth(parts)
	room := max(0, width-ansi.StringWidth(glyph)-1-tail)
	// THE SUBJECT GOES BOLD INSIDE THE BAND and the tail steps up with it: dim
	// grey on a raised ground is grey on grey, which is the rule every row on
	// this surface is painted under (palette.go's [overlayRowTinted]).
	name, tailInk := pal.ink(fit(row.title, room)), pal.dim
	if p.sel || p.hover {
		name, tailInk = pal.bold(name), pal.ink
	}
	line := glyphInk(glyph) + " " + name
	used := ansi.StringWidth(line)
	if pad := width - used - tail; pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	for _, part := range parts {
		if part != "" {
			line += " " + tailInk(part)
		}
	}
	return switcherBand(fit(line, width), width, pal, p)
}

// switcherBand is the one ground this list paints: the row the keyboard is on,
// and the row the pointer is over, at the same rung — whether a person arrived
// with `↓` or with the mouse, the row they are on is the row they are on
// (palette.go's ladder).
func switcherBand(line string, width int, pal palette, p switcherPaint) string {
	if !p.sel && !p.hover {
		return line
	}
	return pal.cursor(line, width)
}

func switcherTailWidth(parts []string) int {
	n := 0
	for _, p := range parts {
		if p != "" {
			n += 1 + ansi.StringWidth(p)
		}
	}
	return n
}

func switcherSides(width int, left, right string, leftInk, rightInk func(string) string) string {
	if right == "" {
		return leftInk(fit(left, width))
	}
	if ansi.StringWidth(right) >= width {
		return rightInk(fit(right, width))
	}
	room := width - ansi.StringWidth(right) - 1
	l, lw := fitWidth(left, room)
	return leftInk(l) + strings.Repeat(" ", max(1, width-lw-ansi.StringWidth(right))) + rightInk(right)
}

func (r switcherReading) at(i int) (switcherRow, bool) {
	if i < 0 || i >= len(r.lines) || r.lines[i].row == nil {
		return switcherRow{}, false
	}
	return *r.lines[i].row, true
}

func (r switcherReading) verbs(i int) []switcherVerb {
	row, ok := r.at(i)
	if !ok {
		return nil
	}
	return switcherVerbsFor(row)
}

// switcherVerbsFor is the same answer taken from a row rather than from its
// position, which is what the surface holding these rows as lines of its own
// column needs (place_home.go).
func switcherVerbsFor(row switcherRow) []switcherVerb {
	if row.kind == switcherStanding {
		verbs := switcherQuestionVerbs(row.options)
		if row.paused {
			return append(verbs, switcherVerb{key: 'r', word: switcherResumeWord})
		}
		// ONE SPELLING FOR ONE VERB. `pause` is what an item's own card and the
		// standing place both call this act (homestanding.go's [homeItemActions],
		// verbstrip.go's [app.standRowVerbs]), and a strip that said it a second
		// way would be two words for one thing on one screen.
		return append(verbs, switcherVerb{key: 'p', word: homeItemPauseWord})
	}
	if row.kind != switcherConversation {
		return nil
	}
	verbs := switcherQuestionVerbs(row.options)
	verbs = append(verbs, switcherVerb{key: 'a', word: "put it away"})
	if strings.TrimSpace(row.session.Workspace) != "" || strings.TrimSpace(row.session.ProjectDir) != "" {
		verbs = append(verbs, switcherVerb{key: 't', word: "new chat here"}, switcherVerb{key: 'o', word: "open folder"}, switcherVerb{key: 'c', word: "copy path"})
	}
	return verbs
}

// switcherQuestionVerbs is 1b's answer-in-place: the question's OWN option
// words, on the two letters a hand already knows, carrying the option key the
// answer has to be sent under.
//
// TWO, AND NEVER THE WHOLE LIST. A strip is one row of the frame and a question
// with five options would push the list down by two more; the digits still
// answer every one of them, on the row, because they are drawn there
// (homeband_answer.go).
func switcherQuestionVerbs(options []session.AnswerOption) []switcherVerb {
	var verbs []switcherVerb
	for i, option := range options {
		if i > 1 {
			break
		}
		key := 'y'
		if i == 1 {
			key = 'n'
		}
		if word := strings.TrimSpace(option.Label); word != "" {
			verbs = append(verbs, switcherVerb{key: key, word: word, answer: option.Key})
		}
	}
	return verbs
}
