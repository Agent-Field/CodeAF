package clusterstate_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// R9/R12 retain the original personal proof through committed application.
// Changing the root, store, route or certificate cannot authorize allocation.
func TestR9R12PersonalAdmissionProofBindsStoreAndCommittedRoot(t *testing.T) {
	id, err := identity.Ensure(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(1700000000000)
	cert := identity.IssueCert(id, pub, now)
	raw, _ := json.Marshal(cert)
	request := cs.PersonalAdmission{Store: strings.Repeat("01", 32), Protocol: cs.ClusterProtocol}
	body, _ := json.Marshal(request)
	proof := cs.V1Request{Method: http.MethodPost, URI: cs.PersonalAdmissionPath, IdentityKey: id.PublicKey(), Certificate: base64.RawURLEncoding.EncodeToString(raw), At: "1700000000000", Body: body}
	proof.Signature = ed25519.Sign(key, cs.V1SigningMessage(proof.Method, proof.URI, proof.At, proof.Body))
	root := cs.IdentityRec{V: 1, Identity: id.ID(), SigningKey: id.PublicKey()}
	got, authority, actor, err := cs.VerifyPersonalAdmission(proof, root, now.UnixMilli())
	if err != nil || got != request || !authority.Personal || !ed25519.PublicKey(authority.Key).Equal(pub) || actor != cert.DeviceID() {
		t.Fatalf("original paired store refused: %+v %v", got, err)
	}
	for _, change := range []string{"store", "route", "certificate", "root", "stale", "malformed-store"} {
		t.Run(change, func(t *testing.T) {
			candidate := proof
			committed := root
			at := now.UnixMilli()
			switch change {
			case "store":
				altered := request
				altered.Store = strings.Repeat("02", 32)
				candidate.Body, _ = json.Marshal(altered)
			case "route":
				candidate.URI = "/v2/cluster/control"
			case "certificate":
				candidate.Certificate = ""
			case "root":
				_, other, err := ed25519.GenerateKey(rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				committed.SigningKey = other.Public().(ed25519.PublicKey)
			case "stale":
				at += cs.V1SkewMillis + 1
			case "malformed-store":
				altered := request
				altered.Store = "new-store"
				candidate.Body, _ = json.Marshal(altered)
				candidate.Signature = ed25519.Sign(key, cs.V1SigningMessage(candidate.Method, candidate.URI, candidate.At, candidate.Body))
			}
			if _, _, _, err := cs.VerifyPersonalAdmission(candidate, committed, at); err == nil {
				t.Fatal("altered or stale enrollment gained authority")
			}
		})
	}
}
