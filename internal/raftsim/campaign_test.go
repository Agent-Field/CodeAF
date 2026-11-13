package raftsim

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Agent-Field/codeaf/internal/raftcore"
	"go.etcd.io/raft/v3"
)

func campaignReplay(t *testing.T, name string) *Simulator {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "campaign", name))
	if err != nil {
		t.Fatal(err)
	}
	var replay Replay
	if err := json.Unmarshal(b, &replay); err != nil {
		t.Fatal(err)
	}
	s, err := Run(replay)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// R4/R5 require eventual majority service through randomized elections. A
// split vote does not turn one timeout's upper bound into a total failover cap.
func TestR4R5SplitVoteReplayKeepsProgressing(t *testing.T) {
	s := campaignReplay(t, "r4-election-292.json")
	if s.Digest() != "6dbfc575ae9dc90f83c8de07a4f05b8a5f679fe6c081fd3d0abbd8df2ca0de61" {
		t.Fatal("original election prefix no longer replays exactly")
	}
	if s.Leader() >= 0 || s.Nodes[0].Status.Role != raft.StatePreCandidate || s.Nodes[2].Status.Role != raft.StateCandidate || s.Nodes[2].Status.Term != 4 {
		t.Fatal("expected the unresolved split vote at the old 25-tick boundary")
	}
	before := s.Events
	leader := freshLeader(t, s, 1)
	if s.Nodes[leader].Status.Term <= 4 || s.Nodes[leader].Stable.Applied <= 16 || s.Events <= before {
		t.Fatal("R4/R5: election did not progress to a committed majority term")
	}
	cell := create(t, s, "after-split")
	if cell.Fence != 1 {
		t.Fatal("R4: recovered majority did not accept a fresh operation")
	}
	finish(t, s)
}

// R17 applies to committed results. This exact lost request initially has no
// result; its original proposal must commit before testing a lost-success retry.
func TestR17LostReplyReplayWaitsForOriginalCommit(t *testing.T) {
	s := campaignReplay(t, "r17-lost-result-1343.json")
	id := len(s.Clients) - 1
	c := s.Clients[id]
	key := c.History.Input.Actor + "/" + c.History.Input.Session
	if _, exists := s.Nodes[c.Node].Stable.App.Sessions[key]; exists || !c.Lost || c.Done {
		t.Fatal("expected an uncommitted original whose reply was lost")
	}
	applied := s.Nodes[c.Node].Stable.Applied
	first := lostResult(t, s, id)
	if s.Nodes[c.Node].Stable.Applied <= applied || first.Code != "ok" || first.Cell.Fence != 2 {
		t.Fatal("R17: original did not durably commit its fence-2 result")
	}
	if again := submit(t, s, c.Node, c.History.Input); again != first {
		t.Fatalf("R17: duplicate changed committed result: %+v %+v", first, again)
	}
	conflict := c.History.Input
	conflict.Digest = "conflicting-digest"
	if out := submit(t, s, c.Node, conflict); out.Code != "conflict" {
		t.Fatalf("R17: conflicting digest succeeded: %+v", out)
	}
	if result := s.Nodes[c.Node].Stable.App.Sessions[key].Result; result != first || s.Clients[id].Done {
		t.Fatal("R17: retry changed the retained result or delivered the lost reply")
	}
	finish(t, s)
}

// R4/R5 bound each election timeout at 6–12 ticks. Check the
// observable election messages, separately from eventual election of a leader.
func TestR4R5SeededElectionTimeoutAndTickPacing(t *testing.T) {
	for seed := 0; seed < seedCount(t); seed++ {
		t.Run(fmt.Sprintf("seed-%03d", seed), func(t *testing.T) {
			s, err := New(Config{Seed: uint64(seed)})
			if err != nil {
				t.Fatal(err)
			}
			s.Drain()
			for node := range s.Nodes {
				isolate(s, node, true)
			}
			last, attempts := make([]int, len(s.Nodes)), make([]int, len(s.Nodes))
			for tick := 1; tick <= 4*raftcore.ElectionTicks; tick++ {
				start := len(s.Trace)
				s.Ticks(1)
				if s.Err != nil {
					t.Fatal(s.Err)
				}
				ticks, campaign := make([]int, len(s.Nodes)), make([]bool, len(s.Nodes))
				for _, event := range s.Trace[start:] {
					if event.Phase == "step-raftcore.Tick" {
						ticks[event.Node]++
					}
					if event.Phase == "send-MsgPreVote" {
						campaign[event.Node] = true
					}
				}
				for node := range s.Nodes {
					if ticks[node] != 1 {
						t.Fatalf("R4/R5: round %d delivered %d ticks to node %d", tick, ticks[node], node)
					}
					if campaign[node] {
						gap := tick - last[node]
						if gap < raftcore.ElectionTicks || gap > 2*raftcore.ElectionTicks {
							t.Fatalf("R4/R5: node %d election after %d ticks", node, gap)
						}
						last[node], attempts[node] = tick, attempts[node]+1
					} else if tick-last[node] > 2*raftcore.ElectionTicks {
						t.Fatalf("R4/R5: node %d election timer stopped", node)
					}
				}
			}
			for node, count := range attempts {
				if count < 2 {
					t.Fatalf("R4/R5: node %d did not retry its election", node)
				}
			}
			finish(t, s)
		})
	}
}
