package appwire

import (
	"encoding/json"
	"strings"
	"testing"
)

// A model's prices, window and Arena Elo are optional on the wire: an unknown
// price is absent, never $0, and a known free price is a present zero.
func TestModelFactsWireIsOptionalAndUnknownPriceStaysAbsent(t *testing.T) {
	data, err := json.Marshal(Model{ID: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"price_in_per_m", "price_out_per_m", "context_length", "arena_elo"} {
		if strings.Contains(string(data), key) {
			t.Fatalf("unknown %s was sent: %s", key, data)
		}
	}
	free, in, out := 0.0, 0.6, 2.2
	data, err = json.Marshal(Model{ID: "free", PriceInPerM: &free, PriceOutPerM: &free})
	if err != nil || !strings.Contains(string(data), `"price_in_per_m":0`) || !strings.Contains(string(data), `"price_out_per_m":0`) {
		t.Fatalf("a known free price must be sent as 0: %s %v", data, err)
	}
	data, err = json.Marshal(Model{ID: "priced", PriceInPerM: &in, PriceOutPerM: &out, ContextLength: 202752, ArenaElo: 1312.5})
	if err != nil {
		t.Fatal(err)
	}
	var back Model
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.PriceInPerM == nil || *back.PriceInPerM != in || back.PriceOutPerM == nil || *back.PriceOutPerM != out || back.ContextLength != 202752 || back.ArenaElo != 1312.5 {
		t.Fatalf("facts lost: %s → %+v", data, back)
	}
	var legacy Model
	if err := json.Unmarshal([]byte(`{"id":"legacy"}`), &legacy); err != nil || legacy.PriceInPerM != nil || legacy.PriceOutPerM != nil || legacy.ContextLength != 0 || legacy.ArenaElo != 0 {
		t.Fatalf("legacy invented facts: %+v %v", legacy, err)
	}
}

// A model's limits are optional on the wire, under the names the desktop app
// reads: a model nothing is known against sends no limits at all, every small
// window carries its figure and how it is known, and a limit that is not about
// the window sends its kind alone.
func TestModelLimitsWireIsAbsentUnlessTheServerSaidSo(t *testing.T) {
	data, err := json.Marshal(Model{ID: "plain"})
	if err != nil || strings.Contains(string(data), "limits") {
		t.Fatalf("a model with no limits sent some: %s %v", data, err)
	}
	limited := Model{ID: "ollama/qwen3:8b", Limits: []ModelLimit{
		{Kind: LimitSmallWindow, Window: 4096, Needs: 32000, Most: 40960, Source: WindowLoaded},
		{Kind: LimitSmallWindow, Window: 8192, Needs: 32000, Source: WindowModelfile},
		{Kind: LimitSmallWindow, Window: 4096, Needs: 32000, Most: 40960, Source: WindowLastLoaded},
		{Kind: LimitNoTools},
	}}
	data, err = json.Marshal(limited)
	want := `"limits":[` +
		`{"kind":"small_window","window":4096,"needs":32000,"most":40960,"source":"loaded"},` +
		`{"kind":"small_window","window":8192,"needs":32000,"source":"modelfile"},` +
		`{"kind":"small_window","window":4096,"needs":32000,"most":40960,"source":"last_loaded"},` +
		`{"kind":"no_tools"}]`
	if err != nil || !strings.Contains(string(data), want) {
		t.Fatalf("limits wire = %s %v, want %s", data, err, want)
	}
	var back Model
	if err := json.Unmarshal(data, &back); err != nil || len(back.Limits) != 4 || back.Limits[0] != limited.Limits[0] || back.Limits[3] != limited.Limits[3] {
		t.Fatalf("limits lost: %s → %+v %v", data, back.Limits, err)
	}
	// The schema names the words a kind and a source may be.
	schema, err := SchemaJSON()
	if err != nil || !strings.Contains(string(schema), `"small_window"`) || !strings.Contains(string(schema), `"last_loaded"`) {
		t.Fatalf("the schema does not describe a model's limits: %v", err)
	}
}
