package agentdid

import (
	"bytes"
	"crypto/ed25519"
	"strings"
	"testing"
)

func TestNewMintsDistinctValidDIDs(t *testing.T) {
	seen := map[string]bool{}
	for range 64 {
		did, err := New()
		if err != nil {
			t.Fatal(err)
		}
		if !Valid(did) {
			t.Fatalf("minted DID is not valid: %q", did)
		}
		if seen[did] {
			t.Fatalf("minted the same DID twice: %q", did)
		}
		seen[did] = true
	}
}

// The spelling matches AgentField's did:key: z + base64url(0xed01 || key).
func TestFromPublicKeyMatchesAgentFieldSpelling(t *testing.T) {
	public := ed25519.PublicKey(bytes.Repeat([]byte{0}, ed25519.PublicKeySize))
	got := FromPublicKey(public)
	want := "did:key:z7QE" + strings.Repeat("A", 43)
	if got != want {
		t.Fatalf("FromPublicKey = %q, want %q", got, want)
	}
}

func TestValidRefusesOtherShapes(t *testing.T) {
	did, err := New()
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "cwd:/tmp", "did:web:example.com:agents:a", "did:key:zAAAA", Worker(did, 3), strings.Replace(did, "did:key:z", "did:key:", 1)} {
		if Valid(bad) {
			t.Fatalf("Valid(%q) = true", bad)
		}
	}
}

func TestWorkerAddsTheTaskFragment(t *testing.T) {
	if got := Worker("did:key:zABC", 7); got != "did:key:zABC#w7" {
		t.Fatalf("Worker = %q", got)
	}
	if got := Worker("did:key:zABC", 0); got != "did:key:zABC" {
		t.Fatalf("Worker with no task = %q", got)
	}
	if got := Worker("", 7); got != "" {
		t.Fatalf("Worker with no agent = %q", got)
	}
}
