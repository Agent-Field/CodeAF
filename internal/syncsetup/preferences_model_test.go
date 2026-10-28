package syncsetup

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

func TestModelSyncM1UnavailableProvider(t *testing.T) {
	t.Setenv(config.APIKeyEnv, "")
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	for _, m := range []*keyMachine{a, b} {
		m.Carry = []vaultsync.Carrier{preferences(m.profile, filepath.Dir(config.BudgetConfigPath(m.profile)))}
	}
	key := config.ModelSettingKey("talk")
	at := time.Now().Add(-time.Hour)
	b.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne, key: "deepseek/deepseek-chat"}, at)
	a.saveConfig(t, map[string]any{key: "ollama/llama3.2:latest"}, at.Add(time.Minute))
	before := config.PreferenceTime(b.profile, key)
	a.push(t)
	b.pull(t)
	if got := b.config(t)[key]; got != "deepseek/deepseek-chat" {
		t.Fatalf("M1 unavailable provider replaced local model: %v", got)
	}
	if got := config.PreferenceTime(b.profile, key); !got.Equal(before) {
		t.Fatalf("M2 skip advanced local clock: %v -> %v", before, got)
	}
}

func TestModelSyncM1MissingCustomProvider(t *testing.T) {
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	for _, m := range []*keyMachine{a, b} {
		m.Carry = []vaultsync.Carrier{preferences(m.profile, m.profile)}
		m.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne, config.KeyChatModel: "openai/gpt-5"}, time.Now().Add(-time.Hour))
	}
	mustNil(t, config.WriteSources(a.profile, []config.PersistedSource{{ID: "custom-local", Written: "local-models", Address: "http://127.0.0.1:11434/v1", KeyOptional: true}}))
	mustNil(t, config.WriteChatModel(a.profile, "local-models/qwen3"))
	a.push(t)
	b.pull(t)
	if got := config.ChatModelAt(b.profile); got != "openai/gpt-5" {
		t.Fatalf("M1 missing custom provider fell through to OpenRouter: %q", got)
	}
}

func TestModelSyncM1EverySlotUnavailable(t *testing.T) {
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	av, bv := map[string]any{}, map[string]any{config.KeyAPIKey: fakeKeyOne}
	for _, slot := range config.ModelSlots() {
		key := config.ModelSettingKey(slot.Slot)
		av[key], bv[key] = "ollama/qwen3", "openai/gpt-5"
	}
	a.saveConfig(t, av, time.Now().Add(-time.Minute))
	b.saveConfig(t, bv, time.Now().Add(-time.Hour))
	mustNil(t, config.WriteSources(a.profile, []config.PersistedSource{{ID: "ollama", Written: "ollama", Address: "http://127.0.0.1:11434/v1"}}))
	for _, m := range []*keyMachine{a, b} {
		m.Carry = []vaultsync.Carrier{preferences(m.profile, m.profile)}
	}
	a.push(t)
	b.pull(t)
	for _, slot := range config.ModelSlots() {
		key := config.ModelSettingKey(slot.Slot)
		if b.config(t)[key] != "openai/gpt-5" {
			t.Fatalf("M1 unavailable model applied to %s", key)
		}
	}
}

func TestModelSyncM2DifferentProvidersNeverFlap(t *testing.T) {
	for _, name := range config.ProviderKeyEnvNamesAt(t.TempDir()) {
		t.Setenv(name, "")
	}
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	for _, m := range []*keyMachine{a, b} {
		m.Carry = []vaultsync.Carrier{preferences(m.profile, m.profile)}
	}
	key := config.ModelSettingKey("talk")
	a.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne, key: "deepseek/deepseek-chat"}, time.Now().Add(-time.Hour))
	b.saveConfig(t, map[string]any{key: "ollama/llama3.2:latest"}, time.Now().Add(-time.Minute))
	mustNil(t, config.WriteSources(b.profile, []config.PersistedSource{{ID: "ollama", Written: "ollama", Address: "http://127.0.0.1:11434/v1"}}))
	mustNil(t, config.WriteDefaultEffort(a.profile, "high"))
	mustNil(t, config.WriteDailyBudgetUSD(b.profile, 7))
	for range 5 {
		a.push(t)
		b.push(t)
		a.pull(t)
		b.pull(t)
		if a.config(t)[key] != "deepseek/deepseek-chat" || b.config(t)[key] != "ollama/llama3.2:latest" {
			t.Fatalf("M2 model flapped: A=%v B=%v", a.config(t)[key], b.config(t)[key])
		}
	}
	budget, err := config.DailyBudgetUSDAt(a.profile)
	mustNil(t, err)
	if config.DefaultEffortAt(b.profile) != "high" || budget != 7 {
		t.Fatal("M2 non-model preferences stopped travelling")
	}
}

func TestModelSyncM3UsableProviderTravels(t *testing.T) {
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	for _, m := range []*keyMachine{a, b} {
		m.Carry = []vaultsync.Carrier{preferences(m.profile, m.profile)}
		m.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne}, time.Now())
	}
	mustNil(t, config.WriteChatModel(a.profile, "deepseek/deepseek-chat"))
	a.push(t)
	b.pull(t)
	if config.ChatModelAt(b.profile) != "deepseek/deepseek-chat" {
		t.Fatal("M3 usable model did not arrive")
	}
}

func TestModelSyncM3CustomAliasWithDifferentLocalIDs(t *testing.T) {
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	for i, m := range []*keyMachine{a, b} {
		m.Carry = []vaultsync.Carrier{preferences(m.profile, m.profile)}
		id := []string{"custom", "custom-lab"}[i]
		mustNil(t, config.WriteSources(m.profile, []config.PersistedSource{{ID: id, Written: "lab", Address: "http://127.0.0.1:11434/v1", KeyOptional: true}}))
	}
	mustNil(t, config.WriteChatModel(a.profile, "lab/qwen3"))
	a.push(t)
	b.pull(t)
	if config.ChatModelAt(b.profile) != "lab/qwen3" {
		t.Fatal("M3 usable custom alias was refused because of a machine-local ID")
	}
}

func TestModelSyncM1LegacyChoiceMetadataKeepsItsClock(t *testing.T) {
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	mustNil(t, config.WriteSources(a.profile, []config.PersistedSource{{ID: "custom", Written: "lab", Address: "http://127.0.0.1:11434/v1", KeyOptional: true}}))
	mustNil(t, config.WriteChatModel(a.profile, "lab/qwen3"))
	b.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne, config.KeyChatModel: "openai/gpt-5"}, time.Now().Add(-time.Hour))
	ledger := filepath.Join(a.profile, "preferences-sync.ledger")
	a.Carry = []vaultsync.Carrier{vaultsync.Keys("preferences", legacyModelPreferences{a.profile}, ledger)}
	a.push(t)
	before, err := a.Vault.(*keys.Vault).Revisions("home:keys:preferences")
	mustNil(t, err)
	a.Carry = []vaultsync.Carrier{preferences(a.profile, a.profile)}
	b.Carry = []vaultsync.Carrier{preferences(b.profile, b.profile)}
	a.push(t)
	b.pull(t)
	after, err := a.Vault.(*keys.Vault).Revisions("home:keys:preferences")
	mustNil(t, err)
	if after[config.KeyChatModel].Stamp != before[config.KeyChatModel].Stamp {
		t.Fatal("M1 metadata upgrade advanced the model choice clock")
	}
	if config.ChatModelAt(b.profile) != "openai/gpt-5" {
		t.Fatal("M1 a legacy custom selection reached a receiver without its provider")
	}
}

func TestModelSyncM1LegacyReceiverCannotDescribeSendersCustomProvider(t *testing.T) {
	r := newKeyRig(t)
	a, b, c := r.machine(), r.machine(), r.machine()
	b.saveConfig(t, map[string]any{}, time.Now().Add(-time.Hour))
	mustNil(t, config.WriteSources(a.profile, []config.PersistedSource{{ID: "custom", Written: "lab", Address: "http://127.0.0.1:11434/v1", KeyOptional: true}}))
	mustNil(t, config.WriteChatModel(a.profile, "lab/qwen3"))
	for _, m := range []*keyMachine{a, b} {
		m.Carry = []vaultsync.Carrier{vaultsync.Keys("preferences", legacyModelPreferences{m.profile}, filepath.Join(m.profile, "preferences-sync.ledger"))}
	}
	a.push(t)
	b.pull(t)
	b.Carry = []vaultsync.Carrier{preferences(b.profile, b.profile)}
	b.push(t)
	revisions, err := b.Vault.(*keys.Vault).Revisions("home:keys:preferences")
	mustNil(t, err)
	if revisions[config.KeyChatModel].Metadata != "" {
		t.Fatal("M1 receiver claimed provider identity for an old received model")
	}
	a.Carry = []vaultsync.Carrier{preferences(a.profile, a.profile)}
	a.push(t)
	c.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne, config.KeyChatModel: "openai/gpt-5"}, time.Now().Add(-time.Hour))
	c.Carry = []vaultsync.Carrier{preferences(c.profile, c.profile)}
	c.pull(t)
	if config.ChatModelAt(c.profile) != "openai/gpt-5" {
		t.Fatal("M1 receiver-first upgrade poisoned the provider metadata")
	}
}

type legacyModelPreferences struct{ dir string }

func (p legacyModelPreferences) Values() (map[string]string, error) {
	return config.ReadPreferences(p.dir)
}
func (p legacyModelPreferences) Holds(key string) bool { return config.PreferenceKey(key) }
func (p legacyModelPreferences) Apply(changes map[string]string) error {
	return config.ApplyPreferences(p.dir, changes)
}
func (p legacyModelPreferences) KeyTime(key string) time.Time {
	return config.PreferenceTime(p.dir, key)
}

func TestModelSyncM4ProviderSetupDoesNotReplaySkippedChoice(t *testing.T) {
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	for _, m := range []*keyMachine{a, b} {
		m.Carry = []vaultsync.Carrier{preferences(m.profile, m.profile)}
		m.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne, config.ModelSettingKey("talk"): "deepseek/deepseek-chat"}, time.Now().Add(-time.Hour))
	}
	mustNil(t, config.WriteSources(a.profile, []config.PersistedSource{{ID: "ollama", Written: "ollama", Address: "http://127.0.0.1:11434/v1"}}))
	mustNil(t, config.WriteChatModel(a.profile, "ollama/llama3.2:latest"))
	a.push(t)
	b.pull(t)
	mustNil(t, config.WriteSources(b.profile, []config.PersistedSource{{ID: "ollama", Written: "ollama", Address: "http://127.0.0.1:11434/v1"}}))
	// Recreate the carrier to prove the rejection survives a process restart.
	b.Carry = []vaultsync.Carrier{preferences(b.profile, b.profile)}
	for range 3 {
		b.push(t)
		a.push(t)
		b.pull(t)
	}
	if config.ChatModelAt(b.profile) != "deepseek/deepseek-chat" {
		t.Fatal("M4 provider setup replayed an old choice")
	}
	mustNil(t, config.WriteChatModel(a.profile, "ollama/qwen3:latest"))
	a.push(t)
	b.pull(t)
	if config.ChatModelAt(b.profile) != "ollama/qwen3:latest" {
		t.Fatalf("M4 a newer usable choice did not arrive: A=%q B=%q", config.ChatModelAt(a.profile), config.ChatModelAt(b.profile))
	}
	mustNil(t, config.WriteChatModel(a.profile, "ollama/llama3.2:latest"))
	a.push(t)
	b.pull(t)
	if config.ChatModelAt(b.profile) != "ollama/llama3.2:latest" {
		t.Fatal("M4 a new revision of the originally skipped value did not arrive")
	}
}
