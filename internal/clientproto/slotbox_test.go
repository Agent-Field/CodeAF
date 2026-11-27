package clientproto

import (
	"bytes"
	"testing"

	"golang.org/x/crypto/curve25519"
)

func keyPair(b byte) (pub, priv *[32]byte) {
	priv = new([32]byte)
	copy(priv[:], bytes.Repeat([]byte{b}, 32))
	pub = new([32]byte)
	curve25519.ScalarBaseMult(pub, priv)
	return pub, priv
}

func samplePlain() SlotPlain {
	return SlotPlain{V: SlotVersion, Slot: "agents.entries.dev_computer", Client: "dev_phone", Executor: "dev_computer",
		At: 1700000000123, Ver: 3, Payload: []byte(`[{"kind":"text","text":"hello"}]`)}
}

// L3: a slot opens only for the pair of keys it was boxed between, and any changed byte refuses it.
func TestSlotOpensOnlyForTheKeyPairItWasSealedFor(t *testing.T) {
	execPub, execPriv := keyPair(0x11)
	phonePub, phonePriv := keyPair(0x12)
	nonce, err := NewNonce()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := SealSlot(samplePlain(), nonce, phonePub, execPriv)
	if err != nil {
		t.Fatal(err)
	}
	got, err := OpenSlot(sealed, execPub, phonePriv)
	if err != nil || got.Slot != "agents.entries.dev_computer" || !bytes.Equal(got.Payload, samplePlain().Payload) {
		t.Fatalf("the slot must open for its pair: %+v %v", got, err)
	}
	otherPub, _ := keyPair(0x13)
	if _, err := OpenSlot(sealed, otherPub, phonePriv); err == nil {
		t.Fatal("a slot must not open under another executor key, which is what a forging relay would have to use")
	}
	_, otherPhone := keyPair(0x14)
	if _, err := OpenSlot(sealed, execPub, otherPhone); err == nil {
		t.Fatal("a slot must not open for another phone")
	}
	for i := range sealed {
		c := append([]byte(nil), sealed...)
		c[i] ^= 1
		if _, err := OpenSlot(c, execPub, phonePriv); err == nil {
			t.Fatalf("flipping byte %d must refuse the slot", i)
		}
	}
	if _, err := OpenSlot(sealed[:NonceSize+10], execPub, phonePriv); err == nil {
		t.Fatal("a truncated slot must be refused")
	}
}

// L6: the phone refuses a slot under another name, for another phone or computer, or not newer.
func TestSlotCheckIsTheReplayAndRollbackGuard(t *testing.T) {
	p := samplePlain()
	ok := func(slot, client, writer string, last int64) bool { return p.Check(slot, client, writer, last) == nil }
	if !ok("agents.entries.dev_computer", "dev_phone", "dev_computer", 0) || !ok("agents.entries.dev_computer", "dev_phone", "dev_computer", p.At-1) {
		t.Fatal("a slot where it was fetched and newer than the last accepted must pass")
	}
	if ok("agents.list.dev_computer", "dev_phone", "dev_computer", 0) {
		t.Fatal("a slot served under another name must be refused")
	}
	if ok("agents.entries.dev_computer", "dev_other", "dev_computer", 0) {
		t.Fatal("a slot for another phone must be refused")
	}
	if ok("agents.entries.dev_computer", "dev_phone", "dev_other", 0) {
		t.Fatal("a slot from another writer must be refused")
	}
	if ok("agents.entries.dev_computer", "dev_phone", "dev_computer", p.At) || ok("agents.entries.dev_computer", "dev_phone", "dev_computer", p.At+5) {
		t.Fatal("a slot whose at is not above the last accepted must be refused")
	}
}

// Every sealed slot starts with its own random nonce.
func TestEverySlotCarriesAFreshNonce(t *testing.T) {
	a, _ := NewNonce()
	b, _ := NewNonce()
	if *a == *b {
		t.Fatal("two nonces must differ")
	}
}

// L3: a sealed card opens for its phone and is then verified by its own signature chain.
func TestSealedCardOpensForThePhoneAndStillNeedsItsChain(t *testing.T) {
	id := testIdentity(t, 1)
	_, card := testCard(t, id, 0x31, "desk", 100)
	phonePub, phonePriv := keyPair(0x12)
	sealed, err := SealCard(card, phonePub, bytes.NewReader(bytes.Repeat([]byte{0x41}, 64)))
	if err != nil {
		t.Fatal(err)
	}
	got, err := OpenCard(sealed, phonePub, phonePriv)
	if err != nil || got.Verify(id.PublicKey()) != nil {
		t.Fatalf("the phone must open and verify its card: %v", err)
	}
	otherPub, otherPriv := keyPair(0x14)
	if _, err := OpenCard(sealed, otherPub, otherPriv); err == nil {
		t.Fatal("another phone must not open the card")
	}
}
