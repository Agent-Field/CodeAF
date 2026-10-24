package raftdirectory_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Agent-Field/codeaf/internal/wireauth"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"go.etcd.io/raft/v3"
)

var ctx = context.Background()

const cluster = "p09-directory"
const devA = "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const devB = "dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func encoded(t *testing.T, v any) []byte {
	t.Helper()
	b, e := clusterstate.Encode(v)
	must(t, e)
	return b
}
func key(actor string) ed25519.PrivateKey {
	h := sha256.Sum256([]byte(actor))
	return ed25519.NewKeyFromSeed(h[:])
}

type group struct {
	t       *testing.T
	mu      sync.Mutex
	clock   func() time.Time
	paths   [3]string
	stores  [3]*raftstore.SQLite
	nodes   [3]*raftcore.Core
	status  [3]raftcore.Status
	queue   []raftcore.Outbound
	results [3][]clusterstate.Result
	reads   map[string]bool
	keys    map[string]ed25519.PrivateKey
	seq     map[string]uint64
	genesis []raftstore.Write
	blocked bool
	readID  uint64
}

func newGroup(t *testing.T, clock func() time.Time, extra ...raftstore.Write) *group {
	g := &group{t: t, clock: clock, reads: map[string]bool{}, seq: map[string]uint64{}}
	actors := []string{devA, devB, "a", "b"}
	for i := range 50 {
		actors = append(actors, fmt.Sprintf("dev_%032x", i+1))
	}
	g.genesis = append(g.genesis, raftstore.Write{Key: raftstore.Key{Namespace: raftstore.State, Key: clusterstate.IdentityKey().Key}, Value: encoded(t, clusterstate.IdentityRec{V: 1, Identity: clusterstate.IdentityID(key("root").Public().(ed25519.PublicKey)), SigningKey: key("root").Public().(ed25519.PublicKey)})})
	for _, actor := range actors {
		k := clusterstate.AuthorityKey(actor)
		g.genesis = append(g.genesis, raftstore.Write{Key: raftstore.Key{Namespace: raftstore.Enrollment, Key: k.Key}, Value: encoded(t, clusterstate.Authority{Cluster: cluster, Key: key(actor).Public().(ed25519.PublicKey), Personal: true})})
	}
	g.genesis = append(g.genesis, raftstore.Write{Key: raftstore.Key{Namespace: raftstore.Enrollment, Key: clusterstate.AuthorityKey("effects").Key}, Value: encoded(t, clusterstate.Authority{Cluster: cluster, Key: key("effects").Public().(ed25519.PublicKey), Effects: true})})
	g.genesis = append(g.genesis, extra...)
	for i := range 3 {
		g.paths[i] = filepath.Join(t.TempDir(), "member.db")
		s, e := raftstore.Open(g.paths[i], raftstore.Identity{ClusterUUID: cluster, NodeID: uint64(i + 1), Protocol: 1})
		must(t, e)
		g.stores[i] = s
		n, e := raftcore.New(raftcore.Config{ClusterUUID: cluster, NodeID: uint64(i + 1), Seed: uint64(i + 13), Protocol: 1, Bootstrap: []uint64{1, 2, 3}}, raftcore.RecoveredState{})
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
		t.Fatal("no leader")
	}
	t.Cleanup(func() {
		for _, s := range g.stores {
			must(t, s.Close())
		}
	})
	return g
}
func (g *group) drive(i int, in raftcore.Input) {
	e, err := g.nodes[i].Step(in)
	must(g.t, err)
	for n := 0; n < 100; n++ {
		g.status[i] = e.Status
		g.queue = append(g.queue, e.Send...)
		for _, r := range e.Reads {
			app, _, err := g.stores[i].ApplicationView(ctx)
			must(g.t, err)
			if app.Applied < r.Index {
				g.t.Fatal("read before apply")
			}
			g.reads[r.RequestID] = true
		}
		if e.Persist != nil {
			must(g.t, g.stores[i].Persist(ctx, *e.Persist))
			e, err = g.nodes[i].Step(raftcore.Persisted{BatchID: e.Persist.ID})
		} else if e.Apply != nil {
			app, _, err := g.stores[i].ApplicationView(ctx)
			must(g.t, err)
			if app.Applied == 0 {
				must(g.t, g.stores[i].Apply(ctx, *e.Apply, raftstore.Update{ExpectedApplied: 0, Writes: g.genesis}))
			} else {
				r, err := raftdirectory.Apply(ctx, g.stores[i], *e.Apply, cluster, clusterstate.DefaultApplicationBytes)
				must(g.t, err)
				g.results[i] = append(g.results[i], r...)
			}
			e, err = g.nodes[i].Step(raftcore.Applied{BatchID: e.Apply.ID})
		} else {
			return
		}
		must(g.t, err)
	}
	g.t.Fatal("completion budget")
}
func (g *group) drain() {
	for n := 0; len(g.queue) > 0; n++ {
		if n > 10000 {
			g.t.Fatal("network budget")
		}
		m := g.queue[0]
		g.queue = g.queue[1:]
		if !g.blocked {
			g.drive(int(m.To)-1, raftcore.PeerMessage{Bytes: m.Bytes})
		}
	}
}
func (g *group) leader() int {
	for i, s := range g.status {
		if s.Role == raft.StateLeader {
			return i
		}
	}
	return -1
}
func (g *group) submit(c clusterstate.Command) (clusterstate.Result, error) {
	i := g.leader()
	if i < 0 {
		return clusterstate.Result{}, directory.ErrUnreachable
	}
	raw, e := clusterstate.EncodeCommand(c)
	if e != nil {
		return clusterstate.Result{}, e
	}
	before := len(g.results[i])
	g.drive(i, raftcore.Propose{Command: raw})
	g.drain()
	if len(g.results[i]) == before {
		return clusterstate.Result{}, directory.ErrUnreachable
	}
	return g.results[i][before], nil
}
func (g *group) commandKey(actor string) ed25519.PrivateKey {
	if k := g.keys[actor]; k != nil {
		return k
	}
	return key(actor)
}
func (g *group) Change(_ context.Context, actor string, m clusterstate.Mutation) (clusterstate.Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.seq[actor]++
	seq := g.seq[actor]
	c, e := clusterstate.Sign(clusterstate.Command{Schema: 1, Cluster: cluster, Actor: actor, Session: "api", Sequence: seq, Ack: seq - 1, Now: g.clock().UnixMilli(), Change: m}, g.commandKey(actor))
	if e != nil {
		return clusterstate.Result{}, e
	}
	return g.submit(c)
}
func (g *group) barrier(actor string) (int, raftstore.Application, error) {
	i := g.leader()
	if i < 0 {
		return i, raftstore.Application{}, directory.ErrUnreachable
	}
	// Public expiry reads commit the ingress clock before their fresh barrier.
	g.seq[actor]++
	seq := g.seq[actor]
	c, e := clusterstate.Sign(clusterstate.Command{Schema: 1, Cluster: cluster, Actor: actor, Session: "api", Sequence: seq, Ack: seq - 1, Now: g.clock().UnixMilli(), Change: clusterstate.Mutation{AdvanceTime: &clusterstate.AdvanceTime{}}}, g.commandKey(actor))
	if e != nil {
		return i, raftstore.Application{}, e
	}
	result, e := g.submit(c)
	if e != nil {
		return i, raftstore.Application{}, e
	}
	if result.Code == clusterstate.Revoked {
		return i, raftstore.Application{}, directory.ErrRevoked
	}
	if result.Code != clusterstate.OK {
		return i, raftstore.Application{}, directory.ErrUnauthorized
	}
	g.readID++
	id := fmt.Sprint(g.readID)
	g.drive(i, raftcore.Read{RequestID: id})
	g.drain()
	if !g.reads[id] {
		return i, raftstore.Application{}, directory.ErrUnreachable
	}
	app, e := raftdirectory.AuthorizedRead(ctx, g.stores[i], cluster, actor)
	return i, app, e
}
func (g *group) Listing(_ context.Context, actor string) (directory.Listing, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i, app, e := g.barrier(actor)
	if e != nil {
		return directory.Listing{}, e
	}
	l := directory.Listing{Now: app.LogicalTime, Version: app.VisibleVersion, Devices: map[string]directory.Device{}, Cells: map[string]directory.Cell{}}
	after := ""
	for {
		rows, _, err := g.stores[i].ScanPage(ctx, app.Applied, raftstore.State, after)
		if err != nil {
			return l, err
		}
		if len(rows) == 0 {
			break
		}
		for _, r := range rows {
			after = r.Key.Key
			switch {
			case after == "identity":
				must(g.t, clusterstate.Decode(r.Value, &l.Identity))
			case strings.HasPrefix(after, "device/"):
				var d directory.Device
				must(g.t, clusterstate.Decode(r.Value, &d))
				b, e := hex.DecodeString(strings.TrimPrefix(after, "device/"))
				must(g.t, e)
				l.Devices[string(b)] = d
			case strings.HasPrefix(after, "cell/"):
				var c directory.Cell
				must(g.t, clusterstate.Decode(r.Value, &c))
				b, e := hex.DecodeString(strings.TrimPrefix(after, "cell/"))
				must(g.t, e)
				l.Cells[string(b)] = c
			}
		}
	}
	return l, nil
}
func (g *group) ReadCell(_ context.Context, actor, id string) (directory.CellView, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	i, app, e := g.barrier(actor)
	if e != nil {
		return directory.CellView{}, e
	}
	k := raftstore.Key{Namespace: raftstore.State, Key: clusterstate.CellKey(id).Key}
	rows, _, e := g.stores[i].Read(ctx, app.Applied, []raftstore.Key{k})
	if e != nil {
		return directory.CellView{}, e
	}
	if rows[k] == nil {
		return directory.CellView{}, directory.ErrNotFound
	}
	var c directory.Cell
	e = clusterstate.Decode(rows[k], &c)
	return directory.CellView{Now: app.LogicalTime, Cell: c}, e
}

// R1/R2/R16/R25/R30 exercise the existing Client verbs through three real
// durable stores and the real quorum/persistence/application protocol.
func TestR1R2R16R25R30RaftDirectoryConformance(t *testing.T) {
	directorytest.RunConsensus(t, func(t *testing.T, clock *directorytest.FakeClock) directorytest.Devices {
		g := newGroup(t, clock.Now)
		return directory.NewRaft(g).For
	})
}
func TestR4MinorityDirectoryCannotReadOrWrite(t *testing.T) {
	clock := directorytest.NewFakeClock()
	g := newGroup(t, clock.Now)
	cl := directory.NewRaft(g).For("a")
	_, e := cl.Create(ctx, "chat", directory.CellInit{Head: "one"})
	must(t, e)
	g.blocked = true
	_, e = cl.Cell(ctx, "chat")
	if !errors.Is(e, directory.ErrUnreachable) {
		t.Fatalf("minority read: %v", e)
	}
}

func TestR3R13R17DirectoryRetryAfterSnapshotAndNativeRestart(t *testing.T) {
	clock := directorytest.NewFakeClock()
	g := newGroup(t, clock.Now)
	cl := directory.NewRaft(g).For("a")
	_, err := cl.Create(ctx, "chat", directory.CellInit{Head: "sealed"})
	must(t, err)
	c, err := clusterstate.Sign(clusterstate.Command{Schema: 1, Cluster: cluster, Actor: "b", Session: "lost-reply", Sequence: 1, Now: clock.Now().UnixMilli(), Change: clusterstate.Mutation{Acquire: &clusterstate.AcquireCell{Cell: "chat", Force: true, Expect: clusterstate.LeaseExpectation{Device: "a", Fence: 1}}}}, key("b"))
	must(t, err)
	first, err := g.submit(c)
	must(t, err)
	if first.Code != clusterstate.OK || first.View.Cell.Lease.Fence != 2 {
		t.Fatal(first)
	}
	for i := range 3 {
		p, err := g.stores[i].CreateSnapshot(ctx)
		must(t, err)
		must(t, g.stores[i].Compact(ctx, p.GetMetadata().GetIndex()))
		g.drive(i, raftcore.CompactSnapshot{Snapshot: p})
	}
	g.drain()
	for i := range 3 {
		must(t, g.stores[i].Close())
		s, err := raftstore.Open(g.paths[i], raftstore.Identity{ClusterUUID: cluster, NodeID: uint64(i + 1), Protocol: 1})
		must(t, err)
		g.stores[i] = s
		recovered, err := s.Recover(ctx)
		must(t, err)
		node, err := raftcore.New(raftcore.Config{ClusterUUID: cluster, NodeID: uint64(i + 1), Protocol: 1, Seed: uint64(i + 13)}, recovered.Core)
		must(t, err)
		g.nodes[i] = node
		g.drive(i, raftcore.Poll{})
	}
	g.drain()
	for n := 0; n < 40 && g.leader() < 0; n++ {
		for i := range 3 {
			g.drive(i, raftcore.Tick{})
		}
		g.drain()
	}
	c.Now += 5000
	c, err = clusterstate.Sign(c, key("b"))
	must(t, err)
	retry, err := g.submit(c)
	must(t, err)
	firstBytes, err := clusterstate.EncodeResult(first)
	must(t, err)
	retryBytes, err := clusterstate.EncodeResult(retry)
	must(t, err)
	if string(firstBytes) != string(retryBytes) {
		t.Fatal("lost result changed after snapshot/restart")
	}
}

// R30 covers the remaining Client verbs at the actual committed adapter door.
// External LinkRequest delivery and certificate admission remain P10's gate.
func TestR16R30PresenceAndPairDecisionDependencies(t *testing.T) {
	clock := directorytest.NewFakeClock()
	g := newGroup(t, clock.Now)
	cl := directory.NewRaft(g).For("a")
	p, err := cl.Presence(ctx)
	must(t, err)
	if len(p.Devices) != 0 {
		t.Fatal(p)
	}
	if err := cl.DenyRequest(ctx, "unknown"); !errors.Is(err, directory.ErrRequestGone) {
		t.Fatal(err)
	}
	if err := cl.ApproveRequest(ctx, "unknown", directory.Approval{}); !errors.Is(err, directory.ErrBadRequest) {
		t.Fatal(err)
	}
	must(t, cl.PutDevice(ctx, "b", directory.Device{V: 1, Name: "sealed-name"}))
	must(t, cl.Revoke(ctx, "b"))
	revoked := directory.NewRaft(g).For("b")
	if _, err := revoked.Presence(ctx); !errors.Is(err, directory.ErrRevoked) {
		t.Fatal(err)
	}
	if err := revoked.DenyRequest(ctx, "unknown"); !errors.Is(err, directory.ErrRevoked) {
		t.Fatal(err)
	}
}

func TestR16R30ApproveAndDenyCommitBeforeAnswer(t *testing.T) {
	clock := directorytest.NewFakeClock()
	pub := key("joiner").Public().(ed25519.PublicKey)
	sum := sha256.Sum256(pub)
	device := "dev_" + hex.EncodeToString(sum[:16])
	writes := []raftstore.Write{}
	for _, code := range []string{"approve", "deny"} {
		k := clusterstate.JoinKey(code)
		writes = append(writes, raftstore.Write{Key: raftstore.Key{Namespace: raftstore.Outbox, Key: k.Key}, Value: encoded(t, clusterstate.JoinRequest{Device: device, PublicKey: pub, Expires: clock.Now().UnixMilli() + 600000})})
	}
	g := newGroup(t, clock.Now, writes...)
	cl := directory.NewRaft(g).For("a")
	cert := struct {
		V       uint16 `json:"V"`
		Device  string `json:"device"`
		Created int64  `json:"created"`
		Sig     string `json:"sig"`
	}{1, hex.EncodeToString(pub), 1, ""}
	unsigned, _ := json.Marshal(cert)
	cert.Sig = hex.EncodeToString(ed25519.Sign(key("root"), append([]byte("codeaf/device-cert/v1\n"), unsigned...)))
	raw, err := json.Marshal(cert)
	must(t, err)
	approval := directory.Approval{Device: directory.Device{V: 1, Name: "sealed"}, Cert: base64.RawURLEncoding.EncodeToString(raw), Grant: "c2VhbGVkLWdyYW50"}
	must(t, cl.ApproveRequest(ctx, "approve", approval))
	must(t, cl.ApproveRequest(ctx, "approve", approval))
	if err := cl.DenyRequest(ctx, "approve"); !errors.Is(err, directory.ErrAlreadyDecided) {
		t.Fatal(err)
	}
	must(t, cl.DenyRequest(ctx, "deny"))
	must(t, cl.DenyRequest(ctx, "deny"))
	l, err := cl.List(ctx)
	must(t, err)
	if l.Devices[device].Name != "sealed" {
		t.Fatal(l)
	}
	for _, store := range g.stores {
		app, _, err := store.ApplicationView(ctx)
		must(t, err)
		k := raftstore.Key{Namespace: raftstore.Outbox, Key: clusterstate.JoinKey("approve").Key}
		rows, _, err := store.Read(ctx, app.Applied, []raftstore.Key{k})
		must(t, err)
		var request clusterstate.JoinRequest
		must(t, clusterstate.Decode(rows[k], &request))
		if request.Decision == nil || request.Decision.Grant != "c2VhbGVkLWdyYW50" {
			t.Fatal("answer preceded committed sealed decision")
		}
	}
}

func TestR1R25R30DirectoryHTTPFacadeUsesConsensus(t *testing.T) {
	clock := directorytest.NewFakeClock()
	g := newGroup(t, clock.Now)
	adapter := directory.NewRaft(g)
	auth := func(r *http.Request, _ []byte) (string, string, error) {
		actor := r.Header.Get("X-Test-Actor")
		if actor != "a" && actor != "b" {
			return "", "", wireauth.ErrUnauthorized
		}
		return cluster, actor, nil
	}
	server := httptest.NewServer(directory.Handler(auth, func(id string) (directory.Directory, error) {
		if id != cluster {
			return nil, directory.ErrUnauthorized
		}
		return adapter, nil
	}))
	defer server.Close()
	client := func(actor string) *directory.HTTP {
		return directory.NewHTTP(server.URL, func(r *http.Request, _ []byte) { r.Header.Set("X-Test-Actor", actor) }, server.Client())
	}
	a, b := client("a"), client("b")
	id := "01J0000000000000000000000A"
	head := strings.Repeat("1", 64)
	created, err := a.Create(ctx, id, directory.CellInit{Head: head, Class: "chat", Size: 1})
	must(t, err)
	expect := directory.LeaseExpectation{Device: created.Cell.Lease.Device, Fence: created.Cell.Lease.Fence}
	taken, err := b.Acquire(ctx, id, directory.AcquireOpts{Force: true, Expect: &expect})
	must(t, err)
	if taken.Cell.Lease.Fence != 2 {
		t.Fatal(taken)
	}
	if _, err := a.Acquire(ctx, id, directory.AcquireOpts{Force: true}); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatal("blind v1 force changed the holder", err)
	}
	_, err = a.Publish(ctx, id, directory.Publish{Fence: 1, OldHead: head, Head: strings.Repeat("2", 64)})
	if !errors.Is(err, directory.ErrFenceStale) {
		t.Fatal(err)
	}
	l, err := a.List(ctx)
	must(t, err)
	if l.Cells[id].Lease.Device != "b" {
		t.Fatal(l)
	}
}
