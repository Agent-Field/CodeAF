package serve

import (
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// RUN LIMITS AND A FAIR QUEUE (owner decisions 2026-11-17 §10; DESIGN §11.3,
// Q3). live2 WL started eight Afi tasks in one workstream at once and all
// eight ran together; nothing stopped one workstream's twenty-task plan from
// taking every slot and every dollar. Now at most taskRunsPerWorkstream runs
// go at once in one workstream and taskRunsPerOrg in the organization.
//
// A start over either limit is ACCEPTED, NOT REFUSED: the task says running
// (it has been started, and a run is on its way) and carries its place in the
// queue, which the row reads as "Queued · 2nd". Inside a workstream the queue
// is first come, first served; across workstreams a free place goes to the
// workstream that least recently had a run started (round robin), so one big
// plan cannot starve the rest. A queued run is kept on its lease (queued, with
// its words), so a restart queues it again instead of losing it; Stop takes it
// out of the queue.

const (
	// taskRunsPerWorkstream is how many agent runs one workstream has going
	// at once.
	taskRunsPerWorkstream = 3
	// taskRunsPerOrg is how many the organization has going at once.
	taskRunsPerOrg = 10
)

// pendingRun is a run waiting for its place.
type pendingRun struct {
	key     taskRunKey
	ws      factory.OrgWorkstream
	task    factory.WorkstreamTask
	starter string
	words   string
	// typed is what a person typed for this run (startTaskRunTyped).
	typed   string
	resumes int
	// answered is a run an answer to the task's Ask started (D1): it goes
	// on with what the earlier run made, after a wait in the queue too.
	answered bool
	seq      int
	// retried says the run already had its one more try after a model
	// error (workstream_task_limits.go).
	retried bool
}

// runningInLocked is how many runs ws has going; "" counts the organization's.
func (d *agentDesk) runningInLocked(ws string) int {
	if ws == "" {
		return len(d.tasks)
	}
	n := 0
	for k := range d.tasks {
		if k.ws == ws {
			n++
		}
	}
	return n
}

// roomLocked is whether a new run in ws may start now: under both limits,
// and with nothing of ws's already waiting ahead of it.
func (d *agentDesk) roomLocked(ws string) bool {
	if d.runningInLocked("") >= taskRunsPerOrg || d.runningInLocked(ws) >= taskRunsPerWorkstream {
		return false
	}
	for _, q := range d.queue {
		if q.key.ws == ws {
			return false
		}
	}
	return true
}

// queuedLocked is key's index in the queue, or -1.
func (d *agentDesk) queuedLocked(key taskRunKey) int {
	for i, q := range d.queue {
		if q.key == key {
			return i
		}
	}
	return -1
}

// enqueueLocked puts r at the back of the queue and answers it as kept.
func (d *agentDesk) enqueueLocked(r pendingRun) pendingRun {
	d.queueN++
	r.seq = d.queueN
	d.queue = append(d.queue, r)
	return r
}

// dequeueLocked takes key out of the queue, reporting whether it was there.
func (d *agentDesk) dequeueLocked(key taskRunKey) bool {
	i := d.queuedLocked(key)
	if i < 0 {
		return false
	}
	d.queue = append(d.queue[:i:i], d.queue[i+1:]...)
	return true
}

// servedLocked notes that ws just had a run started, for the round robin.
func (d *agentDesk) servedLocked(ws string) {
	if d.served == nil {
		d.served = map[string]int{}
	}
	d.servedN++
	d.served[ws] = d.servedN
}

// placeLocked is key's place in its workstream's queue, from 1, or 0 when it
// is not waiting.
func (d *agentDesk) placeLocked(key taskRunKey) int {
	n := 0
	for _, q := range d.queue {
		if q.key.ws == key.ws {
			n++
			if q.key == key {
				return n
			}
		}
	}
	return 0
}

// nextLocked is the index of the run that takes the next free place, or -1:
// the oldest waiting run of the workstream, under its limit, that least
// recently had a run started.
func (d *agentDesk) nextLocked() int {
	if d.runningInLocked("") >= taskRunsPerOrg {
		return -1
	}
	best, seen := -1, map[string]bool{}
	for i, q := range d.queue {
		if seen[q.key.ws] {
			continue
		}
		seen[q.key.ws] = true
		if d.runningInLocked(q.key.ws) >= taskRunsPerWorkstream {
			continue
		}
		if best < 0 || d.served[q.key.ws] < d.served[d.queue[best].key.ws] {
			best = i
		}
	}
	return best
}

// dispatchQueued starts waiting runs while there is room.
func (h *Handler) dispatchQueued() {
	d := &h.agentDesk
	for {
		d.mu.Lock()
		if d.ctx == nil || d.ctx.Err() != nil {
			d.mu.Unlock()
			return
		}
		i := d.nextLocked()
		if i < 0 {
			d.mu.Unlock()
			return
		}
		r := d.queue[i]
		d.queue = append(d.queue[:i:i], d.queue[i+1:]...)
		err := h.launchLocked(r)
		d.mu.Unlock()
		if err != nil {
			// The run could not begin (no runner any more): its task must
			// not keep saying running, so the sweep settles it.
			h.dropQueued(r.key, "")
		}
	}
}

// keepQueued keeps a queued run on its lease, so a restart queues it again.
// It runs in its own goroutine: the caller may be inside a task write.
func (h *Handler) keepQueued(r pendingRun, owner string) {
	st := h.config.Store
	if st == nil {
		return
	}
	at := h.clock().UTC().Format(time.RFC3339Nano)
	guard.Go("factory workstream task queue", func() {
		_ = st.PutTaskRun(store.TaskRun{WS: r.key.ws, Task: r.key.task, Owner: owner, Starter: r.starter,
			Started: at, Beat: at, Resumes: r.resumes, Words: r.words, Queued: true, Answered: r.answered})
	})
}

// dropQueued forgets a queued run's lease, as owner ("" whoever holds it).
func (h *Handler) dropQueued(key taskRunKey, owner string) {
	if st := h.config.Store; st != nil {
		guard.Go("factory workstream task queue", func() { _ = st.DropTaskRun(key.ws, key.task, owner) })
	}
}
