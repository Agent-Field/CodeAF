package factory

import "time"

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
	Began       time.Time `json:"began,omitempty"`
	Ended       time.Time `json:"ended,omitempty"`
	// Status is the root execution's last known status, in AgentField's words.
	Status string `json:"status,omitempty"`
	Error  string `json:"error,omitempty"`
	// A harness step's conversation keeps its own relay record beside its
	// transcript (agentfield.json): which execution it is, how much of it is
	// kept, whether it is steerable now. It lives with the conversation,
	// because the conversation is what this factory keeps.
}
