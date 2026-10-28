package raftsim

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/raftsim/model"
)

// R9/R11/R12/R13 keep three voters while 32 learners join and depart. Sleeping
// learners are real cores with durable images, not a second consensus model.
// A lagging learner installs a post-admission snapshot instead of pinning logs.
func TestR9R11R12R13ThirtyTwoDeadLearnersDoNotPinCommitOrCompaction(t *testing.T) {
	s := group(t, Config{Seed: 121211, Nodes: 35, Duplicate: 50, Delay: 2})
	leader := s.Leader()
	for node := 3; node < 35; node++ {
		s.Do(Action{Kind: "crash", Node: node, Value: 1})
	}
	// One bounded catch-up batch exercises the full 32-member fanout; signed
	// policy tests independently prove sequential admission and its live cap.
	s.Do(Action{Kind: "configure", Node: leader, Count: 5})
	s.Drain()
	if s.Err != nil {
		t.Fatal(s.Err)
	}
	conf := s.Nodes[leader].Stable.Conf
	if len(conf.GetVoters()) != 3 || len(conf.GetLearners()) != 32 {
		t.Fatal("R9 learner admission changed voting topology", conf)
	}
	create(t, s, "a")
	s.Do(Action{Kind: "snapshot", Node: leader})
	s.Drain()
	before := s.Nodes[leader].Stable.Applied
	for n := 0; n < 2; n++ {
		submit(t, s, leader, model.Call{Kind: "advance_time", Actor: "owner", At: int64(n + 1000)})
	}
	if s.Nodes[leader].Stable.Applied <= before {
		t.Fatal("R11 dead learners blocked commitment")
	}
	s.Do(Action{Kind: "snapshot", Node: leader})
	s.Drain()
	if len(s.Nodes[leader].Stable.Entries) != 0 {
		t.Fatal("R11 dead learners pinned retained log")
	}
	s.Do(Action{Kind: "restart", Node: 3})
	for n := 0; n < 16 && s.Nodes[3].Stable.Applied < s.Nodes[leader].Stable.Applied; n++ {
		s.Ticks(1)
	}
	if s.Nodes[3].Stable.Applied != s.Nodes[leader].Stable.Applied || len(s.Nodes[3].Stable.Generations) == 0 {
		t.Fatalf("R13 learner failed snapshot catch-up: %v, events=%d", s.Err, s.Events)
	}
	s.Do(Action{Kind: "configure", Node: leader, Other: 3, Count: 4})
	s.Drain()
	for _, id := range s.Nodes[leader].Stable.Conf.GetLearners() {
		if id == 4 {
			t.Fatal("R12 removal did not apply")
		}
	}
	s.Do(Action{Kind: "configure", Node: leader, Count: 6})
	s.Drain()
	if len(s.Nodes[leader].Stable.Conf.GetVoters()) != 3 || len(s.Nodes[leader].Stable.Conf.GetLearners()) != 0 {
		t.Fatal("R11 churn did not retire all learners")
	}
	t.Logf("R11 full fanout/churn used %d/%d events", s.Events, MaxEvents)
	finish(t, s)
}

// R9 checks every subset of sleeping paired laptops without changing quorum.
func TestR9EverySleepingSubsetOfTwoLearnerLaptops(t *testing.T) {
	for mask := 0; mask < 4; mask++ {
		s := group(t, Config{Seed: uint64(1290 + mask), Nodes: 5})
		leader := s.Leader()
		s.Do(Action{Kind: "configure", Node: leader, Count: 5})
		s.Drain()
		for node := 3; node < 5; node++ {
			if mask&(1<<(node-3)) != 0 {
				s.Do(Action{Kind: "crash", Node: node, Value: 1})
			}
		}
		create(t, s, "a")
		if len(s.Nodes[leader].Stable.Conf.GetVoters()) != 3 {
			t.Fatal("R9 pairing requires a laptop for quorum")
		}
		finish(t, s)
	}
}
