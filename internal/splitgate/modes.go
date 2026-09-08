package splitgate

import (
	"os"
	"strings"
)

// Which reading of "is this division real" a run is on.
//
// THE GATE IS OFF UNLESS SOMEBODY PINNED IT ON. It shipped armed, with a
// six-item floor under every division, and issue #418 said the floor folds real
// divisions: a brief that names three lanes over one file counts zero items and
// is refused. Two repairs were proposed and neither was argued to a conclusion,
// so the owner ruled that the choice would be made by a designed experiment —
// four planner arms against four readings of this gate, 273 judged draws on the
// plan door (docs/design/plan-gate-doe/REPORT.md).
//
// THE EXPERIMENT PICKED THE GATE OFF. The front it drew is the stages planner
// with this gate having no say: the same quality as the best armed cell, the
// fewest divisions the tests could not recover (4 of 21 against 12 and 18), and
// the lowest cost. Every armed reading paid for the narrow divisions it refused
// by folding wide ones, and the counting repair helped one planner and hurt
// another — an interaction, not an improvement. So on 2026-09-02 the default
// moved: with nothing pinned, every division the planner drew is kept.
//
// THE COUNT AND THE SIZING STAY REACHABLE AS PINS. The experiment measured
// them, they are not wrong everywhere, and a run that wants the shipped floor
// back should not need a build to get it — but neither is now what a person
// gets without asking.

// GateMode is which reading of "is this division real" the binary is running.
// It is a string rather than an integer because the pin is read by people
// typing it in front of a command, and `AFORGE_SPLITGATE=judgment` says what it
// is doing where `AFORGE_SPLITGATE=3` would not.
type GateMode string

const (
	// ModeOff is the gate having no say: a division stands as it was drawn.
	// It is what an unset pin selects — and what `0`, the rollback switch this
	// gate has always carried, still selects for anybody who spells it out.
	ModeOff GateMode = "0"

	// ModeCount is the gate as it shipped until 2026-09-02: [Items] over the
	// brief against [Floor]. It is what `1` selects, and it is still the right
	// reading for a run that wants a floor under the divisions it pays for.
	ModeCount GateMode = "1"

	// ModeJudgment is #418's other repair — stop reading the brief. The planner
	// has already sized every node and drawn the edges between them, so the
	// gate asks that instead: a division whose work leaves are each a sitting
	// and which owe each other nothing is a real division whatever the brief
	// counted.
	//
	// IT ONLY EVER ADDS KEEPS TO THE COUNT. When the plan's sizing does not say
	// yes — a leaf with no size, a leaf still oversized, or any edge between two
	// leaves — the count decides, exactly as in [ModeCount]. So this pin is the
	// count with a second way to say yes, which is what let the experiment read
	// it as one factor against the count rather than as a different gate.
	ModeJudgment GateMode = "judgment"
)

// Mode reports which reading this process is running.
//
// AN UNRECOGNISED PIN READS AS OFF, WHICH IS THE DEFAULT, and that is a
// deliberate reversal. While the gate was armed by default an unreadable pin
// had to leave a run on the shipped gate, because the danger was a typo
// silently moving somebody onto an experimental arm. Now the danger runs the
// other way: the measured behaviour is the gate having no say, and a typo
// (`AFORGE_SPLITGATE=on`, `AFORGE_SPLITGATE=true`, `AFORGE_SPLITGATE=lanes`
// from the experiment that has since ended) must not quietly put a floor back
// under somebody's divisions. So the two words that arm it are exact, and
// everything else — including nothing at all — is off.
func Mode() GateMode {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("AFORGE_SPLITGATE"))) {
	case string(ModeCount):
		return ModeCount
	case string(ModeJudgment):
		return ModeJudgment
	default:
		return ModeOff
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

	// Items is what the brief counted, whether or not the count decided. It is
	// what a fold is explained by, and it is filled in on a keep as well so a
	// run's log can show what the count was when nothing was made of it.
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
	count := Items(text)
	switch Mode() {
	case ModeOff:
		// The default, and the experiment's answer: the planner drew the
		// division and nothing here has a better reading of it than the planner
		// did. The count is still reported so a log can show what a floor would
		// have made of this division.
		return Decision{Keep: true, Items: count}
	case ModeJudgment:
		if sizedIndependentDivision(leaves) {
			// The plan said yes and the plan is the better witness.
			return Decision{Keep: true, Items: count}
		}
	}
	return Decision{Keep: count >= Floor, Items: count}
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
// [ModeCount] would have done, never take one away.
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
