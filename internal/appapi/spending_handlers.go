package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var spendingContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/spending", Request: "", Response: "SpendingResponse"}, handle: (*Server).spendingHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/spending/today", Request: "", Response: "SpendingToday"}, handle: (*Server).spendingHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/caps/raise", Request: "RaiseCapRequest", Response: "ActionResponse"}, handle: (*Server).spendingHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) spendingHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
