package relay

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

// hostedRelayVar names a hosted relay to hold these tests to: `wrangler dev` of
// relay/hosted, or a deployment. Unset, they skip.
const hostedRelayVar = "CODEAF_RELAY_HOSTED_URL"

func hostedRelay(t *testing.T) string {
	t.Helper()
	address := os.Getenv(hostedRelayVar)
	if address == "" {
		t.Skip(hostedRelayVar + " is not set")
	}
	return address
}

// The hosted relay carries a machine the way `codeaf relay` does: a round trip,
// a payload bigger than one frame, and several surfaces on one carrier.
func TestAHostedRelayCarriesAMachine(t *testing.T) {
	address := hostedRelay(t)
	reachAndEcho(t, address)

	registration, err := Register(context.Background(), address, aKey(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = registration.Close() }()
	big := make([]byte, 3*MaxPayload+17)
	_, _ = rand.Read(big)
	for i := 0; i < 3; i++ {
		surface, err := Dial(context.Background(), address, registration.Name())
		if err != nil {
			t.Fatal(err)
		}
		engine, err := registration.Accept()
		if err != nil {
			t.Fatal(err)
		}
		go func() { _, _ = io.Copy(engine, engine) }()
		go func() { _, _ = surface.Write(big) }()
		got := make([]byte, len(big))
		if _, err := io.ReadFull(surface, got); err != nil || !bytes.Equal(got, big) {
			t.Fatalf("surface %d: echo of %d bytes came back wrong: %v", i, len(big), err)
		}
		_ = surface.Close()
	}
}

// Rule three holds on the hosted relay: a live registration is not taken over,
// and once it ends the name is free again at once.
func TestAHostedRelayKeepsANameForItsLiveMachine(t *testing.T) {
	address := hostedRelay(t)
	key := aKey(t)
	first, err := Register(context.Background(), address, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Register(context.Background(), address, key); !errors.Is(err, ErrNameTaken) {
		t.Fatalf("a second registration under a live name gave %v; want ErrNameTaken", err)
	}
	_ = first.Close()
	var again *Registration
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(200 * time.Millisecond) {
		if again, err = Register(context.Background(), address, key); err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("the name was not free after its machine left: %v", err)
	}
	_ = again.Close()

	if _, err := Dial(context.Background(), address, NameFor(aKey(t).PublicKey().Bytes())); !errors.Is(err, ErrNoMachine) {
		t.Fatalf("dialling a name nothing holds gave %v; want ErrNoMachine", err)
	}
}
