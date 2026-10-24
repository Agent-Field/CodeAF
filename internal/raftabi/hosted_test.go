package raftabi

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"testing"

	cs "github.com/Agent-Field/codeaf/internal/clusterstate"
)

// R15/R16/R25 keep effect keys out of commands, preserve opaque map values and
// check the committed certificate/revocation/rotation at each authorization.
func TestR15R16R25HostedBoundary(t *testing.T) {
	runtime := New()
	seed := bytes.Repeat([]byte{17}, 32)
	key := ed25519.NewKeyFromSeed(seed)
	e := request(30)
	e.str("hosted")
	e.u64(1000)
	e.blob(seed)
	e.blob([]byte(`{"device_seen":{"device":"a"}}`))
	d := answer(t, runtime.Call(e.b))
	raw := d.blob()
	if d.end() != nil {
		t.Fatal("trailing effect")
	}
	command, err := cs.DecodeCommand(raw)
	if err != nil {
		t.Fatal(err)
	}
	signed, _ := cs.SigningBytes(command)
	if !ed25519.Verify(key.Public().(ed25519.PublicKey), signed, command.Signature) || bytes.Contains(raw, []byte(base64.StdEncoding.EncodeToString(seed))) {
		t.Fatal("R15 effect key boundary")
	}
	bad := request(30)
	bad.str("hosted")
	bad.u64(1000)
	bad.blob(seed)
	bad.blob([]byte(`{"revoke":{"device":"a"}}`))
	r := decoder{b: runtime.Call(bad.b)}
	r.u32()
	if r.str() == "" {
		t.Fatal("effect signer admitted owner mutation")
	}
	rootKey := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{33}, 32))
	pub := key.Public().(ed25519.PublicKey)
	actor := cs.DeviceIDOfKey(pub)
	cert := struct {
		V       uint16 `json:"V"`
		Device  string `json:"device"`
		Created int64  `json:"created"`
		Sig     string `json:"sig"`
	}{1, hex.EncodeToString(pub), 1, ""}
	certRaw, _ := json.Marshal(cert)
	cert.Sig = hex.EncodeToString(ed25519.Sign(rootKey, append([]byte("codeaf/device-cert/v1\n"), certRaw...)))
	certRaw, _ = json.Marshal(cert)
	identity := cs.IdentityRec{V: 1, Identity: cs.IdentityID(rootKey.Public().(ed25519.PublicKey)), SigningKey: rootKey.Public().(ed25519.PublicKey)}
	for _, test := range []struct {
		revoked, write, frozen bool
		want                   cs.Code
	}{{false, false, false, cs.OK}, {true, false, false, cs.Revoked}, {false, true, true, cs.Rotated}, {false, false, true, cs.OK}} {
		if test.frozen {
			identity.Rotation = &cs.Rotation{State: "frozen", By: actor, At: 1000}
		} else {
			identity.Rotation = nil
		}
		root, _ := cs.Encode(identity)
		device, _ := cs.Encode(cs.Device{V: 1, Revoked: test.revoked})
		e = request(32)
		e.blob(root)
		e.blob(device)
		e.str(actor)
		e.str(base64.RawURLEncoding.EncodeToString(certRaw))
		e.flag(test.write)
		if got := answer(t, runtime.Call(e.b)).str(); got != string(test.want) {
			t.Fatalf("R16 got %s want %s", got, test.want)
		}
	}
	cell := cs.Cell{V: 1, Size: 7, Lease: cs.Lease{Fence: 9}, Keys: map[string]map[string]string{"fence": {"size": "opaque"}}}
	raw, _ = cs.Encode(cell)
	e = request(31)
	e.str("cell")
	e.blob(raw)
	projected := answer(t, runtime.Call(e.b)).blob()
	var v cs.Cell
	if json.Unmarshal(projected, &v) != nil || v.Lease.Fence != 9 || v.Keys["fence"]["size"] != "opaque" {
		t.Fatal("R25 typed v1 projection changed sealed bytes")
	}
}
