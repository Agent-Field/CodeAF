package appapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type inboxForwardKey struct{}

// Source answer routes enter the same hold as Inbox. Reading and Later remain
// with their resource lane; the held callback bypasses only this coordinator.
func (s *Server) inboxSourceGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Context().Value(inboxForwardKey{}) != nil {
			next.ServeHTTP(w, r)
			return
		}
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/app/v1/"), "/")
		if !(len(parts) == 3 && parts[0] == "asks" && parts[2] == "answer") && !(len(parts) == 2 && parts[0] == "permissions") {
			next.ServeHTTP(w, r)
			return
		}
		items, err := s.RefreshInbox(r.Context())
		if err != nil {
			agentFailure(w, err)
			return
		}
		id := ""
		for _, item := range items {
			if item.Source.Factory != "" || !strings.HasPrefix(item.ID, "ask:") {
				continue
			}
			token := strings.TrimPrefix(item.ID, "ask:")
			bits := strings.Split(token, ":")
			if len(bits) == 3 {
				kind, _ := url.QueryUnescape(bits[1])
				ref, _ := url.QueryUnescape(bits[2])
				token = kind + ":" + ref
			}
			if token == parts[1] {
				id = item.ID
				break
			}
		}
		if id == "" {
			next.ServeHTTP(w, r)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 32<<10))
		if err != nil {
			fail(w, 422, "bad_input", "answer exceeds 32 KiB")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		var probe InboxAnswerRequest
		_ = json.Unmarshal(raw, &probe)
		if probe.Verb == "later" {
			next.ServeHTTP(w, r)
			return
		}
		s.idempotent(func(w http.ResponseWriter, r *http.Request) {
			var answer InboxAnswerRequest
			if !decodeInbox(w, r, &answer) {
				return
			}
			reply, err := s.AnswerInbox(r.Context(), id, answer)
			if err != nil {
				agentFailure(w, err)
				return
			}
			write(w, 200, ActionResponse{OK: reply.OK, Receipt: reply.Receipt})
		})(w, r)
	})
}
func inboxForwardContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, inboxForwardKey{}, true)
}
