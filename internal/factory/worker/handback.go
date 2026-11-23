package worker

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/lease"
)

// ── a quitting runner gives its holds back ─────────────────────────────────
//
// A RUNNER THAT QUIT LET ITS HOLDS RUN OUT. Each held job then sat on a dead
// hold for the lease's whole silence, about a minute, before the factory took
// it back and marked it offline mid-step, so an engine restarted at once found
// nothing to take and the job waited on a retry. A clean quit now hands each
// hold back ([lease.Desk.HandBack]) once its runner has let the job go: the
// job is queued again at once with no failure, a native step is run again by
// the next pull, and a step whose AgentField run goes on at the control plane
// is followed by the runner that takes it.
//
// IT IS BOUNDED, AND IT IS ONLY A SHORTCUT. One request of at most
// handBackWait, after [job.letGo]'s own wait for the runner, inside the
// app's ten-second wait for its runner at quit. Whatever it
// cannot do safely it leaves to expiry, exactly as before: a job its runner
// has not finished letting go (a step still running here must not be offered
// to another runner), a job whose results are retained but unconfirmed (their
// recovery is keyed to this hold), and a factory that does not answer.

// handBackWait bounds the one hand-back request a quitting job makes.
const handBackWait = 2 * time.Second

// handBacks counts the holds handed back, so a test can say a quit gave its
// hold back and a job with unconfirmed results did not.
var handBacks atomic.Int64

// handBack gives the job's hold back to the factory, when that is safe.
func (j *job) handBack() bool {
	if j.pending != nil || j.blocked != "" || j.o.Runner.Running(j.id) {
		return true
	}
	wait := handBackWait
	if j.o.BeatEvery > 0 && j.o.BeatEvery < lease.HeartbeatEvery {
		wait = min(wait, max(100*time.Millisecond, j.o.BeatEvery*4))
	}
	ctx, cancel := context.WithTimeout(context.Background(), wait)
	defer cancel()
	err := j.w.HandBack(ctx, j.lease)
	if err == nil {
		handBacks.Add(1)
		return true
	}
	return gone(err)
}
