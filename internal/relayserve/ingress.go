package relayserve

import (
	"io"
	"net/http"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

// clusterAPI authenticates the entire selected request before activation. A
// worker delegation is accepted only here, never at personal pairing doors.
func (n *namespaces) clusterAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, cs.MaxCommandBytes))
	var request cs.ClusterRequest
	if err != nil || cs.Decode(raw, &request) != nil {
		http.Error(w, "invalid cluster request", 400)
		return
	}
	actor, err := cs.VerifyClusterRequest(request, n.now().UnixMilli())
	if err != nil {
		http.Error(w, "this computer is not allowed to use this cluster", 403)
		return
	}
	d, err := n.activate(r.Context(), cs.IdentityID(request.IdentityKey), request.IdentityKey)
	if err != nil {
		http.Error(w, cs.LeadershipUnavailable, 503)
		return
	}
	clock, err := cs.Sign(cs.Command{Schema: cs.SchemaVersion, Cluster: d.Cluster, Actor: d.effects.Actor, Session: "ingress", Sequence: 1, Now: n.now().UnixMilli(), Change: cs.Mutation{DeviceSeen: &cs.DeviceSeen{Device: actor}}}, d.effects.Key)
	if err != nil {
		http.Error(w, cs.LeadershipUnavailable, 503)
		return
	}
	result, err := d.host.ClusterAPI(r.Context(), request, clock)
	if err != nil {
		http.Error(w, "the selected work is unavailable; refresh before trying again", 503)
		return
	}
	raw, err = cs.Encode(result)
	if err != nil || len(raw) > cs.MaxEvaluationBytes {
		http.Error(w, cs.LeadershipUnavailable, 503)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(raw)
}
