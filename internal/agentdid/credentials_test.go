package agentdid

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"testing"
)

func TestCredentialsProveTheDIDAndSeparatePurposes(t *testing.T) {
	d, keys, err := Generate("chat")
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("a statement")
	sig, err := keys.Sign(d.DID, "receipt/v1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(d.DID, "receipt/v1", payload, sig) {
		t.Fatal("public DID did not verify")
	}
	for _, purpose := range []string{"receipt/v2", "other/v1", ""} {
		if Verify(d.DID, purpose, payload, sig) {
			t.Fatal("cross-purpose signature accepted")
		}
	}
	if Verify(d.DID, "receipt/v1", []byte("changed"), sig) {
		t.Fatal("changed statement accepted")
	}
	other, _, _ := Generate("other")
	if _, err := keys.Sign(other.DID, "receipt/v1", payload); err == nil {
		t.Fatal("signed another DID")
	}
	if _, err := keys.Sign(d.DID, "unversioned", payload); err == nil {
		t.Fatal("unversioned purpose accepted")
	}
}

func TestCustodyIsEncryptedAndBoundToTheDocument(t *testing.T) {
	d, keys, _ := Generate("chat")
	wrapping := bytes.Repeat([]byte{3}, 32)
	if err := d.Protect(keys, wrapping); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(d)
	for _, secret := range [][]byte{keys.signing[:], keys.encryption[:]} {
		for _, encoded := range [][]byte{secret, []byte(base64.StdEncoding.EncodeToString(secret)), []byte(encode(secret)), []byte(hex.EncodeToString(secret))} {
			if bytes.Contains(raw, encoded) || bytes.Contains([]byte(fmt.Sprintf("%v %+v %#v", keys, keys, keys)), encoded) {
				t.Fatal("plaintext key escaped JSON or diagnostics")
			}
		}
	}
	restored, err := d.Unlock(wrapping)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restored.Sign(d.DID, "test/v1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Unlock(bytes.Repeat([]byte{4}, 32)); err == nil {
		t.Fatal("wrong home opened custody")
	}
	d.Session = "copy"
	if _, err := d.Unlock(wrapping); err == nil {
		t.Fatal("copy opened original custody")
	}
}

func TestCustodyRejectsCorruptionAndPublicKeySubstitution(t *testing.T) {
	d, keys, err := Generate("chat")
	if err != nil {
		t.Fatal(err)
	}
	wrapping := bytes.Repeat([]byte{7}, 32)
	d, err = d.Persist(wrapping)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Document){
		func(d *Document) { d.Custody[0] ^= 1 },
		func(d *Document) { d.Custody = d.Custody[:5] },
		func(d *Document) { d.DID, _ = New() },
		func(d *Document) { d.Signing.Public = "substituted" },
		func(d *Document) { d.Encryption.Public = "substituted" },
		func(d *Document) { d.SigningUnavailable = true },
	} {
		raw, _ := json.Marshal(d)
		var changed Document
		if err := json.Unmarshal(raw, &changed); err != nil {
			t.Fatal(err)
		}
		mutate(&changed)
		if opened, err := changed.Unlock(wrapping); err == nil || opened != nil {
			t.Fatal("tampered custody returned private credentials")
		}
	}
	for _, purpose := range []string{"", "receipt", "/v1", "receipt/v0", "receipt/v01", "receipt/v1/other"} {
		if _, err := keys.Sign(d.DID, purpose, nil); err == nil {
			t.Fatalf("invalid purpose %q accepted", purpose)
		}
	}
}

func TestOnlyTheRecipientCanOpenSealedMaterial(t *testing.T) {
	d, keys, _ := Generate("chat")
	other, wrong, _ := Generate("other")
	box, err := Seal(d, "message/v1", []byte("for one agent"))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := keys.Open(d, "message/v1", box)
	if err != nil || string(plain) != "for one agent" {
		t.Fatalf("open: %q %v", plain, err)
	}
	if _, err := wrong.Open(other, "message/v1", box); err == nil {
		t.Fatal("other agent opened message")
	}
	if _, err := keys.Open(d, "other/v1", box); err == nil {
		t.Fatal("cross-purpose message opened")
	}
	box[len(box)-1] ^= 1
	if _, err := keys.Open(d, "message/v1", box); err == nil {
		t.Fatal("damaged message opened")
	}
}

func TestUnknownDocumentRoundTrips(t *testing.T) {
	raw := []byte(`{"v":"v99", "did":"future", "session":"chat", "keys":{"future":[1,2]}}`)
	var d Document
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	got, _ := d.MarshalJSON()
	if !bytes.Equal(got, raw) {
		t.Fatalf("future document changed: %s", got)
	}
	if _, err := d.Unlock(make([]byte, 32)); err == nil {
		t.Fatal("future document unlocked")
	}
}

func TestCredentialsRejectMalformedEnvelopesAndSupportHierarchicalPurposes(t *testing.T) {
	d, keys, err := Generate("chat")
	if err != nil {
		t.Fatal(err)
	}
	const purpose = "codeaf/verification/receipt/v1"
	sig, err := keys.Sign(d.DID, purpose, nil)
	if err != nil || !Verify(d.DID, purpose, nil, sig) {
		t.Fatalf("hierarchical versioned purpose: %v", err)
	}
	for _, did := range []string{"", "did:key:z", "did:key:z!!!!", d.DID + "#w1"} {
		if Verify(did, purpose, nil, sig) {
			t.Fatalf("malformed agent DID %q verified", did)
		}
	}
	box, err := Seal(d, purpose, []byte("private"))
	if err != nil {
		t.Fatal(err)
	}
	for _, broken := range [][]byte{nil, box[:5], box[:32], append(make([]byte, 32), box[32:]...)} {
		if _, err := keys.Open(d, purpose, broken); err == nil {
			t.Fatal("malformed or low-order envelope opened")
		}
	}
	if _, err := (*Credentials)(nil).Sign(d.DID, purpose, nil); err == nil {
		t.Fatal("nil custody signed a statement")
	}
	if _, err := (*Credentials)(nil).Open(d, purpose, box); err == nil {
		t.Fatal("nil custody opened an envelope")
	}
	for _, public := range []string{"!", encode(make([]byte, 31)), encode(make([]byte, 32))} {
		broken := d
		broken.Encryption = &Key{Type: "X25519", Public: public}
		if _, err := Seal(broken, purpose, nil); err == nil {
			t.Fatal("invalid recipient key accepted")
		}
	}
}
