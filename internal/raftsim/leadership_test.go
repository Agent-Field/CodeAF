package raftsim

import (
	"encoding/json"
	"fmt"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsim/model"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func leadershipHeader(s *Simulator) cs.Membership {
	l := s.Leader()
	idx := s.Nodes[l].Stable.Applied
	return cs.Membership{Revision: 3, Index: idx, NextNode: 6, Anchors: []uint64{1, 2, 3}, Nodes: []uint64{1, 2, 3, 4, 5}, Live: 2, Limit: 4096, Leadership: &cs.LeadershipIntent{Node: 4, Standby: 3, Revision: 3, Barrier: idx, Term: s.Nodes[l].Status.Term}}
}
func simEvidence(s *Simulator, h cs.Membership) raftcore.LeadershipEvidence {
	node := h.Leadership.Node
	if h.Leadership.Restore {
		node = h.Leadership.Standby
	}
	return raftcore.LeadershipEvidence{Node: node, Revision: h.Revision, Applied: s.Nodes[node-1].Stable.Applied, Protocol: 1, Durable: true}
}
func simLeadership(t *testing.T, s *Simulator, h cs.Membership) {
	t.Helper()
	leader := elect(t, s)
	e := simEvidence(s, h)
	s.Do(Action{Kind: "leadership", Node: leader, Leadership: &h, Evidence: &e})
	s.Drain()
	if s.Err != nil {
		finish(t, s)
	}
}
func swapSim(t *testing.T, s *Simulator, h cs.Membership) {
	t.Helper()
	for n := 0; n < 20; n++ {
		leader := elect(t, s)
		p, e := s.Nodes[leader].Core.PlanLeadership(h, simEvidence(s, h))
		if e == raftcore.ErrTransfer || e == raftcore.ErrBusy {
			s.Ticks(1)
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		if p.Settle {
			return
		}
		if p.Fallback {
			t.Fatal("unexpected fallback while applying recorded restore")
		}
		simLeadership(t, s, h)
	}
	t.Fatal("swap failed")
}

// R6/R7/R8/R9 exercise the real core and durable completion protocol for each
// seed. P leads without content; P and every worker disappear, leaving only
// A+B voting. Standby C catches up and re-enters through joint consensus. A
// simultaneous P+anchor loss remains unavailable. Histories are independently
// checked, including the operation after restoration.
func leadershipSeed(t *testing.T, seed int) int {
	s := group(t, Config{Seed: uint64(seed), Nodes: 5})
	initial := create(t, s, "a")
	leader := s.Leader()
	s.Do(Action{Kind: "configure", Node: leader, Count: 5})
	s.Drain()
	s.Ticks(1)
	h := leadershipHeader(s)
	swapSim(t, s, h)
	h.Leadership.Settled = true
	s.Ticks(1)
	simLeadership(t, s, h)
	if s.Leader() != 3 {
		t.Fatal("R6 requested P is not leader")
	}
	// Read receipt must come from P's current term, not its old learner state.
	submit(t, s, 3, model.Call{Kind: "read", Actor: "owner", Cell: "a"})
	if _, e := s.Nodes[3].Core.LeadershipReceipt(h); e != nil {
		t.Fatal("R6 missing actual quorum barrier", e)
	}
	h.Leadership.Completed = true
	s.Do(Action{Kind: "crash", Node: 3, Value: 1})
	s.Do(Action{Kind: "crash", Node: 4, Value: 1})
	// Minority never manufactures a successful coordination result.
	if seed%2 == 0 {
		s.Do(Action{Kind: "crash", Node: 1, Value: 1})
		s.Ticks(13)
		if s.Leader() >= 0 {
			t.Fatal("R8 minority became writable")
		}
		s.Do(Action{Kind: "restart", Node: 1})
		s.Drain()
	}
	startAction := len(s.Actions)
	healthy := 0
	for ; healthy < 13 && s.Leader() < 0; healthy++ {
		s.Ticks(1)
	}
	if s.Leader() < 0 {
		t.Fatalf("R7 fallback exceeded 65 seconds: %d ticks", healthy)
	}
	// A+B commit immediately, before C is promoted and without P's approval.
	taken := submit(t, s, s.Leader(), model.Call{Kind: "acquire", Actor: "fallback", Cell: "a", Expected: initial, Force: true, At: 1000})
	if taken.Code != "ok" || taken.Cell.Fence != initial.Fence+1 {
		t.Fatal("R7 surviving A+B could not commit a new conditional take", taken)
	}
	fallbackTicks := 0
	for _, action := range s.Actions[startAction:] {
		if action.Kind == "ticks" {
			fallbackTicks += action.Count
		}
	}
	if fallbackTicks > 13 {
		t.Fatalf("R7 conditional acquire exceeded 65 seconds: %d ticks", fallbackTicks)
	}
	p, e := s.Nodes[s.Leader()].Core.PlanLeadership(h, raftcore.LeadershipEvidence{})
	if e != nil || !p.Fallback || p.Transfer != 0 {
		t.Fatal("R7 did not request anchor restoration", p, e)
	}
	h.Revision++
	h.Index = s.Nodes[s.Leader()].Stable.Applied
	h.Leadership = &cs.LeadershipIntent{Node: 4, Standby: 3, Revision: h.Revision, Barrier: h.Index, Restore: true}
	swapSim(t, s, h)
	conf := s.Nodes[s.Leader()].Stable.Conf
	if !slices.Equal(conf.GetVoters(), []uint64{1, 2, 3}) || !slices.Contains(conf.GetLearners(), 4) {
		t.Fatal("R7 topology not restored", conf)
	}
	submit(t, s, s.Leader(), model.Call{Kind: "advance_time", Actor: "owner", At: 2000})
	// Rejoining P remains a learner and cannot replay its old transfer intent.
	s.Do(Action{Kind: "restart", Node: 3})
	s.Drain()
	s.Ticks(2)
	if s.Leader() == 3 {
		t.Fatal("R7 automatic leadership regain")
	}
	finish(t, s)
	return fallbackTicks * 5
}

func TestR6R7R8R9SeededLeadershipAndFallback(t *testing.T) {
	seconds := []int{}
	for seed := 0; seed < seedCount(t); seed++ {
		t.Run(fmt.Sprintf("seed-%03d", seed), func(t *testing.T) { seconds = append(seconds, leadershipSeed(t, seed)) })
	}
	slices.Sort(seconds)
	if len(seconds) > 0 {
		t.Logf("R7 conditional acquire: n=%d p50=%ds p95=%ds max=%ds", len(seconds), seconds[(len(seconds)-1)/2], seconds[(95*len(seconds)+99)/100-1], seconds[len(seconds)-1])
	}
}

// R7/T3 retain EVERY original healthy split-vote failure as fixed regression
// seeds. They run the current timing contract, while versioned old evidence
// separately continues to replay with its original event digests.
func TestR7RankedWindowsRetainedSplitVoteSeeds(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "leadership", "r7-split-vote-seeds.json"))
	if err != nil {
		t.Fatal(err)
	}
	var seeds []int
	if json.Unmarshal(data, &seeds) != nil || len(seeds) != 190 {
		t.Fatal("R7 missing retained counterexamples")
	}
	for _, seed := range seeds {
		t.Run(fmt.Sprintf("seed-%03d", seed), func(t *testing.T) { leadershipSeed(t, seed) })
	}
}

// R12 cuts every production swap boundary, then resumes the committed intent.
// The fixture does not remove a voter to escape an inconvenient joint quorum.
func TestR6R7R12LeadershipJointSwapCrashBoundaries(t *testing.T) {
	for _, stage := range []string{"enter", "leave", "reverse-enter", "reverse-leave"} {
		for node := 0; node < 4; node++ {
			for _, phase := range []string{"persist-write", "persist-flush", "persist-ack", "apply-write", "apply-flush", "apply-ack"} {
				t.Run(fmt.Sprintf("%s/%d/%s", stage, node, phase), func(t *testing.T) {
					s := group(t, Config{Seed: 1312, Nodes: 5})
					s.Do(Action{Kind: "configure", Node: s.Leader(), Count: 5})
					s.Drain()
					s.Ticks(1)
					h := leadershipHeader(s)
					if stage == "reverse-enter" || stage == "reverse-leave" {
						swapSim(t, s, h)
						h.Revision++
						h.Index = s.Nodes[s.Leader()].Stable.Applied
						h.Leadership = &cs.LeadershipIntent{Node: 4, Standby: 3, Revision: h.Revision, Barrier: h.Index, Restore: true}
						s.Ticks(1)
					}
					if stage == "leave" || stage == "reverse-leave" {
						simLeadership(t, s, h)
					}
					s.Do(Action{Kind: "arm-cut", Node: node, Other: 1, Request: model.Call{Kind: phase}})
					simLeadership(t, s, h)
					if !s.CutHit {
						t.Fatal("R12 requested cut not reached")
					}
					s.Do(Action{Kind: "restart", Node: node})
					s.Drain()
					s.Ticks(1)
					swapSim(t, s, h)
					finish(t, s)
				})
			}
		}
	}
}

// R7/R12 failing event schedules must remain exactly replayable when the host
// advances its in-memory workflow after an earlier intent was recorded.
func TestR7R12LeadershipWorkflowReplayRetainsEarlierIntent(t *testing.T) {
	s := group(t, Config{Seed: 1313, Nodes: 5})
	s.Do(Action{Kind: "configure", Node: s.Leader(), Count: 5})
	s.Drain()
	s.Ticks(1)
	h := leadershipHeader(s)
	simLeadership(t, s, h)
	h.Leadership.Settled = true
	h.Leadership.Completed = true
	replay, err := Run(Replay{Config: s.initial, Actions: s.Actions})
	if err != nil || replay.Err != nil || replay.Digest() != s.Digest() || replay.Events != s.Events {
		t.Fatalf("R7 earlier intent changed during replay: %v %v", err, replay.Err)
	}
	finish(t, s)
}

// R7/R8/T3 distinguish losing two active voters from losing A after C is
// restored. In the latter topology B+C have quorum and B must ask first,
// committing a conditional operation within 50 seconds. P remains a learner.
func TestR7R8SeededRestoredAnchorsWithAAndPAbsent(t *testing.T) {
	seconds := []int{}
	for seed := 0; seed < seedCount(t); seed++ {
		t.Run(fmt.Sprintf("seed-%03d", seed), func(t *testing.T) {
			s := group(t, Config{Seed: uint64(seed), Nodes: 4})
			initial := create(t, s, "a")
			s.Do(Action{Kind: "configure", Node: s.Leader(), Count: 5})
			s.Drain()
			s.Ticks(1)
			if s.Leader() != 0 {
				t.Fatal("R7 rank A did not lead healthy anchors")
			}
			s.Do(Action{Kind: "crash", Node: 0, Value: 1})
			s.Do(Action{Kind: "crash", Node: 3, Value: 1})
			start := len(s.Actions)
			for tick := 0; tick < 10 && s.Leader() < 0; tick++ {
				s.Ticks(1)
			}
			if s.Leader() != 1 {
				t.Fatal("R7 B did not lead B+C within 50 seconds", s.Leader())
			}
			taken := submit(t, s, 1, model.Call{Kind: "acquire", Actor: "fallback", Cell: "a", Expected: initial, Force: true, At: 1000})
			if taken.Code != "ok" || taken.Cell.Fence != initial.Fence+1 {
				t.Fatal("R7 B+C conditional acquire failed", taken)
			}
			ticks := 0
			for _, a := range s.Actions[start:] {
				if a.Kind == "ticks" {
					ticks += a.Count
				}
			}
			if ticks > 10 {
				t.Fatal("R7 B+C conditional acquire exceeded 50 seconds", ticks)
			}
			seconds = append(seconds, 5*ticks)
			finish(t, s)
		})
	}
	slices.Sort(seconds)
	if len(seconds) > 0 {
		t.Logf("R7 B+C conditional acquire: n=%d p50=%ds p95=%ds max=%ds", len(seconds), seconds[(len(seconds)-1)/2], seconds[(95*len(seconds)+99)/100-1], seconds[len(seconds)-1])
	}
}
