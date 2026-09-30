package directorytest

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/directory"
)

// cellB is a second cell, for the cases about another device's lease.
const cellB = "01J0000000000000000000000B"

// The moments the lease cases look at, on the directory's clock, counted from
// the lease's creation at 0. Its stored expiry is one TTL (90 s), so a case that
// still sees it held at leasePast is kept live by something other than its
// stored expiry, and one that sees it free at leaseLapsed has lost that too: a
// sign of life at leaseAge vouches until leaseAge plus one TTL, which is 135 s.
const (
	leaseAge    = 45 * time.Second
	leasePast   = 95 * time.Second
	leaseLapsed = 140 * time.Second
)

// timeline moves the directory's clock to named moments of one case.
type timeline struct {
	w   watchEnv
	now time.Duration
}

func (w watchEnv) timeline() *timeline { return &timeline{w: w} }

// to waits until the clock reads d since the case began.
func (t *timeline) to(d time.Duration) {
	t.w.env.clock.Wait(d - t.now)
	t.now = d
}

// leaseCases are the named rows of section 21.11 of the contract that a relay
// proves; the client's rows are proved where the client lives.
var leaseCases = map[string]func(watchEnv){
	"SocketKeepsLeaseLive":                socketKeepsLeaseLive,
	"SilentSocketLapses":                  silentSocketLapses,
	"DeadSocketLapses":                    deadSocketLapses,
	"TakeoverSilencesOldSocket":           takeoverSilencesOldSocket,
	"HoldOfUnheldCellVouchesNothing":      holdOfUnheldCellVouchesNothing,
	"HoldAtStaleFenceVouchesNothing":      holdAtStaleFenceVouchesNothing,
	"OtherDevicesSocketVouchesNothing":    otherDevicesSocketVouchesNothing,
	"ReleaseEndsVouching":                 releaseEndsVouching,
	"SocketWithoutHoldVouchesNothing":     socketWithoutHoldVouchesNothing,
	"MalformedHoldIsRefused":              malformedHoldIsRefused,
	"TooManyHoldsIsRefused":               tooManyHoldsIsRefused,
	"UpgradeAnnouncesVouching":            upgradeAnnouncesVouching,
	"PingIsNotAChange":                    pingIsNotAChange,
	"LapseBumpsNothing":                   lapseBumpsNothing,
	"HoldsAreSignedWithTheRequest":        holdsAreSignedWithTheRequest,
	"BeatUnderVouchIsSilent":              beatUnderVouchIsSilent,
	"TwoFencesOfOneCellVouchEachOwnFence": twoFencesOfOneCellVouchEachOwnFence,
}

// RunLease runs every lease-liveness case of the contract (section 21.11)
// against a fresh rig each. Like RunWatch, the cases run in parallel, each on
// its own identity; a live relay's cases take as long as the lease does.
func RunLease(t *testing.T, factory WatchRigFactory) {
	for name, fn := range leaseCases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fn(newWatchEnv(t, factory(t)))
		})
	}
}

// openHolding dials the watch route as device, naming holds, and reads the
// opening version so the case starts from a quiet socket.
func (w watchEnv) openHolding(device string, holds ...directory.Hold) *sock {
	w.t.Helper()
	return w.dialHolding(device, holds).opened()
}

func (w watchEnv) dialHolding(device string, holds []directory.Hold) *sock {
	w.t.Helper()
	dialCtx, cancel := context.WithTimeout(context.Background(), frameWait)
	defer cancel()
	conn, resp, err := DialWatchHolding(dialCtx, w.rig.Base, w.rig.Sign(device), holds)
	if err != nil {
		w.t.Fatalf("watch dial holding %v: %v (%s)", holds, err, describe(resp))
	}
	s := startSock(w.t, conn)
	s.resp = resp
	return s
}

// opened consumes the opening version frame.
func (s *sock) opened() *sock {
	s.t.Helper()
	s.version()
	return s
}

// ping sends a ping and waits for its pong, skipping version frames that a
// change made meanwhile. When it returns the relay has recorded the sign of life.
func (s *sock) ping() {
	s.t.Helper()
	s.send(websocket.MessageText, "ping")
	for {
		text := s.text()
		if text == "pong" {
			return
		}
		if _, ok := parseVersion(text); !ok {
			s.t.Fatalf("frame %q while waiting for a pong", text)
		}
	}
}

// hold is the hold a device names for the lease it believes it has.
func hold(cellID string, fence uint64) directory.Hold {
	return directory.Hold{Cell: cellID, Fence: fence}
}

// held says whether device sees the lease on id as held, reading the lease
// the way the home list draws it: its expiry against the relay's own now.
func (w watchEnv) held(device, id string) bool {
	w.t.Helper()
	v, err := w.dirAs(device).Cell(ctx, id)
	must(w.t, err)
	return v.Cell.Lease.Expires > v.Now
}

// wantHeld asserts the lease on id reads held and that a plain acquire by by,
// another device, is refused.
func (w watchEnv) wantHeld(by, id string) {
	w.t.Helper()
	w.wantReading(by, id, true)
	_, err := w.dirAs(by).Acquire(ctx, id, directory.AcquireOpts{})
	wantErr(w.t, err, directory.ErrLeaseHeld)
}

// wantFree asserts the lease on id reads free and that a plain acquire by by
// succeeds.
func (w watchEnv) wantFree(by, id string) {
	w.t.Helper()
	w.wantReading(by, id, false)
	_, err := w.dirAs(by).Acquire(ctx, id, directory.AcquireOpts{})
	must(w.t, err)
}

// wantReading asserts only what a list shows, so a case can go on watching a
// lease that an acquire would end.
func (w watchEnv) wantReading(by, id string, held bool) {
	w.t.Helper()
	if got := w.held(by, id); got != held {
		w.t.Fatalf("lease on %s reads held=%v, want %v", id, got, held)
	}
}

// A holder that only pings keeps its lease live past the stored expiry.
func socketKeepsLeaseLive(w watchEnv) {
	w.create()
	s := w.openHolding(devA, hold(cell, 1))
	tl := w.timeline()
	tl.to(leaseAge)
	s.ping()
	tl.to(leasePast)
	w.wantHeld(devB, cell)
}

// A socket that never pings counts from its accept time.
func silentSocketLapses(w watchEnv) {
	w.create()
	tl := w.timeline()
	tl.to(leaseAge)
	w.openHolding(devA, hold(cell, 1))
	tl.to(leasePast)
	w.wantReading(devB, cell, true)
	tl.to(leaseLapsed)
	w.wantFree(devB, cell)
}

// A socket that stops pinging stops vouching TTL after its last ping.
func deadSocketLapses(w watchEnv) {
	w.create()
	s := w.openHolding(devA, hold(cell, 1))
	tl := w.timeline()
	tl.to(leaseAge)
	s.ping()
	tl.to(leasePast)
	w.wantReading(devB, cell, true)
	tl.to(leaseLapsed)
	w.wantFree(devB, cell)
}

// After a takeover the old holder's socket, still pinging, names a fence that
// is gone: the new lease lapses on its own clock.
func takeoverSilencesOldSocket(w watchEnv) {
	w.create()
	old := w.openHolding(devA, hold(cell, 1))
	tl := w.timeline()
	tl.to(leaseAge)
	old.ping()
	_, err := w.dirAs(devB).Acquire(ctx, cell, directory.AcquireOpts{Force: true})
	must(w.t, err)
	tl.to(leasePast)
	old.ping()
	w.wantReading(devA, cell, true)
	tl.to(leaseLapsed)
	w.wantFree(devA, cell)
}

// A hold of a cell the device does not hold, or that does not exist, is
// accepted and vouches for nothing: the holder's lease is neither kept live nor
// shortened, and nothing about it is told.
func holdOfUnheldCellVouchesNothing(w watchEnv) {
	_, err := w.dirAs(devB).Create(ctx, cellB, directory.CellInit{Head: head, Class: "chat", Size: 10, Title: "t"})
	must(w.t, err)
	before, err := w.dirAs(devB).Cell(ctx, cellB)
	must(w.t, err)
	s := w.openHolding(devA, hold(cellB, 1), hold("01J0000000000000000000000Z", 1))
	s.wantSilent()
	tl := w.timeline()
	tl.to(leaseAge)
	s.ping()
	s.wantSilent()
	after, err := w.dirAs(devB).Cell(ctx, cellB)
	must(w.t, err)
	if after.Cell.Lease != before.Cell.Lease {
		w.t.Fatalf("a hold changed the holder's lease: %+v -> %+v", before.Cell.Lease, after.Cell.Lease)
	}
	tl.to(leasePast)
	s.ping()
	w.wantFree(devA, cellB)
}

// A hold at a fence the lease has moved past vouches for nothing.
func holdAtStaleFenceVouchesNothing(w watchEnv) {
	w.create()
	_, err := w.dirAs(devA).Acquire(ctx, cell, directory.AcquireOpts{})
	must(w.t, err)
	s := w.openHolding(devA, hold(cell, 1))
	tl := w.timeline()
	tl.to(leaseAge)
	s.ping()
	tl.to(leasePast)
	s.ping()
	w.wantFree(devB, cell)
}

// A socket of another device that names a cell held by device A vouches for nothing.
func otherDevicesSocketVouchesNothing(w watchEnv) {
	w.create()
	s := w.openHolding(devB, hold(cell, 1))
	tl := w.timeline()
	tl.to(leaseAge)
	s.ping()
	tl.to(leasePast)
	w.wantFree(devB, cell)
}

// After a release the socket that named the lease no longer vouches.
func releaseEndsVouching(w watchEnv) {
	w.create()
	s := w.openHolding(devA, hold(cell, 1))
	w.timeline().to(leaseAge)
	s.ping()
	must(w.t, w.dirAs(devA).Release(ctx, cell, 1))
	s.ping()
	w.wantFree(devB, cell)
}

// A socket with no hold is the home screen's and vouches for nothing.
func socketWithoutHoldVouchesNothing(w watchEnv) {
	w.create()
	s := w.openHolding(devA)
	tl := w.timeline()
	tl.to(leaseAge)
	s.ping()
	tl.to(leasePast)
	s.ping()
	w.wantFree(devB, cell)
}

// refusedHolds are values the relay must answer 400 before the upgrade.
func refusedHolds() []string {
	return []string{
		"nocolon", ":5", cell + ":", cell + ":-1", cell + ":+1", cell + ": 1", cell + ":1x",
		cell + ":0x10", cell + ":9007199254740992", cell + ":12345678901234567",
		strings.Repeat("c", directory.MaxHoldCellBytes+1) + ":1", "",
	}
}

// acceptedHolds are values right at the edge that the relay must still accept.
func acceptedHolds() []string {
	return []string{
		cell + ":0", "a:b:3", cell + ":9007199254740991", cell + ":0000000000000007",
		strings.Repeat("c", directory.MaxHoldCellBytes) + ":1",
	}
}

func malformedHoldIsRefused(w watchEnv) {
	for _, v := range refusedHolds() {
		w.wantHoldQuery("hold="+url.QueryEscape(v), http.StatusBadRequest)
	}
	for _, v := range acceptedHolds() {
		w.wantHoldQuery("hold="+url.QueryEscape(v), http.StatusSwitchingProtocols)
	}
}

func tooManyHoldsIsRefused(w watchEnv) {
	query := func(n int) string {
		q := make([]string, n)
		for i := range q {
			q[i] = "hold=" + url.QueryEscape(cell+":"+strconv.Itoa(i+1))
		}
		return strings.Join(q, "&")
	}
	w.wantHoldQuery(query(directory.MaxHolds), http.StatusSwitchingProtocols)
	w.wantHoldQuery(query(directory.MaxHolds+1), http.StatusBadRequest)
}

// wantHoldQuery dials with a raw query and asserts the status: a refusal is a
// 400 bad_request before the upgrade, and an accepted socket is closed at once.
func (w watchEnv) wantHoldQuery(query string, status int) {
	w.t.Helper()
	dialCtx, cancel := context.WithTimeout(context.Background(), frameWait)
	defer cancel()
	conn, resp, err := dialWatchQuery(dialCtx, w.rig.Base, w.rig.Sign(devA), query)
	if status == http.StatusSwitchingProtocols {
		if err != nil {
			w.t.Fatalf("query %q refused: %v (%s)", query, err, describe(resp))
		}
		conn.CloseNow()
		return
	}
	wantRefusal(w.t, conn, resp, err, status, "bad_request")
}

func upgradeAnnouncesVouching(w watchEnv) {
	for _, holds := range [][]directory.Hold{nil, {hold(cell, 1)}} {
		s := w.dialHolding(devA, holds)
		if got := s.resp.Header.Get(directory.VouchHeader); got != "1" {
			w.t.Fatalf("101 answer has %s=%q with holds %v, want 1", directory.VouchHeader, got, holds)
		}
	}
}

// A ping is neither a version nor a frame: nothing changes for anyone.
func pingIsNotAChange(w watchEnv) {
	w.create()
	s := w.openHolding(devA, hold(cell, 1))
	other := w.open(devB).opened()
	n := s.last
	for range 3 {
		w.env.clock.Wait(time.Second)
		s.ping()
	}
	s.wantSilent()
	other.wantSilent()
	if got := w.listVersion(devA); got != n {
		w.t.Fatalf("list version %d after pings, want %d", got, n)
	}
}

// A vouched lease that lapses changes no version: it lapses at read time.
func lapseBumpsNothing(w watchEnv) {
	w.create()
	s := w.openHolding(devA, hold(cell, 1))
	n := s.last
	tl := w.timeline()
	tl.to(leaseAge)
	s.ping()
	tl.to(leasePast)
	w.wantReading(devB, cell, true)
	tl.to(leaseLapsed)
	l, err := w.dirAs(devB).List(ctx)
	must(w.t, err)
	if c := l.Cells[cell]; c.Lease.Expires > l.Now {
		w.t.Fatalf("the list still reads the lease held: %+v at %d", c.Lease, l.Now)
	}
	if got := w.listVersion(devB); got != n {
		w.t.Fatalf("list version %d after a lapse, want %d", got, n)
	}
	s.wantSilent()
}

// The holds are part of what the request signature covers: a holder's signed
// request cannot be replayed with another hold added.
func holdsAreSignedWithTheRequest(w watchEnv) {
	w.create()
	signed := w.rig.Sign(devA)
	tampered := func(r *http.Request, body []byte) {
		signed(r, body)
		r.URL.RawQuery = directory.HoldQuery([]directory.Hold{hold(cell, 1)})
	}
	dialCtx, cancel := context.WithTimeout(context.Background(), frameWait)
	defer cancel()
	conn, resp, err := DialWatchHolding(dialCtx, w.rig.Base, tampered, nil)
	if conn != nil {
		conn.CloseNow()
		w.t.Fatal("a watch request with a hold added after signing was accepted")
	}
	if err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
		w.t.Fatalf("tampered hold answered %v (%v), want 401", describe(resp), err)
	}
	resp.Body.Close()
}

// Two holds for one cell at different fences are both stored and each vouches
// for its own fence only: the live one keeps the lease, the stale one does not.
func twoFencesOfOneCellVouchEachOwnFence(w watchEnv) {
	w.create()
	_, err := w.dirAs(devA).Acquire(ctx, cell, directory.AcquireOpts{})
	must(w.t, err)
	s := w.openHolding(devA, hold(cell, 1), hold(cell, 2), hold(cell, 2))
	tl := w.timeline()
	tl.to(leaseAge)
	s.ping()
	tl.to(leasePast)
	w.wantHeld(devB, cell)
}

// A write bumps exactly when held-ness flips because of it: a beat that lands
// after the stored expiry, while a socket vouches, finds the lease held already.
func beatUnderVouchIsSilent(w watchEnv) {
	w.create()
	s := w.openHolding(devA, hold(cell, 1))
	n := s.last
	tl := w.timeline()
	tl.to(leaseAge)
	s.ping()
	tl.to(leasePast)
	_, err := w.dirAs(devA).Heartbeat(ctx, cell, directory.Beat{Fence: 1})
	must(w.t, err)
	s.wantSilent()
	if got := w.listVersion(devA); got != n {
		w.t.Fatalf("list version %d after a beat under a vouch, want %d", got, n)
	}
}
