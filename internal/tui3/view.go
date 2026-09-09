package tui3

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"golang.org/x/text/unicode/norm"

	tea "charm.land/bubbletea/v2"
)

// THE FRAME, AS OF THE STATUS-DOWN WAVE.
//
//	(header)       the room's pinned line, while one is open
//	(strip)        ⠙ Fix nil-map · ◆ Auth tests · +2, while work is running
//	conversation   everything that has happened, scrolled
//	(blank)                                              ↓ latest · ctrl+l
//	───────────    a thin dim rule: below it is your business
//	(consent)      the approval question, when one is waiting
//	(follow)       after yield · N, when something is queued
//	(blank)
//	 › the draft   one row, or up to six of a pasted block
//	(overlay)      the open list, when one is open
//	status         title · model · $cost · N% ctx · state        /help · ctrl+o
//
// THE TWO BLANK ROWS ARE ONE LADDER, not two decisions: [app.breathingRows] is
// the resting gap this window can afford between the last thing said and the
// box the next thing is typed into — two rows on a tall frame, one on the
// everyday one, none on a short one — and the rows are spent top down, so the
// row a narrow window keeps is the one nearest the draft. The jump chip rides
// the FIRST of them (jumpchip.go), which is why the diagram draws it there.
//
// The status bar used to be the FIRST row, and this wave moved it to the last.
// The reason is where a person's eye already is: on this surface everything
// that changes — the reply arriving, the calls running, the question being
// asked, the sentence being typed — happens at the bottom, and a line about
// what is happening was the one fact parked two feet away from all of it. A
// status line at the bottom is read in the same glance as the box above it,
// which is what makes it worth drawing at all. The footer hints went with it
// (the same line's right end), so the frame lost a whole row of chrome rather
// than moving one.
//
// The rule above the input is the only HORIZONTAL line this surface draws, and
// it draws one thing: where the conversation stops and where the person's own
// business starts. There are still no borders and there are not going to be.
//
// THE RAIL ARRIVED WITH THE TASKER, and it is the frame's one vertical seam:
// thirty columns on the right, drawn only while there is a task to stand in
// them and only on a frame wide enough to lend them (task.go's railFloor). It
// takes its columns from the CONVERSATION and from nothing else — the status
// row and the legend span the whole window, because they are about the window.
//
// THE TWO PINNED ROWS ABOVE IT ARE ABOUT THE WINDOW TOO, so they span it: the
// room's focus header, and the task strip under it. Their combined height is
// [app.topHeight], and it is subtracted from the conversation in the one place
// every geometric question resolves through ([app.viewHeight]).
//
// A FRAME TOO NARROW FOR THE RAIL CAN STILL OPEN THE ROSTER, and there it opens
// over the body instead of beside it (alt+t, task.go's [app.railFull]). The
// strip is what makes that reachable without a key at all.
//
// A terminal too short for all of it gives up its breathing room first — the
// rule and the blank above the draft — and its status line last: what is
// happening and what you are typing are the two facts a one-inch window still
// has to carry.

// inputPad is the one cell the draft block is inset by. It is what makes the
// box read as an object below the rule rather than as one more line of the
// transcript, and it is one cell because two would be a margin.
const inputPad = " "

// The two floors the frame's breathing room stands on.
//
// roomyFloor is the height below which this surface stops drawing whitespace
// at all: the rule, the gap and the pinned header all go, and what is left is
// the conversation, the box and the status line. It is the floor
// [app.headHeight] and the task strip already stood on, written down once.
//
// airyFloor is where a SECOND gap row is affordable. The row costs a row of
// conversation, so it is spent only where there is conversation to spend: at
// sixteen the four resting rows of chrome and both gap rows still leave eleven
// for the transcript, which is a reply's worth. Below it the gap steps back to
// the one row it has always been, so no window that was comfortable yesterday
// pays for this wave.
const (
	roomyFloor = 6
	airyFloor  = 16
)

// chromeKind says what one chrome row IS, for the pointer. The frame is the
// only thing that knows where these rows land on screen, so it is the only
// thing that answers the question — see [app.chrome].
type chromeKind uint8

const (
	chromeNone chromeKind = iota
	// chromeChoices is the consent block's offer line, which is interactive by
	// keyboard and hoverable by pointer.
	chromeChoices
	// chromeConnectAsk is the connect offer's answers row, which is interactive
	// by keyboard and hoverable by pointer — the approval question's arrangement
	// one block down (connect.go).
	chromeConnectAsk
	// chromeHarnessAsk is the sub-harness offer's row, which is the same
	// arrangement one rung further down: one pressable row, answered by keyboard
	// or by pointer (harness.go).
	chromeHarnessAsk
	// chromeRoomApproval is the answers row of the design approval block pinned
	// inside a design's own room (roomapproval.go). Only the second row of that
	// block carries this kind, because it is the only one that answers.
	chromeRoomApproval
	// chromeOverlay is one row of whichever list is open; index is its position
	// in that list's own rows.
	chromeOverlay
	// chromeWelcome is one row of the welcome box; index is its position within
	// the box, which [app.welcomeSlotAt] turns back into a recent session.
	chromeWelcome
	// chromeStatus is one row of the HUD's status line; index is its position
	// within that block (0 is the identity's row, 1 the telemetry's on a narrow
	// frame). It is hoverable by nothing and pressable in one place: the model
	// segment, whose columns the render records (render.go's [app.identityParts]).
	chromeStatus
	// chromeStop is one row of the stop confirmation (stop.go); index is its
	// position within the card, and the answers are on index 1. It is the guard's
	// own slot, so the two share a kind's worth of the frame and never a moment
	// of it.
	chromeStop
	// chromeTabClose is one row of the close-a-tab confirmation (tabclose.go);
	// index is its position within the card, and the answers are on index 1. It
	// shares the guard's slot with [chromeStop] for the reason those two share
	// it: they are the same shape of thing and can never be up together.
	chromeTabClose
	// chromeParked is one row of a message waiting for the answer to finish
	// (park.go); index is which parked message that row belongs to, so a press
	// pulls that one back into the box to be edited. The dim line under the block
	// belongs to no message and is marked with nothing.
	chromeParked
	// chromeParkedHint is the dim line UNDER that block — the one that says what
	// the waiting message is doing and which keys change it (park.go's
	// [parkedWord]). It belongs to no message, which is why it is not a
	// [chromeParked], and it is marked at all because one clause on it is a door:
	// `→ steers it in` puts the message into the running answer, by pointer as
	// well as by key (steer.go's [app.steerDoorPress]). The press is a question
	// about the column as well as the row, since the other clauses on the line are
	// statements.
	chromeParkedHint
	// chromeJump is the gap row the jump-to-latest chip is floating on. The row
	// is EMPTY apart from the chip, and the chip is right-aligned, so a press on
	// it is a question about the column as well as the row (jumpchip.go).
	chromeJump
	// chromeLegend is the rule between the transcript and the box. Its right
	// end carries the hint slot, and the one thing in that slot a person can
	// press is the door home (home.go's [app.homeDoorPress]).
	chromeLegend
	// chromeDraft is one row of the input block; index is its position within
	// the block as [app.inputBlock] built it, tray row included. It exists so a
	// click on the box can put the caret under the pointer (draftclick.go) —
	// before it, a press on the draft fell through to the body's drag parking
	// and moved nothing, which made the one place a person types the one place
	// their pointer did not work.
	chromeDraft
)

// chromeRow is one row of the frame below the conversation.
type chromeRow struct {
	kind  chromeKind
	index int
}

// View declares the frame and the terminal state it wants. Alt screen, because
// the conversation scrolls under our own anchor; ALL motion, because a tool
// line is a thing you click and now a thing you hover — cell motion would
// deliver the wheel and the press and nothing in between.
func (a *app) View() tea.View {
	// A MESSAGE THAT CHANGED NOTHING DRAWS THE FRAME BEFORE IT. Bubble Tea builds
	// a frame per message and writes one per sixtieth of a second, so the frames
	// it builds for a burst are mostly frames nobody is ever shown — and a sweep
	// is six hundred messages that touched two integers and a stored position
	// between them. [app.still] is the fold saying so, and it says so only about
	// the paths that mutate nothing this function reads (coalesce.go).
	if a.drawn && a.ptr.still {
		return a.shown
	}
	frame, caretX, caretY := a.frame()
	v := tea.NewView(a.tabTitlePreview(frame))
	v.AltScreen = true
	// THE POINTER IS OURS BY DEFAULT, AND THAT HAS A COST WORTH NAMING. Asking
	// for it — at any motion level — makes this app the owner of every drag, so
	// the terminal's own drag-to-select is dead from that moment; in an
	// alt-screen app there is no scrollback to fall back on either. The setting
	// (config's ui.mouse) is the person's standing call between hover/click here
	// and select-to-copy everywhere, and it says on, because hover and click are
	// this surface's own language.
	//
	// WHICH IS WHY released EXISTS. "Hold shift and drag" is the usual answer and
	// it is not an answer: it is a fact about terminals that most people have
	// never been told, and the ones who have been told use three of them with
	// three different modifiers. ctrl+s hands the pointer over for as long as
	// somebody is using it and takes it back on their next keystroke, which is a
	// thing they can be told once, in one dim line, at the moment it is true
	// (copymode.go, render.go's [app.hintWord]).
	if a.mouse && !a.released {
		v.MouseMode = tea.MouseModeAllMotion
	}
	// FOCUS REPORTING IS ON, and it buys exactly one thing: a turn that ends on
	// a window nobody is looking at can say so (notify.go). It costs two escape
	// sequences at startup and a message per alt-tab, and a terminal that does
	// not speak it simply never sends one — which the notification treats as
	// "focused", i.e. as silence.
	v.ReportFocus = true
	// Bracketed paste stays ON — a pasted stack trace arrives as one
	// tea.PasteMsg with its newlines intact instead of as a stack of enters,
	// each of which would submit. v2 enables it unless this says otherwise, and
	// it says so out loud because the default is the thing being relied on.
	v.DisableBracketedPasteMode = false
	// The tab's one line (windowtitle.go). Declared rather than written: the
	// renderer compares it with the last frame's and emits OSC 2 only when it
	// moved, so declaring it on every frame costs nothing on the frames where
	// nothing changed.
	v.WindowTitle = a.windowTitle()
	// The caret is hidden on surfaces with nothing to type into (home at rest),
	// where a blinking bar over the heading would be a cursor with no box to
	// live in. [app.frame] sets [app.caret] on every render.
	if a.caret {
		v.Cursor = &tea.Cursor{
			Position: tea.Position{X: caretX, Y: caretY},
			Shape:    tea.CursorBar,
			Blink:    true,
		}
	}
	a.shown, a.drawn = v, true
	return v
}

// frame is the whole screen and where the caret sits in it, COMPOSED.
//
// THE COMPOSITION IS THE LAST THING THAT HAPPENS TO A FRAME, AND IT IS HERE
// BECAUSE OF ONE RUNE. A conversation named `the café pricing page` is stored
// the way a Mac's own keyboard writes it — `e` followed by U+0301, the combining
// acute — and every layer of ours carries the mark faithfully: [titleCase],
// [fit] and ansi.Truncate all hand it on, and tmux captures it when a shell
// prints it. It is lost BELOW us. bubbletea asks the terminal for mode 2027 at
// startup and only switches its cell buffer to grapheme widths once the terminal
// answers yes; tmux answers no, so the buffer resets the cell it is filling
// before a zero-width rune arrives and the accent lands on a cell of its own,
// where it is either dropped or drawn beside the letter it belongs to. The
// surface then renames somebody's conversation, which is the one thing
// [titleCase] promises never to do.
//
// NFC is the fix at OUR level: `e` + U+0301 becomes the single rune `é`, which
// is one cell whichever width table the buffer is using, and no information is
// lost — NFC is a canonical mapping, so what a person typed and what we draw are
// the same text.
//
// AND IT COSTS NOTHING ON THE FRAMES THAT DO NOT NEED IT, WHICH IS ALL BUT A
// FEW. This runs on every draw and this surface is under an allocation law —
// PERF.md's four-thousand-line scroll ceiling is measured through this very
// function — so what it must not do is build a second copy of the screen sixty
// times a second. [norm.Form.String] is written for exactly that: it spans the
// string for the first byte composition could change and, finding none, HANDS
// BACK THE STRING IT WAS GIVEN, with no allocation at all.
//
// SO THERE IS NO GATE IN FRONT OF IT, and that is a decision and not an
// oversight. Two were tried. [norm.Form.IsNormalString] answers the same
// question and costs one allocation per call whatever it finds — an allocation
// per keystroke to avoid a call that allocates nothing. A hand-rolled walk
// looking for a combining mark allocates nothing and is about twice as quick on
// a frame of this program's own chrome (10µs against 21µs on six kilobytes),
// and it buys that by answering a question norm is the authority on, in a
// second place, for a saving smaller than one entry render.
// [TestComposingAFrameCostsNothingWhenThereIsNothingToCompose] is the law that
// holds the fast path, so a normaliser that rebuilt the frame — [norm.Form.Bytes]
// is the one somebody reaches for — fails rather than quietly costing a frame.
func (a *app) frame() (string, int, int) {
	body, caretX, caretY := a.frameBody()
	return norm.NFC.String(body), caretX, caretY
}

// frameBody is the frame as every surface in this package builds it, before the
// one composition pass [app.frame] puts over the whole of it.
func (a *app) frameBody() (string, int, int) {
	a.inlineWaitShowing = false
	width, height := a.size()
	if a.pasteEdit.open {
		return a.pasteEditorFrame(width, height)
	}
	// The caret is shown by default and hidden only by the surfaces that have
	// nothing to type into (home at rest). Set here so every path below starts
	// from the same answer and only the ones that hide it say so.
	a.caret = true
	// AND THERE IS NO TAB BAR UNTIL A FRAME DRAWS ONE. Every place goes through
	// [placeFrame], which records the row it put the bar on; the frames that do
	// not — home's phone inbox and sheet, the task record card — draw something
	// else in those cells entirely, and a press resolved against the last bar
	// this window happened to paint would open a place for a click on a rule
	// (placemouse.go's [app.placeTabPress]).
	a.tabRow = -1
	// THE FIRST-RUN SETUP IS DECIDED BEFORE EVERY OTHER FULLSCREEN SURFACE,
	// because it is the one that may be open before any of them exists and it
	// goes away to reveal whichever of them was decided underneath (firstrun.go).
	// One block, centred, no chrome.
	if a.setup.open {
		lines, caretX, caretY := a.setupFrame(width, height)
		return strings.Join(lines, "\n"), caretX, caretY
	}
	// AND THEN WHATEVER PLACE IS STANDING, in ONE call and never seven
	// (pages.go's [app.placeFrameNow]). Each of the seven takes the frame WHOLE:
	// no conversation above it, no input line under it, nothing of the frame
	// below showing through at the edges — a sheet drawn into a viewport is a
	// sheet you read past. Which one is up is [app.page]; what rows it has is the
	// registry's answer, so this file never knows a place by name.
	//
	// THEY CANNOT BE UP TOGETHER, and that is one field rather than an invariant
	// now: [app.showPage] closes what was standing before it opens what is asked
	// for, so the frame that used to ask seven `open` flags in a fixed order —
	// with a written-down note that an order only true while nobody makes a
	// mistake is an order that draws a blank frame the day somebody does — asks
	// one.
	if lines, _, caretX, caretY, up := a.placeFrameNow(width, height); up {
		return strings.Join(lines, "\n"), caretX, caretY
	}
	// AND A BACKGROUND JOB'S PAGE, on the same terms and outside the bar
	// (jobpage.go). It takes the frame WHOLE and draws no composer under itself,
	// which is the whole of why it is here rather than inside the conversation:
	// there is nobody in a job to read a line, and the surface used to say so
	// with a refusal under a box that should never have been on the page.
	//
	// IT IS UNDER THE PLACES because a place is a room in the machine and this is
	// one job in one conversation; alt+1…7 leaves it, and [app.standDownRest]
	// closes it on the way out so it cannot reappear under a place somebody has
	// since walked away from.
	//
	// It draws nothing when the job it named has gone — a page opened on work
	// this window no longer holds — and the frame then falls through to the
	// conversation, which is [app.expandShowing]'s own rule below.
	if a.jobPageOpen() {
		if lines, _, caretX, caretY := a.jobPageFrame(width, height); len(lines) > 0 {
			// NOTHING ON THIS PAGE IS TYPED INTO — a blinking bar at (0, 0) was
			// a cursor with no box, the same law home-at-rest and the task card
			// follow by hiding the caret rather than parking it on the title.
			a.caret = false
			return strings.Join(lines, "\n"), caretX, caretY
		}
	}
	// AND THE REWIND TIMELINE, on the same terms and outside the bar
	// (rewindsheet.go). It is the whole conversation, laid out as the list a
	// person picks a point out of, and a picker read past the very conversation it
	// is picking from would be the page arguing with itself — which is also why
	// the inline mode draws no list at all (rewind.go).
	if a.rewSheet.open {
		lines, _, caretX, caretY := a.rewindSheetFrame(width, height)
		return strings.Join(lines, "\n"), caretX, caretY
	}
	// AND THE STATUS SHEET IS THE FIFTH, on the phone tier only: the deck's two
	// rows are what fits at forty-four columns, and the sheet is everything the
	// status line can carry, one per line (statusdeck.go). It takes the frame
	// whole for the reason the panel does — a sheet drawn into a viewport that
	// narrow is a sheet you read past.
	if a.deck.open && layoutTier(width) != tierPhone {
		// A frame that grew out of the phone tier has its whole status row back,
		// and a sheet standing in for a row that is on screen again is a sheet
		// nobody asked for.
		a.deck = deckSheet{}
	}
	if a.deck.open {
		lines, _, caretX, caretY := a.deckSheetFrame(width, height)
		return strings.Join(lines, "\n"), caretX, caretY
	}
	// And the phone's tool detail is the sixth, for the same reason at the
	// other end of the width range: a call opened at tierPhone is a diff, a
	// command or a log, and every one of those wants the lines the chrome would
	// otherwise take (expand.go). It draws nothing when the call it named has
	// gone, and the frame falls through to the conversation.
	//
	// It is NOT closed on the way out of the phone tier the way the status sheet
	// above is: [app.expandShowing] derives the answer from the width every
	// frame, so a terminal dragged wider expands the call inline and narrowing
	// again brings the sheet back (expand.go).
	if a.expandShowing() {
		if lines, _, caretX, caretY := a.expandFrame(width, height); len(lines) > 0 {
			return strings.Join(lines, "\n"), caretX, caretY
		}
	}
	// AND THE CONTEXT CHOOSER OVER THE WHOLE OF IT (contextmodal.go). It is the
	// LAST of these because it is the only one that keeps what is underneath on
	// the screen: the conversation is composed exactly as it would have been and
	// then faded, so a person choosing a folder can still see the message they
	// were writing — and cannot touch it. Everything above this line is a surface
	// that REPLACES the conversation; this one covers it.
	if a.contextModalShowing() {
		under, _, _ := a.chatFrameLines(width, height)
		lines, caretX, caretY := a.contextModalOver(under, width, height)
		return strings.Join(lines, "\n"), caretX, caretY
	}
	lines, caretX, caretY := a.chatFrameLines(width, height)
	return strings.Join(lines, "\n"), caretX, caretY
}

// chatFrameLines is the ordinary conversation frame — the transcript, its rail,
// the chrome under it and the status line — as LINES rather than as one string.
//
// It was the tail of [app.frameBody] and is its own function for one caller: the
// context chooser draws over a finished frame and has to be handed one
// (contextmodal.go). Splitting a joined frame back apart would have been the
// same rows measured twice, and a sheet composited onto a second measurement is
// a sheet one row away from where the pointer thinks it is.
func (a *app) chatFrameLines(width, height int) ([]string, int, int) {
	// The body decides who owns activity before the footer is drawn. This
	// uses the same cached rows that the frame places below; status painting
	// never rebuilds a hidden transcript to infer ownership.
	view := a.viewHeight()
	fullRail := a.railFull()
	var body []row
	pad := 0
	if !fullRail {
		body, pad = a.bodyRows(a.bodyWidth(), view)
		for _, r := range body {
			a.inlineWaitShowing = a.inlineWaitShowing || r.inlineWait
		}
	}
	chrome, chromeMarks, caretX, caretRow := a.chrome(width)
	// The welcome box rides at the top of the frame rather than at the bottom
	// with the chrome it is built with ([welcomeLift] states why). Splitting it
	// off here keeps [app.frameOut]'s law intact: what is left is still the tail,
	// and the caret is still counted back through it.
	lift := welcomeLift(chromeMarks)
	lifted := chrome[:lift]
	chrome = chrome[lift:]
	// THE FOCUS HEADER IS THE FRAME'S ONE PINNED ROW ABOVE the conversation, and
	// it spans the WHOLE window for the reason the status row does: it is about
	// the window — which page this is, and how to leave it — rather than about
	// the transcript, so it is not one of the columns the rail borrows from
	// (room.go).
	// THE TAB STRIP IS THE ROW ABOVE IT, and it is a different question: the
	// header says where you are INSIDE a conversation, and the strip says which
	// conversation that is and which others this window can go back to
	// (chattabs.go). They are two rows because they are two questions — the one
	// that used to carry both carried neither well.
	tabs := a.tabsRow(width)
	head := a.roomHeadRows(width)
	// AND THE TASK STRIP IS THE ROW UNDER IT, for the same reason and at the same
	// width: what is running is a fact about the SESSION, not about the
	// transcript, and it is pinned because a door that scrolls away is a door
	// only the person at the bottom of the page has (taskstrip.go).
	// It is ROWS and not a row: a session running one thing is the single line
	// this surface has always drawn, and a session running an adaptive tree is
	// that line with the family under it, one node per row (taskstrip.go).
	strip := a.stripRows(width)
	// THE RAIL COSTS COLUMNS, AND IT COSTS THEM HERE. The conversation is laid
	// out at [app.bodyWidth] — everything below this line, the wheel and the
	// hit-testing included, resolves through the same number — and the chrome is
	// drawn at the FULL width, because the status line and the legend are about
	// the whole window rather than about the transcript (task.go).

	rows := make([]string, 0, height)
	if tabs != "" {
		if a.tabsLineRow() > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, tabs)
		if a.tabsBottomPad() > 0 {
			rows = append(rows, "")
		}
		// AND THE SEAM UNDER THEM OUT IN THE CONVERSATION, where there is no trail
		// and no facts row to close the panel off (chattabs.go's
		// [app.chatRuleHeight]). It is charged for by [app.headHeight] on the same
		// floor, so the row the frame draws and the row the scrolling subtracts are
		// the same row.
		if a.chatRuleHeight(width) > 0 {
			rows = append(rows, a.rule(width))
		}
	}
	if len(head) > 0 {
		rows = append(rows, head...)
		// AND THE FAMILY UNDER IT, dim, where this node has one: who handed the
		// work out and what it handed out itself, which is the fact the roster's
		// tree carries in its shape and this page had no shape to carry it in
		// (room.go's [app.roomKinRows]). They ride with the header rather than
		// with the page because they are true of the page as a whole, and a fact
		// that scrolls away is only true at the top.
		rows = append(rows, a.roomKinRows(width)...)
	}
	rows = append(rows, strip...)
	// THE ROSTER TAKES THE BODY WHOLE on a frame with no columns to lend it: the
	// same rows, the same folds, the same footer, laid out at the full width
	// instead of squeezed into thirty columns that are not there (task.go's
	// [app.railFull]). The conversation is not drawn under it — an overlay you
	// read past is an overlay that made the page harder to read — and the chrome
	// below stays, because the draft is still where this surface types.
	if fullRail {
		// THE SWITCHER IS DRAWN OVER THE ROSTER TOO. On a frame with no columns
		// to lend, the roster IS the body, and a card that skipped this branch
		// would be a key that did nothing at sixty columns (hop.go).
		a.hop.originY = len(rows)
		rows = append(rows, a.hopMaybe(a.railRows(view), width)...)
		// The lifted rows are still part of this frame's height even here, where
		// the roster has taken the body: dropping them would draw a window short
		// of the terminal by exactly the box. [app.chromeAt] does not resolve
		// them while the roster is up, which is right — the roster is over them.
		liftedAt := len(rows)
		rows = append(rows, lifted...)
		return a.frameLines(rows, chrome, height, caretX, caretRow, lift, liftedAt)
	}
	rail := a.railRows(view)
	railAt := func(i int) string {
		if i < len(rail) {
			return rail[i]
		}
		return ""
	}
	// THE CONVERSATION HANGS FROM THE TOP AND THE SLACK FALLS BELOW IT. The
	// blank rows used to go above, which put a two-line conversation down at the
	// bottom of an empty screen and made a new session look like the tail of one
	// that had scrolled away. A person opening aforge reads from the top of the
	// window like they read everything else, so the first thing said is the
	// first thing drawn and the emptiness is under it where it costs nothing.
	//
	// The chrome is untouched by this and stays the tail ([app.frameOut]): the
	// draft is still at the bottom of the window, where a terminal has always
	// put the thing you type into. Only the body moved.
	//
	// Once the conversation is longer than the region there is no slack at all —
	// pad is zero, [app.offsetFor] has already chosen the window that ends at
	// the newest row, and the frame is exactly what it always was.
	// A sweep in flight paints its rows with THE GROUND LADDER'S MARK STEP — the
	// same statement copy mode's selection makes, because it is the same
	// selection: these rows are the ones, and on release their text is the copy
	// (dragselect.go). The two used to be painted two rungs apart, a sweep on
	// the pointer's step and copy mode's span on the mark's, which said one of
	// them was a shadow and the other a selection. NEITHER IS A SHADOW: a span a
	// person is building is a span whichever way they built it.
	selFrom, selTo, selOn := a.dragSpan()
	// The selection is CONTENT rows (dragselect.go), so it is compared against
	// each drawn row's index in the body's own list — which is this screen
	// row's index plus the scroll — and a body that streams under a sweep keeps
	// the highlight on the text rather than on the glass.
	scroll := a.bodyScroll()
	// THE SWITCHER TAKES THE WHOLE REGION, THE SLACK INCLUDED (hop.go). It is
	// spliced here rather than into `body` alone because a two-line conversation
	// is two rows of body and thirty of pad, and a card centred in the body would
	// sit at the top of an empty screen. What it is centred in is what a person
	// sees, which is the region.
	if a.hopShowing() {
		texts := make([]string, view)
		for i, r := range body {
			if i < len(texts) {
				texts[i] = r.text
			}
		}
		a.hop.originY = len(rows)
		texts = a.hopOver(texts, a.bodyWidth(), a.pal)
		body, pad = make([]row, len(texts)), 0
		for i, text := range texts {
			// NO HIT AND NO ENTRY. A click on a card row is a click on the card,
			// not on whatever piece of the transcript the ladder happened to be
			// drawing there (render.go's [row]).
			body[i] = row{text: text, entry: -1}
		}
		selOn = false
	}
	for i, r := range body {
		text := r.text
		if selOn {
			// THE SELECTION IS CELLS, not rows: the span this row carries is
			// painted and the rest of the row keeps its own paint, so what is
			// lit is exactly what a release copies (dragselect.go's
			// [app.dragCells], which snaps the span to whole glyphs).
			if at := scroll + i; at >= selFrom && at <= selTo {
				if from, to, ok := a.dragCells(at, ansi.Strip(text)); ok {
					text = a.markCells(text, from, to)
				}
			}
		}
		rows = append(rows, a.railJoin(text, a.hopFadeRail(railAt(i))))
	}
	// THE GREETING SITS IN THE SLACK, a shade above its middle, with whatever
	// the conversation already holds — a notice, an order standing here — above
	// it where it was. The split is [welcomeAbove]'s and the pointer reads the
	// same split back ([app.chromeAt]).
	above := welcomeAbove(lift, pad)
	for i := 0; i < above; i++ {
		rows = append(rows, a.railJoin("", railAt(len(body)+i)))
	}
	liftedAt := len(rows)
	rows = append(rows, lifted...)
	for i := above; i < pad; i++ {
		rows = append(rows, a.railJoin("", railAt(len(body)+i)))
	}
	return a.frameLines(rows, chrome, height, caretX, caretRow, lift, liftedAt)
}

// welcomeAbove is how much of the body's slack goes ABOVE the lifted greeting:
// nothing when nothing is lifted, and two fifths of it otherwise. Two fifths
// rather than a half because an object at the exact middle of a tall window
// reads as sitting low — the eye's centre is above the frame's — and a shade
// above is where a centred thing looks centred.
func welcomeAbove(lift, pad int) int {
	if lift == 0 || pad <= 0 {
		return 0
	}
	return pad * 2 / 5
}

// frameOut closes a frame: the chrome under whatever the body drew, cut to the
// terminal, and the caret turned into a screen row.
//
// It is one function because the two body layouts — the conversation beside its
// rail, and the roster over the whole of it — must end the same way. A second
// copy of this arithmetic is a caret that lands on the right row in one of them.
//
// THE CARET IS COUNTED FROM WHICHEVER HALF OF THE CHROME IT IS IN. caretRow is
// a row of the chrome block as [app.chrome] built it — the lifted greeting
// first, then the tail. A caret inside the greeting is liftedAt rows down plus
// its row; a caret in the tail is counted back from the foot of the frame
// through the tail alone. Counting it back through the tail with the lift still
// in it is the arithmetic this replaced, and it put the terminal's cursor on the
// status row for as long as the greeting was up.
func (a *app) frameOut(rows, chrome []string, height, caretX, caretRow, lift, liftedAt int) (string, int, int) {
	lines, caretX, caretY := a.frameLines(rows, chrome, height, caretX, caretRow, lift, liftedAt)
	return strings.Join(lines, "\n"), caretX, caretY
}

// frameLines is [app.frameOut] with the rows still apart, for the one caller
// that draws OVER a finished frame rather than beside it: the context chooser
// composites its sheet onto these lines and needs them as lines
// (contextmodal.go). Joining and re-splitting would be the same arithmetic done
// twice, and the second copy is the one that would be wrong.
func (a *app) frameLines(rows, chrome []string, height, caretX, caretRow, lift, liftedAt int) ([]string, int, int) {
	rows = append(rows, chrome...)
	// A frame taller than the terminal loses rows from the TOP: the chrome is
	// the tail, and everything the caret's row is counted back through is in it.
	cut := 0
	if len(rows) > height {
		cut = len(rows) - height
		rows = rows[cut:]
	}
	caretY := height - len(chrome) + caretRow - lift
	if caretRow < lift {
		caretY = liftedAt + caretRow - cut
	}
	if caretY < 0 {
		caretY = 0
	}
	if caretY >= height {
		caretY = height - 1
	}
	return rows, caretX, caretY
}

// chrome is everything below the conversation: the rows, what each row is for
// the pointer, the caret's column and the caret's row within the block.
//
// It is ONE function because it is one geometry. The frame draws these rows,
// [app.viewHeight] subtracts their count from the conversation, and
// [app.chromeAt] resolves a pointer to one of them — three questions that must
// never be able to disagree about where the input line is.
func (a *app) chrome(width int) ([]string, []chromeRow, int, int) {
	gap := a.breathingRows()
	roomy := gap > 0

	rows := make([]string, 0, 8)
	marks := make([]chromeRow, 0, 8)
	add := func(text string, mark chromeRow) {
		rows = append(rows, text)
		marks = append(marks, mark)
	}

	// THE CHIP RIDES THE FIRST ROW OF THE GAP, which is the row nearest the
	// conversation it is about: above the rule where the window is airy enough
	// to have a row up there, and the old blank above the draft where it is not.
	// It is drawn into a row that ALREADY EXISTS rather than onto the last line
	// of the transcript, and that is the whole reason it composes: a conversation
	// row is cached per entry (render.go's entryRows) and shortened by the rail's
	// columns, so painting a chip into one would mean invalidating somebody's
	// cache every time the pointer moved and right-aligning to a width that
	// changes when a task starts. A gap row is built fresh every frame, spans the
	// whole window, and carries a mark the pointer already knows how to resolve.
	chip := a.jumpChip(width)
	jumped := false
	// addGap spends one row of the ladder, and hands it to the chip if the chip
	// has not been placed yet.
	addGap := func() {
		if chip != "" && !jumped {
			jumped = true
			add(chip, chromeRow{kind: chromeJump})
			return
		}
		add("", chromeRow{})
	}
	// THE GREETING IS THE HEAD OF THIS BLOCK AND, WHILE IT IS UP, IT IS MOST OF
	// IT. The unit's rows are built here and marked here so that they are
	// hit-tested with the rest of the chrome, and then lifted to the middle of
	// the frame ([welcomeLift]). While it is drawn, the rule, the breathing rows
	// and the box at the foot are not: the unit is centred in the slack, and a
	// legend under nothing is a seam between two things that are not there.
	// The status row still closes the frame, and any question the session
	// raises before the first sentence still stacks above it.
	unit, _, unitX, unitRow := a.welcomeUnit(width)
	greeted := len(unit) > 0
	for i, line := range unit {
		add(line, chromeRow{kind: chromeWelcome, index: i})
	}
	// The rows above the rule are the SECOND helping of breathing room, so there
	// is one of them or none (see [app.breathingRows]).
	for i := 1; i < gap && !greeted; i++ {
		addGap()
	}
	if roomy && !greeted {
		// THE RULE IS A LEGEND NOW: the same one line, with where you are written
		// into it (render.go). It degrades back to the plain rule on a frame with
		// no room for a label.
		add(a.legend(width), chromeRow{kind: chromeLegend})
	}
	for i, line := range a.consentRows(width) {
		// The offer is the second row of the block, and it is the only row of it
		// a pointer can be over — the call above it is a transcript row that
		// happens to be repeated here, and the rule and the count below it are
		// statements. The phone sheet answers to the pointer over its whole
		// height, which is [app.consentMark]'s other half (consent.go).
		add(line, a.consentMark(i, width))
	}
	// AND THE CONNECT OFFER SITS DIRECTLY UNDER IT, because it is the same kind
	// of thing one rung quieter: a question the session is waiting on, drawn
	// where this surface draws everything it wants answered (connect.go). The
	// two STACK rather than share a slot — an approval question is about a call
	// and this is about an account, and either can be raised while the other is
	// up — and the block above keeps the keyboard while it is there.
	for i, line := range a.connectAskRows(width) {
		add(line, a.connectMark(i))
	}
	// AND THE HARNESS OFFER UNDER THAT, the third and quietest rung of the same
	// lane (harness.go): a question the session is holding a turn on, one row,
	// whose no is free. It stacks under the two above it for their own reason —
	// any of the three can be raised while another is up.
	for i, line := range a.harnessAskRows(width) {
		add(line, a.harnessMark(i))
	}
	// AND A DESIGN ROOM'S APPROVAL ROW UNDER THAT (roomapproval.go). It is the
	// same shape of thing one rung quieter still, and it STACKS rather than
	// sharing: the three blocks above are questions the SESSION is blocked on,
	// which own the keyboard while they are up, and this one is a standing
	// question in a room the person is also talking in — so it can be on screen
	// under any of them, and it takes only its own two chords.
	for i, line := range a.roomApprovalRows(width) {
		add(line, a.roomApprovalMark(i))
	}
	// THE STEER GUARD SITS WHERE THE APPROVAL QUESTION SITS, because it is the
	// same kind of thing: the surface holding words back until it is told where
	// to send them (room.go). The two can never be up together — a question the
	// SESSION is blocked on suspends the box the guard is raised from — so they
	// share the slot rather than stacking in it.
	for i, line := range a.guardRows(width) {
		add(line, a.guardMark(i))
	}
	if line := a.followRow(width); line != "" {
		add(line, chromeRow{})
	}
	// AND WHAT IS WAITING TO GO INTO ANOTHER FOLDER, in the same slot and by the
	// same law: one dim row while there is something to land, nothing at all
	// otherwise (landcmd.go).
	if line := a.landRow(width); line != "" {
		add(line, chromeRow{})
	}
	// AND WHAT YOU TYPED WHILE THE ANSWER WAS STILL COMING SITS DIRECTLY ABOVE
	// THE BOX (park.go). It is the LAST block of the chrome for a reason that is
	// the whole point of it: pinned here, between everything that has happened
	// and the box it was typed into, a waiting message cannot be spliced into the
	// middle of the reply that is still streaming above it. Each of its rows is
	// pressable — a click pulls that message back in to be edited.
	for i, line := range a.parkedRows(width) {
		add(line, a.parkedMark(i, width))
	}
	if roomy && !greeted {
		addGap()
	}

	// THE CARET IS IN THE UNIT WHILE THE UNIT HOLDS THE BOX, and at the foot
	// otherwise. Both are a row counted from the head of this block — the unit's
	// rows are its first rows — and the frame turns each into a screen row from
	// where it drew that half ([app.frameOut]).
	caretX, caretRow := unitX, unitRow
	if !a.welcomeHolds() {
		if a.roomRecipientHeight() > 0 {
			add(inputPad+a.pal.accent(fit(a.roomRecipientWord(), width-len(inputPad))), chromeRow{})
		}
		input, x, row := a.inputBlock(width - len(inputPad))
		// THE BOX IS THE REDIRECT LANE while a proposal is open: the placeholder
		// is applied to the block the input already rendered, because the hint
		// slot inside it belongs to the picker's filter and the two are never up
		// together (task.go).
		input = a.redirectLane(input, width-len(inputPad))
		// AND THE BOX TALKS TO THE NODE while a room is open: same box, same
		// rules, a placeholder that says who is listening (room.go). The two lanes
		// cannot be up together — a proposal is a question about work that has not
		// started, a room is a page for work that has — and [app.roomSteerLaneRows]
		// defers to the one above it rather than assuming so.
		input = a.roomSteerLaneRows(input, width-len(inputPad))
		caretX, caretRow = x+len(inputPad), row+len(rows)
		for i, line := range input {
			// Marked so the pointer can answer for the box: which row of the
			// block a press landed on is the y half of putting the caret under it
			// (draftclick.go). While the welcome unit holds the box its rows are
			// the unit's and carry no draft mark — a press there is the
			// greeting's own business.
			add(inputPad+line, chromeRow{kind: chromeDraft, index: i})
		}
	}
	// AND WHAT THE DRAFT WOULD MEAN SITS DIRECTLY UNDER THE BOX (spellout.go).
	// Below, because it is not part of the message and being under the sentence
	// is how a person reads that at a glance; and above the open list, because a
	// list is the keyboard's and this is only ever text.
	for _, line := range a.spellRows(width) {
		add(line, chromeRow{})
	}
	for i, line := range a.overlayRows(width, a.overlayHeight()) {
		add(line, chromeRow{kind: chromeOverlay, index: i})
	}
	for i, line := range a.statusRow(width) {
		add(line, chromeRow{kind: chromeStatus, index: i})
	}

	return rows, marks, caretX, caretRow
}

// statusRow is the HUD's status row — one row, or two on a narrow frame where
// the telemetry stops sharing with the identity (render.go's [app.statusRows])
// — with the reasoning level on the model segment:
// "anthropic/claude-sonnet-4.5:high" where a level has been dialled, and the
// bare model id — the line exactly as it was — where none has.
//
// The level belongs on that line because it is a fact about what the next
// request will cost and how long it will take, and the model segment is where a
// person already looks for both. It is spelled with a colon rather than a fourth
// segment for the same reason it is spelled that way on the picker row: it is
// not a thing beside the model, it is how this model is being run.
//
// The splice happens by LENDING the model field its suffixed form for the length
// of one call. [app.status] reads a.model directly (render.go), the frame is
// drawn on the model goroutine one row at a time, and the alternative is a
// second copy of the status line's segment layout — width budget, narrow-frame
// dropping and all — kept in step with the first by nothing but attention.
func (a *app) statusRow(width int) []string {
	level := a.reasoningFor(a.model)
	if level == "" || a.model == "" {
		return a.statusRows(width)
	}
	id := a.model
	a.model = id + ":" + level
	defer func() { a.model = id }()
	return a.statusRows(width)
}

// chromeAt resolves a screen row to the chrome row drawn on it. It is the
// pointer's half of [app.chrome] and it asks the same function the frame does,
// so a hover cannot land on a row the frame drew somewhere else.
//
// IT REBUILDS THE CHROME RATHER THAN READING A RECORDED COPY OF IT, and that is
// a choice against the cheaper one. Recording the marks at layout — the bargain
// [app.modelSpan] and the strip's chips make — would answer in no time at all,
// and it would answer from the LAST frame: the renderer paints on its own clock,
// so an approval question that arrived two messages ago is on the frame the
// person is looking at and not yet in any recorded list. A pointer resolved
// against yesterday's chrome brightens the wrong row, which is the one thing
// this file exists to prevent. The rebuild is paid only where the pointer is
// actually below the conversation — [app.hoverTarget] asks the transcript first
// and returns on a hit — so it is the bottom few rows of the frame that cost it,
// and it is exact there.
func (a *app) chromeAt(y int) (chromeRow, bool) {
	width, height := a.size()
	_, marks, _, _ := a.chrome(width)
	lift := welcomeLift(marks)
	// The tail is the chrome minus whatever was lifted to the top of the frame,
	// and it is still the last rows of the window.
	tail := len(marks) - lift
	if at := y - (height - tail); at >= 0 && at < tail {
		return marks[lift+at], true
	}
	// The lifted rows sit under the conversation and its share of the slack,
	// which is where the frame drew them. Asking [app.bodyRows] again, and
	// [welcomeAbove] again, is what keeps this answer and the drawn one the same
	// answer.
	if lift > 0 && !a.railFull() {
		top := a.bodyTop()
		if top < 0 {
			return chromeRow{}, false
		}
		body, pad := a.bodyRows(a.bodyWidth(), a.viewHeight())
		start := top + len(body) + welcomeAbove(lift, pad)
		if at := y - start; at >= 0 && at < lift {
			return marks[at], true
		}
	}
	return chromeRow{}, false
}

// welcomeLift is how many rows at the HEAD of the chrome block are drawn at the
// top of the frame instead of at the bottom with the rest of it.
//
// The welcome box is chrome by construction — it is built, marked and
// hit-tested with the legend and the draft, and it belongs there because it is
// what stands in for a conversation rather than part of one. But it is the one
// piece of chrome that reads as the TOP of the page: a person opening aforge
// meets the box first and the input second, and a box pinned to the bottom of an
// empty window put the greeting below a screenful of nothing.
//
// So the box alone is lifted, and the slack falls between it and everything
// under it. It is a lift and not a move because moving it would mean a second
// copy of the geometry — [app.chromeAt] reads this same count back, so the row a
// pointer lands on and the row the frame drew cannot disagree.
//
// Zero whenever the box is not showing, which is every frame with a conversation
// in it.
func welcomeLift(marks []chromeRow) int {
	lift := 0
	for i, mark := range marks {
		if mark.kind == chromeWelcome {
			lift = i + 1
		}
	}
	return lift
}

// chromeHeight is how many rows the frame spends below the conversation.
func (a *app) chromeHeight() int {
	width, _ := a.size()
	// The status (one row, or two when the telemetry wraps — and always two at
	// the phone tier, where it is a deck rather than a row: [app.statusHeight]
	// answers that one from the tier alone, so this count never has to run a
	// layout to learn how tall the bottom of the frame is), the input block, and
	// whatever the two optional blocks, the open list and the welcome box are
	// holding.
	n := a.statusHeight(width) + a.overlayHeight() + a.consentHeight() +
		a.connectAskHeight() + a.harnessAskHeight() + a.roomApprovalHeight() + a.guardHeight() +
		a.followHeight() + a.landHeight() + a.parkedHeight() + a.welcomeHeight() + a.spellHeight()
	// THE GREETING'S ROWS ALREADY HOLD THE BOX while it holds the box, and the
	// rule and its breathing room are not drawn under a greeting at all — both
	// are [app.chrome]'s own decisions, read back here so the conversation is
	// charged exactly what the frame draws.
	if !a.welcomeHolds() {
		n += a.inputHeight() + a.roomRecipientHeight()
	}
	if gap := a.breathingRows(); gap > 0 && a.welcomeHeight() == 0 {
		n += gap + 1 // the breathing room, and the rule standing in it
	}
	return n
}

// breathingRows is the resting gap between the conversation and the box, in
// rows. It is the ONE ladder both halves of the frame read — [app.chrome] spends
// these rows and [app.chromeHeight] charges the conversation for them — because
// a gap the layout drew and the geometry did not count is a caret one row below
// where the terminal puts its cursor.
//
// THE LADDER STEPS DOWN, NEVER UP. Two rows is the resting state of a window
// with the height to lend them; the everyday window keeps the single blank above
// the draft it has always had; and below [roomyFloor] the surface stops drawing
// whitespace altogether, along with the rule, the pinned header and the strip.
// A short window never pays for the wave that made a tall one roomier.
func (a *app) breathingRows() int {
	_, height := a.size()
	switch {
	case height >= airyFloor:
		return 2
	case height >= roomyFloor:
		return 1
	}
	return 0
}

// rule is the one line this surface draws: the seam between what happened and
// what you are about to say.
func (a *app) rule(width int) string {
	if width < 1 {
		return ""
	}
	return a.pal.dim(strings.Repeat("─", width))
}

// size is the frame's working size: the terminal's, or the classic default
// when nobody has said. A headless boot and a terminal that answers zero are
// the same case, and drawing into a zero-by-zero frame would mean drawing
// nothing at all.
func (a *app) size() (int, int) {
	width, height := a.width, a.height
	if width < 8 {
		width = 8
	}
	if height < 1 {
		height = 1
	}
	return width, height
}

// window is the visible slice of the row list and the padding above it.
//
// Every geometric question on this surface goes through here — what the frame
// draws, where the wheel lands, which row a click hit — so a row's position on
// screen has exactly ONE definition. The padding is the SLACK under a short
// conversation: the rows hang from the top of the region and this is what is
// left over beneath them (the frame states the law and why it changed). It is
// zero the moment the conversation is longer than the region, which is every
// interesting case.
func (a *app) window(width, height int) ([]row, int) {
	if height <= 0 {
		return nil, 0
	}
	rows := a.visible(width)
	offset := a.offsetFor(len(rows), height)
	end := min(offset+height, len(rows))
	visible := rows[offset:end]
	if pad := height - len(visible); pad > 0 {
		return visible, pad
	}
	return visible, 0
}

// bodyRows is what the frame draws above the chrome: the live conversation, or
// the frozen snapshot while copy mode is up (copymode.go).
//
// It is the ONE place the two can be swapped, and the swap is deliberately not
// in [app.window]: window is what the wheel, the click hit-testing and the
// selection all resolve through, and every one of those questions is about the
// LIVE conversation whatever is on screen. A frozen view answers "what is
// drawn" and nothing else, which is why the pointer paths return early while it
// is up rather than being redirected here.
// THE ROOM IS THE THIRD ANSWER, and it is the whole of what "a task is a place"
// costs the frame: while one is open the body region draws that node's page
// instead of the conversation (room.go). Everything else about the frame is
// unchanged — the rail is still beside it, the chrome is still under it, the
// turn underneath is still streaming into a transcript nobody is looking at —
// which is what makes esc restore the conversation exactly.
func (a *app) bodyRows(width, height int) ([]row, int) {
	// THE NEW-CHAT START PAGE STANDS OVER THE CONVERSATION AND NOT UNDER IT
	// (chatstart.go). It is the greeting's unit drawn in the middle of the frame,
	// and the greeting is only ever drawn on an EMPTY surface — over a transcript
	// it would be centred in whatever slack that transcript left, which on a full
	// screen is none, and the two would be drawn through each other.
	//
	// IT IS ANSWERED HERE AND NOWHERE ELSE because this is the one reading three
	// things share: the draw, the pointer ([app.chromeAt] asks it again to find
	// the lifted rows) and the hit-testing ([app.rowAt]). A frame that hid the
	// transcript in the draw alone would keep answering clicks with rows nobody
	// can see.
	if a.startingChat() {
		if a.welcomeFits() {
			return nil, height
		}
		// A window too small for the unit still gets the page: the box falls back
		// to the foot of the frame where it lives on every other screen, and this
		// row is what says which page it belongs to.
		return []row{{text: a.pal.dim(fit(startTinyWord, width)), entry: -1}}, max(0, height-1)
	}
	if a.copy.on {
		return a.copyRows(width, height)
	}
	if a.roomOpen() {
		return a.roomWindow(width, height)
	}
	return a.window(width, height)
}

// bodyTop is the screen row the conversation starts on, or -1 when the frame is
// too short to have one. The conversation now opens the frame — the status bar
// that used to sit above it moved to the bottom — so the answer is zero
// wherever there is a conversation at all, and one under the focus header a room
// pins above it (room.go).
func (a *app) bodyTop() int {
	if a.viewHeight() <= 0 {
		return -1
	}
	return a.topHeight()
}

// rowAt resolves a screen line to the row drawn on it.
//
// A ROOM ANSWERS ITS OWN ROWS HERE. While one is open the transcript's rows are
// not on screen — so a pointer answered from the transcript would brighten and
// expand tool calls from a conversation the person cannot see — and the room's
// rows are now the same rows, built by the same renderers from the same kind of
// entry (room.go). A call on a node's page opens the way a call in the
// conversation does, because it IS one.
func (a *app) rowAt(y int) (row, bool) {
	top := a.bodyTop()
	if top < 0 {
		return row{}, false
	}
	// AND THE ROSTER ANSWERS FOR THE BODY WHILE IT IS OVER IT, which it does
	// through its own hit-testing (task.go's [app.railEntryAt]). There is no row
	// list under it to resolve to, so the pointer gets nothing here rather than a
	// transcript row nobody can see.
	if a.railFull() {
		return row{}, false
	}
	// AND SO DOES THE NEW-CHAT START PAGE, for the same reason said about a page
	// rather than a column (chatstart.go): the conversation is not drawn under it
	// ([app.bodyRows]), and a pointer answered from a transcript nobody can see
	// would expand tool calls and open task pages belonging to the conversation
	// the person has just stepped away from. The page's own rows are the
	// greeting's, and they are hit-tested as chrome ([app.chromeAt]).
	if a.startingChat() {
		return row{}, false
	}
	// The body starts AT the top of its region and the padding falls below it
	// (see the frame's own note), so a screen row resolves by distance from the
	// top with nothing to subtract. A pointer on the slack lands past the end of
	// the row list and gets nothing, which is what it should get.
	if a.roomOpen() {
		body, _ := a.roomWindow(a.bodyWidth(), a.viewHeight())
		at := y - top
		if at < 0 || at >= len(body) {
			return row{}, false
		}
		return body[at], true
	}
	body, _ := a.window(a.bodyWidth(), a.viewHeight())
	at := y - top
	if at < 0 || at >= len(body) {
		return row{}, false
	}
	return body[at], true
}

// viewHeight is how many rows the conversation gets: everything the chrome did
// not take.
//
// The subtraction belongs here rather than at the call sites because this is
// the number every geometric question is answered from — what the frame draws,
// where the wheel lands, which row a click hit. An overlay the layout knew
// about but the hit-testing did not would deliver clicks to rows twelve lines
// from where they were drawn.
func (a *app) viewHeight() int {
	_, height := a.size()
	if body := height - a.chromeHeight() - a.topHeight(); body > 0 {
		return body
	}
	return 0
}

// topHeight is everything the frame pins ABOVE the body region: the room's focus
// header, and the task strip under it (taskstrip.go).
//
// It is one function for the reason [app.chrome] is one function: the frame
// draws these rows, the conversation is shortened by their count, and a pointer
// is resolved through them — three questions that must never be able to disagree
// about where the body starts. Neither of the two may ask [app.viewHeight] back,
// which is why both answer from the terminal's size alone.
func (a *app) topHeight() int { return a.headHeight() + a.stripHeight() }

// headHeight is what the pinned focus header costs the body region: one row
// while a room is open on a frame with the height to spare, the kin rows under
// it where there are any, and nothing otherwise (room.go).
//
// It is subtracted HERE, in the number every geometric question resolves
// through, rather than at the frame — a header the frame drew and the scrolling
// did not know about would put the room's last row under the input box.
func (a *app) headHeight() int {
	// THE TAB STRIP IS THE FIRST OF THOSE ROWS AND IS CHARGED FOR HERE, on its
	// own two floors (chattabs.go's [app.tabsHeight]): it is drawn over the
	// conversation and over every page inside it, because which conversation this
	// is stays true wherever you have walked to inside one.
	width, _ := a.size()
	head := a.tabsHeight(width)
	if a.room == nil {
		// AND THE CONVERSATION ITSELF HAS NO TRAIL ROW. `main` with nothing after
		// it is the tab above it said twice, and the emptiness law is exactly this:
		// a row that carries no news is a row that is not drawn.
		//
		// WHAT IT DOES HAVE IS THE SEAM UNDER THE TABS, which is what closes the
		// header panel off from the transcript and from the roster beside it on a
		// terminal whose background this program does not control
		// (chattabs.go's [app.chatRuleHeight]).
		return head + a.chatRuleHeight(width)
	}
	// A ROOM'S OWN ROWS ARE CHARGED FOR THROUGH THEIR OWN LADDER, which knows the
	// two floors the header stands on — too narrow for a trail and a way out, too
	// short for breathing room — and which of its two rows a short frame can
	// afford (room.go's [app.roomHeadHeight]). It is asked rather than counted off
	// a rendered slice, because this runs before anything is drawn.
	//
	// THE KIN ROWS ARE PART OF THE PINNED REGION AND ARE CHARGED FOR HERE, for
	// exactly the reason the header's own rows are: they are drawn above the body
	// by the frame, and rows the scrolling has not subtracted push the room's
	// last row under the input box. They are asked at the frame's OWN width,
	// which is the width [app.view] hands the header, so the count here and the
	// rows drawn there can never disagree (room.go's [app.roomKinRows]).
	return head + a.roomHeadHeight(width) + len(a.roomKinRows(width))
}

// scrollPage is how many rows one pgup or pgdown moves: a screenful less a line
// of overlap, so a person reading a long thing keeps one row of context across
// the jump. It was called `page` until the router took that word for a PLACE
// (pages.go); the two meanings had nothing to do with each other and one of them
// had to move.
func (a *app) scrollPage() int {
	if p := a.viewHeight() - 1; p > 1 {
		return p
	}
	return 1
}

// offsetFor resolves the scroll position for a rendered length. Sticking is
// resolved here rather than stored, so a turn that streams six lines while the
// reader is at the bottom keeps them at the bottom without anybody recomputing
// an offset per delta.
func (a *app) offsetFor(total, height int) int {
	bottom := total - height
	if bottom < 0 {
		bottom = 0
	}
	if a.stick || a.offset > bottom {
		return bottom
	}
	if a.offset < 0 {
		return 0
	}
	return a.offset
}

// scroll moves the window by delta rows and re-decides whether the reader is
// following the live edge. Reaching the bottom re-arms sticking: leaving it
// off would mean a reader who scrolled up once never sees a new reply again.
//
// AND AN UPWARD SCROLL PREFETCHES BEFORE IT RUNS OFF THE TOP. A resumed session
// draws its last tailful and nothing else, so [app.prefetchHistory] begins the
// next local page while a whole screen still remains. The gesture only changes
// an offset in already-rendered memory; the command that materializes history
// lands later on the update loop.
func (a *app) scroll(delta int) tea.Cmd {
	height := a.viewHeight()
	total := len(a.visible(a.bodyWidth()))
	at := a.offsetFor(total, height) + delta
	bottom := total - height
	if bottom < 0 {
		bottom = 0
	}
	switch {
	case at >= bottom:
		a.offset, a.stick = bottom, true
	case at <= 0:
		a.offset, a.stick = 0, false
	default:
		a.offset, a.stick = at, false
	}
	if delta < 0 {
		return a.prefetchHistory()
	}
	return nil
}

// reveal scrolls just enough to put an entry's first row on screen. It is what
// keeps ↑/↓ selection from walking off the top of the window.
func (a *app) reveal(entry int) {
	height := a.viewHeight()
	if height <= 0 {
		return
	}
	rows := a.visible(a.bodyWidth())
	at := -1
	for i, r := range rows {
		if r.entry == entry {
			at = i
			break
		}
	}
	if at < 0 {
		return
	}
	offset := a.offsetFor(len(rows), height)
	switch {
	case at < offset:
		a.offset, a.stick = at, false
	case at >= offset+height:
		a.offset, a.stick = at-height+1, false
	}
}

// clampScroll keeps the offset legal after a resize.
func (a *app) clampScroll() {
	a.offset = a.offsetFor(len(a.visible(a.bodyWidth())), a.viewHeight())
}

// resizeGrace is how long a resize is given to stop moving before the scroll is
// clamped against it. It is longer than the paint clock's tick and far shorter
// than the gap between two deliberate resizes, which is the window a DRAG lives
// in: the sizes a person sweeps through on the way to the one they want.
const resizeGrace = 80 * time.Millisecond

// resizeSettledMsg is the end of a resize burst.
type resizeSettledMsg struct{}

// resized takes one new terminal size.
//
// THE SIZE IS TAKEN IMMEDIATELY AND THE CLAMP IS THE ONLY THING DEFERRED, and
// that is the whole of this coalescing — stated here because the tempting
// version is the wrong one. Holding the WIDTH back until a drag settles would
// mean painting a frame laid out for a width the terminal no longer has, and a
// terminal that just got narrower would wrap every one of those rows itself: the
// chug would be replaced by garbage. So every size lands the moment it arrives,
// the row list rekeys on it ([app.visible]), and the frame a person sees is
// always laid out for the window they are dragging.
//
// What a burst actually cost was [app.clampScroll], which lays the WHOLE
// transcript out again to learn how many rows it has — once per message, twenty
// or thirty times across a drag, while the terminal's own renderer paints a
// handful of them. Deferring it is free of consequence because nothing reads the
// stored offset raw: [app.offsetFor] clamps every answer it gives, so a scroll
// left standing past the end of a shorter list draws the bottom of that list
// exactly as it would have. The clamp is a bookkeeping write, not a frame.
//
// A SIZE WITH NO LAYOUT STANDING BEHIND IT IS CLAMPED ON THE SPOT — the startup
// one, and the one after a rewind threw the row list away (rewind.go's
// [app.rebuildTranscript]). There is no burst to wait out at either, and a
// surface that opened with its scroll a tick behind would be one that opened
// scrolled to the wrong place.
//
// THE ROOM NEEDS NOTHING HERE. Its rows are keyed on the width they were built
// for and rebuilt lazily by [app.roomRows] when the frame asks, so a page open
// over a drag re-lays exactly as often as it is painted, which is what this
// makes true of the conversation.
func (a *app) resized(width, height int) tea.Cmd {
	// A terminal repeating a size it has already sent is a terminal saying
	// nothing, and multiplexers say it often — on every pane focus, on every
	// attach.
	if width == a.width && height == a.height {
		return nil
	}
	a.width, a.height = width, height
	a.touch()
	if a.rows == nil {
		a.clampScroll()
		return nil
	}
	if a.sizing {
		return nil
	}
	a.sizing = true
	return tea.Tick(resizeGrace, func(time.Time) tea.Msg { return resizeSettledMsg{} })
}

// follow is what every append calls: content grew, and a reader at the live
// edge stays at the live edge.
func (a *app) follow() {
	if a.stick {
		a.offset = 0 // resolved from the bottom by offsetFor
	}
}

// tier is the frame's size class. Every surface that has a compact variant of
// itself reads this ONE function rather than comparing widths on its own — a
// second breakpoint table would drift from this one within a release.
type tier int

const (
	tierWide     tier = iota // 120+: the full frame, rail column and all
	tierStandard             // 80–119: the everyday laptop frame
	tierNarrow               // 60–79: split panes, the roster already overlays
	tierPhone                // <60: a phone in a terminal — everything stacks
)

// layoutTier is the frame's size class from its width. The floors are the
// ones the surfaces already negotiate around (railSlimFloor is where the
// roster lost its column); phone is the floor a status row can no longer hold
// what it is asked to carry.
func layoutTier(width int) tier {
	switch {
	case width >= 120:
		return tierWide
	case width >= 80:
		return tierStandard
	case width >= 60:
		return tierNarrow
	default:
		return tierPhone
	}
}
