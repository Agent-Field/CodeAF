package raftdirectory_test

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/cellsync"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

type assignmentLocal struct{}

func (assignmentLocal) Dirty(context.Context, cell.Cell) (bool, error)        { return false, nil }
func (assignmentLocal) Unpublished(cell.Cell, string) (string, uint32, error) { return "", 0, nil }
func (assignmentLocal) Seal(context.Context, cell.Cell) (string, uint32, error) {
	return "", 0, errors.New("unexpected seal")
}
func (assignmentLocal) Follow(context.Context, cell.Cell, cell.Cell) error { return nil }

type assignmentDir struct {
	directory.Client
	read    func(context.Context, string) (directory.CellView, error)
	release func(context.Context, string, uint64) error
	publish func(context.Context, string, directory.Publish) (directory.CellView, error)
}

func (d assignmentDir) Cell(ctx context.Context, id string) (directory.CellView, error) {
	return d.read(ctx, id)
}
func (d assignmentDir) Release(ctx context.Context, id string, f uint64) error {
	return d.release(ctx, id, f)
}
func (d assignmentDir) Publish(ctx context.Context, id string, p directory.Publish) (directory.CellView, error) {
	return d.publish(ctx, id, p)
}

// R18–R21 drive two disposable homes through a loopback coordination relay,
// three FULL-synchronous stores, the real Taker, Publisher and Fetcher. The
// tiny scripted worker never receives the identity's private/content keys.
func TestR18R19R20R21ScriptedWorkerHandoff(t *testing.T) {
	now := int64(1000)
	g := newGroup(t, func() time.Time { return time.UnixMilli(now) })
	serial := 0
	signed := func(actor string, k ed25519.PrivateKey, inc uint64, m cs.Mutation) cs.Command {
		serial++
		c, e := cs.Sign(cs.Command{Schema: 1, Cluster: cluster, Actor: actor, WorkerIncarnation: inc, Session: hex.EncodeToString([]byte{byte(serial >> 8), byte(serial)}), Sequence: 1, Now: now, Change: m}, k)
		must(t, e)
		return c
	}
	owner := func(m cs.Mutation) cs.Result {
		r, e := g.submit(signed("a", key("a"), 0, m))
		must(t, e)
		if r.Code != cs.OK {
			t.Fatal(r)
		}
		return r
	}
	workerKey := key("scripted-worker")
	id := cs.DeviceIDOfKey(workerKey.Public().(ed25519.PublicKey))
	cert := cs.WorkerCertificate{V: 1, Identity: cs.IdentityID(key("root").Public().(ed25519.PublicKey)), Cluster: cluster, Key: workerKey.Public().(ed25519.PublicKey), Made: 1000, Expires: 100000, Cells: []string{"child-one", "child-two"}}
	b, e := cs.WorkerCertificateBytes(cert)
	must(t, e)
	cert.Signature = ed25519.Sign(key("root"), b)
	// A local credential contains only this worker's seed and PUBLIC delegation.
	doc := encoded(t, struct {
		V           uint16               `json:"v"`
		Seed        string               `json:"worker_seed"`
		Certificate cs.WorkerCertificate `json:"certificate"`
	}{1, hex.EncodeToString(workerKey.Seed()), cert})
	credential, e := identity.UnmarshalWorker(doc, key("root").Public().(ed25519.PublicKey), cluster, time.UnixMilli(now))
	must(t, e)
	owner(cs.Mutation{EnrollWorker: &cs.EnrollWorker{Certificate: cert, Grants: map[string][]byte{"child-one": {0x81}, "child-two": {0x82}}}})
	r, e := g.submit(signed(id, workerKey, 0, cs.Mutation{RegisterWorker: &cs.RegisterWorker{Incarnation: 1}}))
	must(t, e)
	if r.Code != cs.OK {
		t.Fatal(r)
	}
	beat := cs.WorkerHeartbeat{Worker: id, Incarnation: 1, Sequence: 1, SeenAt: now}
	b, e = cs.WorkerHeartbeatBytes(cluster, beat)
	must(t, e)
	beat.Signature = ed25519.Sign(workerKey, b)
	owner(cs.Mutation{WorkerBeatBatch: &cs.WorkerBeatBatch{Items: []cs.WorkerHeartbeat{beat}}})
	homeA, homeW := t.TempDir(), t.TempDir()
	engA := cellsync.NewFakeEngine(filepath.Join(homeA, "frames"))
	engW := cellsync.NewFakeEngine(filepath.Join(homeW, "frames"))
	store := blobstore.NewMemory()
	template, e := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	must(t, e)
	meta, e := os.ReadFile(filepath.Join(template.Root, cell.MetaPath))
	must(t, e)
	parent := owner(cs.Mutation{Create: &cs.CreateCell{V1: true, Cell: "parent", Init: cs.CellInit{Head: "parent-head"}}})
	var assignments []cs.Assignment
	for _, cellID := range cert.Cells {
		at := cell.Cell{ID: cellID, Root: filepath.Join(homeA, cellID)}
		head := engA.Seal(at, map[string]string{cell.MetaPath: string(meta), "input.txt": "source files"})
		pub := cellsync.Publisher{Engine: engA, Store: store, Dir: directory.NewRaft(g).For("a")}
		_, e = pub.Upload(ctx, at, head)
		must(t, e)
		owner(cs.Mutation{Create: &cs.CreateCell{V1: true, Cell: cellID, Init: cs.CellInit{Head: head, ParentCell: "parent"}}})
		view, e := directory.NewRaft(g).For("a").Cell(ctx, cellID)
		must(t, e)
		// Each parallel task uses a separate existing child and its own lease.
		spec := cs.Assign{ID: "run-" + cellID, Revision: 1, Cell: cellID, ParentCell: "parent", Expect: cs.LeaseExpectation{Device: "a", Fence: view.Cell.Lease.Fence}, Target: id, Incarnation: 1, Expires: 90000, SealedPayload: []byte{0x80}}
		assignments = append(assignments, *owner(cs.Mutation{Assign: &spec}).Assignment)
	}
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(io.LimitReader(r.Body, cs.MaxCommandBytes+1))
		if err != nil {
			http.Error(w, "read", 400)
			return
		}
		c, err := cs.DecodeCommand(raw)
		if err != nil {
			http.Error(w, "command", 400)
			return
		}
		g.mu.Lock()
		result, err := g.submit(c)
		g.mu.Unlock()
		if err != nil {
			http.Error(w, "unavailable", 503)
			return
		}
		w.Write(encoded(t, result))
	}))
	defer relay.Close()
	submit := func(ctx context.Context, c cs.Command) (cs.Result, error) {
		raw, e := cs.EncodeCommand(c)
		if e != nil {
			return cs.Result{}, e
		}
		req, e := http.NewRequestWithContext(ctx, http.MethodPost, relay.URL, bytes.NewReader(raw))
		if e != nil {
			return cs.Result{}, e
		}
		res, e := relay.Client().Do(req)
		if e != nil {
			return cs.Result{}, e
		}
		defer res.Body.Close()
		b, e := io.ReadAll(res.Body)
		var out cs.Result
		if e == nil {
			e = cs.Decode(b, &out)
		}
		return out, e
	}
	current := func(ctx context.Context, ref cs.AssignmentRef) (cs.Assignment, directory.CellView, error) {
		g.mu.Lock()
		defer g.mu.Unlock()
		i, _, e := g.barrier("a")
		if e != nil {
			return cs.Assignment{}, cs.CellView{}, e
		}
		return raftdirectory.AuthorizedWorkerAssignment(ctx, g.stores[i], cluster, id, 1, ref)
	}
	dir := assignmentDir{read: func(ctx context.Context, cell string) (directory.CellView, error) {
		g.mu.Lock()
		defer g.mu.Unlock()
		i, _, e := g.barrier("a")
		if e != nil {
			return directory.CellView{}, e
		}
		v, _, e := raftdirectory.AuthorizedWorkerRead(ctx, g.stores[i], cluster, id, cell, 1)
		return v, e
	}, release: func(ctx context.Context, cell string, f uint64) error {
		r, e := submit(ctx, signed(id, workerKey, 1, cs.Mutation{Release: &cs.ReleaseCell{Cell: cell, Fence: f}}))
		if e == nil && r.Code != cs.OK {
			return directory.ErrFenceStale
		}
		return e
	}}
	dir.publish = func(ctx context.Context, cell string, p directory.Publish) (directory.CellView, error) {
		// Receipt creation is a restricted ingress operation after confirming all
		// uploaded frames and the indexed head, never a worker-minted attestation.
		loc, e := store.Locate(ctx, []string{p.Head})
		if e != nil {
			return directory.CellView{}, e
		}
		if _, ok := loc[p.Head]; !ok {
			return directory.CellView{}, fmt.Errorf("missing uploaded head")
		}
		for _, frame := range p.Frames {
			if _, e := store.GetFrame(ctx, frame); e != nil {
				return directory.CellView{}, e
			}
		}
		receipt := cs.UploadReceipt{Device: id, Cell: cell, WorkerIncarnation: 1, Head: p.Head, HeadRID: p.Head, Frames: p.Frames}
		r, e := g.submit(signed("effects", key("effects"), 0, cs.Mutation{RecordUpload: &receipt}))
		if e != nil {
			return directory.CellView{}, e
		}
		if r.Code != cs.OK {
			return directory.CellView{}, directory.ErrUnauthorized
		}
		r, e = submit(ctx, signed(id, workerKey, 1, cs.Mutation{Publish: &cs.PublishCell{Cell: cell, Publish: p, Receipt: cs.UploadID(receipt)}}))
		if e == nil && r.Code != cs.OK {
			return directory.CellView{}, directory.ErrFenceStale
		}
		if e != nil {
			return directory.CellView{}, e
		}
		return *r.View, nil
	}
	taker := handoff.Taker{Dir: dir, Fetch: &cellsync.Fetcher{Engine: engW, Store: store, Inbox: func(c cell.Cell) string { return filepath.Join(homeW, "inbox", c.ID) }}, Local: assignmentLocal{}, RootFor: func(id string) string { return filepath.Join(homeW, id) }}
	worker := syncsetup.WorkerAdapter{Credential: credential, Incarnation: 1, StateDir: filepath.Join(homeW, "attempts"), Taker: taker, Submit: submit, Current: current, Now: func() int64 { return now }}
	invocations := 0
	for _, a := range assignments {
		must(t, worker.Run(ctx, a, func(ctx context.Context, taken handoff.Taken) (string, error) {
			invocations++
			b, e := os.ReadFile(filepath.Join(taken.Cell.Root, "input.txt"))
			if e != nil || string(b) != "source files" {
				t.Fatal("R21 incomplete closure", string(b), e)
			}
			head := engW.Seal(taken.Cell, map[string]string{cell.MetaPath: string(meta), "result.txt": "scripted result"})
			d := taken.Driving
			p := cellsync.Publisher{Engine: engW, Store: store, Dir: dir}
			p.Store = &assignmentFailStore{Store: store}
			if e := p.Publish(ctx, &d, head, cellsync.PublishInfo{}); e == nil {
				t.Fatal("R21 failed upload published")
			}
			_, unchanged, e := current(ctx, a.Ref())
			must(t, e)
			if unchanged.Cell.Head != taken.Driving.Head {
				t.Fatal("R21 head exposed before upload")
			}
			p.Store = store
			return head, p.Publish(ctx, &d, head, cellsync.PublishInfo{})
		}))
		must(t, worker.Run(ctx, a, func(context.Context, handoff.Taken) (string, error) {
			t.Fatal("R20 completed worker replayed")
			return "", nil
		}))
		// A fresh home retrieves the ordinary cellsync result, not assignment text.
		engResult := cellsync.NewFakeEngine(t.TempDir())
		dest := cell.Cell{ID: a.Spec.Cell, Root: filepath.Join(t.TempDir(), "chat")}
		_, v, e := current(ctx, a.Ref())
		must(t, e)
		f := cellsync.Fetcher{Engine: engResult, Store: store, Inbox: func(cell.Cell) string { return t.TempDir() }}
		must(t, f.Fetch(ctx, dest, v.Cell.Head))
		b, e := os.ReadFile(filepath.Join(dest.Root, "result.txt"))
		must(t, e)
		if string(b) != "scripted result" {
			t.Fatal(string(b))
		}
	}
	if invocations != 2 {
		t.Fatal("R19 parallel child invocation count", invocations)
	}
	for i, store := range g.stores {
		snap, e := store.CreateSnapshot(ctx)
		must(t, e)
		must(t, store.Compact(ctx, snap.GetMetadata().GetIndex()))
		g.drive(i, raftcore.CompactSnapshot{Snapshot: snap})
	}
	g.drain()
	recoverMembership(t, g)
	files, e := filepath.Glob(filepath.Join(homeW, "attempts", "*.json"))
	must(t, e)
	for _, path := range files {
		if strings.HasPrefix(filepath.Base(path), "cell-") {
			continue
		}
		raw, e := os.ReadFile(path)
		must(t, e)
		var saved struct{ Claim cs.Command }
		must(t, json.Unmarshal(raw, &saved))
		r, e := submit(ctx, saved.Claim)
		must(t, e)
		if r.Code != cs.OK || r.View == nil || r.View.Cell.Lease.Fence != 2 || r.Assignment.Status != "claimed" {
			t.Fatal("R3/R17 exact claim after compaction/restart", r)
		}
	}
	after, e := directory.NewRaft(g).For("a").Cell(ctx, "parent")
	must(t, e)
	if after.Cell.Head != parent.View.Cell.Head || after.Cell.Lease.Fence != 1 {
		t.Fatal("R19 worker wrote parent")
	}
}

// R20 retains both completed and incomplete call records without running any
// recovered call. The no-execution rule holds independently of assignment TTL.
func TestR20RetainedWorkerCallRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.wal")
	wal, _, e := cellstore.OpenWAL(path)
	must(t, e)
	in := cellstore.Intent{V: 2, Tool: "exec", ArgsHash: "opaque", Started: 1000, SideEffect: "external", Brief: "scripted call"}
	must(t, wal.Begin(in))
	_, rec, e := cellstore.OpenWAL(path)
	must(t, e)
	if len(rec.Incomplete) != 1 || rec.Incomplete[0].Brief != "scripted call" {
		t.Fatal(rec)
	}
	raw, e := json.Marshal(rec)
	must(t, e)
	if !bytes.Contains(raw, []byte("Incomplete")) {
		t.Fatal("incomplete display lost")
	}
}

type assignmentFailStore struct{ blobstore.Store }

func (s *assignmentFailStore) PutFrame(context.Context, []byte) (blobstore.FrameID, error) {
	return "", errors.New("injected upload failure")
}
