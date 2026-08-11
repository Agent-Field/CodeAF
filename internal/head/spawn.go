package head

import (
	"fmt"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// spawn: the tool that ends the split brain.
//
// "work issues 12, 41, 77 and 93" was one job for as long as the head could emit
// one command per message. Four issues became four leaves of one subtree, and
// everything downstream inherited that shape: a redirect could only aim at the
// whole thing, the board carried one card, one deliverable came back, and the
// person's mental model — four jobs — never reconverged with the graph's. The
// fix was the head journaling N splices rather than the store learning a
// multi-root one, which keeps the invariant that one command means one subtree
// and one root, and it is unchanged here.
//
// What IS changed is where the judgment sits. It used to be a field on a router
// decision, made once, terminally, by a call that could not read anything first;
// it is now an argument to a tool the loop may call after reading the board.
// Part 6 decision 2 requires the two guards that made the terminal position safe
// to move with it, and both are here rather than in a caller:
//
//   - the fan-out cap of six. Past it the message is not a handful of asks, it
//     is a list — and a list is one job that enumerates, which is what the
//     compiler already does well. Falling back to a single order loses nothing:
//     every item is still in the words that travel.
//   - the consequence gate. A reflex whose words buy, send, publish or delete
//     beyond the workspace is not a reflex; it is ordinary work a person gets to
//     see coming. The gate is applied at the journaling door — the last place it
//     can still be true — rather than trusted from wherever the flag was set.

// spawn commissions new work. It returns what the loop is allowed to say about
// it and nothing more: a receipt names commands that actually exist.
func (run *beltRun) spawn(args map[string]any) (string, bool) {
	orders := beltStrings(args, "orders")
	single := strings.TrimSpace(beltString(args, "instruction"))
	if len(orders) == 0 && single == "" {
		return "instruction must carry the user's own words for the work, verbatim", true
	}

	reflex := beltBool(args, "reflex")
	after := strings.TrimSpace(beltString(args, "after"))
	if after != "" {
		// Target on a splice means "the prior work this one continues", which is
		// how a promoted reflex hands its partial forward. It has to be real work
		// of the person's, or the store refuses the row and the receipt would be a
		// promise about nothing.
		node, err := run.head.beltRecordedJob(after, "after")
		if err != nil {
			return err.Error(), true
		}
		after = node.ID
	}

	// One ask or several. The judgment of independence is the model's, made with
	// the board in hand; the arithmetic of what that judgment costs is here.
	if len(orders) == 0 {
		orders = []string{single}
	} else if single != "" && len(orders) == 1 {
		orders = []string{strings.TrimSpace(orders[0])}
	}
	orders = dedupeOrders(orders)
	if len(orders) == 0 {
		return "instruction must carry the user's own words for the work, verbatim", true
	}
	if len(orders) > fanOutLimit {
		// A list is one job that enumerates. The whole message travels, so nothing
		// the person said is lost by collapsing it.
		whole := single
		if whole == "" {
			whole = strings.TrimSpace(run.user.Body)
		}
		orders = []string{whole}
	}
	// Independent pieces of work are independent: continuity and reflex are both
	// claims about ONE ask, and neither survives a fan-out.
	if len(orders) > 1 {
		reflex, after = false, ""
	}

	journaled := make([]string, 0, len(orders))
	var first int64
	for _, instruction := range orders {
		asReflex := reflex
		if asReflex && (consequenceGated(instruction) || after != "") {
			// A reflex is untargeted by definition and never consequential. The
			// honest repair is the ordinary route, which the person sees coming.
			asReflex = false
		}
		command, err := run.head.store.RequestCommand(store.Command{
			SessionID:   run.user.SessionID,
			Kind:        store.CommandSplice,
			Reflex:      asReflex,
			Target:      after,
			Instruction: instruction,
			Attachments: append([]string(nil), run.user.Attachments...),
		})
		if err != nil {
			continue
		}
		if first == 0 {
			first = command.Seq
		}
		journaled = append(journaled, instruction)
	}
	if len(journaled) == 0 {
		return "none of that could be queued", true
	}

	// 5.20.1: prose turned into work is never a silent side effect. The receipt
	// is recorded here, from what was journaled, so a turn whose words fail still
	// says out loud what it commissioned.
	receipt := "Queued: " + truncateBytes(firstLine(journaled[0]), spawnReceiptBytes) + "."
	if len(journaled) > 1 {
		receipt = fmt.Sprintf("Queued %d separate pieces of work: %s.", len(journaled),
			strings.Join(spawnLabels(journaled), "; "))
	}
	run.record(first, receipt)
	if len(journaled) == 1 {
		if reflex && !consequenceGated(journaled[0]) && after == "" {
			return "queued as one immediate action; it runs as soon as the workforce reaches it", false
		}
		if after != "" {
			return "queued as work that follows on from " + after +
				"; it starts when the workforce reaches it", false
		}
		return "queued; it is compiled, planned and started when the workforce reaches it", false
	}
	return fmt.Sprintf("queued %d separate pieces of work, each with its own plan and its own deliverable", len(journaled)), false
}

// spawnReceiptBytes keeps one order's words to a clause. The receipt names what
// was commissioned; the work itself carries the whole sentence.
const spawnReceiptBytes = 80

func spawnLabels(orders []string) []string {
	labels := make([]string, 0, len(orders))
	for _, order := range orders {
		labels = append(labels, truncateBytes(firstLine(order), spawnReceiptBytes))
	}
	return labels
}

// dedupeOrders drops the empties and the repeats. A model that lists the same
// ask twice has described one piece of work, and journaling it twice buys two
// plans, two prices and two deliverables for it.
func dedupeOrders(orders []string) []string {
	kept := make([]string, 0, len(orders))
	seen := make(map[string]bool, len(orders))
	for _, order := range orders {
		order = strings.TrimSpace(order)
		if order == "" || seen[order] {
			continue
		}
		seen[order] = true
		kept = append(kept, order)
	}
	return kept
}
