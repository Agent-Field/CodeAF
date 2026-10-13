package appapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/Agent-Field/codeaf/internal/config"
)

// Canonical Settings POSTs share their resource's validation and mutation doors.
func (s *Server) canonicalSettingsHandler(w http.ResponseWriter, r *http.Request) {
	s.profileHandler(s.idempotent(s.serveCanonicalSettings))(w, r)
}
func (s *Server) serveCanonicalSettings(w http.ResponseWriter, r *http.Request) {
	clone := r.Clone(r.Context())
	switch {
	case strings.HasSuffix(r.URL.Path, "/poll"):
		var body EmptyRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		s.settingsState.Lock()
		defer s.settingsState.Unlock()
		values, _, err := config.AppSettingsAt(s.options.ProfileDir)
		if err != nil {
			settingError(w, err)
			return
		}
		row, ok := s.account(r.PathValue("id"), values)
		if !ok {
			fail(w, 404, "not_found", "service not found")
			return
		}
		state := "revoked"
		if row.State == "connected" {
			state = "connected"
		}
		s.accounts.Lock()
		if s.accounts.flows[row.ID] != nil {
			state = "waiting"
		}
		s.accounts.Unlock()
		// Polling reports progress without consuming the native core's credential claim.
		write(w, 200, ServicePollResponse{State: state})
		return
	case strings.HasPrefix(r.URL.Path, "/app/v1/accounts/"):
		if strings.HasSuffix(r.URL.Path, "/disconnect") {
			var body EmptyRequest
			if !decodeFactory(w, r, &body) {
				return
			}
			clone.Method = "DELETE"
		} else {
			clone.Method = "PATCH"
			clone.SetPathValue("aid", r.PathValue("action_id"))
		}
		settingsReceipt(w, clone, s.serveAccount)
	case strings.HasPrefix(r.URL.Path, "/app/v1/mcp/"):
		clone.Method = "PATCH"
		settingsReceipt(w, clone, s.serveMcp)
	case strings.Contains(r.URL.Path, "/repos/"):
		clone.Method = "PATCH"
		settingsReceipt(w, clone, s.serveFactorySettings)
	case strings.Contains(r.URL.Path, "/links/"):
		var body EmptyRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		clone.Method = "DELETE"
		settingsReceipt(w, clone, s.serveFactorySettings)
	case strings.HasSuffix(r.URL.Path, "/advanced"), strings.HasSuffix(r.URL.Path, "/runner"):
		clone.Method = "PATCH"
		var body PatchFactorySettingsRequest
		if !decodeFactory(w, r, &body) {
			return
		}
		if strings.HasSuffix(r.URL.Path, "/advanced") {
			if body.RunnerTimeoutSeconds == nil || body.KeepOutputDays != nil || body.KeepOutputMB != nil {
				fail(w, 422, "bad_input", "advanced needs only runner_timeout_s")
				return
			}
		} else if body.RunnerTimeoutSeconds != nil || body.KeepOutputDays == nil && body.KeepOutputMB == nil {
			fail(w, 422, "bad_input", "runner needs keep_output_days or keep_output_mb")
			return
		}
		raw, _ := json.Marshal(body)
		clone.Body = io.NopCloser(bytes.NewReader(raw))
		settingsReceipt(w, clone, s.serveFactorySettings)
	default:
		fail(w, 404, "not_found", "no such Settings route")
	}
}

// A successful mutation returns its declared receipt; resource reads remain available separately.
func settingsReceipt(w http.ResponseWriter, r *http.Request, handler http.HandlerFunc) {
	captured := &replyWriter{header: make(http.Header)}
	handler(captured, r)
	if captured.code >= 200 && captured.code < 300 {
		write(w, 200, ActionResponse{OK: true})
		return
	}
	sendReply(w, mutationReply{code: captured.code, header: captured.header, body: captured.Bytes()})
}
