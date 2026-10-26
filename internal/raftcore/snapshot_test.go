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

// R13/R14 preserve the portable manifest when the library fills an omitted
// default ConfState field. Every effective configuration field remains bound.
func TestR13R14PortableSnapshotDefaultPresenceThroughRealCore(t *testing.T) {
	conf := &pb.ConfState{Voters: []uint64{1, 2, 3}}
	pointer := func(index, term uint64) *pb.Snapshot {
		encoded, e := raftsnapshot.Configuration(conf)
		if e != nil {
			t.Fatal(e)
		}
		m := raftsnapshot.Manifest{Version: 1, Cluster: "snapshot-default", Protocol: 1, Index: index, Term: term, Conf: encoded, RecordsHash: strings.Repeat("0", 64), Digest: raftsnapshot.ID(nil)}
		data, e := m.Encode()
		if e != nil {
			t.Fatal(e)
		}
		return &pb.Snapshot{Data: data, Metadata: &pb.SnapshotMetadata{Index: proto.Uint64(index), Term: proto.Uint64(term), ConfState: clone(conf)}}
	}
	saved := pointer(5, 1)
	c, e := New(Config{ClusterUUID: "snapshot-default", NodeID: 1, Seed: 1, Protocol: 1, Anchors: []uint64{1, 2, 3}}, RecoveredState{HardState: &pb.HardState{Term: proto.Uint64(1), Commit: proto.Uint64(5)}, ConfState: conf, Snapshot: saved, Applied: 5})
	if e != nil {
		t.Fatal(e)
	}
	retained, e := c.store.Snapshot()
	if e != nil || !proto.Equal(retained, saved) {
		t.Fatal("library default changed durable snapshot", retained.GetMetadata(), e)
	}
	incoming := pointer(7, 2)
	raw, e := wire.Marshal(&pb.Message{Type: pb.MsgSnap.Enum(), From: proto.Uint64(2), To: proto.Uint64(1), Term: proto.Uint64(2), Snapshot: incoming})
	if e != nil {
		t.Fatal(e)
	}
	effects, e := c.Step(PeerMessage{Bytes: raw})
	if e != nil || effects.Persist == nil || !proto.Equal(effects.Persist.Snapshot, incoming) {
		t.Fatal("received snapshot lost its manifest-bound presence", effects, e)
	}
	effects, e = c.Step(Persisted{BatchID: effects.Persist.ID})
	if e != nil || effects.Apply == nil || !proto.Equal(effects.Apply.Snapshot, incoming) || !proto.Equal(effects.Apply.ConfState, conf) {
		t.Fatal("apply changed portable snapshot", effects, e)
	}
	if _, e = c.Step(Applied{BatchID: effects.Apply.ID}); e != nil {
		t.Fatal(e)
	}
	bad := clone(incoming)
	bad.Metadata.ConfState.AutoLeave = proto.Bool(true)
	if _, e = canonicalSnapshot(bad); !errors.Is(e, ErrSnapshot) {
		t.Fatal("changed effective joint policy accepted", e)
	}
}
