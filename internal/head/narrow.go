package head

import (
	"strings"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// The belt narrows; it never empties.
//
// THE INCIDENT. A person asked for the figures in a paper. The head opened the
// images that already existed, was told "no, from the paper itself", and went to
// work: render the pages, find the caption boxes, prepare the crops. Sixteen
// calls in, mid-campaign, it reached the runaway bound — and the bound took away
// every hand at once. What was left could only speak, so it said the belt was
// spent and that it would finish next turn. It had no next turn: the turn is the
// only thing the head owns, and a promise made about the one after it is a
// promise made by nobody. The person had to type "use and submit it as a task
// then" themselves, which is the product asking its owner to operate it.
//
// THE FIX IS THE BELT, NOT A RULE. A prompt line saying "when you run out of
// calls, submit a task" is case law: it works on the case we saw and on nothing
// else, and this package spent a year removing exactly that kind of sentence.
// So the bound changes what the head is HOLDING rather than what it is told. At
// the cap the next completion is armed with the work verbs alone — the hands
// that give work away, not the hands that do it — and one round later the turn
// ends whatever it did with them. The economics do the judging: a head with
// unfinished work and only `task` in its hand reaches for `task`, and a head
// that finished simply answers. Neither of those is a case we enumerated.
//
// The bound still bounds. One narrowed round is one round: it cannot read, it
// cannot run anything, and the turn ends after it whether it produced a receipt
// or nothing at all.

// beltWorkVerb names the narrowed belt: the two verbs that hand work over.
//
// task gives unfinished work to the workforce and stop withdraws it, and those
// are the only two honest endings for a turn that ran out of room to work. The
// third verb of the triad is deliberately not here: change relays the PERSON's
// words to work already under way, and a turn holding words of theirs to relay
// would have relayed them with its first call rather than its seventeenth.
//
// It is written as a predicate over names, like beltReadOnly above it, so the
// guard in the loop and the definitions handed to the model are the same
// statement read twice rather than two lists to keep in step.
func beltWorkVerb(name string) bool {
	switch strings.TrimSpace(name) {
	case beltToolTask, beltToolStop:
		return true
	}
	return false
}

// beltWorkDefinitions is the narrowed belt as definitions, filtered from the one
// list for the same reason the reads are (toolbelt.go): there is no second
// account of what these tools are that could disagree with the first.
func beltWorkDefinitions() []ai.ToolDefinition {
	whole := beltDefinitions()
	verbs := make([]ai.ToolDefinition, 0, 2)
	for _, definition := range whole {
		if beltWorkVerb(definition.Function.Name) {
			verbs = append(verbs, definition)
		}
	}
	return verbs
}

const (
	// turnFindingBytes keeps one discovery to a paragraph. It is larger than an
	// inherited conversation turn because a tool result is the evidence itself
	// rather than a line somebody said about it, and smaller than the read it
	// came from because the work is going to look again with its own hands.
	turnFindingBytes = 600
	// turnFindingsBytes bounds the whole block, in the fork context's league:
	// this rides in the same column and a brief that is mostly transcript has
	// stopped being one either way.
	turnFindingsBytes = 3 << 10
	// turnFindingsHeader opens the block. It says what these lines ARE — already
	// established, by the conversation, before the work was handed over — because
	// a worker that reads them as instructions has read them wrong, exactly as
	// the fence above the inherited conversation exists to prevent.
	turnFindingsHeader = "what was already found out about this before it was handed over, and need not be found out again:"
)

// learned records one tool result as something this turn found out.
//
// The label is the activity gloss (activity.go) rather than the call's
// arguments: it is the one rendering of "what this call was" already written in
// a person's words, and the worker reading the handoff is no more entitled to a
// JSON object than the person watching was.
func (run *beltRun) learned(name, arguments, result string) {
	result = strings.TrimSpace(promptSafe(result))
	if run == nil || result == "" {
		return
	}
	// A tool result is a page and a finding is one item of a list, so every line
	// after the first sits under its own bullet instead of reading as a new one.
	body := strings.ReplaceAll(truncateBytes(result, turnFindingBytes), "\n", "\n  ")
	run.found = append(run.found, "- "+toolGloss(name, arguments)+": "+body)
}

// findings renders what the turn found out, newest kept first.
//
// The walk is backwards and the render is forwards: when the block will not hold
// everything, what survives is the END of the investigation — the reads that
// were still open when the belt narrowed — rather than the opening moves, which
// are the ones the work would repeat cheapest.
func (run *beltRun) findings() string {
	if run == nil || len(run.found) == 0 {
		return ""
	}
	kept, size := 0, len(turnFindingsHeader)
	for index := len(run.found) - 1; index >= 0; index-- {
		size += len(run.found[index]) + 1
		if size > turnFindingsBytes && kept > 0 {
			break
		}
		kept++
	}
	return turnFindingsHeader + "\n" + strings.Join(run.found[len(run.found)-kept:], "\n")
}

// joinContext puts two blocks in one context column. Either half may be empty —
// a turn can narrow before anything was discussed, and a task can inherit a
// conversation without a single tool having run.
func joinContext(first, second string) string {
	switch {
	case strings.TrimSpace(first) == "":
		return strings.TrimSpace(second)
	case strings.TrimSpace(second) == "":
		return strings.TrimSpace(first)
	}
	return strings.TrimSpace(first) + "\n\n" + strings.TrimSpace(second)
}
