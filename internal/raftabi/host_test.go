package raftabi

import (
	"bytes"
	"crypto/sha256"
	"testing"

	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

func TestR13R14HostStreamingHashAndProtoAdmission(t *testing.T) {
	r := New()
	var state []byte
	all := bytes.Repeat([]byte("sealed"), 100000)
	for at := 0; at < len(all); {
		end := min(at+256<<10, len(all))
		w := encoder{}
		w.u32(Version)
		w.u32(8)
		w.blob(state)
		w.blob(all[at:end])
		d := decoder{b: r.Call(w.b)}
		if d.u32() != Version || d.str() != "" {
			t.Fatal("R14 hash ABI refused")
		}
		state = d.blob()
		digest := d.blob()
		expected := sha256.Sum256(all[:end])
		if !bytes.Equal(digest, expected[:]) || d.end() != nil {
			t.Fatal("R13 streamed checksum differs")
		}
		at = end
	}
	for _, kind := range []pb.MessageType{pb.MsgHup, pb.MsgProp, pb.MsgReadIndex, pb.MsgStorageAppend} {
		b, _ := wire.Marshal(&pb.Message{Type: kind.Enum(), From: proto.Uint64(1), To: proto.Uint64(2), Term: proto.Uint64(1)})
		w := encoder{}
		w.u32(Version)
		w.u32(7)
		w.u32(4)
		w.blob(b)
		d := decoder{b: r.Call(w.b)}
		d.u32()
		if d.str() == "" {
			t.Fatalf("R14 untrusted local verb %v admitted", kind)
		}
	}
}
