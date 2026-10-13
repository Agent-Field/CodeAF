package appapi

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/Agent-Field/codeaf/internal/catalog"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/roles"
)

// CatalogModel, ModelsView and RoleView use the registered wire vocabulary.
type CatalogModel = Model
type ModelsView = ModelsResponse
type RoleView = ModelRole

func (s *Server) registerModels() {
	s.mux.HandleFunc("/app/v1/models", s.profileHandler(s.models))
	s.mux.HandleFunc("/app/v1/models/pinned", s.profileHandler(s.pinnedModels))
	s.mux.HandleFunc("/app/v1/models/default", s.profileHandler(s.defaultModel))
	s.mux.HandleFunc("/app/v1/models/roles", s.profileHandler(s.modelRoles))
	s.mux.HandleFunc("/app/v1/models/roles/{role}", s.profileHandler(s.modelRole))
}

func (s *Server) modelList(ctx context.Context) []CatalogModel {
	if s.options.ModelCatalog != nil {
		rows, err := s.options.ModelCatalog(ctx)
		if err == nil {
			return rows
		}
	}
	rows := []CatalogModel{}
	if s.options.Credentials == nil {
		return rows
	}
	s.keyState.Lock()
	defer s.keyState.Unlock()
	keys, _ := s.options.Credentials.Snapshot()
	ids := make([]string, 0, len(keys))
	for id, key := range keys {
		if key != "" && id != "github" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	if latest := s.keyState.lastProvider; latest != "" {
		for i, id := range ids {
			if id == latest {
				copy(ids[1:i+1], ids[:i])
				ids[0] = latest
				break
			}
		}
	}
	services := config.ResolveSources(s.options.ProfileDir, "", config.DefaultBaseURL)
	for _, id := range ids {
		// Launch handoffs have no listing. Discovery is deferred until a model
		// read, and a successful key check's listing is reused without a second call.
		models, known := s.keyState.models[id]
		if !known || time.Since(s.keyState.listedAt[id]) >= 15*time.Minute {
			endpoint, ok := s.keyEndpoint(id)
			if !ok {
				continue
			}
			probe, cancel := context.WithTimeout(ctx, modelsource.ProbeTimeout)
			listed, err := checkKey(probe, s.options.KeyHTTPClient, endpoint, id, keys[id])
			cancel()
			if err == nil {
				models = listed
				s.keyState.models[id] = models
				s.keyState.listedAt[id] = time.Now()
			} else if !known {
				continue
			}
		}
		connected, knownService := services.ByID(id)
		written := id
		names := map[string]string{}
		if knownService {
			if connected.Source.Written != "" {
				written = connected.Source.Written
			}
			remembered := catalog.Recall(config.CatalogOptionsFor(connected, s.options.ProfileDir))
			for _, row := range remembered.ModelsNow() {
				if row.Name != "" {
					names[row.ID] = row.Name
				}
			}
		}

		for _, bare := range models {
			model := bare
			if id != modelsource.DefaultID {
				model = written + "/" + bare
			}
			name := bare
			if names[bare] != "" {
				name = names[bare]
			}
			rows = append(rows, CatalogModel{ID: model, Name: name, Provider: id, Available: knownService, Efforts: append([]string{}, roles.Efforts...)})
		}
	}
	return rows
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) {
	if !acceptMethod(w, r, "GET") {
		return
	}
	values, _, ok := s.readSettings(w)
	if !ok {
		return
	}
	rows := s.modelList(r.Context())
	seen := map[string]bool{}
	for _, row := range rows {
		seen[row.ID] = true
	}
	// A disconnected saved choice stays visible, with its actual availability.
	saved := []string{values["models.default"].(string)}
	raw, _ := json.Marshal(values["models.pinned"])
	var pinned []string
	_ = json.Unmarshal(raw, &pinned)
	saved = append(saved, pinned...)
	for _, id := range saved {
		if id != "" && !seen[id] {
			rows = append(rows, CatalogModel{ID: id, Name: id, Available: false, Efforts: append([]string{}, roles.Efforts...)})
			seen[id] = true
		}
	}
	write(w, 200, ModelsView{Models: rows, Default: values["models.default"].(string), Pinned: pinned})
}

func (s *Server) modelsAllowed(w http.ResponseWriter, r *http.Request, patch map[string]json.RawMessage) bool {
	var wanted []string
	for key, raw := range patch {
		switch key {
		case "models.default":
			var id string
			if json.Unmarshal(raw, &id) == nil {
				wanted = append(wanted, id)
			}
		case "models.pinned":
			var ids []string
			if json.Unmarshal(raw, &ids) == nil {
				wanted = append(wanted, ids...)
			}
		case "models.roles.checks", "models.roles.planner":
			var choice config.AppRoleChoice
			if json.Unmarshal(raw, &choice) == nil {
				wanted = append(wanted, choice.Model)
			}
		}
	}
	if len(wanted) == 0 {
		return true
	}
	available := map[string]bool{}
	for _, row := range s.modelList(r.Context()) {
		if row.Available {
			available[row.ID] = true
		}
	}
	for _, id := range wanted {
		if id != "" && !available[id] {
			fail(w, 422, "bad_input", "that model is not available from a connected provider")
			return false
		}
	}
	return true
}

func (s *Server) applyModel(w http.ResponseWriter, r *http.Request, patch map[string]json.RawMessage, expect string) bool {
	if !s.modelsAllowed(w, r, patch) {
		return false
	}
	if err := config.ApplyAppSettings(s.options.ProfileDir, patch, expect); err != nil {
		settingError(w, err)
		return false
	}
	s.settingsChanged(patch)
	return true
}

func (s *Server) pinnedModels(w http.ResponseWriter, r *http.Request) {
	if !acceptMethod(w, r, "PUT") {
		return
	}
	var body struct {
		Models []string        `json:"models"`
		Expect *settingsExpect `json:"expect,omitempty"`
	}
	if !decodeObject(w, r, &body) {
		return
	}
	if !validSettingsExpect(w, body.Expect) {
		return
	}
	s.settingsState.Lock()
	defer s.settingsState.Unlock()
	raw, _ := json.Marshal(body.Models)
	if !s.applyModel(w, r, map[string]json.RawMessage{"models.pinned": raw}, expectVersion(body.Expect)) {
		return
	}
	values, _, ok := s.readSettings(w)
	if ok {
		write(w, 200, map[string]any{"pinned": values["models.pinned"]})
	}
}

func (s *Server) defaultModel(w http.ResponseWriter, r *http.Request) {
	if !acceptMethod(w, r, "PUT") {
		return
	}
	var body struct {
		Model  string          `json:"model"`
		Effort string          `json:"effort,omitempty"`
		Expect *settingsExpect `json:"expect,omitempty"`
	}
	if !decodeObject(w, r, &body) {
		return
	}
	if !validSettingsExpect(w, body.Expect) {
		return
	}
	s.settingsState.Lock()
	defer s.settingsState.Unlock()
	raw, _ := json.Marshal(body.Model)
	patch := map[string]json.RawMessage{"models.default": raw}
	if body.Effort != "" {
		raw, _ = json.Marshal(body.Effort)
		patch["models.effort"] = raw
	}
	if !s.applyModel(w, r, patch, expectVersion(body.Expect)) {
		return
	}
	values, _, ok := s.readSettings(w)
	if ok {
		write(w, 200, map[string]any{"model": values["models.default"], "effort": values["models.effort"]})
	}
}

func expectVersion(expect *settingsExpect) string {
	if expect == nil {
		return ""
	}
	return expect.Version
}

func (s *Server) rolesView(values map[string]any) []RoleView {
	result := []RoleView{}
	for _, id := range []string{"checks", "planner"} {
		raw, _ := json.Marshal(values["models.roles."+id])
		var choice config.AppRoleChoice
		_ = json.Unmarshal(raw, &choice)
		result = append(result, RoleView{ID: id, Model: choice.Model, Effort: choice.Effort})
	}
	return result
}

func (s *Server) modelRoles(w http.ResponseWriter, r *http.Request) {
	if !acceptMethod(w, r, "GET") {
		return
	}
	values, _, ok := s.readSettings(w)
	if ok {
		write(w, 200, map[string]any{"roles": s.rolesView(values)})
	}
}

func (s *Server) modelRole(w http.ResponseWriter, r *http.Request) {
	if !acceptMethod(w, r, "PUT") {
		return
	}
	id := r.PathValue("role")
	if id != "checks" && id != "planner" {
		fail(w, 404, "not_found", "no such app model role")
		return
	}
	var body struct {
		Model  string          `json:"model"`
		Effort string          `json:"effort"`
		Expect *settingsExpect `json:"expect,omitempty"`
	}
	if !decodeObject(w, r, &body) {
		return
	}
	if !validSettingsExpect(w, body.Expect) {
		return
	}
	s.settingsState.Lock()
	defer s.settingsState.Unlock()
	raw, _ := json.Marshal(config.AppRoleChoice{Model: body.Model, Effort: body.Effort})
	if !s.applyModel(w, r, map[string]json.RawMessage{"models.roles." + id: raw}, expectVersion(body.Expect)) {
		return
	}
	values, _, ok := s.readSettings(w)
	if !ok {
		return
	}
	for _, view := range s.rolesView(values) {
		if view.ID == id {
			write(w, 200, view)
			return
		}
	}
}
