package head

import (
	"github.com/Agent-Field/aforge-v2/internal/store"
)

// "work issues 12, 41, 77 and 93" was one job for as long as the head could only
// emit one command per message. Four issues became four leaves of one subtree,
// and everything downstream inherited that shape: a redirect could only ever aim
// at the whole thing, the board carried one card, one deliverable came back, and
// the person's mental model — four jobs — never reconverged with the graph's.
//
// The fix has two candidate seams and only one of them keeps the journal honest.
// The store could accept a multi-root splice; then one command would produce four
// roots, and every read that walks from a node back to the command that bought it
// would have to learn that a command owns a set. The head can instead journal one
// splice per piece of work. Nothing about the store changes, the invariant one
// command means one subtree and one root survives untouched, each order compiles
// on its own — its own goal, its own plan, its own price, its own card, its own
// deliverable — and the replay of the journal produces exactly the graph the
// person was picturing. So the head journals N, and the store keeps its rule.
//
// The judgment of what is independent is the model's, made in the routing call
// that was already happening, on the same decision object. It is deliberately not
// a rule about commas: "flights, a hotel and somewhere to eat in Lisbon" has more
// commas than the four issues do and is one plan with one thing to hand back.

// spliceWorkOrders journals one splice per piece of work and answers once. The
// reply is the receipt the router wrote for the whole message, because that is
// what the person said — one sentence — and four receipts for one sentence is
// three messages nobody asked for.
//
// Continuity is deliberately not applied here. A splice with no target inherits
// the adjacent job when it is a single ask arriving mid-flight, on the argument
// that new work typed while a job is mid-sentence is usually about that job.
// Work the model has just certified as several independent pieces is the one
// case that argument does not cover.
func (h *Head) spliceWorkOrders(user store.Message, decision routeDecision) error {
	first := int64(0)
	journaled := 0
	for _, order := range decision.Commands {
		command, err := h.store.RequestCommand(store.Command{
			SessionID:   user.SessionID,
			Kind:        store.CommandSplice,
			Instruction: order.Instruction,
			Attachments: append([]string(nil), user.Attachments...),
		})
		if err != nil {
			continue
		}
		journaled++
		if first == 0 {
			first = command.Seq
		}
	}
	if journaled == 0 {
		return h.postAgent(user.SessionID, commandErrorReply, 0)
	}
	return h.postAgentFloor(user.SessionID, decision.Reply, first, decision.model, decision.parts())
}
