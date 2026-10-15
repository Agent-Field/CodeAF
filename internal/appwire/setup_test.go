package appwire

import (
	"encoding/json"
	"testing"
)

func (f *fakeEngine) ListProviders() ([]Provider, error) {
	return []Provider{{ID: "openrouter", Name: "OpenRouter", SignedIn: true, KeyHint: "…a91f", SignIn: true}}, nil
}
func (f *fakeEngine) CheckKey(provider, key string) (KeyCheck, error) {
	if key == "good" {
		return KeyCheck{OK: true, Models: []Model{{ID: "m", Name: "m", Provider: provider}}}, nil
	}
	return KeyCheck{OK: false, Status: 401, Message: "No auth credentials found"}, nil
}
func (f *fakeEngine) SetKey(provider, key string) error { return nil }
func (f *fakeEngine) RemoveKey(provider string) error   { return nil }
func (f *fakeEngine) SignIn(provider string) (string, error) {
	if provider != "openrouter" {
		return "", Errorf(CodeBadParams, "%s has no browser sign-in", provider)
	}
	return "https://openrouter.ai/auth?x", nil
}
func (f *fakeEngine) Setup() (Setup, error) {
	cap := 20.0
	return Setup{Step: SetupStep{N: 1}, DayCapUSD: &cap}, nil
}
func (f *fakeEngine) SetSetup(setup Setup) (Setup, error) { return setup, nil }
func (f *fakeEngine) SetSettings(patch SettingsPatch) (Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.patch = patch
	return Settings{ProposedTaskClockS: 12, IdleCloseDays: 7}, nil
}
func (f *fakeEngine) RaiseCap(raise CapRaise) error { return nil }

func TestSetupMethodsAnswerTheirShapes(t *testing.T) {
	engine := &fakeEngine{}
	w := startWire(t, engine)
	w.next()
	result := func(line string) map[string]any {
		t.Helper()
		w.send(line)
		got := w.next()
		if got["error"] != nil {
			t.Fatalf("%s = %v", line, got)
		}
		return got["result"].(map[string]any)
	}
	providers := result(`{"id":1,"method":"providers.list"}`)["providers"].([]any)
	if p := providers[0].(map[string]any); p["id"] != "openrouter" || p["signed_in"] != true || p["key_hint"] != "…a91f" || p["sign_in"] != true {
		t.Fatalf("providers = %v", providers)
	}
	bad := result(`{"id":2,"method":"keys.check","params":{"provider":"openrouter","key":"nope"}}`)
	if bad["ok"] != false || bad["status"] != float64(401) || bad["message"] != "No auth credentials found" {
		t.Fatalf("bad key = %v", bad)
	}
	setup := result(`{"id":3,"method":"setup.get"}`)["setup"].(map[string]any)
	if setup["step"] != float64(1) || setup["day_cap_usd"] != float64(20) {
		t.Fatalf("setup = %v", setup)
	}
	done := result(`{"id":4,"method":"setup.set","params":{"step":"done","start":"agents"}}`)["setup"].(map[string]any)
	if done["step"] != "done" || done["start"] != "agents" {
		t.Fatalf("setup done = %v", done)
	}
	if url := result(`{"id":5,"method":"providers.sign_in","params":{"provider":"OpenRouter"}}`)["url"]; url == "" {
		t.Fatal("sign in answered no url")
	}
	settings := result(`{"id":6,"method":"settings.set","params":{"budget_usd":null,"day_cap_usd":40,"idle_close_days":3}}`)["settings"].(map[string]any)
	if settings["proposed_task_clock_s"] != float64(12) {
		t.Fatalf("settings = %v", settings)
	}
	engine.mu.Lock()
	patch := engine.patch
	engine.mu.Unlock()
	if patch.BudgetUSD == nil || patch.BudgetUSD.USD != nil || patch.DayCapUSD == nil || *patch.DayCapUSD.USD != 40 || *patch.IdleCloseDays != 3 || patch.DefaultModel != nil {
		t.Fatalf("patch = %+v", patch)
	}
	result(`{"id":7,"method":"caps.raise","params":{"scope":"day","to_usd":60,"for":"today"}}`)
}

func TestSetupMethodsRefuseBadParams(t *testing.T) {
	w := startWire(t, &fakeEngine{})
	w.next()
	for i, line := range []string{
		`{"id":1,"method":"keys.check","params":{"provider":"openrouter"}}`,
		`{"id":2,"method":"keys.set","params":{"provider":"openrouter","key":"a\nb"}}`,
		`{"id":3,"method":"setup.set","params":{"step":5}}`,
		`{"id":4,"method":"setup.set","params":{}}`,
		`{"id":5,"method":"settings.set","params":{"colour":"blue"}}`,
		`{"id":6,"method":"settings.set","params":{"day_cap_usd":-1}}`,
		`{"id":7,"method":"caps.raise","params":{"scope":"agent","to_usd":5,"for":"today"}}`,
		`{"id":8,"method":"caps.raise","params":{"scope":"day","to_usd":5,"for":"ever"}}`,
		`{"id":9,"method":"providers.sign_in","params":{"provider":"deepseek"}}`,
	} {
		w.send(line)
		got := w.next()
		if got["id"] != float64(i+1) || got["error"] == nil || got["error"].(map[string]any)["code"] != CodeBadParams {
			t.Fatalf("%s = %v", line, got)
		}
	}
}

func TestSetupStepIsANumberOrDone(t *testing.T) {
	for _, step := range []SetupStep{{N: 3}, {Done: true}} {
		raw, err := json.Marshal(step)
		if err != nil {
			t.Fatal(err)
		}
		var back SetupStep
		if err := json.Unmarshal(raw, &back); err != nil || back != step {
			t.Fatalf("%s read back as %+v (%v)", raw, back, err)
		}
	}
}
