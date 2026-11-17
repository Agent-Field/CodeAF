package syncsetup

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/inventory"
	"github.com/Agent-Field/codeaf/internal/preflight"
)

// toldAt is a surface that files each sentence under the chat it is about, as
// the app does.
type toldAt struct {
	mu    sync.Mutex
	lines map[string][]string
}

func (n *toldAt) add(at cell.Cell, line string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.lines == nil {
		n.lines = map[string][]string{}
	}
	n.lines[at.Root] = append(n.lines[at.Root], line)
}

func (n *toldAt) about(root string) string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return strings.Join(n.lines[root], "\n")
}

// breakVault makes the vault exchange of home impossible: its lock file cannot
// be opened, so a continue's vault pull fails with an ordinary error.
func breakVault(t *testing.T, home string) {
	t.Helper()
	lock := filepath.Join(home, "state-sync.lock")
	_ = os.Remove(lock)
	if err := os.MkdirAll(lock, 0o700); err != nil {
		t.Fatal(err)
	}
}

// A surface that holds several chats (the app) is told, for the chat that
// moved, when something its after-work does behind the open fails. On dev the
// vault pull ran before the open and its failure refused the take; it runs
// behind the open now, and a failure with nowhere to go is one nobody sees.
func TestT2AfterWorkFailureIsToldForTheChatThatMoved(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)
	a := h.openA()
	a.mustSay("a chat with files")
	h.durable(h.cell.ID, h.cell)
	breakVault(t, h.b.Home)

	told := &toldAt{}
	cont := h.continuerB()
	cont.opt.AsyncFiles = true
	cont.opt.Notify = nil
	cont.opt.NotifyAt = told.add

	got, err := cont.Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	// The vault is one of the after-work steps, so it fails the wait too; what
	// matters is that the person is told, not that the chat is lost.
	_ = got.Wait(t.Context())

	line := told.about(got.Taken.Cell.Root)
	if !strings.Contains(line, "part of its state could not be updated yet") {
		t.Fatalf("T2: the vault pull failed behind the open and the chat's surface was told %q", line)
	}
	if _, err := os.Stat(transcriptOf(got.Taken.Cell)); err != nil {
		t.Fatalf("T2: the chat did not stay open: %v", err)
	}
}

// The terminal has one desk for the whole window and keeps hearing Notify.
func TestT2AfterWorkFailureIsToldOnTheTerminalDeskToo(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)
	a := h.openA()
	a.mustSay("a chat with files")
	h.durable(h.cell.ID, h.cell)
	breakVault(t, h.b.Home)

	cont := h.continuerB()
	cont.opt.AsyncFiles = true
	got, err := cont.Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	_ = got.Wait(t.Context())

	if !strings.Contains(strings.Join(h.wall.all(), "\n"), "part of its state could not be updated yet") {
		t.Fatalf("T2: the terminal desk heard %q", h.wall.all())
	}
}

// A chat that opens before its files arrive cannot know what this computer
// lacks for the project, or what is uncommitted in the folder. The look that
// can know ends the after-work, and its report reaches the surface then, with
// the whole of what dev's open (which looked after the files) said.
func TestT3ArrivedReportReachesTheSurfaceWhenTheFilesFinish(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)
	// The chat used a tool this computer does not have.
	seed, err := inventory.Open(h.cell.Root)
	mustNil(t, err)
	mustNil(t, seed.Update(func(inv *inventory.Inventory) {
		inv.Tools = []inventory.Tool{{Name: "setupdemo", BinaryHash: "from-elsewhere"}}
	}))
	a := h.openA()
	a.mustSay("a chat that used a tool")
	h.durable(h.cell.ID, h.cell)

	var mu sync.Mutex
	var arrivedAt cell.Cell
	var arrived preflight.Resume
	heard := 0
	cont := h.continuerB()
	cont.opt.AsyncFiles = true
	cont.opt.OnArrived = func(at cell.Cell, r preflight.Resume) {
		mu.Lock()
		defer mu.Unlock()
		arrivedAt, arrived, heard = at, r, heard+1
	}
	got, err := cont.Take(t.Context(), h.cell.ID)
	mustNil(t, err)
	mustNil(t, got.Wait(t.Context()))

	mu.Lock()
	defer mu.Unlock()
	if heard != 1 || arrivedAt.Root != got.Taken.Cell.Root {
		t.Fatalf("T3: the surface heard the arrival %d times for %q, want once for %q", heard, arrivedAt.Root, got.Taken.Cell.Root)
	}
	if len(arrived.Lacks) != 1 || arrived.Lacks[0].Name != "setupdemo" || arrived.Lacks[0].Bucket != preflight.Installable {
		t.Fatalf("T3: the report after the files arrived does not say this computer lacks the tool: %+v", arrived)
	}
	if arrived.Empty() {
		t.Fatal("T3: a report with a missing tool in it is not empty, and raises no card if it is")
	}
}
