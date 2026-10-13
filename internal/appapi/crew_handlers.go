package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var crewContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/crews", Request: "", Response: "CrewsResponse"}, handle: (*Server).crewHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/crews/{cid}", Request: "", Response: "CrewResponse"}, handle: (*Server).crewHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/bring-in-helpers", Request: "ExpectRequest", Response: "HelperProposal"}, handle: (*Server).crewHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews", Request: "CreateCrewRequest", Response: "Crew"}, handle: (*Server).crewHandler, reserved: true},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/crews/{cid}", Request: "PatchCrewRequest", Response: "Crew"}, handle: (*Server).crewHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/replace-manager", Request: "ReplaceManagerRequest", Response: "Crew"}, handle: (*Server).crewHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) crewHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
