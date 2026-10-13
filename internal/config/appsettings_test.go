package config

import (
	"encoding/json"
	"github.com/Agent-Field/codeaf/internal/roles"
	"os"
	"strings"
	"sync"
	"testing"
)

func appPatch(raw string) map[string]json.RawMessage {
	var patch map[string]json.RawMessage
	_ = json.Unmarshal([]byte(raw), &patch)
	return patch
}

func TestAppSettingsAtomicPatchSharesNativeKeysAndPreservesSecrets(t *testing.T) {
	dir := t.TempDir()
	if err := writeProfileValues(dir, map[string]any{KeyAPIKey: "terminal-secret", KeyGitHubToken: "terminal-github", "future": true}); err != nil {
		t.Fatal(err)
	}
	restore := UseAppCredentials(NewAppCredentials())
	defer restore()
	_, version, err := AppSettingsAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	patch := appPatch(`{"models.default":"vendor/selected:free","models.effort":"medium","spending.day_cap":{"usd":"20.00"},"spending.per_task":{"usd":"2.00"},"crews.depth":3,"models.roles.planner":{"model":"vendor/brain","effort":"high"}}`)
	if err := ApplyAppSettings(dir, patch, version); err != nil {
		t.Fatal(err)
	}
	if ChatModelAt(dir) != "vendor/selected:free" || CrewTaskCapAt(dir) != 2 || TaskAutoApproveAt(dir) != DefaultTaskAutoApprove {
		t.Fatal("native readers lost their settings")
	}
	if cap, err := DailyBudgetUSDAt(dir); err != nil || cap != 20 {
		t.Fatalf("cap=%v %v", cap, err)
	}
	pins, err := ParseModelRoles(ModelRolesAt(dir))
	if err != nil || pins["planner"] != "vendor/brain:high" {
		t.Fatalf("role choice does not reach engine: %v %v", pins, err)
	}
	raw, err := os.ReadFile(BudgetConfigPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "terminal-secret") || !strings.Contains(string(raw), "terminal-github") || !strings.Contains(string(raw), "future") {
		t.Fatal("app patch discarded terminal rows")
	}
	values, _, err := AppSettingsAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(values)
	if strings.Contains(string(encoded), "secret") || strings.Contains(string(encoded), "terminal-github") {
		t.Fatal("app settings disclosed terminal credentials")
	}
}

func TestAppSettingsRejectEveryInvalidRowWithoutPartialWrites(t *testing.T) {
	dir := t.TempDir()
	if err := WriteChatModel(dir, "vendor/original"); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(BudgetConfigPath(dir))
	invalid := []string{
		`{"models.effort":"max"}`, `{"models.effort":"xhigh"}`, `{"models.effort":"auto"}`,
		`{"models.pinned":["a","b"]}`, `{"models.pinned":["a","a",""]}`, `{"models.roles.checks":{"model":"a","effort":"high","extra":true}}`,
		`{"spending.day_cap":{"usd":"-1.00"}}`, `{"spending.day_cap":{"usd":"1"}}`, `{"spending.day_cap":20}`,
		`{"permissions.always_ask":[]}`, `{"permissions.mode":"allow"}`, `{"memory.agents_may_add":1}`,
		`{"advanced.proposal_s":61}`, `{"advanced.proposal_s":0.5}`, `{"advanced.work_folder":"relative"}`,
		`{"accounts.github":"allow"}`, `{"crews.depth":0}`, `{"crews.share_pct":101}`, `{"factory.polling_s":0}`,
		`{"advanced.context_tokens":0}`, `{"permissions.question_clock_min":0}`, `{"advanced.idle_days":-1}`,
		`{"theme":"dark"}`, `{"api_key":"new-secret"}`, `{"models.roles.guardian":{}}`,
	}
	for _, raw := range invalid {
		patch := appPatch(raw)
		patch["models.default"] = json.RawMessage(`"vendor/changed"`)
		if err := ApplyAppSettings(dir, patch, ""); err == nil {
			t.Errorf("accepted %s", raw)
		}
		after, _ := os.ReadFile(BudgetConfigPath(dir))
		if string(after) != string(before) {
			t.Fatalf("partial write for %s", raw)
		}
	}
}

func TestAppSettingsConcurrentExpectedVersionHasOneWinner(t *testing.T) {
	dir := t.TempDir()
	_, version, err := AppSettingsAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for _, raw := range []string{`{"advanced.idle_days":8}`, `{"advanced.idle_days":9}`} {
		wg.Go(func() { results <- ApplyAppSettings(dir, appPatch(raw), version) })
	}
	wg.Wait()
	close(results)
	success, stale := 0, 0
	for err := range results {
		if err == nil {
			success++
		} else if err == ErrAppStale {
			stale++
		} else {
			t.Fatal(err)
		}
	}
	if success != 1 || stale != 1 {
		t.Fatalf("winners=%d stale=%d", success, stale)
	}
}

func TestAppSettingsDefaultsAndAllRowsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	values, _, err := AppSettingsAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	if values["models.effort"] != "medium" || values["advanced.proposal_s"] != 12 || values["permissions.mode"] != "ask_risky" {
		t.Fatalf("wrong defaults: %v", values)
	}
	values["models.default"] = "vendor/model"
	raw, _ := json.Marshal(values)
	if err := ApplyAppSettings(dir, appPatch(string(raw)), ""); err != nil {
		t.Fatal(err)
	}
	if TaskAutoApproveAt(dir) != 12 {
		t.Fatal("proposal setting does not reach the engine")
	}
	if err := ApplyAppSettings(dir, appPatch(`{"accounts.mcp:calendar":"off"}`), ""); err != nil {
		t.Fatal(err)
	}
	reread, _, err := AppSettingsAt(dir)
	if err != nil || reread["accounts.mcp:calendar"] != "off" {
		t.Fatal("MCP preference was not remembered")
	}
}

func TestAppSettingsMalformedSavedRowsCannotBreakTheCodec(t *testing.T) {
	dir := t.TempDir()
	if err := writeProfileValues(dir, map[string]any{KeyChatModel: 42, KeyEffort: "max", "app.models.pinned": true}); err != nil {
		t.Fatal(err)
	}
	values, _, err := AppSettingsAt(dir)
	if err != nil || values["models.default"] != "" || values["models.effort"] != "high" {
		t.Fatalf("bad saved settings: %v %v", values, err)
	}
}

func TestAppRolesFollowTheAgentAndReflectNativeTerminalEdits(t *testing.T) {
	dir := t.TempDir()
	source := AppRoleSource(dir, "vendor/agent", "medium", nil)
	for _, engines := range appRoleEngines {
		for _, engine := range engines {
			if got, ok := source(roles.PinKey(engine)); !ok || got != "vendor/agent:medium" {
				t.Fatalf("unpinned role %s=%q", engine, got)
			}
		}
	}
	if err := ApplyAppSettings(dir, appPatch(`{"models.roles.checks":{"model":"vendor/check","effort":"high"}}`), ""); err != nil {
		t.Fatal(err)
	}
	if got, _ := source(roles.PinKey(roles.RoleAuditor)); got != "vendor/check:high" {
		t.Fatal("next role call ignored app choice")
	}
	if err := writeProfileValue(dir, KeyModelRoles, "auditor:vendor/terminal:low"); err != nil {
		t.Fatal(err)
	}
	values, _, err := AppSettingsAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(values["models.roles.checks"])
	var choice AppRoleChoice
	_ = json.Unmarshal(raw, &choice)
	if choice.Model != "vendor/terminal" || choice.Effort != "low" {
		t.Fatal("app role row drifted from native pins")
	}
	if got, _ := source(roles.PinKey(roles.RoleAuditor)); got != "vendor/terminal:low" {
		t.Fatal("native edit missed the role source")
	}
}

func TestAppSettingsAreRecognizedByTheSharedProfileReader(t *testing.T) {
	dir := t.TempDir()
	values, _, err := AppSettingsAt(dir)
	if err != nil {
		t.Fatal(err)
	}
	values["models.default"] = "vendor/model"
	values["accounts.mcp:calendar"] = "off"
	raw, _ := json.Marshal(values)
	if err := WriteAppSetup(dir, 2, appPatch(string(raw)), ""); err != nil {
		t.Fatal(err)
	}
	held, err := readProfileConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if unread := warnUnreadProfileKeys(dir, held); len(unread) != 0 {
		t.Fatalf("app settings reported unread: %v", unread)
	}
	held = map[string]json.RawMessage{"app.typo": json.RawMessage(`true`)}
	if unread := warnUnreadProfileKeys(t.TempDir(), held); len(unread) != 1 {
		t.Fatal("app namespace swallowed an unknown key")
	}
}
