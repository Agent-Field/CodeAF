package syncsetup

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/store"
)

// R7/R23/R26/R27 keep whole homes quiet with a personal leader and after its
// use-time sync-off departure, including cold presence and first-input wake.
func TestR23R26R27HostedLeadingHomesIdleEvictionAndWake(t *testing.T) {
	hostedLeadingHomesQuiet(t, false)
}
func TestR7R23R26R27HostedSyncOffFallbackIdleEvictionAndWake(t *testing.T) {
	hostedLeadingHomesQuiet(t, true)
}

func hostedLeadingHomesQuiet(t *testing.T, fallback bool) {
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
	receipt := hostedPersonalLeader(t, a)
	live := []*Sync{a, b}
	round := func() { tickA(); tickB() }
	if fallback {
		hostedQuietControl(t, endpoint, a.Identity.ID(), "idle-schedule")
		hostedSettled(t, a)
		hostedSettled(t, b)
		waitMember(t, a, func(m *personalMember) bool { return m.sleeping })
		mustNil(t, SaveRelayURL(b.Home, endpoint))
		t.Setenv(URLVar, "")
		attempts := a.relayAttempts.Load() + a.clusterFrames.Load()
		mustNil(t, SaveRelayURL(a.Home, "off"))
		mustNil(t, a.memberRound(t.Context()))
		if a.member != nil || a.relayAttempts.Load()+a.clusterFrames.Load() != attempts {
			t.Fatal("R7 R23 sync off sent work")
		}
		// The surviving learner remains connected, so only the leader's pair leaves.
		deadline := time.Now().Add(requestTimeout)
		for {
			counts := hostedSocketCounts(t, hostedQuietControl(t, endpoint, a.Identity.ID(), "quiet-state"))
			if counts["peer"] == 1 && counts["forward"] == 1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("R7 leading home did not sleep")
			}
			time.Sleep(time.Millisecond)
		}
		state := hostedFallbackTicks(t, b, 13, "")
		if state.Leader == 0 || state.Leader == receipt.Node || state.Membership.Leadership != nil || !slices.Equal(state.Configuration.Voters, []uint64{1, 2, 3}) {
			t.Fatal("R7 idle departure did not restore anchors", state)
		}
		live = []*Sync{b}
		round = tickB
	}
	for n := 0; n < 3; n++ {
		round()
		for _, s := range live {
			hostedSettled(t, s)
		}
	}
	screens := []*Sync{}
	follows := []dirwatch.Follower{}
	streams := []dirwatch.Stream{}
	watches := []dirwatch.Stream{}
	for _, s := range live {
		screen, ok, e := Open(s.Home)
		mustNil(t, e)
		if !ok {
			t.Fatal("screen open")
		}
		screens = append(screens, screen)
		follow := screen.Follow()
		defer follow.Close()
		// The local observer and the compatibility watch both remain open. Only
		// the latter uses platform auto-response for advisory presence.
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
	idle := hostedQuietControl(t, endpoint, a.Identity.ID(), "idle-schedule")
	if string(idle["sleeping"]) != "true" || hostedNumber(t, idle, "recurringMemberCalls") != 0 {
		t.Fatalf("R26 idle schedule: %s", idle)
	}
	for _, s := range live {
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
	// Presence is advisory; C31's take-back after a computer closes its lid
	// depends on it. Each Home retains its follower and direct watch alongside
	// the compatibility watch, so the inventory is exactly three per computer.
	if !maps.Equal(hostedSocketCounts(t, before), map[string]int{"peer": len(live), "forward": len(live), "watch": 3 * len(live)}) {
		t.Fatalf("R26 missing retained sockets: %s", before)
	}
	generation := recordOf(t, a).Grant.Generation
	cold := hostedQuietControl(t, endpoint, a.Identity.ID(), "evict-idle")
	if string(cold["ready"]) != "false" {
		t.Fatal("R26 fixture did not reconstruct coordinator")
	}
	hostedQuietControl(t, endpoint, a.Identity.ID(), "reset-metrics")
	attempts := count()
	pongs := 0
	for n := 0; n < 120; n++ {
		round()
		for _, follow := range follows {
			follow.Probe()
		}
		for i, screen := range screens {
			ping(streams[i])
			ping(watches[i])
			pongs++
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
	t.Logf("R27 hosted memory: peak core=%d reserved=%d budget=%d active anchors=%d", hostedNumber(t, metrics, "peakMemory"), hostedNumber(t, metrics, "reservedBytes"), hostedNumber(t, metrics, "coreBudget"), hostedNumber(t, metrics, "active"))
	if hostedNumber(t, metrics, "peakMemory")+hostedNumber(t, metrics, "reservedBytes") > hostedNumber(t, metrics, "coreBudget") {
		t.Fatal("R27 hosted core reservation exceeded its shared budget")
	}
	t.Logf("R23 R27 hosted personal-leader mode=%t ten-minute idle: Worker HTTP=%d peer/forward frames=%d client attempts=%d platform presence ping/pongs=%d sweeps=%d", fallback, httpCount, frames, clientAttempts, pongs, hostedNumber(t, schedule, "sweeps"))
	if httpCount != 0 || frames != 0 || clientAttempts != 0 || hostedNumber(t, metrics, "writes") != 0 || hostedNumber(t, metrics, "turns") != 0 {
		t.Fatalf("R27 idle activity: %s", metrics)
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
	for _, follow := range follows {
		follow.Close()
	}
	for _, stream := range streams {
		stream.Close()
	}
	for _, watch := range watches {
		watch.Close()
	}
	deadline := time.Now().Add(requestTimeout)
	for {
		state := hostedQuietControl(t, endpoint, a.Identity.ID(), "quiet-state")
		if hostedSocketCounts(t, state)["watch"] == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("R27 closed viewers retained presence")
		}
		time.Sleep(time.Millisecond)
	}
	noViewers := hostedQuietControl(t, endpoint, a.Identity.ID(), "presence-schedule")
	if hostedNumber(t, noViewers, "sweeps") != 0 {
		t.Fatal("R27 no-viewer presence recurred", noViewers)
	}
	// A first actual read must obtain authority from the voters. The following
	// preference and memory write must reach the other learner after eviction.
	_, e = b.Dir.List(t.Context())
	mustNil(t, e)
	if fallback {
		mustNil(t, SaveRelayURL(a.Home, endpoint))
		tickA()
		hostedReady(t, a)
	}
	wake := hostedQuietControl(t, endpoint, a.Identity.ID(), "quiet-state")
	if string(wake["generation"]) != string(before["generation"]) {
		hostedRefreshed(t, a, tickA, generation)
		hostedRefreshed(t, b, tickB, generation)
	} else {
		for _, s := range []*Sync{a, b} {
			waitMember(t, s, func(m *personalMember) bool { return m.failure == nil && m.conn != nil && m.forwardConn != nil })
		}
	}
	status, e := b.clusterClient().Status(t.Context())
	mustNil(t, e)
	if !fallback && status.Leader != receipt.Node {
		t.Fatal("R26 cold wake disrupted the connected personal leader", status)
	}
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
