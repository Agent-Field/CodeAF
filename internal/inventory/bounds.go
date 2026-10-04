package inventory

// The record travels in every seal, so each list of it has a ceiling. A list
// over its ceiling keeps its newest entries and counts the rest in Omitted, so
// the brief can say "and N more" without the record growing with the work.
const (
	maxRunning  = 16
	maxWithheld = 32
	maxDetached = 8
	// maxCommand cuts one recorded command line, in bytes.
	maxCommand = 240
)

// The names of the lists, as Omitted keys them.
const (
	ListRunning  = "running"
	ListWithheld = "withheld"
	ListDetached = "detached"
)

// newest keeps the last max entries of list, which is ordered oldest first, and
// says how many it dropped.
func newest[T any](list []T, max int) ([]T, int) {
	if len(list) <= max {
		return list, 0
	}
	return list[len(list)-max:], len(list) - max
}

// setOmitted records how many entries of one list were cut, and forgets the
// count when none were.
func (inv *Inventory) setOmitted(list string, n int) {
	if n == 0 {
		delete(inv.Omitted, list)
		if len(inv.Omitted) == 0 {
			inv.Omitted = nil
		}
		return
	}
	if inv.Omitted == nil {
		inv.Omitted = map[string]int{}
	}
	inv.Omitted[list] = n
}

// SetRunning replaces the running list whole: a process that stopped leaves.
func (inv *Inventory) SetRunning(list []Running) {
	kept, dropped := newest(list, maxRunning)
	inv.Running = kept
	inv.setOmitted(ListRunning, dropped)
}

// SetDetached replaces the detached list, cut to its bound.
func (inv *Inventory) SetDetached(list []Detached) {
	kept, dropped := newest(list, maxDetached)
	inv.Detached = kept
	inv.setOmitted(ListDetached, dropped)
}

// SetWithheld replaces the entries whose path own names, and keeps the others.
// The seal's own folders and each task copy's are written by different steps of
// the same seal, and neither may erase the other's.
func (inv *Inventory) SetWithheld(own func(path string) bool, list []Withheld) {
	var all []Withheld
	for _, w := range inv.Withheld {
		if !own(w.Path) {
			all = append(all, w)
		}
	}
	kept, dropped := newest(append(all, list...), maxWithheld)
	inv.Withheld = kept
	inv.setOmitted(ListWithheld, dropped)
}

// OmittedOf is how many entries of a list a bound cut at the last write.
func (inv Inventory) OmittedOf(list string) int { return inv.Omitted[list] }
