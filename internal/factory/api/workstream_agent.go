package api

// ── AN ORGANIZATION WORKSTREAM'S AGENT ON ITS SERVER ───────────────────────
//
// While the operator runs the organization's workstream agents on its server
// (agent_runner "server", WAVE2.md §4.3), each workstream has one agent there,
// run with the organization's own model key. Each person who can edit the
// workstream has their own private conversation with it, which nobody else
// reads; one message of theirs is answered at a time.
//
//	GET    /v1/org/keys                              -> OrgKeyList (operator)
//	PUT    /v1/org/keys/{provider}   OrgKeyPut        -> OrgKey (operator); never answered back
//	DELETE /v1/org/keys/{provider}                    -> 204 (operator)
//	GET    /v1/workstreams/{id}/agent?since=<seq>     -> WorkstreamAgent (view)
//	POST   /v1/workstreams/{id}/agent/messages AgentMessageNew -> 202 WorkstreamAgent (edit)
//	POST   /v1/workstreams/{id}/agent/stop            -> WorkstreamAgent (edit)
//
// Events, heard only by the person whose conversation it is (person):
//
//	agent.entry {id, person, entry}  one new line of the conversation
//	agent.state {id, person, state, turn?, for?, since?}  the agent started, is stopping or is idle again

// The event kinds the server agent publishes.
const (
	EventAgentEntry = "agent.entry"
	EventAgentState = "agent.state"
)

// The kinds of line in an agent's conversation.
const (
	AgentEntryPerson = "person" // what someone asked; By is them
	AgentEntryAgent  = "agent"  // the agent's answer
	AgentEntryStep   = "step"   // one thing the agent did on the way (Tool, Failed)
	AgentEntryNote   = "note"   // the server saying what happened: stopped, spent its budget, could not reach the model
)

// The states an agent is in.
const (
	AgentIdle     = "idle"
	AgentWorking  = "working"
	AgentStopping = "stopping"
)

// AgentEntry is one line of a workstream agent's conversation. Seq counts up
// from 1 per workstream; Turn names the message it belongs to.
type AgentEntry struct {
	Seq    int    `json:"seq"`
	At     string `json:"at"`
	Kind   string `json:"kind" jsonschema:"enum=person,enum=agent,enum=step,enum=note"`
	By     string `json:"by,omitempty"`
	Text   string `json:"text"`
	Tool   string `json:"tool,omitempty"`
	Failed bool   `json:"failed,omitempty"`
	Turn   string `json:"turn,omitempty"`
}

// AgentState is what the agent is doing: For is whose message it is
// answering, Since when it started.
type AgentState struct {
	State string `json:"state" jsonschema:"enum=idle,enum=working,enum=stopping"`
	Turn  string `json:"turn,omitempty"`
	For   string `json:"for,omitempty"`
	Since string `json:"since,omitempty"`
}

// WorkstreamAgent is GET .../agent's answer: the agent's state, the lines
// after since (the newest AgentEntriesMost when since is 0), the last seq,
// and whether the caller may talk to it now. Cannot is the one sentence that
// says why not, empty when they can, and also empty for a reader who could
// never talk to it (view only), whose screen simply has no composer.
type WorkstreamAgent struct {
	ID string `json:"id"`
	AgentState
	Entries []AgentEntry `json:"entries"`
	Seq     int          `json:"seq"`
	CanTalk bool         `json:"can_talk"`
	Cannot  string       `json:"cannot,omitempty"`
	// Private is true from a server where each person has their own
	// conversation with the agent, which no one else reads.
	Private bool `json:"private,omitempty"`
}

// AgentEntriesMost is how many lines one read answers.
const AgentEntriesMost = 200

// AgentMessageMost is the longest message a person may send the agent, in
// bytes.
const AgentMessageMost = 16 << 10

// AgentMessageNew is what a person says to the agent.
type AgentMessageNew struct {
	Text string `json:"text"`
	// TZ is the sender's time zone, an IANA name ("America/Toronto"), so the
	// agent reckons today's date and deadlines where they are. Optional.
	TZ string `json:"tz,omitempty"`
}

// AgentEvent is agent.entry's and agent.state's body. Person is whose
// conversation it is; only they hear it.
type AgentEvent struct {
	ID     string      `json:"id"`
	Person string      `json:"person,omitempty"`
	Entry  *AgentEntry `json:"entry,omitempty"`
	*AgentState
}

// OrgKey is one of the organization's model keys as a screen may see it:
// the provider, its last four characters (empty for a key too short to hide
// anything), and who set it when. THE KEY ITSELF IS NEVER ANSWERED.
type OrgKey struct {
	Provider string `json:"provider"`
	Last4    string `json:"last4"`
	SetAt    string `json:"set_at"`
	SetBy    string `json:"set_by"`
}

// OrgKeyList is GET /v1/org/keys's answer, and Providers the ones a key may
// be kept for.
type OrgKeyList struct {
	Keys      []OrgKey `json:"keys"`
	Providers []string `json:"providers"`
}

// OrgKeyPut is the key the operator hands the server.
type OrgKeyPut struct {
	Key string `json:"key"`
}

// ── A TASK'S WORKING CONVERSATION ──────────────────────────────────────────
//
// A task Afi works on the server runs in a seat of its own, and what it says
// and does there is the task's Working conversation: EVERYONE WHO CAN SEE THE
// WORKSTREAM READS IT (OWNER-DECISIONS 2026-11-17 §8). The task's owner and
// whoever started the run steer it there, and a steer is read by everyone too.
// Afi's pane and a person's own chats stay private; only typing on a task is
// shared.
//
//	GET  /v1/workstreams/{id}/tasks/{tid}/work?since=<seq>   -> TaskWork (view)
//	POST /v1/workstreams/{id}/tasks/{tid}/work/messages TaskSteerNew -> 202 TaskWork (owner or starter: into the run going, or a revision run on a done Afi task)
//
// Event, heard by everyone who can see the workstream:
//
//	task.work {id, task, entry?, state?}  one new line of the run, or the run started or ended

// EventTaskWork is one line of a task's run, or a change to whether it runs.
const EventTaskWork = "task.work"

// TaskWork is a task's Working conversation as the caller reads it: whether a
// run is going (State working, For is who started it), the lines after since
// (the newest AgentEntriesMost when since is 0), and the last seq. CanSteer is
// whether the caller may steer it now: a run is going, and they own the task
// or started the run. Steerer is whether they would be allowed while one is.
type TaskWork struct {
	ID   string `json:"id"`
	Task string `json:"task"`
	AgentState
	Entries  []AgentEntry `json:"entries"`
	Seq      int          `json:"seq"`
	CanSteer bool         `json:"can_steer"`
	Steerer  bool         `json:"steerer,omitempty"`
	// CanRevise is whether the caller's words would start a revision run now:
	// the task is Afi's and done, nothing is working, and they may steer it.
	CanRevise bool `json:"can_revise,omitempty"`
	// USD is what every run of this task has spent; absent while nothing has.
	USD float64 `json:"usd,omitempty"`
}

// TaskSteerNew is one steer: words to the agent working the task, which
// everyone in the workstream reads.
type TaskSteerNew struct {
	Text string `json:"text"`
}

// TaskWorkEvent is task.work's body.
type TaskWorkEvent struct {
	ID    string      `json:"id"`
	Task  string      `json:"task"`
	Entry *AgentEntry `json:"entry,omitempty"`
	*AgentState
}
