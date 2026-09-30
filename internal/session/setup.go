package session

import (
	"context"
	"errors"
	"strings"

	"github.com/Agent-Field/codeaf/internal/approval"
	"github.com/Agent-Field/codeaf/internal/preflight"
)

// ── THE SETUP TURN (docs/ARCHITECTURE.md 8.4) ────────────────────────────────
//
// A chat moved here may need tools this machine lacks. The harness compares what
// the chat has used with what this machine has ([preflight.Report]) and, when a
// person says so, starts a turn whose instruction is those installable lines.
// The AGENT installs; the harness only sets the terms:
//
//   - every tool call of the turn runs on the setup form of the seat, so the
//     network is open and the call is recorded external and sealed as a Setup
//     turn (the seat says so, not this file);
//   - every call passes the ordinary gate, with one floor under it: an open
//     network is asked about unless the posture is a blanket allow (--yolo);
//   - when the turn ends the harness looks at what is now on this machine and
//     the inventory learns it by observation. The agent never writes it (L13).
//
// A setup turn is never replayed and never touches an impossible item: the brief
// names installable lines only.

const (
	setupBusyWord    = "wait for this turn to finish or stop it before setting up this machine"
	setupAbsentWord  = "session: this conversation has no cell to set up"
	setupNothingWord = "nothing to set up: everything this chat uses is already here"
)

// SealState is what a session's seat knows about whether its last seal held.
// cellstore.SealWatch is the one implementation.
type SealState interface {
	// Failing reports whether the most recent seal failed.
	Failing() bool
	// Take hands over the next sentence worth saying, and "" when there is none.
	Take() string
}

// TurnEnder is what a SealState may also do: hear that the agent finished a turn
// and now waits for the person. The sync side uses it to upload at once instead
// of at the end of its window.
type TurnEnder interface {
	// TurnEnded is called once each time a turn is sealed. It must not wait.
	TurnEnded()
}

// Machine is the session's view of the device it runs on. preflight.Machine is
// the one implementation.
type Machine interface {
	// Plan is what this device lacks for the chat.
	Plan() preflight.Report
	// Settle records what is now on the device for the items the report listed.
	Settle(preflight.Report)
}

// SetupPlan is the preflight of this machine, and false where the session has
// no machine to look at.
func (a *Agent) SetupPlan() (preflight.Report, bool) {
	if a.config.Machine == nil {
		return preflight.Report{}, false
	}
	return a.config.Machine.Plan(), true
}

// SubmitSetup runs the setup turn for what SetupPlan lists as installable. It
// refuses, with no stream, where there is nothing to do or a turn is running.
func (a *Agent) SubmitSetup(ctx context.Context) (<-chan Event, error) {
	plan, ok := a.SetupPlan()
	switch {
	case !ok:
		return nil, errors.New(setupAbsentWord)
	case len(plan.Pending()) == 0:
		return nil, errors.New(setupNothingWord)
	}
	events, err := a.submitUser(ctx, setupMessage(plan))
	if err != nil {
		return nil, err
	}
	return settlingAfter(events, func() { a.config.Machine.Settle(plan) }), nil
}

// setupMessage is the turn's opening: the brief for the model, and one line of
// the person's own for the journal.
func setupMessage(plan preflight.Report) userMessage {
	user := userText(plan.SetupBrief())
	user.said = "set up this machine: " + strings.Join(pendingNames(plan), ", ")
	user.setup = true
	return user
}

func pendingNames(plan preflight.Report) []string {
	var names []string
	for _, it := range plan.Pending() {
		names = append(names, it.Name)
	}
	return names
}

// settlingAfter forwards a turn's events and runs done once the turn is over, before
// the stream closes, so whoever sees the close sees the settled inventory.
func settlingAfter(events <-chan Event, done func()) <-chan Event {
	out := make(chan Event)
	go func() {
		for e := range events {
			out <- e
		}
		done()
		close(out)
	}()
	return out
}

type setupKey struct{}

// turnContext is the context a turn starts under: a setup turn's carries the
// mark that puts its tool calls on the setup seat and under the network floor.
func (u userMessage) turnContext(ctx context.Context) context.Context {
	if u.setup {
		return context.WithValue(ctx, setupKey{}, true)
	}
	return ctx
}

func inSetup(ctx context.Context) bool {
	set, _ := ctx.Value(setupKey{}).(bool)
	return set
}

// setupFloor makes a setup turn's call ask when the gate would let it through:
// the network is open for the whole turn, and that is what the person is asked
// to allow. A blanket allow (--yolo, the allow posture) has already said yes.
func (a *Agent) setupFloor(ctx context.Context, decision approval.Decision) approval.Decision {
	if !inSetup(ctx) || decision.Action != approval.ActionAllow || a.approvalGate().Default == approval.ActionAllow {
		return decision
	}
	return approval.Decision{Action: approval.ActionPrompt, Rule: "a setup call opens the network"}
}
