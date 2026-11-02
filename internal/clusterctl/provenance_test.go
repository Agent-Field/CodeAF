package clusterctl_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	ctl "github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

type provenanceBackend struct{ calls atomic.Int32 }

func (b *provenanceBackend) Status(context.Context, string) (ctl.Status, error) {
	return ctl.Status{}, ctl.ErrUnavailable
}
func (b *provenanceBackend) Lead(context.Context, cs.Command) (cs.LeadershipReceipt, error) {
	b.calls.Add(1)
	return cs.LeadershipReceipt{}, ctl.ErrUnavailable
}

// R15/R25 require the public v2 door to reject compatibility provenance BEFORE
// proposal. A signed v2 body must not hide arbitrary bytes in an old HTTP proof.
func TestR15R25V2IngressRejectsV1ProvenanceBeforeDispatch(t *testing.T) {
	b := &provenanceBackend{}
	var requests atomic.Int32
	h := ctl.Handler(func(*http.Request, []byte) (string, string, error) { return "identity", "device", nil }, func(string) (ctl.Backend, error) { return b, nil })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1); h.ServeHTTP(w, r) }))
	defer srv.Close()
	c := cs.Command{Schema: 1, Cluster: "cluster", Actor: "device", Session: "leadership", Sequence: 1, RequestDigest: "digest", Change: cs.Mutation{RequestLeadership: &cs.RequestLeadership{Revision: 1, Node: 4, Device: "device"}}}
	good, err := cs.EncodeCommand(c)
	if err != nil {
		t.Fatal(err)
	}
	res, err := srv.Client().Post(srv.URL+ctl.Path, "application/json", bytes.NewReader(good))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if b.calls.Load() != 1 {
		t.Fatal("ordinary command never reached dispatch")
	}
	c.V1 = &cs.V1Request{Body: []byte("private bytes from an invalid v2 request")}
	raw, err := cs.EncodeCommand(c)
	if err != nil {
		t.Fatal(err)
	}
	res, err = srv.Client().Post(srv.URL+ctl.Path, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || b.calls.Load() != 1 {
		t.Fatal("v2 provenance entered consensus dispatch", res.StatusCode, b.calls.Load())
	}
	before := requests.Load()
	client := ctl.Client{Base: srv.URL, Sign: func(*http.Request, []byte) {}, HTTP: srv.Client()}
	if _, err = client.Lead(context.Background(), c); err == nil {
		t.Fatal("client sent v1 provenance")
	}
	if requests.Load() != before {
		t.Fatal("invalid client command reached the wire")
	}
}
