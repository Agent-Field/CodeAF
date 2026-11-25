package main

import (
	"errors"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE APP WAKES A CREW MANAGER THAT MOVED OUT OF IT ───────────────────────
//
// A crew's members run in the app's process, and a member's reply to its
// manager wakes the manager (internal/session's team_wakewatch.go). When the
// manager had moved to another window, and that window had since closed,
// nothing held the manager: the app's process had no door to open it, so every
// reply logged "could not wake ◆: no window has it open, and this process
// cannot open a conversation headless…" and the crew stalled until a person
// opened the manager again (blackmac-team/CREWS.md, gap 1).
//
// SO THE APP GIVES ITS PROCESS THE DOOR ([session.SetTeamResume]), and it
// opens the conversation WHERE IT LIVES NOW:
//
//   - A CONVERSATION THAT IS ONE OF THE APP'S OWN AGENTS COMES BACK TO THE APP.
//     The moved mark is cleared and the agent is opened here as a click on its
//     row would open it, so the woken turn is drawn in the desk the crew is
//     drawn in, and its row stops saying it moved. The window it moved to is
//     gone (the session asks the door only for a journal nothing holds), so
//     nothing is taken from anybody.
//   - ANYTHING ELSE IS OPENED IN ITS FOLDER'S HOST, the terminal's road
//     ([resumeTeamConversation]).
//
// A door that fails is not the end of it: the session keeps the wake and asks
// the door again until it opens (team_rousequeue.go).

// armAppTeamResume gives the app's process the door, and returns the disarm.
func (b *appBackend) armAppTeamResume() func() {
	session.SetTeamResume(func(file, workspace string) error {
		if mine, err := b.reopenForTeam(file); mine {
			return err
		}
		return resumeTeamConversation(file, workspace, attachEngineHost)
	})
	return func() { session.SetTeamResume(nil) }
}

// reopenForTeam opens the app agent whose transcript is file, clearing its
// moved mark, and reports whether file was one of the app's agents at all.
func (b *appBackend) reopenForTeam(file string) (bool, error) {
	key := sameFileKey(file)
	var a *appAgent
	b.locked(func() {
		for _, candidate := range b.agents {
			if candidate.transcript != "" && sameFileKey(candidate.transcript) == key {
				a = candidate
				return
			}
		}
	})
	if a == nil {
		return false, nil
	}
	if b.isClosing() {
		return true, errors.New("codeaf is shutting down")
	}
	a.gate.Lock()
	defer a.gate.Unlock()
	var moved bool
	var location *appwire.Location
	b.locked(func() {
		moved, location = a.windowMoved, a.view.Location
		a.windowMoved = false
	})
	view, opened, err := b.ensureLocked(a)
	if err != nil {
		b.locked(func() {
			if a.live == nil {
				a.windowMoved = moved
			}
		})
		return true, err
	}
	if moved && location != nil && !location.Here {
		b.located(a, nil)
	}
	if opened {
		b.emit.Emit(appwire.EventAgentChanged, appwire.AgentChanged{Agent: view})
	}
	return true, nil
}
