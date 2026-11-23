package session

import (
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// TeamOpenPending asks the same delivery cursors and traffic rules as an opened
// conversation, without opening its journal or claiming ownership. The caller
// subscribes to traffic before checking, and carries since into the opener so
// a conversation with no saved cursor still reads the event that opened it.
func TeamOpenPending(profile, transcript string, file *teams.File, defaults teams.Defaults, since time.Time) bool {
	probe := &Agent{config: Config{Place: Place{Dir: filepath.Dir(transcript)}, SessionFile: transcript}, startedAt: since}
	roles := rolesFor(file, probe.teamKeysLocked(), defaults)
	probe.readTeamCursorsLocked()
	for _, role := range roles {
		if role.handle == "" && !role.manager {
			continue
		}
		cursor, ok := probe.team.cursors[role.id]
		if !ok {
			cursor = probe.firstTeamCursor(profile, role)
		}
		for {
			entries, err := teams.ReadTraffic(profile, role.id, cursor, teamPageLimit)
			if err != nil {
				return true
			}
			for _, entry := range entries {
				if pendingTeamEntry(profile, role, entry) {
					return true
				}
			}
			if len(entries) < teamPageLimit {
				break
			}
			cursor = entries[len(entries)-1].ID
		}
	}
	return false
}

// pendingTeamEntry shares the engine's wake rules, including the finished
// event it suppresses after a reply. A member's own start also needs opening.
func pendingTeamEntry(profile string, role teamRole, entry teams.Entry) bool {
	if role.manager && entry.Kind == teams.KindEvent && entry.State == teams.StateFinished && teamFinishedAfterReply(profile, role.id, entry) {
		return false
	}
	if !role.manager && entry.Kind == teams.KindStart && entry.To == role.handle {
		return true
	}
	return teamWakes(role, entry) || teamPacketWakes(profile, role, entry) || teamLine(role, entry) != ""
}
