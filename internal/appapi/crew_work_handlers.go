package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var crewWorkContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/wrap-up", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).crewWorkHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/keep-going", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).crewWorkHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/finish", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).crewWorkHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/reopen", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).crewWorkHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/archive", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).crewWorkHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/bring-in-helpers", Request: "ExpectRequest", Response: "HelperProposal"}, handle: (*Server).crewWorkHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/crews/{cid}/report", Request: "", Response: "CrewReport"}, handle: (*Server).crewWorkHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/crews/{cid}/log", Request: "", Response: "CrewLogResponse"}, handle: (*Server).crewWorkHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/items", Request: "CrewItemRequest", Response: "CrewItem"}, handle: (*Server).crewWorkHandler, reserved: true},
	{RouteContract: RouteContract{Method: "DELETE", Path: "/app/v1/crews/{cid}/items/{ref}", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).crewWorkHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/crews/{cid}/summary-since", Request: "", Response: "CrewSummaryResponse"}, handle: (*Server).crewWorkHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) crewWorkHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
