package tui3

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/tui2/tokens"
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
// AND IT IS THE SWITCHER. `enter` on any row on this screen opens it, whichever
// project it belongs to, and the conversation you were in stays OPEN behind it —
// still streaming its turn, still running its tasks, one keystroke away.
//
// It used to refuse, with a dim `elsewhere` on every project but this window's
// own, and the reasoning behind that refusal was right: the approval gate, the
// crew, the spend rail and the saved shapes of work are all resolved from a
// workspace at launch, and carrying a conversation across without carrying them
// would be a window quietly running under another project's permissions. What
// changed is the conclusion. A conversation never moves between projects here
// either — a second project means a SECOND CONVERSATION, built the way the first
// one was, on its own workspace, with its own gate (keeper.go). Nothing is
// carried across, because nothing crosses.

// homeShown is how many conversations a project draws before the rest collapse
// into one line. Four is what the reference layout holds under a heading and it
// is roughly what a person scans without reading: past it a section stops being
// a shape on the page and becomes a list.
//
// IT IS A FLOOR AND NOT A CEILING. Every row with work running or work left
// unfinished is drawn whatever the count says — those are the rows this screen
// exists for — and the collapse takes only the quiet ones underneath them.
const homeShown = 4

// homeOpenProjects is how many projects home draws OPEN — heading, rows, item
// band, quiet fold — before the rest collapse to one line each.
//
// THREE, AND THE FIRST OF THEM IS ALWAYS THIS WINDOW'S OWN. The screen was an
// unorganised wall: every project on the machine got a heading and four rows,
// most of them saying `elsewhere`, and a person looking for the one thing that
// wanted them had to read past six projects they had not touched in a week. So
// home now opens the project you are standing in and the two you spoke in most
// recently, and folds everything else into the `elsewhere` block below them —
// which is exactly the shape of the question this screen answers: here is what
// you are doing, and here is everything else, one line each, still reachable.
//
// A FOLDED PROJECT IS NEVER A HIDDEN ONE. Its line says how many conversations
// it holds and how long since anybody spoke in it, it surfaces anything that
// needs somebody or is running, and enter opens it in place. And a search sees
// through the whole arrangement: with anything typed there are no tiers at all
// (see [homeView.buildWorld]).
const homeOpenProjects = 3

// homeFoldedProjects is how many folded project lines the `elsewhere` block
// draws before the remainder go behind one further fold. Eight is about what
// fits under three open projects on an ordinary frame without the block
// becoming the screen — and past it the block would be the wall again, one line
// per row instead of four.
const homeFoldedProjects = 8

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

// homeTickMsg is that clock's beat, carrying the generation of the home it was
// armed by.
//
// THE GENERATION IS THE SAME DEVICE EVERY LANE ON THIS SURFACE USES, and home
// needed one the moment it became the switcher. [app.homeBeat] re-arms whenever
// home is open, so closing home and opening it again before an old tick landed
// started a SECOND self-rearming chain — two clocks re-reading the disk, then
// four. That was theoretical while home was a screen somebody visited; it is not
// while home is the way between conversations.
type homeTickMsg struct{ gen int }

// homeTick schedules the next reading.
func homeTick(gen int) tea.Cmd {
	return tea.Tick(homeEvery, func(time.Time) tea.Msg { return homeTickMsg{gen: gen} })
}

// homeBeat is the beat, arriving. A beat that finds home closed re-arms
// nothing, which is how the clock stops — and one from a home that has since
// been closed and reopened re-arms nothing either, which is how there stays one
// clock.
func (a *app) homeBeat(gen int) tea.Cmd {
	if !a.home.open || gen != a.homeGen {
		return nil
	}
	a.refreshHome()
	// A task starting in another window arrives on this beat, and the spinner it
	// earns needs the fast clock — woken here because this is the only moment
	// home learns anything ([app.homeAnimating]; paint keeps it turning and lets
	// it stop by the same test).
	if a.homeAnimating() {
		return tea.Batch(homeTick(a.homeGen), a.wake())
	}
	return homeTick(a.homeGen)
}

// homeAnimating reports whether something on home is truly MOVING: a row on the
// column with work running this instant. It is what earns the paint clock —
// home is otherwise a still page on a three-second beat ([homeEvery]), and that
// law holds exactly until a spinner has to keep a promise. The clock is woken
// where home learns things ([app.openHome], [app.homeBeat]) and [app.paint]
// keeps it turning against this same test, so the moment the last running row
// lands or its presence goes stale the page falls still again on its own.
//
// The linear tier never animates ([glyphRunASCII]'s block states the law), so
// it never earns the clock either.
func (a *app) homeAnimating() bool {
	if !a.home.open || a.linear {
		return false
	}
	for _, line := range a.home.lines {
		if line.kind == homeSession && line.row.Tasks.Running > 0 {
			return true
		}
	}
	return false
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
// taught, only "wants you", "moving", "stopped mid-way", "landed something new"
// and "at rest".
//
// THE TRIANGLE IS THE ONLY ONE THAT POINTS AT ANYTHING. The other three are
// round and read as weather; a conversation stopped on a question is the one
// row on this screen that is asking for a hand, and it gets the one shape that
// looks like it is asking.
//
// A ROW WITH WORK RUNNING SPINS. On an animating frame the ● gives way to the
// braille spinner the rail already turns for the same fact ([app.taskStateMark],
// task.go), because "something is happening this instant" is that vocabulary's
// one promise — and home KNOWS it through the same presence file the counts are
// read from, believed for the same fifteen seconds. The ● stays as the still
// tier: the linear tier does not animate, and the rail's away rows keep it too.
//
// AND A ROW AT REST THAT LANDED WORK SINCE YOU LAST LOOKED WEARS THE TICK — the
// rail's own ✓ ([glyphDone]) — instead of the empty circle, which is the whole
// of home's "while you were away": no notification, no banner, one cell of one
// row saying something finished here (see [homeView.seen]).
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
	// homeOpenWord is what a conversation THIS PROCESS holds says when it has
	// nothing more urgent to say. It goes where `another window` goes — below
	// the states, above `N landed` — and it is the word [session.SessionRow]
	// already uses for the same fact seen from another terminal, which is why a
	// conversation open here and one open in another window are told apart by
	// WHICH window rather than by a second word.
	homeOpenWord = "open"
	// homeGoneWord is the refusal on a row whose project folder is not there any
	// more. It is the sentence the door says too ([WorkspaceGoneWord], and the
	// door quotes this constant so there is one of it), because home can be
	// beaten to the answer by a directory removed between the scan and the
	// keystroke.
	//
	// IT IS NEEDED BECAUSE ENTER CAN NOW LEAVE THIS PROJECT. Home stats the
	// transcript and never the workspace, which was harmless while enter only
	// opened conversations of the folder you were standing in. A repository
	// deleted or moved since its last conversation would otherwise be opened as
	// an agent whose tool root does not exist, and every bash and every relative
	// path in it would fail in a way nothing on screen explains.
	homeGoneWord = WorkspaceGoneWord
	// homeElsewhereWord names the block of projects home is not drawing open.
	//
	// IT NO LONGER MARKS A ROW THIS WINDOW CANNOT OPEN, because there is no such
	// row: enter opens any project on this screen. What it says now is purely
	// about the SHAPE of the list — three projects open, the rest one line each
	// under a dim rule — and everything under that rule is as reachable as
	// everything above it.
	homeElsewhereWord = "elsewhere"
	// homeElsewhereRuleWord is that rule, before the dashes are run out to the
	// column's width. A rule and not a heading, because the thing under it is
	// not a project — it is the REST of them.
	homeElsewhereRuleWord = "─ " + homeElsewhereWord + " "
	// homeElsewhereRuleASCII is the same rule where box drawing cannot be drawn.
	homeElsewhereRuleASCII = "- " + homeElsewhereWord + " "
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
	// homeLandedWord trails a count on a quiet row whose work finished since
	// home was last closed — `2 landed · 3h` — and homeFreshWord is the dim
	// caption the detail column hangs over those rows. Both are the delta the
	// look stamp buys (session's look.go), said in a person's words: nothing
	// "completed", nothing "notified", work landed while they were not looking.
	homeLandedWord = "landed"
	homeFreshWord  = "since you last looked"
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
	// typed and always at the very BOTTOM of the list. It is a cursor stop and
	// it is where the cursor RESTS by default, which is what keeps type-and-enter
	// meaning exactly what it meant before the box could also search.
	//
	// IT USED TO LEAD THE LIST, AND THAT SPLIT A PERSON'S ATTENTION IN TWO. The
	// characters appear in the box at the FOOT of the frame, and the row that
	// says what enter will do with them stood at the TOP — so typing made the eye
	// jump between the two far ends of the screen, and the cursor was up at one
	// end while the caret blinked at the other. Everything about typing now
	// clusters at the foot: the box, the row directly above it, and the hint line
	// under it, with the matches growing UPWARD above them. It is the drop-up the
	// command list and the "@" list already are (render.go's overlay), which is
	// what this screen should have been from the start — a list that rises out of
	// the thing you are typing into.
	//
	// IT IS ALSO THE ONLY THING THAT MOVES THE LIST. With nothing typed home is
	// a dashboard hanging from the top of its region and this row does not exist;
	// the first character brings it into being at the foot and lifts the list to
	// meet it (the block above [homeView.buildWorld] states both halves).
	homeAction
	// homeBlank is the empty line between projects.
	homeBlank
	// homeItem is ONE STANDING ITEM — a reminder, a watch, a rule, an overnight
	// job — under the project it belongs to (homestanding.go). It is a cursor
	// stop and it answers three keys: enter opens the conversation that asked
	// for it, p pauses it, s stops it.
	//
	// It is a kind of its own and not a conversation with a different glyph,
	// because the two objects have two lives and two doors: a conversation is
	// opened, an item is opened THROUGH — the door it offers is the chat that
	// made it, which is provenance and not identity.
	homeItem
	// homeItemFold is the band's tail — "…2 more keeping an eye" — and it is a
	// DOOR exactly as [homeQuiet] is, with the same two marks and the same
	// gestures. A line that says work is being hidden and cannot be asked to
	// stop hiding it is a dead end somebody hits and gives up at.
	homeItemFold
	// homeElsewhereRule is the dim line the folded block hangs under —
	// `─ elsewhere ─────`. IT IS NOT A CURSOR STOP: it names a section rather
	// than a thing, exactly as [homeHeading] does, and a cursor that had to be
	// walked over it would charge a keystroke for a piece of punctuation.
	homeElsewhereRule
	// homeProject is ONE WHOLE PROJECT ON ONE LINE — `▸ wisp   6 · 2d` — under
	// the rule, and it is a cursor stop and a DOOR: enter or → opens it IN
	// PLACE, where it becomes a block shaped like a tier-one project with a `▾`
	// on this same line; enter or ← folds it back. It is [homeQuiet]'s gesture
	// widened from a project's tail to the whole project ([homeView.expanded]
	// keeps both).
	//
	// ITS CARD IS THE PROJECT'S, not a conversation's ([bandKindProject]).
	homeProject
	// homeMoreProjects is the folded block's own tail — `▸ …4 more` — when the
	// block itself runs past [homeFoldedProjects]. A fold over a fold, with the
	// same two marks and the same gestures, because it is the same kind of
	// thing: a line standing for rows you cannot see.
	homeMoreProjects
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
	// bare marks a heading over a project with NO conversations in it, which is
	// on the screen because something is keeping an eye on that workspace
	// (homestanding.go's [app.readBareBands]).
	bare bool
	// proj is the WHOLE project, for [homeProject]: a folded project line says
	// how many conversations it holds and what is happening in them, and an
	// opened one carries the same line as its heading. It is the project as the
	// last reading saw it, held here rather than looked up again, for
	// [homeLine.row]'s reason — a rescan replaces the world underneath and an
	// index into it would go stale.
	proj session.Project
	// ex is the errand this line stands for, for [homeExchangeRow]. It is the
	// live object and not a reading of one: the row's tail says what the
	// exchange is doing at this instant, and a copy taken when the line was
	// built would be a spinner turning beside a state from four seconds ago.
	ex *homeExchange
	// note is the phone inbox's `since you left` row — one thing that happened
	// while you were away — and is nil on every other line (homephone.go).
	note *homePhoneNote
	// item is the standing item, for [homeItem], and view carries the two facts
	// about NOW that the document does not hold (homestanding.go's
	// [StandingItemView]). They are resolved when the row is built, so a row and
	// the card beside it can never disagree about whether something is firing.
	view StandingItemView
	item standing.Item
}

// homeBare is one project home knows only through the things keeping an eye on
// it. It carries a [session.Project] because everything downstream — the
// heading, the fold, the item rows — reads one, and the time separately because
// [session.Project.At] is a question about conversations and this project has
// none.
type homeBare struct {
	project session.Project
	at      time.Time
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
	// pane is, for each SCREEN row, which row of the right pane was drawn there
	// (-1 for none). It is the second half of [app.homeFrame]'s hit map — the
	// first half answers for the left column — and it exists for the same
	// reason: a press and a hover both index what the draw actually put on the
	// screen, so neither can reach a row the frame did not draw.
	pane []int

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
	// items is each project's standing band, keyed by the project's BUCKET
	// directory, as it stood at the last reading (homestanding.go). It is held
	// beside the world rather than read per frame for [app.homeHeld]'s reason:
	// the column is drawn on every keystroke and every pointer movement, and the
	// store is a directory of documents.
	items map[string][]StandingItemView
	// bare is the projects home knows ONLY through their standing items: a
	// workspace with a watch or a reminder on it and no conversation on this
	// machine at all (homestanding.go's [app.readBareBands]). They are kept
	// beside the world rather than folded into it because the world is a
	// reading of the projects root and these are not in it.
	bare []homeBare
	// itemsOpen is the bands somebody opened by hand, by the same key. It is a
	// second map and not a flag beside [homeView.expanded] because they are two
	// folds over two different things, and a person who opened the watches
	// should not thereby have opened eleven quiet conversations.
	itemsOpen map[string]bool

	// seen is when home was last closed — the look stamp, read once when the
	// screen opens (session's look.go). Work that landed after it is NEWS, and
	// news is marked: the ✓ on a resting row, the `landed` count in its note,
	// and the caption over the fresh rows of the card. Zero means there is no
	// origin to measure from — a first look — and nothing at all is marked.
	//
	// IT DOES NOT MOVE WHILE THE SCREEN IS UP. A stamp that advanced on every
	// rescan would unmark the news between two glances at it; the marks hold
	// until home closes, and closing is what writes the next stamp.
	seen time.Time
	// bucket is the project directory THIS window is in, which is what decides
	// whether enter can open a row (see this file's header).
	bucket string
	// exchanges is the errands somebody asked from this screen — `ask here` —
	// as the column draws them. They are real conversations with real
	// transcripts, kept OUTSIDE v3/projects so that this list can never grow a
	// session row for one, and they belong to the APP rather than to this view:
	// an exchange outlives the screen it was asked on, and this field is a copy
	// of the app's slice handed over by [app.showExchanges] whenever the list
	// itself changes (homeexchange.go's header states the whole lifecycle).
	exchanges []*homeExchange
	// exchangeIn is which project block each of those rows is drawn in, decided
	// once per build ([homeView.placeExchanges]).
	exchangeIn map[*homeExchange]string
	// last caches the tail of a conversation's journal by transcript path.
	// Reading one is a scan of the file ([session.Peek]) and the cursor moves
	// on every arrow key, so the second look at a row is free.
	last map[string]session.Summary
	// news and deliverables cache disk-backed bands by transcript. Both expire
	// with home's refresh clock so another window's arrivals become visible
	// without either index being read on the paint clock.
	news         map[string]homeNewsCache
	deliverables map[string]homeDeliverablesCache

	// msg is the last refusal, in this surface's own words. msgPath is the
	// directory that refusal NAMES, kept beside it rather than dug back out of
	// the sentence: the one refusal that carries a path is the one telling you
	// to go and stand somewhere else, and that place is a door (pathlink.go).
	// Empty for every other refusal, which name no file.
	msg     string
	msgPath string

	// The phone tier's own state (homephone.go, homesheet.go): the sheet over
	// the inbox, the triage sections somebody folded, the machine's news as the
	// `since you left` section reads it, and where the action bar landed.
	phone     bool
	sheet     homeSheet
	sheetHits []homeSheetHit
	sections  map[string]bool
	inbox     []homePhoneNote
	inboxAt   time.Time
	standRoot string
	bar       []hudSpan
	barRow    int

	// bandOpen is which list-shaped bands of the right column a person opened,
	// by band and subject (homebands.go); foldLines is the fold lines painted
	// this frame, so a click can find one. Both die with the screen.
	bandOpen  map[string]bool
	foldLines []bandFoldLine
	repos     map[string]homeRepoReading
	// week is what the standing ledger says about the last seven days, by item
	// id, and weekAt when it was read. ONE READING SERVES EVERY CARD on the
	// screen (homestanding.go's [app.standWeek]): the ledger is a file per day,
	// and a card asking per item would open the same week once per row.
	week   map[string]standing.Spend
	weekAt time.Time
}

// say replaces the refusal on screen, together with the directory it names.
//
// It is one call rather than two assignments because the path is the part that
// can go STALE: every refusal replaces the sentence, only one of them names a
// place, and a msgPath left behind by an earlier one would hang a link on a
// sentence that is no longer about it. Passing "" is how the other refusals say
// they name no file.
func (h *homeView) say(msg, path string) {
	h.msg, h.msgPath = msg, path
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
		open:         true,
		world:        session.ReadWorld(a.placesRoot()),
		seen:         session.LastLook(a.placesRoot()),
		bucket:       homeBucketOf(a.file),
		hover:        -1,
		last:         map[string]session.Summary{},
		news:         map[string]homeNewsCache{},
		deliverables: map[string]homeDeliverablesCache{},
		expanded:     map[string]bool{},
		itemsOpen:    map[string]bool{},
		// AND THE ERRANDS ARE STILL HERE. They belong to the window, not to the
		// screen, so opening home again finds every one that was still going —
		// with its row, its tail and its pane exactly as they were left
		// (homeexchange.go).
		exchanges: a.exchanges,
	}
	a.readStandBands()
	a.home.build()
	a.home.point(a.file)
	a.refreshHomeRepo(time.Now())
	a.touch()
	// THE PAINT CLOCK JOINS THE SLOW TICK when a row on the column is running:
	// the spinner and the count-up are claims about this instant, and a still
	// page cannot make them ([app.homeAnimating]).
	if a.homeAnimating() {
		return tea.Batch(homeTick(a.homeGen), a.wake())
	}
	return homeTick(a.homeGen)
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
	if a.hosted() || !a.canOpen() {
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
		open:      true,
		world:     world,
		seen:      session.LastLook(a.placesRoot()),
		bucket:    homeBucketOf(a.file),
		hover:     -1,
		last:      map[string]session.Summary{},
		expanded:  map[string]bool{},
		itemsOpen: map[string]bool{},
		exchanges: a.exchanges,
	}
	a.readStandBands()
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

// THE CLOCK'S GENERATION IS BUMPED HERE, which is what stops a tick armed by
// this home from re-arming itself into the next one ([homeTickMsg]).
func (a *app) closeHome() {
	a.homeGen++
	// The world in hand on the way out is the freshest reading there will be
	// until home opens again, so the door's advertisement is trued up here
	// rather than left as it was at boot.
	if len(a.home.world.Projects) > 0 {
		a.homeWorth = worldHasElsewhere(a.home.world, a.file)
	}
	// CLOSING IS THE LOOK. The stamp the next open measures news against is
	// written here and only here — see [homeView.seen] for why not on the way
	// in, and session's look.go for why a window that dies instead loses
	// nothing but a repeat of the same news.
	if a.home.open {
		session.NoteLook(a.placesRoot(), a.now())
	}
	// AN ERRAND DOES NOT DIE WITH THE SCREEN IT WAS ASKED ON, and that is the
	// repair this whole wave is about. It used to: closing home closed the
	// agent, so opening another conversation to check something ended the errand
	// mid-question and the engine answered the person's own card with "the card
	// was left unanswered — nothing was set up". The exchanges live on the app
	// ([app.exchanges]); this assignment takes away the SCREEN and nothing else,
	// and opening home again finds every one of them still going
	// (homeexchange.go's header).
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
	// THE BANDS ARE READ WITH THE WORLD AND NEVER SEPARATELY. An item's row and
	// the conversation rows above it are one triage order, and two readings taken
	// a beat apart would sort a firing item against a world that had not heard of
	// it yet.
	a.readStandBands()
	// [homeView.build] is the one that keeps the cursor on its conversation, so
	// this is a rescan and a rebuild and nothing else.
	a.home.build()
	a.touch()
}

// build turns the world into lines, applying the filter when one is typed.
func (h *homeView) build() {
	previous := h.focused()
	// AND THE ITEM UNDER THE CURSOR IS FOLLOWED THE SAME WAY. A band re-sorts
	// when something starts firing, exactly as the conversations above it do, and
	// a cursor that held its line number would land the person on a different
	// watch between two glances.
	previousItem := h.focusedItem()
	// AND SO IS THE PROJECT UNDER IT. A folded project line is a cursor stop like
	// any other, and the second tier re-sorts when something starts running in a
	// project nobody has touched for a week.
	previousProject := h.focusedProject()
	// AND SO IS THE ERRAND. An exchange row re-sorts the moment its own state
	// changes — a card arriving lifts it over everything else in the block — and
	// the pane is about the row under the cursor, so a cursor that held its line
	// number would take the exchange off the screen at the instant it asked a
	// question (homeexchange.go).
	previousExchange := h.focusedExchange()
	// An empty box is not a choice anybody has made yet, so the next character
	// typed starts on the action row again.
	if !h.searching() {
		h.picked = false
	}
	h.lines = h.lines[:0]
	// phone lane: at [tierPhone] the column is an inbox (homephone.go).
	h.buildFor()
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
			// AND THE ACTION ROW IS AT THE BOTTOM NOW, so resting on it is no
			// longer the same thing as resting at the top of the list ([homeAction]
			// says why it moved). It is found rather than counted to: how many rows
			// a query left above it is not a number this function knows.
			h.pointAction()
			return
		}
	}
	if previousExchange != nil {
		h.pointExchange(previousExchange)
		return
	}
	if previous.Transcript != "" {
		h.point(previous.Transcript)
		return
	}
	if previousItem != "" {
		h.pointItem(previousItem)
		return
	}
	if previousProject != "" {
		h.pointProject(previousProject)
	}
}

// focusedProject is the bucket directory of the folded project under the
// cursor, and "" when the cursor is not on one.
func (h *homeView) focusedProject() string {
	if h.cursor < 0 || h.cursor >= len(h.lines) || h.lines[h.cursor].kind != homeProject {
		return ""
	}
	return h.lines[h.cursor].dir
}

// pointProject puts the cursor on a project's own line, and leaves it where it
// is when that project is not on the list any more.
func (h *homeView) pointProject(dir string) {
	if dir == "" {
		return
	}
	for at, line := range h.lines {
		if line.kind == homeProject && line.dir == dir {
			h.cursor = at
			return
		}
	}
}

// focusedItem is the id of the standing item under the cursor, and "" when the
// cursor is not on one.
func (h *homeView) focusedItem() string {
	if h.cursor < 0 || h.cursor >= len(h.lines) || h.lines[h.cursor].kind != homeItem {
		return ""
	}
	return h.lines[h.cursor].item.ID
}

// pointItem puts the cursor on the row holding an item, and leaves it where it
// is when that item is not on the list any more.
func (h *homeView) pointItem(id string) {
	if id == "" {
		return
	}
	for at, line := range h.lines {
		if line.kind == homeItem && line.item.ID == id {
			h.cursor = at
			return
		}
	}
}

// pointAction puts the cursor on "start a new conversation", which is the last
// line of the list whenever there is one at all.
func (h *homeView) pointAction() {
	for at, line := range h.lines {
		if line.kind == homeAction {
			h.cursor = at
			return
		}
	}
}

// dropUp reports whether the list is drawn as a DROP-UP: its bottom row against
// the box at the foot, and the matches rising above it.
//
// It is exactly "something is typed", because that is exactly when the action
// row exists ([homeView.buildWorld]) and exactly when a person is looking at the
// box rather than reading down a roster. With nothing typed home is a dashboard
// somebody is reading, and it hangs from the top like every other list here.
func (h *homeView) dropUp() bool { return h.searching() }

// buildWorld is the column: projects as dim headings with their conversations
// under them, filtered and ranked by whatever is in the box.
//
// ── A DASHBOARD AT REST, A DROP-UP WHILE TYPING ─────────────────────────────
//
// THESE ARE TWO SHAPES ON PURPOSE, and the wave that made them one had to be
// taken back out. What it did was anchor the list at the foot in both states so
// the cursor never moved between them — and what that cost was the screen
// itself: a machine with a handful of conversations drew most of a frame of
// nothing with a clump of rows against the box, and the preview card beside it
// went blank the moment the cursor's row was not a conversation. Home IS the
// dashboard. The drop-up is what typing needs, and it is worth exactly one
// keystroke of re-anchoring and not one row of the dashboard.
//
// AT REST home hangs from the TOP of its region:
//
//   - Projects in the world's own order — most recently spoken in first
//     ([session.Project.At]) — each a heading with its conversations under it,
//     and inside a project what wants you first ([session.sortSessions]).
//   - The cursor opens on the conversation THIS WINDOW IS IN, and the preview
//     card on the right follows it.
//   - Nothing is lifted, so the frame reads top-down as a page of everything
//     this machine holds, which is the one thing this surface is for.
//
// WHILE SOMETHING IS TYPED it becomes a drop-up: the action row is appended as
// the list's last line, [homeLift] pushes the whole column down so that row
// lands against the box, and the matches rise above it ([homeAction] carries
// the defect that bought that). Clearing the box puts the dashboard back.
//
// AND IN THAT SHAPE THE RANKING IS DRAWN UPSIDE-DOWN, which is the one thing
// about the drop-up that is not simply the dashboard moved. A ranked list read
// downward puts its best answer first; a ranked list read UPWARD out of a box
// has to put its best answer LAST, or the row somebody wants is the furthest one
// from the key they reach for. It was the other way round and it cost real
// keystrokes: with three matches on screen, one ↑ landed on the WORST of them and
// the best took three. So the sections and the rows inside them are both turned
// over ([homeRank] is untouched — the scoring is right, only the drawing was
// backwards), and the law is:
//
//	THE FIRST MATCH THE WALK REACHES IS THE TOP-RANKED ONE.
//
// It is the SECOND ↑ and not the first, because `ask here` sits between the
// action row and the matches (homeexchange.go): the two rows that do something
// with the SENTENCE are one cluster against the box, and the rows that are other
// conversations begin above them. Further ↑ walks into progressively weaker ones
// and ↓ comes back toward the box, which is the same grammar the action row
// already had. A project's heading still sits ABOVE its own rows: sections stack
// by rank and the rows inside one do too, but a name drawn under the things it
// names reads upside-down.
//
// AND THE CONVERSATION THIS WINDOW IS IN MAY NOT BE ON THE LIST AT ALL. A
// session folder nobody has spoken in yet is not a row the world reports
// (session's readSessionRow drops one whose meta names it but records no
// message), and a launch that home GREETS is exactly that folder — so
// [homeView.point] finds nothing to point at and the cursor stays where
// [homeView.clamp] left it, on the first conversation of the first project.
// That is the honest place for it, and the thing that matters is that it is a
// CONVERSATION: the card beside it is drawn from the row under the cursor and
// draws nothing for a heading, a fold line or the action row, so a cursor
// resting anywhere but a conversation is a resting home with half its screen
// empty. That is precisely what shipped, and
// [TestAFreshLaunchStillRestsOnAConversationWithItsCard] is the pin that keeps
// it from shipping twice.
func (h *homeView) buildWorld() {
	query := h.query()
	var found []homeHit
	for _, project := range h.world.Projects {
		hit := homeHit{project: project}
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
		// A PROJECT HOLDING AN ERRAND IS ON THE SCREEN whether or not any of its
		// conversations survived the box. The exchange row has to be somewhere —
		// it is a live thing with a question in it — and its own project's block
		// is where it belongs ([homeView.placeExchanges] carries the fallback).
		if len(hit.rows) == 0 && !h.holdsExchange(project.Dir) {
			continue
		}
		hit.at = project.At()
		if query != "" {
			// Inside a project the best match sits CLOSEST TO THE BOX, which in a
			// drop-up means last (the block above [homeView.buildWorld] states the
			// law). With nothing typed the rows keep the world's own triage order,
			// which is what the screen is for when nobody is searching (session's
			// sortSessions).
			rows := hit.rows
			sort.SliceStable(rows, func(i, j int) bool {
				a, _ := homeRank(rows[i], project, query, h.world.Read)
				b, _ := homeRank(rows[j], project, query, h.world.Read)
				return a > b
			})
			// SORTED BEST-FIRST AND THEN TURNED OVER, rather than sorted worst-first
			// in one pass. The two are not the same list: a stable sort leaves rows
			// of EQUAL score in the world's own order, so sorting ascending would
			// put the LAST of a tie group nearest the box while turning the
			// best-first list over puts the FIRST of it there — which is the one
			// the world already judged hottest.
			for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
		found = append(found, hit)
	}
	if query != "" {
		// And the project holding the best row is the one against the box, so the
		// thing somebody is hunting is under their hand rather than four headings
		// up the screen. Same two steps and the same reason as the rows inside one.
		sort.SliceStable(found, func(i, j int) bool { return found[i].score > found[j].score })
		for i, j := 0, len(found)-1; i < j; i, j = i+1, j-1 {
			found[i], found[j] = found[j], found[i]
		}
		// A SEARCH HAS NO TIERS AT ALL. Every project that holds a match is drawn
		// open, wherever it lives, because a filter that folded away half of what
		// it found would be a filter lying about the machine — the same law the
		// quiet tail already keeps ([homeView.split]).
		//
		// AND THE ERRAND ROWS ARE DRAWN UNDER A QUERY TOO, unlike the standing
		// bands. A band is a description of something at rest that the query
		// never considered; an exchange is a conversation happening right now
		// with the person's own question in it, and a filter that hid one would
		// be this screen losing an errand because somebody typed three letters.
		h.placeExchanges(found)
		for _, hit := range found {
			h.blank()
			h.lines = append(h.lines, homeLine{
				kind: homeHeading, project: hit.project.Name, dir: hit.project.Dir,
			})
			h.projectBlock(hit, query)
		}
		// THE ACTION ROW CLOSES THE LIST, directly above the box the words were
		// typed into ([homeAction] says why it is not at the top any more). It is
		// separated from the matches by the same blank line that separates two
		// projects, because it is not one of them: everything above it exists, and
		// it is the one row that is a thing that does not.
		h.blank()
		// AND `ask here` SITS DIRECTLY ON TOP OF IT, with no blank between them,
		// because the two rows are one cluster: they are the two things enter can
		// do with the same characters, and a gap would read as two unrelated
		// offers. The cursor still RESTS on `start a new conversation` — typing and
		// pressing enter means today what it meant yesterday — and this row is the
		// one ↑ that asks the sentence instead of opening a conversation for it
		// (homeexchange.go).
		h.lines = append(h.lines, homeLine{kind: homeAskHere})
		h.lines = append(h.lines, homeLine{kind: homeAction})
		return
	}
	// A PROJECT WITH ITEMS AND NO CONVERSATIONS IS STILL A PROJECT. It has no
	// rows, so the loop above dropped it before it could have a heading — which
	// is how a workspace whose only content is a watch became invisible on the
	// one screen that exists to say what is true (homestanding.go's
	// [app.readBareBands] finds them). A search shows none of them, for the
	// reason the item bands themselves disappear under a query: the box searches
	// CONVERSATIONS, and a section with none is one the query never considered.
	for _, bare := range h.bare {
		if len(h.items[bare.project.Dir]) == 0 {
			continue
		}
		found = append(found, homeHit{project: bare.project, at: bare.at, bare: true})
	}
	// The projects in recency order, newest first. The world already handed
	// its projects over in that order, so a STABLE sort by the same key leaves
	// them exactly where they were and only settles where the bare ones belong
	// among them — which is what [homeTiers] reads as "the two most recent".
	sort.SliceStable(found, func(i, j int) bool { return found[i].at.After(found[j].at) })
	// AT REST, TWO TIERS ([homeOpenProjects] says why).
	open, folded := homeTiers(found, h.bucket)
	// THE ERRAND ROWS GO IN THE BLOCKS THAT ARE ACTUALLY DRAWN OPEN, which is
	// why this is asked here and not inside one of them: an exchange asked in a
	// project that ended up folded away under `elsewhere` still needs a row, and
	// only a caller holding the whole tier can say where it goes.
	h.placeExchanges(open)
	for _, hit := range open {
		h.blank()
		h.lines = append(h.lines, homeLine{
			kind: homeHeading, project: hit.project.Name, dir: hit.project.Dir,
			bare: hit.bare,
		})
		h.projectBlock(hit, "")
	}
	h.buildElsewhere(folded)
}

// homeHit is one project and the conversations of it that survived the box.
// score is the best rank any of those rows scored, and zero for every project
// while nothing is typed.
type homeHit struct {
	project session.Project
	rows    []session.SessionRow
	score   int
	// at is where this project sits in the recency order, which is the
	// project's own stamp for one with conversations in it and the newest
	// thing its items have done for one without ([standBareAt]).
	at time.Time
	// bare says this project has NO conversations at all and is on the screen
	// because something is keeping an eye on it (homestanding.go's
	// [app.readBareBands]).
	bare bool
}

// homeTiers splits the projects into the ones home draws OPEN and the ones it
// folds to a line each.
//
// THE WINDOW'S OWN PROJECT IS ALWAYS FIRST AND ALWAYS OPEN, whatever its
// recency says. It is the one project this window can actually open a
// conversation in (see this file's header), so a screen that pushed it under
// two projects somebody merely spoke in more recently would put the only
// actionable rows on it below the ones that refuse.
func homeTiers(found []homeHit, bucket string) (open, folded []homeHit) {
	rest := make([]homeHit, 0, len(found))
	for _, hit := range found {
		if len(open) == 0 && bucket != "" && filepath.Clean(hit.project.Dir) == bucket {
			open = append(open, hit)
			continue
		}
		rest = append(rest, hit)
	}
	// The world is already ordered by when somebody last spoke in a project
	// ([session.ReadWorld]), so "the two most recent others" is simply the next
	// two off the front.
	for _, hit := range rest {
		if len(open) < homeOpenProjects {
			open = append(open, hit)
			continue
		}
		folded = append(folded, hit)
	}
	return open, folded
}

// projectBlock is one project drawn OPEN: its standing band split around its
// conversations, and the quiet tail under them.
//
// It is one function because two tiers draw it. A tier-one project is this
// block under a dim heading; a folded project somebody opened is this block
// under its own `▾` line, which is what "opens in place" means — the same shape
// arriving where the one line was, rather than a different screen.
func (h *homeView) projectBlock(hit homeHit, query string) {
	// THE BAND SPLITS AROUND THE CONVERSATIONS, and the split is triage
	// (homestanding.go's header states it whole): an item that needs somebody
	// or is firing right now sits ABOVE the conversations, with the rows this
	// screen exists for; everything still waiting for its time sits under
	// them, above the quiet fold.
	//
	// AND A SEARCH DRAWS NO BAND AT ALL. The box searches conversations — by
	// name, by project, by what their tasks came to (see [homeRank]) — and a
	// band of items riding along under every hit would be rows the query
	// never considered, drawn as though it had.
	var hot, cold []StandingItemView
	var itemsFolded int
	if query == "" {
		shownItems, folded := standSplit(h.items[hit.project.Dir], h.itemsOpen[hit.project.Dir])
		itemsFolded = folded
		for _, view := range shownItems {
			if standHot(view) {
				hot = append(hot, view)
				continue
			}
			cold = append(cold, view)
		}
	}
	// AND THE ERRANDS SIT ABOVE ALL OF IT. An exchange is the hottest thing a
	// block can hold — it is a conversation this person started seconds ago and
	// it may be holding a question for them — so it takes the top of the block,
	// above the items that are firing and above the conversations
	// (homeexchange.go's [homeView.exchangeLines] carries the order inside).
	h.lines = append(h.lines, h.exchangeLines(hit.project)...)
	for _, view := range hot {
		h.lines = append(h.lines, h.itemLine(hit.project, view))
	}
	shown, quiet, since := h.split(hit.project, hit.rows, query)
	for _, row := range shown {
		h.lines = append(h.lines, homeLine{
			kind: homeSession, project: hit.project.Name, dir: hit.project.Dir, row: row,
		})
	}
	for _, view := range cold {
		h.lines = append(h.lines, h.itemLine(hit.project, view))
	}
	// A BAND WITH NOTHING BEHIND IT DRAWS NO DOOR, opened or not. An opened
	// band whose items have since dropped under the cap is a band that is
	// hiding nothing, and a fold control over nothing is a control that does
	// nothing (home.go's quiet tail follows the same rule for a search).
	if itemsFolded > 0 {
		h.lines = append(h.lines, homeLine{
			kind: homeItemFold, project: hit.project.Name, dir: hit.project.Dir,
			quiet: itemsFolded, folded: !h.itemsOpen[hit.project.Dir],
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

// buildElsewhere is the second tier: a dim rule, and then every other project
// on the machine as ONE LINE each.
//
// ORDER IS TRIAGE AND THEN RECENCY, which is the same law the conversations
// inside a project already follow: a project holding something that needs
// somebody, or something running, sorts above the quiet ones — otherwise a
// waiting conversation could sit three folds down under six projects nobody has
// touched, which is the exact failure this whole screen exists to prevent.
func (h *homeView) buildElsewhere(folded []homeHit) {
	if len(folded) == 0 {
		return
	}
	sort.SliceStable(folded, func(i, j int) bool {
		return h.projectHot(folded[i].project) && !h.projectHot(folded[j].project)
	})
	shown, hidden := folded, 0
	if !h.expanded[homeElsewhereKey] && len(folded) > homeFoldedProjects {
		shown, hidden = folded[:homeFoldedProjects], len(folded)-homeFoldedProjects
	}
	h.blank()
	h.lines = append(h.lines, homeLine{kind: homeElsewhereRule})
	opened := false
	for _, hit := range shown {
		open := h.expanded[homeProjectKey(hit.project.Dir)]
		// AN OPENED PROJECT GETS AIR ON BOTH SIDES, the same blank that separates
		// two projects in the tier above — a block wedged between two one-line
		// rows with nothing around it reads as part of them.
		if open || opened {
			h.blank()
		}
		h.lines = append(h.lines, homeLine{
			kind: homeProject, project: hit.project.Name, dir: hit.project.Dir,
			proj: hit.project, folded: !open,
		})
		if open {
			h.projectBlock(hit, "")
		}
		opened = open
	}
	// THE BLOCK FOLDS TOO, past [homeFoldedProjects]. The line stays while it is
	// open, because that line is the way back.
	if hidden > 0 || h.expanded[homeElsewhereKey] && len(folded) > homeFoldedProjects {
		if opened {
			h.blank()
		}
		h.lines = append(h.lines, homeLine{
			kind: homeMoreProjects, quiet: len(folded) - homeFoldedProjects,
			folded: !h.expanded[homeElsewhereKey],
		})
	}
}

// holdsExchange reports whether an errand was asked in one project's bucket.
func (h *homeView) holdsExchange(dir string) bool {
	for _, ex := range h.exchanges {
		if ex.bucket == dir {
			return true
		}
	}
	return false
}

// projectHot reports whether a project holds anything a person would want to be
// told about from behind a fold.
//
// IT COUNTS BOTH KINDS OF ROW, and it must: a project's standing items are
// exactly as capable of needing somebody as its conversations are
// (homestanding.go's [standTriage] ranks the two kinds on one ladder for the
// same reason), and a project sorted under the quiet ones while a watch of its
// own sits stopped on a question would be this screen hiding the row it exists
// for. It is the same pair of counts [homeProjectNote] then says out loud, so
// the order of the block and the words on its lines can never disagree.
func (h *homeView) projectHot(project session.Project) bool {
	waiting, running := h.projectCounts(project)
	return waiting > 0 || running > 0
}

// projectCounts is what a folded project has to say for itself: everything
// waiting on somebody and everything moving, over both kinds of row.
func (h *homeView) projectCounts(project session.Project) (waiting, running int) {
	waiting, running = project.NeedsPerson(), project.Running()
	itemsWaiting, itemsRunning := standCounts(h.items[project.Dir])
	return waiting + itemsWaiting, running + itemsRunning
}

// blank appends the one empty line that separates two sections, and never two
// of them in a row or one at the very top.
func (h *homeView) blank() {
	if len(h.lines) == 0 || h.lines[len(h.lines)-1].kind == homeBlank {
		return
	}
	h.lines = append(h.lines, homeLine{kind: homeBlank})
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

// previewLine is the line THE CARD IS ABOUT, which is not always the line the
// cursor is on: it is the row under the POINTER while the pointer is resting on
// one, and the cursor's row every other moment.
//
// THE POINTER PREVIEWS AND THE CURSOR SELECTS, and the two are allowed to
// disagree. Reading about a neighbouring conversation should cost nothing —
// moving the pointer down the column swaps the card without moving the
// selection, so the hand that was about to press enter is still aimed at the
// same chat when it gets there. The cursor keeps its selected look on the left
// while this happens and the hovered row keeps its hover look, which is the
// screen saying plainly that they are two different things.
//
// The pointer's row is only ever a line the cursor could stop on
// ([app.homeHover] refuses everything else), so a hover this finds always has a
// card; and the moment the pointer leaves the column the hover is dropped and
// the card is the cursor's again, with nothing to remember on either side.
func (h *homeView) previewLine() (homeLine, bool) {
	if h.hover >= 0 && h.hover < len(h.lines) && h.lines[h.hover].stop() {
		return h.lines[h.hover], true
	}
	return h.focusedLine()
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

// itemLine is one standing item as a line of the column.
func (h *homeView) itemLine(project session.Project, view StandingItemView) homeLine {
	return homeLine{
		kind: homeItem, project: project.Name, dir: project.Dir,
		view: view, item: view.Item,
	}
}

// stop reports whether the cursor may rest on this line. A heading names a
// project and a blank separates two, and neither is a thing to do anything to;
// everything else on the column answers enter.
func (l homeLine) stop() bool {
	switch l.kind {
	case homeSession, homeQuiet, homeAction, homeItem, homeItemFold, homeAskHere,
		homeProject, homeMoreProjects, homeExchangeRow:
		return true
	// phone lane: the inbox's own two stops (homephone.go).
	case homePhoneNews, homePhoneMore:
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
	// phone lane: a sheet over the inbox holds the keyboard (homesheet.go).
	if cmd, took := a.homeSheetKeyFirst(msg); took {
		return cmd
	}
	// AN OPEN ERRAND HOLDS THE KEYBOARD WHILE IT IS FOCUSED, and gives it back on
	// tab or esc with itself still standing in the right pane (homeexchange.go).
	// The list underneath is untouched by any of it: it keeps its cursor, its
	// query and its fold, and one key brings it all back under the hand.
	//
	// ── THE TWO ZONES ──────────────────────────────────────────────────────
	//
	// While an exchange exists this screen has a LIST and a PANE, and exactly
	// one of them has the keyboard. The rules are four and they are all here:
	//
	//	tab      toggles the two, from any state either of them is in
	//	esc      in the pane, hands the keyboard to the list (one layer at a
	//	         time: a half-typed follow-up clears first)
	//	a click  puts the keyboard where the pointer is — a row selects and
	//	         takes the list, a press in the pane takes the pane
	//	a yes    on the card hands it back to the list by itself, because the
	//	         thing that was asked for is now being made
	//
	// AND THE ARROWS ALWAYS MOVE THE ZONE THAT HAS THE KEYBOARD. With the list
	// focused ↑/↓, ctrl+p/ctrl+n and pgup/pgdown walk the column exactly as they
	// do with no exchange on screen, and THE PANE FOLLOWS THE CURSOR while they
	// do (home.go's [app.homeDetail]): the exchange's own row keeps its place in
	// the column with its state in the tail, and every other row gets its
	// ordinary card back. That is the trade this surface makes now, and it is
	// the one the report asked for — "once the reminder is set I am unable to
	// see other previews on the right".
	//
	// THE KEYBOARD IS SETTLED BEFORE THE KEY IS READ. An exchange holds it only
	// while the cursor is on that exchange's row, so walking away can never
	// leave the arrows moving a pane nobody is looking at (homeexchange.go's
	// [app.settleExchangeFocus]).
	a.settleExchangeFocus()
	// AND THE SWEEP RUNS AFTER THE KEY, not before it: what a key does is move
	// the cursor, and "have they moved off it" is a question only answerable
	// once they have (homeexchange.go's [app.sweepExchanges]).
	defer a.sweepExchanges()
	if ex := a.paneExchange(); ex != nil && ex.focused {
		defer a.touch()
		h.say("", "")
		return a.exchangeKey(ex, msg)
	}
	defer a.touch()
	h.say("", "")
	// MORE. The right column has no cursor, so `m` acts on the card: it opens
	// every folded band on the row under the cursor, and folds them again
	// (homebands.go). Only with nothing typed — in the box an m is an m.
	if msg.String() == "m" && h.box.empty() {
		if subject, ok := a.homeSubject(); ok {
			a.toggleAllBandFolds(subject)
			return nil
		}
	}
	// A DIGIT ANSWERS THE QUESTION UNDER THE CURSOR. It is read here, ahead of
	// everything, and taken only when the row is a conversation stopped on a
	// card that offered that key and there is nothing typed — every other
	// moment a digit is a character going into the box, exactly as it always
	// was ("2 hours later" begins with a 2). The chips it answers are drawn by
	// homeband_answer.go, which is also where the whole rule lives.
	if h.box.empty() {
		if cmd, took := a.answerKey(msg.String()); took {
			return cmd
		}
	}
	// THESE LETTERS ARE DOORS ONLY ON AN EMPTY SESSION CARD. Once somebody has
	// typed, every bare letter belongs to their sentence.
	if h.box.empty() {
		if line, ok := h.focusedLine(); ok && line.kind == homeSession {
			switch msg.String() {
			case "n":
				// `n` LEAVES THIS PROJECT TOO, on enter's own law: the row's own
				// folder is where the fresh conversation is built, so a row from
				// somewhere else starts one THERE and puts the conversation in
				// front into the keeper (keeper.go's [app.startBeside]). It used
				// to refuse with `elsewhere · <path>`, which is a sentence this
				// screen no longer has any business saying.
				if where := homeWhere(line); where != "" && where != a.workspace {
					if !homeFolderThere(where) {
						h.say(homeGoneWord+" · "+where, "")
						return nil
					}
					return a.homeStart(where)
				}
				return a.homeStart("")
			case "o":
				path := strings.TrimSpace(line.row.Workspace)
				if path == "" || processOpener(path) != nil {
					h.say("could not open "+path, "")
					return nil
				}
				h.say("opened "+path, path)
				return nil
			case "y":
				path := strings.TrimSpace(line.row.Workspace)
				if path == "" {
					h.say("could not copy path", "")
					return nil
				}
				h.say("copied "+path, path)
				return tea.Raw(osc52(path, a.tmux))
			}
		}
	}
	switch msg.String() {
	case "tab":
		// THE OTHER HALF OF THE TOGGLE, and it is about the row under the
		// cursor: tab takes the keyboard into the exchange the pane is drawing.
		// With the cursor anywhere else there is one zone and nothing to toggle,
		// so tab does what it has always done on this screen — nothing. It never
		// was a character the box could take.
		if ex := a.paneExchange(); ex != nil {
			ex.focused = true
		}
		return nil

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
		a.refreshHomeRepo(time.Now())
		return nil
	case "down", "ctrl+n":
		h.move(1)
		a.refreshHomeRepo(time.Now())
		return nil
	case "pgup":
		h.move(-homeShown)
		a.refreshHomeRepo(time.Now())
		return nil
	case "pgdown":
		h.move(homeShown)
		a.refreshHomeRepo(time.Now())
		return nil

	case "enter":
		return a.homeEnter()

	case "ctrl+enter", "alt+enter":
		// `ask here` WITHOUT LEAVING THE BOX. Two spellings because terminals
		// disagree about which one they can send — the same law input.go states
		// for alt+enter and ctrl+j — and ctrl+enter reaches this switch only on a
		// terminal that can distinguish it from a plain enter at all (the kitty
		// protocol, win32-input). alt+enter is the one that survives everywhere,
		// and the hint line names ctrl+enter because it is the one a hand
		// reaches for.
		return a.askHere(strings.TrimSpace(h.box.String()))

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
		if line, ok := h.focusedLine(); ok && line.kind == homeItemFold {
			h.foldItems(line.dir, true)
			return nil
		}
		// AND THE SAME TWO ARROWS OVER A WHOLE PROJECT, and over the folded block
		// itself. One gesture at three scales ([homeProject]).
		if line, ok := h.focusedLine(); ok && line.kind == homeProject && line.folded {
			h.foldProject(line.dir, true)
			return nil
		}
		if line, ok := h.focusedLine(); ok && line.kind == homeMoreProjects && line.folded {
			h.foldElsewhere(true)
			return nil
		}
		h.box.right()
		return nil
	case "left":
		if line, ok := h.focusedLine(); ok && (line.kind == homeQuiet || line.kind == homeSession) && h.expanded[line.dir] {
			h.fold(line.dir, false)
			return nil
		}
		if line, ok := h.focusedLine(); ok && (line.kind == homeItemFold || line.kind == homeItem) && h.itemsOpen[line.dir] {
			h.foldItems(line.dir, false)
			return nil
		}
		if line, ok := h.focusedLine(); ok && line.kind == homeProject && !line.folded {
			h.foldProject(line.dir, false)
			return nil
		}
		if line, ok := h.focusedLine(); ok && line.kind == homeMoreProjects && !line.folded {
			h.foldElsewhere(false)
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
		// PAUSE AND STOP ARE BARE LETTERS ON AN ITEM ROW, and they are read HERE
		// — inside the default branch, ahead of typing — because home's box is a
		// search AND a new conversation at the same moment. A letter is only a
		// key while there is nothing typed and the cursor is on an item; every
		// other moment it is a character, and it falls through to the box below
		// exactly as it always did (homestanding.go's [app.homeItemWrite] does
		// the write).
		if key := msg.String(); (key == "p" || key == "s") && !h.searching() {
			if line, ok := h.focusedLine(); ok && line.kind == homeItem {
				if key == "p" {
					return a.homeItemWrite(line, standing.StatusPaused)
				}
				return a.homeItemWrite(line, standing.StatusRetired)
			}
		}
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

// ONE MAP HOLDS EVERY FOLD A PERSON OPENED BY HAND, and these are the keys it
// is written under. They are keys and not three maps because they are one
// mechanism used at three scales — a project's quiet tail, a whole folded
// project, and the folded block itself — and folding is a thing a person did
// rather than a thing the data said, so all three outlive a rescan and a query
// together ([homeView.expanded]).
//
// A PROJECT'S QUIET TAIL IS KEYED BY THE BUCKET DIRECTORY ALONE, which is what
// it always was; the two below are prefixed so that opening a folded project
// cannot also open eleven quiet conversations inside it. The prefixes start
// with a NUL, which no directory path contains.
const homeElsewhereKey = "\x00elsewhere"

func homeProjectKey(dir string) string { return "\x00project\x00" + dir }

// setFold writes one of those keys. Opening records; folding forgets, so the
// map only ever holds what somebody actually opened.
func (h *homeView) setFold(key string, open bool) {
	if h.expanded == nil {
		h.expanded = map[string]bool{}
	}
	if open {
		h.expanded[key] = true
		return
	}
	delete(h.expanded, key)
}

// foldProject opens or folds ONE WHOLE PROJECT of the second tier, in place: the
// line it was becomes a block shaped like a tier-one project, with the same line
// at its head wearing `▾`. The cursor stays on that line, so the gesture can be
// reversed without moving.
func (h *homeView) foldProject(dir string, open bool) {
	h.setFold(homeProjectKey(dir), open)
	held := h.cursor
	h.rebuild()
	for at, line := range h.lines {
		if line.kind == homeProject && line.dir == dir {
			h.cursor = at
			h.picked = true
			return
		}
	}
	h.cursor = h.clamp(held)
}

// foldElsewhere opens or folds the tail of the folded block itself — the
// `▸ …4 more` line — and is [homeView.foldProject] one scale up.
func (h *homeView) foldElsewhere(open bool) {
	h.setFold(homeElsewhereKey, open)
	held := h.cursor
	h.rebuild()
	for at, line := range h.lines {
		if line.kind == homeMoreProjects {
			h.cursor = at
			h.picked = true
			return
		}
	}
	h.cursor = h.clamp(held)
}

// fold opens or closes one project's QUIET TAIL, and leaves the cursor on the
// line that did it so the gesture can be reversed without moving.
func (h *homeView) fold(dir string, open bool) {
	h.setFold(dir, open)
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

// foldItems opens or closes one project's standing band, and leaves the cursor
// on the line that did it so the gesture can be reversed without moving. It is
// [homeView.fold] over the other fold, and it is a second function rather than a
// parameter because the two folds are two maps: opening the watches must not
// open eleven quiet conversations.
func (h *homeView) foldItems(dir string, open bool) {
	if h.itemsOpen == nil {
		h.itemsOpen = map[string]bool{}
	}
	if open {
		h.itemsOpen[dir] = true
	} else {
		delete(h.itemsOpen, dir)
	}
	held := h.cursor
	h.rebuild()
	for at, line := range h.lines {
		if line.kind == homeItemFold && line.dir == dir {
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
	// phone lane: at [tierPhone] the column is an inbox (homephone.go).
	h.buildFor()
	h.cursor = h.clamp(h.cursor)
}

// buildFor is which SHAPE the column takes, and it is asked in the two places
// that fill it ([homeView.build] and [homeView.rebuild]) so the two can never
// disagree. The phone's inbox is homephone.go's; every wider frame is
// [homeView.buildWorld]'s, untouched.
func (h *homeView) buildFor() {
	if h.phone {
		h.buildPhone()
		return
	}
	h.buildWorld()
}

// homeEnter is the one decision this surface makes, and it makes a different
// one depending on what is in the box.
func (a *app) homeEnter() tea.Cmd {
	h := &a.home
	// phone lane: enter opens the row's card as a sheet (homesheet.go).
	if cmd, took := a.homePhoneEnter(); took {
		return cmd
	}
	line, ok := h.focusedLine()
	if !ok {
		return nil
	}
	switch line.kind {
	case homeAction:
		// The row the cursor rests on while something is typed, which is what
		// makes type-and-enter mean today what it meant yesterday.
		return a.homeStart(strings.TrimSpace(h.box.String()))
	case homeAskHere:
		// The same sentence, asked rather than opened (homeexchange.go).
		return a.askHere(strings.TrimSpace(h.box.String()))
	case homeExchangeRow:
		// ENTER ON AN ERRAND HANDS IT THE KEYBOARD. There is nothing to open —
		// the exchange is already drawn beside the row, or on a narrow frame is
		// about to take the whole screen — so the one thing enter can mean here
		// is "I am talking to this one now", which is what tab means from the
		// same row and what a second click on it means.
		if line.ex != nil {
			line.ex.focused = true
		}
		return nil
	case homeQuiet:
		h.fold(line.dir, line.folded)
		return nil
	case homeItemFold:
		h.foldItems(line.dir, line.folded)
		return nil
	case homeProject:
		// A WHOLE PROJECT, OPENED WHERE IT STANDS. enter is the same key it is on
		// every other fold on this column, and it is the only thing enter can mean
		// here: there is no one conversation a project line stands for.
		h.foldProject(line.dir, line.folded)
		return nil
	case homeMoreProjects:
		h.foldElsewhere(line.folded)
		return nil
	case homeItem:
		// THE DOOR AN ITEM OFFERS IS ITS PROVENANCE and not itself: "why did I
		// get this?" opens the conversation that asked for it
		// (homestanding.go's [app.homeItemEnter]).
		return a.homeItemEnter(line)
	}
	return a.homeOpenLine(line)
}

// homeOpenLine is [app.homeEnter]'s SESSION HALF, on its own so that the phone
// sheet's door reaches exactly the same checks in exactly the same order
// (homesheet.go's [app.homeOpenRow]). Two spellings of "open the row under the
// cursor" is two answers to whether a project somewhere else may be opened, and
// the phone tier had the older one.
func (a *app) homeOpenLine(line homeLine) tea.Cmd {
	h := &a.home
	// THE ORDER OF THESE CHECKS IS THE FEATURE. Identity comes first, because a
	// transcript THIS PROCESS holds answers [session.InUse] true about itself —
	// a flock rides the open file description rather than the process — so a
	// conversation one keystroke away would otherwise be refused as somebody
	// else's window (keeper.go's [app.holding] states the whole rule).
	switch {
	case a.holding(line.row.Transcript):
		// A conversation this terminal already has open: the one on screen, or
		// one running behind it. Either way enter goes to it rather than
		// opening anything — reopening would drop the lock, replay the journal
		// and land exactly where it started.
		//
		// IT SAYS NOTHING. The picker notes `already here` because it stays open
		// and owes an explanation for a keystroke that did nothing; home CLOSES,
		// and closing into the conversation somebody just confirmed is the thing
		// happening rather than the absence of one. A note here would be the
		// surface narrating a door it just walked through.
		cmd, _ := a.bringForward(line.row.Transcript)
		a.closeHome()
		return cmd
	case !a.canOpen():
		h.say(resumeUnavailableWord, "")
		return nil
	case a.homeHeldNow(line.row):
		// THE DOOR ANNOUNCES ITSELF LOCKED RATHER THAN SLAMMING. Home read the
		// same flock the open would take, seconds ago and again just now, so it
		// KNOWS. The resume picker reports this failure after the fact because
		// it genuinely cannot know beforehand; home can, and a screen that
		// offers a door it has already established goes nowhere is a screen that
		// wastes a keystroke and a second of somebody's attention on a raw error.
		h.say(sessionBusyWord, "")
		return nil
	}
	where := homeWhere(line)
	if !homeFolderThere(where) {
		// ONE os.Stat, ON THE KEYSTROKE, in the same place the flock probe puts
		// its one syscall. Home stats the transcript and never the workspace,
		// which cost nothing while enter could only open this project — you were
		// standing in the folder. It stops being free the moment enter opens
		// somebody else's: an agent whose tool root does not exist fails every
		// bash and every relative path in a way nothing on screen explains.
		h.say(homeGoneWord+" · "+where, "")
		return nil
	}
	if word, room := a.roomForAnother(); !room {
		h.say(word, "")
		return nil
	}
	// AND THE CONVERSATION THIS WINDOW WAS IN GOES ON RUNNING. It is detached
	// rather than closed and put in the keeper, which is the whole of what makes
	// home a switcher rather than a list of places to go to in another terminal.
	cmd, refusal := a.openBeside(where, line.row.Transcript)
	if refusal != "" {
		// HOME TAKES THE REFUSAL ITSELF rather than letting it be said in the
		// conversation. A refusal on this screen belongs to this screen: notes
		// stack in a transcript, and pressing enter twice on a locked row is
		// exactly how somebody would find that out.
		h.say(refusal, "")
		return nil
	}
	a.closeHome()
	return cmd
}

// homeFolderThere reports whether a project's directory is still on the disk.
// An unnamed one is not refused: a row with no recorded project directory is an
// older session shape, and the door resolves the workspace for it.
func homeFolderThere(where string) bool {
	if strings.TrimSpace(where) == "" {
		return true
	}
	resolved, err := filepath.EvalSymlinks(where)
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Clean(resolved))
	return err == nil && info.IsDir()
}

// homeStart is the door: a fresh conversation in this project, carrying the
// sentence that opened it.
//
// It is /new and then a submit, in that order and with nothing invented in
// between — [app.renew] swaps the agent synchronously and hands back the lanes
// the new conversation owes itself, so the submit below is talking to the new
// agent and not to the one that just closed.
func (a *app) homeStart(text string) tea.Cmd {
	if !a.canStart() {
		a.home.say(newUnavailableWord, "")
		return nil
	}
	// A PATH IS THE OTHER THING THIS ROW CAN MEAN. What was typed either names a
	// directory on this machine — an absolute path, a ~ path, or a project name
	// that matches exactly one heading on the list — or it is the first sentence
	// of a conversation in this project. The row says which before enter is
	// pressed ([homeView.startLabel]).
	if place := a.home.typedPlace(text); place != "" {
		if word, room := a.roomForAnother(); !room {
			a.home.say(word, "")
			return nil
		}
		cmd, refusal := a.startBeside(place)
		if refusal != "" {
			a.home.say(refusal, "")
			return nil
		}
		a.closeHome()
		return cmd
	}
	a.closeHome()
	renewed := a.renew()
	return tea.Batch(renewed, a.submit(text))
}

// typedPlace is the directory what was typed resolves to, or "" for anything
// that is a sentence rather than a place.
//
// THE PATH IS RESOLVED AND NEVER CREATED. A path that does not exist resolves to
// nothing and the row goes back to being the ordinary one — a surface that made
// a folder because somebody mistyped one would be the worst possible answer to a
// typo.
//
// A project NAME counts when exactly one heading on the list carries it. Two
// projects can share a base name, and opening whichever sorted first would be
// the screen guessing at the one thing a person was most specific about.
func (h *homeView) typedPlace(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if strings.HasPrefix(text, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			text = filepath.Join(home, strings.TrimPrefix(text, "~"))
		}
	}
	if filepath.IsAbs(text) || strings.HasPrefix(text, ".") {
		if homeFolderThere(text) {
			return filepath.Clean(text)
		}
		return ""
	}
	var found string
	for _, line := range h.lines {
		if line.kind != homeSession || !strings.EqualFold(line.project, text) {
			continue
		}
		where := homeWhere(line)
		if where == "" || where == found {
			continue
		}
		if found != "" {
			// Two projects, one name. See above.
			return ""
		}
		found = where
	}
	if found != "" && homeFolderThere(found) {
		return found
	}
	return ""
}

// startLabel is what the action row says enter will do, which on a screen where
// enter has two possible meanings must be legible without looking away from the
// list.
func (h *homeView) startLabel() string {
	text := strings.TrimSpace(h.box.String())
	if text == "" {
		return homeStartWord
	}
	if place := h.typedPlace(text); place != "" {
		return homeStartWord + " in " + place
	}
	return homeStartWord + ": " + strconv.Quote(text)
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
// A conversation THIS PROCESS is holding is never held against it — the one on
// screen or one open behind it. We are the ones holding it, and the way there is
// `enter` rather than another terminal (keeper.go's [app.holding]).
func (a *app) homeHeld(row session.SessionRow) bool {
	if row.Transcript == "" || a.holding(row.Transcript) {
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
//
// IDENTITY IS ASKED BEFORE THE LOCK IS, and this is the syscall that rule is
// about. [session.InUse] takes a flock on a fresh descriptor, and a flock rides
// the OPEN FILE DESCRIPTION rather than the process — so a transcript this
// process is already holding conflicts with its own lock and would be reported
// as somebody else's window. The keeper answers first.
func (a *app) homeHeldNow(row session.SessionRow) bool {
	if row.Transcript == "" || a.holding(row.Transcript) {
		return false
	}
	return session.InUse(row.Transcript) || a.homeHeld(row)
}

// homeWhere is the project a row belongs to, in the words a person would type:
// the project's own path, and its bucket name when nothing recorded one.
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
	return a.homeWorth && a.canOpen() && !a.hosted() && !a.home.open
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

// homePress is a click on this screen, and WHICH COLUMN IT LANDED IN is the
// first thing it answers.
//
// A press in the right pane while an exchange is up belongs to the exchange
// (homeexchange.go's [app.exchangePress]); everything else belongs to the left
// column, where the first click puts the cursor on a row and the second opens
// it. That two-step is the settings panel's and it is here for its reason — a
// single click that switched conversations would make a mis-aimed pointer close
// somebody's session.
//
// AND A CLICK ON A ROW SELECTS IT, WHICH MEANS TAKING THE KEYBOARD. It used to
// move the cursor and leave the hand in the pane, so the row lit up and then the
// arrows went on driving the exchange — a row that looks chosen and does not
// answer the next keystroke is the pointer and the keyboard disagreeing about
// where somebody is.
func (a *app) homePress(x, y int) tea.Cmd {
	if !a.home.open {
		return nil
	}
	// phone lane: the inbox and the sheet resolve their own presses, in one
	// gesture rather than two (homephone.go).
	if a.homePhone() {
		return a.homePhonePress(x, y)
	}
	// A CLICK MOVES THE CURSOR, so it is one of the two gestures that can leave
	// a settled exchange behind ([app.sweepExchanges] is the other half of
	// [app.homeKey]'s own deferred sweep).
	defer a.sweepExchanges()
	// A CHIP ON THE CARD IS PRESSED WHERE IT IS DRAWN. It is read before the
	// list below because the two answer different halves of the frame — the
	// chips are in the right column, which nothing else here claims — and a
	// press that fell through to the list would move somebody's cursor instead
	// of answering the question they aimed at (homeband_answer.go).
	if cmd, took := a.answerPress(x, y); took {
		a.touch()
		return cmd
	}
	width, height := a.size()
	lines, hits, _, _ := a.homeFrame(width, height)
	if y < 0 || y >= len(hits) {
		return nil
	}
	// A CLICK IN THE RIGHT PANE ACTS ON THE CARD, and the only thing on the card
	// a pointer can act on is a fold line. The column has no cursor of its own —
	// `m` opens every fold on the card at once (homebands.go) — so this is the
	// one gesture that opens ONE band, which is what a person means when they aim
	// at `▸ …5 more tasks` and press.
	//
	// The paint above recorded every fold line it drew this frame, so the line is
	// found by its own text rather than by counting rows: the card is assembled
	// band by band and drops whole bands on a short frame, and a row number
	// computed against it would be a second answer to where things ended up.
	if left, right := homeColumns(width); right > 0 && x >= left+homeGutter && y < len(lines) {
		if fold, ok := a.bandFoldAt(ansi.Strip(lines[y])); ok {
			a.toggleBandFold(fold.band, fold.subject)
			a.touch()
			return nil
		}
	}
	if row, column, ok := a.homePane(x, y); ok {
		return a.exchangePress(column, row)
	}
	at := hits[y]
	if at < 0 || at >= len(a.home.lines) || !a.home.lines[at].stop() {
		return nil
	}
	// THE KEYBOARD FOLLOWS THE POINTER ONTO THE COLUMN, whatever the row turns
	// out to be. It is done before the row is acted on so that a fold, an open
	// and a plain selection all leave the hand in the same place.
	a.homeTakeList()
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
	if a.home.lines[at].kind == homeItemFold {
		a.home.cursor = at
		a.home.picked = true
		a.home.foldItems(a.home.lines[at].dir, a.home.lines[at].folded)
		a.touch()
		return nil
	}
	if a.home.lines[at].kind == homeProject {
		a.home.cursor = at
		a.home.picked = true
		a.home.foldProject(a.home.lines[at].dir, a.home.lines[at].folded)
		a.touch()
		return nil
	}
	if a.home.lines[at].kind == homeMoreProjects {
		a.home.cursor = at
		a.home.picked = true
		a.home.foldElsewhere(a.home.lines[at].folded)
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

// homeTakeList hands the keyboard to the left column, leaving whatever is in
// the pane exactly as it was. It is what a click on a row does and what tab and
// esc do from the other side.
func (a *app) homeTakeList() {
	for _, ex := range a.exchanges {
		ex.focused = false
	}
}

// homePane resolves a pointer position against the RIGHT PANE while an exchange
// is drawn in it: which of the pane's own rows it landed on, and which column
// within the pane.
//
// It is one function because the press and the hover both ask it, exactly as
// they both index what [app.homeFrame] returned — a hover that measured the
// gutter differently from the press would light up a row that clicking does not
// reach.
func (a *app) homePane(x, y int) (row, column int, ok bool) {
	if a.paneExchange() == nil || y < 0 || y >= len(a.home.pane) {
		return 0, 0, false
	}
	if a.home.pane[y] < 0 {
		return 0, 0, false
	}
	width, _ := a.size()
	left, right := homeColumns(width)
	// STACKED, THE PANE IS THE WHOLE FRAME and every column of it belongs to the
	// pane — there is no list beside it to have missed ([app.homeStacked]).
	if _, stacked := a.homeStacked(); stacked {
		return a.home.pane[y], x, true
	}
	if right <= 0 {
		return 0, 0, false
	}
	// THE GUTTER BELONGS TO THE PANE. It is padding drawn on the pane's side of
	// the list ([app.homeBody]), and a press in it is a press that missed the
	// pane's first cell by two — which is a person aiming at the pane.
	if x < left {
		return 0, 0, false
	}
	return a.home.pane[y], x - left - homeGutter, true
}

// homeStacked is the narrow frame's answer to `ask here`, and it is the phone's
// own pattern rather than a refusal.
//
// THE EXCHANGE *IS* THE RIGHT PANE, so a window with no second column used to
// turn the whole door down: `ask here needs a wider window`, which is a person
// on a narrow terminal being told that the one feature they reached for is for
// other people. What a narrow frame does instead is STACK the two zones rather
// than sitting them side by side — the list is the screen until you enter an
// exchange, the exchange is the screen while it holds the keyboard, and `esc`
// or `tab` puts the list back with the row still on it wearing its tail. It is
// the same two zones and the same keys; only the geometry changed.
//
// IT IS THE SAME [homeExchange.focused] FLAG that decides it, which is what
// makes a resize between the two shapes cost nothing: a window dragged narrow
// while the pane has the keyboard keeps the pane, and dragged wide again puts
// it back beside the list with everything in it.
func (a *app) homeStacked() (*homeExchange, bool) {
	width, _ := a.size()
	if _, right := homeColumns(width); right > 0 {
		return nil, false
	}
	ex := a.paneExchange()
	if ex == nil || !ex.focused {
		return nil, false
	}
	return ex, true
}

// homeHover records which line the pointer is over, repainting only when the
// answer changed. It reads BOTH columns: the list's rows, and the one row in the
// pane a pointer can act on (homeexchange.go's [app.exchangeHover]).
func (a *app) homeHover(x, y int) {
	if !a.home.open {
		return
	}
	// phone lane: there is no hover on glass, so motion is dropped rather than
	// hit-tested per cell (homephone.go).
	if a.homePhone() {
		return
	}
	width, height := a.size()
	_, hits, _, _ := a.homeFrame(width, height)
	row, _, inPane := a.homePane(x, y)
	if !inPane {
		row = -1
	}
	a.exchangeHover(row)
	was := a.home.hover
	a.home.hover = -1
	// THE LIST'S HOVER BELONGS TO THE LIST'S COLUMN. The hover is what the card
	// previews ([homeView.previewLine]), so a pointer resting on the CARD must
	// not count as a hover on the list row that happens to share its screen line
	// — the card would then be about a row nobody is pointing at, and it would
	// change under the very pointer that came to read it. The gutter counts as
	// the pane's side, exactly as it does for a press ([app.homePane]).
	left, right := homeColumns(width)
	if !inPane && (right <= 0 || x < left) && y >= 0 && y < len(hits) {
		if at := hits[y]; at >= 0 && at < len(a.home.lines) && a.home.lines[at].stop() {
			a.home.hover = at
		}
	}
	if a.home.hover != was {
		// A HOVER THAT MOVED MOVED THE CARD, so the card's reading of the
		// repository is taken for the row now under the pointer — hovering a row
		// in another project shows THAT project's branch (homeband_repo.go). It
		// is behind the same cache and the same one-second bound the cursor's
		// own arrival is, and it is taken here rather than on every motion event
		// because this is the only branch where the answer changed.
		a.refreshHomeRepo(time.Now())
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
	// phone lane: under sixty columns this screen is an inbox and a sheet
	// (homephone.go). THE SHAPE IS SETTLED BEFORE THE FRAME IS DRAWN, so a
	// terminal dragged across the breakpoint — a phone being rotated — is rebuilt
	// here, with the cursor kept on whatever row it was on.
	if phone := layoutTier(width) == tierPhone; phone != a.home.phone {
		a.home.phone = phone
		a.home.build()
	}
	if a.home.phone {
		return a.homePhoneFrame(width, height)
	}
	pal := a.pal
	var lines []string
	var hits []int
	// panes is the pane's own hit map, one entry per screen row: which row of
	// the right column was drawn there, and -1 everywhere else. Only the body
	// ever fills it in.
	var panes []int
	add := func(text string, hit int) {
		lines = append(lines, text)
		hits = append(hits, hit)
		panes = append(panes, -1)
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

	// THE LIST NEVER TOUCHES THE RULE ABOVE THE BOX. One blank row always sits
	// between the last line of the body and the foot, and the region gives it up
	// rather than the foot: a column of rows butted straight against a rule reads
	// as one block with a lid on it, and the last conversation on the screen —
	// which in a drop-up is the row somebody is about to press enter on — is the
	// one that suffers for it.
	const foot = 3
	const pad = 1
	room := height - len(lines) - foot - pad
	if room < 1 {
		room = 1
	}

	left, right := homeColumns(width)
	a.home.top = listTop(a.home.cursor, a.home.top, len(a.home.lines), room)
	body := a.homeBody(left, right, room, pal)
	for _, drawn := range body {
		add(drawn.text, drawn.hit)
		panes[len(panes)-1] = drawn.pane
	}
	add("", -1)

	add(pal.dim(rule(width)), -1)
	caretX, caretY := 0, 0
	if ex := a.paneExchange(); ex != nil && ex.focused {
		// THE FOOT BELONGS TO WHOEVER HOLDS THE KEYBOARD. A follow-up typed into
		// home's own box would re-filter the list behind the pane, so the exchange
		// brings its own line and the caret sits in it (homeexchange.go).
		text := ex.box.String()
		add(" "+pal.accent("› ")+pal.ink(fit(text, width-4)), -1)
		caretX, caretY = 3+ansi.StringWidth(text), len(lines)-1
		if caretX > width-1 {
			caretX = width - 1
		}
	} else if a.home.box.empty() {
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
		// AND THE PLACE IT SENDS YOU IS A DOOR. The refusal's whole job is to
		// name where that conversation lives, so the sentence that names it opens
		// it — the link is applied to the FITTED text, after the width was
		// measured, and a directory that is not there stays plain (pathlink.go).
		add(" "+pal.dim(a.pathLink(a.home.msgPath, fit(a.home.msg, width-2))), -1)
	} else {
		add(" "+pal.dim(fit(a.homeHint(), width-2)), -1)
	}

	// A frame too short for the whole thing keeps its head and its last rows:
	// the same clamp the settings panel takes, so a tiny terminal shows a
	// truncated screen rather than a screen scrolled off the top.
	if len(lines) > height {
		keep := lines[:1]
		keepHits := hits[:1]
		keepPanes := panes[:1]
		lines = append(keep, lines[len(lines)-(height-1):]...)
		hits = append(keepHits, hits[len(hits)-(height-1):]...)
		panes = append(keepPanes, panes[len(panes)-(height-1):]...)
	}
	for len(lines) < height {
		add("", -1)
	}
	// THE HIT MAP IS KEPT WHERE THE POINTER CAN FIND IT, and it is written by
	// the draw for the reason [standingCard.choiceRow] is: the press and the
	// hover resolve against what this frame actually drew, so a stale map is a
	// click answering for a row that has moved.
	a.home.pane = panes
	return lines, hits, caretX, caretY
}

// homeDrawn is one screen line, the column line it belongs to, and — while an
// exchange is drawn beside it — the pane row that shares it.
type homeDrawn struct {
	text string
	hit  int
	// pane is which row of the right column landed on this screen line, or -1.
	pane int
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
	// STACKED: the exchange takes the frame and the list stands down behind it
	// ([app.homeStacked] says why a narrow window no longer refuses). The rows
	// carry no list hit at all — there is no list on the screen to press — and
	// every one of them is the pane's, so the pointer resolves against it
	// exactly as it does on a wide frame.
	if ex, stacked := a.homeStacked(); stacked {
		drawn := make([]homeDrawn, 0, room)
		pane := a.exchangePane(ex, left, room, pal)
		for i := 0; i < room; i++ {
			text := ""
			if i < len(pane) {
				text = pane[i]
			}
			drawn = append(drawn, homeDrawn{text: text, hit: -1, pane: i})
		}
		return drawn
	}
	// THE DROP-UP LIFTS THE LIST AND LEAVES THE CARD WHERE IT IS. While something
	// is typed the left column hangs from the BOTTOM of the region so that its
	// last row — the action row — lands against the box at the foot
	// ([homeAction]). At rest it hangs from the top, because at rest this is a
	// page somebody is reading rather than a thing they are typing at (the block
	// above [homeView.buildWorld]).
	//
	// THE LIFT IS MEASURED FROM WHAT WAS DRAWN and not from how many lines the
	// column holds, which is what keeps a machine with no conversations on it
	// honest: that case draws ONE line out of a list of NONE, and a lift counted
	// off the list would push the only sentence on the screen off the bottom of
	// it.
	//
	// THE DETAIL COLUMN IS NOT LIFTED WITH IT, and that is deliberate rather than
	// an oversight. It is a CARD about the row under the cursor, assembled to fill
	// the height it is given and dropping whole bands from the bottom when it
	// cannot ([homeBands]) — so lifting it would not move it down the screen, it
	// would take the outcome, the last line said and the arithmetic off the card
	// entirely and leave a title floating in the middle of the frame. The list is
	// the thing typing is about; the card beside it reads top down, as a card does.
	column := a.homeList(left, room, pal)
	lift := a.homeLift(len(column), room)
	var detail []string
	if right > 0 {
		detail = a.homeDetail(right, room, pal)
	}
	drawn := make([]homeDrawn, 0, room)
	for i := 0; i < room; i++ {
		text, hit, pane := "", -1, -1
		if at := i - lift; at >= 0 && at < len(column) {
			text, hit = column[at].text, column[at].hit
		}
		if right > 0 && i < len(detail) {
			// THE PANE'S ROWS ARE THE BODY'S ROWS, ONE FOR ONE. The detail
			// column is not lifted with the list (see above), so the pane's own
			// row index IS the body row it landed on — which is what makes a
			// press resolvable without the pane knowing anything about the frame
			// it is drawn in.
			pane = i
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
		drawn = append(drawn, homeDrawn{text: text, hit: hit, pane: pane})
	}
	return drawn
}

// homeLift is how many blank rows sit ABOVE the body, which is what makes the
// list a drop-up: the shorter the list, the further down the region it starts,
// so its last row always lands against the foot.
//
// It is zero for a list nobody is typing at ([homeView.dropUp]) — the resting
// dashboard hangs from the top — and zero again for a list longer than the
// region, where the window is already full and [listTop] has bottom-anchored it
// by following a cursor that starts on the last row.
func (a *app) homeLift(drawn, room int) int {
	if !a.home.dropUp() {
		return 0
	}
	if lift := room - drawn; lift > 0 {
		return lift
	}
	return 0
}

// homeList is the left column: the window of lines the cursor is inside.
func (a *app) homeList(width, room int, pal palette) []homeDrawn {
	h := &a.home
	if len(h.lines) == 0 {
		word := homeEmptyWord
		if h.searching() {
			word = homeNoMatchWord
		}
		return []homeDrawn{{text: "  " + pal.dim(fit(word, width-2)), hit: -1, pane: -1}}
	}
	drawn := make([]homeDrawn, 0, room)
	for at := h.top; at < len(h.lines) && len(drawn) < room; at++ {
		drawn = append(drawn, homeDrawn{text: a.homeLine(h.lines[at], at, width, pal), hit: at, pane: -1})
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
		// THE HEADING IS THE PROJECT'S NAME AND NOTHING ELSE. It used to carry a
		// dim `elsewhere` on every project but this window's own, which was the
		// screen spending a column saying what it could not do; enter opens any
		// of them now, so there is nothing to mark. The word survives one floor
		// down, on the rule over the folded block, where it is about the SHAPE of
		// the list and not about a door ([homeElsewhereRuleWord]).
		return "  " + pal.dim(fit(line.project, width-2))
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
	case homeElsewhereRule:
		// THE ONE RULE ON THIS SCREEN, and it is drawn because the thing under it
		// is not another project — it is the REST of them. A blank line was
		// tried and could not say that: the folded lines simply read as a fourth
		// project with very short rows. The word rides the rule rather than
		// sitting on a heading of its own, so the section costs one row.
		return pal.dim(fit(homeElsewhereRuleLine(width-2, pal.ascii), width))
	case homeProject:
		// THE SAME FOLD MARK AS EVERYTHING ELSE THAT HIDES ROWS, at the scale of
		// a whole project: `▸` while it is one line, `▾` once it is a block.
		return overlayRowTinted(homeFoldMark(line.folded, pal)+" "+line.project,
			h.projectNote(line.proj, h.world.Read, pal.ascii), h.projectInk(line.proj),
			at == h.cursor, markNone, at == h.hover, width, pal)
	case homeMoreProjects:
		return overlayRow(homeFoldMark(line.folded, pal)+" "+homeMoreProjectsWord(line), "",
			at == h.cursor, false, at == h.hover, width, pal)
	case homeItem:
		// ONE ITEM, ONE ROW, drawn by the renderer home's errand box shares
		// (homestanding.go's [StandingItemRow]).
		return StandingItemRow(a, line.view, width, h.world.Read, at == h.cursor, at == h.hover)
	case homeItemFold:
		// THE SAME FOLD MARK AS THE QUIET TAIL, over the same kind of thing: a
		// line standing for rows you cannot see, and an arrow saying which way it
		// goes.
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
		return overlayRow(mark+" "+standFoldWord(line.quiet, line.folded), "",
			at == h.cursor, false, at == h.hover, width, pal)
	case homeExchangeRow:
		// ONE ERRAND, ONE ROW, wearing what it is doing (homeexchange.go's
		// [app.exchangeRowLine]).
		return a.exchangeRowLine(line, at, width, pal)
	case homeAskHere:
		// The same shape as the action row under it and the same words quoted
		// back, because they are the two readings of one sentence
		// (homeexchange.go).
		label := homeAskHereWord
		if text := strings.TrimSpace(h.box.String()); text != "" {
			label += ": " + strconv.Quote(text)
		}
		return overlayRow(homeAskHereGlyph+" "+label, "", at == h.cursor, false, at == h.hover, width, pal)
	case homeAction:
		// It carries the words back at the person, cut to fit. The box at the
		// foot holds them too, but the box is where you are typing and this is
		// what enter will DO with it — and on a screen where enter has two
		// possible meanings, the one it currently has must be legible without
		// looking away from the list.
		return overlayRow(homeStartGlyph+" "+h.startLabel(), "", at == h.cursor, false, at == h.hover, width, pal)
	}
	// OUR OWN ROWS ARE READ FROM THE AGENT AND NOT FROM THE PRESENCE FILE. The
	// file is written on a five-second heartbeat and believed for fifteen, which
	// is right for another window and wrong for an agent whose pointer is in
	// this process's own map: a person who switches away from a question and
	// opens home would watch their own row say the wrong thing for five seconds
	// ([app.homeTrue]).
	row := a.homeTrue(line.row)
	label := a.homeRowGlyph(row) + " " + homeName(row)
	note := homeNote(row, a.homeHeld(row), a.homeMark(row) == markOurs, a.homeFresh(row), h.world.Read)
	// THE LEFT COLUMN IS AN INDEX AND STAYS CALM. Every row is dim except the
	// one the cursor is on, which takes the band and the ink — the same
	// treatment the detail column's title takes across the gutter, so the two
	// read as one thing rather than as two lists.
	//
	// The one exception is a row that wants somebody. `waiting on you` is
	// brought up out of the dim, because a screen whose whole job is triage
	// cannot render its most urgent fact in the same grey as an age.
	return overlayRowTinted(label, note, homeNoteInk(row, a.homeHeld(row)),
		at == h.cursor, a.homeMark(row), at == h.hover, width, pal)
}

// homeMark is which of the three kinds of row this is: the conversation on
// screen, one this terminal is holding behind it, or somebody else's.
func (a *app) homeMark(row session.SessionRow) rowMark {
	switch {
	case row.Transcript == "":
		return markNone
	case convKey(row.Transcript) == convKey(a.file):
		return markFront
	case a.behind[convKey(row.Transcript)] != nil:
		return markOurs
	}
	return markNone
}

// homeTrue is a row with the facts THIS PROCESS knows better than the disk does
// put back on it.
//
// It is the presence file's two claims — is this conversation waiting on
// somebody, and how much work has it out — asked of the agent instead, for a row
// we are holding. One predicate, three readers: this, the status line's count
// and the desktop banner all go through [session.Agent.NeedsPerson], so they
// cannot disagree.
//
// Every other row is returned untouched, because the file is the only thing that
// knows about another terminal.
func (a *app) homeTrue(row session.SessionRow) session.SessionRow {
	held := a.behind[convKey(row.Transcript)]
	if held == nil || held.conv.Agent == nil {
		return row
	}
	row.Live, row.Open = true, true
	row.Presence.State = session.PresenceIdle
	running := 0
	if door, ok := held.conv.Agent.(interface {
		TaskIndex() []session.TaskIndexEntry
	}); ok {
		for _, entry := range door.TaskIndex() {
			if entry.Status == string(session.TaskRunning) {
				running++
			}
		}
	}
	if running > 0 {
		row.Presence.State = session.PresenceWorking
	}
	if needsPerson(held.conv.Agent) {
		row.Presence.State = session.PresenceWaiting
	}
	row.Tasks.Running = running
	return row
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

// homeFoldMark is the arrow a line that hides rows wears — the task column's own
// two marks (task.go's [glyphShut] and [glyphOpen]), because it is the same
// gesture over the same kind of thing at every scale this screen folds at.
func homeFoldMark(folded bool, pal palette) string {
	if pal.ascii {
		if folded {
			return ">"
		}
		return glyphOpenASCII
	}
	if folded {
		return glyphShut
	}
	return glyphOpen
}

// homeElsewhereRuleLine is `─ elsewhere ─────`, run out to the width it is
// given. The dashes after the word are what make it a rule rather than a
// heading — the eye reads the line as a lid over everything below it.
func homeElsewhereRuleLine(width int, ascii bool) string {
	word, dash := homeElsewhereRuleWord, "─"
	if ascii {
		word, dash = homeElsewhereRuleASCII, "-"
	}
	line := "  " + word
	if pad := width - ansi.StringWidth(word); pad > 0 {
		line += strings.Repeat(dash, pad)
	}
	return line
}

// homeProjectNote is a folded project's dim tail: how many conversations it
// holds, and then the ONE thing worth knowing about them from out here.
//
// A FOLD MUST NOT HIDE THE ROW THIS SCREEN EXISTS FOR. A project with a
// conversation stopped on a question says so on its one line — `▲ 1 waiting` —
// and so does one with work running, and both sort above the quiet projects
// ([homeView.projectHot]). Everything else says how long since anybody spoke in
// it, which is the only fact a quiet project has.
//
// THE COUNTS INCLUDE THE PROJECT'S STANDING ITEMS. A watch stopped on a
// question needs a person exactly as a conversation does, and one firing right
// now is work in flight; the count says how many things want you, not how many
// chats do. The leading number stays the conversations, because that is what
// opening the line shows you.
func (h *homeView) projectNote(project session.Project, now time.Time, ascii bool) string {
	parts := []string{itoa(len(project.Sessions))}
	waiting, running := h.projectCounts(project)
	switch {
	case waiting > 0:
		glyph := homeAskGlyph
		if ascii {
			glyph = homeAskASCII
		}
		parts = append(parts, glyph+" "+itoa(waiting)+" waiting")
	case running > 0:
		glyph := homeLiveGlyph
		if ascii {
			glyph = homeLiveASCII
		}
		parts = append(parts, glyph+" "+itoa(running)+" running")
	default:
		if age := sinceAt(project.At(), now); age != "" {
			parts = append(parts, age)
		}
	}
	return strings.Join(parts, " · ")
}

// projectInk brings a folded project that is waiting on somebody up out of the
// dim, exactly as [homeNoteInk] does for one conversation. Everything else
// keeps the ordinary rule.
//
// It asks the same count the note draws, so the line that SAYS `▲ 1 waiting`
// is the line that is brought up: an item waiting is a person waiting.
func (h *homeView) projectInk(project session.Project) noteInk {
	if waiting, _ := h.projectCounts(project); waiting == 0 {
		return nil
	}
	return func(pal palette, note string, selected bool) string {
		if selected {
			return pal.ink(note)
		}
		return pal.accent(note)
	}
}

// homeMoreProjectsWord is the folded block's own tail line.
func homeMoreProjectsWord(line homeLine) string {
	if !line.folded {
		return "…" + itoa(line.quiet) + " fewer"
	}
	return "…" + itoa(line.quiet) + " more"
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
func homeNote(row session.SessionRow, held, ours bool, fresh int, now time.Time) string {
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
	case ours:
		// A CONVERSATION THIS TERMINAL IS HOLDING. It goes where `another window`
		// goes and never instead of it — the two are different facts about
		// different doors, and this one's door is `enter` (keeper.go).
		parts = append(parts, homeOpenWord)
	case fresh > 0:
		// THE NEWS OUTRANKS THE TALLY AND NOTHING ELSE. `2 landed` is the count
		// of tasks that finished since home was last closed ([homeView.seen]);
		// a row that is asking, running, half-done or locked keeps those words,
		// because each of them is about NOW and this one is about since.
		parts = append(parts, itoa(fresh)+" "+homeLandedWord)
	case row.Tasks.Total() > 0:
		parts = append(parts, itoa(row.Tasks.Total())+plural(" task", row.Tasks.Total()))
	}
	if age := sinceAt(row.At, now); age != "" {
		parts = append(parts, age)
	}
	return strings.Join(parts, " · ")
}

// homeRowGlyph is [homeGlyph] with the two facts only the app can add: the
// frame count that turns a running row's spinner, and the look stamp that earns
// a resting row the tick (the glyph block above says why each exists).
func (a *app) homeRowGlyph(row session.SessionRow) string {
	if row.NeedsPerson() {
		return homeGlyph(row, a.pal.ascii)
	}
	if row.Tasks.Running > 0 && !a.linear {
		return tokens.Spinner(a.paints / spinnerStep)
	}
	if row.Tasks.Running == 0 && row.Tasks.Incomplete == 0 && a.homeFresh(row) > 0 {
		if a.pal.ascii {
			return glyphDoneASCII
		}
		return glyphDone
	}
	return homeGlyph(row, a.pal.ascii)
}

// homeFresh is how many of a row's tasks landed since home was last closed, and
// zero whenever that question has no honest answer: no stamp yet (a first look
// has no origin), or the conversation this window is sitting in, whose landings
// were watched happening rather than missed.
func (a *app) homeFresh(row session.SessionRow) int {
	if a.home.seen.IsZero() || row.Transcript == a.file {
		return 0
	}
	count := 0
	for _, entry := range row.Tasks.Rows {
		if a.homeEntryFresh(row, entry) {
			count++
		}
	}
	return count
}

// homeEntryFresh is the same question of one task: landed, and landed after the
// stamp. A live row is never fresh — it has not landed at all — and a landed
// row with no end stamp compares as never-after, which is the emptiness law
// applied to a time.
func (a *app) homeEntryFresh(row session.SessionRow, entry session.TaskIndexEntry) bool {
	if a.home.seen.IsZero() || row.Transcript == a.file {
		return false
	}
	return !entry.Live() && entry.EndedAt.After(a.home.seen)
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
//   - BANDS SEPARATED BY BLANK LINES. Whitespace where a lesser surface would
//     put rules.
//   - A FLOOR ON WHAT SURVIVES. A short frame drops bands FROM THE BOTTOM, so
//     the facts go first and the title never goes at all. A pane that truncated
//     its own title would be a preview that cannot say what it is previewing.
//
// AND THE CARD FOLLOWS THE POINTER WHEN THERE IS ONE ([homeView.previewLine]).
// Hovering a row on the left previews that row here, without moving the cursor;
// a pointer that leaves the column, or rests on a heading, gives the card back
// to the cursor's row. Everything below is true of whichever row that is.
//
// THERE ARE ALWAYS TWO PANES, and only the LEFT one changes shape with the state
// of the box. The card follows the previewed row through every keystroke of a
// filter exactly as it does at rest — a person walking ↑ through matches is
// choosing between conversations, and choosing between them by name alone is what
// the card exists to stop. It is the LIST that becomes a drop-up while typing
// ([homeLift]); this stays where it is and keeps answering.
func (a *app) homeDetail(width, room int, pal palette) []string {
	a.resetBandFoldLines()
	line, ok := a.home.previewLine()
	if ok && line.kind == homeExchangeRow && line.ex != nil {
		// THE ERRAND UNDER THE CURSOR, drawn where every other row's card is
		// drawn. It used to take this column for as long as an exchange existed
		// anywhere, which is how setting one reminder blanked every preview on
		// the screen until home was closed (homeexchange.go's [app.exchangePane]).
		return a.exchangePane(line.ex, width, room, pal)
	}
	if ok && line.kind == homeItem {
		// THE OTHER KIND OF CARD, in the same column and the same bands
		// (homestanding.go's [StandingItemCard]). It is a card about an item
		// rather than about a conversation, and it is assembled by the same
		// [homeBands] so a short frame drops from the bottom on both.
		return StandingItemCard(a, line.view, line.project, strings.TrimSpace(line.item.Workspace), width, room, a.home.world.Read)
	}
	if ok && line.kind == homeProject {
		// THE CARD FOR A WHOLE PROJECT. This function still owns only the two
		// lines nothing may displace — what it is called, and where it is — and
		// everything under them is the registry's ([homebands.go]). No band draws
		// for [bandKindProject] yet, so today the pane is those two lines; the
		// day one is registered it appears here without this function changing.
		subject := bandSubject{
			kind: bandKindProject, project: line.project,
			dir: homeProjectPath(line.proj), world: a.home.world,
		}
		bands := [][]string{{pal.bold(pal.ink(fit(line.project, width)))}}
		if place := subject.dir; place != "" {
			bands = append(bands, []string{pal.dim(a.pathLink(place, fitLeft(place, width)))})
		}
		bands = append(bands, a.drawHomeBands(bandContext{
			subject: subject, width: width, now: a.home.world.Read, pal: pal,
		})...)
		return homeBands(bands, room)
	}
	if !ok || line.kind != homeSession {
		// The action row and a folded tail are not things with a detail; the
		// column stays empty rather than keeping the last conversation's up,
		// which would be the pane answering for a row nobody is on.
		//
		// THE ACTION ROW IS THE EMPTINESS LAW AT ITS PLAINEST. "start a new
		// conversation" is a chat that DOES NOT EXIST YET, so there is nothing
		// true to preview about it — and a card left standing from the last match
		// somebody walked past would be the pane describing a row the cursor is
		// not on any more.
		return nil
	}
	row := line.row

	// The bands, in order, each already painted. The first is the title and is
	// never dropped; the rest go from the bottom up as the frame shortens.
	bands := [][]string{{pal.bold(pal.ink(fit(homeName(row), width)))}}

	place := line.project
	dir := strings.TrimSpace(row.ProjectDir)
	if dir != "" && dir != place {
		place += " · " + dir
	}
	// THE BAND IS A PLACE, SO THE BAND IS A DOOR (pathlink.go). The anchor covers
	// the whole of it rather than the path half, because the project word and the
	// path are two spellings of one directory and a link that stopped at the
	// second would be a target a narrow right column had already cut off. A
	// directory that is not on this disk is drawn plain, as it always was.
	bands = append(bands, []string{pal.dim(a.pathLink(dir, fitLeft(place, width)))})

	// EVERYTHING UNDER THE PLACE LINE IS A BAND FROM THE REGISTRY (homebands.go):
	// each band is its own file, says what it is about, and is drawn in the
	// order its key gives it. This function owns only the title and the place,
	// which are the two lines no band may displace.
	bands = append(bands, a.drawHomeBands(bandContext{
		subject: bandSubject{kind: bandKindSession, row: row, project: line.project, dir: dir},
		width:   width,
		now:     a.home.world.Read,
		pal:     pal,
	})...)
	return homeBands(bands, room)
}

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

// homeFacts is the dim arithmetic under the card: the weight of what this
// conversation left behind, and when it was last touched.
//
// EVERY FACT IS OMITTED WHEN IT IS NOT ONE. The emptiness law is at its most
// literal on a line like this — a footer reading "$0.00 · 0 tok · last active"
// is three absences dressed as three facts — so each part appears only when
// there is something to say, and a footer with nothing to say is not drawn.
//
// THE SUM IS THE TALKING PLUS THE WORK IT COMMISSIONED, and it is added up
// here because it is written down in two places for two good reasons. The
// conversation's own turns are stamped on its meta.json by the session that
// held them ([session.SessionRow.Spend]); every task it started is a row of the
// project's index with its own bill ([session.TaskRollup.Spend]). A person
// looking at a card does not have that distinction in their head — they asked
// what this conversation cost — so the card answers with one figure, and the
// two halves stay separate everywhere they are recorded.
//
// AND THE FILES ARE HERE TOO, because nothing else on the card carries them and
// it is the most physical number the index holds: tokens are what the work
// cost, files are what it DID.
func homeFacts(row session.SessionRow, now time.Time) string {
	var parts []string
	if files := homeFilesTouched(row); files > 0 {
		parts = append(parts, "touched "+itoa(files)+plural(" file", files))
	}
	if spend := row.Spend + row.Tasks.Spend; spend > 0 {
		parts = append(parts, "spent "+dollars(spend))
	}
	if tokens := row.Tokens + row.Tasks.Tokens; tokens > 0 {
		parts = append(parts, tokenWord(tokens)+" tokens")
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
//
// THE WORDS THEMSELVES ARE NOT THIS FILE'S. They are [taskStateWord]
// (taskview.go), which the task page's own record rows and the record card both
// answer through — one vocabulary, so a task called `needs your look` on this
// screen is not called something else on the next one. What belongs to home is
// the LIVENESS QUESTION: this screen judges a row against the conversation that
// wrote it, and the task page judges it against the windows that are open.
func homeTaskWord(entry session.TaskIndexEntry, row session.SessionRow) string {
	return taskStateWord(entry, row.Runs(entry))
}

// homeTaskGlyph is a task's state in one painted cell, and it is the rail's own
// vocabulary ([app.taskStateMark], task.go) asked home's liveness question: a
// row spins only when [session.SessionRow.Runs] vouches that the session still
// has the node out, exactly as the counts and the old state words did. So:
//
//	⠋ (accent)  running this instant — the spinner, home's one moving part
//	◌ (dim)     queued, or left mid-way by a window that went — nothing turns
//	✗ (bad)     failed;  ? (warn)  finished and needs your look
//	✓ (muted)   landed — and ACCENT when it landed since you last looked
//
// The one departure from the rail: a queued node and an incomplete one share
// the empty circle here where the rail never shows incomplete at all (its rows
// vanish when work settles). Both are "started and not turning", the left
// column already makes the same choice ([homeStuckGlyph]), and a third mark
// would be a state a person has to be taught.
func (a *app) homeTaskGlyph(entry session.TaskIndexEntry, row session.SessionRow) string {
	pal := a.pal
	if row.Runs(entry) && entry.Status == string(session.TaskRunning) {
		if a.linear {
			return pal.accent(glyphRunASCII)
		}
		return pal.accent(tokens.Spinner(a.paints / spinnerStep))
	}
	switch entry.Status {
	case string(session.TaskRunning), string(session.TaskQueued):
		if pal.ascii {
			return pal.dim(glyphQueuedASCII)
		}
		return pal.dim(glyphQueued)
	case string(session.TaskFailed):
		return pal.bad(pal.badGlyph())
	case string(session.TaskUnverified):
		return pal.warn(glyphUnverified)
	}
	mark := glyphDone
	if pal.ascii {
		mark = glyphDoneASCII
	}
	if a.homeEntryFresh(row, entry) {
		return pal.accent(mark)
	}
	return pal.muted(mark)
}

// homeTaskTail is the right edge of a task's row: a count-up while it runs,
// taken from when the session's presence says the node started, and the age it
// landed at otherwise. A running node whose start nobody recorded shows nothing
// — the emptiness law applied to a clock.
func (a *app) homeTaskTail(entry session.TaskIndexEntry, row session.SessionRow) string {
	if row.Runs(entry) && entry.Status == string(session.TaskRunning) {
		if started := homeStarted(row, entry.ID); !started.IsZero() {
			return countUpWord(a.now().Sub(started))
		}
		return ""
	}
	return sinceAt(entry.EndedAt, a.home.world.Read)
}

// homeStarted is when one running node began, from the presence file the
// session itself refreshes — the same file whose naming of the node is what let
// the row spin at all.
func homeStarted(row session.SessionRow, id string) time.Time {
	id = strings.TrimSpace(id)
	for _, out := range row.Presence.RunningTasks {
		if strings.TrimSpace(out.ID) == id {
			return out.StartedAt
		}
	}
	return time.Time{}
}

// homeFilesTouched is how many files this conversation's work wrote, summed
// across its rows. The list of which files is the transcript's; the count is
// the card's one physical fact about the work.
func homeFilesTouched(row session.SessionRow) int {
	total := 0
	for _, entry := range row.Tasks.Rows {
		total += entry.FilesChanged
	}
	return total
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
	if ex := a.paneExchange(); ex != nil {
		if ex.focused {
			return exchangeHint(ex)
		}
		// THE WAY IN IS NAMED WHILE THE LIST HAS THE KEYBOARD. An exchange
		// standing beside the column with no line saying how to reach it is the
		// half of the toggle nobody finds; the list's own verbs come first,
		// because that is the zone the hand is in.
		return "↑↓ move · enter or tab answer this " + homeAskHereWord + " · esc close"
	}
	line, _ := a.home.focusedLine()
	switch {
	case line.kind == homeAskHere:
		// The row that asks rather than opens, and the chord that reaches it
		// without walking up to it (homeexchange.go).
		return "enter asks this here and keeps the record · ↓ start a conversation instead · esc clear"
	case line.kind == homeAction:
		// The THREE readings of the box, all said, because all three are true of
		// what is on screen right now: enter opens a conversation for it,
		// ctrl+enter asks it here (homeexchange.go), and ↑ walks into what it
		// found.
		//
		// THE ARROW IS ↑ BECAUSE THE MATCHES ARE ABOVE. The action row is the last
		// line of the list, against the box ([homeAction]), so walking into the
		// results is walking up the screen — and a hint naming the other arrow
		// would be this line lying about the next keystroke. It names the arrow
		// and not a count, because the row it passes through on the way is the
		// one named two clauses earlier.
		return "enter starts a new conversation and sends this · ctrl+enter ask here · ↑ pick a match · esc clear"
	case line.kind == homeQuiet && line.folded:
		return "enter or → show them · esc close"
	case line.kind == homeQuiet:
		return "enter or ← fold them away · esc close"
	case line.kind == homeItemFold && line.folded:
		return "enter or → show them · esc close"
	case line.kind == homeItemFold:
		return "enter or ← fold them away · esc close"
	case line.kind == homeProject && line.folded:
		return "enter or → open this project here · esc close"
	case line.kind == homeProject:
		return "enter or ← fold this project away · esc close"
	case line.kind == homeMoreProjects && line.folded:
		return "enter or → show them · esc close"
	case line.kind == homeMoreProjects:
		return "enter or ← fold them away · esc close"
	case line.kind == homeItem:
		// THE KEYS THE CARD BESIDE IT ALREADY NAMES, said once more where the
		// hand is. One vocabulary, two places (homestanding.go's
		// [homeItemActions]).
		return homeItemActions + " · esc close"
	case a.home.searching():
		return "enter open · ↓ back to starting a new conversation · esc clear"
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

// homeSubject is the thing THE CARD IS ABOUT as the band registry sees it, or
// false on a row that has no card (the action row, a folded tail).
//
// It reads [homeView.previewLine] rather than the cursor so that everything
// hanging off the card — `m`, a click on one of its fold lines, the chips it
// draws, the repository reading it takes — acts on the card a person is
// LOOKING AT. A card previewing the row under the pointer while `m` opened the
// folds of the row under the cursor would be one screen answering to two
// different rows.
func (a *app) homeSubject() (bandSubject, bool) {
	line, ok := a.home.previewLine()
	if !ok {
		return bandSubject{}, false
	}
	switch line.kind {
	case homeSession:
		return bandSubject{kind: bandKindSession, row: line.row, project: line.project, dir: strings.TrimSpace(line.row.ProjectDir), world: a.home.world}, true
	case homeItem:
		return bandSubject{kind: bandKindItem, item: line.view, project: line.project, dir: strings.TrimSpace(line.item.Workspace), world: a.home.world}, true
	case homeProject:
		// A WHOLE PROJECT IS A SUBJECT TOO ([bandKindProject]). The dir is the
		// workspace the sessions recorded rather than the bucket, which is what
		// every other subject on this screen carries and what a card would put on
		// its place line; a project that never recorded one falls back to the
		// bucket, which is the only address it has.
		return bandSubject{kind: bandKindProject, project: line.project, dir: homeProjectPath(line.proj), world: a.home.world}, true
	}
	return bandSubject{}, false
}

// homeProjectPath is where a project IS: the workspace its conversations
// recorded, and the bucket directory for one that never named a place.
func homeProjectPath(project session.Project) string {
	if path := strings.TrimSpace(project.Path); path != "" {
		return path
	}
	return project.Dir
}
