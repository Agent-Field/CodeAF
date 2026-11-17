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
