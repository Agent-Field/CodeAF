package github

import "time"

// ForgetSweeps stands in for a new process, which has swept nothing yet.
func ForgetSweeps() {
	sweepsMu.Lock()
	defer sweepsMu.Unlock()
	sweeps = map[string]issueSweep{}
	openReads = map[string]time.Time{}
}
