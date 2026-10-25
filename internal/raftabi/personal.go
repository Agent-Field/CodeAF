package raftabi

import (
	"encoding/json"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
	pb "go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

type personalInput struct {
	Action       string             `json:"action"`
	Cluster      string             `json:"cluster"`
	Identity     string             `json:"identity"`
	Epoch        uint64             `json:"epoch"`
	Generation   uint64             `json:"generation"`
	Applied      uint64             `json:"applied"`
	Now          int64              `json:"now"`
	CommittedNow int64              `json:"committed_now"`
	Leader       uint64             `json:"leader"`
	Actor        string             `json:"actor"`
	Certificate  string             `json:"certificate"`
	Proof        cs.V1Request       `json:"proof"`
	Conf         []byte             `json:"conf"`
	Progress     []personalProgress `json:"progress"`
	Rows         []personalRow      `json:"rows"`
}
type personalProgress struct {
	Node    uint64 `json:"node"`
	Applied uint64 `json:"applied"`
}
type personalRow struct {
	Namespace string `json:"namespace"`
	Key       string `json:"key"`
	Data      []byte `json:"data"`
}
type personalOutput struct {
	Need    []cs.RecordKey   `json:"need,omitempty"`
	Code    cs.Code          `json:"code"`
	Message string           `json:"message,omitempty"`
	Plan    *cs.PersonalPlan `json:"plan,omitempty"`
	Wire    []byte           `json:"wire,omitempty"`
}

// The host supplies immutable rows at a fresh quorum barrier. Missing reads
// request bounded dependencies instead of embedding a second policy in JavaScript.
func hostedPersonal(d *decoder) ([]byte, error) {
	raw := d.blob()
	if d.end() != nil {
		return nil, errFrame
	}
	var in personalInput
	if cs.Decode(raw, &in) != nil {
		return nil, errFrame
	}
	rows := map[cs.RecordKey][]byte{}
	size := 0
	for _, row := range in.Rows {
		k := cs.RecordKey{Namespace: row.Namespace, Key: row.Key}
		if _, exists := rows[k]; exists {
			return nil, errFrame
		}
		rows[k] = row.Data
		size += len(row.Data) + len(row.Key) + len(row.Namespace)
	}
	if size > cs.MaxEvaluationBytes {
		return nil, errFrame
	}
	// Discover a complete bounded page before evaluation so each retained peer
	// does not require re-decoding every earlier record in another host round.
	need := []cs.RecordKey{}
	want := func(k cs.RecordKey) {
		if _, ok := rows[k]; !ok {
			for _, old := range need {
				if old == k {
					return
				}
			}
			need = append(need, k)
		}
	}
	want(cs.IdentityKey())
	want(cs.MembershipKey())
	actor := in.Actor
	if in.Action != "status" {
		var identity cs.IdentityRec
		if cs.Decode(rows[cs.IdentityKey()], &identity) == nil {
			request, _, who, e := cs.VerifyPersonalAdmission(in.Proof, identity, in.Now)
			if e == nil {
				actor = who
				want(cs.MemberStoreKey(who, request.Store))
			}
		}
	}
	if actor != "" {
		want(cs.DeviceKey(actor))
		want(cs.AuthorityKey(actor))
		want(cs.MemberDeviceKey(actor))
		var mapping cs.DeviceMember
		if cs.Decode(rows[cs.MemberDeviceKey(actor)], &mapping) == nil && mapping.Node != 0 {
			want(cs.MemberKey(mapping.Node))
		}
		if in.Action != "status" {
			var identity cs.IdentityRec
			if cs.Decode(rows[cs.IdentityKey()], &identity) == nil {
				request, _, _, e := cs.VerifyPersonalAdmission(in.Proof, identity, in.Now)
				if e == nil && cs.Decode(rows[cs.MemberStoreKey(actor, request.Store)], &mapping) == nil && mapping.Node != 0 {
					want(cs.MemberKey(mapping.Node))
				}
			}
		}
	}
	if in.Action == "status" || in.Action == "grant" {
		var h cs.Membership
		if cs.Decode(rows[cs.MembershipKey()], &h) == nil {
			for _, node := range h.Nodes {
				want(cs.MemberKey(node))
				var m cs.MemberRecord
				if cs.Decode(rows[cs.MemberKey(node)], &m) == nil && m.Device != "" {
					want(cs.DeviceKey(m.Device))
					if m.Worker {
						want(cs.AuthorityKey(m.Device))
						want(cs.WorkerKey(m.Device))
					}
				}
			}
		}
	}
	if len(need) > 0 {
		out, _ := cs.Encode(personalOutput{Code: cs.OK, Need: need})
		e := encoder{}
		e.blob(out)
		return e.b, nil
	}
	missing := []cs.RecordKey{}
	read := func(k cs.RecordKey) ([]byte, error) {
		v, ok := rows[k]
		if !ok {
			missing = append(missing, k)
			return nil, cs.ErrDependency
		}
		return v, nil
	}
	conf := new(pb.ConfState)
	if proto.Unmarshal(in.Conf, conf) != nil {
		return nil, errFrame
	}
	out := personalOutput{Code: cs.OK}
	var err error
	switch in.Action {
	case "admission", "grant":
		var plan cs.PersonalPlan
		plan, err = cs.PlanPersonalAdmission(in.Cluster, in.Proof, in.Now, read)
		out.Code = plan.Code
		out.Plan = &plan
		if err == nil && plan.Code == cs.OK && in.Action == "grant" && plan.Node != 0 {
			var m cs.MemberRecord
			v, e := read(cs.MemberKey(plan.Node))
			if e != nil {
				err = e
			} else if cs.Decode(v, &m) != nil {
				err = cs.ErrDependency
			} else {
				_, peers, e := cs.MembershipViewAt(read, raftcore.Configuration(conf), in.Applied, in.CommittedNow)
				err = e
				if err == nil {
					out.Wire, err = json.Marshal(struct {
						Scope       peerScope            `json:"scope"`
						Generation  uint64               `json:"generation,string"`
						Node        uint64               `json:"node,string"`
						Incarnation uint64               `json:"incarnation,string"`
						Barrier     uint64               `json:"barrier,string"`
						Members     []personalWireMember `json:"members"`
					}{peerScope{in.Identity, in.Cluster, in.Epoch, raftcore.ProtocolVersion}, in.Generation, m.Node, m.Incarnation, m.ReadyIndex, personalMembers(peers)})
				}
			}
		}
	case "status":
		progress := map[uint64]uint64{}
		for _, p := range in.Progress {
			progress[p.Node] = p.Applied
		}
		var status cs.PersonalControl
		status, out.Code, err = cs.PersonalControlView(in.Cluster, in.Actor, in.Certificate, raftcore.Configuration(conf), in.Applied, in.Leader, in.CommittedNow, progress, read)
		if err == nil && out.Code == cs.OK {
			out.Wire, err = cs.Encode(status)
		}
	default:
		return nil, errFrame
	}
	if len(missing) > 0 {
		out = personalOutput{Need: missing, Code: cs.OK}
		err = nil
	}
	if err != nil {
		return nil, err
	}
	out.Message = cs.EnrollmentMessage(out.Code)
	if out.Code == cs.Unauthorized || out.Code == cs.Revoked {
		out.Message = "this computer is not allowed to join this cluster"
	}
	b, err := cs.Encode(out)
	if err != nil {
		return nil, err
	}
	e := encoder{}
	e.blob(b)
	return e.b, nil
}

type personalWireMember struct {
	Node        uint64
	Incarnation uint64
	Key         []byte
	Voter       bool
	Revoked     bool
}

func personalMembers(peers []cs.PeerEnrollment) []personalWireMember {
	out := make([]personalWireMember, 0, len(peers))
	for _, p := range peers {
		out = append(out, personalWireMember{p.Node, p.Incarnation, p.Key, p.Voter, p.Revoked})
	}
	return out
}
