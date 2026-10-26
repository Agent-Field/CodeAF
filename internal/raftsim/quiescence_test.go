package raftsim

import (
	"fmt"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsim/model"
)

func sleepMarker(t *testing.T, s *Simulator, node int) cs.Quiescence {
	t.Helper()
	var m cs.Quiescence
	if cs.Decode(s.Nodes[node].Stable.App.Coordination["membership/quiescence"], &m) != nil {
		t.Fatal("R26 missing durable marker")
	}
	return m
}

// R26 covers all partial final-delivery sets, cold followers, old certificates,
// first minority reads and first conditional writes after wake. Every marker
// uses the real production evaluator and every ack follows its durable apply.
func TestR26SeededPartialSleepAndWake(t *testing.T) {
	for seed := 0; seed < seedCount(t); seed++ {
		t.Run(fmt.Sprintf("seed-%04d", seed), func(t *testing.T) {
			s := group(t, Config{Seed: uint64(seed)})
			initial := create(t, s, "chat")
			l := s.Leader()
			submit(t, s, l, model.Call{Kind: "release", Actor: "owner", Cell: "chat", Fence: initial.Fence, At: 1000})
			submit(t, s, l, model.Call{Kind: "read", Actor: "owner", Cell: "chat"})
			for node := range s.Nodes {
				s.Do(Action{Kind: "schedule", Node: node, Schedule: &raftcore.ScheduleInput{Now: 0}})
			}
			s.Do(Action{Kind: "quiesce", Node: l, Value: 1000})
			s.Drain()
			s.Ticks(1)
			if s.Err != nil {
				finish(t, s)
			}
			marker := sleepMarker(t, s, l)
			acks := []raftcore.SleepAck{}
			for node, n := range s.Nodes {
				local := sleepMarker(t, s, node)
				if local.Index != marker.Index || n.Stable.Applied != marker.Index {
					t.Fatal("R26 premature applied ack", node, local, n.Stable.Applied)
				}
				acks = append(acks, raftcore.SleepAck{Node: uint64(node + 1), Index: local.Index, Term: local.Term})
			}
			// A majority applied acknowledgment cannot suppress even the leader's timer.
			s.Do(Action{Kind: "schedule", Node: l, Schedule: &raftcore.ScheduleInput{Now: 60_000, Marker: marker, Acks: acks[:2]}})
			s.Drain()
			if s.Nodes[l].SchedulePlan.Sleeping {
				t.Fatal("R26 partial acknowledgments slept")
			}
			// The final certificate can reach any subset. Unreached members remain active.
			for node := range s.Nodes {
				if seed%8&(1<<node) != 0 {
					s.Do(Action{Kind: "schedule", Node: node, Schedule: &raftcore.ScheduleInput{Now: 60_000, Marker: marker, Acks: acks}})
				}
			}
			s.Drain()
			cold := seed % 3
			s.Do(Action{Kind: "crash", Node: cold, Value: 1})
			s.Do(Action{Kind: "restart", Node: cold})
			s.Drain()
			s.Do(Action{Kind: "schedule", Node: cold, Schedule: &raftcore.ScheduleInput{Now: 60_001, Marker: marker, Acks: acks}})
			s.Drain()
			if s.Nodes[cold].SchedulePlan.Sleeping {
				t.Fatal("R26 old generation slept after recovery")
			}
			// Isolate the remembered leader before its first authority read.
			l = elect(t, s)
			isolate(s, l, true)
			read := s.Do(Action{Kind: "submit", Node: l, Request: model.Call{Kind: "read", Actor: "owner", Cell: "chat"}})
			s.Drain()
			if s.Clients[read].Done && !s.Clients[read].History.Unknown {
				t.Fatal("R26 minority wake served cached authority")
			}
			s.Do(Action{Kind: "timeout", Node: read})
			isolate(s, l, false)
			s.Drain()
			l = elect(t, s)
			submit(t, s, l, model.Call{Kind: "read", Actor: "owner", Cell: "chat"})
			next := submit(t, s, l, model.Call{Kind: "acquire", Actor: "next", Cell: "chat", Expected: initial, At: 2000})
			if next.Code != "ok" || next.Cell.Fence != initial.Fence+1 {
				t.Fatal("R26 wake write failed", next)
			}
			s.Do(Action{Kind: "schedule", Node: l, Schedule: &raftcore.ScheduleInput{Now: 60_002, Marker: marker, Acks: acks}})
			s.Drain()
			if s.Nodes[l].SchedulePlan.Sleeping {
				t.Fatal("R26 delayed old certificate slept after mutation")
			}
			finish(t, s)
			// Exact replay includes timer/certificate delivery, crashes and all flushes.
			replay, e := Run(s.Evidence().Replay)
			if e != nil || replay.Digest() != s.Digest() {
				t.Fatal("R26 non-replayable wake", e)
			}
		})
	}
}
