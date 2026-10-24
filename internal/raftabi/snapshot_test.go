package raftabi

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

// R13/R14 require host checkpoint inputs and maintenance counters to preserve
// the same core semantics across the binary native/WASI boundary.
func TestR13R14SnapshotMaintenanceABI(t *testing.T) {
	r := New()
	id := create(t, r)
	conf := &pb.ConfState{Voters: []uint64{1}}
	entries := make([]*pb.Entry, raftcore.SnapshotEntries)
	for i := range entries {
		entries[i] = &pb.Entry{Index: proto.Uint64(uint64(i + 1)), Term: proto.Uint64(1), Type: pb.EntryNormal.Enum()}
	}
	c, e := raftcore.New(raftcore.Config{ClusterUUID: "R14", NodeID: 1, Protocol: 1, Seed: 42}, raftcore.RecoveredState{HardState: &pb.HardState{Term: proto.Uint64(1), Commit: proto.Uint64(raftcore.SnapshotEntries)}, ConfState: conf, Entries: entries, Applied: raftcore.SnapshotEntries})
	if e != nil {
		t.Fatal(e)
	}
	r.nodes[id] = c
	query := request(6)
	query.u32(id)
	d := answer(t, r.Call(query.b))
	if d.u32() != 1 || d.u64() != raftcore.SnapshotEntries || d.u64() != 1 || d.end() != nil {
		t.Fatal("R14 maintenance counters changed")
	}
	config, e := raftsnapshot.Configuration(conf)
	if e != nil {
		t.Fatal(e)
	}
	manifest := raftsnapshot.Manifest{Version: 1, Cluster: "R14", Protocol: 1, Index: raftcore.SnapshotEntries, Term: 1, Conf: config, RecordsHash: strings.Repeat("0", 64), Digest: raftsnapshot.ID(nil)}
	data, e := manifest.Encode()
	if e != nil {
		t.Fatal(e)
	}
	p := &pb.Snapshot{Data: data, Metadata: &pb.SnapshotMetadata{Index: proto.Uint64(raftcore.SnapshotEntries), Term: proto.Uint64(1), ConfState: conf}}
	frame := request(3)
	frame.u32(id)
	frame.u32(11)
	frame.message(p)
	// Validate framing before stepping the core, including checkpoint inputs.
	bad := append(append([]byte{}, frame.b...), 1)
	invalid := &decoder{b: r.Call(bad)}
	invalid.u32()
	if invalid.str() == "" {
		t.Fatal("R14 malformed checkpoint frame accepted")
	}
	answer(t, r.Call(frame.b))
	d = answer(t, r.Call(query.b))
	if d.u32() != 0 || d.end() != nil {
		t.Fatal("R13 checkpoint did not compact ABI core")
	}
}
