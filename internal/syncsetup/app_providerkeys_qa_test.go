package syncsetup

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/keys"
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

func TestQADesktopProviderKeyRestoreDoesNotUndoAnEditThatReturnedToTheSameValue(t *testing.T) {
	r := newKeyRig(t)
	appA, appB := config.NewAppCredentials(), config.NewAppCredentials()
	a, b := r.appMachine(appA), r.appMachine(appB)
	appA.Set("aiand", fakeKeyOne)
	a.push(t)
	b.pull(t)
	mustNil(t, b.Capture())
	appB.Set("aiand", "FAKE-intervening-key")
	appB.Set("aiand", fakeKeyOne)
	appA.Set("aiand", fakeKeyTwo)
	a.push(t)
	b.pull(t)
	if got, _ := appB.Key("aiand"); got != fakeKeyOne {
		t.Fatal("a pull overwrote a local edit that restored the earlier value")
	}
}

func TestQADesktopProviderKeyFailedNativeWriteDoesNotRollBackTheVault(t *testing.T) {
	r := newKeyRig(t)
	appA, appB := config.NewAppCredentials(), config.NewAppCredentials()
	var desired string
	appB.OnSynced(func(_ string, _ config.AppKeyState, key string) { desired = key })
	a, b := r.appMachine(appA), r.appMachine(appB)
	appA.Set("aiand", fakeKeyOne)
	a.push(t)
	b.pull(t)
	if desired != "" {
		t.Fatal("an unready host received a key from the vault")
	}
	appB.Set("aiand", fakeKeyOne)
	appB.MarkSyncReady()
	b.pull(t)
	appB.SyncLoaded()
	appA.Set("aiand", fakeKeyTwo)
	a.push(t)
	mustNil(t, b.Capture())
	b.pull(t)
	if desired != fakeKeyTwo {
		t.Fatal("the newer key did not reach the secure-store proposal")
	}
	// The write was rejected. The previous key remains active and must not be
	// sent as a fresh local edit, even after it is handed back on restart.
	if key, _ := appB.Key("aiand"); key != fakeKeyOne {
		t.Fatal("an unconfirmed update changed the local key")
	}
	restarted := config.NewAppCredentials()
	restarted.OnSynced(func(_ string, _ config.AppKeyState, key string) { desired = key })
	restarted.Set("aiand", fakeKeyOne)
	restarted.MarkSyncReady()
	func() {
		defer config.UseAppCredentials(restarted)()
		b.Carry = []vaultsync.Carrier{providerKeys(b.profile, b.home)}
	}()
	b.pull(t)
	restarted.SyncLoaded()
	b.push(t)
	a.pull(t)
	if key, _ := appA.Key("aiand"); key != fakeKeyTwo {
		t.Fatal("a failed native write rolled back the peer's newer key")
	}
}

func TestQADesktopProviderKeyExplicitEditSurvivesRestartBeforeCapture(t *testing.T) {
	for _, tc := range []struct{ name, edit string }{
		{name: "saved key", edit: fakeKeyTwo},
		{name: "disconnect"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const provider = "aiand"
			r := newKeyRig(t)
			appA, restarted := config.NewAppCredentials(), config.NewAppCredentials()
			a, b := r.appMachine(appA), r.appMachine(restarted)
			appA.Set(provider, fakeKeyOne)
			a.push(t)
			// B saved or removed its native credential, then exited before
			// capturing the edit. B has never downloaded the vault's copy.
			mustNil(t, RememberAppProviderEdit(b.home, provider, tc.edit))
			edits, err := readAppProviderEdits(b.home)
			mustNil(t, err)
			peerVault, err := keys.Open(a.home)
			mustNil(t, err)
			peerRevisions, err := peerVault.Revisions("home:keys:provider")
			mustNil(t, err)
			name := appProviderKeyName(provider)
			if peerRevisions[name].Stamp == 0 {
				t.Fatal("the peer did not publish its initial key")
			}
			edit := edits[name]
			edit.At = time.UnixMilli(peerRevisions[name].Stamp).UnixNano()
			edits[name] = edit
			// Fix the edit to the same timestamp resolution as the peer's
			// write. Capturing it before the read would let both copies win
			// their own merge, so the peer would keep the old key.
			mustNil(t, writeAppProviderEdits(b.home, edits))
			raw, err := os.ReadFile(appProviderEditsPath(b.home))
			mustNil(t, err)
			if bytes.Contains(raw, []byte(fakeKeyTwo)) {
				t.Fatal("the durable edit stored the credential on disk")
			}
			if tc.edit != "" {
				restarted.Set(provider, tc.edit)
			}
			proposals := 0
			restarted.OnSynced(func(string, config.AppKeyState, string) { proposals++ })
			restarted.MarkSyncReady()
			b.pull(t)
			if proposals != 0 {
				t.Fatal("the initial pull proposed reviving the key the user changed")
			}
			restarted.SyncLoaded()
			b.push(t)
			a.pull(t)
			if got, known := appA.Key(provider); !known || got != tc.edit {
				t.Fatal("the peer did not receive the saved edit after restart")
			}
			edits, err = readAppProviderEdits(b.home)
			mustNil(t, err)
			if len(edits) != 0 {
				t.Fatal("a captured edit remained pending")
			}
		})
	}
}

func TestQADesktopProviderKeyUnsavedBrowserSignInDoesNotSync(t *testing.T) {
	const provider = "aiand"
	r := newKeyRig(t)
	appA, appB := config.NewAppCredentials(), config.NewAppCredentials()
	a, b := r.appMachine(appA), r.appMachine(appB)
	appA.SetTentative(provider, fakeKeyOne)
	if got, known := appA.Key(provider); !known || got != fakeKeyOne {
		t.Fatal("browser sign-in could not be used this launch")
	}
	a.push(t)
	b.pull(t)
	if _, known := appB.Key(provider); known {
		t.Fatal("a browser key went to a peer before its secure-store save")
	}
	mustNil(t, RememberAppProviderEdit(a.home, provider, fakeKeyOne))
	appA.Set(provider, fakeKeyOne)
	a.push(t)
	b.pull(t)
	if got, known := appB.Key(provider); !known || got != fakeKeyOne {
		t.Fatal("the securely saved browser key did not sync")
	}
}

func TestQADesktopProviderKeyDefaultSlotIsOnlySharedAtOfficialEndpoint(t *testing.T) {
	for _, address := range []string{"https://custom.invalid/v1", "http://openrouter.ai/api/v1"} {
		t.Run(address, func(t *testing.T) {
			t.Setenv("CODEAF_BASE_URL", config.DefaultBaseURL)
			r := newKeyRig(t)
			appA, appB := config.NewAppCredentials(), config.NewAppCredentials()
			a, b := r.appMachine(appA), r.appMachine(appB)
			appA.Set(modelsource.DefaultID, fakeKeyOne)
			a.push(t)
			t.Setenv("CODEAF_BASE_URL", address)
			appB.Set(modelsource.DefaultID, fakeKeyTwo)
			b.pull(t)
			if key, _ := appB.Key(modelsource.DefaultID); key != fakeKeyTwo {
				t.Fatal("an official provider key was restored into a different endpoint")
			}
			b.push(t)
			t.Setenv("CODEAF_BASE_URL", config.DefaultBaseURL)
			a.pull(t)
			if key, _ := appA.Key(modelsource.DefaultID); key != fakeKeyOne {
				t.Fatal("a custom endpoint key replaced the official provider key")
			}
		})
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
