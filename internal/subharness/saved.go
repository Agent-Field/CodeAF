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
	names, err := s.Names()
	if err != nil {
		return nil, err
	}
	rows := make([]SavedHarness, 0, len(names))
	for _, name := range names {
		page, err := s.Load(name, 0)
		if err != nil {
			continue
		}
		row := SavedHarness{Harness: page}
		if paths, err := s.Runs(name); err == nil {
			row.Runs = len(paths)
			if len(paths) > 0 {
				if trace, err := s.LoadRun(paths[len(paths)-1]); err == nil {
					row.Last = &trace
				}
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}
