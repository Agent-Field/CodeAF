package relay

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// live starts a relay and hands back its address, closing it when the test ends.
func live(t *testing.T) (*Server, string) {
	t.Helper()
	service := &Server{}
	server := httptest.NewServer(service)
	t.Cleanup(server.Close)
	t.Cleanup(server.CloseClientConnections)
	return service, server.URL
}

func aKey(t *testing.T) *ecdh.PrivateKey {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// The whole service in one test: a machine walks out to the relay, a surface
// dials the name it registered under, and bytes cross in both directions.
func TestAMachineRegistersAndASurfaceReachesIt(t *testing.T) {
	_, address := live(t)
	key := aKey(t)

	registration, err := Register(context.Background(), address, key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = registration.Close() }()
	if registration.Name() != NameFor(key.PublicKey().Bytes()) {
		t.Fatalf("registered as %q, which is not the name that key derives", registration.Name())
	}

	surface, err := Dial(context.Background(), address, registration.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = surface.Close() }()

	engine, err := registration.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()

	if _, err := surface.Write([]byte("out")); err != nil {
		t.Fatal(err)
	}
	if got := readN(t, engine, 3); got != "out" {
		t.Fatalf("the machine read %q", got)
	}
	if _, err := engine.Write([]byte("back")); err != nil {
		t.Fatal(err)
	}
	if got := readN(t, surface, 4); got != "back" {
		t.Fatalf("the surface read %q", got)
	}
}

// Several surfaces on one machine, which is the reason the carrier is
// multiplexed at all: a desk and a phone attached at once.
func TestSeveralSurfacesShareOneCarrier(t *testing.T) {
	_, address := live(t)
	registration, err := Register(context.Background(), address, aKey(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = registration.Close() }()

	const many = 4
	var wait sync.WaitGroup
	for i := 0; i < many; i++ {
		wait.Add(1)
		go func(i int) {
			defer wait.Done()
			engine, err := registration.Accept()
			if err != nil {
				t.Error(err)
				return
			}
			defer func() { _ = engine.Close() }()
			word := make([]byte, 8)
			if _, err := io.ReadFull(engine, word); err != nil {
				t.Error(err)
				return
			}
			if _, err := engine.Write(word); err != nil {
				t.Error(err)
			}
		}(i)
	}
	for i := 0; i < many; i++ {
		surface, err := Dial(context.Background(), address, registration.Name())
		if err != nil {
			t.Fatal(err)
		}
		word := fmt.Sprintf("surface%d", i)
		if _, err := surface.Write([]byte(word)); err != nil {
			t.Fatal(err)
		}
		if got := readN(t, surface, len(word)); got != word {
			t.Fatalf("surface %d got %q back", i, got)
		}
		_ = surface.Close()
	}
	wait.Wait()
}

// RULE ONE of the name policy: a name must agree with the key claiming it.
func TestANameThatDoesNotMatchTheKeyIsRefused(t *testing.T) {
	_, address := live(t)
	other := aKey(t)
	// Register by hand so the wrong name can be put in the header at all.
	conn, err := upgrade(context.Background(), address, EnginePath, header(NameFor(aKey(t).PublicKey().Bytes()), EncodeKey(other.PublicKey().Bytes())))
	if err == nil {
		_ = conn.Close()
		t.Fatal("a machine registered under a name its key does not derive")
	}
	if !strings.Contains(err.Error(), "does not belong to that key") {
		t.Fatalf("the refusal was %v", err)
	}
}

// RULE TWO: possession is proved. A public key somebody merely copied does not
// register.
func TestAKeySomebodyOnlyCopiedCannotRegister(t *testing.T) {
	_, address := live(t)
	real := aKey(t)
	name := NameFor(real.PublicKey().Bytes())

	conn, err := upgrade(context.Background(), address, EnginePath, header(name, EncodeKey(real.PublicKey().Bytes())))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	// A different private key answering the challenge: the shape is right and
	// the arithmetic is not.
	err = proveToRelay(conn, name, aKey(t))
	if err == nil {
		t.Fatal("a machine registered without holding the key")
	}
	if !strings.Contains(err.Error(), "did not prove") {
		t.Fatalf("the refusal was %v", err)
	}
}

// RULE THREE: a live registration is never taken over.
func TestALiveNameIsNotHandedToASecondMachine(t *testing.T) {
	_, address := live(t)
	key := aKey(t)
	first, err := Register(context.Background(), address, key)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()

	second, err := Register(context.Background(), address, key)
	if err == nil {
		_ = second.Close()
		t.Fatal("a second machine took a name that was already connected")
	}
	if !errors.Is(err, ErrNameTaken) {
		t.Fatalf("the refusal was %v, which a caller cannot tell apart from any other", err)
	}
}

// A name nothing is registered under is a fact the surface has to be able to
// tell apart from every other failure.
func TestDiallingAMachineThatIsNotThereIsItsOwnFact(t *testing.T) {
	_, address := live(t)
	_, err := Dial(context.Background(), address, NameFor(aKey(t).PublicKey().Bytes()))
	if !errors.Is(err, ErrNoMachine) {
		t.Fatalf("dialling a name nothing holds gave %v", err)
	}
}

func TestARelayThatIsNotThereIsItsOwnFact(t *testing.T) {
	// Port 1 on the loopback, which nothing in a test environment listens on.
	_, err := Dial(context.Background(), "http://127.0.0.1:1", "otter-lamp-42")
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("dialling a relay that is not there gave %v", err)
	}
	if _, err := Register(context.Background(), "http://127.0.0.1:1", aKey(t)); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("registering with a relay that is not there gave %v", err)
	}
}

// The rate limit, driven by a clock the test owns so it does not have to wait
// a minute to see one.
func TestTooManyDialsFromOneAddressAreTurnedAway(t *testing.T) {
	service := &Server{}
	frozen := time.Now()
	service.Now = func() time.Time { return frozen }
	server := httptest.NewServer(service)
	t.Cleanup(server.Close)
	t.Cleanup(server.CloseClientConnections)

	name := NameFor(aKey(t).PublicKey().Bytes())
	var last error
	for i := 0; i < DialsPerMinute+1; i++ {
		_, last = Dial(context.Background(), server.URL, name)
	}
	if !errors.Is(last, ErrTooMany) {
		t.Fatalf("the %dth dial in one minute gave %v", DialsPerMinute+1, last)
	}
	// The next minute is a fresh allowance.
	frozen = frozen.Add(time.Minute)
	if _, err := Dial(context.Background(), server.URL, name); !errors.Is(err, ErrNoMachine) {
		t.Fatalf("the first dial of the next minute gave %v", err)
	}
}

// The relay's own account of what it knows is the whole of what it knows.
func TestTheLedgerIsNamesTimesAndCounts(t *testing.T) {
	service, address := live(t)
	registration, err := Register(context.Background(), address, aKey(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = registration.Close() }()

	surface, err := Dial(context.Background(), address, registration.Name())
	if err != nil {
		t.Fatal(err)
	}
	engine, err := registration.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := surface.Write([]byte("twelve bytes")); err != nil {
		t.Fatal(err)
	}
	readN(t, engine, len("twelve bytes"))
	_ = surface.Close()
	_ = engine.Close()

	// The count lands once the copy that carried it has finished, so this waits
	// on the fact rather than on a duration.
	deadline := time.Now().Add(2 * time.Second)
	for {
		ledgers := service.Ledgers()
		if len(ledgers) == 1 && ledgers[0].Up == int64(len("twelve bytes")) {
			if ledgers[0].Name != registration.Name() {
				t.Fatalf("the ledger names %q", ledgers[0].Name)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("the ledger settled at %+v", ledgers)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// THE STRUCTURAL HALF OF "THE RELAY CANNOT READ A FRAME". A promise in a
// comment is worth nothing; an import that does not exist is worth something.
// This package must not be able to reach the session wire or the crypto that
// rides it, because a package that cannot name a type cannot decode one.
func TestTheRelayCannotEvenNameTheThingsItCarries(t *testing.T) {
	forbidden := []string{
		"github.com/Agent-Field/aforge-v2/internal/remote",
		"github.com/Agent-Field/aforge-v2/internal/pair",
		"github.com/Agent-Field/aforge-v2/internal/session",
	}
	set := token.NewFileSet()
	packages, err := parser.ParseDir(set, ".", func(info fs.FileInfo) bool {
		return !strings.HasSuffix(info.Name(), "_test.go")
	}, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range packages {
		for path, file := range pkg.Files {
			for _, imported := range file.Imports {
				for _, banned := range forbidden {
					if strings.Trim(imported.Path.Value, `"`) == banned {
						t.Fatalf("%s imports %s — the relay must not be able to name what it forwards", path, banned)
					}
				}
			}
		}
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func header(name, key string) map[string][]string {
	return map[string][]string{HeaderName: {name}, HeaderKey: {key}}
}

func readN(t *testing.T, r io.Reader, n int) string {
	t.Helper()
	buffer := make([]byte, n)
	if _, err := io.ReadFull(r, buffer); err != nil {
		t.Fatal(err)
	}
	return string(buffer)
}
