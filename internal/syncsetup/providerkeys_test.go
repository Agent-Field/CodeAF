package syncsetup

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/keys"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

// The values are obvious fakes; no test prints a key.
const (
	fakeKeyOne = "FAKE-key-one"
	fakeKeyTwo = "FAKE-key-two"
)

// keyRig is one person's machines over a shared store and directory, each with
// its own profile directory holding a config.json.
type keyRig struct {
	t     *testing.T
	store *blobstore.Memory
	dir   *directory.Memory
	id    identity.Identity
	n     int
}

type keyMachine struct {
	vaultsync.Syncer
	profile string
}

func newKeyRig(t *testing.T) *keyRig {
	return &keyRig{t: t, store: blobstore.NewMemory(), dir: directory.NewMemory(time.Now)}
}

func (r *keyRig) machine() *keyMachine {
	r.t.Helper()
	r.n++
	home := filepath.Join(r.t.TempDir(), "home")
	if r.n == 1 {
		id, err := identity.Ensure(home)
		if err != nil {
			r.t.Fatal(err)
		}
		r.id = id
	} else if err := identity.Save(home, r.id); err != nil {
		r.t.Fatal(err)
	}
	v, err := keys.Open(home)
	if err != nil {
		r.t.Fatal(err)
	}
	dev := "dev_" + strings.Repeat(string(rune('a'+r.n)), 32)
	m := &keyMachine{profile: filepath.Join(r.t.TempDir(), "profile")}
	m.Syncer = vaultsync.Syncer{Store: r.store, Dir: r.dir.For(dev), Vault: v, CellKeyID: r.id.CellKeyID(),
		Carry: []vaultsync.Carrier{providerKeys(m.profile, home)}}
	return m
}

// saveConfig writes config.json as a person's save would, at a chosen time.
func (m *keyMachine) saveConfig(t *testing.T, fields map[string]any, at time.Time) {
	t.Helper()
	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatal(err)
	}
	path := config.BudgetConfigPath(m.profile)
	for _, step := range []error{os.MkdirAll(m.profile, 0o700), os.WriteFile(path, raw, 0o600), os.Chtimes(path, at, at)} {
		if step != nil {
			t.Fatal(step)
		}
	}
}

func (m *keyMachine) config(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(config.BudgetConfigPath(m.profile))
	if os.IsNotExist(err) {
		return map[string]any{}
	}
	var out map[string]any
	if err != nil || json.Unmarshal(raw, &out) != nil {
		t.Fatalf("config.json unreadable: %v", err)
	}
	return out
}

func (m *keyMachine) push(t *testing.T) { t.Helper(); mustNil(t, m.Push(context.Background())) }
func (m *keyMachine) pull(t *testing.T) { t.Helper(); mustNil(t, m.Pull(context.Background())) }

func mustNil(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func (m *keyMachine) wantKey(t *testing.T, want string) {
	t.Helper()
	if got := m.config(t)[config.KeyAPIKey]; got != want {
		t.Fatalf("api_key on this machine matches the wanted key: %v", got == want)
	}
}

func TestProviderKeySetOnAIsUsableOnB(t *testing.T) {
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	a.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne}, time.Now())
	a.push(t)
	b.pull(t)
	if got := config.PersistedAPIKey(b.profile); got != fakeKeyOne {
		t.Fatal("the key B's sessions resolve is not A's key")
	}
	if info, err := os.Stat(config.BudgetConfigPath(b.profile)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("config.json must stay private: %v %v", err, info)
	}
}

// B's newer key survives A's later push, when A changed nothing about it.
func TestProviderKeyNewerOnBIsNotClobbered(t *testing.T) {
	a, b := syncedPair(t)
	b.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyTwo}, time.Now())
	b.push(t)
	a.push(t)
	b.pull(t)
	a.wantKey(t, fakeKeyTwo)
	b.wantKey(t, fakeKeyTwo)
}

// An edit to something else in A's config.json does not make A's old key look
// newer: only a key whose value changed is stamped.
func TestUnrelatedConfigEditOnADoesNotBeatBsNewerKey(t *testing.T) {
	a, b := syncedPair(t)
	b.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyTwo}, time.Now())
	b.push(t)
	a.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne, "daily_budget": 7.0}, time.Now())
	a.push(t)
	b.pull(t)
	a.pull(t)
	a.wantKey(t, fakeKeyTwo)
	b.wantKey(t, fakeKeyTwo)
	if a.config(t)["daily_budget"] != 7.0 {
		t.Fatal("A's own budget was lost")
	}
}

// syncedPair is two machines that both hold key one.
func syncedPair(t *testing.T) (a, b *keyMachine) {
	t.Helper()
	r := newKeyRig(t)
	a, b = r.machine(), r.machine()
	a.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne}, time.Now())
	a.push(t)
	b.pull(t)
	return a, b
}

// B lists no service X. Its push of an unrelated key edit neither tombstones
// nor drops A's key for X: it stays in the vault, and on A.
func TestProviderKeyOfAServiceBLacksSurvivesBsPush(t *testing.T) {
	r := newKeyRig(t)
	a, b, c := r.machine(), r.machine(), r.machine()
	a.withService(t, "acme", "FAKE-acme")
	a.push(t)
	b.pull(t)
	b.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyTwo}, time.Now())
	b.push(t)
	a.pull(t)
	if got, _ := config.ReadProviderKeys(a.profile); got.Sources["acme"] != "FAKE-acme" {
		t.Fatal("A lost its service key")
	}
	c.withService(t, "acme", "")
	c.pull(t)
	if got, _ := config.ReadProviderKeys(c.profile); got.Sources["acme"] != "FAKE-acme" {
		t.Fatal("the vault lost the service key")
	}
}

// withService lists a service on this machine, with an inline key when one is given.
func (m *keyMachine) withService(t *testing.T, id, key string) {
	t.Helper()
	mustNil(t, config.WriteSources(m.profile, []config.PersistedSource{{ID: id, Written: id, Region: "us", Key: key}}))
}

func TestProviderKeyRemovedOnATombstonesOnB(t *testing.T) {
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	a.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne}, time.Now())
	a.push(t)
	b.pull(t)
	a.saveConfig(t, map[string]any{}, time.Now())
	a.push(t)
	b.pull(t)
	if _, kept := b.config(t)[config.KeyAPIKey]; kept {
		t.Fatal("the removal did not reach B")
	}
}

// B's budget, its model pick and a field a later version might add survive a
// key arriving, untouched.
func TestProviderKeyLeavesEveryOtherConfigFieldOnB(t *testing.T) {
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	a.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne, "daily_budget": 99.0}, time.Now())
	a.push(t)
	own := map[string]any{"daily_budget": 5.0, "future_field": map[string]any{"a": "b"}}
	b.saveConfig(t, own, time.Now().Add(-time.Hour))
	b.pull(t)
	got := b.config(t)
	b.wantKey(t, fakeKeyOne)
	delete(got, config.KeyAPIKey)
	want, _ := json.Marshal(own)
	have, _ := json.Marshal(got)
	if string(want) != string(have) {
		t.Fatalf("B's own fields changed: %s", have)
	}
	if a.config(t)["daily_budget"] != 99.0 {
		t.Fatal("A's budget was touched")
	}
}

// A key that lives in the environment is not in config.json, so it is never
// stored in the vault.
func TestProviderKeyFromTheEnvironmentIsNeverCaptured(t *testing.T) {
	t.Setenv(config.APIKeyEnv, fakeKeyOne)
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	a.saveConfig(t, map[string]any{"daily_budget": 5.0}, time.Now())
	a.push(t)
	b.pull(t)
	if _, has := b.config(t)[config.KeyAPIKey]; has {
		t.Fatal("an environment key reached B's config.json")
	}
	if _, err := os.Stat(config.BudgetConfigPath(b.profile)); err == nil {
		t.Fatal("B has a config.json from nothing")
	}
}

// A key set on A before its first sync, with the rest of a real config beside it,
// against a vault that has never seen it, is neither dropped on A by the start-up
// sync nor lost on the way to B. A machine never removes a key it has never
// shared: no ledger entry is never a removal.
func TestProviderKeySetBeforeTheFirstSyncSurvivesTheStartUpSync(t *testing.T) {
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	a.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne, "daily_budget_usd": 500.0, "model_pool": false, "setup_seen_at": "2026-09-29T00:00:00Z"}, time.Now())
	a.push(t) // the chat starts: capture, then push against an empty vault
	a.push(t) // and its next flush
	a.wantKey(t, fakeKeyOne)
	b.pull(t)
	b.wantKey(t, fakeKeyOne)
}
