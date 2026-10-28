package raftsnapshot

import (
	"bytes"
	"encoding/hex"
	"io"
	"strings"
	"testing"

	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

// R13/R14 require a runtime-independent framing contract, including integers
// beyond JavaScript's exact-number range and opaque bytes across chunk cuts.
func TestR13R14PortableManifestAndRowFraming(t *testing.T) {
	conf := &pb.ConfState{Voters: []uint64{1, 2, 9007199254740993}, VotersOutgoing: []uint64{1, 2, 3}, Learners: []uint64{5}, LearnersNext: []uint64{3}, AutoLeave: proto.Bool(true)}
	c, e := Configuration(conf)
	if e != nil {
		t.Fatal(e)
	}
	m := Manifest{Version: 1, Cluster: "portable", Protocol: 1, Index: ^uint64(0), Term: 9007199254740993, Conf: c, VisibleVersion: 9007199254740994, LogicalTime: -1, RecordsHash: strings.Repeat("0", 64), Digest: ID(nil)}
	b, e := m.Encode()
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Contains(b, []byte(`"index":"18446744073709551615"`)) || !bytes.Contains(b, []byte(`"term":"9007199254740993"`)) {
		t.Fatal("R14 uint64 lost precision")
	}
	decoded, e := Decode(b)
	if e != nil {
		t.Fatal(e)
	}
	got, e := decoded.Configuration()
	if e != nil || !proto.Equal(got, conf) {
		t.Fatal("R12 joint configuration changed")
	}
	for _, bad := range [][]byte{append(append([]byte{}, b...), 10), bytes.Replace(b, []byte(`"format":"1"`), []byte(`"format":1`), 1), bytes.Replace(b, []byte(`"index":"18446744073709551615"`), []byte(`"index":"018446744073709551615"`), 1), bytes.Replace(b, []byte(`"cluster":"portable"`), []byte(`"cluster":"portable","unknown":1`), 1)} {
		if _, e := Decode(bad); e == nil {
			t.Fatal("R14 noncanonical manifest accepted")
		}
	}
	var stream bytes.Buffer
	row := Row{"state", "k", []byte{0, 255}}
	if e = WriteRow(&stream, row); e != nil {
		t.Fatal(e)
	}
	// This vector is specified independently from WriteRow's implementation.
	if hex.EncodeToString(stream.Bytes()) != "00000005000000010000000273746174656b00ff" {
		t.Fatal("R14 framing differs from the portable vector")
	}
	round, e := ReadRow(bytes.NewReader(stream.Bytes()))
	if e != nil || round.Namespace != row.Namespace || round.Key != row.Key || !bytes.Equal(round.Value, row.Value) {
		t.Fatal("R13 row changed")
	}
	for n := 1; n < len(stream.Bytes()); n++ {
		if _, e = ReadRow(bytes.NewReader(stream.Bytes()[:n])); e == nil {
			t.Fatal("R13 truncated record accepted")
		}
	}
	if _, e = ReadRow(bytes.NewReader(nil)); e != io.EOF {
		t.Fatal("R13 empty stream is not EOF")
	}
	for _, bad := range []Row{{"other", "k", nil}, {"state", "", nil}, {"state", "k", make([]byte, RecordBytes+1)}} {
		if WriteRow(io.Discard, bad) == nil {
			t.Fatal("R13 unsupported row accepted")
		}
	}
}
