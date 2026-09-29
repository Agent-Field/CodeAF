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
		Carry: []vaultsync.Carried{providerKeys(m.profile)}}
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

func TestProviderKeyNewerOnBIsNotClobbered(t *testing.T) {
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	now := time.Now()
	a.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne}, now.Add(-time.Hour))
	a.push(t)
	b.pull(t)
	b.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyTwo}, now.Add(-time.Minute))
	a.saveConfig(t, map[string]any{config.KeyAPIKey: "FAKE-key-older"}, now.Add(-30*time.Minute))
	b.push(t)
	a.push(t)
	b.pull(t)
	a.wantKey(t, fakeKeyTwo)
	b.wantKey(t, fakeKeyTwo)
}

func TestProviderKeyRemovedOnAIsRemovedOnB(t *testing.T) {
	r := newKeyRig(t)
	a, b := r.machine(), r.machine()
	a.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne}, time.Now().Add(-time.Hour))
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
