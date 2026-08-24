package pair

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/relay"
	"golang.org/x/crypto/curve25519"
)

// ── the key ─────────────────────────────────────────────────────────────────

// TWO LIBRARIES AGREEING ABOUT X25519 IS A THING TO ASSERT, NEVER TO ASSUME.
// The relay's registration challenge is answered with crypto/ecdh and the
// tunnel's handshake is done with flynn/noise over golang.org/x/crypto; if the
// two ever disagreed about which public key a private key has, a machine would
// register under one name and be unable to complete a handshake under it.
func TestTheTwoLibrariesAgreeAboutThisDevicesPublicKey(t *testing.T) {
	for i := 0; i < 20; i++ {
		private, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		theirs, err := curve25519.X25519(private.Bytes(), curve25519.Basepoint)
		if err != nil {
			t.Fatal(err)
		}
		if string(theirs) != string(private.PublicKey().Bytes()) {
			t.Fatal("crypto/ecdh and x/crypto/curve25519 disagree about a public key")
		}
	}
}

func TestADeviceKeyIsMadeOnceAndKeptForEver(t *testing.T) {
	keeper := FileKeeper{Path: filepath.Join(t.TempDir(), "device.key")}
	first, err := ThisDevice(keeper)
	if err != nil {
		t.Fatal(err)
	}
	again, err := ThisDevice(keeper)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.Public()) != string(again.Public()) {
		t.Fatal("opening the device twice made two keys")
	}
	if first.Name() != relay.NameFor(first.Public()) {
		t.Fatal("a device's name is not the one its key derives")
	}
	info, err := os.Stat(keeper.Path)
	if err != nil {
		t.Fatal(err)
	}
	// A KEY THAT WAS EVER WORLD-READABLE IS A KEY THAT HAS TO BE ASSUMED READ.
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("the device key file is %o, not 0600", mode)
	}
}

// The seam is honest about itself: today it is a file and it says so.
func TestTheKeeperSaysWhereTheKeyActuallyIs(t *testing.T) {
	where := FileKeeper{Path: "/somewhere/device.key"}.Where()
	if !strings.Contains(where, "a file on this machine") {
		t.Fatalf("the keeper describes itself as %q, which does not say it is a file", where)
	}
	if strings.Contains(strings.ToLower(where), "keychain") || strings.Contains(strings.ToLower(where), "touch id") {
		t.Fatalf("the keeper claims %q, and this build has neither", where)
	}
}

// ── the code ────────────────────────────────────────────────────────────────

func TestAPairingCodeIsSixDigitsShownWithASpace(t *testing.T) {
	code, err := NewCode(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(code.secret()) != 6 {
		t.Fatalf("the code is %q", code.secret())
	}
	shown := code.Shown()
	if len(shown) != 7 || shown[3] != ' ' {
		t.Fatalf("the code is shown as %q", shown)
	}
}

func TestACodeIsReadWithOrWithoutItsSeparator(t *testing.T) {
	for _, typed := range []string{"715302", "715 302", "715-302", " 715 302 "} {
		got, err := ReadCode(typed)
		if err != nil {
			t.Fatalf("%q: %v", typed, err)
		}
		if got != "715302" {
			t.Fatalf("%q read as %q", typed, got)
		}
	}
	for _, typed := range []string{"", "71530", "7153021", "abc def", "715.302"} {
		if _, err := ReadCode(typed); err == nil {
			t.Fatalf("%q was accepted as a pairing code", typed)
		}
	}
}

// The guess budget is the other half of what makes six digits enough.
func TestACodeIsThrownAwayAfterItsAttemptsAreSpent(t *testing.T) {
	now := time.Now()
	desk := &Desk{Now: func() time.Time { return now }}
	code, err := desk.Offer()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < CodeAttempts; i++ {
		got, err := desk.Spend()
		if err != nil {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
		if got != code.secret() {
			t.Fatal("the desk handed out a different code mid-budget")
		}
	}
	if _, err := desk.Spend(); err == nil {
		t.Fatalf("a code survived %d attempts", CodeAttempts+1)
	}
}

func TestACodeExpires(t *testing.T) {
	now := time.Now()
	desk := &Desk{Now: func() time.Time { return now }}
	first, err := desk.Offer()
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(CodeValidFor + time.Second)
	if _, err := desk.Spend(); err == nil {
		t.Fatal("an expired code was still spendable")
	}
	next, err := desk.Offer()
	if err != nil {
		t.Fatal(err)
	}
	if next.secret() == first.secret() {
		t.Fatal("the desk offered the expired code again")
	}
}

// The line `aforge serve` prints, exactly as the design spells it, with the
// validity interpolated rather than typed.
func TestTheServeLinesReadTheWayTheyWereDesigned(t *testing.T) {
	code := &Code{digits: "715302"}
	got := Lines("otter-lamp-42", code)
	want := "  this machine is reachable as  otter-lamp-42\n  pair a new device with code   715 302   (valid 10 minutes)\n"
	if got != want {
		t.Fatalf("serve prints\n%q\nand should print\n%q", got, want)
	}
}

// ── pairing ─────────────────────────────────────────────────────────────────

func aDevice(t *testing.T) Device {
	t.Helper()
	device, err := ThisDevice(FileKeeper{Path: filepath.Join(t.TempDir(), "device.key")})
	if err != nil {
		t.Fatal(err)
	}
	return device
}

func TestPairingCarriesBothKeysPastTheMiddle(t *testing.T) {
	surfaceEnd, machineEnd := net.Pipe()
	machine := aDevice(t)
	surface := aDevice(t)
	now := time.Now()

	admitted := make(chan Paired, 1)
	failed := make(chan error, 1)
	go func() {
		var intent [1]byte
		if _, err := io.ReadFull(machineEnd, intent[:]); err != nil {
			failed <- err
			return
		}
		one, err := pairAsMachine(machineEnd, machine.Name(), "715302", machine, now)
		if err != nil {
			failed <- err
			return
		}
		admitted <- one
	}()

	learned, err := pairAsSurface(surfaceEnd, "http://relay", machine.Name(), "715302", "laptop", surface, now)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-failed:
		t.Fatal(err)
	case one := <-admitted:
		if one.Label != "laptop" {
			t.Fatalf("the machine wrote the device down as %q", one.Label)
		}
		key, err := decodeStoredKey(one.Key)
		if err != nil {
			t.Fatal(err)
		}
		if string(key) != string(surface.Public()) {
			t.Fatal("the machine pinned a key that is not the device's")
		}
	}
	key, err := decodeStoredKey(learned.Key)
	if err != nil {
		t.Fatal(err)
	}
	if string(key) != string(machine.Public()) {
		t.Fatal("the device pinned a key that is not the machine's")
	}
	if learned.Name != machine.Name() {
		t.Fatal("the device wrote down the wrong name")
	}
}

// A WRONG CODE PAIRS NOTHING, and it fails as a refusal rather than as a
// connection that quietly means something different.
func TestTheWrongCodePairsNothing(t *testing.T) {
	surfaceEnd, machineEnd := net.Pipe()
	machine := aDevice(t)
	surface := aDevice(t)
	now := time.Now()

	machineSaid := make(chan error, 1)
	go func() {
		var intent [1]byte
		_, _ = io.ReadFull(machineEnd, intent[:])
		_, err := pairAsMachine(machineEnd, machine.Name(), "715302", machine, now)
		machineSaid <- err
		_ = machineEnd.Close()
	}()

	_, err := pairAsSurface(surfaceEnd, "http://relay", machine.Name(), "000000", "laptop", surface, now)
	if !errors.Is(err, ErrWrongCode) {
		t.Fatalf("a wrong code gave %v", err)
	}
	if err := <-machineSaid; !errors.Is(err, ErrWrongCode) {
		t.Fatalf("the machine read a wrong code as %v", err)
	}
}

// ── the tunnel ──────────────────────────────────────────────────────────────

func TestAPairedDeviceOpensATunnelAndAnUnknownOneDoesNot(t *testing.T) {
	machine := aDevice(t)
	surface := aDevice(t)

	t.Run("paired", func(t *testing.T) {
		surfaceEnd, machineEnd := net.Pipe()
		opened := make(chan *Tunnel, 1)
		go func() {
			var intent [1]byte
			_, _ = io.ReadFull(machineEnd, intent[:])
			tunnel, _, err := acceptAsMachine(machineEnd, machine, func(key []byte, label string) error {
				if string(key) != string(surface.Public()) {
					return errors.New(Stopped())
				}
				return nil
			})
			if err != nil {
				t.Error(err)
				opened <- nil
				return
			}
			opened <- tunnel
		}()
		here, err := connectAsSurface(surfaceEnd, machine.Public(), surface, "laptop")
		if err != nil {
			t.Fatal(err)
		}
		there := <-opened
		if there == nil {
			t.Fatal("the machine did not open a tunnel")
		}
		// The write happens on its own goroutine because net.Pipe is
		// synchronous: a write blocks until somebody reads it, which is exactly
		// what a real connection does not do and exactly what makes this pipe
		// worth testing against.
		go func() {
			if _, err := here.Write([]byte("a message the relay must never see")); err != nil {
				t.Error(err)
			}
		}()
		got := make([]byte, len("a message the relay must never see"))
		if _, err := io.ReadFull(there, got); err != nil {
			t.Fatal(err)
		}
		if string(got) != "a message the relay must never see" {
			t.Fatalf("the machine read %q", got)
		}
		if string(there.Peer()) != string(surface.Public()) {
			t.Fatal("the machine's tunnel names the wrong peer")
		}
	})

	t.Run("stopped", func(t *testing.T) {
		surfaceEnd, machineEnd := net.Pipe()
		go func() {
			var intent [1]byte
			_, _ = io.ReadFull(machineEnd, intent[:])
			_, _, _ = acceptAsMachine(machineEnd, machine, func(key []byte, label string) error {
				return errors.New(Stopped())
			})
			_ = machineEnd.Close()
		}()
		_, err := connectAsSurface(surfaceEnd, machine.Public(), surface, "laptop")
		if err == nil {
			t.Fatal("a stopped device opened a tunnel")
		}
		if !strings.Contains(err.Error(), "has been stopped") {
			t.Fatalf("a stopped device was told %q", err)
		}
	})
}

// A SURFACE DIALLING THE WRONG MACHINE SENDS NOTHING. The pinned key is what
// makes the relay untrusted, and this is that property in one test.
func TestASurfaceRefusesAMachineThatIsNotTheOneItPinned(t *testing.T) {
	real := aDevice(t)
	impostor := aDevice(t)
	surface := aDevice(t)

	surfaceEnd, machineEnd := net.Pipe()
	go func() {
		var intent [1]byte
		_, _ = io.ReadFull(machineEnd, intent[:])
		_, _, _ = acceptAsMachine(machineEnd, impostor, func([]byte, string) error { return nil })
		_ = machineEnd.Close()
	}()
	// AN IMPOSTOR CANNOT EVEN READ THE FIRST MESSAGE, so what this device sees
	// is silence — and the sentence it prints covers exactly that.
	_, err := connectAsSurface(surfaceEnd, real.Public(), surface, "laptop")
	if !errors.Is(err, ErrNoAnswer) {
		t.Fatalf("dialling an impostor gave %v", err)
	}
	said := NoAnswer("otter-lamp-42").Error()
	if !strings.Contains(said, "nothing was sent") {
		t.Fatalf("the sentence does not say the conversation stayed here: %q", said)
	}
}

// ── the four honest failures ────────────────────────────────────────────────

// EACH ONE NAMES A DIFFERENT THING AND A DIFFERENT NEXT STEP. A shrug that
// covered all four would send somebody to check their wifi when the answer was
// `aforge serve`.
func TestEveryWayThisFailsSaysWhichWayItFailed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("AFORGE_HOME", home)
	t.Setenv(RelayEnv, "")

	reach := Reach{
		Name:     "otter-lamp-42",
		Device:   aDevice(t),
		Machines: BookAt(filepath.Join(home, "machines.json")),
		Label:    "laptop",
	}

	t.Run("no relay", func(t *testing.T) {
		_, err := reach.Open(context.Background())
		if err == nil {
			t.Fatal("--at opened with no relay set up")
		}
		if !strings.Contains(err.Error(), "no relay is set up on this machine") {
			t.Fatalf("with no relay, --at said %q", err)
		}
		if !strings.Contains(err.Error(), "--host over ssh") {
			t.Fatalf("the sentence does not name the road that does work: %q", err)
		}
	})

	// A door that CAN ask for a code gets past the not-paired refusal and meets
	// the relay, which is where the next two facts live.
	asking := reach
	asking.AskCode = func(string) (string, error) { return "715 302", nil }

	t.Run("cannot reach the relay", func(t *testing.T) {
		t.Setenv(RelayEnv, "http://127.0.0.1:1")
		_, err := asking.Open(context.Background())
		if err == nil || !strings.Contains(err.Error(), "cannot be reached from here") {
			t.Fatalf("with an unreachable relay, --at said %v", err)
		}
	})

	t.Run("that machine is not connected", func(t *testing.T) {
		_, address := liveRelay(t)
		t.Setenv(RelayEnv, address)
		_, err := asking.Open(context.Background())
		if err == nil || !strings.Contains(err.Error(), "is not connected to the relay right now") {
			t.Fatalf("with nothing registered, --at said %v", err)
		}
		if !strings.Contains(err.Error(), "aforge serve") {
			t.Fatalf("the sentence does not say what to do: %q", err)
		}
	})

	t.Run("this device is not paired", func(t *testing.T) {
		_, address := liveRelay(t)
		t.Setenv(RelayEnv, address)
		machine := aDevice(t)
		registration, err := relay.Register(context.Background(), address, machine.Private())
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = registration.Close() }()

		// AskCode is nil, which is a door that cannot ask — so an unpaired
		// machine is refused rather than hung on a prompt nobody can answer.
		unpaired := reach
		unpaired.Name = machine.Name()
		_, err = unpaired.Open(context.Background())
		if err == nil || !strings.Contains(err.Error(), "is not paired with") {
			t.Fatalf("with no pairing, --at said %v", err)
		}
	})

	t.Run("not a name at all", func(t *testing.T) {
		wrong := reach
		wrong.Name = "devbox"
		_, err := wrong.Open(context.Background())
		if err == nil || !strings.Contains(err.Error(), "otter-lamp-42") {
			t.Fatalf("a name of the wrong shape said %v", err)
		}
	})
}

// The weight of a pairing is stated before the code is typed, in a person's
// words rather than in security vocabulary.
func TestThePairingSaysWhatAPairedDeviceCanDoBeforeTheCodeIsTyped(t *testing.T) {
	if !strings.Contains(PairingWeight, "ssh key") {
		t.Fatalf("the pairing line does not name the weight: %q", PairingWeight)
	}
	for _, machinery := range []string{"auditor", "verdict", "verified", "refuted", "attestation", "principal"} {
		if strings.Contains(strings.ToLower(PairingWeight), machinery) {
			t.Fatalf("the pairing line uses machinery vocabulary: %q", PairingWeight)
		}
	}
}
