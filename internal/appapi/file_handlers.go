package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var fileContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}/files/text", Request: "", Response: "FileTextResponse"}, handle: (*Server).fileHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}/files/stat", Request: "", Response: "FileStatsResponse"}, handle: (*Server).fileHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}/files/find", Request: "", Response: "FileFindResponse"}, handle: (*Server).fileHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}/files/changes", Request: "", Response: "FileChangesResponse"}, handle: (*Server).fileHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}/files/diff", Request: "", Response: "FileDiffResponse"}, handle: (*Server).fileHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) fileHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
