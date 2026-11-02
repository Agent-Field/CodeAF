package raftsim

import (
	"bytes"
	"fmt"
	"slices"

	"github.com/Agent-Field/codeaf/internal/raftcore"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

func (s *Simulator) checkpoint(node int) {
	n := s.Nodes[node]
	if !n.Alive || n.Written != nil {
		s.fail("checkpoint requires an idle actor")
		return
	}
	g, err := n.Stable.checkpoint()
	if err != nil {
		s.fail("checkpoint: %v", err)
		return
	}
	// Publication is one durable boundary. Chunk/install crash cuts are exercised
	// separately, and the portable decoder validates every complete generation.
	n.Stable.retain(g)
	n.Stable.Snapshot = clonePB(g.Pointer)
	n.Stable.Entries = slices.DeleteFunc(n.Stable.Entries, func(e *pb.Entry) bool { return e.GetIndex() <= n.Stable.Applied })
	s.record(node, "compact", 0, raftMessageDigest(g.Pointer.Data))
	s.step(node, raftcore.CompactSnapshot{Snapshot: g.Pointer})
	s.invariants()
}
func (s *Simulator) snapshotFor(node int, pointer *pb.Snapshot) (generation, bool) {
	for _, g := range s.Nodes[node].Stable.Generations {
		if proto.Equal(g.Pointer, pointer) && g.Complete {
			return cloneGeneration(g), true
		}
	}
	return generation{}, false
}
func (s *Simulator) hasComplete(node int, pointer *pb.Snapshot) bool {
	_, ok := s.snapshotFor(node, pointer)
	return ok
}
func (s *Simulator) snapshotEvent(e event) {
	n := s.Nodes[e.Node]
	if n.Written != nil {
		e.At = s.Time + 1
		s.schedule(e)
		return
	}
	g := e.Snapshot
	if g.Pointer.Metadata.GetIndex() <= n.Stable.Applied {
		return
	}
	index := -1
	for i, old := range n.Stable.Generations {
		if bytes.Equal(old.Pointer.Data, g.Pointer.Data) {
			index = i
			break
		}
	}
	switch e.Kind {
	case "snapshot-begin":
		if index < 0 {
			n.Stable.Generations = append(n.Stable.Generations, generation{Pointer: clonePB(g.Pointer)})
		}
		e.Kind = "snapshot-chunk"
		e.Chunk = 0
	case "snapshot-chunk":
		if index < 0 {
			s.fail("missing snapshot stage")
			return
		}
		stage := &n.Stable.Generations[index]
		if len(stage.Chunks) == e.Chunk {
			stage.Chunks = append(stage.Chunks, bytes.Clone(g.Chunks[e.Chunk]))
		}
		if len(stage.Chunks) <= e.Chunk || !bytes.Equal(stage.Chunks[e.Chunk], g.Chunks[e.Chunk]) {
			s.fail("conflicting snapshot chunk")
			return
		}
		e.Chunk++
		if e.Chunk == len(g.Chunks) {
			e.Kind = "snapshot-complete"
		}
	case "snapshot-complete":
		if index < 0 {
			s.fail("missing snapshot completion")
			return
		}
		stage := n.Stable.Generations[index]
		stage.Complete = true
		if _, err := decodeGeneration(stage); err != nil {
			s.fail("complete snapshot: %v", err)
			return
		}
		n.Stable.retain(stage)
		e.Kind = "input"
	default:
		s.fail("snapshot phase %s", fmt.Sprint(e.Kind))
		return
	}
	e.At = s.Time + 1
	s.schedule(e)
}

func (s *Simulator) reportSnapshot(source origin, target int, success bool) {
	s.schedule(event{At: s.Time + 1, Node: source.Node, Generation: source.Generation, Kind: "input", Input: raftcore.TransportResult{Peer: uint64(target + 1), Snapshot: true, Success: success}})
}
