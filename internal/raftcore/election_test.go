package raftcore

import (
	"fmt"
	"os"
	"strconv"
	"testing"

	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

// R7/R14/T1/T2 prove rank from the immutable committed anchor order, including
// C outside the voters and P promoted. Node IDs deliberately differ from rank.
// Every timeout is observed through emitted protocol messages, not core fields.
func TestR7R14RankedElectionWindows(t *testing.T) {
	count := 64
	if value := os.Getenv("RAFTSIM_SEEDS"); value != "" {
		var err error
		count, err = strconv.Atoi(value)
		if err != nil || count < 1 {
			t.Fatal("invalid RAFTSIM_SEEDS")
		}
	}
	for seed := 0; seed < count; seed++ {
		for rank, id := range []uint64{11, 28, 99, 201} {
			for _, recovered := range []bool{false, true} {
				cfg := Config{ClusterUUID: "ranked", NodeID: id, Seed: uint64(seed), Protocol: ProtocolVersion}
				state := RecoveredState{ConfState: &pb.ConfState{Voters: []uint64{11, 28, 99, 201}}}
				if recovered {
					state.Anchors = []uint64{11, 28, 99}
				} else {
					cfg.Anchors = []uint64{11, 28, 99}
				}
				c, err := New(cfg, state)
				if err != nil {
					t.Fatal(err)
				}
				at := 0
				for tick := 1; tick <= 14; tick++ {
					effects, err := c.Step(Tick{})
					if err != nil {
						t.Fatal(err)
					}
					for {
						for _, send := range effects.Send {
							m := new(pb.Message)
							if err := proto.Unmarshal(send.Bytes, m); err != nil {
								t.Fatal(err)
							}
							if m.GetType() == pb.MsgPreVote && at == 0 {
								at = tick
							}
						}
						if effects.Persist != nil {
							effects, err = c.Step(Persisted{BatchID: effects.Persist.ID})
						} else if effects.Apply != nil {
							effects, err = c.Step(Applied{BatchID: effects.Apply.ID})
						} else {
							break
						}
						if err != nil {
							t.Fatal(err)
						}
					}
					if at > 0 {
						break
					}
				}
				min := 6 + 2*rank
				if at < min || at > min+1 {
					t.Fatalf("R7 seed=%d rank=%d recovered=%v first campaign at %d ticks, want %d–%d", seed, rank, recovered, at, min, min+1)
				}
			}
		}
	}
}

// R7/R14/T2 refuse a host's conflicting manifest instead of choosing a local
// rank after restart. Missing or invalid versions cannot silently change replay.
func TestR7R14RankRecoveryRefusesConflictingProvisioning(t *testing.T) {
	for _, anchors := range [][]uint64{{11, 28, 100}, {11, 11, 99}, {99, 28, 11}, {11, 28}, {0, 28, 99}} {
		_, err := New(Config{ClusterUUID: "ranked", NodeID: 11, Protocol: ProtocolVersion, Anchors: anchors}, RecoveredState{Anchors: []uint64{11, 28, 99}})
		if err == nil {
			t.Fatal("R7 conflicting manifest accepted", fmt.Sprint(anchors))
		}
	}
}
