package syncsetup

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/chatlist"
	"github.com/Agent-Field/codeaf/internal/filesdoor"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	relaypkg "github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// These tests are written from the contract for turning remote control off
// while codeaf keeps running, not from the code that implements it:
//
//	C1  With remote control on and a live view open, turning it off refuses any
//	    NEW live view at once: the next one is refused before any chat byte.
//	C2  Within one watch tick after it goes off, every live view already open is
//	    closed and the door no longer serves remote control. If live transfer
//	    still needs the files door, the door keeps running for that and only the
//	    live handlers go away.
//	C3  With remote control never turned on, no door is started, stopped or
//	    restarted that was not before; turning it ON still takes effect at the
//	    next tick.
//	C4  A stopped run has been waited on: its done channel is closed when the
//	    tick returns, so nothing of it is left running.
//
// A "tick" is one call of ensureFilesDoor, which is all the 30 second watch loop
// of StartLiveTransfer does for the door.

// rawLiveView is one live channel a viewer opened and said hello on. ended gets
// the error its read finished with, which is how a closed view looks from the
// viewer's side; welcomed is closed once the host's first bytes arrived.
type rawLiveView struct {
	rwc      io.ReadWriteCloser
	welcomed chan struct{}
	ended    chan error
}

func openRawLiveView(t *testing.T, link *livedoor.Link, chat string) *rawLiveView {
	t.Helper()
	open := link.Opener(chat)
	var rwc io.ReadWriteCloser
	waitFor(t, "the viewer to open a live channel", func() bool {
		c, err := open()
		if err != nil {
			return false
		}
		rwc = c
		return true
	})
	t.Cleanup(func() { _ = rwc.Close() })

	payload, err := json.Marshal(remote.Hello{Version: remote.Version, Join: true})
	mustNil(t, err)
	frame, err := json.Marshal(remote.Frame{Kind: "hello", Payload: payload})
	mustNil(t, err)
	if _, err := rwc.Write(append(frame, '\n')); err != nil {
		t.Fatalf("writing the viewer's hello: %v", err)
	}

	view := &rawLiveView{rwc: rwc, welcomed: make(chan struct{}), ended: make(chan error, 1)}
	guard.Go("syncsetup.test.rawLiveView", func() {
		buf := make([]byte, 4096)
		first := true
		for {
			n, err := rwc.Read(buf)
			if n > 0 && first {
				first = false
				close(view.welcomed)
			}
			if err != nil {
				view.ended <- err
				return
			}
		}
	})
	waitFor(t, "the host's first bytes on the live view", func() bool {
		select {
		case <-view.welcomed:
			return true
		default:
			return false
		}
	})
	return view
}

// requireViewEnded fails unless the viewer's read finished, which is the stream
// of an open live view ending.
func requireViewEnded(t *testing.T, view *rawLiveView, what string) {
	t.Helper()
	select {
	case <-view.ended:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: the live view's stream did not end", what)
	}
}

// requireDone fails unless the run's done channel is already closed: the tick
// that stopped it waited for it.
func requireDone(t *testing.T, run *doorRun, what string) {
	t.Helper()
	select {
	case <-run.done:
	default:
		t.Fatalf("%s: the stopped door run was not waited on (its done channel is still open)", what)
	}
}

func doorRunOf(r *liveRig) *doorRun {
	filesDoors.Lock()
	defer filesDoors.Unlock()
	return filesDoors.runs[r.a.Home]
}

// dialLiveUntil keeps dialing the host's live door until ok accepts what it said.
func dialLiveUntil(t *testing.T, r *liveRig, what string, ok func(error) bool) {
	t.Helper()
	waitFor(t, what, func() bool {
		tun, err := filesdoor.DialLive(context.Background(), r.a.Relay, r.b.Identity, r.b.Device, r.a.Device.ID())
		if err == nil {
			_ = tun.Close()
		}
		return ok(err)
	})
}

// liveViewOnHost builds the scene the C1 and C2 tests start from: remote control
// on, a chat held on the host, the host's door up, and one viewer with its live
// channel open and welcomed.
func liveViewOnHost(t *testing.T, r *liveRig) (*remote.Session, *livedoor.Link, *rawLiveView) {
	t.Helper()
	allowViewerDrive(t, r)
	sess, _ := hostHeldChat(t, r, r.cell.ID, &stubLiveAgent{title: "Switched Off Chat"})
	r.a.ensureFilesDoor()
	link := r.b.LiveLink(r.a.Device.ID(), nameA, func() bool { return true }, nil)
	t.Cleanup(link.Close)
	view := openRawLiveView(t, link, r.cell.ID)
	waitFor(t, "the host to count the viewer's tunnel", func() bool {
		return livedoor.DefaultRegistry(r.a.Home).Count(r.b.Device.ID()) == 1
	})
	return sess, link, view
}

// TestC1TurningRemoteControlOffRefusesANewLiveViewAtOnce is C1: with a live view
// open, remote control goes off and NO tick runs; a second viewer's attempt is
// refused as the host having remote control off, before the host attaches it to
// the chat.
func TestC1TurningRemoteControlOffRefusesANewLiveViewAtOnce(t *testing.T) {
	t.Setenv("CODEAF_REMOTE_CONTROL", "on")
	r := newLiveRig(t)
	sess, _, _ := liveViewOnHost(t, r)
	attachedBefore := sess.Attached()
	if attachedBefore == 0 {
		t.Fatal("the first viewer is not attached to the host's chat, so this test proves nothing")
	}

	t.Setenv("CODEAF_REMOTE_CONTROL", "off")

	second := r.b.LiveLink(r.a.Device.ID(), nameA, func() bool { return true }, nil)
	t.Cleanup(second.Close)
	rwc, err := second.Opener(r.cell.ID)()
	if err == nil {
		_ = rwc.Close()
		t.Fatal("a new live view opened with remote control off")
	}
	// THE REFUSAL SAYS THE SWITCH IS OFF, not that this computer's permit is empty
	// (which would send the person to /devices): the host's answer to the dial
	// carries it, and the view that follows once it is back on is not refused.
	if reason, ok := livedoor.ReasonOf(err); !ok || reason != livedoor.ReasonRemoteControlOff {
		t.Fatalf("the refused viewer read %v (reason %q), want the host's %q", err, reason, livedoor.ReasonRemoteControlOff)
	}
	if got := sess.Attached(); got != attachedBefore {
		t.Fatalf("the host's chat has %d viewers after a refused attempt, want %d: the refused viewer was attached", got, attachedBefore)
	}
	if st := second.State(); st.Kind != livedoor.Revoked || st.Reason != livedoor.ReasonRemoteControlOff {
		t.Fatalf("the refused viewer's link is %v/%q, want revoked/%s", st.Kind, st.Reason, livedoor.ReasonRemoteControlOff)
	}
}

// TestC2TurningRemoteControlOffEndsOpenViewsAndTheDoorAtTheNextTick is C2 for a
// computer where only remote control needs the files door (live transfer off):
// the open view's stream ends, the door run is gone, and it was waited on (C4).
func TestC2TurningRemoteControlOffEndsOpenViewsAndTheDoorAtTheNextTick(t *testing.T) {
	r := newLiveRig(t)
	t.Setenv(LiveTransferVar, "off")
	t.Setenv("CODEAF_REMOTE_CONTROL", "on")
	if r.a.liveCarries() {
		t.Fatal("live transfer carries chats with CODEAF_PASSTHROUGH=off, so remote control is not the only reason for the door")
	}
	_, _, view := liveViewOnHost(t, r)
	run := doorRunOf(r)
	if run == nil || !run.remoteControl {
		t.Fatalf("no door run serving remote control to start from: %+v", run)
	}

	t.Setenv("CODEAF_REMOTE_CONTROL", "off")
	r.a.ensureFilesDoor()

	requireViewEnded(t, view, "remote control off")
	if got := livedoor.DefaultRegistry(r.a.Home).Count(r.b.Device.ID()); got != 0 {
		t.Fatalf("the host still counts %d open live tunnels after the tick", got)
	}
	if now := doorRunOf(r); now != nil {
		t.Fatalf("the door run is still registered after the tick: %+v", now)
	}
	requireDone(t, run, "remote control off")
	dialLiveUntil(t, r, "the relay to say nobody serves this computer", func(err error) bool {
		return errors.Is(err, relaypkg.ErrNoMachine)
	})

	// A second tick has nothing left to do.
	r.a.ensureFilesDoor()
	if now := doorRunOf(r); now != nil {
		t.Fatalf("a later tick started a door with remote control off and no live transfer: %+v", now)
	}
}

// TestC2TurningRemoteControlOffKeepsTheDoorForLiveTransferWithoutLiveHandlers is
// C2 for a computer whose live transfer still needs the files door: the open
// view ends, the door restarts without the live handlers (a live intent is
// answered with a refusal while the door itself is still registered), the old
// run was waited on (C4), and turning it back on is taken at the next tick (C3).
func TestC2TurningRemoteControlOffKeepsTheDoorForLiveTransferWithoutLiveHandlers(t *testing.T) {
	t.Setenv("CODEAF_REMOTE_CONTROL", "on")
	r := newLiveRig(t)
	if !r.a.liveCarries() {
		t.Fatal("live transfer is not carrying chats in the rig, so the door has no reason beyond remote control")
	}
	_, _, view := liveViewOnHost(t, r)
	before := doorRunOf(r)
	if before == nil || !before.remoteControl {
		t.Fatalf("no door run serving remote control to start from: %+v", before)
	}

	t.Setenv("CODEAF_REMOTE_CONTROL", "off")
	r.a.ensureFilesDoor()

	requireViewEnded(t, view, "remote control off")
	if got := livedoor.DefaultRegistry(r.a.Home).Count(r.b.Device.ID()); got != 0 {
		t.Fatalf("the host still counts %d open live tunnels after the tick", got)
	}
	requireDone(t, before, "remote control off")
	after := doorRunOf(r)
	if after == nil {
		t.Fatal("the door was stopped although live transfer still needs it")
	}
	if after == before || after.remoteControl {
		t.Fatalf("the door was not restarted without remote control: same run %v, remoteControl %v", after == before, after.remoteControl)
	}
	dialLiveUntil(t, r, "the restarted door to refuse a live intent without being absent", func(err error) bool {
		return err != nil && !errors.Is(err, relaypkg.ErrNoMachine)
	})

	// Turning it back on is taken at the next tick, as it always was.
	t.Setenv("CODEAF_REMOTE_CONTROL", "on")
	r.a.ensureFilesDoor()
	again := doorRunOf(r)
	if again == nil || again == after || !again.remoteControl {
		t.Fatalf("turning remote control back on did not restart the door with it: %+v", again)
	}
	requireDone(t, after, "remote control back on")
	dialLiveUntil(t, r, "a live view to open again", func(err error) bool { return err == nil })
}

// TestC3ARunThatNeverServedRemoteControlIsNeverTouchedByATick is C3: with remote
// control off the whole time, a tick starts a door only for live transfer, a
// second tick leaves it alone, and when live transfer goes off too the door that
// is already up is left exactly as it was (nothing stopped, nothing restarted).
func TestC3ARunThatNeverServedRemoteControlIsNeverTouchedByATick(t *testing.T) {
	t.Setenv("CODEAF_REMOTE_CONTROL", "off")
	r := newLiveRig(t)
	if !r.a.liveCarries() {
		t.Fatal("live transfer is not carrying chats in the rig")
	}

	r.a.ensureFilesDoor()
	run := doorRunOf(r)
	if run == nil || run.remoteControl {
		t.Fatalf("a tick with remote control off should start a plain door for live transfer: %+v", run)
	}
	registrations := r.wire.registrations()

	r.a.ensureFilesDoor()
	if now := doorRunOf(r); now != run {
		t.Fatalf("a second tick replaced the door run: %+v -> %+v", run, now)
	}

	t.Setenv(LiveTransferVar, "off")
	if r.a.liveCarries() {
		t.Fatal("live transfer still carries chats with CODEAF_PASSTHROUGH=off")
	}
	r.a.ensureFilesDoor()
	if now := doorRunOf(r); now != run {
		t.Fatalf("a tick with nothing to do replaced or removed a door that never served remote control: %+v -> %+v", run, now)
	}
	select {
	case <-run.done:
		t.Fatal("a tick with nothing to do stopped a door that never served remote control")
	default:
	}
	if got := r.wire.registrations(); got != registrations {
		t.Fatalf("relay registrations went from %d to %d with remote control never on", registrations, got)
	}
}

// requireNoSignOfAnOfflineHost fails if, over the next stretch of real time, the
// link says anything that claims the host went away or that it is being
// redialled: the host is up and said no, so neither is true.
func requireNoSignOfAnOfflineHost(t *testing.T, link *livedoor.Link, dials func() int, atRest int, over time.Duration) {
	t.Helper()
	deadline := time.Now().Add(over)
	for time.Now().Before(deadline) {
		if st := link.State(); st.Kind == livedoor.WentOffline || st.Kind == livedoor.Reconnecting {
			t.Fatalf("the link says %v although the host is up and said no (reason %q)", st.Kind, st.Reason)
		}
		if got := dials(); got != atRest {
			t.Fatalf("the viewer dialled again by itself: %d dials, want %d", got, atRest)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// TestC1TheViewEndsWithTheSentenceThatRemoteControlWasTurnedOffAndDoesNotRedial
// is the contract of the two-computer hand test: B has a live view of A's chat,
// the person turns remote control OFF on A, and within one tick B's view ends
// saying exactly that. It never says A went offline, the link never redials, and
// the relay sees no further dial from B. The same holds when A's door goes away
// with the switch and when live transfer keeps it up.
func TestC1TheViewEndsWithTheSentenceThatRemoteControlWasTurnedOffAndDoesNotRedial(t *testing.T) {
	for _, tc := range []struct {
		name         string
		liveTransfer string
	}{
		{"the door goes with it", "off"},
		{"the door stays up for live transfer", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CODEAF_REMOTE_CONTROL", "on")
			r := newLiveRig(t)
			t.Setenv(LiveTransferVar, tc.liveTransfer)
			_, link, view := liveViewOnHost(t, r)
			dialedBefore := r.wire.dials()

			t.Setenv("CODEAF_REMOTE_CONTROL", "off")
			r.a.ensureFilesDoor()

			select {
			case err := <-view.ended:
				if reason, ok := livedoor.ReasonOf(err); !ok || reason != livedoor.ReasonRemoteControlOff {
					t.Fatalf("the view ended with %v (reason %q), want %q", err, reason, livedoor.ReasonRemoteControlOff)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the live view did not end")
			}
			waitFor(t, "the link to rest with the host's reason", func() bool {
				st := link.State()
				return st.Kind == livedoor.Revoked && st.Reason == livedoor.ReasonRemoteControlOff
			})
			if said := chatlist.LiveSaid(link.State().Reason, nameA); said != nameA+" turned remote control off." {
				t.Fatalf("the sentence reads %q", said)
			}
			requireNoSignOfAnOfflineHost(t, link, r.wire.dials, dialedBefore, 1500*time.Millisecond)
		})
	}
}

// TestC2AHostNobodyServesIsNeverSaidToHaveTurnedRemoteControlOff keeps the
// default: a viewer that dials a computer with nobody serving, and has never
// been told why, still reads it as a computer it cannot reach and keeps trying.
// It is not given the host's switch as a reason it never heard, and its link is
// not left resting.
func TestC2AHostNobodyServesIsNeverSaidToHaveTurnedRemoteControlOff(t *testing.T) {
	t.Setenv("CODEAF_REMOTE_CONTROL", "on")
	t.Setenv(LiveTransferVar, "off")
	r := newLiveRig(t)
	_, _, _ = liveViewOnHost(t, r)
	t.Setenv("CODEAF_REMOTE_CONTROL", "off")
	r.a.ensureFilesDoor()
	dialLiveUntil(t, r, "the relay to say nobody serves this computer", func(err error) bool {
		return errors.Is(err, relaypkg.ErrNoMachine)
	})

	fresh := r.b.LiveLink(r.a.Device.ID(), nameA, func() bool { return true }, nil)
	t.Cleanup(fresh.Close)
	rwc, err := fresh.Opener(r.cell.ID)()
	if err == nil {
		_ = rwc.Close()
		t.Fatal("a view opened on a computer nobody serves")
	}
	if reason, ok := livedoor.ReasonOf(err); ok && reason == livedoor.ReasonRemoteControlOff {
		t.Fatalf("a viewer that was never told why was handed the host's switch as the reason: %v", err)
	}
	if st := fresh.State(); st.Kind != livedoor.WentOffline && st.Kind != livedoor.Reconnecting {
		t.Fatalf("a viewer that cannot reach the computer reads %v (reason %q), want it still trying", st.Kind, st.Reason)
	}
}
