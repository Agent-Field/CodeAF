package clusterstate

// LeadershipIntent survives election and restart. Settled means the requested
// configuration is durable, not that the target won an election. Only a fresh
// leader-side quorum barrier can produce a successful leadership receipt.
type LeadershipIntent struct {
	Term      uint64 `json:"term"`
	Node      uint64 `json:"node"`
	Standby   uint64 `json:"standby"`
	Revision  uint64 `json:"revision"`
	Barrier   uint64 `json:"barrier"`
	Restore   bool   `json:"restore"`
	Completed bool   `json:"completed"`
	Settled   bool   `json:"settled"`
}

type RequestLeadership struct {
	Device   string `json:"device"`
	Revision uint64 `json:"revision"`
	Node     uint64 `json:"node"`
}
type RestoreAnchors struct {
	Revision uint64 `json:"revision"`
}
type CompleteLeadership struct {
	Revision uint64 `json:"revision"`
}

type SettleLeadership struct {
	Revision uint64 `json:"revision"`
}

// LeadershipReceipt contains coordination metadata only. It grants no chat
// ownership or content access and is issued by the elected target after a
// current-term commit and a quorum read, never by proposal admission.
type LeadershipReceipt struct {
	Node     uint64   `json:"node"`
	Term     uint64   `json:"term"`
	Revision uint64   `json:"revision"`
	Barrier  uint64   `json:"barrier"`
	Voters   []uint64 `json:"voters"`
}

const LeadershipUnavailable = "this cluster cannot coordinate right now; reconnect enough of its voting computers and try again; your saved chats are still here"

func leadershipMutation(m Mutation) bool {
	return m.RequestLeadership != nil || m.RestoreAnchors != nil || m.SettleLeadership != nil || m.CompleteLeadership != nil
}
func leadershipDependencies(m Mutation) []RecordKey {
	keys := []RecordKey{MembershipKey()}
	if v := m.RequestLeadership; v != nil {
		keys = append(keys, MemberKey(v.Node), DeviceKey(v.Device), AuthorityKey(v.Device))
	}
	return keys
}

func sameIDs(a, b []uint64) bool {
	if len(a) != len(b) {
		return false
	}
	for _, id := range a {
		if !hasID(b, id) {
			return false
		}
	}
	return true
}

// LeadershipVoters describes policy; hosts still use Raft's configuration
// machinery to enact it. Three provisioned anchors remain enrolled throughout.
func (h Membership) LeadershipVoters() []uint64 {
	v := append([]uint64(nil), h.Anchors...)
	if l := h.Leadership; l != nil && !l.Restore {
		for i, id := range v {
			if id == l.Standby {
				v[i] = l.Node
			}
		}
	}
	return v
}
func (h Membership) StableTopology(c Configuration) bool {
	if c.Joint() || !sameIDs(c.Voters, h.LeadershipVoters()) {
		return false
	}
	if l := h.Leadership; l != nil {
		learner := l.Standby
		if l.Restore {
			learner = l.Node
		}
		return hasID(c.Learners, learner)
	}
	return true
}

func applyLeadership(c Command, meta ApplicationMeta, rows map[RecordKey][]byte, put func(RecordKey, any), result *Result, h Membership) Code {
	if h.Pending != nil {
		return Conflict
	}
	m := c.Change
	switch {
	case m.RequestLeadership != nil:
		v := m.RequestLeadership
		if meta.Term == 0 {
			return BadRequest
		}
		if v.Revision != h.Revision || meta.Configuration.Joint() || h.Leadership != nil && (!h.Leadership.Settled || h.Leadership.Restore || h.Leadership.Node != v.Node) {
			return Conflict
		}
		if !h.StableTopology(meta.Configuration) {
			return Conflict
		}
		var member MemberRecord
		if Decode(rows[MemberKey(v.Node)], &member) != nil {
			return NotFound
		}
		if member.Worker {
			return Unauthorized
		}
		if member.Anchor || member.Retired || member.Protocol != ClusterProtocol || member.ReadyIndex == 0 || member.Node != v.Node || member.Device != v.Device {
			return Conflict
		}
		// The target's own committed personal authority and revocation are
		// checked independently of the owner who requests the transfer.
		var auth Authority
		var device Device
		if Decode(rows[AuthorityKey(member.Device)], &auth) != nil || !auth.ValidRole() || !auth.Personal || auth.Anchor || auth.Effects || auth.Cluster != c.Cluster {
			return Unauthorized
		}
		if b := rows[DeviceKey(member.Device)]; b != nil {
			if Decode(b, &device) != nil {
				return BadRequest
			}
			if device.Revoked {
				return Revoked
			}
		}
		h.Revision++
		h.Leadership = &LeadershipIntent{Node: v.Node, Standby: h.Anchors[len(h.Anchors)-1], Revision: h.Revision, Barrier: meta.Index, Term: meta.Term}
	case m.RestoreAnchors != nil:
		v := m.RestoreAnchors
		if v.Revision != h.Revision || h.Leadership == nil || h.Leadership.Restore {
			return Conflict
		}
		h.Revision++
		l := *h.Leadership
		l.Revision, l.Barrier, l.Restore, l.Settled, l.Completed = h.Revision, meta.Index, true, false, false
		h.Leadership = &l
	case m.CompleteLeadership != nil:
		if m.CompleteLeadership.Revision != h.Revision || h.Leadership == nil || h.Leadership.Restore || !h.Leadership.Settled || h.Leadership.Completed || !h.StableTopology(meta.Configuration) {
			return Conflict
		}
		l := *h.Leadership
		l.Completed = true
		h.Leadership = &l
	case m.SettleLeadership != nil:
		v := m.SettleLeadership
		if v.Revision != h.Revision || h.Leadership == nil || h.Leadership.Settled || !h.StableTopology(meta.Configuration) {
			return Conflict
		}
		l := *h.Leadership
		if l.Restore {
			h.Leadership = nil
		} else {
			l.Settled = true
			h.Leadership = &l
		}
	default:
		return BadRequest
	}
	h.Index = meta.Index
	put(MembershipKey(), h)
	result.Membership = &MembershipResult{Revision: h.Revision, Barrier: meta.Index}
	if h.Leadership != nil {
		result.Membership.Node = h.Leadership.Node
	}
	return OK
}
