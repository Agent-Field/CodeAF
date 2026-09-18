package poolkey

import (
	"crypto/ed25519"
	"encoding/base64"
	"testing"
)

// The build's one key is a real ed25519 public key: it decoded at init, it is
// the right length, and Keys hands it back without handing back the slice it
// holds — a caller that changed the copy must not change what the next one
// trusts.
func TestTheBuiltInKeyIsAnEd25519PublicKey(t *testing.T) {
	got := Keys()
	if len(got) != 1 {
		t.Fatalf("the build carries %d key(s), want the index signer's one", len(got))
	}
	if len(got[0]) != ed25519.PublicKeySize {
		t.Fatalf("the built-in key is %d bytes, want an ed25519 public key's %d", len(got[0]), ed25519.PublicKeySize)
	}
	if raw, err := base64.StdEncoding.DecodeString(Encoded()); err != nil || len(raw) != ed25519.PublicKeySize {
		t.Fatalf("the encoded literal does not decode to a public key (err %v, %d bytes)", err, len(raw))
	}
}

// Keys hands back a copy: mutating one must not reach the shared list.
func TestKeysHandsBackACopy(t *testing.T) {
	first := Keys()
	first[0][0] ^= 0xff
	if second := Keys(); second[0][0] == first[0][0] {
		t.Fatal("mutating a Keys copy changed the next one")
	}
}
