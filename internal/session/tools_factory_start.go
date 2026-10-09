package session

// THE MANAGER'S START: `factory_start` STARTS THE RUN OF ITS ITEM.
//
// The manager's brief says it starts the run when the person asks, and this
// is that act: the same control as the top bar's button on the item's page.
// It launches a run that has not started, goes on with a paused one in the
// same step, and goes past an approve step the run holds at, and it does
// nothing else. It is on the belt only where the conversation's run door can
// start ([RunStarter]), which a step's conversation and the runner's own
// shaping turn never have.

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
)

// RunStarter is what a [RunDoor] may also do: start, resume or continue the
// run of the ONE item conversation manages, answering the line the floor
// shows, or why nothing changed.
type RunStarter interface {
	StartRun(ctx context.Context, conversation string) (string, error)
}

// RunManagerDoor is what a [RunDoor] may also say: whether conversation is
// the manager of a factory item. Such a conversation leads the item's team,
// whose members are the steps the floor's runner runs, so its team verbs are
// the reads alone (team.go's [Agent.factoryLead]).
type RunManagerDoor interface {
	Manages(conversation string) bool
}

// mayStart says whether `factory_start` belongs on this belt.
func (c Config) mayStart() bool {
	_, ok := c.FactoryRun.(RunStarter)
	return ok
}

const factoryStartDescription = "Start the run of the ONE factory item this conversation manages, when the person asks you to start, run, go, resume or continue. " +
	"It is the same control as the run button on the item's page: a run that has not started is launched with the steps as they stand now, a paused run goes on with the same step in the same chat, and a run held at an approve step goes past it. " +
	"Go past an approve step only when the person clearly said so in this chat, never on your own judgment. " +
	"There is nothing to look up first: call it, then say the line it answers in one short sentence. The answer is what happened, or why nothing changed."

const factoryStartSchema = `{"type":"object","properties":{},"additionalProperties":false}`

// factoryStartTool is the manager's start.
func (a *Agent) factoryStartTool() bare.Tool {
	return bare.Tool{
		Name:        "factory_start",
		Description: factoryStartDescription,
		Schema:      json.RawMessage(factoryStartSchema),
		Execute: func(ctx context.Context, _ json.RawMessage) (string, bool, error) {
			// THE DOOR OF THE TURN IN FLIGHT: a shaping turn the runner lent
			// its own door to cannot start anything.
			starter, ok := a.runDoorForTurn().(RunStarter)
			if !ok {
				return "nothing started: this turn cannot start the run", true, nil
			}
			line, err := starter.StartRun(ctx, a.config.SessionFile)
			if err != nil {
				return "nothing started: " + strings.Join(strings.Fields(err.Error()), " "), true, nil
			}
			return line, false, nil
		},
	}
}
