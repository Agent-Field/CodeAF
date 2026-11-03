package github

// ForgetSweeps stands in for a restart, discarding only nomination scheduling.
func ForgetSweeps() {
	checksMu.Lock()
	defer checksMu.Unlock()
	checks = map[string]*closeChecks{}
}
