package relayserve_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterapi"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// R16/R18/R21 drive original personal and scoped worker signatures through the
// production HTTP door. Selected reads cannot exceed the grant, use stale
// incarnations, survive revocation or turn an old claimed result into authority.
func TestR16R18ProductionScopedWorkerIngress(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	if _, err := r.dir(a).Create(bg, cell, directory.CellInit{Head: head1}); err != nil {
		t.Fatal(err)
	}
	if err := r.dir(a).Release(bg, cell, 1); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(r.store, "raft", id.ID(), "provision.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p struct{ Cluster string }
	if json.Unmarshal(raw, &p) != nil {
		t.Fatal("provision")
	}
	certRaw, _ := json.Marshal(a.dev.Cert)
	personal := base64.RawURLEncoding.EncodeToString(certRaw)
	owner := clusterapi.Client{Base: r.srv.URL, HTTP: r.srv.Client(), Now: r.clock.Now, Allowed: func() bool { return true }, Sign: a.dev.Sign, Request: func() cs.ClusterRequest {
		return cs.ClusterRequest{IdentityKey: id.PublicKey(), Cluster: p.Cluster, Certificate: personal}
	}}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := identity.IssueWorkerCert(id, key.Public().(ed25519.PublicKey), p.Cluster, []string{cell}, r.clock.Now(), r.clock.Now().Add(time.Hour), false)
	if err != nil {
		t.Fatal(err)
	}
	worker := cert.DeviceID()
	incarnation := uint64(0)
	wire := clusterapi.Client{Base: r.srv.URL, HTTP: r.srv.Client(), Now: r.clock.Now, Allowed: func() bool { return true }, Sign: func(b []byte) []byte { return ed25519.Sign(key, b) }, Request: func() cs.ClusterRequest {
		return cs.ClusterRequest{IdentityKey: id.PublicKey(), Cluster: p.Cluster, Worker: &cert, Incarnation: incarnation}
	}}
	sequences := map[string]uint64{}
	sign := func(actor string, mutation cs.Mutation) cs.Command {
		t.Helper()
		sequences[actor]++
		n := sequences[actor]
		c := cs.Command{Schema: 1, Cluster: p.Cluster, Actor: actor, Session: "integration", Sequence: n, Ack: n - 1, Now: r.clock.Now().UnixMilli(), Change: mutation}
		signer := a.dev.Sign
		if actor == worker {
			c.WorkerIncarnation = incarnation
			signer = func(b []byte) []byte { return ed25519.Sign(key, b) }
		} else {
			c.Certificate = personal
		}
		c.RequestDigest, err = cs.Digest(c)
		if err != nil {
			t.Fatal(err)
		}
		b, err := cs.SigningBytes(c)
		if err != nil {
			t.Fatal(err)
		}
		c.Signature = signer(b)
		return c
	}
	submit := func(client clusterapi.Client, c cs.Command, want cs.Code) cs.Result {
		t.Helper()
		result, err := client.Submit(bg, c)
		if err != nil || result.Code != want {
			t.Fatalf("want %s: %+v %v", want, result, err)
		}
		return result
	}
	grant := []byte{0x81, 0x82, 0x83}
	submit(owner, sign(a.dev.ID(), cs.Mutation{EnrollWorker: &cs.EnrollWorker{Certificate: cert, Grants: map[string][]byte{cell: grant}}}), cs.OK)
	registration := sign(worker, cs.Mutation{RegisterWorker: &cs.RegisterWorker{Incarnation: 1}})
	submit(wire, registration, cs.OK)
	incarnation = 1
	view, got, err := wire.Cell(bg, cell)
	if err != nil || view.Cell.Head != head1 || string(got) != string(grant) {
		t.Fatal(view, got, err)
	}
	if _, _, err = wire.Cell(bg, "outside"); err == nil {
		t.Fatal("R16 read outside grant")
	}
	heartbeat := cs.WorkerHeartbeat{Worker: worker, Incarnation: 1, Sequence: 1, SeenAt: r.clock.Now().UnixMilli()}
	b, err := cs.WorkerHeartbeatBytes(p.Cluster, heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	heartbeat.Signature = ed25519.Sign(key, b)
	submit(wire, sign(worker, cs.Mutation{WorkerBeatBatch: &cs.WorkerBeatBatch{Items: []cs.WorkerHeartbeat{heartbeat}}}), cs.OK)
	assignment := cs.Assign{ID: "opaque-run", Revision: 1, Cell: cell, Expect: cs.LeaseExpectation{Device: a.dev.ID(), Fence: 1}, Target: worker, Incarnation: 1, Expires: r.clock.Now().Add(time.Minute).UnixMilli(), SealedPayload: []byte{0x91}}
	submit(owner, sign(a.dev.ID(), cs.Mutation{Assign: &assignment}), cs.OK)
	ref := (cs.Assignment{Spec: assignment}).Ref()
	current, _, err := wire.Current(bg, ref)
	if err != nil || current.Status != "pending" {
		t.Fatal(current, err)
	}
	claim := sign(worker, cs.Mutation{ClaimAssignment: &cs.ClaimAssignment{Ref: ref, Expect: assignment.Expect}})
	claimed := submit(wire, claim, cs.OK)
	if claimed.View == nil || claimed.View.Cell.Lease.Fence != 2 {
		t.Fatal(claimed)
	}
	submit(wire, claim, cs.OK)
	// Retrying coordination never invokes a tool. Current state independently
	// rejects the saved claim once an ordinary conditional take has displaced it.
	expect := directory.LeaseExpectation{Device: worker, Fence: 2}
	if _, err = r.dir(a).Acquire(bg, cell, directory.AcquireOpts{Force: true, Expect: &expect}); err != nil {
		t.Fatal(err)
	}
	current, currentView, err := wire.Current(bg, ref)
	if err != nil {
		t.Fatal(err)
	}
	if current.Fence != 2 || currentView.Cell.Lease.Fence != 3 || currentView.Cell.Lease.Device != a.dev.ID() {
		t.Fatal("R18 current read returned the saved claim as current ownership", current, currentView)
	}
	incarnation = 2
	if _, _, err = wire.Cell(bg, cell); err == nil {
		t.Fatal("R16 stale incarnation read")
	}
	incarnation = 1
	// The worker cannot turn its delegation into a personal control request.
	submit(wire, sign(worker, cs.Mutation{Rotate: &cs.RotateRequest{Op: "freeze"}}), cs.Unauthorized)
	if err = r.dir(a).Revoke(bg, worker); err != nil {
		t.Fatal(err)
	}
	if _, _, err = wire.Cell(bg, cell); err == nil {
		t.Fatal("R16 revoked worker read")
	}
	submit(wire, claim, cs.Revoked)
	// A grant expires at COMMITTED ingress time even with no other active writer.
	r.clock.Advance(2 * time.Hour)
	if _, _, err = wire.Cell(bg, cell); err == nil {
		t.Fatal("R16 expired worker read")
	}
}
