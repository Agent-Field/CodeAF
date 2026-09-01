package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/aforge-v2/internal/approval"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/roles"
	"github.com/Agent-Field/aforge-v2/internal/session"
)

// The settings rows reaching internal/session: the tool gate, the auxiliary
// models, and the ceiling. This is the wiring wave's whole contract with the
// registry, so it is tested against a real profile directory rather than a
// hand-built map.

// v3Profile writes one profile config.json and answers with its directory.
func v3Profile(t *testing.T, rows map[string]any) string {
	t.Helper()
	dir := t.TempDir()
	raw, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestTheSettingsRowsReachTheSessionConfig(t *testing.T) {
	dir := v3Profile(t, map[string]any{
		"tools.approvalMode":            "allow",
		"tools.approval":                "bash:prompt, write:deny",
		"models.tiers.low":              "cheap/model",
		"models.tiers.high":             "capable/model",
		"models.roles":                  "title:pinned/model",
		"session.spendRailUSD":          4.5,
		"bash.background_after_seconds": 12,
		"unrelated_other_person":        "left alone",
	})

	cfg, err := applyV3Governance(session.Config{Model: "session/model"}, dir, false, false)
	if err != nil {
		t.Fatalf("the rows did not load: %v", err)
	}

	// The gate: the mode is the default, the exceptions beat it, and the bash
	// pattern floor still stands over an allow.
	if cfg.ApprovalPolicy == nil {
		t.Fatal("no policy reached the session")
	}
	policy := *cfg.ApprovalPolicy
	for _, want := range []struct {
		tool   string
		args   string
		action approval.Action
	}{
		{"read", `{}`, approval.ActionAllow},                           // the default
		{"bash", `{"command":"go test ./..."}`, approval.ActionPrompt}, // the exception
		{"write", `{"path":"x"}`, approval.ActionDeny},
		{"bash", `{"command":"rm -rf /"}`, approval.ActionPrompt}, // matched, not judged
	} {
		if got := policy.Check(want.tool, json.RawMessage(want.args)); got.Action != want.action {
			t.Fatalf("%s %s → %s, want %s", want.tool, want.args, got, want.action)
		}
	}

	// The auxiliary models: a pin beats a tier, a tier beats the session model,
	// and an unset role still resolves — to the model the person already pays for.
	if cfg.RolesSource == nil {
		t.Fatal("no roles source reached the session")
	}
	model, err := roles.Resolve(cfg.RolesSource, roles.RoleTitle, "session/model")
	if err != nil || model != "pinned/model" {
		t.Fatalf("the title role resolved to %q (%v), want the pin", model, err)
	}
	model, err = roles.Resolve(cfg.RolesSource, roles.RoleCompaction, "session/model")
	if err != nil || model != "capable/model" {
		t.Fatalf("compaction resolved to %q (%v), want the high tier", model, err)
	}
	if got, ok := roles.TierModel(cfg.RolesSource, roles.TierLow); !ok || got != "cheap/model" {
		t.Fatalf("the low tier is %q (ok=%v)", got, ok)
	}

	if cfg.SpendRailUSD != 4.5 {
		t.Fatalf("the ceiling is %v, want 4.5", cfg.SpendRailUSD)
	}
	if cfg.BashBackgroundAfterSeconds != 12 {
		t.Fatalf("the background-after clock is %d, want 12", cfg.BashBackgroundAfterSeconds)
	}
}

func TestAnEmptyProfileGatesEverythingAndFollowsTheSessionModel(t *testing.T) {
	cfg, err := applyV3Governance(session.Config{Model: "session/model"}, t.TempDir(), false, false)
	if err != nil {
		t.Fatalf("a fresh install has to boot: %v", err)
	}
	// The strictest default is the one an unconfigured install gets: a session
	// with no settings should ask, not run.
	if got := cfg.ApprovalPolicy.Check("bash", json.RawMessage(`{"command":"ls"}`)); got.Action != approval.ActionPrompt {
		t.Fatalf("an unconfigured gate answered %s", got)
	}
	// Pure reads never ask — the gate's business is what can change the system.
	if got := cfg.ApprovalPolicy.Check("read", json.RawMessage(`{"path":"x"}`)); got.Action != approval.ActionAllow {
		t.Fatalf("a fresh install asked about a read: %s", got)
	}
	// And every auxiliary call rides THE CLASS IT SHIPS ON rather than the model
	// the person is talking to. That changed when the crew landed
	// (internal/config's crew.go): a whole crew following the conversation means
	// the most expensive model in the build answering the cheapest questions in it.
	model, err := roles.Resolve(cfg.RolesSource, roles.RoleTitle, "session/model")
	if err != nil || model != config.DefaultLowModel {
		t.Fatalf("the title role resolved to %q (%v), want its class's %q", model, err, config.DefaultLowModel)
	}
	// The floor is still there and is still the conversation's model — it is what
	// a class somebody CLEARED falls to.
	cleared, err := applyV3Governance(session.Config{Model: "session/model"},
		v3Profile(t, map[string]any{"models.tiers.low": ""}), false, false)
	if err != nil {
		t.Fatalf("a cleared class has to boot: %v", err)
	}
	model, err = roles.Resolve(cleared.RolesSource, roles.RoleTitle, "session/model")
	if err != nil || model != "session/model" {
		t.Fatalf("the floor resolved to %q (%v)", model, err)
	}
	if cfg.SpendRailUSD != 0 {
		t.Fatalf("an unset ceiling is %v, want 0", cfg.SpendRailUSD)
	}
}

func TestAnUnreadableRowStopsTheLaunchAndNamesItself(t *testing.T) {
	// A typo in the approvals row is a rule somebody thinks is protecting them.
	// It must not be skipped, and the message must say which row to open.
	dir := v3Profile(t, map[string]any{"tools.approval": "bash:always"})
	if _, err := applyV3Governance(session.Config{}, dir, false, false); err == nil {
		t.Fatal("a malformed approvals row booted")
	} else if !strings.Contains(err.Error(), "tools.approval") || !strings.Contains(err.Error(), "always") {
		t.Fatalf("the error does not name the row and the value: %v", err)
	}

	dir = v3Profile(t, map[string]any{"tools.approval": "bash"})
	if _, err := applyV3Governance(session.Config{}, dir, false, false); err == nil {
		t.Fatal("a row that is not a pair booted")
	}

	dir = v3Profile(t, map[string]any{"models.roles": "title"})
	_, err := applyV3Governance(session.Config{}, dir, false, false)
	if err == nil || !strings.Contains(err.Error(), "models.roles") {
		t.Fatalf("a malformed roles row has to name itself: %v", err)
	}
}

func TestYoloIsADefaultAndNotAnOverride(t *testing.T) {
	dir := v3Profile(t, map[string]any{
		"tools.approvalMode": "prompt",
		"tools.approval":     "bash:prompt",
	})
	cfg, err := applyV3Governance(session.Config{}, dir, true, false)
	if err != nil {
		t.Fatal(err)
	}
	policy := *cfg.ApprovalPolicy
	// The posture: everything the person did not write down now runs.
	if got := policy.Check("edit", json.RawMessage(`{"path":"x"}`)); got.Action != approval.ActionAllow {
		t.Fatalf("--yolo left the default at %s", got)
	}
	// What they DID write down still stands. --yolo is "stop asking me about the
	// ordinary things", not "forget the rules I wrote".
	if got := policy.Check("bash", json.RawMessage(`{"command":"ls"}`)); got.Action != approval.ActionPrompt {
		t.Fatalf("--yolo overrode a written rule: %s", got)
	}
	// And without it the same profile asks.
	cfg, err = applyV3Governance(session.Config{}, dir, false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.ApprovalPolicy.Check("edit", json.RawMessage(`{"path":"x"}`)); got.Action != approval.ActionPrompt {
		t.Fatalf("the gate answered %s without --yolo", got)
	}
}

// ── the web-search rows ─────────────────────────────────────────────────────

// V3: The mapping from four settings rows to one [search.Options]: auto means no
// pin, a chosen plug is the pin, and a key comes from the shell or the sheet
// with the shell winning.
func TestTheSearchRowsBecomeSearchOptions(t *testing.T) {
	t.Setenv("EXA_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")
	t.Setenv("JINA_API_KEY", "")

	empty := v3SearchOptions(t.TempDir())
	if empty.Provider != "" || empty.ExaKey != "" || empty.FirecrawlKey != "" || empty.JinaKey != "" {
		t.Fatalf("an untouched profile produced %+v, want an empty auto configuration", empty)
	}

	dir := v3Profile(t, map[string]any{
		"search.provider":     "exa",
		"search.exaKey":       "from-the-sheet",
		"search.firecrawlKey": "firecrawl-from-the-sheet",
		"search.jinaKey":      "jina-from-the-sheet",
	})
	fromRows := v3SearchOptions(dir)
	if fromRows.Provider != "exa" {
		t.Fatalf("the pin did not reach the options: %q", fromRows.Provider)
	}
	if fromRows.ExaKey != "from-the-sheet" || fromRows.FirecrawlKey != "firecrawl-from-the-sheet" || fromRows.JinaKey != "jina-from-the-sheet" {
		t.Fatalf("the keys did not reach the options: %+v", fromRows)
	}

	t.Setenv("EXA_API_KEY", "from-the-shell")
	if got := v3SearchOptions(dir).ExaKey; got != "from-the-shell" {
		t.Fatalf("the environment lost to the sheet: %q", got)
	}
	t.Setenv("FIRECRAWL_API_KEY", "firecrawl-from-the-shell")
	if got := v3SearchOptions(dir).FirecrawlKey; got != "firecrawl-from-the-shell" {
		t.Fatalf("the Firecrawl environment lost to the sheet: %q", got)
	}

	// An auto row is the ABSENCE of a pin, not the word: internal/search reads
	// "auto" as a plug name and would find nothing registered under it.
	auto := v3Profile(t, map[string]any{"search.provider": "auto"})
	if got := v3SearchOptions(auto).Provider; got != "" {
		t.Fatalf("auto reached search as %q, want no pin at all", got)
	}
}

// And the pair itself reaches the session, resolved: a profile with no keys
// still gets both hands, and a pin moves the one it names.
func TestTheSearchPairReachesTheSessionConfig(t *testing.T) {
	t.Setenv("EXA_API_KEY", "")
	t.Setenv("FIRECRAWL_API_KEY", "")
	t.Setenv("JINA_API_KEY", "")

	cfg, err := applyV3Governance(session.Config{}, t.TempDir(), false, false)
	if err != nil {
		t.Fatalf("the rows did not load: %v", err)
	}
	if cfg.SearchProvider == nil || cfg.SearchFetcher == nil {
		t.Fatal("a keyless profile got no search pair; the zero-key rung is the whole point")
	}
	// V1: A fresh v3 session resolves its keyless search hand to Firecrawl.
	if got := cfg.SearchProvider.Name(); got != "firecrawl" {
		t.Fatalf("a keyless profile searches through %q, want the zero-key plug", got)
	}
	if got := cfg.SearchFetcher.Name(); got != "jina" {
		t.Fatalf("a keyless profile fetches through %q, want the zero-key plug", got)
	}

	t.Setenv("EXA_API_KEY", "a-key")
	keyed, err := applyV3Governance(session.Config{}, t.TempDir(), false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := keyed.SearchProvider.Name(); got != "exa" {
		t.Fatalf("a key in the shell did not upgrade the provider: %q", got)
	}

	// A pin beats the ladder, and the fetch half resolves on its own — pinning
	// the search plug leaves the fetcher free.
	pinned, err := applyV3Governance(session.Config{}, v3Profile(t, map[string]any{
		"search.provider": "duckduckgo",
	}), false, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := pinned.SearchProvider.Name(); got != "duckduckgo" {
		t.Fatalf("the pin lost to the key: %q", got)
	}
	if got := pinned.SearchFetcher.Name(); got != "exa-fetch" {
		t.Fatalf("pinning the search half moved the fetch half to %q", got)
	}
}
