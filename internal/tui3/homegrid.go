package tui3

import (
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// ── THE GRID ────────────────────────────────────────────────────────────────
//
// HOME IS A FIXED SET OF PANELS, EACH ANSWERING ONE QUESTION A PERSON HAS WHEN
// THEY WALK UP TO A COLLEAGUE'S DESK (docs/design/home-mission-control/DESIGN.md
// §1). The chat list is one of them. This file is the arrangement: which panel
// stands in which column at which width, how a short terminal gives way, and
// what an empty panel says. What each panel HOLDS is its own file
// (homepanel_<name>.go), and every one of them is a pure reading over what home
// already cached on its three-second beat — a panel opens nothing, stats
// nothing and asks no seam, which is what keeps the place law "a place never
// reads the disk on a draw" true of a screen made of seven readings.
//
// THE LAYOUT IS DECIDED WHEN THE LINES ARE BUILT, NOT WHEN THEY ARE PAINTED. A
// squeezed panel keeps its heading and `N more · <place>`, so which rows exist
// depends on the room — and the cursor may only ever stand on a row that is on
// the screen. So the column count and the height reach [homeView.buildGrid]
// exactly the way the height already reached the switcher before it
// ([placeHome.body] rebuilds when either moves), and the paint only lays the
// columns side by side.
//
// EVERY ROW IS A LINE OF HOME'S OWN COLUMN. A conversation on any panel is a
// [homeSession] line, a watch is a [homeItem] line, a door into a place is a
// [homeLedger] line — so enter, the verbs, the digits, the takeover and the row
// identity every other part of this screen already asks of a line go on
// working unchanged. What a panel adds is [homeLine.cell]: the words the row is
// painted with.

// The width ladder (law 2): one column under a hundred and ten cells, two under
// a hundred and seventy, three past it.
const (
	homeGridTwoAt   = 110
	homeGridThreeAt = 170
	// homeGridMaxCols is the widest the ladder goes, and it sizes the pointer's
	// per-row map ([homeMark.cells]).
	homeGridMaxCols = 3
	// homeGridGutter is the air between two columns. The grid draws no rules, so
	// the columns are made of alignment and this much nothing.
	homeGridGutter = 3
	// homeGridMargin is the one cell every place's body stands in from the left
	// edge (placebodies.go's [placeTeachRows] keeps the same).
	homeGridMargin = 1
	// homeGridLead is what a row spends before its title: the one mark a row may
	// wear and the air after it. A row with no mark keeps the two cells blank,
	// so every title on a panel starts in the same column.
	homeGridLead = 2
)

// homeGridCols is the ladder itself, and the ONE place a width is compared for
// it.
func homeGridCols(width int) int {
	switch {
	case width >= homeGridThreeAt:
		return 3
	case width >= homeGridTwoAt:
		return 2
	}
	return 1
}

// homePanelID names a panel. The order of these constants is the order of the
// table below and nothing else; position on the screen is the table's to say.
type homePanelID uint8

const (
	panelNeeds homePanelID = iota
	panelRecent
	panelProjects
	panelRunning
	panelLeft
	panelSpend
	panelNext
)

// homePanel is one panel: what it is, and its rows out of the cached reading.
//
// ITS STATIC FACTS ARE THE ORDER TABLE'S. The column, the rank, the heading,
// the squeeze floor and the place it opens are one row of [homePanelOrder], and
// [homePanelBase] answers them from there — so a panel file says what the panel
// holds and nothing about where it stands.
type homePanel interface {
	id() homePanelID
	// rows is the panel's rows as lines of home's column, taken out of what the
	// beat already read. It is pure: no clock, no disk, no *app.
	rows(in *homeGridInput) homePanelRows
	// whisper is the one dim line an empty panel keeps under its heading.
	whisper() string
	// min is how many rows a squeezed panel keeps, heading and fold included.
	min() int
	// place is where the panel's fold opens, and no place for a panel whose
	// fold is only a count.
	place() page
}

// homePanelBase answers a panel's static facts from the order table. Every
// panel embeds it, which is what makes the table the one source of them.
type homePanelBase struct{ panel homePanelID }

func (b homePanelBase) id() homePanelID { return b.panel }
func (b homePanelBase) whisper() string { return homeWhisper[b.panel] }
func (b homePanelBase) min() int        { return homeSlotOf(b.panel).least }
func (b homePanelBase) place() page     { return homeSlotOf(b.panel).place }

// homePanelSlot is one row of THE ORDER TABLE.
type homePanelSlot struct {
	panel homePanel
	// word is the heading, exactly as it is drawn.
	word string
	// col2 and col3 are the column this panel stands in at two and at three
	// columns. One column is every panel in table order.
	col2, col3 int
	// keep is the squeeze priority: the LOWEST gives way first (law 5).
	keep int
	// least is how many rows the panel keeps when it is squeezed, heading and
	// fold line included.
	least int
	// rest is how many rows the panel draws before its fold at its natural
	// height, and most how many it may grow to in a tall frame
	// ([growColumn]); a panel whose most is its rest never grows.
	rest, most int
	// place is where the fold line opens; the zero page opens nothing.
	place page
	// more is what the fold says after its count when it is not a place's
	// word — the recent panel's fold is an instruction rather than a door.
	more string
}

// homePanelOrder is THE ORDER TABLE (DESIGN §1 law 2 and law 5). Its row order
// is the rank inside a column; at one column it is the reading order too, which
// is the two-column page read left column first.
//
//	two columns     needs · recent · projects  |  running · left · spend · next
//	three columns   needs · recent  |  running · left · next  |  projects · spend
//
// The keep column is the order a short frame takes rows away in and a tall one
// hands them out in, and rest and most are each panel's natural height and its
// growth budget (owner, 2026-09-10: a fifty-five-row terminal was two short
// columns over thirty rows of air). Spend's budget is its rest: it never grows.
var homePanelOrder = []homePanelSlot{
	{panel: needsPanel{homePanelBase{panelNeeds}}, word: "needs you", col2: 0, col3: 0, keep: 6, least: 4, rest: 4, most: 8, place: pageTasks},
	{panel: recentPanel{homePanelBase{panelRecent}}, word: "where you were", col2: 0, col3: 0, keep: 5, least: 4, rest: 5, most: 10, more: homeFindWord},
	{panel: projectsPanel{homePanelBase{panelProjects}}, word: "projects", col2: 0, col3: 2, keep: 4, least: 3, rest: 5, most: 8, more: homeFindWord},
	{panel: runningPanel{homePanelBase{panelRunning}}, word: "running", col2: 1, col3: 1, keep: 3, least: 4, rest: 4, most: 8, place: pageTasks},
	{panel: leftPanel{homePanelBase{panelLeft}}, word: "since you left", col2: 1, col3: 1, keep: 2, least: 3, rest: 4, most: 8, place: pageTasks},
	{panel: spendPanel{homePanelBase{panelSpend}}, word: "spend", col2: 1, col3: 2, keep: 1, least: 3, rest: 3, most: 3, place: pageSpend},
	{panel: nextPanel{homePanelBase{panelNext}}, word: "next up", col2: 1, col3: 1, keep: 0, least: 3, rest: 3, most: 5, place: pageStanding},
}

// homeFindWord is what a fold says where the rest are reached by typing rather
// than by a place: the box at the foot is a live query over every conversation
// and every project on the machine.
const homeFindWord = "type to find one"

// homeWhisper is what an empty panel says under its heading — THE COPY OF
// RECORD is DESIGN.md §4, and the manual quotes it from here.
//
// A WHISPER NAMES WHAT ARRIVES AND THE ONE THING THAT PUTS IT THERE. It never
// says the panel is empty: `nothing is running` is the emptiness law inverted
// into words, and stays banned (docs/DESIGN-LANGUAGE.md, "presence over
// labels"). Projects has no whisper because it is never empty — the launch
// folder is always a row.
//
// NO WHISPER CARRIES AN ELLIPSIS, not even a quoted one. A whisper wraps rather
// than being cut ([homeWhisperLines]), so a `…` on one of these lines could only
// be read as the screen having run out of room.
var homeWhisper = map[homePanelID]string{
	panelNeeds:   "questions from any chat or task land here · a digit answers them",
	panelRunning: "work you send off with /task runs here on its own",
	panelLeft:    "what watches and tasks did while the terminal was shut",
	panelRecent:  "your conversations · what you type below starts one",
	panelSpend:   "every chat and task is priced here",
	panelNext:    `reminders and routines · "remind me at 6" or "every morning at 9"`,
}

// homePanelCut is a panel's rows cut at its growth budget, with the count of
// what the fold stands for past it. Which of the kept rows are drawn is the
// layout's to say ([homeGridLayout]); a panel only ever hands it at most this
// many, so a machine with four hundred conversations builds ten lines and not
// four hundred.
func homePanelCut(id homePanelID, lines []homeLine) homePanelRows {
	most := homeSlotOf(id).most
	if len(lines) <= most {
		return homePanelRows{lines: lines}
	}
	return homePanelRows{lines: lines[:most], more: len(lines) - most}
}

// homeSlotOf is one panel's row of the table.
func homeSlotOf(id homePanelID) homePanelSlot {
	for _, slot := range homePanelOrder {
		if slot.panel.id() == id {
			return slot
		}
	}
	return homePanelSlot{}
}

// column is which column a panel stands in at this many columns.
func (s homePanelSlot) column(cols int) int {
	switch cols {
	case 3:
		return s.col3
	case 2:
		return s.col2
	}
	return 0
}

// foldWord is what the fold says after `N more · `, and "" for a bare count.
func (s homePanelSlot) foldWord() string {
	if s.more != "" {
		return s.more
	}
	return s.place.word()
}

// ── the reading every panel is taken from ──────────────────────────────────

// homeGridInput is everything a panel may read, gathered from home's own caches
// and never from a seam. It is built by [homeView.gridInput] on the beat and on
// every rebuild, and it is a plain value so a test can hand one to a panel.
type homeGridInput struct {
	// rows is every row the switcher ranks — conversations and the standing
	// things that need somebody or are firing — in its own order: what needs
	// you (oldest first), then what is moving, then the rest by recency.
	rows []switcherRow
	// ledger is the `since you left` lines the switcher reads.
	ledger []switcherRow
	// world is every project, the ones home knows only through a watch
	// included ([homeView.everyProject]).
	world session.World
	// items is each project's standing band, by bucket directory.
	items map[string][]StandingItemView
	// errands are the `ask here` exchanges this window is holding, already as
	// lines of the column (homeexchange.go).
	errands []homeLine
	// bucket is this window's own project directory, launch the folder it is
	// working in, and tilde what `~` abbreviates in a path.
	bucket, launch, tilde string
	// last is the tail of each conversation's journal the beat has read
	// (homecardread.go), by transcript.
	last map[string]session.Summary
	// repos is each workspace's last `git status` reading (homeband_repo.go).
	repos map[string]homeRepoReading
	// spend is the day's figure and the fortnight behind it (homepanel_spend.go).
	spend homeSpendReading
	seen  time.Time
	now   time.Time
}

// gridInput gathers it. EVERY FIELD IS A CACHE HOME ALREADY HOLDS, and the one
// computation is the switcher's pure reading of the world it was handed.
func (h *homeView) gridInput() homeGridInput {
	world := h.world
	world.Projects = h.everyProject()
	here := switcherHere{session: h.here, project: h.bucket, coming: h.claim, hosted: h.far}
	reading := readSwitcher(world, h.items, h.fired, here, h.gone, h.seen, h.world.Read,
		switcherView{all: true}, h.ledger)
	in := homeGridInput{world: world, items: h.items, errands: h.switchExchanges(),
		bucket: h.bucket, launch: h.launch, tilde: h.tilde, last: h.last, repos: h.repos, spend: h.spend,
		seen: h.seen, now: h.world.Read}
	for _, line := range reading.lines {
		if line.row == nil || line.row.fold {
			continue
		}
		if line.row.kind == switcherLedger {
			in.ledger = append(in.ledger, *line.row)
			continue
		}
		in.rows = append(in.rows, *line.row)
	}
	return in
}

// ── what a panel hands back ────────────────────────────────────────────────

// homePanelRows is one panel's reading.
type homePanelRows struct {
	// lines are the rows, in order, each carrying the cell it is painted from.
	lines []homeLine
	// more is how many rows the panel is holding past these, which the grid
	// says on the fold.
	more int
	// said is what the heading carries after its word — a count, an age — and
	// "" for the word alone.
	said string
	// right is a clause the heading carries at its right margin, and money the
	// figure inside it drawn in the money ink — the day's spend on `spend`.
	right, money string
}

// homeCellKind is which shape one line of a panel is drawn in.
type homeCellKind uint8

const (
	// cellRow is a row: a mark or its blank lead, a title, facts at the right,
	// and — when it has one — a line under it.
	cellRow homeCellKind = iota
	cellHead
	cellWhisper
	cellFold
	// cellBar, cellSpark and cellFacts are the spend panel's three lines: the
	// day against its allowance, the fortnight, and who it went to and what for
	// (homepanel_spend.go).
	cellBar
	cellSpark
	cellFacts
)

// homeCellMark is the one mark a row may wear. There are two (law 8): the
// question mark on a row waiting for a person, and the spinner cell on the
// first running row. Nothing else on home wears a glyph.
type homeCellMark uint8

const (
	cellMarkNone homeCellMark = iota
	cellMarkNeeds
	cellMarkSpin
)

// homeCell is the words one line of a panel is painted from, taken when the
// line is built. It is a reading and holds no state: the cursor, the pointer
// and the spinner's frame are the paint's.
type homeCell struct {
	kind  homeCellKind
	panel homePanelID
	mark  homeCellMark
	title string
	// pad is the cells the title is padded to before the note, so a panel's
	// notes stand in one column (the projects panel's counts).
	pad int
	// note is a dim clause after the title; tag and right are the dim facts
	// at the right margin, the tag giving way first.
	note, tag, right string
	// bold is this window's own conversation.
	bold bool
	// path says the title is a folder's path, which is cut FROM THE LEFT —
	// `…/code/aforge-v2` — so the folder's own name and the facts beside it
	// stay on the row ([homeCellPathTitle]).
	path bool
	// hold says the right-hand word is a fact about the DOOR — `folder gone`,
	// `another window`, `coming here`, `here` — and is cut around rather than
	// dropped, because it is what enter will do; door is the longer sentence a
	// held row grows into under the cursor ([app.homeCellDoor]).
	hold bool
	door string
	// sub is the line under the row, and subRight what that line carries at
	// its right — the answers a digit sends.
	sub, subRight string
	// share is how full the spend bar is, and spark the fortnight's days.
	share float64
	spark []float64
	// money is the figure inside a heading's right-hand clause that is drawn
	// in the money ink rather than the dim.
	money string
	// row is the switcher's own row behind a conversation or a watch, which is
	// what its verbs are read from (place_home.go's [app.homeRowVerbs]).
	row *switcherRow
	// key tells apart two rows of one panel that stand for the same
	// conversation — two of its tasks running — so the cursor that was on the
	// second is put back on the second after a rebuild ([homeLine.sameRow]).
	// It is "" on every row that is the only one of its conversation.
	key string
}

// height is how many screen rows one line takes.
func (l homeLine) height() int {
	if l.cell != nil && l.cell.sub != "" {
		return 2
	}
	return 1
}

// ── the layout ─────────────────────────────────────────────────────────────

// homeGrid is the shape the last build settled: how many columns, and which
// column each line of [homeView.lines] stands in.
type homeGrid struct {
	cols int
	col  []int
}

// homeGridPanel is one panel as the layout holds it while it squeezes.
type homeGridPanel struct {
	slot homePanelSlot
	read homePanelRows
	// whisper is the panel's whisper as the lines it takes at its column's
	// width ([homeWhisperLines]), and nothing for a panel with rows.
	whisper []string
	// shown is how many of the panel's rows it keeps, and dropped is the panel
	// gone from the page.
	shown   int
	dropped bool
}

// natural is how many rows the panel shows at its natural height: its rest,
// or all it holds when that is fewer.
func (p homeGridPanel) natural() int { return min(p.slot.rest, len(p.read.lines)) }

// empty reports a panel with no rows at all, which draws its whisper instead.
func (p homeGridPanel) empty() bool { return len(p.read.lines) == 0 && p.read.more == 0 }

// hidden is how many rows the fold stands for.
func (p homeGridPanel) hidden() int { return len(p.read.lines) - p.shown + p.read.more }

// height is how many screen rows the panel draws: its heading, its rows or its
// whisper, and its fold.
func (p homeGridPanel) height() int {
	if p.dropped {
		return 0
	}
	if p.empty() {
		// A HEADING NEVER DRAWS OVER NOTHING. A panel with no rows and no
		// whisper is not on the page at all.
		if len(p.whisper) == 0 {
			return 0
		}
		return 1 + len(p.whisper)
	}
	n := 1
	for _, line := range p.read.lines[:p.shown] {
		n += line.height()
	}
	if p.hidden() > 0 {
		n++
	}
	return n
}

// homeColumnHeight is a column's height with one blank row between panels.
func homeColumnHeight(column []*homeGridPanel) int {
	n, drawn := 0, 0
	for _, p := range column {
		if h := p.height(); h > 0 {
			n += h
			drawn++
		}
	}
	if drawn > 1 {
		n += drawn - 1
	}
	return n
}

// fitColumn fits one column into room: a column taller than its room is
// squeezed, and one with room to spare grows. ZERO ROOM IS NO ANSWER — a build
// before the first frame has no height to fit, and every panel keeps its
// natural height.
func fitColumn(column []*homeGridPanel, room int) {
	switch {
	case room <= 0:
	case homeColumnHeight(column) > room:
		squeezeColumn(column, room)
	default:
		growColumn(column, room)
	}
}

// byKeep is a column's panels in the order a squeeze takes from them: the
// lowest keep first.
func byKeep(column []*homeGridPanel) []*homeGridPanel {
	order := append([]*homeGridPanel(nil), column...)
	sort.SliceStable(order, func(i, j int) bool { return order[i].slot.keep < order[j].slot.keep })
	return order
}

// squeezeColumn fits one column into room (law 5): the lowest-priority panel is
// shrunk to its floor first, then the next, and only when every panel is at its
// floor are panels dropped, lowest first.
func squeezeColumn(column []*homeGridPanel, room int) {
	if room <= 0 || homeColumnHeight(column) <= room {
		return
	}
	order := byKeep(column)
	for _, p := range order {
		if homeColumnHeight(column) <= room {
			break
		}
		p.shrink()
	}
	for _, p := range order {
		if homeColumnHeight(column) <= room {
			break
		}
		p.dropped = true
	}
	regrowColumn(order, column, room)
}

// regrowColumn hands back what the squeeze did not need, a row at a time, to
// the panels it cut, the most important first. A floor is a whole step, and a
// drop frees a whole panel, so the squeeze can overshoot — and air under a
// column while `where you were` is folded is the squeeze spending the wrong
// panel's rows.
//
// IT GIVES BACK ONLY UP TO A PANEL'S NATURAL HEIGHT. A squeezed column is a
// short frame, and growth past the resting fold is for a column with room once
// every panel has its natural height ([growColumn]).
func regrowColumn(order, column []*homeGridPanel, room int) {
	for i := len(order) - 1; i >= 0; i-- {
		p := order[i]
		for !p.dropped && p.shown < p.natural() {
			p.shown++
			if homeColumnHeight(column) > room {
				p.shown--
				break
			}
		}
	}
}

// growColumn hands a tall column's spare rows to its panels, IN THE SQUEEZE'S
// ORDER REVERSED — what a person came for first — one row each, round and
// round, until no panel wants another or the next would not fit. A panel wants
// one while it holds rows past its fold, up to its budget ([homePanelCut]); the
// fold line goes when the last of them is shown. What room is left stays as air
// under the column, never between its panels.
func growColumn(column []*homeGridPanel, room int) {
	order := byKeep(column)
	for grew := true; grew; {
		grew = false
		for i := len(order) - 1; i >= 0; i-- {
			p := order[i]
			if p.dropped || p.shown >= len(p.read.lines) {
				continue
			}
			p.shown++
			if homeColumnHeight(column) > room {
				p.shown--
				continue
			}
			grew = true
		}
	}
}

// shrink cuts a panel to its floor: the heading, as many whole rows as fit in
// what the floor leaves after the fold line, and the fold. A panel already
// inside its floor is left alone.
func (p *homeGridPanel) shrink() {
	if p.height() <= p.slot.least {
		return
	}
	budget := p.slot.least - 2
	kept, used := 0, 0
	for _, line := range p.read.lines {
		if used+line.height() > budget {
			break
		}
		used += line.height()
		kept++
	}
	p.shown = kept
}

// homeGridLayout reads every panel and fits each column: the panels in table
// order, each in the column the ladder puts it in, at that column's width.
func homeGridLayout(in *homeGridInput, cols, width, room int) [][]*homeGridPanel {
	columns := make([][]*homeGridPanel, cols)
	_, widths := homeGridGeometry(width, cols)
	for _, slot := range homePanelOrder {
		read := slot.panel.rows(in)
		at := slot.column(cols)
		p := &homeGridPanel{slot: slot, read: read}
		p.shown = p.natural()
		if p.empty() {
			p.whisper = homeWhisperLines(slot.panel.whisper(), widths[at])
		}
		columns[at] = append(columns[at], p)
	}
	for _, column := range columns {
		fitColumn(column, room)
	}
	return columns
}

// homeWhisperLines is a whisper at one column's width, standing in a row's lead.
//
// A WHISPER WRAPS; IT IS NEVER CUT. It is the one sentence an empty panel has,
// and the half after an ellipsis — `a digit answers them`, `"every morning, …"`
// — is the half that says what to do. So it takes the dim lines it needs, and
// the panel's height is its heading and all of them. A build before the first
// frame knows no width, and keeps the sentence on one line until one arrives.
func homeWhisperLines(text string, width int) []string {
	if text == "" {
		return nil
	}
	if width <= homeGridLead {
		return []string{text}
	}
	return wrap(text, width-homeGridLead)
}

// ── the build ──────────────────────────────────────────────────────────────

// buildGrid is the resting column: every panel, laid out for this frame's
// columns and room, as lines of home's own list — column by column, top to
// bottom, so a line's index still means what [homeView.cursor] has always
// meant.
func (h *homeView) buildGrid() {
	cols := max(1, h.cols)
	h.grid = homeGrid{cols: cols}
	// A WORLD THAT IS NOT AN ANSWER YET DRAWS NOTHING. Over --host the first
	// frames come before the far machine has replied, and a panel whispering
	// what arrives there over a machine full of work would be a sentence about
	// somebody else's disk that is not true ([homeView.known]).
	if !h.known {
		return
	}
	in := h.gridInput()
	for at, column := range homeGridLayout(&in, cols, h.gridWidth, h.room) {
		first := true
		for _, p := range column {
			lines := p.lines()
			if len(lines) == 0 {
				continue
			}
			if !first {
				h.addGridLine(homeLine{kind: homeBlank}, at)
			}
			first = false
			for _, line := range lines {
				h.addGridLine(line, at)
			}
		}
	}
}

// addGridLine puts one line in the list and records its column.
func (h *homeView) addGridLine(line homeLine, col int) {
	h.lines = append(h.lines, line)
	h.grid.col = append(h.grid.col, col)
}

// lines is one laid-out panel as lines of the column: its heading, its rows or
// its whisper, and its fold.
func (p homeGridPanel) lines() []homeLine {
	if p.height() == 0 {
		return nil
	}
	id := p.slot.panel.id()
	head := p.slot.word
	if p.read.said != "" {
		head += rowSep + p.read.said
	}
	out := []homeLine{{kind: homeSwitchHead, cell: &homeCell{kind: cellHead, panel: id, title: head, right: p.read.right, money: p.read.money}}}
	if p.empty() {
		for _, words := range p.whisper {
			out = append(out, homeLine{kind: homeSwitchHead, cell: &homeCell{kind: cellWhisper, panel: id, title: words}})
		}
		return out
	}
	out = append(out, p.read.lines[:p.shown]...)
	if n := p.hidden(); n > 0 {
		out = append(out, p.fold(n))
	}
	return out
}

// fold is `N more · <place>` (law 9: nothing grows, and the fold is the door).
// A fold that names a place is a door into it, the same [homeLedger] line a
// `since you left` row is; one that names only a count or an instruction is not
// a stop.
func (p homeGridPanel) fold(n int) homeLine {
	words := groupedInt(n) + " more"
	word := p.slot.foldWord()
	if word != "" {
		words += rowSep + word
	}
	cell := &homeCell{kind: cellFold, panel: p.slot.panel.id(), title: words}
	if p.slot.more == "" && word != "" {
		return homeLine{kind: homeLedger, project: word, dir: homeFoldKey, cell: cell}
	}
	return homeLine{kind: homeSwitchHead, cell: cell}
}

// homeFoldKey is the identity a fold door carries beside its place word, so the
// cursor on `3 more · tasks` is told apart from a `since you left` line that
// opens the same place ([homeLine.sameRow]). It starts with a NUL, which no
// directory path contains.
const homeFoldKey = "\x00fold"

// gridOn reports that the resting column is the grid: not the phone's inbox,
// not the typed drop-up, and built at least once.
func (h *homeView) gridOn() bool {
	return !h.phone && !h.searching() && h.grid.cols > 0
}

// panelOf is which panel a line belongs to, and false for the blank between
// two panels.
func (l homeLine) panelOf() (homePanelID, bool) {
	if l.cell == nil {
		return 0, false
	}
	return l.cell.panel, true
}

// cursorPanel is the panel the cursor is standing in.
func (h *homeView) cursorPanel() (homePanelID, bool) {
	if h.cursor < 0 || h.cursor >= len(h.lines) {
		return 0, false
	}
	return h.lines[h.cursor].panelOf()
}

// countWord is a count on a heading, and nothing for none.
func countWord(n int) string {
	if n <= 0 {
		return ""
	}
	return groupedInt(n)
}

// ── the cursor ─────────────────────────────────────────────────────────────

// columnOf is which column a line stands in, and -1 off the grid.
func (h *homeView) columnOf(at int) int {
	if at < 0 || at >= len(h.grid.col) {
		return -1
	}
	return h.grid.col[at]
}

// columnStops is every line of one column the cursor may rest on, top to
// bottom.
func (h *homeView) columnStops(col int) []int {
	var out []int
	for at, line := range h.lines {
		if h.columnOf(at) == col && line.stop() {
			out = append(out, at)
		}
	}
	return out
}

// gridMove walks the cursor up or down ITS OWN COLUMN, crossing from one panel
// into the next at a panel's ends and clamping at the column's (`↑` off the top
// is the router's, and reaches the tab bar — pages.go's [app.barReach] asks
// [placeHome.stops], which answers this column).
func (h *homeView) gridMove(delta int) {
	stops := h.columnStops(h.columnOf(h.cursor))
	if len(stops) == 0 {
		return
	}
	at := 0
	for i, line := range stops {
		if line == h.cursor {
			at = i
		}
	}
	at = max(0, min(len(stops)-1, at+delta))
	h.cursor, h.picked = stops[at], true
}

// rowOf is the screen row a line starts on inside its own column.
func (h *homeView) rowOf(at int) int {
	col, row := h.columnOf(at), 0
	for i := 0; i < at && i < len(h.lines); i++ {
		if h.columnOf(i) == col {
			row += h.lines[i].height()
		}
	}
	return row
}

// gridCross moves the cursor into the neighbouring column, onto the stop whose
// row is nearest the one it left — the same rank a person's eye was at. It
// reports false when there is no column that way, or none with a row in it, so
// the arrow keeps whatever else it means at the edge (the verb strip).
func (h *homeView) gridCross(dir int) bool {
	next := h.columnOf(h.cursor) + dir
	if next < 0 || next >= h.grid.cols {
		return false
	}
	y := h.rowOf(h.cursor)
	best, gap := -1, 0
	for _, at := range h.columnStops(next) {
		d := h.rowOf(at) - y
		if d < 0 {
			d = -d
		}
		if best < 0 || d < gap {
			best, gap = at, d
		}
	}
	// A COLUMN WITH NOTHING TO STAND ON IS NO COLUMN THAT WAY: every panel in
	// it is whispering, and the arrow keeps its other meaning.
	if best < 0 {
		return false
	}
	h.cursor, h.picked = best, true
	return true
}

// homeGridCross is `←` and `→` on the resting grid: the neighbouring column.
//
// IT IS READ BEFORE THE ROUTER'S ARROWS (place_home.go's [placeHome.owns]),
// because the router's `→` opens a row's verbs and nearly every row on home has
// some — so the geography would lose to the strip on every row a person could
// stand on. The strip is still one arrow away where no column lies to the right.
// Nothing else that holds the arrows is overruled: the tab bar, an open strip, a
// question this window raised about a row.
func (a *app) homeGridCross(msg tea.KeyPressMsg) bool {
	// AN ERRAND'S ROW KEEPS ITS `→`, which takes the keyboard into the errand
	// (home.go's [app.homeKey]) — the one row on home whose arrow already meant
	// "into what is beside me".
	if a.bar.on || a.strip.open || a.home.ask != nil || !a.home.gridOn() || a.paneExchange() != nil {
		return false
	}
	dir := 0
	switch msg.String() {
	case "left":
		dir = -1
	case "right":
		dir = 1
	default:
		return false
	}
	if !a.home.gridCross(dir) {
		return false
	}
	// A KEY IS THE PERSON TAKING THE CURSOR BACK, and it clears what every other
	// key on home clears ([app.homeKey]).
	a.movedFrom = ""
	a.home.say("", "")
	a.sweepExchanges()
	a.touch()
	return true
}

// pointGrid puts the cursor back on the row a person had chosen: the same thing
// in the same panel first, and the same thing anywhere second — a conversation
// can stand on two panels (this window's own, waiting on a question).
func (h *homeView) pointGrid(want homeLine) bool {
	panel, hasPanel := want.panelOf()
	for at, line := range h.lines {
		if got, ok := line.panelOf(); ok && hasPanel && got == panel && line.sameRow(want) {
			h.cursor = at
			return true
		}
	}
	return h.pointSame(want)
}

// homeColsNow is the ladder's answer for the frame this app draws into, asked
// when home opens for [homeTierNow]'s reason: the shape is known before the
// first frame.
func (a *app) homeColsNow() int {
	width, _ := a.size()
	return homeGridCols(width)
}

// homeGridWidthNow is the frame width the grid lays out for, asked when home
// opens for the same reason: a whisper wraps at its column's width, and a
// build before the first frame that wrapped it at no width would move every
// line under it the moment the frame arrived.
func (a *app) homeGridWidthNow() int {
	width, _ := a.size()
	return width
}

// ── the pointer ────────────────────────────────────────────────────────────

// homeHitAt is which line of the list a pointer at (x, y) is on. On the grid a
// screen row holds a line of every column and the x decides which; everywhere
// else the row's one line is the answer.
func (a *app) homeHitAt(x, y int, hits []int) int {
	if y < 0 || y >= len(hits) {
		return -1
	}
	if marks := a.home.gridMarks; y < len(marks) && marks[y].grid {
		return marks[y].cells[a.home.gridColumnAt(x)]
	}
	return hits[y]
}

// gridColumnAt is the column an x falls in: the last one starting at or before
// it, so a gutter belongs to the column on its left.
func (h *homeView) gridColumnAt(x int) int {
	col := 0
	for c, start := range h.gridX {
		if x >= start && c < homeGridMaxCols {
			col = c
		}
	}
	return col
}

// switcherRowLine is one of the switcher's rows as a line of home's column,
// wearing a panel's cell: a conversation is a [homeSession] line and a watch a
// [homeItem] one, which is what keeps every door on them the door it was.
func switcherRowLine(row switcherRow, cell *homeCell) homeLine {
	cell.row = &row
	if row.kind == switcherStanding {
		return homeLine{kind: homeItem, view: row.item, item: row.item.Item, project: row.project, cell: cell}
	}
	return homeLine{kind: homeSession, row: row.session, project: row.project,
		dir: homeBucketOf(row.session.Transcript), cell: cell}
}

// homeGridAnswer is a digit on the resting grid: THE TOP QUESTION, FROM
// ANYWHERE ON HOME, WITH NO CURSOR MOVE (law 7). The top question is the first
// row of `needs you` that draws its answers, so the key a person presses is one
// they can see on the screen; a digit with no such row falls through to the
// row under the cursor and then to the box, as it always did.
func (a *app) homeGridAnswer(key string) (tea.Cmd, bool) {
	if !a.home.gridOn() {
		return nil, false
	}
	for _, line := range a.home.lines {
		if line.cell == nil || line.cell.panel != panelNeeds {
			continue
		}
		if words := a.homeRowAnswers(line); words != "" && words != answerWaitingWord && words != needsOpenWord {
			return a.answerRowKey(a.homeTrue(line.row), key)
		}
	}
	return nil, false
}

// homeRowAnswers is what a `needs you` row draws at the right of its question:
// the answers, under the answer band's own four rules ([drawAnswerBand]) — the
// question is fresh and offered answers, this window has somewhere to leave
// one, it has not already sent one (the waiting word stands in for a moment
// after it has), and no question this window raised about the row is standing
// over it ([app.answersStepAside]). Every fact is in memory; nothing is read.
//
// A ROW THAT SAYS `enter` KEEPS SAYING IT. It is an instruction rather than an
// answer — the row is not the top question, or its question has a paragraph —
// and the gate is about answers (homepanel_needs.go).
func (a *app) homeRowAnswers(line homeLine) string {
	if line.cell == nil || line.cell.subRight == "" {
		return ""
	}
	if line.cell.subRight == needsOpenWord {
		return needsOpenWord
	}
	if line.kind != homeSession {
		return ""
	}
	row, now := a.homeTrue(line.row), a.home.world.Read
	question, ok := answerable(row, now)
	if !ok {
		return ""
	}
	if sent, ok := a.answerSent(row, question); ok {
		if now.Sub(sent.at) < answerHoldFor {
			return answerWaitingWord
		}
		return ""
	}
	if (a.leaveAnswer == nil && !a.answeringHere(row)) || a.answersStepAside(row) {
		return ""
	}
	return line.cell.subRight
}

// ── the readings the grid asks for ─────────────────────────────────────────

// refreshGridReadings asks for the two readings the resting grid draws that are
// about one row rather than about the machine: the tail of this window's own
// journal, for the line under its row, and each project's `git status`, for its
// repository clause. Both come back as messages and rebuild the grid when they
// land ([app.tookHomeLeftOff], [app.tookHomeRepo]); both are behind the caches
// that keep a second ask from costing anything (homecardread.go,
// homeband_repo.go).
func (a *app) refreshGridReadings(now time.Time) tea.Cmd {
	var asked []tea.Cmd
	for _, line := range a.home.lines {
		switch {
		case line.kind == homeProjectRow:
			asked = append(asked, a.refreshRepoOf(strings.TrimSpace(line.proj.Path), now))
		case line.cell != nil && line.cell.bold:
			asked = append(asked, a.askHomeLeftOff(line.row.Transcript))
		}
	}
	return tea.Batch(asked...)
}

// homePreselect puts the cursor on THE CONVERSATION THIS WINDOW WAS IN BEFORE
// THIS ONE (law 6): the most recent key on this window's own stack that is not
// the one in front and is on the grid. Enter is then a switch in two keys, and
// esc still goes back to the conversation behind home.
func (a *app) homePreselect() {
	if !a.home.gridOn() {
		return
	}
	front := a.frontTabKey()
	for at := len(a.prev) - 1; at >= 0; at-- {
		if key := a.prev[at]; key != "" && key != front {
			a.home.point(key)
			return
		}
	}
}
