package tui3

import (
	"fmt"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// standingHereAgent is the optional door onto the orders governing the
// conversation. Scripted agents that predate standing orders remain valid.
type standingHereAgent interface {
	StandingHere() (stand []standing.Item, excepted []standing.Item)
}

// noteStandingHere marks the threshold once when a conversation is opened.
func (a *app) noteStandingHere() {
	agent, ok := a.agent.(standingHereAgent)
	if !ok {
		return
	}
	stand, _ := agent.StandingHere()
	if len(stand) == 0 {
		// THE EMPTINESS LAW: zero orders draw nothing.
		return
	}
	noun := "standing orders"
	if len(stand) == 1 {
		noun = "standing order"
	}
	a.note(fmt.Sprintf("%d %s here — /standing", len(stand), noun))
}
