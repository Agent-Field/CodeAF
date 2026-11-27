package clientproto

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"testing"
)

func testEnrollment(t *testing.T) (ed25519.PrivateKey, Enrollment) {
	t.Helper()
	identity := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, 32))
	phone := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, 32)).Public().(ed25519.PublicKey)
	e := Enrollment{V: 1, Device: DeviceIDOf(phone), SignPub: phone, BoxPub: bytes.Repeat([]byte{3}, 32),
		Methods: []string{"agents.send", "agents.create"}, Reads: []string{"agents.list"}, Ver: 1}
	return identity, e.Sign(identity)
}

// R2: an enrollment signed by the identity verifies, and any change to a signed field does not.
func TestEnrollmentVerifiesOnlyUnderTheSigningIdentity(t *testing.T) {
	identity, e := testEnrollment(t)
	pub := identity.Public().(ed25519.PublicKey)
	if err := e.Verify(pub); err != nil {
		t.Fatalf("a signed enrollment must verify: %v", err)
	}
	other := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, 32)).Public().(ed25519.PublicKey)
	if e.Verify(other) == nil {
		t.Fatal("an enrollment must not verify under another identity")
	}
	changes := map[string]func(*Enrollment){
		"ver":     func(e *Enrollment) { e.Ver++ },
		"methods": func(e *Enrollment) { e.Methods = append(e.Methods, "agents.delete") },
		"reads":   func(e *Enrollment) { e.Reads = nil },
		"box_pub": func(e *Enrollment) { e.BoxPub = bytes.Repeat([]byte{4}, 32) },
	}
	for name, change := range changes {
		c := e
		c.Methods, c.Reads = append([]string(nil), e.Methods...), append([]string(nil), e.Reads...)
		change(&c)
		if c.Verify(pub) == nil {
			t.Errorf("changing %s must break the signature", name)
		}
	}
}

// R2: the device id must be the one derived from the signing key.
func TestEnrollmentDeviceMustBeBoundToItsSigningKey(t *testing.T) {
	identity, e := testEnrollment(t)
	e.Device = DeviceIDOf(bytes.Repeat([]byte{7}, 32))
	e = e.Sign(identity)
	if e.Validate() == nil {
		t.Fatal("a device id that is not the hash of sign_pub must be refused even when correctly signed")
	}
}

// R2: shape limits: version, lists, duplicates, lengths.
func TestEnrollmentShapeIsStrict(t *testing.T) {
	identity, good := testEnrollment(t)
	bad := map[string]func(*Enrollment){
		"v is not 1":            func(e *Enrollment) { e.V = 2 },
		"ver is zero":           func(e *Enrollment) { e.Ver = 0 },
		"short sign_pub":        func(e *Enrollment) { e.SignPub = e.SignPub[:31] },
		"short box_pub":         func(e *Enrollment) { e.BoxPub = e.BoxPub[:31] },
		"duplicate method":      func(e *Enrollment) { e.Methods = []string{"a", "a"} },
		"method with a comma":   func(e *Enrollment) { e.Methods = []string{"a,b"} },
		"empty method name":     func(e *Enrollment) { e.Methods = []string{""} },
		"too many reads":        func(e *Enrollment) { e.Reads = make([]string, MaxEnrollNames+1) },
		"method with a newline": func(e *Enrollment) { e.Methods = []string{"a\nb"} },
	}
	for name, change := range bad {
		c := good
		change(&c)
		if c.Validate() == nil {
			t.Errorf("%s must be refused", name)
		}
	}
	_ = identity
}

// The signature text does not depend on the order the lists were written in.
func TestEnrollmentSignTextIgnoresListOrder(t *testing.T) {
	_, a := testEnrollment(t)
	b := a
	b.Methods = []string{"agents.create", "agents.send"}
	if !bytes.Equal(a.SignText(), b.SignText()) || !a.Same(b) {
		t.Fatal("method order must not change the signed text or the statement")
	}
}

// An empty list is stored and sent as [], never null.
func TestSignedEnrollmentHasNoNullLists(t *testing.T) {
	identity := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, 32))
	phone := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{2}, 32)).Public().(ed25519.PublicKey)
	e := Enrollment{V: 1, Device: DeviceIDOf(phone), SignPub: phone, BoxPub: bytes.Repeat([]byte{3}, 32), Ver: 1}.Sign(identity)
	raw, _ := json.Marshal(e)
	if !bytes.Contains(raw, []byte(`"methods":[]`)) || !bytes.Contains(raw, []byte(`"reads":[]`)) {
		t.Fatalf("empty lists must encode as arrays: %s", raw)
	}
}
