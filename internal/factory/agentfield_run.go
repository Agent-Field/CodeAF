package factory

import (
	"strings"
	"time"
)

// ── A JOB RUN BY AN AGENTFIELD REASONER ─────────────────────────────────────
//
// A job filed with RunOn is run by an AgentField reasoner on some node, not by
// this factory's own steps, and it reads exactly like any other job: the same
// lists, page, Start and Stop. Its steps ARE the run's top-level calls — the
// reasoner the job dispatched called plan, investigate, draft_reply, so those
// are the job's steps, by the reasoner's own name — and a step that ran the
// codeaf harness has a chat (Phase.Chat) that is an ordinary conversation on
// this machine, written from the lines the node streamed. Nothing the
// developer of that node wrote names a step: the tree is AgentField's own.

// AgentFieldRun is the run behind a RunOn job.
type AgentFieldRun struct {
	// Target is the reasoner dispatched, "<node>.<reasoner>".
	Target string `json:"target"`
	// ExecutionID is the run's root execution; RunID its workflow.
	ExecutionID string    `json:"execution_id,omitempty"`
	RunID       string    `json:"run_id,omitempty"`
	Began       time.Time `json:"began,omitzero"`
	Ended       time.Time `json:"ended,omitzero"`
	// Status is the root execution's last known status, in AgentField's words.
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
	// URL is the run's page on the control plane's own UI, when it has one.
	URL string `json:"url,omitempty"`
	// Host is the machine the node said it runs on when the run began.
	Host string `json:"host,omitempty"`
	// A harness step's conversation keeps its own relay record beside its
	// transcript (agentfield.json): which execution it is, how much of it is
	// kept, whether it is steerable now. It lives with the conversation,
	// because the conversation is what this factory keeps.
}

// AgentFieldRunner is where an AgentField job runs, in the words the job's
// "runs on" fact shows: the node, and the node's machine when it named one —
// "support · dumb". A RunOn job never runs on this factory's machine, so it
// is never the factory's own machine word.
func AgentFieldRunner(it Item) string {
	node, _, _ := strings.Cut(strings.TrimSpace(it.RunOn), ".")
	if node == "" {
		return ""
	}
	if it.AgentField != nil && strings.TrimSpace(it.AgentField.Host) != "" {
		return node + " · " + strings.TrimSpace(it.AgentField.Host)
	}
	return node
}

// RunOnPending says the job is run by an AgentField reasoner ([Item.RunOn])
// whose run has not begun. ITS STEPS ARE THE RUN'S, NOT THE FACTORY'S: until
// the run reports its tree there is no step to show, so the default pipeline
// the job was filed with is neither drawn nor counted — a rail that showed
// plan, approve and write for a job that will never run them would be a lie.
func RunOnPending(it Item) bool {
	return it.RunOn != "" && it.AgentField == nil
}
