package run

// ObserveTerminalRootHeld reports when a pass has observed a done root whose
// worker has not returned, after applying the bound that keeps the run open.
func ObserveTerminalRootHeld(s *Supervisor, observed func()) {
	s.terminalRootHeld = observed
}
