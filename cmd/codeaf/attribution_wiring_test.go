package main

import (
	"os"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/manual"
	"github.com/Agent-Field/codeaf/internal/session"
)

// Every entry point that builds a leaf loop must wire both the attribution
// switch and the model-name row into its signature, and chat.go builds no loop
// of its own.
func TestBothExecutorConstructionSitesCarryAttribution(t *testing.T) {
	for _, name := range []string{"subharness.go", "run.go", "exec.go", "chatv3_subharness.go", "subharness_run.go"} {
		raw, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		source := string(raw)
		if strings.Count(source, "exec.NewLinear(") != 1 {
			t.Fatalf("%s no longer builds exactly one leaf loop", name)
		}
		if !strings.Contains(source, "WithAttribution(config.AttributionAt(") {
			t.Fatalf("%s builds an executor without wiring the attribution row into its signature", name)
		}
		if !strings.Contains(source, "WithAssistedBy(config.AssistedByModelAt(") {
			t.Fatalf("%s builds an executor without wiring the model-name row into its signature", name)
		}
	}
	raw, err := os.ReadFile("chat.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "exec.NewLinear(") {
		t.Fatal("chat.go builds a leaf loop of its own again instead of asking the table for one")
	}
}

// Every v3 session door passes through applyV3Governance, which must wire both
// attribution rows from the profile and environment into session.Config.
func TestApplyV3GovernanceCarriesAttributionRows(t *testing.T) {
	profile := t.TempDir()
	cfg, _ := applyV3Governance(session.Config{}, profile, false, false)
	if cfg.AttributionOff || cfg.AttributionModelOff {
		t.Fatalf("default profile turned attribution off: %+v", cfg)
	}

	t.Setenv("CODEAF_ATTRIBUTION", "off")
	t.Setenv("CODEAF_ATTRIBUTION_MODEL", "off")
	cfg, _ = applyV3Governance(session.Config{}, profile, false, false)
	if !cfg.AttributionOff || !cfg.AttributionModelOff {
		t.Fatalf("applyV3Governance did not read the attribution env pins: %+v", cfg)
	}
}

// A capability codeaf has and cannot explain is one the user meets first as a
// surprise in their own git history.
func TestManualExplainsAttribution(t *testing.T) {
	for _, term := range []string{"attribution", "CODEAF_ATTRIBUTION", "attribution.model", "CODEAF_ATTRIBUTION_MODEL", "Assisted-by", "sharing", "CONTRIBUTING"} {
		if !manual.Mentions(term) {
			t.Fatalf("no manual page mentions %q", term)
		}
	}
}
