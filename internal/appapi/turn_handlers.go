package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var turnContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/turn", Request: "TurnRequest", Response: "TurnResponse"}, handle: (*Server).turnHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/queue/{qid}", Request: "TextRequest", Response: "ActionResponse"}, handle: (*Server).turnHandler, reserved: true},
	{RouteContract: RouteContract{Method: "DELETE", Path: "/app/v1/agents/{id}/queue/{qid}", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).turnHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/stop", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).turnHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/retry", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).turnHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/switch-model", Request: "ModelRequest", Response: "ActionResponse"}, handle: (*Server).turnHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/move-reply-to-task", Request: "ExpectRequest", Response: "MoveReplyResponse"}, handle: (*Server).turnHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}/entries", Request: "", Response: "EntriesResponse"}, handle: (*Server).turnHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}/tools/{call_id}", Request: "", Response: "ToolResponse"}, handle: (*Server).turnHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) turnHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
