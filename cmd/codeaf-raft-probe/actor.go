package main

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftabi"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	"github.com/Agent-Field/codeaf/internal/raftsnapshot"
	"github.com/Agent-Field/codeaf/internal/raftstore"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

type actorRequest struct {
	Header     []byte                     `json:"header"`
	Rows       []clusterstate.RecordWrite `json:"rows"`
	Op         string                     `json:"op"`
	Cluster    string                     `json:"cluster"`
	ID         uint64                     `json:"id,string"`
	Tag        uint32                     `json:"tag"`
	Value      json.RawMessage            `json:"value"`
	Data       []byte                     `json:"data"`
	Generation string                     `json:"generation"`
	Offset     uint64                     `json:"offset"`
	Hash       string                     `json:"hash"`
}
type probeActor struct {
	core  *raftcore.Core
	store *raftstore.SQLite
	path  string
	cfg   raftcore.Config
}

var actorWire = proto.MarshalOptions{Deterministic: true}

func (a *probeActor) input(in raftcore.Input) (any, error) {
	ctx := context.Background()
	e, err := a.core.Step(in)
	if err != nil {
		return nil, err
	}
	type send struct {
		To    string `json:"to"`
		Bytes []byte `json:"bytes"`
	}
	type barrier struct {
		ID    string `json:"id"`
		Index string `json:"index"`
	}
	var sends = []send{}
	var reads = []barrier{}
	for count := 0; count < 256; count++ {
		for _, m := range e.Send {
			sends = append(sends, send{strconv.FormatUint(m.To, 10), m.Bytes})
		}
		for _, r := range e.Reads {
			reads = append(reads, barrier{r.RequestID, strconv.FormatUint(r.Index, 10)})
		}
		if e.Persist != nil {
			if err = a.store.Persist(ctx, *e.Persist); err != nil {
				return nil, err
			}
			e, err = a.core.Step(raftcore.Persisted{BatchID: e.Persist.ID})
		} else if e.Apply != nil {
			recovered, err := a.store.Recover(ctx)
			if err != nil {
				return nil, err
			}
			rows, app, err := a.store.Read(ctx, recovered.Application.Applied, []raftstore.Key{{Namespace: raftstore.State, Key: "cell"}})
			if err != nil {
				return nil, err
			}
			update := raftstore.Update{ExpectedApplied: app.Applied, VisibleVersion: app.VisibleVersion, LogicalTime: app.LogicalTime}
			row := rows[raftstore.Key{Namespace: raftstore.State, Key: "cell"}]
			for _, entry := range e.Apply.Entries {
				if entry.GetType() == pb.EntryNormal && len(entry.Data) > 0 {
					row, err = raftabi.ProbeApply(entry.Data, row)
					if err != nil {
						return nil, err
					}
					update.VisibleVersion++
					update.Writes = []raftstore.Write{{Key: raftstore.Key{Namespace: raftstore.State, Key: "cell"}, Value: row}}
				}
			}
			if err = a.store.Apply(ctx, *e.Apply, update); err != nil {
				return nil, err
			}
			e, err = a.core.Step(raftcore.Applied{BatchID: e.Apply.ID})
		} else {
			s := e.Status
			return map[string]any{"send": sends, "reads": reads, "status": map[string]any{"id": fmt.Sprint(s.NodeID), "leader": fmt.Sprint(s.Leader), "term": fmt.Sprint(s.Term), "commit": fmt.Sprint(s.Commit), "applied": fmt.Sprint(s.Applied), "role": uint32(s.Role)}}, nil
		}
		if err != nil {
			return nil, err
		}
	}
	return nil, fmt.Errorf("probe actor completion budget")
}
func (a *probeActor) open() error {
	var err error
	a.store, err = raftstore.Open(a.path, raftstore.Identity{ClusterUUID: a.cfg.ClusterUUID, NodeID: a.cfg.NodeID, Protocol: 1})
	if err != nil {
		return err
	}
	recovered, err := a.store.Recover(context.Background())
	if err != nil {
		return err
	}
	cfg := a.cfg
	if !recovered.Fresh {
		cfg.Bootstrap = nil
	}
	a.core, err = raftcore.New(cfg, recovered.Core)
	return err
}
func (a *probeActor) call(r actorRequest, dir string) (any, error) {
	ctx := context.Background()
	switch r.Op {
	case "leadership-plan", "leadership-receipt":
		var h clusterstate.Membership
		if err := clusterstate.Decode(r.Header, &h); err != nil {
			return nil, err
		}
		var value any
		if r.Op == "leadership-receipt" {
			v, err := a.core.LeadershipReceipt(h)
			if err != nil {
				return nil, err
			}
			value = v
		} else {
			var evidence raftcore.LeadershipEvidence
			if err := clusterstate.Decode(r.Value, &evidence); err != nil {
				return nil, err
			}
			v, err := a.core.PlanLeadership(h, evidence)
			if err != nil {
				return nil, err
			}
			value = v
		}
		b, err := clusterstate.Encode(value)
		return json.RawMessage(b), err

	case "legacy-import":
		var m clusterstate.LegacyManifest
		if err := clusterstate.Decode(r.Value, &m); err != nil {
			return nil, err
		}
		if a.store == nil {
			a.cfg = raftcore.Config{ClusterUUID: r.Cluster, NodeID: r.ID, Seed: 100 + r.ID, Protocol: 1}
			a.path = filepath.Join(dir, fmt.Sprintf("%s-%d.db", r.Cluster, r.ID))
			s, err := raftstore.Open(a.path, raftstore.Identity{ClusterUUID: r.Cluster, NodeID: r.ID, Protocol: 1})
			if err != nil {
				return nil, err
			}
			a.store = s
		}
		if err := a.store.ImportLegacy(ctx, m, func(visit func(clusterstate.RecordWrite) error) error {
			for _, row := range r.Rows {
				if e := visit(row); e != nil {
					return e
				}
			}
			return nil
		}); err != nil {
			return nil, err
		}
		recovered, err := a.store.Recover(ctx)
		if err != nil {
			return nil, err
		}
		snapshot, err := actorWire.Marshal(recovered.Core.Snapshot)
		if err != nil {
			return nil, err
		}
		return map[string]any{"snapshot": snapshot, "applied": fmt.Sprint(recovered.Application.Applied)}, nil
	case "init":
		if a.store != nil {
			if err := a.store.Close(); err != nil {
				return nil, err
			}
		}
		a.cfg = raftcore.Config{ClusterUUID: r.Cluster, NodeID: r.ID, Seed: 100 + r.ID, Protocol: 1, Anchors: []uint64{1, 2, 3}, Bootstrap: []uint64{1, 2, 3}}
		a.path = filepath.Join(dir, fmt.Sprintf("%s-%d.db", r.Cluster, r.ID))
		if err := a.open(); err != nil {
			return nil, err
		}
		return a.input(raftcore.Poll{})
	case "restart":
		if err := a.store.Close(); err != nil {
			return nil, err
		}
		if err := a.open(); err != nil {
			return nil, err
		}
		return a.input(raftcore.Poll{})
	case "input":
		var in raftcore.Input
		var b []byte
		var s string
		switch r.Tag {
		case 0:
			in = raftcore.Poll{}
		case 1:
			in = raftcore.Tick{}
		case 2:
			json.Unmarshal(r.Value, &b)
			in = raftcore.PeerMessage{Bytes: b}
		case 3:
			json.Unmarshal(r.Value, &b)
			in = raftcore.Propose{Command: b}
		case 4:
			json.Unmarshal(r.Value, &s)
			in = raftcore.Read{RequestID: s}
		case 7:
			json.Unmarshal(r.Value, &s)
			id, err := strconv.ParseUint(s, 10, 64)
			if err != nil {
				return nil, err
			}
			in = raftcore.Transfer{Target: id}
		case 8:
			json.Unmarshal(r.Value, &b)
			in = raftcore.Configure{Change: b}
		case 12:
			var v struct {
				Peer     string `json:"peer"`
				Snapshot bool   `json:"snapshot"`
				Success  bool   `json:"success"`
			}
			if err := json.Unmarshal(r.Value, &v); err != nil {
				return nil, err
			}
			id, err := strconv.ParseUint(v.Peer, 10, 64)
			if err != nil {
				return nil, err
			}
			in = raftcore.TransportResult{Peer: id, Snapshot: v.Snapshot, Success: v.Success}
		default:
			return nil, fmt.Errorf("probe tag")
		}
		return a.input(in)
	case "row":
		recovered, err := a.store.Recover(ctx)
		if err != nil {
			return nil, err
		}
		rows, _, err := a.store.Read(ctx, recovered.Application.Applied, []raftstore.Key{{Namespace: raftstore.State, Key: "cell"}})
		return rows[raftstore.Key{Namespace: raftstore.State, Key: "cell"}], err
	case "checkpoint":
		p, err := a.store.CreateSnapshot(ctx)
		if err != nil {
			return nil, err
		}
		if err = a.store.Compact(ctx, p.GetMetadata().GetIndex()); err != nil {
			return nil, err
		}
		if _, err = a.core.Step(raftcore.CompactSnapshot{Snapshot: p}); err != nil {
			return nil, err
		}
		return actorWire.Marshal(p)
	case "begin":
		return a.store.BeginSnapshot(ctx, r.Data)
	case "chunk":
		return nil, a.store.PutSnapshotChunk(ctx, r.Generation, r.Offset, r.Data, r.Hash)
	case "complete":
		p, err := a.store.CompleteSnapshot(ctx, r.Generation)
		if err != nil {
			return nil, err
		}
		return actorWire.Marshal(p)
	case "read-chunk":
		return a.store.ReadSnapshotChunk(ctx, r.Generation, r.Offset)
	case "describe":
		p := new(pb.Snapshot)
		if err := proto.Unmarshal(r.Data, p); err != nil {
			return nil, err
		}
		m, err := raftsnapshot.Decode(p.Data)
		return m, err
	default:
		return nil, fmt.Errorf("probe operation")
	}
}

// actorProof uses actual native SQLite stores beside hosted actors. The only
// application is the explicit development lease probe, never production policy.
func actorProof(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	actors := map[uint64]*probeActor{}
	defer func() {
		for _, a := range actors {
			if a.store != nil {
				a.store.Close()
			}
		}
	}()
	for {
		var length uint32
		if err := binary.Read(os.Stdin, binary.LittleEndian, &length); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if length == 0 || length > raftabi.MaxBuffer {
			return fmt.Errorf("probe frame capacity")
		}
		b := make([]byte, length)
		if _, err := io.ReadFull(os.Stdin, b); err != nil {
			return err
		}
		var request actorRequest
		var out any
		if err := json.Unmarshal(b, &request); err != nil {
			out = map[string]string{"error": err.Error()}
		} else {
			a := actors[request.ID]
			if a == nil {
				a = &probeActor{}
				actors[request.ID] = a
			}
			result, err := a.call(request, dir)
			if err != nil {
				out = map[string]string{"error": err.Error()}
			} else {
				out = result
			}
		}
		b, err := json.Marshal(out)
		if err != nil {
			return err
		}
		if err = binary.Write(os.Stdout, binary.LittleEndian, uint32(len(b))); err != nil {
			return err
		}
		if _, err = os.Stdout.Write(b); err != nil {
			return err
		}
	}
}
