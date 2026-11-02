package raftabi

import (
	"crypto/ed25519"
	"encoding/json"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/clusterwire"
	"github.com/Agent-Field/codeaf/internal/raftcore"
)

// Application signatures use the same native encoding, including omitted
// fields and numeric routing hints, without importing a network client.
func hostedForward(d *decoder) ([]byte, error) {
	op, raw, key := d.u32(), d.blob(), d.blob()
	if d.end() != nil || len(raw) > peerMaxExchangeBytes {
		return nil, errFrame
	}
	var work clusterwire.Forward
	if op == 0 {
		var in forwardInput
		if cs.Decode(raw, &in) != nil {
			return nil, errFrame
		}
		work = clusterwire.Forward{Scope: clusterwire.Scope{Identity: in.Scope.Identity, Cluster: in.Scope.Cluster, Epoch: in.Scope.Epoch, Protocol: in.Scope.Protocol}, Generation: in.Generation, From: in.From, To: in.To, ID: in.ID, Action: in.Action, Command: in.Command, Revision: in.Revision, Barrier: in.Barrier, Schedule: in.Schedule, Now: in.Now}
	} else if peerDecode(raw, &work) != nil {
		return nil, errFrame
	}
	e := encoder{}
	switch op {
	case 0:
		if len(key) != ed25519.SeedSize {
			return nil, errFrame
		}
		private := ed25519.NewKeyFromSeed(key)
		work.Sign(func(b []byte) []byte { return ed25519.Sign(private, b) })
		b, err := json.Marshal(work)
		if err != nil {
			return nil, err
		}
		e.blob(b)
	case 1:
		if work.Verify(key) {
			e.u32(1)
			b, err := cs.Encode(forwardReply{work.ID, work.Error, work.Barrier, work.Applied, work.Result, work.Evidence, work.Receipt, work.Plan, work.Membership, work.SleepAck, work.Status})
			if err != nil {
				return nil, err
			}
			e.blob(b)
		} else {
			e.u32(0)
		}
	default:
		return nil, errFrame
	}
	return e.b, nil
}

// The canonical host projection keeps every index lossless in JavaScript;
// the signed peer frame retains the established native wire exactly.
type forwardInput struct {
	Scope      peerScopeState          `json:"scope"`
	Generation uint64                  `json:"generation"`
	From       uint64                  `json:"from"`
	To         uint64                  `json:"to"`
	ID         string                  `json:"id"`
	Action     string                  `json:"action"`
	Command    []byte                  `json:"command"`
	Revision   uint64                  `json:"revision"`
	Barrier    uint64                  `json:"barrier"`
	Schedule   *raftcore.ScheduleInput `json:"schedule"`
	Now        int64                   `json:"now"`
}
type forwardReply struct {
	ID         string                       `json:"id"`
	Error      string                       `json:"error"`
	Barrier    uint64                       `json:"barrier"`
	Applied    uint64                       `json:"applied"`
	Result     *cs.Result                   `json:"result"`
	Evidence   *raftcore.LeadershipEvidence `json:"evidence"`
	Receipt    *cs.LeadershipReceipt        `json:"receipt"`
	Plan       *raftcore.SchedulePlan       `json:"plan"`
	Membership *raftcore.MembershipPlan     `json:"membership"`
	SleepAck   *raftcore.SleepAck           `json:"sleep_ack"`
	Status     clusterwire.ForwardStatus    `json:"status"`
}
