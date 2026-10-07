package config

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

// This file ports the config-related tests from tests/test_config.py. Each Go
// test names the Python test it derives from so reviewers can diff coverage.

func approx(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("got %v, want ~%v", got, want)
	}
}

func ptrF(f float64) *float64 { return &f }
func ptrI(i int) *int         { return &i }

// sampleAuditInput mirrors tests/conftest.py::sample_audit_input.
func sampleAuditInput() AuditInputFields {
	return AuditInputFields{
		Depth:                "standard",
		SeverityThreshold:    "low",
		ScanTypes:            []string{"sast", "secrets", "config"},
		OutputFormats:        []string{"json", "sarif", "markdown"},
		ComplianceFrameworks: []string{"PCI-DSS", "SOC2", "OWASP"},
		IncludePaths:         []string{"src/"},
		ExcludePaths:         []string{"tests/", "vendor/", ".git/"},
		MaxCostUSD:           ptrF(10.0),
		MaxProvers:           ptrI(4),
		MaxDurationSeconds:   ptrI(900),
	}
}

// TestDepthProfileValuesAreStable ports
// test_config.py::test_depth_profile_values_are_stable.
func TestDepthProfileValuesAreStable(t *testing.T) {
	if DepthQuick != "quick" {
		t.Errorf("DepthQuick = %q", DepthQuick)
	}
	if DepthStandard != "standard" {
		t.Errorf("DepthStandard = %q", DepthStandard)
	}
	if DepthThorough != "thorough" {
		t.Errorf("DepthThorough = %q", DepthThorough)
	}
}

// TestBudgetConfigDefaultsSumTo100Percent ports
// test_config.py::test_budget_config_defaults_sum_to_100_percent.
func TestBudgetConfigDefaultsSumTo100Percent(t *testing.T) {
	b := DefaultBudgetConfig()
	approx(t, b.ReconBudgetPct+b.HuntBudgetPct+b.ProveBudgetPct, 1.0)
	if b.MaxCostUSD != nil || b.MaxProvers != nil || b.MaxDurationSeconds != nil {
		t.Errorf("caps default to non-None: %#v", b)
	}
	// The remaining pydantic defaults, which the Python test does not cover but
	// the phases depend on.
	if b.MaxConcurrentHunters != 4 || b.MaxConcurrentProvers != 3 || b.HunterEarlyStopFileThreshold != 30 {
		t.Errorf("concurrency/threshold defaults = %#v", b)
	}
}

// TestAuditConfigFromInputMapsFieldsAndBudget ports
// test_config.py::test_audit_config_from_input_maps_fields_and_budget.
func TestAuditConfigFromInputMapsFieldsAndBudget(t *testing.T) {
	cfg, err := AuditConfig{}.FromInputFields(sampleAuditInput(), "/tmp/sec-af-repo")
	if err != nil {
		t.Fatalf("FromInputFields: %v", err)
	}
	if cfg.RepoPath != "/tmp/sec-af-repo" {
		t.Errorf("RepoPath = %q", cfg.RepoPath)
	}
	if cfg.Depth != DepthStandard {
		t.Errorf("Depth = %q", cfg.Depth)
	}
	if !reflect.DeepEqual(cfg.ScanTypes, []string{"sast", "secrets", "config"}) {
		t.Errorf("ScanTypes = %#v", cfg.ScanTypes)
	}
	if !reflect.DeepEqual(cfg.OutputFormats, []string{"json", "sarif", "markdown"}) {
		t.Errorf("OutputFormats = %#v", cfg.OutputFormats)
	}
	if cfg.Budget.MaxCostUSD == nil || *cfg.Budget.MaxCostUSD != 10.0 {
		t.Errorf("Budget.MaxCostUSD = %#v", cfg.Budget.MaxCostUSD)
	}
	if cfg.Budget.MaxProvers == nil || *cfg.Budget.MaxProvers != 4 {
		t.Errorf("Budget.MaxProvers = %#v", cfg.Budget.MaxProvers)
	}
	if cfg.Budget.MaxDurationSeconds == nil || *cfg.Budget.MaxDurationSeconds != 900 {
		t.Errorf("Budget.MaxDurationSeconds = %#v", cfg.Budget.MaxDurationSeconds)
	}
	// BudgetConfig(...) is constructed with only the three caps, so everything
	// else keeps its own default.
	if cfg.Budget.MaxConcurrentHunters != 4 || cfg.Budget.MaxConcurrentProvers != 3 {
		t.Errorf("budget concurrency lost its defaults: %#v", cfg.Budget)
	}
	approx(t, cfg.Budget.ReconBudgetPct, 0.10)
	// Fields from_input does not pass keep their pydantic defaults.
	if cfg.SeverityThreshold != "low" {
		t.Errorf("SeverityThreshold = %q", cfg.SeverityThreshold)
	}
	if !reflect.DeepEqual(cfg.IncludePaths, []string{"src/"}) {
		t.Errorf("IncludePaths = %#v", cfg.IncludePaths)
	}
	if !reflect.DeepEqual(cfg.ExcludePaths, []string{"tests/", "vendor/", ".git/"}) {
		t.Errorf("ExcludePaths = %#v", cfg.ExcludePaths)
	}
	if !reflect.DeepEqual(cfg.ComplianceFrameworks, []string{"PCI-DSS", "SOC2", "OWASP"}) {
		t.Errorf("ComplianceFrameworks = %#v", cfg.ComplianceFrameworks)
	}
}

// TestAuditConfigRejectsInvalidDepth ports
// test_config.py::test_audit_config_rejects_invalid_depth — from_input uses the
// STRICT enum constructor, so an unknown depth fails rather than falling back to
// STANDARD the way _normalize_depth would.
func TestAuditConfigRejectsInvalidDepth(t *testing.T) {
	in := sampleAuditInput()
	in.Depth = "invalid"
	_, err := AuditConfig{}.FromInputFields(in, "/tmp/sec-af-repo")
	if err == nil {
		t.Fatal("FromInputFields accepted an invalid depth")
	}
	if want := "'invalid' is not a valid DepthProfile"; err.Error() != want {
		t.Errorf("error = %q, python ValueError = %q", err.Error(), want)
	}
}

// TestFromInputProjectsAnAuditInputShapedValue covers the `in any` projection
// path — the one the node package will use with schemas.AuditInput.
func TestFromInputProjectsAnAuditInputShapedValue(t *testing.T) {
	// Deliberately a DIFFERENT struct with extra fields, standing in for
	// schemas.AuditInput.
	type auditInputLike struct {
		RepoURL              string   `json:"repo_url"`
		Branch               string   `json:"branch"`
		Depth                string   `json:"depth"`
		SeverityThreshold    string   `json:"severity_threshold"`
		ScanTypes            []string `json:"scan_types"`
		OutputFormats        []string `json:"output_formats"`
		ComplianceFrameworks []string `json:"compliance_frameworks"`
		IncludePaths         []string `json:"include_paths"`
		ExcludePaths         []string `json:"exclude_paths"`
		MaxCostUSD           *float64 `json:"max_cost_usd"`
		MaxProvers           *int     `json:"max_provers"`
		MaxDurationSeconds   *int     `json:"max_duration_seconds"`
		IsPR                 bool     `json:"is_pr"`
	}
	in := auditInputLike{
		RepoURL:           "https://github.com/Agent-Field/sec-af",
		Branch:            "main",
		Depth:             "thorough",
		SeverityThreshold: "high",
		ScanTypes:         []string{"sast"},
		OutputFormats:     []string{"json"},
		MaxCostUSD:        ptrF(2.5),
		IsPR:              true,
	}
	cfg, err := AuditConfig{}.FromInput(in, "/repo")
	if err != nil {
		t.Fatalf("FromInput: %v", err)
	}
	if cfg.Depth != DepthThorough {
		t.Errorf("Depth = %q", cfg.Depth)
	}
	if cfg.SeverityThreshold != "high" {
		t.Errorf("SeverityThreshold = %q", cfg.SeverityThreshold)
	}
	if cfg.Budget.MaxCostUSD == nil || *cfg.Budget.MaxCostUSD != 2.5 {
		t.Errorf("Budget.MaxCostUSD = %#v", cfg.Budget.MaxCostUSD)
	}
	if cfg.RepoPath != "/repo" {
		t.Errorf("RepoPath = %q", cfg.RepoPath)
	}
}

// TestNormalizeDepth covers the lenient in-pipeline helper (phases.py:52 and its
// three verbatim copies): case-insensitive, and ANY unknown value becomes
// STANDARD.
func TestNormalizeDepth(t *testing.T) {
	cases := []struct {
		in   string
		want DepthProfile
	}{
		{"quick", DepthQuick},
		{"standard", DepthStandard},
		{"thorough", DepthThorough},
		{"QUICK", DepthQuick},
		{"Thorough", DepthThorough},
		{"invalid", DepthStandard},
		{"", DepthStandard},
		{"deep", DepthStandard},
		{" quick", DepthStandard}, // python: DepthProfile(" quick") raises -> STANDARD
	}
	for _, c := range cases {
		if got := NormalizeDepth(c.in); got != c.want {
			t.Errorf("NormalizeDepth(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// The business_logic.py `str | DepthProfile` variant: an already-typed
	// profile round-trips unchanged.
	if got := NormalizeDepth(string(DepthThorough)); got != DepthThorough {
		t.Errorf("NormalizeDepth(DepthThorough) = %q", got)
	}
}

// TestDefaultAIConfigKeepsTheNodeDefaults pins the retry schedule the node
// read from SEC_AF_AI_MAX_RETRIES / SEC_AF_AI_INITIAL_BACKOFF_SECONDS /
// SEC_AF_AI_MAX_BACKOFF_SECONDS when they were unset, and that the default
// names no model, leaving the choice to the App.
func TestDefaultAIConfigKeepsTheNodeDefaults(t *testing.T) {
	cfg := DefaultAIConfig()
	if cfg.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", cfg.MaxRetries)
	}
	approx(t, cfg.InitialBackoffSeconds, 2.0)
	approx(t, cfg.MaxBackoffSeconds, 8.0)
	if cfg.AIModel != "" {
		t.Errorf("AIModel = %q, want empty (the App chooses)", cfg.AIModel)
	}
}

// TestDefaultAIConfigIgnoresTheEnvironment: the variables the node read no
// longer steer anything.
func TestDefaultAIConfigIgnoresTheEnvironment(t *testing.T) {
	t.Setenv("SEC_AF_AI_MAX_RETRIES", "9")
	t.Setenv("SEC_AF_AI_MODEL", "provider/model")
	cfg := DefaultAIConfig()
	if cfg.MaxRetries != 3 || cfg.AIModel != "" {
		t.Errorf("DefaultAIConfig read the environment: %#v", cfg)
	}
}

// TestBudgetConfigUnmarshalSeedsDefaults: a BudgetConfig arriving over the wire
// (checkpoint, phase payload) must not silently become "0 hunters".
func TestBudgetConfigUnmarshalSeedsDefaults(t *testing.T) {
	var b BudgetConfig
	if err := json.Unmarshal([]byte(`{"max_provers": 7}`), &b); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if b.MaxProvers == nil || *b.MaxProvers != 7 {
		t.Errorf("MaxProvers = %#v", b.MaxProvers)
	}
	if b.MaxConcurrentHunters != 4 || b.MaxConcurrentProvers != 3 || b.HunterEarlyStopFileThreshold != 30 {
		t.Errorf("defaults lost: %#v", b)
	}
	approx(t, b.HuntBudgetPct, 0.45)
}

// TestAuditConfigUnmarshalSeedsDefaults.
func TestAuditConfigUnmarshalSeedsDefaults(t *testing.T) {
	var c AuditConfig
	if err := json.Unmarshal([]byte(`{"repo_path": "/r"}`), &c); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if c.RepoPath != "/r" {
		t.Errorf("RepoPath = %q", c.RepoPath)
	}
	if c.Depth != DepthStandard || c.SeverityThreshold != "low" {
		t.Errorf("defaults lost: %#v", c)
	}
	if !reflect.DeepEqual(c.ScanTypes, []string{"sast", "sca", "secrets", "config"}) {
		t.Errorf("ScanTypes = %#v", c.ScanTypes)
	}
	if !reflect.DeepEqual(c.ExcludePaths, []string{"tests/", "vendor/", "node_modules/", ".git/"}) {
		t.Errorf("ExcludePaths = %#v", c.ExcludePaths)
	}
	if c.IncludePaths != nil {
		t.Errorf("IncludePaths = %#v, want nil (python None)", c.IncludePaths)
	}
	if c.Budget.MaxConcurrentHunters != 4 {
		t.Errorf("nested budget defaults lost: %#v", c.Budget)
	}
}

// TestDefaultAuditConfigReturnsFreshSlices: pydantic's default_factory hands
// each model its own list; a shared package-level slice would let one audit's
// mutation leak into the next.
func TestDefaultAuditConfigReturnsFreshSlices(t *testing.T) {
	a := DefaultAuditConfig()
	b := DefaultAuditConfig()
	a.ScanTypes[0] = "mutated"
	if b.ScanTypes[0] != "sast" {
		t.Error("DefaultAuditConfig shares its slices between calls")
	}
}
