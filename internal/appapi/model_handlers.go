package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var modelContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/models", Request: "", Response: "ModelsResponse"}, handle: (*Server).modelHandler, reserved: true},
	{RouteContract: RouteContract{Method: "PUT", Path: "/app/v1/models/pinned", Request: "PinnedModelsRequest", Response: "ActionResponse"}, handle: (*Server).modelHandler, reserved: true},
	{RouteContract: RouteContract{Method: "PUT", Path: "/app/v1/models/default", Request: "DefaultModelRequest", Response: "ActionResponse"}, handle: (*Server).modelHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/models/roles", Request: "", Response: "ModelRolesResponse"}, handle: (*Server).modelHandler, reserved: true},
	{RouteContract: RouteContract{Method: "PUT", Path: "/app/v1/models/roles/{role}", Request: "ModelRoleRequest", Response: "ActionResponse"}, handle: (*Server).modelHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/providers/{p}/sign-in", Request: "ExpectRequest", Response: "ProviderSignInResponse"}, handle: (*Server).modelHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) modelHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
