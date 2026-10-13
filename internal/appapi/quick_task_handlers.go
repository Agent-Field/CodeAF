package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var quickTaskContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}/quick-tasks", Request: "", Response: "QuickTasksResponse"}, handle: (*Server).quickTaskHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/quick-tasks/{qid}/undo", Request: "ExpectRequest", Response: "UndoQuickTaskResponse"}, handle: (*Server).quickTaskHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/quick-tasks/{qid}/promote", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).quickTaskHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/quick-tasks/{qid}/cancel", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).quickTaskHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) quickTaskHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
