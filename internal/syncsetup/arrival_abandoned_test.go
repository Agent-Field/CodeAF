package syncsetup

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
)

const abandonedOwnerEnv = "CODEAF_TEST_W10B_FAILED_CELL"

// A chat's files failed to arrive on this computer and the window that was
// bringing them closed. The tree on disk is partial. Until the files are fetched
// again, a seal of it refuses (it would publish the partial tree as the chat's
// files), and then the next time the person opens or takes the chat the take IS
// the restart: it begins a fresh arrival that replaces the stale failure,
// fetches the files through the ordinary background stage, never seals or
// captures the partial tree, and keeps the hold the computer already had.
func TestW10bTakingAChatAgainRestartsTheFilesOfAnAbandonedArrival(t *testing.T) {
	if cellRoot := os.Getenv(abandonedOwnerEnv); cellRoot != "" {
		a := cell.BeginWorkspaceArrival(cellRoot, workspaceOf(cellRoot), nil, nil)
		a.Fail(errors.New("sync could not be reached"))
		os.Exit(0)
	}

	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	h := newTwoHomes(t)
	seedTree(t, h.work)
	a := h.openA()
	a.mustSay("a turn before the move")
	got := h.takeOnB(a)
	if got.Wait != nil {
		mustNil(t, got.Wait(t.Context()))
	}
	bCell := got.Taken.Cell
	bWork := workspaceOf(bCell.Root)
	want := readTree(t, h.work)
	sameTree(t, "B after the first move", want, readTree(t, bWork))
	heldBefore, err := h.b.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)

	// The files did not all arrive: some are missing and one is cut short.
	mustNil(t, os.RemoveAll(filepath.Join(bWork, "src")))
	mustNil(t, os.Remove(filepath.Join(bWork, "data", "blob.bin")))
	mustNil(t, os.WriteFile(filepath.Join(bWork, "README.md"), []byte("# pro"), 0o644))
	// And the window that was bringing them failed, and closed.
	cmd := exec.Command(os.Args[0], "-test.run=^TestW10bTakingAChatAgainRestartsTheFilesOfAnAbandonedArrival$")
	cmd.Env = append(os.Environ(), abandonedOwnerEnv+"="+bCell.Root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child process failed: %v\n%s", err, out)
	}
	marker := cell.ArrivalMarkerPath(bCell.Root)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("expected the failed arrival's marker at %s: %v", marker, err)
	}

	// Until the restart, a seal refuses, with what to do.
	_, sealErr := h.engB.Seal(t.Context(), bCell, cellstore.TurnInfo{})
	if !errors.Is(sealErr, cell.ErrWorkspaceArrivalAbandoned) || !strings.Contains(sealErr.Error(), "sync could not be reached") || !strings.Contains(sealErr.Error(), "move the chat here again") {
		t.Fatalf("a seal of the partial tree = %v; want a refusal carrying the reason and saying to move the chat here again", sealErr)
	}

	// Opening the chat again is the restart.
	again, err := h.continuerB().Take(t.Context(), h.cell.ID)
	if err != nil {
		t.Fatalf("taking the chat again after its files failed and its window closed: %v", err)
	}
	if again.Wait != nil {
		mustNil(t, again.Wait(t.Context()))
	}
	sameTree(t, "B after the restart", want, readTree(t, bWork))
	if len(readTree(t, bWork)) != len(want) {
		t.Fatalf("after the restart B holds %d files, the source %d", len(readTree(t, bWork)), len(want))
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("the files landed, so the failure must be gone; stat err = %v", err)
	}
	if cell.WorkspaceArriving(bCell.Root) {
		t.Fatal("WorkspaceArriving = true after the files landed")
	}
	_, err = h.engB.Seal(t.Context(), bCell, cellstore.TurnInfo{})
	mustNil(t, err)

	// Nothing was claimed or given up on the way: the hold is the one B had.
	heldAfter, err := h.b.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	if heldAfter.Cell.Lease.Device != heldBefore.Cell.Lease.Device || heldAfter.Cell.Lease.Fence != heldBefore.Cell.Lease.Fence {
		t.Fatalf("the lease moved during the restart: before %+v, after %+v", heldBefore.Cell.Lease, heldAfter.Cell.Lease)
	}
}
