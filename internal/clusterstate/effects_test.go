package clusterstate_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/rand"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

func joinCert(t *testing.T, actor string) string {
	t.Helper()
	c := struct {
		V       uint16 `json:"V"`
		Device  string `json:"device"`
		Created int64  `json:"created"`
		Sig     string `json:"sig"`
	}{1, hex.EncodeToString(actorKey(actor).Public().(ed25519.PublicKey)), 1, ""}
	b, _ := json.Marshal(c)
	c.Sig = hex.EncodeToString(ed25519.Sign(actorKey("root"), append([]byte("codeaf/device-cert/v1\n"), b...)))
	b, _ = json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(b)
}
func service(a *application, now int64, m cs.Mutation) cs.Result {
	a.put(cs.AuthorityKey("effects"), cs.Authority{Cluster: "test", Key: actorKey("effects").Public().(ed25519.PublicKey), Effects: true})
	c, e := cs.Sign(cs.Command{Schema: 1, Cluster: "test", Actor: "effects", Session: "effects", Sequence: 1, Now: now, Change: m}, actorKey("effects"))
	if e != nil {
		a.t.Fatal(e)
	}
	return a.evaluate(c).Result
}
func TestR21UploadReceiptBeforeHeadAndFenceBeforeReceipt(t *testing.T) {
	a := newApplication(t)
	create := cs.Mutation{Create: &cs.CreateCell{Cell: "chat", Init: cs.CellInit{Head: "sealed"}}}
	if r := a.evaluate(a.prepareBare("a", 1000, create)).Result; r.Code != cs.BadRequest || a.rows[cs.CellKey("chat")] != nil {
		t.Fatal(r)
	}
	upload := cs.UploadReceipt{Device: "a", Head: "sealed", HeadRID: "remote", Frames: []string{"frame"}}
	if r := a.run("a", 1001, cs.Mutation{RecordUpload: &upload}); r.Code != cs.Unauthorized {
		t.Fatal("personal caller minted upload", r)
	}
	a.seq["a"]-- // An unauthorized request did not consume its directory sequence.
	if r := service(a, 1002, cs.Mutation{RecordUpload: &upload}); r.Code != cs.OK {
		t.Fatal(r)
	}
	create.Create.Receipt = cs.UploadID(upload)
	if r := a.evaluate(a.prepareBare("a", 1003, create)).Result; r.Code != cs.BadRequest {
		t.Fatal("receipt accepted a changed plan", r)
	}
	create.Create.Init.Frames = upload.Frames
	if r := a.evaluate(a.prepareBare("b", 1004, create)).Result; r.Code != cs.BadRequest {
		t.Fatal("cross-device receipt", r)
	}
	if r := a.evaluate(a.prepareBare("a", 1005, create)).Result; r.Code != cs.OK {
		t.Fatal(r)
	}
	p := cs.Mutation{Publish: &cs.PublishCell{Cell: "chat", Publish: cs.Publish{Fence: 1, OldHead: "sealed", Head: "next"}}}
	if r := a.evaluate(a.prepareBare("a", 1006, p)).Result; r.Code != cs.BadRequest || a.cell().Head != "sealed" {
		t.Fatal(r)
	}
	p.Publish.Publish.Fence = 0
	p.Publish.Publish.OldHead = "wrong"
	if r := a.evaluate(a.prepareBare("a", 1007, p)).Result; r.Code != cs.FenceStale {
		t.Fatal("receipt masked stale fence", r)
	}
	if r := service(a, 1008, cs.Mutation{Revoke: &cs.DeviceID{Device: "b"}}); r.Code != cs.Unauthorized {
		t.Fatal("effect key acquired directory authority", r)
	}
}

// R16/R17/R21 enumerate authorization orderings with independently selected
// expected outcomes. Every seed honors the wave's 2,000-seed soak lever.
func TestR16R17R21EffectsAuthorizationCampaign(t *testing.T) {
	for seed := 0; seed < seedCount(t); seed++ {
		t.Run(fmt.Sprintf("seed-%04d", seed), func(t *testing.T) {
			a := newApplication(t)
			rng := rand.New(rand.NewSource(int64(seed)))
			public := actorKey("joiner").Public().(ed25519.PublicKey)
			id := cs.DeviceIDOfKey(public)
			if r := service(a, 1000, cs.Mutation{AdmitJoin: &cs.AdmitJoin{Code: "request", PublicKey: public, Expires: 10000}}); r.Code != cs.OK {
				t.Fatal(r)
			}
			decision := cs.JoinDecision{Code: "request", Device: id, Cert: joinCert(t, "joiner"), Grant: "c2VhbGVk", Record: cs.Device{V: 1, Name: "c2VhbGVk"}}
			c := a.prepareBare("b", 1001, cs.Mutation{DecideJoin: &decision})
			revoke := rng.Intn(2) == 0
			rotate := rng.Intn(2) == 0
			if revoke {
				a.run("a", 1002, cs.Mutation{Revoke: &cs.DeviceID{Device: "b"}})
			}
			if rotate {
				a.run("a", 1003, cs.Mutation{Rotate: &cs.RotateRequest{Op: "freeze"}})
			}
			want := cs.OK
			if rotate {
				want = cs.Rotated
			}
			if revoke {
				want = cs.Revoked
			}
			r := a.evaluate(c).Result
			if r.Code != want {
				t.Fatal(seed, r.Code, want)
			}
			if (a.rows[cs.AuthorityKey(id)] != nil) != (want == cs.OK) {
				t.Fatal("authorization escaped committed decision")
			}
			if want == cs.OK {
				retry := a.evaluate(c).Result
				if fmt.Sprint(retry) != fmt.Sprint(r) {
					t.Fatal("decision retry changed")
				}
				wrong := decision
				wrong.Grant = "b3RoZXI"
				if r := a.run("a", 1004, cs.Mutation{DecideJoin: &wrong}); r.Code != cs.AlreadyDecided {
					t.Fatal(r)
				}
				done := cs.CompleteJoin{Code: "request", Decision: cs.DecisionID(decision), Outcome: "delivered"}
				if r := service(a, 1005, cs.Mutation{CompleteJoin: &done}); r.Code != cs.OK {
					t.Fatal(r)
				}
				if r := service(a, 1006, cs.Mutation{CompleteJoin: &done}); r.Code != cs.OK {
					t.Fatal(r)
				}
			}
		})
	}
}
func TestR16PairingCertificateAndStickyRevocation(t *testing.T) {
	a := newApplication(t)
	pub := actorKey("joiner").Public().(ed25519.PublicKey)
	id := cs.DeviceIDOfKey(pub)
	service(a, 1000, cs.Mutation{AdmitJoin: &cs.AdmitJoin{Code: "request", PublicKey: pub, Expires: 10000}})
	d := cs.JoinDecision{Code: "request", Device: id, Cert: joinCert(t, "different"), Grant: "c2VhbGVk"}
	if r := a.run("a", 1001, cs.Mutation{DecideJoin: &d}); r.Code != cs.BadRequest {
		t.Fatal(r)
	}
	d.Cert = joinCert(t, "joiner")
	a.put(cs.DeviceKey(id), cs.Device{V: 1, Revoked: true})
	if r := a.run("a", 1002, cs.Mutation{DecideJoin: &d}); r.Code != cs.Revoked || a.rows[cs.AuthorityKey(id)] != nil {
		t.Fatal(r)
	}
}

// R25 preserves real old-client requests that cannot name an encrypted head
// RID, while R21 requires evidence at the distinct v2 command door. Ingress
// supplies protocol provenance; P17 must never copy compatibility flags from v2.
func TestR21R25LegacyProtocolKeepsExistingUploadOrdering(t *testing.T) {
	a := newApplication(t)
	m := cs.Mutation{Create: &cs.CreateCell{Cell: "chat", Init: cs.CellInit{Head: "legacy"}, V1: true}}
	if r := a.evaluate(a.prepareBare("a", 1000, m)).Result; r.Code != cs.OK {
		t.Fatal(r)
	}
	p := cs.Mutation{Publish: &cs.PublishCell{Cell: "chat", Publish: cs.Publish{Fence: 1, OldHead: "legacy", Head: "legacy-next"}}}
	if r := a.evaluate(a.prepareBare("a", 1001, p)).Result; r.Code != cs.BadRequest {
		t.Fatal("v2 publish accepted absent evidence", r)
	}
	p.Publish.V1 = true
	if r := a.evaluate(a.prepareBare("a", 1002, p)).Result; r.Code != cs.OK {
		t.Fatal("legacy request changed behaviour", r)
	}
}
