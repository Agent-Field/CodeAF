package splitgate

import (
	"os"
	"strings"
)

// The gate's answer, under experiment.
//
// THE MODE IS A PIN, SO THE EXPERIMENT CAN ASK EACH ONE. Issue #418 says the
// shipped gate folds a real division because its reading of the brief is a
// count of digits beside plurals, and a brief that names three lanes over one
// file counts zero. Two repairs were proposed and neither was argued to a
// conclusion: count better, or stop counting and ask the plan. The owner ruled
// that the choice is made by a designed experiment — a factorial of planner arm
// against gate mode — rather than by whichever repair was written down first.
// So all of them live here at once, one binary runs any of them, and the pin
// that has always been this gate's rollback switch is what picks.
//
// THIS FILE IS SCAFFOLDING WITH A KNOWN END. When the experiment names a
// winner, that mode becomes the gate, the selector goes away, and
// AFORGE_SPLITGATE goes back to meaning nothing but `0`.
//
// THE DEFAULT DOES NOT MOVE. With the pin unset or set to anything this file
// does not recognise, every answer in this package is the one the shipped
// binary gives, down to the wording of the sentence written into a folded
// node — [TestTheUnpinnedBinaryDecidesExactlyAsItShipped] holds it there.

// GateMode is which reading of "is this division real" the binary is running.
// It is a string rather than an integer because the pin is read by people
// typing it in front of a command, and `AFORGE_SPLITGATE=judgment` says what it
// is doing where `AFORGE_SPLITGATE=3` would not.
type GateMode string

const (
	// ModeCount is the gate as shipped: [Items] over the brief against
	// [Floor]. It is what "" and "1" and any unrecognised word select, because
	// an unreadable pin must not silently change the arm a run is on.
	ModeCount GateMode = "1"

	// ModeOff is the rollback switch that has always been here: the gate has no
	// say and a division stands as drawn.
	ModeOff GateMode = "0"

	// ModeLanes is #418's cheaper repair — count better. The brief is still all
	// the gate reads, but [Lanes] also reads the shapes people write a division
	// down in: labelled lanes, a numbered or bulleted list, a spelled-out
	// number beside a plural, a list of distinct file paths.
	//
	// IT MOVES THE COUNT AND NOT THE FLOOR, which means a brief naming three
	// lanes still folds: three is under six. That is the mode honestly stated
	// rather than a hole in it — the count was wrong about "thirty chapters"
	// and about a list of twelve files, and this fixes those; it does not claim
	// that naming your parts is by itself enough to buy a worker apiece.
	ModeLanes GateMode = "lanes"

	// ModeJudgment is #418's recommended repair — stop reading the brief. The
	// planner has already sized every node and drawn the edges between them, so
	// the gate asks that instead: a division whose work leaves are each a
	// sitting and which owe each other nothing is a real division whatever the
	// brief counted.
	//
	// IT ONLY EVER ADDS KEEPS. When the plan's sizing does not say yes — a leaf
	// with no size, a leaf still oversized, or any edge between two leaves —
	// the count decides, exactly as in [ModeCount]. So this mode differs from
	// the shipped one in one direction only, which is what makes it readable as
	// a factor in the experiment rather than as a different gate.
	ModeJudgment GateMode = "judgment"
)

// Mode reports which reading this process is running.
//
// ONE SPELLING OF THE ESCAPE HATCH, STILL. "0" and only "0" turns the gate off,
// because that is what the switch has always meant and a second spelling would
// be a rollback somebody thought they had taken. Everything the list below does
// not name reads as [ModeCount] for the same reason in reverse: a typo must not
// quietly move a run onto an experimental arm.
func Mode() GateMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AFORGE_SPLITGATE"))) {
	case "0":
		return ModeOff
	case string(ModeLanes):
		return ModeLanes
	case string(ModeJudgment):
		return ModeJudgment
	default:
		return ModeCount
	}
}

// Leaf is the whole of what this gate needs to know about one planned work
// node: how big the planner made it and what it waits on.
//
// It is a plain struct and not internal/plan's own node because this package
// has no dependency on either product that asks it, and acquiring one to read
// two fields would put the counting behind the planner's import graph — the
// same reason the counting lives here at all.
type Leaf struct {
	// ID is the leaf's identity in its own graph, which is what the other
	// leaves' Needs are written in terms of.
	ID int

	// Size is the planner's sizing word — "atomic", "borderline", "oversized" —
	// or empty where nothing sized this node. Empty is a different fact from
	// oversized and the gate treats it as one: unsized means the plan has no
	// opinion, and an opinion is what [ModeJudgment] came for.
	Size string

	// Needs are the IDs this leaf waits on. They may name nodes that are not
	// leaves at all, which is why independence is asked only about the pairs
	// inside the set handed over.
	Needs []int
}

// SizeOversized is the planner's word for work that is still too big to be one
// sitting (internal/plan's SizeOversized, spelled here rather than imported).
const SizeOversized = "oversized"

// Decision is what the gate answers and what a caller writes down about it.
type Decision struct {
	// Keep is the answer: true leaves the division standing.
	Keep bool

	// Items is the reading that decided, in whichever counting the mode uses.
	// It is what a fold is explained by, and it is filled in even on a keep so
	// a run's log can show what the count was when the sizing overruled it.
	Items int
}

// Judge answers the one question this package exists for: does this division
// stand? It is asked at the plan door with the planned leaves and at a running
// leaf's own request with none, and the mode is read once, here.
//
// Leaves may be nil, and nil is not "a graph with no parts" — it is "nobody
// can tell me about the parts", which is the honest situation at a leaf's own
// division request, where all that exists is the evidence it wrote down. A
// caller holding a graph passes its work leaves; a caller holding only text
// passes nil and gets the count.
func Judge(text string, leaves []Leaf) Decision {
	mode := Mode()
	if mode == ModeOff {
		return Decision{Keep: true, Items: Items(text)}
	}
	if mode == ModeJudgment && sizedIndependentDivision(leaves) {
		// The plan said yes and the plan is the better witness. The count is
		// still reported, because the interesting rows of the experiment are
		// exactly the ones where these two disagree.
		return Decision{Keep: true, Items: Items(text)}
	}
	count := Count(text)
	return Decision{Keep: count >= Floor, Items: count}
}

// Count is the item reading the current mode goes by. [ModeLanes] reads the
// shapes a division is written down in as well as the digits; every other mode
// reads what the shipped binary reads, [ModeJudgment] included — its repair is
// the sizing, and giving it a second counter as well would make the experiment
// unable to say which of the two moved a result.
func Count(text string) int {
	if Mode() == ModeLanes {
		return Lanes(text)
	}
	return Items(text)
}

// sizedIndependentDivision reports whether the plan itself says these parts are
// a real division: two or more of them, each one sized and none of them still
// oversized, and no edge between any two.
//
// AN OVERSIZED LEAF IS NEVER A REASON TO KEEP A DIVISION. A leaf the planner
// could not get down to a sitting is the very shape #384 is about, and a gate
// that read "the planner drew several parts" as evidence would be keeping
// divisions on the strength of the planner having failed to finish sizing them.
// It is not a reason to fold either — the count still gets its say — it is
// simply not a yes.
//
// A STRICT CHAIN IS NOT INDEPENDENT, and falls through to the count rather than
// being refused outright. Three atomic nodes that each wait on the one before
// are one sitting split across three workers, two of whom sit idle; internal/plan
// already folds that shape itself (collapseAtomicChain). Falling through rather
// than refusing keeps this mode's one guarantee: it can only add keeps to what
// the shipped gate would have done, never take one away.
func sizedIndependentDivision(leaves []Leaf) bool {
	if len(leaves) < 2 {
		return false
	}
	inSet := make(map[int]bool, len(leaves))
	for _, leaf := range leaves {
		inSet[leaf.ID] = true
	}
	for _, leaf := range leaves {
		if leaf.Size == "" || leaf.Size == SizeOversized {
			return false
		}
		for _, need := range leaf.Needs {
			if need != leaf.ID && inSet[need] {
				return false
			}
		}
	}
	return true
}
