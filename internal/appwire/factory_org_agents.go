package appwire

import (
	"encoding/json"
	"net/http"
	"net/url"
)

// ── AN ORGANIZATION'S AGENTS (factory/serve org_agents.go) ─────────────────
//
// An agent in an organization is a name and instructions one person made in
// their space. It is theirs alone until they share it into a workstream or the
// operator approves it for the organization's list; the operator is never told
// of one its person keeps to themselves.
//
//	factory.org_agents                 OrgConn                -> {agents[]}   GET    /v1/agents
//	factory.org_agent_new              OrgAgentNewParams      -> the agent    POST   /v1/agents
//	factory.org_agent_set              OrgAgentSetParams      -> the agent    PATCH  /v1/agents/{agent}
//	factory.org_agent_delete           OrgAgentParams         -> {}           DELETE /v1/agents/{agent}
//	factory.org_agent_roster           OrgAgentRosterParams   -> the agent    PUT    /v1/agents/{agent}/roster
//	factory.org_workstream_agent_list  OrgWorkstreamParams    -> {agents[]}   GET    /v1/workstreams/{id}/agents
//	factory.org_workstream_agent_add   OrgWorkstreamAgentLink -> {agents[]}   PUT    /v1/workstreams/{id}/agents/{agent}
//	factory.org_workstream_agent_drop  OrgWorkstreamAgentLink -> {agents[]}   DELETE /v1/workstreams/{id}/agents/{agent}
//
// factory.org_workstream_agent_list is the list a task's doer is picked from
// (People and rules › Agents): each row is {id, name, kind: "agent", from:
// "space"|"org", owner, owner_name?, team?, by, at}. A chat names one with
// factory.org_workstream_chat_new's agent.
//
// An engine that serves them says "org_agents" in hello.

// FeatureOrgAgents is the hello word, exactly as the desk reads it.
const FeatureOrgAgents = "org_agents"

// OrgAgentParams names one agent.
type OrgAgentParams struct {
	OrgConn
	Agent string `json:"agent"`
}

// OrgAgentNewParams makes an agent in the caller's space.
type OrgAgentNewParams struct {
	OrgConn
	Name         string `json:"name"`
	Instructions string `json:"instructions"`
	Team         string `json:"team,omitempty"`
}

// OrgAgentSetParams changes what is sent.
type OrgAgentSetParams struct {
	OrgConn
	Agent        string  `json:"agent"`
	Name         *string `json:"name,omitempty"`
	Instructions *string `json:"instructions,omitempty"`
	Team         *string `json:"team,omitempty"`
}

// OrgAgentRosterParams asks for, approves, declines or leaves the
// organization's list.
type OrgAgentRosterParams struct {
	OrgConn
	Agent string `json:"agent"`
	On    bool   `json:"on"`
}

// OrgWorkstreamAgentLink adds an agent to a workstream, or takes it off.
type OrgWorkstreamAgentLink struct {
	OrgConn
	ID    string `json:"id"`
	Agent string `json:"agent"`
}

func orgAgentPath(agent string) (string, error) {
	agent, err := orgSegment("agent", agent)
	if err != nil {
		return "", err
	}
	return "/v1/agents/" + url.PathEscape(agent), nil
}

func orgWorkstreamAgentPath(id, agent string) (string, error) {
	path, err := orgWorkstreamPath(id)
	if err != nil {
		return "", err
	}
	agent, err = orgSegment("agent", agent)
	if err != nil {
		return "", err
	}
	return path + "/agents/" + url.PathEscape(agent), nil
}

func withFactoryOrgAgentsFeature(hello Hello, e Engine) Hello {
	if _, ok := e.(Factory); !ok {
		return hello
	}
	if _, ok := e.(FactoryOrg); ok {
		hello = withFeature(hello, FeatureOrgAgents)
	}
	return hello
}

func init() {
	methods["factory.org_agents"] = orgMethod[OrgConn, json.RawMessage]("factory.org_agents", func(OrgConn) (OrgRequest, error) {
		return OrgRequest{Method: http.MethodGet, Path: "/v1/agents"}, nil
	})
	methods["factory.org_agent_new"] = orgMethod[OrgAgentNewParams, json.RawMessage]("factory.org_agent_new", func(p OrgAgentNewParams) (OrgRequest, error) {
		body := map[string]string{"name": p.Name, "instructions": p.Instructions}
		if p.Team != "" {
			body["team"] = p.Team
		}
		return OrgRequest{Method: http.MethodPost, Path: "/v1/agents", Body: body}, nil
	})
	methods["factory.org_agent_set"] = orgMethod[OrgAgentSetParams, json.RawMessage]("factory.org_agent_set", func(p OrgAgentSetParams) (OrgRequest, error) {
		path, err := orgAgentPath(p.Agent)
		if err != nil {
			return OrgRequest{}, err
		}
		body := map[string]*string{}
		if p.Name != nil {
			body["name"] = p.Name
		}
		if p.Instructions != nil {
			body["instructions"] = p.Instructions
		}
		if p.Team != nil {
			body["team"] = p.Team
		}
		if len(body) == 0 {
			return OrgRequest{}, Errorf(CodeBadParams, "send name, instructions or team")
		}
		return OrgRequest{Method: http.MethodPatch, Path: path, Body: body}, nil
	})
	methods["factory.org_agent_delete"] = orgMethod[OrgAgentParams, json.RawMessage]("factory.org_agent_delete", func(p OrgAgentParams) (OrgRequest, error) {
		path, err := orgAgentPath(p.Agent)
		return OrgRequest{Method: http.MethodDelete, Path: path}, err
	})
	methods["factory.org_agent_roster"] = orgMethod[OrgAgentRosterParams, json.RawMessage]("factory.org_agent_roster", func(p OrgAgentRosterParams) (OrgRequest, error) {
		path, err := orgAgentPath(p.Agent)
		return OrgRequest{Method: http.MethodPut, Path: path + "/roster", Body: map[string]bool{"on": p.On}}, err
	})
	methods["factory.org_workstream_agent_list"] = orgMethod[OrgWorkstreamParams, json.RawMessage]("factory.org_workstream_agent_list", func(p OrgWorkstreamParams) (OrgRequest, error) {
		path, err := orgWorkstreamPath(p.ID)
		return OrgRequest{Method: http.MethodGet, Path: path + "/agents"}, err
	})
	methods["factory.org_workstream_agent_add"] = orgMethod[OrgWorkstreamAgentLink, json.RawMessage]("factory.org_workstream_agent_add", func(p OrgWorkstreamAgentLink) (OrgRequest, error) {
		path, err := orgWorkstreamAgentPath(p.ID, p.Agent)
		return OrgRequest{Method: http.MethodPut, Path: path}, err
	})
	methods["factory.org_workstream_agent_drop"] = orgMethod[OrgWorkstreamAgentLink, json.RawMessage]("factory.org_workstream_agent_drop", func(p OrgWorkstreamAgentLink) (OrgRequest, error) {
		path, err := orgWorkstreamAgentPath(p.ID, p.Agent)
		return OrgRequest{Method: http.MethodDelete, Path: path}, err
	})
}
