package raftabi

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/clusterctl"
	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	rt "github.com/Agent-Field/codeaf/internal/rafttransport"
)

// R6 preserves the signed application wire across native and hosted routing.
func TestR6HostedForwardUsesOriginalSignedWire(t *testing.T) {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{31}, 32))
	work := clusterctl.Forward{Scope: rt.Scope{Identity: "identity", Cluster: "cluster", Epoch: 1, Protocol: 1}, Generation: 9, From: 1, To: 4, ID: "request-one", Action: "submit", Command: []byte("original signed command")}
	raw, err := cs.Encode(forwardInput{Scope: peerScopeState{"identity", "cluster", 1, 1}, Generation: 9, From: 1, To: 4, ID: work.ID, Action: work.Action, Command: work.Command})
	if err != nil {
		t.Fatal(err)
	}
	f := request(39)
	f.u32(0)
	f.blob(raw)
	f.blob(key.Seed())
	d := answer(t, New().Call(f.b))
	signed := d.blob()
	const unsigned = `{"scope":{"identity":"identity","cluster":"cluster","epoch":"1","protocol":1},"generation":"9","from":"1","to":"4","id":"request-one","action":"submit","command":"b3JpZ2luYWwgc2lnbmVkIGNvbW1hbmQ=","status":{"Leader":0,"Term":0,"Applied":0},"signature":null}`
	var signature struct {
		Signature []byte `json:"signature"`
	}
	if json.Unmarshal(signed, &signature) != nil || !ed25519.Verify(key.Public().(ed25519.PublicKey), append([]byte("codeaf/cluster/forward/1\n"), []byte(unsigned)...), signature.Signature) {
		t.Fatal("R6 hosted frame changed the established native signed encoding")
	}

	var got clusterctl.Forward
	if rt.Decode(signed, &got) != nil || !got.Verify(key.Public().(ed25519.PublicKey)) || !bytes.Equal(got.Command, work.Command) || got.ID != work.ID {
		t.Fatal("R6 hosted signing changed the original request")
	}
	f = request(39)
	f.u32(1)
	f.blob(signed)
	f.blob(key.Public().(ed25519.PublicKey))
	d = answer(t, New().Call(f.b))
	if d.u32() != 1 {
		t.Fatal("R6 signed reply was refused")
	}
	got.Barrier++
	raw, _ = rt.Encode(got)
	f = request(39)
	f.u32(1)
	f.blob(raw)
	f.blob(key.Public().(ed25519.PublicKey))
	d = answer(t, New().Call(f.b))
	if d.u32() != 0 {
		t.Fatal("R6 altered barrier was accepted")
	}
}
