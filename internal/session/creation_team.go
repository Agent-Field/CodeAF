package session

import (
	"context"
	"fmt"

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

// acceptCreation checks a stated creation context at the moment the message is
// accepted, and refuses the message whole when it names a team this
// conversation was not sent for.
//
// THE CHECK IS OF THE CLAIM, NOT A SOURCE FOR IT. nil and an explicit NONE pass
// without a lookup. A team id must be a team in this engine's own teams file
// and that team must hold this conversation, in either spelling of its key: a
// forged id, a team this conversation is not in, or a conversation that keeps
// no teams (a task, a session with no transcript) is refused as a mark that
// does not fit, before anything is recorded. A CLOSED OR ARCHIVED TEAM IS
// ACCEPTED: whether a closed crew may be messaged is the door's decision, and
// provenance never forbids what the door allowed. Once accepted the id is kept
// whatever later happens to the team.
func (a *Agent) acceptCreation(creation *CreationContext) error {
	if creation == nil || creation.Team == "" {
		return nil
	}
	if !creationTeamIDOK(creation.Team) {
		return fmt.Errorf("%w: the message's team is not a team id", ErrMessageMark)
	}
	profile := a.config.teamProfile()
	if profile == "" {
		return fmt.Errorf("%w: this conversation keeps no teams, so it cannot take a message sent to one", ErrMessageMark)
	}
	a.team.mu.Lock()
	keys := append([]string(nil), a.teamKeysLocked()...)
	a.team.mu.Unlock()
	file, err := teams.Load(profile)
	if err != nil {
		return fmt.Errorf("%w: the message's team could not be checked", ErrMessageMark)
	}
	team, ok := file.Team(creation.Team)
	if !ok {
		return fmt.Errorf("%w: the message's team does not exist", ErrMessageMark)
	}
	for _, key := range keys {
		if team.Holds(key) {
			return nil
		}
	}
	return fmt.Errorf("%w: the message's team does not hold this conversation", ErrMessageMark)
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
