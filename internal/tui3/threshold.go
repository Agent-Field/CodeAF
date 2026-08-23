package tui3

import (
	"fmt"

	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// standingCountAgent is the one door the threshold needs: how many orders
// stand here. It is DELIBERATELY NARROWER than the page's [standingHereAgent]
// — the threshold only reads, and an agent that can be counted over but not
// written to (a test's fake, a surface with no write doors) still gets its
// line. Each surface asserts the slice of the engine it actually uses.
type standingCountAgent interface {
	StandingHere() (stand []standing.Item, excepted []standing.Item)
}

// noteStandingHere marks the threshold once when a conversation is opened.
func (a *app) noteStandingHere() {
	agent, ok := a.agent.(standingCountAgent)
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
	// THE COUNT IS WHAT THIS LINE IS FOR (payload.go): a person crossing the
	// threshold is being told how much already stands over this conversation, and
	// "how much" is the digit. The noun is prose and /standing is a door, so it
	// wears the chip every door wears.
	a.noteFacts(fmt.Sprintf("%d %s here — /standing", len(stand), noun), itoa(len(stand)))
}
