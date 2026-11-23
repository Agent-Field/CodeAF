package config

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// TestTheClientDoorStatesBillingOnlyFromTheBoundDoor: a named door's own
// Metered bit is the only billing statement a client is built with. A service
// with no named door, a direct key and a codex sign-in all state nothing.
func TestTheClientDoorStatesBillingOnlyFromTheBoundDoor(t *testing.T) {
	zai := modelsource.Source{ID: "z-ai", Written: "z-ai"}
	cases := map[string]struct {
		service modelsource.Connected
		want    provider.BillingFact
	}{
		"plan door": {
			modelsource.Connected{Source: zai, Key: "k", Door: modelsource.Door{ID: "coding-plan", Name: "coding plan"}},
			provider.IncludedDoorFact("z-ai", "coding-plan"),
		},
		"metered door": {
			modelsource.Connected{Source: zai, Key: "k", Door: modelsource.Door{ID: "metered", Name: "pay-as-you-go", Metered: true}},
			provider.MeteredDoorFact("z-ai", "metered"),
		},
		"one-door direct key": {
			modelsource.Connected{Source: zai, Key: "k"},
			provider.BillingFact{},
		},
		"default router": {
			modelsource.Connected{Source: modelsource.DefaultSource(""), Key: "k"},
			provider.BillingFact{},
		},
		"codex sign-in, even on a named plan door": {
			modelsource.Connected{Source: modelsource.Source{ID: "codex", Written: "codex"}, Door: modelsource.Door{ID: "plus"}},
			provider.BillingFact{},
		},
	}
	for name, c := range cases {
		got := ClientConfigFor(modelsource.NewSet(c.service), "some/model").Billing
		if got != c.want {
			t.Errorf("%s: billing = %+v, want %+v", name, got, c.want)
		}
		if got.Included() != (c.want.Kind == provider.BillingIncluded) {
			t.Errorf("%s: included = %t", name, got.Included())
		}
	}
}
