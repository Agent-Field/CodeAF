package appapi

import (
	"encoding/json"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/config"
)

type SetupView = SetupResponse

func (s *Server) setupView(values map[string]any) SetupView {
	step := config.AppSetupStepAt(s.options.ProfileDir)
	hasKey := false
	if s.options.Credentials != nil {
		keys, _ := s.options.Credentials.Snapshot()
		for id, key := range keys {
			if key != "" && id != "github" {
				hasKey = true
			}
		}
	}
	raw, _ := json.Marshal(values["spending.day_cap"])
	var cap Money
	_ = json.Unmarshal(raw, &cap)
	return SetupView{Step: step, HasKey: hasKey, DefaultModel: values["models.default"].(string), DayCap: &cap, Complete: step == 5}
}

func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	if !acceptMethod(w, r, "GET", "POST") {
		return
	}
	s.settingsState.Lock()
	defer s.settingsState.Unlock()
	values, version, ok := s.readSettings(w)
	if !ok {
		return
	}
	if r.Method == "GET" {
		write(w, 200, s.setupView(values))
		return
	}
	var body struct {
		Step         int             `json:"step"`
		DefaultModel *string         `json:"default_model,omitempty"`
		DayCap       json.RawMessage `json:"day_cap,omitempty"`
		Expect       *settingsExpect `json:"expect,omitempty"`
	}
	if !decodeObject(w, r, &body) {
		return
	}
	if !validSettingsExpect(w, body.Expect) {
		return
	}
	if body.Expect != nil && body.Expect.Version != "" && body.Expect.Version != version {
		settingError(w, config.ErrAppStale)
		return
	}
	current := config.AppSetupStepAt(s.options.ProfileDir)
	if body.Step < 1 || body.Step > 5 {
		fail(w, 422, "bad_input", "setup step must be from 1 to 5")
		return
	}
	if body.Step > current+1 || current == 5 && body.Step != 5 {
		fail(w, 409, "conflict", "setup advances one step at a time")
		return
	}
	patch := map[string]json.RawMessage{}
	if body.DefaultModel != nil {
		raw, _ := json.Marshal(*body.DefaultModel)
		patch["models.default"] = raw
	}
	if len(body.DayCap) > 0 {
		patch["spending.day_cap"] = body.DayCap
	}
	if len(patch) > 0 && current != 3 {
		fail(w, 422, "bad_input", "choose defaults at setup step 3")
		return
	}
	if body.Step >= 3 {
		if !s.setupView(values).HasKey {
			fail(w, 409, "conflict", "connect a model provider before continuing")
			return
		}
		models := s.modelList(r.Context())
		available := []CatalogModel{}
		for _, row := range models {
			if row.Available {
				available = append(available, row)
			}
		}
		if len(available) == 0 {
			fail(w, 409, "conflict", "a connected provider must return models before continuing")
			return
		}
		if current == 2 && body.Step == 3 && values["models.default"] == "" {
			raw, _ := json.Marshal(available[0].ID)
			patch["models.default"] = raw
		}
		if current == 3 && body.Step == 4 {
			if _, set := patch["spending.day_cap"]; !set {
				raw, _ := json.Marshal(values["spending.day_cap"])
				patch["spending.day_cap"] = raw
			}
			if _, set := patch["models.default"]; !set {
				raw, _ := json.Marshal(values["models.default"])
				patch["models.default"] = raw
			}
			if !s.modelsAllowed(w, r, patch) {
				return
			}
		}
	}
	if !s.modelsAllowed(w, r, patch) {
		return
	}
	// Check the version read above even when the caller did not provide expect.
	// A second engine or terminal edit between read and write must win honestly.
	if err := config.WriteAppSetup(s.options.ProfileDir, body.Step, patch, version); err != nil {
		settingError(w, err)
		return
	}
	s.settingsChanged(patch)
	values, _, ok = s.readSettings(w)
	if ok {
		write(w, 200, s.setupView(values))
	}
}
