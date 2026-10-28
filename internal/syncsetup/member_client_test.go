package syncsetup

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"github.com/Agent-Field/codeaf/internal/store"
)

// peerFrameCounter observes actual socket bytes at the relay, independently of
// client counters. It parses complete masked/unmasked frames across I/O splits.
type peerFrameCounter struct {
	total *peerFrameCounter
	mu    sync.Mutex
	b     []byte
	count atomic.Uint64
}

func (c *peerFrameCounter) add(b []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.b = append(c.b, b...)
	for len(c.b) >= 2 {
		n := uint64(c.b[1] & 127)
		header := 2
		if n == 126 {
			if len(c.b) < 4 {
				return
			}
			n = uint64(binary.BigEndian.Uint16(c.b[2:4]))
			header = 4
		}
		if n == 127 {
			if len(c.b) < 10 {
				return
			}
			n = binary.BigEndian.Uint64(c.b[2:10])
			header = 10
		}
		if c.b[1]&128 != 0 {
			header += 4
		}
		if n > rafttransport.MaxExchangeBytes {
			panic("unbounded peer frame")
		}
		if uint64(len(c.b)) < uint64(header)+n {
			return
		}
		c.count.Add(1)
		if c.total != nil {
			c.total.count.Add(1)
		}
		c.b = c.b[header+int(n):]
	}
}

type peerCountConn struct {
	net.Conn
	in, out *peerFrameCounter
}

func (c peerCountConn) Read(b []byte) (int, error) {
	n, e := c.Conn.Read(b)
	c.in.add(b[:n])
	return n, e
}
func (c peerCountConn) Write(b []byte) (int, error) {
	n, e := c.Conn.Write(b)
	c.out.add(b[:n])
	return n, e
}

type peerCountWriter struct {
	http.ResponseWriter
	in, out *peerFrameCounter
}

func (w peerCountWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, rw, e := w.ResponseWriter.(http.Hijacker).Hijack()
	if e != nil {
		return nil, nil, e
	}
	// Each socket owns its framing buffer; independent streams can split frames
	// concurrently while contributing to the same aggregate counters.
	wrapped := peerCountConn{c, &peerFrameCounter{total: w.in}, &peerFrameCounter{total: w.out}}
	// The HTTP handshake already flushed through ResponseWriter before Hijack.
	rw.Writer.Reset(wrapped)
	return wrapped, rw, nil
}

type memberRig struct {
	departures        chan struct{}
	parkExchanges     atomic.Bool
	storage           string
	t                 *testing.T
	clock             *directorytest.FakeClock
	svc               *relayserve.Service
	server            *httptest.Server
	requests          atomic.Uint64
	in, out           peerFrameCounter
	watchIn, watchOut peerFrameCounter
	homes             []*Sync
	interrupt         atomic.Bool
}

func newMemberRig(t *testing.T) *memberRig {
	t.Helper()
	t.Setenv(config.ProfileDirEnv, "")
	r := &memberRig{t: t, clock: directorytest.NewFakeClock(), departures: make(chan struct{}, rafttransport.MaxInflight)}
	r.clock.Advance(time.Now().Sub(r.clock.Now()))
	r.storage = t.TempDir()
	r.svc = relayserve.New(relayserve.Config{Store: r.storage, Now: r.clock.Now, After: r.clock.After})
	r.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		r.requests.Add(1)
		if q.URL.Path == clusterctl.ForwardPath {
			defer func() {
				select {
				case r.departures <- struct{}{}:
				default:
				}
			}()
		}
		if q.URL.Path == "/v2/cluster/enroll" && r.interrupt.Swap(false) {
			// Commit admission and lose its reply before the client's store is opened.
			recorder := httptest.NewRecorder()
			r.svc.Handler.ServeHTTP(recorder, q)
			http.Error(w, "interrupted", 503)
			return
		}
		if q.URL.Path == rafttransport.ExchangePath && r.parkExchanges.Load() {
			recorder := httptest.NewRecorder()
			r.svc.Handler.ServeHTTP(recorder, q)
			if recorder.Code != http.StatusOK {
				w.WriteHeader(recorder.Code)
				w.Write(recorder.Body.Bytes())
				return
			}
			var response rafttransport.Response
			if json.Unmarshal(recorder.Body.Bytes(), &response) != nil {
				panic("invalid real exchange")
			}
			response.Messages = nil
			raw, _ := rafttransport.Encode(response)
			w.Write(raw)
			return
		}
		if strings.EqualFold(q.Header.Get("Upgrade"), "websocket") {
			in, out := &r.in, &r.out
			if q.URL.Path == "/v1/dir/watch" {
				in, out = &r.watchIn, &r.watchOut
			}
			r.svc.Handler.ServeHTTP(peerCountWriter{w, in, out}, q)
		} else {
			r.svc.Handler.ServeHTTP(w, q)
		}
	}))
	t.Cleanup(func() {
		for _, s := range r.homes {
			s.stopMember()
		}
		r.server.Close()
		if r.svc != nil {
			r.svc.Close()
		}
	})
	return r
}
func (r *memberRig) home(id *identity.Identity) *Sync {
	r.t.Helper()
	home := machine(r.t, r.server.URL)
	if id != nil {
		_, err := identity.Adopt(home, *id, true)
		mustNil(r.t, err)
	}
	own, err := identity.Load(home)
	mustNil(r.t, err)
	dev, err := identity.Device(home)
	mustNil(r.t, err)
	s := buildAt(home, r.server.URL, own, dev, DefaultInterval, r.clock.Now)
	r.homes = append(r.homes, s)
	return s
}
func waitMember(t *testing.T, s *Sync, predicate func(*personalMember) bool) {
	t.Helper()
	timer := time.NewTimer(requestTimeout)
	defer timer.Stop()
	for {
		s.memberMu.Lock()
		m := s.member
		if m == nil {
			s.memberMu.Unlock()
			t.Fatal("R9 no running learner")
		}
		m.mu.Lock()
		s.memberMu.Unlock()
		err := m.failure
		pass := predicate(m)
		changed := m.changed
		m.mu.Unlock()
		if err != nil {
			t.Fatalf("R9 peer failed: %v", err)
		}
		if pass {
			return
		}
		select {
		case <-changed:
		case <-timer.C:
			t.Fatal("R9 learner did not reach its applied state")
		}
	}
}
func readyMember(t *testing.T, s *Sync) {
	t.Helper()
	mustNil(t, s.memberRound(t.Context()))
	waitMember(t, s, func(m *personalMember) bool { return m.idle && m.status.Applied >= m.record.Grant.Barrier })
}

// R16 denies a saved peer request as soon as device revocation commits, even
// when the membership revision and transport generation have not changed.
func TestR16CommittedRevocationInvalidatesPeerView(t *testing.T) {
	r := newMemberRig(t)
	a := r.home(nil)
	id := a.Identity
	b := r.home(&id)
	mustNil(t, b.Dir.PutDevice(t.Context(), b.Device.ID(), directory.Device{V: 1, Name: "c2VhbGVk"}))
	readyMember(t, b)
	m := b.member
	m.mu.Lock()
	request, err := m.request()
	m.mu.Unlock()
	mustNil(t, err)
	b.stopMember()
	client := rafttransport.Client{URL: r.server.URL, HTTP: r.server.Client()}
	_, err = client.Exchange(t.Context(), request)
	mustNil(t, err)
	before, err := a.clusterClient().Status(t.Context())
	mustNil(t, err)
	mustNil(t, a.Dir.Revoke(t.Context(), b.Device.ID()))
	after, err := a.clusterClient().Status(t.Context())
	mustNil(t, err)
	if after.Membership.Revision != before.Membership.Revision {
		t.Fatal("revocation fixture changed the membership revision")
	}
	if _, err = client.Exchange(t.Context(), request); !errors.Is(err, rafttransport.ErrAuth) {
		t.Fatalf("saved peer request after committed revocation = %v, want refused authority", err)
	}
}
func nodeOf(s *Sync) uint64 {
	s.memberMu.Lock()
	defer s.memberMu.Unlock()
	return s.member.record.Grant.Node
}
func recordOf(t *testing.T, s *Sync) memberRecord {
	t.Helper()
	b, e := os.ReadFile(filepath.Join(s.memberPath(), "identity.json"))
	mustNil(t, e)
	var record memberRecord
	mustNil(t, json.Unmarshal(b, &record))
	return record
}

// H1/R9 proves both real homes own persistent learners while all three anchors
// alone vote, and ready requires signed evidence after actual local application.
func TestR9PersonalHomesJoinAsDurableLearners(t *testing.T) {
	r := newMemberRig(t)
	a := r.home(nil)
	id := a.Identity
	b := r.home(&id)
	before := r.requests.Load()
	opened, ok, e := Open(a.Home)
	mustNil(t, e)
	if !ok || opened == nil || r.requests.Load() != before {
		t.Fatal("R23 Open used the network")
	}
	readyMember(t, a)
	readyMember(t, b)
	status, e := a.ClusterStatus(t.Context())
	mustNil(t, e)
	if len(status.Configuration.Voters) != 3 || !slices.Equal(status.Configuration.Voters, []uint64{1, 2, 3}) || len(status.Configuration.Learners) != 2 {
		t.Fatalf("R9 topology: %+v", status)
	}
	for _, s := range []*Sync{a, b} {
		node := nodeOf(s)
		if !slices.Contains(status.Configuration.Learners, node) || !slices.Contains(status.Ready, node) {
			t.Fatalf("R9 learner not ready: %+v", status)
		}
		if _, e := os.Stat(filepath.Join(s.memberPath(), "member.db")); e != nil {
			t.Fatal(e)
		}
	}
	// R6 now offers leadership to this caught-up home; the earlier slice's
	// unsupported boundary must not contradict the production contract.
	receipt, e := a.LeadCluster(t.Context(), 0)
	mustNil(t, e)
	if receipt.Node != nodeOf(a) || receipt.Barrier == 0 {
		t.Fatal("R6 joined home did not receive its elected read receipt", receipt)
	}
}

// H2/R9 proves reopening retains authority, a lost reply resumes the saved
// enrollment, and wiping only the authoritative store retires the old node.
func TestR9PersonalStoreReopenWipeAndInterruptedEnrollment(t *testing.T) {
	r := newMemberRig(t)
	s := r.home(nil)
	r.interrupt.Store(true)
	s.memberRound(t.Context())
	pending := recordOf(t, s)
	if pending.Store == "" || s.member != nil {
		t.Fatal("R9 interruption lost pending identity")
	}
	reopened := buildAt(s.Home, s.Relay, s.Identity, s.Device, s.Interval, r.clock.Now)
	r.homes = append(r.homes, reopened)
	readyMember(t, reopened)
	first := recordOf(t, reopened)
	if first.Store != pending.Store {
		t.Fatal("R9 enrollment retry changed store identity")
	}
	oldNode := nodeOf(reopened)
	reopened.stopMember()
	again := buildAt(s.Home, s.Relay, s.Identity, s.Device, s.Interval, r.clock.Now)
	r.homes = append(r.homes, again)
	readyMember(t, again)
	if nodeOf(again) != oldNode || recordOf(t, again).Store != first.Store {
		t.Fatal("R9 reopened with new authority")
	}
	again.stopMember()
	mustNil(t, os.Remove(filepath.Join(s.memberPath(), "member.db")))
	// A wiped database includes its WAL sidecars; a normal close checkpointed them.
	fresh := buildAt(s.Home, s.Relay, s.Identity, s.Device, s.Interval, r.clock.Now)
	r.homes = append(r.homes, fresh)
	readyMember(t, fresh)
	if nodeOf(fresh) == oldNode || recordOf(t, fresh).Store == first.Store {
		t.Fatal("R9 wiped store reused authority")
	}
	status, e := fresh.ClusterStatus(t.Context())
	mustNil(t, e)
	if slices.Contains(status.Configuration.Learners, oldNode) {
		t.Fatal("R9 old node remains configured")
	}
	m := fresh.member
	m.mu.Lock()
	app, _, e := m.store.ApplicationView(t.Context())
	mustNil(t, e)
	key := raftstore.Key{Namespace: raftstore.Enrollment, Key: cs.MemberKey(oldNode).Key}
	rows, _, e := m.store.Read(t.Context(), app.Applied, []raftstore.Key{key})
	mustNil(t, e)
	var retired cs.MemberRecord
	decodeErr := cs.Decode(rows[key], &retired)
	m.mu.Unlock()
	mustNil(t, decodeErr)
	if !retired.Retired {
		t.Fatal("R9 old node resurrected")
	}
}

// H3/R23 checks each retained HTTP/frame/callback path after sync-off or an
// unpair, including startup and discovery. No quiet attempt reaches the relay.
func TestR23PersonalLearnerStopsAtEveryUse(t *testing.T) {
	for _, unpair := range []bool{false, true} {
		t.Run(map[bool]string{false: "off", true: "unpaired"}[unpair], func(t *testing.T) {
			r := newMemberRig(t)
			s := r.home(nil)
			readyMember(t, s)
			m := s.member
			if unpair {
				mustNil(t, os.Remove(filepath.Join(s.Home, identity.File)))
			} else {
				t.Setenv(URLVar, "off")
			}
			requests, frames, attempts := r.requests.Load(), r.in.count.Load(), s.relayAttempts.Load()+s.clusterFrames.Load()
			m.mu.Lock()
			_, e := m.request()
			if !errors.Is(e, ErrQuiet) {
				t.Fatal(e)
			}
			e = m.exchange(t.Context())
			if !errors.Is(e, ErrQuiet) {
				t.Fatal(e)
			}
			e = m.receive(t.Context(), rafttransport.Response{Generation: m.record.Grant.Generation})
			if !errors.Is(e, ErrQuiet) {
				t.Fatal(e)
			}
			e = m.start(t.Context())
			if !errors.Is(e, ErrQuiet) {
				t.Fatal(e)
			}
			m.mu.Unlock()
			_, e = s.enrollMember(t.Context(), m.record.Store)
			if !errors.Is(e, ErrQuiet) {
				t.Fatal(e)
			}
			_, e = s.ClusterStatus(t.Context())
			if !errors.Is(e, ErrQuiet) {
				t.Fatal(e)
			}
			s.memberRound(t.Context())
			mustNil(t, s.SyncState(t.Context()))
			if r.requests.Load() != requests || r.in.count.Load() != frames || s.relayAttempts.Load()+s.clusterFrames.Load() != attempts {
				t.Fatal("R23 quiet learner sent traffic")
			}
		})
	}
}

// H4/H5/R26/R27/C30 drive ten virtual idle minutes on the REAL native relay
// and real open home state rounds, then require remote and local state changes
// to pass the normal quorum-backed read/write doors and arrive within a round.
func TestR26R27C30OpenLearnerIdleAndStateWake(t *testing.T) {
	r := newMemberRig(t)
	a := r.home(nil)
	id := a.Identity
	b := r.home(&id)
	// M1 requires usable provider credentials before a model preference can arrive.
	for _, s := range []*Sync{a, b} {
		mustNil(t, config.WriteAPIKey(s.Home, fakeKeyOne))
	}
	readyMember(t, a)
	readyMember(t, b)
	mustNil(t, a.SyncState(t.Context()))
	mustNil(t, b.SyncState(t.Context()))
	// Settle useful startup sharing before beginning the idle accounting window.
	settle := func(s *Sync) {
		waitMember(t, s, func(m *personalMember) bool { return m.idle })
		mustNil(t, s.SyncState(t.Context()))
	}
	for round := 0; round < 3; round++ {
		settle(a)
		settle(b)
	}
	// Exercise the same owner used by the real terminal during the entire
	// measured window. Host ticks advance explicitly; no wall sleeps are used.
	tickHome := func(s *Sync) func() {
		ctx, cancel := context.WithCancel(t.Context())
		ticks := make(chan time.Time)
		done := make(chan struct{})
		go func() { s.runState(ctx, ticks); close(done) }()
		t.Cleanup(func() {
			cancel()
			select {
			case <-done:
			case <-time.After(requestTimeout):
				t.Error("R23 state owner did not stop")
			}
		})
		tick := func() {
			select {
			case ticks <- r.clock.Now():
			case <-time.After(requestTimeout):
				t.Fatal("R27 state round stalled")
			}
		}
		tick()
		tick()
		return func() { tick(); tick() }
	}
	tickA, tickB := tickHome(a), tickHome(b)
	// The surface and standing observer hold separate Sync handles. Their
	// follower, roster and backstop must all use the terminal-owned learner.
	screenA := buildAt(a.Home, a.Relay, a.Identity, a.Device, a.Interval, r.clock.Now)
	screenB := buildAt(b.Home, b.Relay, b.Identity, b.Device, b.Interval, r.clock.Now)
	followA, followB := screenA.Follow(), screenB.Follow()
	defer followA.Close()
	defer followB.Close()
	streams := []dirwatch.Stream{}
	for _, screen := range []*Sync{screenA, screenB} {
		watcher := screen.Dir.(directory.Watcher)
		stream, err := screen.followStream(t.Context(), watcher)
		mustNil(t, err)
		defer stream.Close()
		streams = append(streams, stream)
	}
	c31AwaitFeed(t, followA, requestTimeout, func(st dirwatch.State) bool { return st.Up })
	c31AwaitFeed(t, followB, requestTimeout, func(st dirwatch.State) bool { return st.Up })
	countAttempts := func() uint64 {
		return a.relayAttempts.Load() + a.clusterFrames.Load() + b.relayAttempts.Load() + b.clusterFrames.Load() + screenA.relayAttempts.Load() + screenA.clusterFrames.Load() + screenB.relayAttempts.Load() + screenB.clusterFrames.Load()
	}
	// Settle socket admission before measuring idle, and service the real
	// presence sockets at their advertised cadence as the relay clock advances.
	for tick := 0; tick < 20; tick++ {
		r.clock.Advance(DefaultInterval)
		if time.Duration(tick+1)*DefaultInterval%dirwatch.KeepEvery == 0 {
			for _, stream := range streams {
				pingHomeStream(t, stream)
			}
			followA.Probe()
			followB.Probe()
		}
		settle(a)
		settle(b)
	}
	watchBefore := r.watchIn.count.Load() + r.watchOut.count.Load()
	requests, frames, attempts := r.requests.Load(), r.in.count.Load()+r.out.count.Load(), countAttempts()
	for tick := 0; tick < 120; tick++ {
		r.clock.Advance(DefaultInterval)
		tickA()
		tickB()
		for i, screen := range []*Sync{screenA, screenB} {
			if time.Duration(tick+1)*DefaultInterval%dirwatch.KeepEvery == 0 {
				pingHomeStream(t, streams[i])
			}
			_, _, err := screen.RowsAt(t.Context())
			mustNil(t, err)
			_, err = screen.Roster(t.Context())
			mustNil(t, err)
			_, err = screen.FleetSize(t.Context())
			mustNil(t, err)
		}
		if time.Duration(tick+1)*DefaultInterval%dirwatch.KeepEvery == 0 {
			followA.Probe()
			followB.Probe()
		}
	}
	gotHTTP, gotFrames := r.requests.Load()-requests, r.in.count.Load()+r.out.count.Load()-frames
	gotAttempts := countAttempts() - attempts
	t.Logf("R27 native ten-minute idle: relay HTTP=%d socket frames=%d client attempts=%d", gotHTTP, gotFrames, gotAttempts)
	if gotHTTP != 0 || gotFrames != 0 || gotAttempts != 0 {
		t.Fatal("R27 idle home generated traffic")
	}
	// Advisory presence permits at most 488 watch frames in ten minutes: four
	// sockets times two frames times (ten minutes / KeepEvery), plus two initial
	// snapshot frames per socket. C31 take-back after a computer closes its lid
	// depends on keeping this advisory socket alive.
	const watchSockets = 4
	const watchBudget = watchSockets * (2*(10*time.Minute/dirwatch.KeepEvery) + 2)
	watchFrames := r.watchIn.count.Load() + r.watchOut.count.Load() - watchBefore
	t.Logf("Home watch frames=%d, ten-minute bound=%d", watchFrames, watchBudget)
	if watchFrames > uint64(watchBudget) {
		t.Fatal("Home presence exceeded the ten-minute watch frame budget")
	}
	// The first public read after idle and the next state write retain ordinary
	// ReadIndex/proposal authority. Local learner state is only a change signal.
	_, e := b.Dir.List(t.Context())
	mustNil(t, e)
	mustNil(t, config.WriteChatModel(b.Home, "stub/after-idle"))
	brain, e := store.Open(filepath.Join(b.Home, "graph.db"))
	mustNil(t, e)
	memory, e := brain.AddMemory(store.Memory{Owner: store.OwnerUser, Text: "The idle wake carries memory too.", Origin: store.OriginPerson})
	mustNil(t, e)
	mustNil(t, brain.Close())
	mustNil(t, b.SyncState(t.Context()))
	want, _ := b.Dir.List(t.Context())
	waitMember(t, a, func(m *personalMember) bool { return appliedVault(m) == want.Identity.Vault && m.idle })
	rid, local, e := a.memberVault(t.Context())
	mustNil(t, e)
	if !local || rid != want.Identity.Vault {
		t.Fatalf("R26 remote committed vault did not arrive: %q != %q", rid, want.Identity.Vault)
	}
	mustNil(t, a.SyncState(t.Context()))
	raw, e := os.ReadFile(filepath.Join(a.Home, "config.json"))
	mustNil(t, e)
	if !strings.Contains(string(raw), "stub/after-idle") {
		t.Fatal("C30 preference did not arrive within one state round")
	}
	received, e := store.Open(filepath.Join(a.Home, "graph.db"))
	mustNil(t, e)
	remembered, found, e := received.MemoryRecord(memory.ID)
	mustNil(t, e)
	mustNil(t, received.Close())
	if !found || remembered.Text != memory.Text {
		t.Fatal("C30 shared memory did not arrive within one state round")
	}
	mustNil(t, config.WriteChatModel(a.Home, "stub/local-wake"))
	mustNil(t, a.SyncState(t.Context()))
	l, e := a.Dir.List(t.Context())
	mustNil(t, e)
	if l.Identity.Vault == want.Identity.Vault {
		t.Fatal("C30 local change was not pushed promptly")
	}
}

// H6/R23/C30 pins the hosted capability boundary and its unchanged polling
// behavior. A relay with no control/enrollment door never opens a member store.
func TestR23C30HostedStateKeepsExistingPolling(t *testing.T) {
	server := relay(t)
	s, ok, e := Open(machine(t, server.URL))
	mustNil(t, e)
	if !ok {
		t.Fatal("open")
	}
	s.memberRound(t.Context())
	s.memberRound(t.Context())
	if s.member != nil || !s.memberUnsupported {
		t.Fatal("R23 hosted started a learner")
	}
	if _, e := os.Stat(s.memberPath()); !errors.Is(e, os.ErrNotExist) {
		t.Fatal("R23 hosted created learner storage", e)
	}
	_, e = s.ClusterStatus(t.Context())
	if !errors.Is(e, clusterctl.ErrUnsupported) {
		t.Fatal(e)
	}
	_, e = s.LeadCluster(t.Context(), 0)
	if !errors.Is(e, clusterctl.ErrUnsupported) {
		t.Fatal(e)
	}
	mustNil(t, config.WriteChatModel(s.Home, "stub/hosted-state"))
	mustNil(t, s.SyncState(t.Context()))
	// Count ordinary directory requests independently at the actual HTTP server.
	var requests atomic.Uint64
	counted := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); server.Config.Handler.ServeHTTP(w, r) }))
	defer counted.Close()
	active := build(s.Home, counted.URL, s.Identity, s.Device, s.Interval)
	active.stateSent = s.stateSent
	for tick := 0; tick < 120; tick++ {
		mustNil(t, active.SyncState(t.Context()))
	}
	t.Logf("R27 hosted ten-minute idle: relay HTTP=%d (polling retained)", requests.Load())
	if requests.Load() != 2*uint64(10*time.Minute/DefaultInterval) {
		t.Fatalf("C30 hosted polling changed: %d", requests.Load())
	}
}

// H1/H3/C30 prove the existing terminal lifetime notices pairing while Home
// stands open, starts the real learner, and joins all its work on cancellation.
func TestR9R23C30PairingStartsLearnerInsideOpenHomeLifetime(t *testing.T) {
	r := newMemberRig(t)
	home := machine(t, r.server.URL)
	// A solo first launch has local identity state but no network authority.
	mustNil(t, os.Remove(filepath.Join(home, identity.File)))
	_, e := identity.EnsureSolo(home)
	mustNil(t, e)
	s, ok, e := Open(home)
	mustNil(t, e)
	if !ok {
		t.Fatal("open solo")
	}
	ctx, cancel := context.WithCancel(t.Context())
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() { s.runState(ctx, ticks); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	step := func() {
		select {
		case ticks <- r.clock.Now():
		case <-time.After(requestTimeout):
			t.Fatal("C30 state lifetime did not complete round")
		}
	}
	step()
	if r.requests.Load() != 0 {
		t.Fatal("R23 solo Home attempted discovery")
	}
	fleet, e := identity.Ensure(t.TempDir())
	mustNil(t, e)
	_, e = identity.Adopt(home, fleet, true)
	mustNil(t, e)
	step()
	step()
	active, ok, e := Open(home)
	mustNil(t, e)
	if !ok {
		t.Fatal("open paired")
	}
	status, e := active.clusterClient().Status(t.Context())
	mustNil(t, e)
	if status.Self == 0 || !slices.Contains(status.Configuration.Learners, status.Self) {
		t.Fatal("R9 live pairing did not start learner")
	}
	current, e := s.currentDisplaySync()
	mustNil(t, e)
	if current.Identity.ID() != fleet.ID() {
		t.Fatal("C30 Home retained its old metadata key after pairing")
	}
	before := r.requests.Load()
	_, _, e = s.RowsAt(t.Context())
	mustNil(t, e)
	if r.requests.Load() != before {
		t.Fatal("R27 paired-open Home did not use its newly owned learner")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(requestTimeout):
		t.Fatal("R23 lifetime did not join learner shutdown")
	}
	// Shutdown must release the sole member lock and SQLite connection.
	reopened, e := active.openMember(t.Context())
	mustNil(t, e)
	reopened.close()
}

// H2/R9/R23 require same-store generation recovery after relay inbox loss,
// rather than trusting stale delivery cursors or enrolling a replacement node.
func TestR9R23PersonalLearnerRefreshesColdRouterGeneration(t *testing.T) {
	r := newMemberRig(t)
	s := r.home(nil)
	readyMember(t, s)
	old := recordOf(t, s)
	s.stopMember()
	mustNil(t, r.svc.Close())
	r.svc = relayserve.New(relayserve.Config{Store: r.storage, Now: r.clock.Now, After: r.clock.After})
	readyMember(t, s)
	current := recordOf(t, s)
	if current.Store != old.Store || current.Grant.Node != old.Grant.Node || current.Grant.Generation <= old.Grant.Generation {
		t.Fatal("R9 cold router replaced store or reused generation")
	}
	mustNil(t, config.WriteChatModel(s.Home, "stub/cold"))
	mustNil(t, s.SyncState(t.Context()))
	want, e := s.Dir.List(t.Context())
	mustNil(t, e)
	waitMember(t, s, func(m *personalMember) bool { return m.idle && appliedVault(m) == want.Identity.Vault })
}

// H3/R23 prohibits discovery, enrollment, SQLite creation or delayed callbacks
// before pairing or when sync was already off at the start of the lifetime.
func TestR23PersonalLearnerDoesNotStartWhileQuiet(t *testing.T) {
	for _, solo := range []bool{true, false} {
		t.Run(map[bool]string{true: "solo", false: "off"}[solo], func(t *testing.T) {
			r := newMemberRig(t)
			s := r.home(nil)
			if solo {
				mustNil(t, os.Remove(filepath.Join(s.Home, identity.File)))
				_, e := identity.EnsureSolo(s.Home)
				mustNil(t, e)
			} else {
				t.Setenv(URLVar, "off")
			}
			s.memberRound(t.Context())
			_, e := s.openMember(t.Context())
			if !errors.Is(e, ErrQuiet) {
				t.Fatal(e)
			}
			mustNil(t, s.SyncState(t.Context()))
			if s.member != nil || r.requests.Load() != 0 || s.relayAttempts.Load() != 0 || s.clusterFrames.Load() != 0 {
				t.Fatal("R23 quiet startup reached the relay")
			}
			if _, e := os.Stat(s.memberPath()); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("R23 quiet startup created store", e)
			}
		})
	}
}

// appliedVault reads only the learner's own durable application under its actor.
func appliedVault(m *personalMember) string {
	ctx := context.Background()
	app, _, err := m.store.ApplicationView(ctx)
	if err != nil {
		return ""
	}
	key := raftstore.Key{Namespace: raftstore.State, Key: cs.IdentityKey().Key}
	rows, _, err := m.store.Read(ctx, app.Applied, []raftstore.Key{key})
	if err != nil {
		return ""
	}
	var id cs.IdentityRec
	if cs.Decode(rows[key], &id) != nil {
		return ""
	}
	return id.Vault
}

// H1/R9 proves the production learner installs a multi-chunk snapshot and then
// applies later log entries, rather than reporting router delivery as readiness.
func TestR9PersonalHomeInstallsSnapshotChunksAndLog(t *testing.T) {
	r := newMemberRig(t)
	s := r.home(nil)
	id := s.Identity
	var last *Sync
	for n := 0; n < 14; n++ {
		writer := r.home(&id)
		last = writer
		mustNil(t, writer.Dir.PutDevice(t.Context(), writer.Device.ID(), directory.Device{V: 1, Name: strings.Repeat("e", 20000)}))
	}
	readyMember(t, s)
	m := s.member
	m.mu.Lock()
	recovered, e := m.store.Recover(t.Context())
	m.mu.Unlock()
	mustNil(t, e)
	manifest, e := raftsnapshot.Decode(recovered.Core.Snapshot.GetData())
	mustNil(t, e)
	if manifest.Chunks < 2 {
		t.Fatal("R9 fixture did not install multiple snapshot chunks")
	}
	mustNil(t, last.Dir.PutDevice(t.Context(), last.Device.ID(), directory.Device{V: 1, Name: "after installed snapshot"}))
	key := cs.DeviceKey(last.Device.ID())
	waitMember(t, s, func(m *personalMember) bool {
		app, _, err := m.store.ApplicationView(t.Context())
		if err != nil {
			return false
		}
		local := raftstore.Key{Namespace: raftstore.Namespace(key.Namespace), Key: key.Key}
		rows, _, err := m.store.Read(t.Context(), app.Applied, []raftstore.Key{local})
		if err != nil {
			return false
		}
		var device cs.Device
		return cs.Decode(rows[local], &device) == nil && device.Name == "after installed snapshot" && m.idle
	})
	status, e := s.ClusterStatus(t.Context())
	mustNil(t, e)
	if !slices.Contains(status.Ready, nodeOf(s)) {
		t.Fatal("R9 durable snapshot/log learner not ready")
	}
}

// H3/R23 exercises a real established socket callback after the person's off
// decision. A different authorized machine writes through the public door;
// this home's delayed receive must discard it without sending another frame.
func TestR23OpenLearnerDiscardsDelayedPushAfterOff(t *testing.T) {
	r := newMemberRig(t)
	s := r.home(nil)
	readyMember(t, s)
	m := s.member
	remote := directory.NewHTTP(r.server.URL, reqsign.SignFor(deviceSigner{s.Identity, s.Device}, r.clock.Now), r.server.Client())
	t.Setenv(URLVar, "off")
	attempts, frames := s.relayAttempts.Load(), s.clusterFrames.Load()
	m.mu.Lock()
	changed := m.changed
	m.mu.Unlock()
	mustNil(t, remote.PutDevice(t.Context(), s.Device.ID(), directory.Device{V: 1, Name: "delayed while off"}))
	select {
	case <-changed:
	case <-time.After(requestTimeout):
		t.Fatal("R23 real delayed socket callback never arrived")
	}
	m.mu.Lock()
	failure := m.failure
	m.mu.Unlock()
	if !errors.Is(failure, ErrQuiet) {
		t.Fatal("R23 delayed callback did not stop", failure)
	}
	if attempts != s.relayAttempts.Load() || frames != s.clusterFrames.Load() {
		t.Fatal("R23 delayed callback sent after off")
	}
	s.memberRound(t.Context())
}

// H2/R9/R27 requires interrupted catch-up to retain its store and retry next
// round without busy polling or treating a transport response as readiness.
func TestR9R27PersonalCatchupResumesWithoutEmptyPolling(t *testing.T) {
	r := newMemberRig(t)
	s := r.home(nil)
	r.parkExchanges.Store(true)
	err := s.memberRound(t.Context())
	if !errors.Is(err, errMemberWaiting) || s.member != nil {
		t.Fatal("R9 empty catch-up did not remain pending", err)
	}
	pending := recordOf(t, s)
	attempts := s.relayAttempts.Load()
	for round := 0; round < 3; round++ {
		s.memberRound(t.Context())
	}
	if s.relayAttempts.Load() != attempts {
		t.Fatal("R27 retry backoff made empty polls")
	}
	r.parkExchanges.Store(false)
	r.clock.Advance(DefaultInterval)
	readyMember(t, s)
	resumed := recordOf(t, s)
	if resumed.Store != pending.Store || resumed.Grant.Node != pending.Grant.Node {
		t.Fatal("R9 partial catch-up replaced authority")
	}
}

// H4/H5/R9/R23 require the actual Home follower and its separate Sync handle
// to receive local version/join/refusal signals without periodic network use.
func TestR9R23LocalHomeFollowerSharesTheOwnedLearner(t *testing.T) {
	r := newMemberRig(t)
	a := r.home(nil)
	mustNil(t, a.Rename(t.Context(), "home-a"))
	readyMember(t, a)
	screen := buildAt(a.Home, a.Relay, a.Identity, a.Device, a.Interval, r.clock.Now)
	stream, e := screen.followStream(t.Context(), screen.Dir.(directory.Watcher))
	mustNil(t, e)
	defer stream.Close()
	drain := func() {
		mustNil(t, stream.Ping(t.Context()))
		for {
			f, e := stream.Next(t.Context())
			mustNil(t, e)
			if f.Pong {
				return
			}
		}
	}
	drain()
	baseline := r.requests.Load()
	_, _, e = screen.RowsAt(t.Context())
	mustNil(t, e)
	roster, e := screen.Roster(t.Context())
	mustNil(t, e)
	if len(roster) != 1 || r.requests.Load() != baseline {
		t.Fatal("R27 Home display backstop used the network")
	}
	id := a.Identity
	b := r.home(&id)
	mustNil(t, b.Rename(t.Context(), "home-b"))
	var joined bool
	ctx, cancel := context.WithTimeout(t.Context(), requestTimeout)
	defer cancel()
	for !joined {
		frame, e := stream.Next(ctx)
		mustNil(t, e)
		joined = frame.Event != nil && frame.Event.T == dirwatch.EventJoined && frame.Event.Device == b.Device.ID()
	}
	roster, e = screen.Roster(t.Context())
	mustNil(t, e)
	if len(roster) != 2 {
		t.Fatal("R9 committed joined roster did not reach Home")
	}
	// Display hints grant no ownership or stale-read shortcut to authority.
	before := r.requests.Load()
	_, e = screen.Dir.List(t.Context())
	mustNil(t, e)
	if r.requests.Load() <= before {
		t.Fatal("R26 public authority read used local display state")
	}
	t.Setenv(URLVar, "off")
	before = r.requests.Load()
	if e = stream.Ping(t.Context()); !errors.Is(e, ErrQuiet) {
		t.Fatal(e)
	}
	if _, e = stream.Next(t.Context()); !errors.Is(e, ErrQuiet) {
		t.Fatal(e)
	}
	if r.requests.Load() != before {
		t.Fatal("R23 local follower sent after off")
	}
}

func pingHomeStream(t *testing.T, stream dirwatch.Stream) {
	t.Helper()
	mustNil(t, stream.Ping(t.Context()))
	for {
		frame, err := stream.Next(t.Context())
		mustNil(t, err)
		if frame.Pong {
			return
		}
	}
}
