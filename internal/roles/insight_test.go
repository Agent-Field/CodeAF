package roles

import "testing"

// registerInsight registers the page-AI family the way the errand door does,
// inside a test's own registry.
func registerInsight(t *testing.T) {
	t.Helper()
	restore(t)
	Register(RoleInsight, TierLow)
	for _, role := range InsightOverrides {
		RegisterUnder(role, RoleInsight)
	}
}

// A PAGE AI's LADDER IS ITS OWN PIN, THEN THE FAMILY'S PIN, THEN THE FAMILY'S
// DEFAULT, THEN THE TIER, THEN THE FLOOR: one choice made on `insight` reaches
// every page, and an override is only ever more specific.
func TestAnInsightRoleClimbsItsOwnPinThenTheFamilyThenTheTier(t *testing.T) {
	registerInsight(t)
	tests := []struct {
		name string
		src  map[string]string
		want string
	}{
		{"own pin wins", map[string]string{"roles.insight_notes": "own", "roles.insight": "family", "defaults.insight": "seeded", "tiers.low": "low"}, "own"},
		{"the family pin", map[string]string{"roles.insight": "family", "defaults.insight": "seeded", "tiers.low": "low"}, "family"},
		{"the family default", map[string]string{"defaults.insight": "seeded", "tiers.low": "low"}, "seeded"},
		{"the tier", map[string]string{"tiers.low": "low"}, "low"},
		{"the floor", map[string]string{}, "session"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := Resolve(settings(test.src), RoleInsightNotes, "session")
			if err != nil || got != test.want {
				t.Fatalf("Resolve(insight_notes) = %q, %v, want %q", got, err, test.want)
			}
		})
	}
	rungs, err := Ladder(settings(map[string]string{"roles.insight_ask": "own", "roles.insight": "family", "defaults.insight": "seeded", "tiers.low": "low"}), RoleInsightAsk, "session")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, rung := range rungs {
		got = append(got, rung.Model)
	}
	if want := []string{"own", "family", "seeded", "low", "session"}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] || got[4] != want[4] {
		t.Fatalf("Ladder(insight_ask) = %v, want %v", got, want)
	}
	if tier, ok := TierOf(RoleInsightThemes); !ok || tier != TierLow {
		t.Fatalf("an override takes its family's tier: %q, %v", tier, ok)
	}
}

// A role with no family never reads another role's pin or a default it was
// not given: the ladder every existing role climbs is unchanged.
func TestARoleWithNoFamilyReadsOnlyItsOwnRungs(t *testing.T) {
	registerInsight(t)
	src := settings(map[string]string{"roles.insight": "family", "defaults.insight": "seeded", "tiers.low": "low"})
	if got, _ := Resolve(src, RoleTitle, "session"); got != "low" {
		t.Fatalf("title = %q, want its tier", got)
	}
}

// A pin row may name every page AI, registered or not, so a pin written for
// one never reads as a typo.
func TestTheInsightRolesAreKnownPinWords(t *testing.T) {
	for _, role := range append([]Role{RoleInsight}, InsightOverrides...) {
		if !Known(string(role)) {
			t.Errorf("%q is not a known role word", role)
		}
	}
}

func TestRegisterUnderRefusesAFamilyThatIsNotOne(t *testing.T) {
	registerInsight(t)
	for name, parent := range map[string]Role{"unregistered": Role("nobody"), "a child": RoleInsightNotes} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("RegisterUnder(x, %q) did not panic", parent)
				}
			}()
			RegisterUnder(Role("x"), parent)
		})
	}
}
