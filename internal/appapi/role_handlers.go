package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var roleContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/roles", Request: "", Response: "RolesResponse"}, handle: (*Server).roleHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/roles", Request: "CreateRoleRequest", Response: "CrewRole"}, handle: (*Server).roleHandler, reserved: true},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/roles/{rid}", Request: "PatchRoleRequest", Response: "CrewRole"}, handle: (*Server).roleHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) roleHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
