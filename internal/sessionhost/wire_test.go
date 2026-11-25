//go:build linux

package sessionhost

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type wireTestLauncher struct {
	fakeLauncher
	started chan *wireTestChild
}

type wireTestChild struct {
	env     []string
	in      *io.PipeReader
	out     *io.PipeWriter
	signals chan os.Signal
	exit    func()
}

func newWireTestLauncher() *wireTestLauncher {
	return &wireTestLauncher{started: make(chan *wireTestChild, 4)}
}

func (l *wireTestLauncher) start(_ int, _ layout, env []string) (*engineChild, error) {
	childIn, bridgeIn := io.Pipe()
	bridgeOut, childOut := io.Pipe()
	done := make(chan struct{})
	peer := &wireTestChild{env: env, in: childIn, out: childOut, signals: make(chan os.Signal, 4)}
	peer.exit = sync.OnceFunc(func() {
		childIn.Close()
		childOut.Close()
		close(done)
	})
	l.started <- peer
	return &engineChild{
		in: bridgeIn, out: bridgeOut, done: done,
		signal: func(signal os.Signal) error {
			peer.signals <- signal
			peer.exit()
			return nil
		},
	}, nil
}

func (l *wireTestLauncher) child(t *testing.T) *wireTestChild {
	t.Helper()
	select {
	case child := <-l.started:
		t.Cleanup(child.exit)
		return child
	case <-time.After(3 * time.Second):
		t.Fatal("wire did not start a child")
		return nil
	}
}

func (l *wireTestLauncher) environment(t *testing.T) []string {
	t.Helper()
	return l.child(t).env
}

func dialTestWire(t *testing.T, s *supervisor) *websocket.Conn {
	t.Helper()
	return dialWire(t, s, chatA)
}

func dialWire(t *testing.T, s *supervisor, id string) *websocket.Conn {
	t.Helper()
	server := httptest.NewServer(s)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, response, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/v1/sessions/"+id+"/wire", &websocket.DialOptions{
		HTTPHeader: http.Header{
			"Authorization":   {"Bearer " + s.cfg.Secret},
			"X-Codeaf-Person": {"alice"},
		},
	})
	if err != nil {
		t.Fatalf("upgrade failed: %v, %v", response, err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func TestC5_WireCopiesBinaryAndTerminatesOnlyItsChild(t *testing.T) {
	cfg := testConfig(t)
	launcher := newWireTestLauncher()
	procs := &fakeProcesses{live: processTable{}}
	s := newTestSupervisor(t, cfg, healthyReport(), launcher, procs)
	createChat(t, s, "alice", chatA)
	procs.live[cfg.UIDBase] = []int{4321}
	conn := dialTestWire(t, s)
	child := launcher.child(t)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	in := []byte{0, 0xff, 3, 0x80}
	gotIn := make(chan []byte, 1)
	go func() {
		got := make([]byte, len(in))
		_, _ = io.ReadFull(child.in, got)
		gotIn <- got
	}()
	if err := conn.Write(ctx, websocket.MessageBinary, in); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-gotIn:
		if !bytes.Equal(got, in) {
			t.Fatalf("child received %x, wanted %x", got, in)
		}
	case <-ctx.Done():
		t.Fatal("client bytes did not reach the child")
	}
	out := []byte{0x80, 0, 0xff}
	go func() { _, _ = child.out.Write(out) }()
	typ, got, err := conn.Read(ctx)
	if err != nil || typ != websocket.MessageBinary || !bytes.Equal(got, out) {
		t.Fatalf("client received %x (%v), want %x: %v", got, typ, out, err)
	}
	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
	select {
	case sig := <-child.signals:
		if sig != syscall.SIGTERM {
			t.Fatalf("bridge child received %v, wanted SIGTERM", sig)
		}
	case <-ctx.Done():
		t.Fatal("disconnect left the bridge child running")
	}
	w := request(s, "GET", "/v1/sessions/"+chatA, cfg.Secret, "alice", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"state":"running"`) {
		t.Fatalf("disconnect changed the detached chat: %d %s", w.Code, w.Body)
	}
}

// VALIDATION CONTRACT: a chat whose connection has just ended can be attached
// again at once. The refusal for a chat that is being stopped ("unavailable")
// must never be given to a person who only reconnected.
func TestAConnectionThatJustEndedDoesNotRefuseTheNextAttach(t *testing.T) {
	cfg := testConfig(t)
	launcher := newWireTestLauncher()
	procs := &fakeProcesses{live: processTable{}}
	s := newTestSupervisor(t, cfg, healthyReport(), launcher, procs)
	createChat(t, s, "alice", chatA)
	procs.live[cfg.UIDBase] = []int{4321}
	conn := dialTestWire(t, s)
	child := launcher.child(t)

	// The test stands in for a reconnect that wins the supervisor's lock just as the old connection ends.
	s.mu.Lock()
	released := false
	release := func() {
		if !released {
			released = true
			s.mu.Unlock()
		}
	}
	defer release()
	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil && !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
	select {
	case <-child.signals:
	case <-time.After(3 * time.Second):
		t.Fatal("disconnect left the bridge child running")
	}
	// The ending connection can go no further than the lock. Let it get there.
	time.Sleep(100 * time.Millisecond)

	w := httptest.NewRecorder()
	if !s.checkSlot(w, s.registry.sessions[chatA]) {
		t.Fatalf("a reconnect as the old connection ended was refused: %d %s", w.Code, w.Body)
	}
	release()
}
