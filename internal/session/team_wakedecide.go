package session

// ── ONE WAKE PATH: ONE DECISION, ONE LINE ───────────────────────────────────
//
// A turn nobody typed is started by several things: a manager's directive, a
// team's replies, a member's own start, a command or a watch ending, a task
// landing, news that arrived as a turn ended. Every one of them starts the turn
// through the same door ([Agent.wakeAnsweringLocked]), and the team's rules
// about whether it may were written above that door once per caller. They
// disagreed. The traffic watch read the pause before it started anything and a
// command's ending did not; the watch handed a directive over and then found
// the turn refused; a fresh member held at the cap said nothing where the watch
// said why; and each caller wrote its own line, so `woke` was written for a
// turn that had been running all along.
//
// A hold that stands on the conversation itself, a stop until a turn next
// begins, is the same table's second half ([wakeStanding]): it binds the news
// that arrives through the session's own queue, and is read there.
//
// SO ONE FUNCTION DECIDES AND ONE WRITES. A caller says what is asking for the
// turn (a [wakeCause]); [Agent.decideWake] answers start, queue, hold or
// nothing (a [wakeDecision]) from one table of which rule binds which cause;
// and [Agent.sayWake] is the only place a wake's line is written to a team's
// traffic. A rule added for one cause is a row in the table, read by all.
//
// THE DECISION COMES IN TWO HALVES BECAUSE THE LOCKS DO. The team's rules read
// the teams file, the hour's wakes and the ledger, under the seat's lock and
// off the agent's ([wakeRules]). The engine's gates are the agent's own state
// and are read under a.mu ([Agent.decideWakeLocked]), where the turn is then
// started in the same hold, so the answer is the wake's own.

import (
	"fmt"
	"time"

	"github.com/Agent-Field/codeaf/internal/teams"
)

// wakeKind is what is asking for a turn nobody typed.
type wakeKind int

const (
	// wakeDirective is a member's manager: a directive, the answer to its
	// question, or the lines a pause held, found again when it is lifted.
	wakeDirective wakeKind = iota + 1
	// wakeReplies is a manager's team: its members' posts and events.
	wakeReplies
	// wakeStart is a fresh member's own start: the brief its manager started
	// it with (team_wake.go).
	wakeStart
	// wakeCommand is a background command ending ([Agent.enqueueJobNote]).
	wakeCommand
	// wakeWatch is a watch firing ([Agent.enqueueWatchNote]).
	wakeWatch
)

// wakeVerdict is what becomes of the turn that was asked for.
type wakeVerdict int

const (
	// wakeNothing is no turn and nothing to say: what asked is already read.
	wakeNothing wakeVerdict = iota
	// wakeStarts is a turn started on it.
	wakeStarts
	// wakeQueues is a turn already in flight, which takes it at its next step.
	wakeQueues
	// wakeHolds is no turn, for the reason the decision carries.
	wakeHolds
)

// wakeHolder is the rule that held a wake, which is what chooses its line.
type wakeHolder int

const (
	heldByNothing wakeHolder = iota
	// heldByPause is the person's pause of this member (team_pause.go).
	heldByPause
	// heldByLimit is the hour's bound on team wakes ([teamWakesPerHour]).
	heldByLimit
	// heldByCap is a team's daily cap (team_cap.go).
	heldByCap
	// heldByLoop is the loop breaker ([teamLoopRounds]).
	heldByLoop
	// heldByGate is one of the engine's own gates ([Agent.wakeRefusalLocked]).
	heldByGate
	// heldByStop is a stop of this conversation's turn, standing until a turn
	// next begins (team_stophold.go).
	heldByStop
	// heldByStandby is a membership made with no work to start on.
	heldByStandby
	// heldByAutoWakeOff is a team whose traffic starts no turn.
	heldByAutoWakeOff
)

// wakeCause is one request for a turn nobody typed.
type wakeCause struct {
	kind wakeKind
	// roles are the teams whose traffic asks, as this conversation sits in
	// them. A rule that holds some of them and not others takes those out.
	roles []teamRole
	// batch is, for a manager, the entries that ask in each team.
	batch map[string][]teams.Entry
	// pools are the roles whose caps bind this wake, when they are more than
	// the roles that ask: a first turn is held by a cap on any team the
	// conversation is in.
	pools []teamRole
}

// wakeDecision is the answer, and why when it is not a start.
type wakeDecision struct {
	verdict wakeVerdict
	by      wakeHolder
	// gate is the engine's gate, for [heldByGate].
	gate wakeGate
	why  string
}

// wakeRule is one team rule a wake may be held by: a hold, or the zero
// decision when it does not bind. It may take roles out of the cause.
type wakeRule func(a *Agent, profile string, c *wakeCause, now time.Time) wakeDecision

// wakeRules is which team rules bind which cause, in the order they are asked.
// The engine's gates bind every cause and are not listed ([Agent.decideWakeLocked]).
//
// THE PAUSE IS ASKED LAST, AND FRESH. The look that chose a member's roles read
// the teams file through a memo up to a tick old ([sharedTeamsStamp]), so a
// directive sent in the second after the person pressed Pause reached the wake
// as if nothing held it, and the turn it started consumed the directive the
// pause was meant to keep. A manager is never paused, so its row has no pause;
// a member has no loop to break. A first turn is not counted against the hour:
// it is the one turn its manager asked for by starting the member.
var wakeRules map[wakeKind][]wakeRule

// The table is filled at init because the rules reach, through the turn they
// may start, the watch that reads the table: a literal would be a cycle.
func init() {
	wakeRules = map[wakeKind][]wakeRule{
		wakeDirective: {wakeLimitRule, wakeCapRule, wakePauseRule},
		wakeReplies:   {wakeLoopRule, wakeLimitRule, wakeCapRule},
		wakeStart:     {wakeStandbyRule, wakePauseRule, wakeAutoWakeOffRule, wakeCapRule},
	}
}

// wakeStanding is which of a conversation's standing holds bind which cause.
// A standing hold is the agent's own state, read under a.mu where the news is
// queued, and what it holds is kept in the conversation as a record that
// starts no turn: the record is its line, and the traffic is told nothing.
//
// Only a command's ending and a watch's firing are held by a stop. The
// person's own words, a manager's next hand-off and a task landing still start
// a turn, which ends that hold (team_stophold.go).
var wakeStanding = map[wakeKind]struct{ stop bool }{
	wakeCommand: {stop: true},
	wakeWatch:   {stop: true},
}

// standingHoldLocked is the standing hold that keeps news of this kind from
// starting a turn, with the sentence its record is given as why; the zero
// decision when none does. The caller holds a.mu.
func (a *Agent) standingHoldLocked(kind wakeKind) wakeDecision {
	binds := wakeStanding[kind]
	if binds.stop && a.team.stopHold.held.Load() {
		return wakeDecision{verdict: wakeHolds, by: heldByStop, why: teamStopHeldRule}
	}
	return wakeDecision{}
}

// decideTeamWake asks the team's rules for this cause, and answers the first
// hold or a start. It takes no lock of its own and must not be called under
// a.mu: the rules read the teams file and the ledger.
func (a *Agent) decideTeamWake(profile string, c *wakeCause, now time.Time) wakeDecision {
	for _, rule := range wakeRules[c.kind] {
		if d := rule(a, profile, c, now); d.by != heldByNothing {
			return d
		}
	}
	return wakeDecision{verdict: wakeStarts}
}

// decideWake is the whole decision, made before anything is read or moved: the
// team's rules for this cause, then the engine's gates as they stand. A start
// answered here is a start still to be made, under a later hold of a.mu
// ([Agent.wakeWith]), which asks the gates again.
func (a *Agent) decideWake(profile string, c *wakeCause, now time.Time) wakeDecision {
	if d := a.decideTeamWake(profile, c, now); d.verdict != wakeStarts {
		return d
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.decideWakeLocked()
}

// decideWakeLocked is the engine's half of the decision: a start, a turn in
// flight to queue behind, or the gate that refuses and why. The caller holds
// a.mu, and a start decided here is true until that hold is let go.
func (a *Agent) decideWakeLocked() wakeDecision {
	switch gate, why := a.wakeRefusalLocked(false, false); gate {
	case wakeGateOpen:
		return wakeDecision{verdict: wakeStarts}
	case wakeGateRunning:
		return wakeDecision{verdict: wakeQueues}
	default:
		return wakeDecision{verdict: wakeHolds, by: heldByGate, gate: gate, why: why}
	}
}

func wakeLimitRule(a *Agent, _ string, _ *wakeCause, now time.Time) wakeDecision {
	if why := a.teamWakeLimited(now); why != "" {
		return wakeDecision{verdict: wakeHolds, by: heldByLimit, why: why}
	}
	return wakeDecision{}
}

func wakeCapRule(a *Agent, profile string, c *wakeCause, _ time.Time) wakeDecision {
	pools := c.pools
	if pools == nil {
		pools = c.roles
	}
	if why := a.teamCapHold(profile, pools); why != "" {
		return wakeDecision{verdict: wakeHolds, by: heldByCap, why: why}
	}
	return wakeDecision{}
}

// wakePauseRule keeps the roles the person has not paused, remembering a hold
// for each they have, and holds the wake when none is left.
func wakePauseRule(a *Agent, profile string, c *wakeCause, now time.Time) wakeDecision {
	kept, paused := a.teamPausedSplit(profile, c.roles)
	return a.wakeSetAside(profile, c, now, wakeDecision{verdict: wakeHolds, by: heldByPause, why: teamPausedWhy}, kept, paused)
}

// wakeStandbyRule sets aside the memberships made on standby: the start line
// is the member's role, read at its first turn, and the manager's first
// directive is what begins that turn (team_pause.go).
func wakeStandbyRule(a *Agent, profile string, c *wakeCause, now time.Time) wakeDecision {
	return a.wakeSetAsideWhere(profile, c, now, heldByStandby, func(role teamRole) bool { return role.standby })
}

// wakeAutoWakeOffRule honours the team's switch. The conversation is already
// open, which is what "opened" means in its line, and the brief stays unread
// until a turn something else starts: consuming it now would hand it over with
// nobody to read it.
func wakeAutoWakeOffRule(a *Agent, profile string, c *wakeCause, now time.Time) wakeDecision {
	return a.wakeSetAsideWhere(profile, c, now, heldByAutoWakeOff, func(role teamRole) bool { return !role.wakes })
}

// wakeSetAsideWhere is [Agent.wakeSetAside] for a rule that reads the role.
func (a *Agent) wakeSetAsideWhere(profile string, c *wakeCause, now time.Time, by wakeHolder, holds func(teamRole) bool) wakeDecision {
	var kept, aside []teamRole
	for _, role := range c.roles {
		if holds(role) {
			aside = append(aside, role)
		} else {
			kept = append(kept, role)
		}
	}
	return a.wakeSetAside(profile, c, now, wakeDecision{verdict: wakeHolds, by: by}, kept, aside)
}

// wakeSetAside is a rule that holds a wake in some of its teams and not in
// others. The teams it holds are taken out of the cause and told so here; the
// wake goes on for the rest, and is held outright when none is left, which its
// caller then says of the teams that are.
func (a *Agent) wakeSetAside(profile string, c *wakeCause, now time.Time, held wakeDecision, kept, aside []teamRole) wakeDecision {
	if len(kept) == 0 {
		c.roles = aside
		return held
	}
	if len(aside) > 0 {
		a.sayWake(profile, wakeCause{kind: c.kind, roles: aside}, held, now)
	}
	c.roles = kept
	return wakeDecision{}
}

// wakeLoopRule is the loop breaker. Its hold carries words only on the wake
// that trips it: the person is asked once, and every wake held after that is
// held unsaid until they speak.
func wakeLoopRule(a *Agent, _ string, _ *wakeCause, _ time.Time) wakeDecision {
	held, trip := a.teamLoopHeld()
	if !held {
		return wakeDecision{}
	}
	d := wakeDecision{verdict: wakeHolds, by: heldByLoop}
	if trip {
		d.why = fmt.Sprintf("asks: the team has woken me %d times with no word from you, so I have stopped being woken by it until you say something", teamLoopRounds)
	}
	return d
}

// ── the one writer ──────────────────────────────────────────────────────────

// sayWake writes what became of a wake to the traffic of each team that asked
// for it, and is the only place a wake's line is written. Most decisions have
// a line and some have none: a directive's pause is said by the send that met
// it (team_sendoutcome.go), a loop already tripped has been said, a turn that
// was already running was not woken, and a first turn on a brief says nothing.
//
// THE WORDS ARE READ BACK BY MACHINES. `team_send` answers its manager from
// these lines ([Agent.teamSendOutcome]), the rail and the digest parse `woke `,
// and the desktop app matches the held and stopped lines whole. They are
// written here and nowhere else so that none can drift.
func (a *Agent) sayWake(profile string, c wakeCause, d wakeDecision, now time.Time) {
	if !a.wakeSayOnce(d, now) {
		return
	}
	for _, role := range c.roles {
		entry, ok := a.wakeLine(c, role, d)
		if !ok || (wakeSaidPerEntry(d) && teamSaidAlready(profile, role.id, entry)) {
			continue
		}
		a.teamSay(profile, role.id, entry)
	}
}

// wakeSaidPerEntry reports whether a decision's line is said once for the entry
// it answers, by looking at the log. A member's first turn is looked at by two
// watches (team_wake.go, team_wakewatch.go) and a fresh conversation is fresh
// every time it is opened, so these holds would otherwise be said again.
func wakeSaidPerEntry(d wakeDecision) bool {
	return d.by == heldByStandby || d.by == heldByPause || d.by == heldByGate
}

// wakeSayOnce reports whether this decision's line is still to be said. A cap
// hold is said once per conversation and reason, because every held wake says
// the same thing and the cap's packet is what asks the person; the hour's limit
// is said once an hour.
func (a *Agent) wakeSayOnce(d wakeDecision, now time.Time) bool {
	a.team.mu.Lock()
	defer a.team.mu.Unlock()
	w := &a.team.watch
	switch d.by {
	case heldByCap:
		said := w.capSaid == d.why
		w.capSaid = d.why
		return !said
	case heldByLimit:
		if !w.limitSaid.IsZero() && now.Sub(w.limitSaid) < time.Hour {
			return false
		}
		w.limitSaid = now
	}
	return true
}

// wakeLine is the line one team's traffic is given for a decision, and false
// when the decision has none.
func (a *Agent) wakeLine(c wakeCause, role teamRole, d wakeDecision) (teams.Entry, bool) {
	who, to := "◆", teams.ToManager
	if !role.manager {
		who, to = "@"+role.handle, role.handle
	}
	entry := teams.Entry{Kind: teams.KindEvent, From: teams.FromSystem, To: to, Member: role.key, State: teams.StateIdle}
	switch {
	case d.verdict == wakeQueues:
		// A TURN ALREADY RUNNING WAS NOT WOKEN. `woke` was written for it, and a
		// manager's send read that back as the member having started on its
		// directive. The turn takes the lines at its next step and the traffic
		// says nothing: the send reads a running member off its journal.
		return teams.Entry{}, false
	case d.verdict == wakeStarts && c.kind == wakeStart:
		// A first turn on a brief has never had a line: the start above it is
		// the manager's own, and the member's turn says the rest.
		return teams.Entry{}, false
	case d.verdict == wakeStarts:
		entry.State = teams.StateRunning
		if role.manager {
			// Under the first waker's name, with the others who replied.
			entry.From, entry.Text = teamWakers(c.batch[role.id])[0], teamWokeManagerText(c.batch[role.id])
			break
		}
		// The wake answers the directive that caused it, the one the boundary
		// just handed over, so a reader draws the member as working on that
		// thread rather than as a line of its own.
		entry.From, entry.Text, entry.Answers = teams.FromManager, teamWokePrefix+who, a.teamAnswering(role.id)
	case d.by == heldByCap:
		entry.Text = teamHeldPrefix + who + ": " + d.why
	case d.by == heldByLimit:
		entry.Text = teamCouldNotPrefix + who + ": " + d.why
	case d.by == heldByGate && d.why != "":
		entry.Text = teamCouldNotPrefix + who + ": " + d.why
		if !role.manager {
			entry.Answers = a.teamAsked(role.id)
		}
	case d.by == heldByPause && c.kind == wakeStart:
		// A directive's pause is said by the send that met it. A start has no
		// send behind it, so the member says the same line, answering its start.
		entry.Text, entry.Answers = teamHeldPausedText(role.handle), a.teamAsked(role.id)
	case d.by == heldByStandby:
		entry.Text = teamStandbyText(role.handle)
	case d.by == heldByAutoWakeOff:
		entry.Text = teamAutoWakeOffText(role.handle)
	case d.by == heldByLoop && d.why != "":
		// The manager asks the room: the rail draws it in the needs-you colour
		// and a digest reads it as the manager waiting.
		entry.From, entry.To, entry.State, entry.Text = teams.FromManager, teams.ToRoom, teams.StateAsking, d.why
	default:
		return teams.Entry{}, false
	}
	return entry, true
}
