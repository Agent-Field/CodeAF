package appapi

// These wire types reserve the app vocabulary before this resource has behaviour.
type AgentsResponse struct {
	Agents []Agent `json:"agents"`
}
type CreateAgentRequest struct {
	Text      string         `json:"text,omitempty"`
	Model     string         `json:"model,omitempty"`
	Effort    string         `json:"effort,omitempty" jsonschema:"enum=low,enum=medium,enum=high"`
	Budget    *Money         `json:"budget,omitempty"`
	Approvals string         `json:"approvals,omitempty" jsonschema:"enum=risky,enum=everything,enum=none"`
	Context   []AgentContext `json:"context,omitempty"`
	Start     string         `json:"start" jsonschema:"enum=send,enum=empty"`
	Expect    *AgentExpect   `json:"expect,omitempty"`
}
type PatchAgentRequest struct {
	Name   *string      `json:"name,omitempty"`
	Model  *string      `json:"model,omitempty"`
	Effort *string      `json:"effort,omitempty" jsonschema:"enum=low,enum=medium,enum=high"`
	Budget *Money       `json:"budget,omitempty"`
	Pinned *bool        `json:"pinned,omitempty"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type CloseAgentRequest struct {
	StopRunning bool         `json:"stop_running,omitempty"`
	Expect      *AgentExpect `json:"expect,omitempty"`
}
type CloseIdleRequest struct {
	IDs    []string     `json:"ids"`
	Expect *AgentExpect `json:"expect,omitempty"`
}
type CloseIdleResponse struct {
	Closed []string `json:"closed"`
}

func agentContract() resourceContract {
	return resourceContract{Name: "agent", Types: []any{AgentsResponse{}, CreateAgentRequest{}, PatchAgentRequest{}, CloseAgentRequest{}, CloseIdleRequest{}, CloseIdleResponse{}, Agent{}, AgentContext{}, AgentEvent{}}, Routes: agentContractRoutes, Events: []eventContract{{"agent.created", AgentEvent{}}, {"agent.changed", AgentEvent{}}, {"agent.removed", AgentEvent{}}}}
}
