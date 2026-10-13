package appapi

import (
	"net/http"
	"strings"
)

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var processContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/processes", Response: "ProcessesResponse"}, handle: (*Server).processHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/processes/{pid}/output", Response: "ProcessOutputResponse"}, handle: (*Server).processHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/processes/{pid}/stop", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).processHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/processes/{pid}/rerun", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).processHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/agents/{id}/processes/stop-all", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).processHandler},
}

func (s *Server) processHandler(w http.ResponseWriter, r *http.Request) {
	if s.options.Agents == nil {
		fail(w, 503, "not_allowed", "the agent registry is unavailable")
		return
	}
	s.options.Agents.mu.Lock()
	s.options.Agents.events = s.events
	s.options.Agents.mu.Unlock()
	if r.Method == "POST" {
		if len(r.Header.Values("Idempotency-Key")) > 1 {
			fail(w, 422, "bad_input", "use one Idempotency-Key")
			return
		}
		s.idempotent(s.mutateProcess)(w, r)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/output") {
		s.processOutput(w, r)
		return
	}
	if _, err := matchProcess(Process{}, r.URL.Query().Get("q")); err != nil {
		fail(w, 422, "bad_input", err.Error())
		return
	}
	rows, err := s.options.Agents.processes(r.Context(), r.URL.Query().Get("agent"))
	if err != nil {
		agentFailure(w, err)
		return
	}
	state := r.URL.Query().Get("state")
	if state != "" && state != "ended" && !processState(state) {
		fail(w, 422, "bad_input", "unknown process state")
		return
	}
	out := ProcessesResponse{}
	for _, row := range rows {
		if state != "" && state != row.Process.State && !(state == "ended" && row.Process.State != "running") {
			continue
		}
		match, err := matchProcess(row.Process, r.URL.Query().Get("q"))
		if err != nil {
			fail(w, 422, "bad_input", err.Error())
			return
		}
		if match {
			out = append(out, row.Process)
		}
	}
	write(w, 200, out)
}
