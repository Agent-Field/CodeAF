package session

// A GATE IS A QUESTION ON EVERY SURFACE.
//
// ── THE RUN THIS WAS WRITTEN FROM ──
//
// A nested part — task 3, two levels down — landed needing somebody's look. Its
// journal read "no answer in 5m0s, so nothing was accepted". A fifteen-second
// capture of the whole run shows NO ANSWERS ROW FOR THAT NODE, EVER: the gate
// expired without a person ever being able to see it, let alone answer it.
// Meanwhile the roster's footer counted the node under `done` while its parent
// was still working.
//
// ── THE LAW ──
//
// A QUESTION TO A PERSON IS NEVER ON A TIMER. It waits, visibly, on every
// surface, until it is answered — or it is not a question. A timer may expire
// only into a state that says UNANSWERED, never into accepted or rejected. And a
// parent's running is A FOLD, NOT A MUTE: the family head carries the demand,
// and no count ever says done about a node nobody has decided.
//
// ── ONE REGISTRY, MANY RENDERERS ──
//
// This file owns the registry and nothing else renders from anywhere else. It is
// DERIVED rather than stored, and that is deliberate: a second list of which
// nodes are waiting would be a second source of truth for a fact the graph
// already holds, and the two would disagree the first time a node was resolved
// on a road that forgot to remove it. Entry and exit are therefore free — a node
// is on the list exactly while its state is [TaskUnverified], which is exactly
// while nobody has said whether its work holds.
//
// AND IT IS DEPTH-INDEPENDENT, which is the whole defect. A part two levels down
// is a decision somebody has to make in exactly the way a root is; that its
// parent was the one being asked while it ran changes WHO is asked, never
// WHETHER anybody is.

import (
	"strconv"
	"time"
)

// PendingDecision is one node waiting on somebody to say whether its work holds.
//
// IT IS NOT [Decision], and the two are spelled apart on purpose: that one is the
// principal's answer about what a session should do next, and this is a question
// put to a person about work that has already happened.
//
// It carries the node's whole notice rather than a handful of fields lifted out
// of it, because every surface that renders a decision already knows how to draw
// a notice — the card, the roster row, the room's foot — and a second shape
// would be a second thing to keep in step with the first.
type PendingDecision struct {
	// Notice is the node as every other surface already reads it.
	Notice TaskNotice
	// Journal is where the node's transcript is, as a URI a surface can open. It
	// is the one thing a notice does not carry and the one thing somebody being
	// asked to decide almost always wants.
	Journal string
	// Depth is how far down the family this node sits — 0 for a root. It is here
	// so a surface can FOLD a decision under its family head without having to
	// walk the graph itself, and never so that one can be hidden.
	Depth int
}

// Waiting names the person's own words for what this node is doing: it finished,
// and it needs a look. It is a method rather than a field because there is one
// spelling of it in this package ([taskUnverifiedNews]) and a copy on a struct
// would be the second.
func (d PendingDecision) Waiting() string { return taskUnverifiedNews }

// PendingDecisions is every node in this session that finished with nobody able
// to say whether its work holds, in admission order.
//
// IT IS THE ONE LIST, and it is exported because three kinds of caller need it:
// a surface drawing the answers row, the model's own `tasks` tool, and the tests
// that hold this package to the law above. Resolving a node — accept, look
// again, not right, or a late verdict landing — takes it off this list by moving
// its state, and nothing else has to remember to.
func (a *Agent) PendingDecisions() []PendingDecision {
	graph := a.tasker()
	if graph == nil {
		return nil
	}
	graph.mu.Lock()
	defer graph.mu.Unlock()
	var out []PendingDecision
	for _, id := range graph.order {
		node := graph.nodes[id]
		if node == nil || node.state != TaskUnverified {
			continue
		}
		out = append(out, PendingDecision{
			Notice: node.noticeLocked(0),
			// The journal is read off the node DIRECTLY rather than through
			// [TaskNode.journalPath], which takes this same lock: the graph's
			// mutex is not reentrant, and the walk holds it.
			Journal: taskURI(node.journal),
			Depth:   graph.depthLocked(node),
		})
	}
	return out
}

// depthLocked is how far down the family a node sits, walking up by parent with
// the graph held. It is bounded by the number of nodes so a record that somehow
// pointed at its own ancestor cannot spin here.
func (g *TaskGraph) depthLocked(node *TaskNode) int {
	depth := 0
	for up := node; up != nil && up.parent != 0 && depth < len(g.order); depth++ {
		up = g.nodes[up.parent]
	}
	return depth
}

// taskUnverifiedNews is what a node on this list is DOING, in the person's own
// words. The vocabulary law bans the machinery word for this state from anything
// anybody reads, and this is the one spelling of the replacement in the engine.
const taskUnverifiedNews = "needs your look"

// ── re-addressing, which is not re-delivering ───────────────────────────────

// readdressedLead is what the model is told when a parent settles and the
// decisions it was holding become somebody else's.
//
// IT NAMES THE ROW RATHER THAN REPEATING IT. The old line pasted the child's
// whole landing note into the conversation a second time, an hour after the
// first, so a person reading two identical blocks had no way to tell which one
// still needed them — and the note was the ONLY place the demand ever appeared,
// because no surface drew a card for a nested node at all. The card exists now,
// at every depth, so what is owed here is one sentence saying the question has
// changed hands.
func readdressedLead(parent *TaskNode, kids []*TaskNode) string {
	lead := "task " + strconv.FormatUint(parent.id, 10) + " has finished, and "
	if len(kids) == 1 {
		return lead + "the piece of work it handed out that nobody could check — task " +
			strconv.FormatUint(kids[0].id, 10) + ", " + kids[0].title() +
			" — is now waiting on you rather than on it."
	}
	return lead + strconv.Itoa(len(kids)) + " pieces of work it handed out that nobody could check — " +
		namedDecisions(kids) + " — are now waiting on you rather than on it."
}

// namedDecisions lists the nodes by number and name, in the order they were
// admitted, which is the order every other list of them is in.
func namedDecisions(kids []*TaskNode) string {
	names := make([]string, 0, len(kids))
	for _, kid := range kids {
		names = append(names, "task "+strconv.FormatUint(kid.id, 10)+", "+kid.title())
	}
	return namedFew(names, conflictNamesShown)
}

// ── the checker's own clock ─────────────────────────────────────────────────

// auditWindowFor is how long one audit gets, and it is the ONE place that
// question is answered now.
//
// The door's own answer is the product's ([auditDoor.window]): a check that has
// something to run gets the time a run takes, and one that has nothing gets the
// time reading takes. What is added here is a SEAM, nil everywhere but a test —
// the whole of "a person was never able to answer" is a window running out, and
// a test that could only prove it by waiting five real minutes is a test nobody
// runs. It is the clock this package's other testable timers already use in
// spirit (fixstore.go's [fixStore.clock]), turned the one way this one can be:
// the expiry is a context deadline, so what a test moves is the deadline.
func (a *Agent) auditWindowFor(door auditDoor) time.Duration {
	if a.config.auditWindow > 0 {
		return a.config.auditWindow
	}
	window := door.window()
	steward := a.steward()
	if steward == nil {
		return window
	}
	budget := steward.Budget()
	if budget.Wall == 0 {
		return window
	}
	left, _ := budget.Left()
	// AN AUDIT CAN ALWAYS JUDGE FROM READING. A spent wall is therefore floored
	// at the reading deadline instead of handing the checker a closed context
	// that would misreport "nobody could check it" when there was no check time.
	return max(auditReadingDeadline, min(window, left))
}
