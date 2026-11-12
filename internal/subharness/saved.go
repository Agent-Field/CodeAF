package subharness

// SavedHarness is the current page and its recorded run history. Both lists
// read this snapshot so a saved page has one account of its newest run.
type SavedHarness struct {
	Harness Harness
	Runs    int
	Last    *Trace
}

// Saved loads the head of every name in registry order. Unreadable pages are
// omitted just as the terminal has always omitted them; unreadable history
// leaves the page visible without inventing an outcome.
func (s *Store) Saved() ([]SavedHarness, error) {
	return s.saved(nil)
}
