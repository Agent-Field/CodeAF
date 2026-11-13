package raftdirectory_test

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftstore"
)

func migrationCertificate(t *testing.T, public ed25519.PublicKey) string {
	t.Helper()
	c := struct {
		V       uint16 `json:"V"`
		Device  string `json:"device"`
		Created int64  `json:"created"`
		Sig     string `json:"sig"`
	}{V: 1, Device: hex.EncodeToString(public), Created: 1000}
	b, e := json.Marshal(c)
	must(t, e)
	c.Sig = hex.EncodeToString(ed25519.Sign(key("root"), append([]byte("codeaf/device-cert/v1\n"), b...)))
	b, e = json.Marshal(c)
	must(t, e)
	return base64.RawURLEncoding.EncodeToString(b)
}

type migrationHome struct {
	id  identity.Identity
	dev identity.Dev
}

func (h migrationHome) IdentityKey() ed25519.PublicKey { return h.id.PublicKey() }
func (h migrationHome) Cert() identity.Cert            { return h.dev.Cert }
func (h migrationHome) Sign(b []byte) []byte           { return h.dev.Sign(b) }
func savedMigrationHome(t *testing.T, name string) migrationHome {
	t.Helper()
	root := hex.EncodeToString(key("root").Seed())
	id, e := identity.Unmarshal([]byte(`{"V":1,"signing_seed":"` + root + `","dedup_secret":"` + root + `","cell_key":"` + root + `"}`))
	must(t, e)
	home := t.TempDir()
	must(t, identity.Save(home, id))
	var cert identity.Cert
	raw, e := base64.RawURLEncoding.DecodeString(migrationCertificate(t, key(name).Public().(ed25519.PublicKey)))
	must(t, e)
	must(t, json.Unmarshal(raw, &cert))
	doc := struct {
		V    uint16        `json:"V"`
		Seed string        `json:"device_seed"`
		Cert identity.Cert `json:"cert"`
	}{1, hex.EncodeToString(key(name).Seed()), cert}
	body, e := json.Marshal(doc)
	must(t, e)
	must(t, os.WriteFile(filepath.Join(home, identity.DeviceFile), body, 0600))
	dev, e := identity.Device(home)
	must(t, e)
	if dev.ID() != clusterstate.DeviceIDOfKey(key(name).Public().(ed25519.PublicKey)) {
		t.Fatal("saved fixture device changed")
	}
	if !ed25519.Verify(key(name).Public().(ed25519.PublicKey), []byte("fixture"), dev.Sign([]byte("fixture"))) {
		t.Fatal("saved device key differs from certificate")
	}
	return migrationHome{id, dev}
}

// R24 exercises the old Client and the committed Client at their real doors.
// Both homes keep their certified authority; the source routes only after a
// fresh current-term quorum barrier, and cannot return to legacy writes.
func TestR24TwoCertifiedHomesMigrateThroughQuorumAndRestart(t *testing.T) {
	clock := func() time.Time { return time.UnixMilli(1000) }
	akey, bkey := key("paired-a").Public().(ed25519.PublicKey), key("paired-b").Public().(ed25519.PublicKey)
	a, b := clusterstate.DeviceIDOfKey(akey), clusterstate.DeviceIDOfKey(bkey)
	source, e := directory.OpenSQLite(filepath.Join(t.TempDir(), "old.db"), clock)
	must(t, e)
	defer source.Close()
	homes := []migrationHome{savedMigrationHome(t, "paired-a"), savedMigrationHome(t, "paired-b")}
	var routeMu sync.RWMutex
	var routed directory.Directory = source
	server := httptest.NewServer(directory.Handler(func(r *http.Request, body []byte) (string, string, error) {
		id, dev, e := reqsign.Verify(r, body, clock())
		if e != nil {
			t.Logf("real ingress refusal: %v", e)
		}
		return id, dev, e
	}, func(id string) (directory.Directory, error) {
		routeMu.RLock()
		defer routeMu.RUnlock()
		if id != homes[0].id.ID() {
			return nil, directory.ErrUnauthorized
		}
		return routed, nil
	}))
	defer server.Close()
	clients := []*directory.HTTP{directory.NewHTTP(server.URL, reqsign.SignFor(homes[0], clock), server.Client()), directory.NewHTTP(server.URL, reqsign.SignFor(homes[1], clock), server.Client())}
	for i, id := range []string{a, b} {
		must(t, clients[i].PutDevice(ctx, id, directory.Device{V: 1, Name: "c2VhbGVk"}))
	}
	view, e := clients[0].Create(ctx, "chat", directory.CellInit{Head: "saved", Class: "host-bound", Frames: []string{"sealed-frame"}})
	must(t, e)
	root := key("root").Public().(ed25519.PublicKey)
	effects := key("effects").Public().(ed25519.PublicKey)
	cfg := clusterstate.LegacyConfig{Cluster: cluster, Generation: 1, Identity: clusterstate.IdentityID(root), SigningKey: root, EffectsKey: effects, Anchors: []uint64{1, 2, 3}, Now: 1000, Certificates: []string{migrationCertificate(t, akey), migrationCertificate(t, bkey)}}
	must(t, source.FreezeLegacy(ctx, cfg))
	manifest, e := source.InventoryLegacy(ctx)
	must(t, e)
	g := &group{paths: make([]string, 3), stores: make([]*raftstore.SQLite, 3), nodes: make([]*raftcore.Core, 3), status: make([]raftcore.Status, 3), results: make([][]clusterstate.Result, 3), absent: map[uint64]bool{}, t: t, clock: clock, reads: map[string]bool{}, seq: map[string]uint64{}, keys: map[string]ed25519.PrivateKey{a: key("paired-a"), b: key("paired-b")}}
	defer func() {
		for _, store := range g.stores {
			if store != nil {
				must(t, store.Close())
			}
		}
	}()
	for i := range 3 {
		g.paths[i] = filepath.Join(t.TempDir(), "member.db")
		g.stores[i], e = raftstore.Open(g.paths[i], raftstore.Identity{ClusterUUID: cluster, NodeID: uint64(i + 1), Protocol: 1})
		must(t, e)
		must(t, g.stores[i].ImportLegacy(ctx, manifest, func(fn func(clusterstate.RecordWrite) error) error { _, e := source.ExportLegacy(ctx, fn); return e }))
		recovered, e := g.stores[i].Recover(ctx)
		must(t, e)
		g.nodes[i], e = raftcore.New(raftcore.Config{ClusterUUID: cluster, NodeID: uint64(i + 1), Protocol: 1, Seed: uint64(i + 51)}, recovered.Core)
		must(t, e)
	}
	for n := 0; n < 40 && g.leader() < 0; n++ {
		for i := range 3 {
			g.drive(i, raftcore.Tick{})
		}
		g.drain()
	}
	if g.leader() < 0 {
		t.Fatal("imported group cannot elect")
	}
	// The fixture keys match real certified IDs rather than its older short IDs.
	for _, actor := range []string{a, b} {
		g.seq[actor] = 0
	}
	propose := func(actor string, private ed25519.PrivateKey, m clusterstate.Mutation) clusterstate.Result {
		g.seq[actor]++
		seq := g.seq[actor]
		c, e := clusterstate.Sign(clusterstate.Command{Schema: 1, Cluster: cluster, Actor: actor, Session: "paired", Sequence: seq, Ack: seq - 1, Now: 1000, Change: m}, private)
		must(t, e)
		r, e := g.submit(c)
		must(t, e)
		return r
	}
	for _, actor := range []struct{ id, name string }{{a, "paired-a"}, {b, "paired-b"}} {
		r := propose(actor.id, key(actor.name), clusterstate.Mutation{AdvanceTime: &clusterstate.AdvanceTime{}})
		if r.Code != clusterstate.OK {
			t.Fatal("paired home stranded", r.Code)
		}
	}
	leader := g.leader()
	g.drive(leader, raftcore.Read{RequestID: "migration"})
	g.drain()
	if !g.reads["migration"] {
		t.Fatal("no fresh quorum barrier")
	}
	receipts := []directory.LegacyReceipt{}
	for i, s := range g.stores {
		m, e := s.ImportedLegacy(context.Background())
		must(t, e)
		app, _, e := s.ApplicationView(ctx)
		must(t, e)
		receipts = append(receipts, directory.LegacyReceipt{Node: uint64(i + 1), Manifest: *m, Applied: app.Applied})
	}
	must(t, source.OwnLegacy(ctx, receipts))
	next, e := source.MigrationRoute(ctx, func(clusterstate.LegacyManifest) (directory.RaftBackend, error) { return g, nil })
	must(t, e)
	routeMu.Lock()
	routed = next
	routeMu.Unlock()
	g.seq[a] = 0
	g.seq[b] = 0
	for _, client := range clients {
		listing, e := client.List(ctx)
		must(t, e)
		if listing.Cells["chat"].Head != "saved" {
			t.Fatal("paired HTTP home lost saved chat")
		}
	}

	taken, e := clients[1].Acquire(ctx, "chat", directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: a, Fence: view.Cell.Lease.Fence}})
	must(t, e)
	if taken.Cell.Lease.Fence != 2 {
		t.Fatal("HTTP continuation failed")
	}
	if _, e = source.For(a).Acquire(ctx, "chat", directory.AcquireOpts{Force: true}); !errors.Is(e, clusterstate.ErrMigration) {
		t.Fatal("old writer reopened", e)
	}
	for i, s := range g.stores {
		must(t, s.Close())
		g.stores[i], e = raftstore.Open(g.paths[i], raftstore.Identity{ClusterUUID: cluster, NodeID: uint64(i + 1), Protocol: 1})
		must(t, e)
		recovered, e := g.stores[i].Recover(ctx)
		must(t, e)
		g.nodes[i], e = raftcore.New(raftcore.Config{ClusterUUID: cluster, NodeID: uint64(i + 1), Protocol: 1, Seed: uint64(i + 99)}, recovered.Core)
		must(t, e)
		g.status[i] = raftcore.Status{}
	}
	for n := 0; n < 40 && g.leader() < 0; n++ {
		for i := range 3 {
			g.drive(i, raftcore.Tick{})
		}
		g.drain()
	}
	if g.leader() < 0 {
		t.Fatal("restart did not elect")
	}
	_, e = clients[0].Acquire(ctx, "chat", directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: a, Fence: 1}})
	if !errors.Is(e, directory.ErrLeaseHeld) {
		t.Fatal("restart lost winning fence", e)
	}
}
