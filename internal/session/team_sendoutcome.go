package session

// ── A DIRECTIVE'S ANSWER SAYS WHAT HAPPENED TO IT ───────────────────────────
//
// `team_send` used to answer every directive with one sentence written before
// anything was known: "a member that is idle starts a turn on it now". A
// manager is a model, and a model reports what its tools tell it. The measured
// case told the person "I've started the setup" about a member that never
// began, twice, and then found an empty folder.
//
// SO THE ANSWER WAITS FOR THE EVIDENCE, BRIEFLY. A wake writes a line to the
// team's traffic that answers the directive by its id (team_wakewatch.go), a
// refused wake writes why, a paused member is known from the teams file before
// anything is asked, and a member in a turn is known from its own journal. The send looks at the traffic for a few seconds
// and says, per member, one of: started, in a turn already, did not start and
// why, or no sign of a start yet. The last is said as plainly as the rest: a
// manager told "no sign yet" checks before it reports.
//
// A REFUSAL IS NOT BELIEVED UNTIL THE WAIT IS OVER. Two processes may try to
// open one member, and the one that cannot may say so a moment before the one
// that can says it did; a start that arrives inside the wait is the answer.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// teamSendSettle is how long a directive's answer waits to see its members
// start. A member some process holds starts within a second ([teamWatchEvery]);
// one that has to be opened first took half a second where it was measured.
// Four seconds covers both with room, and is short enough that a manager does
// not read the wait as a stall. teamSendLook is how often the traffic is read
// while waiting. Both are vars so a test does not sleep, read under
// teamWatchClockMu like the watch's own clocks ([teamSendWait]).
var (
	teamSendSettle = 4 * time.Second
	teamSendLook   = 200 * time.Millisecond
)

// teamSendWait is the two waits as this moment has them.
func teamSendWait() (settle, look time.Duration) {
	teamWatchClockMu.RLock()
	defer teamWatchClockMu.RUnlock()
	return teamSendSettle, teamSendLook
}

// How the traffic lines about a wake open. The writers (team_wakewatch.go,
// team_pause.go) and the send that reads them back use these, so neither can
// drift from the other.
const (
	teamWokePrefix     = "woke "
	teamCouldNotPrefix = "could not wake "
	teamHeldPrefix     = "held "
)

// teamNotUnderWayRule closes an answer in which some member did not start.
const teamNotUnderWayRule = "Do not tell the person this work is under way."

// teamSendOutcome is the sentences that follow "Sent a directive to …": what
// became of it for each member it was sent to on this computer. ready are the
// members a wake was asked for and paused the ones the person paused.
func (a *Agent) teamSendOutcome(ctx context.Context, profile string, team teams.Team, id string, ready, paused []teams.Member) string {
	var started, busy, asking, unseen []string
	refused := map[string]string{}
	var refusedOrder []string
	waiting := map[string]teams.Member{}
	now := time.Now()
	for _, member := range ready {
		if member.Handle == "" {
			continue
		}
		if state, _, ok := a.team.journals.stateOf(member.File, now); ok {
			switch state.State {
			case teams.StateRunning:
				busy = append(busy, "@"+member.Handle)
				continue
			case teams.StateAsking:
				asking = append(asking, "@"+member.Handle)
				continue
			}
		}
		waiting[member.Key] = member
	}
	settle, look := teamSendWait()
	deadline := now.Add(settle)
	for len(waiting) > 0 {
		entries, err := teams.ReadTraffic(profile, team.ID, id, teamPageLimit)
		if err == nil {
			for _, entry := range entries {
				member, ok := waiting[entry.Member]
				if !ok || entry.Kind != teams.KindEvent {
					continue
				}
				text := strings.TrimSpace(entry.Text)
				switch {
				case entry.State == teams.StateRunning && strings.HasPrefix(text, teamWokePrefix+"@"):
					started = append(started, "@"+member.Handle)
					delete(waiting, entry.Member)
					delete(refused, member.Handle)
				case entry.From == teams.FromSystem && (strings.HasPrefix(text, teamCouldNotPrefix) || strings.HasPrefix(text, teamHeldPrefix)):
					if _, said := refused[member.Handle]; !said {
						refusedOrder = append(refusedOrder, member.Handle)
					}
					_, why, _ := strings.Cut(text, ": ")
					refused[member.Handle] = strings.TrimSpace(why)
				}
			}
		}
		if len(waiting) == 0 || !time.Now().Before(deadline) {
			break
		}
		select {
		case <-ctx.Done():
			deadline = time.Time{}
		case <-time.After(look):
		}
	}
	for _, member := range ready {
		if _, still := waiting[member.Key]; still {
			if _, said := refused[member.Handle]; said {
				continue
			}
			// A MEMBER WHOSE TURN BEGAN DURING THE WAIT WROTE NO LINE. It was
			// idle when the send first looked and in a turn by the time its
			// watch reached the directive, and a turn already running is not
			// said to have been woken (team_wakedecide.go). Its journal is read
			// once more, so the answer is that it is in a turn, not that it has
			// shown no sign of starting.
			if state, _, ok := a.team.journals.stateOf(member.File, time.Now()); ok && state.State == teams.StateRunning {
				busy = append(busy, "@"+member.Handle)
				continue
			}
			unseen = append(unseen, "@"+member.Handle)
		}
	}
	var said []string
	if len(started) > 0 {
		said = append(said, strings.Join(started, ", ")+" started a turn on it.")
	}
	if len(busy) > 0 {
		said = append(said, strings.Join(busy, ", ")+" "+isAre(len(busy))+" in a turn and "+readsRead(len(busy))+" it at the next step.")
	}
	if len(asking) > 0 {
		said = append(said, strings.Join(asking, ", ")+" "+isAre(len(asking))+" waiting on the person and "+readsRead(len(asking))+" it once they answer.")
	}
	if len(started)+len(busy)+len(asking) > 0 {
		said = append(said, teamSendWakesSentence(team.Root))
	}
	for _, member := range paused {
		said = append(said, fmt.Sprintf("@%s did NOT start: %s. Tell the person it is paused; only they can resume it.", member.Handle, teamPausedWhy))
	}
	for _, handle := range refusedOrder {
		why, still := refused[handle]
		if !still {
			continue
		}
		if why == "" {
			why = "its wake was refused"
		}
		said = append(said, fmt.Sprintf("@%s did NOT start: %s.", handle, strings.TrimSuffix(why, ".")))
	}
	if len(unseen) > 0 {
		said = append(said, strings.Join(unseen, ", ")+" "+hasHave(len(unseen))+" shown no sign of starting yet; team_status says whether "+itThey(len(unseen))+" did.")
	}
	if len(paused)+len(refused)+len(unseen) > 0 {
		said = append(said, teamNotUnderWayRule)
	}
	return strings.Join(said, " ")
}

// splitPaused divides a directive's local targets into the members the person
// paused, in this team or another of theirs ([teams.File.PausedAnywhere]), and
// the rest. The manager is never paused.
func splitPaused(file *teams.File, team teams.Team, targets []teams.Member) (ready, paused []teams.Member) {
	for _, member := range targets {
		_, elsewhere := file.PausedAnywhere(member.Key)
		if (member.Paused || elsewhere) && member.Key != team.Manager {
			paused = append(paused, member)
			continue
		}
		ready = append(ready, member)
	}
	return ready, paused
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func readsRead(n int) string {
	if n == 1 {
		return "reads"
	}
	return "read"
}

func hasHave(n int) string {
	if n == 1 {
		return "has"
	}
	return "have"
}

func itThey(n int) string {
	if n == 1 {
		return "it"
	}
	return "they"
}

// teamSendWakesSentence is what a send's answer says comes back on its own.
// The global manager is not woken by a turn ending in the root, finished or
// stopped ([teamEventWakesManagerOf]), so its answer does not promise that,
// and says what closes a directive instead. Its sends reach top-level
// managers only ([managedView]), which is who the sentence names.
func teamSendWakesSentence(root bool) string {
	if root {
		return "Their replies, questions and failures wake you when they arrive, so there is no need to wait or poll. A top-level team's manager answers a directive with team_post: its turn finishing or being stopped does not wake you, and team_status says where it stands."
	}
	return "Replies and endings wake you when they arrive, so there is no need to wait or poll."
}
