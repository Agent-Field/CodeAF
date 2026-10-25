package clusterstate

import "bytes"

// PersonalPlan describes one durable admission stage. The host commits it and
// reconciles configuration before asking again, so a lost reply cannot allocate
// another node or discard the original device proof.
type PersonalPlan struct {
	Code   Code      `json:"code"`
	Change *Mutation `json:"change,omitempty"`
	Node   uint64    `json:"node"`
}

func personalAllowed(cluster, actor, certificate string, read ReadRecord) (Code, error) {
	var identity IdentityRec
	raw, err := read(IdentityKey())
	if err != nil {
		return BadRequest, err
	}
	if Decode(raw, &identity) != nil || PersonalCertificate(identity, actor, certificate) != nil {
		return Unauthorized, nil
	}
	raw, err = read(DeviceKey(actor))
	if err != nil {
		return BadRequest, err
	}
	if raw != nil {
		var d Device
		if Decode(raw, &d) != nil || d.Revoked {
			return Unauthorized, nil
		}
	}
	raw, err = read(AuthorityKey(actor))
	if err != nil {
		return BadRequest, err
	}
	if raw != nil {
		var a Authority
		if Decode(raw, &a) != nil || !a.ValidRole() || !a.Personal || a.Cluster != cluster {
			return Unauthorized, nil
		}
	}
	return OK, nil
}

// PlanPersonalAdmission reads only the committed root, revocations and registry.
// The replicated evaluator repeats these checks when the original proof commits.
func PlanPersonalAdmission(cluster string, proof V1Request, now int64, read ReadRecord) (PersonalPlan, error) {
	out := PersonalPlan{Code: Unauthorized}
	var identity IdentityRec
	raw, err := read(IdentityKey())
	if err != nil {
		return out, err
	}
	if Decode(raw, &identity) != nil {
		return out, ErrDependency
	}
	request, granted, actor, err := VerifyPersonalAdmission(proof, identity, now)
	if err != nil {
		return out, nil
	}
	if code, e := personalAllowed(cluster, actor, proof.Certificate, read); e != nil {
		return out, e
	} else if code != OK {
		out.Code = code
		return out, nil
	}
	raw, err = read(AuthorityKey(actor))
	if err != nil {
		return out, err
	}
	if raw != nil {
		var a Authority
		if Decode(raw, &a) != nil {
			return out, ErrDependency
		}
		if !bytes.Equal(a.Key, granted.Key) {
			return out, nil
		}
	}
	if request.Protocol != ClusterProtocol {
		out.Code = UpdateRequired
		return out, nil
	}
	var h Membership
	raw, err = read(MembershipKey())
	if err != nil {
		return out, err
	}
	if Decode(raw, &h) != nil || h.Validate() != nil {
		return out, ErrDependency
	}
	var mapping DeviceMember
	raw, err = read(MemberStoreKey(actor, request.Store))
	if err != nil {
		return out, err
	}
	if raw == nil {
		if len(h.Nodes) >= int(h.Limit) {
			out.Code = Capacity
			return out, nil
		}
		raw, err = read(MemberDeviceKey(actor))
		if err != nil {
			return out, err
		}
		if raw != nil && Decode(raw, &mapping) != nil {
			return out, ErrDependency
		}
		out.Code = OK
		if mapping.Node != 0 {
			out.Change = &Mutation{RemoveMember: &RemoveMember{Node: mapping.Node, Device: actor, Revision: h.Revision, Admission: &proof}}
			return out, nil
		}
		if h.Live >= MaxLearners || !validNode(h.NextNode+1) {
			out.Code = Capacity
			return out, nil
		}
		out.Change = &Mutation{EnrollMember: &EnrollMember{Node: h.NextNode, Revision: h.Revision, Device: actor, Protocol: request.Protocol, Store: request.Store, Admission: &proof}}
		return out, nil
	}
	if Decode(raw, &mapping) != nil {
		return out, ErrDependency
	}
	raw, err = read(MemberKey(mapping.Node))
	if err != nil {
		return out, err
	}
	var m MemberRecord
	if Decode(raw, &m) != nil {
		return out, ErrDependency
	}
	if m.Device != actor || m.Store != request.Store || m.Retired || m.Worker || m.Anchor {
		return out, nil
	}
	out.Code = OK
	out.Node = m.Node
	return out, nil
}

// PersonalControl is the shared status shape. All counters use the canonical
// directory encoding, including fields nested in membership and configuration.
type PersonalControl struct {
	Ready                 []uint64      `json:"ready,omitempty"`
	LeadershipUnavailable bool          `json:"leadership_unavailable,omitempty"`
	Cluster               string        `json:"cluster"`
	Self                  uint64        `json:"self"`
	Leader                uint64        `json:"leader"`
	Membership            Membership    `json:"membership"`
	Configuration         Configuration `json:"configuration"`
}

func PersonalControlView(cluster, actor, certificate string, conf Configuration, applied, leader uint64, now int64, progress map[uint64]uint64, read ReadRecord) (PersonalControl, Code, error) {
	out := PersonalControl{Cluster: cluster, Leader: leader, Configuration: conf, LeadershipUnavailable: true}
	if code, e := personalAllowed(cluster, actor, certificate, read); e != nil {
		return out, code, e
	} else if code != OK {
		return out, code, nil
	}
	h, peers, err := MembershipViewAt(read, conf, applied, now)
	if err != nil {
		return out, BadRequest, err
	}
	if h.Revision == 0 {
		return out, BadRequest, ErrDependency
	}
	out.Membership = h
	for _, peer := range peers {
		if peer.Revoked || peer.Voter {
			continue
		}
		raw, e := read(MemberKey(peer.Node))
		if e != nil {
			return out, BadRequest, e
		}
		var m MemberRecord
		if Decode(raw, &m) != nil {
			return out, BadRequest, ErrDependency
		}
		if LearnerReady(m, conf, progress[m.Node], m.ReadyIndex) {
			out.Ready = append(out.Ready, m.Node)
		}
	}
	raw, err := read(MemberDeviceKey(actor))
	if err != nil {
		return out, BadRequest, err
	}
	if raw == nil {
		return out, OK, nil
	}
	var mapping DeviceMember
	if Decode(raw, &mapping) != nil {
		return out, BadRequest, ErrDependency
	}
	if mapping.Node == 0 {
		return out, OK, nil
	}
	raw, err = read(MemberKey(mapping.Node))
	if err != nil {
		return out, BadRequest, err
	}
	var m MemberRecord
	if Decode(raw, &m) != nil || m.Node != mapping.Node || m.Device != actor || m.Anchor || m.Worker {
		return out, BadRequest, ErrDependency
	}
	if !m.Retired {
		out.Self = m.Node
	}
	return out, OK, nil
}
