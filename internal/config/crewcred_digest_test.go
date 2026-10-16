package config

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/Agent-Field/codeaf/internal/modelsource"
	"github.com/Agent-Field/codeaf/internal/provider"
)

// A PRINT TAKEN FROM A CONNECTION AND A DIGEST TAKEN FROM A HEADER ARE ONE
// STRING, and it is the string prints have always been. A print written before
// the digest moved into the adapter is the same bytes, so a refusal recorded
// then is still recognised as the same credential and never read as a change.
func TestAHeaderDigestIsTheConnectionsPrint(t *testing.T) {
	const key = "fixture-crew-digest-key"
	for _, id := range []string{"openrouter", "OpenRouter "} {
		service := modelsource.Connected{Source: modelsource.Source{ID: id}, Key: key}
		old := sha256.Sum256([]byte("crew-credential\x00openrouter\x00" + key))
		want := hex.EncodeToString(old[:8])
		if got := CrewDispatchOf(t.TempDir(), service).Cred; got != want {
			t.Fatalf("%q printed %q, want the print it always had %q", id, got, want)
		}
		if got := provider.CredentialDigest(CrewCredentialDomain(id), key); got != want {
			t.Fatalf("%q digested %q in its domain, want %q", id, got, want)
		}
	}
}
