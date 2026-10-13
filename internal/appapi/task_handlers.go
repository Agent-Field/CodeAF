package appapi

import "net/http"

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var taskContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/agents/{id}/tasks", Request: "", Response: "TasksResponse"}, handle: (*Server).taskHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/tasks/{tid}", Request: "", Response: "TaskResponse"}, handle: (*Server).taskHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/tasks/{tid}/note", Request: "TextRequest", Response: "TaskNote"}, handle: (*Server).taskHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/tasks/{tid}/brief", Request: "TextRequest", Response: "TaskNote"}, handle: (*Server).taskHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/tasks/{tid}/pause", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).taskHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/tasks/{tid}/resume", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).taskHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/tasks/{tid}/stop", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).taskHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/tasks/{tid}/start", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).taskHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/tasks/{tid}/hold", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).taskHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/tasks/{tid}/decline", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).taskHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/tasks/{tid}/typing", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).taskHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/tasks/{tid}/model", Request: "ModelRequest", Response: "ActionResponse"}, handle: (*Server).taskHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/tasks/{tid}/decide", Request: "TaskDecisionRequest", Response: "ActionResponse"}, handle: (*Server).taskHandler},
}

// Resource actions use the shared guarded POST replay door.
func (s *Server) taskHandler(w http.ResponseWriter, r *http.Request) {
	s.idempotent(s.taskRequest)(w, r)
}
