package syncsetup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/cellstore"
	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/executor"
	"github.com/Agent-Field/codeaf/internal/filesdoor"
	"github.com/Agent-Field/codeaf/internal/handoff"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/syncwords"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// These tests are the validation contract of live transfer (SPEC.md, C1-C19).
// Each names the item it comes from, and each is written from what a person
// with two computers sees: a relay that keeps nothing, a chat started on one,
// and the other taking it. None of them reads the implementation.

const (
	liveFileMarker = "PLAINTEXT-MARKER-IN-A-WORKSPACE-FILE-3b9d41"
	liveTurnMarker = "PLAINTEXT-MARKER-IN-A-TRANSCRIPT-TURN-8e27c0"
)

// liveChat is a chat started on A with something in it worth moving: a project
// tree, a file and a turn that carry markers a relay must never be able to
// read, and a few turns of conversation.
func liveChat(t *testing.T, r *liveRig) *openChat {
	t.Helper()
	seedLiveTree(t, r.work)
	if err := os.WriteFile(filepath.Join(r.work, "marker.txt"), []byte(liveFileMarker+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := r.openA()
	a.mustSay("first turn")
	a.mustSay("second turn carries " + liveTurnMarker)
	a.mustSay("third turn")
	r.durable(r.cell.ID, r.cell)
	return a
}

// liveTake continues the chat on B and waits for the files that arrive behind
// the open.
func liveTake(t *testing.T, r *liveRig) Continued {
	t.Helper()
	ctx := context.Background()
	got, err := r.continuerB().Take(ctx, r.cell.ID)
	if err != nil {
		t.Fatalf("Take on B = %v; want the chat moved", err)
	}
	// B now keeps the chat where its take put it, which is where its own door
	// looks when another computer asks it for the chat.
	r.placeChat(r.homeB, got.Taken.Cell, workspaceOf(got.Taken.Cell.Root))
	if got.Wait != nil {
		if err := got.Wait(ctx); err != nil {
			t.Fatalf("the files did not finish arriving: %v", err)
		}
	}
	return got
}

// assertMovedIntact is C1's last clause: the transcript and every file arrive
// byte-identical.
func assertMovedIntact(t *testing.T, r *liveRig, got Continued) {
	t.Helper()
	sameTree(t, "the tree on B", readTree(t, r.work), readTree(t, workspaceOf(got.Taken.Cell.Root)))
	want, _ := os.ReadFile(transcriptOf(r.cell))
	have, err := os.ReadFile(transcriptOf(got.Taken.Cell))
	if err != nil || len(want) == 0 || !bytes.Equal(have, want) {
		t.Fatalf("the transcript on B is %d bytes (%v); A's is %d", len(have), err, len(want))
	}
}

// C1: on the default relay (storage gated and off, nothing approved), a chat
// started on A is in B's directory and Home list, and B continues it while it is
// still open on A, and after A has quit but its computer still runs codeaf. The
// transcript and every file arrive byte-identical.
// C2 (here too): the relay is sent no object while it happens.
func TestLiveTransferC1TheChatMovesBetweenTwoRunningComputers(t *testing.T) {
	for _, mode := range []string{"still open on A", "A's window quit and its computer still runs"} {
		t.Run(mode, func(t *testing.T) {
			r := newLiveRig(t)
			a := liveChat(t, r)

			row, ok := rowOf(t, r.b, r.cell.ID)
			if !ok || row.Head == "" {
				t.Fatalf("B's Home list = %+v, %v; want A's chat in it", row, ok)
			}
			if row.Title != "untitled" {
				t.Fatalf("B's list names the chat %q; the record carries no title, so Home shows it as untitled", row.Title)
			}
			if mode != "still open on A" {
				if err := a.drive.Close(context.Background()); err != nil {
					t.Fatal(err)
				}
			}

			got := liveTake(t, r)
			assertMovedIntact(t, r, got)
			if n := r.wire.storeWrites(); n != 0 {
				t.Fatalf("the relay was sent %d object writes; want none", n)
			}
			from := r.sourcesOf(r.b.Device.ID())
			if len(from) == 0 || from[0].Stats().Requests == 0 {
				t.Fatalf("B asked no computer for anything: %+v", from)
			}
			if got.Taken.Cell.ID != r.cell.ID || got.Device != nameB {
				t.Fatalf("Continued = %+v; want the chat taken on %s", got, nameB)
			}
		})
	}
}

// C3: a byte tap on everything between the two computers and the relay never
// holds a plaintext marker planted in a workspace file and in a transcript turn,
// while it does hold a good deal of traffic (so the check is not vacuous).
func TestLiveTransferC3TheRelaySeesCiphertextOnly(t *testing.T) {
	r := newLiveRig(t)
	liveChat(t, r)
	got := liveTake(t, r)
	assertMovedIntact(t, r, got)

	if have, _ := os.ReadFile(filepath.Join(workspaceOf(got.Taken.Cell.Root), "marker.txt")); !strings.Contains(string(have), liveFileMarker) {
		t.Fatalf("the marker never reached B, so the check below would prove nothing: %q", have)
	}
	if log, _ := os.ReadFile(transcriptOf(got.Taken.Cell)); !strings.Contains(string(log), liveTurnMarker) {
		t.Fatal("the transcript marker never reached B, so the check below would prove nothing")
	}
	seen := r.tap.bytes()
	if len(seen) < 9000 {
		t.Fatalf("the tap saw %d bytes; a moved chat is more than that", len(seen))
	}
	for _, marker := range []string{liveFileMarker, liveTurnMarker} {
		if bytes.Contains(seen, []byte(marker)) {
			t.Fatalf("the relay's side of the wire held the plaintext %q", marker)
		}
	}
}

// C18: every write the relay received during C1 is on a list, with its route
// and size, and the list holds no object write and no record with a title or a
// folder. The list is printed: it is the owners' evidence of what the relay keeps.
func TestLiveTransferC18EverythingTheRelayKeeps(t *testing.T) {
	r := newLiveRig(t)
	// The chat holds secret files too (SPEC-3 V2): they move by the same blind
	// pipe, so the relay keeps nothing more for them than for the rest.
	a := passSecretChat(t, r)
	got := liveTake(t, r)
	assertMovedIntact(t, r, got)
	_ = a

	if n := r.wire.storeWrites(); n != 0 {
		t.Fatalf("%d object writes reached the relay", n)
	}
	if n := r.wire.storeRequests(); len(n) != 0 {
		t.Fatalf("the relay's object store was asked %d times (puts, gets and has): %v; want never", len(n), n)
	}
	if bad := r.wire.recordsWithNames(); len(bad) > 0 {
		t.Fatalf("records with a name reached the relay: %v", bad)
	}
	writes := 0
	for _, e := range r.wire.snapshot() {
		if e.Method != "GET" {
			writes++
		}
	}
	if writes == 0 {
		t.Fatal("the relay saw no write at all, so the list below proves nothing")
	}
	t.Logf("everything the relay was written to during a live move:\n  %s", strings.Join(r.wire.summary(), "\n  "))
}

// aFrameOfA is the id of one frame A keeps in its live store, and every object
// id inside it: something a door can be asked for.
func aFrameOfA(t *testing.T, r *liveRig) string {
	t.Helper()
	frames := liveFramesOf(t, r.homeA)
	if len(frames) == 0 {
		t.Fatal("A kept no frame in its live store")
	}
	return frames[0]
}

// liveObjectsOf is every object a computer's live store holds, by id, with its size.
func liveObjectsOf(t *testing.T, home string) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, id := range liveFramesOf(t, home) {
		raw, err := os.ReadFile(filepath.Join(liveStoreDir(home), "frames", id))
		mustNil(t, err)
		header, objects, err := blobstore.Decode(raw)
		mustNil(t, err)
		for i, ref := range header.Objects {
			out[ref.RID] = len(objects[i].Bytes)
		}
	}
	return out
}

// doorFor is a source that asks A's files door as the given computer would.
func doorFor(r *liveRig, id identity.Identity, dev identity.Dev) *filesdoor.Source {
	return filesdoor.NewSource(filesdoor.SourceConfig{
		Service: r.a.Relay, Identity: id, Device: dev,
		TargetID: r.a.Device.ID(), TargetName: nameA, RetryWithin: 300 * time.Millisecond,
	})
}

// C4: only your own computers. A dialer with another identity, and a computer
// that was removed, are refused before any object byte is sent; one of the
// person's own computers, in the same place, is answered.
func TestLiveTransferC4OnlyYourOwnComputers(t *testing.T) {
	r := newLiveRig(t)
	liveChat(t, r)
	ctx := context.Background()
	frame := aFrameOfA(t, r)

	own := doorFor(r, r.b.Identity, r.b.Device)
	t.Cleanup(func() { _ = own.Close() })
	if got, err := own.GetFrame(ctx, frame); err != nil || len(got) == 0 {
		t.Fatalf("one of your own computers was refused: %d bytes, %v", len(got), err)
	}

	strangerHome := t.TempDir()
	strangerID, err := identity.Ensure(strangerHome)
	mustNil(t, err)
	strangerDev, err := identity.Device(strangerHome)
	mustNil(t, err)
	stranger := doorFor(r, strangerID, strangerDev)
	t.Cleanup(func() { _ = stranger.Close() })
	if got, err := stranger.GetFrame(ctx, frame); err == nil || len(got) != 0 {
		t.Fatalf("a computer of another person was answered: %d bytes, %v", len(got), err)
	}
	if st := stranger.Stats(); st.BytesDown != 0 {
		t.Fatalf("a stranger was sent %d bytes", st.BytesDown)
	}

	mustNil(t, r.a.Dir.Revoke(ctx, r.b.Device.ID()))
	time.Sleep(3 * liveDoorRosterFor)
	removed := doorFor(r, r.b.Identity, r.b.Device)
	t.Cleanup(func() { _ = removed.Close() })
	_, err = removed.GetFrame(ctx, frame)
	var refused *filesdoor.RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("a removed computer was answered: %v", err)
	}
	if st := removed.Stats(); st.Requests != 0 || st.BytesDown > 4096 {
		t.Fatalf("a removed computer got past the greeting: %+v", st)
	}
}

// C19: a computer removed while it is fetching is cut off within one response,
// and is sent nothing further.
func TestLiveTransferC19RemovedMidStream(t *testing.T) {
	r := newLiveRig(t)
	liveChat(t, r)
	ctx := context.Background()
	frame := aFrameOfA(t, r)

	src := doorFor(r, r.b.Identity, r.b.Device)
	t.Cleanup(func() { _ = src.Close() })
	if got, err := src.GetFrame(ctx, frame); err != nil || len(got) == 0 {
		t.Fatalf("the first response: %d bytes, %v", len(got), err)
	}
	mustNil(t, r.a.Dir.Revoke(ctx, r.b.Device.ID()))
	time.Sleep(3 * liveDoorRosterFor)
	before := src.Stats().BytesDown
	_, err := src.GetFrame(ctx, frame)
	var refused *filesdoor.RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("the next response after removal = %v; want a refusal", err)
	}
	if sent := src.Stats().BytesDown - before; sent > 4096 {
		t.Fatalf("a removed computer was sent %d more bytes", sent)
	}
}

// failedTake is a take of the chat on B that is expected to fail, with the
// state B is left in.
func failedTake(t *testing.T, r *liveRig) error {
	t.Helper()
	_, err := r.continuerB().Take(context.Background(), r.cell.ID)
	if err == nil {
		t.Fatal("the take succeeded with the computer that has the chat switched off")
	}
	return err
}

// assertNothingOnB is "nothing is half-installed": B has neither the chat's
// folder nor its staging copy, and no lease.
func assertNothingOnB(t *testing.T, r *liveRig) {
	t.Helper()
	root := filepath.Join(r.rootsB, r.cell.ID)
	for _, path := range []string{root, root + handoff.StagingSuffix} {
		if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s exists on B after a take that did not happen (%v)", path, err)
		}
	}
	view, err := r.b.Dir.Cell(context.Background(), r.cell.ID)
	mustNil(t, err)
	if view.Cell.Lease.Device == r.b.Device.ID() && view.Cell.Lease.Expires > view.Now {
		t.Fatalf("B still holds the lease of a chat it could not open: %+v", view.Cell.Lease)
	}
}

// C5: when no computer that has the chat runs codeaf, B's take fails with a
// sentence that names that computer, nothing is installed on B, and the very
// same take succeeds once A runs again.
func TestLiveTransferC5TheComputerThatHasTheChatIsOff(t *testing.T) {
	r := newLiveRig(t)
	a := liveChat(t, r)
	mustNil(t, a.drive.Close(context.Background()))
	stopFilesDoor(r.homeA)

	err := failedTake(t, r)
	if !errors.Is(err, syncwords.ErrHolderOffline) {
		t.Fatalf("the take failed with %v; want the holder-offline refusal", err)
	}
	if !strings.Contains(err.Error(), nameA) {
		t.Fatalf("the refusal %q does not name %s", err, nameA)
	}
	assertNothingOnB(t, r)

	r.a.ensureFilesDoor()
	got := liveTake(t, r)
	assertMovedIntact(t, r, got)
}

// rowOnA is the chat's row as A's own Home lists it from the directory.
func rowOnA(t *testing.T, r *liveRig) chatlist.Row {
	t.Helper()
	listing, err := r.a.Dir.List(context.Background())
	mustNil(t, err)
	for _, row := range chatlist.Rows(listing, r.a.Device.ID(), func(sealed string) (string, error) { return sealed, nil }) {
		if row.Cell == r.cell.ID {
			return row
		}
	}
	t.Fatal("A's list does not show the chat")
	return chatlist.Row{}
}

// continuerBOpeningAtOnce is B's take side as the terminal and the app build it:
// the chat opens first and its files arrive behind it, which is also the road on
// which the take asks for the lease beside the fetch.
func (r *liveRig) continuerBOpeningAtOnce() *Continuer {
	r.awayFromA()
	return r.b.Continuer(r.engB, TakeOptions{DeviceName: nameB, Notify: r.wall.add, AsyncFiles: true, RootFor: func(id string) string {
		return filepath.Join(r.rootsB, id)
	}})
}

// F1: a take that fails for want of the frames leaves the chat's record as it
// was. The computer that had the chat is still the one the record names, no
// lease is left behind, and A's next open of its own chat is not offered as a
// move from B, which never had it. The hand test saw the opposite: B's take
// asked for the lease beside the fetch, the fetch failed, and giving the lease
// back left B named as the last holder.
func TestLiveTransferF1AFailedTakeLeavesTheRecordAsItWas(t *testing.T) {
	for _, tc := range []struct {
		name    string
		release bool
		atOnce  bool
	}{
		{"the holder quit its window, so the record names no holder", true, false},
		{"the holder's lease still runs but its computer is off", false, false},
		{"the chat opens at once and its files follow: the holder quit its window", true, true},
		{"the chat opens at once and its files follow: the holder's lease still runs", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newLiveRig(t)
			a := liveChat(t, r)
			if tc.release {
				mustNil(t, a.drive.Close(context.Background()))
			}
			stopFilesDoor(r.homeA)
			before, err := r.b.Dir.Cell(context.Background(), r.cell.ID)
			mustNil(t, err)
			rowBefore := rowOnA(t, r)

			taker := r.continuerB()
			if tc.atOnce {
				taker = r.continuerBOpeningAtOnce()
			}
			_, err = taker.Take(context.Background(), r.cell.ID)
			if !errors.Is(err, syncwords.ErrHolderOffline) {
				t.Fatalf("the take failed with %v; want the holder-offline refusal", err)
			}

			after, err := r.b.Dir.Cell(context.Background(), r.cell.ID)
			mustNil(t, err)
			if got, was := after.Cell.Lease.Device, before.Cell.Lease.Device; got != was || was != r.a.Device.ID() {
				t.Fatalf("the record names %q as the holder after the failed take; it named %q before and A is %q", got, was, r.a.Device.ID())
			}
			if got, was := after.Cell.Lease.Fence, before.Cell.Lease.Fence; got != was {
				t.Fatalf("the failed take left the fence at %d; it was %d", got, was)
			}
			if tc.release && (after.Cell.Lease.Expires != 0 || after.Cell.Lease.ReleasedAt != before.Cell.Lease.ReleasedAt) {
				t.Fatalf("the lease was %+v and is now %+v: a take that never opened the chat left a mark on it", before.Cell.Lease, after.Cell.Lease)
			}
			if after.Cell.Head != before.Cell.Head {
				t.Fatalf("the chat's head moved from %s to %s", before.Cell.Head, after.Cell.Head)
			}
			assertNothingOnB(t, r)

			rowAfter := rowOnA(t, r)
			if rowAfter.Mine != rowBefore.Mine || rowAfter.DeviceID != rowBefore.DeviceID || rowAfter.Status != rowBefore.Status || rowAfter.Released != rowBefore.Released {
				t.Fatalf("A's own list shows the chat as %+v after B's failed take; it was %+v: A would be offered a move from B", rowAfter, rowBefore)
			}
		})
	}
}

// withProductionDialTimings puts the dial timings a person's computers run with
// back in place for one test; the rig shortens them so the tests that only need
// "A is off" do not wait on a real laptop's clocks.
func withProductionDialTimings(t *testing.T) {
	t.Helper()
	oldWithin, oldPoll := liveRetryWithin, liveDoorLockPoll
	liveRetryWithin, liveDoorLockPoll = filesdoor.DefaultRetryWithin, filesdoor.DefaultLockPoll
	t.Cleanup(func() { liveRetryWithin, liveDoorLockPoll = oldWithin, oldPoll })
}

// F3: with no codeaf process of the holder's computer registered at the relay,
// the take ends with the holder-off sentence within a few seconds, not after the
// ten the door keeps redialling for a door that is moving between processes. The
// timings are the production ones: the hand test saw about twelve seconds of
// `opening conversation...` for a computer that was simply off.
func TestLiveTransferF3TheHolderBeingOffIsSaidQuickly(t *testing.T) {
	r := newLiveRig(t)
	a := liveChat(t, r)
	mustNil(t, a.drive.Close(context.Background()))
	stopFilesDoor(r.homeA)
	withProductionDialTimings(t)

	started := time.Now()
	err := failedTake(t, r)
	took := time.Since(started)
	if !errors.Is(err, syncwords.ErrHolderOffline) {
		t.Fatalf("the take failed with %v; want the holder-offline refusal", err)
	}
	if took > 4*time.Second {
		t.Fatalf("the holder-off sentence took %v; want it within about 4 seconds", took)
	}
	assertNothingOnB(t, r)
}

// F3, the other half: a door that is moving from one codeaf process of A to
// another is still found. The waiting process polls for the lock at the pace a
// real computer does, and the take that starts the moment the first process
// stops succeeds inside the shortened window.
func TestLiveTransferF3TheDoorMovingProcessesIsStillFoundAtProductionTimings(t *testing.T) {
	r := newLiveRig(t)
	a := liveChat(t, r)
	mustNil(t, a.drive.Close(context.Background()))
	r.b.ensureFilesDoor()
	quietRelay(r)
	withProductionDialTimings(t)

	cfg, err := r.a.filesDoorConfig()
	mustNil(t, err)
	second, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = filesdoor.Serve(second, cfg) }()
	t.Cleanup(func() { stop(); <-done })
	time.Sleep(300 * time.Millisecond)

	started := time.Now()
	stopFilesDoor(r.homeA)
	got := liveTake(t, r)
	assertMovedIntact(t, r, got)
	if took := time.Since(started); took > 4*time.Second {
		t.Fatalf("the other process took %v to serve; want under 4 seconds", took)
	}
}

// doorGate makes A's files door go quiet at a point the test chooses: once
// armed, a request waits until the door is stopped. It stands for a laptop whose
// lid closes mid-transfer.
type doorGate struct {
	blobstore.Store
	armed   atomic.Bool
	arrived chan struct{}
	once    sync.Once
}

func newDoorGate() *doorGate { return &doorGate{arrived: make(chan struct{})} }

func (g *doorGate) hold(ctx context.Context) error {
	if !g.armed.Load() {
		return nil
	}
	g.once.Do(func() { close(g.arrived) })
	<-ctx.Done()
	return ctx.Err()
}

func (g *doorGate) Has(ctx context.Context, rids []string) ([]bool, error) {
	if err := g.hold(ctx); err != nil {
		return nil, err
	}
	return g.Store.Has(ctx, rids)
}

func (g *doorGate) Get(ctx context.Context, rid string) ([]byte, error) {
	if err := g.hold(ctx); err != nil {
		return nil, err
	}
	return g.Store.Get(ctx, rid)
}

func (g *doorGate) GetMany(ctx context.Context, rids []string) ([]blobstore.Object, error) {
	if err := g.hold(ctx); err != nil {
		return nil, err
	}
	return g.Store.GetMany(ctx, rids)
}

func (g *doorGate) GetFrame(ctx context.Context, frame string) ([]byte, error) {
	if err := g.hold(ctx); err != nil {
		return nil, err
	}
	return g.Store.GetFrame(ctx, frame)
}

func (g *doorGate) Locate(ctx context.Context, rids []string) (map[string]blobstore.Location, error) {
	if err := g.hold(ctx); err != nil {
		return nil, err
	}
	return g.Store.Locate(ctx, rids)
}

// gatedA starts a live rig whose A serves its files through a gate, and a chat
// on A, and leaves the gate open.
func gatedA(t *testing.T) (*liveRig, *doorGate) {
	t.Helper()
	gate := newDoorGate()
	r := newLiveRig(t)
	old := liveDoorStore
	liveDoorStore = func(home string, s blobstore.Store) blobstore.Store {
		if home != r.homeA {
			return s
		}
		gate.Store = s
		return gate
	}
	t.Cleanup(func() { liveDoorStore = old })
	a := liveChat(t, r)
	mustNil(t, a.drive.Close(context.Background()))
	return r, gate
}

// restoreDoorOfA puts A's door back, as it was before the gate, and lets it serve.
func restoreDoorOfA(t *testing.T, r *liveRig, gate *doorGate) {
	t.Helper()
	gate.armed.Store(false)
	liveDoorStore = nil
	r.a.ensureFilesDoor()
}

// C13, first half: A goes quiet while B fetches the open state. B does not keep
// the lease, nothing is installed, and the take can be made again once A is back.
func TestLiveTransferC13ACutWhileTheOpenStateIsFetched(t *testing.T) {
	r, gate := gatedA(t)
	gate.armed.Store(true)

	failed := make(chan error, 1)
	go func() {
		_, err := r.continuerB().Take(context.Background(), r.cell.ID)
		failed <- err
	}()
	select {
	case <-gate.arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("B never asked A for anything")
	}
	stopFilesDoor(r.homeA)

	err := <-failed
	if err == nil || !errors.Is(err, syncwords.ErrHolderOffline) {
		t.Fatalf("the take B started when A was cut = %v; want the holder-offline refusal", err)
	}
	assertNothingOnB(t, r)

	restoreDoorOfA(t, r, gate)
	assertMovedIntact(t, r, liveTake(t, r))
}

// C13, second half: A goes quiet after the chat has opened on B but before the
// files arrived. B keeps the chat with files missing, and reopening it
// (RestartFiles) completes them byte-identical once A is back.
func TestLiveTransferC13ACutAfterTheChatOpened(t *testing.T) {
	r, gate := gatedA(t)
	cont := r.asyncContinuerB()
	cont.opt.OnPhase = func(p handoff.Phase) {
		// The open state is fetched when "fetch" ends; the files come after.
		if p.Name == "fetch" && !p.Ended.IsZero() {
			gate.armed.Store(true)
		}
	}

	got, err := cont.Take(context.Background(), r.cell.ID)
	if err != nil {
		t.Fatalf("the take failed when only the files were cut off: %v", err)
	}
	select {
	case <-gate.arrived:
	case <-time.After(30 * time.Second):
		t.Fatal("B never asked A for its files")
	}
	stopFilesDoor(r.homeA)
	if got.Wait == nil {
		t.Fatal("the take carries no wait for its files")
	}
	if err := got.Wait(context.Background()); err == nil {
		t.Fatal("the files arrived from a computer that was cut off")
	}
	if view, err := r.b.Dir.Cell(context.Background(), r.cell.ID); err != nil || view.Cell.Lease.Device != r.b.Device.ID() {
		t.Fatalf("B lost the chat when its files failed: %+v, %v", view.Cell.Lease, err)
	}

	// The window that was fetching is gone, as it is when a laptop is closed.
	arrival := cell.LookupWorkspaceArrival(got.Taken.Cell.Root, workspaceOf(got.Taken.Cell.Root))
	if arrival == nil {
		t.Fatal("the failed arrival is not on record")
	}
	arrival.Abandon("the computer that has this chat went away")

	restoreDoorOfA(t, r, gate)
	restarted, err := r.asyncContinuerB().RestartFiles(context.Background(), r.cell.ID)
	if err != nil || !restarted {
		t.Fatalf("RestartFiles = %v, %v; want the files started again", restarted, err)
	}
	want := readTree(t, r.work)
	waitFor(t, "the files to arrive", func() bool {
		have := readTree(t, workspaceOf(got.Taken.Cell.Root))
		if len(have) != len(want) {
			return false
		}
		for path, w := range want {
			if h, ok := have[path]; !ok || h != w {
				return false
			}
		}
		return true
	})
	// THE FILES ARE IN PLACE BEFORE THE ARRIVAL IS OVER. The tree above is whole
	// once the files are written, and the restarted stage still has its hooks (the
	// vault pull and the index rebuild, which open the vault's lock and the chat's
	// stores) and its look at the folder to finish, a second or more behind it.
	// A test that returns on the tree leaves that stage writing into folders the
	// temporary-directory cleanup is removing, which failed this test on the full
	// suite with "directory not empty". The arrival is complete only after all of
	// that is done, and it leaves the record of arrivals when it is.
	waitFor(t, "the restarted arrival to finish", func() bool {
		a := cell.LookupWorkspaceArrival(got.Taken.Cell.Root)
		return a == nil || a.Ready()
	})
}

// C6: after B writes turns, A takes the chat back while B runs and fetches only
// what it lacks. The requests and bytes A asks B for are a small part of what B
// asked A for in the first place.
func TestLiveTransferC6BackAgainFetchesOnlyWhatIsMissing(t *testing.T) {
	r := newLiveRig(t)
	a := liveChat(t, r)
	mustNil(t, a.drive.Close(context.Background()))
	took := liveTake(t, r)
	first := r.sourcesOf(r.b.Device.ID())
	if len(first) == 0 {
		t.Fatal("B asked nobody for the chat")
	}
	firstStats := first[0].Stats()

	b := r.openOn(r.b, r.engB, took.Taken.Cell, workspaceOf(took.Taken.Cell.Root), nameB)
	b.mustSay("a turn written on B")
	r.durable(r.cell.ID, took.Taken.Cell)
	mustNil(t, b.drive.Close(context.Background()))
	// What A lacks is the frames B holds that carry an object A does not.
	heldByA := liveObjectsOf(t, r.homeA)
	var lacking int64
	for _, id := range liveFramesOf(t, r.homeB) {
		raw, err := os.ReadFile(filepath.Join(liveStoreDir(r.homeB), "frames", id))
		mustNil(t, err)
		header, _, err := blobstore.Decode(raw)
		mustNil(t, err)
		for _, ref := range header.Objects {
			if _, ok := heldByA[ref.RID]; !ok {
				lacking += int64(len(raw))
				break
			}
		}
	}
	if lacking == 0 {
		t.Fatal("B holds nothing A lacks, so there is nothing to take back")
	}

	back, err := r.continuerA().Take(context.Background(), r.cell.ID)
	if err != nil {
		t.Fatalf("A could not take the chat back: %v", err)
	}
	if back.Wait != nil {
		mustNil(t, back.Wait(context.Background()))
	}
	raw, _ := os.ReadFile(transcriptOf(r.cell))
	if !strings.Contains(string(raw), "a turn written on B") {
		t.Fatal("B's turn did not reach A")
	}
	fromA := r.sourcesOf(r.a.Device.ID())
	if len(fromA) == 0 {
		t.Fatal("A asked nobody for what it lacked")
	}
	back1 := fromA[0].Stats()
	// A fetched only frames that carry something it lacked, plus the framing of
	// each response; a frame it already held coming through the door again
	// would show as a good deal more than this.
	if allowed := lacking + int64(back1.Requests)*512; back1.BytesDown > allowed {
		t.Fatalf("taking back moved %d bytes but A lacked only %d (%d requests)", back1.BytesDown, lacking, back1.Requests)
	}
	if back1.Requests > firstStats.Requests {
		t.Fatalf("taking back made %d requests; the first move made %d", back1.Requests, firstStats.Requests)
	}
	t.Logf("first move: %d requests, %d bytes; taking back: %d requests, %d bytes, A lacked %d", firstStats.Requests, firstStats.BytesDown, back1.Requests, back1.BytesDown, lacking)
}

// C20: a file changed on B while B holds the chat is in A's folder after A
// takes the chat back, and stays there when B takes the chat again. Found in a
// hand test on the hosted relay with storage off: A's transcript had every turn
// B ran and the "Everything as you left it" line, but a README edited on B from
// a shell was still the original on A, and when B took the chat from A once more
// the edit was gone from both computers.
// The edit is made outside any tool call, then B runs two more turns, with a
// tool call in them or as plain conversation; A takes the chat back while B
// still runs, with A's window left open (as the hand test did) or not.
func TestLiveTransferC20AFileChangedOnBSurvivesTheRoundTrip(t *testing.T) {
	for _, c := range []struct {
		name         string
		windowOnA    bool // A's window stays open while B has the chat
		toolsInTurns bool
		staleHome    bool // A takes from a Home list read before B's last turns
		storage      bool // sync storage is approved: the road that was there before live transfer
	}{
		{"A's window quit, turns that run a tool", false, true, false, false},
		{"A's window open, plain turns", true, false, false, false},
		{"Home on A is a turn behind", false, true, true, false},
		{"storage approved, A's window open, plain turns", true, false, false, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := newLiveRig(t)
			if c.storage {
				r.approveStorage()
			}
			// The hand test's project was a git repository, and the engine reads one differently.
			if _, err := exec.LookPath("git"); err == nil {
				if out, err := exec.Command("git", "-C", r.work, "init", "-q").CombinedOutput(); err != nil {
					t.Fatalf("git init: %v: %s", err, out)
				}
			}
			a := liveChat(t, r)
			if !c.windowOnA {
				mustNil(t, a.drive.Close(context.Background()))
			}
			took := liveTake(t, r)
			workB := workspaceOf(took.Taken.Cell.Root)

			b := r.openSeated(r.b, r.engB, took.Taken.Cell, workB, nameB, nil, cellstore.WithPipeline(), cellstore.WithWarmCapture())
			headBefore := headOf(t, took.Taken.Cell)
			var staleListing directory.Listing
			if c.staleHome {
				// B's first turn is the last thing Home on A has seen.
				b.mustSay("a turn Home has seen")
				mustNil(t, executor.Drain(context.Background(), b.seat))
				waitFor(t, "the directory to hold B's turn", func() bool {
					head, err := cellstore.Head(took.Taken.Cell)
					return err == nil && head != nil && r.directoryHead(r.cell.ID) == head.Turn.ID
				})
				var err error
				staleListing, err = r.a.Dir.List(context.Background())
				mustNil(t, err)
				headBefore = headOf(t, took.Taken.Cell)
			}
			// A shell edit outside any tool call, then two turns.
			f, err := os.OpenFile(filepath.Join(workB, "README.md"), os.O_WRONLY|os.O_APPEND, 0)
			mustNil(t, err)
			_, err = f.WriteString("edited on B\n")
			mustNil(t, err)
			mustNil(t, f.Close())
			for _, text := range []string{"first turn on B", "second turn on B"} {
				if c.toolsInTurns {
					b.mustSay(text)
				} else {
					appendTo(t, transcriptOf(b.cell), fmt.Sprintf(`{"type":"message","role":"assistant","content":%q}`+"\n", text))
				}
				mustNil(t, executor.Drain(context.Background(), b.seat))
			}
			waitFor(t, "B's turns to reach the directory", func() bool {
				head, err := cellstore.Head(took.Taken.Cell)
				return err == nil && head != nil && head.Turn.ID != headBefore && r.directoryHead(r.cell.ID) == head.Turn.ID
			})
			edit := func(where string) {
				t.Helper()
				if got, _ := os.ReadFile(filepath.Join(where, "README.md")); !strings.HasSuffix(string(got), "edited on B\n") {
					t.Fatalf("README.md in %s = %q; want B's edit in it", where, got)
				}
			}
			if got, _ := os.ReadFile(filepath.Join(r.work, "README.md")); strings.HasSuffix(string(got), "edited on B\n") {
				t.Fatal("A's README already has B's edit before A took the chat back")
			}
			if c.staleHome {
				// Home on A was listed before B's last turn reached the directory;
				// the take uses that record, as a take from Home does.
				r.a.RememberViews(staleListing)
			}

			back, err := r.continuerA().Take(context.Background(), r.cell.ID)
			if err != nil {
				t.Fatalf("A could not take the chat back: %v", err)
			}
			if back.Wait != nil {
				mustNil(t, back.Wait(context.Background()))
			}
			if raw, _ := os.ReadFile(transcriptOf(r.cell)); !strings.Contains(string(raw), "second turn on B") {
				t.Fatal("B's turns did not reach A")
			}
			sameTree(t, "the tree on A", readTree(t, workB), readTree(t, r.work))
			edit(r.work)

			// A opens the chat it took back and the next move goes to B again.
			again := r.openOn(r.a, r.engine, back.Taken.Cell, r.work, nameA)
			again.mustSay("a turn back on A")
			waitFor(t, "A's turn to reach the directory", func() bool {
				head, err := cellstore.Head(back.Taken.Cell)
				return err == nil && head != nil && r.directoryHead(r.cell.ID) == head.Turn.ID
			})
			second, err := r.continuerB().Take(context.Background(), r.cell.ID)
			if err != nil {
				t.Fatalf("B could not take the chat again: %v", err)
			}
			if second.Wait != nil {
				mustNil(t, second.Wait(context.Background()))
			}
			edit(workspaceOf(second.Taken.Cell.Root))
			edit(r.work)
		})
	}
}

// C21: a file B changed by hand and never saved in a turn is not thrown away
// when B takes the chat again. A takes the chat back while the edit sits only in
// B's folder, so A cannot have it; B's next take must keep it, in B's folder or
// as turns kept apart, as it does for any edit that was never sent. The hand test
// lost it: B's folder came back with the original file and nothing was kept.
// The same scene runs on the storage road, where nothing here is new, so a
// difference between the two is the change's doing.
func TestLiveTransferC21AnEditNeverSavedIsKeptWhenBTakesTheChatAgain(t *testing.T) {
	for _, road := range []string{"live transfer", "storage approved"} {
		t.Run(road, func(t *testing.T) {
			r := newLiveRig(t)
			if road == "storage approved" {
				r.approveStorage()
			}
			a := liveChat(t, r)
			mustNil(t, a.drive.Close(context.Background()))
			took := liveTake(t, r)
			workB := workspaceOf(took.Taken.Cell.Root)
			b := r.openSeated(r.b, r.engB, took.Taken.Cell, workB, nameB, nil, cellstore.WithPipeline(), cellstore.WithWarmCapture())
			b.mustSay("a turn on B")
			mustNil(t, executor.Drain(context.Background(), b.seat))
			waitFor(t, "B's turn to reach the directory", func() bool {
				head, err := cellstore.Head(took.Taken.Cell)
				return err == nil && head != nil && r.directoryHead(r.cell.ID) == head.Turn.ID
			})

			// The edit is made by hand, after B's last seal, and nothing seals it.
			f, err := os.OpenFile(filepath.Join(workB, "README.md"), os.O_WRONLY|os.O_APPEND, 0)
			mustNil(t, err)
			_, err = f.WriteString("edited on B\n")
			mustNil(t, err)
			mustNil(t, f.Close())

			back, err := r.continuerA().Take(context.Background(), r.cell.ID)
			if err != nil {
				t.Fatalf("A could not take the chat back: %v", err)
			}
			if back.Wait != nil {
				mustNil(t, back.Wait(context.Background()))
			}
			again, err := r.continuerB().Take(context.Background(), r.cell.ID)
			if err != nil {
				t.Fatalf("B could not take the chat again: %v", err)
			}
			if again.Wait != nil {
				mustNil(t, again.Wait(context.Background()))
			}
			kept := uint32(0)
			if again.KeptBranch != nil {
				_, kept = again.KeptBranch()
			}
			got, _ := os.ReadFile(filepath.Join(workspaceOf(again.Taken.Cell.Root), "README.md"))
			t.Logf("%s: B's README after taking again = %q; kept turns = %d", road, got, kept)
			if !strings.HasSuffix(string(got), "edited on B\n") && kept == 0 && again.KeptTurns == 0 {
				t.Fatalf("B's edit was thrown away: README = %q and nothing was kept", got)
			}
		})
	}
}

// C17: with three computers, C takes a chat from B while A, which had the chat
// first, is off. Every object comes from B (its own publishes and the copies it
// kept of what A sent it), and A is never asked.
func TestLiveTransferC17ThreeComputers(t *testing.T) {
	r := newLiveRig(t)
	a := liveChat(t, r)
	mustNil(t, a.drive.Close(context.Background()))
	took := liveTake(t, r)
	_ = took
	r.b.ensureFilesDoor()
	stopFilesDoor(r.homeA)

	homeC := t.TempDir()
	_, err := identity.Adopt(homeC, r.a.Identity, false)
	mustNil(t, err)
	c := openSync(t, homeC)
	mustNil(t, SaveStorage(homeC, c.Relay, c.Identity.ID(), "off"))
	ctx := context.Background()
	mustNil(t, c.putDevice(ctx, "phone"))
	if _, err := c.Dir.List(ctx); err != nil {
		t.Fatal(err)
	}
	if !c.OthersPaired() {
		t.Fatal("C does not know of the other computers")
	}
	var dialedByC []string
	var mu sync.Mutex
	old := liveSourceHook
	liveSourceHook = func(from, to string, s *filesdoor.Source) {
		if old != nil {
			old(from, to, s)
		}
		if from == c.Device.ID() {
			mu.Lock()
			dialedByC = append(dialedByC, to)
			mu.Unlock()
		}
	}
	t.Cleanup(func() { liveSourceHook = old })

	engC := cellstore.EngineFor("")
	engC.DataRoot, engC.Binary, engC.Transport = t.TempDir(), r.bin, cellstore.Spawn{Binary: r.bin}
	rootsC := t.TempDir()
	got, err := c.Continuer(engC, TakeOptions{DeviceName: "phone", RootFor: func(id string) string { return filepath.Join(rootsC, id) }}).Take(ctx, r.cell.ID)
	if err != nil {
		t.Fatalf("C could not take the chat from B while A is off: %v", err)
	}
	if got.Wait != nil {
		mustNil(t, got.Wait(ctx))
	}
	sameTree(t, "the tree on C", readTree(t, r.work), readTree(t, workspaceOf(got.Taken.Cell.Root)))
	mu.Lock()
	defer mu.Unlock()
	if len(dialedByC) == 0 {
		t.Fatal("C asked no computer for the chat")
	}
	for _, to := range dialedByC {
		if to != r.b.Device.ID() {
			t.Fatalf("C asked %s, which is not B", to)
		}
	}
}

// quietRelay waits until the doors that were just started have registered, so a
// count of registrations taken after it is a count of new ones.
func quietRelay(r *liveRig) {
	last := -1
	for stable := 0; stable < 3; {
		n := r.wire.registrations()
		if n == last {
			stable++
		} else {
			stable, last = 0, n
		}
		time.Sleep(60 * time.Millisecond)
	}
}

// C15: with two codeaf processes on A, the one holding the files-door lock
// stopping lets the other serve within a few seconds, and a take on B that
// starts at that moment still succeeds within its retry window. The waiting
// process makes no request to the relay while it waits.
func TestLiveTransferC15TheDoorChangesProcess(t *testing.T) {
	r := newLiveRig(t)
	a := liveChat(t, r)
	mustNil(t, a.drive.Close(context.Background()))
	r.b.ensureFilesDoor()
	quietRelay(r)

	cfg, err := r.a.filesDoorConfig()
	mustNil(t, err)
	second, stop := context.WithCancel(context.Background())
	done := make(chan struct{})
	registered := r.wire.registrations()
	go func() { defer close(done); _ = filesdoor.Serve(second, cfg) }()
	t.Cleanup(func() { stop(); <-done })
	time.Sleep(300 * time.Millisecond)
	if n := r.wire.registrations(); n != registered {
		t.Fatalf("the second process on A made %d requests to the relay while the first held the door", n-registered)
	}

	started := time.Now()
	stopFilesDoor(r.homeA)
	got := liveTake(t, r)
	assertMovedIntact(t, r, got)
	if took := time.Since(started); took > 5*time.Second {
		t.Fatalf("the other process took %v to serve; want about 5 seconds at most", took)
	}
}

// C16: one take of a chat of 2,500 files makes at most one dial to each
// computer it asks, and no new registration with the relay.
func TestLiveTransferC16TheRequestBudgetOfOneTake(t *testing.T) {
	r := newLiveRig(t)
	seedLiveTree(t, r.work)
	for i := 0; i < 2500; i++ {
		path := filepath.Join(r.work, "many", fmt.Sprintf("d%02d", i%50), fmt.Sprintf("file-%04d.txt", i))
		mustNil(t, os.MkdirAll(filepath.Dir(path), 0o755))
		mustNil(t, os.WriteFile(path, []byte(fmt.Sprintf("file number %d of the many\n", i)), 0o644))
	}
	a := r.openA()
	a.mustSay("a chat with many files")
	r.durable(r.cell.ID, r.cell)
	mustNil(t, a.drive.Close(context.Background()))
	r.b.ensureFilesDoor()
	quietRelay(r)

	dials, registrations := r.wire.dials(), r.wire.registrations()
	got := liveTake(t, r)
	sameTree(t, "the tree on B", readTree(t, r.work), readTree(t, workspaceOf(got.Taken.Cell.Root)))
	sources := r.sourcesOf(r.b.Device.ID())
	if len(sources) != 1 {
		t.Fatalf("B dialled %d computers; want 1", len(sources))
	}
	if st := sources[0].Stats(); st.Dials != 1 {
		t.Fatalf("B dialled A %d times for one take; want 1 (%+v)", st.Dials, st)
	}
	if n := r.wire.dials() - dials; n > 1 {
		t.Fatalf("the relay saw %d dials for one take; want at most 1", n)
	}
	if n := r.wire.registrations() - registrations; n != 0 {
		t.Fatalf("the take made %d new registrations with the relay", n)
	}
	t.Logf("2,500 files: %+v", sources[0].Stats())
}

// C10: the chat shows before its files, with the existing progress line. The
// phases line of the take says shown= first and files= later, and the progress
// reports count the bytes that came through the other computer.
func TestLiveTransferC10TheChatShowsBeforeItsFiles(t *testing.T) {
	var logged strings.Builder
	var logMu sync.Mutex
	log.SetOutput(writerFunc(func(p []byte) (int, error) { logMu.Lock(); defer logMu.Unlock(); return logged.Write(p) }))
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	r := newLiveRig(t)
	a := liveChat(t, r)
	mustNil(t, a.drive.Close(context.Background()))
	cont := r.asyncContinuerB()
	var progress []MoveProgress
	var pmu sync.Mutex
	cont.opt.OnMove = func(p MoveProgress) { pmu.Lock(); progress = append(progress, p); pmu.Unlock() }
	got, err := cont.Take(context.Background(), r.cell.ID)
	mustNil(t, err)
	if got.Wait != nil {
		mustNil(t, got.Wait(context.Background()))
	}

	logMu.Lock()
	text := logged.String()
	logMu.Unlock()
	m := regexp.MustCompile(`continue ` + r.cell.ID + ` phases .* shown=(\S+) files=(\S+) total=`).FindStringSubmatch(text)
	if m == nil {
		t.Fatalf("no phases line for the take in:\n%s", text)
	}
	shown, err1 := time.ParseDuration(m[1])
	files, err2 := time.ParseDuration(m[2])
	if err1 != nil || err2 != nil || shown <= 0 || files < shown {
		t.Fatalf("shown=%s files=%s; the chat must show first and its files after", m[1], m[2])
	}
	pmu.Lock()
	defer pmu.Unlock()
	var moved int64
	steps := map[string]bool{}
	for _, p := range progress {
		moved = max(moved, p.Done)
		steps[p.Step] = true
	}
	if moved == 0 || !steps[MoveDone] {
		t.Fatalf("the progress line heard %d bytes and steps %v; want bytes counted and a done", moved, steps)
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// C9: a chat of at least 100 MB moves with one bounded response in flight per
// stream. At the door's client the request size, the response size and the number
// of requests outstanding at once never exceed the stated bounds.
func TestLiveTransferC9ABigChatMovesInBoundedResponses(t *testing.T) {
	if testing.Short() {
		t.Skip("moves 100 MB")
	}
	r := newLiveRig(t)
	seedLiveTree(t, r.work)
	big := make([]byte, 1<<20)
	for i := 0; i < 100; i++ {
		// Distinct and not compressible, so the chat really is 100 MB on the wire.
		seed := uint32(i*2654435761 + 1)
		for j := range big {
			seed = seed*1664525 + 1013904223
			big[j] = byte(seed >> 24)
		}
		mustNil(t, os.WriteFile(filepath.Join(r.work, fmt.Sprintf("big-%03d.bin", i)), big, 0o644))
	}
	a := r.openA()
	a.mustSay("a chat of 100 MB")
	r.durable(r.cell.ID, r.cell)
	mustNil(t, a.drive.Close(context.Background()))

	got := liveTake(t, r)
	sameTree(t, "the tree on B", readTree(t, r.work), readTree(t, workspaceOf(got.Taken.Cell.Root)))
	sources := r.sourcesOf(r.b.Device.ID())
	if len(sources) != 1 {
		t.Fatalf("B dialled %d computers", len(sources))
	}
	st := sources[0].Stats()
	if st.BytesDown < 100<<20 {
		t.Fatalf("only %d bytes came through the door; the chat is 100 MB", st.BytesDown)
	}
	if st.MaxInFlight != 1 {
		t.Fatalf("%d requests were outstanding at once on one stream; want 1", st.MaxInFlight)
	}
	if st.MaxResponseBytes > filesdoor.MaxResponseBytes+filesdoor.ResponseSlack || st.MaxRequestBytes > filesdoor.MaxRequestBytes {
		t.Fatalf("a message passed its bound: %+v", st)
	}
	t.Logf("100 MB chat: %+v", st)
}

// C7: where live transfer does not apply nothing about it is on the path.
// With storage approved, or on a relay that does not gate storage, a chat moves
// through the relay as it always did: no door is registered, none is dialled,
// the live store is never made, and the relay is sent the chat's objects.
func TestLiveTransferC7UnchangedWhereItDoesNotApply(t *testing.T) {
	for _, mode := range []string{"storage approved", "a relay that does not gate storage"} {
		t.Run(mode, func(t *testing.T) {
			r := newLiveRig(t)
			if mode == "storage approved" {
				r.approveStorage()
			} else {
				r.wire.allowStore.Store(true)
				for _, one := range []struct {
					home string
					s    *Sync
				}{{r.homeA, r.a}, {r.homeB, r.b}} {
					mustNil(t, SaveStorage(one.home, one.s.Relay, one.s.Identity.ID(), ""))
				}
			}
			dials, registrations := r.wire.dials(), r.wire.registrations()
			a := liveChat(t, r)
			mustNil(t, a.drive.Close(context.Background()))
			got := liveTake(t, r)
			assertMovedIntact(t, r, got)
			if n := r.wire.dials() - dials; n != 0 {
				t.Fatalf("%d door dials where live transfer does not apply", n)
			}
			if n := r.wire.registrations() - registrations; n != 0 {
				t.Fatalf("%d door registrations where live transfer does not apply", n)
			}
			if len(r.sourcesOf(r.b.Device.ID())) != 0 {
				t.Fatal("the take asked a computer for a chat the relay holds")
			}
			if r.wire.storeWrites() == 0 {
				t.Fatal("the relay was sent no object: the chat did not go through it")
			}
			if _, err := os.Stat(liveStoreDir(r.homeA)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("a live store was made where live transfer does not apply (%v)", err)
			}
		})
	}
}

// C8: CODEAF_PASSTHROUGH=off restores today's storage-off behaviour exactly: a
// chat is not shared, no door is registered or dialled, no live store is made,
// and a take of a chat that only a live computer holds says why in a sentence.
func TestLiveTransferC8TheOffSwitch(t *testing.T) {
	r := newLiveRig(t)
	a := liveChat(t, r)
	mustNil(t, a.drive.Close(context.Background()))

	t.Setenv(LiveTransferVar, "off")
	// The chat is in the directory, shared while live transfer was on. A
	// computer with the switch off cannot fetch it, and says so.
	_, err := r.continuerB().Take(context.Background(), r.cell.ID)
	if !errors.Is(err, syncwords.ErrLiveTransferOff) {
		t.Fatalf("a take with the switch off = %v; want the live-transfer-off sentence", err)
	}
	assertNothingOnB(t, r)

	// A new chat on A is not shared at all: today's behaviour.
	other, err := cell.CreateIn(t.TempDir(), cell.Options{Class: cell.HostBound})
	mustNil(t, err)
	_, err = r.a.Drive(context.Background(), r.engine, other, DriveOptions{DeviceName: nameA})
	if !errors.Is(err, wireauth.ErrStorageOff) {
		t.Fatalf("Drive with the switch off = %v; want today's storage-off refusal", err)
	}
}

// C8, after a chat was shared live: the switch is off on a computer that has
// already shared a chat, and the marker that says its frames wait for the relay
// does not keep live transfer going there. Found in a hand test: the taker had
// shared a chat the hour before, and with CODEAF_PASSTHROUGH=off it still
// fetched the other computer's newest turns through that computer's door.
func TestLiveTransferC8TheOffSwitchWinsOnAComputerThatSharedAChat(t *testing.T) {
	ctx := context.Background()
	r := newLiveRig(t)
	a := liveChat(t, r)
	mustNil(t, a.drive.Close(ctx))
	took := liveTake(t, r)
	// B has the chat and shares it in turn, so both computers carry the marker.
	b := r.openOn(r.b, r.engB, took.Taken.Cell, workspaceOf(took.Taken.Cell.Root), nameB)
	b.mustSay("a turn written on B")
	r.durable(r.cell.ID, took.Taken.Cell)
	if !livePending(r.homeA) || !livePending(r.homeB) {
		t.Fatalf("the pending marker is missing (A %v, B %v)", livePending(r.homeA), livePending(r.homeB))
	}

	// The taker has the switch off: A takes the chat back and is refused in
	// words, and makes no request of B's door.
	t.Setenv(LiveTransferVar, "off")
	dials, dialed := r.wire.dials(), len(r.sourcesOf(r.a.Device.ID()))
	_, err := r.continuerA().Take(ctx, r.cell.ID)
	if !errors.Is(err, syncwords.ErrLiveTransferOff) {
		t.Fatalf("a take with the switch off on a computer that shared a chat = %v; want the live-transfer-off sentence", err)
	}
	if n := r.wire.dials() - dials; n != 0 {
		t.Fatalf("the take made %d dials with the switch off", n)
	}
	if n := len(r.sourcesOf(r.a.Device.ID())) - dialed; n != 0 {
		t.Fatalf("the take opened %d tunnels to a door with the switch off", n)
	}
	if bare := r.a.chatScope("probe"); bare.release != nil {
		t.Fatal("the chat scope of a computer with the switch off is the live composite")
	}

	// The holder has the switch off: its door does not start again, and a
	// taker that has the switch on is told the computer is not on.
	stopFilesDoor(r.homeB)
	registrations := r.wire.registrations()
	r.b.ensureFilesDoor()
	stop := StartLiveTransfer(ctx, r.homeB)
	time.Sleep(10 * liveDoorLockPoll)
	stop()
	filesDoors.Lock()
	_, running := filesDoors.runs[r.homeB]
	filesDoors.Unlock()
	if running || r.wire.registrations() != registrations {
		t.Fatalf("the files door of a computer with the switch off is up (running %v, %d new registrations)", running, r.wire.registrations()-registrations)
	}
	t.Setenv(LiveTransferVar, "")
	if err := func() error { _, err := r.continuerA().Take(ctx, r.cell.ID); return err }(); !errors.Is(err, syncwords.ErrHolderOffline) {
		t.Fatalf("a take while the holder's door is off = %v; want the holder-offline sentence", err)
	}
}

// The surfaces that say why a chat did not sync ask the home alone whether live
// transfer is carrying chats, without a request: it answers yes while storage is
// closed and another computer is paired, and no once the switch is off or storage
// is approved, which is when the old sentence is the true one.
func TestLiveTransferForHomeAnswersFromLocalFiles(t *testing.T) {
	r := newLiveRig(t)
	if !LiveTransferForHome(r.homeA) || !LiveTransferForHome(r.homeB) {
		t.Fatal("live transfer is not reported as carrying chats on two paired computers with storage closed")
	}
	t.Run("the switch is off", func(t *testing.T) {
		t.Setenv(LiveTransferVar, "off")
		if LiveTransferForHome(r.homeA) {
			t.Fatal("live transfer is reported as carrying chats with the switch off")
		}
	})
	r.approveStorage()
	if LiveTransferForHome(r.homeA) {
		t.Fatal("live transfer is reported as carrying chats once storage is approved")
	}
	binding := deviceBindingFor(r.b.Relay, r.b.Identity, r.b.Device)
	mustNil(t, os.Remove(othersPath(r.homeB, binding)))
	if LiveTransferForHome(r.homeB) {
		t.Fatal("live transfer is reported as carrying chats on a computer with nobody to send to")
	}
}

// Rule 1a of the design: a computer with nobody to send a chat to behaves
// exactly as it does today and never copies its chats into the live store.
func TestLiveTransferUnpairedComputerStaysAsToday(t *testing.T) {
	r := newLiveRig(t)
	binding := deviceBindingFor(r.a.Relay, r.a.Identity, r.a.Device)
	mustNil(t, os.Remove(othersPath(r.homeA, binding)))
	if r.a.OthersPaired() || r.a.LiveTransfer() {
		t.Fatal("the rig could not make A a computer with no other")
	}
	_, err := r.a.Drive(context.Background(), r.engine, r.cell, DriveOptions{DeviceName: nameA})
	if !errors.Is(err, wireauth.ErrStorageOff) {
		t.Fatalf("Drive on a computer with no other = %v; want today's storage-off refusal", err)
	}
	if _, err := os.Stat(liveStoreDir(r.homeA)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("an unpaired computer made a live store (%v)", err)
	}
}

// C14: chats that were shared only live, then storage is approved. After the
// backfill pass the relay holds every frame, and a take with A off succeeds from
// the relay without dialling anyone.
func TestLiveTransferC14ApprovalLater(t *testing.T) {
	r := newLiveRig(t)
	a := liveChat(t, r)
	mustNil(t, a.drive.Close(context.Background()))
	ctx := context.Background()
	frames := liveFramesOf(t, r.homeA)
	if len(frames) == 0 || !livePending(r.homeA) {
		t.Fatalf("A has %d live frames and pending=%v before approval", len(frames), livePending(r.homeA))
	}

	r.approveStorage()
	mustNil(t, r.a.BackfillLive(ctx))
	disk, err := r.a.liveDisk()
	mustNil(t, err)
	for _, id := range frames {
		frame, err := disk.GetFrame(ctx, id)
		mustNil(t, err)
		header, _, err := blobstore.Decode(frame)
		mustNil(t, err)
		rids := make([]string, len(header.Objects))
		for i, ref := range header.Objects {
			rids[i] = ref.RID
		}
		have, err := r.b.Store.Has(ctx, rids)
		mustNil(t, err)
		for i, held := range have {
			if !held {
				t.Fatalf("the relay lacks object %s of frame %s after the backfill", rids[i], id)
			}
		}
	}
	if livePending(r.homeA) {
		t.Fatal("the backfill left its marker behind")
	}

	stopFilesDoors()
	dialsBefore := r.wire.dials()
	got := liveTake(t, r)
	assertMovedIntact(t, r, got)
	if n := r.wire.dials() - dialsBefore; n != 0 {
		t.Fatalf("the take dialled %d computers though the relay holds the chat", n)
	}
}

// C14, by itself: the process that shared chats live notices the approval and
// sends them, without anyone asking.
func TestLiveTransferC14TheBackfillRunsByItself(t *testing.T) {
	r := newLiveRig(t)
	a := liveChat(t, r)
	mustNil(t, a.drive.Close(context.Background()))
	stop := StartLiveTransfer(context.Background(), r.homeA)
	t.Cleanup(stop)

	r.approveStorage()
	waitFor(t, "the live frames to reach the relay", func() bool { return !livePending(r.homeA) && r.wire.storeWrites() > 0 })
}
