package appapi

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
)

func TestSetupResumesEachStepAndWritesTheSameSettingsKeys(t *testing.T) {
	s := settingsServer(t)
	for step := 1; step <= 5; step++ {
		w := request(t, s, "GET", "/app/v1/setup", "", bearer(), "127.0.0.1:4000")
		var view SetupView
		_ = json.Unmarshal(w.Body.Bytes(), &view)
		if view.Step != step || view.Complete != (step == 5) {
			t.Fatalf("step %d: %s", step, w.Body)
		}
		if step == 5 {
			break
		}
		body := `{"step":` + string(rune('0'+step+1)) + `}`
		if step == 3 {
			body = `{"step":4,"default_model":"vendor/a-second:free","day_cap":{"usd":"25.00"}}`
		}
		w = request(t, s, "POST", "/app/v1/setup", body, bearer(), "127.0.0.1:4000")
		if w.Code != 200 {
			t.Fatalf("advance %d: %d %s", step, w.Code, w.Body)
		}
		if step == 2 && !strings.Contains(w.Body.String(), "vendor/z-first") {
			t.Fatal("setup ranked models instead of using first returned")
		}
		options := s.options
		s.Close()
		s = New(testToken, options)
		defer s.Close()
	}
	if config.ChatModelAt(s.options.ProfileDir) != "vendor/a-second:free" {
		t.Fatal("setup wrote another default-model key")
	}
	if cap, err := config.DailyBudgetUSDAt(s.options.ProfileDir); err != nil || cap != 25 {
		t.Fatal("setup did not write the engine day cap")
	}
	w := request(t, s, "POST", "/app/v1/setup", `{"step":1}`, bearer(), "127.0.0.1:4000")
	if w.Code != 409 {
		t.Fatal("completed setup restarted")
	}
}

func TestSetupRequiresAModelKeyAndPreventsSkippedSteps(t *testing.T) {
	s := settingsServer(t)
	s.options.Credentials.Set("openrouter", "")
	s.options.Credentials.Set("github", "github-only")
	for _, body := range []string{`{"step":3}`, `{"step":4}`} {
		w := request(t, s, "POST", "/app/v1/setup", body, bearer(), "127.0.0.1:4000")
		if w.Code != 409 {
			t.Fatal("skipped welcome")
		}
	}
	_ = request(t, s, "POST", "/app/v1/setup", `{"step":2}`, bearer(), "127.0.0.1:4000")
	w := request(t, s, "POST", "/app/v1/setup", `{"step":3}`, bearer(), "127.0.0.1:4000")
	if w.Code != 409 || !strings.Contains(w.Body.String(), "connect a model provider") {
		t.Fatal("GitHub key passed model setup")
	}
	w = request(t, s, "GET", "/app/v1/setup", "", bearer(), "127.0.0.1:4000")
	if !strings.Contains(w.Body.String(), `"has_key":false`) {
		t.Fatal("setup counted GitHub as model key")
	}
}

func TestSetupDefaultDayCapIsTwentyAndBackKeepsChoices(t *testing.T) {
	s := settingsServer(t)
	for _, body := range []string{`{"step":2}`, `{"step":3}`, `{"step":4}`, `{"step":3}`} {
		w := request(t, s, "POST", "/app/v1/setup", body, bearer(), "127.0.0.1:4000")
		if w.Code != 200 {
			t.Fatal(w.Body)
		}
	}
	if cap, err := config.DailyBudgetUSDAt(s.options.ProfileDir); err != nil || cap != config.AppDefaultDayCap {
		t.Fatalf("setup cap %v %v", cap, err)
	}
	w := request(t, s, "POST", "/app/v1/setup", `{"step":4,"day_cap":{"usd":"bad"}}`, bearer(), "127.0.0.1:4000")
	if w.Code != 422 || config.AppSetupStepAt(s.options.ProfileDir) != 3 {
		t.Fatal("invalid defaults advanced setup")
	}
}

func TestSetupPostIdempotencyReplaysAndRefusesDifferentInput(t *testing.T) {
	s := settingsServer(t)
	headers := bearer()
	headers.Set("Idempotency-Key", "setup-step")
	first := request(t, s, "POST", "/app/v1/setup", `{"step":2}`, headers, "127.0.0.1:4000")
	replay := request(t, s, "POST", "/app/v1/setup", `{"step":2}`, headers, "127.0.0.1:4000")
	if first.Code != 200 || replay.Code != 200 || first.Body.String() != replay.Body.String() {
		t.Fatal("setup was not idempotent")
	}
	conflict := request(t, s, "POST", "/app/v1/setup", `{"step":3}`, headers, "127.0.0.1:4000")
	if conflict.Code != 409 {
		t.Fatal("same idempotency key accepted changed step")
	}
}
