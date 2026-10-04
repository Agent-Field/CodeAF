package tui3

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	machine "github.com/Agent-Field/codeaf/internal/preflight"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── SET THIS MACHINE UP LIKE THE OTHER ONE HAD IT ───────────────────────────
//
// A chat that moved here arrives without what its machine left out of the copy
// and without what it left running. The transcript cannot say that, and the
// agent would find out one failing command at a time. So a takeover that has
// something to say raises ONE card in the chat it opens, listing exactly what
// the agent will be told, and asks whether to bring it back.
//
// NOTHING RUNS BEFORE `set up`. `not now`, esc and no answer run nothing; the
// same facts then reach the agent once, as news that asks for nothing, at its
// next step (internal/preflight's stash). `set up` is the same setup turn
// /setup now starts, opened with what the chat left behind, and it is consent for
// exactly the commands the card lists: any command the agent adds passes the
// ordinary gate.
//
// A takeover with nothing to say raises nothing, and writes nothing to the
// transcript: the emptiness law.

// resumeKind is the lane the card travels under. Nothing in the engine raises or
// answers it; it is answered by the closure the card carries.
const resumeKind session.QuestionKind = "surface-restore"

const (
	resumeSetUpKey  = "1"
	resumeNotNowKey = "2"
	// resumeNotNowAt is where the cursor starts: on the answer that runs nothing.
	resumeNotNowAt = 1
)

// offerSetup raises the card for a resume. One with something to set up asks
// whether to bring it back, when this conversation has a machine of its own to
// set up (a capability that cannot work is absent). One with only where the chat
// stands, uncommitted files and the last test run, raises the same card as a
// plain brief that runs nothing.
func (a *app) offerSetup(r machine.Resume) {
	if !r.Worth() {
		return
	}
	far, ok := a.agent.(machineDoor)
	switch {
	case !r.Empty() && ok:
		a.raiseQuestion(a.setupShown(r, far))
	case r.Empty():
		a.raiseQuestion(a.arrivedShown(r))
	}
}

// arrivedShown is the hero card of an arrival with nothing to set up: the
// facts, and one answer that closes it.
func (a *app) arrivedShown(r machine.Resume) questionShown {
	return questionShown{
		question: session.Question{
			Kind:    resumeKind,
			Ask:     session.AskConfirmation,
			Form:    session.FormCard,
			Asker:   session.Asker{Kind: session.AskerSurface},
			Head:    chatlist.ArrivedHead(r.From),
			Reason:  strings.Join(chatlist.SetupReasons(setupFacts(r)), " · "),
			Options: []session.AnswerOption{{Key: resumeSetUpKey, Label: chatlist.OfferGotIt, Safe: true}},
			Stakes:  session.StakesReversible,
			Asked:   a.now(),
		},
		local: func(session.Answer) tea.Cmd { return nil },
	}
}

// setupShown is the card and the closure that acts on it. It blocks nothing: the
// person walked into this chat and nothing waits behind the question.
func (a *app) setupShown(r machine.Resume, far machineDoor) questionShown {
	return questionShown{
		question: session.Question{
			Kind:   resumeKind,
			Ask:    session.AskConfirmation,
			Form:   session.FormCard,
			Asker:  session.Asker{Kind: session.AskerSurface},
			Head:   chatlist.SetupHead(r.From),
			Reason: strings.Join(chatlist.SetupReasons(setupFacts(r)), " · "),
			Options: []session.AnswerOption{
				{Key: resumeSetUpKey, Label: chatlist.OfferSetUp},
				{Key: resumeNotNowKey, Label: chatlist.OfferNotNow, Safe: true},
			},
			Stakes: session.StakesReversible,
			Asked:  a.now(),
		},
		pick: resumeNotNowAt,
		local: func(answer session.Answer) tea.Cmd {
			if answer.FirstKey() != resumeSetUpKey {
				a.note(chatlist.SetupLater)
				return nil
			}
			return a.withSetupPlan(far, func(plan machine.Report) tea.Cmd { return a.startSetup(far, plan) })
		},
	}
}

// setupFacts is the resume as the card's three lines.
func setupFacts(r machine.Resume) chatlist.SetupFacts {
	return chatlist.SetupFacts{Missing: r.MissingNames(), Running: r.RunningLines(), Needed: r.NeededLines(), Changed: r.Uncommitted, Tests: testsLine(r)}
}

// testsLine is the last recorded test run as the card's line, "" when the chat
// ran none: a line that is not backed by a run is not shown.
func testsLine(r machine.Resume) string {
	if r.Tests == nil {
		return ""
	}
	return chatlist.TestsLine(r.Tests.Passed, r.Tests.Failed)
}
