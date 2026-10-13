package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var memberContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/members", Request: "CreateMemberRequest", Response: "Member"}, handle: (*Server).memberHandler, reserved: true},
	{RouteContract: RouteContract{Method: "PATCH", Path: "/app/v1/crews/{cid}/members/{mid}", Request: "PatchMemberRequest", Response: "Member"}, handle: (*Server).memberHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/members/{mid}/pause", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).memberHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/members/{mid}/resume", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).memberHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/members/{mid}/remove", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).memberHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/crews/{cid}/members/{mid}/move", Request: "MoveMemberRequest", Response: "Member"}, handle: (*Server).memberHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) memberHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
