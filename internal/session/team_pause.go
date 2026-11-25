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
// AND WHERE THE TRAFFIC IS DELIVERED. A turn that is not the person's own reads
// none of a paused member's teams' lines and moves no cursor
// ([Agent.teamNewsLocked]): its first request used to carry the held directive,
// and the resume then had nothing to start on. A turn that does deliver them,
// the person's own, ends the hold, so the resume starts no second turn on
// lines already read.
//
// AND NOTHING ELSE STARTS A TURN EITHER. The pause was decided for traffic and
// for nothing else: a background command ending, a watch firing, a task landing
// and news recorded as a turn ended each started the paused member's turn, and
// only an interface that happened to hold the member ended it again
// (cmd/codeaf's appwakes.go), after it had begun and where no interface held
// it, not at all. The engine decides those too now, by the cause of the news
// ([wakeStanding]): what arrived is kept in the conversation as a record that
// starts no turn, with [teamPauseHeldRule] saying why, exactly as a stop keeps
// a command's ending (team_stophold.go). The next turn reads it: the person's
// own words, or the turn Resume starts on a directive that waited. The
// interface's stop stays as it was, for a wake this file does not decide.
//
// A PAUSE IS THE PERSON'S WORD ABOUT THE CONVERSATION, so it holds in every
// team the conversation is a member of ([teams.File.PausedAnywhere]), not only
// the one it was pressed in: an interface that finds the member paused ends a
// turn it did not start, whichever team's traffic began it.
//
// THE SENDER SAYS SO, ONCE, AT THE SEND. `team_send` reads the same record and
// answers that the member is paused, and writes the one traffic line that says
// the directive is held ([teamHeldPausedText]). The paused member writes
// nothing for a directive, so the log carries one account whether or not
// anything has it open. A start has no send behind it: a member paused before
// its first turn writes that line itself, once ([Agent.teamWakeStart]).
//
// THE PERSON'S OWN WORDS ARE NOT HELD, as with the daily cap (team_cap.go): a
// person typing into a paused member's conversation starts its turn, and that
// turn reads what was waiting.

import (
	"strings"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// teamPausedWhy is why a paused member did not start, in the words the manager
// and the traffic are both given.
const teamPausedWhy = "the person paused it; it starts on this when they resume it"

// teamPauseHeldRule is said with news that started no turn because the person
// had the conversation paused.
const teamPauseHeldRule = "This arrived while the person had this conversation paused, so it started no turn. It is a record of what happened."

// teamPauseHold is one conversation's pause, for the wakes that are decided
// under the agent's lock and may read neither the teams file nor the seat's
// lock there.
type teamPauseHold struct {
	// paused is whether the person has this conversation paused in any of its
	// teams, as of the last time its roles were read. Read without a lock or a
	// disk.
	paused atomic.Bool
}

// pausedRoles reports whether any of roles is paused. A pause holds the
// conversation in every team it is a member of ([rolesFor]), so one is all.
func pausedRoles(roles []teamRole) bool {
	for _, role := range roles {
		if role.paused {
			return true
		}
	}
	return false
}

// teamPauseFresh reads this conversation's roles again when the teams store has
// moved, so the pause a wake is about to be decided on is the one on the
// record now and not the one the last boundary read. It costs one look at the
// store's stamp for each piece of news a pause could hold, and nothing for a
// conversation that can be in no team. It takes the seat's lock and must not
// be called under a.mu.
func (a *Agent) teamPauseFresh() { a.teamRoles() }

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

// teamStandbyText is the traffic line a member opened on standby writes, once:
// "opened @mac-dev on standby; …".
func teamStandbyText(handle string) string {
	return "opened @" + handle + " on standby; it starts on its manager's first directive"
}

// teamAutoWakeOffText is the traffic line a member started into a team whose
// auto-wake is off writes: it is open, and nothing started its turn.
func teamAutoWakeOffText(handle string) string {
	return "opened @" + handle + "; this team's auto-wake is off, so no turn was started. It reads the brief when it next runs."
}

// teamSaidAlready reports whether team's recent traffic already carries this
// line about this member, answering the same entry. A conversation with
// nothing in it yet is fresh every time it is opened, and both watches may
// meet the same hold, so a hold is said once for what it holds. A log that
// cannot be read is not written to either.
func teamSaidAlready(profile, team string, line teams.Entry) bool {
	tail, err := teams.ReadTraffic(profile, team, "", teamFirstLook)
	if err != nil {
		return true
	}
	for _, entry := range tail {
		if entry.Kind == teams.KindEvent && entry.Member == line.Member && entry.Text == line.Text && entry.Answers == line.Answers {
			return true
		}
	}
	return false
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

// teamPausedSplit reads the teams file afresh and divides roles into those
// still not paused and those that now are, remembering a hold for each of the
// latter. A role this conversation no longer has is in neither.
func (a *Agent) teamPausedSplit(profile string, roles []teamRole) (kept, paused []teamRole) {
	a.team.mu.Lock()
	defer a.team.mu.Unlock()
	fresh := map[string]teamRole{}
	for _, role := range a.teamRolesLocked(profile) {
		fresh[role.id] = role
	}
	for _, role := range roles {
		now, still := fresh[role.id]
		if !still {
			continue
		}
		if a.teamPauseHoldLocked(now, true) {
			paused = append(paused, now)
			continue
		}
		kept = append(kept, now)
	}
	return kept, paused
}

// teamHandedLocked notes that role's manager handed this conversation work:
// a directive, or the brief a started member begins on. A standby member's
// start line is its role, not work. The caller holds a.team.mu.
func (a *Agent) teamHandedLocked(role teamRole, entry teams.Entry) {
	if role.manager || entry.From != teams.FromManager {
		return
	}
	if entry.Kind != teams.KindDirective && (entry.Kind != teams.KindStart || role.standby) {
		return
	}
	if a.team.handed == nil {
		a.team.handed = map[string]time.Time{}
	}
	// THE TIME IS THE LATEST HAND-OFF'S. A manager's stop on the log since
	// this time is read as the stop of the turn being stopped
	// ([Agent.teamStoppedByPerson]). Kept at the first hand-off while work
	// carried from turn to turn, it let a manager's stop that had found the
	// member idle, and so stopped nothing, pass for the stop of a turn a later
	// directive started, and the person's stop of that turn told nobody.
	a.team.handed[role.id] = time.Now()
}

// teamHandedTake is the teams whose handed-out work this conversation is on,
// with when it was handed, taken: the work ends here, stopped or finished with
// nothing left to wait for ([Agent.teamHandedCarried]).
func (a *Agent) teamHandedTake() map[string]time.Time {
	a.team.mu.Lock()
	defer a.team.mu.Unlock()
	handed := a.team.handed
	a.team.handed = nil
	return handed
}

// teamStoppedByPerson is the teams in which a stopped turn's ending is said as
// the person's ([teams.StoppedByPerson]), which wakes that team's manager: the
// person stopped work the manager had handed out, and nothing else tells the
// manager it ended without a report.
//
// THREE THINGS MUST HOLD, PER TEAM. The cancel came through the person's door;
// the app's own stops (quitting, a paused member's stray wake) name another.
// The conversation was on that manager's work, whoever opened the turn: a
// directive read at a step of a turn the person had started is as undone by
// their stop as one that woke the member, and so is work that waits on a
// command while the person's own turn is stopped. A turn with no handed work
// alive is theirs alone. And no manager's stop is on that team's log since the work
// was handed over: an interface holding the member performs the manager's
// `team_stop` through the same door as the person's stop key (the app, the
// terminal, a remote window), usually before the member's own session reads
// the line, so the door alone would call the manager's stop the person's.
func (a *Agent) teamStoppedByPerson(door StopDoor, handed map[string]time.Time) map[string]bool {
	profile := a.config.teamProfile()
	if door != StopByPerson || profile == "" || len(handed) == 0 {
		return nil
	}
	var by map[string]bool
	for _, role := range a.teamRoles() {
		since, was := handed[role.id]
		if !was || !role.managed || role.manager || role.shared || role.handle == "" {
			continue
		}
		tail, err := teams.ReadTraffic(profile, role.id, "", teamFirstLook)
		if err != nil {
			continue
		}
		managers := false
		for _, entry := range tail {
			if entry.Kind == teams.KindStop && entry.From == teams.FromManager &&
				(entry.Member == role.key || entry.Member == "" && entry.To == role.handle) &&
				!entry.At.Before(since) {
				managers = true
			}
		}
		if !managers {
			if by == nil {
				by = map[string]bool{}
			}
			by[role.id] = true
		}
	}
	return by
}

// teamHeldFirst is the oldest directive in tail that was held for role because
// the person had paused it ([teamHeldPausedText]), "" when there is none.
func teamHeldFirst(role teamRole, tail []teams.Entry) string {
	if role.manager || role.handle == "" {
		return ""
	}
	first := ""
	for _, entry := range tail {
		if entry.Kind != teams.KindEvent || entry.From != teams.FromSystem || entry.Member != role.key || entry.Answers == "" {
			continue
		}
		if !strings.HasPrefix(entry.Text, teamHeldPrefix) || !strings.HasSuffix(entry.Text, teamPausedWhy) {
			continue
		}
		if first == "" || entry.Answers < first {
			first = entry.Answers
		}
	}
	return first
}
