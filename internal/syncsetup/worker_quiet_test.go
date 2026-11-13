package syncsetup

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/identity"
)

type workerGateTransport struct{ calls int }

func (w *workerGateTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	w.calls++
	return &http.Response{StatusCode: http.StatusServiceUnavailable, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
}

// R16/R23 keep a standalone delegated worker independent of a personal identity
// while making a later process/home off decision override its retained handle.
func TestR16R23WorkerClusterHandleStopsAfterOff(t *testing.T) {
	home := t.TempDir()
	id, err := identity.Mint()
	if err != nil {
		t.Fatal(err)
	}
	seed := bytes.Repeat([]byte{91}, ed25519.SeedSize)
	key := ed25519.NewKeyFromSeed(seed)
	now := time.Now()
	cert, err := identity.IssueWorkerCert(id, key.Public().(ed25519.PublicKey), "scoped-cluster", []string{"selected"}, now.Add(-time.Minute), now.Add(time.Hour), false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := cs.Encode(struct {
		V           uint16               `json:"v"`
		Seed        string               `json:"worker_seed"`
		Certificate cs.WorkerCertificate `json:"certificate"`
	}{1, hex.EncodeToString(seed), cert})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := identity.UnmarshalWorker(raw, id.PublicKey(), "scoped-cluster", now)
	if err != nil {
		t.Fatal(err)
	}
	transport := &workerGateTransport{}
	t.Setenv(URLVar, "http://worker.invalid")
	client := WorkerClusterAPI(home, "http://worker.invalid", id.PublicKey(), worker, 1, &http.Client{Transport: transport}, func() bool { return true })
	if _, _, err = client.Cell(context.Background(), "selected"); err == nil || transport.calls != 1 {
		t.Fatal("R23 explicit worker did not use its controlled wire", transport.calls, err)
	}
	t.Setenv(URLVar, "off")
	if _, _, err = client.Cell(context.Background(), "selected"); err == nil || transport.calls != 1 {
		t.Fatal("R23 retained worker sent after off", transport.calls, err)
	}
	if _, err = identity.Load(home); err == nil {
		t.Fatal("R16 worker client created a personal identity")
	}
}
