package raftabi

import (
	"bytes"
	"crypto/ed25519"
	"encoding/json"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
	rt "github.com/Agent-Field/codeaf/internal/rafttransport"
)

// R23 preserves the native authenticated wire, including signed applied and
// decimal cursors. Alternate JSON and hostile inner messages fail closed.
func TestR23HostedPeerWireMatchesNative(t *testing.T) {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{21}, 32))
	pub := key.Public().(ed25519.PublicKey)
	scope := rt.Scope{Identity: "identity", Cluster: "cluster", Epoch: 1, Protocol: 1}
	req := rt.Request{Scope: scope, Node: 4, Incarnation: 1, Generation: 7, Applied: 20}
	if e := req.Sign(key); e != nil {
		t.Fatal(e)
	}
	raw, _ := rt.Encode(req)
	var copied peerRequest
	if e := peerDecode(raw, &copied); e != nil {
		t.Fatal(e)
	}
	encoded, _ := peerEncode(copied)
	if !bytes.Equal(encoded, raw) {
		t.Fatal("R23 request wire differs")
	}
	trusted := peerView{Scope: peerScopeState{"identity", "cluster", 1, 1}, Generation: 7, Applied: 20, Members: []cs.PeerEnrollment{{Node: 1, Incarnation: 1, Key: pub, Voter: true}, {Node: 4, Incarnation: 1, Key: pub}}}
	runtime := New()
	check := func(raw []byte, want string) {
		t.Helper()
		view, e := cs.Encode(trusted)
		if e != nil {
			t.Fatal(e)
		}
		f := request(37)
		f.blob(raw)
		f.blob(view)
		d := answer(t, runtime.Call(f.b))
		var out struct {
			Code     string   `json:"code"`
			Obsolete []string `json:"obsolete"`
		}
		if json.Unmarshal(d.blob(), &out) != nil || out.Code != want {
			t.Fatalf("R23 got %+v want %s", out, want)
		}
	}
	check(raw, "ok")
	check(append(bytes.Clone(raw), ' '), "invalid")
	trusted.Generation++
	check(raw, "generation")
	trusted.Generation--
	trusted.Members[1].Revoked = true
	check(raw, "unauthorized")
	trusted.Members[1].Revoked = false
	req.Applied = 21
	_ = req.Sign(key)
	raw, _ = rt.Encode(req)
	check(raw, "invalid")
	req.Applied = 20
	for _, kind := range []byte{0, 1, 2, 11, 15, 16, 19, 20, 21, 22} {
		envelope := rt.Envelope{Scope: scope, From: 4, To: 1, Incarnation: 1, Destination: 1, Sequence: 1, Payload: []byte{8, kind, 16, 1, 24, 4, 32, 1}}
		envelope.Digest = peerDigest(envelope.Payload)
		b, _ := rt.Encode(envelope)
		envelope.Signature = ed25519.Sign(key, append([]byte("codeaf/raft/envelope/1\n"), b...))
		req.Messages = []rt.Envelope{envelope}
		if e := req.Sign(key); e != nil {
			t.Fatal(e)
		}
		raw, _ = rt.Encode(req)
		check(raw, "invalid")
	}
}

// R23 keeps response fields byte-compatible with the unchanged transport client.
func TestR23HostedPeerResponseMatchesNative(t *testing.T) {
	native := rt.Response{Generation: 7, Messages: []rt.Delivery{}, Accepted: []string{"4"}, Rejected: []string{"5"}, Chunks: []rt.ChunkAck{{Node: 4, Sequence: 9}}}
	raw, _ := rt.Encode(native)
	f := request(38)
	f.u32(1)
	f.blob(raw)
	d := answer(t, New().Call(f.b))
	got := d.blob()
	if !bytes.Equal(raw, got) {
		t.Fatalf("R23 response differs: %s != %s", got, raw)
	}
}

// R9/R12 personal certificates can enroll before any optional device-name row.
func TestR9HostedPersonalPeerWithoutDeviceMetadata(t *testing.T) {
	key := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{8}, 32))
	h := cs.Membership{Revision: 2, Index: 10, NextNode: 5, Anchors: []uint64{1, 2, 3}, Nodes: []uint64{1, 2, 3, 4}, Live: 1, Limit: 4096, Pending: &cs.MembershipIntent{Node: 4, Revision: 2}}
	m := cs.MemberRecord{Node: 4, Device: "personal", Key: key.Public().(ed25519.PublicKey), Incarnation: 1, Protocol: 1}
	header, _ := cs.Encode(h)
	member, _ := cs.Encode(m)
	f := request(16)
	f.u64(4)
	f.u64(10)
	f.blob(nil)
	f.blob(header)
	f.blob(member)
	f.blob(nil)
	d := answer(t, New().Call(f.b))
	var peer cs.PeerEnrollment
	if cs.Decode(d.blob(), &peer) != nil || peer.Node != 4 || peer.Revoked || peer.Voter {
		t.Fatal("R9 personal peer without optional metadata was refused")
	}
}

// R9 keeps capacity and protocol refusals identical across relay runtimes.
func TestR9HostedEnrollmentRefusalSentences(t *testing.T) {
	for _, code := range []cs.Code{cs.Capacity, cs.UpdateRequired} {
		f := request(38)
		f.u32(2)
		f.str(string(code))
		d := answer(t, New().Call(f.b))
		if d.str() != cs.EnrollmentMessage(code) || d.end() != nil {
			t.Fatal("R9 refusal differs from shared admission policy")
		}
	}
}
