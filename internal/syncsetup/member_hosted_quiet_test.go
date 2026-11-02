package syncsetup

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/store"
)

// presenceCountDirectory counts the actual Home watch writes and reads, including
// platform auto-responses that never enter the Worker's application callbacks.
type presenceCountDirectory struct {
	directory.Client
	frames *atomic.Uint64
}

func (d presenceCountDirectory) Watch(ctx context.Context) (dirwatch.Stream, error) {
	stream, err := d.Client.(directory.Watcher).Watch(ctx)
	if err != nil {
		return nil, err
	}
	return presenceCountStream{Stream: stream, frames: d.frames}, nil
}

type presenceCountStream struct {
	dirwatch.Stream
	frames *atomic.Uint64
}

func (s presenceCountStream) Ping(ctx context.Context) error {
	err := s.Stream.Ping(ctx)
	if err == nil {
		s.frames.Add(1)
	}
	return err
}
func (s presenceCountStream) Next(ctx context.Context) (dirwatch.Frame, error) {
	frame, err := s.Stream.Next(ctx)
	if err == nil {
		s.frames.Add(1)
	}
	return frame, err
}

func hostedQuietControl(t *testing.T, endpoint, identity, op string) map[string]json.RawMessage {
	t.Helper()
	raw, _ := json.Marshal(map[string]string{"identity": identity, "op": op})
	req, e := http.NewRequestWithContext(t.Context(), http.MethodPost, endpoint+"/fixture", bytes.NewReader(raw))
	mustNil(t, e)
	resp, e := http.DefaultClient.Do(req)
	mustNil(t, e)
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("R27 fixture %s: %s", op, resp.Status)
	}
	var result map[string]json.RawMessage
	if op == "reset-metrics" || op == "presence-turn" {
		return nil
	}
	mustNil(t, json.NewDecoder(resp.Body).Decode(&result))
	return result
}
func hostedSocketCounts(t *testing.T, result map[string]json.RawMessage) map[string]int {
	t.Helper()
	var sockets []struct{ Kind string }
	mustNil(t, json.Unmarshal(result["sockets"], &sockets))
	counts := map[string]int{}
	for _, socket := range sockets {
		kind := socket.Kind
		if kind == "" {
			kind = "watch"
		}
		counts[kind]++
	}
	return counts
}

func hostedNumber(t *testing.T, result map[string]json.RawMessage, key string) uint64 {
	t.Helper()
	var n uint64
	mustNil(t, json.Unmarshal(result[key], &n))
	return n
}
func hostedTicks(t *testing.T, s *Sync) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() { s.runState(ctx, ticks); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(requestTimeout):
			t.Error("R23 hosted state lifetime did not stop")
		}
	})
	step := func() {
		select {
		case ticks <- time.Now():
		case <-time.After(requestTimeout):
			t.Fatal("R27 hosted state round stalled")
		}
	}
	step()
	step()
	return func() { step(); step() }
}
func hostedRefreshed(t *testing.T, s *Sync, tick func(), generation uint64) {
	t.Helper()
	deadline := time.NewTimer(requestTimeout)
	defer deadline.Stop()
	retry := time.NewTicker(100 * time.Millisecond)
	defer retry.Stop()
	for {
		tick()
		s.memberMu.Lock()
		m := s.member
		ready := false
		if m != nil {
			m.mu.Lock()
			ready = m.failure == nil && m.conn != nil && m.forwardConn != nil && m.record.Grant.Generation > generation
			m.mu.Unlock()
		}
		s.memberMu.Unlock()
		if ready {
			return
		}
		select {
		case <-retry.C:
		case <-deadline.C:
			t.Fatal("R26 real input did not refresh the retained learner connection")
		}
	}
}

func hostedSettled(t *testing.T, s *Sync) {
	t.Helper()
	waitMember(t, s, func(m *personalMember) bool { return m.idle && len(m.sent) == 0 && len(m.pending) == 0 })
}

// R23/R26/R27/C30 measure both real terminal lifetimes, local Home observers
// and platform watch pings, then require quorum wake and shared state delivery.
func TestR23R26R27C30HostedOpenHomesIdleEvictionAndWake(t *testing.T) {
	endpoint := hostedLearnerEndpoint(t)
	a, ok, e := Open(machine(t, endpoint))
	mustNil(t, e)
	if !ok {
		t.Fatal("open")
	}
	homeB := machine(t, endpoint)
	_, e = identity.Adopt(homeB, a.Identity, true)
	mustNil(t, e)
	b, ok, e := Open(homeB)
	mustNil(t, e)
	if !ok {
		t.Fatal("second open")
	}
	tickA, tickB := hostedTicks(t, a), hostedTicks(t, b)
	hostedReady(t, a)
	hostedReady(t, b)
	for n := 0; n < 3; n++ {
		tickA()
		tickB()
		hostedSettled(t, a)
		hostedSettled(t, b)
	}
	var watchFrames atomic.Uint64
	screens := []*Sync{}
	follows := []dirwatch.Follower{}
	streams := []dirwatch.Stream{}
	watches := []dirwatch.Stream{}
	for _, s := range []*Sync{a, b} {
		screen, ok, e := Open(s.Home)
		mustNil(t, e)
		if !ok {
			t.Fatal("screen open")
		}
		screen.Dir = presenceCountDirectory{Client: screen.Dir, frames: &watchFrames}
		screens = append(screens, screen)
		follow := screen.Follow()
		defer follow.Close()
		// Home and the compatibility watch both retain advisory presence through
		// platform auto-response without waking coordination.
		stream, e := screen.followStream(t.Context(), screen.Dir.(directory.Watcher))
		mustNil(t, e)
		defer stream.Close()
		streams = append(streams, stream)
		watch, e := screen.Dir.(directory.Watcher).Watch(t.Context())
		mustNil(t, e)
		defer watch.Close()
		watches = append(watches, watch)
		follows = append(follows, follow)
	}
	for _, follow := range follows {
		c31AwaitFeed(t, follow, requestTimeout, func(st dirwatch.State) bool { return st.Up })
	}
	idle := hostedQuietControl(t, endpoint, a.Identity.ID(), "idle-schedule")
	if string(idle["sleeping"]) != "true" || hostedNumber(t, idle, "recurringMemberCalls") != 0 {
		t.Fatalf("R26 idle schedule: %s", idle)
	}
	for _, s := range []*Sync{a, b} {
		hostedSettled(t, s)
	}
	ping := func(stream dirwatch.Stream) {
		mustNil(t, stream.Ping(t.Context()))
		for {
			frame, e := stream.Next(t.Context())
			mustNil(t, e)
			if frame.Pong {
				return
			}
		}
	}
	count := func() uint64 {
		var n uint64
		for _, s := range append([]*Sync{a, b}, screens...) {
			n += s.relayAttempts.Load() + s.clusterFrames.Load()
		}
		return n
	}
	before := hostedQuietControl(t, endpoint, a.Identity.ID(), "quiet-state")
	var alarms map[string]json.RawMessage
	mustNil(t, json.Unmarshal(before["memberAlarms"], &alarms))
	for node, alarm := range alarms {
		if string(alarm) != "null" {
			t.Fatalf("R26 sleeping voter %s retained alarm: %s", node, alarm)
		}
	}
	if len(alarms) != 3 || string(before["maintenance"]) != "false" {
		t.Fatalf("R26 sleeping group retained maintenance: %s", before)
	}
	// Advisory presence retains exactly six watches: a Home follower, a direct
	// Home stream and a compatibility watch per computer. C31 take-back after
	// a computer closes its lid depends on these advisory Home connections.
	if !maps.Equal(hostedSocketCounts(t, before), map[string]int{"peer": 2, "forward": 2, "watch": 6}) {
		t.Fatalf("R26 missing retained sockets: %s", before)
	}
	generation := recordOf(t, a).Grant.Generation
	cold := hostedQuietControl(t, endpoint, a.Identity.ID(), "evict-idle")
	if string(cold["ready"]) != "false" {
		t.Fatal("R26 fixture did not reconstruct coordinator")
	}
	hostedQuietControl(t, endpoint, a.Identity.ID(), "reset-metrics")
	attempts := count()
	watchBefore := watchFrames.Load()
	pongs := 0
	for n := 0; n < 120; n++ {
		tickA()
		tickB()
		if time.Duration(n+1)*DefaultInterval%dirwatch.KeepEvery == 0 {
			for _, follow := range follows {
				follow.Probe()
			}
		}
		for i, screen := range screens {
			if time.Duration(n+1)*DefaultInterval%dirwatch.KeepEvery == 0 {
				ping(streams[i])
				ping(watches[i])
				pongs += 2
			}
			_, _, e := screen.RowsAt(t.Context())
			mustNil(t, e)
			_, e = screen.Roster(t.Context())
			mustNil(t, e)
			_, e = screen.FleetSize(t.Context())
			mustNil(t, e)
		}
	}
	schedule := hostedQuietControl(t, endpoint, a.Identity.ID(), "presence-schedule")
	if hostedNumber(t, schedule, "milliseconds") != 600000 {
		t.Fatal("R27 missing ten-minute presence measurement")
	}
	metrics := hostedQuietControl(t, endpoint, a.Identity.ID(), "metrics")
	httpCount := hostedNumber(t, metrics, "http")
	frames := hostedNumber(t, metrics, "peerIn") + hostedNumber(t, metrics, "peerOut")
	clientAttempts := count() - attempts
	t.Logf("R23 R27 hosted ten-minute idle: Worker HTTP=%d peer/forward frames=%d client attempts=%d platform presence ping/pongs=%d sweeps=%d", httpCount, frames, clientAttempts, pongs, hostedNumber(t, schedule, "sweeps"))
	if httpCount != 0 || frames != 0 || clientAttempts != 0 || hostedNumber(t, metrics, "writes") != 0 || hostedNumber(t, metrics, "turns") != 0 {
		t.Fatalf("R27 idle activity: %s", metrics)
	}
	// Advisory presence permits 732 watch frames in ten minutes: six sockets
	// times two frames per KeepEvery, plus two initial snapshot frames each.
	// C31 take-back after a computer closes its lid depends on these advisory
	// sockets, whose platform pongs must leave coordination asleep.
	const watchBudget = 6 * (2*(10*time.Minute/dirwatch.KeepEvery) + 2)
	measuredWatchFrames := watchFrames.Load() - watchBefore
	t.Logf("Home watch frames=%d, ten-minute bound=%d", measuredWatchFrames, watchBudget)
	if measuredWatchFrames > uint64(watchBudget) {
		t.Fatal("Home presence exceeded the ten-minute watch frame budget")
	}
	// R26/R27 retain real hibernating sockets across two complete presence
	// windows. Auto-response timestamps, rather than a mocked renewal, age them.
	hostedQuietControl(t, endpoint, a.Identity.ID(), "reset-metrics")
	started := time.Now()
	nextPing := time.NewTicker(dirwatch.KeepEvery)
	defer nextPing.Stop()
	for time.Since(started) < 2*dirwatch.DeadAfter+dirwatch.KeepEvery {
		select {
		case <-nextPing.C:
			for _, watch := range watches {
				ping(watch)
				pongs++
			}
		case <-t.Context().Done():
			t.Fatal(t.Context().Err())
		}
	}
	real := hostedQuietControl(t, endpoint, a.Identity.ID(), "metrics")
	after := hostedQuietControl(t, endpoint, a.Identity.ID(), "quiet-state")
	if hostedNumber(t, real, "sweeps") < 2 || hostedNumber(t, real, "turns") != 0 || hostedNumber(t, real, "writes") != 0 || hostedNumber(t, real, "peerIn")+hostedNumber(t, real, "peerOut") != 0 || hostedNumber(t, real, "http") != 0 {
		t.Fatalf("R26 cold real-time presence: %s", real)
	}
	if string(after["ready"]) != "false" || string(after["generation"]) != string(before["generation"]) {
		t.Fatal("R26 sweep activated coordinator or changed generation")
	}
	if !maps.Equal(hostedSocketCounts(t, before), hostedSocketCounts(t, after)) {
		t.Fatal("R26 cold presence lost a retained socket")
	}
	t.Logf("R26 R27 real-time presence: duration=%s sweeps=%d member calls=0 writes=0 peer frames=0", time.Since(started), hostedNumber(t, real, "sweeps"))
	for _, watch := range watches {
		watch.Close()
	}
	// A first actual read must obtain authority from the voters. The following
	// preference and memory write must reach the other learner after eviction.
	_, e = b.Dir.List(t.Context())
	mustNil(t, e)
	hostedRefreshed(t, a, tickA, generation)
	hostedRefreshed(t, b, tickB, generation)
	mustNil(t, config.WriteChatModel(b.Home, "stub/hosted-wake"))
	brain, e := store.Open(filepath.Join(b.Home, "graph.db"))
	mustNil(t, e)
	memory, e := brain.AddMemory(store.Memory{Owner: store.OwnerUser, Text: "A hosted idle wake carries memory.", Origin: store.OriginPerson})
	mustNil(t, e)
	mustNil(t, brain.Close())
	mustNil(t, b.SyncState(t.Context()))
	want, e := b.Dir.List(t.Context())
	mustNil(t, e)
	// Generation fencing may refresh the same durable stores on real input.
	tickA()
	tickB()
	waitMember(t, a, func(m *personalMember) bool { return appliedVault(m) == want.Identity.Vault && m.idle })
	mustNil(t, a.SyncState(t.Context()))
	raw, e := os.ReadFile(filepath.Join(a.Home, "config.json"))
	mustNil(t, e)
	if !strings.Contains(string(raw), "stub/hosted-wake") {
		t.Fatal("C30 preference missed the first state round")
	}
	received, e := store.Open(filepath.Join(a.Home, "graph.db"))
	mustNil(t, e)
	got, found, e := received.MemoryRecord(memory.ID)
	mustNil(t, e)
	mustNil(t, received.Close())
	if !found || got.Text != memory.Text {
		t.Fatal("C30 memory missed the first state round")
	}
	mustNil(t, config.WriteChatModel(a.Home, "stub/hosted-local"))
	mustNil(t, a.SyncState(t.Context()))
	latest, e := a.Dir.List(t.Context())
	mustNil(t, e)
	if latest.Identity.Vault == want.Identity.Vault {
		t.Fatal("C30 local change was not pushed promptly")
	}
}

// R23 every use after off or unpair must stop the joined member without an
// enrollment retry, HTTP request or protocol frame reaching the Worker.
func TestR23HostedJoinedLearnerQuietGuards(t *testing.T) {
	endpoint := hostedLearnerEndpoint(t)
	for _, mode := range []string{"off", "unpair"} {
		t.Run(mode, func(t *testing.T) {
			s, ok, e := Open(machine(t, endpoint))
			mustNil(t, e)
			if !ok {
				t.Fatal("open")
			}
			hostedOwner(t, s)
			hostedReady(t, s)
			hostedSettled(t, s)
			hostedQuietControl(t, endpoint, s.Identity.ID(), "idle-schedule")
			hostedSettled(t, s)
			hostedQuietControl(t, endpoint, s.Identity.ID(), "reset-metrics")
			before := s.relayAttempts.Load() + s.clusterFrames.Load()
			if mode == "off" {
				t.Setenv(URLVar, "off")
			} else {
				mustNil(t, os.Remove(filepath.Join(s.Home, identity.File)))
				_, e = identity.EnsureSolo(s.Home)
				mustNil(t, e)
			}
			mustNil(t, s.memberRound(t.Context()))
			for n := 0; n < 3; n++ {
				mustNil(t, s.SyncState(t.Context()))
				_, _, e = s.RowsAt(t.Context())
				mustNil(t, e)
			}
			if s.member != nil || s.relayAttempts.Load()+s.clusterFrames.Load() != before {
				t.Fatal("R23 joined quiet home kept sending")
			}
			metrics := hostedQuietControl(t, endpoint, s.Identity.ID(), "metrics")
			if hostedNumber(t, metrics, "http") != 0 {
				t.Fatalf("R23 quiet Worker requests: %s", metrics)
			}
			t.Logf("R23 %s: further client requests and frames=0", mode)
		})
	}
}
