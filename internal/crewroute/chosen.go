package crewroute

import (
	"strings"
)

// applyChosen is the crew with the caller's pick seated where this engine's
// differs: for every unpinned seat the request named ([Request.Chosen]), the
// named model is seated through [pinned] — the same machinery a pin runs, so
// its quality, its cost and its route are read exactly as a pin's would be —
// and then marked NOT pinned, because it is not one. A seat keeps everything
// a routed seat has: the ladder computed after this runs, the caller's
// rescue list, and a receipt that says `routed`.
//
// A pick is left standing when the seat is pinned (a pin is the person's),
// when the request names no model for it, or when no candidate carries the
// named model — a pick made outside the models the person allows cannot be
// seated, and the crew that can is.
func applyChosen(t *table, class Class, crew []Pick, candidates []Candidate, pins map[Seat]Pin, chosen map[Seat]string) []Pick {
	out := append([]Pick(nil), crew...)
	for i := range out {
		pick := out[i]
		if pick.Pinned {
			continue
		}
		if _, pinned := pins[pick.Seat]; pinned {
			continue
		}
		want := strings.TrimSpace(chosen[pick.Seat])
		if want == "" || Lineage(want) == Lineage(pick.Model) {
			continue
		}
		offered := false
		for _, c := range candidates {
			if Lineage(c.Model.ID) == Lineage(want) {
				offered = true
				break
			}
		}
		if !offered {
			continue
		}
		next := pinned(t, class, pick.Seat, Pin{Model: want}, candidates)
		next.Pinned = false
		out[i] = next
	}
	return out
}
