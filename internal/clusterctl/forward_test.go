package clusterctl_test

import (
	"crypto/ed25519"
	"testing"

	ctl "github.com/Agent-Field/codeaf/internal/clusterctl"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
)

// R6/R15 bind original application ingress and its fresh read ID to a single
// enrolled scope and direction, independently of the peer-envelope domain.
func TestR6R15ForwardSignaturesBindOriginalRequestAndScope(t *testing.T) {
	key := ed25519.NewKeyFromSeed(make([]byte, ed25519.SeedSize))
	sign := func(b []byte) []byte { return ed25519.Sign(key, b) }
	frame := ctl.Forward{Scope: rafttransport.Scope{Identity: "identity", Cluster: "cluster", Epoch: 1, Protocol: 1}, Generation: 2, From: 1, To: 4, ID: "read-17", Action: "submit", Command: []byte("original signed command")}
	frame.Sign(sign)
	raw, err := rafttransport.Encode(frame)
	if err != nil {
		t.Fatal(err)
	}
	var copy ctl.Forward
	if rafttransport.Decode(raw, &copy) != nil || !copy.Verify(key.Public().(ed25519.PublicKey)) {
		t.Fatal("R6 original frame did not round trip")
	}
	mutations := []func(*ctl.Forward){func(f *ctl.Forward) { f.ID = "read-18" }, func(f *ctl.Forward) { f.Generation++ }, func(f *ctl.Forward) { f.Scope.Cluster = "other" }, func(f *ctl.Forward) { f.From, f.To = f.To, f.From }, func(f *ctl.Forward) { f.Action = "read" }, func(f *ctl.Forward) { f.Command = []byte("replacement command") }, func(f *ctl.Forward) { f.Barrier = 99 }}
	for i, mutate := range mutations {
		changed := frame
		mutate(&changed)
		if changed.Verify(key.Public().(ed25519.PublicKey)) {
			t.Fatalf("R6 substitution %d kept authority", i)
		}
	}
	if ed25519.Verify(key.Public().(ed25519.PublicKey), append([]byte("codeaf/raft/exchange/1\n"), raw...), frame.Signature) {
		t.Fatal("R15 application signature authorized a peer request")
	}
}
