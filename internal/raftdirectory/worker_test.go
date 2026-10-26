package raftdirectory_test

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"reflect"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
)

// R3/R13/R15/R16/R17/R18 exercise real quorum/application persistence, selected
// grant retrieval, learner protocol authority and delayed worker commands across
// drain/incarnation/revoke, with exact retry after snapshot/compaction/restart.
func TestR3R13R15R16R17R18WorkerRegistryNative(t *testing.T) {
	now := int64(1000)
	g := newGroup(t, func() time.Time { return time.UnixMilli(now) })
	k := key("delegated-native-worker")
	id := cs.DeviceIDOfKey(k.Public().(ed25519.PublicKey))
	serial := 0
	command := func(actor string, key ed25519.PrivateKey, inc uint64, m cs.Mutation) cs.Command {
		serial++
		c, e := cs.Sign(cs.Command{Schema: 1, Cluster: cluster, Actor: actor, Session: fmt.Sprint("worker-", serial), Sequence: 1, Now: now, WorkerIncarnation: inc, Change: m}, key)
		must(t, e)
		return c
	}
	submit := func(c cs.Command, want cs.Code) cs.Result {
		t.Helper()
		r, e := g.submit(c)
		must(t, e)
		if r.Code != want {
			t.Fatalf("want %s: %+v", want, r)
		}
		return r
	}
	owner := func(m cs.Mutation, want cs.Code) cs.Result { return submit(command("a", key("a"), 0, m), want) }
	cert := cs.WorkerCertificate{V: 1, Identity: cs.IdentityID(key("root").Public().(ed25519.PublicKey)), Cluster: cluster, Key: k.Public().(ed25519.PublicKey), Made: 1000, Expires: 1000000, Cells: []string{"selected"}, Learner: true}
	body, e := cs.WorkerCertificateBytes(cert)
	must(t, e)
	cert.Signature = ed25519.Sign(key("root"), body)
	grant := []byte{0xde, 0xad, 0xbe, 0xef}
	owner(cs.Mutation{EnrollWorker: &cs.EnrollWorker{Certificate: cert, Grants: map[string][]byte{"selected": grant}}}, cs.OK)
	registration := command(id, k, 0, cs.Mutation{RegisterWorker: &cs.RegisterWorker{Incarnation: 1, Caps: cs.Caps{OS: "linux", Arch: "amd64", Cow: "none"}, SealedLabels: []byte{1, 2, 3}}})
	first := submit(registration, cs.OK)
	owner(cs.Mutation{Create: &cs.CreateCell{V1: true, Cell: "selected", Init: cs.CellInit{Head: "head"}}}, cs.OK)
	owner(cs.Mutation{Create: &cs.CreateCell{V1: true, Cell: "outside", Init: cs.CellInit{Head: "other"}}}, cs.OK)
	i := g.leader()
	read := func(i int, k cs.RecordKey) ([]byte, error) {
		app, _, e := g.stores[i].ApplicationView(ctx)
		if e != nil {
			return nil, e
		}
		sk := raftstore.Key{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}
		rows, _, e := g.stores[i].Read(ctx, app.Applied, []raftstore.Key{sk})
		return rows[sk], e
	}
	// The fresh owner barrier is the host's explicit prerequisite to scoped reads.
	_, _, e = g.barrier("a")
	must(t, e)
	view, got, e := raftdirectory.AuthorizedWorkerRead(ctx, g.stores[i], cluster, id, "selected", 1)
	must(t, e)
	if view.Cell.Head != "head" || !bytes.Equal(got, grant) {
		t.Fatal("selected grant/head lost")
	}
	if _, _, e = raftdirectory.AuthorizedWorkerRead(ctx, g.stores[i], cluster, id, "outside", 1); e == nil {
		t.Fatal("R16 cross-scope read succeeded")
	}
	if _, e = raftdirectory.AuthorizedRead(ctx, g.stores[i], cluster, id); e == nil {
		t.Fatal("worker read identity-wide directory")
	}
	anchors := []cs.MemberRecord{}
	for n := uint64(1); n <= 3; n++ {
		anchors = append(anchors, cs.MemberRecord{Node: n, Key: key(fmt.Sprint("anchor-", n)).Public().(ed25519.PublicKey), Incarnation: 1, Protocol: 1, Anchor: true})
	}
	owner(cs.Mutation{InitMembership: &cs.InitMembership{Anchors: anchors, Limit: cs.MaxMemberRecords}}, cs.OK)
	h := registry(t, g, i)
	owner(cs.Mutation{EnrollMember: &cs.EnrollMember{Node: h.NextNode, Revision: h.Revision, Device: id, Protocol: 1}}, cs.OK)
	// A real learner may be absent; it cannot pin owner commits or compaction.
	plan, e := g.nodes[i].PlanMembership(registry(t, g, i))
	must(t, e)
	g.drive(i, raftcore.Configure{Change: plan.Change})
	g.drain()
	h = registry(t, g, g.leader())
	owner(cs.Mutation{SettleMembership: &cs.SettleMembership{Revision: h.Revision, Node: 4}}, cs.OK)
	peers := func() rafttransport.View {
		i := g.leader()
		app, _, e := g.stores[i].ApplicationView(ctx)
		must(t, e)
		v, e := rafttransport.CommittedViewAt(rafttransport.Scope{Identity: cert.Identity, Cluster: cluster, Epoch: 1, Protocol: 1}, 1, app.Applied, raftcore.Configuration(app.ConfState), app.LogicalTime, func(k cs.RecordKey) ([]byte, error) { return read(i, k) })
		must(t, e)
		return v
	}
	if v := peers(); v.Members[3].Revoked || v.Members[3].Voter {
		t.Fatal("worker is not a learner", v)
	}
	for j, s := range g.stores {
		m, e := s.CreateSnapshot(ctx)
		must(t, e)
		must(t, s.Compact(ctx, m.GetMetadata().GetIndex()))
		g.drive(j, raftcore.CompactSnapshot{Snapshot: m})
	}
	g.drain()
	recoverMembership(t, g)
	if r := submit(registration, cs.OK); !reflect.DeepEqual(r, first) {
		t.Fatal("R17 compacted result changed", r, first)
	}
	pending := command(id, k, 1, cs.Mutation{Publish: &cs.PublishCell{Cell: "outside", Publish: cs.Publish{Fence: 1, Head: "forbidden"}}})
	submit(pending, cs.Unauthorized)
	router, e := rafttransport.NewRouter(rafttransport.DefaultLimits())
	must(t, e)
	initialView := peers()
	must(t, router.Install(initialView))
	req := rafttransport.Request{Scope: initialView.Scope, Node: 4, Incarnation: 1, Generation: 1}
	must(t, req.Sign(k))
	_, e = router.Exchange(req, now)
	must(t, e)
	owner(cs.Mutation{DrainWorker: &cs.DrainWorker{Worker: id, Incarnation: 1}}, cs.OK)
	submit(command(id, k, 1, cs.Mutation{RegisterWorker: &cs.RegisterWorker{Previous: 1, Incarnation: 2}}), cs.OK)
	if !peers().Members[3].Revoked {
		t.Fatal("old peer survived worker restart")
	}
	// A real signed router exchange is denied after installing the narrowed view.
	must(t, router.Install(peers()))
	if _, e = router.Exchange(req, now); e == nil {
		t.Fatal("R16 authenticated old transport survived incarnation")
	}
	owner(cs.Mutation{Revoke: &cs.DeviceID{Device: id}}, cs.OK)
	submit(command(id, k, 2, cs.Mutation{Release: &cs.ReleaseCell{Cell: "selected", Fence: 1}}), cs.Revoked)
	if _, _, e = raftdirectory.AuthorizedWorkerRead(ctx, g.stores[g.leader()], cluster, id, "selected", 2); e == nil {
		t.Fatal("revoked worker retrieved a grant")
	}
}
