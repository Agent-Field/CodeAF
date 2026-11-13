package syncsetup

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/identity"
)

type workerTestFetch struct{ meta []byte }

func (f workerTestFetch) Fetch(_ context.Context, c cell.Cell, head string) error {
	if e := os.MkdirAll(filepath.Join(c.Root, ".cell"), 0700); e != nil {
		return e
	}
	return os.WriteFile(filepath.Join(c.Root, cell.MetaPath), f.meta, 0600)
}
func (f workerTestFetch) Complete(ctx context.Context, c cell.Cell, head string) error {
	return f.Fetch(ctx, c, head)
}

type workerTestLocal struct{}

func (workerTestLocal) Dirty(context.Context, cell.Cell) (bool, error)        { return false, nil }
func (workerTestLocal) Unpublished(cell.Cell, string) (string, uint32, error) { return "", 0, nil }
func (workerTestLocal) Seal(context.Context, cell.Cell) (string, uint32, error) {
	return "", 0, errors.New("unexpected seal")
}
func (workerTestLocal) Follow(context.Context, cell.Cell, cell.Cell) error { return nil }

type workerTestDirectory struct {
	directory.Client
	read func() directory.CellView
}

func (d workerTestDirectory) Cell(context.Context, string) (directory.CellView, error) {
	return d.read(), nil
}
func (d workerTestDirectory) Release(context.Context, string, uint64) error { return nil }

// R20/C28 kill the real worker process at each execution boundary. Only the
// pre-execution claim can resume automatically; every recorded execution intent
// is retained and never causes a second invocation, including after reassignment.
func TestR20WorkerProcessCutsNeverReplayTools(t *testing.T) {
	for _, phase := range []string{"claim", "intent", "execution", "seal", "upload", "publish", "finish"} {
		t.Run(phase, func(t *testing.T) {
			dir := t.TempDir()
			run := func(mode string) string {
				cmd := exec.Command(os.Args[0], "-test.run=^TestR20WorkerProcessHelper$", "-test.v")
				cmd.Env = append(os.Environ(), "CODEAF_P16_HELPER="+dir, "CODEAF_P16_PHASE="+phase, "CODEAF_P16_MODE="+mode)
				out, e := cmd.CombinedOutput()
				if mode == "cut" {
					var exit *exec.ExitError
					if !errors.As(e, &exit) || exit.ExitCode() != 41 {
						t.Fatalf("cut %s: %v %s", phase, e, out)
					}
				} else if e != nil {
					t.Fatalf("recover %s: %v %s", phase, e, out)
				}
				return string(out)
			}
			run("cut")
			out := run("recover")
			count, e := os.ReadFile(filepath.Join(dir, "invocations"))
			if !errors.Is(e, os.ErrNotExist) && e != nil {
				t.Fatal(e)
			}
			want := 1
			if phase == "intent" {
				want = 0
			}
			if len(count) != want {
				t.Fatalf("R20 %s invoked %d times, want %d", phase, len(count), want)
			}
			if phase != "claim" && phase != "finish" && !strings.Contains(out, ErrWorkerIncomplete.Error()) {
				t.Fatal("missing incomplete display", out)
			}
		})
	}
}
func TestR20WorkerProcessHelper(t *testing.T) {
	dir := os.Getenv("CODEAF_P16_HELPER")
	if dir == "" {
		return
	}
	phase, mode := os.Getenv("CODEAF_P16_PHASE"), os.Getenv("CODEAF_P16_MODE")
	cut := func(at string) {
		if mode == "cut" && phase == at {
			os.Exit(41)
		}
	}
	var seed [32]byte
	seed[0] = 7
	k := ed25519.NewKeyFromSeed(seed[:])
	root := ed25519.NewKeyFromSeed(make([]byte, 32))
	cert := cs.WorkerCertificate{V: 1, Identity: cs.IdentityID(root.Public().(ed25519.PublicKey)), Cluster: "cuts", Key: k.Public().(ed25519.PublicKey), Made: 1, Expires: 10000, Cells: []string{"chat"}}
	raw, _ := cs.WorkerCertificateBytes(cert)
	cert.Signature = ed25519.Sign(root, raw)
	doc, _ := cs.Encode(struct {
		V           uint16               `json:"v"`
		Seed        string               `json:"worker_seed"`
		Certificate cs.WorkerCertificate `json:"certificate"`
	}{1, hex.EncodeToString(seed[:]), cert})
	worker, e := identity.UnmarshalWorker(doc, root.Public().(ed25519.PublicKey), "cuts", time.UnixMilli(1000))
	if e != nil {
		t.Fatal(e)
	}
	a := cs.Assignment{Spec: cs.Assign{ID: "opaque", Revision: 1, Cell: "chat", Target: worker.ID(), Incarnation: 1, Expires: 9000, Expect: cs.LeaseExpectation{Device: "owner", Fence: 1}, SealedPayload: []byte{0x80}}, Status: "pending"}
	remotePath := filepath.Join(dir, "remote.json")
	load := func() (cs.Assignment, directory.CellView) {
		b, e := os.ReadFile(remotePath)
		if e == nil {
			if json.Unmarshal(b, &a) != nil {
				t.Fatal("damaged remote fixture")
			}
		}
		holder, fence := "owner", uint64(1)
		if a.Fence != 0 {
			holder, fence = worker.ID(), a.Fence
		}
		head := "head"
		if a.Head != "" {
			head = a.Head
		}
		return a, directory.CellView{Now: 1000, Cell: cs.Cell{Head: head, Lease: cs.Lease{Device: holder, Fence: fence, Expires: 9000}}}
	}
	save := func() {
		b, _ := json.Marshal(a)
		if e := os.WriteFile(remotePath, b, 0600); e != nil {
			t.Fatal(e)
		}
	}
	_, view := load()
	template, e := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.FilesOnly})
	if e != nil {
		t.Fatal(e)
	}
	meta, e := os.ReadFile(filepath.Join(template.Root, cell.MetaPath))
	if e != nil {
		t.Fatal(e)
	}
	adapter := WorkerAdapter{Credential: worker, Incarnation: 1, StateDir: filepath.Join(dir, "attempts"), Now: func() int64 { return 1000 }, Taker: handoff.Taker{Dir: workerTestDirectory{read: func() directory.CellView { _, v := load(); return v }}, Fetch: workerTestFetch{meta}, Local: workerTestLocal{}, RootFor: func(string) string { return filepath.Join(dir, "chat") }}, Current: func(context.Context, cs.AssignmentRef) (cs.Assignment, directory.CellView, error) {
		a, v := load()
		return a, v, nil
	}}
	adapter.Submit = func(_ context.Context, c cs.Command) (cs.Result, error) {
		if c.Change.ClaimAssignment != nil {
			a, _ = load()
			if a.Status == "pending" {
				a.Status = "claimed"
				a.Fence = 2
				a.Head = "head"
				save()
			}
			cut("claim")
			a, view = load()
			return cs.Result{Code: cs.OK, Assignment: &a, View: &view}, nil
		}
		if c.Change.FinishAssignment != nil {
			a, _ = load()
			a.Status = "finished"
			a.Head = c.Change.FinishAssignment.Head
			save()
			cut("finish")
			return cs.Result{Code: cs.OK, Assignment: &a}, nil
		}
		return cs.Result{Code: cs.BadRequest}, nil
	}
	err := adapter.Run(context.Background(), a, func(ctx context.Context, taken handoff.Taken) (string, error) {
		cut("intent")
		wal, _, e := cellstore.OpenWAL(filepath.Join(dir, "calls.wal"))
		if e != nil {
			return "", e
		}
		in := cellstore.Intent{V: 2, Tool: "exec", ArgsHash: "opaque", Started: 1000, SideEffect: "external", Brief: "scripted call"}
		if e = wal.Begin(in); e != nil {
			return "", e
		}
		f, e := os.OpenFile(filepath.Join(dir, "invocations"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if e != nil {
			return "", e
		}
		_, e = f.Write([]byte("x"))
		f.Close()
		if e != nil {
			return "", e
		}
		cut("execution")
		if e = wal.Finish(in, cellstore.Executed{}); e != nil {
			return "", e
		}
		cut("seal")
		if e = os.WriteFile(filepath.Join(dir, "sealed-frame"), []byte("sealed"), 0600); e != nil {
			return "", e
		}
		cut("upload")
		a.Head = "result"
		save()
		cut("publish")
		return "result", nil
	})
	if err != nil {
		if !errors.Is(err, ErrWorkerIncomplete) {
			t.Fatal(err)
		}
		t.Log(err)
	}
	if mode == "cut" {
		t.Fatal("did not reach requested cut")
	}
}
