package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var factoryContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/factories", Request: "", Response: "FactoriesResponse"}, handle: (*Server).factoryHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/factories/solo", Request: "ExpectRequest", Response: "Factory"}, handle: (*Server).factoryHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/factories", Request: "AttachFactoryRequest", Response: "Factory"}, handle: (*Server).factoryHandler, reserved: true},
	{RouteContract: RouteContract{Method: "DELETE", Path: "/app/v1/factories/{id}", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).factoryHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/hand-off", Request: "ExpectRequest", Response: "HandOffCard"}, handle: (*Server).factoryHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/tasks/{tid}/hand-off", Request: "ExpectRequest", Response: "HandOffCard"}, handle: (*Server).factoryHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/hand-off/{card}/send", Request: "SendHandOffRequest", Response: "HandOffResponse"}, handle: (*Server).factoryHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/power", Request: "PowerRequest", Response: "ActionResponse"}, handle: (*Server).factoryHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) factoryHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
