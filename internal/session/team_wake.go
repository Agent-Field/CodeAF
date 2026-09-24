package session

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// ── A MEMBER THE MANAGER STARTED TAKES ITS FIRST TURN ON ITS OWN ────────────
//
// `team_start` writes a start to the team's Traffic, and the interface that
// holds the manager answers it by opening a new conversation BEHIND the one in
// front and putting it in the team under the manager's handle for it
// (internal/tui3's teamtraffic.go). Nobody types into that conversation: the
// person's words are theirs, and a brief sent as if they had typed it would
// outrank the manager and draw as the person's own line. So the brief has to
// reach the member by the one road the manager's words take, the Traffic
// delivery at a step boundary ([Agent.teamBoundary]), and there has to be a
// turn for that boundary to be in.
//
// THIS IS THAT TURN, and nothing else starts it. The interface and the session
// meet only through internal/teams, and over a session host the interface holds
// a client with no door for "start a turn with no words" (internal/remote). So
// the conversation watches for its own start: for a short window after it
// opens, and only on a profile where some team has a manager, it looks at the
// teams file (a stat, and a read only when the stat moved) until it finds
// itself a member with a handle and a start addressed to that handle. Then it
// reads what is addressed to it exactly as a boundary would, and if that is not
// empty it queues it as a note that wakes the conversation ([Agent.wakeLocked]).
// The watch ends at the first of: the wake, a turn begun some other way, the
// window running out, or the session closing.
//
// A conversation that is never started by a manager pays one stat of the teams
// file at open and, where a team has a manager, one stat every
// [teamWakeEvery] for [teamWakeFor].

const (
	// teamWakeEvery is how often the teams file is looked at while waiting.
	teamWakeEvery = 250 * time.Millisecond
	// teamWakeFor is how long a fresh conversation waits to be made a member.
	// The interface writes the member within a moment of opening it.
	teamWakeFor = 20 * time.Second
)

// watchTeamStart starts the watch described above, or does nothing.
func (a *Agent) watchTeamStart() {
	profile := a.config.teamProfile()
	// AND EVERY CONVERSATION THAT COULD BE IN A TEAM WATCHES ITS TRAFFIC for
	// the lines that wake it (team_wakewatch.go), which costs nothing while it
	// is in no managed team but a stat the whole process shares.
	a.watchTeamTraffic(profile)
	if profile == "" || !teamsHaveManager(profile) {
		return
	}
	// FRESH IS MEASURED FROM HERE: whatever the transcript holds at open, a
	// turn begun some other way grows it, and that ends the watch.
	// ONLY A CONVERSATION WITH NOTHING IN IT YET. One reopened with a history
	// is not being started; what wakes it later is the traffic watch.
	base, busy := a.teamWakeState()
	if busy || base > 0 {
		return
	}
	guard.Go("team start wake", func() { a.awaitTeamStart(profile, base) })
}

// teamsHaveManager reports whether any team in the profile has a manager.
func teamsHaveManager(profile string) bool {
	if !stampOf(teams.Path(profile)).present {
		return false
	}
	file, err := teams.Load(profile)
	if err != nil {
		return false
	}
	for _, t := range file.Teams {
		if t.Manager != "" {
			return true
		}
	}
	return false
}

// teamWakeState is how many messages the transcript holds past its system
// prompt, and whether a turn is running or the session has closed, which both
// end the watch.
func (a *Agent) teamWakeState() (int, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	said := 0
	for _, message := range a.messages {
		if message.Role != "system" {
			said++
		}
	}
	return said, a.running || a.closed
}

// awaitTeamStart is the watch's body, off every lock but for the moments it
// asks the agent whether it is still fresh.
func (a *Agent) awaitTeamStart(profile string, base int) {
	ticker := time.NewTicker(teamWakeEvery)
	defer ticker.Stop()
	deadline := time.Now().Add(teamWakeFor)
	for time.Now().Before(deadline) {
		<-ticker.C
		if grown, busy := a.teamWakeState(); busy || grown > base {
			return
		}
		if !a.teamStartWaiting(profile) {
			continue
		}
		if news := a.teamBoundary(); news != "" {
			// AT THE CAP THE BRIEF WAITS: it is queued without the wake, and
			// the member reads it at its first turn (team_cap.go).
			held := a.teamCapHold(profile, a.teamRoles()) != ""
			a.enqueueNote(userMessage{message: textMessage("user", news), wake: !held})
		}
		return
	}
}

// teamStartWaiting reports whether this conversation is a member with a
// handle, and a start addressed to that handle is in its team's recent traffic.
func (a *Agent) teamStartWaiting(profile string) bool {
	for _, role := range a.teamRoles() {
		if role.manager || role.handle == "" {
			continue
		}
		tail, err := teams.ReadTraffic(profile, role.id, "", teamFirstLook)
		if err != nil {
			continue
		}
		for _, entry := range tail {
			if entry.Kind == teams.KindStart && entry.To == role.handle &&
				!entry.At.Before(a.startedAt.Add(-teamStartGrace)) {
				return true
			}
		}
	}
	return false
}
