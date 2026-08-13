package head

import (
	"errors"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/store"
)

// task: one of three verbs, and the only door onto new work.
//
// Three tools used to stand here — spawn, fork, correct — and the difference
// between them was never a difference the person made. "Go and do this", "take
// what we just worked out and go do it", and "that's wrong, do it again" are
// all one intent with different things travelling alongside it: nothing, the
// conversation, or the previous attempt. So they are arguments now, and the
// belt carries one verb for commissioning (chat-simplify §2.3).
//
// What did NOT survive is the orders array. The chat never splits work: a
// message carrying four asks is one ask that enumerates, and the compiler
// already decomposes better than a tool call can, with the whole message in
// hand. The fan-out cap, the dedupe of repeated orders and the collapse
// arithmetic all die with it; genuinely unrelated asks are two task calls, and
// the turn-level guard below still stops the loop commissioning one twice.
//
// Two guards stay exactly where they were, because both are the last place they
// can still be true: the consequence gate at the journaling door — a reflex
// whose words buy, send, publish or delete beyond the workspace is not a
// reflex — and alreadyCommissioned, which is one turn's memory of what it has
// already turned into work. The third guard, the duplicate-ask check against
// work already waiting in the funnel, has MOVED to the store (admission.go), so
// a headless `aforge do` is protected by it too.

// task commissions one piece of work. It returns what the loop is allowed to
// say about it and nothing more: a receipt names commands that actually exist.
func (run *beltRun) task(args map[string]any) (string, bool) {
	instruction := strings.TrimSpace(beltString(args, "instruction"))
	if instruction == "" {
		return "instruction must carry the user's own words for the work, verbatim", true
	}

	// amends and after are the two ways one ask points at another, and they are
	// different relations: amends says "this one was wrong", after says "this one
	// comes next". Both name real work of the person's or the store refuses the
	// row and the receipt would be a promise about nothing.
	amends := strings.TrimSpace(beltString(args, "amends"))
	after := strings.TrimSpace(beltString(args, "after"))
	if amends != "" && after != "" {
		return "a task either amends finished work or follows on from work — not both; pick the one they meant", true
	}

	target, correcting := "", ""
	switch {
	case amends != "":
		node, found, err := run.head.store.Node(amends)
		if err != nil {
			return "that job could not be read: " + err.Error(), true
		}
		if !found || !correctable(node) {
			return "there is no finished work of the user's with id " + quoted(amends) +
				" — amends is for work that already delivered; read again for the id", true
		}
		previous := run.head.jobResult(node)
		if strings.TrimSpace(previous) == "" {
			return surgeryTargetLabel(node) + " recorded nothing, so there is no deliverable to be wrong. " +
				"If they want something new, commission it without amends", true
		}
		// correction.go's doctrine, unchanged: their words lead, the previous
		// version and its files travel underneath, and the user outranks the
		// predecessor's account of itself.
		instruction = SpliceCorrection(instruction, node, previous,
			resultFiles(node, run.head.budget.deepFiles), run.head.budget.correction)
		target, correcting = node.ID, surgeryTargetLabel(node)
	case after != "":
		node, err := run.head.beltRecordedJob(after, "after")
		if err != nil {
			return err.Error(), true
		}
		target = node.ID
	}

	// The conversation travels (fork.go). It is fenced and labelled as context
	// rather than pasted in as more instruction, because the goal is what they
	// asked for and the transcript is only evidence about what they meant.
	if beltBool(args, "context") && correcting == "" {
		context, err := run.head.forkContext(run.user)
		if err != nil {
			return "the conversation could not be read: " + err.Error(), true
		}
		if context == "" {
			return "there is nothing discussed in this conversation yet for the work to inherit — " +
				"commission it without context", true
		}
		instruction = ForkedInstruction(instruction, context)
	}

	// The model words, read here and resolved where the catalog is (modelwords.go).
	// The head cannot know which models exist; what it can do is write down what
	// was asked for, on one marked line, so the tasker resolves the same reading
	// every route produces.
	instruction = MarkTaskModel(instruction, beltString(args, "model"))

	if run.alreadyCommissioned(instruction) {
		// Not a failure and not silence: the work exists, this turn made it, and
		// the loop needs to know it must not say it twice either.
		return "that is already in hand from this turn — it was commissioned a moment ago and nothing more was queued; " +
			"say what is happening, do not commission it again", false
	}

	reflex := beltBool(args, "reflex")
	if reflex && (consequenceGated(instruction) || target != "") {
		// A reflex is untargeted by definition and never consequential. The
		// honest repair is the ordinary route, which the person sees coming.
		reflex = false
	}

	command, err := run.head.store.RequestCommand(store.Command{
		SessionID: run.user.SessionID,
		Kind:      store.CommandSplice,
		Reflex:    reflex,
		Fresh:     beltBool(args, "fresh"),
		// separate is the person's own override on admission control and never
		// the model's convenience: it may be set when they have said, in so many
		// words, that this is a job beside the running one.
		Deliberate:  beltBool(args, "separate"),
		Target:      target,
		Instruction: instruction,
		Attachments: append([]string(nil), run.user.Attachments...),
	})
	if err != nil {
		if errors.Is(err, store.ErrDuplicate) {
			return "that is already queued from an earlier ask and is still waiting for the workforce — " +
				"nothing new was commissioned; say it is queued and will start, do not commission it again. " +
				"If they truly want a second one beside it, task takes separate=true", false
		}
		return "that could not be queued: " + err.Error(), true
	}

	// Prose turned into work is never a silent side effect. The receipt is
	// recorded from what was journaled, so a turn whose words fail still says out
	// loud what it commissioned.
	switch {
	case correcting != "":
		run.record(command.Seq, "Taking that back to "+correcting+
			" — redoing it with the previous version and the correction in hand.")
		return "queued as a revision of " + correcting +
			": it goes again with its previous version and those words, and keeps everything they did not object to", false
	case target != "":
		run.record(command.Seq, "Queued: "+truncateBytes(firstLine(instruction), taskReceiptBytes)+".")
		return "queued as work that follows on from " + target +
			"; it starts when the workforce reaches it", false
	case reflex:
		run.record(command.Seq, "Queued: "+truncateBytes(firstLine(instruction), taskReceiptBytes)+".")
		return "queued as one immediate action; it runs as soon as the workforce reaches it", false
	}
	run.record(command.Seq, "Queued: "+truncateBytes(firstLine(instruction), taskReceiptBytes)+".")
	return "queued; it is compiled, planned and started when the workforce reaches it", false
}

// taskReceiptBytes keeps one ask's words to a clause. The receipt names what
// was commissioned; the work itself carries the whole sentence.
const taskReceiptBytes = 80

func quoted(value string) string { return "\"" + value + "\"" }
