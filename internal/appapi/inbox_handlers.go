package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var inboxContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/inbox", Request: "", Response: "InboxResponse"}, handle: (*Server).inboxHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/inbox/{id}/answer", Request: "InboxAnswerRequest", Response: "InboxAnswerResponse"}, handle: (*Server).inboxHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/inbox/{id}/undo", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).inboxHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) inboxHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
