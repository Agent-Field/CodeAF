package reasoners

// Registration parity for the router surface.
//
// Validation contract (behaviour, derived from src/sec_af/reasoners/*.py and
// DESIGN.md §3 — NOT from register.go):
//
//   - the router carries exactly 33 reasoners, with the exact names and in the
//     exact order DESIGN.md §3 lists (which is the Python import + decorator
//     order);
//   - no name is registered twice (the registry panics on a collision rather
//     than letting the second registration win);
//   - every registered name is actually reachable through Registry.Lookup —
//     read back from the registry, not from RegisterAll's own bookkeeping, so
//     the two cannot drift together.

import (
	"context"
	"reflect"
	"testing"

	"github.com/Agent-Field/codeaf/internal/secaf/appx"
)

// pythonSurface is the independent parity checklist: the 33 router reasoner
// names in DESIGN.md §3 order, written out from the Python inventory rather
// than derived from Names, so drift in either direction fails the test.
var pythonSurface = []string{
	// reasoners/recon.py
	"run_architecture_mapper",
	"run_dependency_auditor",
	"run_config_scanner",
	"run_data_flow_mapper",
	"run_security_context_profiler",
	// reasoners/hunt.py
	"run_injection_hunter",
	"run_dos_hunter",
	"run_ssrf_hunter",
	"run_auth_hunter",
	"run_xss_hunter",
	"run_crypto_hunter",
	"run_business_logic_hunter",
	"run_logic_bugs_hunter",
	"run_data_exposure_hunter",
	"run_supply_chain_hunter",
	"run_config_secrets_hunter",
	"run_api_security_hunter",
	"run_deduplicator",
	// reasoners/prove.py
	"run_dep_reachability",
	"run_verifier",
	"run_tracer",
	"run_sanitization_analyzer",
	"run_exploit_hypothesizer",
	"run_verdict_agent",
	"run_remediation",
	"run_remediation_agent",
	"run_dast_verifier",
	"run_cross_service_analyzer",
	// reasoners/phases.py
	"run_cwe_expansion",
	"recon_phase",
	"hunt_phase",
	"prove_phase",
	"remediation_phase",
}

func TestNamesMatchPythonSurface(t *testing.T) {
	if !reflect.DeepEqual(Names, pythonSurface) {
		t.Fatalf("Names mismatch:\n got  = %v\n want = %v", Names, pythonSurface)
	}
	if len(Names) != 33 {
		t.Fatalf("surface size = %d, want 33", len(Names))
	}
}

func TestRegisterAllExactOrderedSurface(t *testing.T) {
	reg := NewRegistry()
	got := RegisterAll(reg, &appx.Fake{})

	if !reflect.DeepEqual(got, pythonSurface) {
		t.Fatalf("registered surface mismatch:\n got  = %v\n want = %v", got, pythonSurface)
	}
	if names := reg.Names(); !reflect.DeepEqual(names, pythonSurface) {
		t.Fatalf("registry order mismatch:\n got  = %v\n want = %v", names, pythonSurface)
	}

	seen := map[string]int{}
	for _, name := range got {
		seen[name]++
	}
	for name, count := range seen {
		if count > 1 {
			t.Errorf("reasoner %q registered %d times (collision)", name, count)
		}
	}
}

// TestRegisterAllReachableInRegistry reads the surface back OUT of the
// registry: every name RegisterAll reports must resolve through Lookup, and the
// registry must hold nothing else.
func TestRegisterAllReachableInRegistry(t *testing.T) {
	reg := NewRegistry()
	names := RegisterAll(reg, &appx.Fake{})

	if got := len(reg.Names()); got != len(names) {
		t.Errorf("registry holds %d reasoners, want %d", got, len(names))
	}
	for _, name := range names {
		if h, ok := reg.Lookup(name); !ok || h == nil {
			t.Errorf("reasoner %q is not reachable in the registry", name)
		}
	}
	if _, ok := reg.Lookup(NameAudit); ok {
		t.Error("audit is the entry point, not a registered reasoner")
	}
}

// TestRegistryRefusesADuplicateName: a second registration under one name is a
// wiring bug, and must fail at construction.
func TestRegistryRefusesADuplicateName(t *testing.T) {
	reg := NewRegistry()
	h := func(context.Context, map[string]any) (any, error) { return nil, nil }
	reg.Register("run_x", h, nil)
	defer func() {
		if recover() == nil {
			t.Fatal("a duplicate registration returned instead of panicking")
		}
	}()
	reg.Register("run_x", h, nil)
}

// TestRegisterAllHandlersBindDefaults proves the registration path really runs
// afx.Bind into the typed input (and therefore the default-seeding
// UnmarshalJSON): run_config_secrets_hunter is invoked through its REGISTERED
// handler with a body that omits max_files_without_signal, and the prompt the
// harness receives must still carry the Python default of 30.
func TestRegisterAllHandlersBindDefaults(t *testing.T) {
	fake := newScanFake()

	reg := NewRegistry()
	RegisterAll(reg, fake)
	h, ok := reg.Lookup("run_config_secrets_hunter")
	if !ok {
		t.Fatal("run_config_secrets_hunter is not registered")
	}

	if _, err := h(context.Background(), map[string]any{
		"repo_path":     t.TempDir(),
		"recon_context": map[string]any{},
		"depth":         "standard",
	}); err != nil {
		t.Fatalf("handler: %v", err)
	}

	if len(fake.Harnesses) != 1 {
		t.Fatalf("harness calls = %d, want 1", len(fake.Harnesses))
	}
	assertPromptHasFileBudget(t, fake.Harnesses[0].Prompt, "30")
}
