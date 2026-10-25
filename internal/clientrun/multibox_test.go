package clientrun_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

func TestAnotherExecutorCanEnrollAndClaimAnyWork(t *testing.T) {
	g := newRig(t)
	first := g.pair(t, "agents.create")
	seed, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	dev, err := seed.Join(identity.IssueCert(g.s.id, seed.Public(), g.now))
	if err != nil {
		t.Fatal(err)
	}
	secondSigner := signer{g.s.id, dev}
	wall := func() time.Time { return g.now }
	dir := directory.NewHTTP(g.url, reqsign.SignFor(secondSigner, wall), http.DefaultClient)
	if err := dir.PutDevice(context.Background(), dev.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	secondEngine := &engine{}
	r, err := clientrun.Open(clientrun.Config{Home: t.TempDir(), Relay: g.url, Signer: secondSigner, Directory: dir, Engine: secondEngine, Log: noopLog, Now: wall})
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.Pair(context.Background(), clientrun.Pairing{Cert: g.client.Cert(), BoxKey: g.pub[:], Methods: []string{"agents.create"}, Reads: []string{"asks.list"}})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"method":"agents.create","params":{"text":"start new work"}}`)
	sealed, err := clientrun.SealForExecutors(payload, map[string][]byte{first.Writer: first.BoxKey, second.Writer: second.BoxKey})
	if err != nil {
		t.Fatal(err)
	}
	g.request(t, g.client, "POST", "/v1/clients/"+g.client.dev.ID()+"/queue", map[string]any{"number": 1, "at": g.now.UnixMilli(), "method": "agents.create", "target": "any", "sealed": sealed}, nil, 204)
	if err := r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if secondEngine.created != 1 || g.e.created != 0 {
		t.Fatal("any work was not executed once on the second box")
	}
	var queue struct{ Entries []json.RawMessage }
	g.request(t, g.s, "GET", "/v1/clients/"+g.client.dev.ID()+"/queue", nil, &queue, 200)
	if len(queue.Entries) != 0 {
		t.Fatal("any call was not acked")
	}
}
