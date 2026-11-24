package session

import (
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// ── A WAKE THAT COULD NOT BE DELIVERED IS KEPT, NEVER DROPPED ──────────────
//
// [Agent.teamRouse] opens a conversation nobody holds so the traffic written
// for it can wake it. It used to try once: a crew manager that had moved to
// another window, and whose window had since closed, was "could not wake ◆"
// on every member reply, and the crew stalled until a person opened the
// manager by hand. The lines themselves were never lost (they wait in the
// Traffic, and the conversation's own cursor hands them over when it is next
// opened), but the WAKE was, and a wake is the whole of what a reply asks for.
//
// SO A FAILED WAKE IS QUEUED HERE, per conversation, and tried again on a
// growing interval ([teamRouseRetry] doubling to [teamRouseRetryMax]) until one
// of three things is true:
//
//   - SOMETHING HOLDS THE CONVERSATION NOW. A window opened it, or a host did;
//     whatever holds it watches its own Traffic and wakes on the lines that
//     are waiting, so the queue lets go without opening anything.
//   - THE DOOR OPENED IT. The engine (or the app, which reopens its own moved
//     agents, cmd/codeaf's appteamresume.go) answered the retry.
//   - THERE IS NOTHING LEFT TO WAKE: the team closed, the member left it, or its
//     transcript is gone. Only then does the queue give up, and it says so.
//
// The failure is said in the Traffic ONCE, when the wake is first queued, and
// the line says that the wake is kept ([teamRouseKept]); a retry that fails
// again says nothing more. Each queued wake costs a stat of its journal per
// retry, and nothing while the queue is empty, because the queue's one
// goroutine runs only while something is in it.

// teamRouseRetry is the first wait before a queued wake is tried again; each
// failure doubles it, up to [teamRouseRetryMax]. A var so a test can ask in
// milliseconds what the product decides in seconds; nothing in the product
// writes it.
var teamRouseRetry = 15 * time.Second

// teamRouseRetryMax is the longest a queued wake waits between tries: five
// minutes is how stale a reply may get before a manager that has become
// reachable is woken on it. A var for the same reason as [teamRouseRetry];
// both are read under teamWatchClockMu.
var teamRouseRetryMax = 5 * time.Minute

func teamRouseLongestWait() time.Duration {
	teamWatchClockMu.RLock()
	defer teamWatchClockMu.RUnlock()
	return teamRouseRetryMax
}

// teamRouseKept is what the first failure's Traffic line adds, so the rail
// does not read a wake that is still owed as one that was given up on.
const teamRouseKept = "; codeaf keeps the wake and starts it as soon as it can be reached"

func teamRouseFirstWait() time.Duration {
	teamWatchClockMu.RLock()
	defer teamWatchClockMu.RUnlock()
	return teamRouseRetry
}

// queuedRouse is one conversation's owed wake.
type queuedRouse struct {
	profile string
	teamID  string
	member  teams.Member
	due     time.Time
	wait    time.Duration
	// said is the reason the Traffic was last told, so the same failure is
	// said once and a different one is said again.
	said string
}

// rouseQueue is the process's owed wakes, by transcript path.
var rouseQueue struct {
	mu      sync.Mutex
	pending map[string]*queuedRouse
	running bool
	// nudge wakes the goroutine's sleep when a wake is queued, so one due
	// sooner than the wake it is sleeping for is not left waiting behind it.
	nudge chan struct{}
}

// queueRouse keeps member's wake and reports whether its failure is to be
// said: a wake already owed for the same conversation is the same wake, and
// the same reason has been said already; a new reason is said again.
func queueRouse(profile, teamID string, member teams.Member, reason string) bool {
	file := strings.TrimSpace(member.File)
	if file == "" || profile == "" {
		return false
	}
	rouseQueue.mu.Lock()
	defer rouseQueue.mu.Unlock()
	if rouseQueue.pending == nil {
		rouseQueue.pending = map[string]*queuedRouse{}
	}
	if owed, ok := rouseQueue.pending[file]; ok {
		say := owed.said != reason
		owed.said = reason
		return say
	}
	wait := teamRouseFirstWait()
	rouseQueue.pending[file] = &queuedRouse{profile: profile, teamID: teamID, member: member, due: time.Now().Add(wait), wait: wait, said: reason}
	if rouseQueue.nudge == nil {
		rouseQueue.nudge = make(chan struct{}, 1)
	}
	select {
	case rouseQueue.nudge <- struct{}{}:
	default:
	}
	if !rouseQueue.running {
		rouseQueue.running = true
		guard.Go("team rouse queue", runRouseQueue)
	}
	return true
}

// forgetRouses empties the queue (tests: the queue is the process's, and a
// fixture's owed wake must not outlive its home).
func forgetRouses() {
	rouseQueue.mu.Lock()
	defer rouseQueue.mu.Unlock()
	rouseQueue.pending = nil
}

// rouseOwed reports whether a wake is queued for a transcript (tests).
func rouseOwed(file string) bool {
	rouseQueue.mu.Lock()
	defer rouseQueue.mu.Unlock()
	_, owed := rouseQueue.pending[strings.TrimSpace(file)]
	return owed
}

// runRouseQueue is the queue's one goroutine: it tries every wake that is due
// and ends when nothing is owed.
func runRouseQueue() {
	for {
		rouseQueue.mu.Lock()
		if len(rouseQueue.pending) == 0 {
			rouseQueue.running = false
			rouseQueue.mu.Unlock()
			return
		}
		now := time.Now()
		next := now.Add(teamRouseLongestWait())
		var due []string
		for file, owed := range rouseQueue.pending {
			if !owed.due.After(now) {
				due = append(due, file)
			} else if owed.due.Before(next) {
				next = owed.due
			}
		}
		nudge := rouseQueue.nudge
		rouseQueue.mu.Unlock()
		for _, file := range due {
			retryRouse(file)
		}
		if len(due) == 0 {
			// A wake queued meanwhile may be due sooner than the one slept
			// for, so queueing one ends the sleep and the next look is taken
			// from the queue as it now stands.
			timer := time.NewTimer(time.Until(next))
			select {
			case <-timer.C:
			case <-nudge:
				timer.Stop()
			}
		}
	}
}

// retryRouse tries one owed wake, and keeps it, with a longer wait, when it
// still cannot be delivered.
func retryRouse(file string) {
	rouseQueue.mu.Lock()
	owed := rouseQueue.pending[file]
	rouseQueue.mu.Unlock()
	if owed == nil {
		return
	}
	done, gone := rouseQueueAttempt(owed)
	rouseQueue.mu.Lock()
	defer rouseQueue.mu.Unlock()
	if done {
		delete(rouseQueue.pending, file)
		if gone != "" {
			who, to := rouseWho(owed)
			_ = teams.AppendTraffic(owed.profile, owed.teamID, teams.Entry{
				Kind: teams.KindEvent, From: teams.FromSystem, To: to, Member: owed.member.Key,
				State: teams.StateIdle, Text: "stopped trying to wake " + who + ": " + gone,
			})
		}
		return
	}
	owed.wait *= 2
	if longest := teamRouseLongestWait(); owed.wait > longest {
		owed.wait = longest
	}
	owed.due = time.Now().Add(owed.wait)
}

// rouseQueueAttempt is one try: done when the wake no longer needs this
// queue, with gone naming why when that is because nothing is left to wake.
func rouseQueueAttempt(owed *queuedRouse) (done bool, gone string) {
	file := strings.TrimSpace(owed.member.File)
	if _, err := os.Stat(owed.profile); err != nil {
		// The profile itself is gone (a throwaway home): there is no Traffic
		// left to say anything in.
		return true, ""
	}
	if _, err := os.Stat(file); err != nil {
		return true, "its transcript is no longer on this machine"
	}
	if f, err := teams.LoadTeams(owed.profile, owed.teamID); err == nil {
		team, found := teamIn(f, owed.teamID)
		if !found || team.Closed() {
			return true, "its crew is closed"
		}
		if !teamHasMember(team, owed.member.Key) {
			return true, "it is no longer in the crew"
		}
	}
	if journalHeld(file) {
		// Whatever holds it watches its own Traffic, and its cursor still
		// stands before the lines that asked for this wake.
		return true, ""
	}
	if teamResume.Load() == nil {
		return false, ""
	}
	return rouseMember(owed.member) == "", ""
}

func rouseWho(owed *queuedRouse) (string, string) {
	if f, err := teams.LoadTeams(owed.profile, owed.teamID); err == nil {
		if team, ok := teamIn(f, owed.teamID); ok && team.Manager == owed.member.Key {
			return "◆", teams.ToManager
		}
	}
	if owed.member.Handle == "" {
		return "a member with no handle", teams.ToRoom
	}
	return "@" + owed.member.Handle, owed.member.Handle
}

func teamIn(f *teams.File, id string) (teams.Team, bool) {
	if f == nil {
		return teams.Team{}, false
	}
	for _, team := range f.Teams {
		if team.ID == id {
			return team, true
		}
	}
	return teams.Team{}, false
}

func teamHasMember(team teams.Team, key string) bool {
	if team.Manager == key {
		return true
	}
	for _, member := range team.Members {
		if member.Key == key {
			return true
		}
	}
	return false
}
