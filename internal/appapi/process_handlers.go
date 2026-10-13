package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var processContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/processes", Request: "", Response: "ProcessesResponse"}, handle: (*Server).processHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/processes/{pid}/output", Request: "", Response: "ProcessOutputResponse"}, handle: (*Server).processHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/processes/{pid}/stop", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).processHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/processes/{pid}/rerun", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).processHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/processes/stop-all", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).processHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) processHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
