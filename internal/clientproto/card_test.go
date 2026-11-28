package clientproto

import (
	"bytes"
	"crypto/ed25519"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
)

func testIdentity(t *testing.T, b byte) identity.Identity {
	t.Helper()
	hexSeed := strings.Repeat(string("0123456789abcdef"[b>>4])+string("0123456789abcdef"[b&15]), 32)
	id, err := identity.Unmarshal([]byte(`{"V":1,"signing_seed":"` + hexSeed + `","dedup_secret":"` + hexSeed + `","cell_key":"` + hexSeed + `"}`))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testCard(t *testing.T, id identity.Identity, deviceSeed byte, name string, at int64) (ed25519.PrivateKey, Card) {
	t.Helper()
	dev := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{deviceSeed}, 32))
	pub := dev.Public().(ed25519.PublicKey)
	cert := identity.IssueCert(id, pub, time.UnixMilli(1700000000000))
	return dev, Card{V: 1, Writer: cert.DeviceID(), Cert: cert, BoxPub: bytes.Repeat([]byte{0x22}, 32), Name: name, At: at}.Sign(dev)
}

// L3: a card verifies only under the identity that certified its device, and only as signed.
func TestCardVerifiesOnlyUnderTheCertifyingIdentity(t *testing.T) {
	id := testIdentity(t, 1)
	_, card := testCard(t, id, 0x31, "desk", 100)
	if err := card.Verify(id.PublicKey()); err != nil {
		t.Fatalf("a signed card must verify: %v", err)
	}
	if card.Verify(testIdentity(t, 9).PublicKey()) == nil {
		t.Fatal("a card must not verify under another identity")
	}
	for name, change := range map[string]func(*Card){
		"name":    func(c *Card) { c.Name = "other" },
		"at":      func(c *Card) { c.At++ },
		"box_pub": func(c *Card) { c.BoxPub = bytes.Repeat([]byte{0x23}, 32) },
		"writer":  func(c *Card) { c.Writer = DeviceIDOf(bytes.Repeat([]byte{7}, 32)) },
	} {
		c := card
		change(&c)
		if c.Verify(id.PublicKey()) == nil {
			t.Errorf("changing %s must break the card", name)
		}
	}
}

// L3: a card signed by some other device key, even with a good certificate, is refused.
func TestCardMustBeSignedByItsCertifiedDevice(t *testing.T) {
	id := testIdentity(t, 1)
	_, card := testCard(t, id, 0x31, "desk", 100)
	forger := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{0x32}, 32))
	card = card.Sign(forger)
	if card.Verify(id.PublicKey()) == nil {
		t.Fatal("a card signed by a key that is not the certificate's device must be refused")
	}
}

// L3: names are readable text of 1 to 32 runes.
func TestCardNameRules(t *testing.T) {
	for name, ok := range map[string]bool{
		"desk": true, "my laptop": true, strings.Repeat("x", 32): true, strings.Repeat("é", 32): true,
		"": false, strings.Repeat("x", 33): false, "a\nb": false, "tab\there": false, "bell\a": false, "bad\xffutf8": false,
	} {
		if ValidCardName(name) != ok {
			t.Errorf("ValidCardName(%q) = %v, want %v", name, !ok, ok)
		}
	}
}

// K1: Ed25519 verification is strict: a signature whose S is not below the group order is refused.
func TestCardRefusesAMalleatedSignature(t *testing.T) {
	id := testIdentity(t, 1)
	_, card := testCard(t, id, 0x31, "desk", 100)
	card.Sig = malleate(card.Sig)
	if card.Verify(id.PublicKey()) == nil {
		t.Fatal("a signature with S + L must be refused")
	}
}

// malleate adds the group order to S, the standard non-canonical twin of a valid signature.
func malleate(sig []byte) []byte {
	order, _ := new(big.Int).SetString("7237005577332262213973186563042994240857116359379907606001950938285454250989", 10)
	s := new(big.Int).SetBytes(reverse(sig[32:]))
	s.Add(s, order)
	out := append([]byte(nil), sig[:32]...)
	le := reverse(s.Bytes())
	for len(le) < 32 {
		le = append(le, 0)
	}
	return append(out, le[:32]...)
}

func reverse(b []byte) []byte {
	out := make([]byte, len(b))
	for i := range b {
		out[len(b)-1-i] = b[i]
	}
	return out
}
