package taskdoor_test

import (
	"encoding/hex"
	"testing"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/taskdoor"
)

// Contract 14 requires a private door derived from the shared identity, with a
// different key for each device so the relay can register both at once.
func TestDoorKeysHaveStableDeviceSpecificNames(t *testing.T) {
	id, err := identity.Unmarshal([]byte(`{"V":1,"signing_seed":"0000000000000000000000000000000000000000000000000000000000000000","dedup_secret":"0000000000000000000000000000000000000000000000000000000000000000","cell_key":"000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"}`))
	if err != nil {
		t.Fatal(err)
	}
	key, err := taskdoor.DoorKey(id, "dev_0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(key.Bytes()); got != "ab6e375a971e40662d79e86158ad7876fb3578b9d6c99b2a74bfb2d9b1989bdd" {
		t.Fatalf("derived private key = %s", got)
	}
	name, err := taskdoor.DoorName(id, "dev_0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	if name != relay.NameFor(key.PublicKey().Bytes()) {
		t.Fatalf("name %q does not register under its public key", name)
	}
	other, err := taskdoor.DoorName(id, "dev_another")
	if err != nil {
		t.Fatal(err)
	}
	if name == other {
		t.Fatal("two device doors have the same name")
	}
}
