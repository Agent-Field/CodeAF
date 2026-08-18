package roles

import (
	"strings"
	"testing"
)

// THE LEVEL A TIER VALUE CARRIES, from both sides: what the notation means, and
// what it must never do to a model id.
//
// The whole risk this file guards is one mistake: a colon in a model id read as
// an effort. Provider ids carry their own suffixes — `…:free`, `…:nitro`,
// `…:thinking` — and a split that took every colon would send requests for
// models nobody serves, silently, on every tier a person had written a variant
// into.
func TestSplitEffortReadsALevelAndLeavesEveryOtherSuffixAlone(t *testing.T) {
	for _, c := range []struct {
		value, model, effort string
	}{
		{"moonshotai/kimi-k3:low", "moonshotai/kimi-k3", "low"},
		{"moonshotai/kimi-k3:medium", "moonshotai/kimi-k3", "medium"},
		{"moonshotai/kimi-k3:high", "moonshotai/kimi-k3", "high"},
		// Case and whitespace are the shapes a hand-edited config file has.
		{"  moonshotai/kimi-k3 : HIGH ", "moonshotai/kimi-k3", "high"},
		// A PLAIN ID IS UNTOUCHED, which is every value written before this
		// notation existed.
		{"deepseek/deepseek-v4-flash", "deepseek/deepseek-v4-flash", ""},
		// A PROVIDER VARIANT IS PART OF THE ID. This is the case that would
		// break quietly.
		{"deepseek/deepseek-v4-flash:free", "deepseek/deepseek-v4-flash:free", ""},
		{"some/model:nitro", "some/model:nitro", ""},
		{"", "", ""},
	} {
		model, effort := SplitEffort(c.value)
		if model != c.model || effort != c.effort {
			t.Errorf("SplitEffort(%q) = %q, %q; want %q, %q", c.value, model, effort, c.model, c.effort)
		}
	}
}

// ResolveCall hands the two halves back separately, at every rung of the ladder
// a value can be written on.
func TestResolveCallCarriesTheLevelOffTheTierAndThePin(t *testing.T) {
	tier := settings(map[string]string{"tiers.mastermind": "moonshotai/kimi-k3:high"})
	call, err := ResolveCall(tier, RolePlanner, "session-model")
	if err != nil {
		t.Fatal(err)
	}
	if call.Model != "moonshotai/kimi-k3" || call.Effort != "high" {
		t.Fatalf("the mastermind tier resolved to %+v", call)
	}
	if got := call.String(); got != "moonshotai/kimi-k3:high" {
		t.Fatalf("the call reads back as %q", got)
	}

	pin := settings(map[string]string{
		"tiers.mastermind": "moonshotai/kimi-k3:high",
		"roles.planner":    "deepseek/deepseek-v4-pro:low",
	})
	call, err = ResolveCall(pin, RolePlanner, "session-model")
	if err != nil {
		t.Fatal(err)
	}
	if call.Model != "deepseek/deepseek-v4-pro" || call.Effort != "low" {
		t.Fatalf("a pin carrying a level resolved to %+v", call)
	}

	// THE SESSION MODEL IS NOT SPLIT: an effort the person dialled into their
	// own conversation is not an instruction about an errand.
	call, err = ResolveCall(nil, RolePlanner, "vendor/model:free")
	if err != nil {
		t.Fatal(err)
	}
	if call.Model != "vendor/model:free" || call.Effort != "" {
		t.Fatalf("the ladder's floor resolved to %+v", call)
	}
	if got := call.String(); got != "vendor/model:free" {
		t.Fatalf("a call with no level reads back as %q", got)
	}
}

// Resolve is ResolveCall with the level dropped, which is the right answer for
// every caller that has no request option to put one in.
func TestResolveDropsTheLevelSoAnIdIsNeverSentWithOneOnIt(t *testing.T) {
	src := settings(map[string]string{"tiers.mastermind": "moonshotai/kimi-k3:low"})
	model, err := Resolve(src, RoleDesigner, "session-model")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(model, ":") {
		t.Fatalf("Resolve handed back %q — a level reached the model field", model)
	}
	if model != "moonshotai/kimi-k3" {
		t.Fatalf("Resolve handed back %q", model)
	}
}

func TestValidEffortKnowsThreeLevelsAndNotOff(t *testing.T) {
	for _, word := range Efforts {
		if !ValidEffort(word) {
			t.Errorf("ValidEffort(%q) is false, and it is in Efforts", word)
		}
	}
	// "off" is a different request — it asks a provider to suppress the thinking
	// pass, which some endpoints refuse — and a tier value is written once and
	// forgotten, which is the wrong place for a knob that can 400.
	for _, word := range []string{"off", "none", "max", "", "lo"} {
		if ValidEffort(word) {
			t.Errorf("ValidEffort(%q) is true", word)
		}
	}
}

// EVERY TIER IS REGISTRABLE, and the list a settings surface renders is the same
// list Register validates against — so a fifth tier is one line and not two.
func TestEveryTierInTheListIsRegistrable(t *testing.T) {
	restoreRegistry(t)
	for _, tier := range Tiers {
		role := Role("probe-" + string(tier))
		Register(role, tier)
		got, ok := TierOf(role)
		if !ok || got != tier {
			t.Fatalf("TierOf(%q) = %q, %v after registering on %q", role, got, ok, tier)
		}
	}
}

func TestRegisterPanicsOnATierThatIsNotInTheList(t *testing.T) {
	restoreRegistry(t)
	defer func() {
		if recover() == nil {
			t.Fatal("registering on an unknown tier did not panic")
		}
	}()
	Register(Role("probe-nonsense"), Tier("frontier"))
}

// A ROLE SAYS WHAT IT IS IN A PERSON'S WORDS. The settings list prints this line
// under each role's name, and before it existed "small work" and "careful work"
// were two model ids with no way of finding out what actually ran on them.
func TestEveryBuiltInRoleHasAPlainDescription(t *testing.T) {
	for role, want := range roleDescriptions {
		if got := Describe(role); got != want {
			t.Errorf("Describe(%q) = %q, want %q", role, got, want)
		}
		if strings.Contains(want, string(role)) && len(want) < 20 {
			t.Errorf("Describe(%q) = %q — the role's own name is not a description", role, want)
		}
	}
	// The three roles internal/session registers are described here even though
	// their tier is declared there: a description is what a person reads, and
	// the settings surface is nowhere near those files.
	for _, role := range []Role{RoleGuardian, RoleAuditor, RoleVision} {
		if Describe(role) == "" {
			t.Errorf("%q has no description, so its settings row reads as nothing", role)
		}
	}
	if Describe(Role("nothing-registered-this")) != "" {
		t.Error("an unknown role invented a description")
	}
}

// A package registering its own role may pass its own line, and it wins.
func TestRegisterTakesADescription(t *testing.T) {
	restoreRegistry(t)
	Register(Role("probe-described"), TierLow, "does one small thing")
	if got := Describe(Role("probe-described")); got != "does one small thing" {
		t.Fatalf("Describe = %q", got)
	}
}

// restoreRegistry puts the registry back after a test has written to it, so the
// tests that read the shipped assignment are not reading another test's probes.
func restoreRegistry(t *testing.T) {
	t.Helper()
	registryMu.Lock()
	savedRegistry := make(map[Role]Tier, len(registry))
	for role, tier := range registry {
		savedRegistry[role] = tier
	}
	savedDescriptions := make(map[Role]string, len(descriptions))
	for role, text := range descriptions {
		savedDescriptions[role] = text
	}
	registryMu.Unlock()
	t.Cleanup(func() {
		registryMu.Lock()
		registry, descriptions = savedRegistry, savedDescriptions
		registryMu.Unlock()
	})
}
