package raftcore

import (
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

func TestIdleReadAfterCommittedMarkerPreservesQuiescence(t *testing.T) {
	for _, public := range []bool{false, true} {
		name := "maintenance"
		if public {
			name = "public"
		}
		t.Run(name, func(t *testing.T) {
			g := newGroup(t)
			l := g.elect()
			c := g.nodes[l-1]
			s := c.status()
			marker := cs.Quiescence{Index: s.Applied, Term: s.Term, Barrier: s.Applied - 1, Voters: []uint64{1, 2, 3}}
			acks := []SleepAck{{1, s.Applied, s.Term}, {2, s.Applied, s.Term}, {3, s.Applied, s.Term}}
			if _, err := c.Schedule(ScheduleInput{Now: 0}); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Schedule(ScheduleInput{Now: 60000, Marker: marker, Acks: acks[:2]}); err != nil {
				t.Fatal(err)
			}
			if public {
				g.call(l, Read{RequestID: "public"})
			} else {
				g.call(l, IdleRead{RequestID: "maintenance"})
			}
			g.pump()
			plan, err := c.Schedule(ScheduleInput{Now: 60000, Marker: marker, Acks: acks})
			if err != nil {
				t.Fatal(err)
			}
			if plan.Sleeping == public {
				t.Fatalf("completed %s read: sleeping=%t, want %t", name, plan.Sleeping, !public)
			}
		})
	}
}

func TestIdleReadWakesSleepingQuiescence(t *testing.T) {
	g := newGroup(t)
	l := g.elect()
	c := g.nodes[l-1]
	s := c.status()
	marker := cs.Quiescence{Index: s.Applied, Term: s.Term, Barrier: s.Applied - 1, Voters: []uint64{1, 2, 3}}
	acks := []SleepAck{{1, s.Applied, s.Term}, {2, s.Applied, s.Term}, {3, s.Applied, s.Term}}
	if _, err := c.Schedule(ScheduleInput{Now: 0}); err != nil {
		t.Fatal(err)
	}
	plan, err := c.Schedule(ScheduleInput{Now: 60000, Marker: marker, Acks: acks})
	if err != nil || !plan.Sleeping {
		t.Fatal(plan, err)
	}
	g.call(l, IdleRead{RequestID: "wake-internal"})
	g.pump()
	plan, err = c.Schedule(ScheduleInput{Now: 60001, Marker: marker, Acks: acks})
	if err != nil || plan.Sleeping || len(plan.Wake) != 3 {
		t.Fatal("internal read reused sleeping certificate", plan, err)
	}
}

func TestUnconfirmedIdleReadCannotReuseQuiescence(t *testing.T) {
	g := newGroup(t)
	l := g.elect()
	c := g.nodes[l-1]
	s := c.status()
	marker := cs.Quiescence{Index: s.Applied, Term: s.Term, Barrier: s.Applied - 1, Voters: []uint64{1, 2, 3}}
	acks := []SleepAck{{1, s.Applied, s.Term}, {2, s.Applied, s.Term}, {3, s.Applied, s.Term}}
	if _, err := c.Schedule(ScheduleInput{Now: 0}); err != nil {
		t.Fatal(err)
	}
	g.drop = func(from, to uint64) bool { return from == l || to == l }
	g.call(l, IdleRead{RequestID: "no-quorum"})
	g.pump()
	for _, read := range g.reads {
		if read.RequestID == "no-quorum" {
			t.Fatal("internal read completed without quorum")
		}
	}
	plan, err := c.Schedule(ScheduleInput{Now: 60000, Marker: marker, Acks: acks})
	if err != nil || plan.Sleeping {
		t.Fatal("unconfirmed read reused sleeping certificate", plan, err)
	}
}
