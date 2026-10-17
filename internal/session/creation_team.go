package session

import (
	"context"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// creationTeamKey carries the creation context the door that accepted the
// person's message stated ([CreationContext]). It is hung once when the turn
// opens from that message and never re-read from the current manager, founder,
// membership or workspace.
type creationTeamKey struct{}

// creationTeamStamp is the stamp. Its presence on a context IS the fact that
// the turn's opening message carried a creation context; ID empty then says
// that message was an ordinary one, sent to no team.
type creationTeamStamp struct{ id string }

// withCreationTeam hangs the stamp for a message whose door stated a context.
// ID "" is an explicit NONE.
func withCreationTeam(ctx context.Context, teamID string) context.Context {
	return context.WithValue(ctx, creationTeamKey{}, creationTeamStamp{id: teamID})
}

// creationTeamFrom is the stamped team id and whether a stamp exists at all.
//
// NO STAMP IS UNKNOWN, NEVER NONE. A turn whose opening message stated no
// context (a plain Submit, an older door, a task's note, an automation's run, a
// line recorded before the field) carries nothing on its context, and an
// automation made in it must stay unknown rather than be upgraded to "made in
// no team".
func creationTeamFrom(ctx context.Context) (id string, known bool) {
	s, ok := ctx.Value(creationTeamKey{}).(creationTeamStamp)
	return s.id, ok
}

// creationContext is the context the turn this message opens carries: the one
// read back from its recorded line when it is asked again, else the one its
// door stated on its mark, else nil (unknown).
func (u userMessage) creationContext() *CreationContext {
	if u.creation != nil {
		return u.creation
	}
	if u.person != nil {
		return u.person.Creation
	}
	return nil
}

// provenCreation is the creation context a message is accepted with: the one
// its door stated when this engine can prove it, and UNKNOWN (nil) when it
// cannot.
//
// PROVENANCE IS NOT PERMISSION. Whether the message may be sent at all is the
// door's decision and is never changed here: a context this engine cannot
// prove is dropped to unknown and the message goes through exactly as it did
// before contexts existed. nil stays nil and an explicit NONE stays NONE without
// a lookup. A team id is kept only when it is a team id, the team is in this
// engine's own teams file, and that team holds this conversation in either
// spelling of its key. A forged id, a team this conversation is not in, a
// team in another machine's profile, an unreadable file or a conversation that
// keeps no teams (a task, a session with no transcript) all become unknown —
// never NONE, never a team guessed from who manages what now. A CLOSED OR
// ARCHIVED TEAM IS KEPT: the door accepted the message, and once kept the id
// is never checked or moved again.
func (a *Agent) provenCreation(creation *CreationContext) *CreationContext {
	if creation == nil {
		return nil
	}
	if creation.Team == "" {
		return &CreationContext{}
	}
	if !creationTeamIDOK(creation.Team) {
		return nil
	}
	profile := a.config.teamProfile()
	if profile == "" {
		return nil
	}
	a.team.mu.Lock()
	keys := append([]string(nil), a.teamKeysLocked()...)
	a.team.mu.Unlock()
	file, err := teams.Load(profile)
	if err != nil {
		return nil
	}
	team, ok := file.Team(creation.Team)
	if !ok {
		return nil
	}
	for _, key := range keys {
		if team.Holds(key) {
			return creation.clone()
		}
	}
	return nil
}

// creationTeamIDOK matches the teams directory-name law (letters, digits, _
// and -, at most 64 bytes) without exporting teams' private checker.
func creationTeamIDOK(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
