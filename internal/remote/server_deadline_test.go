package remote

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/session"
)

type deadlineCountingConn struct {
	net.Conn
	deadlines atomic.Int64
}

func (c *deadlineCountingConn) SetWriteDeadline(t time.Time) error {
	if !t.IsZero() {
		c.deadlines.Add(1)
	}
	return c.Conn.SetWriteDeadline(t)
}

// TestAFrozenRemoteConnectionNeverStallsTheHostsOwnWindow verifies V17: when a
// session has one local surface and one remote guest whose reader stops reading,
// broadcast turn events still reach the local surface within bounded real time,
// the frozen guest is marked lagging, closed, and removed from the room without
// cancelling the engine or turn, and a local connection takes the unchanged
// no-deadline write path.
func TestAFrozenRemoteConnectionNeverStallsTheHostsOwnWindow(t *testing.T) {
	prev := GuestWriteDeadline
	GuestWriteDeadline = 50 * time.Millisecond
	t.Cleanup(func() { GuestWriteDeadline = prev })

	agent := &fakeAgent{}
	sess := heldSession(agent)
	t.Cleanup(func() { _ = agent.Close() })

	// Attach the host's own local window over a deadline-counting connection so
	// the control assertion can verify that local writes arm no write deadline.
	localSurfaceRaw, localEngineRaw := net.Pipe()
	localEngineConn := &deadlineCountingConn{Conn: localEngineRaw}
	localFrames := make(chan Frame, 64)
	localServed := make(chan error, 1)
	guard.Go("test/local-surface-serve", func() {
		err := ServeAttach(localEngineConn, localEngineConn, AttachOptions{
			Open: func(Hello) (*Session, error) { return sess, nil },
		})
		_ = localEngineConn.Close()
		localServed <- err
	})
	guard.Go("test/local-surface-read", func() {
		scan := bufio.NewScanner(localSurfaceRaw)
		scan.Buffer(make([]byte, 0, 64*1024), frameCap)
		for scan.Scan() {
			var frame Frame
			if err := json.Unmarshal(scan.Bytes(), &frame); err == nil {
				localFrames <- frame
			}
		}
		close(localFrames)
	})
	t.Cleanup(func() {
		_ = localSurfaceRaw.Close()
		select {
		case <-localServed:
		case <-time.After(time.Second):
		}
	})

	writeRawFrame := func(w io.Writer, frame Frame) {
		t.Helper()
		line, err := json.Marshal(frame)
		if err != nil {
			t.Fatalf("encode frame: %v", err)
		}
		if _, err := w.Write(append(line, '\n')); err != nil {
			t.Fatalf("write frame: %v", err)
		}
	}
	recvLocal := func(within time.Duration, match func(Frame) bool) Frame {
		t.Helper()
		timer := time.NewTimer(within)
		defer timer.Stop()
		for {
			select {
			case frame, ok := <-localFrames:
				if !ok {
					t.Fatal("local surface closed unexpectedly")
				}
				if match(frame) {
					return frame
				}
			case <-timer.C:
				t.Fatalf("local surface did not receive matching frame within %v", within)
				return Frame{}
			}
		}
	}

	writeRawFrame(localSurfaceRaw, Frame{
		Kind:    "hello",
		Payload: raw(t, Hello{Version: Version, Surface: "macbook"}),
	})
	welcomeLocal := recvLocal(time.Second, func(f Frame) bool { return f.Kind == "welcome" })
	if welcomeLocal.Kind != "welcome" {
		t.Fatalf("local welcome = %+v", welcomeLocal)
	}

	// Attach a remote guest over a net.Pipe whose reader reads only the welcome
	// and initial driver push, then never reads again.
	guestSurfaceRaw, guestEngineRaw := net.Pipe()
	guestEngineConn := &deadlineCountingConn{Conn: guestEngineRaw}
	guestServed := make(chan error, 1)
	guard.Go("test/frozen-guest-serve", func() {
		err := ServeAttach(guestEngineConn, guestEngineConn, AttachOptions{
			Open: func(Hello) (*Session, error) { return sess, nil },
		})
		_ = guestEngineConn.Close()
		guestServed <- err
	})
	t.Cleanup(func() { _ = guestSurfaceRaw.Close() })

	guestHello := Hello{
		Version: Version,
		Surface: "phone",
		Session: sess.engine.SessionFile,
		Join:    true,
		Remote: &RemoteViewer{
			Device: "dev-frozen",
			Label:  "phone",
			Watch:  true,
			Drive:  false,
			Road:   "relay",
		},
	}
	guestFirstReadDone := make(chan Frame, 1)
	guard.Go("test/frozen-guest-handshake-read", func() {
		buf := make([]byte, 64*1024)
		var line []byte
		for {
			n, err := guestSurfaceRaw.Read(buf)
			if n > 0 {
				line = append(line, buf[:n]...)
				for i, b := range line {
					if b == '\n' {
						var frame Frame
						_ = json.Unmarshal(line[:i], &frame)
						guestFirstReadDone <- frame
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	})
	writeRawFrame(guestSurfaceRaw, Frame{Kind: "hello", Payload: raw(t, guestHello)})
	select {
	case frame := <-guestFirstReadDone:
		if frame.Kind != "welcome" {
			t.Fatalf("guest handshake answered %q: %s", frame.Kind, frame.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("guest did not receive welcome")
	}
	// Drain the local surface's driver notification from the guest's arrival.
	recvLocal(time.Second, func(f Frame) bool { return f.Kind == "driver" })

	if got := sess.Attached(); got != 2 {
		t.Fatalf("attached surfaces before freeze = %d, want 2", got)
	}

	// Start a turn on the local window and stream events while nobody reads from
	// guestSurfaceRaw. The fan-out must drop the frozen guest within a few
	// hundred milliseconds and deliver every event to the local surface.
	started := time.Now()
	writeRawFrame(localSurfaceRaw, Frame{
		Kind:    "call",
		ID:      1,
		Method:  MethodSubmit,
		Payload: raw(t, SubmitArgs{Text: "keep streaming"}),
	})
	submitResult := recvLocal(500*time.Millisecond, func(f Frame) bool {
		return f.Kind == "result" && f.ID == 1
	})
	if submitResult.Error != "" {
		t.Fatalf("Submit failed: %s", submitResult.Error)
	}
	ref := decode[StreamRef](t, submitResult.Payload)

	turnStream := agent.stream(0)
	if turnStream == nil {
		t.Fatal("Submit opened no turn stream on agent")
	}
	for _, token := range []string{"alpha", "beta", "gamma"} {
		turnStream <- session.Event{Kind: session.EventTextDelta, Text: token}
	}

	var received []string
	for len(received) < 3 {
		evFrame := recvLocal(500*time.Millisecond, func(f Frame) bool {
			return f.Kind == "event" && f.ID == ref.Stream
		})
		ev := decode[EventWire](t, evFrame.Payload).Unwire()
		received = append(received, ev.Text)
	}
	if elapsed := time.Since(started); elapsed > 600*time.Millisecond {
		t.Fatalf("fan-out with frozen guest took %v, want bounded within 600ms", elapsed)
	}
	if received[0] != "alpha" || received[1] != "beta" || received[2] != "gamma" {
		t.Fatalf("local surface received %v, want [alpha beta gamma]", received)
	}

	// The frozen guest has been closed and removed from the room.
	if got := sess.Attached(); got != 1 {
		t.Fatalf("attached surfaces after guest write deadline = %d, want 1 (only local)", got)
	}
	select {
	case <-guestServed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("frozen guest server loop did not exit after deadline overrun")
	}
	var probe [1]byte
	if _, err := guestSurfaceRaw.Read(probe[:]); err == nil {
		t.Fatal("frozen guest connection remained readable after deadline overrun")
	}

	// The engine and turn were not interrupted or closed when the guest left.
	if agent.interrupts != 0 {
		t.Fatalf("agent interrupts = %d, want 0", agent.interrupts)
	}
	if agent.closed() != 0 {
		t.Fatalf("agent closes = %d, want 0", agent.closed())
	}
	turnStream <- session.Event{Kind: session.EventTextDelta, Text: "delta"}
	afterDrop := recvLocal(500*time.Millisecond, func(f Frame) bool {
		return f.Kind == "event" && f.ID == ref.Stream
	})
	if got := decode[EventWire](t, afterDrop.Payload).Unwire().Text; got != "delta" {
		t.Fatalf("event after guest drop = %q, want delta", got)
	}
	agent.finish(turnStream)
	recvLocal(500*time.Millisecond, func(f Frame) bool {
		return f.Kind == "closed" && f.ID == ref.Stream
	})

	// Control: the remote guest armed write deadlines on its connection, whereas
	// the local surface never armed a write deadline on any frame.
	if got := guestEngineConn.deadlines.Load(); got == 0 {
		t.Fatal("remote guest connection never armed SetWriteDeadline")
	}
	if got := localEngineConn.deadlines.Load(); got != 0 {
		t.Fatalf("local surface connection armed SetWriteDeadline %d times, want 0", got)
	}

	// Also verify the watchdog timer fallback for a guest writer that does not
	// implement SetWriteDeadline (an io.Pipe pair whose reader stops reading).
	pipeInR, pipeInW := io.Pipe()
	pipeOutR, pipeOutW := io.Pipe()
	pipeGuestServed := make(chan error, 1)
	guard.Go("test/frozen-pipe-guest-serve", func() {
		err := ServeAttach(pipeInR, pipeOutW, AttachOptions{
			Open: func(Hello) (*Session, error) { return sess, nil },
		})
		_ = pipeOutW.Close()
		pipeGuestServed <- err
	})
	t.Cleanup(func() {
		_ = pipeInW.Close()
		_ = pipeOutR.Close()
	})
	pipeWelcomeCh := make(chan Frame, 1)
	guard.Go("test/frozen-pipe-guest-welcome", func() {
		buf := make([]byte, 64*1024)
		var line []byte
		for {
			n, err := pipeOutR.Read(buf)
			if n > 0 {
				line = append(line, buf[:n]...)
				for i, b := range line {
					if b == '\n' {
						var frame Frame
						_ = json.Unmarshal(line[:i], &frame)
						pipeWelcomeCh <- frame
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	})
	writeRawFrame(pipeInW, Frame{Kind: "hello", Payload: raw(t, guestHello)})
	select {
	case frame := <-pipeWelcomeCh:
		if frame.Kind != "welcome" {
			t.Fatalf("pipe guest handshake answered %q: %s", frame.Kind, frame.Error)
		}
	case <-time.After(time.Second):
		t.Fatal("pipe guest did not receive welcome")
	}
	recvLocal(time.Second, func(f Frame) bool { return f.Kind == "driver" })
	if got := sess.Attached(); got != 2 {
		t.Fatalf("attached surfaces with pipe guest = %d, want 2", got)
	}

	writeRawFrame(localSurfaceRaw, Frame{
		Kind:    "call",
		ID:      2,
		Method:  MethodSubmit,
		Payload: raw(t, SubmitArgs{Text: "stream with pipe guest frozen"}),
	})
	submitResult2 := recvLocal(500*time.Millisecond, func(f Frame) bool {
		return f.Kind == "result" && f.ID == 2
	})
	ref2 := decode[StreamRef](t, submitResult2.Payload)
	turnStream2 := agent.stream(0)
	turnStream2 <- session.Event{Kind: session.EventTextDelta, Text: "watchdog-ok"}
	evFrame2 := recvLocal(500*time.Millisecond, func(f Frame) bool {
		return f.Kind == "event" && f.ID == ref2.Stream
	})
	if got := decode[EventWire](t, evFrame2.Payload).Unwire().Text; got != "watchdog-ok" {
		t.Fatalf("local event with frozen pipe guest = %q, want watchdog-ok", got)
	}
	agent.finish(turnStream2)
	if got := sess.Attached(); got != 1 {
		t.Fatalf("attached surfaces after pipe guest watchdog = %d, want 1", got)
	}
	select {
	case <-pipeGuestServed:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("frozen pipe guest server loop did not exit after watchdog deadline")
	}
}
