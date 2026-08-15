package main

import (
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/exec"
	"github.com/Agent-Field/aforge-v2/internal/profile"
	"github.com/Agent-Field/aforge-v2/internal/provider"
)

// The loop that has to close inside one session: a worker's leaves land in its
// profile, the profile crosses its evidence gate, and the very next compile
// chooses from a menu that says what that worker has actually cost — not from
// the paragraph it shipped with.
//
// The gate is the whole test. Below it the menu is exactly the registration's
// own words, because a median of three leaves is not a measurement and a
// compiler that routed on one would be routing on noise. Above it the same menu
// carries a line nobody wrote.
func TestAProfileCrossingItsGateChangesTheMenuTheCompilerReads(t *testing.T) {
	defer exec.ForgetSubharnesses()
	exec.RegisterSubharness(exec.SubharnessInfo{Name: "swe", Purpose: "software engineering taken whole"})
	settings := config.Config{ProfileDir: t.TempDir(), Model: "worker/model"}

	// The hook chat installs, installed here: read at render time rather than
	// captured, which is what lets a worker that crosses its gate mid-session be
	// grounded in that session.
	exec.UseSubharnessKnowledge(func(subharness string) string {
		return subharnessKnowledge(settings, settings.Model, subharness)
	})

	measured, err := profile.Load(settings.ProfileDir, settings.Model, "swe")
	if err != nil {
		t.Fatal(err)
	}
	for index := 1; index < profile.MinSamples; index++ {
		measured.Add(profile.Record{Title: "coding", Size: profile.BucketDirect,
			Turns: 10, Tokens: 90_000, Cost: 1.25, Verdict: provider.VerdictVerifiedSuccess})
	}
	if err := measured.Save(); err != nil {
		t.Fatal(err)
	}
	below := exec.MenuText()
	if !strings.Contains(below, "swe — software engineering taken whole") {
		t.Fatalf("the worker is not on the menu at all:\n%s", below)
	}
	if strings.Contains(below, "measured here so far") {
		t.Fatalf("the menu quoted a measurement below the evidence gate:\n%s", below)
	}

	// One more leaf, no restart, no re-registration.
	measured.Add(profile.Record{Title: "coding", Size: profile.BucketDirect,
		Turns: 10, Tokens: 90_000, Cost: 1.25, Verdict: provider.VerdictVerifiedSuccess})
	if err := measured.Save(); err != nil {
		t.Fatal(err)
	}
	above := exec.MenuText()
	if above == below {
		t.Fatalf("crossing the gate changed nothing the compiler reads:\n%s", above)
	}
	for _, want := range []string{"measured here so far", "median 90000 tokens", "over 8 runs", "$1.2500"} {
		if !strings.Contains(above, want) {
			t.Fatalf("the menu is missing %q:\n%s", want, above)
		}
	}
	// The retry menu is built from the same registrations and carries the same
	// measurement, so a judgement about who takes a second attempt is grounded
	// in exactly what the compiler's first choice was grounded in.
	if retry := exec.MenuTextExcept(exec.LinearSubharness); !strings.Contains(retry, "measured here so far") {
		t.Fatalf("the retry menu is not grounded in measurement:\n%s", retry)
	}

	// And the same history reaches the compile's own context block, which is the
	// other half of what the choosing model is given.
	selfKnowledgeCached = cachedSelfKnowledge{}
	knowledge := selfKnowledge(settings, settings.Model)
	if !strings.Contains(knowledge, "swe: median") {
		t.Fatalf("the specialist's history never reached the compile context:\n%s", knowledge)
	}
}
