package relayserve

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/raftdirectory"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func (n *namespaces) enrollPersonal(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost || r.URL.RawQuery != "" {
		http.NotFound(w, r)
		return
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, cs.MaxCommandBytes))
	if err != nil {
		http.Error(w, "invalid enrollment", 400)
		return
	}
	id, _, err := n.authenticate(r, raw)
	if err != nil {
		http.Error(w, "this computer is not allowed to join this cluster", 403)
		return
	}
	proof, err := reqsign.Capture(r, raw)
	if err != nil {
		http.Error(w, "invalid enrollment", 403)
		return
	}
	n.mu.Lock()
	d := n.groups[id]
	n.mu.Unlock()
	if d == nil {
		http.Error(w, "cluster is unavailable", 503)
		return
	}
	out, err := d.host.EnrollPersonal(r.Context(), proof)
	if err != nil {
		var refusal raftdirectory.EnrollmentRefusal
		if errors.As(err, &refusal) {
			status := http.StatusConflict
			if refusal.Code == cs.Capacity {
				status = http.StatusTooManyRequests
			}
			http.Error(w, refusal.Error(), status)
			return
		}
		http.Error(w, "this computer is not allowed to join this cluster", 403)
		return
	}
	d.armMembers()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// exchangePersonal never discovers or creates a namespace from a peer key.
// Only admission or ordinary signed public ingress can activate its committed
// stores. Established sockets use the router's per-frame current view checks.
func (n *namespaces) exchangePersonal(w http.ResponseWriter, r *http.Request) {
	var raw []byte
	var err error
	if r.URL.Path == rafttransport.SocketPath || r.URL.Path == clusterctl.ForwardPath {
		raw, err = base64.RawURLEncoding.DecodeString(r.Header.Get(rafttransport.SocketAuthHeader))
	} else {
		raw, err = io.ReadAll(http.MaxBytesReader(w, r.Body, rafttransport.MaxExchangeBytes))
		r.Body = io.NopCloser(bytes.NewReader(raw))
	}
	var request rafttransport.Request
	if err != nil || rafttransport.Decode(raw, &request) != nil {
		http.Error(w, "invalid peer request", 400)
		return
	}
	n.mu.Lock()
	d := n.groups[request.Scope.Identity]
	n.mu.Unlock()
	if d == nil && checkedID(request.Scope.Identity) == nil {
		// A returning peer may activate an EXISTING durable namespace, but an
		// unregistered key cannot create storage or invent a cluster root.
		if _, err := os.Stat(filepath.Join(n.root, "raft", request.Scope.Identity, "provision.json")); err == nil {
			d, _ = n.activate(r.Context(), request.Scope.Identity, nil)
		}
	}
	if d == nil || d.peers == nil {
		http.Error(w, "member authorization refused", 403)
		return
	}
	if r.URL.Path == clusterctl.ForwardPath {
		d.host.ForwardHTTP(w, r)
		if d.host.PersonalDeparted(request.Node) {
			d.armMembers()
		}
	} else {
		d.peers.ServeHTTP(w, r)
	}
	// Merely polling an idle learner does not arm recurring work. Actual peer
	// ingress wakes the same scheduler as every other consensus input.
	if len(request.Messages) > 0 {
		d.armMembers()
	}
}
