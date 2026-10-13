package appapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// This table belongs to the resource lane, so implementing it needs no shared router edit.
var inboxContractRoutes = []contractRoute{
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/inbox/{id}", Response: "InboxItem"}, handle: (*Server).inboxHandler},
	{RouteContract: RouteContract{Method: "GET", Path: "/app/v1/inbox", Response: "InboxResponse"}, handle: (*Server).inboxHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/inbox/{id}/answer", Request: "InboxAnswerRequest", Response: "InboxAnswerResponse"}, handle: (*Server).inboxHandler},
	{RouteContract: RouteContract{Method: "POST", Path: "/app/v1/inbox/{id}/undo", Request: "ExpectRequest", Response: "ActionResponse"}, handle: (*Server).inboxHandler},
}

func (s *Server) inboxHandler(w http.ResponseWriter, r *http.Request) {
	s.idempotent(s.serveInbox)(w, r)
}
func (s *Server) serveInbox(w http.ResponseWriter, r *http.Request) {
	if r.Method == "GET" {
		if id := r.PathValue("id"); id != "" {
			item, err := s.ReadInbox(r.Context(), id)
			if err != nil {
				agentFailure(w, err)
				return
			}
			write(w, 200, item)
			return
		}
		items, err := s.RefreshInbox(r.Context())
		if err != nil {
			agentFailure(w, err)
			return
		}
		write(w, 200, InboxResponse{Items: items})
		return
	}
	var answer InboxAnswerRequest
	if !decodeInbox(w, r, &answer) {
		return
	}
	id := r.PathValue("id")
	if strings.HasSuffix(r.URL.Path, "/undo") {
		if answer.Choice != "" || answer.Words != "" || answer.Verb != "" || answer.Rule != nil || answer.Items != nil {
			fail(w, 422, "bad_input", "Undo takes only expect")
			return
		}
		if err := s.UndoInbox(r.Context(), id, answer.Expect); err != nil {
			agentFailure(w, err)
			return
		}
		write(w, 200, ActionResponse{OK: true})
		return
	}
	reply, err := s.AnswerInbox(r.Context(), id, answer)
	if err != nil {
		agentFailure(w, err)
		return
	}
	write(w, 200, reply)
}
func decodeInbox(w http.ResponseWriter, r *http.Request, answer *InboxAnswerRequest) bool {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
	if err != nil {
		fail(w, 422, "bad_input", "needs one JSON object of at most 32 KiB")
		return false
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		fail(w, 422, "bad_input", "needs one JSON object")
		return false
	}
	for _, value := range object {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			fail(w, 422, "bad_input", "answer fields cannot be null")
			return false
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(answer) != nil || !errors.Is(decoder.Decode(new(any)), io.EOF) {
		fail(w, 422, "bad_input", "invalid answer fields")
		return false
	}
	return true
}

// A local forward uses the source's existing route without traversing the
// transport guard again. It never makes an HTTP call with a launch credential.
func (s *Server) forwardInbox(ctx context.Context, path string, body any) (InboxAnswerResponse, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return InboxAnswerResponse{}, err
	}
	r, err := http.NewRequestWithContext(inboxForwardContext(ctx), "POST", path, bytes.NewReader(raw))
	if err != nil {
		return InboxAnswerResponse{}, err
	}
	w := &replyWriter{header: make(http.Header)}
	s.mux.ServeHTTP(w, r)
	if w.code < 200 || w.code >= 300 {
		var failure struct{ Error, Reason string }
		_ = json.Unmarshal(w.Bytes(), &failure)
		return InboxAnswerResponse{}, agentError(w.code, failure.Reason, failure.Error)
	}
	var reply InboxAnswerResponse
	_ = json.Unmarshal(w.Bytes(), &reply)
	reply.OK = true
	return reply, nil
}
