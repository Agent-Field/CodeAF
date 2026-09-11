package main

import (
	"sort"
	"strings"
	"time"
)

// ── WHAT A RETRY CHAIN IS, AND WHY IT IS RECONSTRUCTED RATHER THAN READ ──────
//
// One `id` is one ATTEMPT: a retry gets a fresh id and an incremented attempt,
// so nothing in the file says "these six rows were one question". The chain has
// to be rebuilt, and the design (§8) says how: rows that share a tag, a node and
// a model, whose attempt number rises, within fifteen minutes of each other.
//
// IT IS THE READING THE WHOLE RECOVERY DESIGN TURNS ON. A chain that never left
// the machine it started on is a retry that meant "again" rather than
// "differently", and the first census found that was true of more than half of
// them — three of which spent sixteen and seventeen consecutive sends on one
// rate-limited queue over eleven minutes and still ended refused.

// chainGap is how far apart two attempts may be and still be one question. A
// person who waited a quarter of an hour and asked again is asking again, not
// being retried.
const chainGap = 15 * time.Minute

// chain is one question's attempts, in the order they went out.
type chain struct {
	tag   string
	node  string
	model string
	rows  []row
}

// began and ended are the window the chain occupied.
func (c chain) began() time.Time { return c.rows[0].at }
func (c chain) ended() time.Time { return c.rows[len(c.rows)-1].at }

func (c chain) duration() time.Duration {
	if len(c.rows) < 2 {
		return 0
	}
	return c.ended().Sub(c.began())
}

// machines is every machine this chain was served by or asked for, in order and
// without immediate repeats — which is what makes a chain that moved readable
// beside one that did not.
//
// SERVED FIRST AND THE DEMAND SECOND. `lane` is who the preference asked for
// and `served` is who answered, and on a third of this log's rows they
// disagree; a chain read from the demand alone reports a walk that never
// happened (DESIGN.md §1's third reading).
func (c chain) machines() []string {
	var walked []string
	for _, r := range c.rows {
		machine := strings.TrimSpace(r.Served)
		if machine == "" {
			machine = strings.TrimSpace(r.Lane)
		}
		if machine == "" {
			machine = "(none)"
		}
		if len(walked) > 0 && walked[len(walked)-1] == machine {
			continue
		}
		walked = append(walked, machine)
	}
	return walked
}

// stayedPut reports whether this chain never left the machine it started on:
// every attempt served or demanded the same one name, or none of them named a
// machine at all.
func (c chain) stayedPut() bool {
	return len(c.machines()) <= 1
}

// outcome is what the chain's last attempt came back as.
func (c chain) outcome() statusClass { return c.rows[len(c.rows)-1].statusClass() }

// chainsIn reconstructs every chain in a log, in the order they started.
//
// A ROW WITH NO ATTEMPT NUMBER IS ITS OWN CHAIN. Every attempt this build makes
// carries one; a row without it came from a build that did not, and folding it
// into a neighbour would invent a retry that never happened.
func chainsIn(rows []row) []chain {
	type key struct{ tag, node, model string }
	grouped := map[key][]row{}
	var order []key
	for _, r := range rows {
		if !r.finished() || r.Model == "" {
			continue
		}
		at := key{tag: r.Tag, node: r.Node, model: r.Model}
		if _, seen := grouped[at]; !seen {
			order = append(order, at)
		}
		grouped[at] = append(grouped[at], r)
	}
	var chains []chain
	for _, at := range order {
		group := grouped[at]
		sort.SliceStable(group, func(i, j int) bool { return group[i].at.Before(group[j].at) })
		current := chain{tag: at.tag, node: at.node, model: at.model}
		for _, r := range group {
			continues := len(current.rows) > 0 &&
				r.Attempt > current.rows[len(current.rows)-1].Attempt &&
				r.at.Sub(current.rows[len(current.rows)-1].at) <= chainGap
			if !continues {
				if len(current.rows) > 0 {
					chains = append(chains, current)
				}
				current = chain{tag: at.tag, node: at.node, model: at.model}
			}
			current.rows = append(current.rows, r)
		}
		if len(current.rows) > 0 {
			chains = append(chains, current)
		}
	}
	sort.SliceStable(chains, func(i, j int) bool { return chains[i].began().Before(chains[j].began()) })
	return chains
}

// multiAttempt is the chains that are actually about retrying — the ones with
// more than one attempt in them. Every share the census reports about chains is
// a share of these, because a chain of one cannot have stayed put or moved.
func multiAttempt(chains []chain) []chain {
	var many []chain
	for _, c := range chains {
		if len(c.rows) > 1 {
			many = append(many, c)
		}
	}
	return many
}

// longestChains is the n chains that took the longest, which is the list the
// first census printed and the one a reader looks at first: a chain's LENGTH in
// attempts is bounded by a constant somebody chose, and its DURATION is what
// the person actually sat through.
func longestChains(chains []chain, n int) []chain {
	sorted := append([]chain(nil), chains...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].duration() != sorted[j].duration() {
			return sorted[i].duration() > sorted[j].duration()
		}
		return len(sorted[i].rows) > len(sorted[j].rows)
	})
	if n < len(sorted) {
		sorted = sorted[:n]
	}
	return sorted
}
