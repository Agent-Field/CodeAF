package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var memoryContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/memory", Request: "", Response: "MemoryResponse"}, handle: (*Server).memoryHandler, reserved: true},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/memory/{id}", Request: "PatchMemoryRequest", Response: "MemoryItem"}, handle: (*Server).memoryHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/memory/{id}/forget", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).memoryHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/memory/{id}/restore", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).memoryHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) memoryHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
