package tui3

// homeengine.go is HOW HOME TELLS AN ENGINE FROM A WINDOW.
//
// A row home draws as held is a journal under somebody's flock, and the kernel's
// answer stops there: it says a process has it and says nothing about which kind
// of process. Two live behind that one fact and they are opposites.
//
//	a window    another terminal has the conversation open in its own process.
//	            Nothing can join it; it can only be asked for (takeover.go).
//	the engine  this workspace's session host has it (internal/enginehost). It
//	            has no terminal, it survives every window that ever attached to
//	            it, and it hands the running conversation to whoever asks — which
//	            is what makes enter on such a row an ordinary open.
//
// SAYING `open in another window` ABOUT THE SECOND ONE WAS THE DEFECT. A person
// closed a terminal on four running tasks, came back, and home sent them looking
// for a window that had not existed for an hour — and the door under the row
// spent ten minutes asking it to let go before giving up.
//
// ── ASKED ON THE BEAT, ABOUT THE HELD ROWS, AND NOWHERE ELSE ────────────────
//
// The question is a connect to a unix socket. That is a keystroke's cost and not
// a label's — home draws twenty rows on every pointer movement, and [app.homeHeld]
// states the law — so it is asked the way the world itself is asked: on home's
// own three-second beat ([homeEvery]), OFF THE LOOP in a command, and only about
// the projects that actually have a held row on the screen. A home with nothing
// held asks nothing at all, which is almost every home.
//
// AND IT BUILDS NOTHING. [Options.EngineAnswers] answers under internal/enginehost's
// own law — ASKING WHETHER SOMEBODY IS THERE MUST NOT BUILD THEM A HOUSE — so a
// project that has never had an engine is left exactly as it was found.

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/Agent-Field/aforge-v2/internal/session"
)

// engineReplyMsg is one round of that asking, on its way back to the loop.
type engineReplyMsg struct {
	gen int
	// answers is every workspace that was asked and what it said. It REPLACES
	// the map rather than merging into it, so a project whose engine has since
	// retired stops being remembered as one — a stale yes here would draw
	// `open in the engine` over a row a window really is holding.
	answers map[string]bool
}

// engineHeld reports that this row's journal is held by an engine, from the last
// round of asking. False for a row nothing has been asked about yet, which is
// the honest floor: `open in another window` is what home has always said, and a
// beat later it says better.
func (a *app) engineHeld(row session.SessionRow) bool {
	if a.engines == nil {
		return false
	}
	return a.engines[strings.TrimSpace(row.ProjectDir)]
}

// askEngines is that round, as a command. Nil when there is nothing to ask —
// no engine road, or no held row on the screen.
func (a *app) askEngines() tea.Cmd {
	if a.engineAnswers == nil {
		return nil
	}
	ask := map[string]bool{}
	for _, line := range a.home.lines {
		if line.kind != homeSession || !a.homeHeld(line.row) {
			continue
		}
		if where := homeWhere(line); where != "" {
			ask[where] = true
		}
	}
	if len(ask) == 0 {
		if len(a.engines) > 0 {
			// NOTHING IS HELD ANY MORE, so nothing may still be remembered as
			// engine-held. The map is emptied on the loop rather than left to
			// age out, because the row it would colour is drawn thirty times a
			// second and the answer is already known here.
			a.engines = nil
		}
		return nil
	}
	answer, gen := a.engineAnswers, a.homeGen
	return func() tea.Msg {
		said := make(map[string]bool, len(ask))
		for where := range ask {
			said[where] = answer(where)
		}
		return engineReplyMsg{gen: gen, answers: said}
	}
}

// engineReply takes one round's answers. A round from a home that has since been
// closed and reopened is dropped, which is the same generation rule every other
// lane on this surface keeps.
func (a *app) engineReply(msg engineReplyMsg) {
	if msg.gen != a.homeGen {
		return
	}
	a.engines = msg.answers
	a.touch()
}
