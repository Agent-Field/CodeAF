package appwire

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
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
		`{"id":3,"method":"setup.set","params":{"step":4}}`,
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
		if err := json.Unmarshal(raw, &back); err != nil || back != step || !back.Valid() {
			t.Fatalf("%s read back as %+v (%v)", raw, back, err)
		}
	}
	// A step a four-step build kept is read, so the file it is in is not
	// refused, but it is not a step a setup can be at now.
	var kept SetupStep
	if err := json.Unmarshal([]byte(`4`), &kept); err != nil || kept != (SetupStep{N: 4}) || kept.Valid() {
		t.Fatalf("a kept 4 = %+v (%v), valid %v", kept, err, kept.Valid())
	}
	for _, bad := range []string{`0`, `-1`, `"halfway"`, `1.5`} {
		if err := json.Unmarshal([]byte(bad), &kept); err == nil {
			t.Fatalf("%s was read as a step", bad)
		}
	}
}

// SETUP IS THREE STEPS, and the schema says so: a number from one to three, or
// "done" — never the Go struct the step is held in.
func TestTheSchemaSaysSetupIsThreeSteps(t *testing.T) {
	out, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Defs map[string]struct {
			Properties map[string]json.RawMessage `json:"properties"`
		} `json:"$defs"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	for _, def := range []string{"Setup", "in.Setup"} {
		var step struct {
			AnyOf []struct {
				Type    string `json:"type"`
				Maximum int    `json:"maximum"`
				Const   string `json:"const"`
			} `json:"anyOf"`
		}
		raw := doc.Defs[def].Properties["step"]
		if err := json.Unmarshal(raw, &step); err != nil || len(step.AnyOf) != 2 ||
			step.AnyOf[0].Type != "integer" || step.AnyOf[0].Maximum != 3 || step.AnyOf[1].Const != "done" {
			t.Fatalf("%s.step in the schema = %s", def, raw)
		}
	}
}

// connectorEngine is an engine that connects every kind of provider, and
// remembers what each keys.check and keys.set handed it.
type connectorEngine struct {
	*fakeEngine
	got chan KeyParams
}

func (c *connectorEngine) CheckProvider(p KeyParams) (KeyCheck, error) {
	c.got <- p
	listed := false
	return KeyCheck{OK: true, Connection: &Connection{Address: p.Address, Region: p.Region, Listed: &listed}}, nil
}

func (c *connectorEngine) SetProvider(p KeyParams) error {
	c.got <- p
	return nil
}

// AN ENGINE THAT CONNECTS EVERY PROVIDER IS HANDED THE WHOLE REQUEST: the
// region and the address keys.check reads, the connection keys.set keeps, and
// an empty key for it to judge. One that does not is asked for a key as before.
func TestKeysCarryTheConnectionToAnEngineThatConnectsEveryProvider(t *testing.T) {
	engine := &connectorEngine{fakeEngine: &fakeEngine{}, got: make(chan KeyParams, 4)}
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go func() {
		_ = NewServer(outW).Serve(context.Background(), inR, Hello{Version: "test", PID: 42, Home: "/h"}, engine)
		outW.Close()
	}()
	t.Cleanup(func() { inW.Close() })
	w := &wire{t: t, in: inW, lines: bufio.NewScanner(outR), engine: engine.fakeEngine}
	w.next()
	w.send(`{"id":1,"method":"keys.check","params":{"provider":"Ollama","key":"","address":" http://box.local:11434/v1 ","region":"intl"}}`)
	check := w.next()
	if check["error"] != nil || check["result"].(map[string]any)["connection"].(map[string]any)["address"] != "http://box.local:11434/v1" {
		t.Fatalf("check = %v", check)
	}
	if p := <-engine.got; p.Provider != "ollama" || p.Key != "" || p.Address != "http://box.local:11434/v1" || p.Region != "intl" {
		t.Fatalf("keys.check handed %+v", p)
	}
	w.send(`{"id":2,"method":"keys.set","params":{"provider":"z-ai","key":"zai-dummy","connection":{"region":"cn","door":"coding-plan","listed":true}}}`)
	if set := w.next(); set["error"] != nil {
		t.Fatalf("set = %v", set)
	}
	if p := <-engine.got; p.Key != "zai-dummy" || p.Connection == nil || p.Connection.Region != "cn" || p.Connection.Door != "coding-plan" || p.Connection.Listed == nil || !*p.Connection.Listed {
		t.Fatalf("keys.set handed %+v", p)
	}
	w.send(`{"id":3,"method":"keys.set","params":{"provider":"custom","key":"a\nb"}}`)
	if got := w.next(); got["error"] == nil {
		t.Fatal("a key of two lines was handed over")
	}
}
