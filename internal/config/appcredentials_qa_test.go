package config

import "testing"

func TestQASharedCredentialKeepsNewerLocalChoices(t *testing.T) {
	credentials := NewAppCredentials()
	kept := 0
	credentials.OnKept(func(string, string) { kept++ })
	credentials.Set("aiand", "FAKE-new-local-key")
	if credentials.KeepIf("aiand", "", false, "FAKE-older-shared-key") {
		t.Fatal("a shared key overwrote a newer local key")
	}
	credentials.Remove("openrouter")
	if credentials.KeepIf("openrouter", "", false, "FAKE-shared-key") {
		t.Fatal("an explicit local disconnect was mistaken for an absent key")
	}
	if kept != 0 {
		t.Fatal("a refused shared change reached the credential store")
	}
	if !credentials.KeepIf("aiand", "FAKE-new-local-key", true, "FAKE-newer-shared-key") || kept != 1 {
		t.Fatal("a matching shared change was not handed to the credential store once")
	}
}
