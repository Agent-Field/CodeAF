package raftdirectory_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
)

type effectHost struct {
	g            *group
	failComplete bool
}

func (h *effectHost) CommitEffect(_ context.Context, c clusterstate.Command) (clusterstate.Result, error) {
	h.g.mu.Lock()
	defer h.g.mu.Unlock()
	if h.failComplete && c.Change.CompleteJoin != nil {
		return clusterstate.Result{}, directory.ErrUnreachable
	}
	return h.g.submit(c)
}
func (h *effectHost) ReadEffect(_ context.Context, k clusterstate.RecordKey) ([]byte, error) {
	g := h.g
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.leader()
	if i < 0 {
		return nil, directory.ErrUnreachable
	}
	g.readID++
	id := "effect-" + strings.Repeat("x", int(g.readID))
	g.drive(i, raftcore.Read{RequestID: id})
	g.drain()
	if !g.reads[id] {
		return nil, directory.ErrUnreachable
	}
	app, e := raftdirectory.AuthorizedEffectRead(ctx, g.stores[i], cluster, "effects")
	if e != nil {
		return nil, e
	}
	sk := raftstore.Key{Namespace: raftstore.Namespace(k.Namespace), Key: k.Key}
	rows, _, e := g.stores[i].Read(ctx, app.Applied, []raftstore.Key{sk})
	return rows[sk], e
}
func (h *effectHost) ScanEffects(_ context.Context, after string, limit int) ([]clusterstate.RecordWrite, error) {
	if _, e := h.ReadEffect(ctx, clusterstate.IdentityKey()); e != nil {
		return nil, e
	}
	g := h.g
	g.mu.Lock()
	defer g.mu.Unlock()
	i := g.leader()
	app, _, e := g.stores[i].ApplicationView(ctx)
	if e != nil {
		return nil, e
	}
	rows, _, e := g.stores[i].ScanPage(ctx, app.Applied, raftstore.Outbox, after)
	if e != nil {
		return nil, e
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	out := []clusterstate.RecordWrite{}
	for _, r := range rows {
		out = append(out, clusterstate.RecordWrite{RecordKey: clusterstate.RecordKey{Namespace: string(r.Namespace), Key: r.Key.Key}, Data: r.Value})
	}
	return out, nil
}
func restartEffects(t *testing.T, g *group) {
	t.Helper()
	g.queue = nil
	for i := range 3 {
		p, e := g.stores[i].CreateSnapshot(ctx)
		must(t, e)
		must(t, g.stores[i].Compact(ctx, p.GetMetadata().GetIndex()))
		must(t, g.stores[i].Close())
		s, e := raftstore.Open(g.paths[i], raftstore.Identity{ClusterUUID: cluster, NodeID: uint64(i + 1), Protocol: 1})
		must(t, e)
		g.stores[i] = s
		recovered, e := s.Recover(ctx)
		must(t, e)
		n, e := raftcore.New(raftcore.Config{ClusterUUID: cluster, NodeID: uint64(i + 1), Protocol: 1, Seed: uint64(i + 13)}, recovered.Core)
		must(t, e)
		g.nodes[i] = n
		g.drive(i, raftcore.Poll{})
	}
	g.drain()
	for n := 0; n < 40 && g.leader() < 0; n++ {
		for i := range 3 {
			g.drive(i, raftcore.Tick{})
		}
		g.drain()
	}
	if g.leader() < 0 {
		t.Fatal("no recovered leader")
	}
}
func TestR3R16R17R21CommittedBlobAndPairOutbox(t *testing.T) {
	clock := directorytest.NewFakeClock()
	g := newGroup(t, clock.Now)
	host := &effectHost{g: g}
	effects := raftdirectory.Effects{Host: host, Cluster: cluster, Actor: "effects", Key: key("effects")}
	disk, e := blobstore.NewDisk(t.TempDir())
	must(t, e)
	rid := strings.Repeat("1", 64)
	frame, e := blobstore.Encode(strings.Repeat("0", 32), []blobstore.Object{{RID: rid, Bytes: []byte("AGEO\x01sealed-object")}})
	must(t, e)
	upload := clusterstate.UploadReceipt{Device: "a", Head: "local-head", HeadRID: rid, Frames: []string{blobstore.IDOf(frame)}}
	if _, e = effects.Upload(ctx, clock.Now().UnixMilli(), disk, upload); !errors.Is(e, blobstore.ErrNotFound) {
		t.Fatal(e)
	}
	if _, e = disk.PutFrame(ctx, frame); e != nil {
		t.Fatal(e)
	}
	receipt, e := effects.Upload(ctx, clock.Now().UnixMilli(), disk, upload)
	must(t, e)
	command, e := clusterstate.Sign(clusterstate.Command{Schema: 1, Cluster: cluster, Actor: "a", Session: "v2", Sequence: 1, Now: clock.Now().UnixMilli(), Change: clusterstate.Mutation{Create: &clusterstate.CreateCell{Cell: "chat", Init: clusterstate.CellInit{Head: upload.Head, Frames: upload.Frames}, Receipt: receipt}}}, key("a"))
	must(t, e)
	result, e := g.submit(command)
	must(t, e)
	if result.Code != clusterstate.OK {
		t.Fatal(result)
	}
	restartEffects(t, g)
	result, e = g.submit(command)
	must(t, e)
	if result.Code != clusterstate.OK || result.View.Cell.Head != upload.Head {
		t.Fatal(result)
	}
	taken, e := directory.NewRaft(g).For("b").Acquire(ctx, "chat", directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: "a", Fence: 1}})
	must(t, e)
	if taken.Cell.Lease.Fence != 2 {
		t.Fatal(taken)
	}
	stale, e := directory.NewRaft(g).For("a").Publish(ctx, "chat", directory.Publish{Fence: 1, OldHead: upload.Head, Head: "different"})
	if !errors.Is(e, directory.ErrFenceStale) {
		t.Fatal(stale, e)
	}
	if _, e = disk.GetFrame(ctx, upload.Frames[0]); e != nil {
		t.Fatal("failed publication deleted uploaded frame", e)
	}
	links := directory.NewLinks(clock.Now, directory.DefaultLinkLimits)
	public := key("new-joiner").Public().(ed25519.PublicKey)
	b64 := base64.RawURLEncoding.EncodeToString
	opened, e := links.Open("local-fixture", directory.NewRequest{Pubkey: b64(public), X25519: b64(public), NameSealed: b64(make([]byte, 40)), Platform: "linux"})
	must(t, e)
	must(t, effects.Admit(ctx, clock.Now().UnixMilli(), links, opened.Code))
	if e = effects.Deliver(ctx, clock.Now().UnixMilli(), links, opened.Code); !errors.Is(e, directory.ErrRequestGone) {
		t.Fatal("uncommitted grant exposed", e)
	}
	cert := struct {
		V       uint16 `json:"V"`
		Device  string `json:"device"`
		Created int64  `json:"created"`
		Sig     string `json:"sig"`
	}{1, hex.EncodeToString(public), 1, ""}
	unsigned, _ := json.Marshal(cert)
	cert.Sig = hex.EncodeToString(ed25519.Sign(key("root"), append([]byte("codeaf/device-cert/v1\n"), unsigned...)))
	raw, _ := json.Marshal(cert)
	approval := directory.Approval{Device: directory.Device{V: 1, Name: "c2VhbGVk"}, Cert: b64(raw), Grant: "c2VhbGVkLWdyYW50"}
	must(t, directory.NewRaft(g).For("a").ApproveRequest(ctx, opened.Code, approval))
	// Simulate losing the completion after the external destination stored its answer.
	host.failComplete = true
	if e = effects.Deliver(ctx, clock.Now().UnixMilli(), links, opened.Code); !errors.Is(e, directory.ErrUnreachable) {
		t.Fatal(e)
	}
	request, e := links.Read(ctx, "local-fixture", opened.Code, 0)
	must(t, e)
	if request.State != directory.RequestApproved || request.Grant == nil || *request.Grant != approval.Grant {
		t.Fatal(request)
	}
	restartEffects(t, g)
	host.failComplete = false
	_, e = effects.Replay(ctx, clock.Now().UnixMilli(), links, "")
	must(t, e)
	_, e = effects.Replay(ctx, clock.Now().UnixMilli(), links, "")
	must(t, e)
	data, e := host.ReadEffect(ctx, clusterstate.JoinKey(opened.Code))
	must(t, e)
	var row clusterstate.JoinRequest
	must(t, clusterstate.Decode(data, &row))
	if row.Outcome != "delivered" {
		t.Fatal(row)
	}
	auth, e := host.ReadEffect(ctx, clusterstate.AuthorityKey(opened.Device))
	must(t, e)
	var enrolled clusterstate.Authority
	must(t, clusterstate.Decode(auth, &enrolled))
	if !enrolled.Personal || !ed25519.PublicKey(enrolled.Key).Equal(public) {
		t.Fatal(enrolled)
	}
	// Denials use the same committed outbox; expiry is a terminal delivery
	// outcome, rather than an instruction to invent a replacement grant.
	for _, expired := range []bool{false, true} {
		pending, err := links.Open("local-fixture", directory.NewRequest{Pubkey: b64(public), X25519: b64(public), NameSealed: b64(make([]byte, 40)), Platform: "linux"})
		must(t, err)
		must(t, effects.Admit(ctx, clock.Now().UnixMilli(), links, pending.Code))
		must(t, directory.NewRaft(g).For("b").DenyRequest(ctx, pending.Code))
		if expired {
			clock.Advance(directory.RequestTTL)
		}
		must(t, effects.Deliver(ctx, clock.Now().UnixMilli(), links, pending.Code))
		data, err := host.ReadEffect(ctx, clusterstate.JoinKey(pending.Code))
		must(t, err)
		var denied clusterstate.JoinRequest
		must(t, clusterstate.Decode(data, &denied))
		expected := "delivered"
		if expired {
			expected = "gone"
		}
		if denied.Outcome != expected || !denied.Decision.Deny {
			t.Fatal(denied)
		}
	}
	// Revocation after ingress still refuses a delayed upload receipt in the log.
	must(t, directory.NewRaft(g).For("b").PutDevice(ctx, "a", directory.Device{V: 1}))
	must(t, directory.NewRaft(g).For("b").Revoke(ctx, "a"))
	if _, e = effects.Upload(ctx, clock.Now().UnixMilli(), disk, upload); !errors.Is(e, directory.ErrRevoked) {
		t.Fatal(e)
	}
	if _, e = disk.GetFrame(ctx, upload.Frames[0]); e != nil {
		t.Fatal("revocation deleted reusable bytes", e)
	}
}
