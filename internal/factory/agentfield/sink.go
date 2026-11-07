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
// the same way, and a job never learns which. This sink is two halves over
// one rule ([Update]):
//
//   - [replier] is an [inbox.Replier], registered for every agentfield:*
//     source, so the moments the bus carries today — the pull request opened,
//     merged, the job stopped for a person — reach the control plane the
//     moment the worker reports them;
//   - [Node.reportAll] reads every open execution's job on a short tick, for
//     the moments the bus does not carry yet (the job started, asked a
//     question, ended with no pull request) and to keep a long job's
//     execution from looking stale.
//
// THE ADAPTER IS THIN ON PURPOSE. Lane gi-trigger-api is adding started,
// question and done to the bus with a sink registration of its own; when it
// lands, the tick's moment reading is the part that goes, and [Update] stays.
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
// Each execution is told each distinct state once ([Execution.Said]); a
// running one is told again after [keepAlive].

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

// replier is the node as the inbox's answerer.
type replier struct{ n *Node }

func (r replier) Reply(ctx context.Context, _ string, ev inbox.Event) error {
	n := r.n
	n.mu.Lock()
	run := n.running
	n.mu.Unlock()
	if run == nil {
		return nil
	}
	n.reportJob(ctx, run.client, ev.JobID)
	return nil
}

// reportAll tells every open execution where its job stands.
func (n *Node) reportAll(ctx context.Context, c *Client) {
	seen := map[int]bool{}
	for _, e := range n.table().open() {
		if !seen[e.Job] {
			seen[e.Job] = true
			n.reportJob(ctx, c, e.Job)
		}
	}
}

// reportJob tells job's open executions where it stands. One report runs at a
// time, so the tick and the bus never tell one moment twice.
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
