package appapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"sync"

	"github.com/Agent-Field/codeaf/internal/config"
)

type settingsState struct{ sync.Mutex }

type settingsExpect = SettingsExpect

func (s *Server) registerSettings() {
	s.mux.HandleFunc("/app/v1/settings", s.profileHandler(s.settings))
	s.mux.HandleFunc("/app/v1/setup", s.profileHandler(s.idempotent(s.setup)))
	s.registerModels()
	s.registerSignIn()
}

func acceptMethod(w http.ResponseWriter, r *http.Request, methods ...string) bool {
	for _, method := range methods {
		if r.Method == method {
			return true
		}
	}
	allow := ""
	for i, method := range methods {
		if i > 0 {
			allow += ", "
		}
		allow += method
	}
	w.Header().Set("Allow", allow)
	fail(w, 405, "not_allowed", "this route does not accept that method")
	return false
}

func decodeObject(w http.ResponseWriter, r *http.Request, value any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	decoder.DisallowUnknownFields()
	var object map[string]json.RawMessage
	if decoder.Decode(&object) != nil || object == nil || decoder.Decode(new(any)) != io.EOF {
		fail(w, 422, "bad_input", "needs one JSON object of at most 32 KiB")
		return false
	}
	raw, _ := json.Marshal(object)
	strict := json.NewDecoder(bytes.NewReader(raw))
	strict.DisallowUnknownFields()
	if strict.Decode(value) != nil {
		fail(w, 422, "bad_input", "unknown field or invalid value")
		return false
	}
	return true
}

func (s *Server) readSettings(w http.ResponseWriter) (map[string]any, string, bool) {
	values, version, err := config.AppSettingsAt(s.options.ProfileDir)
	if err != nil {
		fail(w, 500, "conflict", "could not read saved settings")
		return nil, "", false
	}
	w.Header().Set("ETag", `"`+version+`"`)
	return values, version, true
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request) {
	if !acceptMethod(w, r, "GET", "PATCH") {
		return
	}
	s.settingsState.Lock()
	defer s.settingsState.Unlock()
	if r.Method == "GET" {
		values, _, ok := s.readSettings(w)
		if ok {
			write(w, 200, values)
		}
		return
	}
	var patch map[string]json.RawMessage
	if !decodeObject(w, r, &patch) {
		return
	}
	expect, ok := takeExpect(w, patch)
	if !ok {
		return
	}
	if len(patch) == 0 {
		fail(w, 422, "bad_input", "settings patch needs at least one setting")
		return
	}
	if !s.modelsAllowed(w, r, patch) {
		return
	}
	if err := config.ApplyAppSettings(s.options.ProfileDir, patch, expect); err != nil {
		settingError(w, err)
		return
	}
	s.settingsChanged(patch)
	values, _, ok := s.readSettings(w)
	if ok {
		write(w, 200, values)
	}
}

func takeExpect(w http.ResponseWriter, patch map[string]json.RawMessage) (string, bool) {
	raw, ok := patch["expect"]
	if !ok {
		return "", true
	}
	delete(patch, "expect")
	var expect settingsExpect
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if string(raw) == "null" || decoder.Decode(&expect) != nil || expect.State != "" {
		fail(w, 422, "bad_input", "expect needs a settings version")
		return "", false
	}
	return expect.Version, true
}

func settingError(w http.ResponseWriter, err error) {
	if errors.Is(err, config.ErrAppStale) {
		fail(w, 409, "stale", err.Error())
		return
	}
	// Validation errors name only registered non-secret settings and values.
	fail(w, 422, "bad_input", err.Error())
}

func (s *Server) settingsChanged(patch map[string]json.RawMessage) {
	keys := make([]string, 0, len(patch))
	for key := range patch {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	_, _ = s.events.Publish(EventSettingsChanged, map[string]any{"keys": keys})
}

func validSettingsExpect(w http.ResponseWriter, expect *settingsExpect) bool {
	if expect != nil && expect.State != "" {
		fail(w, 422, "bad_input", "expect needs a settings version")
		return false
	}
	return true
}

// New agents resolve defaults inside the binary. Existing agents keep their
// choices, and an explicit creation value wins over the installation's row.
func (s *Server) fillAgentDefaults(body *agentMutation) error {
	if s.options.ProfileDir == "" {
		return nil
	}
	values, _, err := config.AppSettingsAt(s.options.ProfileDir)
	if err != nil {
		return err
	}
	if body.Model == nil {
		model := values["models.default"].(string)
		if model != "" {
			body.Model = &model
		}
	}
	if body.Effort == nil {
		effort := values["models.effort"].(string)
		body.Effort = &effort
	}
	if body.Budget == nil {
		raw, _ := json.Marshal(values["spending.per_agent"])
		var money Money
		if err := json.Unmarshal(raw, &money); err != nil {
			return err
		}
		body.Budget = &money
	}
	if body.Approvals == "" {
		body.Approvals = map[string]string{"ask_risky": "risky", "ask_everything": "everything", "dont_ask": "none"}[values["permissions.mode"].(string)]
	}
	return nil
}

// Contract routes remain registered even in transport-only embeddings, while
// an absent profile door cannot fall through to the person's ordinary home.
func (s *Server) profileHandler(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.options.ProfileDir == "" {
			fail(w, 404, "not_found", "this engine has no app settings")
			return
		}
		next(w, r)
	}
}
