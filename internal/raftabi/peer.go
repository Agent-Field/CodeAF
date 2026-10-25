package raftabi

import (
	"bytes"
	"encoding/json"
	"strconv"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/clusterwire"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

type peerScopeState struct {
	Identity string `json:"identity"`
	Cluster  string `json:"cluster"`
	Epoch    uint64 `json:"epoch"`
	Protocol uint16 `json:"protocol"`
}
type peerView struct {
	Scope      peerScopeState      `json:"scope"`
	Generation uint64              `json:"generation"`
	Members    []cs.PeerEnrollment `json:"members"`
	Applied    uint64              `json:"applied"`
	Term       uint64              `json:"term"`
	Sleeping   bool                `json:"sleeping"`
}

// The core checks the complete native wire and committed keys before any
// routing side effect. Its result contains only dispatch and sleep hints.
func hostedPeer(d *decoder) ([]byte, error) {
	raw, trusted := d.blob(), d.blob()
	if d.end() != nil {
		return nil, errFrame
	}
	var v peerView
	if cs.Decode(trusted, &v) != nil {
		return nil, errFrame
	}
	var req peerRequest
	code := "ok"
	obsolete := []string{}
	if peerDecode(raw, &req) != nil || req.validate() != nil {
		code = "invalid"
	} else if req.Scope != peerScope(v.Scope) {
		code = "unauthorized"
	} else {
		members := map[uint64]cs.PeerEnrollment{}
		for _, m := range v.Members {
			members[m.Node] = m
		}
		source, ok := members[req.Node]
		if !ok || source.Revoked || source.Incarnation != req.Incarnation || req.Verify(source.Key) != nil {
			code = "unauthorized"
		} else if req.Generation != v.Generation {
			code = "generation"
		} else if !source.Voter && req.Applied > v.Applied {
			code = "invalid"
		} else {
			for _, e := range req.Messages {
				to, ok := members[e.To]
				if e.Scope != req.Scope || e.From != req.Node || e.Incarnation != req.Incarnation || !ok || to.Revoked || to.Incarnation != e.Destination {
					code = "unauthorized"
					break
				}
				if err := e.Verify(source.Key); err != nil {
					if err == peerErrWire {
						code = "invalid"
					} else {
						code = "unauthorized"
					}
					break
				}
				if v.Sleeping && !source.Voter && e.Chunk == nil {
					m := new(pb.Message)
					if proto.Unmarshal(e.Payload, m) == nil && m.GetTerm() <= v.Term && (m.GetType() == pb.MsgAppResp || m.GetType() == pb.MsgHeartbeatResp) {
						obsolete = append(obsolete, strconv.FormatUint(e.Sequence, 10))
					}
				}
			}
		}
	}
	out, err := json.Marshal(struct {
		Code     string   `json:"code"`
		Obsolete []string `json:"obsolete"`
	}{code, obsolete})
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.blob(out)
	return e.b, nil
}

type peerDelivery struct {
	Lane     uint8        `json:"lane"`
	Cursor   uint64       `json:"cursor,string"`
	Envelope peerEnvelope `json:"envelope"`
}
type peerCredit struct {
	Node     uint64 `json:"node,string"`
	Sequence uint64 `json:"sequence,string"`
}
type peerResponse struct {
	Chunks     []peerCredit   `json:"chunks,omitempty"`
	Accepted   []string       `json:"accepted,omitempty"`
	Rejected   []string       `json:"rejected,omitempty"`
	Generation uint64         `json:"generation,string"`
	Messages   []peerDelivery `json:"messages"`
}

func hostedPeerWire(d *decoder) ([]byte, error) {
	action := d.u32()
	e := encoder{}
	if action == 2 {
		code := cs.Code(d.str())
		if d.end() != nil {
			return nil, errFrame
		}
		e.str(cs.EnrollmentMessage(code))
		return e.b, nil
	}
	if action == 0 {
		if d.end() != nil {
			return nil, errFrame
		}
		raw, err := json.Marshal(struct {
			Batch, Chunk, Exchange, Messages, Inflight, Retention, ForwardTimeout int
			Slots                                                                 [3]int
			Budget                                                                [3]int
		}{peerMaxBatchBytes, peerMaxChunkBytes, peerMaxExchangeBytes, peerMaxMessages, peerMaxInflight, int(cs.PeerRetentionMillis), int(clusterwire.ForwardTimeout.Milliseconds()), [3]int{peerMaxInflight / 2, peerMaxInflight / 4, peerMaxInflight / 4}, [3]int{3 * peerMaxQueuedBytes / 8, peerMaxQueuedBytes / 8, peerMaxQueuedBytes / 2}})
		if err != nil {
			return nil, err
		}
		e.blob(raw)
		return e.b, nil
	}
	raw := d.blob()
	if d.end() != nil || action != 1 {
		return nil, errFrame
	}
	var out peerResponse
	j := json.NewDecoder(bytes.NewReader(raw))
	j.DisallowUnknownFields()
	if j.Decode(&out) != nil || out.Generation == 0 || len(out.Messages) > peerMaxMessages {
		return nil, errFrame
	}
	if out.Messages == nil {
		out.Messages = []peerDelivery{}
	}
	b, err := json.Marshal(out)
	if err != nil || len(b) > peerMaxExchangeBytes {
		return nil, errFrame
	}
	e.blob(b)
	return e.b, nil
}
