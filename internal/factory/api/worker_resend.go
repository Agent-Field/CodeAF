package api

import "net/http"

// WorkerResendable reports that a worker request the factory answered with
// status may be sent again unchanged: the factory was down, slow or busy, and
// did not refuse the request itself. The worker resends on exactly these, and
// the factory keeps no replay of them, so a passing outage never becomes a
// cached refusal.
func WorkerResendable(status int) bool {
	return status >= http.StatusInternalServerError ||
		status == http.StatusRequestTimeout ||
		status == http.StatusTooManyRequests
}

// ReportCommittedNothing reports that a worker's report answered with status
// was refused as invalid before any of its results were saved. After a repair
// the same report must be checked again, so the factory keeps no replay of it.
func ReportCommittedNothing(status int) bool {
	return status == http.StatusUnprocessableEntity
}
