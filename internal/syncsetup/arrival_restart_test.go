package syncsetup

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
)

// #2781: OPENING A CHAT THIS COMPUTER ALREADY HOLDS RESTARTS ITS FILES. The door
// is RestartFiles, the one place the app's open, the terminal's local open and
// its launch ask. These tests are the contract of that place, on the real engine
// and a real relay: what it fetches, what it must leave alone.

const restartOwnerEnv = "CODEAF_TEST_RESTART_FILES_OWNER"

// TestRestartFilesChildOwner is not a test: it is the process that was fetching
// the files and failed, run by the tests below so that its owner is really gone.
func TestRestartFilesChildOwner(t *testing.T) {
	root := os.Getenv(restartOwnerEnv)
	if root == "" {
		t.Skip("only runs as the child of a restart test")
	}
	a := cell.BeginWorkspaceArrival(root, workspaceOf(root), nil, nil)
	a.Fail(errors.New("sync could not be reached"))
	os.Exit(0)
}

// asyncContinuerB is B's take side as the app and the terminal build it: the
// files come in behind the answer.
func (h *twoHomes) asyncContinuerB() *Continuer {
	h.awayFromA()
	return h.b.Continuer(h.engB, TakeOptions{DeviceName: nameB, Notify: h.wall.add, AsyncFiles: true, RootFor: func(id string) string {
		return filepath.Join(h.rootsB, id)
	}})
}

// movedToB is a chat that moved to B, whose files arrived, in a state a person
// who closes a laptop can leave it in: B holds it, and nothing runs.
type movedToB struct {
	h     *twoHomes
	cell  cell.Cell
	work  string
	want  map[string]entry
	fence uint64
}

func moveToB(t *testing.T) *movedToB {
	t.Helper()
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
	m := &movedToB{h: h, cell: got.Taken.Cell, work: workspaceOf(got.Taken.Cell.Root), want: readTree(t, h.work)}
	sameTree(t, "B after the move", m.want, readTree(t, m.work))
	view, err := h.b.Dir.Cell(t.Context(), h.cell.ID)
	mustNil(t, err)
	m.fence = view.Cell.Lease.Fence
	return m
}

// breakFiles leaves B's copy as a fetch that did not finish would.
func (m *movedToB) breakFiles(t *testing.T) {
	t.Helper()
	mustNil(t, os.RemoveAll(filepath.Join(m.work, "src")))
	mustNil(t, os.Remove(filepath.Join(m.work, "data", "blob.bin")))
	mustNil(t, os.WriteFile(filepath.Join(m.work, "README.md"), []byte("# pro"), 0o644))
}

// ownerLeaves runs the fetch that failed, in a process that then exits.
func (m *movedToB) ownerLeaves(t *testing.T) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRestartFilesChildOwner$")
	cmd.Env = append(os.Environ(), restartOwnerEnv+"="+m.cell.Root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the child that was fetching the files failed: %v\n%s", err, out)
	}
	if !cell.WorkspaceArriving(m.cell.Root) {
		t.Fatal("the failed arrival does not hold the gate shut")
	}
}

func (m *movedToB) lease(t *testing.T) (device string, fence uint64) {
	t.Helper()
	view, err := m.h.b.Dir.Cell(t.Context(), m.h.cell.ID)
	mustNil(t, err)
	return view.Cell.Lease.Device, view.Cell.Lease.Fence
}

func (m *movedToB) marker(t *testing.T) []byte {
	t.Helper()
	raw, err := os.ReadFile(cell.ArrivalMarkerPath(m.cell.Root))
	if err != nil {
		t.Fatalf("the arrival marker: %v", err)
	}
	return raw
}

func waitWithin(t *testing.T, what string, within time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// OPENING A HELD CHAT WHOSE FILES FAILED TO ARRIVE AND WHOSE FETCHER IS GONE
// FETCHES THEM AGAIN. Behind the answer the files land byte for byte, the
// failure goes away, a seal of the chat succeeds, and the hold is the one this
// computer already had: nothing was claimed, fenced or released for it.
func TestRestartFilesFetchesTheFilesOfAHeldChatWhoseArrivalWasAbandoned(t *testing.T) {
	m := moveToB(t)
	heldBy, fence := m.lease(t)
	m.breakFiles(t)
	m.ownerLeaves(t)

	// Until the restart a seal refuses, with what the person would do about it.
	if _, err := m.h.engB.Seal(t.Context(), m.cell, cellstore.TurnInfo{}); !errors.Is(err, cell.ErrWorkspaceArrivalAbandoned) {
		t.Fatalf("a seal of the partial tree = %v; want the abandoned-arrival refusal", err)
	}

	restarted, err := m.h.asyncContinuerB().RestartFiles(t.Context(), m.h.cell.ID)
	if err != nil || !restarted {
		t.Fatalf("RestartFiles = %v, %v; want it to have restarted the files", restarted, err)
	}
	waitWithin(t, "the restarted files to land", 3*time.Minute, func() bool { return !cell.WorkspaceArriving(m.cell.Root) })
	sameTree(t, "B after the open", m.want, readTree(t, m.work))
	if got := len(readTree(t, m.work)); got != len(m.want) {
		t.Fatalf("after the open B holds %d files, the source %d", got, len(m.want))
	}
	if _, err := os.Stat(cell.ArrivalMarkerPath(m.cell.Root)); !os.IsNotExist(err) {
		t.Fatalf("the files landed, so the failure must be gone; stat err = %v", err)
	}
	if _, err := m.h.engB.Seal(t.Context(), m.cell, cellstore.TurnInfo{}); err != nil {
		t.Fatalf("a seal after the files landed: %v", err)
	}
	if device, now := m.lease(t); device != heldBy || now != fence {
		t.Fatalf("the hold moved during the open: was %q at %d, now %q at %d", heldBy, fence, device, now)
	}
	// And a second open finds nothing left to restart.
	if again, err := m.h.asyncContinuerB().RestartFiles(t.Context(), m.h.cell.ID); err != nil || again {
		t.Fatalf("a second RestartFiles = %v, %v; want nothing to do", again, err)
	}
}

// THE HOLD DOES NOT HAVE TO BE LIVE. A computer that quit, or whose lid stayed
// shut past the lease, has no send receipt left for the chat (the close forgets
// it, and the next read of a lapsed lease does), yet the lease still names it and
// the chat is still its own: no other computer has taken it. That is the state a
// person finds the chat in when they open it the next morning, and it is the one
// the issue is about, so a restart that needed a live receipt would never run.
func TestRestartFilesFetchesTheFilesOfAHeldChatWhoseHoldEnded(t *testing.T) {
	for _, how := range []struct {
		name string
		end  func(t *testing.T, m *movedToB)
	}{
		{"released at a quit", func(t *testing.T, m *movedToB) {
			_, fence := m.lease(t)
			mustNil(t, m.h.b.Dir.Release(t.Context(), m.h.cell.ID, fence))
			mustNil(t, m.h.b.forgetSendLease(m.cell.ID, sendLease{Cell: m.h.cell.ID, Fence: fence}))
		}},
		{"lapsed while the lid was shut", func(t *testing.T, m *movedToB) { m.h.lapse() }},
	} {
		t.Run(how.name, func(t *testing.T) {
			m := moveToB(t)
			heldBy, fence := m.lease(t)
			m.breakFiles(t)
			m.ownerLeaves(t)
			how.end(t, m)

			restarted, err := m.h.asyncContinuerB().RestartFiles(t.Context(), m.h.cell.ID)
			if err != nil || !restarted {
				t.Fatalf("RestartFiles = %v, %v; want it to have restarted the files", restarted, err)
			}
			waitWithin(t, "the restarted files to land", 3*time.Minute, func() bool { return !cell.WorkspaceArriving(m.cell.Root) })
			sameTree(t, "B after the open", m.want, readTree(t, m.work))
			if got := len(readTree(t, m.work)); got != len(m.want) {
				t.Fatalf("after the open B holds %d files, the source %d", got, len(m.want))
			}
			if _, err := m.h.engB.Seal(t.Context(), m.cell, cellstore.TurnInfo{}); err != nil {
				t.Fatalf("a seal after the files landed: %v", err)
			}
			// The restart claims nothing: the lease is still this computer's, with
			// the fence it had, and the open that follows is what takes it up again.
			if device, now := m.lease(t); device != heldBy || now != fence {
				t.Fatalf("the restart moved the lease from %q at %d to %q at %d", heldBy, fence, device, now)
			}
		})
	}
}

// NOTHING IS STARTED OVER A LIVE OWNER'S ARRIVAL. The files are on their way, and
// a second fetch over the same folder would race the first: the open starts no
// arrival, fetches nothing and replaces nothing.
func TestRestartFilesStartsNothingWhileALiveOwnersArrivalIsInFlight(t *testing.T) {
	m := moveToB(t)
	m.breakFiles(t)
	owner := cell.BeginWorkspaceArrival(m.cell.Root, m.work, nil, nil)
	t.Cleanup(owner.Complete)
	before := m.marker(t)

	restarted, err := m.h.asyncContinuerB().RestartFiles(t.Context(), m.h.cell.ID)
	if err != nil || restarted {
		t.Fatalf("RestartFiles under a live owner = %v, %v; want it to do nothing", restarted, err)
	}
	if cell.LookupWorkspaceArrival(m.cell.Root) != owner || owner.Ready() {
		t.Fatal("the live owner's arrival was replaced")
	}
	if got := m.marker(t); !bytes.Equal(got, before) {
		t.Fatalf("the live owner's marker was rewritten: %q, was %q", got, before)
	}
	if _, err := os.Stat(filepath.Join(m.work, "src", "main.go")); !os.IsNotExist(err) {
		t.Fatalf("the open fetched files while an owner was still bringing them: %v", err)
	}
}

// A CHAT ANOTHER COMPUTER NOW HOLDS IS NOT RESTARTED. A fetch here would set the
// local copy aside and seal its partial tree as edits, so the refusal stands: the
// open claims no hold, fetches nothing and leaves the failure as it was, for the
// person's own continue to deal with.
func TestRestartFilesLeavesAnAbandonedChatAloneWhenAnotherComputerHoldsIt(t *testing.T) {
	m := moveToB(t)
	// A takes the chat back; B is no longer the holder.
	_, err := m.h.continuerA().Take(t.Context(), m.h.cell.ID)
	mustNil(t, err)
	heldBy, fence := m.lease(t)
	if heldBy == m.h.b.Device.ID() {
		t.Fatal("fixture: B still holds the chat")
	}
	m.breakFiles(t)
	m.ownerLeaves(t)
	before := m.marker(t)

	restarted, err := m.h.asyncContinuerB().RestartFiles(t.Context(), m.h.cell.ID)
	if err != nil || restarted {
		t.Fatalf("RestartFiles for a chat another computer holds = %v, %v; want it to do nothing", restarted, err)
	}
	if device, now := m.lease(t); device != heldBy || now != fence {
		t.Fatalf("the open moved the hold: was %q at %d, now %q at %d", heldBy, fence, device, now)
	}
	if got := m.marker(t); !bytes.Equal(got, before) {
		t.Fatalf("the failure was rewritten: %q, was %q", got, before)
	}
	if _, err := os.Stat(filepath.Join(m.work, "src", "main.go")); !os.IsNotExist(err) {
		t.Fatalf("the open fetched files for a chat another computer holds: %v", err)
	}
	if _, err := m.h.engB.Seal(context.Background(), m.cell, cellstore.TurnInfo{}); !errors.Is(err, cell.ErrWorkspaceArrivalAbandoned) {
		t.Fatalf("a seal of the partial tree = %v; the refusal must stand", err)
	}
}

// A CHAT WITH NO FAILURE COSTS NOTHING. The open asks no one, reads nothing from
// the relay and answers false: most opens are of chats that arrived whole.
func TestRestartFilesDoesNothingForAChatWhoseFilesArrived(t *testing.T) {
	m := moveToB(t)
	before := readTree(t, m.work)
	restarted, err := m.h.asyncContinuerB().RestartFiles(t.Context(), m.h.cell.ID)
	if err != nil || restarted {
		t.Fatalf("RestartFiles for a whole chat = %v, %v; want nothing to do", restarted, err)
	}
	sameTree(t, "B after a plain open", before, readTree(t, m.work))
	if restarted, err := (*Continuer)(nil).RestartFiles(t.Context(), "no-chat"); err != nil || restarted {
		t.Fatalf("RestartFiles on a computer with no sync = %v, %v", restarted, err)
	}
}

// A RESTART THAT CANNOT FETCH NEVER GIVES BACK THE HOLD THIS COMPUTER HAD. A take
// that acquired a chat hands the lease back when its files cannot come, so the
// chat is not left held by a computer that never got it; a restart acquired
// nothing, and releasing on its failure would let go of a chat the person is
// sitting in. The synchronous road, where the take waits for the files, is the
// one that used to.
func TestARestartWhoseFetchFailsKeepsTheHoldTheComputerAlreadyHad(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("the fetch is made to fail by a folder it may not write to, which needs Unix permissions and a user who is not root")
	}
	m := moveToB(t)
	view, err := m.h.b.Dir.Cell(t.Context(), m.h.cell.ID)
	mustNil(t, err)
	held := view.Cell.Lease
	if held.Expires == 0 {
		t.Fatal("fixture: B does not hold the chat")
	}
	m.breakFiles(t)
	m.ownerLeaves(t)
	// The objects are all here already, so the fetch cannot fail on the wire; a
	// folder the files may not be written into fails it where it puts them.
	mustNil(t, os.Chmod(m.work, 0o500))
	t.Cleanup(func() { _ = os.Chmod(m.work, 0o700) })

	if _, err := m.h.continuerB().Take(t.Context(), m.h.cell.ID); err == nil {
		t.Fatal("the restart succeeded into a folder it may not write to")
	}
	after, err := m.h.b.Dir.Cell(t.Context(), m.h.cell.ID)
	mustNil(t, err)
	if after.Cell.Lease.Device != held.Device || after.Cell.Lease.Fence != held.Fence || after.Cell.Lease.Expires == 0 {
		t.Fatalf("a restart that could not fetch changed the hold: was %+v, now %+v", held, after.Cell.Lease)
	}
}
