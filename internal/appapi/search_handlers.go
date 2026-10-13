package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var searchContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/search", Request: "", Response: "SearchResponse"}, handle: (*Server).searchHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/history", Request: "", Response: "HistoryResponse"}, handle: (*Server).searchHandler, reserved: true},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/activity", Request: "", Response: "ActivityResponse"}, handle: (*Server).searchHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) searchHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
