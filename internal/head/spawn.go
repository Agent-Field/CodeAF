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

	// The commission-or-amend door (amend.go). It runs before anything is
	// journaled and before the fan-out arithmetic, because what it decides is
	// whether there is a new job here at all — and it is asked of the PERSON'S
	// words rather than of the model's paraphrase, since reference to work in
	// flight is a property of what they said.
	//
	// separate is the person's own override and never the model's convenience:
	// it may be set when they have said, in so many words, that this is a job
	// beside the running one. The tool description says exactly that.
	if !beltBool(args, "separate") {
		if change, amending, err := run.head.amendmentFor(run.user); err == nil && amending {
			return run.amendInstead(change)
		}
	}

	reflex := beltBool(args, "reflex")
	// The opt-out from learned know-how. It is a reading of what a sentence MEANT
	// — "don't use the template this time", "plan this one properly", "start over
	// on this" are all the same intent and no phrase list will hold them — and it
	// has to be made here or it is made by a list somewhere downstream that has
	// to be taught every spelling. The field has existed on the command, migrated,
	// replayed and been consumed by the craft mind all along; between the router's
	// death and this argument, nothing a person could say reached it.
	fresh := beltBool(args, "fresh")
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

	// The turn guard only remembers ONE turn, and the workforce may take a
	// while to reach a splice — so "get me a list of X" followed a minute later
	// by "ok start it" journaled a second identical job while the first sat
	// pending (user-reported, 2026-08-11: two "20 best stocks" runs, then a
	// "queued" claim over an already-queued job). Pending splices for this
	// session are read once here so the loop can be told the work already
	// exists instead of minting a twin.
	pending := run.pendingSplices()
	journaled := make([]string, 0, len(orders))
	repeated, waiting := 0, 0
	var first int64
	for _, instruction := range orders {
		// The same sentence twice in one turn is one job. dedupeOrders catches it
		// inside a single call; this catches the loop calling spawn again with
		// words it has already commissioned, which is the only way one submission
		// could ever have produced two identical jobs — two compiles, two plans,
		// and the person reading the same reading of their request twice.
		if run.alreadyCommissioned(instruction) {
			repeated++
			continue
		}
		if pendingTwin(instruction, pending) {
			waiting++
			continue
		}
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
			Fresh:       fresh,
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
		if repeated > 0 {
			// Not a failure and not silence: the work exists, this turn made it,
			// and the loop needs to know it must not say it twice either.
			return "that is already in hand from this turn — it was commissioned a moment ago and nothing more was queued; say what is happening, do not commission it again", false
		}
		if waiting > 0 {
			return "that is already queued from an earlier ask and is still waiting for the workforce — nothing new was commissioned; say it is queued and will start, do not commission it again", false
		}
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

// pendingSplices is this session's commissioned-but-not-yet-consumed work, read
// once per spawn call. The window it guards is real: a splice sits pending for
// as long as the workforce takes to reach it, and every turn inside that window
// is a turn that could honestly believe the work does not exist yet.
func (run *beltRun) pendingSplices() []store.Command {
	all, err := run.head.store.PendingCommands(pendingSpliceRead)
	if err != nil {
		return nil
	}
	mine := all[:0]
	for _, command := range all {
		if command.Kind == store.CommandSplice && command.SessionID == run.user.SessionID {
			mine = append(mine, command)
		}
	}
	return mine
}

// pendingSpliceRead bounds the pending read. A session with more than this many
// splices waiting has a stalled workforce, and the guard degrades to catching
// the newest of them — which is the one a re-ask would twin.
const pendingSpliceRead = 32

// pendingTwin reports that an instruction is the same ask as a splice already
// waiting. Exact words are too strict across turns — the loop re-reads the
// request each time — so the test is shared content words: most of the shorter
// sentence's meaningful words appearing in the other. The bar is deliberately
// high (two thirds, and at least three shared words) because a false twin
// silently swallows a genuinely new job, which is worse than the duplicate it
// exists to prevent — a duplicate at least shows up on the board.
func pendingTwin(instruction string, pending []store.Command) bool {
	words := spawnContentWords(instruction)
	if len(words) < 3 {
		return false
	}
	for _, command := range pending {
		theirs := spawnContentWords(command.Instruction)
		if len(theirs) < 3 {
			continue
		}
		shorter, longer := words, theirs
		if len(theirs) < len(words) {
			shorter, longer = theirs, words
		}
		shared := 0
		for word := range shorter {
			if longer[word] {
				shared++
			}
		}
		if shared >= 3 && shared*3 >= len(shorter)*2 {
			return true
		}
	}
	return false
}

// spawnContentWords is the sentence as a set of meaningful lowercased words —
// what survives when the connective tissue is taken out.
func spawnContentWords(sentence string) map[string]bool {
	words := make(map[string]bool)
	for _, word := range strings.FieldsFunc(strings.ToLower(sentence), func(r rune) bool {
		return !('a' <= r && r <= 'z' || '0' <= r && r <= '9')
	}) {
		if len(word) < 3 || spawnStopWords[word] {
			continue
		}
		words[word] = true
	}
	return words
}

var spawnStopWords = map[string]bool{
	"the": true, "and": true, "for": true, "that": true, "this": true,
	"with": true, "have": true, "from": true, "please": true, "you": true,
	"can": true, "get": true, "our": true, "are": true, "was": true,
	"will": true, "would": true, "should": true, "into": true, "out": true,
	"all": true, "any": true, "its": true, "then": true, "them": true,
}

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
