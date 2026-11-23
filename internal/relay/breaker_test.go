package relay

import (
	"context"
	"io"
	"net"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const mib = 1 << 20

// The breaker is a leaky bucket: a burst over the window, or a flow above the
// rate, trips it; a flow under the rate never does, however long it runs.
func TestTheBreakerIsALeakyBucket(t *testing.T) {
	start := time.Unix(1_000_000, 0)
	capacity := int(float64(BreakerRate) * BreakerWindow.Seconds())

	var burst breaker
	if burst.charge(capacity, start) {
		t.Fatal("exactly the window's allowance is not over the rate")
	}
	if !burst.charge(1, start) {
		t.Fatal("one byte more than the window's allowance is")
	}

	var twice breaker
	tripped := 0
	for second := 1; second <= 20 && tripped == 0; second++ {
		if twice.charge(8*mib, start.Add(time.Duration(second)*time.Second)) {
			tripped = second
		}
	}
	if tripped < 5 || tripped > 6 {
		t.Fatalf("twice the rate tripped at second %d, want 5 or 6", tripped)
	}

	var under breaker
	for second := 1; second <= 3600; second++ {
		if under.charge(3*mib, start.Add(time.Duration(second)*time.Second)) {
			t.Fatalf("3 MiB a second is under the rate and tripped at second %d", second)
		}
	}

	var rests breaker
	rests.charge(capacity, start)
	if rests.charge(capacity, start.Add(BreakerWindow)) {
		t.Fatal("a stream that rested for the window did not start again from empty")
	}

	var backwards breaker
	backwards.charge(capacity, start)
	if !backwards.charge(1, start.Add(-time.Hour)) {
		t.Fatal("a clock that stepped backwards drained the bucket")
	}
}

// reads yields a source of `total` bytes in 32 KiB reads, moving a fake clock by
// `each` before every read, so a test fixes the rate and the clock is not real.
type reads struct {
	now        *time.Time
	each       time.Duration
	total      int
	sinceStart time.Duration
}

func (r *reads) Read(p []byte) (int, error) {
	if r.total <= 0 {
		return 0, io.EOF
	}
	*r.now = r.now.Add(r.each)
	r.sinceStart += r.each
	n := min(len(p), r.total)
	r.total -= n
	return n, nil
}

// The pace a correct host holds, one MiB a second, never meets the breaker, and
// twice the rate is cut inside the window and after its allowance is spent.
func TestACorrectPaceNeverMeetsTheBreakerAndTwiceTheRateDoes(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	server := &Server{Now: func() time.Time { return now }}

	paced := &reads{now: &now, each: time.Second / 32, total: 600 * mib} // 1 MiB a second for ten minutes
	moved, over := server.carry(io.Discard, paced, DoorLive)
	if over || moved != int64(600*mib) {
		t.Fatalf("a correct pace moved %d bytes and over=%v; want all 600 MiB and no cut", moved, over)
	}

	fast := &reads{now: &now, each: time.Second / 256, total: 600 * mib} // 8 MiB a second
	moved, over = server.carry(io.Discard, fast, DoorLive)
	if !over {
		t.Fatal("twice the rate was never cut")
	}
	if fast.sinceStart < 4900*time.Millisecond || fast.sinceStart > 6100*time.Millisecond {
		t.Fatalf("twice the rate was cut after %v, want after about 5 s", fast.sinceStart)
	}
	if moved < 35*mib || moved > 45*mib {
		t.Fatalf("%d bytes crossed before the cut, want about 40 MiB", moved)
	}
}

// A stream that did not claim the live class is copied exactly as it always was:
// a move's 8 MiB answers and every older client's traffic arrive in an instant
// and are never counted.
func TestAStreamWithNoLiveClassIsNeverHeldToARate(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	server := &Server{Now: func() time.Time { return now }}
	for _, class := range []string{"", DoorMove} {
		source := &reads{now: &now, each: 0, total: 200 * mib}
		moved, over := server.carry(io.Discard, source, class)
		if over || moved != int64(200*mib) {
			t.Fatalf("class %q moved %d bytes, over=%v; want 200 MiB and no cut", class, moved, over)
		}
	}
}

// startFlood registers a machine on a relay whose clock never moves, so every
// byte sent counts against the breaker, and returns what the machine received.
func startFlood(t *testing.T, class string) (surface net.Conn, received *atomic.Int64, ended chan struct{}) {
	t.Helper()
	frozen := time.Unix(1_000_000, 0)
	service := &Server{Now: func() time.Time { return frozen }}
	server := httptest.NewServer(service)
	t.Cleanup(server.Close)
	t.Cleanup(server.CloseClientConnections)
	address := server.URL
	registration, err := Register(context.Background(), address, aKey(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = registration.Close() })
	conn, err := DialDoor(context.Background(), address, registration.Name(), class)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	engine, err := registration.Accept()
	if err != nil {
		t.Fatal(err)
	}
	received = new(atomic.Int64)
	ended = make(chan struct{})
	go func() {
		buf := make([]byte, 64<<10)
		for {
			n, err := engine.Read(buf)
			received.Add(int64(n))
			if err != nil {
				close(ended)
				return
			}
		}
	}()
	return conn, received, ended
}

// A live stream sending far faster than any live view is cut, the surface is
// told with close code 1008, and the machine is told the stream is over.
func TestALiveStreamSendingFasterThanAnyViewIsCutWithPolicyViolation(t *testing.T) {
	surface, received, ended := startFlood(t, DoorLive)
	chunk := make([]byte, 64<<10)
	for sent := 0; sent < 64*mib; sent += len(chunk) {
		if _, err := surface.Write(chunk); err != nil {
			break
		}
	}
	buf := make([]byte, 1)
	_, err := surface.Read(buf)
	if websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
		t.Fatalf("the surface heard %v, want a close with code 1008 (%d)", err, websocket.StatusPolicyViolation)
	}
	select {
	case <-ended:
	case <-time.After(10 * time.Second):
		t.Fatal("the machine was never told the cut stream is over")
	}
	allowance := int64(float64(BreakerRate) * BreakerWindow.Seconds())
	if got := received.Load(); got <= 0 || got > allowance {
		t.Fatalf("the machine received %d bytes before the cut, want something up to the %d-byte allowance", got, allowance)
	}
}

// The same flood on a stream that claimed no live class, or a move, is carried
// whole: the breaker is class-gated, and an unclassified dial behaves as today.
func TestTheSameFloodOnAStreamWithNoLiveClassIsCarriedWhole(t *testing.T) {
	for _, class := range []string{"", DoorMove} {
		t.Run("class="+class, func(t *testing.T) {
			surface, received, _ := startFlood(t, class)
			const total = 40 * mib
			chunk := make([]byte, 64<<10)
			for sent := 0; sent < total; sent += len(chunk) {
				if _, err := surface.Write(chunk); err != nil {
					t.Fatalf("a stream with no live class was cut after %d bytes: %v", sent, err)
				}
			}
			deadline := time.Now().Add(30 * time.Second)
			for received.Load() < total {
				if time.Now().After(deadline) {
					t.Fatalf("the machine received %d of %d bytes", received.Load(), total)
				}
				time.Sleep(10 * time.Millisecond)
			}
		})
	}
}
