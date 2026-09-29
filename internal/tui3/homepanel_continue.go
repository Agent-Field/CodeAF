package tui3

import (
	"context"
	"errors"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
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
// cursor on the answer that loses nothing. The takeover sentence is the card's
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
	lostRaceWord  = "another device continued this chat first"
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
var remoteOffers = map[chatlist.OfferKind]func(*app, chatlist.Row, chatlist.Offer) tea.Cmd{
	chatlist.ContinueHere: (*app).offerContinue,
	chatlist.Merge:        (*app).offerBranch,
	chatlist.Watching:     (*app).sayOffer,
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
	if a.machineRead.down && offer.Kind != chatlist.Watching {
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

// offerContinue raises the takeover screen.
func (a *app) offerContinue(row chatlist.Row, offer chatlist.Offer) tea.Cmd {
	if a.taker == nil {
		return a.sayOffer(row, chatlist.Offer{Line: chatlist.StatusLine(row)})
	}
	a.raiseMachineAsk(row, a.continueShown(row, offer))
	return nil
}

// continueShown is the takeover question: the offer's own words as the yes, and
// the takeover sentence as the reason. The cursor starts on `leave it there`.
func (a *app) continueShown(row chatlist.Row, offer chatlist.Offer) questionShown {
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
				{Key: "1", Label: offer.Line},
				{Key: "2", Label: continueStay, Safe: true},
			},
			Stakes: session.StakesReversible,
			Asked:  a.now(),
		},
		pick: 1,
		local: func(answer session.Answer) tea.Cmd {
			if answer.FirstKey() != "1" {
				return nil
			}
			return a.takeRemote(row)
		},
	}
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
	a.home.say(chatlist.OfferContinue+"…", "")
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
		a.home.say(continueFails, "")
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

// rebuildMachines redraws home from the reading it holds.
func (a *app) rebuildMachines() {
	a.home.others = a.machineRead
	if a.home.gridOn() {
		a.home.build()
	}
	a.touch()
}

// openTaken opens the chat a takeover just materialized, when this machine now
// lists it, and says that edits were kept when they were. A chat that is not on
// the list yet stays on home and the listing is asked for again.
func (a *app) openTaken(msg homeTakenMsg) tea.Cmd {
	a.refreshHome()
	kept := ""
	if msg.taken.Kept != "" {
		kept = chatlist.KeptEdits(msg.taken.KeptTurns, msg.taken.Device)
	}
	line, ok := a.homeLineOf(msg.row.Cell)
	if !ok {
		a.home.say(kept, "")
		return a.askMachines()
	}
	cmd := a.homeOpenLine(line)
	if kept != "" {
		a.note(kept)
	}
	return tea.Batch(cmd, a.askMachines())
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
