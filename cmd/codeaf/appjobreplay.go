package main

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/session"
)

// Old bash results named the job before typed process metadata existed.
// Only the producer's opening sentence is a claim; quoted command output is not.
var appLegacyJob = regexp.MustCompile(`^(?:still running as job |job )([1-9][0-9]*)(?:\b)`)

func appHistoryJob(item session.DisplayEntry) (int, bool) {
	if item.Role != "tool" || item.Tool != "bash" || !item.Answered {
		return 0, false
	}
	match := appLegacyJob.FindStringSubmatch(strings.TrimSpace(item.Output))
	if match == nil {
		return 0, false
	}
	id, err := strconv.Atoi(match[1])
	return id, err == nil
}

// appReplayJobs reads history once when the app takes a conversation.
// Existing typed facts win; an old row gains only what its starting call says.
func appReplayJobs(transcript string) map[int]session.JobNotice {
	rows, err := session.ReadProcessNotices(transcript)
	if err != nil || len(rows) == 0 {
		return nil
	}
	jobs := make(map[int]session.JobNotice, len(rows))
	for _, row := range rows {
		jobs[row.ID] = row
	}
	_ = appRecords.WithRecord(transcript, func(record session.Record) error {
		appFillJobHistory(jobs, appRecordItems(record))
		return nil
	})
	return jobs
}

func appFillJobHistory(jobs map[int]session.JobNotice, items []session.DisplayEntry) {
	claims := map[int]int{}
	for _, item := range items {
		if id, ok := appHistoryJob(item); ok {
			claims[id]++
		}
	}
	for _, item := range items {
		id, ok := appHistoryJob(item)
		if !ok {
			continue
		}
		job, kept := jobs[id]
		// Multiple starts naming the same old number cannot establish its owner.
		if job.ToolCallID == "" && claims[id] != 1 {
			continue
		}
		if !kept || (job.ToolCallID != "" && job.ToolCallID != item.CallID) {
			continue
		}
		// A typed result from a different run cannot fill a reused job number.
		if item.ToolProcess != nil && (item.ToolProcess.Job != id ||
			(!job.Started.IsZero() && !job.Started.Equal(item.ToolProcess.Started))) {
			continue
		}
		if job.Command == "" {
			var args struct {
				Command string `json:"command"`
			}
			if json.Unmarshal([]byte(item.Args), &args) == nil {
				job.Command = args.Command
			}
		}
		if job.ToolCallID == "" {
			job.ToolCallID = item.CallID
		}
		if job.Started.IsZero() && item.ToolProcess != nil {
			job.Started = item.ToolProcess.Started
		}
		jobs[id] = job
	}
}

// appRecoverJob overlays only missing facts, so live updates keep their truth.
func appRecoverJob(job session.JobNotice, kept map[int]session.JobNotice) session.JobNotice {
	old, ok := kept[job.ID]
	if !ok || (!job.Started.IsZero() && !old.Started.IsZero() && !job.Started.Equal(old.Started)) {
		return job
	}
	if job.Command == "" {
		job.Command = old.Command
	}
	if job.ToolCallID == "" {
		job.ToolCallID = old.ToolCallID
	}
	if job.Started.IsZero() {
		job.Started = old.Started
	}
	return job
}

// appLinkJobHistory copies the cache's slices before adding legacy links.
// Each result occurrence owns its link even when provider call ids repeat.
func appLinkJobHistory(items []session.DisplayEntry, jobs map[int]session.JobNotice) []session.DisplayEntry {
	out := append([]session.DisplayEntry(nil), items...)
	for i, item := range out {
		if item.ToolProcess != nil {
			continue
		}
		if id, ok := appHistoryJob(item); ok {
			if job, kept := jobs[id]; kept && job.ToolCallID == item.CallID {
				out[i].ToolProcess = &session.ToolProcess{Job: id, Started: job.Started}
			}
		}
	}
	return out
}
