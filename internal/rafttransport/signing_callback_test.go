package rafttransport

import (
	"crypto/ed25519"
	"reflect"
	"testing"

	pb "go.etcd.io/raft/v3/raftpb"
)

// R23 keeps the canonical authenticated bytes unchanged when a personal device
// signs through its existing callback instead of exporting its private seed.
func TestR23DeviceSigningCallbackKeepsPeerWire(t *testing.T) {
	key := key(1)
	callback := func(b []byte) []byte { return ed25519.Sign(key, b) }
	original := message(t, 1, 2, 1, pb.MsgApp, 0)
	actual := original
	actual.Signature = nil
	must(t, actual.SignWith(callback))
	if !reflect.DeepEqual(actual, original) {
		t.Fatal("R23 callback changed canonical envelope")
	}
	originalRequest := request(t, 1, original)
	actualRequest := originalRequest
	actualRequest.Signature = nil
	must(t, actualRequest.SignWith(callback))
	if !reflect.DeepEqual(actualRequest, originalRequest) {
		t.Fatal("R23 callback changed canonical request")
	}
	for _, sign := range []func([]byte) []byte{nil, func([]byte) []byte { return nil }} {
		if actual.SignWith(sign) != ErrAuth || actualRequest.SignWith(sign) != ErrAuth {
			t.Fatal("R23 empty signer admitted")
		}
	}
}
