package relayserve_test

import (
	"bytes"
	"crypto/ed25519"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
)

// R3/R24/R28 prove the real first-use relay path imports a preexisting source,
// preserves its exact original bytes, and resumes the SAME three stores after
// restart. Neither API hints nor a taught legacy refusal can satisfy this test.
func TestR3R24R28NativeProductionMigratesAndRestartsAllAnchors(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	r.stop()
	path := filepath.Join(r.store, "directory", id.ID()+".db")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	source, err := directory.OpenSQLite(path, r.clock.Now)
	if err != nil {
		t.Fatal(err)
	}
	client := source.For(a.dev.ID())
	if err = client.PutDevice(bg, a.dev.ID(), directory.Device{V: 1, Name: "c2VhbGVk"}); err != nil {
		t.Fatal(err)
	}
	if _, err = client.Create(bg, cell, directory.CellInit{Head: head1, Frames: []string{"old-sealed-frame"}}); err != nil {
		t.Fatal(err)
	}
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}
	original := func() string {
		t.Helper()
		db, e := sql.Open("sqlite", "file:"+path+"?mode=ro")
		if e != nil {
			t.Fatal(e)
		}
		defer db.Close()
		var raw string
		if e = db.QueryRow("SELECT rec FROM cells WHERE id=?", cell).Scan(&raw); e != nil {
			t.Fatal(e)
		}
		return raw
	}
	before := original()
	r.start()
	// R22/R24: the first use of an old held chat may be a signed watch,
	// before a returning device sends any metadata or ordinary directory call.
	watch, _, err := directorytest.DialWatchHolding(bg, r.srv.URL, r.signer(a), []directory.Hold{{Cell: cell, Fence: 1}})
	if err != nil {
		t.Fatal(err)
	}
	defer watch.CloseNow()
	if _, _, err = watch.Read(bg); err != nil {
		t.Fatal(err)
	}
	got, err := r.dir(a).Cell(bg, cell)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cell.Head != head1 || got.Cell.Lease.Fence != 1 || len(got.Cell.Frames) != 1 {
		t.Fatal(got)
	}
	provisionPath := filepath.Join(r.store, "raft", id.ID(), "provision.json")
	provision, err := os.ReadFile(provisionPath)
	if err != nil {
		t.Fatal(err)
	}
	// At each externally observed successful write, all three local voters have
	// applied the same state through independent FULL-synchronous databases.
	check := func(want uint64) {
		t.Helper()
		var p struct{ Cluster string }
		if json.Unmarshal(provision, &p) != nil {
			t.Fatal("provision")
		}
		for i := uint64(1); i <= 3; i++ {
			s, e := raftstore.Open(filepath.Join(r.store, "raft", id.ID(), string(rune('0'+i))+".db"), raftstore.Identity{ClusterUUID: p.Cluster, NodeID: i, Protocol: 1})
			if e != nil {
				t.Fatal(e)
			}
			app, _, e := s.ApplicationView(bg)
			if e != nil {
				t.Fatal(e)
			}
			key := raftstore.Key{Namespace: raftstore.State, Key: clusterstate.CellKey(cell).Key}
			rows, _, e := s.Read(bg, app.Applied, []raftstore.Key{key})
			if e != nil {
				t.Fatal(e)
			}
			var c directory.Cell
			if e = clusterstate.Decode(rows[key], &c); e != nil {
				t.Fatal(e)
			}
			if c.Lease.Fence != want || c.Head != head1 || app.Applied <= 1 {
				t.Fatalf("anchor %d lost state: %+v", i, c)
			}
			if e = s.Close(); e != nil {
				t.Fatal(e)
			}
		}
	}
	check(1)
	b := newDevice(t, id, t.TempDir())
	expect := directory.LeaseExpectation{Device: a.dev.ID(), Fence: 1}
	if _, err = r.dir(b).Acquire(bg, cell, directory.AcquireOpts{Force: true, Expect: &expect}); err != nil {
		t.Fatal(err)
	}
	check(2)
	r.restart()
	if _, err = r.dir(a).Acquire(bg, cell, directory.AcquireOpts{Force: true, Expect: &expect}); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatal(err)
	}
	check(2)
	after, e := os.ReadFile(provisionPath)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(provision, after) || original() != before {
		t.Fatal("migration/restart rewrote its source or provision")
	}
}

// R24/R28 require incomplete migration to refuse rather than silently reset a
// damaged provision or resume the old writer. Restoring the original bytes
// resumes the same group without changing saved ownership.
func TestR24R28NativeProductionRefusesDamagedProvisionAndResumes(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	if _, err := r.dir(a).Create(bg, cell, directory.CellInit{Head: head1}); err != nil {
		t.Fatal(err)
	}
	r.stop()
	path := filepath.Join(r.store, "raft", id.ID(), "provision.json")
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	r.start()
	if _, err = r.dir(a).Cell(bg, cell); !errors.Is(err, directory.ErrUnreachable) {
		t.Fatalf("damaged authoritative provision must be retryable: %v", err)
	}
	r.stop()
	// R24 also refuses a different, internally valid anchor key. Its public
	// bytes are anchored in the SAME durable source manifest, not just JSON length.
	var provision map[string]json.RawMessage
	if err = json.Unmarshal(original, &provision); err != nil {
		t.Fatal(err)
	}
	var anchors [][]byte
	if err = json.Unmarshal(provision["anchors"], &anchors); err != nil {
		t.Fatal(err)
	}
	anchors[0] = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	provision["anchors"], err = json.Marshal(anchors)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := json.Marshal(provision)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, changed, 0600); err != nil {
		t.Fatal(err)
	}
	r.start()
	if _, err = r.dir(a).Cell(bg, cell); !errors.Is(err, directory.ErrUnreachable) {
		t.Fatal("source manifest accepted changed anchor authority", err)
	}
	r.stop()
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	r.start()
	got, err := r.dir(a).Cell(bg, cell)
	if err != nil || got.Cell.Head != head1 || got.Cell.Lease.Fence != 1 {
		t.Fatal(got, err)
	}
}

// R22/R30 preserve ordinary directory requests while a real watch is open.
func TestR22R30NativeWatchDoesNotChangeRequestAuthority(t *testing.T) {
	r := newRig(t)
	_, a := newIdentity(t)
	conn, _, err := directorytest.DialWatch(bg, r.srv.URL, r.signer(a))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if _, _, err = conn.Read(bg); err != nil {
		t.Fatal(err)
	}
	if err = r.dir(a).PutDevice(bg, a.dev.ID(), directory.Device{V: 1, Name: "sealed"}); err != nil {
		t.Fatalf("put after watch: %v; safe request log=%v", err, r.logs)
	}
}

// R3/R8/R24 prohibit re-importing a wiped published voter under its old ID.
// Two actual surviving stores coordinate; one cannot recreate a majority.
func TestR3R8R24NativeProductionUsesOnlySurvivingAnchorStores(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	if _, err := r.dir(a).Create(bg, cell, directory.CellInit{Head: head1}); err != nil {
		t.Fatal(err)
	}
	r.stop()
	first := filepath.Join(r.store, "raft", id.ID(), "1.db")
	second := filepath.Join(r.store, "raft", id.ID(), "2.db")
	if err := os.Rename(first, first+".offline"); err != nil {
		t.Fatal(err)
	}
	r.start()
	b := newDevice(t, id, t.TempDir())
	if _, err := r.dir(b).Acquire(bg, cell, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: a.dev.ID(), Fence: 1}}); err != nil {
		t.Fatal("two surviving anchors could not coordinate", err)
	}
	if _, err := os.Stat(first); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("lost voter was re-imported under its old ID", err)
	}
	r.stop()
	if err := os.Rename(second, second+".offline"); err != nil {
		t.Fatal(err)
	}
	r.start()
	if _, err := r.dir(a).Cell(bg, cell); !errors.Is(err, directory.ErrUnreachable) {
		t.Fatal("one surviving voter must stay unavailable", err)
	}
	for _, path := range []string{first, second} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("minority recreated missing authority", path, err)
		}
	}
	r.stop()
	for _, path := range []string{first, second} {
		if err := os.Rename(path+".offline", path); err != nil {
			t.Fatal(err)
		}
	}
	r.start()
	v, err := r.dir(a).Cell(bg, cell)
	if err != nil || v.Cell.Lease.Fence != 2 || v.Cell.Lease.Device != b.dev.ID() {
		source, e := directory.OpenSQLite(filepath.Join(r.store, "directory", id.ID()+".db"), r.clock.Now)
		if e == nil {
			gate, e := source.LegacyGate(bg)
			if e == nil && gate != nil && gate.Manifest != nil {
				host, e := raftdirectory.OpenNative(bg, filepath.Join(r.store, "raft", id.ID()), source, *gate.Manifest)
				t.Logf("R3/R8/R24 recovery cause: %v", e)
				if host != nil {
					host.Close()
				}
			}
			source.Close()
		}
		t.Fatal("returning actual stores lost committed ownership", v, err)
	}
}

// R3/R8/R24 require a published voter to retain its own import provenance.
// A fresh database with the old identity is not a returning durable member.
func TestR3R8R24NativeProductionRefusesUnclaimedReplacement(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	if _, err := r.dir(a).Create(bg, cell, directory.CellInit{Head: head1}); err != nil {
		t.Fatal(err)
	}
	r.stop()
	base := filepath.Join(r.store, "raft", id.ID())
	raw, err := os.ReadFile(filepath.Join(base, "provision.json"))
	if err != nil {
		t.Fatal(err)
	}
	var provision struct{ Cluster string }
	if err = json.Unmarshal(raw, &provision); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "1.db")
	if err = os.Rename(path, path+".actual"); err != nil {
		t.Fatal(err)
	}
	member := raftstore.Identity{ClusterUUID: provision.Cluster, NodeID: 1, Protocol: 1}
	fresh, err := raftstore.Open(path, member)
	if err != nil {
		t.Fatal(err)
	}
	if err = fresh.Close(); err != nil {
		t.Fatal(err)
	}
	r.start()
	b := newDevice(t, id, t.TempDir())
	if _, err = r.dir(b).Acquire(bg, cell, directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: a.dev.ID(), Fence: 1}}); err != nil {
		t.Fatal("actual majority must remain available", err)
	}
	r.stop()
	fresh, err = raftstore.Open(path, member)
	if err != nil {
		t.Fatal(err)
	}
	imported, err := fresh.ImportedLegacy(bg)
	if err != nil {
		t.Fatal(err)
	}
	app, _, err := fresh.ApplicationView(bg)
	if err != nil {
		t.Fatal(err)
	}
	if err = fresh.Close(); err != nil {
		t.Fatal(err)
	}
	if imported != nil || app.Applied != 0 {
		t.Fatal("published identity was silently re-imported", app.Applied)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = os.Rename(path+".actual", path); err != nil {
		t.Fatal(err)
	}
	r.start()
	view, err := r.dir(a).Cell(bg, cell)
	if err != nil || view.Cell.Lease.Fence != 2 || view.Cell.Lease.Device != b.dev.ID() {
		t.Fatal("actual returning store lost majority work", view, err)
	}
}
