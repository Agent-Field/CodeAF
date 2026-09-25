package remote

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Agent-Field/codeaf/internal/session"
)

// ── THE WALL'S TWO MODEL ASKS, BOTH HALVES ──────────────────────────────────
//
// wire_teams.go says what [MethodTeamsName] and [MethodTeamsPropose] are and
// why they carry a budget. This is the engine answering them from the agent it
// has open, and the surface asking them, in a file of their own for teams.go's
// reason.

// teamAskDoor is the engine agent's two asks, as internal/tui3 asserts them
// (its teamNamer and teamProposer). The whole pair is advertised or neither:
// an engine that could name a team and not organize one would still have to
// say which in a flag of its own, and no engine is built that way.
type teamAskDoor interface {
	NameTeam(ctx context.Context, titles []string) (string, error)
	ProposeTeams(ctx context.Context, in session.TeamProposalInput) (session.TeamProposal, error)
}

// teamAskKnown is [Welcome.TeamAsk]: whether the agent this engine has open
// answers both asks.
func teamAskKnown(agent any) bool { _, ok := agent.(teamAskDoor); return ok }

// teamAskOffWord is an engine that does not answer the asks. The wall never
// shows it; it is the error the wall's ask fails with, which it reads as no
// suggestion.
const teamAskOffWord = "this engine cannot suggest team names or teams"

// teamAskCeiling bounds a peer's request while leaving the wall's five-second
// name wait and ten-second Organize wait in control of their own calls.
const teamAskCeiling = 30 * time.Second

// teamAskWithin uses the shorter of the wall's budget and the engine's ceiling.
func teamAskWithin(budget time.Duration) (context.Context, context.CancelFunc) {
	if budget == 0 || budget > teamAskCeiling {
		budget = teamAskCeiling
	}
	return context.WithTimeout(context.Background(), budget)
}

// teamAskCall answers the two asks from agent, and says whether the method was
// one of them at all. A false hands the call on to the refusal an engine from
// before these doors answers.
func teamAskCall(agent WrappedAgent, call Frame) (json.RawMessage, bool, error) {
	switch call.Method {
	case MethodTeamsName, MethodTeamsPropose:
	default:
		return nil, false, nil
	}
	door, ok := agent.(teamAskDoor)
	if !ok {
		return nil, true, errors.New(teamAskOffWord)
	}
	if call.Method == MethodTeamsName {
		args, err := arg[TeamNameArgs](call)
		if err != nil {
			return nil, true, err
		}
		if args.Budget < 0 {
			return nil, true, context.DeadlineExceeded
		}
		ctx, cancel := teamAskWithin(args.Budget)
		defer cancel()
		name, err := door.NameTeam(ctx, args.Titles)
		if err != nil {
			return nil, true, err
		}
		payload, err := json.Marshal(name)
		return payload, true, err
	}
	args, err := arg[TeamProposeArgs](call)
	if err != nil {
		return nil, true, err
	}
	if args.Budget < 0 {
		return nil, true, context.DeadlineExceeded
	}
	ctx, cancel := teamAskWithin(args.Budget)
	defer cancel()
	proposal, err := door.ProposeTeams(ctx, args.In)
	if err != nil {
		return nil, true, err
	}
	payload, err := json.Marshal(proposal)
	return payload, true, err
}

// teamAskBudget is what is left of ctx's deadline, for the engine to bound the
// model call by, and false when nothing is left to spend.
func teamAskBudget(ctx context.Context) (time.Duration, bool) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return 0, true
	}
	left := time.Until(deadline)
	return left, left > 0
}

// NameTeam asks the engine's naming role for a short name for a group of
// conversations, given their titles: the wall's teamNamer door, over the wire.
// An engine without the ask is refused here, before anything is written.
//
// THE WALL'S DEADLINE BOUNDS THE CALL as well as [callDeadline] does: the wall
// waits teamNameWait and no longer, so the call gives up with it and the
// engine is told the same span, rather than answering a card that has already
// kept its word.
func (a *Agent) NameTeam(ctx context.Context, titles []string) (string, error) {
	if !a.c.Welcome().TeamAsk {
		return "", errors.New(teamAskOffWord)
	}
	budget, ok := teamAskBudget(ctx)
	if !ok {
		return "", context.DeadlineExceeded
	}
	payload, err := a.c.call(ctx, MethodTeamsName, TeamNameArgs{Titles: titles, Budget: budget})
	if err != nil {
		return "", err
	}
	var name string
	if err := json.Unmarshal(payload, &name); err != nil {
		return "", err
	}
	return name, nil
}

// ProposeTeams asks the engine's naming role which teams the conversations in
// in could form: the wall's teamProposer door, over the wire, refused here for
// an engine without it as [Agent.NameTeam] is.
func (a *Agent) ProposeTeams(ctx context.Context, in session.TeamProposalInput) (session.TeamProposal, error) {
	if !a.c.Welcome().TeamAsk {
		return session.TeamProposal{}, errors.New(teamAskOffWord)
	}
	budget, ok := teamAskBudget(ctx)
	if !ok {
		return session.TeamProposal{}, context.DeadlineExceeded
	}
	payload, err := a.c.call(ctx, MethodTeamsPropose, TeamProposeArgs{In: in, Budget: budget})
	if err != nil {
		return session.TeamProposal{}, err
	}
	var out session.TeamProposal
	if err := json.Unmarshal(payload, &out); err != nil {
		return session.TeamProposal{}, err
	}
	return out, nil
}
