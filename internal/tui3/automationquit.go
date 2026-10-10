package tui3

// automationquit.go is leaving while an automation is running.
//
// AUTOMATIONS RUN ONLY WHILE A WINDOW IS OPEN, so closing the last one stops
// whatever is in hand (internal/automation's clock). That is the design and not
// an accident, but it is a consequence a person should meet before it happens
// rather than in the run's history afterwards — so the door out says it once,
// and the same gesture again leaves.
//
// IT ASKS ONLY WHEN IT IS TRUE. A window with other windows open beside it stops
// nothing by closing, and a reading that has not landed yet is not grounds for
// a warning; both leave at once, as ctrl+c at rest always has (input.go).
//
// AND A SIGNAL NEVER ASKS. Somebody who named this process from outside it is
// owed the ordinary door, and the run is recorded as stopped either way.

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// quitArmedFor is how long the warning holds: the same gesture inside it
// leaves, and after it the warning is said again.
const quitArmedFor = 10 * time.Second

// The second half of the warning, per door.
const (
	quitAgainChord   = " · ctrl+c again to quit"
	quitAgainCommand = " · /quit again to quit"
)

// requestQuit is the door out for a person: ctrl+c at rest, and the last /quit.
// The clause names the gesture that was used, because that is the one that
// will be pressed again.
func (a *app) requestQuit(again string) tea.Cmd {
	now := a.now()
	if !a.quitArmed.IsZero() && now.Sub(a.quitArmed) < quitArmedFor {
		return a.quit()
	}
	if !a.watch.known || a.watch.running == 0 || a.watch.others > 0 {
		return a.quit()
	}
	a.quitArmed = now
	a.note(automationRunningLine(a.watch.running) + again)
	a.touch()
	return nil
}
