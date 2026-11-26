package filesdoor

import (
	"bufio"
	"context"
	"crypto/ecdh"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relay"
)

// countedRelay counts the requests that reach the two doors of a real relay, so
// a test can say how many registrations and dials a behaviour cost.
type countedRelay struct {
	inner  http.Handler
	engine atomic.Int64
	dial   atomic.Int64

	mu          sync.Mutex
	engineConns []net.Conn
}

// hijackSpy remembers the connection the relay takes over, so a test can end a
// registration the way a network drop would: from underneath it.
type hijackSpy struct {
	http.ResponseWriter
	saw func(net.Conn)
}

func (h hijackSpy) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := h.ResponseWriter.(http.Hijacker).Hijack()
	if err == nil {
		h.saw(conn)
	}
	return conn, rw, err
}

// dropRegistrations closes the connection of every registration that has been
// made, which is what the network going away looks like to the door.
func (c *countedRelay) dropRegistrations() {
	c.mu.Lock()
	conns := c.engineConns
	c.engineConns = nil
	c.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
	}
}

func (c *countedRelay) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == relay.EnginePath:
		c.engine.Add(1)
		w = hijackSpy{ResponseWriter: w, saw: func(conn net.Conn) {
			c.mu.Lock()
			c.engineConns = append(c.engineConns, conn)
			c.mu.Unlock()
		}}
	case strings.HasPrefix(r.URL.Path, relay.DialPrefix):
		c.dial.Add(1)
	}
	c.inner.ServeHTTP(w, r)
}

// countingStore wraps a store and counts every read the door makes on it. A
// write fails the test, because a door must never write.
type countingStore struct {
	t     *testing.T
	inner blobstore.Store

	reads       atomic.Int64
	frameReads  atomic.Int64
	frameBytes  atomic.Int64
	afterFrame  func() // called after each GetFrame read, for a mid-stream change
	afterFrameM sync.Mutex
}

func (c *countingStore) PutFrame(context.Context, []byte) (blobstore.FrameID, error) {
	c.t.Error("the files door wrote to its store")
	return "", blobstore.ErrFull
}
func (c *countingStore) Get(ctx context.Context, rid string) ([]byte, error) {
	c.reads.Add(1)
	return c.inner.Get(ctx, rid)
}
func (c *countingStore) Has(ctx context.Context, rids []string) ([]bool, error) {
	c.reads.Add(1)
	return c.inner.Has(ctx, rids)
}
func (c *countingStore) GetMany(ctx context.Context, rids []string) ([]blobstore.Object, error) {
	c.reads.Add(1)
	return c.inner.GetMany(ctx, rids)
}
func (c *countingStore) Locate(ctx context.Context, rids []string) (map[string]blobstore.Location, error) {
	c.reads.Add(1)
	return c.inner.Locate(ctx, rids)
}
func (c *countingStore) GetFrame(ctx context.Context, frame string) ([]byte, error) {
	c.reads.Add(1)
	c.frameReads.Add(1)
	b, err := c.inner.GetFrame(ctx, frame)
	c.frameBytes.Add(int64(len(b)))
	c.afterFrameM.Lock()
	hook := c.afterFrame
	c.afterFrameM.Unlock()
	if hook != nil {
		hook()
	}
	return b, err
}
func (c *countingStore) onFrame(f func()) {
	c.afterFrameM.Lock()
	defer c.afterFrameM.Unlock()
	c.afterFrame = f
}

// world is one person with two computers, a (which holds chats and serves its
// door) and b (which dials it), and a real relay between them.
type world struct {
	t       *testing.T
	id      identity.Identity
	a, b    identity.Dev
	service string
	relay   *countedRelay
	lock    string

	mu      sync.Mutex
	allowed map[string]bool
}

func newWorld(t *testing.T) *world {
	t.Helper()
	t.Setenv("CODEAF_TELEMETRY", "off")
	id, err := identity.Mint()
	if err != nil {
		t.Fatal(err)
	}
	a, err := identity.NewDevice(id)
	if err != nil {
		t.Fatal(err)
	}
	b, err := identity.NewDevice(id)
	if err != nil {
		t.Fatal(err)
	}
	counted := &countedRelay{inner: &relay.Server{}}
	server := httptest.NewServer(counted)
	t.Cleanup(server.Close)
	t.Cleanup(server.CloseClientConnections)
	return &world{
		t: t, id: id, a: a, b: b, service: server.URL, relay: counted,
		// A nested path that does not exist yet, as the home's is on first launch.
		lock:    filepath.Join(t.TempDir(), "v3", "sync", "files-door.lock"),
		allowed: map[string]bool{a.ID(): true, b.ID(): true},
	}
}

func (w *world) roster(context.Context) (map[string]bool, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make(map[string]bool, len(w.allowed))
	for k, v := range w.allowed {
		out[k] = v
	}
	return out, nil
}

func (w *world) allow(device string, ok bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.allowed[device] = ok
}

func (w *world) remove(device string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.allowed, device)
}

// config is a door for computer a over store, with a short lock poll so a
// handover test does not wait on the real one.
func (w *world) config(store blobstore.Store) ServeConfig {
	return ServeConfig{
		Service: w.service, Identity: w.id, Device: w.a, Roster: w.roster,
		Store: store, LockPath: w.lock, LockPoll: 20 * time.Millisecond,
	}
}

// served is one running Serve call.
type served struct {
	t     *testing.T
	stop  func()
	ready chan struct{}
	regs  atomic.Int64
}

// serve starts a door and waits until it is registered.
func (w *world) serve(cfg ServeConfig) *served {
	s := w.serveNoWait(cfg)
	select {
	case <-s.ready:
	case <-time.After(10 * time.Second):
		w.t.Fatal("the door did not register")
	}
	return s
}

func (w *world) serveNoWait(cfg ServeConfig) *served {
	w.t.Helper()
	s := &served{t: w.t, ready: make(chan struct{}, 8)}
	cfg.Ready = func() {
		s.regs.Add(1)
		select {
		case s.ready <- struct{}{}:
		default:
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, cfg) }()
	var once sync.Once
	s.stop = func() {
		once.Do(func() {
			cancel()
			select {
			case err := <-done:
				if err != nil {
					w.t.Errorf("Serve returned %v", err)
				}
			case <-time.After(10 * time.Second):
				w.t.Error("Serve did not stop")
			}
		})
	}
	w.t.Cleanup(s.stop)
	return s
}

// source is a client on computer b for computer a's door.
func (w *world) source() *Source {
	w.t.Helper()
	s := NewSource(SourceConfig{
		Service: w.service, Identity: w.id, Device: w.b,
		TargetID: w.a.ID(), TargetName: "selfhost-pc",
	})
	w.t.Cleanup(func() { _ = s.Close() })
	return s
}

// rawTunnel opens a tunnel to a's door with the given static key, which is what
// a hostile or confused client does, and no hello.
func (w *world) rawTunnel(key *ecdh.PrivateKey) *pair.Tunnel {
	w.t.Helper()
	name, err := DoorName(w.id, w.a.ID())
	if err != nil {
		w.t.Fatal(err)
	}
	stream, err := relay.Dial(context.Background(), w.service, name)
	if err != nil {
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { _ = stream.Close() })
	target, err := DoorKey(w.id, w.a.ID())
	if err != nil {
		w.t.Fatal(err)
	}
	tunnel, err := pair.ConnectWithKeyPrologue(stream, target.PublicKey().Bytes(), key, FilesTunnelPrologue)
	if err != nil {
		w.t.Fatal(err)
	}
	return tunnel
}

type incoming struct {
	typ  byte
	body []byte
	err  error
}

// receive reads one message, failing the test when none comes in time.
func receive(t *testing.T, r io.Reader, within time.Duration) incoming {
	t.Helper()
	got := make(chan incoming, 1)
	go func() {
		typ, body, _, err := readMessage(r, maxResponseMessage)
		got <- incoming{typ, body, err}
	}()
	select {
	case in := <-got:
		return in
	case <-time.After(within):
		t.Fatal("no message arrived")
	}
	return incoming{}
}

func sendRaw(t *testing.T, w io.Writer, typ byte, parts ...[]byte) {
	t.Helper()
	if _, err := writeMessage(w, typ, parts...); err != nil {
		t.Fatal(err)
	}
}

func jsonOf(t *testing.T, v any) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// rid is a deterministic object id.
func rid(n int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("object %d", n)))
	return hex.EncodeToString(sum[:])
}

// objectBytes is n bytes that look random and are the same for the same seed.
// They begin with the magic of a sealed engine object, because a store refuses
// a frame holding anything that is not sealed.
func objectBytes(seed int64, n int) []byte {
	b := make([]byte, n)
	rand.New(rand.NewSource(seed)).Read(b)
	copy(b, "AGEO\x01")
	return b
}

// frameOf encodes objects of the given sizes into one frame. Object k is
// numbered first+k.
func (w *world) frameOf(first int, sizes ...int) ([]byte, []blobstore.Object) {
	w.t.Helper()
	objects := make([]blobstore.Object, len(sizes))
	for i, n := range sizes {
		objects[i] = blobstore.Object{RID: rid(first + i), Bytes: objectBytes(int64(first+i), n)}
	}
	frame, err := blobstore.Encode(w.id.CellKeyID(), objects)
	if err != nil {
		w.t.Fatal(err)
	}
	return frame, objects
}

func putFrame(t *testing.T, store blobstore.Store, frame []byte) blobstore.FrameID {
	t.Helper()
	id, err := store.PutFrame(context.Background(), frame)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func newDisk(t *testing.T) *blobstore.Disk {
	t.Helper()
	disk, err := blobstore.NewDisk(filepath.Join(t.TempDir(), "live-store"))
	if err != nil {
		t.Fatal(err)
	}
	return disk
}

// wrap makes the door's view of a store the counting one.
func wrap(t *testing.T, store blobstore.Store) *countingStore {
	return &countingStore{t: t, inner: store}
}

// fakeDoor registers under a's real door key and answers one connection with a
// proper handshake and hello, then hands the tunnel to behave. It is how a test
// plays a door that misbehaves after it has been let in.
func (w *world) fakeDoor(behave func(*pair.Tunnel)) {
	w.t.Helper()
	w.fakeDoorWith(func(tunnel *pair.Tunnel) hello { return helloFor(w.a, tunnel) }, behave)
}

// fakeDoorWith is fakeDoor with the door's own hello chosen by the test.
func (w *world) fakeDoorWith(answer func(*pair.Tunnel) hello, behave func(*pair.Tunnel)) {
	w.t.Helper()
	key, err := DoorKey(w.id, w.a.ID())
	if err != nil {
		w.t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	registration, err := relay.Register(ctx, w.service, key)
	if err != nil {
		cancel()
		w.t.Fatal(err)
	}
	w.t.Cleanup(func() { cancel(); _ = registration.Close() })
	go func() {
		for {
			stream, err := registration.Accept()
			if err != nil {
				return
			}
			go func() {
				defer stream.Close()
				tunnel, err := pair.AcceptWithKeyPrologue(stream, key, FilesTunnelPrologue)
				if err != nil {
					return
				}
				typ, body, _, err := readMessage(tunnel, MaxRequestBytes)
				var h hello
				if err != nil || typ != typeHello || json.Unmarshal(body, &h) != nil || checkHello(w.id, tunnel, h) != nil {
					return
				}
				if _, err := writeMessage(tunnel, typeHello, jsonOf(w.t, answer(tunnel))); err != nil {
					return
				}
				behave(tunnel)
			}()
		}
	}()
}

// listIDs is n distinct valid rids starting at first.
func listIDs(first, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = rid(first + i)
	}
	return out
}
