package serve

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/acl"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/coder/websocket"
)

// VALIDATION CONTRACT for finishing a removal the chats' host could not finish,
// written before the code it checks (overnight fixes, fix 1):
//
//  1. A person removed while the chats' host is unreachable has every chat of
//     theirs deleted on that host once it answers again, with no second removal
//     by anyone.
//  2. That still holds when the organization server restarts between the removal
//     and the host's return.
//  3. Until the cleanup finishes, nothing of the removed person can be woken or
//     given a model bearer: their chats' model door answers 401 and a guest
//     can neither read nor attach.
//  4. A removal with the host up behaves as it always did: the chats are deleted
//     at once and nothing is left pending, no timer included.
//  5. The retry does not spin: each pass makes a bounded number of host calls
//     however many chats there are, the wait between passes grows to a ceiling,
//     the log carries a bounded number of lines, and nothing is scheduled once
//     the work is done.
//  7. (Added by the design) The retry only deletes what existed when the person
//     was removed: a chat made later under the same handle, by a person who
//     joined again, is left alone.
//
// Item numbers follow the PR body; item 6 is the plain-HTTP warning, proved in
// cmd/codeaf.

// flakyHost fronts the fake chats' host with a switch, so a test can take the
// host away and give it back at the same address. A down host drops the
// connection, which is what a stopped container does to a caller.
type flakyHost struct {
	down  atomic.Bool
	calls atomic.Int64
	srv   *httptest.Server
}

func newFlakyHost(t *testing.T, inner http.Handler) *flakyHost {
	t.Helper()
	f := &flakyHost{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls.Add(1)
		if f.down.Load() {
			panic(http.ErrAbortHandler)
		}
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// fakeTimers stands in for the clock the retry waits on: a test sees every wait
// the server asked for and decides when each one ends, so nothing sleeps.
type fakeTimers struct {
	mu      sync.Mutex
	waits   []time.Duration
	pending []*fakeTimer
}

type fakeTimer struct {
	run     func()
	stopped bool
	fired   bool
}

func (f *fakeTimers) after(d time.Duration, run func()) func() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	timer := &fakeTimer{run: run}
	f.waits = append(f.waits, d)
	f.pending = append(f.pending, timer)
	return func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		was := !timer.stopped && !timer.fired
		timer.stopped = true
		return was
	}
}

// armed is how many waits are running right now.
func (f *fakeTimers) armed() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, timer := range f.pending {
		if !timer.stopped && !timer.fired {
			n++
		}
	}
	return n
}

func (f *fakeTimers) asked() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.waits...)
}

// fire ends the oldest running wait and runs what it was waiting to do, to the
// end, in the caller's goroutine.
func (f *fakeTimers) fire(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	var next *fakeTimer
	for _, timer := range f.pending {
		if !timer.stopped && !timer.fired {
			next = timer
			break
		}
	}
	if next != nil {
		next.fired = true
	}
	f.mu.Unlock()
	if next == nil {
		t.Fatal("no wait is running, so there is nothing to end")
	}
	next.run()
}

type logLines struct {
	mu    sync.Mutex
	lines []string
}

func (l *logLines) printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logLines) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.lines)
}

func (l *logLines) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.lines...)
}

// removalRig is the hosted scene with a chats' host that can go away, and with
// the retry's waits and log lines in the test's hands.
type removalRig struct {
	*hostedRig
	front  *flakyHost
	timers *fakeTimers
	logs   *logLines
}

func newRemovalRig(t *testing.T) *removalRig {
	t.Helper()
	rg := newHostedRig(t)
	r := &removalRig{hostedRig: rg, front: newFlakyHost(t, rg.host), timers: &fakeTimers{}, logs: &logLines{}}
	r.restart(t)
	return r
}

// restart is the organization server stopping and starting again over the same
// store, pointed at the same host.
func (r *removalRig) restart(t *testing.T) {
	t.Helper()
	r.configure(t, func(c *HostedConfig) {
		c.URL = r.front.srv.URL
		c.removalAfter = r.timers.after
		c.removalLogf = r.logs.printf
	})
}

func (r *removalRig) remove(t *testing.T, who string) {
	t.Helper()
	if w := r.send("owner", http.MethodDelete, "/v1/people/"+who+"/sessions", nil, nil); w.Code != http.StatusOK {
		t.Fatalf("removing %s = %d %s", who, w.Code, w.Body)
	}
}

// pendingRemovals is how many removed people the server still owes a cleanup.
func (r *removalRig) pendingRemovals(t *testing.T) int {
	t.Helper()
	rows, err := r.st.HostedRemovals()
	if err != nil {
		t.Fatalf("reading the removals still owed: %v", err)
	}
	return len(rows)
}

// assertClosed is contract item 3: nothing of the removed person's chats can be
// woken or given a model bearer. guest was let into guestChat before the removal.
func (r *removalRig) assertClosed(t *testing.T, tokens map[string]string, guest, guestChat string) {
	t.Helper()
	for name, token := range tokens {
		if code := r.model(token); code != http.StatusUnauthorized {
			t.Errorf("while the cleanup is pending, %s's model call = %d, want 401", name, code)
		}
	}
	if w := r.send(guest, http.MethodGet, hostedSessionsPath+"/"+guestChat, nil, nil); w.Code != http.StatusNotFound || w.Body.String() != hostNoSuchChat {
		t.Errorf("while the cleanup is pending, the guest's read = %d %s, want 404 %q", w.Code, w.Body, hostNoSuchChat)
	}
	srv := httptest.NewServer(r.h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	conn, res, _ := websocket.Dial(ctx, wireURL(srv, guestChat), &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + r.tokens[guest]}},
	})
	if conn != nil {
		_ = conn.CloseNow()
	}
	if res == nil || res.StatusCode != http.StatusNotFound {
		t.Errorf("while the cleanup is pending, the guest's attach did not answer 404: %v", res)
	}
}

func (r *removalRig) assertGone(t *testing.T, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if owner := r.host.ownerOf(id); owner != "" {
			t.Errorf("chat %s is still on the chats' host, owned by %q", id, owner)
		}
		if _, found, err := r.st.Resource(store.SessionKind, id); err != nil || found {
			t.Errorf("chat %s is still in the registry (found=%v, err=%v)", id, found, err)
		}
	}
}

func (r *removalRig) assertKept(t *testing.T, owner string, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if got := r.host.ownerOf(id); got != owner {
			t.Errorf("chat %s is owned by %q on the chats' host, want %q", id, got, owner)
		}
		if _, found, err := r.st.Resource(store.SessionKind, id); err != nil || !found {
			t.Errorf("chat %s left the registry (found=%v, err=%v)", id, found, err)
		}
	}
}

// removedWhileHostIsDown builds the scene every pending-cleanup test starts
// from: mia has two chats (santosh was let into the first), santosh has one of
// his own, mia's model bearers work, and then the host goes away and mia is
// removed. The removal itself succeeds.
func removedWhileHostIsDown(t *testing.T) (r *removalRig, miaChats []string, miaTokens map[string]string, kept string) {
	t.Helper()
	r = newRemovalRig(t)
	first, second := r.create(t, "mia"), r.create(t, "mia")
	hostedGrant(t, r.hostedRig, "mia", first, "santosh", "edit")
	kept = r.create(t, "santosh")
	miaTokens = map[string]string{"first": r.host.tokenOf(first), "second": r.host.tokenOf(second)}
	for name, token := range miaTokens {
		if code := r.model(token); code != http.StatusOK {
			t.Fatalf("before the removal, %s's model call = %d", name, code)
		}
	}
	r.front.down.Store(true)
	r.remove(t, "mia")
	return r, []string{first, second}, miaTokens, kept
}

// Contract 1 and 3: the host comes back and the next retry finishes the removal.
func TestARemovalTheHostCouldNotFinishIsFinishedWhenItReturns(t *testing.T) {
	r, chats, tokens, kept := removedWhileHostIsDown(t)
	if r.pendingRemovals(t) != 1 || r.timers.armed() != 1 {
		t.Fatalf("after a removal the host could not finish, pending = %d, retries waiting = %d, want 1 and 1", r.pendingRemovals(t), r.timers.armed())
	}
	r.assertClosed(t, tokens, "santosh", chats[0])

	// Still down: the retry runs, fails, and waits again; nothing is lost and nothing is opened.
	r.timers.fire(t)
	if r.pendingRemovals(t) != 1 || r.timers.armed() != 1 {
		t.Fatalf("after a retry that found the host still down, pending = %d, retries waiting = %d, want 1 and 1", r.pendingRemovals(t), r.timers.armed())
	}
	r.assertKept(t, "mia", chats...)
	r.assertClosed(t, tokens, "santosh", chats[0])

	r.front.down.Store(false)
	r.timers.fire(t)
	r.assertGone(t, chats...)
	r.assertKept(t, "santosh", kept)
	if r.pendingRemovals(t) != 0 || r.timers.armed() != 0 {
		t.Errorf("once the host confirmed, pending = %d, retries waiting = %d, want 0 and 0", r.pendingRemovals(t), r.timers.armed())
	}
	for name, token := range tokens {
		if code := r.model(token); code != http.StatusUnauthorized {
			t.Errorf("after the cleanup, %s's model call = %d, want 401", name, code)
		}
	}
}

// Contract 2 and 3: the same, with the organization server restarting while the host is away.
func TestARemovalTheHostCouldNotFinishSurvivesARestart(t *testing.T) {
	r, chats, tokens, kept := removedWhileHostIsDown(t)

	r.restart(t)
	if r.pendingRemovals(t) != 1 {
		t.Fatalf("after a restart, pending = %d, want the removal still owed", r.pendingRemovals(t))
	}
	if r.timers.armed() != 1 {
		t.Fatalf("after a restart, retries waiting = %d, want the server to schedule the cleanup itself", r.timers.armed())
	}
	r.assertKept(t, "mia", chats...)
	r.assertClosed(t, tokens, "santosh", chats[0])

	r.front.down.Store(false)
	r.timers.fire(t)
	r.assertGone(t, chats...)
	r.assertKept(t, "santosh", kept)
	if r.pendingRemovals(t) != 0 || r.timers.armed() != 0 {
		t.Errorf("after the restarted server finished, pending = %d, retries waiting = %d, want 0 and 0", r.pendingRemovals(t), r.timers.armed())
	}
}

// Contract 4: with the host up nothing changes from what a removal always did.
func TestARemovalWithTheHostUpLeavesNothingPending(t *testing.T) {
	r := newRemovalRig(t)
	first, second := r.create(t, "mia"), r.create(t, "mia")
	kept := r.create(t, "santosh")
	r.remove(t, "mia")
	r.assertGone(t, first, second)
	r.assertKept(t, "santosh", kept)
	if r.pendingRemovals(t) != 0 {
		t.Errorf("pending = %d after a removal the host finished, want 0", r.pendingRemovals(t))
	}
	if r.timers.armed() != 0 || len(r.timers.asked()) != 0 {
		t.Errorf("a removal the host finished scheduled a retry: waits asked = %v", r.timers.asked())
	}
}

// Contract 5: the retry backs off, makes few calls, logs little, and stops.
func TestTheRemovalRetryBacksOffLogsLittleAndStopsWhenDone(t *testing.T) {
	r := newRemovalRig(t)
	var chats []string
	for range 3 {
		chats = append(chats, r.create(t, "mia"))
	}
	r.front.down.Store(true)
	r.remove(t, "mia")

	const passes = 12
	for pass := range passes {
		before := r.front.calls.Load()
		r.timers.fire(t)
		if used := r.front.calls.Load() - before; used > 4 {
			t.Fatalf("pass %d made %d calls to a host that is down; a pass must give up at the first failure, not try each of %d chats", pass, used, len(chats))
		}
		if r.timers.armed() != 1 {
			t.Fatalf("after failed pass %d, retries waiting = %d, want exactly 1", pass, r.timers.armed())
		}
	}
	waits := r.timers.asked()
	if len(waits) != passes+1 {
		t.Fatalf("waits asked for = %d, want %d (one after the removal, one after each failed pass): %v", len(waits), passes+1, waits)
	}
	if waits[0] < time.Second {
		t.Errorf("the first wait is %v; it must not be a spin", waits[0])
	}
	for i := 1; i < len(waits); i++ {
		if waits[i] < waits[i-1] {
			t.Errorf("wait %d (%v) is shorter than the one before (%v); a retry backs off, it does not speed up", i, waits[i], waits[i-1])
		}
		if waits[i] > 10*time.Minute {
			t.Errorf("wait %d is %v; the ceiling must stay within ten minutes so a returned host is noticed", i, waits[i])
		}
	}
	if waits[len(waits)-1] <= waits[0] {
		t.Errorf("the waits never grew: %v", waits)
	}
	if n := r.logs.count(); n > 5 {
		t.Errorf("%d log lines for %d failed passes, want a handful: %q", n, passes, r.logs.all())
	}

	r.front.down.Store(false)
	r.timers.fire(t)
	r.assertGone(t, chats...)
	if r.timers.armed() != 0 || r.pendingRemovals(t) != 0 {
		t.Errorf("after the work is done, retries waiting = %d and pending = %d, want 0 and 0", r.timers.armed(), r.pendingRemovals(t))
	}
	if n := len(r.timers.asked()); n != len(waits) {
		t.Errorf("a wait was asked for after the work was done (%d waits, was %d)", n, len(waits))
	}
	if n := r.logs.count(); n > 6 {
		t.Errorf("%d log lines in all, want a handful: %q", n, r.logs.all())
	}
}

// Contract 7: a chat of the same handle made after the removal is not the removal's to delete.
func TestThePendingCleanupLeavesAChatMadeAfterTheRemoval(t *testing.T) {
	r, chats, _, _ := removedWhileHostIsDown(t)

	// The person joins again and starts a chat while the old cleanup is still owed.
	row, found, err := r.st.Resource(store.SessionKind, chats[0])
	if err != nil || !found {
		t.Fatalf("the old chat is not in the registry: found=%v, err=%v", found, err)
	}
	fresh := "hs_0123456789abcdef01234567"
	row.ID, row.CreatedAt, row.Visibility = fresh, time.Now().Add(time.Hour).UTC(), acl.Private
	if err := r.st.PutResource(row); err != nil {
		t.Fatal(err)
	}
	r.host.adopt(fresh, "mia")

	r.front.down.Store(false)
	r.timers.fire(t)
	r.assertGone(t, chats...)
	r.assertKept(t, "mia", fresh)
	if r.pendingRemovals(t) != 0 {
		t.Errorf("pending = %d once the old chats were deleted, want 0", r.pendingRemovals(t))
	}
}
