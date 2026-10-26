package syncsetup

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

type leadershipBackend struct {
	t        *testing.T
	commands []cs.Command
	fail     bool
}

func (b *leadershipBackend) Status(context.Context, string) (clusterctl.Status, error) {
	return clusterctl.Status{Cluster: "retry-cluster", Self: 4, Membership: cs.Membership{Revision: 1}}, nil
}
func (b *leadershipBackend) Lead(_ context.Context, c cs.Command) (cs.LeadershipReceipt, error) {
	b.commands = append(b.commands, c)
	if b.fail {
		b.fail = false
		return cs.LeadershipReceipt{}, errors.New("reply lost after commit")
	}
	return cs.LeadershipReceipt{Node: 4, Term: 3, Revision: 2, Barrier: 12, Voters: []uint64{1, 2, 4}}, nil
}

// R6/R17/R23 persist the original signed request across a lost reply and a new
// Sync handle. A later sync-off decision stops both status and lead traffic.
func TestR6R17R23LeadershipRequestRetryAndQuietWire(t *testing.T) {
	backend := &leadershipBackend{t: t, fail: true}
	calls := 0
	server := httptest.NewServer(clusterctl.Handler(func(r *http.Request, b []byte) (string, string, error) {
		calls++
		return reqsign.Verify(r, b, time.Now())
	}, func(string) (clusterctl.Backend, error) { return backend, nil }))
	defer server.Close()
	home := machine(t, server.URL)
	s, ok, e := Open(home)
	if e != nil || !ok {
		t.Fatal(e)
	}
	if _, e = s.LeadCluster(context.Background(), 0); e == nil {
		t.Fatal("lost reply reported success")
	}
	again, ok, e := Open(home)
	if e != nil || !ok {
		t.Fatal(e)
	}
	receipt, e := again.LeadCluster(context.Background(), 0)
	if e != nil || receipt.Node != 4 {
		t.Fatal(receipt, e)
	}
	if len(backend.commands) != 2 || !reflect.DeepEqual(backend.commands[0], backend.commands[1]) {
		t.Fatal("retry changed committed intent")
	}
	c := backend.commands[0]
	b, e := cs.SigningBytes(c)
	if e != nil {
		t.Fatal(e)
	}
	pub, e := hex.DecodeString(s.Device.Cert.Device)
	if e != nil {
		t.Fatal(e)
	}
	if !ed25519.Verify(pub, b, c.Signature) {
		t.Fatal("request lacks original personal signature")
	}
	n := calls
	t.Setenv(URLVar, "off")
	if _, e = again.ClusterStatus(context.Background()); e == nil {
		t.Fatal("off status succeeded")
	}
	if _, e = again.LeadCluster(context.Background(), 0); e == nil {
		t.Fatal("off lead succeeded")
	}
	if calls != n {
		t.Fatal("R23 sync-off sent cluster traffic")
	}
	t.Setenv(URLVar, server.URL)
	solo := t.TempDir()
	if _, e = identity.EnsureSolo(solo); e != nil {
		t.Fatal(e)
	}
	s, ok, e = Open(solo)
	if e != nil || !ok {
		t.Fatal(e)
	}
	if _, e = s.ClusterStatus(context.Background()); e == nil {
		t.Fatal("solo status succeeded")
	}
	if _, e = s.LeadCluster(context.Background(), 0); e == nil {
		t.Fatal("solo lead succeeded")
	}
	if calls != n {
		t.Fatal("R23 never-paired home sent cluster traffic")
	}
}
