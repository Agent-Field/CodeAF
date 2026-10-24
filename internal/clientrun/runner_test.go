package clientrun_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/clientrun"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relayserve"
	"github.com/Agent-Field/codeaf/internal/reqsign"
	"golang.org/x/crypto/nacl/box"
)

type signer struct {
	id  identity.Identity
	dev identity.Dev
}

func (s signer) IdentityKey() ed25519.PublicKey { return s.id.PublicKey() }
func (s signer) Cert() identity.Cert            { return s.dev.Cert }
func (s signer) Sign(p []byte) []byte           { return s.dev.Sign(p) }

type engine struct {
	answers int
	appwire.Engine
	reads int
	asks  []appwire.Ask
}

func (e *engine) ListAsks(agent string) ([]appwire.Ask, error) { e.reads++; return e.asks, nil }
func (e *engine) ListAgents(string) ([]appwire.Agent, error) {
	return []appwire.Agent{{ID: chat}, {ID: otherChat}}, nil
}

const chat = "01J0000000000000000000000A"
const otherChat = "01J0000000000000000000000B"

type rig struct {
	r         *clientrun.Runner
	e         *engine
	s         signer
	client    signer
	pub, priv *[32]byte
	url       string
	now       time.Time
	dir       directory.Client
	state     string
}

func newRig(t *testing.T) *rig {
	t.Helper()
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	relay := relayserve.New(relayserve.Config{Store: t.TempDir(), Now: func() time.Time { return now }})
	srv := httptest.NewServer(relay.Handler)
	t.Cleanup(func() { srv.Close(); _ = relay.Close() })
	home := t.TempDir()
	id, err := identity.Ensure(home)
	if err != nil {
		t.Fatal(err)
	}
	dev, err := identity.Device(home)
	if err != nil {
		t.Fatal(err)
	}
	s := signer{id, dev}
	dir := directory.NewHTTP(srv.URL, reqsign.SignFor(s, func() time.Time { return now }), srv.Client())
	if err := dir.PutDevice(context.Background(), dev.ID(), directory.Device{V: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := dir.Create(context.Background(), chat, directory.CellInit{Head: "1111111111111111111111111111111111111111111111111111111111111111", Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	seed, err := identity.NewSeed()
	if err != nil {
		t.Fatal(err)
	}
	c, err := seed.Join(identity.IssueCert(id, seed.Public(), now))
	if err != nil {
		t.Fatal(err)
	}
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	e := &engine{}
	state := t.TempDir()
	r, err := clientrun.Open(clientrun.Config{Home: state, Relay: srv.URL, Signer: s, Directory: dir, Engine: e, Log: func(clientrun.Receipt) error { return nil }, Now: func() time.Time { return now }, HTTP: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	return &rig{r: r, e: e, s: s, client: signer{id, c}, pub: pub, priv: priv, url: srv.URL, now: now, dir: dir, state: state}
}
func (g *rig) pair(t *testing.T, methods ...string) clientrun.Grant {
	t.Helper()
	grant, err := g.r.Pair(context.Background(), clientrun.Pairing{Cert: g.client.Cert(), BoxKey: g.pub[:], Methods: methods, Reads: []string{"asks.list"}})
	if err != nil {
		t.Fatal(err)
	}
	return grant
}
func TestNoPairedClientDoesNoWork(t *testing.T) {
	r, err := clientrun.Open(clientrun.Config{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestSlotsUseDesktopDispatchAndKeepEmptyChatScope(t *testing.T) {
	g := newRig(t)
	g.pair(t, "asks.answer")
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	var slot struct{ Sealed []byte }
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+clientrun.SlotName("asks.list", g.s.dev.ID()), nil, &slot, 200)
	plain, ok := box.OpenAnonymous(nil, slot.Sealed, g.pub, g.priv)
	if !ok {
		t.Fatal("slot not sealed to client")
	}
	var envelopes []struct {
		Writer string
		At     time.Time
		Agent  string
		Result appwire.AsksResult
	}
	if err := json.Unmarshal(plain, &envelopes); err != nil {
		t.Fatal(err)
	}
	if len(envelopes) != 1 || envelopes[0].Agent != chat || envelopes[0].Writer != g.s.dev.ID() || !envelopes[0].At.Equal(g.now) || envelopes[0].Result.Asks == nil {
		t.Fatalf("slot: %s", plain)
	}
	// An unchanged result keeps its publication time even after another step.
	first := string(slot.Sealed)
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+clientrun.SlotName("asks.list", g.s.dev.ID()), nil, &slot, 200)
	if first != string(slot.Sealed) {
		t.Fatal("unchanged slot was resealed")
	}
}

func TestSnapshotsOnlyContainLeasedChats(t *testing.T) {
	g := newRig(t)
	_, err := g.r.Pair(context.Background(), clientrun.Pairing{Cert: g.client.Cert(), BoxKey: g.pub[:], Reads: []string{"agents.list", "asks.list"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	var slot struct{ Sealed []byte }
	g.request(t, g.client, "GET", "/v1/clients/"+g.client.dev.ID()+"/slots/"+clientrun.SlotName("agents.list", g.s.dev.ID()), nil, &slot, 200)
	plain, ok := box.OpenAnonymous(nil, slot.Sealed, g.pub, g.priv)
	if !ok {
		t.Fatal("cannot decrypt")
	}
	var got []struct {
		Agent  string
		Result appwire.AgentsResult
	}
	if err := json.Unmarshal(plain, &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Agent != chat || len(got[0].Result.Agents) != 1 || got[0].Result.Agents[0].ID != chat {
		t.Fatalf("unleased chat leaked: %s", plain)
	}
	if err := g.dir.Release(context.Background(), chat, 1); err != nil {
		t.Fatal(err)
	}
	reads := g.e.reads
	if err := g.r.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if g.e.reads != reads {
		t.Fatal("read a chat after releasing its lease")
	}
}
