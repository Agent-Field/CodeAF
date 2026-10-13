package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var aiContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/ai/call", Request: "AICallRequest", Response: "AICallResponse"}, handle: (*Server).aiHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) aiHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
