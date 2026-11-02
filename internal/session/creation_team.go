package session

import (
	"context"
	"path/filepath"
	"strings"
	"sync/atomic"

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
//
// mixed is set when a person's message with a DIFFERENT context — or none — is
// spliced into the running turn ([Agent.mixCreationLocked]). From then on the
// turn answers more than one message and cannot say which one an automation
// was made for, so it is unknown: never the opener's crew, never a false none.
type creationTeamStamp struct {
	id    string
	mixed atomic.Bool
}

// withCreationTeam hangs the stamp for a message whose door stated a context.
// ID "" is an explicit NONE.
func withCreationTeam(ctx context.Context, teamID string) context.Context {
	ctx, _ = withCreationStamp(ctx, teamID)
	return ctx
}

// withCreationStamp is withCreationTeam answering the stamp it hung, so the
// turn that hangs it can mark it mixed later.
func withCreationStamp(ctx context.Context, teamID string) (context.Context, *creationTeamStamp) {
	stamp := &creationTeamStamp{id: teamID}
	return context.WithValue(ctx, creationTeamKey{}, stamp), stamp
}

// mixCreationLocked records that a person's message is being spliced into the
// running turn. A message stating the turn's own context changes nothing; any
// other — another team, an explicit none into a crew turn, or one stating
// nothing — makes the turn's creation team unknown. The caller holds a.mu.
func (a *Agent) mixCreationLocked(user userMessage) {
	stamp := a.turnCreation
	if stamp == nil {
		return
	}
	if creation := user.creationContext(); creation == nil || creation.Team != stamp.id {
		stamp.mixed.Store(true)
	}
}

// creationTeamFrom is the stamped team id and whether a stamp exists at all.
//
// NO STAMP IS UNKNOWN, NEVER NONE. A turn whose opening message stated no
// context (a plain Submit, an older door, a task's note, an automation's run, a
// line recorded before the field) carries nothing on its context, and an
// automation made in it must stay unknown rather than be upgraded to "made in
// no team".
func creationTeamFrom(ctx context.Context) (id string, known bool) {
	s, ok := ctx.Value(creationTeamKey{}).(*creationTeamStamp)
	if !ok || s.mixed.Load() {
		return "", false
	}
	return s.id, true
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
	keys := creationKeys(a.config.transcriptPath())
	file, err := teams.LoadTeams(profile, creation.Team)
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

// acceptedCreation is a journaled acceptance stamp as a resumed turn may reuse
// it: a copy when its shape is a creation context, nil (unknown) when it is
// not. It reads no teams file; the stamp was proven when it was written.
func acceptedCreation(stamp *CreationContext) *CreationContext {
	if stamp == nil || (stamp.Team != "" && !creationTeamIDOK(stamp.Team)) {
		return nil
	}
	return stamp.clone()
}

// creationKeys is every spelling the teams file may know this conversation's
// journal by: the path cleaned, the path with every link resolved, and — for a
// journal that does not exist yet, as a crew start's manager's on its very
// first message — its folder resolved and its own name, the spelling the app's
// start writes. It takes no lock, so a door holding a.mu may ask it.
func creationKeys(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	clean := filepath.Clean(path)
	keys := []string{clean}
	if real, err := filepath.EvalSymlinks(clean); err == nil {
		keys = append(keys, filepath.Clean(real))
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(clean)); err == nil {
		keys = append(keys, filepath.Join(filepath.Clean(dir), filepath.Base(clean)))
	}
	return keys
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
