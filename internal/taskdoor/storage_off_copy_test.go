//go:build linux || darwin

package taskdoor_test

import (
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// storeAsks records every request that leaves for the relay's object store.
type storeAsks struct {
	mu    sync.Mutex
	paths []string
}

func (s *storeAsks) RoundTrip(r *http.Request) (*http.Response, error) {
	if strings.HasPrefix(r.URL.Path, "/v1/store/") {
		s.mu.Lock()
		s.paths = append(s.paths, r.Method+" "+r.URL.Path)
		s.mu.Unlock()
	}
	return http.DefaultTransport.RoundTrip(r)
}

func (s *storeAsks) all() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.paths...)
}

// gated puts d's store behind the production permission gate, reading the
// account's storage state from home, and records what the store is asked.
func gated(t *testing.T, d taskdoor.CopyDeps, home, state string) (taskdoor.CopyDeps, *storeAsks) {
	t.Helper()
	asks := &storeAsks{}
	if err := syncsetup.SaveStorage(home, d.Service, d.Identity.ID(), state); err != nil {
		t.Fatal(err)
	}
	client := blobstore.NewHTTP(d.Service, func(*http.Request, []byte) {}, &http.Client{Transport: asks})
	d.Store = syncsetup.GateStore(client, home, d.Service, d.Identity.ID())
	return d, asks
}

// C5.1 (send): with the account's storage off, sending a task copy asks the
// relay's object store nothing and does none of the work that would end in an
// upload, and the person is told what they are told today: storage is off.
func TestSendingATaskCopyWithStorageOffAsksTheRelayNothing(t *testing.T) {
	a, _ := copyHomes(t)
	a, asks := gated(t, a, t.TempDir(), "off")
	local := copyRepo(t)

	_, err := taskdoor.SendCopy(t.Context(), a, local)
	if !errors.Is(err, wireauth.ErrStorageOff) {
		t.Fatalf("SendCopy with storage off = %v, want the storage-off refusal", err)
	}
	if got := asks.all(); len(got) != 0 {
		t.Fatalf("SendCopy with storage off asked the relay's store: %v", got)
	}
	// Nothing was sealed, locked or captured: the task folder here is untouched.
	if _, err := os.Stat(a.Root); !os.IsNotExist(err) {
		t.Fatalf("SendCopy with storage off did work before refusing: %v", err)
	}
}

// C5.1 (receive): a computer whose storage permission is closed does not fetch a
// task copy either, and does not begin the work that surrounds a fetch.
func TestReceivingATaskCopyWithStorageOffAsksTheRelayNothing(t *testing.T) {
	a, b := copyHomes(t)
	// The sender's storage is on, so the copy really is on the relay.
	a, sent := gated(t, a, t.TempDir(), "on")
	in, err := taskdoor.SendCopy(t.Context(), a, copyRepo(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(sent.all()) == 0 {
		t.Fatal("the control send never reached the relay's store; the recorder sees nothing")
	}

	// The receiving computer's permission reads closed.
	b, asks := gated(t, b, t.TempDir(), "off")
	err = taskdoor.ReceiveCopy(t.Context(), b, in, t.TempDir()+"/work")
	if !errors.Is(err, wireauth.ErrStorageOff) {
		t.Fatalf("ReceiveCopy with storage off = %v, want the storage-off refusal", err)
	}
	if got := asks.all(); len(got) != 0 {
		t.Fatalf("ReceiveCopy with storage off asked the relay's store: %v", got)
	}
	if _, err := os.Stat(b.Root); !os.IsNotExist(err) {
		t.Fatalf("ReceiveCopy with storage off did work before refusing: %v", err)
	}
}

// Returning a finished task's work has both halves behind the same permission:
// the computer that did the work cannot upload it, and the computer that sent
// the task does not go looking for it.
func TestReturningATaskResultWithStorageOffAsksTheRelayNothing(t *testing.T) {
	a, b := copyHomes(t)
	a, _ = gated(t, a, t.TempDir(), "on")
	b, _ = gated(t, b, t.TempDir(), "on")
	local, work := copyRepo(t), t.TempDir()+"/work"
	in, err := taskdoor.SendCopy(t.Context(), a, local)
	if err != nil {
		t.Fatal(err)
	}
	if err := taskdoor.ReceiveCopy(t.Context(), b, in, work); err != nil {
		t.Fatal(err)
	}
	copyFile(t, work, "edit.txt", "from B\n")
	copyGit(t, work, "add", "--all")
	copyGit(t, work, "commit", "-qm", "finished on B")

	// Both permissions close after the work is done.
	b, bAsks := gated(t, b, t.TempDir(), "off")
	if _, err := taskdoor.SendResult(t.Context(), b, work, in); !errors.Is(err, wireauth.ErrStorageOff) {
		t.Fatalf("SendResult with storage off = %v, want the storage-off refusal", err)
	}
	a, aAsks := gated(t, a, t.TempDir(), "off")
	prepared, err := taskdoor.PrepareResult(t.Context(), a, in, local, in.Fingerprint)
	if err == nil {
		prepared.Close()
	}
	if !errors.Is(err, wireauth.ErrStorageOff) {
		t.Fatalf("PrepareResult with storage off = %v, want the storage-off refusal", err)
	}
	if got := append(bAsks.all(), aAsks.all()...); len(got) != 0 {
		t.Fatalf("a task result with storage off asked the relay's store: %v", got)
	}
}

// A store that cannot say whether it may store (a wrapper that hides the
// permission) keeps the old road: the work is attempted and the upload itself
// refuses. It still asks the relay nothing, because the store's own existence
// question is closed while storage is off, and nothing is refused that used to
// succeed.
func TestAStoreThatHidesItsPermissionStillRefusesAndStillAsksNothing(t *testing.T) {
	a, _ := copyHomes(t)
	a, asks := gated(t, a, t.TempDir(), "off")
	a.Store = struct{ blobstore.Store }{a.Store}
	_, err := taskdoor.SendCopy(t.Context(), a, copyRepo(t))
	if !errors.Is(err, wireauth.ErrStorageOff) {
		t.Fatalf("SendCopy through a wrapped store with storage off = %v, want the storage-off refusal", err)
	}
	if got := asks.all(); len(got) != 0 {
		t.Fatalf("a wrapped store with storage off asked the relay: %v", got)
	}
}

// ReceiveCopy of a head that cannot be read still reports the head, not storage,
// when the store may store: the gate must not change an ordinary refusal.
func TestAnUnreadableHeadIsStillAnUnreadableHeadWhenStorageIsOn(t *testing.T) {
	_, b := copyHomes(t)
	b, _ = gated(t, b, t.TempDir(), "on")
	id, err := cell.NewID(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	bad := taskdoor.Input{Cell: id, Head: hex.EncodeToString([]byte("short"))}
	err = taskdoor.ReceiveCopy(t.Context(), b, bad, t.TempDir()+"/work")
	if err == nil || errors.Is(err, wireauth.ErrStorageOff) {
		t.Fatalf("ReceiveCopy of a bad head with storage on = %v, want the head refusal", err)
	}
}
