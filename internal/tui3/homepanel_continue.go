package tui3

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	machine "github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── ENTER ON A CHAT ON ANOTHER MACHINE ──────────────────────────────────────
//
// A row from another machine is a cursor stop and enter RUNS THE OFFER the row
// makes ([chatlist.OfferFor]): a chat its machine let go of offers `continue
// here`, a branch offers what can be done with it, and a chat still running
// somewhere says so and does nothing else (watching is a later stage).
//
// NOTHING IS DECIDED BY ONE KEYSTROKE. Continuing takes the chat from a machine
// that may still hold work, and discarding sets turns aside, so both raise the
// card home already has for a question about a row (homeconfirm.go), with the
// cursor on the answer that loses nothing, except where the person just chose to
// continue (enter on the row, the left-off chord, the device chord): there it
// starts on `continue here`. The takeover sentence is the card's
// reason, quoted whole from [chatlist.TakeoverLine].
//
// A VERB THAT CANNOT WORK IS ABSENT. With no [Taker] injected a chat that went
// off has no `continue here`, and with no merge injected a branch says only
// `discard`: the row's sentence and its card never promise a verb nothing is
// behind.

// Taken is what a takeover reports to the surface. Kept names the branch that
// holds edits made here which the takeover set aside before it fetched, with
// how many turns it holds and the name of this machine.
type Taken struct {
	Kept      string
	KeptTurns uint32
	Device    string
	// TaskCopies names the tasks whose working copies came along with the chat.
	TaskCopies []string
	// Resume is what the chat left behind on the other machine that this one
	// lacks. A takeover with something in it raises the setup card in the chat
	// it opens.
	Resume machine.Resume
	// Transcript is the journal of the chat as it lies here. The chat may live
	// in a folder home does not list, so the takeover names it.
	Transcript string
	// Elapsed is how long the takeover took, measured by whoever ran it.
	Elapsed time.Duration
}

// Taker continues a chat here. `handoff.Taker` is what stands behind it; the
// surface knows only this shape so it can be driven by a fake.
type Taker interface {
	Take(ctx context.Context, cell string) (Taken, error)
}

// BranchActions is what a branch row can do. A nil verb is not offered.
type BranchActions struct {
	Merge   func(ctx context.Context, branch string) error
	Discard func(ctx context.Context, branch string) error
}

// The words of the two cards. The sentences that are contract copy come from
// [chatlist]; these are the questions around them.
const (
	continueAsk   = "Continue this chat here?"
	continueStay  = "leave it there"
	lostRaceWord  = chatlist.LostRace
	continuing    = "continuing here…"
	continueFails = "could not continue this chat here"
	branchAsk     = "What should happen to these turns?"
	branchLeave   = "leave them"
	branchFails   = "could not change those turns"
)

// homeContinueKind and homeBranchKind are the lanes the two questions travel
// under; like [takeoverQuestionKind] nothing in the engine raises or answers
// them.
const (
	homeContinueKind session.QuestionKind = "surface-continue"
	homeBranchKind   session.QuestionKind = "surface-branch"
)

// machineAsks are the questions a row from another machine raises. Their
// sentences are contract copy and too long for a one-line foot, so a frame with
// no column to draw the card in shows them on a screen of their own
// ([app.machineScreen]).
var machineAsks = map[session.QuestionKind]bool{homeContinueKind: true, homeBranchKind: true}

// armedBy reports that a question was raised from this row. A chat on another
// machine is armed by its key, which no transcript path can spell.
func (l homeLine) armedBy(armed string) bool {
	if l.kind == homeMachineRow && l.remote != nil {
		return armed == machineArm(l.remote.Cell)
	}
	return l.kind == homeSession && strings.TrimSpace(l.row.Transcript) == armed
}

func machineArm(cell string) string { return "machine:" + cell }

// raiseMachineAsk raises a question about a row and arms the row, so the card
// stands beside it and comes down when the cursor walks off.
func (a *app) raiseMachineAsk(row chatlist.Row, q questionShown) {
	a.home.armed = machineArm(row.Cell)
	a.raiseHomeAsk(q)
	a.sayHomeAsk()
}

// machineScreen is THE TAKEOVER SCREEN ON A FRAME WITH NO COLUMN FOR A CARD: the
// question as the block draws it, wrapped to the width, in place of the list.
// It is nil when there is no such question or the card fits beside its row.
//
// A ONE-LINE FOOT CANNOT CARRY THE SENTENCE. The foot drops whole clauses from
// the end and the takeover sentence is the clause that matters, so at forty
// columns it would be the first thing to go; the screen wraps it instead.
func (a *app) machineScreen(width int) []string {
	ask, ok := a.homeAsking()
	if !ok || !machineAsks[ask.question.Kind] || a.homeAskFitsColumn() {
		return nil
	}
	return a.homeAskRows(max(1, width-1))
}

// remoteOffers answers each offer with what enter does on it. A table rather
// than a switch, so a new offer is a new entry.
//
// A CHAT ANOTHER MACHINE LET GO OF (`Open`, which is what a released chat offers
// where it is held) IS CONTINUED THE WAY A CHAT THAT WENT OFF IS: it is not on
// this machine, so opening it is continuing it, and the lease is free. A chat
// that is running on another machine is continued the same way, and the lease
// is taken from it at once: the screen names the machine, the person decides.
var remoteOffers = map[chatlist.OfferKind]func(*app, chatlist.Row, chatlist.Offer) tea.Cmd{
	chatlist.Open:         (*app).offerContinue,
	chatlist.ContinueHere: (*app).offerContinue,
	chatlist.Merge:        (*app).offerBranch,
}

// homeMachineEnter is enter on a chat from another machine.
func (a *app) homeMachineEnter(line homeLine) tea.Cmd {
	row := *line.remote
	offer := chatlist.OfferFor(row)
	run, ok := remoteOffers[offer.Kind]
	if !ok {
		return nil
	}
	// THE DIRECTORY IS THE ONLY THING THAT CAN GRANT A LEASE OR ARCHIVE A
	// BRANCH, so with it out of reach the acting offers are disabled and say why
	// on the foot, where every other refusal on this screen is said.
	if a.machineRead.down {
		a.home.say(chatlist.Unreachable, "")
		return nil
	}
	return run(a, row, offer)
}

// sayOffer puts an offer that does nothing yet on the foot.
func (a *app) sayOffer(_ chatlist.Row, offer chatlist.Offer) tea.Cmd {
	a.home.say(offer.Line, "")
	return nil
}

// ── continue here ───────────────────────────────────────────────────────────

// The two places the takeover card's cursor can start.
const (
	continueYesAt  = 0
	continueStayAt = 1
)

// offerContinue raises the takeover screen for a person who chose to continue
// (enter on the row, the left-off chord, the device chord): the cursor starts
// on `continue here`, so enter completes the move they just asked for.
func (a *app) offerContinue(row chatlist.Row, _ chatlist.Offer) tea.Cmd {
	return a.raiseContinue(row, continueYesAt)
}

// askContinue raises the same screen when nobody chose yet (a message sent into
// a chat another machine holds): the cursor starts on `leave it there`.
func (a *app) askContinue(row chatlist.Row) tea.Cmd {
	return a.raiseContinue(row, continueStayAt)
}

func (a *app) raiseContinue(row chatlist.Row, at int) tea.Cmd {
	if a.taker == nil {
		return a.sayOffer(row, chatlist.Offer{Line: chatlist.StatusLine(row)})
	}
	a.raiseMachineAsk(row, a.continueShown(row, at))
	return nil
}

// continueShown is the takeover question: `continue here` as the yes, and the
// takeover sentence as the reason. The cursor starts on option `at`.
func (a *app) continueShown(row chatlist.Row, at int) questionShown {
	row = a.presentRow(row)
	return questionShown{
		question: session.Question{
			Kind:    homeContinueKind,
			Ask:     session.AskConfirmation,
			Form:    session.FormCard,
			Asker:   session.Asker{Kind: session.AskerSurface},
			Head:    continueAsk,
			Reason:  chatlist.TakeoverLine(row),
			Subject: session.SubjectRef{Name: row.Title},
			Options: []session.AnswerOption{
				{Key: "1", Label: chatlist.OfferContinue},
				{Key: "2", Label: continueStay, Safe: true},
			},
			Stakes: session.StakesReversible,
			Asked:  a.now(),
		},
		pick: at,
		local: func(answer session.Answer) tea.Cmd {
			if answer.FirstKey() != "1" {
				return nil
			}
			return a.takeRemote(row)
		},
	}
}

// presentRow is the row as the device's presence reads now: a chat whose holder
// is not online is one that stopped, so the card does not say it runs there.
func (a *app) presentRow(row chatlist.Row) chatlist.Row {
	if a.resumeState().away(row) {
		return row.Quiet()
	}
	return row
}

// homeTakenMsg is the takeover's answer, coming back off the update loop.
type homeTakenMsg struct {
	row   chatlist.Row
	taken Taken
	err   error
}

// takeRemote asks the taker, off the update loop: a takeover fetches and waits
// on the directory, and a keystroke may not wait on a network.
func (a *app) takeRemote(row chatlist.Row) tea.Cmd {
	a.home.say(continuing, "")
	taker, ctx := a.taker, a.ctx
	return func() tea.Msg {
		taken, err := taker.Take(ctx, row.Cell)
		return homeTakenMsg{row: row, taken: taken, err: err}
	}
}

// tookTakeover files the answer. A takeover that failed leaves the row where it
// was, and one line says why; one that worked opens the chat.
func (a *app) tookTakeover(msg homeTakenMsg) tea.Cmd {
	switch {
	case errors.Is(msg.err, directory.ErrLeaseHeld):
		return a.lostRace(msg.row)
	case errors.Is(msg.err, directory.ErrUnreachable):
		a.machineRead.down = true
		a.home.say(chatlist.Unreachable, "")
	case msg.err != nil:
		// Say why: the bare sentence left a failing takeover undiagnosable.
		a.home.say(continueFails+" · "+firstLine(msg.err.Error()), "")
	default:
		return a.openTaken(msg)
	}
	a.touch()
	return nil
}

// lostRace is another device winning the takeover. The row goes back to
// `running on <device>` at once, from the listing this window already holds,
// and the directory is asked again for the winner's own name.
func (a *app) lostRace(row chatlist.Row) tea.Cmd {
	rows := append([]chatlist.Row(nil), a.machineRead.rows...)
	for i := range rows {
		if rows[i].Cell == row.Cell {
			rows[i].Status = chatlist.Running
		}
	}
	a.machineRead.rows = rows
	a.rebuildMachines()
	a.home.say(lostRaceWord, "")
	return a.askMachines()
}

// rebuildMachines redraws home from the reading it holds. The compact inbox
// draws these rows too, so it is rebuilt as well: left alone, a machine with no
// chat of its own would keep showing `nothing here yet` over the rows that just
// arrived.
func (a *app) rebuildMachines() {
	a.home.others = a.othersNow()
	if a.home.gridOn() || a.home.phone {
		a.home.build()
	}
	a.touch()
}

// openTaken opens the chat a takeover just materialized, when this machine now
// lists it, and says that edits were kept when they were. A chat that is not on
// the list yet stays on home and the listing is asked for again.
//
// THE SENTENCE RIDES THE OPEN ([app.homeTakenSaid]). The ordinary open from
// home goes off the loop and lands later, so a note written here would be said
// into the conversation the person is leaving; the open's landing says it into
// the one they are arriving in. A shared window opens in place, and the ride
// is spent here rather than at the landing.
func (a *app) openTaken(msg homeTakenMsg) tea.Cmd {
	a.refreshHome()
	kept := takenSaid(msg.taken)
	moved := movedSaid(msg.taken)
	line, ok := a.takenLine(msg)
	if !ok {
		a.home.say(joinSaid(moved, kept), "")
		return a.askMachines()
	}
	a.homeTakenSaid = func() tea.Cmd {
		a.note(joinSaid(moved, kept))
		if a.file == line.row.Transcript {
			a.offerSetup(msg.taken.Resume)
		}
		return nil
	}
	cmd := a.homeOpenLine(line)
	switch {
	case cmd == nil && a.at(pageHome):
		// THE OPEN REFUSED and home still stands: the sentence has no
		// conversation to arrive in, so home itself says the takeover
		// happened, the way it says every row's refusal.
		a.home.say(joinSaid(moved, kept), "")
		a.homeTakenSaid = nil
		return a.askMachines()
	case !a.at(pageHome):
		// THE OPEN LANDED IN PLACE — a shared window walks in synchronously —
		// so the ride is spent here rather than at a landing that already
		// happened.
		said := a.homeTakenSaid
		a.homeTakenSaid = nil
		return tea.Batch(cmd, said(), a.askMachines())
	}
	return tea.Batch(cmd, a.askMachines())
}

// takenLine is the line of the chat a takeover just materialized: the one home
// lists, else one built from where the takeover put it, so the chat opens
// whether or not home lists its folder.
func (a *app) takenLine(msg homeTakenMsg) (homeLine, bool) {
	if line, ok := a.homeLineOf(msg.row.Cell); ok {
		return line, true
	}
	t := msg.taken
	if t.Transcript == "" {
		return homeLine{}, false
	}
	return homeLine{kind: homeSession, row: session.SessionRow{ID: msg.row.Cell, Transcript: t.Transcript, ProjectDir: t.Resume.Now}}, true
}

// movedSaid is the line every takeover says, with the time it took.
func movedSaid(t Taken) string {
	return chatlist.Moved(t.Resume.From, t.Elapsed, len(t.Resume.Stopped) > 0)
}

// joinSaid joins the lines of a takeover that have something to say.
func joinSaid(lines ...string) string {
	var said []string
	for _, l := range lines {
		if l != "" {
			said = append(said, l)
		}
	}
	return strings.Join(said, " ")
}

// takenSaid is the one line a takeover says about what it brought or kept,
// empty when it did neither.
func takenSaid(t Taken) string {
	var said []string
	if t.Kept != "" {
		said = append(said, chatlist.KeptEdits(t.KeptTurns, t.Device))
	}
	if copies := chatlist.CopiesCame(t.TaskCopies); copies != "" {
		said = append(said, copies)
	}
	return strings.Join(said, "; ")
}

// homeLineOf is the conversation line of a chat this machine lists.
func (a *app) homeLineOf(cell string) (homeLine, bool) {
	for _, line := range a.home.lines {
		if line.kind == homeSession && line.row.ID == cell {
			return line, true
		}
	}
	return homeLine{}, false
}

// ── a branch: merge / discard ───────────────────────────────────────────────

// branchVerb is one thing that can be done with a branch.
type branchVerb struct {
	label, consequence string
	do                 func(ctx context.Context, branch string) error
}

// branchVerbs are the verbs this surface has behind it, in the order they are
// offered. A verb with nothing behind it is not in the list.
func (a *app) branchVerbs() []branchVerb {
	var verbs []branchVerb
	if a.branches.Merge != nil {
		verbs = append(verbs, branchVerb{"merge", "the turns join this chat", a.branches.Merge})
	}
	if a.branches.Discard != nil {
		verbs = append(verbs, branchVerb{"discard", "they are archived, not deleted", a.branches.Discard})
	}
	return verbs
}

// offerBranch raises the branch card, or says the row's sentence when nothing
// can be done with it from here.
func (a *app) offerBranch(row chatlist.Row, _ chatlist.Offer) tea.Cmd {
	verbs := a.branchVerbs()
	if len(verbs) == 0 {
		a.home.say(chatlist.BranchLine(row, false), "")
		return nil
	}
	a.raiseMachineAsk(row, a.branchShown(row, verbs))
	return nil
}

// branchShown is the branch question. The answers are the verbs in order and
// `leave them` last, under the cursor.
func (a *app) branchShown(row chatlist.Row, verbs []branchVerb) questionShown {
	options := make([]session.AnswerOption, 0, len(verbs)+1)
	for i, v := range verbs {
		options = append(options, session.AnswerOption{Key: string(rune('1' + i)), Label: v.label, Consequence: v.consequence})
	}
	options = append(options, session.AnswerOption{Key: string(rune('1' + len(verbs))), Label: branchLeave, Safe: true})
	return questionShown{
		question: session.Question{
			Kind:    homeBranchKind,
			Ask:     session.AskConfirmation,
			Form:    session.FormCard,
			Asker:   session.Asker{Kind: session.AskerSurface},
			Head:    branchAsk,
			Reason:  chatlist.BranchLine(row, a.branches.Merge != nil),
			Subject: session.SubjectRef{Name: row.Title},
			Options: options,
			Stakes:  session.StakesReversible,
			Asked:   a.now(),
		},
		pick: len(verbs),
		local: func(answer session.Answer) tea.Cmd {
			// A KEY NOT A VERB'S IS `leave them`, and so is esc, which has none.
			for i := range verbs {
				if answer.FirstKey() == string(rune('1'+i)) {
					return a.runBranchVerb(verbs[i], row)
				}
			}
			return nil
		},
	}
}

// homeBranchMsg is a branch verb's answer.
type homeBranchMsg struct{ err error }

// runBranchVerb runs one verb off the update loop, for takeRemote's reason.
func (a *app) runBranchVerb(v branchVerb, row chatlist.Row) tea.Cmd {
	ctx := a.ctx
	return func() tea.Msg { return homeBranchMsg{err: v.do(ctx, row.Cell)} }
}

// tookBranch files the answer: a failure says so on the foot, and either way
// the listing is asked for again, so a branch that was archived leaves it.
func (a *app) tookBranch(msg homeBranchMsg) tea.Cmd {
	if msg.err != nil {
		a.home.say(branchFails, "")
	}
	return a.askMachines()
}

// othersNow is the held reading with presence as the feed says it now.
func (a *app) othersNow() machineReading {
	reading := a.machineRead
	reading.away = a.resumeState().away
	return reading
}
