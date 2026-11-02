package directory

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftstore"
)

func legacyConfig() clusterstate.LegacyConfig {
	root := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, 32)).Public().(ed25519.PublicKey)
	effect := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{8}, 32)).Public().(ed25519.PublicKey)
	return clusterstate.LegacyConfig{Cluster: "r24-one-uuid", Generation: 1, Identity: clusterstate.IdentityID(root), SigningKey: root, EffectsKey: effect, Anchors: []uint64{1, 2, 3}, Now: 200000}
}
func legacyMust(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func legacySeed(t *testing.T, path string) (*SQLite, Cell) {
	t.Helper()
	s, e := OpenSQLite(path, func() time.Time { return time.UnixMilli(1000) })
	legacyMust(t, e)
	legacyMust(t, s.For("a").PutDevice(context.Background(), "b", Device{V: 1, Name: "c2VhbGVk", Revoked: true, Caps: Caps{OS: "linux"}}))
	legacyMust(t, s.For("a").SetVault(context.Background(), "", "opaque-vault"))
	v, e := s.For("a").Create(context.Background(), "chat", CellInit{Head: "opaque-head", Frames: []string{"opaque-frame"}, Title: "c2VhbGVk", ParentCell: "parent", Keys: map[string]map[string]string{"key": {"identity": "wrapped"}}, Size: 71})
	legacyMust(t, e)
	return s, v.Cell
}

// R24/R28 compares the full retained application, not List's stripped frame
// hints. Both source handles and every future opener remain fenced forever.
func TestR24R28FrozenExportSameGenesisAndSourceRetention(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "source.db")
	s, original := legacySeed(t, path)
	defer s.Close()
	other, e := OpenSQLite(path, s.clock)
	legacyMust(t, e)
	defer other.Close()
	sub, e := s.Feed().Subscribe("a", []Hold{{Cell: "chat", Fence: original.Lease.Fence}})
	legacyMust(t, e)
	defer sub.Close()
	// Sampling current evidence preserves a held lease after its SQL expiry.
	s.clock = func() time.Time { return time.UnixMilli(200000) }
	s.feed.clock = s.clock
	sub.Ping()
	legacyMust(t, s.FreezeLegacy(ctx, legacyConfig()))
	if _, e = s.feed.Subscribe("a", nil); e == nil {
		t.Fatal("frozen feed accepted a watch")
	}
	if _, e = other.For("a").Acquire(ctx, "chat", AcquireOpts{Force: true}); !errors.Is(e, clusterstate.ErrMigration) {
		t.Fatal(e)
	}
	if _, e = s.For("a").List(ctx); !errors.Is(e, clusterstate.ErrMigration) {
		t.Fatal(e)
	}
	m, e := s.InventoryLegacy(ctx)
	legacyMust(t, e)
	if e = s.OwnLegacy(ctx, []LegacyReceipt{{Node: 1, Manifest: m, Applied: 1}}); e == nil {
		t.Fatal("one anchor published ownership")
	}
	var genesis []byte
	for i := uint64(1); i <= 3; i++ {
		target, e := raftstore.Open(filepath.Join(t.TempDir(), "anchor.db"), raftstore.Identity{ClusterUUID: m.Config.Cluster, NodeID: i, Protocol: 1})
		legacyMust(t, e)
		stream := func(visit func(clusterstate.RecordWrite) error) error {
			got, e := s.ExportLegacy(ctx, visit)
			if e == nil && got.Digest != m.Digest {
				return fmt.Errorf("source changed")
			}
			return e
		}
		legacyMust(t, target.ImportLegacy(ctx, m, stream))
		legacyMust(t, target.ImportLegacy(ctx, m, func(func(clusterstate.RecordWrite) error) error { t.Fatal("retry reread source"); return nil }))
		r, e := target.Recover(ctx)
		legacyMust(t, e)
		if !bytes.Equal(genesis, r.Core.Snapshot.Data) && genesis != nil {
			t.Fatal("anchors have different genesis")
		}
		genesis = bytes.Clone(r.Core.Snapshot.Data)
		if len(r.Application.ConfState.Voters) != 3 || r.Application.VisibleVersion != m.Version {
			t.Fatal("lost voters/version")
		}
		k := clusterstate.CellKey("chat")
		rows, _, e := target.Read(ctx, r.Application.Applied, []raftstore.Key{{Namespace: raftstore.State, Key: k.Key}})
		legacyMust(t, e)
		var got Cell
		legacyMust(t, clusterstate.Decode(rows[raftstore.Key{Namespace: raftstore.State, Key: k.Key}], &got))
		want := original
		want.Lease.Expires = 290000
		a, _ := json.Marshal(got)
		b, _ := json.Marshal(want)
		if !bytes.Equal(a, b) {
			t.Fatalf("lost cell fields: %s", a)
		}
		legacyMust(t, target.Close())
	}
	legacyMust(t, s.OwnLegacy(ctx, []LegacyReceipt{{Node: 1, Manifest: m, Applied: 2}, {Node: 2, Manifest: m, Applied: 2}}))
	var raw string
	legacyMust(t, s.db.QueryRow(`SELECT rec FROM cells WHERE id='chat'`).Scan(&raw))
	var retained Cell
	legacyMust(t, json.Unmarshal([]byte(raw), &retained))
	if retained.Lease.Expires != original.Lease.Expires || len(retained.Frames) != 1 {
		t.Fatal("original changed")
	}
	g, e := s.LegacyGate(ctx)
	legacyMust(t, e)
	if g.Stage != "raft-owned" {
		t.Fatal(g)
	}
	wrong := legacyConfig()
	wrong.Cluster = "different"
	if s.FreezeLegacy(ctx, wrong) == nil {
		t.Fatal("allocated a second UUID")
	}
}

// R24 inventories an identity above the default ceiling without holding the
// whole export in memory, and refuses an unsupported row without truncation.
func TestR24OversizeRowsRotationAndConcurrentOldWrites(t *testing.T) {
	ctx := context.Background()
	s, _ := legacySeed(t, filepath.Join(t.TempDir(), "source.db"))
	defer s.Close()
	payload := strings.Repeat("s", 300000)
	for i := 0; i < 30; i++ {
		_, e := s.For("a").Create(ctx, fmt.Sprint(i), CellInit{Head: "head", Title: payload, Frames: []string{"frame"}})
		legacyMust(t, e)
	}
	_, e := s.For("a").Rotate(ctx, RotationReq{Op: OpFreeze})
	legacyMust(t, e)
	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan struct{})
	go func() {
		defer wg.Done()
		defer close(done)
		for range 20 {
			_ = s.seenForTest("b")
		}
	}()
	legacyMust(t, s.FreezeLegacy(ctx, legacyConfig()))
	wg.Wait()
	<-done
	m, e := s.InventoryLegacy(ctx)
	legacyMust(t, e)
	if m.Ceiling <= clusterstate.DefaultApplicationBytes || m.Bytes < 9000000 {
		t.Fatal("oversized inventory stranded", m.Bytes)
	}
	target, e := raftstore.Open(filepath.Join(t.TempDir(), "anchor.db"), raftstore.Identity{ClusterUUID: m.Config.Cluster, NodeID: 1, Protocol: 1})
	legacyMust(t, e)
	defer target.Close()
	legacyMust(t, target.ImportLegacy(ctx, m, func(fn func(clusterstate.RecordWrite) error) error { _, e := s.ExportLegacy(ctx, fn); return e }))
	r, e := target.Recover(ctx)
	legacyMust(t, e)
	if r.Application.RecordCount != m.Rows+1 {
		t.Fatal("rows lost")
	}
	var id IdentityRec
	key := raftstore.Key{Namespace: raftstore.State, Key: "identity"}
	rows, _, e := target.Read(ctx, 1, []raftstore.Key{key})
	legacyMust(t, e)
	legacyMust(t, clusterstate.Decode(rows[key], &id))
	if id.Rotation == nil || id.Vault != "opaque-vault" || !bytes.Equal(id.SigningKey, m.Config.SigningKey) {
		t.Fatal("lost identity state")
	}
	bad, _ := legacySeed(t, filepath.Join(t.TempDir(), "bad.db"))
	defer bad.Close()
	raw, _ := json.Marshal(Cell{V: 1, Title: strings.Repeat("x", clusterstate.MaxEvaluationBytes+1)})
	_, e = bad.db.Exec(`UPDATE cells SET rec=? WHERE id='chat'`, string(raw))
	legacyMust(t, e)
	legacyMust(t, bad.FreezeLegacy(ctx, legacyConfig()))
	if _, e = bad.InventoryLegacy(ctx); e == nil || !strings.Contains(e.Error(), `cells record "chat"`) {
		t.Fatal("oversized row was accepted or its location was not reported", e)
	}
	var kept string
	legacyMust(t, bad.db.QueryRow(`SELECT rec FROM cells WHERE id='chat'`).Scan(&kept))
	if kept != string(raw) {
		t.Fatal("oversize source truncated")
	}
	if _, e = bad.For("a").List(ctx); !errors.Is(e, clusterstate.ErrMigration) {
		t.Fatal("failed import reopened source", e)
	}
}
func (s *SQLite) seenForTest(id string) error {
	return s.For("a").PutDevice(context.Background(), id, Device{V: 1})
}

// R24 permits a write admitted before the gate to finish, includes it exactly,
// and refuses a request that was authenticated earlier but entered SQL later.
func TestR24FreezeWaitsForAnAdmittedLegacyWrite(t *testing.T) {
	ctx := context.Background()
	s, _ := legacySeed(t, filepath.Join(t.TempDir(), "source.db"))
	defer s.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	writeDone := make(chan error, 1)
	go func() {
		writeDone <- s.inTx(ctx, func(tx *sql.Tx) error {
			close(entered)
			<-release
			var id IdentityRec
			if e := get(tx, "identity", "1", &id); e != nil {
				return e
			}
			id.Vault = "last-admitted-vault"
			return put(tx, s.feed, "identity", "1", id, 1000)
		})
	}()
	<-entered
	started := make(chan struct{})
	frozen := make(chan error, 1)
	go func() { close(started); frozen <- s.FreezeLegacy(ctx, legacyConfig()) }()
	<-started
	select {
	case e := <-frozen:
		t.Fatalf("freeze passed an open write: %v", e)
	default:
	}
	close(release)
	legacyMust(t, <-writeDone)
	legacyMust(t, <-frozen)
	if e := s.For("a").SetVault(ctx, "last-admitted-vault", "too-late"); !errors.Is(e, clusterstate.ErrMigration) {
		t.Fatal("delayed old request changed source", e)
	}
	_, e := s.InventoryLegacy(ctx)
	legacyMust(t, e)
	found := false
	_, e = s.ExportLegacy(ctx, func(w clusterstate.RecordWrite) error {
		if w.RecordKey == clusterstate.IdentityKey() {
			var id IdentityRec
			if e := clusterstate.Decode(w.Data, &id); e != nil {
				return e
			}
			found = id.Vault == "last-admitted-vault"
		}
		return nil
	})
	legacyMust(t, e)
	if !found {
		t.Fatal("admitted write disappeared")
	}
}

// R24/C1 drains a legacy approval through its public decision before taking
// ownership. The sealed grant stays readable at its original destination.
func TestR24FreezeDrainsAnAdmittedPairingDecision(t *testing.T) {
	ctx := context.Background()
	s, _ := legacySeed(t, filepath.Join(t.TempDir(), "source.db"))
	defer s.Close()
	links := NewLinks(s.clock, DefaultLinkLimits)
	s.SetLinks(links)
	public := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{9}, 32)).Public().(ed25519.PublicKey)
	id := clusterstate.DeviceIDOfKey(public)
	code := "k7m2q9xd"
	links.reqs[code] = &entry{rec: Request{Code: code, Device: id, State: RequestPending, ExpiresAt: 1000000}, until: 1000000, done: make(chan struct{})}
	cert := struct {
		V      uint16 `json:"V"`
		Device string `json:"device"`
		Made   int64  `json:"created"`
		Sig    string `json:"sig"`
	}{V: 1, Device: hex.EncodeToString(public), Made: 1000}
	body, e := json.Marshal(cert)
	legacyMust(t, e)
	cert.Sig = hex.EncodeToString(ed25519.Sign(ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, 32)), append([]byte("codeaf/device-cert/v1\n"), body...)))
	body, e = json.Marshal(cert)
	legacyMust(t, e)
	approval := Approval{Device: Device{V: 1, Name: "c2VhbGVk"}, Grant: "c2VhbGVk", Cert: base64.RawURLEncoding.EncodeToString(body)}
	entered, release := make(chan struct{}), make(chan struct{})
	s.SetOnJoin(func(Joined) { close(entered); <-release })
	approved := make(chan error, 1)
	go func() { approved <- s.For("a").ApproveRequest(ctx, code, approval) }()
	<-entered
	started := make(chan struct{})
	frozen := make(chan error, 1)
	go func() { close(started); frozen <- s.FreezeLegacy(ctx, legacyConfig()) }()
	<-started
	select {
	case e := <-frozen:
		t.Fatalf("freeze passed an undecided grant: %v", e)
	default:
	}
	close(release)
	legacyMust(t, <-approved)
	legacyMust(t, <-frozen)
	if links.reqs[code].rec.State != RequestApproved || links.reqs[code].rec.Grant == nil || *links.reqs[code].rec.Grant != approval.Grant {
		t.Fatal("admitted sealed grant lost")
	}
	_, e = s.InventoryLegacy(ctx)
	legacyMust(t, e)
	found := false
	_, e = s.ExportLegacy(ctx, func(w clusterstate.RecordWrite) error {
		found = found || w.RecordKey == clusterstate.DeviceKey(id)
		return nil
	})
	legacyMust(t, e)
	if !found {
		t.Fatal("granted device missing from import")
	}
	if e = s.For("a").ApproveRequest(ctx, code, approval); !errors.Is(e, clusterstate.ErrMigration) {
		t.Fatal("old duplicate decision bypassed source gate", e)
	}
}
