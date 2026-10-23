package config

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/credits"
	"github.com/Agent-Field/codeaf/internal/crewroute"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/router"
)

func TestLowBalanceDoesNotGrantFreeRouteConsent(t *testing.T) {
	dir := creditsProfile(t)
	if err := WriteAPIKey(dir, "fixture-key"); err != nil {
		t.Fatal(err)
	}
	if err := WriteCreditsReading(dir, "fixture-key", credits.Reading{Known: true, Low: true}); err != nil {
		t.Fatal(err)
	}
	if CrewFreeRoutesAt(dir) || ChatDefaultAt(dir) != DefaultModel {
		t.Fatal("low balance changed the free-route choice")
	}
	for tier := range freeHelperModels {
		if got := TierModelAt(dir, tier); got != builtinTierModel(tier) {
			t.Fatalf("%s automatically changed to %s", tier, got)
		}
	}
	if crewHealthAt(dir).unaffordable[modelsource.DefaultID] {
		t.Fatal("a low positive balance was incorrectly treated as empty")
	}
	if err := SetCrewFreeRoutes(dir, true); err != nil {
		t.Fatal(err)
	}
	if ChatDefaultAt(dir) != FreeChatModel {
		t.Fatal("explicit opt-in did not enable free defaults")
	}
	for tier, want := range freeHelperModels {
		if got := TierModelAt(dir, tier); got != want {
			t.Fatalf("opted-in %s=%s want%s", tier, got, want)
		}
	}
	if err := SetCrewFreeRoutes(dir, false); err != nil {
		t.Fatal(err)
	}
	if ChatDefaultAt(dir) != DefaultModel {
		t.Fatal("opting out left the implicit free default active")
	}
}

func TestRememberedPaymentFailureDoesNotGrantFreeRouteConsent(t *testing.T) {
	dir := crewProfile(t)
	history := []router.CrewRouteOutcome{{At: time.Now(), Seat: "worker", Send: "z-ai/glm-5.3-flash", Provider: "openrouter", Kind: "payment"}}
	withRouteHistory(t, &history)
	for _, seat := range crewroute.Seats {
		for _, rung := range CrewRescue(dir, crewroute.Bugfix, seat, "vendor/chat:free") {
			if rung.Kind == crewroute.Free || crewroute.IsFree(rung.Send) {
				t.Fatalf("%s rescued onto free route without consent: %+v", seat, rung)
			}
		}
	}
	candidates, note := crewCandidatesNoticed(dir, crewHealthAt(dir))
	if strings.Contains(note, "free routes in use") {
		t.Fatal(note)
	}
	for _, candidate := range candidates {
		for _, route := range candidate.Routes {
			if route.Kind == crewroute.Free {
				t.Fatalf("free candidate despite off: %+v", route)
			}
		}
	}
	if CrewFreeRoutesAt(dir) {
		t.Fatal("recovery changed saved preference")
	}
}

func TestSavedAutomaticRungsHonorCurrentChoiceWithoutBlockingConnectedPlans(t *testing.T) {
	dir := creditsProfile(t)
	rungs := []crewroute.Pick{
		{Send: "vendor/free:free", Kind: crewroute.Free},
		{Send: "vendor/paid", Kind: crewroute.Metered},
		{Send: "plan/chat", Kind: crewroute.Plan},
		{Send: "local/chat", Kind: crewroute.Local},
	}
	got := AutomaticCrewRungs(dir, rungs)
	if len(got) != 3 || got[0].Send != "vendor/paid" || got[1].Kind != crewroute.Plan || got[2].Kind != crewroute.Local {
		t.Fatalf("eligible connected alternatives lost: %+v", got)
	}
	if len(rungs) != 4 || rungs[0].Kind != crewroute.Free {
		t.Fatal("saved ladder was mutated")
	}
	if err := SetCrewFreeRoutes(dir, true); err != nil {
		t.Fatal(err)
	}
	if got := AutomaticCrewRungs(dir, rungs); len(got) != 4 {
		t.Fatal(got)
	}
}

func TestFreeNoticeDoesNotInventFreeUseWithoutFreeRoutes(t *testing.T) {
	for _, routes := range [][]crewroute.Route{nil, {{Kind: crewroute.Plan}}, {{Kind: crewroute.Local}}, {{Kind: crewroute.Plan}, {Kind: crewroute.Free}}} {
		if onlyFreeRoutes([]crewroute.Candidate{{Routes: routes}}) {
			t.Fatalf("claimed free use with routes %+v", routes)
		}
	}
	if !onlyFreeRoutes([]crewroute.Candidate{{Routes: []crewroute.Route{{Kind: crewroute.Free}}}}) {
		t.Fatal("free-only available routes not recognized")
	}
}
