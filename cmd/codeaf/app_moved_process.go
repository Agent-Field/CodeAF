package main

import (
	"github.com/Agent-Field/codeaf/internal/inventory"
	"github.com/Agent-Field/codeaf/internal/session"
)

// A carried process keeps its original execution folder; only its managed log
// is read here. An arbitrary path in a record is never accepted as a log grant.
func appMovedProcessRecord(transcript string, job session.JobNotice) session.JobNotice {
	return appMovedProcessRecords(transcript, []session.JobNotice{job})[0]
}

// A whole-list read shares its inventory lookup across all returned rows.
func appMovedProcessRecords(transcript string, jobs []session.JobNotice) []session.JobNotice {
	if len(jobs) == 0 {
		return jobs
	}
	root := session.DirOf(transcript)
	if root == "" {
		return jobs
	}
	store, err := inventory.Open(root)
	if err != nil {
		return jobs
	}
	from := store.Snapshot().ChatDir
	for i := range jobs {
		jobs[i] = session.CarriedProcessLog(root, from, jobs[i])
	}
	return jobs
}
