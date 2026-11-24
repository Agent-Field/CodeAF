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
// traffic it would only hold ([TeamOpenPending]), and the writer's rouse
// leaves it closed ([Agent.teamRouse]).
//
// THE SENDER SAYS SO, ONCE, AT THE SEND. `team_send` reads the same record and
// answers that the member is paused, and writes the one traffic line that says
// the directive is held ([teamHeldPausedText]). The paused member writes
// nothing, so the log carries one account whether or not anything has it open.
//
// THE PERSON'S OWN WORDS ARE NOT HELD, as with the daily cap (team_cap.go): a
// person typing into a paused member's conversation starts its turn, and that
// turn reads what was waiting.

import "github.com/Agent-Field/codeaf/internal/teams"

// teamPausedWhy is why a paused member did not start, in the words the manager
// and the traffic are both given.
const teamPausedWhy = "the person paused it; it starts on this when they resume it"

// teamHeldPausedText is the traffic line for a directive a paused member was
// sent: "held @mac-dev: the person paused it; …".
func teamHeldPausedText(handle string) string { return "held @" + handle + ": " + teamPausedWhy }

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
