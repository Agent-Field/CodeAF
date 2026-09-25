package tui3

// serviceLands is the desk between a fetch goroutine and the update loop: the
// pairs a warm or a ctrl+r walk landed since the loop last read. The goroutine
// that stocked a compartment writes here and rings [app.landedBell]; the Update
// that takes the ring reads and clears the desk ON the loop, drops the memo
// under each pair, and restocks an open picker — so a group fills WITHOUT a
// reopen and never off the loop (issue #1508).
type serviceLands struct {
	pairs map[[2]string]bool
}

// put records one landed pair. Safe from any goroutine.
func (l *serviceLands) put(source, address string) {
	// The desk is written before the bell is rung and read on the loop after
	// the ring; the loop's read is the synchronisation point, so the write
	// happens-before every read. A second write of the same pair before the
	// loop looks collapses onto the first — one ring says both.
	l.pairs[[2]string{source, address}] = true
}

// take reads and clears the desk. Called on the loop only.
func (l *serviceLands) take() [][2]string {
	out := make([][2]string, 0, len(l.pairs))
	for pair := range l.pairs {
		out = append(out, pair)
	}
	l.pairs = map[[2]string]bool{}
	return out
}

// serviceModelsLandedMsg is the message the door carries. Nothing is read out
// of it: the pairs are on the desk ([serviceLands]) by the time it arrives,
// and it exists only to bring the loop back around to read them.
type serviceModelsLandedMsg struct{}
