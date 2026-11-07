package agentfield

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/inbox"
)

// ── THE AGENTFIELD SINK: A JOB'S MOMENTS AS EXECUTION STATUS ────────────────
//
// The design's "one event bus out": the lifecycle's moments reach every sink
// the same way, and a job never learns which. This node is one sink
// ([inbox.RegisterSink] under [SinkName]) beside the source's own replier
// and the signed webhook: each moment — filed, started, a question, the pull
// request opened, merged, stuck, done, a message read — tells the job's open
// executions where it stands now, and [Node.retellQuiet] on the lease's
// renewal tells an execution that has gone quiet for [keepAlive] again.
//
// Whatever reports, the same rule decides, from the job as it is now:
//
//   - a pull request opened → succeeded, result {job, state, pr_url,
//     steps_done, steps_total, …};
//   - the job ended without one (closed, dropped, merged elsewhere) →
//     succeeded with the same result and no pr_url;
//   - the job stopped and needs a person (a step failed, a person stopped
//     it, its publish failed) → failed, with the job's own words as the
//     error and the result beside it;
//   - otherwise → running, with progress as the share of steps done and the
//     job's question in the result while it waits on one.
//
// Each execution is told each distinct state once ([Execution.Said]); an
// open one is told again after [keepAlive].

// Update is what an execution waiting on it is told now, a fingerprint of
// it, and whether it completes the execution.
func Update(it factory.Item) (ExecutionUpdate, string, bool) {
	facts := Facts(it)
	done, total := factory.StepCount(it)
	state := factory.JobState(it)
	pr := ""
	if it.PullRequest != nil {
		pr = it.PullRequest.URL
	}
	fingerprint := fmt.Sprintf("%s|%s|%d/%d|%s", state, pr, done, total, it.Question)
	switch {
	case pr != "" || state == "done":
		return ExecutionUpdate{Status: ExecSucceeded, Result: facts}, ExecSucceeded + "|" + fingerprint, true
	case stuck(it):
		return ExecutionUpdate{Status: ExecFailed, Result: facts, Error: stuckWords(it)}, ExecFailed + "|" + fingerprint, true
	}
	progress := 0
	if total > 0 {
		progress = done * 100 / total
	}
	return ExecutionUpdate{Status: ExecRunning, Result: facts, Progress: &progress}, ExecRunning + "|" + fingerprint, false
}

// stuck says the job stopped and will not go on until a person acts. A job
// waiting on an answer to its question is NOT stuck: it goes on when answered.
func stuck(it factory.Item) bool {
	return factory.StoppedByPerson(it) || it.Failure != nil || it.PublishError != "" ||
		(it.PublishStatus != nil && it.PublishStatus.State == "failed")
}

func stuckWords(it factory.Item) string {
	switch {
	case factory.StoppedByPerson(it):
		return "a person stopped this job"
	case it.Failure != nil && it.Failure.Detail != "":
		return "codeaf stopped on this and needs a person: " + oneLine(it.Failure.Detail, 300)
	case it.PublishError != "":
		return "codeaf could not publish this: " + oneLine(it.PublishError, 300)
	}
	return "codeaf stopped on this and needs a person"
}

// sink is the node on the event bus.
type sink struct{ n *Node }

func (s sink) Send(ctx context.Context, m inbox.Moment) error {
	n := s.n
	n.mu.Lock()
	run := n.running
	n.mu.Unlock()
	if run == nil {
		return nil
	}
	n.reportJob(ctx, run.client, m.JobID)
	return nil
}

// retellQuiet tells every open execution untold for [keepAlive] where its
// job stands.
func (n *Node) retellQuiet(ctx context.Context, c *Client) {
	seen := map[int]bool{}
	now := n.now()
	for _, e := range n.table().open() {
		if !seen[e.Job] && now.Sub(e.SaidAt) >= keepAlive {
			seen[e.Job] = true
			n.reportJob(ctx, c, e.Job)
		}
	}
}

// reportJob tells job's open executions where it stands. One report runs at a
// time, so two moments, or a moment and the lease's retelling, never tell one
// state twice.
func (n *Node) reportJob(ctx context.Context, c *Client, job int) {
	n.reportMu.Lock()
	defer n.reportMu.Unlock()
	open := n.table().forJob(job)
	if len(open) == 0 {
		return
	}
	it, err := n.Store.Get(job)
	if err != nil {
		// A job that is gone is told as done, so its execution does not run on.
		for _, e := range open {
			n.tell(ctx, c, e, ExecutionUpdate{Status: ExecFailed, Error: "the job behind this execution is gone"}, "gone", true)
		}
		return
	}
	u, fingerprint, ends := Update(it)
	for _, e := range open {
		if e.Said == fingerprint && (ends || n.now().Sub(e.SaidAt) < keepAlive) {
			continue
		}
		n.tell(ctx, c, e, u, fingerprint, ends)
	}
}

// tell sends u for e and records it. An execution the control plane says has
// already ended, or never had, is closed here too: nothing it is told again
// can change it.
func (n *Node) tell(ctx context.Context, c *Client, e Execution, u ExecutionUpdate, fingerprint string, ends bool) {
	now := n.now()
	if ends {
		at := now.UTC()
		took := now.Sub(e.Began).Milliseconds()
		u.CompletedAt, u.DurationMS = &at, &took
	}
	err := c.Report(ctx, e.ID, u)
	switch {
	case err == nil:
		_ = n.table().said(e.ID, fingerprint, ends, now)
	case RefusedAs(err, RefusedEnded) || RefusedAs(err, RefusedMissing) || strings.Contains(err.Error(), "terminal state"):
		_ = n.table().said(e.ID, fingerprint, true, now)
	default:
		n.trouble("reporting job " + strconv.Itoa(e.Job) + " to AgentField: " + err.Error())
	}
}

// Open is the executions still waiting on a job, for the status page.
func (n *Node) Open() []Execution { return n.table().open() }
