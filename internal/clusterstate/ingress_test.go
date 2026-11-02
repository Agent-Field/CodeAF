package clusterstate_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

func personalProof(t *testing.T, key ed25519.PrivateKey) string {
	t.Helper()
	c := struct {
		V         int    `json:"V"`
		Device    string `json:"device"`
		Created   int64  `json:"created"`
		Signature string `json:"sig"`
	}{V: 1, Device: hex.EncodeToString(key.Public().(ed25519.PublicKey)), Created: 1}
	raw, _ := json.Marshal(c)
	c.Signature = hex.EncodeToString(ed25519.Sign(actorKey("root"), append([]byte("codeaf/device-cert/v1\n"), raw...)))
	raw, _ = json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// R16/R18 bind selected requests, incarnations, role, root and time BEFORE
// namespace activation. A valid delegation can never become a personal proof.
func TestR16R18ClusterIngressOriginalProof(t *testing.T) {
	key := actorKey("scoped-http")
	root := actorKey("root").Public().(ed25519.PublicKey)
	cert := cs.WorkerCertificate{V: 1, Identity: cs.IdentityID(root), Cluster: "test", Key: key.Public().(ed25519.PublicKey), Made: 1, Expires: 1000000, Cells: []string{"selected"}}
	raw, err := cs.WorkerCertificateBytes(cert)
	if err != nil {
		t.Fatal(err)
	}
	cert.Signature = ed25519.Sign(actorKey("root"), raw)
	request := cs.ClusterRequest{IdentityKey: root, Cluster: "test", Worker: &cert, At: 1000, Incarnation: 1, Action: "cell", Cell: "selected"}
	raw, err = cs.ClusterRequestBytes(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Signature = ed25519.Sign(key, raw)
	actor, err := cs.VerifyClusterRequest(request, 1000)
	if err != nil || actor != cert.DeviceID() {
		t.Fatal(actor, err)
	}
	for _, test := range []struct {
		name   string
		change func(*cs.ClusterRequest)
	}{
		{"selected chat", func(q *cs.ClusterRequest) { q.Cell = "outside" }},
		{"incarnation", func(q *cs.ClusterRequest) { q.Incarnation = 2 }},
		{"cluster", func(q *cs.ClusterRequest) { q.Cluster = "other" }},
		{"root", func(q *cs.ClusterRequest) { q.IdentityKey = actorKey("other-root").Public().(ed25519.PublicKey) }},
		{"action", func(q *cs.ClusterRequest) { q.Action = "submit" }},
		{"personal confusion", func(q *cs.ClusterRequest) { q.Certificate = personalProof(t, key) }},
		{"time", func(q *cs.ClusterRequest) { q.At = 0 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			q := request
			test.change(&q)
			if _, err := cs.VerifyClusterRequest(q, 1000); err == nil {
				t.Fatal("R16 rewritten request accepted")
			}
		})
	}
	// Fresh signatures on both sides of the exclusive boundary isolate
	// certificate expiry from the independent request freshness check.
	for _, at := range []int64{cert.Expires - 1, cert.Expires} {
		q := request
		q.At = at
		raw, err = cs.ClusterRequestBytes(q)
		if err != nil {
			t.Fatal(err)
		}
		q.Signature = ed25519.Sign(key, raw)
		_, err = cs.VerifyClusterRequest(q, at)
		if (err == nil) != (at < cert.Expires) {
			t.Fatalf("R16 exclusive expiry at %d: %v", at, err)
		}
	}
	if _, err = cs.VerifyClusterRequest(request, 1000+cs.V1SkewMillis+1); err == nil {
		t.Fatal("R16 delayed original request accepted")
	}
}

// R16/R24 let an old personal home authenticate its original v2 request while
// retaining committed role and revocation precedence. Metadata alone grants no
// signing authority, and a foreign root cannot rewrite the committed root.
func TestR16R24PersonalCommandCertificate(t *testing.T) {
	key := actorKey("old-personal")
	actor := cs.DeviceIDOfKey(key.Public().(ed25519.PublicKey))
	certificate := personalProof(t, key)
	for _, test := range []struct {
		name  string
		setup func(*application)
		want  cs.Code
	}{
		{"original root", func(a *application) {}, cs.OK},
		{"revoked", func(a *application) { a.put(cs.DeviceKey(actor), cs.Device{V: 1, Revoked: true}) }, cs.Revoked},
		{"retained service role", func(a *application) {
			a.put(cs.AuthorityKey(actor), cs.Authority{Cluster: "test", Key: key.Public().(ed25519.PublicKey), Effects: true})
		}, cs.Unauthorized},
		{"foreign root", func(a *application) {
			a.put(cs.IdentityKey(), cs.IdentityRec{Identity: cs.IdentityID(actorKey("foreign").Public().(ed25519.PublicKey)), SigningKey: actorKey("foreign").Public().(ed25519.PublicKey)})
		}, cs.Unauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			a := newApplication(t)
			test.setup(a)
			c, err := cs.Sign(cs.Command{Schema: 1, Cluster: "test", Actor: actor, Certificate: certificate, Session: "personal", Sequence: 1, Now: 1000, Change: cs.Mutation{AdvanceTime: &cs.AdvanceTime{}}}, key)
			if err != nil {
				t.Fatal(err)
			}
			if out := a.evaluate(c); out.Result.Code != test.want {
				t.Fatal(out.Result.Code, test.want)
			}
		})
	}
}
