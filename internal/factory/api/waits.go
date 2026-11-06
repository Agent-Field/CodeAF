package api

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// JobBlock is a reason to fix a parent; it leaves the job's state and doors alone.
type JobBlock struct {
	By     string    `json:"by"`
	Reason string    `json:"reason"`
	Since  time.Time `json:"since"`
	Job    int       `json:"job,omitempty"`
}

func waitsFromItem(j *Job, it factory.Item) {
	j.WaitsOn = append([]int(nil), it.WaitsOn...)
	if !factory.WaitsPending(it) {
		return
	}
	j.WaitingOn = append([]int(nil), it.WaitingOn...)
	if b := it.Blocked; b != nil {
		j.Blocked = &JobBlock{By: b.By, Reason: b.Reason, Since: b.Since, Job: b.Job}
	}
}

func init() {
	Lost["Item.WaitsOn"] = "read-only parent ids; only the factory's waits_on door changes them, never a worker report or a client snapshot"
	Lost["Item.WaitingOn"] = "read-only unsatisfied parent ids; only the factory's waits code updates them, never a worker report or a client snapshot"
	Lost["Item.WaitsReleased"] = "service-owned once-only release mark; workers and client snapshots cannot restore waits"
	Lost["Item.Blocked"] = "read-only scheduler decision; workers and client snapshots cannot set or clear it"
}
