package acl

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

// P2-9
// Hosted settings are optional and accept only the documented values.
func TestP2_9_HostingPolicyDefaultsAndValidation(t *testing.T) {
	p := DefaultPolicy()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if _, exists := fields["hosting"]; exists {
		t.Fatalf("default policy includes hosting: %s", data)
	}
	for _, tc := range []struct {
		setting, withHost, withoutHost string
	}{
		{"", "refuse", "allow"}, {"allow", "allow", "allow"},
		{"warn", "warn", "warn"}, {"refuse", "refuse", "refuse"},
	} {
		p.Hosting = Hosting{LocalSessions: tc.setting, MaxSessionCostUSD: 1_000_000}
		if err := p.Validate(Policy{}, nil); err != nil {
			t.Fatal(err)
		}
		if got := p.Hosting.LocalSessionsFor(true); got != tc.withHost {
			t.Errorf("with host, %q resolves to %q, want %q", tc.setting, got, tc.withHost)
		}
		if got := p.Hosting.LocalSessionsFor(false); got != tc.withoutHost {
			t.Errorf("without host, %q resolves to %q, want %q", tc.setting, got, tc.withoutHost)
		}
	}
	var validation *ValidationError
	p.Hosting = Hosting{LocalSessions: "sometimes"}
	if err := p.Validate(Policy{}, nil); !errors.As(err, &validation) || err.Error() != "hosting local sessions must be allow, warn or refuse" {
		t.Fatalf("invalid local sessions: %v", err)
	}
	for _, cost := range []float64{-1, math.NaN(), math.Inf(-1), math.Inf(1), 1_000_001} {
		p.Hosting = Hosting{MaxSessionCostUSD: cost}
		if err := p.Validate(Policy{}, nil); !errors.As(err, &validation) {
			t.Errorf("invalid cost %g: %v", cost, err)
		}
	}
}
