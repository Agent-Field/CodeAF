package tui3

// ASK HERE: the errand you say from home, and the record it leaves.
//
// "remind me at 6" is a sentence a person says from wherever they happen to be
// standing, and the place they are usually standing is home. It is not a
// project conversation and it is not a single question either — getting to a
// card takes a tool call or two and a sentence back and forth — so the two
// answers that were already on the screen were both wrong. Typing it and
// pressing enter started a whole conversation, and home's list filled up with
// one-off errands that were finished; a chat that was simply not stored would
// have been worse, because "why did I get this reminder?" must open the thing
// that made it (docs/AMBIENT.md Part 5).
//
// So there is a THIRD door, and this file is all of it:
//
//   - A second action row, `ask here`, drawn directly above
//     `start a new conversation` while something is typed. It is one ↑ away and
//     it is also a chord — ctrl+enter, with alt+enter as the spelling terminals
//     that cannot send the first one do send (input.go's alt+enter/ctrl+j pair
//     is the same law about the same key).
//   - A REAL session.Agent behind it, built through the door's own seam
//     ([Options.Errand]) with its transcript in a folder under the errands
//     root rather than under v3/projects — so home never lists it, and the
//     record still exists.
//   - A ROW IN THE LEFT COLUMN for every exchange, in its project's block where
//     the hot things go, wearing what it is doing right now — `working`,
//     `waiting on you`, `saved`. It is a cursor stop like any other row.
//   - The exchange itself in home's RIGHT PANE **while the cursor is on that
//     row**: the person's line, the reply as it streams, one line per tool call,
//     a live strip of what is happening this second, and the ratification card
//     when one arrives. Answering it is 1 / 2 / 3, a follow-up is typing and
//     enter, and esc puts the keyboard back on the list with the exchange still
//     alive beside it.
//
// ── AN EXCHANGE IS A ROW, AND IT OUTLIVES THE SCREEN IT WAS ASKED ON ────────
//
// It used to live on [homeView], which [app.closeHome] assigns the zero value
// to — so opening another conversation to check something ended the errand
// mid-question, and the engine answered the card nobody could see any more with
// "the card was left unanswered — nothing was saved". The exchange DIED
// because somebody looked elsewhere. So the list of them is on the APP
// ([app.exchanges]), home merely draws it, and the four rules are:
//
//	several at once   a second `ask here` ADDS one; nothing is replaced
//	the pane is the ROW's   the exchange pane is drawn only while the cursor is
//	                        on an exchange row, so every other row keeps its
//	                        ordinary card
//	it never dies of neglect   closing home, walking away, opening another
//	                           conversation — none of them touch the agent
//	it goes when it is over AND SEEN   a settled exchange is filed once its pane
//	                                   has been drawn after it settled and the
//	                                   cursor has left it ([app.sweepExchanges])
//
// WHERE THE FOLDER LIVES, AT EVERY STAGE. There is one folder and it only ever
// MOVES; nothing here copies a transcript and nothing here deletes one.
//
//	made                          <errands root>/<16-hex id>/transcript.jsonl
//	promoted to a conversation    <project bucket>/<16-hex id>/  with meta.json
//	anything else                 it stays where it was made
//
// AN AUTOMATION SAVED FROM HERE NAMES THIS FOLDER AS WHERE IT WAS ASKED
// ([automation.Origin]), which is what makes "why did I get this reminder?" a
// door: the place's `open` opens this transcript. So the folder does not move
// when something is saved — a record that moved after it was pointed at would
// be a door onto nothing.
//
// THE AGENT IS CLOSED BEFORE THE FOLDER MOVES, always. The transcript's flock
// rides the open file and a rename carries the inode with it, so a folder moved
// under a live writer would leave a lock held on a path nobody can name.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui2/tokens"
)

// The words this door says. Each is quoted in the manual exactly as it is
// spelled here (internal/manual/chat/asking-from-home.md).
const (
	// homeAskHereWord is the second action row's label, with what was typed
	// quoted after it exactly as `start a new conversation` quotes it.
	homeAskHereWord = "ask here"
	// homeAskHereGlyph marks it. A question mark, because that is what the row
	// is: the one thing on this column you ASK rather than open.
	homeAskHereGlyph = "?"
	// homeContinueWord is the row under a finished exchange that turns it into
	// an ordinary conversation, folder and all.
	homeContinueWord = "continue as a conversation"
	// homeAskUnavailableWord is what the row says where no errand seam was
	// wired — a capability that cannot work is absent, not broken, and this is
	// the honest half of that for a row that is drawn before the seam is asked.
	homeAskUnavailableWord = "this window cannot ask from home"
	// homeAskChangeWord is what the card says after `2`: the correction is
	// typed into the box, not into a second card.
	homeAskChangeWord = "type the change and press enter"
	// homeAskSavedWord is what the pane says once an automation it asked for
	// is saved, and where to find it.
	homeAskSavedWord = "saved · /automations lists it"
	// homeAskPromotedWord is the refusal for a second promotion of one folder.
	homeAskPromotedWord = "this exchange is already a conversation"
)

// What an exchange's ROW says it is doing, and what the pane says while a turn
// is in flight. They are the conversation's own words, not a second vocabulary:
// `working` is [runState.String]'s own (app.go), `waiting on you` is the phrase
// a conversation stopped on a question wears in this column (homebands.go), and
// `running` is what an unfinished call says about itself (toolview.go's
// [liveWord]).
const (
	// homeAskWorkingWord is the tail of a row whose turn is in flight, and it is
	// the same word the status line spends on the same fact.
	homeAskWorkingWord = "working"
	// homeAskWaitingWord is the tail of a row with a card up and unanswered. It
	// is the one tail on this row that takes the accent, for the reason a
	// conversation that needs somebody does: a screen whose whole job is triage
	// cannot draw its most urgent fact in the same grey as an age.
	homeAskWaitingWord = "waiting on you"
	// homeAskAnsweredTail and homeAskSavedTail are the two ways an exchange is
	// over: it came to nothing anybody has to answer, or an automation was saved.
	homeAskAnsweredTail = "answered"
	homeAskSavedTail    = "saved"
	// homeAskThinkWord is what the pane says before the first token of a turn
	// has arrived, and homeAskWriteWord once the reply is streaming. Two words
	// and not one, because "is it stuck or is it typing" is exactly the question
	// a person watching a still pane is asking.
	homeAskThinkWord = "thinking"
	homeAskWriteWord = "writing"
	// homeAskRunWord is the third of them: a call of this turn is executing, and
	// the strip under the line says which one.
	homeAskRunWord = "running"
)

// homeExchangeRow is the row kind an exchange wears in the left column.
//
// IT IS DECLARED HERE FOR [homeAskHere]'S REASON and given the next value above
// it, so neither can collide with the iota block another lane is editing. The
// name carries `Row` because [homeExchange] is the thing itself and this is its
// line on the screen — two names for two objects that must not be confused.
const homeExchangeRow homeRowKind = 201

// exchangeStripRows is how many lines of the live strip the pane shows: the
// newest two, and never a third.
//
// A STRIP AND NOT A LOG. Everything that happens is already a row above it —
// this is a WINDOW onto the moment, two lines tall, so that a follow-up typed
// into a settled exchange visibly does something before the reply lands. Two,
// because one shows a tool call and hides the reply growing under it, and three
// starts to be a second transcript in a pane forty cells wide.
const exchangeStripRows = 2

// homeAskHere is the row kind of that second action row.
//
// IT IS DECLARED HERE AND NOT IN [homeRowKind]'s OWN BLOCK, on purpose: the
// iota block in home.go is being edited by another lane in the same wave, and a
// constant appended to it would be a conflict over a line that says nothing.
// The value is far above the block's last member so the two can never collide,
// and [homeLine.stop] and [app.homeEnter] name it the way they name the rest.
const homeAskHere homeRowKind = 200

// exchangeKind is what one drawn line of the exchange is.
type exchangeKind uint8

const (
	// exchangeSaid is the person's own sentence.
	exchangeSaid exchangeKind = iota
	// exchangeReply is the model's answer, which grows while it streams.
	exchangeReply
	// exchangeTool is ONE call, on ONE dim line. The right pane is thirty to
	// fifty cells wide and the conversation's own renderer draws a cluster with
	// arguments, a clock and an expandable result in it; a reduced line that
	// says which verb ran is the honest thing to put in the space there is.
	exchangeTool
	// exchangeNote is this pane speaking for itself — a refusal, or what became
	// of the folder.
	exchangeNote
	// exchangeCard is the ratification card, IN THE TRANSCRIPT rather than
	// pinned under it.
	//
	// THE CARD STAYS, ANSWERED. It used to be a slot beside the rows that was
	// emptied the moment somebody pressed 1 or 3 — so the one thing on the pane
	// that recorded what was decided vanished at the instant it had something to
	// record, and what was left was a reply and a dim line. It is a row now, in
	// the place it arrived, and answering it SETTLES it the way the
	// conversation's own card settles (automation.go's [automationCard.verdict]):
	// the frame goes grey, the question hue goes, and the foot carries the
	// answer and what it came to. A later card from a re-proposal replaces it,
	// because two cards about one proposal would be one question asked twice.
	exchangeCard
)

// exchangeRow is one such line, before it is wrapped to a width.
type exchangeRow struct {
	kind exchangeKind
	text string
	// hint is a tool row's result gloss, added when the call comes back.
	hint string
	// done marks a tool row that has its result, so a second call of the same
	// verb opens its own row instead of overwriting the first one's.
	done bool
	// failed says the call came back an error, which is the ONE thing a tool row
	// here spends a glyph on — the conversation's own rule, where a call that
	// succeeded says so by saying nothing (toolview.go's [app.mark]).
	failed bool
	// settled says a reply row has stopped growing, and it is [entry.settled]
	// said about this pane's rows: a block nobody is writing any more is a
	// finished document, so it renders as one. It is flipped at the BOUNDARY and
	// never by looking at the text ([homeExchange.closeReply]).
	settled bool
	// edge is the live edge of an arriving reply, the same cursor a conversation
	// block walks (reveal.go). Zero means the row is not pacing.
	edge int
	// began is when a tool row's call started and took is how long it ran, so
	// the row can carry a clock while it is alive and its own figure after. They
	// are the pane's reduced reading of what a tool line in the conversation
	// says with [app.countClock] and [elapsedWord].
	began time.Time
	took  time.Duration
	// card is the proposal this row draws, for [exchangeCard]. It is the SAME
	// object [homeExchange.view] holds while it is the current one, so the row
	// and the keyboard can never disagree about what was decided.
	card *automationCard
}

// homeExchange is one errand: the agent, the folder it writes into, what has
// been said, and what the exchange came to.
//
// THE ZERO VALUE IS NOT A STATE ANYBODY REACHES. An exchange exists only when
// [app.askHere] built an agent and put it on [app.exchanges] — which is why
// every method here may assume the agent is there.
type homeExchange struct {
	agent Agent
	// view is the card as the shared renderer reads it, built once per notice
	// ([app.exchangeProposal]) and drawn by the conversation's own renderer
	// ([AutomationCardRows]), so a card met at home and a card met
	// mid-conversation are one card. nil until a card arrives.
	view *automationCard
	// dir is the folder the transcript lives in and id is its name, which is
	// also the id a promoted session keeps.
	dir string
	id  string
	// workspace is the project the errand belongs to — the one under the cursor
	// when it was asked, or the person's home directory when the cursor was on
	// no project. bucket is that project's directory under v3/projects, which
	// is where a promotion moves the folder to.
	workspace string
	bucket    string

	// box is the exchange's own line at the foot. It is NOT home's box: those
	// characters are a query over every project on the machine, and a follow-up
	// typed into them would re-filter the list underneath.
	box editor
	// chips is the tray THAT box carries, and it is the exchange's own for the
	// box's own reason: a picture dropped into a follow-up belongs to this errand
	// and not to whatever conversation the window is holding behind the screen
	// (attach.go's tray, imagepaste.go's one door). It is emptied by the send
	// that takes it.
	chips []chip
	// focused says the keyboard belongs to the exchange rather than to the
	// list. THERE ARE TWO ZONES WHILE AN EXCHANGE IS UP — the list and this pane
	// — and this bool is which of them has the hand. `tab` toggles it, esc hands
	// it to the list, a click on a row takes it to the list and a click in the
	// pane brings it back, and a yes on the card gives it to the list by itself
	// (home.go's [app.homeKey] states the whole model).
	focused bool
	// onOffer says the cursor inside the pane is on `continue as a
	// conversation` rather than in the box. ↓ puts it there and ↑ takes it back,
	// which is the grammar the action row and the matches above it already have.
	onOffer bool
	// hover says the pointer is over `continue as a conversation`. It is the
	// same reading every other row on this screen has ([homeView.hover]) and it
	// is here rather than there because the pane's rows are not the list's.
	hover bool
	// offerAt is where the last draw PUT `continue as a conversation` — an index
	// into the pane's own rows, or -1 when it is not on screen. Written by the
	// render and read by the hit-testing, so a click can never promote an
	// exchange the frame no longer offers to promote.
	offerAt int

	rows []exchangeRow
	// live is the reply being streamed into, and -1 between turns.
	live    int
	working bool
	// card is the automation card waiting for an answer, and nil when none is.
	card *session.AutomationNotice
	// askAt is where the last draw PUT the answers of that question, as pane row
	// indexes: one entry per answer, in the order the card drew them, or nil when
	// no card is on screen. Written by the render and read by the hit-testing, so
	// a click can never answer a question the frame drew somewhere else.
	//
	// EVERY ANSWER IS PRESSABLE ALONG ITS WHOLE ROW, which is the card's own
	// bargain in the conversation ([app.questionCardBody] records the same thing
	// as [questionBand]): the card puts each answer on a row of its own, so a
	// press is resolved BY ROW.
	askAt []int
	// ask is that card's QUESTION — the object the block draws and routes, held
	// here rather than on [app.questions] or on [homeView.ask].
	//
	// IT IS THE PANE'S OWN AND NOT THE BLOCK'S QUEUE, because it is about
	// ANOTHER conversation: this pane is an errand running in a folder of its
	// own, and a question about it queued above the message box would be a
	// decision offered in a window that has no way to show what it is about. It
	// is not home's card either ([app.raiseHomeAsk]), because that one is taken
	// down the moment the cursor moves off the row it is about and this one has
	// to keep asking while somebody reads their list.
	//
	// What it shares with both is everything that matters: the renderer
	// ([app.questionCardRows]) and the router ([app.questionOptionKey]), so the
	// answers, their consequences and the digits that take them are the block's
	// and not a third set.
	ask *questionShown
	// changing arms the correction: `2` on the card says the next enter is not
	// a follow-up but the person's own wording of what is wrong with it.
	changing bool

	// saved is set once an automation this exchange asked for was saved. The
	// automation names this folder as where it was asked, so it is also what
	// stops the folder being promoted out from under that name.
	saved bool
	// promoted is set once the folder became a project session.
	promoted bool
	// spoke is the first thing the person said, which is what names a promoted
	// conversation before the model has titled it.
	spoke string
	// began is when the folder was made, for a promoted session's meta.json.
	began time.Time
	// said is when the person last said something, the ordering law everywhere
	// in this codebase ([session.Meta.LastUserAt]).
	said time.Time

	// turnBegan is when the turn in flight started and turnAt is where its rows
	// begin, which is the whole of what the live strip needs to know: it is a
	// window onto THIS turn and not a second copy of the transcript.
	turnBegan time.Time
	turnAt    int
	// seen says the pane was drawn at least once after this exchange settled.
	// It is what makes filing safe to do behind somebody's back: a row that
	// disappears before anybody read what it came to is a row that took its
	// answer with it ([app.sweepExchanges] states the lifecycle whole).
	seen bool
	// filed says the agent has been closed and the folder put where it belongs.
	// It is checked rather than assumed because two doors reach it — the sweep,
	// and the window closing — and closing one agent twice is a grace period
	// spent for nothing.
	filed bool
}

// ── what an exchange IS at this moment ──────────────────────────────────────
//
// THREE STATES AND NO FOURTH, and every one of them is a thing a person can
// act on: it is waiting for you, it is working, or it is over. They are asked
// as questions rather than held as a field because each of them is already
// true of something else on the object — a card that is still a question, a
// turn in flight — and a fourth copy of a fact is a fourth thing to keep in
// step.

// waiting reports whether a card is up and unanswered. It is the state that
// sorts first everywhere on this screen.
func (ex *homeExchange) waiting() bool { return ex.asking() }

// over reports whether the exchange has nothing left to do: the card, if there
// was one, has been answered — saved or declined — and the agent is idle.
func (ex *homeExchange) over() bool { return !ex.working && !ex.asking() }

// spent reports whether the exchange may be filed: it is over AND the person
// has seen it that way. [app.sweepExchanges] adds the third condition, which is
// that they have moved off it.
func (ex *homeExchange) spent() bool { return ex.over() && ex.seen }

// closeReply ends the reply row being streamed into, and it is this pane's
// [feed.closeLive] — the same law, one column narrower.
//
// SETTLING IS A PROPERTY OF THE BOUNDARY AND NOT OF THE EVENT THAT FIRED. A
// reply stops growing for five different reasons — a call starts under it, a
// card arrives, the turn is done, the stream closes behind the turn, or the
// person says something else — and every one of those already had to put the
// live index back. So they all come through here instead, and a block cannot
// end up permanently unsettled because it finished down a lane somebody forgot
// to teach. That is exactly the shape of the defect #178 closed in the
// transcript, and it is why this is one function rather than five assignments.
//
// It is idempotent, so a turn that ends twice settles once.
func (ex *homeExchange) closeReply() { settleReply(ex.rows, &ex.live) }

// exchangeRank is the triage order of the rows: what wants you, then what is
// moving, then what is done. It is [homeState]'s law at the scale of one
// errand, and the ties under it are settled by when the exchange began.
func exchangeRank(ex *homeExchange) int {
	switch {
	case ex.waiting():
		return 0
	case ex.working:
		return 1
	default:
		return 2
	}
}

// ── the messages this lane moves on ─────────────────────────────────────────

// errandMsg is every message this file produces. It is ONE case in the app's
// update switch rather than three, because three cases in a switch another lane
// is also editing is three conflicts over one feature.
type errandMsg interface{ errand() *homeExchange }

type (
	// errandStartedMsg is what a Submit answered: the stream, or why there is
	// none.
	errandStartedMsg struct {
		ex  *homeExchange
		ch  <-chan session.Event
		err error
	}
	// errandEventMsg is one event off that stream. It CARRIES THE STREAM as well
	// as the event, because the fold has to arm the next read: a message that
	// said only what happened would be the last one this lane ever saw.
	errandEventMsg struct {
		ex *homeExchange
		ch <-chan session.Event
		ev session.Event
	}
	// errandClosedMsg is the stream ending.
	errandClosedMsg struct{ ex *homeExchange }
)

func (m errandStartedMsg) errand() *homeExchange { return m.ex }
func (m errandEventMsg) errand() *homeExchange   { return m.ex }
func (m errandClosedMsg) errand() *homeExchange  { return m.ex }

// errandUpdate folds one of those messages in.
//
// IT DOES NOT ASK WHETHER HOME IS OPEN, and that is the whole repair. It used
// to fold only into the exchange the pane happened to be drawing, so closing
// home — which is what opening another conversation does — stopped the pump
// dead and the card the person had not answered yet was answered for them.
// The only message dropped now is one for an exchange this window has already
// FILED: its agent is closed, its folder has moved, and painting into it would
// be a lane writing to a record nobody can reach.
func (a *app) errandUpdate(msg errandMsg) tea.Cmd {
	ex := msg.errand()
	if ex == nil || !a.holdsExchange(ex) {
		return nil
	}
	defer a.touch()
	// THE ROW RE-SORTS WHEN ITS STATE DOES AND NEVER OFTENER. A card arriving
	// lifts an exchange over everything else in its block and a turn finishing
	// lets it settle down again, so the column is rebuilt exactly on those two
	// moments — rebuilding on every text delta would re-walk the world once per
	// token for a row whose tail is already redrawn every frame.
	rank := exchangeRank(ex)
	defer func() {
		if a.at(pageHome) && exchangeRank(ex) != rank {
			a.home.build()
		}
	}()
	switch m := msg.(type) {
	case errandStartedMsg:
		if m.err != nil {
			ex.working = false
			ex.rows = append(ex.rows, exchangeRow{kind: exchangeNote, text: m.err.Error()})
			return nil
		}
		if m.ch == nil {
			// A steering submit: the running turn took the message and the
			// caller got a closed channel, exactly as session.Agent documents.
			return nil
		}
		return errandWait(ex, m.ch)
	case errandEventMsg:
		// The fold first, then the next read. Both, always: an event that told
		// the pane nothing still has to be followed by the one that does.
		cmd := a.errandEvent(ex, m.ev)
		return tea.Batch(cmd, errandWait(ex, m.ch))
	case errandClosedMsg:
		ex.working = false
		ex.closeReply()
	}
	return nil
}

// exchangeAnimating reports whether anything on the home screen is turning
// because an errand is working. It is what keeps the frame clock running while
// the conversation underneath is idle ([app.paint] names it among the ten).
func (a *app) exchangeAnimating() bool {
	if !a.at(pageHome) {
		return false
	}
	for _, ex := range a.exchanges {
		if ex.working {
			return true
		}
	}
	return false
}

// holdsExchange reports whether this window is still driving an exchange. A
// filed one is gone from the list, which is the same answer said once.
func (a *app) holdsExchange(ex *homeExchange) bool {
	for _, open := range a.exchanges {
		if open == ex {
			return true
		}
	}
	return false
}

// errandWait is [waitEvent] for this lane. It carries the exchange itself
// rather than a generation number, because an exchange IS its own generation:
// it is a pointer nothing else can be, and several of them are open at once.
func errandWait(ex *homeExchange, ch <-chan session.Event) tea.Cmd {
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return errandClosedMsg{ex: ex}
		}
		return errandEventMsg{ex: ex, ch: ch, ev: ev}
	}
}

// errandEvent is the reduced fold: text, tools, the card, the update that says
// an automation was saved, and nothing else.
//
// IT IGNORES MOST OF WHAT A TURN EMITS, and that is the design rather than a
// gap. Reasoning, compaction, harness offers, task proposals and the rest are
// all things the conversation's own surface draws with rows, clocks and answer
// lanes it has the width for; an errand is two sentences and a card, and a pane
// that tried to be a second transcript in forty cells would be neither.
func (a *app) errandEvent(ex *homeExchange, ev session.Event) tea.Cmd {
	switch ev.Kind {
	case session.EventTextDelta:
		if ex.live < 0 || ex.live >= len(ex.rows) {
			ex.rows = append(ex.rows, exchangeRow{kind: exchangeReply})
			ex.live = len(ex.rows) - 1
		}
		row := &ex.rows[ex.live]
		row.text += ev.Text
		catchReveal(&row.edge, row.text, len(ev.Text), isLump(len(ev.Text)), a.linear)

	case session.EventToolBegin:
		ex.closeReply()
		ex.rows = append(ex.rows, exchangeRow{
			kind: exchangeTool, text: exchangeToolLine(ev), began: a.now(),
		})

	case session.EventToolEnd, session.EventToolFailed:
		a.closeExchangeTool(ex, ev)

	case session.EventAutomationProposal:
		if ev.Automation != nil {
			a.exchangeProposal(ex, *ev.Automation)
		}

	case session.EventAutomationUpdate:
		if ev.Automation != nil {
			return a.errandUpdated(ex, *ev.Automation)
		}

	case session.EventError:
		ex.closeReply()
		ex.rows = append(ex.rows, exchangeRow{kind: exchangeNote, text: errText(ev.Err)})

	case session.EventTurnDone:
		ex.closeReply()
		ex.working = false
	}
	return nil
}

// exchangeProposal puts one automation card into the pane, in the transcript
// where it arrived.
//
// A SECOND CARD REPLACES THE FIRST and does not stack under it. The only way to
// get one is `Change…` and a correction, which is one question being asked
// again in better words — so the row the first card drew is taken out and the
// new one is appended where the conversation now is, rather than leaving a
// settled `you asked for something different` above a card that supersedes it.
func (a *app) exchangeProposal(ex *homeExchange, notice session.AutomationNotice) {
	if ex.view != nil {
		for i := range ex.rows {
			if ex.rows[i].kind == exchangeCard && ex.rows[i].card == ex.view {
				ex.rows = append(ex.rows[:i], ex.rows[i+1:]...)
				break
			}
		}
	}
	kept := notice
	ex.card, ex.changing = &kept, false
	ex.closeReply()
	// THE VIEW IS BUILT HERE AND NOT AT DRAW TIME. Whether the digits belong to
	// the card is a question the keyboard asks before any frame has been
	// painted, and a view that only existed once something had been drawn would
	// make the answer depend on the terminal having repainted.
	ex.view = &automationCard{id: kept.ID, notice: kept}
	ex.rows = append(ex.rows, exchangeRow{kind: exchangeCard, card: ex.view})
	ex.ask = a.exchangeShown(ex, kept)
}

// exchangeShown is the pane's card as the question block holds it.
//
// THE TOKEN CARRIES THE FOLDER, which is the one thing this question needs that
// the conversation's does not: an errand is a DIFFERENT session, so its
// proposal's id belongs to another id space entirely and `automation:7` here and
// `automation:7` in the window behind it would be one token for two decisions
// ([session.Question.Token] prefers the Ref).
//
// AND IT IS ANSWERED THROUGH THE CLOSURE. The errand has its own agent
// ([homeExchange.agent]) and this window's door reaches this window's engine, so
// the answer goes where the card came from ([questionShown.local]).
func (a *app) exchangeShown(ex *homeExchange, notice session.AutomationNotice) *questionShown {
	built := session.AutomationQuestion(notice)
	built.Asked = a.now()
	built.Ref = ex.id + "/" + itoa(int(notice.ID))
	// A CARD IN THIS PANE HAS NO ROW ABOVE IT THE BLOCK CAN FIND. The transcript
	// the subject would name is the errand's, drawn in the pane by the pane, so
	// the question says its own reason rather than pointing at a row
	// ([app.questionSubjectAt] would answer -1 anyway; this says so out loud).
	built.Subject = session.SubjectRef{}
	kept := notice
	shown := questionShown{
		question: built,
		local: func(answer session.Answer) tea.Cmd {
			return a.answerExchangeCard(ex, &kept, answer)
		},
		commented: func() { ex.changing = true },
	}
	// THE STAMP IS WRITTEN AT THE RAISE, which is [app.raiseHomeAsk]'s own
	// bargain: the settle guard exists to catch a key aimed at the screen a
	// quarter-second ago, and the pane redraws this card on every frame it is on
	// — so a stamp waiting for the block's own draw would never be written at
	// all, and the digits would never answer.
	shown.shown = a.now()
	return &shown
}

// answerExchangeCard is the one place the errand's own automation card is
// answered, whichever key or press gave the answer.
//
// IT SETTLES THE ROW IN THE CONVERSATION'S WORDS ([app.automationAnswered]'s),
// so a card met here and a card met mid-conversation cannot settle into two
// vocabularies, and then hands the answer to the errand's own engine.
func (a *app) answerExchangeCard(ex *homeExchange, notice *session.AutomationNotice, answer session.Answer) tea.Cmd {
	if ex.view == nil || ex.view.settled() {
		return nil
	}
	if words := strings.TrimSpace(answer.Words()); words != "" && answer.FirstKey() == "" {
		return a.changeExchangeCard(ex, notice, words)
	}
	key := answer.FirstKey()
	switch key {
	case session.AutomationSaveKey, session.AutomationRunNowKey:
		ex.settle(autoSavedWord, automationOptionLabel(*notice, key))
	case session.AutomationNoKey:
		ex.settle(autoNotSaved, "")
	default:
		return nil
	}
	sent := a.resolveExchange(ex, notice, answer)
	// AND THE KEYBOARD GOES BACK TO THE LIST on a yes and on a no alike. The
	// question is over either way, and a hand left in a pane with nothing left
	// to answer is how the arrows stop moving the column. The exchange stays
	// alive beside it — tab or a click brings it back for a follow-up.
	ex.focused, ex.onOffer = false, false
	return sent
}

// changeExchangeCard is the correction: nothing is saved, the model proposes
// again on the person's own words, and a second card arrives to replace this
// one.
func (a *app) changeExchangeCard(ex *homeExchange, notice *session.AutomationNotice, words string) tea.Cmd {
	ex.box.reset()
	ex.changing = false
	ex.settle(autoChangedWord, autoChangeWord)
	ex.rows = append(ex.rows, exchangeRow{kind: exchangeSaid, text: words})
	ex.said = a.now()
	// THE CORRECTION IS A TURN LIKE ANY OTHER from the pane's point of view: the
	// model is going to answer it, so the strip and the state line have to start
	// counting or the screen sits still while it does.
	ex.startTurn(a.now())
	return a.resolveExchange(ex, notice, session.Answer{Change: words})
}

// asking reports whether a card is up AND still a question. It is what owns
// `1`, `2` and `3`: a settled card keeps its rows and gives the digits back to
// the box, which is what a person pressing `2` in the middle of "make it 2pm"
// meant.
func (ex *homeExchange) asking() bool {
	return ex.card != nil && ex.view != nil && !ex.view.settled()
}

// settle writes the decision onto the card and leaves it exactly where it is.
//
// THE WORDS ARE THE CONVERSATION'S OWN (automation.go's verdicts), because a
// card met at home and a card met mid-conversation are one card and must not
// settle into two vocabularies.
func (ex *homeExchange) settle(verdict, answer string) {
	if ex.view == nil {
		return
	}
	ex.view.verdict, ex.view.answer = verdict, answer
	// AND THE QUESTION GOES WITH IT. A settled card is not a question, so the
	// object the pane routes keys through has to stop being there — otherwise
	// the digits would go on answering a card that already says what it came to.
	ex.ask, ex.changing = nil, false
}

// closeExchangeTool puts a call's result on the row that opened it — the newest row of
// that verb with nothing back yet — and opens a fresh row when there is none,
// so a result can never land on the wrong call.
//
// AND NOTHING HERE EVER SAYS "unknown". It used to reach for [errText] as the
// fallback gloss, and [errText] answers "unknown" for a nil error — so every
// successful call whose hint the engine left empty drew `stand · unknown`,
// which is this surface telling a person something went wrong with a call that
// went perfectly. A call that came back with nothing to say says nothing: the
// emptiness law, applied to a word rather than to a number.
func (a *app) closeExchangeTool(ex *homeExchange, ev session.Event) {
	failed := ev.Kind == session.EventToolFailed || ev.Err != nil
	// The result gloss, and on a failure the fault itself, which is the one
	// thing a person genuinely cannot read the row without.
	hint := strings.TrimSpace(ev.Hint)
	if failed && ev.Err != nil {
		hint = strings.TrimSpace(ev.Err.Error())
	}
	for i := len(ex.rows) - 1; i >= 0; i-- {
		row := &ex.rows[i]
		if row.kind != exchangeTool || row.done {
			continue
		}
		row.done, row.failed, row.hint = true, failed, hint
		if !row.began.IsZero() {
			row.took = a.now().Sub(row.began)
		}
		return
	}
	ex.rows = append(ex.rows, exchangeRow{
		kind: exchangeTool, text: exchangeToolLine(ev), hint: hint,
		done: true, failed: failed,
	})
}

// exchangeToolLine is what ONE call reads as in a pane forty cells wide: the
// tool's own name, and what it is pointed at.
//
// IT IS THE CONVERSATION'S OWN TWO-PART READING, reduced (toolview.go's
// [toolWords] and toolstat.go's [toolTarget]): the name is chrome, the target
// is the substance, and the payload is asked before the hint because session's
// hint is a one-liner built for a log column. What the row must never be is
// what it was — the bare registered name of a tool, with nothing a person can
// read beside it.
func exchangeToolLine(ev session.Event) string {
	name, target := exchangeToolWords(ev)
	if strings.TrimSpace(target) == "" {
		return name
	}
	return name + " · " + target
}

// exchangeToolWords splits one call into those two parts.
func exchangeToolWords(ev session.Event) (name, target string) {
	tool := strings.TrimSpace(ev.Tool)
	if tool == "automation" {
		// THE AUTOMATION TOOL SAYS WHAT IT IS DOING AND NOT WHAT IT IS CALLED. It
		// has no entry in either gloss table, so the engine's hint for it is the
		// bare word — and `automation` beside `automation` is a row that costs a
		// line and says nothing.
		return tool, exchangeAutomationWords(ev.Args)
	}
	name, gloss := toolWords(tool, ev.Hint)
	if target := toolTarget(tool, ev.Args, ev.Hint); target != "" {
		return name, target
	}
	return name, gloss
}

// exchangeAutomationWords is what an `automation` call is doing, in the words
// the place and the card already use. An op nobody named draws nothing, which
// is honest: the card that follows says what it was about.
func exchangeAutomationWords(args string) string {
	fields := argsOf(args)
	switch strings.ToLower(strings.TrimSpace(argString(fields, "op"))) {
	case "propose":
		if title := strings.TrimSpace(firstLine(argString(fields, "title"))); title != "" {
			return "proposing " + title
		}
		return "proposing an automation"
	case "list":
		return "reading the automations"
	case "history":
		return "reading what one did"
	case "pause":
		return "pausing one"
	case "resume":
		return "starting one again"
	case "delete":
		return "deleting one"
	case "run":
		return "running one now"
	}
	return ""
}

// errandUpdated is what an automation update does to the exchange: a line in
// the pane, and on "saved" the mark that this exchange is where something now
// on the clock was asked.
//
// AND THE KEYBOARD GOES BACK TO THE LIST. The thing they asked for now exists;
// the list is where a person goes next, and the exchange stays alive beside it
// for a follow-up that tab or a click reaches.
func (a *app) errandUpdated(ex *homeExchange, notice session.AutomationNotice) tea.Cmd {
	if text := strings.TrimSpace(notice.Text); text != "" {
		ex.rows = append(ex.rows, exchangeRow{kind: exchangeNote, text: text})
	}
	if notice.Update != "saved" || ex.saved || ex.promoted {
		return nil
	}
	// A card still asking when the thing it proposed has been saved is a
	// question nobody can answer any more, so it settles into the answer the
	// world just gave it rather than staying a live question over a decided fact.
	if ex.asking() {
		ex.settle(autoSavedWord, "")
	}
	ex.saved = true
	ex.focused, ex.onOffer, ex.changing = false, false, false
	ex.rows = append(ex.rows, exchangeRow{kind: exchangeNote, text: homeAskSavedWord})
	return nil
}

// fileExchange ENDS one exchange: the agent is closed, and the folder stays
// where it is.
//
// THE FOLDER IS THE RECORD AND IT IS NEVER REMOVED HERE. The record outlives the
// window, which is the whole reason it is a folder and not a buffer — and an
// automation saved from this exchange names it as where it was asked.
func (a *app) fileExchange(ex *homeExchange) {
	if ex == nil || ex.filed {
		return
	}
	ex.filed = true
	// A working exchange is only ever filed by the window closing, and a turn
	// left running into a closed agent's grace period is the pause a person
	// feels on the way out.
	if ex.working {
		ex.agent.InterruptFor(session.StopByLeaving)
	}
	_ = ex.agent.Close()
}

// moveExchange renames one folder onto another path, making the parent first. A
// rename is the whole of it: both ends are under the same state root, so there
// is no cross-device case to fall back from, and a copy would leave two records
// of one exchange with nothing saying which is the real one.
func moveExchange(from, to string) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	return os.Rename(from, to)
}

// ── asking ──────────────────────────────────────────────────────────────────

// errandsDir is where an exchange is made and where it stays. It is NOT under
// v3/projects, which is the whole mechanism: home lists what is in projects/,
// so an errand cannot become a row on the screen it was typed at. The field is
// the test's door and the option is the launch's; the fallback is arithmetic on
// the projects root, because `<state root>/v3/projects` and
// `<state root>/v3/errands` are siblings by construction.
func (a *app) errandsDir() string {
	if root := strings.TrimSpace(a.errandsRoot); root != "" {
		return root
	}
	return filepath.Join(filepath.Dir(a.placesRoot()), "errands")
}

// askHere is the second action row, and the chord.
//
// It mints the folder, builds an agent whose transcript is inside it, puts the
// keyboard on the exchange and sends the sentence — in that order, because each
// step is the previous one's proof. A seam that is not wired, a directory that
// cannot be made and an agent that will not open are all the same kind of
// answer: the row says why, the list is untouched, and nothing half-made is
// left on the disk.
func (a *app) askHere(text string) tea.Cmd {
	return a.askHereWith(text, ErrandOrders{})
}

// askHereWith is [app.askHere] WITH THE THREE FACTS THE COMPOSER LAYER SETTLED:
// where it runs, what its work runs on, how much it may spend (SCREEN 2e,
// composerlayer.go). The zero orders are the plain door — the project the cursor
// is on, the launch's own model binding, the launch's own rail — which is what
// every caller that never opened the layer passes.
//
// THE WORKSPACE IN THE ORDERS OUTRANKS THE CURSOR'S. A person who pressed
// `alt+w` said where this one goes, and the bucket goes with it, because a
// folder promoted into one project's bucket while its meta.json named another
// workspace would be a conversation filed under a project it says it is not in
// ([app.errandPlace] holds the whole argument).
func (a *app) askHereWith(text string, orders ErrandOrders) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	// The door was found, whatever it answers below (notice.go).
	a.noticeEvent(eventAsked)
	h := &a.home
	if a.errand == nil {
		h.say(homeAskUnavailableWord, "")
		return nil
	}
	// A NARROW WINDOW NO LONGER REFUSES. It used to: the exchange WAS the right
	// pane, and a frame with no second column ([homeColumns]) had nowhere to
	// draw one — so the sentence was taken, a session opened, and none of it
	// shown, which is the one failure worse than saying no. The answer is the
	// phone's own pattern instead of a refusal: on a narrow frame the pane is
	// STACKED, taking the whole screen while it holds the keyboard, with the
	// list one esc away and the row still standing on it (home.go's
	// [app.homeStacked]).
	workspace, bucket := a.errandPlace()
	if chosen := strings.TrimSpace(orders.Workspace); chosen != "" && chosen != workspace {
		workspace, bucket = chosen, a.errandBucketOf(chosen)
	}
	id := session.NewSessionID()
	dir := filepath.Join(a.errandsDir(), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		h.say(err.Error(), "")
		return nil
	}
	orders.Dir, orders.Workspace = dir, workspace
	agent, err := a.errand(orders)
	if err != nil {
		// The folder is left behind on purpose: it is empty, it carries the
		// sweep's own TTL, and removing a directory after a failure is how a
		// bug in one lane deletes another lane's evidence.
		h.say(err.Error(), "")
		return nil
	}
	now := a.now()
	ex := &homeExchange{
		agent: agent, dir: dir, id: id,
		workspace: workspace, bucket: bucket,
		focused: true, live: -1, working: true,
		offerAt: -1,
		spoke:   text, began: now, said: now,
		turnBegan: now,
	}
	// AND HOME'S TRAY COMES WITH THE SENTENCE, because the sentence came out of
	// home's box and the pictures were dropped into it (imagepaste.go). They move
	// rather than being copied: two trays holding one file would be two answers
	// to what the next message carries, which is the law attach.go states about
	// there being one tray and not two.
	ex.chips, a.home.chips = a.home.chips, nil
	a.home.carrying = false
	text = errandSentence(text, ex.chips)
	ex.rows = append(ex.rows, exchangeRow{kind: exchangeSaid, text: text})
	// A SECOND `ask here` ADDS ONE. It used to close the first, because the pane
	// held one — and what that meant in a person's hands was that asking a
	// second thing killed the first question before they had answered it. They
	// are rows now, and a column holds as many rows as somebody asks for.
	a.exchanges = append(a.exchanges, ex)
	// THE BOX IS CLEARED AND THE LIST GOES BACK TO ITS RESTING SHAPE. The words
	// are in the exchange now; leaving them in the box would keep the drop-up up
	// and keep filtering the column behind a pane nobody is reading it through.
	h.box.reset()
	a.showExchanges()
	// AND THE CURSOR LANDS ON THE ROW THAT WAS JUST MADE, with the pane focused
	// — which is exactly the first experience this door always had, now said in
	// the vocabulary the column uses for everything else. The pane is about the
	// row under the cursor, so a cursor left where it was would have opened an
	// exchange and shown a preview of something else.
	h.pointExchange(ex)
	a.touch()
	return tea.Batch(errandSend(ex, text), a.wake())
}

// errandSend is one Submit, off the update loop for the reason [app.submit] is:
// building a request is not instant and a surface that waited for it would drop
// a frame at the exact moment a person is watching for one.
func errandSend(ex *homeExchange, text string) tea.Cmd {
	// THE TRAY IS TAKEN HERE AND NOT INSIDE THE COMMAND, for [app.submitImages]'
	// reason: what the message carries is decided at the moment enter was
	// pressed, and a tray read on the other side of the loop is a tray somebody
	// may have added to while the request was being built.
	chips := ex.chips
	ex.chips = nil
	return func() tea.Msg {
		if len(chips) == 0 {
			ch, err := ex.agent.Submit(errandContext(), text)
			return errandStartedMsg{ex: ex, ch: ch, err: err}
		}
		// THE FILES ARE READ OFF THE LOOP, and the pictures travel as bytes while
		// an ordinary file travels as the path it already has — which is exactly
		// what a conversation does with the same tray, because an errand runs on
		// this machine and the file is already on the engine's own disk
		// (attach.go's header holds the whole law).
		images, err := readAttachments(pictureChips(chips))
		if err != nil {
			return errandStartedMsg{ex: ex, err: err}
		}
		spoken := text
		if files := fileChips(chips); len(files) > 0 {
			spoken = remote.AttachedSentence(text, chipPaths(files))
		}
		ch, err := ex.agent.SubmitImage(errandContext(), spoken, images)
		return errandStartedMsg{ex: ex, ch: ch, err: err}
	}
}

// errandSentence is what an errand carrying pictures actually says, and it is
// [imageSentence] with the tray's files left out of the count: a file carries no
// `[image #n]` in the words, because the model is told its path (attach.go's
// [chipLabels] states the same rule about the chip itself).
func errandSentence(text string, chips []chip) string {
	return imageSentence(text, pictureChips(chips))
}

// errandPlace is which project an errand belongs to, and which bucket a
// promotion of it would land in.
//
// It is docs/AMBIENT.md Part 5's proposal in both of its halves. THE PROJECT
// UNDER THE CURSOR is the first: walking ↑ onto a project's row and pressing the
// chord is how somebody says "this one", and it is the same gesture that already
// means "this one" everywhere else on this column. AND THE `elsewhere` LIMIT IS
// THE SECOND: while something is typed the cursor rests on an action row, which
// belongs to no project — and the honest answer there is the project THIS WINDOW
// is in, because it is the only one this window can open anything in. A window
// standing in no project at all falls through to the person's home directory,
// which is where an automation that belongs to no project runs
// ([automation.Automation.Workspace]).
//
// THE BUCKET AND THE WORKSPACE ALWAYS AGREE, and that is why they are answered
// together rather than in two places. A folder promoted into one project's
// bucket while its meta.json named another workspace would be a conversation
// filed under a project it says it is not in — and home groups by the bucket and
// names the project from the meta, so the row would argue with its own heading.
func (a *app) errandPlace() (workspace, bucket string) {
	if line, ok := a.home.focusedLine(); ok && line.dir != "" {
		if path := a.errandWorkspaceOf(line.dir); path != "" {
			return path, line.dir
		}
	}
	if here := a.home.bucket; here != "" {
		if path := a.errandWorkspaceOf(here); path != "" {
			return path, here
		}
		if path := strings.TrimSpace(a.workspace); path != "" {
			return path, here
		}
	}
	return a.errandHome, a.home.bucket
}

// errandBucketOf is the bucket one workspace's conversations are filed in, and
// "" for a workspace home has never seen. It is [app.errandWorkspaceOf] read the
// other way round, and it exists so that a destination a person chose on the
// composer layer carries its own bucket rather than the one the cursor happened
// to be resting on.
func (a *app) errandBucketOf(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	for _, project := range a.home.world.Projects {
		if strings.TrimSpace(project.Path) == workspace {
			return project.Dir
		}
	}
	return ""
}

// errandWorkspaceOf is the real workspace one bucket recorded, and "" for a
// bucket nothing named one for. It is read off the world in hand rather than off
// the disk: the same reading the rows were drawn from is the one the cursor is
// pointing into.
func (a *app) errandWorkspaceOf(dir string) string {
	for _, project := range a.home.world.Projects {
		if project.Dir == dir {
			return strings.TrimSpace(project.Path)
		}
	}
	return ""
}

// errandHomeDir is the `~` project: a reminder belongs to no repository, and
// the person's own home directory is where an automation that belongs to no
// project runs ([automation.Automation.Workspace] says the same). A process
// with no home directory falls back to where it is standing, which is the last
// ABSOLUTE answer there is — and the answer has to be absolute, because it is
// written into an automation's own `Workspace` and read back by another process
// with another working directory, the clock that runs it. `.` would name
// wherever that process stands, and it would draw as a literal `.` where the
// composer's line draws `~`.
//
// IT IS READ ONCE, AT `open`, AND HELD ON [app.errandHome]. The frame reaches
// the answer — `a.composerOpensAt` names where a send lands on every paint —
// and a syscall there is one syscall per frame, which is the law in
// framedisk_law_test.go. This function is what `open` calls to learn the fact;
// nothing a paint can reach calls it, and the law is what says so.
func errandHomeDir() string {
	if dir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(dir) != "" {
		return dir
	}
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return "."
}

// forgetExchange takes one exchange off the list and off the column. It is the
// half of ending an exchange that is about the SCREEN; [app.fileExchange] is
// the half that is about the disk, and every caller does both.
func (a *app) forgetExchange(ex *homeExchange) {
	for i, open := range a.exchanges {
		if open != ex {
			continue
		}
		a.exchanges = append(a.exchanges[:i], a.exchanges[i+1:]...)
		break
	}
	a.showExchanges()
}

// showExchanges hands the column the list it draws rows from. The app owns the
// exchanges — they outlive home ([homeView] is assigned the zero value when the
// screen closes) — and this is the one line that keeps the two in step, called
// wherever the list itself changes rather than on every frame.
func (a *app) showExchanges() {
	a.home.exchanges = a.exchanges
	if a.at(pageHome) {
		a.home.build()
	}
}

// sweepExchanges files the exchanges that are over, and it is the whole answer
// to "when does one go away?".
//
// THREE THINGS HAVE TO BE TRUE, and each of them is a way of saying that
// nothing disappears out from under somebody:
//
//  1. IT IS OVER. The card, if there was one, has been answered — stood, once,
//     or declined — and no turn is in flight. A working exchange and one
//     holding a question are never swept, whatever the cursor is doing.
//  2. THEY HAVE SEEN IT THAT WAY. The pane was drawn at least once after it
//     settled ([app.exchangePane] sets the flag), so the answer the exchange
//     came to was on the screen before the row that carried it went.
//  3. THEY HAVE MOVED OFF IT. The row under the cursor is never swept — the
//     pane a person is reading does not vanish while they are reading it.
//
// And an exchange that STOOD leaves an ordinary item row on this same screen,
// under the same project, so the row going is not the fact going.
func (a *app) sweepExchanges() {
	var filed []*homeExchange
	keep := a.paneExchange()
	for _, ex := range a.exchanges {
		if ex != keep && ex.spent() {
			filed = append(filed, ex)
		}
	}
	if len(filed) == 0 {
		return
	}
	for _, ex := range filed {
		a.fileExchange(ex)
		a.forgetExchange(ex)
	}
	a.touch()
}

// fileEveryExchange is the window closing: everything still open is ended, the
// way the conversation's own agent is ([app.quit]). An errand is a session with
// a lock on a transcript, and a lock held by a process that has gone is a
// conversation nobody can reopen.
func (a *app) fileEveryExchange() {
	for _, ex := range a.exchanges {
		a.fileExchange(ex)
	}
	a.exchanges = nil
	a.home.exchanges = nil
}

// ── the keyboard, while the exchange holds it ───────────────────────────────

// exchangeKey routes one keypress into the pane. It is modal in the small way
// the pane is small: the list underneath keeps every row it had and gets the
// keyboard back on tab or esc, with the exchange still standing beside it.
//
// AND IT NEVER HOLDS THE KEYBOARD HOSTAGE. `tab` and `esc` both leave from
// every state this pane has — the box, the offer row, a card, a half-written
// correction — because a pane that had one way out and a state that did not
// offer it is exactly the trap somebody reports as "stuck".
func (a *app) exchangeKey(ex *homeExchange, msg tea.KeyPressMsg) tea.Cmd {
	if ex == nil {
		return nil
	}
	defer a.touch()
	// THE CARET'S OWN CHORDS FIRST, from the one vocabulary every box on this
	// surface shares (editkeys.go). A follow-up typed into the pane is a
	// sentence like any other, and the word jump it answers has to be the word
	// jump the message box answers.
	if editorMotion(&ex.box, msg.String()) {
		return nil
	}
	// AND ctrl+z TAKES BACK WHAT WAS TYPED, in every box on this surface and not
	// only in the message one (editundo.go).
	if editorUndo(&ex.box, msg.String()) {
		return nil
	}
	if editorWordKill(&ex.box, msg.String()) {
		return nil
	}
	switch msg.String() {
	case "tab":
		// THE ZONE TOGGLE, and it is unconditional. Whatever is half-typed and
		// whichever row the pane's own cursor is on, tab hands the keyboard to
		// the list and leaves all of it standing to come back to.
		ex.focused = false
		return nil

	case "esc":
		// ONE LAYER AT A TIME, home's own rule: a half-typed follow-up is
		// cleared first and the second esc hands the keyboard back. NEITHER
		// CLOSES THE EXCHANGE — it stands as a row on the column, which on a
		// narrow frame is also how the list comes back over the stacked pane.
		if !ex.box.empty() {
			ex.box.reset()
			return nil
		}
		ex.focused, ex.onOffer, ex.changing = false, false, false
		return nil

	case "1", "2", "3", session.DeclineKey, questionCommentKey:
		// THE CARD OWNS ITS OWN DIGITS WHILE IT IS STILL A QUESTION, and it is
		// the BLOCK'S router that says which they are ([app.questionOptionKey],
		// [app.questionVerbKey]) — one grammar, whether the card is met here or
		// in a conversation. Settled, or with no card up at all, they fall
		// through to the box below and are typed, which is what a person pressing
		// `2` in the middle of "make it 2pm" meant.
		//
		// `0` IS THE DECLINE AND IN THIS PANE IT IS THE ONLY ONE THERE IS. In a
		// conversation `esc` is *later*; here esc is the one-layer undo that
		// hands the keyboard back to the list, and a card left standing on the
		// column is not an answer.
		//
		// AND `c` ARMS THE CORRECTION, which is what `2 change when or where`
		// used to be — a chip that asked for the box rather than an answer. It is
		// the same key on every question this surface asks.
		if ex.asking() && ex.ask != nil {
			if cmd, took := a.questionOptionKey(*ex.ask, msg.String()); took {
				return cmd
			}
			if cmd, took := a.questionVerbKey(*ex.ask, msg.String()); took {
				return cmd
			}
			// AND A KEY WITH NO ANSWER UNDER IT IS INERT RATHER THAN TYPED: the
			// card owns its digits while it is asking, and a stray `3` left in
			// the box would turn the next `1` into a correction instead of a yes.
			return nil
		}

	case "shift+enter":
		ex.box.insert("\n")
		ex.onOffer = false
		return nil

	case "enter":
		return a.exchangeEnter(ex)

	case "down", "ctrl+n":
		if ex.offering() {
			ex.onOffer = true
		}
		return nil
	case "up", "ctrl+p":
		ex.onOffer = false
		return nil

	case "backspace":
		ex.box.deleteBackward()
		return nil
	case "ctrl+u":
		ex.box.killToStart()
		return nil
	case "ctrl+k":
		ex.box.killToEnd()
		return nil
	case "ctrl+w":
		ex.box.deleteWord()
		return nil
	case "left", "ctrl+b":
		ex.box.left()
		return nil
	case "right", "ctrl+f":
		ex.box.right()
		return nil
	}
	if text := msg.Key().Text; text != "" {
		at := ex.box.cursor
		ex.box.insert(text)
		// TYPING LEAVES THE OFFER ROW. The box is where characters go, and a
		// person who starts typing has said which of the two things under the
		// pane they meant.
		ex.onOffer = false
		// AND A DROP TYPED IN CHARACTER BY CHARACTER IS WATCHED FOR HERE, on the
		// pane's own box and the pane's own tray, exactly as the draft and
		// home's line watch on theirs (dropkeys.go).
		return a.dropWatch(&ex.box, &ex.chips, at, text)
	}
	return nil
}

// exchangeEnter is what enter means in the pane, and it means exactly one of
// three things depending on what is on screen.
func (a *app) exchangeEnter(ex *homeExchange) tea.Cmd {
	// A DROP THE FOLD IS STILL HOLDING IS SPENT BEFORE THE LINE IS READ, which
	// is input.go's law about enter said at the third of this surface's send
	// doors: two frames of quiet is not a wait a person owes before pressing
	// enter (dropkeys.go).
	a.spendDrop()
	text := strings.TrimSpace(ex.box.String())
	switch {
	case ex.changing:
		// The correction the card asked for, which is the question answered in
		// WORDS — so it goes through the question's own door and settles the row
		// exactly as a digit would ([app.answerExchangeCard]). Nothing is created
		// on a change: the model re-proposes and a second card arrives, which
		// replaces this one, and until it does this one stands in the transcript
		// wearing what was asked of it.
		if text == "" || ex.ask == nil {
			return nil
		}
		return a.answerQuestion(*ex.ask, session.Answer{Change: text})
	case ex.onOffer:
		return a.promoteExchange(ex)
	case text == "" && len(ex.chips) == 0:
		// A FULL TRAY IS A MESSAGE, which is input.go's law about enter said here:
		// an empty line with a picture on it is not an empty message.
		return nil
	}
	ex.box.reset()
	// AND A FOLLOW-UP CARRIES WHAT WAS DROPPED INTO IT, by the same door and the
	// same numbering the first sentence used ([errandSentence]).
	text = errandSentence(text, ex.chips)
	ex.rows = append(ex.rows, exchangeRow{kind: exchangeSaid, text: text})
	ex.said = a.now()
	ex.startTurn(a.now())
	return tea.Batch(errandSend(ex, text), a.wake())
}

// startTurn is the pane admitting that something is now happening, and it is
// called on the SUBMIT rather than on the first event back.
//
// THE SIGNAL HAS TO EXIST BEFORE THE ANSWER DOES. A follow-up typed into a
// settled exchange used to draw the sentence and then nothing at all until the
// first token arrived seconds later — which is a person pressing enter and
// watching a still screen, the exact complaint this lane exists to answer. The
// state word, the clock and the strip all hang off these three fields, so they
// are set the moment the words leave the box.
func (ex *homeExchange) startTurn(now time.Time) {
	ex.working = true
	ex.closeReply()
	ex.turnBegan, ex.turnAt = now, len(ex.rows)
	// A row settled by the last turn has been seen; a new turn is a new thing to
	// see, and an exchange swept while it is answering would be the pane going
	// out from under the person who typed into it.
	ex.seen = false
}

// resolveExchange hands one answer back to the errand's engine. An agent that
// cannot be answered is one that could not have drawn a card in the first place
// — so the type assertion failing is unreachable through anything a person can
// do, and answering nothing is the right thing to do with a card that came from
// nowhere.
func (a *app) resolveExchange(ex *homeExchange, card *session.AutomationNotice, answer session.Answer) tea.Cmd {
	if card == nil {
		return nil
	}
	door, ok := ex.agent.(questionResolver)
	if !ok {
		return nil
	}
	// THE ANSWER IS ADDRESSED IN THE ERRAND'S OWN NUMBERS. The Ref the pane gave
	// the question is this window's way of telling two sessions' ids apart; the
	// errand's engine knows its card by kind and id alone.
	answer.Kind, answer.ID, answer.Ref = session.QuestionAutomation, card.ID, ""
	// FROM A COMMAND, NEVER FROM THE LOOP (offloop.go): the pane's agent is in
	// another process exactly as the conversation's is.
	return a.offLoop(func() func(bool) tea.Cmd {
		_ = door.ResolveQuestion(answer)
		return nil
	})
}

// offering reports whether `continue as a conversation` is on the pane: the
// first reply has landed, and the folder has not already gone somewhere.
func (ex *homeExchange) offering() bool {
	if ex.saved || ex.promoted {
		return false
	}
	for _, row := range ex.rows {
		if row.kind == exchangeReply && strings.TrimSpace(row.text) != "" {
			return true
		}
	}
	return false
}

// ── promotion: the errand that turned out to be a conversation ──────────────

// promoteExchange turns the exchange into an ordinary session and opens it.
//
// IT IS A MOVE AND A meta.json AND NOTHING ELSE. The folder already holds
// everything a session folder holds — the transcript is the record — so what a
// project session has that an errand does not is a place in a bucket and an
// identity file a picker can read without opening the journal (place.go). Home
// then opens it through the same door a session row opens through, because two
// arrangements for one act would be two things to keep in step.
func (a *app) promoteExchange(ex *homeExchange) tea.Cmd {
	h := &a.home
	if ex.promoted || ex.saved {
		h.say(homeAskPromotedWord, "")
		return nil
	}
	if strings.TrimSpace(ex.bucket) == "" {
		h.say(homeElsewhereWord, "")
		return nil
	}
	if ex.working {
		ex.agent.InterruptFor(session.StopByLeaving)
	}
	// THE AGENT CLOSES BEFORE THE FOLDER MOVES (this file's header says why),
	// and the close failing is not a reason to keep somebody out of their own
	// conversation: the transcript is flushed on every line, so what is lost is
	// a buffered tail and not the record.
	if err := ex.agent.Close(); err != nil {
		h.say(err.Error(), "")
	}
	dest := filepath.Join(ex.bucket, ex.id)
	if err := moveExchange(ex.dir, dest); err != nil {
		h.say(err.Error(), "")
		return nil
	}
	ex.dir, ex.promoted = dest, true
	// The write is not checked for the reason chatv3_layout.go's mint does not
	// check it: meta.json is a citation and not the record, and a conversation
	// that would not open because a lookup file could not be written would be
	// the wrong trade twice over.
	_ = session.SaveMeta(dest, session.Meta{
		ID:         ex.id,
		Title:      exchangeTitle(ex.spoke),
		Workspace:  ex.workspace,
		LaunchDir:  ex.workspace,
		Created:    ex.began,
		LastUserAt: ex.said,
	})
	transcript := filepath.Join(dest, "transcript.jsonl")
	// The exchange is now a conversation, so it stops being a row: its folder
	// has already moved and its agent is already closed, which is everything
	// [app.fileExchange] would have done.
	ex.filed = true
	a.forgetExchange(ex)
	cmd, refusal := a.openSession(Session{
		Title: exchangeTitle(ex.spoke),
		File:  transcript,
		At:    ex.said,
	})
	if refusal != "" {
		// Home keeps the refusal itself, exactly as [app.homeEnter] does: a
		// sentence about a door belongs on the screen the door is on.
		h.say(refusal, "")
		return nil
	}
	a.closeHome()
	return cmd
}

// exchangeTitleCut is how much of the first sentence names a promoted
// conversation. It is the picker's own comfortable row width and no more: a
// title is a thing you recognize, not a thing you read.
const exchangeTitleCut = 60

// exchangeTitle is what the promoted conversation is called until the model
// names it: the first line of what the person said, cut. A multi-line errand is
// its first line, because the rest of a paste is not a name.
func exchangeTitle(said string) string {
	line := strings.TrimSpace(said)
	if cut := strings.IndexByte(line, '\n'); cut >= 0 {
		line = strings.TrimSpace(line[:cut])
	}
	runes := []rune(line)
	if len(runes) > exchangeTitleCut {
		return strings.TrimSpace(string(runes[:exchangeTitleCut])) + "…"
	}
	return line
}

// ── the drawing ─────────────────────────────────────────────────────────────

// exchangePane is the exchange under the cursor drawn whole: the conversation
// itself, the card, the live strip while a turn is in flight, and the offer row
// under them.
//
// IT IS THE PANE OF ONE ROW AND NOT OF THE SCREEN. It used to be drawn for as
// long as an exchange existed, which meant that setting one reminder took the
// right-hand column hostage — every other row on the list lost its preview
// until home was closed. It is a card about the row under the cursor now, like
// every other card in this column ([app.homeDetail]).
//
// IT IS A TAIL AND NOT A CARD, though. The preview beside a session row is
// assembled top-down and drops whole bands off the bottom ([homeBands]) because
// it is a description of something that already happened; this is a
// conversation happening now, so it keeps the LAST rows that fit — the newest
// thing said is the thing being read.
func (a *app) exchangePane(ex *homeExchange, width, room int, pal palette) []string {
	if ex == nil || width <= 0 || room <= 0 {
		return nil
	}
	// SEEING IT SETTLED IS WHAT LETS IT GO. The sweep files an exchange that is
	// over only once its pane has been drawn that way, so the row cannot vanish
	// before the answer it came to was on somebody's screen
	// ([app.sweepExchanges]).
	if ex.over() {
		ex.seen = true
	}
	// The hit targets are rebuilt with the rows that carry them, and cleared
	// first: a stale offer row is a click that promotes an exchange the frame
	// no longer offers to promote (hover.go states the law).
	ex.offerAt, ex.askAt = -1, nil
	var out []string
	out = append(out, pal.bold(pal.ink(fit(homeAskHereWord, width))))
	out = append(out, "")
	for _, row := range ex.rows {
		if row.kind == exchangeCard {
			if row.card == nil {
				continue
			}
			out = append(out, AutomationCardRows(a, row.card, width, true)...)
			if row.card == ex.view && ex.ask != nil {
				// AND THE QUESTION DIRECTLY UNDER THE CARD IT IS ABOUT, drawn by
				// the block's own renderer. It is not pinned above a message box
				// here because there is no message box in this pane that belongs
				// to this errand's conversation — the card and its answers are one
				// thing on the column, and separating them would put a decision on
				// a different part of the screen from the thing being decided.
				at := len(out)
				out = append(out, a.questionPanelBody(*ex.ask, width)...)
				ex.askAt = make([]int, len(ex.ask.question.Options))
				for i := range ex.askAt {
					ex.askAt[i] = -1
				}
				for _, band := range a.questionBands {
					if band.at >= 0 && band.at < len(ex.askAt) {
						ex.askAt[band.at] = at + band.row
					}
				}
			}
			out = separated(out)
			continue
		}
		out = append(out, a.exchangeRowLines(row, width, pal)...)
	}
	if ex.changing {
		out = append(out, pal.accent(fit(homeAskChangeWord, width)))
	}
	if ex.working {
		out = append(out, a.exchangeLive(ex, width, pal)...)
	}
	if ex.offering() {
		out = separated(out)
		ex.offerAt = len(out)
		out = append(out, overlayRow(homeStartGlyph+" "+homeContinueWord, "",
			ex.focused && ex.onOffer, false, ex.hover, width, pal))
	}
	// A BLANK LAST ROW IS A ROW OF THE PANE SPENT ON NOTHING, and the pane is
	// short. The separators between what was said belong BETWEEN things, so the
	// ones the last thing left behind it come off before the tail is measured.
	for len(out) > 0 && strings.TrimSpace(ansi.Strip(out[len(out)-1])) == "" {
		out = out[:len(out)-1]
	}
	if len(out) > room {
		// THE TAIL IS TAKEN AND THE TARGETS MOVE WITH IT. A row scrolled off the
		// top goes negative, which is the same answer as "not on screen" — a
		// target left at its pre-cut index would be a click answering whatever
		// happens to be drawn there now.
		cut := len(out) - room
		out = out[cut:]
		ex.offerAt -= cut
		for i := range ex.askAt {
			ex.askAt[i] -= cut
		}
	}
	return out
}

// exchangeRowLines draws one row of the exchange at a width.
//
// THE THREE SHAPES ARE THE TRANSCRIPT'S THREE, reduced but never renamed: what
// the person said is flush left and bright, what the model said is flush left
// and plain, and what it DID is indented and dim — which is THE INDENT LAW the
// conversation's own renderer keeps (render.go), kept here at one column
// instead of two because the pane has forty cells and not a hundred.
//
// ── AND THE ANSWER IS MARKDOWN, THROUGH THE SURFACE'S ONE RENDERER ──────────
//
// A model answers in markdown whether or not anyone asked it to, so a settled
// reply drawn as wrapped plain text is a pane showing `**bold**`, `## heading`
// and backticks to a person who asked a question from home — the same output
// that reads as prose two keystrokes away in a conversation. So it goes through
// [app.renderMarkdown], which is the door render.go's [app.settledMarkdown]
// opens for the transcript: one parser, one styler, one measure, one answer to
// what a sixteen-colour terminal may draw. Adapting it costs a line; a second
// renderer here would be a second heading ladder drifting from the first
// (markdown.go's own header says why).
//
// THE NARROW COLUMN IS ALREADY ANSWERED BY THAT DOOR. The pane is thirty to
// fifty cells, which is [tierPhone], and [phoneMarkdown] is the tier that wraps
// a fence instead of cutting it and stacks a table instead of fitting it —
// written for exactly this case, a reader with no horizontal scroll.
//
// A REPLY STILL ARRIVING STAYS PLAIN, and that is the transcript's behaviour
// rather than a shortcut: render.go's [app.liveTail] wraps the growing edge
// without parsing it, because markdown of a half-written sentence re-flows
// under the reader's eye. This pane has no promotion throttle — the head that
// chat promotes every [markdownThrottle] is a few hundred bytes here, and the
// whole block formats the instant the boundary settles it.
//
// It memoizes nothing, for [renderMarkdown]'s own reason: the pane redraws on a
// growing reply, and a cache keyed on text that is still growing is wrong at
// exactly the moment somebody is reading it. The one exchange under the cursor
// is the only one drawn ([app.paneExchange]), so the cost is bounded by a pane.
func (a *app) exchangeRowLines(row exchangeRow, width int, pal palette) []string {
	var out []string
	switch row.kind {
	case exchangeSaid:
		for i, wrapped := range wrap(row.text, width-2) {
			mark := "› "
			if i > 0 {
				mark = "  "
			}
			out = append(out, pal.muted(mark)+pal.ink(wrapped))
		}
	case exchangeReply:
		if row.settled {
			// prose paints its own rows, body ink included, so nothing here may
			// wrap them in a second foreground.
			out = append(out, trimBlanks(a.renderMarkdown(row.text, width))...)
			break
		}
		for _, wrapped := range wrap(revealedText(row.text, row.edge, row.settled), width) {
			out = append(out, pal.ink(wrapped))
		}
	case exchangeTool:
		out = append(out, a.exchangeToolLine(row, row.hint, width, pal))
	case exchangeNote:
		for _, wrapped := range wrap(row.text, width) {
			out = append(out, pal.dim(wrapped))
		}
	}
	if len(out) > 0 {
		out = append(out, "")
	}
	return out
}

// exchangeToolLine draws one call: its mark, what it is doing, whatever came
// back, and its clock.
//
//	⠹ bash · go test ./…               running, with its age once it has one
//	  bash · go test ./… · 2.4s        finished, quietly
//	✗ stand · proposing … · no store   failed, loudly
//
// THE MARKS ARE THE CONVERSATION'S OWN (toolview.go's [app.mark]) and there are
// only two of them: the braille spinner while a call is executing, and the
// failure glyph when one did not. A call that succeeded says so by saying
// nothing — the cell is a space, so the words underneath stay in one column.
func (a *app) exchangeToolLine(row exchangeRow, hint string, width int, pal palette) string {
	word := row.text
	if hint = strings.TrimSpace(hint); hint != "" {
		word += " · " + hint
	}
	if clock := a.exchangeClock(row); clock != "" {
		word += " · " + clock
	}
	return a.exchangeToolMark(row, pal) + " " + pal.dim(fit(word, width-2))
}

// exchangeToolMark is that one cell.
func (a *app) exchangeToolMark(row exchangeRow, pal palette) string {
	switch {
	case row.failed:
		return pal.bad(pal.badGlyph())
	case row.done:
		return " "
	default:
		return pal.muted(a.exchangeSpin())
	}
}

// exchangeClock is a call's one figure of time: its age while it runs, its own
// duration once it is over, and NOTHING under the floors either of those keep
// (toolview.go's [countUpFloor] and [elapsedFloor]). Two spellings for two
// questions, exactly as a tool line in the conversation has.
func (a *app) exchangeClock(row exchangeRow) string {
	if row.done {
		return tookWord(row.took)
	}
	if row.began.IsZero() {
		return ""
	}
	return countUpWord(a.now().Sub(row.began))
}

// exchangeSpin is the one moving glyph this pane spends, on the house grid so
// it never beats against the spinners elsewhere on the screen ([spinnerStep]).
// Linear mode and a terminal with no braille both get the still `*`, for the
// reason every spinner here does: a claim made thirty times a second is heard
// thirty times a second by a surface being read aloud.
func (a *app) exchangeSpin() string {
	if a.linear || a.pal.ascii {
		return glyphRunASCII
	}
	return tokens.Spinner(a.paints / spinnerStep)
}

// ── the live block: what is happening RIGHT NOW ─────────────────────────────
//
//	⠹ thinking · 4s          the turn has started and nothing has come back
//	⠹ writing · 12s          the reply is streaming
//	⠹ running · 12s          a call is executing, and the strip says which
//	  ⠹ bash · go test ./…
//	  read · session.go · 0.3s
//
// THE COMPLAINT THIS ANSWERS, VERBATIM: "I am unable to see what's happening —
// no waiting or thinking or any UI response to know something is happening."
// The pane drew a single dim `…` for the whole of a turn, which is the same
// character a stalled surface would draw and the same character it drew four
// seconds and four minutes in. So it says WHAT it is doing and FOR HOW LONG, in
// the conversation's own words and on the conversation's own clock.
//
// AND THE STRIP UNDER IT IS A WINDOW, NOT A SECOND RECORD. Everything in it is
// already a row above; what it adds is that the newest two lines of THIS turn
// are always at the foot, against the box, where somebody who just pressed
// enter is looking — which is what a follow-up into a settled exchange had
// none of.

// exchangeLive is that block: the state line, and the strip under it.
func (a *app) exchangeLive(ex *homeExchange, width int, pal palette) []string {
	out := []string{a.exchangeStateLine(ex, width, pal)}
	return append(out, a.exchangeStrip(ex, width, pal)...)
}

// exchangeStateLine is the spinner and the word.
func (a *app) exchangeStateLine(ex *homeExchange, width int, pal palette) string {
	word := homeAskThinkWord
	switch {
	case ex.running():
		word = homeAskRunWord
	case ex.writing():
		word = homeAskWriteWord
	}
	if !ex.turnBegan.IsZero() {
		if clock := countUpWord(a.now().Sub(ex.turnBegan)); clock != "" {
			word += " · " + clock
		}
	}
	return pal.muted(a.exchangeSpin()) + " " + pal.dim(fit(word, width-2))
}

// exchangeStrip is the newest [exchangeStripRows] lines of this turn: a call
// starting, a call finishing, and the growing tail of the reply.
func (a *app) exchangeStrip(ex *homeExchange, width int, pal palette) []string {
	var lines []string
	for _, row := range ex.turnRows() {
		switch row.kind {
		case exchangeTool:
			// The hint is left off here: the strip is about what is HAPPENING,
			// and the row above already carries what came back.
			lines = append(lines, "  "+a.exchangeToolLine(row, "", width-2, pal))
		case exchangeReply:
			// THE LAST LINE OF THE REPLY AND NOT THE FIRST, so that a long answer
			// visibly grows instead of sitting still under a spinner.
			if tail := exchangeLastLine(revealedText(row.text, row.edge, row.settled), width-4); tail != "" {
				lines = append(lines, "  "+pal.dim(fit(tail, width-2)))
			}
		}
	}
	if len(lines) > exchangeStripRows {
		lines = lines[len(lines)-exchangeStripRows:]
	}
	return lines
}

// turnRows is the rows this turn has put down, and none of the ones before it.
func (ex *homeExchange) turnRows() []exchangeRow {
	from := ex.turnAt
	if from < 0 || from > len(ex.rows) {
		return nil
	}
	return ex.rows[from:]
}

// writing reports whether the reply is streaming: there is a live row and it
// has something in it.
func (ex *homeExchange) writing() bool {
	return ex.live >= 0 && ex.live < len(ex.rows) &&
		strings.TrimSpace(ex.rows[ex.live].text) != ""
}

// running reports whether a call of this turn is still executing.
func (ex *homeExchange) running() bool {
	for _, row := range ex.turnRows() {
		if row.kind == exchangeTool && !row.done {
			return true
		}
	}
	return false
}

// exchangeLastLine is the tail of a block of text at a width — the line the
// next character will land on.
func exchangeLastLine(text string, width int) string {
	lines := wrap(text, width)
	if len(lines) == 0 {
		return ""
	}
	return strings.TrimSpace(lines[len(lines)-1])
}

// ── the row in the left column ──────────────────────────────────────────────

// paneExchange is the exchange the pane is about, which is whichever one the
// CURSOR is on and nothing else.
//
// IT IS THE WHOLE OF THE THIRD REPAIR. "Once the reminder is set I am unable to
// see other previews on the right" — because the pane was drawn for as long as
// an exchange existed. One question, asked in one place, answers it for the
// pointer, the keyboard, the foot and the draw together.
func (a *app) paneExchange() *homeExchange {
	line, ok := a.home.focusedLine()
	if !ok || line.kind != homeExchangeRow {
		return nil
	}
	return line.ex
}

// settleExchangeFocus takes the keyboard back from every exchange the cursor is
// not on. It is called before a keypress is read, so walking onto another row
// can never leave the arrows moving a pane the person is no longer looking at.
func (a *app) settleExchangeFocus() {
	keep := a.paneExchange()
	for _, ex := range a.exchanges {
		if ex != keep {
			ex.focused = false
		}
	}
}

// exchangeLines is one project block's exchange rows, in triage order: what
// wants you, then what is moving, then what is done, and older first inside
// each. It is [homeView.projectBlock]'s own split applied to a third kind of
// row — an exchange is a hot thing and it sits where the hot things sit.
func (h *homeView) exchangeLines(project session.Project) []homeLine {
	var mine []*homeExchange
	for _, ex := range h.exchanges {
		if h.exchangeIn[ex] == project.Dir {
			mine = append(mine, ex)
		}
	}
	sort.SliceStable(mine, func(i, j int) bool {
		if a, b := exchangeRank(mine[i]), exchangeRank(mine[j]); a != b {
			return a < b
		}
		return mine[i].began.Before(mine[j].began)
	})
	lines := make([]homeLine, 0, len(mine))
	for _, ex := range mine {
		lines = append(lines, homeLine{
			kind: homeExchangeRow, project: project.Name, dir: project.Dir, ex: ex,
		})
	}
	return lines
}

// placeExchanges decides which block each exchange's row is drawn in, once per
// build, and it is a map rather than a test done per block for one reason: an
// exchange whose own project is not on the screen must still have a row
// SOMEWHERE, and "somewhere" is a decision that needs to see all the blocks.
//
// THE PROJECT IT WAS ASKED IN COMES FIRST. Failing that — a project folded away
// under `elsewhere`, a window standing in no project at all, a query that
// matched none of its conversations — the row goes in the FIRST block on the
// screen, which is this window's own ([homeTiers] puts it there). A row in a
// slightly wrong place is a row; a row nowhere is an errand a person cannot get
// back to.
func (h *homeView) placeExchanges(blocks []homeHit) {
	h.exchangeIn = nil
	if len(h.exchanges) == 0 || len(blocks) == 0 {
		return
	}
	h.exchangeIn = make(map[*homeExchange]string, len(h.exchanges))
	for _, ex := range h.exchanges {
		where := blocks[0].project.Dir
		for _, block := range blocks {
			if block.project.Dir == ex.bucket {
				where = block.project.Dir
				break
			}
		}
		h.exchangeIn[ex] = where
	}
}

// pointExchange puts the cursor on one exchange's row, and leaves it where it
// is when that exchange has no row on the list.
func (h *homeView) pointExchange(ex *homeExchange) {
	if ex == nil {
		return
	}
	h.pointAt(func(line homeLine) bool {
		return line.kind == homeExchangeRow && line.ex == ex
	})
}

// focusedExchange is the exchange under the cursor as [homeView] sees it, for
// the rebuild that has to keep the cursor on it.
func (h *homeView) focusedExchange() *homeExchange {
	if h.cursor < 0 || h.cursor >= len(h.lines) || h.lines[h.cursor].kind != homeExchangeRow {
		return nil
	}
	return h.lines[h.cursor].ex
}

// exchangeRowLine draws one exchange as a line of the column: the `?` this door
// is marked with, what was asked, and what it is doing right now.
//
//	? remind me at 6 to leave        ▲ waiting on you
//	? what did we decide about …     ⠹ working · 4s
//	? tell me when CI goes red       ∙ stood
//
// THE TAIL IS THE WHOLE POINT OF THE ROW. An errand that is thinking, an errand
// holding a question nobody has answered and an errand that is finished are
// three different claims on a person's attention, and a row that looked the
// same in all three would be a row saying only that an errand exists.
func (a *app) exchangeRowLine(line homeLine, at, width int, pal palette) string {
	label := homeAskHereGlyph + " " + exchangeTitle(line.ex.spoke)
	// bridge lane: an errand thinking is one of the moving things on this page,
	// so its tail turns only when this row is the one the page gave the spinner
	// to and holds the still `●` otherwise (homespinner.go).
	return overlayRowTinted(label, a.exchangeTail(line.ex, a.homeSpins(at)), exchangeTailInk(line.ex),
		at == a.home.cursor, markNone, at == a.home.hover && at == a.home.cursor, width, pal)
}

// exchangeTail is that trailing fact. spins is whether this row is the ONE the
// page animates; a working errand that is not it says the same thing with the
// still mark ([app.homeSpins]).
func (a *app) exchangeTail(ex *homeExchange, spins bool) string {
	switch {
	case ex.waiting():
		// THE ONE SHAPE ON THIS SCREEN THAT POINTS AT ANYTHING, and an exchange
		// with an unanswered card in it is exactly what it is for (home.go's
		// glyph block): the one row here asking for a hand.
		glyph := homeAskGlyph
		if a.pal.ascii {
			glyph = homeAskASCII
		}
		return glyph + " " + homeAskWaitingWord
	case ex.working:
		word := homeAskWorkingWord
		if !ex.turnBegan.IsZero() {
			if clock := countUpWord(a.now().Sub(ex.turnBegan)); clock != "" {
				word += " · " + clock
			}
		}
		mark := a.exchangeSpin()
		if !spins {
			mark = homeLiveGlyph
			if a.pal.ascii {
				mark = homeLiveASCII
			}
		}
		return mark + " " + word
	default:
		// AN EXCHANGE THAT IS OVER WEARS THE SETTLED MARK, through the one door
		// every mark comes through (docs/design/icons/DESIGN.md).
		glyph := a.icon(tokens.GSettled)
		word := homeAskAnsweredTail
		if ex.saved {
			word = homeAskSavedTail
		}
		return glyph + " " + word
	}
}

// exchangeTailInk brings `waiting on you` up out of the dim, and leaves every
// other tail in it. It is [homeNoteInk]'s rule said again about a third kind of
// row: a screen whose whole job is triage cannot draw its most urgent fact in
// the same grey as an age.
func exchangeTailInk(ex *homeExchange) noteInk {
	if !ex.waiting() {
		return nil
	}
	return func(pal palette, note string, selected bool) string {
		if selected {
			return pal.ink(note)
		}
		// AMBER, BECAUSE IT IS A PERSON BEING WAITED ON. The design spends one
		// colour on that reading everywhere it appears (styles.go's
		// [hueWarn]); this note used to take the accent, which on a place now
		// means work in flight — the opposite fact.
		return pal.warn(note)
	}
}

// ── the pointer, inside the pane ────────────────────────────────────────────

// exchangePress is a click inside the right pane, resolved against the rows the
// last draw put there ([app.exchangePane] writes the two targets).
//
// EVERY PRESS IN THE PANE GIVES IT THE KEYBOARD, whether or not it landed on
// something. The pane is one of home's two zones and a click is how a hand says
// which zone it is in — a press that highlighted nothing and left the arrows
// moving the column behind it would be the pointer and the keyboard disagreeing
// about where the person is.
//
// row is the pane's own row index and x is the column WITHIN the pane, both
// worked out by home's frame (home.go's [app.homePress]).
func (a *app) exchangePress(x, row int) tea.Cmd {
	ex := a.paneExchange()
	if ex == nil {
		return nil
	}
	ex.focused = true
	defer a.touch()
	if row >= 0 && row == ex.offerAt {
		ex.onOffer = true
		return a.promoteExchange(ex)
	}
	if ex.asking() && ex.ask != nil {
		for at, drawn := range ex.askAt {
			if drawn < 0 || drawn != row {
				continue
			}
			// A PRESS ANYWHERE ALONG AN ANSWER'S ROW IS THAT ANSWER'S, which is
			// the card's own rule wherever it is drawn: the answers are a column
			// here, so a press is resolved by row and there is no gap between two
			// of them to fall through.
			return a.questionPick(*ex.ask, at)
		}
	}
	// A press in the body is the zone change and nothing else: the box keeps
	// what is in it, and the pane's own cursor stays where it was.
	ex.onOffer = false
	return nil
}

// exchangeHover records whether the pointer is over `continue as a
// conversation`, which is the one row in this pane a pointer can act on and the
// one that had no hover at all until now — a row that lights up under nothing
// is a row people do not know they can click.
func (a *app) exchangeHover(row int) {
	ex := a.paneExchange()
	if ex == nil {
		return
	}
	was := ex.hover
	ex.hover = row >= 0 && row == ex.offerAt
	if ex.hover != was {
		a.touch()
	}
}

// errandContext is the context an errand's turn runs under. It is background on
// purpose: the pane's turn belongs to the exchange and not to the frame, and a
// context cancelled when the cursor moved would be an errand that died because
// somebody looked at their list.
func errandContext() context.Context { return context.Background() }

// exchangeAnswerWords is one card's answers as the pane's hint names them:
// every key the card drew and the word beside it, and nothing about the way out.
//
// IT IS THE QUESTION'S OWN OPTION LIST and never a second one — the same list
// [app.questionCardRows] draws a row apiece from, so a hint can never name an
// answer the card is not offering.
func exchangeAnswerWords(q session.Question) string {
	parts := make([]string, 0, len(q.Options))
	for _, option := range q.Options {
		key, word := strings.TrimSpace(option.Key), strings.TrimSpace(option.Label)
		if key == "" || word == "" {
			continue
		}
		parts = append(parts, key+" "+word)
	}
	return strings.Join(parts, railSep)
}

// exchangeHint is the line under the foot while the exchange holds the
// keyboard: what the keys do here, and how to get back to the list. It names
// only what is actually on screen — a card's three answers appear when a card
// does, and `continue as a conversation` when the first reply has landed.
func exchangeHint(ex *homeExchange) string {
	if ex.changing {
		return homeAskChangeWord + " · esc clear"
	}
	var parts []string
	if ex.asking() {
		// THE ANSWERS THE CARD DREW, off the question that drew them
		// ([app.questionHintOn]). This clause used to be a hardcoded copy of a
		// line kept correctly elsewhere, and it named `3 just once` over a
		// one-off reminder's card, which offers no such chip — a hint naming a
		// digit the chips do not is the same defect as a chip that does nothing
		// (#189).
		//
		// THE WAY OUT IS NOT NAMED HERE, which is the one thing this pane says
		// differently: the block's own line ends in `esc later`, and esc in this
		// pane hands the keyboard back to the list rather than putting anything
		// off. The decline is the `0` chip and the way out is named two clauses
		// down, in the pane's own words.
		if ex.ask != nil {
			parts = append(parts, exchangeAnswerWords(ex.ask.question))
			// AND THE KEY THAT ASKS FOR THE BOX, which is the one answer with no
			// row on the card: `c` says "I will take one of these, but not as it
			// stands", and the sentence that follows is the correction.
			//
			// ITS WORD IS THE KEY TABLE'S OWN (questionkeys.go's
			// [questionVerbWord]) and not this lane's, because a key spelled in
			// two places is a key that means two things the first week one of
			// them moves — an automation card's `Change…` included.
			if verb, ok := questionVerbFor(questionCommentKey); ok {
				parts = append(parts, verb.key+" "+questionVerbWord(*ex.ask, verb))
			}
		}
	}
	parts = append(parts, "enter sends a follow-up")
	if ex.offering() {
		parts = append(parts, "↓ "+homeContinueWord)
	}
	// BOTH WAYS OUT ARE NAMED. tab is the zone toggle and esc is the one-layer
	// undo, and a hint that named only one of them would be this line teaching
	// half of the way back to the list. On a narrow frame this pane is the whole
	// screen and the same two keys are how the list comes back, which is why the
	// sentence says "the list" rather than "the column".
	parts = append(parts, "tab or esc back to the list")
	return strings.Join(parts, " · ")
}
