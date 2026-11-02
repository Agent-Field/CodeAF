package syncsetup

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/clusterapi"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/identity"
)

// ClusterAPI uses the same use-time network gate as directory and blob clients.
// Constructing it does not enroll a member, discover a relay or open a socket.
func (s *Sync) ClusterAPI(cluster string) clusterapi.Client {
	return clusterapi.Client{Base: s.Relay, HTTP: s.ClusterHTTP(nil), Allowed: func() bool { return MayTalk(s.Home) }, Sign: s.Device.Sign, Request: func() cs.ClusterRequest {
		cert, _ := json.Marshal(s.Device.Cert)
		return cs.ClusterRequest{IdentityKey: s.Identity.PublicKey(), Cluster: cluster, Certificate: base64.RawURLEncoding.EncodeToString(cert)}
	}}
}

// WorkerClusterAPI accepts only delegated credentials and an explicit use-time
// policy. It never loads a personal identity or discovers a relay on its behalf.
// The caller supplies the SAME policy for peer frames and its content transport.
// The worker home and process sync-off decision still take precedence at use time.
func WorkerClusterAPI(home, base string, root ed25519.PublicKey, worker identity.Worker, incarnation uint64, client *http.Client, allowed func() bool) clusterapi.Client {
	return clusterapi.Client{Base: base, HTTP: client, Allowed: func() bool { return Resolve(home).On() && allowed != nil && allowed() }, Sign: worker.Sign, Request: func() cs.ClusterRequest {
		cert := worker.Certificate
		return cs.ClusterRequest{IdentityKey: append([]byte(nil), root...), Cluster: cert.Cluster, Worker: &cert, Incarnation: incarnation}
	}}
}

// SignClusterCommand retains the original personal signature and certificate
// for hosts whose legacy directory never saved a personal authority row.
func (s *Sync) SignClusterCommand(c cs.Command) (cs.Command, error) {
	cert, err := json.Marshal(s.Device.Cert)
	if err != nil {
		return c, err
	}
	c.Certificate = base64.RawURLEncoding.EncodeToString(cert)
	c.Actor = s.Device.ID()
	c.RequestDigest, err = cs.Digest(c)
	if err != nil {
		return c, err
	}
	raw, err := cs.SigningBytes(c)
	if err != nil {
		return c, err
	}
	c.Signature = s.Device.Sign(raw)
	return c, nil
}
