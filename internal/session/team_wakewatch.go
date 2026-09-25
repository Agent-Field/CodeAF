package session

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/teams"
)

// ── TEAM TRAFFIC WAKES THE CONVERSATION IT IS FOR ───────────────────────────
//
// v1 delivered a team's lines only when a conversation next ran, and a manager
// is idle as often as its members are: it sent a directive to three idle
// members, told the person the answers would be reported as they arrived, and
// nothing ran again until the person typed. So the lines that ask for an
// answer now start one:
//
//   - A DIRECTIVE WAKES THE MEMBER IT IS ADDRESSED TO (its handle, or
//     everyone). A note does not: it is information, and it is read at the
//     member's next turn as before. A member that is busy is not started again;
//     it reads the directive at its next step boundary, the steering road
//     ([Agent.teamBoundary]).
//   - A MEMBER'S REPLY WAKES THE MANAGER: a team_post addressed to the manager,
//     and the events a member writes on its own (finished, failed, asking,
//     teamevent.go). A finished event after a reply in the same member turn
//     starts no second wake. Other arrivals COALESCE: the first arms [teamWakeSettle], and one
//     turn carries everything that arrived by the time it runs out, because
//     three members finishing within a few seconds of each other is one thing
//     to act on, not three turns that each see a third of it.
//
// THE WOKEN TURN IS NEVER THE PERSON'S. What wakes it is the same marked note
// the step boundary hands over ("◆ directive from manager: …", "from @web: …"),
// queued as the session's line with the wake bit ([Agent.wakeLocked]), so it
// is drawn in the harness's lane and every gate a wake passes (the spend rail,
// the wall, a closed or stopped session) still decides whether it may run.
//
// THE WATCH LIVES WITH THE CONVERSATION, ON THE ENGINE. Every conversation is
// an [Agent] in the process that holds its journal (the workspace's session
// host, locally and over --host alike), and the Traffic log is the one channel
// between conversations (team.go), so each conversation watches the log for
// itself. It costs, per tick of [teamWatchEvery]:
//
//   - while a turn runs, one stat of each managed member team's log, and a
//     read only when it moves, so a manager's stop can reach a headless member;
//     nothing once the session is closed;
//   - one stat of the teams file SHARED BY EVERY CONVERSATION IN THE PROCESS
//     ([sharedTeamsStamp]) while it is idle, which is how a conversation made a
//     member while it sat idle is noticed;
//   - one stat of each team's log only while it is idle AND in a managed team
//     with wake on, and a page read only when that stat moved.
//
// A CONVERSATION NOBODY HAS OPEN IS OPENED. A directive to a member no window
// and no host holds, and a reply to a manager in the same state, would wake
// nobody, so the side that wrote the line asks the engine to open that
// conversation headless ([SetTeamResume]); it attaches no surface, and a window
// that opens it later joins the running one. Where there is no such road, the
// Traffic says so (`could not wake @x: …`) rather than the writer pretending.
//
// AND IT IS BOUNDED TWICE. A conversation is woken at most [teamWakesPerHour]
// times in an hour, and a manager woken [teamLoopRounds] times by its team with
// no word from the person stops being woken and asks the person instead: a
// manager and its members answering each other forever is spend nobody asked
// for. Both are said in the Traffic, and each wake is too (`◆ woke @web`,
// `@web woke ◆`), so the rail shows why a conversation is running.

// teamWatchEvery is how often an idle conversation looks at its team's
// Traffic. A var so a test can ask in milliseconds what the product decides in
// seconds; nothing in the product writes it.
var teamWatchEvery = time.Second

// teamWakeSettle is how long a manager's wake waits after the first reply
// that asks for it, gathering whatever else arrives. Five seconds: long enough
// that members finishing on one burst of work (a fan-out's replies land within
// a second or two of each other) are one turn, short enough that a person
// watching the manager does not read the pause as the promise being broken. It
// is measured from the FIRST reply and never extended, so a steady trickle
// cannot hold the manager asleep. A var for the reason [teamWatchEvery] is.
var teamWakeSettle = 5 * time.Second

// teamWakesPerHour is the most times team traffic starts one conversation's
// turn in an hour. Twenty is a directive every three minutes for an hour,
// which is a busy team and not yet a runaway one; past it the lines still
// arrive, at the conversation's next turn.
const teamWakesPerHour = 20

// teamLoopRounds is how many times a manager is woken by its team with no word
// from the person before it stops being woken and asks the person. Ten rounds
// of hand out, hear back, hand out again is a real piece of delegated work; a
// manager and members still answering each other after that are more likely
// talking in a circle than finishing, and the person is the one who can tell.
const teamLoopRounds = 10

// teamResumeWait is how long opening an absent conversation may take before it
// is reported as a failure: a session host has to start and load it.
const teamResumeWait = 60 * time.Second

// teamWatch is one conversation's side of the wake: how far into each team's
// Traffic it has looked, the batch a manager is gathering, and the two bounds.
// It is the watch goroutine's own, under [teamSeat.mu] where it shares a read
// with the boundary.
type teamWatch struct {
	cursors   map[string]string
	trafficAt map[string]fileStamp
	// stopCursors and stopAt are how far a RUNNING turn has looked for its
	// manager's stop in each team's log ([Agent.teamStopTick]), and stopTurn
	// is the turn they were taken for: a new turn starts again from the tail,
	// because a stop written before it began is not addressed to it.
	stopCursors map[string]string
	stopAt      map[string]fileStamp
	stopTurn    uint64
	// pending is what has arrived for a manager since the first line that asks
	// for a wake, per team, and due is when the batch is handed over.
	pending map[string][]teams.Entry
	due     time.Time
	// woken is when this conversation was woken in the last hour, and limitSaid
	// when the Traffic was last told the limit was reached.
	woken     []time.Time
	limitSaid time.Time
	// rounds is how many team wakes this conversation (as a manager) has had
	// since the person last spoke, heard the person's count at, and held says
	// the loop breaker has tripped and is waiting for the person.
	rounds int
	heard  int64
	held   bool
	// capSaid is the last cap hold the Traffic was told of.
	capSaid string
}

// personTurns counts turns the person started, for the loop breaker. It is
// bumped where every turn begins ([Agent.startTurnLocked]) and read by the
// watch, so it is atomic rather than under either lock.
type personTurns struct{ n atomic.Int64 }

// notePersonTurn counts a turn that opens on the person's own words.
func (a *Agent) notePersonTurn(user userMessage) {
	if user.wake || user.authored || user.empty() {
		return
	}
	a.team.person.n.Add(1)
}

// watchTeamTraffic starts the watch, or does nothing for a conversation that
// is never in a team ([Config.teamProfile]).
func (a *Agent) watchTeamTraffic(profile string) {
	if profile == "" {
		return
	}
	// A wrap-up that was in progress when this process last stopped is armed
	// before the loop, so one already past its bound closes on the start
	// rather than waiting out a tick (team_wrapup.go).
	a.teamWrapUpResume(profile, time.Now())
	guard.Go("team traffic wake", func() { a.teamWatchLoop(profile) })
}

func (a *Agent) teamWatchLoop(profile string) {
	ticker := time.NewTicker(teamWatchEvery)
	defer ticker.Stop()
	for range ticker.C {
		if !a.teamWatchTick(profile, time.Now()) {
			return
		}
	}
}

// teamWatchTick is one look, and false once the session has closed.
func (a *Agent) teamWatchTick(profile string, now time.Time) bool {
	a.mu.Lock()
	closed, running, turnAt, turnSerial := a.closed, a.running, a.teamTurnAt, a.teamTurnSerial
	a.mu.Unlock()
	if closed {
		return false
	}
	// THE WRAP-UP'S CLOCK is looked at running or idle (team_wrapup.go), and
	// costs nothing while no wrap-up is in progress.
	a.teamWrapUpDue(profile, now)
	if running {
		a.teamStopTick(profile, turnAt, turnSerial)
		// A RUNNING TURN READS ITS OWN TRAFFIC at every step boundary. A batch a
		// manager was gathering goes with it: its posts land at the next
		// boundary, and its events are in the digest that turn carries.
		a.team.mu.Lock()
		a.team.watch.pending, a.team.watch.due = nil, time.Time{}
		a.team.mu.Unlock()
		return true
	}
	a.team.mu.Lock()
	roles := a.team.roles
	if sharedTeamsStamp(profile) != a.team.teamsAt {
		roles = a.teamRolesLocked(profile)
	}
	var wakeMembers []teamRole
	for _, role := range roles {
		if !role.managed || !role.wakes || (!role.manager && role.handle == "") {
			continue
		}
		arrived := a.teamWatchReadLocked(profile, role)
		var waking []teams.Entry
		for _, entry := range arrived {
			if role.manager && entry.Kind == teams.KindEvent && entry.State == teams.StateFinished &&
				teamFinishedAfterReply(profile, role.id, entry) {
				continue
			}
			if teamWakes(role, entry) || teamPacketWakes(profile, role, entry) {
				waking = append(waking, entry)
			}
		}
		if len(waking) == 0 {
			continue
		}
		if role.manager {
			if a.team.watch.pending == nil {
				a.team.watch.pending = map[string][]teams.Entry{}
			}
			if a.team.watch.due.IsZero() {
				a.team.watch.due = now.Add(teamWakeSettle)
			}
			a.team.watch.pending[role.id] = append(a.team.watch.pending[role.id], waking...)
			continue
		}
		wakeMembers = append(wakeMembers, role)
	}
	var batch map[string][]teams.Entry
	if w := &a.team.watch; len(w.pending) > 0 && !now.Before(w.due) {
		batch, w.pending, w.due = w.pending, nil, time.Time{}
	}
	a.team.mu.Unlock()
	if len(wakeMembers) > 0 {
		a.teamWakeMember(profile, wakeMembers, now)
	}
	if len(batch) > 0 {
		a.teamWakeManager(profile, roles, batch, now)
	}
	return true
}

// teamStopTick is a running member performing its manager's stop itself.
//
// A STOP USED TO BE A WINDOW'S TO PERFORM, and only a window's: team_stop
// writes a [teams.KindStop] line and the interface holding the member ends its
// turn the way the person's Stop does (tui3's teamtraffic.go). A member codeaf
// opened in the background to run a woken turn ([rouseMember]) has no window,
// so its turn ran to its end whatever the manager said. The member's own
// session reads the same line instead, which works wherever it runs.
//
// IT COSTS one stat of each managed team's log per tick while a turn runs, and
// a read only when that log moved. Only a membership that takes this team's
// orders is looked at: not the manager's own, and not a shared one, whose
// manager here is a link and whose stops do not reach it (team_stop refuses
// them). A stop written before the running turn began is history, and a stop
// is performed only while the turn it was read in is still the one running.
func (a *Agent) teamStopTick(profile string, turnAt time.Time, serial uint64) {
	a.team.mu.Lock()
	roles := a.team.roles
	if sharedTeamsStamp(profile) != a.team.teamsAt {
		roles = a.teamRolesLocked(profile)
	}
	w := &a.team.watch
	if w.stopCursors == nil || w.stopTurn != serial {
		w.stopCursors, w.stopAt, w.stopTurn = map[string]string{}, map[string]fileStamp{}, serial
	}
	stop := false
	for _, role := range roles {
		if !role.managed || role.manager || role.shared || role.handle == "" {
			continue
		}
		stamp := stampOf(teams.TrafficPath(profile, role.id))
		if stamp == w.stopAt[role.id] {
			continue
		}
		entries, err := teams.ReadTraffic(profile, role.id, w.stopCursors[role.id], teamPageLimit)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			w.stopCursors[role.id] = entry.ID
			if entry.Kind == teams.KindStop && entry.From == teams.FromManager &&
				(entry.Member == role.key || entry.Member == "" && entry.To == role.handle) &&
				!entry.At.Before(turnAt) {
				stop = true
			}
		}
		if len(entries) < teamPageLimit {
			w.stopAt[role.id] = stamp
		} else {
			w.stopAt[role.id] = fileStamp{}
		}
	}
	a.team.mu.Unlock()
	if stop {
		a.interruptTeamTurn(serial)
	}
}

// interruptTeamTurn is [Agent.Interrupt] with the manager's door, for the turn
// the stop was read in and no other.
//
// THE TURN IS ASKED FOR TWICE. A window holding the member may perform the same
// stop, and the turn it ends may be followed at once by another (a directive
// arriving behind the stop); a stop performed on that one would end work the
// manager never saw. So the serial is checked before anything is done, and
// again under the lock the cancel is taken under, and a turn that has ended
// is left alone, which is also what keeps a queued follow-up from being dropped
// by a stop that arrived after its turn was over.
func (a *Agent) interruptTeamTurn(serial uint64) {
	live := func() bool { return a.running && a.teamTurnSerial == serial && a.cancel != nil }
	a.mu.Lock()
	ok := live()
	a.mu.Unlock()
	if !ok {
		return
	}
	a.interruptDiscussions()
	a.interrupt.begin()
	a.mu.Lock()
	if !live() {
		a.mu.Unlock()
		return
	}
	cancel := a.cancel
	a.dropFollowUpsLocked()
	a.stopSteerGraceLocked()
	a.mu.Unlock()
	cancel(stopFor(StopByManager))
}

// teamFinishedAfterReply reports whether a member's finished event ends a turn
// that already replied to the manager: scanning back from it through the same
// member's lines, a note to the manager comes before that member's previous
// turn ending (finished, failed, or stopped).
//
// ONE REPLY IS ONE WAKE. The reply woke the manager, or reached a turn it was
// already running; the ending of the same member turn arrives seconds later,
// often after the settle has handed the reply over, and waking again for it
// paid a second manager turn for news the first one already acted on, and
// tripped the loop breaker after half its rounds. It is read from the log and
// not remembered, because the manager may have restarted in between; and only
// when a finished event would otherwise wake or be handed over, which is rare.
// A failed or asking event still wakes: that is news the reply did not carry.
func teamFinishedAfterReply(profile, teamID string, finished teams.Entry) bool {
	tail, err := teams.ReadTraffic(profile, teamID, "", teamFirstLook)
	if err != nil {
		return false
	}
	for i := len(tail) - 1; i >= 0; i-- {
		e := tail[i]
		if e.ID >= finished.ID || e.From != finished.From || (finished.Member != "" && e.Member != "" && e.Member != finished.Member) {
			continue
		}
		if e.Kind == teams.KindEvent && (e.State == teams.StateFinished || e.State == teams.StateFailed || e.State == teams.StateIdle) {
			return false
		}
		if e.Kind == teams.KindNote && e.To == teams.ToManager && strings.TrimSpace(e.Text) != "" {
			return true
		}
	}
	return false
}

// teamWatchReadLocked is what has been written to one team's log since this
// conversation last looked, or nothing when the log has not moved. The caller
// holds a.team.mu.
//
// THE LOOK NEVER STARTS BEHIND THE DELIVERY. What a turn's boundary already
// handed over is not a reason to start another turn, so the watch cursor is
// moved up to the delivery cursor whenever that is further on; and a
// conversation with neither starts where its delivery would ([Agent.firstTeamCursor]).
func (a *Agent) teamWatchReadLocked(profile string, role teamRole) []teams.Entry {
	w := &a.team.watch
	if w.cursors == nil {
		w.cursors, w.trafficAt = map[string]string{}, map[string]fileStamp{}
	}
	a.readTeamCursorsLocked()
	cursor, known := w.cursors[role.id]
	if delivered, ok := a.team.cursors[role.id]; ok && (!known || delivered > cursor) {
		cursor, known = delivered, true
	}
	if !known {
		cursor = a.firstTeamCursor(profile, role)
	}
	stamp := stampOf(teams.TrafficPath(profile, role.id))
	if known && stamp == w.trafficAt[role.id] && cursor == w.cursors[role.id] {
		return nil
	}
	entries, err := teams.ReadTraffic(profile, role.id, cursor, teamPageLimit)
	if err != nil {
		w.cursors[role.id] = cursor
		return nil
	}
	for _, entry := range entries {
		cursor = entry.ID
	}
	w.cursors[role.id] = cursor
	if len(entries) < teamPageLimit {
		w.trafficAt[role.id] = stamp
	} else {
		w.trafficAt[role.id] = fileStamp{}
	}
	return entries
}

// teamWakes reports whether an entry starts this conversation's turn when it
// is idle: a directive from the manager to its handle or to everyone, for a
// member; a member's post to the manager, or a member's finished, failed or
// asking event, for the manager. The watch suppresses a finished event when
// the same turn already posted a reply.
func teamWakes(role teamRole, entry teams.Entry) bool {
	if role.manager && teams.IsWrapUp(entry) {
		return true
	}
	// A CONFLICT'S RULING wakes the party it names, member or manager, whoever
	// wrote it (team_nest.go).
	if teams.IsRuling(entry) {
		return rulingFor(role, entry)
	}
	if role.manager {
		switch entry.From {
		case "", teams.FromManager, teams.FromYou, teams.FromSystem:
			return false
		}
		switch entry.Kind {
		case teams.KindNote:
			return entry.To == teams.ToManager && strings.TrimSpace(entry.Text) != ""
		case teams.KindEvent:
			switch entry.State {
			case teams.StateFinished, teams.StateFailed, teams.StateAsking:
				return true
			}
		}
		return false
	}
	if entry.Kind != teams.KindDirective || entry.From != teams.FromManager || strings.TrimSpace(entry.Text) == "" || role.shared {
		return false
	}
	return entry.Addressed(role.handle)
}

// ── the two wakes ───────────────────────────────────────────────────────────

// teamWakeMember starts a member's turn on the directives it was sent.
func (a *Agent) teamWakeMember(profile string, roles []teamRole, now time.Time) {
	if reason := a.teamWakeLimited(now); reason != "" {
		a.teamWakeLimitSay(profile, roles, reason, now)
		return
	}
	if reason := a.teamCapHold(profile, roles); reason != "" {
		a.teamCapSay(profile, roles, reason)
		return
	}
	news := a.teamBoundary()
	if news == "" {
		// A boundary that ran in the moment between the look and here has
		// handed the directive over already; there is nothing left to wake on.
		return
	}
	text := teamWakeMemberLead + "\n\n" + news
	woke, reason := a.teamWakeWith(text)
	if woke {
		a.teamWakeCount(now)
	}
	for _, role := range roles {
		// The wake answers the directive that caused it, the one the
		// boundary above just handed over, so a reader draws the member as
		// working on that thread rather than as a line of its own.
		answers := a.teamAnswering(role.id)
		if woke {
			a.teamSay(profile, role.id, teams.Entry{
				Kind: teams.KindEvent, From: teams.FromManager, To: role.handle, Member: role.key,
				State: teams.StateRunning, Text: "woke @" + role.handle, Answers: answers,
			})
			continue
		}
		if reason != "" {
			a.teamSay(profile, role.id, teams.Entry{
				Kind: teams.KindEvent, From: teams.FromSystem, To: role.handle, Member: role.key,
				State: teams.StateIdle, Text: "could not wake @" + role.handle + ": " + reason, Answers: answers,
			})
		}
	}
}

// teamWakeManager starts a manager's turn on the batch its members' replies
// made, or, past the loop bound, asks the person instead.
func (a *Agent) teamWakeManager(profile string, roles []teamRole, batch map[string][]teams.Entry, now time.Time) {
	byID := map[string]teamRole{}
	for _, role := range roles {
		byID[role.id] = role
	}
	if a.teamLoopHeld(profile, byID, batch) {
		return
	}
	if reason := a.teamWakeLimited(now); reason != "" {
		var said []teamRole
		for id := range batch {
			said = append(said, byID[id])
		}
		a.teamWakeLimitSay(profile, said, reason, now)
		return
	}
	var batchRoles []teamRole
	for id := range batch {
		batchRoles = append(batchRoles, byID[id])
	}
	if reason := a.teamCapHold(profile, batchRoles); reason != "" {
		a.teamCapSay(profile, batchRoles, reason)
		return
	}
	news := a.teamBoundary()
	var groups []string
	for id, entries := range batch {
		role := byID[id]
		if lines := teamEventLines(entries); len(lines) > 0 {
			groups = append(groups, fmt.Sprintf("What your members in %q did:\n%s", role.name, strings.Join(lines, "\n")))
		}
	}
	if news != "" {
		groups = append(groups, news)
	}
	if len(groups) == 0 {
		return
	}
	text := teamWakeManagerLead + "\n\n" +
		strings.Join(groups, "\n\n")
	woke, reason := a.teamWakeWith(text)
	if woke {
		a.teamWakeCount(now)
		a.team.mu.Lock()
		a.team.watch.rounds++
		a.team.mu.Unlock()
	}
	for id, entries := range batch {
		role := byID[id]
		if woke {
			a.teamSay(profile, id, teams.Entry{
				Kind: teams.KindEvent, From: teamWakers(entries)[0], To: teams.ToManager, Member: role.key,
				State: teams.StateRunning, Text: teamWokeManagerText(entries),
			})
			continue
		}
		if reason != "" {
			a.teamSay(profile, id, teams.Entry{
				Kind: teams.KindEvent, From: teams.FromSystem, To: teams.ToManager, Member: role.key,
				State: teams.StateIdle, Text: "could not wake ◆: " + reason,
			})
		}
	}
}

// teamWakeWith queues text as the session's note and starts a turn on it,
// under one hold of the agent's lock so the answer is the wake's own. It
// reports whether a turn was started (or one was already running, which takes
// the note at its next boundary), and why not when neither.
func (a *Agent) teamWakeWith(text string) (bool, string) {
	note := userMessage{message: textMessage("user", strings.TrimSpace(text)), wake: true, authored: true}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return false, "its conversation has closed"
	}
	a.steering = append(a.steering, note)
	if a.running {
		return true, ""
	}
	if a.wakeLocked() {
		return true, ""
	}
	switch {
	case a.workStopped:
		return false, "its work was stopped; it reads this at its next turn"
	case wallIsUp(a.steward()):
		return false, "its time limit has passed; it reads this at its next turn"
	}
	if err := a.railBlockLocked(); err != nil {
		return false, "its spending limit is reached; it reads this at its next turn"
	}
	return false, "it cannot start a turn on its own here; it reads this at its next turn"
}

// ── the bounds ──────────────────────────────────────────────────────────────

// teamWakeLimited is why this conversation may not be woken again this hour,
// "" when it may.
func (a *Agent) teamWakeLimited(now time.Time) string {
	a.team.mu.Lock()
	defer a.team.mu.Unlock()
	w := &a.team.watch
	kept := w.woken[:0]
	for _, at := range w.woken {
		if now.Sub(at) < time.Hour {
			kept = append(kept, at)
		}
	}
	w.woken = kept
	if len(kept) < teamWakesPerHour {
		return ""
	}
	return fmt.Sprintf("woken %d times in the last hour, the most team traffic may; it reads the rest at its next turn", teamWakesPerHour)
}

// teamWakeCount records one wake against the hour.
func (a *Agent) teamWakeCount(now time.Time) {
	a.team.mu.Lock()
	defer a.team.mu.Unlock()
	a.team.watch.woken = append(a.team.watch.woken, now)
}

// teamCapSay tells the Traffic a wake was held at the cap, once per
// conversation and reason: every held wake says the same thing, and the cap
// packet is what asks the person.
func (a *Agent) teamCapSay(profile string, roles []teamRole, reason string) {
	a.team.mu.Lock()
	say := a.team.watch.capSaid != reason
	a.team.watch.capSaid = reason
	a.team.mu.Unlock()
	if !say {
		return
	}
	for _, role := range roles {
		who, to := "◆", teams.ToManager
		if !role.manager {
			who, to = "@"+role.handle, role.handle
		}
		a.teamSay(profile, role.id, teams.Entry{
			Kind: teams.KindEvent, From: teams.FromSystem, To: to, Member: role.key,
			State: teams.StateIdle, Text: "held " + who + ": " + reason,
		})
	}
}

// teamWakeLimitSay tells the Traffic the limit was reached, once an hour.
func (a *Agent) teamWakeLimitSay(profile string, roles []teamRole, reason string, now time.Time) {
	a.team.mu.Lock()
	say := a.team.watch.limitSaid.IsZero() || now.Sub(a.team.watch.limitSaid) >= time.Hour
	if say {
		a.team.watch.limitSaid = now
	}
	a.team.mu.Unlock()
	if !say {
		return
	}
	for _, role := range roles {
		who, to := "◆", teams.ToManager
		if !role.manager {
			who, to = "@"+role.handle, role.handle
		}
		a.teamSay(profile, role.id, teams.Entry{
			Kind: teams.KindEvent, From: teams.FromSystem, To: to, Member: role.key,
			State: teams.StateIdle, Text: "could not wake " + who + ": " + reason,
		})
	}
}

// teamLoopHeld is the loop breaker, and true when the manager is not to be
// woken. A turn the person started since the last team wake resets it.
//
// ON THE TRIP IT ASKS THE PERSON: an asking event from the manager in each
// team's Traffic, which the rail draws in the needs-you colour and a digest
// reads as the manager waiting, and a note queued (not waking) in the
// manager's own conversation, so its next turn knows why nothing woke it.
func (a *Agent) teamLoopHeld(profile string, byID map[string]teamRole, batch map[string][]teams.Entry) bool {
	heard := a.team.person.n.Load()
	a.team.mu.Lock()
	w := &a.team.watch
	if heard != w.heard {
		w.heard, w.rounds, w.held = heard, 0, false
	}
	if w.rounds < teamLoopRounds {
		a.team.mu.Unlock()
		return false
	}
	trip := !w.held
	w.held = true
	a.team.mu.Unlock()
	if !trip {
		return true
	}
	said := fmt.Sprintf("asks: the team has woken me %d times with no word from you, so I have stopped being woken by it until you say something", teamLoopRounds)
	for id := range batch {
		role := byID[id]
		a.teamSay(profile, id, teams.Entry{
			Kind: teams.KindEvent, From: teams.FromManager, To: teams.ToRoom, Member: role.key,
			State: teams.StateAsking, Text: said,
		})
	}
	a.enqueueNote(userMessage{message: textMessage("user", fmt.Sprintf(
		"codeaf stopped waking you on your team's replies: they woke you %d times with no word from the person. "+
			"Their lines are still delivered at your next turn. Tell the person where the work stands and ask whether to go on.", teamLoopRounds))})
	return true
}

// ── what is said ────────────────────────────────────────────────────────────

// teamSay appends one entry and forgets a failure: a wake that could not be
// logged still happened, and the conversation's own turn is the record.
func (a *Agent) teamSay(profile, teamID string, entry teams.Entry) {
	_ = teams.AppendTraffic(profile, teamID, entry)
}

// teamEventLines are a manager's batch's events, one line each.
func teamEventLines(entries []teams.Entry) []string {
	var lines []string
	for _, entry := range entries {
		if entry.Kind != teams.KindEvent {
			continue
		}
		who := "@" + strings.TrimPrefix(entry.From, "@")
		text := strings.TrimSpace(entry.Text)
		switch entry.State {
		case teams.StateFinished:
			lines = append(lines, who+" finished its turn")
		case teams.StateFailed:
			if text == "" {
				text = "failed"
			}
			lines = append(lines, who+" "+text)
		case teams.StateAsking:
			lines = append(lines, who+" is waiting on the person: "+strings.TrimPrefix(text, "asks: "))
		}
	}
	return lines
}

// teamWakers is who in a batch woke the manager, in the order they wrote,
// each once.
func teamWakers(entries []teams.Entry) []string {
	var out []string
	seen := map[string]bool{}
	for _, entry := range entries {
		if !seen[entry.From] {
			seen[entry.From] = true
			out = append(out, entry.From)
		}
	}
	if len(out) == 0 {
		out = append(out, teams.FromSystem)
	}
	return out
}

// teamWokeManagerText is the wake's line on the rail, under the first waker's
// name: "woke ◆", and the others who replied in the same batch.
func teamWokeManagerText(entries []teams.Entry) string {
	wakers := teamWakers(entries)
	if len(wakers) == 1 {
		return "woke ◆"
	}
	others := make([]string, 0, len(wakers)-1)
	for _, from := range wakers[1:] {
		others = append(others, "@"+from)
	}
	return "woke ◆ (with " + strings.Join(others, ", ") + ")"
}

// ── one stat of the teams file per process per tick ─────────────────────────

// teamsStampMemo is the teams file's stamp, shared by every conversation this
// process holds, so a host with a dozen idle conversations stats the file once
// per [teamWatchEvery] and not a dozen times.
var teamsStampMemo struct {
	mu sync.Mutex
	at map[string]memoStamp
}

type memoStamp struct {
	stamp fileStamp
	taken time.Time
}

// sharedTeamsStamp is the teams file's stamp, at most [teamWatchEvery] old.
func sharedTeamsStamp(profile string) fileStamp {
	path := teams.Path(profile)
	teamsStampMemo.mu.Lock()
	defer teamsStampMemo.mu.Unlock()
	if teamsStampMemo.at == nil {
		teamsStampMemo.at = map[string]memoStamp{}
	}
	if held, ok := teamsStampMemo.at[path]; ok && time.Since(held.taken) < teamWatchEvery {
		return held.stamp
	}
	stamp := stampOf(path)
	teamsStampMemo.at[path] = memoStamp{stamp: stamp, taken: time.Now()}
	return stamp
}

// ── opening a conversation nobody holds ─────────────────────────────────────

// teamResume is the engine's door for opening a conversation headless, set by
// the process that can ([SetTeamResume]); nil where nothing can.
var teamResume atomic.Pointer[func(file, workspace string) error]

// SetTeamResume gives this process a way to open a team conversation that no
// window and no host holds, by its transcript and its folder, so the traffic
// that should wake it can. cmd/codeaf's engine sets it to a hello to that
// folder's session host, which opens the conversation and keeps it running
// with no surface attached; a window that opens it later joins that one. nil
// takes the door away.
func SetTeamResume(open func(file, workspace string) error) {
	if open == nil {
		teamResume.Store(nil)
		return
	}
	teamResume.Store(&open)
}

// teamRouse opens every one of targets that nothing holds, off the path, and
// says in the Traffic when one could not be. A conversation that is open
// somewhere watches for itself and is left alone. answers is the entry that
// asked for the wake, which a failure to wake answers, "" for none.
func (a *Agent) teamRouse(profile string, team teams.Team, wakes bool, targets []teams.Member, answers string) {
	if profile == "" || !wakes || len(targets) == 0 {
		return
	}
	a.team.mu.Lock()
	self := append([]string(nil), a.teamKeysLocked()...)
	a.team.mu.Unlock()
	var absent []teams.Member
	for _, member := range targets {
		if teamHoldsKey(self, member.Key) {
			continue
		}
		absent = append(absent, member)
	}
	if len(absent) == 0 {
		return
	}
	guard.Go("team rouse", func() {
		for _, member := range absent {
			if reason := rouseMember(member); reason != "" {
				who, to := "@"+member.Handle, member.Handle
				if member.Key == team.Manager {
					who, to = "◆", teams.ToManager
				}
				if to == "" {
					who, to = "a member with no handle", teams.ToRoom
				}
				a.teamSay(profile, team.ID, teams.Entry{
					Kind: teams.KindEvent, From: teams.FromSystem, To: to, Member: member.Key,
					State: teams.StateIdle, Text: "could not wake " + who + ": " + reason, Answers: answers,
				})
			}
		}
	})
}

// rouseMember opens one conversation when nothing holds it, and says why not
// when it could not; "" is open, either already or now.
func rouseMember(member teams.Member) string {
	file := strings.TrimSpace(member.File)
	if file == "" {
		return "the team has no transcript recorded for it"
	}
	if _, err := os.Stat(file); err != nil {
		return "its transcript is not on this machine"
	}
	if journalHeld(file) {
		return ""
	}
	open := teamResume.Load()
	if open == nil {
		return "no window has it open, and this process cannot open a conversation headless; it reads the message when it is next opened"
	}
	done := make(chan error, 1)
	guard.Go("team resume", func() { done <- (*open)(file, strings.TrimSpace(member.Where)) })
	select {
	case err := <-done:
		if err != nil {
			return "opening it headless failed: " + oneLineTeam(err.Error())
		}
		return ""
	case <-time.After(teamResumeWait):
		return "opening it headless did not finish in time"
	}
}

// journalHeld reports whether some process has a conversation's journal open:
// the journal's own flock ([lockSessionFile]), probed without blocking and
// let go at once. A file that cannot be locked at all reads as not held.
func journalHeld(path string) bool {
	file, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	defer file.Close()
	if err := filelock.Lock(file, true, true); err != nil {
		return filelock.IsBusy(err)
	}
	_ = filelock.Unlock(file)
	return false
}
