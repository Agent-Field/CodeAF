package session

// WHAT A MEMBER TELLS ITS TEAM WITHOUT BEING ASKED: that it finished a turn,
// that a turn failed, and that it is waiting on the person.
//
// These are the three facts the manager and the Traffic rail most need and the
// three a member's words never say, because a member does not narrate its own
// ending. They are [teams.KindEvent] entries in the team's Traffic log, from the
// member's handle to the manager, with [teams.Entry.State] saying what the
// member is now, so a reader colours by a field and not by reading words.
//
// ONE APPEND PER CHANGE OF STATE, NEVER PER STEP. A turn's end is one entry,
// the first question a turn is held on is one entry however many a batch raises
// at once, and the end of that wait is one more. Nothing is written for a step,
// a token or a tool call, and nothing at all is written by a conversation that
// is not a member of a team with a manager: that is decided off an atomic the
// step boundary already keeps current ([Agent.teamRolesLocked]), so a
// conversation in no team pays no disk for any of this.
//
// THE WRITE IS OFF THE PATH. An append takes the log's lock and reads its tail
// for the next id, and neither may stand between a turn and its ending or
// between a question and the person seeing it. So an event is queued here and
// written by one goroutine per burst, in the order it was queued, and
// [Agent.SettleWrites] waits for it the way it waits for every other deferred
// write.
//
// THE PERSON'S WORDS ARE NEVER WRITTEN. An event says what the member is doing
// in the session's own words, and a question's text is the model's or the
// gate's; there is no [teams.KindYou] entry anywhere in this package.

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// teamEventText is how many characters of an event's text are written: a
// failure's first line or a question's head, not a report.
const teamEventText = 200

// teamStateLook is how much of a team's Traffic a status or a digest reads to
// learn who is asking: the last entries, of which the digest shows the newest
// [teamRecent]. [teams.ReadTraffic] reads the log whole either way, so looking
// further back costs no more disk than showing twenty.
const teamStateLook = teamFirstLook

// teamEventLane is one conversation's queue of events waiting to be written.
type teamEventLane struct {
	// member is whether this conversation is, as of the last time its roles
	// were read, a member (not the manager) of a team that has a manager. It is
	// the whole gate, read without a lock or a disk.
	member atomic.Bool

	mu sync.Mutex
	// asking is how many questions holding this conversation's turn are open.
	asking int
	queue  []teamEvent
	// done is the running writer's, closed when it has emptied the queue; nil
	// when no writer is running.
	done chan struct{}
}

// teamEvent is one event waiting to be written.
type teamEvent struct {
	state string
	text  string
	at    time.Time
}

// eventfulRoles reports whether roles make this conversation one whose events
// are written: a member, not the manager, of a team with a manager.
func eventfulRoles(roles []teamRole) bool {
	for _, role := range roles {
		if role.managed && !role.manager && role.handle != "" {
			return true
		}
	}
	return false
}

// teamEventOwed queues one event, and starts the writer when none is running.
func (a *Agent) teamEventOwed(state, text string) {
	if !a.team.events.member.Load() || a.config.teamProfile() == "" {
		return
	}
	lane := &a.team.events
	lane.mu.Lock()
	defer lane.mu.Unlock()
	lane.queue = append(lane.queue, teamEvent{state: state, text: cutRunesTeam(oneLineTeam(text), teamEventText), at: time.Now()})
	if lane.done == nil {
		done := make(chan struct{})
		lane.done = done
		go a.writeTeamEvents(done)
	}
}

// writeTeamEvents writes the queue in order until it is empty.
func (a *Agent) writeTeamEvents(done chan struct{}) {
	defer close(done)
	lane := &a.team.events
	for {
		lane.mu.Lock()
		if len(lane.queue) == 0 {
			lane.done = nil
			lane.mu.Unlock()
			return
		}
		next := lane.queue[0]
		lane.queue = lane.queue[1:]
		lane.mu.Unlock()
		a.writeTeamEvent(next)
	}
}

// writeTeamEvent appends one event to every team this conversation is a
// managed member of. The roles are read again here, off the path: a stat of
// the teams file, and a read only when it moved. A failed append is silence;
// the digest still reads the member's journal.
func (a *Agent) writeTeamEvent(event teamEvent) {
	profile := a.config.teamProfile()
	if profile == "" {
		return
	}
	for _, role := range a.teamRoles() {
		if !role.managed || role.manager || role.handle == "" {
			continue
		}
		// The event answers what the member was last told by its manager, so
		// the rail folds it into that thread's reply (internal/teams' thread.go).
		err := teams.AppendTraffic(profile, role.id, teams.Entry{
			At:      event.at,
			Kind:    teams.KindEvent,
			From:    role.handle,
			To:      teams.ToManager,
			Member:  role.key,
			Text:    event.text,
			State:   event.state,
			Answers: a.teamAnswering(role.id),
		})
		if err != nil || !role.wakes {
			continue
		}
		switch event.state {
		case teams.StateFinished, teams.StateFailed, teams.StateAsking:
			// These wake the manager, so a manager nobody has open is opened
			// (team_wakewatch.go).
			a.rouseManager(profile, role)
		}
	}
}

// rouseManager opens the manager of role's team if nothing holds it.
func (a *Agent) rouseManager(profile string, role teamRole) {
	a.team.mu.Lock()
	file := a.team.file
	a.team.mu.Unlock()
	if file == nil {
		return
	}
	team, ok := file.Team(role.id)
	if !ok {
		return
	}
	if manager, ok := team.Member(team.Manager); ok {
		a.teamRouse(profile, team, role.wakes, []teams.Member{manager}, "")
	}
}

// settleTeamEvents waits until every queued event is written.
func (a *Agent) settleTeamEvents() {
	lane := &a.team.events
	lane.mu.Lock()
	done := lane.done
	lane.mu.Unlock()
	if done != nil {
		<-done
	}
}

// ── the three moments ───────────────────────────────────────────────────────

// teamTurnEnded is a turn's ending, told once: failed when the turn ended on
// an error, stopped when somebody stopped it, finished otherwise. It is called
// by the turn's own cleanup (agent.go) after the questions the turn raised are
// retired, so a wait the turn ended inside is closed before the ending is said.
func (a *Agent) teamTurnEnded(ctx context.Context, hub *eventHub) {
	if !a.team.events.member.Load() {
		return
	}
	a.dropOwedResume()
	if err := hub.turnError(); err != nil {
		a.teamEventOwed(teams.StateFailed, "failed: "+firstLineTeam(err.Error()))
		return
	}
	if ctx.Err() != nil {
		a.teamEventOwed(teams.StateIdle, "stopped")
		return
	}
	a.teamEventOwed(teams.StateFinished, "finished")
}

// dropOwedResume takes back a "no longer waiting" that has not been written
// yet, because the ending about to be queued says more.
func (a *Agent) dropOwedResume() {
	lane := &a.team.events
	lane.mu.Lock()
	defer lane.mu.Unlock()
	kept := lane.queue[:0]
	for _, event := range lane.queue {
		if event.state != teams.StateRunning {
			kept = append(kept, event)
		}
	}
	lane.queue = kept
}

// teamAsking is a question raised, and the function that says it came down.
//
// ONLY A QUESTION THE TURN IS STOPPED ON COUNTS, because that is what "waiting
// on the person" means: a permission prompt, an `ask`, a proposal the turn
// waits for. The first one up is an event and the last one down is another;
// the ones between are the same state and write nothing.
func (a *Agent) teamAsking(q Question) func() {
	if !q.Blocking.Turn || !a.team.events.member.Load() {
		return func() {}
	}
	lane := &a.team.events
	lane.mu.Lock()
	lane.asking++
	first := lane.asking == 1
	lane.mu.Unlock()
	if first {
		a.teamEventOwed(teams.StateAsking, askingWords(q))
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			lane.mu.Lock()
			lane.asking--
			last := lane.asking == 0
			lane.mu.Unlock()
			if last {
				a.teamEventOwed(teams.StateRunning, "no longer waiting")
			}
		})
	}
}

// askingWords is what a waiting event says: the gate's own line for a
// permission prompt ("needs your ok to run bash"), and the question for
// anything else.
func askingWords(q Question) string {
	head := strings.TrimSpace(q.Head)
	if q.Ask == AskPermission {
		return head
	}
	return "asks: " + head
}

// turnError is the error a turn ended on, nil for a turn that did not end on
// one. It reads the backlog the hub keeps for the turn, once, at the turn's
// end: an [EventError] is only ever sent as a turn's last word.
func (h *eventHub) turnError() error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	for index := len(h.backlog) - 1; index >= 0; index-- {
		if event := h.backlog[index]; event.Kind == EventError {
			if event.Err == nil {
				return fmt.Errorf("the turn ended on an error")
			}
			return event.Err
		}
	}
	return nil
}

// ── reading them back ───────────────────────────────────────────────────────

// askingStaleBound is how long an asking event may keep a member asking. A
// permission prompt nobody has answered in half an hour is not still waiting
// in the digest, and a process that died on one leaves the event as its last
// word with the journal unmoved.
const askingStaleBound = 30 * time.Minute

// askingFromEvents is a member's state with its newest event weighed in.
//
// A PERMISSION PROMPT IS NOT IN THE JOURNAL: the call it is about was written
// before the prompt was raised and its result is written after it is answered,
// so a member held on one reads as running off its journal alone. Its own
// asking event says otherwise, and it holds while the journal has not moved
// since: a journal line written after the event (the call's result, a turn's
// pace line) is the member having moved on, whatever the log says after.
//
// IT GOES STALE TWO WAYS, and then the member reads idle. Nothing holding the
// transcript lock means the process that raised the prompt is gone (the same
// probe a wake uses, [journalHeld]). An event older than [askingStaleBound]
// is over even while a process still holds the lock.
func askingFromEvents(state teams.MemberState, last time.Time, member teams.Member, log []teams.Entry, now time.Time) teams.MemberState {
	for index := len(log) - 1; index >= 0; index-- {
		entry := log[index]
		if entry.Kind != teams.KindEvent || !eventConcerns(entry, member) {
			continue
		}
		if entry.State != teams.StateAsking || entry.At.Before(last) {
			return state
		}
		if askingStale(member, entry, now) {
			state.State = teams.StateIdle
			state.Question = ""
			return state
		}
		state.State = teams.StateAsking
		if state.Question == "" {
			state.Question = strings.TrimPrefix(entry.Text, "asks: ")
		}
		return state
	}
	return state
}

// askingStale reports whether an asking event no longer means the member is
// waiting on the person.
func askingStale(member teams.Member, entry teams.Entry, now time.Time) bool {
	if !entry.At.IsZero() && now.Sub(entry.At) >= askingStaleBound {
		return true
	}
	return !journalHeld(member.File)
}

// eventConcerns reports whether an event is about member.
func eventConcerns(entry teams.Entry, member teams.Member) bool {
	if entry.Member != "" {
		return entry.Member == member.Key
	}
	return member.Handle != "" && entry.From == member.Handle
}

// recentOf is the newest [teamRecent] entries of log.
func recentOf(log []teams.Entry) []teams.Entry {
	if len(log) > teamRecent {
		return log[len(log)-teamRecent:]
	}
	return log
}

// ── the brief ───────────────────────────────────────────────────────────────

// teamBriefText is how many characters of a brief are delivered. A brief is
// the whole assignment, so it is allowed more than a message.
const teamBriefText = 12000

// teamBriefLine is a [teams.KindStart] entry as the member it started is told
// it: the manager's brief, marked as the manager's, on the new conversation's
// first request. Only the member the start names is told, and only a start the
// manager wrote; every other reader is told nothing, because the interface
// performs a start and the manager already knows what it asked for.
func teamBriefLine(role teamRole, entry teams.Entry) string {
	if role.manager || role.handle == "" || entry.From != teams.FromManager || entry.To != role.handle {
		return ""
	}
	text := strings.TrimSpace(entry.Text)
	if text == "" {
		return ""
	}
	return teamBriefWord + teamNumber(entry) + ": " + indentAfterFirst(cutRunesTeam(text, teamBriefText))
}

// teamBriefWord opens the brief's line, beside "◆ from manager" and
// "◆ directive from manager".
const teamBriefWord = "◆ brief from manager"

// teamCursorBefore is the cursor that reads id itself next: the id one below
// it, zero-padded the way [teams.AppendTraffic] pads.
func teamCursorBefore(id string) string {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil || n <= 1 {
		return teamLogStart
	}
	return fmt.Sprintf("%0*d", len(teamLogStart), n-1)
}

// firstLineTeam is text's first non-empty line.
func firstLineTeam(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return ""
}

// oneLineTeam is text with its whitespace runs made single spaces.
func oneLineTeam(text string) string { return strings.Join(strings.Fields(text), " ") }
