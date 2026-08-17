package subharness

// THE PARALLELISM FORCING FUNCTION.
//
// The designer guide has always MANDATED the independence reasoning — step 4 of
// PART TWO says two jobs where neither needs the other's output must not run in
// sequence, and says to name the pairs. It was prose, and prose is advisory: a
// real run on a goal that asked for three approaches to be compared produced ONE
// mega-node and a justification that said, in words, "no parallelization
// opportunity". That was false, and nothing in the rig could tell.
//
// So the pair table stops being a paragraph and becomes DATA. The envelope now
// carries a `derivation`: every pair of planned nodes marked `depends` (naming the
// data that flows) or `independent` (saying why none does). The claim is then
// checked against the drawn edges, and a pair the designer called independent
// while wiring one into the other's ancestry is a validation error like any other
// — fed back through the same retry loop that catches a bad rung or a dangling
// edge, with the pair and the offending path named so the next attempt has
// something to fix rather than something to guess at.
//
// WHAT THIS CAN AND CANNOT SEE. It can see the contradiction between a claim and
// a topology, which is the lie the mega-node run told. It cannot see a job that
// was never named at all — a designer that plans one node has no pairs to
// contradict. The one thread from there to here is the unknown-id check below: a
// table that names the three evaluations it planned and a program that draws one
// node no longer agrees with itself, and that disagreement is reportable.
//
// The check lives beside Validate rather than inside it because cues,
// justification and derivation are all things the PAGE has nowhere to put (see
// the envelope in cmd/harness-design) — Validate takes a Harness, and a Harness
// does not carry the designer's reasoning about it.

import (
	"fmt"
	"strings"
)

// The two words a pair may be marked with. `depends` is "this one cannot begin
// until it has that one's answer"; `independent` is "no data flows between them
// in either direction".
const (
	RelDepends     = "depends"
	RelIndependent = "independent"
)

// Derivation is one pair of planned nodes and the designer's verdict on whether
// data flows between them. A and B are node ids in the program the same envelope
// carries; Why is the one line that makes the verdict an argument rather than an
// assertion — the data that flows, or the reason none does.
type Derivation struct {
	A   string `json:"a"`
	B   string `json:"b"`
	Rel string `json:"rel"`
	Why string `json:"why,omitempty"`
}

// CheckDerivation holds a derivation table to the program it was drawn for.
//
// An ABSENT table is not an error. The check is a new law and a page designed by
// an older prompt has no table to hold; refusing those would fail designs for
// having been made yesterday. A table that IS present is held to every line of
// itself, because a table that can be switched off by a typo in `rel` is not a
// machine check.
//
// A pair NOT in the table is not an error either. The edges are the truth about
// dependency; the table is a claim about the edges, and silence makes no claim.
func CheckDerivation(h Harness, pairs []Derivation) error {
	if len(pairs) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(pairs))
	for index, pair := range pairs {
		a, b := strings.TrimSpace(pair.A), strings.TrimSpace(pair.B)
		if a == "" || b == "" {
			return fmt.Errorf("subharness: %s: derivation pair %d is %q and %q: a pair is two node ids",
				h.Id.Name, index, pair.A, pair.B)
		}
		if a == b {
			return fmt.Errorf("subharness: %s: derivation pairs node %q with itself", h.Id.Name, a)
		}
		// A pair that names a job the program does not contain is the collapse
		// this table exists to catch: three evaluations enumerated in the
		// derivation and one mega-node drawn in the program do not agree, and
		// the disagreement is worth a refusal rather than a shrug.
		for _, id := range []string{a, b} {
			if _, found := h.Program.Node(id); !found {
				return fmt.Errorf("subharness: %s: derivation names %q, which is not a node in the program: every pair is a pair of drawn nodes, so a job you derived and did not draw is either a missing node or a pair that should not be there",
					h.Id.Name, id)
			}
		}
		key := a + "\x00" + b
		if a > b {
			key = b + "\x00" + a
		}
		if seen[key] {
			return fmt.Errorf("subharness: %s: the pair %q and %q is derived twice", h.Id.Name, a, b)
		}
		seen[key] = true

		rel := strings.TrimSpace(pair.Rel)
		if rel != RelDepends && rel != RelIndependent {
			return fmt.Errorf("subharness: %s: the pair %q and %q is marked %q, which is neither %q nor %q",
				h.Id.Name, a, b, pair.Rel, RelDepends, RelIndependent)
		}
		if strings.TrimSpace(pair.Why) == "" {
			return fmt.Errorf("subharness: %s: the pair %q and %q is marked %s with no `why`: %s",
				h.Id.Name, a, b, rel, whyWanted(rel))
		}
		if rel != RelIndependent {
			continue
		}
		// The contradiction itself. `independent` is a claim about BOTH
		// directions, so both are asked — and reachability, not adjacency, is
		// the question: a job three steps downstream of another has that
		// other's answer just as surely as the node next to it.
		if path := pathBetween(h.Program, a, b); path != nil {
			return derivationConflict(h, a, b, path)
		}
		if path := pathBetween(h.Program, b, a); path != nil {
			return derivationConflict(h, a, b, path)
		}
	}
	return nil
}

func whyWanted(rel string) string {
	if rel == RelDepends {
		return "name the data that flows"
	}
	return "say in one line why no data flows"
}

func derivationConflict(h Harness, a, b string, path []string) error {
	return fmt.Errorf("subharness: %s: the pair %q and %q is derived as %s, but the program runs one into the other: %s. Either they are dependent — mark the pair %s and name the data that flows — or they are not, and the sequence has to come apart into lanes",
		h.Id.Name, a, b, RelIndependent, strings.Join(path, "->"), RelDepends)
}

// pathBetween returns the shortest walk from one node to another, ids included at
// both ends, or nil when there is none. It is a breadth-first search in edge
// order, so the path a refusal quotes is the same path on every machine and in
// every decoding of the same page.
//
// Note what is NOT a path: two lanes of the same `parallel.split`. Sibling lanes
// reach their join and nothing reaches back out of it to the other lane, which is
// exactly why a fanned-out set of independent evaluations passes this check while
// the same evaluations drawn in a line does not.
func pathBetween(p Program, from, to string) []string {
	came := map[string]string{from: ""}
	queue := []string{from}
	for len(queue) > 0 {
		at := queue[0]
		queue = queue[1:]
		for _, next := range p.Successors(at) {
			if _, walked := came[next]; walked {
				continue
			}
			came[next] = at
			if next == to {
				path := []string{to}
				for back := at; back != ""; back = came[back] {
					path = append([]string{back}, path...)
				}
				return path
			}
			queue = append(queue, next)
		}
	}
	return nil
}
