package effort

// ── the resolver ────────────────────────────────────────────────────────────
//
// ONE LADDER, ONE RESOLVER, ONE PLACE EFFORT COMES FROM.
//
// Every model call in this process asks [Resolve] how hard to think, and a
// spawn site that decides for itself is a defect however sensible its guess is.
// The reason is not tidiness: a person who dials a rung expects it to be the
// rung, and a call that quietly kept its own answer is a knob that does nothing
// with no way to tell from the outside.

// Role is what the call is FOR, and it is the rung of last resort: the answer
// when nobody has said anything more specific.
//
// It exists because the honest default is not one number. A person's turn and
// the work they handed out deserve the depth they configured; the machinery that
// runs while nobody is watching does not, and a sentinel deliberating for a
// minute over "has CI gone red" is money spent on a yes-or-no.
type Role string

const (
	// RoleChat is a person's own turn in the conversation.
	RoleChat Role = "chat"

	// RoleWorker is a task, a part of a divided task, or an adaptive-run node —
	// work the person handed out, which is their work at one remove and gets
	// their depth.
	RoleWorker Role = "worker"

	// RoleErrand is the session's own housekeeping: naming a conversation,
	// summarising it, judging a route. THE PERSON'S DIAL IS NOT SPENT ON THESE
	// and neither is a default — a rung somebody set so their question would be
	// thought about would be an odd thing to spend on titling it.
	RoleErrand Role = "errand"

	// RoleStanding is one firing of a standing item, running unattended.
	RoleStanding Role = "standing"

	// RoleSentinel is the yes-or-no in front of a firing: has the thing the
	// person asked about happened? It is a judgment on evidence already
	// gathered, it runs on every check of every item forever, and it is the one
	// call here where deliberation buys nothing.
	RoleSentinel Role = "sentinel"
)

// roleFloor is the rung a role falls back to when nothing above it was set.
//
// A ROLE MISSING FROM THIS MAP FALLS THROUGH TO THE INSTALL'S DEFAULT, which is
// the whole difference between the two halves of the table: chat and worker are
// absent on purpose, because their answer is whatever the person configured, and
// the three that are present are present because their answer is NOT.
var roleFloor = map[Role]Rung{
	RoleErrand:   None,
	RoleStanding: Low,
	RoleSentinel: Low,
}

// Scope is everything that has an opinion about one call's depth, most specific
// first. Every field may be [None], which means "this scope said nothing".
type Scope struct {
	// Turn is a rung for this one call and nothing after it. In the
	// conversation it is the level dialled onto the model now in use, re-read at
	// the top of every turn — which is why a change made mid-turn lands on the
	// next one and never half-way through the one in flight.
	Turn Rung

	// Conversation is the rung this session was set to, sticky across restarts
	// (session.Meta's `effort`).
	Conversation Rung

	// Task is the rung set on the piece of work this call belongs to
	// (the task checkpoint's `effort`, a standing item's `does.effort`).
	Task Rung

	// Role is what the call is for. It decides nothing when a scope above it
	// spoke; it decides everything when none did.
	Role Role

	// Default is the install's own rung — the `effort` settings row, [Ship] when
	// nobody has chosen. It is last because it is the answer to "and otherwise?".
	Default Rung
}

// Resolve is the whole precedence rule, and it is short on purpose: turn beats
// conversation beats task beats role beats the install's default.
//
// TASK SITS BELOW CONVERSATION and not above it, which is the one ordering
// somebody will want to argue with. The reason is where each one is set from: a
// conversation rung is a person leaning on the dial in front of them right now,
// and a task rung is a decision made when the work was handed out, possibly days
// ago and possibly by the model. The nearer hand wins.
func Resolve(scope Scope) Rung {
	for _, said := range []Rung{scope.Turn, scope.Conversation, scope.Task} {
		if said.Valid() {
			return said
		}
	}
	if floor, stated := roleFloor[scope.Role]; stated {
		return floor
	}
	if scope.Default.Valid() {
		return scope.Default
	}
	return None
}
