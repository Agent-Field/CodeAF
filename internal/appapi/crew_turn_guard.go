package appapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Crew admission wraps the turn resource without changing its handler. The
// transport guard remains outside this adapter, so rejected credentials never
// reach membership state or request-body decoding.
func (s *Server) crewTurnGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if r.Method != "POST" || len(parts) != 5 || parts[0] != "app" || parts[1] != "v1" || parts[2] != "agents" || parts[4] != "turn" || s.options.Agents == nil {
			next.ServeHTTP(w, r)
			return
		}
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			fail(w, 422, "bad_input", "this action needs one JSON object")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(raw))
		if len(r.Header.Values("Idempotency-Key")) > 1 {
			next.ServeHTTP(w, r)
			return
		}
		key := r.Header.Get("Idempotency-Key")
		// A replay retains its original answer even if the member was paused later.
		if key != "" {
			s.mutations.Lock()
			reply, ok := s.mutations.replies[r.URL.Path+"\x00"+key]
			s.mutations.Unlock()
			if ok {
				if reply.fingerprint != sha256.Sum256(raw) {
					fail(w, 409, "conflict", "idempotency key was used with different input")
				} else {
					sendReply(w, reply)
				}
				return
			}
		}
		if err = s.options.Agents.CrewAllowsMessage(parts[3]); err != nil {
			agentFailure(w, err)
			return
		}
		captured := &replyWriter{header: make(http.Header)}
		next.ServeHTTP(captured, r)
		var response TurnResponse
		var body TurnRequest
		if captured.code == 200 && json.Unmarshal(captured.Bytes(), &response) == nil && response.Accepted && json.Unmarshal(raw, &body) == nil {
			identity := ""
			if key != "" {
				identity = fmt.Sprintf("%x", sha256.Sum256([]byte(s.options.Epoch+"\x00"+key)))
			}
			if err = s.options.Agents.recordCrewMessage(parts[3], body.Text, identity); err != nil {
				_, _ = s.Events().Publish(EventTurnEntry, TurnEntryEvent{Agent: parts[3], Entry: DisplayEntry{Kind: "notice", Text: "Message accepted · the crew log could not be saved"}})
			}
		}
		sendReply(w, mutationReply{code: captured.code, header: captured.header, body: captured.Bytes()})
	})
}
