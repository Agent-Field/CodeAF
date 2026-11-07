package agentfield

import (
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ── WHICH AGENTFIELD EXECUTIONS ARE WAITING ON WHICH JOB ────────────────────
//
// A trigger's dispatch is one control plane execution; the intake answers
// 202 and promises to report on it. The promise is kept HERE, in a side table
// beside the floor (agentfield-executions.json), and not on the job: a job
// never learns which outside system is watching it (the design's "one event
// bus out"), and the floor's job record is shared by every lane. One job may
// carry several executions — the same Linear issue updated twice is one job
// and two dispatches — and each is told the job's moments until it is
// complete.

const executionsFile = "agentfield-executions.json"

// keepEnded is how long an ended execution stays in the table, so a late
// duplicate delivery of the same moment is still recognised.
const keepEnded = 7 * 24 * time.Hour

// Execution is one control plane execution and what it was last told.
type Execution struct {
	ID    string    `json:"id"`
	Job   int       `json:"job"`
	Began time.Time `json:"began"`
	// Said is the last report's fingerprint, so a moment is told once.
	Said string `json:"said,omitempty"`
	// SaidAt is when it was last told anything; a running execution is told
	// again after [keepAlive] so the control plane never thinks it stale.
	SaidAt time.Time `json:"said_at,omitempty"`
	// Ended is when it was completed (succeeded or failed); zero while open.
	Ended time.Time `json:"ended,omitempty"`
}

type executionsDoc struct {
	Executions []Execution `json:"executions"`
}

// executions is the table over one floor's folder.
type executions struct {
	root string
	mu   sync.Mutex
}

func (x *executions) path() string { return filepath.Join(x.root, executionsFile) }

func (x *executions) read() executionsDoc {
	var d executionsDoc
	_ = readJSON(x.path(), &d)
	return d
}

// add records execution id as waiting on job. A second add of one id changes
// nothing.
func (x *executions) add(id string, job int, now time.Time) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	d := x.read()
	for _, e := range d.Executions {
		if e.ID == id {
			return nil
		}
	}
	d.Executions = append(d.Executions, Execution{ID: id, Job: job, Began: now})
	return x.write(d, now)
}

// open is every execution not yet ended, oldest first.
func (x *executions) open() []Execution {
	x.mu.Lock()
	defer x.mu.Unlock()
	var out []Execution
	for _, e := range x.read().Executions {
		if e.Ended.IsZero() {
			out = append(out, e)
		}
	}
	return out
}

// forJob is job's open executions.
func (x *executions) forJob(job int) []Execution {
	var out []Execution
	for _, e := range x.open() {
		if e.Job == job {
			out = append(out, e)
		}
	}
	return out
}

// said records what execution id was told, and that it ended when ended.
func (x *executions) said(id, fingerprint string, ended bool, now time.Time) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	d := x.read()
	for i := range d.Executions {
		if d.Executions[i].ID != id {
			continue
		}
		d.Executions[i].Said, d.Executions[i].SaidAt = fingerprint, now
		if ended {
			d.Executions[i].Ended = now
		}
	}
	return x.write(d, now)
}

// write keeps d, dropping executions that ended long ago.
func (x *executions) write(d executionsDoc, now time.Time) error {
	kept := d.Executions[:0]
	for _, e := range d.Executions {
		if !e.Ended.IsZero() && now.Sub(e.Ended) > keepEnded {
			continue
		}
		kept = append(kept, e)
	}
	d.Executions = kept
	if len(d.Executions) == 0 {
		if err := os.Remove(x.path()); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return writePrivate(x.path(), d)
}
