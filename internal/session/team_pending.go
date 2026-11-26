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
		// A paused membership holds its traffic unopened (team_pause.go).
		if role.paused && !role.manager {
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

// pendingTeamEntry reports whether an entry needs the conversation open: it
// would start a turn there by the watch's own rule ([teamEntryWakes]), or it
// carries a line to hand over that the rule has not set aside.
func pendingTeamEntry(profile string, role teamRole, entry teams.Entry) bool {
	// A closed conversation has no birth to test a start against, so its own
	// start is the one sent to its handle, or the one its membership names.
	own := !role.manager && entry.Kind == teams.KindStart && (entry.To == role.handle || (role.startID != "" && entry.ID == role.startID))
	wakes, quiet := teamEntryWakes(profile, role, entry, own)
	return wakes || (!quiet && teamLine(role, entry) != "")
}
