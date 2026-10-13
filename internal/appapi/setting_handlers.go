package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var settingContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/settings", Request: "", Response: "Settings"}, handle: (*Server).settingHandler, reserved: true},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/settings", Request: "PatchSettingsRequest", Response: "Settings"}, handle: (*Server).settingHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/setup", Request: "", Response: "SetupResponse"}, handle: (*Server).settingHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/setup", Request: "SetupRequest", Response: "SetupResponse"}, handle: (*Server).settingHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) settingHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
