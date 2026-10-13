package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var orderContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}/orders", Request: "", Response: "OrdersResponse"}, handle: (*Server).orderHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/orders/{oid}/ratify", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/orders/{oid}/not-now", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/orders/{oid}/pause", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/orders/{oid}/resume", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/orders/{oid}/stop", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/orders/{oid}/change", Request: "ChangeOrderRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/orders/pause-all", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/orders/{oid}/runs/{rid}", Request: "OrderRunRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/reminders/{oid}/change", Request: "ReminderChangeRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler, reserved: true},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/reminders/{oid}/cancel", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).orderHandler, reserved: true},
}

// The owning lane replaces this stub and clears reserved in its own table.
func (s *Server) orderHandler(w http.ResponseWriter, r *http.Request) { notBuilt(w, r) }
