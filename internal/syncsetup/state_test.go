package syncsetup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/store"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

// TestC30PreferencesMergePerSetting proves C30.3: concurrent different-setting
// edits survive, actual edit time decides the same setting, and paths stay local.
func TestC30PreferencesMergePerSetting(t *testing.T) {
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	a.Carry = []vaultsync.Carrier{preferences(a.profile, filepath.Dir(config.BudgetConfigPath(a.profile)))}
	b.Carry = []vaultsync.Carrier{preferences(b.profile, filepath.Dir(config.BudgetConfigPath(b.profile)))}
	at := time.Unix(1700000000, 0)
	a.saveConfig(t, map[string]any{config.ModelSettingKey("talk"): "stub/one", config.KeyIcons: "plain", "machine.path": "/a"}, at)
	b.saveConfig(t, map[string]any{"machine.path": "/b"}, at)
	a.push(t)
	b.pull(t)
	a.saveConfig(t, map[string]any{config.ModelSettingKey("talk"): "stub/older", config.KeyIcons: "plain", config.KeyDailyBudget: 7.0, "machine.path": "/a"}, at.Add(time.Second))
	b.saveConfig(t, map[string]any{config.ModelSettingKey("talk"): "stub/later", config.KeyIcons: "ascii", "machine.path": "/b"}, at.Add(2*time.Second))
	b.push(t)
	a.push(t)
	b.pull(t)
	for _, m := range []*keyMachine{a, b} {
		got := m.config(t)
		if got[config.ModelSettingKey("talk")] != "stub/later" || got[config.KeyIcons] != "ascii" || got[config.KeyDailyBudget] != 7.0 {
			t.Fatalf("merge lost preferences: %v", got)
		}
	}
	if a.config(t)["machine.path"] != "/a" || b.config(t)["machine.path"] != "/b" {
		t.Fatal("machine path travelled")
	}
}

// TestC30MemoryRoundWithoutAChat proves C30.2: hand-written lines and forgotten
// lines travel without continuing any chat; concurrent different lines survive.
func TestC30MemoryRoundWithoutAChat(t *testing.T) {
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	ah, bh := t.TempDir(), t.TempDir()
	a.Carry = []vaultsync.Carrier{sharedMemories(ah)}
	b.Carry = []vaultsync.Carrier{sharedMemories(bh)}
	ab, err := store.Open(filepath.Join(ah, "graph.db"))
	mustNil(t, err)
	defer ab.Close()
	bb, err := store.Open(filepath.Join(bh, "graph.db"))
	mustNil(t, err)
	defer bb.Close()
	one, err := ab.AddMemory(store.Memory{Owner: store.OwnerUser, Text: "Use short paragraphs.", Origin: store.OriginPerson})
	mustNil(t, err)
	_, err = ab.AddMemory(store.Memory{Owner: store.OwnerMachine, Text: "Files on this machine live in C:/local."})
	mustNil(t, err)
	a.push(t)
	b.pull(t)
	got, found, err := bb.MemoryRecord(one.ID)
	mustNil(t, err)
	if !found || got.Text != one.Text {
		t.Fatal("hand-written memory did not arrive")
	}
	lines, err := bb.MemoriesFor([]string{store.OwnerMachine})
	mustNil(t, err)
	if len(lines) != 0 {
		t.Fatal("machine memory travelled")
	}
	_, err = ab.AddMemory(store.Memory{Owner: store.OwnerUser, Text: "Prefer named constants."})
	mustNil(t, err)
	_, err = bb.AddMemory(store.Memory{Owner: store.OwnerUser, Text: "Keep examples runnable."})
	mustNil(t, err)
	mustNil(t, bb.UpdateMemory(one.ID, "Use concise paragraphs."))
	b.push(t)
	a.push(t)
	b.pull(t)
	for _, brain := range []*store.Store{ab, bb} {
		lines, err := brain.MemoriesFor([]string{store.OwnerUser})
		mustNil(t, err)
		if len(lines) != 3 {
			t.Fatalf("lost concurrent line: %d", len(lines))
		}
	}
	mustNil(t, ab.ForgetMemory(one.ID))
	a.push(t)
	b.pull(t)
	got, found, err = bb.MemoryRecord(one.ID)
	mustNil(t, err)
	if !found || got.Status != store.MemoryForgotten {
		t.Fatal("forget did not travel")
	}
	mustNil(t, bb.RestoreMemory(one.ID))
	b.push(t)
	a.pull(t)
	got, found, err = ab.MemoryRecord(one.ID)
	mustNil(t, err)
	if !found || got.Status != store.MemoryActive {
		t.Fatal("restoring a forgotten line did not travel")
	}

}

// TestC30QuietStateDoesNotCapture proves C30.4: unpaired state sync is inert,
// including no vault, memory database or lockfile being created.
func TestC30QuietStateDoesNotCapture(t *testing.T) {
	home := t.TempDir()
	s := &Sync{Home: home}
	if err := s.SyncState(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(home)
	mustNil(t, err)
	if len(entries) != 0 {
		t.Fatal("unpaired home state was captured")
	}
}

// TestC30PairingChangesTheLiveStateRound proves C30.4: Home opened before a
// pairing must use the adopted fleet on its next round, without a restart.
// Authenticated loopback handlers distinguish the two identities' vaults.
func TestC30PairingChangesTheLiveStateRound(t *testing.T) {
	server := relay(t)
	home := machine(t, server.URL)
	t.Setenv("CODEAF_PROFILE_DIR", home)
	old, ok, err := Open(home)
	if err != nil || !ok {
		t.Fatalf("open original home: %v", err)
	}
	fleet, err := identity.Ensure(t.TempDir())
	mustNil(t, err)
	_, err = identity.Adopt(home, fleet, true)
	mustNil(t, err)
	mustNil(t, config.WriteChatModel(home, "stub/after-pair"))
	current, err := old.stateRound(t.Context())
	mustNil(t, err)
	if current.Identity.ID() != fleet.ID() {
		t.Fatal("running worker retained the pre-pairing signer")
	}
	now, err := current.Dir.List(t.Context())
	mustNil(t, err)
	before, err := old.Dir.List(t.Context())
	mustNil(t, err)
	if now.Identity.Vault == "" || before.Identity.Vault != "" {
		t.Fatal("state was not sent exclusively to the adopted fleet")
	}
}
