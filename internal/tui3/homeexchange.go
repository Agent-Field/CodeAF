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
//     ([Options.Errand]) with its transcript in a folder under the standing
//     root rather than under v3/projects — so home never lists it, and the
//     record still exists.
//   - The exchange itself in home's RIGHT PANE: the person's line, the reply as
//     it streams, one dim line per tool call, and the ratification card when
//     one arrives. Answering it is 1 / 2 / 3, a follow-up is typing and enter,
//     and esc puts the keyboard back on the list with the exchange still alive
//     beside it.
//
// WHERE THE FOLDER LIVES, AT EVERY STAGE. There is one folder and it only ever
// MOVES; nothing here copies a transcript and nothing here deletes one.
//
//	made          <standing root>/exchanges/<16-hex id>/transcript.jsonl
//	came to a thing that stands   <standing root>/<item id>/exchange/
//	                              ([standing.Store.ExchangeDir], on "stood")
//	promoted to a conversation    <project bucket>/<16-hex id>/  with meta.json
//	came to nothing               it stays where it was made, and the sweep law
//	                              reaps it after standing.RunKeep
//
// THE AGENT IS CLOSED BEFORE THE FOLDER MOVES, always. The transcript's flock
// rides the open file and a rename carries the inode with it, so a folder moved
// under a live writer would leave a lock held on a path nobody can name.
//
// AND THE MOVE THEREFORE WAITS FOR THE END OF THE EXCHANGE. "stood" arrives
// MID-TURN — the tool call that raised it is still running — so closing the
// agent there cancelled the turn in flight and parked the update loop on
// [session.Agent.Close]'s grace period, which is what a person feels as the
// screen going dead just after they said yes. So a stood exchange only
// REMEMBERS the item it made ([homeExchange.itemID]); the agent stays open,
// follow-ups keep working, and the folder is filed under the item when the
// exchange ends — home closing, or a second `ask here` replacing it
// ([app.dropExchange]) — after the agent has been closed there.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
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
	// homeAskNarrowWord is the refusal on a frame with no second column. The
	// exchange IS the right pane; a window too narrow to draw one would take the
	// sentence, open a session for it and show none of it, which is the one
	// failure worse than saying no.
	homeAskNarrowWord = "ask here needs a wider window"
	// homeAskChangeWord is what the card says after `2`: the correction is
	// typed into the box, not into a second card.
	homeAskChangeWord = "type the change and press enter"
	// homeAskStoodWord is what the pane says once something stands.
	homeAskStoodWord = "kept · this exchange is filed under it"
	// homeAskPromotedWord is the refusal for a second promotion of one folder.
	homeAskPromotedWord = "this exchange is already a conversation"
)

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
	// conversation's own card settles (standing.go's [standingCard.verdict]):
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
	// card is the proposal this row draws, for [exchangeCard]. It is the SAME
	// object [homeExchange.view] holds while it is the current one, so the row
	// and the keyboard can never disagree about what was decided.
	card *standingCard
}

// homeExchange is one errand: the agent, the folder it writes into, what has
// been said, and what the exchange came to.
//
// THE ZERO VALUE IS NOT A STATE ANYBODY REACHES. An exchange exists only when
// [app.askHere] built an agent, and [homeView.exchange] is nil until then and
// again after home closes — which is why every method here may assume the agent
// is there.
type homeExchange struct {
	agent Agent
	// view is the card as the shared renderer reads it, built once per notice
	// ([app.exchangeProposal]) and drawn by the conversation's own renderer
	// ([StandingCardRows]), so a card met at home and a card met mid-conversation
	// are one card. nil until a card arrives.
	view *standingCard
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
	// offerAt and cardAt are where the last draw PUT the two things a pointer
	// can hit — `continue as a conversation`, and the card's row of chips — as
	// indexes into the pane's own rows, or -1 for a thing that is not on screen.
	// Written by the render and read by the hit-testing, exactly as
	// [standingCard.choiceRow] is, so a click can never answer a question the
	// frame drew somewhere else.
	offerAt, cardAt int

	rows []exchangeRow
	// live is the reply being streamed into, and -1 between turns.
	live    int
	working bool
	// card is the ratification card waiting for an answer, and nil when none is.
	card *session.StandingNotice
	// changing arms the correction: `2` on the card says the next enter is not
	// a follow-up but the person's own wording of what is wrong with it.
	changing bool

	// stood is set once something actually stands. It is what stops a second
	// move of one folder, and what the pane says about where the record went.
	stood bool
	// itemID is the thing that stood, and it is the folder's DESTINATION held
	// rather than acted on: the move happens when the exchange ends and the
	// agent has been closed (this file's header says why it cannot happen at
	// the moment the news arrives). "" is a stood item whose notice carried no
	// id, which leaves the folder where it was made.
	itemID string
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

// errandUpdate folds one of those messages in. A message for an exchange that
// is no longer the one on screen is dropped for the reason [streamEventMsg]'s
// generation check drops a late turn's events: home closed, or a second errand
// replaced the first, and painting the old one over the new one would be a pane
// answering for a conversation nobody is in.
func (a *app) errandUpdate(msg errandMsg) tea.Cmd {
	ex := a.home.exchange
	if ex == nil || ex != msg.errand() {
		return nil
	}
	defer a.touch()
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
		ex.live = -1
	}
	return nil
}

// errandWait is [waitEvent] for this lane. It carries the exchange itself
// rather than a generation number, because an exchange IS its own generation:
// there is one at a time and it is a pointer nothing else can be.
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
// something now stands, and nothing else.
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
		ex.rows[ex.live].text += ev.Text

	case session.EventToolBegin:
		ex.live = -1
		ex.rows = append(ex.rows, exchangeRow{kind: exchangeTool, text: firstNonEmpty(ev.Hint, ev.Tool)})

	case session.EventToolEnd, session.EventToolFailed:
		ex.closeTool(ev)

	case session.EventStandingProposal:
		if ev.Standing != nil {
			a.exchangeProposal(ex, *ev.Standing)
		}

	case session.EventStandingUpdate:
		if ev.Standing != nil {
			return a.errandUpdated(ex, *ev.Standing)
		}

	case session.EventError:
		ex.live = -1
		ex.rows = append(ex.rows, exchangeRow{kind: exchangeNote, text: errText(ev.Err)})

	case session.EventTurnDone:
		ex.live = -1
		ex.working = false
	}
	return nil
}

// exchangeProposal puts one ratification card into the pane, in the transcript
// where it arrived.
//
// A SECOND CARD REPLACES THE FIRST and does not stack under it. The only way to
// get one is `2 change when` and a correction, which is one question being
// asked again in better words — so the row the first card drew is taken out and
// the new one is appended where the conversation now is, rather than leaving a
// settled `you asked for a different when` above a card that supersedes it.
func (a *app) exchangeProposal(ex *homeExchange, notice session.StandingNotice) {
	if ex.view != nil {
		for i := range ex.rows {
			if ex.rows[i].kind == exchangeCard && ex.rows[i].card == ex.view {
				ex.rows = append(ex.rows[:i], ex.rows[i+1:]...)
				break
			}
		}
	}
	kept := notice
	ex.card, ex.changing, ex.live = &kept, false, -1
	// THE VIEW IS BUILT HERE AND NOT AT DRAW TIME. Whether the three digits
	// belong to the card is a question the keyboard asks before any frame has
	// been painted, and a view that only existed once something had been drawn
	// would make the answer depend on the terminal having repainted.
	ex.view = a.standingCardFor(kept)
	ex.rows = append(ex.rows, exchangeRow{kind: exchangeCard, card: ex.view})
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
// THE WORDS ARE THE CONVERSATION'S OWN (standing.go's verdicts), because a card
// met at home and a card met mid-conversation are one card and must not settle
// into two vocabularies.
func (ex *homeExchange) settle(verdict, answer string) {
	if ex.view == nil {
		return
	}
	ex.view.verdict, ex.view.answer = verdict, answer
	ex.view.typing = false
}

// closeTool puts a call's result on the row that opened it — the newest row of
// that verb with nothing back yet — and opens a fresh row when there is none,
// so a result can never land on the wrong call.
func (ex *homeExchange) closeTool(ev session.Event) {
	for i := len(ex.rows) - 1; i >= 0; i-- {
		row := &ex.rows[i]
		if row.kind != exchangeTool || row.done {
			continue
		}
		row.done = true
		row.hint = strings.TrimSpace(firstNonEmpty(ev.Hint, errText(ev.Err)))
		return
	}
	ex.rows = append(ex.rows, exchangeRow{
		kind: exchangeTool, text: firstNonEmpty(ev.Hint, ev.Tool), done: true,
	})
}

// errandUpdated is what a standing update does to the exchange.
//
// "stood" IS THE ONE THAT DECIDES WHERE THE FOLDER GOES. The item now exists
// and its own folder is where its origin exchange belongs
// ([standing.Store.ExchangeDir]) — that is what makes "why did I get this
// reminder?" openable. Every other update is a line in the pane and nothing
// else.
//
// IT REMEMBERS THE DESTINATION AND MOVES NOTHING. The news arrives mid-turn,
// so closing the agent to free the transcript's lock here would cancel the turn
// that is still running and park the update loop on the close's grace period
// (this file's header). The move happens at [app.dropExchange] instead, which
// is the one place the agent is actually finished with.
//
// AND THE KEYBOARD GOES BACK TO THE LIST. The thing they asked for now exists;
// the list is where a person goes next, and the exchange stays alive beside it
// for a follow-up that tab or a click reaches.
func (a *app) errandUpdated(ex *homeExchange, notice session.StandingNotice) tea.Cmd {
	if text := strings.TrimSpace(notice.Text); text != "" {
		ex.rows = append(ex.rows, exchangeRow{kind: exchangeNote, text: text})
	}
	if notice.Update != "stood" || ex.stood || ex.promoted {
		return nil
	}
	// A card still asking when the thing it proposed has stood is a question
	// nobody can answer any more, so it settles into the answer the world just
	// gave it rather than staying a live question over a decided fact.
	if ex.asking() {
		ex.settle(standSetWord, "")
	}
	ex.stood = true
	ex.itemID = strings.TrimSpace(notice.Item.ID)
	ex.focused, ex.onOffer, ex.changing = false, false, false
	ex.rows = append(ex.rows, exchangeRow{kind: exchangeNote, text: homeAskStoodWord})
	return nil
}

// fileExchange moves a stood exchange's folder under the item it made. It is
// called from the one place the agent has just been closed, because the
// transcript's flock rides the open file (this file's header).
//
// A STOOD ITEM WITH NO ID, AND AN EXCHANGE THAT CAME TO NOTHING, BOTH STAY PUT.
// The folder is a record in the right place with the wrong name on it, which is
// better than a move to a directory nobody can find again — and the sweep law
// reaps what came to nothing after [standing.RunKeep].
func (a *app) fileExchange(ex *homeExchange) {
	if ex == nil || !ex.stood || ex.promoted || ex.itemID == "" {
		return
	}
	store, err := standing.Open(a.standingHome())
	if err != nil {
		return
	}
	dest := store.ExchangeDir(ex.itemID)
	if dest == ex.dir {
		return
	}
	if err := moveExchange(ex.dir, dest); err != nil {
		return
	}
	ex.dir = dest
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

// standingHome is where everything standing lives. The field is the test's door
// and the option is the launch's; the fallback is arithmetic on the projects
// root, because `<state root>/v3/projects` and `<state root>/v3/standing` are
// siblings by construction (internal/standing's package comment).
func (a *app) standingHome() string {
	if root := strings.TrimSpace(a.standingRoot); root != "" {
		return root
	}
	return filepath.Join(filepath.Dir(a.placesRoot()), "standing")
}

// errandsDir is where an exchange is made and where one that came to nothing
// stays. It is under the standing root and NOT under v3/projects, which is the
// whole mechanism: home lists what is in projects/, so an errand cannot become
// a row on the screen it was typed at.
func (a *app) errandsDir() string { return filepath.Join(a.standingHome(), "exchanges") }

// askHere is the second action row, and the chord.
//
// It mints the folder, builds an agent whose transcript is inside it, puts the
// keyboard on the exchange and sends the sentence — in that order, because each
// step is the previous one's proof. A seam that is not wired, a directory that
// cannot be made and an agent that will not open are all the same kind of
// answer: the row says why, the list is untouched, and nothing half-made is
// left on the disk.
func (a *app) askHere(text string) tea.Cmd {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	h := &a.home
	if a.errand == nil {
		h.say(homeAskUnavailableWord, "")
		return nil
	}
	// THE PANE HAS TO EXIST BEFORE THE SESSION DOES. Under [homeMinDetail] the
	// left column takes the whole frame ([homeColumns]) and there is nowhere for
	// an exchange to be drawn — so the refusal comes before the folder, and
	// nothing half-made is left behind by a window somebody had not widened yet.
	width, _ := a.size()
	if _, right := homeColumns(width); right <= 0 {
		h.say(homeAskNarrowWord, "")
		return nil
	}
	// ONE ERRAND AT A TIME. A second `ask here` closes the first, because the
	// pane holds one and an agent nobody can reach is an agent holding a lock
	// on a transcript for as long as the window lives.
	a.dropExchange()
	workspace, bucket := a.errandPlace()
	id := session.NewSessionID()
	dir := filepath.Join(a.errandsDir(), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		h.say(err.Error(), "")
		return nil
	}
	agent, err := a.errand(dir, workspace)
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
		offerAt: -1, cardAt: -1,
		spoke: text, began: now, said: now,
	}
	ex.rows = append(ex.rows, exchangeRow{kind: exchangeSaid, text: text})
	h.exchange = ex
	// THE BOX IS CLEARED AND THE LIST GOES BACK TO ITS RESTING SHAPE. The words
	// are in the exchange now; leaving them in the box would keep the drop-up up
	// and keep filtering the column behind a pane nobody is reading it through.
	h.box.reset()
	h.build()
	a.touch()
	return errandSend(ex, text)
}

// errandSend is one Submit, off the update loop for the reason [app.submit] is:
// building a request is not instant and a surface that waited for it would drop
// a frame at the exact moment a person is watching for one.
func errandSend(ex *homeExchange, text string) tea.Cmd {
	return func() tea.Msg {
		ch, err := ex.agent.Submit(errandContext(), text)
		return errandStartedMsg{ex: ex, ch: ch, err: err}
	}
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
// which is where a machine-wide item's work runs
// ([standing.Item.Workspace]).
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
	return errandHomeDir(), a.home.bucket
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
// the person's own home directory is where a machine-wide item's work runs
// ([standing.Item.Workspace] says the same). A process with no home directory
// falls back to where it is standing, for the reason internal/home's Dir does.
func errandHomeDir() string {
	if dir, err := os.UserHomeDir(); err == nil && strings.TrimSpace(dir) != "" {
		return dir
	}
	if dir, err := os.Getwd(); err == nil {
		return dir
	}
	return "."
}

// dropExchange ends whatever errand is open: the agent is closed, and THEN the
// folder is filed under the thing the exchange made, if it made one. It is what
// home closing does, and what a second `ask here` does.
//
// THIS IS WHERE THE MOVE LIVES, and it is the one place it can live: the
// rename carries the transcript's inode, so it must happen after the writer is
// gone (this file's header), and "stood" arrives while the writer is still
// mid-turn.
//
// THE FOLDER IS THE RECORD AND IT IS NEVER REMOVED HERE. An exchange that came
// to nothing keeps its transcript under the standing root's exchanges/, where
// the sweep law reaps it after [standing.RunKeep] — the record outlives the
// window, which is the whole reason it is a folder and not a buffer.
func (a *app) dropExchange() {
	ex := a.home.exchange
	if ex == nil {
		return
	}
	a.home.exchange = nil
	if ex.working {
		ex.agent.Interrupt()
	}
	_ = ex.agent.Close()
	a.fileExchange(ex)
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
func (a *app) exchangeKey(msg tea.KeyPressMsg) tea.Cmd {
	ex := a.home.exchange
	if ex == nil {
		return nil
	}
	defer a.touch()
	switch msg.String() {
	case "tab":
		// THE ZONE TOGGLE, and it is unconditional. Whatever is half-typed and
		// whichever row the pane's own cursor is on, tab hands the keyboard to
		// the list and leaves all of it standing to come back to.
		ex.focused = false
		return nil

	case "esc":
		// ONE LAYER AT A TIME, home's own rule: a half-typed follow-up is
		// cleared first and the second esc hands the keyboard back. The
		// exchange is not closed by either — it closes when home does.
		if !ex.box.empty() {
			ex.box.reset()
			return nil
		}
		ex.focused, ex.onOffer, ex.changing = false, false, false
		return nil

	case "1", "2", "3":
		// A CARD OWNS THE THREE DIGITS AND NOTHING ELSE DOES — while it is still
		// a QUESTION. Settled, and with no card up at all, they fall through to
		// the box below and are typed, which is what a person pressing `2` in
		// the middle of "make it 2pm" meant.
		if ex.asking() {
			return a.answerCard(ex, msg.String())
		}

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
		ex.box.reset()
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
		ex.box.insert(text)
		// TYPING LEAVES THE OFFER ROW. The box is where characters go, and a
		// person who starts typing has said which of the two things under the
		// pane they meant.
		ex.onOffer = false
	}
	return nil
}

// exchangeEnter is what enter means in the pane, and it means exactly one of
// three things depending on what is on screen.
func (a *app) exchangeEnter(ex *homeExchange) tea.Cmd {
	text := strings.TrimSpace(ex.box.String())
	switch {
	case ex.changing:
		// The correction the card asked for. Nothing is created on a change:
		// the model re-proposes and a second card arrives, which replaces this
		// one — and until it does, this one stands in the transcript wearing
		// what was asked of it.
		if text == "" {
			return nil
		}
		card := ex.card
		ex.box.reset()
		ex.changing = false
		ex.settle(standChangedWord, standChangeWord)
		ex.rows = append(ex.rows, exchangeRow{kind: exchangeSaid, text: text})
		ex.said = a.now()
		a.resolveStanding(ex, card, session.StandingAnswer{Change: text})
		return nil
	case ex.onOffer:
		return a.promoteExchange(ex)
	case text == "":
		return nil
	}
	ex.box.reset()
	ex.rows = append(ex.rows, exchangeRow{kind: exchangeSaid, text: text})
	ex.said = a.now()
	ex.working, ex.live = true, -1
	return errandSend(ex, text)
}

// answerCard is 1 / 2 / 3 on the ratification card.
//
// THE THREE ANSWERS ARE THE CONTRACT'S THREE, and no fourth is invented here:
// yes stands it up as proposed, a change goes back to the model to re-propose,
// and once runs the action now and creates nothing
// ([session.StandingAnswer]).
func (a *app) answerCard(ex *homeExchange, pressed string) tea.Cmd {
	card := ex.card
	switch pressed {
	case "1":
		ex.settle(standSetWord, standYesWord)
		a.resolveStanding(ex, card, session.StandingAnswer{Approved: true})
		// AND THE KEYBOARD GOES BACK TO THE LIST ON A YES. The thing they asked
		// for is being made; the list is where a person goes next, and leaving
		// the hand in a pane whose question has just been answered is how the
		// arrows stop moving the column for no reason anybody can see. The
		// exchange stays alive beside it — tab or a click brings it back for a
		// follow-up.
		ex.focused, ex.onOffer = false, false
	case "2":
		// The card stays a QUESTION: the person has said what is wrong with it
		// but not yet what would be right, and settling it here would put an
		// answer on a card nobody has answered.
		ex.changing = true
	case "3":
		ex.settle(standOnceDone, standOnceWord)
		a.resolveStanding(ex, card, session.StandingAnswer{Once: true})
	}
	return nil
}

// resolveStanding hands one answer back. An agent with no standing lane on it is
// a session where the ambient side is off, and such a session cannot have drawn
// a card in the first place — so the type assertion failing is unreachable
// through anything a person can do, and answering nothing is the right thing to
// do with a card that came from nowhere.
func (a *app) resolveStanding(ex *homeExchange, card *session.StandingNotice, answer session.StandingAnswer) {
	if card == nil {
		return
	}
	if door, ok := ex.agent.(standingAgent); ok {
		door.ResolveStanding(card.ID, answer)
	}
}

// offering reports whether `continue as a conversation` is on the pane: the
// first reply has landed, and the folder has not already gone somewhere.
func (ex *homeExchange) offering() bool {
	if ex.stood || ex.promoted {
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
	if ex.promoted || ex.stood {
		h.say(homeAskPromotedWord, "")
		return nil
	}
	if strings.TrimSpace(ex.bucket) == "" {
		h.say(homeElsewhereWord, "")
		return nil
	}
	if ex.working {
		ex.agent.Interrupt()
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
	a.home.exchange = nil
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

// exchangePane is home's right column while an errand is open: the conversation
// itself, the card, and the two rows under it.
//
// IT IS A TAIL AND NOT A CARD. The preview beside a session row is assembled
// top-down and drops whole bands off the bottom ([homeBands]) because it is a
// description of something that already happened; this is a conversation
// happening now, so it keeps the LAST rows that fit — the newest thing said is
// the thing being read.
func (a *app) exchangePane(width, room int, pal palette) []string {
	ex := a.home.exchange
	if ex == nil || width <= 0 || room <= 0 {
		return nil
	}
	// The hit targets are rebuilt with the rows that carry them, and cleared
	// first: a stale offer row is a click that promotes an exchange the frame
	// no longer offers to promote ([standingCard.choiceRow] states the law).
	ex.offerAt, ex.cardAt = -1, -1
	var out []string
	out = append(out, pal.bold(pal.ink(fit(homeAskHereWord, width))))
	out = append(out, "")
	for _, row := range ex.rows {
		if row.kind == exchangeCard {
			if row.card == nil {
				continue
			}
			at := len(out)
			out = append(out, StandingCardRows(a, row.card, width, true)...)
			if row.card == ex.view && row.card.choiceRow >= 0 {
				// The chips landed inside the card's own rows; the pane's row is
				// where the card started plus where the renderer put them.
				ex.cardAt = at + row.card.choiceRow
			}
			out = append(out, "")
			continue
		}
		out = append(out, exchangeRowLines(row, width, pal)...)
	}
	if ex.changing {
		out = append(out, pal.accent(fit(homeAskChangeWord, width)))
	}
	if ex.working {
		out = append(out, pal.dim(fit("…", width)))
	}
	if ex.offering() {
		out = append(out, "")
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
		ex.cardAt -= cut
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
func exchangeRowLines(row exchangeRow, width int, pal palette) []string {
	var out []string
	switch row.kind {
	case exchangeSaid:
		for i, wrapped := range wrap(row.text, width-2) {
			mark := "› "
			if i > 0 {
				mark = "  "
			}
			out = append(out, pal.accent(mark)+pal.ink(wrapped))
		}
	case exchangeReply:
		for _, wrapped := range wrap(row.text, width) {
			out = append(out, pal.ink(wrapped))
		}
	case exchangeTool:
		word := row.text
		if row.hint != "" {
			word += " · " + row.hint
		}
		out = append(out, " "+pal.dim(fit(word, width-1)))
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
	ex := a.home.exchange
	if ex == nil {
		return nil
	}
	ex.focused = true
	defer a.touch()
	if row >= 0 && row == ex.offerAt {
		ex.onOffer = true
		return a.promoteExchange(ex)
	}
	if row >= 0 && row == ex.cardAt && ex.asking() {
		// A PRESS ANYWHERE ON THE CHIPS ROW IS THE ROW'S, which is the call
		// [app.standingPress] makes for the same reason: a click in the gap
		// between two answers falling through would make the row a place where
		// missing costs you something.
		for _, span := range ex.view.spans {
			if x >= span.from && x < span.to {
				return a.answerCard(ex, itoa(span.at+1))
			}
		}
		return nil
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
	ex := a.home.exchange
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
		parts = append(parts, "1 yes · 2 change · 3 once")
	}
	parts = append(parts, "enter sends a follow-up")
	if ex.offering() {
		parts = append(parts, "↓ "+homeContinueWord)
	}
	// BOTH WAYS OUT ARE NAMED. tab is the zone toggle and esc is the one-layer
	// undo, and a hint that named only one of them would be this line teaching
	// half of the way back to the list.
	parts = append(parts, "tab or esc back to the list")
	return strings.Join(parts, " · ")
}
