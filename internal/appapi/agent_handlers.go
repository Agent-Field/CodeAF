package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var agentContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents", Request: "", Response: "AgentsResponse"}},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents", Request: "CreateAgentRequest", Response: "Agent"}},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}", Request: "", Response: "AgentResponse"}},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/agents/{id}", Request: "PatchAgentRequest", Response: "Agent"}},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/close", Request: "CloseAgentRequest", Response: "Agent"}},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/archive", Request: "ExpectRequest", Response: "Agent"}},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/reopen", Request: "ExpectRequest", Response: "Agent"}},

	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/close-idle", Request: "CloseIdleRequest", Response: "CloseIdleResponse"}, handle: (*Server).agentHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) agentHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
