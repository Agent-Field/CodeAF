package factory

// ── WHERE AN ORGANIZATION WORKSTREAM'S AGENT RUNS ──────────────────────────
//
// The operator chooses, once for the whole organization (owner decision,
// WAVE2.md §4.3):
//
//   - DRIVER: on the laptop of the person driving the workstream, on their
//     engine and with their model key. One person drives at a time, through
//     the workstream's driver hand, and what the agent spends is posted to the
//     workstream on the server in that person's name.
//   - SERVER: on the organization's server, with the model keys the operator
//     set there. A server that cannot run agents does not offer it.

const (
	AgentRunnerDriver = "driver"
	AgentRunnerServer = "server"
)

// KnownAgentRunner says v is a choice a store may keep; "" is a store written
// before the choice existed.
func KnownAgentRunner(v string) bool {
	return v == "" || v == AgentRunnerDriver || v == AgentRunnerServer
}

// AgentRunnerOf is the choice v stands for: driver when none was made.
func AgentRunnerOf(v string) string {
	if v == "" {
		return AgentRunnerDriver
	}
	return v
}
