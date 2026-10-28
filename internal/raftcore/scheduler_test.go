package raftcore

import (
	"bytes"
	"encoding/json"
	"go.etcd.io/raft/v3"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

// R26 uses actual durable Ready completions and safe reads; sleep certificates
// cannot be inferred from majority transport delivery or a remembered leader.
func TestR26PartialQuiescenceWakeAndRestart(t *testing.T) {
	g := newGroup(t)
	g.elect()
	l := g.leader()
	c := g.nodes[l-1]
	s := c.status()
	marker := cs.Quiescence{Index: s.Applied, Term: s.Term, Barrier: s.Applied - 1, Voters: []uint64{1, 2, 3}}
	acks := []SleepAck{}
	for _, node := range g.nodes {
		acks = append(acks, SleepAck{Node: node.cfg.NodeID, Index: s.Applied, Term: s.Term})
	}
	for _, n := range g.nodes {
		if _, e := n.Schedule(ScheduleInput{Now: 0}); e != nil {
			t.Fatal(e)
		}
	}
	p, e := c.Schedule(ScheduleInput{Now: 60_000, Marker: marker, Acks: acks[:2]})
	if e != nil || p.Sleeping {
		t.Fatal("R26 partial barrier slept", p, e)
	}
	p, e = c.Schedule(ScheduleInput{Now: 60_000, Marker: marker, Acks: acks})
	if e != nil || !p.Sleeping || p.Tick || p.After != 0 {
		t.Fatal("R26 complete barrier did not sleep", p, e)
	}
	// A one-hour scheduler delay injects no tick debt; the operation wakes it.
	g.call(l, Read{RequestID: "wake"})
	g.pump()
	p, e = c.Schedule(ScheduleInput{Now: 3_600_000, Marker: marker, Acks: acks})
	if e != nil || p.Sleeping || len(p.Wake) != 3 {
		t.Fatal("R26 wake reused certificate", p, e)
	}
	d := g.durable[l-1]
	recovered, e := New(Config{ClusterUUID: "test-cluster", NodeID: l, Seed: 88, Protocol: 1, Anchors: []uint64{1, 2, 3}}, d)
	if e != nil {
		t.Fatal(e)
	}
	p, e = recovered.Schedule(ScheduleInput{Now: 0, Marker: marker, Acks: acks})
	if e != nil || p.Sleeping {
		t.Fatal("R26 recovery reused sleep certificate", p, e)
	}
}

// R27 counts virtual whole days with no wall sleeps. A delayed scheduler turn
// runs at most one tick; stable heartbeats create no application/log writes.
func TestR27VirtualDayCounters(t *testing.T) {
	g := newGroup(t)
	g.elect()
	l := g.leader()
	c := g.nodes[l-1]
	st := c.status()
	marker := cs.Quiescence{Index: st.Applied, Term: st.Term, Barrier: st.Applied - 1, Voters: []uint64{1, 2, 3}}
	acks := []SleepAck{{1, st.Applied, st.Term}, {2, st.Applied, st.Term}, {3, st.Applied, st.Term}}
	c.Schedule(ScheduleInput{})
	p, e := c.Schedule(ScheduleInput{Now: 60_000, Marker: marker, Acks: acks})
	if e != nil || !p.Sleeping {
		t.Fatal(p, e)
	}
	for now := int64(65_000); now <= 86_400_000; now += 5_000 {
		p, e = c.Schedule(ScheduleInput{Now: now, Marker: marker, Acks: acks})
		if e != nil || p.Tick || p.After != 0 {
			t.Fatal("R27 recurring idle turn", p, e)
		}
	}
	// A busy identity's scheduler is bounded independently of learner fanout.
	g.call(l, Read{RequestID: "active"})
	g.pump()
	ticks := 0
	for now := int64(86_400_000); now <= 172_800_000; now += 5_000 {
		p, e = c.Schedule(ScheduleInput{Now: now, Busy: true})
		if e != nil {
			t.Fatal(e)
		}
		if p.Tick {
			ticks++
		}
	}
	if ticks != 17_280 {
		t.Fatal("R27 active tick budget", ticks)
	}
	p, e = c.Schedule(ScheduleInput{Now: 500_000_000, Busy: false})
	if e != nil || !p.Tick {
		t.Fatal("R27 resumed scheduler", p, e)
	}
	p, e = c.Schedule(ScheduleInput{Now: 500_000_000, Busy: false})
	if e != nil || p.Tick {
		t.Fatal("R27 replayed tick debt", p, e)
	}
}

// R26's internal idle barrier preserves the sixty-second idle interval while
// still using the real quorum read; a public read begins a new idle interval.
func TestR26IdlePreparationNeedsFreshQuorum(t *testing.T) {
	g := newGroup(t)
	l := g.elect()
	c := g.nodes[l-1]
	c.Schedule(ScheduleInput{Now: 0})
	p, e := c.Schedule(ScheduleInput{Now: 60_000})
	if e != nil || !p.Barrier || p.Prepare {
		t.Fatal(p, e)
	}
	g.call(l, IdleRead{RequestID: "sleep-preparation"})
	g.pump()
	p, e = c.Schedule(ScheduleInput{Now: 60_000})
	if e != nil || p.Barrier || !p.Prepare {
		t.Fatal("R26 idle barrier reset elapsed idle time", p, e)
	}
	g.call(l, Read{RequestID: "public-operation"})
	g.pump()
	p, e = c.Schedule(ScheduleInput{Now: 60_001})
	if e != nil || p.Prepare {
		t.Fatal("R26 public operation retained idle timer", p, e)
	}
}

func TestR27StableHeartbeatHasNoLogWrites(t *testing.T) {
	g := newGroup(t)
	l := g.elect()
	g.tick(2)
	before := g.nodes[l-1].status().Applied
	retained := len(g.durable[l-1].Entries)
	g.trace.Reset()
	g.tick(12)
	if g.nodes[l-1].status().Applied != before || len(g.durable[l-1].Entries) != retained {
		t.Fatal("R27 empty heartbeat wrote log")
	}
	decoder := json.NewDecoder(bytes.NewReader(g.trace.Bytes()))
	messages, writes := 0, 0
	for decoder.More() {
		var e struct {
			Send    []Outbound
			Persist *PersistBatch
		}
		if err := decoder.Decode(&e); err != nil {
			t.Fatal(err)
		}
		messages += len(e.Send)
		if p := e.Persist; p != nil && (len(p.Entries) != 0 || !raft.IsEmptyHardState(p.HardState)) {
			writes++
		}
	}
	if messages != 12*4 || writes != 0 {
		t.Fatal("R27 heartbeat envelope", messages, writes)
	}
	t.Logf("R27 24-hour active voter envelope: ticks=%d messages=%d log_writes=%d", 3*17280, messages/12*17280, writes)
}
