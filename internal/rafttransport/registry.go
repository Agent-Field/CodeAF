package rafttransport

import "github.com/Agent-Field/codeaf/internal/clusterstate"

// CommittedView derives routing authority from the serialized applied store.
// The caller holds its actor lock through Install and before replying to any
// registry/revocation command. Applied index is the view revision, so a device
// revoke fences sockets even when no membership intent changed revision.
func CommittedView(scope Scope, generation, applied uint64, conf clusterstate.Configuration, read clusterstate.ReadRecord) (View, error) {
	_, peers, err := clusterstate.MembershipView(read, conf, applied)
	if err != nil {
		return View{}, err
	}
	if len(peers) == 0 || applied == 0 {
		return View{}, ErrAuth
	}
	v := View{Scope: scope, Generation: generation, Revision: applied}
	for _, p := range peers {
		v.Members = append(v.Members, Member{Node: p.Node, Incarnation: p.Incarnation, Key: p.Key, Voter: p.Voter, Revoked: p.Revoked})
	}
	return v, nil
}

// CommittedViewAt must be used by hosts admitting delegated learner workers.
// Time was committed before the actor barrier; a local clock grants nothing.
func CommittedViewAt(scope Scope, generation, applied uint64, conf clusterstate.Configuration, now int64, read clusterstate.ReadRecord) (View, error) {
	_, peers, err := clusterstate.MembershipViewAt(read, conf, applied, now)
	if err != nil {
		return View{}, err
	}
	if len(peers) == 0 || applied == 0 {
		return View{}, ErrAuth
	}
	v := View{Scope: scope, Generation: generation, Revision: applied}
	for _, p := range peers {
		v.Members = append(v.Members, Member{Node: p.Node, Incarnation: p.Incarnation, Key: p.Key, Voter: p.Voter, Revoked: p.Revoked})
	}
	return v, nil
}
