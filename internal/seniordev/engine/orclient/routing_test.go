//go:build !windows

package orclient

import (
	"strings"
	"testing"
)

func TestParseProviderRoutingAcceptsEveryDocumentedField(t *testing.T) {
	routing, err := ParseProviderRouting([]byte(`{
		"order": ["anthropic", "amazon-bedrock"],
		"allow_fallbacks": true,
		"require_parameters": true,
		"data_collection": "deny",
		"zdr": false,
		"enforce_distillable_text": false,
		"only": ["anthropic"],
		"ignore": ["gmicloud"],
		"quantizations": ["fp8", "bf16"],
		"sort": {"by": "throughput", "partition": "none"},
		"max_price": {"prompt": 1, "completion": 2},
		"preferred_min_throughput": {"p50": 100, "p90": 50},
		"preferred_max_latency": 3
	}`))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := routing.Object().MarshalJSON()
	want := `{"order":["anthropic","amazon-bedrock"],"allow_fallbacks":true,"require_parameters":true,` +
		`"data_collection":"deny","zdr":false,"enforce_distillable_text":false,"only":["anthropic"],` +
		`"ignore":["gmicloud"],"quantizations":["fp8","bf16"],"sort":{"by":"throughput","partition":"none"},` +
		`"max_price":{"prompt":1,"completion":2},"preferred_min_throughput":{"p50":100,"p90":50},` +
		`"preferred_max_latency":3}`
	if string(got) != want {
		t.Fatalf("wire =\n%s\nwant\n%s", got, want)
	}
}

func TestParseProviderRoutingKeepsBareSortString(t *testing.T) {
	routing, err := ParseProviderRouting([]byte(`{"sort": "throughput"}`))
	if err != nil {
		t.Fatal(err)
	}
	got, _ := routing.Object().MarshalJSON()
	if string(got) != `{"sort":"throughput"}` {
		t.Fatalf("wire = %s", got)
	}
}

func TestParseProviderRoutingRejectsWhatOpenRouterWouldIgnore(t *testing.T) {
	// A misspelled or out-of-range rule must fail at config time; a rule that
	// parses and does nothing would route on defaults while looking set.
	for name, raw := range map[string]string{
		"unknown key":          `{"sort": "price", "prefered_max_latency": 3}`,
		"bad sort":             `{"sort": "fastest"}`,
		"bad partition":        `{"sort": {"by": "price", "partition": "provider"}}`,
		"bad data_collection":  `{"data_collection": "never"}`,
		"bad quantization":     `{"quantizations": ["fp8", "q4_k_m"]}`,
		"empty slug":           `{"only": [""]}`,
		"negative price":       `{"max_price": {"prompt": -1}}`,
		"empty threshold":      `{"preferred_max_latency": {}}`,
		"unknown percentile":   `{"preferred_max_latency": {"p95": 3}}`,
		"threshold wrong type": `{"preferred_min_throughput": "fast"}`,
		"not an object":        `["sort"]`,
	} {
		if _, err := ParseProviderRouting([]byte(raw)); err == nil {
			t.Errorf("%s: %s parsed without error", name, raw)
		}
	}
}

func TestParseProviderRoutingEmptyMeansOff(t *testing.T) {
	for _, raw := range []string{``, `null`, `{}`} {
		routing, err := ParseProviderRouting([]byte(raw))
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if !routing.IsZero() || routing.Object() != nil {
			t.Fatalf("%q: routing=%+v object=%v, want nothing", raw, routing, routing.Object())
		}
	}
}

func TestProviderRoutingMergeLaterLevelWins(t *testing.T) {
	base, _ := ParseProviderRouting([]byte(`{
		"sort": "throughput", "require_parameters": true,
		"ignore": ["a", "b"], "max_price": {"prompt": 1, "completion": 2}
	}`))
	override, _ := ParseProviderRouting([]byte(`{
		"sort": {"by": "price"}, "ignore": ["c"], "max_price": {"completion": 5}, "zdr": true
	}`))
	got, _ := base.Merge(override).Object().MarshalJSON()
	// Lists and nested objects replace wholesale; untouched scalars survive.
	want := `{"require_parameters":true,"zdr":true,"ignore":["c"],"sort":"price","max_price":{"completion":5}}`
	if string(got) != want {
		t.Fatalf("merged = %s\nwant     %s", got, want)
	}
	if unchanged, _ := base.Object().MarshalJSON(); !strings.Contains(string(unchanged), `"ignore":["a","b"]`) {
		t.Fatalf("Merge mutated its receiver: %s", unchanged)
	}
	if base.Merge(nil).IsZero() || (*ProviderRouting)(nil).Merge(override).IsZero() {
		t.Fatal("merging with nil lost the configured side")
	}
	if (*ProviderRouting)(nil).Merge(nil) != nil {
		t.Fatal("nil merged with nil must stay nil")
	}
}
