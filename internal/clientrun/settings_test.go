package clientrun_test

import (
	"context"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"testing"
)

func TestPausedClientDoesNoReadsAndUnpairRevokesItsKey(t *testing.T) {
	g := newRig(t)
	g.pair(t, "asks.answer")
	if err := g.r.Pause(g.client.dev.ID(), true); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.e.reads != 0 {
		t.Fatal("paused client read snapshots")
	}
	list := g.r.Clients()
	if len(list) != 1 || !list[0].Paused {
		t.Fatalf("clients: %+v", list)
	}
	if err := g.r.Pause(g.client.dev.ID(), false); err != nil {
		t.Fatal(err)
	}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.e.reads != 1 {
		t.Fatal("resumed client did not read")
	}
	if err := g.r.Unpair(context.Background(), g.client.dev.ID()); err != nil {
		t.Fatal(err)
	}
	if len(g.r.Clients()) != 0 {
		t.Fatal("unpaired client still listed")
	}
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/asks.list", nil, nil, 401)
	g.reopen(t, nil, noopLog)
	if len(g.r.Clients()) != 0 {
		t.Fatal("restart restored revoked pairing")
	}
}

func TestPairingRejectsAnUnusableEncryptionKey(t *testing.T) {
	g := newRig(t)
	_, err := g.r.Pair(context.Background(), clientrun.Pairing{Cert: g.client.Cert(), BoxKey: make([]byte, 32), Reads: []string{"asks.list"}})
	if err == nil {
		t.Fatal("paired a key that cannot receive encrypted slots")
	}
}
