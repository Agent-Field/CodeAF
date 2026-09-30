package relayserve_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

const (
	cell  = "01J0000000000000000000000A"
	head1 = "1111111111111111111111111111111111111111111111111111111111111111"
	head2 = "2222222222222222222222222222222222222222222222222222222222222222"
)

var bg = context.Background()

// device is one machine: a real device key and cert under a real identity.
type device struct {
	id  identity.Identity
	dev identity.Dev
}

func (d device) IdentityKey() ed25519.PublicKey { return d.id.PublicKey() }
func (d device) Cert() identity.Cert            { return d.dev.Cert }
func (d device) Sign(msg []byte) []byte         { return d.dev.Sign(msg) }

func newDevice(t *testing.T, id identity.Identity, home string) device {
	t.Helper()
	if _, err := identity.Adopt(home, id, false); err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(home)
	if err != nil {
		t.Fatal(err)
	}
	return device{id, dev}
}

// newIdentity makes a fresh identity and its first device.
func newIdentity(t *testing.T) (identity.Identity, device) {
	t.Helper()
	home := t.TempDir()
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	return id, newDevice(t, id, home)
}

// rig is a running relay over one store directory and one shared fake clock.
type rig struct {
	t     *testing.T
	store string
	clock *directorytest.FakeClock
	srv   *httptest.Server
	svc   *relayserve.Service
	mu    sync.Mutex
	logs  []string
}

func newRig(t *testing.T) *rig {
	r := &rig{t: t, store: t.TempDir(), clock: directorytest.NewFakeClock()}
	r.start()
	return r
}

func (r *rig) logf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.logs = append(r.logs, strings.TrimSpace(fmt.Sprintf(format, args...)))
}

// start brings the relay up on a fresh port over the same store: a restart.
func (r *rig) start() {
	r.svc = relayserve.New(relayserve.Config{Store: r.store, Now: r.clock.Now, Logf: r.logf})
	r.srv = httptest.NewServer(r.svc.Handler)
	r.t.Cleanup(r.stop)
}

func (r *rig) stop() {
	if r.srv != nil {
		r.srv.Close()
		r.svc.Close()
		r.srv = nil
	}
}

func (r *rig) restart() { r.stop(); r.start() }

func (r *rig) dir(d device) *directory.HTTP {
	return directory.NewHTTP(r.srv.URL, reqsign.SignFor(d, r.clock.Now), r.srv.Client())
}

func (r *rig) blobs(d device) *blobstore.HTTP {
	return blobstore.NewHTTP(r.srv.URL, reqsign.SignFor(d, r.clock.Now), r.srv.Client())
}

func (r *rig) expire() { r.clock.Advance(directory.LeaseTTL + 1) }

func frameOf(t *testing.T, name string) (frame []byte, rid string) {
	t.Helper()
	obj := blobstore.Object{RID: strings.Repeat("ab", 32), Bytes: append([]byte("AGEO\x01"), name...)}
	frame, err := blobstore.Encode(strings.Repeat("0", 32), []blobstore.Object{obj})
	if err != nil {
		t.Fatal(err)
	}
	return frame, obj.RID
}

func TestRelayServesDirAndStore(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	b := newDevice(t, id, t.TempDir())

	if _, err := r.dir(a).Create(bg, cell, directory.CellInit{Head: head1, Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.dir(b).Acquire(bg, cell, directory.AcquireOpts{}); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("B took a live lease: %v", err)
	}
	r.expire()
	v, err := r.dir(b).Acquire(bg, cell, directory.AcquireOpts{})
	if err != nil || v.Cell.Lease.Fence != 2 || v.Cell.Lease.Device != b.dev.ID() {
		t.Fatalf("B acquire after expiry: %v %+v", err, v)
	}
	// L7: A, superseded, is refused by its fence.
	_, err = r.dir(a).Publish(bg, cell, directory.Publish{Fence: 1, OldHead: head1, Head: head2, Class: "chat"})
	if !errors.Is(err, directory.ErrFenceStale) {
		t.Fatalf("stale publish accepted: %v", err)
	}

	frame, rid := frameOf(t, "hello")
	if _, err := r.blobs(a).PutFrame(bg, frame); err != nil {
		t.Fatal(err)
	}
	got, err := r.blobs(b).Get(bg, rid) // the other device of the same identity
	if err != nil || !strings.HasSuffix(string(got), "hello") {
		t.Fatalf("B read A's object: %q %v", got, err)
	}
}

func TestRelayAnswersCarryTheClock(t *testing.T) {
	r := newRig(t)
	resp, err := http.Get(r.srv.URL + "/v1/dir/list") // unsigned: refused, and still stamped
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized || resp.Header.Get("Codeaf-Now") == "" {
		t.Fatalf("status %d, Codeaf-Now %q", resp.StatusCode, resp.Header.Get("Codeaf-Now"))
	}
}

func TestRelayRestartKeepsLease(t *testing.T) {
	r := newRig(t)
	id, a := newIdentity(t)
	b := newDevice(t, id, t.TempDir())
	if _, err := r.dir(a).Create(bg, cell, directory.CellInit{Head: head1, Class: "chat"}); err != nil {
		t.Fatal(err)
	}

	r.restart()
	if _, err := r.dir(b).Acquire(bg, cell, directory.AcquireOpts{}); !errors.Is(err, directory.ErrLeaseHeld) {
		t.Fatalf("live lease lost across restart: %v", err)
	}
	r.expire()
	if v, err := r.dir(b).Acquire(bg, cell, directory.AcquireOpts{}); err != nil || v.Cell.Lease.Fence != 2 {
		t.Fatalf("fence after restart: %v %+v", err, v)
	}

	r.restart()
	_, err := r.dir(a).Publish(bg, cell, directory.Publish{Fence: 1, OldHead: head1, Head: head2, Class: "chat"})
	if !errors.Is(err, directory.ErrFenceStale) {
		t.Fatalf("fence forgotten across restart: %v", err)
	}
	if v, err := r.dir(b).Heartbeat(bg, cell, directory.Beat{Fence: 2}); err != nil || v.Cell.Lease.Fence != 2 {
		t.Fatalf("holder lost its lease across restart: %v %+v", err, v)
	}
}

func TestRelayKeepsIdentitiesApart(t *testing.T) {
	r := newRig(t)
	idOne, one := newIdentity(t)
	idTwo, two := newIdentity(t)

	if _, err := r.dir(one).Create(bg, cell, directory.CellInit{Head: head1, Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	frame, rid := frameOf(t, "private")
	if _, err := r.blobs(one).PutFrame(bg, frame); err != nil {
		t.Fatal(err)
	}

	if l, err := r.dir(two).List(bg); err != nil || len(l.Cells) != 0 {
		t.Fatalf("second identity lists %+v, %v", l, err)
	}
	if _, err := r.dir(two).Cell(bg, cell); !errors.Is(err, directory.ErrNotFound) {
		t.Fatalf("second identity reads the first's cell: %v", err)
	}
	if _, err := r.blobs(two).Get(bg, rid); !errors.Is(err, blobstore.ErrNotFound) {
		t.Fatalf("second identity reads the first's object: %v", err)
	}
	if _, err := r.dir(two).Create(bg, cell, directory.CellInit{Head: head2, Class: "chat"}); err != nil {
		t.Fatalf("the same cell id is free in another namespace: %v", err)
	}

	for _, want := range []string{"directory/" + idOne.ID() + ".db", "directory/" + idTwo.ID() + ".db", "blobs/" + idOne.ID()} {
		if _, err := os.Stat(filepath.Join(r.store, want)); err != nil {
			t.Fatalf("layout: %v", err)
		}
	}
	if _, err := os.Stat(filepath.Join(r.store, "blobs", idTwo.ID(), "objects")); err != nil {
		t.Fatalf("second identity's blob tree: %v", err)
	}
}

// A relay that logged a body or a header would leak exactly what it was built
// not to see, so the log is read back and searched for both.
func TestRelayLogsNoBodyAndNoHeader(t *testing.T) {
	r := newRig(t)
	_, a := newIdentity(t)
	const secret = "c2VjcmV0LXRpdGxl"
	if _, err := r.dir(a).Create(bg, cell, directory.CellInit{Head: head1, Class: "chat", Title: secret}); err != nil {
		t.Fatal(err)
	}
	frame, _ := frameOf(t, "payload-text")
	if _, err := r.blobs(a).PutFrame(bg, frame); err != nil {
		t.Fatal(err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.logs) < 2 {
		t.Fatalf("expected one line per request, got %q", r.logs)
	}
	all := strings.Join(r.logs, "\n")
	for _, leak := range []string{secret, "payload-text", a.dev.Cert.DeviceID(), cell, "Codeaf-Sig"} {
		if strings.Contains(all, leak) {
			t.Fatalf("log leaks %q:\n%s", leak, all)
		}
	}
	if !strings.Contains(all, a.id.ID()[:11]) || !strings.Contains(all, "POST /v1/dir/cells/{id} 200") {
		t.Fatalf("log lacks id prefix, verb or status:\n%s", all)
	}
	if strings.Contains(all, a.id.ID()[:12]) {
		t.Fatalf("log carries more than the id prefix:\n%s", all)
	}
}

func TestRelayWithoutStoreIsOnlyThePipe(t *testing.T) {
	svc := relayserve.New(relayserve.Config{Status: true})
	srv := httptest.NewServer(svc.Handler)
	defer srv.Close()
	for path, want := range map[string]int{"/v1/dir/list": 404, "/v1/store/stats": 404, "/status": 200} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want || resp.Header.Get("Codeaf-Now") == "" {
			t.Fatalf("%s: status %d, Codeaf-Now %q", path, resp.StatusCode, resp.Header.Get("Codeaf-Now"))
		}
	}
}
