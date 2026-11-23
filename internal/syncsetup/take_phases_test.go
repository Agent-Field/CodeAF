package syncsetup

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/session"
)

type measuredRestore struct {
	acquired *atomic.Bool
	count    *atomic.Int32
	before   *atomic.Int32
}

func (m measuredRestore) run(ctx context.Context, dir string, environ []string, argv ...string) ([]byte, error) {
	whole := len(argv) > 2 && argv[2] == "materialize"
	if len(argv) > 2 && argv[2] == "rewind" {
		whole = true
		for _, arg := range argv {
			if arg == "--dry-run" || strings.HasPrefix(arg, "--paths=") {
				whole = false
			}
		}
	}
	if whole {
		m.count.Add(1)
		if !m.acquired.Load() {
			m.before.Add(1)
		}
	}
	command := exec.CommandContext(ctx, argv[0], argv[1:]...)
	command.Dir, command.Env = dir, environ
	return command.Output()
}

// Contract A1 restores an ordinary chat exactly once, after its lease is held.
func TestOrdinaryTakeMaterializesOnceAfterAcquire(t *testing.T) {
	t.Setenv("CODEAF_PROTOTYPE_WORKSPACE_OWNERSHIP", "0")
	var acquired atomic.Bool
	h := newTwoHomes(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/acquire") {
				acquired.Store(true)
			}
			next.ServeHTTP(w, r)
		})
	})
	a := h.openA()
	a.mustSay("latest saved reply")
	h.durable(h.cell.ID, h.cell)
	first, err := h.continuerB().Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	b := h.openOn(h.b, h.engB, first.Taken.Cell, workspaceOf(first.Taken.Cell.Root), nameB)
	b.mustSay("latest reply on the other computer")
	h.durable(h.cell.ID, first.Taken.Cell)
	var count, before atomic.Int32
	measure := measuredRestore{&acquired, &count, &before}
	h.engine.Run = measure.run
	h.engine.Transport = cellstore.Spawn{Binary: h.bin, Run: measure.run}
	continuer := h.continuerA()
	continuer.opt.OnPhase = func(p handoff.Phase) {
		if !p.Ended.IsZero() {
			t.Logf("phase %s: %s", p.Name, p.Ended.Sub(p.Started))
		}
	}
	acquired.Store(false)
	got, err := continuer.Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	if count.Load() != 1 || before.Load() != 0 {
		t.Errorf("whole-tree materializations=%d, before acquire=%d; want 1, 0", count.Load(), before.Load())
	}
	if got.Taken.Kept != "" {
		t.Errorf("unexpected extra continuation branch: %s", got.Taken.Kept)
	}
}

type admissionTarget struct{ tree string }

func (a *admissionTarget) Do(_ context.Context, target cellstore.Target, op cellstore.Op) ([]byte, error) {
	if op.Verb() == "materialize" {
		a.tree = target.Tree
	}
	return []byte(`{}`), nil
}

// Contract A1 keeps pre-acquire workspace staging out of a borrowed folder.
func TestStagedWorkspaceNeverTargetsTheBorrowedFolder(t *testing.T) {
	root := filepath.Join(t.TempDir(), "chat")
	stage := root + handoff.StagingSuffix
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(stage, ".cell"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveMeta(stage, session.Meta{ID: "chat", Workspace: project}); err != nil {
		t.Fatal(err)
	}
	target := &admissionTarget{}
	// Take helpers derive local sync paths, so this fixture must own its home.
	sync := &Sync{Home: t.TempDir()}
	continuer := sync.Continuer(cellstore.Engine{DataRoot: t.TempDir(), Transport: target}, TakeOptions{RootFor: func(string) string { return root }})
	take := continuer.taker(t.Context(), &Scope{Store: blobstore.NewMemory()})
	if err := take.Fetch.Fetch(t.Context(), cell.Cell{ID: "chat", Root: stage}, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if target.tree == "" || filepath.Clean(target.tree) == filepath.Clean(project) {
		t.Fatalf("workspace admission targeted borrowed folder %q", target.tree)
	}
}
