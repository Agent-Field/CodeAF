package relayserve

import (
	"context"
	"net/http"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

type controlCertificateKey struct{}

// controlAuthenticate retains this request's original certificate for the
// backend's second authorization under its own fresh quorum barrier.
func (n *namespaces) controlAuthenticate(r *http.Request, body []byte) (string, string, error) {
	identity, actor, err := n.authenticate(r, body)
	if err == nil {
		*r = *r.WithContext(context.WithValue(r.Context(), controlCertificateKey{}, r.Header.Get(reqsign.HeaderCert)))
	}
	return identity, actor, err
}

func (n *namespaces) controlBackend(identity string) (clusterctl.Backend, error) {
	n.mu.Lock()
	d := n.groups[identity]
	n.mu.Unlock()
	if d == nil {
		return nil, clusterctl.ErrUnavailable
	}
	return nativeControl{d}, nil
}

type nativeControl struct{ owner *nativeNamespace }

func (b nativeControl) Status(ctx context.Context, actor string) (clusterctl.Status, error) {
	b.owner.armMembers()
	certificate, _ := ctx.Value(controlCertificateKey{}).(string)
	return b.owner.host.PersonalStatus(ctx, actor, certificate)
}

// Lead runs the native committed promotion workflow with the original proof.
func (b nativeControl) Lead(ctx context.Context, c cs.Command) (cs.LeadershipReceipt, error) {
	b.owner.armMembers()
	certificate, _ := ctx.Value(controlCertificateKey{}).(string)
	return b.owner.host.PersonalLead(ctx, c, certificate)
}
