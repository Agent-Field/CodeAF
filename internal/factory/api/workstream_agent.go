package api

// ── AN ORGANIZATION WORKSTREAM'S AGENT ON ITS SERVER ───────────────────────
//
// While the operator runs the organization's workstream agents on its server
// (agent_runner "server", WAVE2.md §4.3), each workstream has one agent there,
// run with the organization's own model key, and everyone who can see the
// workstream reads the same conversation with it. Those who can edit the
// workstream talk to it; one message is answered at a time.
//
//	GET    /v1/org/keys                              -> OrgKeyList (operator)
//	PUT    /v1/org/keys/{provider}   OrgKeyPut        -> OrgKey (operator); never answered back
//	DELETE /v1/org/keys/{provider}                    -> 204 (operator)
//	GET    /v1/workstreams/{id}/agent?since=<seq>     -> WorkstreamAgent (view)
//	POST   /v1/workstreams/{id}/agent/messages AgentMessageNew -> 202 WorkstreamAgent (edit)
//	POST   /v1/workstreams/{id}/agent/stop            -> WorkstreamAgent (edit)
//
// Events, heard by everyone who can see the workstream:
//
//	agent.entry {id, entry}  one new line of the conversation
//	agent.state {id, state, turn?, for?, since?}  the agent started, is stopping or is idle again

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
}

// AgentEntriesMost is how many lines one read answers.
const AgentEntriesMost = 200

// AgentMessageMost is the longest message a person may send the agent, in
// bytes.
const AgentMessageMost = 16 << 10

// AgentMessageNew is what a person says to the agent.
type AgentMessageNew struct {
	Text string `json:"text"`
}

// AgentEvent is agent.entry's and agent.state's body.
type AgentEvent struct {
	ID    string      `json:"id"`
	Entry *AgentEntry `json:"entry,omitempty"`
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
