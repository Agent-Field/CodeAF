package lane

import (
	"sync/atomic"
	"time"
)

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
	// RoleRecall prepares a person's next answer. Its output is private, but
	// its delay is interactive because the main request has not started yet.
	RoleRecall Role = "recall"
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
	// RoleTool is a HAND ON THE BELT asking a model a question: `view_image`
	// looking at a picture, `read` sensing a screenshot, a recording or a video
	// (internal/session's toolask.go).
	//
	// IT IS NOT [RoleAuxiliary] AND THE DIFFERENCE IS WHO IS WAITING. A title, a
	// route question and a fold-up happen beside a turn and nobody is held up by
	// them; a tool's question happens INSIDE one, with the person watching a tool
	// row that cannot finish until it answers — so a second of it costs what a
	// second of talk costs, and it is impatient for the same reason talk is.
	// Calling it auxiliary bought it a background errand's patience, which is how
	// a look at a screenshot came to be allowed four and a half minutes.
	//
	// IT IS NOT [RoleMedia] EITHER, and the difference there is what comes back.
	// Media is work that MAKES something — a picture, a piece of music — and
	// produces no token stream at all, which is what excludes it from the token
	// controller. This produces TEXT, so it is watched like every other role that
	// does; what is not true of it is that anybody READS that text arriving,
	// which is [RoleFacts.Visible] and is false here.
	RoleTool Role = "tool"
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
//
// RE-MEASURED ON 2026-09-10 AND KEPT, against 16,427 finished attempts and the
// 10,028 of them that recorded a first token (the reading is beside this wave
// as `af-rec-r4.patience.md`). A watched turn's first token is 1.6s at the
// median, 8.4s at the ninetieth and 13.1s at the ninety-fifth, so ten seconds
// is this build's own ninety-second percentile — which is where Dean and
// Barroso's rule for a hedged request ("issue the second near the ninety-fifth
// percentile of the expected latency, and one or two percent of extra requests
// removes most of the tail") and Nielsen's ten-second limit on holding a
// person's attention land on the same number from opposite directions.
//
// AND FIVE WOULD BE WORSE ON BOTH COUNTS, which is the answer to the obvious
// question. Acting at five seconds touches 28.9% of all calls against 13.9% at
// ten — twice the traffic — and the yield per extra request FALLS, from 22.1
// rescues per hundred to 17.6, because under ten seconds almost everything
// still silent is an ordinary call in progress rather than one in trouble. The
// knee, where silence stops being normal and starts predicting failure, is
// between ten and fifteen seconds: of the calls still silent at 5s, 10s, 15s
// and 20s, 82%, 78%, 73% and 70% still answered cleanly.
const VisiblePatience = 10 * time.Second

// SpokenWithin is how long any wait in the request path may last before the
// person is told what it is waiting for.
//
// IT IS NOT A TIMEOUT AND NOTHING IS CUT AT IT. It is the other half of
// [VisiblePatience], and the half this build was missing: ten seconds is when
// we MOVE, and until this wave it was also the first moment a person heard
// anything at all. A wait that is real is reported (`docs/design/waiting/
// DESIGN.md`), and four waits in the request path had no voice — the limiter's
// slot, the empty-200 re-ask, the abandon grace and the connectivity probe.
//
// ONE SECOND, because that is the oldest measured number in this whole subject:
// Miller 1968 and Card 1991 through Nielsen 1993, one second is the limit of a
// person's uninterrupted flow of thought, and past it they notice the delay and
// the system owes them a sign that it is working. It costs nothing — a phase
// line is words, not a request — so there is no trade to make against it.
const SpokenWithin = time.Second

// TurnGiveUp is how long a conversation turn may spend reaching a model before
// the person is told it could not be reached.
//
// IT IS A BOUND THAT DID NOT EXIST. A turn's give-up was the product of every
// controller under it — attempts times arms times rungs times models — which is
// nobody's number, and in practice unbounded: the call census of 2026-09-10
// found chains of sixteen and seventeen identical sends running eleven minutes
// and still ending refused.
//
// NINETY SECONDS, measured. Of the 167 retry chains in ten days that reached a
// clean answer, 29% landed within thirty seconds, 56% within sixty and 66%
// within ninety; 120 seconds buys nine points more and costs the person another
// half-minute of a dead cursor. The ten points between sixty and ninety are
// exactly where the SECOND MACHINE lands, which is the other fact from the same
// reading: 126 of those 167 winning chains used two distinct machines and only
// 41 won on one. So ninety seconds is one fair try at every model in the chain
// with a move between them, and the third of today's winners that falls outside
// it is made of same-machine repeats that the never-repeat rule deletes.
const TurnGiveUp = 90 * time.Second

// ActionFloor is the shortest silence worth acting on, whatever a belief says.
//
// Below it a second request is racing the network rather than the lane: it has
// its own handshake, its own router hop and its own prefill to pay before it can
// say anything, and the measured floor for that is a few hundred milliseconds.
const ActionFloor = 700 * time.Millisecond

// SpreadFloor is how variable ONE ANSWER from a pair is taken to be when
// nothing has been published about it, in nats of log-spread.
//
// IT IS THE DIFFERENCE BETWEEN TWO SPREADS AND THE WHOLE OF WHY THERE IS A
// FLOOR AT ALL. A posterior's variance is the variance of the ESTIMATE — how
// well the median is known — and it shrinks toward nothing after a few dozen
// observations. What a wait is judged against is how variable ONE DRAW is,
// which never shrinks below the lane's own variability, and a controller handed
// the estimate's spread would believe a tail impossible and would never hedge
// the lane that has one.
//
// AND IT IS A PRIOR ABOUT ONE DRAW RATHER THAN A FLOOR UNDER EVERY LANE, which
// is the rule and not the figure. One nat is the shape the measured sheet
// published on its WORST lanes — a p90 about three and a half times the p50 —
// and the sheet publishes each lane's own: the one §K's rows were proved
// against says 430 ms and 900 ms, which is 0.577. Held under every lane, one
// nat asserted that every lane's tail is the worst tail on the sheet, and
// `bench/lanelab` measured what that cost — a healthy talk turn crossed the
// inequality at the action floor, on every request that had not started by
// then. So a lane's own published dispersion is the floor wherever there is one
// ([Hierarchy.Draw]) and this is the prior for where there is not, which is the
// law the rest of this package keeps everywhere: a measured thing outranks a
// prior, and a prior is what an unmeasured thing gets instead of a certainty.
//
// AND WHERE NOTHING IS PUBLISHED, WHAT IS OBSERVED OUTRANKS IT TOO. A thinking
// phase has no sheet row of any kind, so this prior stood under the duration
// clock permanently and its abnormality gate could never close. It is now what
// the think chain's own dispersion account starts from and shrinks away from as
// thoughts are folded in ([chains.draw]), which is the same law again: a
// measured thing outranks a prior, whoever measured it.
const SpreadFloor = 1.0

// SpreadTightest is the narrowest one draw is ever believed to be, in nats of
// log-spread.
//
// A DISPERSION LEARNED FROM OBSERVATIONS CAN REACH ZERO AND MUST NOT BE
// BELIEVED THERE. A model asked the same question at the same rung really does
// deliberate for nearly the same time, and a handful of such draws would fit a
// spread of nothing — which says the tail is IMPOSSIBLE, and a controller that
// believes that acts on the first draw that is a little late. This is a p90 a
// fifth above the p50, tighter than anything the measured sheet publishes about
// any lane, so it bounds the claim without bounding what a real measurement of
// a real model is allowed to say.
const SpreadTightest = 0.15

// Hysteresis is how much better acting has to look before it is done.
//
// It is the m of the design's one inequality and it is a statement about people
// rather than about machines, like the two above: a quarter of a second is
// about the smallest difference in waiting anybody notices, so buying less than
// that is not a rescue, it is a controller flapping at its own crossing.
const Hysteresis = 250 * time.Millisecond

// roles is the table. THERE ARE NO NUMBERS OUTSIDE IT.
//
// THE PATIENCE COLUMN WAS RE-MEASURED ON 2026-09-10. Each multiplier is a
// percentile of the first token its own roles really see: talk's 1 is ten seconds
// against a watched turn's ninetieth at 8.4s; the unattended 3 is thirty seconds
// against a task node's ninety-seventh (its ninety-fifth is 17.3s and its
// ninety-ninth 58.2s); standing's 6 is sixty seconds, just past that
// ninety-ninth; and the probe's 0.5 is five seconds against a reflex's
// ninety-eighth (ninetieth 0.9s, ninety-ninth 7.2s). Nobody watches an
// unattended call, so the cost of waiting on one is zero and the cost of acting
// is money — which is why the patient roles act at the ninety-seventh and the
// watched one at the ninetieth.
//
// TWO OF THEM MOVED ON 2026-09-11, AND BOTH MOVED FOR ONE REASON: A CEILING
// OUTSIDE ITS OWN CALLER'S WINDOW CAN NEVER FIRE.
//
//   - RECALL, 1 → 0.2. Its caller (internal/reflex's Route) bounded the whole
//     routing pass at this role's CEILING, which made the two numbers the same
//     number: the hazard controller was told to act on silence at exactly the
//     instant the call was killed, so a recall on a slow machine was never once
//     moved to a fast one. The 2026-09-11 census measured the consequence —
//     sixteen recalls, mean 4.3s, max 10.7s, served by DekaLLM at a median of
//     5.5s and Io Net at 4.3s while DeepInfra answered the same role in 1.7s.
//     THE QUANTITY IS THE EARLIEST MOMENT AT WHICH SILENCE STOPS BEING NORMAL
//     FOR THIS ROLE: above the fastest measured machine's median answer, so a
//     healthy one is never cut mid-answer, and below the slow ones', so a slow
//     one is always moved off. That interval is (1.7s, 4.3s) and two seconds is
//     its low end — the soonest this can act without acting on a machine that is
//     working. The give-up that scales with it is eighteen seconds, and the
//     caller now reads THAT as its window so the rescue has somewhere to land.
//   - JUDGE, 6 → 1. Sixty seconds was outside every window its callers set, so
//     the same thing was true: nothing was ever moved off a silent judge machine,
//     and the 2026-09-11 census has four mark readings dying at 30,001–30,002 ms
//     having decided nothing.
//     THE QUANTITY IS THE SAME ONE RECALL'S IS, read against a caller's window
//     rather than a machine's median: the ceiling must be LATE ENOUGH that a
//     healthy machine is never abandoned mid-answer — above the measured first
//     token, 8.4s at the ninetieth — and EARLY ENOUGH that a second machine can
//     still answer one inside the window the caller allows: that window less the
//     same 8.4s. The tightest window that constrains it is the pre-turn route
//     read's twenty seconds, so the interval is (8.4s, 11.6s), and ten seconds is
//     in it and is [VisiblePatience] itself, so the ceiling needs no figure of its
//     own. The give-up that scales with it is ninety seconds, above every
//     caller's own bound, which is what a give-up is for.
//     THE GUARDIAN'S TEN SECONDS IS NOT ONE OF THOSE WINDOWS, and the reason is
//     the reason it is the one named exception everywhere else in this wave: when
//     it goes quiet the fall-through is to ASK THE PERSON, which is a better
//     answer than a second machine's guess and costs nothing. There is nothing a
//     rescue could buy inside that window, so it does not bound this column.
var roles = map[Role]RoleFacts{
	RoleTalk:           {Interactive: true, QualityNeed: 0.9, Horizon: 50, Visible: true, Streams: true, Verb: "writing", Patience: 1},
	RoleLeafAttached:   {Interactive: true, QualityNeed: 0.9, Horizon: 50, Visible: true, Streams: true, Verb: "writing", Patience: 1},
	RoleLeafUnattended: {Interactive: false, QualityNeed: 0.9, Horizon: 50, Visible: false, Streams: true, Verb: "writing", Patience: 3},
	RoleStanding:       {Interactive: false, QualityNeed: 0.9, Horizon: 20, Visible: false, Streams: true, Verb: "writing", Patience: 6},
	RoleRecall:         {Interactive: true, Critical: true, QualityNeed: 0.8, Horizon: 10, Visible: false, Streams: true, Verb: "writing", Patience: 0.2},
	RoleMemory:         {Interactive: false, QualityNeed: 0.8, Horizon: 10, Visible: false, Streams: true, Verb: "writing", Patience: 3},
	RoleAuxiliary:      {Interactive: false, QualityNeed: 0.8, Horizon: 10, Visible: false, Streams: true, Verb: "writing", Patience: 3},
	RoleJudge:          {Interactive: false, Critical: true, QualityNeed: 0.95, Horizon: 10, Visible: false, Streams: true, Verb: "writing", Patience: 1},
	RoleDesign:         {Interactive: false, Critical: true, QualityNeed: 0.9, Horizon: 20, Visible: false, Streams: true, Verb: "writing", Patience: 6},
	RoleProbe:          {Interactive: false, Horizon: 1, Visible: false, Streams: true, Verb: "writing", Patience: 0.5},
	// A hand's question: somebody IS waiting (the tool row is open in front of
	// them), the answer is not on the path to anything else, it is one shot with
	// no conversation behind it, its text is never DRAWN though it is text and is
	// watched as text, and it is as impatient as talk because the wait is the
	// person's own.
	RoleTool:    {Interactive: true, QualityNeed: 0.8, Horizon: 1, Visible: false, Streams: true, Verb: "writing", Patience: 1},
	RoleMedia:   {Interactive: true, Horizon: 1, Visible: true, Streams: false, Verb: "drawing", Patience: 6},
	RoleUnknown: {Interactive: false, QualityNeed: 0.8, Horizon: 10, Visible: false, Streams: true, Verb: "writing", Patience: 3},
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

// GiveUp is how long a call in this role may spend reaching a model before it
// stops trying, and it is the WHOLE of that bound — the one deadline
// docs/design/recovery/DESIGN.md §4 replaced eleven budgets with.
//
// IT IS THE CEILING'S ARITHMETIC APPLIED TO THE OTHER MEASURED NUMBER. The
// ceiling is when we ACT on a silence ([VisiblePatience] × the role's patience);
// this is when we stop acting at all ([TurnGiveUp] × the same patience), so the
// two scale together off one column and a role cannot be patient about one and
// impatient about the other. Talk is ninety seconds, a task node's four and a
// half minutes, a standing pass's nine, a probe's forty-five seconds.
//
// WHAT IT REPLACED, and why none of those numbers is missed: six attempts and
// two minutes for a watched call, sixty attempts and ten minutes for a patient
// one, three transport faults, eight free moves, four arms and one ladder arm —
// each bounding a different axis, their product nobody's number, and the census
// of 2026-09-10 measuring what it produced (chains of seventeen identical sends
// over eleven minutes, ending refused). A person can be told this one.
// AND THE PERSON'S OWN PATIENCE MULTIPLIES IT ([UsePatience]), because the one
// thing they could ever turn about how hard this build tries is a statement
// about how long they are willing to wait — which is this figure and nothing
// else now.
func (r Role) GiveUp() time.Duration {
	patience := r.Facts().Patience
	if patience <= 0 {
		patience = roles[RoleUnknown].Patience
	}
	return time.Duration(patience * asked() * float64(TurnGiveUp))
}

// ── WHAT A PERSON MAY TURN ──────────────────────────────────────────────────
//
// `response.attempts` was a COUNT OF SENDS until 2026-09-11: a person who
// wanted the harness to try harder set it to eight, and what they bought was
// eight identical requests inside a deadline that stopped at ninety seconds
// anyway — a number that multiplied the transport's own and bounded nothing
// (docs/design/recovery/DESIGN.md §7). It is the same intent read the honest
// way now: more patience, which is more TIME, on the one figure that really
// ends a call. Three is three times ninety seconds for a turn, and the default
// of one is exactly the measured behaviour this build already had.
//
// IT IS A PROCESS-WIDE FIGURE AND THERE IS ONE DOOR, because a deadline is
// built in two places that cannot read a profile — `lane.PlanFor` and
// internal/provider's dispatcher — and a setting that reached one of them and
// not the other would be a person told two different things about the same
// wait. internal/session publishes it the moment it resolves the profile's
// limits (taxonomy_boundary.go); until something does, every role is its own
// measured figure, which is the right answer for a process nobody configured.
var patience atomic.Int64

// patienceScale is how [UsePatience] keeps a fraction in an integer: a factor is
// stored as thousandths, so 1.0 is 1000 and a person may say 1.5.
const patienceScale = 1000

// UsePatience publishes the person's own patience factor, floored at one — a
// factor under one is somebody asking this build to give up sooner than the
// measurement says a turn takes, which is not a patience anybody wants and reads
// as the default.
func UsePatience(factor float64) {
	if factor < 1 {
		factor = 1
	}
	patience.Store(int64(factor * patienceScale))
}

// Patience is the factor in force, for a caller that has to SHOW it.
func Patience() float64 { return asked() }

// asked is the factor as the arithmetic wants it, and it is 1 until somebody
// publishes one.
func asked() float64 {
	stored := patience.Load()
	if stored <= patienceScale {
		return 1
	}
	return float64(stored) / patienceScale
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
