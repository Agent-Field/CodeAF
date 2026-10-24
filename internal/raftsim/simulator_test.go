package raftsim

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/raftsim/model"
	"github.com/anishathalye/porcupine"
	"go.etcd.io/raft/v3"
)

var evidenceDir = flag.String("raftsim-evidence-dir", "", "write synthetic replay evidence outside the repository")

func group(t *testing.T, c Config) *Simulator {
	t.Helper()
	s, err := New(c)
	if err != nil {
		t.Fatal(err)
	}
	failureDir := filepath.Join(t.TempDir(), "failure")
	if *evidenceDir != "" {
		failureDir = filepath.Join(*evidenceDir, t.Name())
	}
	t.Cleanup(func() {
		if t.Failed() {
			if err := s.WriteEvidence(failureDir); err != nil {
				t.Log(err)
			}
			t.Logf("exact seed/action/flush/history replay: %s", failureDir)
		}
	})
	s.Drain()
	elect(t, s)
	return s
}
func elect(t *testing.T, s *Simulator) int {
	t.Helper()
	for range 25 {
		if s.Err != nil {
			finish(t, s)
		}
		if l := s.Leader(); l >= 0 {
			return l
		}
		s.Ticks(1)
	}
	t.Fatal("no leader within bounded healthy ticks")
	return -1
}
func finish(t *testing.T, s *Simulator) {
	t.Helper()
	result := model.Check(s.History())
	if s.Err != nil || result != porcupine.Ok {
		dir := filepath.Join(t.TempDir(), "failure")
		if *evidenceDir != "" {
			dir = filepath.Join(*evidenceDir, t.Name())
		}
		if err := s.WriteEvidence(dir); err != nil {
			t.Log(err)
		}
		t.Fatalf("seed=%d events=%d core=%v history=%s evidence=%s", s.initial.Seed, s.Events, s.Err, result, dir)
	}
}
func done(t *testing.T, s *Simulator, id int) model.Outcome {
	t.Helper()
	s.Drain()
	for tries := 0; tries < 12 && !s.Clients[id].Done && s.Err == nil; tries++ {
		s.Ticks(1)
	}
	if s.Err != nil {
		finish(t, s)
	}
	if !s.Clients[id].Done {
		t.Fatalf("request %d did not finish at seed %d", id, s.initial.Seed)
	}
	return s.Clients[id].History.Output
}
func submit(t *testing.T, s *Simulator, node int, op model.Call) model.Outcome {
	t.Helper()
	return done(t, s, s.Submit(node, op))
}
func create(t *testing.T, s *Simulator, cell string) model.Cell {
	t.Helper()
	out := submit(t, s, s.Leader(), model.Call{Kind: "create", Actor: "owner", Cell: cell, Head: "head-0", At: 100})
	if out.Code != "ok" {
		t.Fatal(out)
	}
	return out.Cell
}
func isolate(s *Simulator, node int, blocked bool) {
	value := int64(0)
	if blocked {
		value = 1
	}
	for i := range s.Nodes {
		if i != node {
			s.Do(Action{Kind: "block", Node: node, Other: i, Value: value})
			s.Do(Action{Kind: "block", Node: i, Other: node, Value: value})
		}
	}
}
func freshLeader(t *testing.T, s *Simulator, old int) int {
	t.Helper()
	for range 25 {
		s.Ticks(1)
		if l := s.Leader(); l >= 0 && l != old {
			return l
		}
		if s.Err != nil {
			finish(t, s)
		}
	}
	t.Fatal("healthy majority failed to elect")
	return -1
}

// R1–R5/R13/R17 share one whole-identity history per seed, including unknown
// outcomes. Each run stays below 2,000 effect events and 100 operations.
func TestR1R2R3R4R5R13R17SeededFaultCampaign(t *testing.T) {
	total, maxEvents := 0, 0
	for seed := uint64(0); seed < 100; seed++ {
		t.Run(fmt.Sprintf("seed-%03d", seed), func(t *testing.T) {
			s := group(t, Config{Seed: seed, Nodes: 4, Drop: 10, Duplicate: 100, Delay: 3})
			a := create(t, s, "a")
			create(t, s, "b")
			leader := s.Leader()
			one := s.Submit(leader, model.Call{Kind: "acquire", Actor: "take-a", Cell: "a", Expected: a, Force: true, At: 110})
			two := s.Submit(leader, model.Call{Kind: "acquire", Actor: "take-b", Cell: "a", Expected: a, Force: seed%2 == 0, At: 90})
			s.Drain()
			won, lost := done(t, s, one), done(t, s, two)
			if won.Code == "lease_held" {
				won, lost = lost, won
			}
			if won.Code != "ok" || lost.Code != "lease_held" || won.Cell.Fence != 2 {
				t.Fatalf("R1: %+v %+v", won, lost)
			}
			for _, kind := range []string{"publish", "beat", "release"} {
				out := submit(t, s, leader, model.Call{Kind: kind, Actor: "owner", Cell: "a", Fence: 1, OldHead: "wrong", Head: "displaced", At: 120})
				if out.Code != "fence_stale" {
					t.Fatalf("R2 %s: %+v", kind, out)
				}
			}
			// The explicit clock log includes backward samples and long sleep; it
			// changes expiry semantics, never ticks or ownership fencing.
			submit(t, s, leader, model.Call{Kind: "advance_time", Actor: "owner", At: 3_600_000})
			submit(t, s, leader, model.Call{Kind: "advance_time", Actor: "owner", At: -400})
			take := model.Call{Kind: "acquire", Actor: "retry", Cell: "b", Expected: observed(s.Nodes[leader].Stable.App.Cells["b"]), Force: true, At: 3_600_001, Session: "durable", Sequence: 1, Digest: "opaque-1"}
			dropped := s.Submit(leader, take)
			s.Do(Action{Kind: "timeout", Node: dropped})
			s.Drain()
			first := s.Nodes[leader].Stable.App.Sessions["retry/durable"].Result
			again := submit(t, s, leader, take)
			if again != first || again.Code != "ok" || again.Cell.Fence != 2 {
				t.Fatalf("R17: retry changed result %+v %+v", first, again)
			}
			conflict := take
			conflict.Digest = "other-digest"
			if submit(t, s, leader, conflict).Code != "conflict" {
				t.Fatal("R17: conflicting digest succeeded")
			}
			// A minority leader may accept admission but can produce neither a fresh
			// authority read nor a successful mutation after the majority moves on.
			isolate(s, leader, true)
			next := freshLeader(t, s, leader)
			before := observed(s.Nodes[next].Stable.App.Cells["a"])
			moved := submit(t, s, next, model.Call{Kind: "acquire", Actor: "majority", Cell: "a", Expected: before, Force: true, At: 3_600_010})
			if moved.Code != "ok" {
				t.Fatal(moved)
			}
			read := s.Submit(leader, model.Call{Kind: "read", Actor: "owner", Cell: "a"})
			write := s.Submit(leader, model.Call{Kind: "publish", Actor: won.Cell.Holder, Cell: "a", Fence: won.Cell.Fence, OldHead: "head-0", Head: "isolated", At: 3_600_020})
			s.Drain()
			if s.Clients[read].Done || s.Clients[write].Done {
				t.Fatal("R4: minority success")
			}
			s.Do(Action{Kind: "timeout", Node: read})
			s.Do(Action{Kind: "timeout", Node: write})
			s.Do(Action{Kind: "crash", Node: leader, Value: 1})
			s.Do(Action{Kind: "restart", Node: leader})
			isolate(s, leader, false)
			s.Ticks(2)
			s.Drain()
			leader = s.Leader()
			if out := submit(t, s, leader, model.Call{Kind: "read", Actor: "owner", Cell: "a"}); out.Cell != moved.Cell {
				t.Fatal("R3: recovery lost winner")
			}
			// Snapshot/compaction keeps exact session results through a second crash.
			s.Do(Action{Kind: "snapshot", Node: leader})
			s.Do(Action{Kind: "crash", Node: leader, Value: 1})
			s.Do(Action{Kind: "restart", Node: leader})
			s.Drain()
			s.Ticks(2)
			leader = s.Leader()
			if leader < 0 {
				leader = elect(t, s)
			}
			if out := submit(t, s, leader, take); out != first {
				t.Fatal("R13/R17: compact/restart changed retry")
			}
			// Every seed also crosses the old/new-majority intersection.
			// Node 4 starts empty, catches up (including the compacted prefix),
			// then participates in the explicit joint swap.
			s.Do(Action{Kind: "network"})
			s.Do(Action{Kind: "configure", Node: leader, Other: 3, Count: 1})
			s.Drain()
			// A joining learner must be in the snapshot's ConfState. A
			// pre-admission checkpoint cannot bootstrap that new member.
			s.Do(Action{Kind: "snapshot", Node: leader})
			for attempt := 0; attempt < 20 && s.Nodes[3].Stable.Applied < s.Nodes[leader].Stable.Applied; attempt++ {
				s.Ticks(1)
			}
			if s.Nodes[3].Stable.Applied < s.Nodes[leader].Stable.Applied {
				t.Fatalf("R12: learner did not reach the promotion barrier: %v", s.Err)
			}
			s.Ticks(1)
			s.Do(Action{Kind: "configure", Node: leader, Count: 2})
			s.Drain()
			s.Do(Action{Kind: "configure", Node: leader, Count: 3})
			s.Drain()
			s.Ticks(1)
			if s.Leader() < 0 {
				elect(t, s)
			}
			for _, n := range s.Nodes {
				if !slices.Equal(n.Stable.Conf.GetVoters(), []uint64{1, 2, 4}) || len(n.Stable.Conf.GetVotersOutgoing()) > 0 {
					t.Fatalf("R12: seeded joint swap did not finish: conf=%v status=%+v err=%v events=%d", n.Stable.Conf, n.Status, s.Err, s.Events)
				}
			}
			finish(t, s)
			total += s.Events
			maxEvents = max(maxEvents, s.Events)
			if seed == 42 {
				replayed, replayErr := Run(s.Evidence().Replay)
				if replayErr != nil || replayed.Digest() != s.Digest() {
					t.Fatal("seed replay diverged")
				}
			}
		})
	}
	t.Logf("100 fixed seeds, %d events, maximum %d/2000 per seed", total, maxEvents)
}

func TestR5HundredMinorityFlapsKeepHealthyTerm(t *testing.T) {
	s := group(t, Config{Seed: 501})
	leader := s.Leader()
	term := s.Nodes[leader].Status.Term
	minority := (leader + 1) % 3
	for range 100 {
		isolate(s, minority, true)
		s.Do(Action{Kind: "node-ticks", Node: minority, Count: 7})
		isolate(s, minority, false)
		s.Ticks(1)
		if s.Nodes[leader].Status.Term != term || s.Nodes[leader].Status.Role != raft.StateLeader {
			t.Fatal("R5: flapping minority displaced healthy leader")
		}
	}
	finish(t, s)
	t.Logf("100 flaps, %d events, stable term %d", s.Events, term)
}

// These deliberate faults live only in the development harness. The same
// checker/ordering monitor must reject each, otherwise the gate proves little.
func TestR1R3R4BrokenFenceReadAndFlushAreDetected(t *testing.T) {
	for _, variant := range []string{"fence", "read", "flush"} {
		t.Run(variant, func(t *testing.T) {
			s, err := New(Config{Seed: 77, Broken: variant})
			if err != nil {
				t.Fatal(err)
			}
			s.Drain()
			if variant != "flush" {
				elect(t, s)
				original := create(t, s, "a")
				leader := s.Leader()
				if variant == "fence" {
					s.Submit(leader, model.Call{Kind: "acquire", Actor: "b", Cell: "a", Expected: original, Force: true})
					s.Submit(leader, model.Call{Kind: "acquire", Actor: "c", Cell: "a", Expected: original, Force: true})
					s.Drain()
				} else {
					isolate(s, leader, true)
					next := freshLeader(t, s, leader)
					submit(t, s, next, model.Call{Kind: "publish", Actor: "owner", Cell: "a", Fence: 1, OldHead: "head-0", Head: "new"})
					s.Submit(leader, model.Call{Kind: "read", Actor: "owner", Cell: "a"})
					s.Drain()
				}
				if model.Check(s.History()) != porcupine.Illegal {
					t.Fatal("broken variant escaped independent checker")
				}
				if len(model.Minimize(s.History())) == 0 {
					t.Fatal("missing minimized counterexample")
				}
			} else if s.Err == nil || !strings.Contains(s.Err.Error(), "before flush") {
				t.Fatal("early acknowledgment escaped flush monitor", s.Err)
			}
			if *evidenceDir != "" {
				if err := s.WriteEvidence(filepath.Join(*evidenceDir, "broken-"+variant)); err != nil {
					t.Fatal(err)
				}
			}
			replayed, replayErr := Run(s.Evidence().Replay)
			if replayErr == nil && s.Err != nil || replayed.Digest() != s.Digest() || fmt.Sprint(replayed.Err) != fmt.Sprint(s.Err) {
				t.Fatal("negative replay diverged")
			}
		})
	}
}

func TestR3R12CrashEveryReadyBoundary(t *testing.T) {
	phases := []string{"persist-write", "persist-flush", "persist-ack", "apply-write", "apply-flush", "apply-ack", "reply"}
	for _, phase := range phases {
		for node := 0; node < 3; node++ {
			if phase == "reply" && node > 0 {
				continue
			}
			t.Run(fmt.Sprintf("%s/%d", phase, node), func(t *testing.T) {
				s := group(t, Config{Seed: 1001})
				create(t, s, "a")
				cutNode := node
				if phase == "reply" {
					cutNode = s.Leader()
				}
				// Replay must start from the same initial config, with fault arming recorded.
				s.Do(Action{Kind: "arm-cut", Node: cutNode, Other: 1, Request: model.Call{Kind: phase}})
				leader := s.Leader()
				request := s.Submit(leader, model.Call{Kind: "publish", Actor: "owner", Cell: "a", Fence: 1, OldHead: "head-0", Head: "durable"})
				s.Drain()
				if !s.CutHit {
					t.Fatal("required cut was not reached")
				}
				s.Do(Action{Kind: "restart", Node: cutNode})
				s.Drain()
				if !s.Clients[request].Done {
					s.Do(Action{Kind: "timeout", Node: request})
				}
				s.Ticks(2)
				leader = s.Leader()
				if leader < 0 {
					leader = elect(t, s)
				}
				// Unknown mutation may be absent or present, and a read decides which. No
				// invocation is removed just because its successful reply was lost.
				submit(t, s, leader, model.Call{Kind: "read", Actor: "owner", Cell: "a"})
				finish(t, s)
			})
		}
	}
}

func TestR3DiskFailuresStallsTornWritesAndOldGeneration(t *testing.T) {
	s := group(t, Config{Seed: 801, Duplicate: 200, Delay: 4})
	create(t, s, "a")
	leader := s.Leader()
	follower := (leader + 1) % 3
	s.Do(Action{Kind: "disk-fail", Node: follower})
	s.Do(Action{Kind: "stall", Node: (leader + 2) % 3, Value: 20})
	out := submit(t, s, leader, model.Call{Kind: "publish", Actor: "owner", Cell: "a", Fence: 1, OldHead: "head-0", Head: "after-error"})
	if out.Code != "ok" || s.Nodes[follower].Alive {
		t.Fatal("disk failure did not stop member")
	}
	s.Do(Action{Kind: "restart", Node: follower})
	s.Ticks(2)
	s.Drain()
	if s.Nodes[follower].Stable.App.Cells["a"].Head != "after-error" {
		t.Fatal("failed follower did not recover")
	}
	finish(t, s)
}

func TestR1R4R17ReplayFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/*.json")
	if err != nil || len(files) == 0 {
		t.Fatal("missing replay fixtures", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var fixture struct {
				History  []model.Operation
				Expected porcupine.CheckResult
			}
			if err = json.Unmarshal(data, &fixture); err != nil {
				t.Fatal(err)
			}
			if got := model.Check(fixture.History); got != fixture.Expected {
				t.Fatalf("fixture %s: %s", file, got)
			}
		})
	}
}

// All joint fields must survive recovery; an outgoing member may lead while
// joint but must never regain a vote after the durable leave configuration.
func TestR12JointSwapCrashIntersections(t *testing.T) {
	for _, stage := range []string{"enter", "leave"} {
		for node := 0; node < 4; node++ {
			for _, phase := range []string{"persist-write", "persist-flush", "persist-ack", "apply-write", "apply-flush", "apply-ack"} {
				t.Run(fmt.Sprintf("%s/%d/%s", stage, node, phase), func(t *testing.T) {
					s := group(t, Config{Seed: 1201, Nodes: 4})
					leader := s.Leader()
					s.Do(Action{Kind: "configure", Node: leader, Other: 3, Count: 1})
					s.Drain()
					s.Ticks(1)
					if stage == "leave" {
						s.Do(Action{Kind: "configure", Node: leader, Count: 2})
						s.Drain()
					}
					s.Do(Action{Kind: "arm-cut", Node: node, Other: 1, Request: model.Call{Kind: phase}})
					kind := 2
					if stage == "leave" {
						kind = 3
					}
					s.Do(Action{Kind: "configure", Node: leader, Count: kind})
					s.Drain()
					if !s.CutHit {
						t.Fatal("joint boundary not reached")
					}
					s.Do(Action{Kind: "restart", Node: node})
					s.Drain()
					s.Ticks(2)
					leader = s.Leader()
					if leader < 0 {
						leader = elect(t, s)
					}
					// A pre-commit crash leaves an unknown configuration
					// result. Reconcile the durable state and resubmit the
					// missing stage instead of treating admission as success.
					for attempt := 0; attempt < 25; attempt++ {
						conf := s.Nodes[leader].Stable.Conf
						if slices.Equal(conf.GetVoters(), []uint64{1, 2, 4}) && len(conf.GetVotersOutgoing()) == 0 {
							break
						}
						change := 2
						if len(conf.GetVotersOutgoing()) > 0 {
							change = 3
						}
						s.Do(Action{Kind: "configure", Node: leader, Count: change})
						s.Drain()
						s.Ticks(1)
						leader = s.Leader()
						if leader < 0 {
							leader = elect(t, s)
						}
					}
					s.Ticks(1)
					for i, n := range s.Nodes {
						c := n.Stable.Conf
						if !slices.Equal(c.GetVoters(), []uint64{1, 2, 4}) || len(c.GetVotersOutgoing()) != 0 || !slices.Equal(c.GetLearners(), []uint64{3}) {
							t.Fatalf("node %d lost final configuration: %v", i, c)
						}
					}
					s.Do(Action{Kind: "crash", Node: 2, Value: 1})
					s.Do(Action{Kind: "restart", Node: 2})
					s.Ticks(12)
					if s.Nodes[2].Status.Role == raft.StateLeader {
						t.Fatal("removed voter regained leadership")
					}
					finish(t, s)
				})
			}
		}
	}
}

func TestR12EveryOldNewMajorityIntersection(t *testing.T) {
	for live := 0; live < 16; live++ {
		t.Run(fmt.Sprintf("live-%04b", live), func(t *testing.T) {
			s := group(t, Config{Seed: 1202, Nodes: 4})
			create(t, s, "a")
			leader := s.Leader()
			s.Do(Action{Kind: "configure", Node: leader, Other: 3, Count: 1})
			s.Drain()
			s.Do(Action{Kind: "configure", Node: leader, Count: 2})
			s.Drain()
			old, newCount := 0, 0
			for i := 0; i < 4; i++ {
				if live&(1<<i) == 0 {
					s.Do(Action{Kind: "crash", Node: i, Value: 1})
				} else {
					if i < 3 {
						old++
					}
					if i != 2 {
						newCount++
					}
				}
			}
			quorum := old >= 2 && newCount >= 2
			s.Ticks(15)
			if quorum && s.Leader() < 0 {
				elect(t, s)
			}
			node := s.Leader()
			if node < 0 {
				for i := range s.Nodes {
					if live&(1<<i) != 0 {
						node = i
						break
					}
				}
			}
			request := -1
			if node >= 0 {
				request = s.Submit(node, model.Call{Kind: "publish", Actor: "owner", Cell: "a", Fence: 1, OldHead: "head-0", Head: "joint-result"})
				s.Drain()
				s.Ticks(2)
			}
			if quorum && (request < 0 || !s.Clients[request].Done || s.Clients[request].History.Output.Code != "ok") {
				t.Fatal("joint quorum could not commit")
			}
			if !quorum && request >= 0 && s.Clients[request].Done {
				t.Fatal("joint change accepted only one side's majority")
			}
			if request >= 0 && !s.Clients[request].Done {
				s.Do(Action{Kind: "timeout", Node: request})
			}
			for i, n := range s.Nodes {
				if !n.Alive {
					s.Do(Action{Kind: "restart", Node: i})
				}
			}
			s.Ticks(12)
			s.Drain()
			for _, n := range s.Nodes {
				if len(n.Stable.Conf.GetVotersOutgoing()) != 3 {
					t.Fatal("recovery silently left joint state")
				}
			}
			finish(t, s)
		})
	}
}

func TestR3AcknowledgedPublishSurvivesLeaderAndSecondMemberPowerLoss(t *testing.T) {
	s := group(t, Config{Seed: 303})
	create(t, s, "a")
	leader := s.Leader()
	published := submit(t, s, leader, model.Call{Kind: "publish", Actor: "owner", Cell: "a", Fence: 1, OldHead: "head-0", Head: "retained"})
	other := (leader + 1) % 3
	s.Do(Action{Kind: "crash", Node: leader, Value: 1})
	s.Do(Action{Kind: "crash", Node: other, Value: 1})
	s.Do(Action{Kind: "restart", Node: other})
	s.Drain()
	fallback := elect(t, s)
	if out := submit(t, s, fallback, model.Call{Kind: "read", Actor: "owner", Cell: "a"}); out != published {
		t.Fatal("acknowledged head/fence lost after second-member power recovery")
	}
	s.Do(Action{Kind: "restart", Node: leader})
	s.Ticks(2)
	if s.Nodes[leader].Stable.App.Cells["a"].Head != "retained" {
		t.Fatal("old leader lost acknowledged head")
	}
	finish(t, s)
}

func TestR4DelayedOldReadAcksCannotAuthorizeAFreshMinorityRead(t *testing.T) {
	s := group(t, Config{Seed: 404})
	create(t, s, "a")
	old := s.Leader()
	s.Do(Action{Kind: "delay-read-acks", Node: old, Value: 1000})
	before := s.Submit(old, model.Call{Kind: "read", Actor: "owner", Cell: "a"})
	s.Do(Action{Kind: "drain", Value: 20})
	isolate(s, old, true)
	next := -1
	// Only the reachable majority's clocks advance. The isolated old leader
	// retains its volatile role while old barrier acknowledgments are delayed.
	for attempt := 0; attempt < 25 && next < 0; attempt++ {
		for node := range s.Nodes {
			if node != old {
				s.Do(Action{Kind: "node-ticks", Node: node, Count: 1})
			}
		}
		for node, n := range s.Nodes {
			if node != old && n.Status.Role == raft.StateLeader {
				next = node
			}
		}
	}
	if next < 0 {
		t.Fatal("reachable majority failed to elect")
	}
	changed := s.Submit(next, model.Call{Kind: "publish", Actor: "owner", Cell: "a", Fence: 1, OldHead: "head-0", Head: "new"})
	for attempts := 0; attempts < 150 && !s.Clients[changed].Done; attempts++ {
		s.Do(Action{Kind: "events", Count: 1})
	}
	if !s.Clients[changed].Done {
		t.Fatal("reachable majority failed to publish")
	}
	if s.Clients[before].Done {
		t.Fatal("fixture delivered held old acknowledgments too soon")
	}
	fresh := s.Submit(old, model.Call{Kind: "read", Actor: "owner", Cell: "a"})
	s.Drain()
	if s.Clients[fresh].Done {
		t.Fatal("old quorum contexts authorized a fresh minority read")
	}
	s.Do(Action{Kind: "timeout", Node: fresh})
	finish(t, s)
}

func TestR3PowerLossDiscardsWrittenAndTornUncommittedSuffix(t *testing.T) {
	s := group(t, Config{Seed: 330})
	create(t, s, "a")
	leader := s.Leader()
	baseline := s.Nodes[leader].Stable.copy()
	request := s.Submit(leader, model.Call{Kind: "publish", Actor: "owner", Cell: "a", Fence: 1, OldHead: "head-0", Head: "unflushed"})
	for i := 0; i < 100 && len(s.Nodes[leader].Torn) == 0; i++ {
		s.Do(Action{Kind: "events", Count: 1})
	}
	n := s.Nodes[leader]
	if n.Written == nil || len(n.Torn) == 0 || n.Stable.Hard.GetCommit() != baseline.Hard.GetCommit() || s.Clients[request].Done {
		t.Fatal("fixture did not stop between write and flush")
	}
	s.Do(Action{Kind: "crash", Node: leader, Value: 1})
	if n.Written != nil || len(n.Torn) != 0 || n.Stable.Hard.GetCommit() != baseline.Hard.GetCommit() || n.Stable.App.Cells["a"].Head != "head-0" {
		t.Fatal("power loss retained unflushed or partial bytes")
	}
	s.Do(Action{Kind: "timeout", Node: request})
	s.Do(Action{Kind: "restart", Node: leader})
	s.Drain()
	s.Ticks(2)
	if n.Stable.App.Cells["a"].Head != "head-0" {
		t.Fatal("stale-generation flush completed the lost suffix")
	}
	finish(t, s)
}

func TestR3R12ProcessDeathRetainsCompleteWrittenTransactionForReplay(t *testing.T) {
	s := group(t, Config{Seed: 331})
	create(t, s, "a")
	leader := s.Leader()
	request := s.Submit(leader, model.Call{Kind: "publish", Actor: "owner", Cell: "a", Fence: 1, OldHead: "head-0", Head: "process-retained"})
	for i := 0; i < 100 && s.Nodes[leader].Written == nil; i++ {
		s.Do(Action{Kind: "events", Count: 1})
	}
	n := s.Nodes[leader]
	if n.Written == nil || len(n.Written.Entries) <= len(n.Stable.Entries) {
		t.Fatal("fixture did not reach a complete written append")
	}
	written := n.Written.copy()
	s.Do(Action{Kind: "crash", Node: leader, Value: 0})
	if len(n.Stable.Entries) != len(written.Entries) {
		t.Fatal("process death lost the written transaction")
	}
	s.Do(Action{Kind: "timeout", Node: request})
	s.Do(Action{Kind: "restart", Node: leader})
	s.Drain()
	s.Ticks(2)
	finish(t, s)
}

func TestR1R3R4CounterexampleReplayFixtures(t *testing.T) {
	files, err := filepath.Glob("testdata/replay/*.json")
	if err != nil || len(files) != 3 {
		t.Fatal("missing simulator replay fixtures", err)
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			var fixture struct {
				Requirement string
				Replay      Replay
				Expected    string
			}
			if err = json.Unmarshal(data, &fixture); err != nil {
				t.Fatal(err)
			}
			s, runErr := Run(fixture.Replay)
			if fixture.Requirement == "R3" {
				if runErr == nil || !strings.Contains(runErr.Error(), fixture.Expected) {
					t.Fatal("flush counterexample escaped", runErr)
				}
			} else if runErr != nil || string(model.Check(s.History())) != fixture.Expected {
				t.Fatal("history counterexample escaped", runErr)
			}
			again, againErr := Run(fixture.Replay)
			if s.Digest() != again.Digest() || fmt.Sprint(runErr) != fmt.Sprint(againErr) {
				t.Fatal("stored replay was not deterministic")
			}
		})
	}
}
