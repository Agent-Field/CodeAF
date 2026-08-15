package roles

import (
	"errors"
	"testing"
)

// settings is a Source built from a map: the whole seam is a key lookup, so a
// test needs no store.
func settings(pairs map[string]string) Source {
	return func(key string) (string, bool) {
		value, ok := pairs[key]
		return value, ok
	}
}

// restore snapshots the package registry and puts it back when the test ends.
// Registration is global by design — roles come from init functions across the
// binary — so a test that registers has to clean up after itself or the next
// test sees its role.
func restore(t *testing.T) {
	t.Helper()
	registryMu.RLock()
	saved := make(map[Role]Tier, len(registry))
	for role, tier := range registry {
		saved[role] = tier
	}
	registryMu.RUnlock()
	t.Cleanup(func() {
		registryMu.Lock()
		registry = saved
		registryMu.Unlock()
	})
}

func TestResolvePrecedence(t *testing.T) {
	tests := []struct {
		name     string
		src      Source
		role     Role
		fallback string
		want     string
	}{
		{
			name: "pin beats tier and session",
			src: settings(map[string]string{
				"roles.title": "pinned-model",
				"tiers.low":   "low-model",
				"tiers.high":  "high-model",
			}),
			role:     RoleTitle,
			fallback: "session-model",
			want:     "pinned-model",
		},
		{
			name: "tier beats session",
			src: settings(map[string]string{
				"tiers.low":  "low-model",
				"tiers.high": "high-model",
			}),
			role:     RoleTitle,
			fallback: "session-model",
			want:     "low-model",
		},
		{
			// The default assignment reaching for the other tier is the whole
			// point of tiers: two roles, one setting each, different models.
			name: "compaction takes the high tier",
			src: settings(map[string]string{
				"tiers.low":  "low-model",
				"tiers.high": "high-model",
			}),
			role:     RoleCompaction,
			fallback: "session-model",
			want:     "high-model",
		},
		{
			name:     "session default when nothing is set",
			src:      settings(map[string]string{}),
			role:     RoleCompaction,
			fallback: "session-model",
			want:     "session-model",
		},
		{
			// A fresh install: no settings file at all, and the surface still
			// makes the call.
			name:     "nil source falls to the session model",
			src:      nil,
			role:     RoleTitle,
			fallback: "session-model",
			want:     "session-model",
		},
		{
			// Clearing a setting in a UI writes "", which must fall through
			// rather than resolve to a model named nothing.
			name: "blank settings fall through",
			src: settings(map[string]string{
				"roles.title": "  ",
				"tiers.low":   "",
			}),
			role:     RoleTitle,
			fallback: "session-model",
			want:     "session-model",
		},
		{
			name: "a pin resolves without any tier configured",
			src: settings(map[string]string{
				"roles.compaction": "pinned-model",
			}),
			role:     RoleCompaction,
			fallback: "",
			want:     "pinned-model",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Resolve(test.src, test.role, test.fallback)
			if err != nil {
				t.Fatalf("Resolve(%q) error = %v", test.role, err)
			}
			if got != test.want {
				t.Fatalf("Resolve(%q) = %q, want %q", test.role, got, test.want)
			}
		})
	}
}

func TestResolveUnknownRole(t *testing.T) {
	_, err := Resolve(settings(map[string]string{"tiers.low": "low-model"}), Role("advisor"), "session-model")
	if !errors.Is(err, ErrUnknownRole) {
		t.Fatalf("Resolve(unregistered) error = %v, want ErrUnknownRole", err)
	}
}

func TestResolveNoModelAnywhere(t *testing.T) {
	_, err := Resolve(nil, RoleTitle, "")
	if !errors.Is(err, ErrNoModel) {
		t.Fatalf("Resolve(nothing set) error = %v, want ErrNoModel", err)
	}
}

func TestRegisterAddsResolvableRole(t *testing.T) {
	restore(t)

	Register(Role("advisor"), TierHigh)

	tier, ok := TierOf(Role("advisor"))
	if !ok || tier != TierHigh {
		t.Fatalf("TierOf(advisor) = %q, %v, want high, true", tier, ok)
	}

	// A role registered from elsewhere inherits its tier's model with no
	// settings of its own — the reason the registry is open.
	got, err := Resolve(settings(map[string]string{"tiers.high": "high-model"}), Role("advisor"), "session-model")
	if err != nil {
		t.Fatalf("Resolve(advisor) error = %v", err)
	}
	if got != "high-model" {
		t.Fatalf("Resolve(advisor) = %q, want high-model", got)
	}

	// And it is pinnable like a built-in.
	got, err = Resolve(settings(map[string]string{
		"roles.advisor": "pinned-model",
		"tiers.high":    "high-model",
	}), Role("advisor"), "session-model")
	if err != nil {
		t.Fatalf("Resolve(pinned advisor) error = %v", err)
	}
	if got != "pinned-model" {
		t.Fatalf("Resolve(pinned advisor) = %q, want pinned-model", got)
	}
}

func TestRegisterOverwrites(t *testing.T) {
	restore(t)

	Register(RoleTitle, TierHigh)

	got, err := Resolve(settings(map[string]string{
		"tiers.low":  "low-model",
		"tiers.high": "high-model",
	}), RoleTitle, "session-model")
	if err != nil {
		t.Fatalf("Resolve(title) error = %v", err)
	}
	if got != "high-model" {
		t.Fatalf("Resolve(retuned title) = %q, want high-model", got)
	}
}

func TestRegisterRejectsBadInput(t *testing.T) {
	restore(t)

	tests := []struct {
		name string
		role Role
		tier Tier
	}{
		{name: "empty role", role: Role("  "), tier: TierLow},
		{name: "unknown tier", role: Role("advisor"), tier: Tier("medium")},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("Register(%q, %q) did not panic", test.role, test.tier)
				}
			}()
			Register(test.role, test.tier)
		})
	}
}

func TestRegisteredIsSortedAndComplete(t *testing.T) {
	restore(t)

	Register(Role("advisor"), TierHigh)
	Register(Role("commit"), TierLow)

	want := []Role{Role("advisor"), Role("commit"), RoleCompaction, RoleTitle}
	for range 5 { // map order varies per iteration; the answer must not
		got := Registered()
		if len(got) != len(want) {
			t.Fatalf("Registered() = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("Registered() = %v, want %v", got, want)
			}
		}
	}
}

func TestDefaultAssignment(t *testing.T) {
	// Disposable prose cheap, the session's memory capable.
	for role, want := range map[Role]Tier{RoleTitle: TierLow, RoleCompaction: TierHigh} {
		got, ok := TierOf(role)
		if !ok || got != want {
			t.Fatalf("TierOf(%q) = %q, %v, want %q, true", role, got, ok, want)
		}
	}
}

func TestReadHelpers(t *testing.T) {
	src := settings(map[string]string{
		"roles.title": "pinned-model",
		"tiers.high":  "high-model",
	})

	if model, ok := Pinned(src, RoleTitle); !ok || model != "pinned-model" {
		t.Fatalf("Pinned(title) = %q, %v, want pinned-model, true", model, ok)
	}
	if _, ok := Pinned(src, RoleCompaction); ok {
		t.Fatalf("Pinned(compaction) reported a pin that is not set")
	}
	if model, ok := TierModel(src, TierHigh); !ok || model != "high-model" {
		t.Fatalf("TierModel(high) = %q, %v, want high-model, true", model, ok)
	}
	if _, ok := TierModel(src, TierLow); ok {
		t.Fatalf("TierModel(low) reported a model that is not set")
	}
}

func TestKeys(t *testing.T) {
	if got := PinKey(RoleCompaction); got != "roles.compaction" {
		t.Fatalf("PinKey(compaction) = %q, want roles.compaction", got)
	}
	if got := TierKey(TierLow); got != "tiers.low" {
		t.Fatalf("TierKey(low) = %q, want tiers.low", got)
	}
}
