package lane

import "time"

// ── ROLES: WHO IS ASKING, AND WHAT THAT IS WORTH ────────────────────────────
//
// Every model call in this build is made ON SOMEBODY'S BEHALF, and the four
// numbers that decide how it is routed are all consequences of that one fact:
// what a second of its wait is worth (λ), how sure we have to be the answer is
// usable (QualityNeed), how many more calls there are to learn from (Horizon),
// and whether a person is watching this particular stream (Visible).
//
// Until this table those four were decided at the call site, separately, by
// whoever wrote it. A talk turn said `IntentInteractive` and computed λ from
// `lane.Lambda(true, false, 0, 0, 0)`; a standing run said `IntentBackground`
// and computed nothing; the memory reflex said nothing at all and inherited
// whatever the surrounding context happened to carry. Three sites, three
// answers, and the first time one of them was fixed the other two drifted —
// which is the same argument the response boundary and the lane registry are
// both written to (docs/ARCHITECTURE.md Decisions 8 and 10).
//
// SO THE NUMBERS LIVE HERE AND NOWHERE ELSE. A call site names its role; it
// does not name a λ, an intent, a quality bar or a horizon. A structural test
// fails the build on a call site that reaches past this table for one of them.
//
// AND `Visible` IS THE ONE A PERSON FEELS. A conversation makes several calls
// per turn that are nobody's business but the machine's — a title, a memory
// reflex, a reply check on the reflex tier — and until this field the status
// line drew whichever of them answered last, so a person watching a slow talk
// answer was shown the lane and the throughput of a naming errand that had
// nothing to do with it. Only a visible role owns the phase clock.

// Role is who a model call is being made for.
type Role string

const (
	// RoleUnknown is a call that named no role. It is treated as a hidden
	// background errand — the conservative reading, because a call that claims
	// to be a person waiting when it is not buys speed with somebody's money.
	RoleUnknown Role = ""
	// RoleTalk is the conversation's own turn: a person is sitting in front of
	// it, reading the answer as it arrives.
	RoleTalk Role = "talk"
	// RoleLeafAttached is a task node's turn while somebody is watching the
	// room it runs in, and RoleLeafUnattended the same node with nobody there.
	// They differ ONLY in what a second is worth, which is the whole of the
	// argument in internal/session's turnLambda.
	RoleLeafAttached   Role = "leaf.attached"
	RoleLeafUnattended Role = "leaf.unattended"
	// RoleStanding is a standing order's run: unattended by construction.
	RoleStanding Role = "standing"
	// RoleMemory is the memory reflex and the consolidation pass.
	RoleMemory Role = "memory"
	// RoleAuxiliary is a side errand of the turn's — a title, a route question,
	// a fold-up, a reply check. It is the role the reflex tier mostly serves.
	RoleAuxiliary Role = "auxiliary"
	// RoleJudge is a gate reading an answer: the route judge, the checkpoint
	// reader, the guardian. It is the one role whose quality bar is high and
	// whose speed is worth little.
	RoleJudge Role = "judge"
	// RoleDesign is the harness designer and the craft passes.
	RoleDesign Role = "design"
	// RoleProbe is the one-token measurement bought on a keystroke.
	RoleProbe Role = "probe"
	// THERE IS NO ROLE FOR A HEDGE, and the absence is the law. The second
	// request of a race is the SAME ERRAND as the first — the same person is
	// waiting for the same answer — so it inherits the role it is rescuing and
	// is routed, priced and drawn exactly as that errand is. A role of its own
	// would say that a rescue of a naming errand is something a person is
	// reading, which is how the status line came to show a side call's lane
	// under somebody's talk answer in the first place.
	// RoleMedia is an image, a piece of music, a transcription or a document
	// parse. It produces no token stream at all, which is why it is named here
	// and excluded there (see [RoleFacts.Streams]).
	RoleMedia Role = "media"
)

// RoleFacts is everything the router needs to know about a role, and it is the
// ONLY place any of it is written down.
type RoleFacts struct {
	// Interactive and Critical are what [Lambda] is asked, in that order: is
	// somebody waiting on this answer, and is it on the path to something else
	// that is waiting.
	//
	// AN UNATTENDED NODE IS NEITHER, and the second half of that is worth
	// stating because it looks like an oversight. A node nobody is watching is
	// on the path to a report nobody has asked to read yet, and paying to make
	// it arrive sooner buys a person nothing: the seconds are only worth money
	// once somebody is there to spend them. That is the argument
	// [Agent.turnLambda] has always made and this row is where it now lives.
	Interactive bool
	Critical    bool
	// QualityNeed is the share of answers that must come back usable, and it
	// is a GATE and never a weight (Decision 10's law). Zero is "no bar", which
	// is the honest reading for an errand that can simply be asked again.
	QualityNeed float64
	// Horizon is roughly how many more calls a session in this role will make.
	// It scales exploration: a role that will ask once must not spend that once
	// on a lane it is curious about.
	Horizon int
	// Visible says whether a person is reading THIS stream as it arrives, which
	// is what decides whether the phase clock and the served segment are this
	// call's to move.
	Visible bool
	// Streams says whether this role produces a token stream at all. A role
	// that does not — media — has no first token, no rate and no drift test,
	// so it takes the deadline-only half of the watch and its phase is a
	// single word with a clock under it.
	Streams bool
	// Verb is the phase word for this role while it is producing. It is
	// "writing" for everything that makes text and "drawing" for media, and it
	// is here rather than in the surface because a role is what decides it.
	Verb string
	// Patience is how many times [VisiblePatience] this role will wait before
	// something is done about the silence, whatever is believed about the lane.
	//
	// IT SCALES THE CEILING AND NEVER DECIDES WHETHER THERE IS ONE. That is the
	// funnel law said about waiting: a role sets what a second is worth and how
	// long is too long, and no role is exempt from being asked. A role with no
	// figure here reads as [RoleUnknown]'s, which is the conservative direction
	// — a background errand waits longer than a person does, never less.
	Patience float64
}

// VisiblePatience is the longest a person watching an empty line is asked to
// wait before this build does something about it.
//
// TEN SECONDS IS A CEILING AND NOT A PRIOR. Everything else in this package is
// learned; this one is a statement about people rather than about machines, and
// it is what makes the invariant hold from a cold store, where there is nothing
// to learn from. It is deliberately far above every believed first token the
// measured world has — the slowest lane of seventeen starts at 3.0s at the
// median and 9.3s at the ninetieth — so a healthy lane never reaches it and a
// stalled one always does.
const VisiblePatience = 10 * time.Second

// ActionFloor is the shortest silence worth acting on, whatever a belief says.
//
// Below it a second request is racing the network rather than the lane: it has
// its own handshake, its own router hop and its own prefill to pay before it can
// say anything, and the measured floor for that is a few hundred milliseconds.
const ActionFloor = 700 * time.Millisecond

// SpreadFloor is the smallest per-draw variability the waiting arithmetic will
// use, in nats of log-spread.
//
// IT IS THE DIFFERENCE BETWEEN TWO SPREADS AND THE WHOLE OF WHY IT EXISTS. A
// posterior's variance is the variance of the ESTIMATE — how well the median is
// known — and it shrinks toward nothing after a few dozen observations. What a
// wait is judged against is how variable ONE DRAW is, which never shrinks below
// the lane's own variability. One nat is the shape the measured sheet published:
// a p90 about three and a half times the p50 on the worst lanes. A controller
// handed the estimate's spread instead would believe a tail impossible and would
// never hedge the lane that has one.
const SpreadFloor = 1.0

// Hysteresis is how much better acting has to look before it is done.
//
// It is the m of the design's one inequality and it is a statement about people
// rather than about machines, like the two above: a quarter of a second is
// about the smallest difference in waiting anybody notices, so buying less than
// that is not a rescue, it is a controller flapping at its own crossing.
const Hysteresis = 250 * time.Millisecond

// roles is the table. THERE ARE NO NUMBERS OUTSIDE IT.
var roles = map[Role]RoleFacts{
	RoleTalk:           {Interactive: true, QualityNeed: 0.9, Horizon: 50, Visible: true, Streams: true, Verb: "writing", Patience: 1},
	RoleLeafAttached:   {Interactive: true, QualityNeed: 0.9, Horizon: 50, Visible: true, Streams: true, Verb: "writing", Patience: 1},
	RoleLeafUnattended: {Interactive: false, QualityNeed: 0.9, Horizon: 50, Visible: false, Streams: true, Verb: "writing", Patience: 3},
	RoleStanding:       {Interactive: false, QualityNeed: 0.9, Horizon: 20, Visible: false, Streams: true, Verb: "writing", Patience: 6},
	RoleMemory:         {Interactive: false, QualityNeed: 0.8, Horizon: 10, Visible: false, Streams: true, Verb: "writing", Patience: 3},
	RoleAuxiliary:      {Interactive: false, QualityNeed: 0.8, Horizon: 10, Visible: false, Streams: true, Verb: "writing", Patience: 3},
	RoleJudge:          {Interactive: false, Critical: true, QualityNeed: 0.95, Horizon: 10, Visible: false, Streams: true, Verb: "writing", Patience: 6},
	RoleDesign:         {Interactive: false, Critical: true, QualityNeed: 0.9, Horizon: 20, Visible: false, Streams: true, Verb: "writing", Patience: 6},
	RoleProbe:          {Interactive: false, Horizon: 1, Visible: false, Streams: true, Verb: "writing", Patience: 0.5},
	RoleMedia:          {Interactive: true, Horizon: 1, Visible: true, Streams: false, Verb: "drawing", Patience: 6},
	RoleUnknown:        {Interactive: false, QualityNeed: 0.8, Horizon: 10, Visible: false, Streams: true, Verb: "writing", Patience: 3},
}

// Facts is what is believed about a role. An unregistered role reads as
// [RoleUnknown] rather than as a zero struct, so a name nobody added to the
// table behaves like a background errand instead of like a free one.
func (r Role) Facts() RoleFacts {
	if facts, ok := roles[r]; ok {
		return facts
	}
	return roles[RoleUnknown]
}

// Known reports whether this role is in the table. It is what the funnel's own
// check reads: a call that named nothing is legal in production and a defect in
// a test (see internal/provider's roles_test.go).
func (r Role) Known() bool {
	_, ok := roles[r]
	return ok && r != RoleUnknown
}

// Visible reports whether a person is reading this role's stream as it arrives.
func (r Role) Visible() bool { return r.Facts().Visible }

// Lambda is what a second of this role's wait is worth, in seconds per dollar.
// It is [Lambda] asked with the role's own answers rather than with a call
// site's guess, which is the whole point of the table.
func (r Role) Lambda() float64 {
	facts := r.Facts()
	return Lambda(facts.Interactive, facts.Critical, 0, 0, 0)
}

// Ceiling is the longest this role waits before something is done about a
// silence, whatever is believed about the lane serving it.
//
// EVERY ROLE HAS ONE. A role that returned zero here would be a role outside the
// invariant, and there is no such role: [RoleFacts.Patience] scales the ceiling
// and a role the table forgot borrows [RoleUnknown]'s rather than being handed
// forever.
func (r Role) Ceiling() time.Duration {
	patience := r.Facts().Patience
	if patience <= 0 {
		patience = roles[RoleUnknown].Patience
	}
	return time.Duration(patience * float64(VisiblePatience))
}

// Roles is every role in the table, for the structural test that insists each
// one is exercised. The order is not meaningful.
func Roles() []Role {
	list := make([]Role, 0, len(roles))
	for role := range roles {
		list = append(list, role)
	}
	return list
}
