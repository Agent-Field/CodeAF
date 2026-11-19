package syncsetup

import (
	"bytes"
	"os"
	"testing"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/modelsource"
)

func TestQADesktopProviderKeysUseVaultAndCredentialHandoff(t *testing.T) {
	r := newKeyRig(t)
	appA, appB := config.NewAppCredentials(), config.NewAppCredentials()
	makeApp := func(credentials *config.AppCredentials) *keyMachine {
		restore := config.UseAppCredentials(credentials)
		defer restore()
		return r.machine()
	}
	a, b := makeApp(appA), makeApp(appB)
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
