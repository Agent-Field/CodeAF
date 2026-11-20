package appwire

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/connect"
	"strings"
	"testing"
)

func TestPluginsMethodsOptional(t *testing.T) {
	for _, name := range []string{"plugins.list", "plugins.describe", "plugins.connect_browser", "plugins.cancel", "plugins.connect_key", "plugins.set_capability", "plugins.disconnect", "plugins.read_csv"} {
		m, ok := methods[name]
		if !ok {
			t.Errorf("missing %s", name)
			continue
		}
		_, err := m.call(&fakeEngine{}, json.RawMessage(`{"id":"google"}`))
		if wireCode(t, err) != CodeUnknownMethod {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPluginsPlanPausedSettingsPatch(t *testing.T) {
	patch, err := decodeSettingsPatch(json.RawMessage(`{"when_plan_paused":{"z-ai":"use pay-as-you-go"}}`))
	if err != nil || patch.WhenPlanPaused["z-ai"] != "use pay-as-you-go" {
		t.Fatalf("patch: %+v %v", patch, err)
	}
	for _, raw := range []string{`{"when_plan_paused":null}`, `{"when_plan_paused":{"z-ai":"spend"}}`} {
		if _, err := decodeSettingsPatch(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

type pluginsEngine struct {
	fakeEngine
	calls []string
}

func (e *pluginsEngine) ListPlugins() (PluginsResult, error) {
	e.calls = append(e.calls, "list")
	return PluginsResult{Plugins: []connect.Plugin{}, Providers: []PluginProvider{}, Featured: []string{"google"}, FeaturedWords: map[string]connect.FeaturedWords{"google": {What: "Gmail and Calendar", Does: "Read mail"}}}, nil
}
func (e *pluginsEngine) DescribePlugin(id string) (connect.PluginDescription, error) {
	e.calls = append(e.calls, id)
	return connect.PluginDescription{Capabilities: []connect.PluginCapability{}, ScopesWords: []string{}}, nil
}
func (e *pluginsEngine) ConnectPluginBrowser(p PluginBrowserParams) (PluginBrowserResult, error) {
	e.calls = append(e.calls, p.ID+":"+p.Answer)
	return PluginBrowserResult{State: "waiting", Link: "https://example.test/sign-in"}, nil
}
func (e *pluginsEngine) CancelPlugin(id string) error { e.calls = append(e.calls, id); return nil }
func (e *pluginsEngine) ConnectPluginKey(p PluginKeyParams) (PluginKeyResult, error) {
	e.calls = append(e.calls, p.ID+":"+p.KeyEnv+":"+p.Answer)
	return PluginKeyResult{Error: "shape", Words: "one key"}, nil
}
func (e *pluginsEngine) SetPluginCapability(p PluginCapabilityParams) error {
	e.calls = append(e.calls, p.ID+":"+p.Capability+":"+string(p.State))
	return nil
}
func (e *pluginsEngine) DisconnectPlugin(p PluginDisconnectParams) error {
	e.calls = append(e.calls, p.ID+":"+p.Account)
	return nil
}

func TestPluginsDispatchValidationAndFeature(t *testing.T) {
	e := &pluginsEngine{}
	w := startGraphWire(t, e)
	found := false
	for _, f := range features(w.next()) {
		found = found || f == FeaturePlugins
	}
	if !found {
		t.Fatal("hello does not advertise plugins")
	}
	for _, tt := range []struct{ name, raw, want string }{
		{"plugins.list", `{}`, "list"},
		{"plugins.describe", `{"id":"google"}`, "google"},
		{"plugins.connect_browser", `{"id":"datadog","answer":"us1"}`, "datadog:us1"},
		{"plugins.cancel", `{"id":"google"}`, "google"},
		{"plugins.connect_key", `{"id":"stripe","key_env":"STRIPE_KEY","answer":"workspace"}`, "stripe:STRIPE_KEY:workspace"},
		{"plugins.set_capability", `{"id":"google","capability":"mail-read","state":"off"}`, "google:mail-read:off"},
		{"plugins.disconnect", `{"id":"slack","account":"workspace"}`, "slack:workspace"},
	} {
		if _, err := methods[tt.name].call(e, json.RawMessage(tt.raw)); err != nil {
			t.Fatalf("%s: %v", tt.name, err)
		}
		if got := e.calls[len(e.calls)-1]; got != tt.want {
			t.Fatalf("%s forwarded %q", tt.name, got)
		}
	}
	calls := len(e.calls)
	for _, tt := range []struct{ name, raw string }{
		{"plugins.describe", `{}`},
		{"plugins.set_capability", `{"id":"google","capability":"mail-read","state":"always"}`},
		{"plugins.set_capability", `{"id":"google","state":"off"}`},
		{"plugins.connect_key", `{"id":"stripe","key":"secret","key_env":"STRIPE_KEY"}`},
	} {
		if _, err := methods[tt.name].call(e, json.RawMessage(tt.raw)); wireCode(t, err) != CodeBadParams {
			t.Fatalf("accepted %s: %v", tt.raw, err)
		}
	}
	if len(e.calls) != calls {
		t.Fatal("invalid request reached the engine")
	}
	if h := withPluginsFeature(Hello{}, &fakeEngine{}); len(h.Features) != 0 {
		t.Fatal("unsupported engine advertises plugins")
	}
	raw, err := SchemaJSON()
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"plugins.needed", "plugins.needed_done", "plugins.changed", "featured_words", "key_env", "ask_id"} {
		if !strings.Contains(string(raw), word) {
			t.Errorf("schema lacks %s", word)
		}
	}
}

type csvPluginsEngine struct {
	pluginsEngine
	asked  []PluginReadCSVParams
	answer string
	err    error
}

func (e *csvPluginsEngine) ReadPluginCSV(p PluginReadCSVParams) (PluginReadCSVResult, error) {
	e.asked = append(e.asked, p)
	return PluginReadCSVResult{CSV: e.answer}, e.err
}

// plugins.read_csv is announced only by an engine that can answer it, clamps
// the ceiling to the most it serves, and refuses an answer over the ceiling
// rather than cutting it.
func TestPluginsReadCSVFeatureParamsAndCeiling(t *testing.T) {
	if h := withPluginsFeature(Hello{}, &pluginsEngine{}); len(h.Features) != 1 || h.Features[0] != FeaturePlugins {
		t.Fatalf("an engine that cannot read a CSV announced %v", h.Features)
	}
	e := &csvPluginsEngine{answer: "Deal ID,Amount\nD-2,300\n"}
	h := withPluginsFeature(Hello{}, e)
	if len(h.Features) != 2 || h.Features[1] != FeaturePluginsReadCSV || FeaturePluginsReadCSV != "plugins.read_csv" {
		t.Fatalf("features = %v", h.Features)
	}

	out, err := methods["plugins.read_csv"].call(e, json.RawMessage(`{"id":"linear","query":" deals ","max_bytes":67108864}`))
	if err != nil || out.(*PluginReadCSVResult).CSV != e.answer {
		t.Fatalf("read = %v, %v", out, err)
	}
	if got := e.asked[0]; got.ID != "linear" || got.Query != "deals" || got.MaxBytes != PluginReadCSVMaxBytes {
		t.Fatalf("forwarded %+v", got)
	}
	if _, err := methods["plugins.read_csv"].call(e, json.RawMessage(`{"id":"linear","query":null,"max_bytes":999999999999}`)); err != nil {
		t.Fatal(err)
	}
	if got := e.asked[1]; got.Query != "" || got.MaxBytes != PluginReadCSVMaxBytes {
		t.Fatalf("a null query or an oversize ceiling was not normalized: %+v", got)
	}

	for _, raw := range []string{`{}`, `{"id":"linear","max_bytes":-1}`} {
		if _, err := methods["plugins.read_csv"].call(e, json.RawMessage(raw)); wireCode(t, err) != CodeBadParams {
			t.Fatalf("accepted %s: %v", raw, err)
		}
	}
	if len(e.asked) != 2 {
		t.Fatal("an invalid request reached the engine")
	}

	e.answer = strings.Repeat("x", 40)
	if _, err := methods["plugins.read_csv"].call(e, json.RawMessage(`{"id":"linear","max_bytes":10}`)); wireCode(t, err) != CodeTooLarge {
		t.Fatalf("an answer over the ceiling was not refused: %v", err)
	}
}
