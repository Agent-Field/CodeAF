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
			// The run's two halves, on one settings file: the planner is the
			// call that decides what the tank is spent on, a worker is one of
			// the many small ones that spend it. They are on DIFFERENT TIERS and
			// no longer on adjacent ones — the deciding call moved to the
			// mastermind tier, which is what makes "one careful call, many cheap
			// ones" a price a person can actually set.
			name: "the planner takes the mastermind tier",
			src: settings(map[string]string{
				"tiers.low":        "low-model",
				"tiers.high":       "high-model",
				"tiers.mastermind": "mastermind-model",
			}),
			role:     RolePlanner,
			fallback: "session-model",
			want:     "mastermind-model",
		},
		{
			// The worker has a tier of its own: the seat that does the work is the
			// seat that pays the bill, and it is not the small-work tier beside it.
			name: "a worker takes the worker tier and not the low one",
			src: settings(map[string]string{
				"tiers.low":    "low-model",
				"tiers.worker": "worker-model",
				"tiers.high":   "high-model",
			}),
			role:     RoleWorker,
			fallback: "session-model",
			want:     "worker-model",
		},
		{
			name: "a planner pin outranks its tier",
			src: settings(map[string]string{
				"roles.planner": "pinned-model",
				"tiers.high":    "high-model",
			}),
			role:     RolePlanner,
			fallback: "session-model",
			want:     "pinned-model",
		},
		{
			// An install that never configured a tier runs a whole adaptive run
			// on the model the person is already talking to, which is where every
			// one of these calls went before the roles existed.
			name:     "a worker falls to the session model when no tier is set",
			src:      settings(map[string]string{}),
			role:     RoleWorker,
			fallback: "session-model",
			want:     "session-model",
		},
		{
			// THE DESIGNER IS A MASTERMIND AND NOT A CAREFUL WORKER. What it
			// writes is saved and run again by everybody who picks it
			// afterwards, so its tier is the one that thinks rather than the one
			// that checks.
			name: "the designer takes the mastermind tier",
			src: settings(map[string]string{
				"tiers.low":        "low-model",
				"tiers.high":       "high-model",
				"tiers.mastermind": "mastermind-model",
			}),
			role:     RoleDesigner,
			fallback: "session-model",
			want:     "mastermind-model",
		},
		{
			// And a mastermind tier nobody set falls straight to the session
			// model, NOT to the careful one. A tier is not a ladder of tiers:
			// the rungs are the pin, the tier, and the conversation.
			name: "the planner falls to the session model when the mastermind tier is unset",
			src: settings(map[string]string{
				"tiers.low":  "low-model",
				"tiers.high": "high-model",
			}),
			role:     RolePlanner,
			fallback: "session-model",
			want:     "session-model",
		},
		{
			// A tier value may carry a level, and the level is not part of the
			// id: the ladder hands back the model alone, so a caller with no way
			// to send an effort sends a request that is byte-for-byte what it
			// was.
			name:     "a level on a tier value does not reach the model id",
			src:      settings(map[string]string{"tiers.mastermind": "moonshotai/kimi-k3:low"}),
			role:     RolePlanner,
			fallback: "session-model",
			want:     "moonshotai/kimi-k3",
		},
		{
			// The reflex role takes its OWN tier and not the low one, which is
			// the whole reason the third tier exists: the low model is cheap for
			// a call made once a session and is not cheap for one made twice a
			// turn.
			name: "reflex takes the reflex tier, not the low one",
			src: settings(map[string]string{
				"tiers.reflex": "reflex-model",
				"tiers.low":    "low-model",
				"tiers.high":   "high-model",
			}),
			role:     RoleReflex,
			fallback: "session-model",
			want:     "reflex-model",
		},
		{
			name: "a reflex pin outranks its tier",
			src: settings(map[string]string{
				"roles.reflex": "pinned-model",
				"tiers.reflex": "reflex-model",
			}),
			role:     RoleReflex,
			fallback: "session-model",
			want:     "pinned-model",
		},
		{
			// A cleared reflex row is a person saying "use what I am talking
			// to", and the ladder's floor is what says it.
			name:     "reflex falls to the session model when its tier is blank",
			src:      settings(map[string]string{"tiers.reflex": ""}),
			role:     RoleReflex,
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

	want := []Role{
		Role("advisor"), RoleCaption, Role("commit"), RoleCompaction,
		RoleDesigner, RolePlanner, RoleReflex, RoleRouter, RoleTitle, RoleWorker,
	}
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
	// Disposable prose cheap, the session's memory capable — and the run's
	// balance: one careful call deciding what happens, many cheap ones doing it.
	for role, want := range map[Role]Tier{
		RoleTitle:      TierLow,
		RoleCaption:    TierLow,
		RoleCompaction: TierHigh,
		// The two masterminds. They were on the high tier beside the compaction
		// summary, which made one figure answer two different bills.
		RolePlanner:  TierMastermind,
		RoleDesigner: TierMastermind,
		RoleWorker:   TierWorker,
		// The per-turn pair is the third tier's only tenant, and it is the
		// assignment that would be silently wrong: reflex on the low tier is a
		// cheap model called twice a turn, which reads as thrift and bills as a
		// habit.
		RoleReflex: TierReflex,
	} {
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
