package filesdoor

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relay"
)

// V21: the host admits at most 14 concurrent live-intent streams, closes the
// 15th after its intent byte with no bytes written back, still serves a
// move-intent stream at the same moment, admits a new live stream once one
// ends, and refuses live-intent streams when Live is nil.
func TestTheHostRefusesTheFifteenthLiveIntentBeforeNoise(t *testing.T) {
	t.Run("fourteen live streams hold while the fifteenth is closed before Noise and move still works", func(t *testing.T) {
		w := newWorld(t)
		disk := newDisk(t)
		frame, objects := w.frameOf(1, 128)
		putFrame(t, disk, frame)
		store := wrap(t, disk)

		type liveSlot struct {
			release chan struct{}
			exited  chan struct{}
		}
		entered := make(chan liveSlot, relay.MaxStreams)
		cfg := w.config(store)
		cfg.Live = func(ctx context.Context, tun io.ReadWriteCloser, _ string) error {
			defer tun.Close()
			slot := liveSlot{
				release: make(chan struct{}),
				exited:  make(chan struct{}),
			}
			defer close(slot.exited)
			entered <- slot
			select {
			case <-slot.release:
			case <-ctx.Done():
			}
			return nil
		}
		w.serve(cfg)

		ctx := context.Background()
		var viewers []io.ReadWriteCloser
		var slots []liveSlot
		t.Cleanup(func() {
			for _, s := range slots {
				select {
				case <-s.release:
				default:
					close(s.release)
				}
			}
			for _, v := range viewers {
				_ = v.Close()
			}
		})

		for i := 0; i < relay.LiveStreams; i++ {
			tun, err := DialLive(ctx, w.service, w.id, w.b, w.a.ID())
			if err != nil {
				t.Fatalf("live dial %d failed: %v", i+1, err)
			}
			viewers = append(viewers, tun)
			select {
			case slot := <-entered:
				slots = append(slots, slot)
			case <-time.After(5 * time.Second):
				t.Fatalf("live dial %d did not reach Live handler", i+1)
			}
		}

		// Dial the 15th stream without the relay class header so an older relay
		// passes it through to the host backstop, then send the live intent byte.
		name, err := DoorName(w.id, w.a.ID())
		if err != nil {
			t.Fatal(err)
		}
		fifteenth, err := relay.Dial(ctx, w.service, name)
		if err != nil {
			t.Fatalf("fifteenth raw dial failed: %v", err)
		}
		defer fifteenth.Close()

		if _, err := fifteenth.Write([]byte{pair.IntentLive}); err != nil {
			t.Fatal(err)
		}
		var reply [16]byte
		n, readErr := fifteenth.Read(reply[:])
		if n != 0 || !errors.Is(readErr, io.EOF) {
			t.Fatalf("fifteenth live stream read %d bytes (%v), want 0 bytes and io.EOF before any Noise message", n, readErr)
		}

		// A Move-intent ('C') stream still completes while all 14 live streams hold.
		mover := w.source()
		got, err := mover.Get(ctx, objects[0].RID)
		if err != nil {
			t.Fatalf("move-intent Get while 14 live streams held failed: %v", err)
		}
		if !bytes.Equal(got, objects[0].Bytes) {
			t.Fatal("move-intent Get returned altered object bytes")
		}

		// When one live stream ends, a new live stream is admitted.
		close(slots[0].release)
		select {
		case <-slots[0].exited:
		case <-time.After(5 * time.Second):
			t.Fatal("first live handler did not exit")
		}
		_ = viewers[0].Close()

		// THE LIVE CAP COUNTS EVERY STREAM ON THE MACHINE, NOT ONLY LIVE ONES, so
		// two always stay free for a move (relay.LiveStreams). The move source
		// keeps its tunnel until it is closed, and the fifteenth stream is still
		// the dialler's; with both open, thirteen live views plus those two are
		// fourteen and a new live view is rightly refused. Both are done with, so
		// they are closed here, and what is left is thirteen live streams.
		_ = mover.Close()
		_ = fifteenth.Close()

		// The relay gives a slot back when its splice ends, which is asynchronous
		// to the Close calls above, so the dial is tried again on that one refusal
		// for a few seconds. The pause keeps every try inside the relay's thirty
		// dials a minute, so the rate limit cannot stand in for the cap here.
		var replacement io.ReadWriteCloser
		admitted := time.Now().Add(5 * time.Second)
		for {
			replacement, err = DialLive(ctx, w.service, w.id, w.b, w.a.ID())
			if err == nil || !errors.Is(err, relay.ErrTooMany) || time.Now().After(admitted) {
				break
			}
			time.Sleep(500 * time.Millisecond)
		}
		if err != nil {
			t.Fatalf("replacement live dial after one ended failed: %v", err)
		}
		viewers = append(viewers, replacement)
		select {
		case slot := <-entered:
			slots = append(slots, slot)
		case <-time.After(5 * time.Second):
			t.Fatal("replacement live stream did not reach Live handler")
		}
	})

	t.Run("a door with nil Live refuses live intent before Noise", func(t *testing.T) {
		w := newWorld(t)
		w.serve(w.config(wrap(t, newDisk(t))))

		name, err := DoorName(w.id, w.a.ID())
		if err != nil {
			t.Fatal(err)
		}
		stream, err := relay.Dial(context.Background(), w.service, name)
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()

		if _, err := stream.Write([]byte{pair.IntentLive}); err != nil {
			t.Fatal(err)
		}
		var reply [16]byte
		n, readErr := stream.Read(reply[:])
		if n != 0 || !errors.Is(readErr, io.EOF) {
			t.Fatalf("door with nil Live answered %d bytes (%v), want 0 bytes and io.EOF", n, readErr)
		}
	})
}

// V14 / V28: an existing Source moving a chat with intent 'C' and the files
// prologue still completes a normal round trip against a door with Live set.
func TestAnOlderViewerStillMovesAChat(t *testing.T) {
	w := newWorld(t)
	disk := newDisk(t)
	frame, objects := w.frameOf(1, 256, 512)
	frameID := putFrame(t, disk, frame)

	var liveCalled atomic.Bool
	cfg := w.config(wrap(t, disk))
	cfg.Live = func(_ context.Context, tun io.ReadWriteCloser, _ string) error {
		liveCalled.Store(true)
		return tun.Close()
	}
	w.serve(cfg)

	src := w.source()
	ctx := context.Background()
	have, err := src.Has(ctx, []string{objects[0].RID, objects[1].RID})
	if err != nil || len(have) != 2 || !have[0] || !have[1] {
		t.Fatalf("Has = %v, %v", have, err)
	}
	gotFrame, err := src.GetFrame(ctx, frameID)
	if err != nil || !bytes.Equal(gotFrame, frame) {
		t.Fatalf("GetFrame = %d bytes, %v", len(gotFrame), err)
	}
	gotMany, err := src.GetMany(ctx, []string{objects[0].RID, objects[1].RID})
	if err != nil || len(gotMany) != 2 || !bytes.Equal(gotMany[0].Bytes, objects[0].Bytes) || !bytes.Equal(gotMany[1].Bytes, objects[1].Bytes) {
		t.Fatalf("GetMany = %d objects, %v", len(gotMany), err)
	}
	if liveCalled.Load() {
		t.Fatal("a move stream invoked the Live handler")
	}
}

// D2: a connection that sends the live intent with the files prologue, or the
// files intent with the live prologue, never completes the Noise handshake.
func TestAFilesPrologueAndALivePrologueNeverCompleteAgainstEachOther(t *testing.T) {
	w := newWorld(t)
	store := wrap(t, newDisk(t))
	var liveCalled atomic.Bool
	cfg := w.config(store)
	cfg.Live = func(_ context.Context, tun io.ReadWriteCloser, _ string) error {
		liveCalled.Store(true)
		return tun.Close()
	}
	w.serve(cfg)

	hostKey, err := DoorKey(w.id, w.a.ID())
	if err != nil {
		t.Fatal(err)
	}
	viewerKey, err := DoorKey(w.id, w.b.ID())
	if err != nil {
		t.Fatal(err)
	}
	name, err := DoorName(w.id, w.a.ID())
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		intent   byte
		prologue string
	}{
		{name: "live intent with files prologue", intent: pair.IntentLive, prologue: FilesTunnelPrologue},
		{name: "files intent with live prologue", intent: pair.IntentFiles, prologue: LiveTunnelPrologue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stream, err := relay.Dial(context.Background(), w.service, name)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()

			if _, err := pair.ConnectWithKeyIntent(stream, hostKey.PublicKey().Bytes(), viewerKey, tc.intent, tc.prologue); err == nil {
				t.Fatalf("handshake completed for %s", tc.name)
			}
			if liveCalled.Load() {
				t.Fatal("mismatched handshake reached Live")
			}
			if store.reads.Load() != 0 {
				t.Fatal("mismatched handshake read the store")
			}
		})
	}
}

// V10: removing a device cancels its live tunnel immediately through the push
// callback, cancels it on the next recheck through the pull backstop when no
// push arrives, and closes the tunnel on a roster error past the TTL while
// riding out a roster error younger than the TTL.
func TestRemovingADeviceCancelsItsLiveTunnelThroughThePushAndThroughThePull(t *testing.T) {
	t.Run("push revocation cancels only the removed device", func(t *testing.T) {
		w := newWorld(t)
		c, err := identity.NewDevice(w.id)
		if err != nil {
			t.Fatal(err)
		}
		w.allow(c.ID(), true)

		goneCh := make(chan func([]string), 1)
		type session struct {
			device string
			done   <-chan struct{}
		}
		sessions := make(chan session, 4)

		cfg := w.config(wrap(t, newDisk(t)))
		cfg.Revoked = func(_ context.Context, gone func(devices []string)) func() {
			select {
			case goneCh <- gone:
			default:
			}
			return func() {}
		}
		cfg.Live = func(ctx context.Context, tun io.ReadWriteCloser, device string) error {
			defer tun.Close()
			sessions <- session{device: device, done: ctx.Done()}
			<-ctx.Done()
			return nil
		}
		w.serve(cfg)

		var gone func([]string)
		select {
		case gone = <-goneCh:
		case <-time.After(5 * time.Second):
			t.Fatal("Revoked was not started on registration")
		}

		ctx := context.Background()
		tunB, err := DialLive(ctx, w.service, w.id, w.b, w.a.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer tunB.Close()

		tunC, err := DialLive(ctx, w.service, w.id, c, w.a.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer tunC.Close()

		byDevice := make(map[string]<-chan struct{}, 2)
		for len(byDevice) < 2 {
			select {
			case s := <-sessions:
				byDevice[s.device] = s.done
			case <-time.After(5 * time.Second):
				t.Fatal("both live sessions did not arrive")
			}
		}

		gone([]string{w.b.ID()})

		select {
		case <-byDevice[w.b.ID()]:
		case <-time.After(2 * time.Second):
			t.Fatal("removed device context was not cancelled by push")
		}

		select {
		case <-byDevice[c.ID()]:
			t.Fatal("other device context was cancelled when only b was removed")
		default:
		}
	})

	t.Run("pull recheck cancels a removed device without push", func(t *testing.T) {
		oldRecheck := liveRecheck
		liveRecheck = 15 * time.Millisecond
		t.Cleanup(func() { liveRecheck = oldRecheck })

		w := newWorld(t)
		cfg := w.config(wrap(t, newDisk(t)))
		cfg.RosterFor = 15 * time.Millisecond
		canceled := make(chan struct{})
		entered := make(chan struct{})
		cfg.Live = func(ctx context.Context, tun io.ReadWriteCloser, _ string) error {
			defer tun.Close()
			close(entered)
			<-ctx.Done()
			close(canceled)
			return nil
		}
		w.serve(cfg)

		tun, err := DialLive(context.Background(), w.service, w.id, w.b, w.a.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer tun.Close()

		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("live handler was not reached")
		}

		w.allow(w.b.ID(), false)

		select {
		case <-canceled:
		case <-time.After(2 * time.Second):
			t.Fatal("live link was not cancelled after roster removal and liveRecheck")
		}
	})

	t.Run("a roster error older than the ttl closes the tunnel while a younger one does not", func(t *testing.T) {
		oldRecheck := liveRecheck
		liveRecheck = 15 * time.Millisecond
		t.Cleanup(func() { liveRecheck = oldRecheck })

		w := newWorld(t)
		var failRoster atomic.Bool
		cfg := w.config(wrap(t, newDisk(t)))
		cfg.RosterFor = 160 * time.Millisecond
		cfg.Roster = func(ctx context.Context) (map[string]bool, error) {
			if failRoster.Load() {
				return nil, errors.New("directory unreachable")
			}
			return w.roster(ctx)
		}

		entered := make(chan struct{})
		liveDone := make(chan struct{})
		cfg.Live = func(ctx context.Context, tun io.ReadWriteCloser, _ string) error {
			defer tun.Close()
			defer close(liveDone)
			close(entered)
			_, _ = io.Copy(tun, tun)
			return nil
		}
		w.serve(cfg)

		tun, err := DialLive(context.Background(), w.service, w.id, w.b, w.a.ID())
		if err != nil {
			t.Fatal(err)
		}
		defer tun.Close()

		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("live handler was not reached")
		}

		// Turn on roster errors immediately; while the cached answer is younger
		// than RosterFor (160 ms), recheck ticks at 15 ms must keep the tunnel open.
		failRoster.Store(true)
		youngTimer := time.NewTimer(45 * time.Millisecond)
		defer youngTimer.Stop()
		<-youngTimer.C

		select {
		case <-liveDone:
			t.Fatal("a roster failure younger than the ttl closed the live tunnel")
		default:
		}
		if _, err := tun.Write([]byte("still open")); err != nil {
			t.Fatalf("write during young roster failure failed: %v", err)
		}
		echo := make([]byte, len("still open"))
		if _, err := io.ReadFull(tun, echo); err != nil || string(echo) != "still open" {
			t.Fatalf("read during young roster failure = %q, %v", echo, err)
		}

		// Once the 160 ms TTL expires, the next recheck fails closed and shuts the tunnel.
		select {
		case <-liveDone:
		case <-time.After(2 * time.Second):
			t.Fatal("a roster error older than the ttl did not close the live tunnel")
		}
		var buf [8]byte
		if _, err := tun.Read(buf[:]); err == nil {
			t.Fatal("viewer tunnel remained readable after a roster error older than the ttl")
		}
	})
}

// D2 / 3.2: the files door's idle watchdog closes an idle files-intent stream
// after idleAfter, while a live link idle past idleAfter stays open.
func TestALiveLinkIsNotCutByTheFilesDoorsIdleWatchdog(t *testing.T) {
	oldIdle := idleAfter
	idleAfter = 60 * time.Millisecond
	t.Cleanup(func() { idleAfter = oldIdle })

	w := newWorld(t)
	cfg := w.config(wrap(t, newDisk(t)))
	cfg.Live = func(ctx context.Context, tun io.ReadWriteCloser, _ string) error {
		defer tun.Close()
		_, _ = io.Copy(tun, tun)
		return nil
	}
	w.serve(cfg)

	liveTun, err := DialLive(context.Background(), w.service, w.id, w.b, w.a.ID())
	if err != nil {
		t.Fatal(err)
	}
	defer liveTun.Close()

	key, err := DoorKey(w.id, w.b.ID())
	if err != nil {
		t.Fatal(err)
	}
	filesTun := w.rawTunnel(key)
	sendRaw(t, filesTun, typeHello, jsonOf(t, helloFor(w.b, filesTun)))
	if got := receive(t, filesTun, 5*time.Second); got.err != nil || got.typ != typeHello {
		t.Fatalf("files hello = type %d, %v", got.typ, got.err)
	}

	// Wait for the files-intent stream to be closed by the idle watchdog.
	if got := receive(t, filesTun, 2*time.Second); got.err == nil {
		t.Fatalf("idle files-intent stream stayed open and answered type %d", got.typ)
	}

	// Wait one more idleAfter window so the live tunnel has been idle well past idleAfter.
	extra := time.NewTimer(80 * time.Millisecond)
	defer extra.Stop()
	<-extra.C

	want := []byte("still alive after idleAfter")
	if _, err := liveTun.Write(want); err != nil {
		t.Fatalf("live tunnel write after idleAfter failed: %v", err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(liveTun, got); err != nil {
		t.Fatalf("live tunnel read after idleAfter failed: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("live tunnel echoed %q, want %q", got, want)
	}
}

// 3.2: DialLive reaches the host's Live handler with the authenticated viewer
// device id, carries bytes both ways, and preserves relay.ErrNoMachine when the
// host is not registered.
func TestDialLiveReachesTheHostLiveHandlerWithTheViewersDeviceId(t *testing.T) {
	w := newWorld(t)

	// Before the host registers, DialLive reports relay.ErrNoMachine.
	if _, err := DialLive(context.Background(), w.service, w.id, w.b, w.a.ID()); !errors.Is(err, relay.ErrNoMachine) {
		t.Fatalf("DialLive with unregistered host = %v, want relay.ErrNoMachine", err)
	}

	var (
		mu        sync.Mutex
		gotDevice string
	)
	cfg := w.config(wrap(t, newDisk(t)))
	cfg.Live = func(_ context.Context, tun io.ReadWriteCloser, device string) error {
		defer tun.Close()
		mu.Lock()
		gotDevice = device
		mu.Unlock()

		var in [16]byte
		if _, err := io.ReadFull(tun, in[:]); err != nil {
			return err
		}
		_, err := tun.Write(append([]byte("host heard: "), in[:]...))
		return err
	}
	w.serve(cfg)

	tun, err := DialLive(context.Background(), w.service, w.id, w.b, w.a.ID())
	if err != nil {
		t.Fatal(err)
	}
	defer tun.Close()

	msg := []byte("viewer-sixteen!!")
	if _, err := tun.Write(msg); err != nil {
		t.Fatal(err)
	}
	want := append([]byte("host heard: "), msg...)
	reply := make([]byte, len(want))
	if _, err := io.ReadFull(tun, reply); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(reply, want) {
		t.Fatalf("reply = %q, want %q", reply, want)
	}

	mu.Lock()
	defer mu.Unlock()
	if gotDevice != w.b.ID() {
		t.Fatalf("Live received device %q, want %q", gotDevice, w.b.ID())
	}
}
