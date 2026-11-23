package cellsync

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

type frameHintSigner struct {
	identity identity.Identity
	device   identity.Dev
}

func (s frameHintSigner) IdentityKey() ed25519.PublicKey { return s.identity.PublicKey() }
func (s frameHintSigner) Cert() identity.Cert            { return s.device.Cert }
func (s frameHintSigner) Sign(raw []byte) []byte         { return s.device.Sign(raw) }

type frameHintReads struct {
	blobstore.Store
	bounded int
}

func (s *frameHintReads) GetFrameBounded(ctx context.Context, id string, limit int64) ([]byte, error) {
	s.bounded++
	return s.Store.(blobstore.BoundedFrameStore).GetFrameBounded(ctx, id, limit)
}

func TestFrameHintsWarmAndPlainFetchThroughNativeRaftRelay(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	storage := t.TempDir()
	svc := relayserve.New(relayserve.Config{Store: storage})
	defer svc.Close()
	server := httptest.NewServer(svc.Handler)
	defer server.Close()
	home := t.TempDir()
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	device, err := identity.Device(home)
	if err != nil {
		t.Fatal(err)
	}
	sign := reqsign.SignFor(frameHintSigner{identity: id, device: device}, time.Now)
	dir := directory.NewHTTP(server.URL, sign, server.Client())
	store := blobstore.NewHTTP(server.URL, sign, server.Client())
	if err := dir.PutDevice(ctx, device.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	r := newRig(t)
	r.pub.Dir, r.pub.Store, r.pub.WarmFrameHints = dir, store, true
	first := r.publishFirst(map[string]string{"a": "first"})
	warmReads, plainReads := &frameHintReads{Store: store}, &frameHintReads{Store: store}
	warm, warmCell := fetcherFor(t, warmReads)
	plain, plainCell := fetcherFor(t, plainReads)
	warm.Dir, plain.Dir = dir, dir
	warm.WarmFrameHints = true
	if err := warm.Fetch(ctx, warmCell, first); err != nil {
		t.Fatal(err)
	}
	if err := plain.Fetch(ctx, plainCell, first); err != nil {
		t.Fatal(err)
	}
	next := r.seal(map[string]string{"a": "second", "b": "new output"})
	if err := r.pub.Publish(ctx, r.drv, next, r.info()); err != nil {
		t.Fatal(err)
	}
	view, err := dir.Cell(ctx, cellID)
	if err != nil || len(view.Cell.FrameHints) == 0 {
		t.Fatalf("Raft publish lost hints: %+v, %v", view, err)
	}
	listing, err := dir.List(ctx)
	if err != nil || len(listing.Cells[cellID].FrameHints) != 0 || len(listing.Cells[cellID].Frames) != 0 {
		t.Fatalf("listing carried transfer metadata: %+v, %v", listing, err)
	}
	for _, hint := range view.Cell.FrameHints {
		raw, err := store.GetFrame(ctx, hint.ID)
		if err != nil || int64(len(raw)) != hint.Bytes {
			t.Fatalf("hint does not describe uploaded frame: %v", err)
		}
	}
	if err := warm.Fetch(ctx, warmCell, next); err != nil {
		t.Fatal(err)
	}
	if warmReads.bounded != len(view.Cell.FrameHints) {
		t.Fatalf("warm fetch did not prime hints: %d reads", warmReads.bounded)
	}
	if err := plain.Fetch(ctx, plainCell, next); err != nil {
		t.Fatal(err)
	}
	if plainReads.bounded != 0 {
		t.Fatal("plain fetch used optional hints")
	}
	for _, root := range []string{warmCell.Root, plainCell.Root} {
		for name, want := range map[string]string{"a": "second", "b": "new output"} {
			got, err := os.ReadFile(filepath.Join(root, name))
			if err != nil || string(got) != want {
				t.Fatalf("fetch materialization %s = %q, %v", name, got, err)
			}
		}
	}
	var provision struct{ Cluster string }
	raw, err := os.ReadFile(filepath.Join(storage, "raft", id.ID(), "provision.json"))
	if err != nil {
		t.Fatalf("native Raft group was not provisioned: %v", err)
	}
	if err := json.Unmarshal(raw, &provision); err != nil {
		t.Fatal(err)
	}
	for node := uint64(1); node <= 3; node++ {
		anchor, err := raftstore.Open(filepath.Join(storage, "raft", id.ID(), string(rune('0'+node))+".db"), raftstore.Identity{ClusterUUID: provision.Cluster, NodeID: node, Protocol: cs.ClusterProtocol})
		if err != nil {
			t.Fatal(err)
		}
		func() {
			defer anchor.Close()
			app, _, err := anchor.ApplicationView(ctx)
			if err != nil {
				t.Fatal(err)
			}
			key := raftstore.Key{Namespace: raftstore.State, Key: cs.CellKey(cellID).Key}
			rows, _, err := anchor.Read(ctx, app.Applied, []raftstore.Key{key})
			if err != nil {
				t.Fatal(err)
			}
			var committed cs.Cell
			if err := cs.Decode(rows[key], &committed); err != nil {
				t.Fatal(err)
			}
			if app.Applied <= 1 || committed.Head != next || !reflect.DeepEqual(committed.FrameHints, view.Cell.FrameHints) {
				t.Fatalf("anchor %d did not apply hinted head: %+v", node, committed)
			}
		}()
	}
}
