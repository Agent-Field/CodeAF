package api

import "time"

// RunningRetention is the time a merged job stays beside live work in Running.
const RunningRetention = 24 * time.Hour

// DisplayFor applies the same retention boundary to queue and individual job reads.
func DisplayFor(job Job, now time.Time) JobDisplay {
	visible := job.State == StateRunning || job.State == StateNeedsYou
	if job.PR != nil && job.PR.State == "open" && job.State == StateDone {
		visible = true
	}
	if job.PR != nil && job.PR.State == "merged" && job.PR.MergedAt != nil {
		visible = !now.Before(*job.PR.MergedAt) && now.Before(job.PR.MergedAt.Add(RunningRetention))
	}
	return JobDisplay{RunningVisible: visible}
}
