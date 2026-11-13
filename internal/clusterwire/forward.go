// Package clusterwire preserves signed application routing without network dependencies.
package clusterwire

import (
	"crypto/ed25519"
	"encoding/json"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftcore"
)

const ForwardPath = "/v2/cluster/forward"
const ForwardTimeout = 30 * time.Second

// Scope binds every signature to an identity, cluster epoch and protocol. All
// uint64 fields cross JavaScript as canonical unsigned decimal strings.
type Scope struct {
	Identity string `json:"identity"`
	Cluster  string `json:"cluster"`
	Epoch    uint64 `json:"epoch,string"`
	Protocol uint16 `json:"protocol"`
}

// Forward is application ingress on a separate outbound connection. Original
// commands never become peer envelopes or untrusted Raft proposal messages.
// Each direction signs its complete scope, request ID and body independently.
type Forward struct {
	Scope      Scope                        `json:"scope"`
	Generation uint64                       `json:"generation,string"`
	From       uint64                       `json:"from,string"`
	To         uint64                       `json:"to,string"`
	ID         string                       `json:"id"`
	Action     string                       `json:"action"`
	Command    []byte                       `json:"command,omitempty"`
	Revision   uint64                       `json:"revision,omitempty"`
	Barrier    uint64                       `json:"barrier,omitempty"`
	Schedule   *raftcore.ScheduleInput      `json:"schedule,omitempty"`
	Now        int64                        `json:"now,omitempty"`
	Result     *cs.Result                   `json:"result,omitempty"`
	Evidence   *raftcore.LeadershipEvidence `json:"evidence,omitempty"`
	Receipt    *cs.LeadershipReceipt        `json:"receipt,omitempty"`
	Plan       *raftcore.SchedulePlan       `json:"plan,omitempty"`
	Membership *raftcore.MembershipPlan     `json:"membership,omitempty"`
	SleepAck   *raftcore.SleepAck           `json:"sleep_ack,omitempty"`
	Status     ForwardStatus                `json:"status"`
	Applied    uint64                       `json:"applied,omitempty"`
	Error      string                       `json:"error,omitempty"`
	Signature  []byte                       `json:"signature"`
}

func (f Forward) bytes() []byte {
	f.Signature = nil
	b, _ := json.Marshal(f)
	return append([]byte("codeaf/cluster/forward/1\n"), b...)
}
func (f *Forward) Sign(sign func([]byte) []byte) { f.Signature = sign(f.bytes()) }
func (f Forward) Verify(key ed25519.PublicKey) bool {
	return len(key) == ed25519.PublicKeySize && f.ID != "" && len(f.ID) <= raftcore.MaxRequestIDBytes && ed25519.Verify(key, f.bytes(), f.Signature)
}

// ForwardStatus contains only routing hints; quorum replies carry their own barrier.
type ForwardStatus struct {
	Leader  uint64
	Term    uint64
	Applied uint64
}

func PeerStatus(s raftcore.Status) ForwardStatus {
	return ForwardStatus{Leader: s.Leader, Term: s.Term, Applied: s.Applied}
}
