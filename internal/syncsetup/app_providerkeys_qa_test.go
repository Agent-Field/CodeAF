package syncsetup

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/vaultsync"
)

func (r *keyRig) appMachine(credentials *config.AppCredentials) *keyMachine {
	defer config.UseAppCredentials(credentials)()
	return r.machine()
}

func TestQADesktopProviderKeysUseVaultAndCredentialHandoff(t *testing.T) {
	r := newKeyRig(t)
	appA, appB := config.NewAppCredentials(), config.NewAppCredentials()
	a, b := r.appMachine(appA), r.appMachine(appB)
	kept := map[string]string{}
	appB.OnKept(func(provider, key string) { kept[provider] = key })
	// Pairing has completed before either provider key is added.
	a.push(t)
	b.pull(t)
	appA.Set(modelsource.DefaultID, fakeKeyOne)
	appA.Set("aiand", fakeKeyTwo)
	appA.Set("github", "FAKE-unrelated-credential")
	a.push(t)
	b.pull(t)
	for provider, want := range map[string]string{modelsource.DefaultID: fakeKeyOne, "aiand": fakeKeyTwo} {
		if got, known := appB.Key(provider); !known || got != want || kept[provider] != want {
			t.Fatalf("%s was not delivered to the receiving app's credential store", provider)
		}
	}
	if _, known := appB.Key("github"); known {
		t.Fatal("the model-provider carrier copied an unrelated credential")
	}
	for _, machine := range []*keyMachine{a, b} {
		raw, err := os.ReadFile(config.BudgetConfigPath(machine.profile))
		if err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
		if bytes.Contains(raw, []byte(fakeKeyOne)) || bytes.Contains(raw, []byte(fakeKeyTwo)) {
			t.Fatal("desktop provider keys were written to a plaintext profile")
		}
	}
	appB.Remove("aiand")
	b.push(t)
	a.pull(t)
	if key, known := appA.Key("aiand"); !known || key != "" {
		t.Fatal("disconnecting a provider did not propagate through the encrypted vault")
	}
}

func TestQADesktopProviderKeyUnreadAtLaunchDoesNotDeletePeerKeys(t *testing.T) {
	r := newKeyRig(t)
	appA, appB := config.NewAppCredentials(), config.NewAppCredentials()
	a, b := r.appMachine(appA), r.appMachine(appB)
	appA.Set(modelsource.DefaultID, fakeKeyOne)
	a.push(t)
	b.pull(t)

	// Restart A with the same vault and ledger before the native credential
	// store has supplied any keys. A round can run even if the store is locked.
	func() {
		defer config.UseAppCredentials(config.NewAppCredentials())()
		a.Carry = []vaultsync.Carrier{providerKeys(a.profile, a.home)}
	}()
	a.push(t)
	b.pull(t)
	if got, _ := appB.Key(modelsource.DefaultID); got != fakeKeyOne {
		t.Fatal("a missing startup handoff removed the peer's saved key")
	}
}

func TestQADesktopProviderKeyDisconnectDuringPullStaysRemoved(t *testing.T) {
	r := newKeyRig(t)
	appA, appB := config.NewAppCredentials(), config.NewAppCredentials()
	a, b := r.appMachine(appA), r.appMachine(appB)
	kept := 0
	appB.OnKept(func(string, string) { kept++ })
	mustNil(t, b.Capture())
	appB.Remove(modelsource.DefaultID)
	for _, remote := range []string{fakeKeyOne, fakeKeyTwo} {
		appA.Set(modelsource.DefaultID, remote)
		a.push(t)
		// Pull preserves the pre-request snapshot, including on a repeated merge.
		b.pull(t)
		if got, known := appB.Key(modelsource.DefaultID); !known || got != "" || kept != 0 {
			t.Fatal("the incoming key reconnected a locally disconnected provider")
		}
	}
	b.push(t)
	a.pull(t)
	if got, known := appA.Key(modelsource.DefaultID); !known || got != "" {
		t.Fatal("the next capture failed to share the protected disconnect")
	}
}

func TestQADesktopProviderKeySecondMergeFollowsAnAppliedRemoval(t *testing.T) {
	r := newKeyRig(t)
	appA, appB := config.NewAppCredentials(), config.NewAppCredentials()
	a, b := r.appMachine(appA), r.appMachine(appB)
	appA.Set(modelsource.DefaultID, fakeKeyOne)
	a.push(t)
	b.pull(t)
	mustNil(t, b.Capture())

	appA.Remove(modelsource.DefaultID)
	a.push(t)
	b.pull(t)
	if got, known := appB.Key(modelsource.DefaultID); !known || got != "" {
		t.Fatal("the first merge did not apply the upstream removal")
	}
	appA.Set(modelsource.DefaultID, fakeKeyTwo)
	a.push(t)
	b.pull(t)
	if got, _ := appB.Key(modelsource.DefaultID); got != fakeKeyTwo {
		t.Fatal("the second merge mistook its own applied removal for a local edit")
	}
}

func TestQADesktopProviderKeysKeepEndpointCredentialsLocal(t *testing.T) {
	r := newKeyRig(t)
	legacy := r.machine()
	appA, appB := config.NewAppCredentials(), config.NewAppCredentials()
	a, b := r.appMachine(appA), r.appMachine(appB)
	observer := r.machine()
	rows := func(address, key string) []config.PersistedSource {
		return []config.PersistedSource{
			{ID: modelsource.CustomID, Written: "custom", Address: address, Key: key},
			{ID: "custom-second", Written: "second", Address: address, Key: key},
			{ID: "ollama", Written: "ollama", Address: address, Key: key},
		}
	}
	mustNil(t, config.WriteSources(observer.profile, rows("https://first.invalid/v1", "")))
	legacy.saveConfig(t, map[string]any{config.KeyAPIKey: fakeKeyOne, "model_sources": rows("https://first.invalid/v1", fakeKeyOne)}, time.Now())
	mustNil(t, config.WriteSources(a.profile, rows("https://first.invalid/v1", fakeKeyTwo)))
	mustNil(t, config.WriteSources(b.profile, rows("https://other.invalid/v1", "")))
	legacy.push(t)
	a.push(t)
	b.pull(t)
	if got, _ := appB.Key(modelsource.DefaultID); got != fakeKeyOne {
		t.Fatal("the desktop did not restore a built-in key from the legacy slot")
	}
	observer.pull(t)
	observed, err := config.ReadProviderKeys(observer.profile)
	mustNil(t, err)
	for _, id := range []string{modelsource.CustomID, "custom-second", "ollama"} {
		if _, known := appB.Key(id); known {
			t.Fatalf("an unrelated endpoint received the shared %s key", id)
		}
		if observed.Sources[id] != fakeKeyOne {
			t.Fatalf("desktop publication changed the legacy slot for %s", id)
		}
		appA.Set(id, fakeKeyTwo)
	}
	a.push(t)
	b.pull(t)
	observer.pull(t)
	observed, err = config.ReadProviderKeys(observer.profile)
	mustNil(t, err)
	for _, id := range []string{modelsource.CustomID, "custom-second", "ollama"} {
		if _, known := appB.Key(id); known || observed.Sources[id] != fakeKeyOne {
			t.Fatalf("a secure-store credential changed another machine's %s key", id)
		}
	}
}
