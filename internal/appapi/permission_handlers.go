package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var permissionContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/permissions/{id}", Request: "PermissionRequest", Response: "ActionResponse"}, handle: (*Server).permissionHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/rules", Request: "", Response: "RulesResponse"}, handle: (*Server).permissionHandler, reserved: true},
	{RouteContract: RouteContract{Method: "DELETE", Path: "/app/v1/rules/{id}", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).permissionHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/rules/{id}/restore", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).permissionHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/rules/{id}/why", Request: "", Response: "RuleWhyResponse"}, handle: (*Server).permissionHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) permissionHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
