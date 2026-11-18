package syncsetup

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/filesdoor"
)

// The live-transfer rig: the two-home rig over a relay that behaves like the
// hosted default with sync storage off. Every request the relay receives is
// written down (the owners' evidence, C18), a byte tap sits in front of the
// relay (C3), and the relay answers a frame upload the way the hosted service
// does while storage is off. Nothing about the production code is faked: both
// homes are real Syncs, the doors dial each other through the real pipe, and
// the engine is the real one.

// wireEntry is one request that reached the relay.
type wireEntry struct {
	Method string
	Path   string
	Bytes  int64
	Body   []byte // kept for directory writes only, so C18 can read the records
}

// wireLog is the relay's request list.
type wireLog struct {
	mu      sync.Mutex
	entries []wireEntry
	// allowStore lets uploads through, for the tests that approve storage.
	allowStore atomic.Bool
}

func (w *wireLog) wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		entry := wireEntry{Method: r.Method, Path: r.URL.Path, Bytes: max(r.ContentLength, 0)}
		upgrade := strings.EqualFold(r.Header.Get("Connection"), "upgrade") || r.Header.Get("Upgrade") != ""
		if !upgrade && r.Method != http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/dir") && r.Body != nil {
			body, _ := io.ReadAll(io.LimitReader(r.Body, 64<<10))
			r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), r.Body))
			entry.Body = body
		}
		w.mu.Lock()
		w.entries = append(w.entries, entry)
		w.mu.Unlock()
		if r.Method == http.MethodPost && r.URL.Path == "/v1/store/frames" && !w.allowStore.Load() {
			// The hosted service refuses an upload while storage is off, and
			// this relay says the same, so a client that tries is told no.
			rw.Header().Set("Content-Type", "application/json")
			rw.WriteHeader(http.StatusForbidden)
			_, _ = rw.Write([]byte(`{"error":"storage_off"}`))
			return
		}
		next.ServeHTTP(rw, r)
	})
}

func (w *wireLog) snapshot() []wireEntry {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]wireEntry(nil), w.entries...)
}

// count is how many requests so far have this method and a path with this prefix.
func (w *wireLog) count(method, prefix string) int {
	n := 0
	for _, e := range w.snapshot() {
		if e.Method == method && strings.HasPrefix(e.Path, prefix) {
			n++
		}
	}
	return n
}

// storeWrites is every request that tried to put an object on the relay.
func (w *wireLog) storeWrites() int { return w.count(http.MethodPost, "/v1/store/frames") }

// dials and registrations are the pipe's two doors.
func (w *wireLog) dials() int         { return w.count(http.MethodGet, "/v1/dial/") }
func (w *wireLog) registrations() int { return w.count(http.MethodGet, "/v1/engine") }

// summary is the list the owners read: every route the relay was written to,
// how often, and the largest body.
func (w *wireLog) summary() []string {
	type key struct{ method, route string }
	counts := map[key]int{}
	biggest := map[key]int64{}
	for _, e := range w.snapshot() {
		if e.Method == http.MethodGet && !strings.HasPrefix(e.Path, "/v1/dial/") && e.Path != "/v1/engine" {
			continue
		}
		route := e.Path
		if strings.HasPrefix(route, "/v1/dial/") {
			route = "/v1/dial/{door}"
		}
		if strings.HasPrefix(route, "/v1/dir/cells/") {
			parts := strings.Split(strings.TrimPrefix(route, "/v1/dir/cells/"), "/")
			route = "/v1/dir/cells/{id}"
			if len(parts) > 1 {
				route += "/" + parts[len(parts)-1]
			}
		}
		if strings.HasPrefix(route, "/v1/dir/devices/") {
			route = "/v1/dir/devices/{id}"
		}
		k := key{e.Method, route}
		counts[k]++
		biggest[k] = max(biggest[k], e.Bytes)
	}
	var lines []string
	for k, n := range counts {
		lines = append(lines, fmt.Sprintf("%-6s %-34s x%-3d largest body %d bytes", k.method, k.route, n, biggest[k]))
	}
	sort.Strings(lines)
	return lines
}

// recordsWithNames are the directory writes whose body carries a non-empty
// sealed title or folder: what the legal line forbids while live transfer is
// the only road.
func (w *wireLog) recordsWithNames() []string {
	var bad []string
	for _, e := range w.snapshot() {
		if len(e.Body) == 0 {
			continue
		}
		var body map[string]any
		if json.Unmarshal(e.Body, &body) != nil {
			continue
		}
		for _, field := range []string{"title", "folder"} {
			if v, ok := body[field].(string); ok && v != "" {
				bad = append(bad, fmt.Sprintf("%s %s carries a non-empty %s", e.Method, e.Path, field))
			}
		}
	}
	return bad
}

// byteTap sits between the clients and the relay and keeps a copy of every
// byte that crossed it, in both directions: a superset of anything the relay
// could ever have known (see the same idea in internal/pair's end-to-end test).
type byteTap struct {
	address string
	mu      sync.Mutex
	seen    bytes.Buffer
}

func newByteTap(t *testing.T, upstream string) *byteTap {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	host := strings.TrimPrefix(upstream, "http://")
	tap := &byteTap{address: "http://" + listener.Addr().String()}
	go func() {
		for {
			near, err := listener.Accept()
			if err != nil {
				return
			}
			far, err := net.Dial("tcp", host)
			if err != nil {
				_ = near.Close()
				return
			}
			go func() {
				_, _ = io.Copy(io.MultiWriter(far, tap), near)
				_ = far.Close()
			}()
			go func() {
				_, _ = io.Copy(io.MultiWriter(near, tap), far)
				_ = near.Close()
			}()
		}
	}()
	return tap
}

func (t *byteTap) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.seen.Write(b)
}

func (t *byteTap) bytes() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]byte(nil), t.seen.Bytes()...)
}

// liveRig is two homes of one person on a relay with storage off.
type liveRig struct {
	*twoHomes
	wire *wireLog
	tap  *byteTap
	mu   sync.Mutex
	// dialed is every files-door source a take made: who dialed whom.
	dialed []liveDialed
}

type liveDialed struct {
	from, to string
	source   *filesdoor.Source
}

func (r *liveRig) sourcesOf(from string) []*filesdoor.Source {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*filesdoor.Source
	for _, d := range r.dialed {
		if d.from == from {
			out = append(out, d.source)
		}
	}
	return out
}

// newLiveRig builds the scene every live-transfer test starts from: two homes
// that know of each other, sync storage off for the account, live transfer on.
func newLiveRig(t *testing.T) *liveRig {
	t.Helper()
	t.Setenv(LiveTransferVar, "")
	wire := &wireLog{}
	base := newDriveRig(t, wire.wrap)
	tap := newByteTap(t, base.url)
	t.Setenv(URLVar, tap.address)
	base.a = openSync(t, base.homeA)
	base.b = openSync(t, base.homeB)
	h := twoHomesOver(t, base)
	rig := &liveRig{twoHomes: h, wire: wire, tap: tap}

	// The timings that exist so a test does not wait on a real laptop's clocks.
	oldWithin, oldRoster, oldPoll, oldHook := liveRetryWithin, liveDoorRosterFor, liveDoorLockPoll, liveSourceHook
	liveRetryWithin, liveDoorRosterFor, liveDoorLockPoll = 400*time.Millisecond, 20*time.Millisecond, 20*time.Millisecond
	liveSourceHook = func(from, to string, s *filesdoor.Source) {
		rig.mu.Lock()
		defer rig.mu.Unlock()
		rig.dialed = append(rig.dialed, liveDialed{from: from, to: to, source: s})
	}
	t.Cleanup(func() {
		stopFilesDoors()
		liveRetryWithin, liveDoorRosterFor, liveDoorLockPoll, liveSourceHook = oldWithin, oldRoster, oldPoll, oldHook
	})

	for _, one := range []struct {
		home string
		s    *Sync
	}{{base.homeA, h.a}, {base.homeB, h.b}} {
		mustNil(t, SaveStorage(one.home, one.s.Relay, one.s.Identity.ID(), "off"))
	}
	// Each computer has told the directory it exists, and each has read the
	// roster once, which is how a real pair of computers learn of one another.
	ctx := context.Background()
	mustNil(t, h.a.putDevice(ctx, nameA))
	mustNil(t, h.b.putDevice(ctx, nameB))
	for _, s := range []*Sync{h.a, h.b} {
		if _, err := s.Dir.List(ctx); err != nil {
			t.Fatal(err)
		}
		if !s.OthersPaired() {
			t.Fatal("the rig's computers do not know of each other")
		}
	}
	return rig
}

// seedLiveTree is a project without secrets: the saved keys of a computer wait
// for sync storage, so a file that lives in the vault is not part of a live move.
func seedLiveTree(t *testing.T, work string) {
	t.Helper()
	seedTree(t, work)
	for _, secret := range []string{".env", "web/client/app/.env.production"} {
		if err := os.Remove(filepath.Join(work, secret)); err != nil {
			t.Fatal(err)
		}
	}
}

// liveFramesOf lists the frame ids a home's live store holds.
func liveFramesOf(t *testing.T, home string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(liveStoreDir(home), "frames"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatal(err)
	}
	var ids []string
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), ".") {
			ids = append(ids, e.Name())
		}
	}
	sort.Strings(ids)
	return ids
}

// approveStorage is the account's storage switched on: both computers learn it,
// and the relay now takes uploads.
func (r *liveRig) approveStorage() {
	r.t.Helper()
	r.wire.allowStore.Store(true)
	for _, one := range []struct {
		home string
		s    *Sync
	}{{r.homeA, r.a}, {r.homeB, r.b}} {
		mustNil(r.t, SaveStorage(one.home, one.s.Relay, one.s.Identity.ID(), "on"))
	}
}
