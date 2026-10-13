package appapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	factoryapi "github.com/Agent-Field/codeaf/internal/factory/api"
)

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var factoryContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}/hand-off/{card}", Response: "HandOffCard"}, handle: (*Server).factoryHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/hand-off/{card}/not-now", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).factoryHandler},

	{RouteContract: RouteContract{Method: "GET", Path: "/v1/factory-setup/{id}", Response: "FactorySetup"}, handle: (*Server).nativeFactoryHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/v1/factory-setup/{id}", Request: "FactorySetupRequest", Response: "FactorySetup"}, handle: (*Server).nativeFactoryHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/v1/factory-setup", Request: "FactorySetupRequest", Response: "FactorySetup"}, handle: (*Server).nativeFactoryHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/v1/factory-access", Response: "FactoryAccess"}, handle: (*Server).nativeFactoryHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/v1/factory-access", Request: "FactoryAccessRequest", Response: "FactoryAccess"}, handle: (*Server).nativeFactoryHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/v1/factory-days/today", Response: "FactoryDay"}, handle: (*Server).nativeFactoryHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/factories", Request: "", Response: "FactoriesResponse"}, handle: (*Server).factoryHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/factories/solo", Request: "ExpectRequest", Response: "Factory"}, handle: (*Server).factoryHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/factories", Request: "AttachFactoryRequest", Response: "Factory"}, handle: (*Server).factoryHandler},
	{RouteContract: RouteContract{Method: "DELETE", Path: "/app/v1/factories/{id}", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).factoryHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/hand-off", Request: "ExpectRequest", Response: "HandOffCard"}, handle: (*Server).factoryHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/tasks/{tid}/hand-off", Request: "ExpectRequest", Response: "HandOffCard"}, handle: (*Server).factoryHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/hand-off/{card}/send", Request: "SendHandOffRequest", Response: "HandOffResponse"}, handle: (*Server).factoryHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/power", Request: "PowerRequest", Response: "ActionResponse"}, handle: (*Server).factoryHandler},
}

// Factory mutations share a lock so a card cannot send twice or outlive Leave.
func (s *Server) factoryHandler(w http.ResponseWriter, r *http.Request) {
	s.idempotent(s.serveFactory)(w, r)
}

// Native setup routes use the same mounted handler as the floor, so HTTP guards stay in the factory.
func (s *Server) nativeFactoryHandler(w http.ResponseWriter, r *http.Request) {
	f := s.options.Factories
	if f == nil {
		fail(w, 404, "no_factory", "start a solo factory on this machine")
		return
	}
	if r.URL.Path == "/v1/factory-access" {
		f.openAccess(w, r)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/v1/factory-setup") {
		id := r.PathValue("id")
		var body factoryapi.FactorySetupRequest
		if r.Method == "POST" {
			if !decodeFactory(w, r, &body) {
				return
			}
			data, _ := json.Marshal(body)
			r.Body = io.NopCloser(bytes.NewReader(data))
			if id == "" {
				id = body.ID
			} else if body.ID != "" && body.ID != id {
				fail(w, 422, "bad_input", "setup id does not match the route")
				return
			}
		}
		if id == "" {
			fail(w, 422, "bad_input", "setup needs a factory id")
			return
		}
		if f.teamSetup(w, r, id, body) {
			return
		}
	}
	f.mountMu.RLock()
	handler := f.handler
	f.mountMu.RUnlock()
	if handler == nil {
		fail(w, 404, "no_factory", "start a solo factory on this machine")
		return
	}
	handler.ServeHTTP(w, r)
}
