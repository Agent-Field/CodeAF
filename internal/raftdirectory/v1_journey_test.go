package raftdirectory_test

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cell/celltest"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

type v1Local struct {
	assignmentLocal
	head string
}

func (l v1Local) Unpublished(cell.Cell, string) (string, uint32, error) {
	if l.head != "" {
		return l.head, 1, nil
	}
	return "", 0, nil
}

// R1/R2/R23/R25/R29 use two saved homes and ordinary Sync clients against a
// loopback relay with three FULL stores. Taker/Fetcher/Publisher are real; the
// deterministic frame engine keeps this protocol journey bounded and keyless.
func TestR1R2R23R25R29V1TwoHomesContinueTakeBackQuitOffRevoke(t *testing.T) {
	celltest.Available(t)
	g := newGroup(t, time.Now)
	d := &raftdirectory.V1Directory{Host: g, Cluster: cluster, Now: time.Now}
	disk, e := blobstore.NewDisk(t.TempDir())
	must(t, e)
	var hits atomic.Int64
	auth := func(r *http.Request, raw []byte) (string, string, error) {
		id, actor, e := reqsign.Verify(r, raw, time.Now())
		if e != nil {
			return "", "", e
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		i, app, e := g.barrier("a")
		if e != nil {
			return "", "", e
		}
		k := raftstore.Key{Namespace: raftstore.State, Key: cs.DeviceKey(actor).Key}
		rows, _, e := g.stores[i].Read(ctx, app.Applied, []raftstore.Key{k})
		if e != nil {
			return "", "", e
		}
		if rows[k] != nil {
			var device cs.Device
			if e = cs.Decode(rows[k], &device); e != nil {
				return "", "", e
			}
			if device.Revoked {
				return "", "", wireauth.ErrRevoked
			}
		}
		return id, actor, nil
	}
	mux := http.NewServeMux()
	mux.Handle("/v1/dir/", directory.Handler(reqsign.AuthenticateAt(time.Now), func(string) (directory.Directory, error) { return d, nil }))
	mux.Handle("/v1/store/", blobstore.Handler(auth, func(string) (blobstore.Store, error) { return disk, nil }))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); mux.ServeHTTP(w, r) }))
	defer srv.Close()
	runV1TwoHomeJourney(t, srv.URL, &hits)
}

// R1/R2/R23/R25/R29 repeat the same real handoff, files, quit, off and
// revocation assertions through a local Worker's public HTTP door.
func TestR1R2R23R25R29HostedTwoHomes(t *testing.T) {
	endpoint := os.Getenv("CODEAF_HOSTED_PROOF_URL")
	if endpoint == "" {
		t.Skip("local hosted proof runner only")
	}
	target, err := url.Parse(endpoint)
	if err != nil || target.Scheme != "http" || target.Hostname() != "127.0.0.1" {
		t.Fatal("hosted proof requires loopback HTTP")
	}
	celltest.Available(t)
	proxy := httputil.NewSingleHostReverseProxy(target)
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); proxy.ServeHTTP(w, r) }))
	defer server.Close()
	runV1TwoHomeJourney(t, server.URL, &hits)
}

func runV1TwoHomeJourney(t *testing.T, endpoint string, hits *atomic.Int64) {
	t.Helper()
	t.Setenv(syncsetup.URLVar, endpoint)
	id, e := identity.Unmarshal([]byte(`{"V":1,"signing_seed":"` + hex.EncodeToString(key("root").Seed()) + `","dedup_secret":"` + hex.EncodeToString(key("dedup").Seed()) + `","cell_key":"` + hex.EncodeToString(key("content").Seed()) + `"}`))
	must(t, e)
	open := func() *syncsetup.Sync {
		h := t.TempDir()
		must(t, identity.Save(h, id))
		_, e := identity.Device(h)
		must(t, e)
		must(t, identity.EndSolo(h))
		s, ok, e := syncsetup.Open(h)
		must(t, e)
		if !ok {
			t.Fatal("sync unavailable")
		}
		return s
	}
	a, b := open(), open()
	must(t, a.Dir.PutDevice(ctx, a.Device.ID(), directory.Device{V: 1}))
	must(t, b.Dir.PutDevice(ctx, b.Device.ID(), directory.Device{V: 1}))
	engA, engB := cellsync.NewFakeEngine(filepath.Join(a.Home, "frames")), cellsync.NewFakeEngine(filepath.Join(b.Home, "frames"))
	c, e := cell.CreateIn(filepath.Join(a.Home, "chats"), cell.Options{Class: cell.FilesOnly})
	must(t, e)
	meta, e := os.ReadFile(filepath.Join(c.Root, cell.MetaPath))
	must(t, e)
	files := map[string]string{cell.MetaPath: string(meta), "work.txt": "first files", "transcript.jsonl": "first saved answer\n"}
	head := engA.Seal(c, files)
	pubA := &cellsync.Publisher{Engine: engA, Store: a.Store, Dir: a.Dir}
	_, e = pubA.Upload(ctx, c, head)
	must(t, e)
	v, e := a.Dir.Create(ctx, c.ID, directory.CellInit{Head: head})
	must(t, e)
	seen := directory.LeaseExpectation{Device: v.Cell.Lease.Device, Fence: v.Cell.Lease.Fence}
	taker := func(s *syncsetup.Sync, eng *cellsync.FakeEngine, root string, local handoff.Local) handoff.Taker {
		return handoff.Taker{Dir: s.Dir, Fetch: &cellsync.Fetcher{Engine: eng, Store: s.Store, Inbox: func(c cell.Cell) string { return filepath.Join(s.Home, "inbox", c.ID) }}, Local: local, RootFor: func(string) string { return root }}
	}
	takeB := taker(b, engB, filepath.Join(b.Home, "chat"), v1Local{})
	got, e := takeB.TakeSeen(ctx, c.ID, &seen)
	must(t, e)
	if _, e = takeB.TakeSeen(ctx, c.ID, &seen); !errors.Is(e, directory.ErrLeaseHeld) {
		t.Fatalf("observed loser %v", e)
	}
	if _, e = a.Dir.Publish(ctx, c.ID, directory.Publish{Fence: 1, OldHead: "wrong", Head: "stale"}); !errors.Is(e, directory.ErrFenceStale) {
		t.Fatalf("stale publisher %v", e)
	}
	raw, e := os.ReadFile(filepath.Join(got.Cell.Root, "work.txt"))
	must(t, e)
	if string(raw) != "first files" {
		t.Fatal(string(raw))
	}
	files["work.txt"] = "B saved result"
	next := engB.Seal(got.Cell, files)
	pubB := &cellsync.Publisher{Engine: engB, Store: b.Store, Dir: b.Dir}
	must(t, pubB.Publish(ctx, &got.Driving, next, cellsync.PublishInfo{}))
	files["work.txt"] = "A unpublished work"
	unsent := engA.Seal(c, files)
	back := taker(a, engA, c.Root, v1Local{head: unsent})
	branch := cellsync.Brancher{Dir: a.Dir, Publisher: pubA, NewID: func() (string, error) { return "retained-branch", nil }}
	back.Branch = func(ctx context.Context, from *cellsync.Driving, head string, turns uint32) (string, error) {
		return branch.Branch(ctx, from, head, turns, cellsync.PublishInfo{})
	}
	returned, e := back.Take(ctx, c.ID)
	must(t, e)
	if returned.Kept != "retained-branch" {
		t.Fatal("unsent work not retained")
	}
	raw, e = os.ReadFile(filepath.Join(c.Root, "work.txt"))
	must(t, e)
	if string(raw) != "B saved result" {
		t.Fatal("take-back lost B result")
	}
	branchView, e := a.Dir.Cell(ctx, "retained-branch")
	must(t, e)
	if branchView.Cell.Head != unsent || branchView.Cell.ParentCell != c.ID {
		t.Fatal("branch missing")
	}
	files["work.txt"] = "final quit files"
	final := engA.Seal(c, files)
	batch := &cellsync.Batcher{Publisher: pubA, Driving: &returned.Driving}
	batch.Note(cellstore.Turn{ID: final})
	must(t, batch.Close(ctx))
	v, e = b.Dir.Cell(ctx, c.ID)
	must(t, e)
	if v.Cell.Head != final || v.Cell.Lease.Expires != 0 || v.Cell.Lease.ReleasedAt == 0 {
		t.Fatal("quit did not publish before release", v)
	}
	got, e = takeB.Take(ctx, c.ID)
	must(t, e)
	raw, e = os.ReadFile(filepath.Join(got.Cell.Root, "work.txt"))
	must(t, e)
	if string(raw) != "final quit files" {
		t.Fatal("quit files missing")
	}
	must(t, a.Dir.Revoke(ctx, b.Device.ID()))
	if _, e = b.Dir.Cell(ctx, c.ID); !errors.Is(e, directory.ErrRevoked) {
		t.Fatalf("revoked request %v", e)
	}
	before := hits.Load()
	t.Setenv(syncsetup.URLVar, "off")
	_, _ = a.Dir.List(ctx)
	_, _ = a.Store.Has(ctx, []string{head})
	if hits.Load() != before {
		t.Fatal("retained clients sent after off")
	}
	// The saved homes and their transcript/workspace/branch bytes still exist.
	if _, e = os.Stat(filepath.Join(c.Root, "work.txt")); e != nil {
		t.Fatal(e)
	}
}
