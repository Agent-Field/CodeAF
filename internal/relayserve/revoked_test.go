package relayserve_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// twoOfOne is one identity with two recorded devices, and a third that has
// signed in with the same identity but never recorded itself.
func twoOfOne(t *testing.T, r *rig) (a, b, c device) {
	t.Helper()
	id, a := newIdentity(t)
	b, c = newDevice(t, id, t.TempDir()), newDevice(t, id, t.TempDir())
	for _, d := range []device{a, b} {
		if err := r.dir(d).PutDevice(bg, d.dev.ID(), directory.Device{V: 1, Name: "n"}); err != nil {
			t.Fatal(err)
		}
	}
	return a, b, c
}

// A revoked device is refused on the directory wire and on the store wire, and
// each client names it as the seam's one error.
func TestRevokedDeviceIsRefusedOnBothWires(t *testing.T) {
	r := newRig(t)
	a, b, _ := twoOfOne(t, r)
	if err := r.dir(a).Revoke(bg, b.dev.ID()); err != nil {
		t.Fatal(err)
	}

	if _, err := r.dir(b).List(bg); !errors.Is(err, directory.ErrRevoked) {
		t.Fatalf("directory: %v, want ErrRevoked", err)
	}
	frame, _ := frameOf(t, "x")
	if _, err := r.blobs(b).PutFrame(bg, frame); !errors.Is(err, wireauth.ErrRevoked) {
		t.Fatalf("store: %v, want ErrRevoked", err)
	}
	if _, err := r.blobs(a).PutFrame(bg, frame); err != nil {
		t.Fatalf("the device that revoked was refused: %v", err)
	}
}

// The refusal is the wire's own: status 401 and the body {"err":"revoked"}.
func TestRevokedRefusalIsA401WithItsOwnCode(t *testing.T) {
	r := newRig(t)
	a, b, _ := twoOfOne(t, r)
	if err := r.dir(a).Revoke(bg, b.dev.ID()); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/v1/dir/list", "/v1/store/stats"} {
		req, _ := http.NewRequest(http.MethodGet, r.srv.URL+path, nil)
		reqsign.Sign(req, nil, b, r.clock.Now())
		resp, err := r.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		var body struct{ Err string }
		err = json.NewDecoder(resp.Body).Decode(&body)
		resp.Body.Close()
		if err != nil {
			t.Fatal(err)
		}
		if resp.StatusCode != http.StatusUnauthorized || body.Err != "revoked" {
			t.Errorf("%s: %d %q, want 401 revoked", path, resp.StatusCode, body.Err)
		}
	}
}

// Same identity, different device key: a different device id, so a device
// nobody revoked is untouched, whether or not it ever recorded itself. This is
// what a fresh pairing relies on to bring a machine back.
func TestOtherDeviceOfTheSameIdentityIsUnaffected(t *testing.T) {
	r := newRig(t)
	a, b, c := twoOfOne(t, r)
	if err := r.dir(a).Revoke(bg, b.dev.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.dir(c).List(bg); err != nil {
		t.Fatalf("an unrecorded device of the identity was refused: %v", err)
	}
	if _, err := r.blobs(c).Has(bg, nil); err != nil {
		t.Fatalf("store, unrecorded device: %v", err)
	}
}

// Revoking is kept on disk: a relay that restarts still turns the device away.
func TestRevokedDeviceStaysRevokedAcrossARestart(t *testing.T) {
	r := newRig(t)
	a, b, _ := twoOfOne(t, r)
	if err := r.dir(a).Revoke(bg, b.dev.ID()); err != nil {
		t.Fatal(err)
	}
	r.restart()
	frame, _ := frameOf(t, "y")
	if _, err := r.blobs(b).PutFrame(bg, frame); !errors.Is(err, wireauth.ErrRevoked) {
		t.Fatalf("store after restart: %v, want ErrRevoked", err)
	}
	if err := r.dir(b).PutDevice(bg, b.dev.ID(), directory.Device{V: 1}); !errors.Is(err, directory.ErrRevoked) {
		t.Fatalf("a revoked device wrote its own record back after a restart: %v", err)
	}
}

// One device cannot stop another identity's device, and cannot stop itself.
func TestRevokeStaysInsideOneIdentityAndOffSelf(t *testing.T) {
	r := newRig(t)
	a, _, _ := twoOfOne(t, r)
	_, other := newIdentity(t)
	if err := r.dir(other).PutDevice(bg, other.dev.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	if err := r.dir(a).Revoke(bg, other.dev.ID()); !errors.Is(err, directory.ErrNotFound) {
		t.Fatalf("revoking another identity's device: %v, want ErrNotFound", err)
	}
	if err := r.dir(a).Revoke(bg, a.dev.ID()); !errors.Is(err, directory.ErrSelfRevoke) {
		t.Fatalf("revoking itself: %v, want ErrSelfRevoke", err)
	}
	if _, err := r.dir(other).List(bg); err != nil {
		t.Fatalf("the other identity's device was stopped: %v", err)
	}
}

// Requests racing a revoke each get an answer, and once Revoke has returned no
// later request from that device gets anything else.
func TestRevokeUnderConcurrentRequests(t *testing.T) {
	r := newRig(t)
	a, b, _ := twoOfOne(t, r)
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				if _, err := r.dir(b).List(bg); err != nil && !errors.Is(err, directory.ErrRevoked) {
					t.Errorf("racing request: %v", err)
				}
			}
		}()
	}
	if err := r.dir(a).Revoke(bg, b.dev.ID()); err != nil {
		t.Fatal(err)
	}
	after := func() error { _, err := r.blobs(b).Get(bg, strings.Repeat("ab", 32)); return err }
	if err := after(); !errors.Is(err, wireauth.ErrRevoked) {
		t.Errorf("after Revoke returned: %v, want ErrRevoked", err)
	}
	wg.Wait()
}

// The whole directory conformance suite, revocation cases included, runs over
// real signed HTTP against the relay. Each case gets a fresh relay and a fresh
// identity; a device is made under it the first time a case names it.
func TestRelayPassesDirectoryConformance(t *testing.T) {
	directorytest.RunRigs(t, func(t *testing.T) directorytest.Rig {
		r := newRig(t)
		id, first := newIdentity(t)
		var mu sync.Mutex
		named := map[string]device{}
		as := func(name string) device {
			mu.Lock()
			defer mu.Unlock()
			if d, ok := named[name]; ok {
				return d
			}
			d := first
			if len(named) > 0 {
				d = newDevice(t, id, t.TempDir())
			}
			named[name] = d
			return d
		}
		return directorytest.Rig{
			Clock:   r.clock,
			Devices: func(name string) directory.Client { return r.dir(as(name)) },
			ID:      func(name string) string { return as(name).dev.ID() },
		}
	})
}
