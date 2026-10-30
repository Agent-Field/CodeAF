package taskdoor_test

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/run"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

type wireTap struct {
	address     string
	mu          sync.Mutex
	seen        bytes.Buffer
	connections []net.Conn
}

func (w *wireTap) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seen.Write(p)
}
func (w *wireTap) bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return bytes.Clone(w.seen.Bytes())
}
func tapRelay(t *testing.T, upstream string) *wireTap {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	tap := &wireTap{address: "http://" + listener.Addr().String()}
	go func() {
		for {
			near, err := listener.Accept()
			if err != nil {
				return
			}
			far, err := net.Dial("tcp", strings.TrimPrefix(upstream, "http://"))
			if err != nil {
				_ = near.Close()
				return
			}
			tap.mu.Lock()
			tap.connections = append(tap.connections, near, far)
			tap.mu.Unlock()
			go func() { _, _ = io.Copy(io.MultiWriter(far, tap), near); _ = far.Close() }()
			go func() { _, _ = io.Copy(io.MultiWriter(near, tap), far); _ = near.Close() }()
		}
	}()
	t.Cleanup(tap.drop)
	return tap
}

// Contract 14: recording every byte at the actual relay sockets must reveal
// neither admission text nor streamed work, even while a watch is attached.
func TestTheRelaySeesNoTaskWords(t *testing.T) {
	r := newRig(t)
	tap := tapRelay(t, r.service)
	r.service = tap.address
	r.executor.run = func(ctx context.Context, a *taskdoor.Attempt, j taskdoor.Journal) error {
		if err := j.Append(taskdoor.Step{Step: run.Step{Kind: "step", Step: 1, Command: "secret step cobalt cabbage"}}); err != nil {
			return err
		}
		if err := j.Append(taskdoor.Phase{Words: "secret phase copper carrots"}); err != nil {
			return err
		}
		return j.Append(taskdoor.Finished{Outcome: "done", Report: "secret report salmon squash", Model: "test-model"})
	}
	r.serve(r.config())
	c := r.dial()
	id := attempt(10)
	send(t, c, id, request())
	receive(t, c, "started")
	send(t, c, id, taskdoor.Watch{})
	receive(t, c, "step")
	receive(t, c, "phase")
	receive(t, c, "finished")
	traffic := tap.bytes()
	if len(traffic) == 0 {
		t.Fatal("relay recorder saw no traffic")
	}
	for _, secret := range []string{request().Instruction, request().Title, "secret step cobalt cabbage", "secret phase copper carrots", "secret report salmon squash"} {
		if bytes.Contains(traffic, []byte(secret)) {
			t.Fatalf("relay read %q", secret)
		}
	}
}

func rawTunnel(t *testing.T, r *rig, key *ecdh.PrivateKey) *pair.Tunnel {
	t.Helper()
	name, err := taskdoor.DoorName(r.id, r.b.ID())
	if err != nil {
		t.Fatal(err)
	}
	stream, err := relay.Dial(context.Background(), r.service, name)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stream.Close() })
	target, err := taskdoor.DoorKey(r.id, r.b.ID())
	if err != nil {
		t.Fatal(err)
	}
	tunnel, err := pair.ConnectWithKey(stream, target.PublicKey().Bytes(), key)
	if err != nil {
		t.Fatal(err)
	}
	return tunnel
}
func rawSend(t *testing.T, w io.Writer, id string, payload any) {
	t.Helper()
	frame, err := taskdoor.NewFrame(id, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := taskdoor.WriteFrame(w, frame); err != nil {
		t.Fatal(err)
	}
}
func rawReceive(t *testing.T, r io.Reader, kind string) taskdoor.Frame {
	t.Helper()
	type result struct {
		f   taskdoor.Frame
		err error
	}
	done := make(chan result, 1)
	go func() { f, err := taskdoor.ReadFrame(r); done <- result{f, err} }()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.f.Type != kind {
			t.Fatalf("frame %s, want %s", result.f.Type, kind)
		}
		return result.f
	case <-time.After(5 * time.Second):
		t.Fatalf("no raw %s frame", kind)
	}
	return taskdoor.Frame{}
}
func rawHello(t *testing.T, tunnel *pair.Tunnel, dev identity.Dev) {
	t.Helper()
	rawSend(t, tunnel, "", taskdoor.Hello{V: 1, Cert: dev.Cert, Sig: dev.Sign(tunnel.ChannelBinding())})
	rawReceive(t, tunnel, "hello")
}

// Contract 13's authentication boundary includes certificates from another
// identity, a device impersonating a different static, and old-channel proofs.
func TestAHelloFromAnotherIdentityIsRefused(t *testing.T) {
	for _, kind := range []string{"another identity", "another static", "another channel"} {
		t.Run(kind, func(t *testing.T) {
			r := newRig(t)
			r.serve(r.config())
			id, dev := r.id, r.a
			if kind == "another identity" {
				var err error
				id, err = identity.Mint()
				if err != nil {
					t.Fatal(err)
				}
				dev, err = identity.NewDevice(id)
				if err != nil {
					t.Fatal(err)
				}
			}
			key, err := taskdoor.DoorKey(id, dev.ID())
			if err != nil {
				t.Fatal(err)
			}
			if kind == "another static" {
				key, err = taskdoor.DoorKey(r.id, r.b.ID())
				if err != nil {
					t.Fatal(err)
				}
			}
			tunnel := rawTunnel(t, r, key)
			binding := tunnel.ChannelBinding()
			if kind == "another channel" {
				binding = bytes.Repeat([]byte{1}, len(binding))
			}
			rawSend(t, tunnel, "", taskdoor.Hello{V: 1, Cert: dev.Cert, Sig: dev.Sign(binding)})
			var refused taskdoor.Refused
			decode(t, rawReceive(t, tunnel, "refused"), &refused)
			if refused.Reason != "could not confirm this computer" {
				t.Fatalf("authentication failure = %q", refused.Reason)
			}
			if r.executor.calls.Load() != 0 {
				t.Fatal("unauthorized hello started work")
			}
		})
	}
}

// Contract 8 requires distinct actionable errors for a different build and a
// valid certificate that belongs to a computer other than the selected target.
func TestTheChosenComputerAndWireVersionAreChecked(t *testing.T) {
	for _, kind := range []string{"wrong computer", "wrong version", "wrong signature"} {
		t.Run(kind, func(t *testing.T) {
			r := newRig(t)
			key, err := taskdoor.DoorKey(r.id, r.b.ID())
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			registration, err := relay.Register(ctx, r.service, key)
			if err != nil {
				t.Fatal(err)
			}
			defer registration.Close()
			done := make(chan struct{})
			go func() {
				defer close(done)
				stream, err := registration.Accept()
				if err != nil {
					return
				}
				defer stream.Close()
				tunnel, err := pair.AcceptWithKey(stream, key)
				if err != nil {
					return
				}
				if _, err := taskdoor.ReadFrame(tunnel); err != nil {
					return
				}
				dev := r.b
				if kind == "wrong computer" {
					dev = r.a
				}
				hello := taskdoor.Hello{V: 1, Cert: dev.Cert, Sig: dev.Sign(tunnel.ChannelBinding())}
				if kind == "wrong version" {
					hello.V = 2
				}
				if kind == "wrong signature" {
					hello.Sig = bytes.Repeat([]byte{1}, 64)
				}
				frame, err := taskdoor.NewFrame("", hello)
				if err == nil {
					_ = taskdoor.WriteFrame(tunnel, frame)
				}
				_, _ = taskdoor.ReadFrame(tunnel)
			}()
			c, err := taskdoor.Dial(ctx, r.service, r.id, r.a, r.b.ID(), "chosen computer")
			if c != nil {
				_ = c.Close()
			}
			want := "could not confirm this is chosen computer"
			if kind == "wrong version" {
				want = "chosen computer runs a different codeaf that cannot take this task — update both"
			}
			if err == nil || err.Error() != want {
				t.Fatalf("heard %v, want %s", err, want)
			}
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("refused peer stayed open")
			}
		})
	}
}

// Contract 14's wire boundary caps memory before reading a claimed payload.
func TestFramesRefuseAnOversizedLengthBeforeReadingItsBody(t *testing.T) {
	raw := binary.BigEndian.AppendUint32(nil, 1<<20+1)
	if _, err := taskdoor.ReadFrame(bytes.NewReader(raw)); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("oversized header returned %v", err)
	}
	_, err := taskdoor.NewFrame(attempt(1), taskdoor.Phase{Words: strings.Repeat("x", 1<<20)})
	if err == nil {
		t.Fatal("oversized outgoing frame accepted")
	}
}

func (tap *wireTap) drop() {
	tap.mu.Lock()
	connections := tap.connections
	tap.connections = nil
	tap.mu.Unlock()
	for _, connection := range connections {
		_ = connection.Close()
	}
}

// Contract 9's carrier loss must leave the executor running while the shared
// registration loop emits one lost/back pair and reopens the same task door.
func TestATaskDoorComesBackWithoutRestartingItsAttempt(t *testing.T) {
	r := newRig(t)
	var unavailable atomic.Bool
	service := &relay.Server{}
	front := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if unavailable.Load() {
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
			return
		}
		service.ServeHTTP(w, request)
	}))
	t.Cleanup(front.Close)
	tap := tapRelay(t, front.URL)
	r.service = tap.address
	finish := make(chan struct{})
	r.executor.run = func(ctx context.Context, a *taskdoor.Attempt, j taskdoor.Journal) error {
		if err := j.Append(taskdoor.Phase{Words: "running"}); err != nil {
			return err
		}
		select {
		case <-finish:
			return j.Append(taskdoor.Finished{Outcome: "done", Report: "continued through the outage"})
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	lines := make(chan string, 32)
	config := r.config()
	config.Say = func(line string) { lines <- line }
	r.serve(config)
	c := r.dial()
	id := attempt(40)
	send(t, c, id, request())
	receive(t, c, "started")
	send(t, c, id, taskdoor.Watch{})
	receive(t, c, "phase")
	unavailable.Store(true)
	tap.drop()
	name, err := taskdoor.DoorName(r.id, r.b.ID())
	if err != nil {
		t.Fatal(err)
	}
	lost, back := 0, 0
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for back == 0 {
		select {
		case line := <-lines:
			if line == pair.LostSyncLine {
				lost++
				unavailable.Store(false)
			}
			if line == pair.BackLine(name) {
				back++
			}
		case <-deadline.C:
			t.Fatal("task door did not say it came back")
		}
	}
	if lost != 1 {
		t.Fatalf("outage emitted %d lost lines", lost)
	}
	_ = c.Close()
	c = r.dial()
	send(t, c, id, taskdoor.Watch{From: 1})
	close(finish)
	frame := receive(t, c, "finished")
	if frame.Seq != 2 {
		t.Fatalf("resumed finish seq = %d", frame.Seq)
	}
	if r.executor.calls.Load() != 1 || r.executor.adopts.Load() != 0 {
		t.Fatalf("outage ran/adopted %d/%d", r.executor.calls.Load(), r.executor.adopts.Load())
	}
}

// Contract 8 bounds a registered but silent door as well as an absent one;
// the timeout covers Noise and hello together, not just connecting the relay.
func TestARegisteredDoorThatDoesNotAnswerHasTheServingSentence(t *testing.T) {
	r := newRig(t)
	key, err := taskdoor.DoorKey(r.id, r.b.ID())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	registration, err := relay.Register(ctx, r.service, key)
	if err != nil {
		t.Fatal(err)
	}
	defer registration.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		stream, err := registration.Accept()
		if err != nil {
			return
		}
		defer stream.Close()
		<-ctx.Done()
	}()
	since := time.Now()
	conn, err := taskdoor.Dial(ctx, r.service, r.id, r.a, r.b.ID(), "work computer")
	if conn != nil {
		_ = conn.Close()
	}
	if err == nil || err.Error() != "work computer is not taking tasks right now — run codeaf serve on it" {
		t.Fatalf("silent door heard %v", err)
	}
	// Scheduling adds a small observation margin to the ten-second I/O bound.
	if elapsed := time.Since(since); elapsed > 11*time.Second {
		t.Fatalf("silent dial took %s", elapsed)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("silent fixture did not close")
	}
}
