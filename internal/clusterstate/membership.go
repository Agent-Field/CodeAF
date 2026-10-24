package clusterstate

import (
	"bytes"
	"crypto/ed25519"
	"strconv"
)

const (
	MaxLearners             = 32
	MaxMemberRecords        = 4096
	ClusterProtocol  uint16 = 1
)

// Configuration is supplied by the host from DURABLY APPLIED Raft state, never
// from a request. Joint fields are retained so admission cannot cross a swap.
type Configuration struct {
	Voters       []uint64 `json:"voters"`
	Learners     []uint64 `json:"learners"`
	Outgoing     []uint64 `json:"outgoing"`
	LearnersNext []uint64 `json:"learners_next"`
	AutoLeave    bool     `json:"auto_leave"`
}

func (c Configuration) Joint() bool {
	return len(c.Outgoing) > 0 || len(c.LearnersNext) > 0 || c.AutoLeave
}
func hasID(ids []uint64, id uint64) bool {
	for _, n := range ids {
		if n == id {
			return true
		}
	}
	return false
}

// MemberRecord contains protocol authority only. Retired IDs are never deleted;
// a device that rejoins receives a fresh ID and a fresh transport incarnation.
type MemberRecord struct {
	Worker        bool   `json:"worker,omitempty"`
	WorkerExpires int64  `json:"worker_expires,omitempty"`
	Node          uint64 `json:"node"`
	Device        string `json:"device"`
	Key           []byte `json:"key"`
	Incarnation   uint64 `json:"incarnation"`
	Protocol      uint16 `json:"protocol"`
	Anchor        bool   `json:"anchor"`
	Retired       bool   `json:"retired"`
	ReadyIndex    uint64 `json:"ready_index"`
}
type MembershipIntent struct {
	Node     uint64 `json:"node"`
	Remove   bool   `json:"remove"`
	Revision uint64 `json:"revision"`
}
type Membership struct {
	Leadership *LeadershipIntent `json:"leadership,omitempty"`
	Revision   uint64            `json:"revision"`
	Index      uint64            `json:"index"`
	NextNode   uint64            `json:"next_node"`
	Anchors    []uint64          `json:"anchors"`
	Nodes      []uint64          `json:"nodes"`
	Live       uint32            `json:"live"`
	Limit      uint32            `json:"limit"`
	Pending    *MembershipIntent `json:"pending,omitempty"`
}
type InitMembership struct {
	Anchors []MemberRecord `json:"anchors"`
	Limit   uint32         `json:"limit"`
}
type EnrollMember struct {
	Node     uint64 `json:"node"`
	Revision uint64 `json:"revision"`
	Device   string `json:"device"`
	Protocol uint16 `json:"protocol"`
}
type RemoveMember struct {
	Revision uint64 `json:"revision"`
	Node     uint64 `json:"node"`
	Device   string `json:"device"`
}
type SettleMembership struct {
	Revision uint64 `json:"revision"`
	Node     uint64 `json:"node"`
}
type MembershipResult struct {
	Node     uint64 `json:"node"`
	Revision uint64 `json:"revision"`
	Barrier  uint64 `json:"barrier"`
}
type DeviceMember struct {
	Node uint64 `json:"node"`
}

func AnchorActor(node uint64) string { return "anchor/" + strconv.FormatUint(node, 10) }

func MembershipKey() RecordKey { return RecordKey{"membership", "registry"} }
func MemberKey(node uint64) RecordKey {
	return RecordKey{"enrollment", "node/" + strconv.FormatUint(node, 10)}
}
func MemberDeviceKey(device string) RecordKey {
	return RecordKey{"membership", "device/" + segment(device)}
}
func MemberTombstoneKey(node uint64) RecordKey {
	return RecordKey{"tombstones", "node/" + strconv.FormatUint(node, 10)}
}

func membershipDependencies(m Mutation) []RecordKey {
	keys := []RecordKey{}
	if leadershipMutation(m) {
		return leadershipDependencies(m)
	}
	switch {
	case m.InitMembership != nil:
		keys = append(keys, MembershipKey())
		for _, a := range m.InitMembership.Anchors {
			keys = append(keys, MemberKey(a.Node), AuthorityKey(AnchorActor(a.Node)))
		}
	case m.EnrollMember != nil:
		keys = append(keys, MembershipKey(), DeviceKey(m.EnrollMember.Device), AuthorityKey(m.EnrollMember.Device), MemberDeviceKey(m.EnrollMember.Device), MemberKey(m.EnrollMember.Node), MemberTombstoneKey(m.EnrollMember.Node))
	case m.RemoveMember != nil:
		v := m.RemoveMember
		keys = append(keys, MembershipKey(), MemberKey(v.Node), MemberTombstoneKey(v.Node), MemberDeviceKey(v.Device))
	case m.SettleMembership != nil:
		v := m.SettleMembership
		keys = append(keys, MembershipKey(), MemberKey(v.Node))
	}
	return keys
}
func membershipMutation(m Mutation) bool {
	return leadershipMutation(m) || m.InitMembership != nil || m.EnrollMember != nil || m.RemoveMember != nil || m.SettleMembership != nil
}
func validNode(id uint64) bool { return id > 0 && id < ^uint64(0)-1 }
func applyMembership(c Command, meta ApplicationMeta, rows map[RecordKey][]byte, put func(RecordKey, any), result *Result) Code {
	m := c.Change
	var h Membership
	raw := rows[MembershipKey()]
	if raw != nil && Decode(raw, &h) != nil {
		return BadRequest
	}
	if m.InitMembership != nil {
		v := m.InitMembership
		if raw != nil {
			return Conflict
		}
		if len(v.Anchors) != 3 || meta.Configuration.Joint() || len(meta.Configuration.Voters) != 3 || len(meta.Configuration.Learners) != 0 || v.Limit < 3 || v.Limit > MaxMemberRecords || meta.Index == 0 {
			return BadRequest
		}
		seen := map[string]bool{}
		h = Membership{Revision: 1, Index: meta.Index, Limit: v.Limit}
		for i, a := range v.Anchors {
			if !validNode(a.Node) || !validNode(a.Node+1) || i > 0 && a.Node <= v.Anchors[i-1].Node || !hasID(meta.Configuration.Voters, a.Node) || len(a.Key) != ed25519.PublicKeySize || seen[string(a.Key)] || a.Device != "" || !a.Anchor || a.Worker || a.WorkerExpires != 0 || a.Retired || a.Incarnation != 1 || a.Protocol != ClusterProtocol || a.ReadyIndex != 0 || rows[MemberKey(a.Node)] != nil {
				return BadRequest
			}
			var old Authority
			if b := rows[AuthorityKey(AnchorActor(a.Node))]; b != nil {
				if Decode(b, &old) != nil || !old.ValidRole() || !old.Anchor || old.Personal || old.Effects || old.Cluster != c.Cluster || !bytes.Equal(old.Key, a.Key) {
					return Conflict
				}
			}
			seen[string(a.Key)] = true
			h.Anchors = append(h.Anchors, a.Node)
			h.Nodes = append(h.Nodes, a.Node)
			h.NextNode = a.Node + 1
		}
		for _, a := range v.Anchors {
			put(MemberKey(a.Node), a)
			put(AuthorityKey(AnchorActor(a.Node)), Authority{Cluster: c.Cluster, Key: a.Key, Anchor: true})
		}
		put(MembershipKey(), h)
		result.Membership = &MembershipResult{Revision: h.Revision, Barrier: meta.Index}
		return OK
	}
	if raw == nil || h.Revision == ^uint64(0) || meta.Index == 0 {
		return Conflict
	}
	if leadershipMutation(m) {
		return applyLeadership(c, meta, rows, put, result, h)
	}
	if h.Leadership != nil && !h.Leadership.Settled || !h.StableTopology(meta.Configuration) {
		return Conflict
	}
	switch {
	case m.EnrollMember != nil:
		v := m.EnrollMember
		if v.Revision != h.Revision || h.Pending != nil {
			return Conflict
		}
		if v.Protocol != ClusterProtocol {
			return UpdateRequired
		}
		var authority Authority
		var device Device
		var prior DeviceMember
		if Decode(rows[AuthorityKey(v.Device)], &authority) != nil || authority.Cluster != c.Cluster || !authority.ValidRole() || !authority.Personal && authority.Worker == nil || len(authority.Key) != ed25519.PublicKeySize {
			return Unauthorized
		}
		if b := rows[DeviceKey(v.Device)]; b != nil {
			if Decode(b, &device) != nil {
				return BadRequest
			}
			if device.Revoked {
				return Revoked
			}
		}
		incarnation := uint64(1)
		workerExpires := int64(0)
		if authority.Worker != nil {
			var identity IdentityRec
			if Decode(rows[IdentityKey()], &identity) != nil {
				return BadRequest
			}
			_, w, code := workerFromRows(c.Cluster, v.Device, result.Now, identity, rows)
			if code != OK {
				return code
			}
			if !authority.Worker.Learner || w.Incarnation == 0 || w.Draining {
				return Unauthorized
			}
			incarnation = w.Incarnation
			workerExpires = authority.Worker.Expires
		}
		if b := rows[MemberDeviceKey(v.Device)]; b != nil {
			if Decode(b, &prior) != nil {
				return BadRequest
			}
			if prior.Node != 0 {
				return Conflict
			}
		}
		if h.Live >= MaxLearners || len(h.Nodes) >= int(h.Limit) || !validNode(h.NextNode+1) {
			return Capacity
		}
		node := h.NextNode
		if v.Node != node || rows[MemberKey(node)] != nil || rows[MemberTombstoneKey(node)] != nil {
			return Conflict
		}
		for _, n := range h.Nodes {
			if n >= node {
				return Conflict
			}
		}
		h.NextNode++
		h.Revision++
		h.Index = meta.Index
		h.Live++
		h.Nodes = append(h.Nodes, node)
		h.Pending = &MembershipIntent{Node: node, Revision: h.Revision}
		put(MemberKey(node), MemberRecord{Node: node, Device: v.Device, Key: bytes.Clone(authority.Key), Incarnation: incarnation, Protocol: ClusterProtocol, Worker: authority.Worker != nil, WorkerExpires: workerExpires})
		put(MemberDeviceKey(v.Device), DeviceMember{Node: node})
		put(MembershipKey(), h)
		result.Membership = &MembershipResult{Node: node, Revision: h.Revision}
		return OK
	case m.RemoveMember != nil:
		v := m.RemoveMember
		if v.Revision != h.Revision || h.Pending != nil {
			return Conflict
		}
		var member MemberRecord
		if Decode(rows[MemberKey(v.Node)], &member) != nil {
			return NotFound
		}
		if member.Anchor || member.Retired || member.Node != v.Node || member.Device != v.Device || hasID(meta.Configuration.Voters, v.Node) {
			return Conflict
		}
		var mapping DeviceMember
		if Decode(rows[MemberDeviceKey(v.Device)], &mapping) != nil || mapping.Node != v.Node {
			return Conflict
		}
		member.Retired = true
		member.ReadyIndex = 0
		h.Revision++
		h.Index = meta.Index
		h.Live--
		h.Pending = &MembershipIntent{Node: v.Node, Remove: true, Revision: h.Revision}
		put(MemberKey(v.Node), member)
		put(MemberTombstoneKey(v.Node), member)
		put(MemberDeviceKey(v.Device), DeviceMember{})
		put(MembershipKey(), h)
		result.Membership = &MembershipResult{Node: v.Node, Revision: h.Revision}
		return OK
	case m.SettleMembership != nil:
		v := m.SettleMembership
		if v.Revision != h.Revision || h.Pending == nil || h.Pending.Node != v.Node {
			return Conflict
		}
		present := hasID(meta.Configuration.Learners, v.Node)
		if present == h.Pending.Remove {
			return Conflict
		}
		var member MemberRecord
		if Decode(rows[MemberKey(v.Node)], &member) != nil || member.Node != v.Node || member.Retired != h.Pending.Remove {
			return Conflict
		}
		if !member.Retired {
			member.ReadyIndex = meta.Index
			put(MemberKey(v.Node), member)
		}
		h.Pending = nil
		h.Index = meta.Index
		put(MembershipKey(), h)
		result.Membership = &MembershipResult{Node: v.Node, Revision: h.Revision, Barrier: meta.Index}
		return OK
	}
	return BadRequest
}

// PeerEnrollment is a derived view, not a second registry. Hosts publish it
// under their dispatch lock before acknowledging any committed revocation.
type PeerEnrollment struct {
	Node        uint64 `json:"node"`
	Incarnation uint64 `json:"incarnation"`
	Key         []byte `json:"key"`
	Voter       bool   `json:"voter"`
	Revoked     bool   `json:"revoked"`
}

func MembershipView(read ReadRecord, conf Configuration, applied uint64) (Membership, []PeerEnrollment, error) {
	var h Membership
	raw, err := read(MembershipKey())
	if err != nil {
		return h, nil, err
	}
	if raw == nil {
		return h, nil, nil
	}
	if Decode(raw, &h) != nil || h.Validate() != nil || h.Index > applied {
		return h, nil, ErrDependency
	}
	peers := make([]PeerEnrollment, 0, len(h.Nodes))
	for i, id := range h.Nodes {
		b, e := read(MemberKey(id))
		if e != nil {
			return h, nil, e
		}
		var m MemberRecord
		if Decode(b, &m) != nil || m.Node != id || i > 0 && h.Nodes[i-1] >= id || m.Protocol != ClusterProtocol || len(m.Key) != ed25519.PublicKeySize || m.Incarnation == 0 {
			return h, nil, ErrDependency
		}
		var device []byte
		if m.Device != "" {
			device, e = read(DeviceKey(m.Device))
			if e != nil {
				return h, nil, e
			}
		}
		peer, e := PeerFromRecord(m, device, conf)
		if e != nil {
			return h, nil, e
		}
		peers = append(peers, peer)
	}
	return h, peers, nil
}

// LearnerReady is evaluated on the learner's own durable applied state. A
// leader's replication Match, an uploaded snapshot or a registry grant alone
// cannot make the member ready to serve its advertised enrollment barrier.
func LearnerReady(m MemberRecord, conf Configuration, applied, advertised uint64) bool {
	return !m.Anchor && !m.Retired && m.ReadyIndex > 0 && advertised >= m.ReadyIndex && applied >= advertised && hasID(conf.Learners, m.Node)
}

// AnchorEnrollments validates the source-provisioned transport manifest used
// before the first registry command can commit. Only the SAME three bootstrap
// anchors may exchange protocol traffic; a personal device needs committed
// enrollment. This manifest contains no application or content authority.
func AnchorEnrollments(anchors []MemberRecord, voters []uint64) ([]PeerEnrollment, error) {
	if len(anchors) != 3 || len(voters) != 3 {
		return nil, ErrDependency
	}
	seen := map[string]bool{}
	peers := []PeerEnrollment{}
	for i, m := range anchors {
		if !validNode(m.Node) || i > 0 && anchors[i-1].Node >= m.Node || !hasID(voters, m.Node) || !m.Anchor || m.Worker || m.WorkerExpires != 0 || m.Device != "" || m.Retired || m.ReadyIndex != 0 || m.Protocol != ClusterProtocol || m.Incarnation != 1 || len(m.Key) != ed25519.PublicKeySize || seen[string(m.Key)] {
			return nil, ErrDependency
		}
		seen[string(m.Key)] = true
		peers = append(peers, PeerEnrollment{Node: m.Node, Incarnation: m.Incarnation, Key: bytes.Clone(m.Key), Voter: true})
	}
	return peers, nil
}

func (h Membership) Validate() error {
	if h.Revision == 0 || h.Index == 0 || len(h.Anchors) != 3 || len(h.Nodes) < 3 || h.Limit < 3 || h.Limit > MaxMemberRecords || len(h.Nodes) > int(h.Limit) || h.Live > MaxLearners || !validNode(h.NextNode) {
		return ErrDependency
	}
	for i, id := range h.Nodes {
		if !validNode(id) || id >= h.NextNode || i > 0 && h.Nodes[i-1] >= id {
			return ErrDependency
		}
	}
	for i, id := range h.Anchors {
		if !hasID(h.Nodes, id) || i > 0 && h.Anchors[i-1] >= id {
			return ErrDependency
		}
	}
	if h.Pending != nil && (h.Pending.Revision != h.Revision || !hasID(h.Nodes, h.Pending.Node) || hasID(h.Anchors, h.Pending.Node)) {
		return ErrDependency
	}
	if l := h.Leadership; l != nil {
		if !hasID(h.Nodes, l.Node) || hasID(h.Anchors, l.Node) || !hasID(h.Anchors, l.Standby) || l.Revision > h.Revision || l.Term == 0 && !l.Restore || l.Completed && (!l.Settled || l.Restore) || l.Barrier == 0 || l.Barrier > h.Index || h.Pending != nil && !l.Settled {
			return ErrDependency
		}
	}
	return nil
}

// EnrollmentMessage is shared by later ingress adapters and the manual. The
// protocol refusal distinguishes an old build from missing saved content.
func EnrollmentMessage(code Code) string {
	switch code {
	case UpdateRequired:
		return "this codeaf build must be updated before it can join this cluster; your saved chats are still here"
	case Capacity:
		return "this cluster has reached its enrollment limit; remove a joined computer or ask the relay operator for capacity"
	}
	return ""
}

// PeerFromRecord is shared by native registry scans and paged hosted reads.
// Device metadata only narrows existing enrollment; it never creates a peer.
func PeerFromRecord(m MemberRecord, device []byte, conf Configuration) (PeerEnrollment, error) {
	if !validNode(m.Node) || m.Protocol != ClusterProtocol || len(m.Key) != ed25519.PublicKeySize || m.Incarnation == 0 {
		return PeerEnrollment{}, ErrDependency
	}
	revoked := m.Retired || m.Worker
	if m.Device != "" && device != nil {
		var d Device
		if Decode(device, &d) != nil {
			return PeerEnrollment{}, ErrDependency
		}
		revoked = revoked || d.Revoked
	}
	return PeerEnrollment{Node: m.Node, Incarnation: m.Incarnation, Key: bytes.Clone(m.Key), Voter: hasID(conf.Voters, m.Node) || hasID(conf.Outgoing, m.Node), Revoked: revoked}, nil
}

// PeerFromRecordAt narrows worker routing using the same committed scope and
// incarnation as application commands. A worker can never be a voting peer.
func PeerFromRecordAt(m MemberRecord, device []byte, conf Configuration, now int64, identity IdentityRec, authority Authority, worker WorkerRecord) (PeerEnrollment, error) {
	p, err := PeerFromRecord(m, device, conf)
	if err != nil || !m.Worker {
		return p, err
	}
	var d Device
	if device != nil && Decode(device, &d) != nil {
		return p, ErrDependency
	}
	p.Revoked = m.Retired || p.Voter || worker.Incarnation != m.Incarnation || m.WorkerExpires != authorityExpiry(authority) || workerAuthority(authority.Cluster, m.Device, now, identity, authority, d) != OK || authority.Worker == nil || !authority.Worker.Learner || !bytes.Equal(m.Key, authority.Key)
	return p, nil
}
func authorityExpiry(a Authority) int64 {
	if a.Worker != nil {
		return a.Worker.Expires
	}
	return 0
}

// MembershipViewAt includes expiring delegated peers; the old metadata-only
// view deliberately refuses them until its host supplies committed time.
func MembershipViewAt(read ReadRecord, conf Configuration, applied uint64, now int64) (Membership, []PeerEnrollment, error) {
	h, peers, err := MembershipView(read, conf, applied)
	if err != nil {
		return h, peers, err
	}
	for i, p := range peers {
		var m MemberRecord
		b, e := read(MemberKey(p.Node))
		if e != nil {
			return h, nil, e
		}
		if Decode(b, &m) != nil {
			return h, nil, ErrDependency
		}
		if !m.Worker {
			continue
		}
		var identity IdentityRec
		var a Authority
		var w WorkerRecord
		for _, v := range []struct {
			k    RecordKey
			into any
		}{{IdentityKey(), &identity}, {AuthorityKey(m.Device), &a}, {WorkerKey(m.Device), &w}} {
			b, e = read(v.k)
			if e != nil {
				return h, nil, e
			}
			if Decode(b, v.into) != nil {
				return h, nil, ErrDependency
			}
		}
		d, e := read(DeviceKey(m.Device))
		if e != nil {
			return h, nil, e
		}
		peers[i], e = PeerFromRecordAt(m, d, conf, now, identity, a, w)
		if e != nil {
			return h, nil, e
		}
	}
	return h, peers, nil
}
