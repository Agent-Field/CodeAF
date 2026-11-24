package session

// ── A PAUSED MEMBER IS HELD BEFORE ITS TURN STARTS ──────────────────────────
//
// The person can pause a member of a team ([teams.Member.Paused]). Until this
// file the pause lived in one interface's own file and the engine knew nothing
// of it: a directive opened the member, started its turn and logged "woke",
// and the interface then stopped the turn it found running. The manager was
// told the member had started, the directive was consumed, and resuming the
// member replayed nothing. The measured case was a manager that handed the
// same work to a paused member twice and reported it under way both times.
//
// SO THE PAUSE IS READ WHERE THE WAKE IS DECIDED. Team traffic starts no turn
// for a paused membership: the watch remembers that something asked for one,
// leaves the lines undelivered, and starts the turn on the first look that
// finds the pause lifted. A conversation that is not open is not opened for
// traffic it would only hold ([TeamOpenPending]), and `team_send` does not ask
// for it to be opened. Something else may still open it (a question's answer,
// a wake the retry queue kept); it is then open and held, which costs nothing.
//
// A PAUSE IS THE PERSON'S WORD ABOUT THE CONVERSATION, so it holds in every
// team the conversation is a member of ([teams.File.PausedAnywhere]), not only
// the one it was pressed in: an interface that finds the member paused ends a
// turn it did not start, whichever team's traffic began it.
//
// THE SENDER SAYS SO, ONCE, AT THE SEND. `team_send` reads the same record and
// answers that the member is paused, and writes the one traffic line that says
// the directive is held ([teamHeldPausedText]). The paused member writes
// nothing, so the log carries one account whether or not anything has it open.
//
// THE PERSON'S OWN WORDS ARE NOT HELD, as with the daily cap (team_cap.go): a
// person typing into a paused member's conversation starts its turn, and that
// turn reads what was waiting.

import (
	"strings"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// teamPausedWhy is why a paused member did not start, in the words the manager
// and the traffic are both given.
const teamPausedWhy = "the person paused it; it starts on this when they resume it"

// teamHeldPausedText is the traffic line for a directive a paused member was
// sent: "held @mac-dev: the person paused it; …".
func teamHeldPausedText(handle string) string {
	return teamHeldPrefix + "@" + handle + ": " + teamPausedWhy
}

// teamPauseHoldLocked reports whether role is paused, and remembers that its
// traffic asked for a turn when asked is true. The caller holds a.team.mu.
func (a *Agent) teamPauseHoldLocked(role teamRole, asked bool) bool {
	if !role.paused || role.manager {
		return false
	}
	if asked {
		w := &a.team.watch
		if w.pausedHeld == nil {
			w.pausedHeld = map[string]bool{}
		}
		w.pausedHeld[role.id] = true
		// THE HOLD OUTLIVES THIS PROCESS. A member that never took a turn in
		// this team has no cursor written down, and one opened later would
		// start reading after the line it is held on. So the place it would
		// have started from is written now, once.
		a.readTeamCursorsLocked()
		if _, known := a.team.cursors[role.id]; !known {
			a.team.cursors[role.id] = a.firstTeamCursor(a.config.teamProfile(), role)
			a.saveTeamCursorsLocked()
		}
	}
	return true
}

// teamPauseReleasedLocked reports whether role owes a turn that was held while
// it was paused, and forgets the hold. The caller holds a.team.mu.
func (a *Agent) teamPauseReleasedLocked(role teamRole) bool {
	w := &a.team.watch
	if role.paused || !w.pausedHeld[role.id] {
		return false
	}
	delete(w.pausedHeld, role.id)
	return true
}

// teamOwnStartWakes reports whether entry is the start line that owes this
// member its first turn: its own start, unless the membership was made on
// standby, whose start line is a role to read and not work to begin.
func (a *Agent) teamOwnStartWakes(role teamRole, entry teams.Entry) bool {
	return !role.standby && a.teamOwnStart(role, entry)
}

// teamStandbyText is the traffic line a member opened on standby writes, once:
// "opened @mac-dev on standby; …".
func teamStandbyText(handle string) string {
	return "opened @" + handle + " on standby; it starts on its manager's first directive"
}

// teamStandbySay writes that line unless the team's recent traffic already
// carries it: a conversation with nothing in it yet is fresh every time it is
// opened, and the line is said once.
func (a *Agent) teamStandbySay(profile string, role teamRole) {
	text := teamStandbyText(role.handle)
	tail, err := teams.ReadTraffic(profile, role.id, "", teamFirstLook)
	if err != nil {
		return
	}
	for _, entry := range tail {
		if entry.Kind == teams.KindEvent && entry.Member == role.key && entry.Text == text {
			return
		}
	}
	a.teamSay(profile, role.id, teams.Entry{
		Kind: teams.KindEvent, From: teams.FromSystem, To: role.handle, Member: role.key,
		State: teams.StateIdle, Text: text,
	})
}

// teamStandbyOver clears a member's standby once its start line is handed to
// it ([teams.Member.Standby]), off the boundary's lock. A write that fails
// leaves the record saying standby a little longer; the line was delivered.
func (a *Agent) teamStandbyOver(profile string, role teamRole) {
	id, key := role.id, role.key
	guard.Go("team standby over", func() {
		_ = teams.UpdateOpen(profile, func(file *teams.File) error { return file.SetStandby(id, key, false) })
	})
}

// markPaused marks each member of team the person paused in another of its
// teams ([teams.MemberState].Paused), so this team's manager reads why its
// directive does not start.
func markPaused(file *teams.File, team teams.Team, states map[string]teams.MemberState) {
	if file == nil {
		return
	}
	for _, member := range team.Members {
		if member.Key == team.Manager || member.Paused {
			continue
		}
		if _, paused := file.PausedAnywhere(member.Key); paused {
			state := states[member.Key]
			state.Paused = true
			states[member.Key] = state
		}
	}
}

// teamAt is members as their handles, "@web, @api".
func teamAt(members []teams.Member) string {
	var out []string
	for _, member := range members {
		out = append(out, "@"+member.Handle)
	}
	return strings.Join(out, ", ")
}

// teamStoppedByPersonLine is what a manager is told, after the member's
// handle, when the person stopped a turn the manager's traffic had started.
const teamStoppedByPersonLine = "'s turn on what you handed it was " + teams.StoppedByPerson + " before it finished, so that work is not done. The person chose to stop it: tell them where it stands, and do not hand it out again unless they ask."

// teamPausedSince reads the teams file afresh and returns the roles among
// roles that are still not paused, remembering a hold for each that now is.
func (a *Agent) teamPausedSince(profile string, roles []teamRole) []teamRole {
	a.team.mu.Lock()
	defer a.team.mu.Unlock()
	fresh := map[string]teamRole{}
	for _, role := range a.teamRolesLocked(profile) {
		fresh[role.id] = role
	}
	kept := roles[:0:0]
	for _, role := range roles {
		now, still := fresh[role.id]
		if !still {
			continue
		}
		if a.teamPauseHoldLocked(now, true) {
			continue
		}
		kept = append(kept, now)
	}
	return kept
}

// teamStoppedByPerson reports whether a stopped turn's ending is said as the
// person's ([teams.StoppedByPerson]), which wakes the manager: the person
// stopped work that team traffic had started, and nothing else tells the
// manager it ended without a report.
//
// THREE THINGS MUST HOLD. The cancel came through the person's door. The turn
// was not opened by the person's own message: a person who types into a member
// and stops it has stopped their own request, which the manager never handed
// out. And no manager's stop is on the log for this turn: an interface holding
// the member performs the manager's `team_stop` through the same door as the
// person's stop key (the app, the terminal, a remote window), usually before
// the member's own session reads the line, so the door alone would call the
// manager's stop the person's.
func (a *Agent) teamStoppedByPerson(door StopDoor, origin turnOrigin) bool {
	if door != StopByPerson || origin.turn != "" {
		return false
	}
	profile := a.config.teamProfile()
	if profile == "" {
		return false
	}
	a.mu.Lock()
	turnAt := a.teamTurnAt
	a.mu.Unlock()
	for _, role := range a.teamRoles() {
		if !role.managed || role.manager || role.handle == "" {
			continue
		}
		tail, err := teams.ReadTraffic(profile, role.id, "", teamFirstLook)
		if err != nil {
			continue
		}
		for _, entry := range tail {
			if entry.Kind == teams.KindStop && entry.From == teams.FromManager &&
				(entry.Member == role.key || entry.Member == "" && entry.To == role.handle) &&
				!entry.At.Before(turnAt) {
				return false
			}
		}
	}
	return true
}
