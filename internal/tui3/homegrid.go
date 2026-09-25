package tui3

import (
	"math"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/session"
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
	panelSessions
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
	// explainer is the dim clause the heading carries after its word — what the
	// panel is for, in the fewest words that answer it. It is drawn in the note
	// ink beside the heading and GIVES WAY WHOLE: a column too narrow to hold it
	// beside the heading drops it rather than cutting the heading for it
	// ([homeCellHead]). A panel has none where the heading already says what it
	// is: spend carries its right-hand money clause, and needs you carries its
	// own group line (`unread`) — so a gloss there would repeat the panel.
	explainer string
	// pinned is a panel that stands in the rail whatever it holds. It is for a
	// panel whose height is the same on every machine on every day — spend is
	// three rows forever, and projects is the folders you have opened — so
	// moving it into the field would teach a person a position that then never
	// changes back. Every other panel's column is its content's to say
	// ([homeColumnOf]).
	pinned bool
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
	// head is where a press on the heading goes: THE PLACE THE HEADING NAMES
	// (law 10, home is the summary of the tabs). It is the ONLY door to the
	// rows a panel is not drawing, because the fold under them is a toggle and
	// names no place ([homeGridPanel.fold]). The zero page is a heading that
	// names only its panel.
	head page
}

// homePanelOrder is THE ORDER TABLE (DESIGN §1 law 2 and law 5). Its row order
// is the rank a panel takes wherever it stands, and at one column it is the
// reading order too.
//
// IT NO LONGER SAYS WHICH COLUMN A PANEL IS IN, because the column is a reading
// of what the panel holds rather than a fact about the panel (law 2, ruled
// 2026-09-15). What the table still fixes is the rank inside whichever column
// the panel lands in, so two panels that both fill never swap places:
//
//	field (panels with rows)   recent · needs · running · left · next
//	rail (pinned, then quiet)  projects · spend  ·  the quiet ones in the same rank
//
// The keep column is the order a short frame takes rows away in and a tall one
// hands them out in, and rest and most are each panel's natural height and its
// growth budget (owner, 2026-09-10: a fifty-five-row terminal was two short
// columns over thirty rows of air). Spend's budget is its rest: it never grows.
// The head column is where a press on a heading goes. The conversation list
// has no heading; the projects heading opens nothing. The explainer is the dim clause a
// heading may carry after its word — see [homePanelSlot.explainer].
var homePanelOrder = []homePanelSlot{
	{panel: sessionsPanel{homePanelBase{panelSessions}}, word: sessionsWord, keep: 7, least: 4, rest: homeSessionsLimit, most: homeSessionsLimit, place: pageTasks, head: pageTasks},
	{panel: recentPanel{homePanelBase{panelRecent}}, keep: 5, least: 3, rest: tabsCap + homeClosedLimit, most: tabsCap + homeClosedLimit},
	{panel: needsPanel{homePanelBase{panelNeeds}}, keep: 6, least: 4, rest: 4, most: 8, place: pageTasks, head: pageTasks},
	{panel: projectsPanel{homePanelBase{panelProjects}}, word: "projects", pinned: true, keep: 4, least: 3, rest: 5, most: 8},
	{panel: leftPanel{homePanelBase{panelLeft}}, word: "since you left", keep: 2, least: 3, rest: 4, most: 8, place: pageTasks, head: pageMemory},
	{panel: spendPanel{homePanelBase{panelSpend}}, word: "spend", pinned: true, keep: 1, least: 3, rest: 3, most: 3, place: pageSpend, head: pageSpend},
	{panel: nextPanel{homePanelBase{panelNext}}, word: (placeStanding{}).word(), keep: 0, least: 3, rest: 3, most: 5, place: pageStanding, head: pageStanding},
}

// homeNeedsTaskFresh is how long a task's call stays a row of `needs you` after
// it landed. Past it the call is counted in the panel's fold, `N more`, and
// comes back when the panel is opened ([needsFresh]).
const homeNeedsTaskFresh = 48 * time.Hour

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
	panelSessions: "your recent conversations appear here",
	panelLeft:     "what watches and tasks did while the terminal was shut",
	panelSpend:    "every chat and task is priced here",
	panelNext:     `reminders, routines, watches and rules · "remind me at 6" or "every morning at 9"`,
}

// homePanelCut is a panel's rows cut at its cap, with the count of what the
// fold stands for past it. Which of the kept rows are drawn is the layout's to
// say ([homeGridLayout]); a panel only ever hands it at most this many, so a
// panel with four hundred records builds its budget rather than four hundred
// — unless it is THE panel somebody opened, whose cap is lifted
// ([homeGridInput.cap]).
func homePanelCut(in *homeGridInput, id homePanelID, lines []homeLine) homePanelRows {
	most := in.cap(id)
	if len(lines) <= most {
		return homePanelRows{lines: lines}
	}
	return homePanelRows{lines: lines[:most], more: len(lines) - most}
}

// cap is how many rows a panel hands the layout: its growth budget, the order
// table's `most` — or every row it has, for the one panel whose fold somebody
// opened ([homeView.opened]). The layout still fits that panel to the column,
// so opening a panel with four hundred rows on a forty-row frame shows the
// forty and folds the rest; what the lifted cap buys is that the rows are
// THERE to be shown when the column has room.
func (in *homeGridInput) cap(id homePanelID) int {
	if in.openedOn && in.opened == id {
		return math.MaxInt
	}
	return homeSlotOf(id).most
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

// railCol is the column the rail is: the LAST one the ladder drew, so the rail
// is flush with the right edge at every width it exists at. One column has no
// rail — everything is read top to bottom in table order.
func homeRailCol(cols int) int { return max(0, cols-1) }

// homeColumnOf is which column a panel stands in, and it is THE LAW OF THE
// GRID (DESIGN §1 law 2, ruled 2026-09-15): the field is what has something in
// it, the rail is everything else.
//
// A panel with rows goes in the FIELD — the columns left of the rail — because
// the thing with content is the thing being read, and reading wants the left
// edge and the width. A panel with nothing in it goes in the RAIL, where its
// heading and its whisper stand as a list of what this machine could be doing
// and is not. A pinned panel is in the rail whatever it holds.
//
// THE COST OF THIS LAW IS THAT HOME'S GEOGRAPHY MOVES, and it is paid on
// purpose. The old law kept every panel in one column forever so a person could
// learn where to look; this one spends that to keep the eye on the one part of
// the screen where anything is happening, which on a quiet machine is a very
// small part. The rank inside each column never moves ([homePanelOrder]), so
// what changes is which side a panel is on and never the order it is found in.
func homeColumnOf(slot homePanelSlot, cols int, empty bool) int {
	if cols <= 1 {
		return 0
	}
	if slot.pinned || empty {
		return homeRailCol(cols)
	}
	return 0
}

// ── the reading every panel is taken from ──────────────────────────────────

// homeGridInput is everything a panel may read, gathered from home's own caches
// and never from a seam. It is built by [homeView.gridInput] on the beat and on
// every rebuild, and it is a plain value so a test can hand one to a panel.
type homeGridInput struct {
	openChats, closedChats []switcherRow

	// rows is every row the switcher ranks — conversations and the standing
	// things that need somebody or are firing — in its own order: what needs
	// you (oldest first), then what is moving, then the rest by recency.
	rows []switcherRow
	// ledger is the `since you left` lines the switcher reads.
	ledger []switcherRow
	// calls is every landing whose check is the person's, as the `unread`
	// group draws them, and older how many aged out of it. IT IS READ ONCE,
	// HERE, because three readers need the same answer: the group's rows, the
	// `since you left` line that steps aside for a landing already on the
	// screen, and the pulse's count (homepanel_needs.go's [needsCallOf]).
	calls      []needsItem
	callsOlder int
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
	// opened is the one panel whose fold somebody opened, and openedOn that
	// there is one: its cap is lifted ([homeGridInput.cap]) and, for `needs
	// you`, its aged landings come back into the group ([homeView.gridInput]).
	opened   homePanelID
	openedOn bool
	// desc is that this frame HAS a description column ([homeDescOn]). A row
	// whose sentence only exists to be drawn there does not carry one on a frame
	// with nowhere to draw it — a sub reserves a line in its panel's height
	// ([homeGridPanel.growsARow]), and paying that on a narrow terminal for a
	// column that is not on the screen costs a row a person could have read.
	desc bool
}

// gridInput gathers it. EVERY FIELD IS A CACHE HOME ALREADY HOLDS, and the one
// computation is the switcher's pure reading of the world it was handed.
func (h *homeView) gridInput() homeGridInput {
	world := h.world
	world.Projects = h.everyProject()
	here := switcherHere{session: h.here, coming: h.claim, hosted: h.far}
	reading := readSwitcher(world, h.items, h.fired, here, h.gone, h.seen, h.world.Read, h.ledger)
	calls, older := needsFresh(needsCalls(world, h.world.Read), h.world.Read)
	// AN OPENED `needs you` SHOWS ITS AGED LANDINGS TOO. They aged out of the
	// group as history ([needsFresh]) and the shut fold counts them with the rest
	// it hides; opening the fold is asking for all of it, and a `▸ 3 more` that
	// opened to show one row and kept two behind another word would be a fold
	// that lied about its own count.
	if h.openedOn && h.opened == panelNeeds {
		calls, older = needsCalls(world, h.world.Read), 0
	}
	open, closed := h.conversationRows()
	return homeGridInput{openChats: open, closedChats: closed, rows: reading.rows, ledger: reading.ledger, calls: calls, callsOlder: older,
		opened: h.opened, openedOn: h.openedOn,
		desc: homeDescOn(h.cols), world: world, items: h.items,
		errands: h.switchExchanges(), bucket: h.bucket, launch: h.launch, tilde: h.tilde, last: h.last,
		repos: h.repos, spend: h.spend, seen: h.seen, now: h.world.Read}
}

// ── what a panel hands back ────────────────────────────────────────────────

// homePanelRows is one panel's reading.
type homePanelRows struct {
	// lines are the rows, in order, each carrying the cell it is painted from.
	lines []homeLine
	// more is how many rows the panel is holding past these, which the grid
	// says on the fold.
	more int
	// older is how many rows the panel aged out rather than folded — counted on
	// the same fold line, separately, because they are history and not more of
	// the same (homepanel_needs.go's [needsFresh]).
	older int
	// said is what the heading carries after its word — a count, an age — and
	// "" for the word alone.
	said string
	// group is the run of rows at the FOOT of lines that stands under a dim line
	// of its own and folds before any row above it, and nil for a panel whose
	// rows are one list ([homePanelGroup]).
	group *homePanelGroup
	// right is a clause the heading carries at its right margin, and money the
	// figure inside it drawn in the money ink — the day's spend on `spend`.
	right, money string
}

// homePanelGroup is a SECOND HEADING INSIDE ONE PANEL: a run of the panel's own
// rows, drawn under a dim line of their own, which folds to nothing before the
// panel gives up a row above it.
//
// IT IS THE PANEL'S SHAPE SAID AGAIN ONE LEVEL DOWN, deliberately: a panel is a
// heading, its rows and a fold, and a group is a line, its rows and the same
// fold — so a squeeze needs no second rule. The rows sit at the FOOT of
// [homePanelRows.lines], so the ordinary bottom-up cut already takes them first,
// and when none of them is left on the screen the group's line goes and the
// panel's own fold names the group instead of saying `more`
// ([homeGridPanel.fold]).
//
// `needs you` is the only panel with one today: the landings a person has not
// checked, under the live questions that stopped a conversation
// (homepanel_needs.go).
type homePanelGroup struct {
	// at is where the group's rows begin in [homePanelRows.lines]; everything
	// before it is the panel's own.
	at int
	// word is the group's name — the whole of its line, and the word the
	// panel's fold uses instead of `more` while the group is shut. A group line
	// carries no count and no clause: the rows are under it, and the fold counts
	// what it hides (owner, 2026-09-15; it used to say `to check · 8` with
	// `finished, nobody has checked it` at its right).
	word string
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
	// cellGroup is a group's own line inside a panel: its word and count dim at
	// the left ([homePanelGroup]). It is not a stop and it is never lit.
	cellGroup
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
	// chatKey is an already resolved tab identity for live conversation bullets.
	chatKey string
	kind    homeCellKind
	panel   homePanelID
	mark    homeCellMark
	title   string
	// pad is the cells the title is padded to before the note, so a panel's
	// notes stand in one column (the projects panel's counts).
	pad int
	// note is a dim clause after the title; tag and right are the dim facts
	// at the right margin, the tag giving way first.
	note, tag, right string
	// bold is this window's own conversation.
	bold bool
	// underline marks a hovered project name without lighting its facts.
	underline bool
	// closed conversations stay dim even when the cursor is on them.
	closed bool
	// path says the title is a folder's path, which is cut FROM THE LEFT —
	// `…/code/codeaf` — so the folder's own name and the facts beside it
	// stay on the row ([homeCellPathTitle]).
	path bool
	// hold says the right-hand word is a fact about the DOOR — `folder gone`,
	// `another window`, `coming here`, `here` — and is cut around rather than
	// dropped, because it is what enter will do; door is the longer sentence a
	// held row grows into under the cursor ([app.homeCellDoor]).
	hold bool
	door string
	// thread is the conversation the row belongs to, spelled as `threads`
	// spells it, and it heads the row's description as its own title line —
	// `thread: <name>`, then a blank line, then [sub] (owner, 2026-09-17). It
	// is drawn wherever the description is: in the description column, or
	// under the row on a frame without one, where the row grows three lines
	// rather than one ([homeCell.grownLines]).
	thread string
	// sub is the line under the row, and subRight what that line carries at
	// its right — the answers a key sends.
	sub, subRight string
	// answers is the clause of chips this row WOULD draw at the right of its
	// second line — each key beside its own word — and "" for a row with no
	// answer to offer. Whether they are actually drawn is the frame's to say:
	// exactly one row draws them ([app.homeAnswerAt]).
	answers string
	// grows says the row draws that line ONLY WHILE IT IS THE ROW BEING READ —
	// under the pointer, or under the cursor when nothing is pointed at
	// ([homeView.previewAt]) — which is how every row of the field is one line
	// at rest and two when it is looked at. The line it grows into is reserved
	// by its panel ([homeGridPanel.height]) so the column does not change shape
	// as the cursor walks over the rows.
	grows bool
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
//
// A ROW THAT GROWS UNDER THE CURSOR IS ONE ROW HERE. The line it grows into is
// the PANEL's reservation ([homeGridPanel.height]) rather than this row's
// height, because exactly one row of a frame is under the cursor and a layout
// that changed shape as the cursor walked over these rows would move every panel
// under them on every arrow key ([homeCell.grows]).
func (l homeLine) height() int {
	if l.cell != nil && l.cell.sub != "" && !l.cell.grows {
		return 2
	}
	return 1
}

// grownLines is how many lines a growing row draws under itself while it is
// being read on a frame without a description column: its sentence, or — for a
// row that names its thread — the thread's title line, a blank, and the
// sentence. It is what a panel reserves for the one row that will grow
// ([homeGridPanel.growLines]).
func (c *homeCell) grownLines() int {
	if c == nil || !c.grows || strings.TrimSpace(c.sub) == "" {
		return 0
	}
	if strings.TrimSpace(c.thread) != "" {
		return 3
	}
	return 1
}

// homeThreadWord leads a description's thread line: `thread: Prime Sieve`.
const homeThreadWord = "thread: "

// NO ROW'S SENTENCE IS DRAWN AT REST ANY MORE. `needs you`'s questions were the
// one exception — the owner made them so on 2026-09-15, so that what was waiting
// on a person could be read without walking onto it — and reversed it on
// 2026-09-17: the amber `?` always shows, and the question under it shows only
// while the row is being read, under the pointer or the cursor, like the
// description of every other row of the field ([homeCell.grows]).

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
	// tight is that the column was too short for the panel's full reservation
	// for the row that grows ([homeGridPanel.growLines]): a squeezed column
	// gives up the thread's two extra lines before it gives up a row, keeps one
	// line as it always did, and while a thread row on it is read the lines
	// under it move down two and the column's foot is clipped — a short frame
	// costs a moment of shape, never a row ([squeezeColumn]).
	tight bool
	// desc is that this frame draws the description column, which changes what
	// a ROW is: its second line is drawn there instead of under it, so the panel
	// neither draws that line nor reserves the room for it ([homeDescOn]).
	desc bool
	// gapAbove is a second blank row over this panel's heading. It is set on the
	// first quiet panel of the rail, so the pinned pair at the top is read as a
	// group that lives there and the panels under it as the ones that are only
	// passing through ([homeGridLayout]).
	gapAbove bool
	// expanded is the panel whose fold somebody opened: it wants every row it
	// has, the squeeze takes from it last, and its fold is the way back
	// ([homeGridPanel.fold]).
	expanded bool
}

// natural is how many rows the panel shows at its natural height: its rest, or
// all it holds when that is fewer — and all it holds, however many, for the
// panel somebody opened.
func (p homeGridPanel) natural() int {
	if p.expanded {
		return len(p.read.lines)
	}
	return min(p.slot.rest, len(p.read.lines))
}

// empty reports a panel with no rows at all, which draws its whisper instead.
// A panel whose every row aged out is not empty: its fold is the door to them.
func (p homeGridPanel) empty() bool {
	return len(p.read.lines) == 0 && p.read.more == 0 && p.read.older == 0
}

// hidden is how many rows the fold stands for.
func (p homeGridPanel) hidden() int { return len(p.read.lines) - p.shown + p.read.more }

// folds reports that the panel draws its fold line: while it is hiding rows,
// and while it is open — an open panel's fold is the way to shut it again.
func (p homeGridPanel) folds() bool { return p.hidden() > 0 || p.read.older > 0 || p.expanded }

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
	n := 0
	if p.slot.word != "" {
		n = 1
	}
	for _, line := range p.read.lines[:p.shown] {
		// A ROW IS ONE LINE WHERE THE DESCRIPTION COLUMN HAS ITS SECOND, and the
		// reservation below goes with it — unless it is a row that keeps its own
		// ([homeCell.keepsSub]).
		if p.desc {
			n++
			continue
		}
		n += line.height()
	}
	// A GROUP'S LINE AND THE AIR OVER IT ARE THE PANEL'S ROWS TOO, drawn only
	// while the group has a row of its own left on the screen ([homeGridPanel.lines]).
	if p.groupOpen() {
		n++
		if p.read.group.at > 0 {
			n++
		}
	}
	// AND THE LINES ARE KEPT FOR THE ROW THE CURSOR WILL GROW. Which row that
	// is belongs to the paint; that one of them will grow, and by how much, is
	// known here, and reserving it is what keeps the column the same height
	// whichever row the cursor is standing on ([homeCell.grows]).
	if !p.desc {
		if p.tight {
			n += min(1, p.growLines())
		} else {
			n += p.growLines()
		}
	}
	if p.folds() {
		n++
	}
	return n
}

// groupOpen reports that the panel is drawing its group's own line, which it
// does while at least one of the group's rows is still shown.
func (p homeGridPanel) groupOpen() bool {
	return p.read.group != nil && p.shown > p.read.group.at
}

// growsARow reports that one of the rows on the screen will grow a line under
// the cursor.
func (p homeGridPanel) growsARow() bool { return p.growLines() > 0 }

// growLines is the most lines any row on the screen grows while it is being
// read ([homeCell.grownLines]): the panel's reservation for the one row that
// will.
func (p homeGridPanel) growLines() int {
	most := 0
	for _, line := range p.read.lines[:p.shown] {
		most = max(most, line.cell.grownLines())
	}
	return most
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
	// AND THE RAIL'S OWN GAP IS A ROW OF THE COLUMN. A squeeze that did not
	// count it would fit the column to the row below the screen.
	first := true
	for _, p := range column {
		if p.height() == 0 {
			continue
		}
		if p.gapAbove && !first {
			n++
		}
		first = false
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
// lowest keep first — and THE OPEN PANEL LAST, whatever its keep. Somebody
// asked to see all of it, so every other panel goes to its floor before that
// one gives up a row; and since the regrow hands rows back from the end of this
// order, it is also the first to get them.
func byKeep(column []*homeGridPanel) []*homeGridPanel {
	order := append([]*homeGridPanel(nil), column...)
	sort.SliceStable(order, func(i, j int) bool {
		if order[i].expanded != order[j].expanded {
			return order[j].expanded
		}
		return order[i].slot.keep < order[j].slot.keep
	})
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
	// THE RESERVATION GIVES WAY BEFORE A ROW DOES: every panel keeps one line for
	// the row that grows and no more ([homeGridPanel.tight]), and only if the
	// column is still too tall are rows taken.
	for _, p := range order {
		if homeColumnHeight(column) <= room {
			break
		}
		p.tight = true
	}
	for _, p := range order {
		if homeColumnHeight(column) <= room {
			break
		}
		p.shrink()
	}
	for _, p := range byDrop(order) {
		if homeColumnHeight(column) <= room {
			break
		}
		p.dropped = true
	}
	regrowColumn(order, column, room)
}

// byDrop is the order a squeeze drops panels in: EVERY WHISPERING PANEL BEFORE
// ANY PANEL WITH ROWS, each group lowest keep first. A whisper says what would
// be here; a row is something a person can stand on and open. At 120×14 with
// no question waiting, the old order kept `needs you`'s whisper and dropped
// `threads` whole, so the page had no row at all — a home with nothing
// to press is not a home, whatever its priorities say.
func byDrop(order []*homeGridPanel) []*homeGridPanel {
	drop := make([]*homeGridPanel, 0, len(order))
	for _, p := range order {
		if p.empty() {
			drop = append(drop, p)
		}
	}
	for _, p := range order {
		if !p.empty() {
			drop = append(drop, p)
		}
	}
	return drop
}

// regrowColumn hands back what the squeeze did not need, a row at a time, to
// the panels it cut, the most important first. A floor is a whole step, and a
// drop frees a whole panel, so the squeeze can overshoot — and air under a
// column while `threads` is folded is the squeeze spending the wrong
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

// shrink cuts a panel to its floor: the heading, as many whole rows as fit under
// it, and the fold. A panel already inside its floor is left alone.
//
// IT TAKES ROWS FROM THE BOTTOM, WHICH IS WHY A GROUP NEEDS NO RULE OF ITS OWN.
// A group's rows sit at the foot of the panel's list ([homePanelGroup]), so the
// walk down from the natural height spends them first and the group's line goes
// with the last of them — the `unread` landings fold before a `needs you` row
// is given up, without this function knowing that either exists.
func (p *homeGridPanel) shrink() {
	for p.shown > 0 && p.height() > p.slot.least {
		p.shown--
	}
}

// homeGridLayout reads every panel and fits each column: the panels in table
// order, each in the column its own content puts it in ([homeColumnOf]), at
// that column's width.
//
// THE FIELD IS ONE COLUMN AT EVERY WIDTH, and what will not fit in it FOLDS
// rather than spilling sideways — the same squeeze that has always run at the
// widths with no second column to spill into (law 5). So the first panel with
// rows is at the top left corner always, and the cursor's panel is in column
// zero always, which is what keeps `↓` off the tab bar landing on the same line
// at every width ([homeView.placesTop], homebridge.go): a landing that moved
// with the frame is a landing nobody can build a habit on.
//
// AND THE COLUMN IT NO LONGER SPILLS INTO IS THE DESCRIPTION'S (owner,
// 2026-09-15). A field that could take the middle when it ran out of room made
// the middle a column that was a description sometimes and a panel other times;
// reserving it costs a squeeze on a short frame and buys a column that means one
// thing at every size ([homeDescCol]).
func homeGridLayout(in *homeGridInput, cols, width, room int) [][]*homeGridPanel {
	columns := make([][]*homeGridPanel, cols)
	_, widths := homeGridGeometry(width, cols)
	rail := homeRailCol(cols)
	for _, slot := range homePanelOrder {
		read := slot.panel.rows(in)
		p := &homeGridPanel{slot: slot, read: read, expanded: in.openedOn && in.opened == slot.panel.id()}
		p.shown = p.natural()
		p.desc = homeDescOn(cols)
		at := homeColumnOf(slot, cols, p.empty())
		if p.empty() {
			p.whisper = homeWhisperLines(slot.panel.whisper(), widths[at])
		}
		columns[at] = append(columns[at], p)
	}
	// ONE COLUMN HAS NO RAIL AND THEREFORE NO GROUPS IN IT. Every panel is in
	// the one column in table order, which is the reading order the narrow
	// frame has always had — sorting the pinned pair to the top there would put
	// projects and spend above the question waiting to be answered.
	if cols > 1 {
		columns[rail] = orderRail(columns[rail])
		markRailGap(columns[rail])
	}
	for _, column := range columns {
		fitColumn(column, room)
	}
	return columns
}

// homeDescCol is the column the selected row's description stands in, and
// [homeNoLine] where this frame has no room for one.
//
// IT IS THE MIDDLE COLUMN AND ONLY EXISTS AT THREE (owner, 2026-09-15). Two
// columns are the field and the rail with nothing between them, and one column
// is the whole screen read top to bottom — so at both of those a row keeps its
// description under itself, exactly as every row did before this column existed.
func homeDescCol(cols int) int {
	if !homeDescOn(cols) {
		return homeNoLine
	}
	return 1
}

// homeDescOn reports that this frame draws the description column, which is the
// question the ROW asks: a row whose description is drawn elsewhere does not
// draw it under itself, and does not reserve the line for it either
// ([homeGridPanel.height]).
func homeDescOn(cols int) bool { return cols >= 3 }

// orderRail puts the pinned panels at the top of the rail and the quiet ones
// under them, each group in table order. THE TOP OF THE RAIL IS THE PART THAT
// DOES NOT MOVE: projects and spend are there on every machine on every day, so
// they are the corner a person can aim at, and a panel that is only in the rail
// because it is quiet today is never drawn above them.
func orderRail(rail []*homeGridPanel) []*homeGridPanel {
	out := make([]*homeGridPanel, 0, len(rail))
	for _, p := range rail {
		if p.height() == 0 {
			continue
		}
		if p.slot.pinned {
			out = append(out, p)
		}
	}
	for _, p := range rail {
		if !p.slot.pinned {
			out = append(out, p)
		}
	}
	return out
}

// markRailGap puts the rail's one extra blank over the first panel that is
// there because it is quiet rather than because it is pinned. THE PINNED PAIR
// READS AS THE TOP OF THE RAIL and the quiet ones as a list under it, which is
// the whole difference between a panel that lives in the rail and a panel that
// is in it today.
func markRailGap(rail []*homeGridPanel) {
	seen := false
	for _, p := range rail {
		if p.height() == 0 {
			continue
		}
		if p.slot.pinned {
			seen = true
			continue
		}
		if seen {
			p.gapAbove = true
		}
		return
	}
}

// homeWhisperLines is a whisper at one column's width, standing in a row's lead.
//
// A WHISPER WRAPS; IT IS NEVER CUT. It is the one sentence an empty panel has,
// and the half after an ellipsis — `a digit answers them`, `"every morning at
// 9"` — is the half that says what to do. So it takes the dim lines it needs, and
// the panel's height is its heading and all of them. A build before the first
// frame knows no width, and keeps the sentence on one line until one arrives.
// A place's whisper is wrapped here too (placeprose.go's [placeWhisperLines]).
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
				// AND THE RAIL'S GROUPS ARE TOLD APART BY AIR AND NOTHING ELSE
				// — the grid draws no rules ([homeGridGutter]).
				if p.gapAbove {
					h.addGridLine(homeLine{kind: homeBlank}, at)
				}
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
	var out []homeLine
	if head != "" {
		out = append(out, homeLine{kind: homeSwitchHead, cell: &homeCell{kind: cellHead, panel: id, title: head, note: p.slot.explainer, right: p.read.right, money: p.read.money}})
	}
	if p.empty() {
		for _, words := range p.whisper {
			out = append(out, homeLine{kind: homeSwitchHead, cell: &homeCell{kind: cellWhisper, panel: id, title: words}})
		}
		return out
	}
	if group := p.read.group; group != nil && p.groupOpen() {
		out = append(out, p.read.lines[:group.at]...)
		// A GROUP STANDS OFF THE ROWS ABOVE IT with the same blank row that
		// stands between two panels, and takes none when it is the panel's
		// first line.
		if group.at > 0 {
			out = append(out, homeLine{kind: homeBlank})
		}
		out = append(out, homeLine{kind: homeSwitchHead, cell: &homeCell{kind: cellGroup,
			panel: id, title: group.word}})
		out = append(out, p.read.lines[group.at:p.shown]...)
	} else {
		out = append(out, p.read.lines[:p.shown]...)
	}
	if p.folds() {
		out = append(out, p.fold())
	}
	return out
}

// fold is the panel's last line, and IT IS A TOGGLE (owner, 2026-09-15). Shut,
// it is `N more`, counting everything the panel is not showing — the rows past
// its budget, the rows the squeeze took, and the landings `needs you` aged out
// — and enter opens the panel: its cap is lifted, the other panels squeeze to
// their floors (law 5), and it takes the column. Open, it is `N fewer`, and
// enter shuts it again.
//
// IT WEARS NO MARK. The places' fold doors are `▸ 11 more` and `▾ 11 fewer`
// ([foldDoor]); this one is the words alone, because the grid has two marks and
// no other (law 8) — the amber question and the one moving cell — and a third
// glyph down the column, one per panel, would be the thing that law exists to
// refuse. The words say which way it goes: `more` opens, `fewer` shuts.
//
// AN OPEN PANEL TALLER THAN THE COLUMN STILL COUNTS WHAT IT CANNOT SHOW: its
// fold reads `3 fewer · 40 more` — the way back first, then how many are past
// the frame. THE FOLD NAMES NO PLACE, because `enter` on it toggles the panel
// and a line that named a place `enter` did not go to would be a door drawn on
// a wall (review of #1046). The way to the rest is the panel's HEADING, which
// opens the place that owns the panel (law 10): tasks for `needs you`,
// `tasks` and `since you left`, standing for `standing`. `threads` has no
// place of its own to open (the search place was its door until 2026-09-17;
// the box under home is the search now), so its heading names only the panel.
// It used to be the other way round: law 9 said the fold IS the door, `N more
// · tasks` opened the tasks place, and `threads`'s `N more · type to find one`
// was not a stop at all because typing was its door; the owner found one line
// that opened somewhere and another that could not be stood on and asked for
// one thing that expands.
func (p homeGridPanel) fold() homeLine {
	words := ""
	rest := p.hidden() + p.read.older
	if p.expanded {
		words = foldWords(true, p.opened(), "")
		if rest > 0 {
			words += rowSep + groupedInt(rest) + " " + p.foldMoreWord()
		}
	} else {
		// The group's word where the group's own line is not on the screen,
		// so `8 unread` is still the whole of what a squeezed panel says about
		// its landings ([homeGridPanel.foldMoreWord]).
		words = groupedInt(rest) + " " + p.foldMoreWord()
	}
	cell := &homeCell{kind: cellFold, panel: p.slot.panel.id(), title: words}
	return homeLine{kind: homeFold, dir: homeFoldKey, folded: !p.expanded, quiet: rest, cell: cell}
}

// opened is how many rows opening the panel showed that its resting height
// would not have: what the open fold's `N fewer` counts, and never fewer than
// one, because a fold that reads `0 fewer` is a line about nothing.
func (p homeGridPanel) opened() int {
	return max(1, p.shown-min(p.slot.rest, len(p.read.lines)))
}

// homeFold is a panel's fold line on the grid ([homeGridPanel.fold]): a stop,
// a toggle on enter, and the same line whichever way it is standing.
const homeFold homeRowKind = 245

// homeFoldMoreWord is what the fold calls the rows it is standing for when they
// are simply more of the panel's own.
const homeFoldMoreWord = "more"

// foldMoreWord is what the fold calls the rows it is standing for: the GROUP's
// name where every one of them is the group's and the group's own line is not on
// the screen — `8 unread` is the whole of what a squeezed panel says about its
// landings — and `more` everywhere else.
func (p homeGridPanel) foldMoreWord() string {
	group := p.read.group
	if group == nil || p.groupOpen() || p.shown < group.at {
		return homeFoldMoreWord
	}
	return group.word
}

// homeFoldKey is the identity a fold line carries in its dir, so the cursor on
// `3 more` is told apart from every row that carries a real directory there
// ([homeLine.sameRow] tells folds apart by their panel). It starts with a NUL,
// which no directory path contains.
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

// THE ARROWS STAY IN THEIR COLUMN. `←` and `→` used to cross into the
// neighbouring column, onto the row nearest the one they left (DESIGN §6 ruling
// 6, "columns win the arrow"), and the foot named `ctrl+o open folder` on every
// field row because the strip was then not one arrow away. The owner ruled
// (2026-09-17) that nothing outside the left column is to be walked at all: the
// rail is read, not stood on — `projects` and `spend` have no stops — and a
// person on a field row who pressed `→` landed on a project and then opened its
// strip under the row they had just left. So the two arrows are the router's
// again on every column, as they are at one column: `→` opens the row's verbs
// and `←` closes them, and the foot is the resting sentence alone.

// homeRowFolder is the folder `ctrl+o` opens for a row, and "" for a row that
// belongs to no folder — a spend row, a fold.
//
// EVERY ROW OF THE FIELD ANSWERS, not only a conversation's: a standing order
// carries the workspace it stands over, and a `since you left` line carries the
// conversation the news happened in. The same shortcut works on every kind of row.
func homeRowFolder(line homeLine) string {
	switch line.kind {
	case homeSession:
		return strings.TrimSpace(line.row.Workspace)
	case homeItem:
		return strings.TrimSpace(line.item.Workspace)
	case homeLedger:
		if line.item.ID != "" {
			return strings.TrimSpace(line.item.Workspace)
		}
		if line.cell != nil && line.cell.row != nil {
			return strings.TrimSpace(line.cell.row.session.Workspace)
		}
	}
	return ""
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

// homeHeadPress is a press on a panel's heading: THE PLACE THE HEADING NAMES
// (the order table's head column), exactly as a press on that place's tab
// word would go. It reports whether the press landed on a heading at all, and
// takes one that names no place and does nothing with it — a heading is not a
// row, and letting the press fall through would act on whatever the map says
// is under it.
//
// IT READS THE HEADINGS THE FRAME DREW ([homeMark.heads]), never a second
// count of where the panels ended up: a squeeze drops whole panels, and a
// heading worked out apart from the draw would open the wrong place on exactly
// the frame a person could not tell why.
func (a *app) homeHeadPress(x, y int) (tea.Cmd, bool) {
	at, ok := a.homeHeadAt(x, y)
	if !ok {
		return nil, false
	}
	if !a.homeHeadDoor(at) {
		return nil, true
	}
	return a.showPage(homeSlotOf(a.home.lines[at].cell.panel).head), true
}

// homeHeadAt is the heading line under a pointer at (x, y), read from the
// headings the frame drew ([homeMark.heads]). It is the one lookup a press and
// a hover share, so the heading that underlines under the pointer is the
// heading the press would open ([app.homeHover]).
func (a *app) homeHeadAt(x, y int) (int, bool) {
	marks := a.home.gridMarks
	if y < 0 || y >= len(marks) || !marks[y].grid {
		return -1, false
	}
	at := marks[y].heads[a.home.gridColumnAt(x)]
	if at < 0 || at >= len(a.home.lines) || a.home.lines[at].cell == nil {
		return -1, false
	}
	return at, true
}

// homeHeadDoor reports whether the heading on line at names a place. The
// registry answers rather than the page id, which keeps this file from asking
// a page id what it is (placelaws_test.go, law 1).
func (a *app) homeHeadDoor(at int) bool {
	return homeHeadOpens(a.home.lines[at].cell.panel)
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

// homeGridAnswer is an answer key on the resting grid: THE ONE ANSWERING ROW OF
// THE FRAME, FROM ANYWHERE ON HOME (law 7). That row is the one drawing the
// chips, so the key a person presses is one they can see on the screen; a key
// with no such row falls through to the row under the cursor and then to the
// box, as it always did.
func (a *app) homeGridAnswer(key string) (tea.Cmd, bool) {
	if !a.home.gridOn() {
		return nil, false
	}
	at := a.homeAnswerAt()
	if at < 0 {
		return nil, false
	}
	line := a.home.lines[at]
	if line.task != nil {
		return a.homeAnswerLanding(line, key)
	}
	return a.answerRowKey(a.homeTrue(line.row), key)
}

// homeAnswerAt is THE ONE ROW OF THE FRAME THAT DRAWS THE CHIPS AND TAKES THE
// KEY: the row under the cursor when it can take an answer, and the top row of
// `needs you` that can otherwise. It is -1 when nothing on the frame is
// answerable.
//
// THE CURSOR OUTRANKS THE TOP ROW BECAUSE A PERSON WHO WALKED SOMEWHERE MEANT
// IT. Law 7 put the chips on the top question so that a digit worked without
// moving the cursor, and that still holds for a frame nobody has walked; once
// somebody has walked onto a landing, `a` has to mean THAT landing or the key
// answers a row the person is not looking at. One function, so the chip and the
// key can never be two different rows.
func (a *app) homeAnswerAt() int {
	if a.homeOffersAnswer(a.home.cursor) {
		return a.home.cursor
	}
	for at := range a.home.lines {
		if a.homeOffersAnswer(at) {
			return at
		}
	}
	return -1
}

// homeOffersAnswer reports that the line at `at` is a row this window can
// actually send an answer for.
func (a *app) homeOffersAnswer(at int) bool {
	if at < 0 || at >= len(a.home.lines) {
		return false
	}
	words := a.homeRowOffer(a.home.lines[at])
	return words != "" && words != answerWaitingWord
}

// homeRowOffer is the answers ONE row could take, under the answer band's own
// four rules ([drawAnswerBand]) — the question is fresh and offered answers,
// this window has somewhere to leave one, it has not already sent one (the
// waiting word stands in for a moment after it has), and no question this
// window raised about the row is standing over it ([app.answersStepAside]).
// Every fact is in memory; nothing is read.
//
// A LANDING IS THE SAME FOUR RULES WITH NOTHING ASKING. There is no live
// question to be fresh, so what is left is the doorstep: a conversation with a
// folder to leave the answer in can be answered from here, and one without
// cannot (homepanel_needs.go's [app.homeAnswerLanding]).
func (a *app) homeRowOffer(line homeLine) string {
	if line.cell == nil || line.cell.answers == "" {
		return ""
	}
	row, now := a.homeTrue(line.row), a.home.world.Read
	if line.task != nil {
		question, ok := needsLandingQuestion(line)
		if !ok {
			return ""
		}
		if sent, ok := a.answerSent(row, question); ok {
			if now.Sub(sent.at) < answerHoldFor {
				return answerWaitingWord
			}
			return ""
		}
		if a.leaveAnswer == nil && !a.answeringHere(row) {
			return ""
		}
		return line.cell.answers
	}
	if line.kind != homeSession {
		return ""
	}
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
	return line.cell.answers
}

// homeRowAnswers is what the line under one row carries at its right: the
// answers where this is the frame's one answering row, the waiting word where
// this window has already answered it, and the row's own door word — `enter`,
// `enter on standing` — on the row the cursor is standing on.
//
// THE DOOR WORD IS SAID ONCE, UNDER THE CURSOR. It is a key legend, and a legend
// is for the key a person is about to press; drawn on every question at once it
// was a column of `enter`s that told nobody anything (owner, 2026-09-16, seen on
// a narrow frame where the second line is under every question rather than
// beside the cursor's). The description column already drew it only for the
// selected row ([app.homeDescNote]); this makes the second-line shape agree.
func (a *app) homeRowAnswers(line homeLine, at int) string {
	// The option menu owns the letters while open; normal answer hints would
	// promise a different action for the same key.
	if line.cell == nil || a.strip.open {
		return ""
	}
	if words := a.homeRowOffer(line); words != "" {
		if words == answerWaitingWord || at == a.homeAnswerAt() {
			return words
		}
	}
	if at != a.home.cursor {
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
// Escape returns to the conversation behind Home.
func (a *app) homePreselect() {
	if !a.home.gridOn() {
		return
	}
	front := a.frontTabKey()
	for at := len(a.prev) - 1; at >= 0; at-- {
		if key := a.prev[at]; key != "" && key != front {
			if tab, ok := chatTabAt(a.tabList(), key); ok {
				a.home.point(tab.file)
				return
			}
		}
	}
}
