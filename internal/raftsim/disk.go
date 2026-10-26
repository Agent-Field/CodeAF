package raftsim

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

var wire = proto.MarshalOptions{Deterministic: true}
var errCorrupt = errors.New("raftsim: corrupt complete snapshot")

type generation struct {
	Pointer  *pb.Snapshot
	Chunks   [][]byte
	Complete bool
}
type image struct {
	Hard        *pb.HardState
	Conf        *pb.ConfState
	Entries     []*pb.Entry
	Snapshot    *pb.Snapshot
	Applied     uint64
	App         ProbeApplication
	Generations []generation
}

func freshImage() image { return image{App: freshApp()} }
func clonePB[T proto.Message](p T) T {
	if !p.ProtoReflect().IsValid() {
		return p
	}
	return proto.Clone(p).(T)
}
func (d image) copy() image {
	out := d
	out.Hard = clonePB(d.Hard)
	out.Conf = clonePB(d.Conf)
	out.Snapshot = clonePB(d.Snapshot)
	out.App = d.App.copy()
	out.Entries = nil
	for _, e := range d.Entries {
		out.Entries = append(out.Entries, clonePB(e))
	}
	out.Generations = nil
	for _, g := range d.Generations {
		out.Generations = append(out.Generations, cloneGeneration(g))
	}
	return out
}
func cloneGeneration(g generation) generation {
	out := generation{Pointer: clonePB(g.Pointer), Complete: g.Complete}
	for _, b := range g.Chunks {
		out.Chunks = append(out.Chunks, bytes.Clone(b))
	}
	return out
}
func (d image) recovered() (raftcore.RecoveredState, error) {
	for _, g := range d.Generations {
		if g.Complete {
			if _, e := decodeGeneration(g); e != nil {
				return raftcore.RecoveredState{}, e
			}
		}
	}
	return raftcore.RecoveredState{HardState: clonePB(d.Hard), ConfState: clonePB(d.Conf), Snapshot: clonePB(d.Snapshot), Entries: d.copy().Entries, Applied: d.Applied}, nil
}
func (d *image) persist(p *raftcore.PersistBatch) error {
	if p.Snapshot.GetMetadata().GetIndex() > 0 {
		found := false
		for _, g := range d.Generations {
			if proto.Equal(g.Pointer, p.Snapshot) && g.Complete {
				app, err := decodeGeneration(g)
				if err != nil {
					return err
				}
				d.App = app
				d.Applied = p.Snapshot.Metadata.GetIndex()
				d.Conf = clonePB(p.Snapshot.Metadata.ConfState)
				d.Snapshot = clonePB(p.Snapshot)
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("snapshot persist before complete transfer")
		}
		d.Entries = nil
	}
	if len(p.Entries) > 0 {
		first := p.Entries[0].GetIndex()
		d.Entries = slices.DeleteFunc(d.Entries, func(e *pb.Entry) bool { return e.GetIndex() >= first })
		for _, e := range p.Entries {
			d.Entries = append(d.Entries, clonePB(e))
		}
	}
	if p.HardState != nil {
		d.Hard = clonePB(p.HardState)
	}
	return nil
}
func (d image) term(index uint64) uint64 {
	if d.Snapshot.GetMetadata().GetIndex() == index {
		return d.Snapshot.GetMetadata().GetTerm()
	}
	for _, e := range d.Entries {
		if e.GetIndex() == index {
			return e.GetTerm()
		}
	}
	return 0
}
func rowHash(row raftsnapshot.Row) []byte {
	h := sha256.New()
	for _, b := range [][]byte{[]byte("records"), []byte(row.Namespace), []byte(row.Key), row.Value} {
		var n [8]byte
		binary.BigEndian.PutUint64(n[:], uint64(len(b)))
		h.Write(n[:])
		h.Write(b)
	}
	return h.Sum(nil)
}
func (d image) checkpoint() (generation, error) {
	data, err := json.Marshal(d.App)
	if err != nil {
		return generation{}, err
	}
	row := raftsnapshot.Row{Namespace: "state", Key: "probe", Value: data}
	var stream bytes.Buffer
	if err = raftsnapshot.WriteRow(&stream, row); err != nil {
		return generation{}, err
	}
	payload := stream.Bytes()
	conf, err := raftsnapshot.Configuration(d.Conf)
	if err != nil {
		return generation{}, err
	}
	manifest := raftsnapshot.Manifest{Version: raftsnapshot.Version, Cluster: clusterUUID, Protocol: uint64(raftcore.ProtocolVersion), Index: d.Applied, Term: d.term(d.Applied), Conf: conf, LogicalTime: d.App.Now, RecordCount: 1, RecordsHash: hex.EncodeToString(rowHash(row)), Bytes: uint64(len(payload)), Chunks: uint64((len(payload) + raftsnapshot.ChunkBytes - 1) / raftsnapshot.ChunkBytes), Digest: raftsnapshot.ID(payload)}
	encoded, err := manifest.Encode()
	if err != nil {
		return generation{}, err
	}
	g := generation{Pointer: &pb.Snapshot{Data: encoded, Metadata: &pb.SnapshotMetadata{Index: proto.Uint64(manifest.Index), Term: proto.Uint64(manifest.Term), ConfState: clonePB(d.Conf)}}, Complete: true}
	for len(payload) > 0 {
		n := min(len(payload), raftsnapshot.ChunkBytes)
		g.Chunks = append(g.Chunks, bytes.Clone(payload[:n]))
		payload = payload[n:]
	}
	return g, nil
}
func decodeGeneration(g generation) (ProbeApplication, error) {
	m, err := raftsnapshot.Decode(g.Pointer.GetData())
	if err != nil || !g.Complete || len(g.Chunks) != int(m.Chunks) {
		return ProbeApplication{}, errCorrupt
	}
	conf, confErr := m.Configuration()
	if confErr != nil || m.Cluster != clusterUUID || m.Protocol != uint64(raftcore.ProtocolVersion) || m.Index != g.Pointer.GetMetadata().GetIndex() || m.Term != g.Pointer.GetMetadata().GetTerm() || !proto.Equal(conf, g.Pointer.GetMetadata().GetConfState()) {
		return ProbeApplication{}, errCorrupt
	}
	var payload bytes.Buffer
	for i, b := range g.Chunks {
		if len(b) > raftsnapshot.ChunkBytes || i < len(g.Chunks)-1 && len(b) != raftsnapshot.ChunkBytes {
			return ProbeApplication{}, errCorrupt
		}
		payload.Write(b)
	}
	if uint64(payload.Len()) != m.Bytes || raftsnapshot.ID(payload.Bytes()) != m.Digest {
		return ProbeApplication{}, errCorrupt
	}
	row, err := raftsnapshot.ReadRow(&payload)
	if err != nil || row.Namespace != "state" || row.Key != "probe" || hex.EncodeToString(rowHash(row)) != m.RecordsHash || m.RecordCount != 1 {
		return ProbeApplication{}, errCorrupt
	}
	if _, err = raftsnapshot.ReadRow(&payload); err != io.EOF {
		return ProbeApplication{}, errCorrupt
	}
	var a ProbeApplication
	if json.Unmarshal(row.Value, &a) != nil || a.Now != m.LogicalTime {
		return ProbeApplication{}, errCorrupt
	}
	return a, nil
}
func (d *image) retain(g generation) {
	for i, old := range d.Generations {
		if bytes.Equal(old.Pointer.Data, g.Pointer.Data) {
			d.Generations[i] = cloneGeneration(g)
			return
		}
	}
	d.Generations = append(d.Generations, cloneGeneration(g))
	for len(d.Generations) > 2 {
		remove := 0
		if proto.Equal(d.Generations[0].Pointer, d.Snapshot) {
			remove = 1
		}
		d.Generations = slices.Delete(d.Generations, remove, remove+1)
	}
}
