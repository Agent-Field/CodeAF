package github

// ForgetSweeps stands in for a new process, which has swept nothing yet.
func ForgetSweeps() {
	sweepsMu.Lock()
	defer sweepsMu.Unlock()
	sweeps = map[string]issueSweep{}
}
