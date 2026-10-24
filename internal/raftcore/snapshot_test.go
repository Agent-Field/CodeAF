package raftcore

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

// R11/R13 require deterministic maintenance at the entry threshold and cache
// compaction only after a host checkpoint of the exact applied configuration.
func TestR11R12R13SnapshotHintAndDurableCompactInput(t *testing.T) {
	conf := &pb.ConfState{Voters: []uint64{1, 2, 4}, VotersOutgoing: []uint64{1, 2, 3}, Learners: []uint64{5}, LearnersNext: []uint64{3}, AutoLeave: proto.Bool(true)}
	entries := make([]*pb.Entry, SnapshotEntries)
	for i := range entries {
		entries[i] = &pb.Entry{Index: proto.Uint64(uint64(i + 1)), Term: proto.Uint64(1), Type: pb.EntryNormal.Enum()}
	}
	c, e := New(Config{ClusterUUID: "snapshot", NodeID: 1, Seed: 1, Protocol: 1}, RecoveredState{HardState: &pb.HardState{Term: proto.Uint64(1), Commit: proto.Uint64(SnapshotEntries)}, ConfState: conf, Entries: entries, Applied: SnapshotEntries})
	if e != nil {
		t.Fatal(e)
	}
	effects, e := c.Step(Poll{})
	if e != nil {
		t.Fatal(e)
	}
	if effects.SnapshotNeeded == nil || effects.SnapshotNeeded.Index != SnapshotEntries || effects.SnapshotNeeded.Term != 1 {
		t.Fatal("R11 entry threshold did not request an applied checkpoint")
	}
	encodedConf, e := raftsnapshot.Configuration(conf)
	if e != nil {
		t.Fatal(e)
	}
	m := raftsnapshot.Manifest{Version: 1, Cluster: "snapshot", Protocol: 1, Index: SnapshotEntries, Term: 1, Conf: encodedConf, RecordsHash: strings.Repeat("0", 64), Digest: raftsnapshot.ID(nil)}
	data, e := m.Encode()
	if e != nil {
		t.Fatal(e)
	}
	p := &pb.Snapshot{Data: data, Metadata: &pb.SnapshotMetadata{Index: proto.Uint64(SnapshotEntries), Term: proto.Uint64(1), ConfState: conf}}
	bad := clone(p)
	bad.Metadata.ConfState.LearnersNext = nil
	if _, e = c.Step(CompactSnapshot{Snapshot: bad}); !errors.Is(e, ErrSnapshot) {
		t.Fatal("R12 discarded learner-next during checkpoint")
	}
	bad = clone(p)
	bad.Data = append(bad.Data, ' ')
	if _, e = c.Step(CompactSnapshot{Snapshot: bad}); !errors.Is(e, ErrSnapshot) {
		t.Fatal("R13 accepted noncanonical snapshot")
	}
	effects, e = c.Step(CompactSnapshot{Snapshot: p})
	if e != nil {
		t.Fatal(e)
	}
	first, e := c.store.FirstIndex()
	if e != nil {
		t.Fatal(e)
	}
	retained, e := c.store.Snapshot()
	if e != nil {
		t.Fatal(e)
	}
	if first != SnapshotEntries+1 || !proto.Equal(retained, p) || effects.SnapshotNeeded != nil {
		t.Fatal("R11 cache kept the covered applied prefix")
	}
	// Effects and completion input cannot mutate the checkpoint retained for a
	// lagging learner; those bytes must remain identical across retries.
	original := bytes.Clone(data)
	p.Data[0] ^= 1
	retained, e = c.store.Snapshot()
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(retained.Data, original) {
		t.Fatal("R13 snapshot retained caller aliases")
	}
}
