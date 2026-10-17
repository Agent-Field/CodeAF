package session

import "context"

// creationTeamKey carries what the door that accepted the person's message
// stamped about the team it was sent for. It is hung once when the turn opens
// from that message and never re-read from the current manager, founder,
// membership or workspace.
type creationTeamKey struct{}

// creationTeamStamp is the stamp. Its presence on a context IS the fact that
// the turn came from a newly accepted person message; ID empty then says that
// message was ordinary, not sent to any team.
type creationTeamStamp struct{ id string }

// withCreationTeam hangs the stamp for a newly accepted person message.
// ID "" is an explicit NONE.
func withCreationTeam(ctx context.Context, teamID string) context.Context {
	return context.WithValue(ctx, creationTeamKey{}, creationTeamStamp{id: teamID})
}

// creationTeamFrom is the stamped team id and whether a stamp exists at all.
//
// NO STAMP IS UNKNOWN, NEVER NONE. A turn that did not open from a newly
// accepted person message (a replay, a task's note, an automation's run, any
// door that predates the stamp) carries nothing on its context, and an
// automation made in it must stay unknown rather than be upgraded to "made in
// no team".
func creationTeamFrom(ctx context.Context) (id string, known bool) {
	s, ok := ctx.Value(creationTeamKey{}).(creationTeamStamp)
	return s.id, ok
}
