package relayserve_test

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// retireAfter freezes and retires the identity of a, with a grace of d, and
// answers what the relay says it will do.
func retireAfter(t *testing.T, r *rig, a device, d time.Duration) directory.RotationView {
	t.Helper()
	gate := directory.Gate{Client: r.dir(a)}
	if err := gate.Freeze(bg); err != nil {
		t.Fatal(err)
	}
	if err := gate.Retire(bg, d); err != nil {
		t.Fatal(err)
	}
	v, err := r.dir(a).Rotation(bg)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// A frozen identity takes no frame and no record on either wire, and gives back
// what it already holds.
func TestFrozenIdentityIsReadOnlyOnBothWires(t *testing.T) {
	r := newRig(t)
	_, a := newIdentity(t)
	frame, rid := frameOf(t, "kept")
	if _, err := r.blobs(a).PutFrame(bg, frame); err != nil {
		t.Fatal(err)
	}
	if err := (directory.Gate{Client: r.dir(a)}).Freeze(bg); err != nil {
		t.Fatal(err)
	}
	other, _ := frameOf(t, "new")
	if _, err := r.blobs(a).PutFrame(bg, other); !errors.Is(err, wireauth.ErrRotated) {
		t.Fatalf("store put on a frozen identity: %v, want ErrRotated", err)
	}
	if _, err := r.dir(a).Create(bg, cell, directory.CellInit{Head: head1}); !errors.Is(err, directory.ErrRotated) {
		t.Fatalf("directory create on a frozen identity: %v, want ErrRotated", err)
	}
	if got, err := r.blobs(a).Get(bg, rid); err != nil || len(got) == 0 {
		t.Fatalf("a read of a frozen identity's store: %v", err)
	}
}

// The refusal is the wire's own: status 410 and the body {"err":"rotated"}.
func TestRotatedRefusalIsA410WithItsOwnCode(t *testing.T) {
	r := newRig(t)
	_, a := newIdentity(t)
	if err := (directory.Gate{Client: r.dir(a)}).Freeze(bg); err != nil {
		t.Fatal(err)
	}
	status, code := r.post(t, a, "/v1/dir/cells/"+cell, `{"head":"`+head1+`"}`)
	if status != http.StatusGone || code != "rotated" {
		t.Fatalf("%d %q, want 410 rotated", status, code)
	}
}

// post sends one signed request with a JSON body and answers the status and the error code.
func (r *rig) post(t *testing.T, d device, path, body string) (int, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, r.srv.URL+path, bytesReader(body))
	signAs(req, []byte(body), d, r)
	resp, err := r.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var b struct{ Err string }
	_ = json.NewDecoder(resp.Body).Decode(&b)
	return resp.StatusCode, b.Err
}

// Before the deadline a retired identity is read-only but whole; at the deadline
// the sweep deletes its directory and its blobs, leaves a tombstone, and every
// request afterwards is 410 gone and creates nothing.
func TestRetireSweepDeletesAtTheDeadline(t *testing.T) {
	r := newRig(t)
	_, a := newIdentity(t)
	frame, rid := frameOf(t, "soon gone")
	if _, err := r.blobs(a).PutFrame(bg, frame); err != nil {
		t.Fatal(err)
	}
	v := retireAfter(t, r, a, 5*time.Second)
	if v.Rotation.RetireAt != v.Now+5000 {
		t.Fatalf("retire at %d, now %d", v.Rotation.RetireAt, v.Now)
	}

	r.clock.Advance(4 * time.Second)
	if err := r.svc.Sweep(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.dir(a).List(bg); err != nil {
		t.Fatalf("a retired identity was not readable before its deadline: %v", err)
	}

	r.clock.Advance(2 * time.Second)
	if err := r.svc.Sweep(); err != nil {
		t.Fatal(err)
	}
	id := a.id.ID()
	for _, gone := range []string{"directory/" + id + ".db", "directory/" + id + ".db-wal", "blobs/" + id} {
		if _, err := os.Stat(filepath.Join(r.store, gone)); err == nil {
			t.Errorf("%s survived the sweep", gone)
		}
	}
	if _, err := os.Stat(filepath.Join(r.store, "retired", id)); err != nil {
		t.Fatalf("no tombstone: %v", err)
	}
	if _, err := r.dir(a).List(bg); !errors.Is(err, wireauth.ErrGone) {
		t.Fatalf("list after the sweep: %v, want ErrGone", err)
	}
	if _, err := r.blobs(a).Get(bg, rid); !errors.Is(err, wireauth.ErrGone) {
		t.Fatalf("store get after the sweep: %v, want ErrGone", err)
	}
	if _, err := os.Stat(filepath.Join(r.store, "directory", id+".db")); err == nil {
		t.Fatal("a request to a gone identity made its directory again")
	}
}

// A sweep that died after the tombstone and before the erase is finished by the
// next one, and a relay that was down past a deadline catches up on restart.
func TestSweepFinishesWhatACrashLeft(t *testing.T) {
	r := newRig(t)
	_, a := newIdentity(t)
	frame, _ := frameOf(t, "x")
	if _, err := r.blobs(a).PutFrame(bg, frame); err != nil {
		t.Fatal(err)
	}
	retireAfter(t, r, a, 2*time.Second)
	r.stop()
	id := a.id.ID()
	if err := os.MkdirAll(filepath.Join(r.store, "retired"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.store, "retired", id), []byte("crashed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	r.start()
	if err := r.svc.Sweep(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(r.store, "blobs", id)); err == nil {
		t.Fatal("the blobs of a tombstoned identity survived a sweep")
	}
}

// Sweeping an identity nobody replaced deletes nothing.
func TestSweepLeavesLiveIdentities(t *testing.T) {
	r := newRig(t)
	_, a := newIdentity(t)
	if _, err := r.dir(a).Create(bg, cell, directory.CellInit{Head: head1}); err != nil {
		t.Fatal(err)
	}
	r.clock.Advance(1000 * time.Hour)
	if err := r.svc.Sweep(); err != nil {
		t.Fatal(err)
	}
	if _, err := r.dir(a).Cell(bg, cell); err != nil {
		t.Fatalf("a live identity lost its chat to a sweep: %v", err)
	}
}

// The relay refuses a grace outside its bounds with its own code.
func TestRetireGraceIsBounded(t *testing.T) {
	r := newRig(t)
	_, a := newIdentity(t)
	gate := directory.Gate{Client: r.dir(a)}
	if err := gate.Freeze(bg); err != nil {
		t.Fatal(err)
	}
	if err := gate.Retire(bg, time.Millisecond); !errors.Is(err, directory.ErrBadGrace) {
		t.Fatalf("a one millisecond grace: %v", err)
	}
	if err := gate.Retire(bg, 2*time.Hour); !errors.Is(err, directory.ErrBadGrace) {
		t.Fatalf("a two hour grace against a one hour cap: %v", err)
	}
}
