package syncsetup

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"go.etcd.io/raft/v3"
)

func personalLeader(t *testing.T, r *memberRig, s *Sync) cs.LeadershipReceipt {
	t.Helper()
	readyMember(t, s)
	receipt, err := s.LeadCluster(t.Context(), 0)
	mustNil(t, err)
	waitMember(t, s, func(m *personalMember) bool {
		return m.status.Role == raft.StateLeader && m.status.Term == receipt.Term && m.status.Applied >= receipt.Barrier
	})
	status, err := s.clusterClient().Status(t.Context())
	mustNil(t, err)
	if status.Leader != receipt.Node || !slices.Equal(status.Configuration.Voters, []uint64{1, 2, receipt.Node}) || status.Configuration.Joint() || !slices.Contains(status.Configuration.Learners, 3) || status.Membership.Leadership == nil || !status.Membership.Leadership.Completed {
		t.Fatalf("R6 transfer lacks durable settlement/elected leader: %+v %+v", receipt, status)
	}
	// Status returns as soon as one anchor voter answers ReadIndex quorum, while
	// the remaining peer responses and their cursor acknowledgments finish on
	// the member socket; settle them before callers snapshot post-decision traffic.
	waitMember(t, s, func(m *personalMember) bool { return m.idle })
	return receipt
}

// T1/T2/R6 run the existing directory contract against the REAL external
// leader. The relay is only ingress, storage and A/B/C; P has its own core.
func TestR6PersonalLeaderDirectoryConformance(t *testing.T) {
	fresh := personalLeaderDirectories(t)
	directorytest.RunConsensusRigs(t, func(t *testing.T) directorytest.Rig {
		r, p := fresh(t)
		id := p.Identity
		type apiDevice struct {
			client directory.Client
			id     string
		}
		devices := map[string]apiDevice{}
		var mu sync.Mutex
		get := func(name string) apiDevice {
			mu.Lock()
			defer mu.Unlock()
			if d, ok := devices[name]; ok {
				return d
			}
			seed, err := identity.NewSeed()
			mustNil(t, err)
			dev, err := seed.Join(identity.IssueCert(id, seed.Public(), r.clock.Now()))
			mustNil(t, err)
			d := apiDevice{client: directory.NewHTTP(r.server.URL, reqsign.SignFor(deviceSigner{id, dev}, r.clock.Now), r.server.Client()), id: dev.ID()}
			devices[name] = d
			return d
		}
		return directorytest.Rig{Clock: r.clock, Devices: func(name string) directory.Client { return get(name).client }, ID: func(name string) string { return get(name).id }}
	})
}

// T3/R6 retains the exact saved signature after a committed reply is lost.
// A new client resumes it and cannot choose another target or sequence.
func TestR6PersonalLeadershipLostReplyResumesSignedRequest(t *testing.T) {
	r := newMemberRig(t)
	s := r.home(nil)
	original := r.server.Config.Handler
	var mu sync.Mutex
	commands := []cs.Command{}
	lost := false
	r.server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if q.URL.Path != clusterctl.Path || q.Method != http.MethodPost {
			original.ServeHTTP(w, q)
			return
		}
		record := httptest.NewRecorder()
		original.ServeHTTP(record, q)
		mu.Lock()
		defer mu.Unlock()
		// The client file is the independently persisted request sent before HTTP.
		raw, e := os.ReadFile(filepath.Join(s.Home, "cluster-leadership-"+s.Device.ID()+".json"))
		if e != nil {
			panic(e)
		}
		var saved leadershipSession
		if json.Unmarshal(raw, &saved) != nil || saved.Pending == nil {
			panic("missing saved request")
		}
		commands = append(commands, *saved.Pending)
		if record.Code == 200 && !lost {
			lost = true
			http.Error(w, "lost reply", 503)
			return
		}
		for key, values := range record.Header() {
			w.Header()[key] = values
		}
		w.WriteHeader(record.Code)
		w.Write(record.Body.Bytes())
	})
	readyMember(t, s)
	if _, err := s.LeadCluster(t.Context(), 0); err == nil {
		t.Fatal("R6 lost reply claimed success")
	}
	again := buildAt(s.Home, s.Relay, s.Identity, s.Device, s.Interval, r.clock.Now)
	if _, err := again.LeadCluster(t.Context(), nodeOf(s)+1); err == nil {
		t.Fatal("R6 pending request changed target")
	}
	receipt, err := again.LeadCluster(t.Context(), 0)
	mustNil(t, err)
	mu.Lock()
	defer mu.Unlock()
	if !lost || len(commands) != 2 || !reflect.DeepEqual(commands[0], commands[1]) || receipt.Node != nodeOf(s) {
		t.Fatalf("R6 retry changed request: %+v", commands)
	}
	raw, err := os.ReadFile(filepath.Join(s.Home, "cluster-leadership-"+s.Device.ID()+".json"))
	mustNil(t, err)
	var saved leadershipSession
	mustNil(t, json.Unmarshal(raw, &saved))
	if saved.Pending != nil || saved.Sequence != commands[0].Sequence {
		t.Fatal("R6 completed request not acknowledged")
	}
}

// T4/R7 exercises process death and a use-time quiet decision with virtual
// healthy ticks. A+B must decide before restoring C, without asking P.
func TestR7PersonalLeaderFallbackAndNoAutomaticRegain(t *testing.T) {
	for _, mode := range []string{"death", "death-after-idle", "sync-off", "unpaired"} {
		t.Run(mode, func(t *testing.T) {
			r := newMemberRig(t)
			s := r.home(nil)
			receipt := personalLeader(t, r, s)
			id := s.Identity
			observer := r.home(&id)
			if mode == "death-after-idle" {
				for tick := 0; tick < 20; tick++ {
					r.clock.Advance(DefaultInterval)
					waitMember(t, s, func(m *personalMember) bool { return m.idle })
				}
				waitMember(t, s, func(m *personalMember) bool { return m.sleeping })
			}
			if mode == "death" || mode == "death-after-idle" {
				s.stopMember()
				if mode == "death-after-idle" {
					select {
					case <-r.departures:
					case <-time.After(requestTimeout):
						t.Fatal("R7 server did not observe the closed leader connection")
					}
				}
			} else {
				attempts := s.relayAttempts.Load() + s.clusterFrames.Load()
				if mode == "sync-off" {
					t.Setenv(URLVar, "")
					mustNil(t, SaveRelayURL(s.Home, "off"))
				} else {
					mustNil(t, os.Remove(filepath.Join(s.Home, identity.File)))
				}
				mustNil(t, s.memberRound(t.Context()))
				if s.member != nil || s.relayAttempts.Load()+s.clusterFrames.Load() != attempts {
					t.Fatal("R23 quiet did not stop its leading member locally")
				}
				if mode == "sync-off" {
					mustNil(t, SaveRelayURL(s.Home, r.server.URL))
				} else {
					_, err := os.Stat(filepath.Join(s.Home, identity.File))
					if !errors.Is(err, os.ErrNotExist) {
						t.Fatal(err)
					}
				}
			}
			// Local elapsed time, not real sleeps, drives the healthy ranked windows.
			for tick := 0; tick < 13; tick++ {
				r.clock.Advance(time.Duration(cs.TickEveryMillis) * time.Millisecond)
			}
			status, err := observer.clusterClient().Status(t.Context())
			mustNil(t, err)
			if status.Leader == receipt.Node || status.Leader == 0 {
				t.Fatal("R7 A+B did not elect within 65 virtual seconds", status)
			}
			_, err = observer.Dir.Create(t.Context(), "01J0000000000000000000FAL", directory.CellInit{Head: strings.Repeat("1", 64)})
			mustNil(t, err)
			_, err = observer.Dir.Acquire(t.Context(), "01J0000000000000000000FAL", directory.AcquireOpts{Force: true, Expect: &directory.LeaseExpectation{Device: observer.Device.ID(), Fence: 1}})
			mustNil(t, err)
			for tick := 0; tick < 8; tick++ {
				r.clock.Advance(time.Duration(cs.TickEveryMillis) * time.Millisecond)
			}
			status, err = observer.clusterClient().Status(t.Context())
			mustNil(t, err)
			if !slices.Equal(status.Configuration.Voters, []uint64{1, 2, 3}) || status.Configuration.Joint() || status.Membership.Leadership != nil || !slices.Contains(status.Configuration.Learners, receipt.Node) {
				t.Fatalf("R7 C was not restored: %+v", status)
			}
			if mode == "unpaired" {
				_, err = identity.Adopt(s.Home, id, true)
				mustNil(t, err)
			}
			readyMember(t, s)
			status, err = s.clusterClient().Status(t.Context())
			mustNil(t, err)
			if status.Leader == receipt.Node || !slices.Contains(status.Configuration.Learners, receipt.Node) {
				t.Fatal("R7 returning P regained leadership", status)
			}
		})
	}
}

// T5/R8 retains the SAME three-voter cluster with P+A absent. Surviving B and
// learner C cannot serve a writable minority or force removal.
func TestR8PersonalLeaderAndAnchorLossNeverCreatesSecondCluster(t *testing.T) {
	r := newMemberRig(t)
	s := r.home(nil)
	receipt := personalLeader(t, r, s)
	status, err := s.clusterClient().Status(t.Context())
	mustNil(t, err)
	s.stopMember()
	mustNil(t, r.svc.Close())
	path := filepath.Join(r.storage, "raft", s.Identity.ID(), "1.db")
	mustNil(t, os.Rename(path, path+".asleep"))
	r.svc = relayserve.New(relayserve.Config{Store: r.storage, Now: r.clock.Now, After: r.clock.After})
	_, err = s.clusterClient().Status(t.Context())
	if err == nil || err.Error() != cs.LeadershipUnavailable {
		t.Fatal("R8 minority status", err)
	}
	if _, err = s.Dir.Create(t.Context(), "01J0000000000000000000MIN", directory.CellInit{Head: strings.Repeat("1", 64)}); err == nil {
		t.Fatal("R8 minority write succeeded")
	}
	mustNil(t, r.svc.Close())
	mustNil(t, os.Rename(path+".asleep", path))
	r.svc = relayserve.New(relayserve.Config{Store: r.storage, Now: r.clock.Now, After: r.clock.After})
	after, err := s.clusterClient().Status(context.Background())
	mustNil(t, err)
	if after.Cluster != status.Cluster || receipt.Node == 0 {
		t.Fatal("R8 recovered a different cluster")
	}
}

// T6/R23/R26 repeats the whole open-Home measurement with actual P leadership
// and with P asleep after fallback. Both wire sides AND local attempts count.
func TestR6R7R23R26PersonalLeaderWholeHomeIdle(t *testing.T) {
	for _, mode := range []string{"leading", "asleep-after-fallback"} {
		t.Run(mode, func(t *testing.T) {
			r := newMemberRig(t)
			a := r.home(nil)
			id := a.Identity
			b := r.home(&id)
			readyMember(t, b)
			personalLeader(t, r, a)
			live := []*Sync{a, b}
			if mode == "asleep-after-fallback" {
				a.stopMember()
				live = []*Sync{b}
				for tick := 0; tick < 22; tick++ {
					r.clock.Advance(DefaultInterval)
				}
				status, err := b.clusterClient().Status(t.Context())
				mustNil(t, err)
				if status.Membership.Leadership != nil || !slices.Equal(status.Configuration.Voters, []uint64{1, 2, 3}) {
					t.Fatal("R7 fallback not settled", status)
				}
			}
			ticks := []func(){}
			screens := []*Sync{}
			followers := []dirwatch.Follower{}
			streams := []dirwatch.Stream{}
			for _, s := range live {
				mustNil(t, s.SyncState(t.Context()))
				ctx, cancel := context.WithCancel(t.Context())
				ch := make(chan time.Time)
				done := make(chan struct{})
				go func() { s.runState(ctx, ch); close(done) }()
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
					case ch <- r.clock.Now():
					case <-time.After(requestTimeout):
						t.Fatal("R26 owner stalled")
					}
				}
				ticks = append(ticks, func() { tick(); tick() })
				screen := buildAt(s.Home, s.Relay, s.Identity, s.Device, s.Interval, r.clock.Now)
				screens = append(screens, screen)
				follower := screen.Follow()
				followers = append(followers, follower)
				defer follower.Close()
				stream, err := screen.followStream(t.Context(), screen.Dir.(directory.Watcher))
				mustNil(t, err)
				streams = append(streams, stream)
				defer stream.Close()
			}
			for _, follower := range followers {
				c31AwaitFeed(t, follower, requestTimeout, func(st dirwatch.State) bool { return st.Up })
			}
			round := func() {
				for _, tick := range ticks {
					tick()
				}
				for _, s := range live {
					waitMember(t, s, func(m *personalMember) bool { return m.idle })
				}
			}
			for tick := 0; tick < 24; tick++ {
				r.clock.Advance(DefaultInterval)
				if time.Duration(tick+1)*DefaultInterval%dirwatch.KeepEvery == 0 {
					for _, stream := range streams {
						pingHomeStream(t, stream)
					}
					for _, follower := range followers {
						follower.Probe()
					}
				}
				round()
			}
			attempts := func() uint64 {
				n := a.relayAttempts.Load() + a.clusterFrames.Load() + b.relayAttempts.Load() + b.clusterFrames.Load()
				for _, s := range screens {
					n += s.relayAttempts.Load() + s.clusterFrames.Load()
				}
				return n
			}
			watchBefore := r.watchIn.count.Load() + r.watchOut.count.Load()
			httpBefore, framesBefore, attemptBefore := r.requests.Load(), r.in.count.Load()+r.out.count.Load(), attempts()
			for tick := 0; tick < 120; tick++ {
				r.clock.Advance(DefaultInterval)
				round()
				for i, screen := range screens {
					if time.Duration(tick+1)*DefaultInterval%dirwatch.KeepEvery == 0 {
						pingHomeStream(t, streams[i])
					}
					_, _, err := screen.RowsAt(t.Context())
					mustNil(t, err)
					_, err = screen.Roster(t.Context())
					mustNil(t, err)
					_, err = screen.FleetSize(t.Context())
					mustNil(t, err)
					if time.Duration(tick+1)*DefaultInterval%dirwatch.KeepEvery == 0 {
						followers[i].Probe()
					}
				}
			}
			gotHTTP, gotFrames, gotAttempts := r.requests.Load()-httpBefore, r.in.count.Load()+r.out.count.Load()-framesBefore, attempts()-attemptBefore
			t.Logf("R23/R26 %s ten-minute idle: relay HTTP=%d socket frames=%d client attempts=%d", mode, gotHTTP, gotFrames, gotAttempts)
			if gotHTTP != 0 || gotFrames != 0 || gotAttempts != 0 {
				t.Fatal("R23/R26 whole idle Home generated traffic")
			}
			// Advisory presence permits 488 watch frames with two Homes or 244 with
			// one: two sockets per Home times two frames per KeepEvery in ten minutes,
			// plus two initial snapshot frames per socket. C31 take-back after a
			// computer closes its lid depends on this advisory connection.
			watchBudget := uint64(2*len(live)) * uint64(2*(10*time.Minute/dirwatch.KeepEvery)+2)
			watchFrames := r.watchIn.count.Load() + r.watchOut.count.Load() - watchBefore
			t.Logf("Home watch frames=%d, ten-minute bound=%d", watchFrames, watchBudget)
			if watchFrames > watchBudget {
				t.Fatal("Home presence exceeded the ten-minute watch frame budget")
			}
			_, err := b.Dir.List(t.Context())
			mustNil(t, err)
			mustNil(t, b.Dir.PutDevice(t.Context(), b.Device.ID(), directory.Device{V: 1, Name: "c2VhbGVk"}))
		})
	}
}

// R6/R16 require the new leader to keep serving voters when its configuration
// still names a learner whose committed device revocation already denies use.
func TestR6R16PersonalLeaderSkipsRevokedLearner(t *testing.T) {
	r := newMemberRig(t)
	a := r.home(nil)
	id := a.Identity
	b := r.home(&id)
	readyMember(t, a)
	readyMember(t, b)
	mustNil(t, b.Dir.PutDevice(t.Context(), b.Device.ID(), directory.Device{V: 1, Name: "c2VhbGVk"}))
	mustNil(t, a.Dir.Revoke(t.Context(), b.Device.ID()))
	personalLeader(t, r, a)
	mustNil(t, a.Dir.PutDevice(t.Context(), a.Device.ID(), directory.Device{V: 1, Name: "c2VhbGVk"}))
	if _, err := b.Dir.List(t.Context()); err == nil {
		t.Fatal("R16 revoked learner retained public authority")
	}
}
