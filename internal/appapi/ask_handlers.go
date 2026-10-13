package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var askContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/asks/{id}", Request: "", Response: "Ask"}, handle: (*Server).askHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/asks/{id}/answer", Request: "AnswerAskRequest", Response: "ActionResponse"}, handle: (*Server).askHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) askHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
