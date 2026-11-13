package identity

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

// R15/R16 require a delegated worker to join using its own key and a public
// certificate, without the root/content/dedup secrets used by personal pairing.
func TestR15R16DelegatedWorkerCertificateWithoutRootExport(t *testing.T) {
	id := sample()
	seed, err := NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	cells := []string{"chat-b", "chat-a"}
	cert, err := IssueWorkerCert(id, seed.Public(), "cluster", cells, time.UnixMilli(1000), time.UnixMilli(2000), false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cells, []string{"chat-b", "chat-a"}) {
		t.Fatal("issuance mutated caller scopes")
	}
	w, err := seed.JoinWorker(cert, id.PublicKey(), "cluster", time.UnixMilli(1000))
	if err != nil {
		t.Fatal(err)
	}

	rawPrivate, err := w.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := UnmarshalWorker(rawPrivate, id.PublicKey(), "cluster", time.UnixMilli(1001))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(restored.Sign([]byte("resume")), w.Sign([]byte("resume"))) {
		t.Fatal("worker key did not survive restart")
	}
	for _, secret := range [][]byte{id.seed[:], id.cell[:], id.dedup[:]} {
		if bytes.Contains(rawPrivate, []byte(hex.EncodeToString(secret))) {
			t.Fatal("worker credential contains an identity-wide secret")
		}
	}
	if _, err := UnmarshalWorker(rawPrivate, id.PublicKey(), "cluster", time.UnixMilli(2000)); err == nil {
		t.Fatal("expired credential resumed")
	}
	msg := []byte("worker proof")
	if !ed25519.Verify(seed.Public(), msg, w.Sign(msg)) || w.ID() != cert.DeviceID() {
		t.Fatal("worker lost its own key")
	}
	cert.Cells[0] = "changed"
	if !w.Certificate.Allows("chat-a") {
		t.Fatal("caller changed retained delegation")
	}
	raw, _ := json.Marshal(w)
	for _, secret := range [][]byte{id.seed[:], id.cell[:], id.dedup[:]} {
		if bytes.Contains(raw, secret) {
			t.Fatal("R15 worker exported identity secret")
		}
	}
	var personal Cert
	if json.Unmarshal(raw, &personal) != nil || personal.Verify(id.PublicKey()) == nil {
		t.Fatal("delegation became personal certificate")
	}
	if reflect.TypeOf(w).NumField() != 2 {
		t.Fatal("review worker secret inventory")
	}
}

// R16 binds every permission and the exclusive expiry boundary to the root
// signature; changing any scope, key, role or lifetime must refuse enrollment.
func TestR16DelegatedWorkerCertificateTamperingAndExpiry(t *testing.T) {
	id := sample()
	s, _ := NewSeed()
	other, _ := NewSeed()
	c, e := IssueWorkerCert(id, s.Public(), "cluster", []string{"a", "b"}, time.UnixMilli(1000), time.UnixMilli(2000), true)
	if e != nil {
		t.Fatal(e)
	}
	tests := map[string]func(*WorkerCert){
		"version": func(c *WorkerCert) { c.V++ }, "cluster": func(c *WorkerCert) { c.Cluster = "other" }, "identity": func(c *WorkerCert) { c.Identity = "other" }, "key": func(c *WorkerCert) { c.Key = other.Public() }, "scope": func(c *WorkerCert) { c.Cells[0] = "outside" }, "learner": func(c *WorkerCert) { c.Learner = false }, "made": func(c *WorkerCert) { c.Made-- }, "expires": func(c *WorkerCert) { c.Expires++ }, "signature": func(c *WorkerCert) { c.Signature[0] ^= 1 },
	}
	for name, change := range tests {
		t.Run(name, func(t *testing.T) {
			raw, _ := cs.Encode(c)
			var modified WorkerCert
			_ = cs.Decode(raw, &modified)
			change(&modified)
			if cs.VerifyWorkerCertificate(modified, id.PublicKey(), "cluster", 1000) == nil {
				t.Fatal("R16 accepted altered permission")
			}
		})
	}
	for _, now := range []int64{999, 2000, 3000} {
		if _, e := s.JoinWorker(c, id.PublicKey(), "cluster", time.UnixMilli(now)); e == nil {
			t.Fatal("R16 lifetime boundary accepted", now)
		}
	}
	if _, e := other.JoinWorker(c, id.PublicKey(), "cluster", time.UnixMilli(1000)); e == nil {
		t.Fatal("other seed joined")
	}
	for _, cells := range [][]string{nil, {"a", "a"}, {""}} {
		if _, e := IssueWorkerCert(id, s.Public(), "cluster", cells, time.UnixMilli(1000), time.UnixMilli(2000), false); e == nil {
			t.Fatal("invalid scopes issued")
		}
	}
}
