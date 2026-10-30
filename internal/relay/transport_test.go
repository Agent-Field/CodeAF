package relay

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// mounted serves a relay beneath base, the way the hosted relay lives at
// https://host/fabric, and refuses every path outside it as that relay does.
func mounted(t *testing.T, base string, door http.Handler) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(base+"/", http.StripPrefix(base, door))
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	t.Cleanup(server.CloseClientConnections)
	return server.URL + base
}

// reachAndEcho registers a machine at address, dials it, and checks one round
// trip, which is the whole of what a person needs from the relay.
func reachAndEcho(t *testing.T, address string) {
	t.Helper()
	registration, err := Register(context.Background(), address, aKey(t))
	if err != nil {
		t.Fatalf("Register(%s): %v", address, err)
	}
	defer func() { _ = registration.Close() }()
	surface, err := Dial(context.Background(), address, registration.Name())
	if err != nil {
		t.Fatalf("Dial(%s): %v", address, err)
	}
	defer func() { _ = surface.Close() }()
	engine, err := registration.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = engine.Close() }()
	if _, err := surface.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	if got := readN(t, engine, 4); got != "ping" {
		t.Fatalf("the machine read %q", got)
	}
	if _, err := engine.Write([]byte("pong")); err != nil {
		t.Fatal(err)
	}
	if got := readN(t, surface, 4); got != "pong" {
		t.Fatalf("the surface read %q", got)
	}
}

// A relay address keeps its path, as every other client's does: a relay at
// https://host/fabric is called at /fabric/v1/engine and /fabric/v1/dial/<name>.
func TestARelayAddressKeepsItsPath(t *testing.T) {
	for _, base := range []string{"/fabric", "/a/relay"} {
		for _, slash := range []string{"", "/"} {
			reachAndEcho(t, mounted(t, base, &Server{})+slash)
		}
	}
	_, root := live(t)
	reachAndEcho(t, root)
	reachAndEcho(t, root+"/")
}

// rawOnly is a relay from before the WebSocket door: it answers a WebSocket
// upgrade with 426, as every earlier `codeaf relay` does.
func rawOnly(door http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "this door speaks "+Protocol, http.StatusUpgradeRequired)
			return
		}
		door.ServeHTTP(w, r)
	})
}

// wsOnly is a front end that passes WebSocket upgrades and nothing else, which
// is what a Cloudflare Worker is.
func wsOnly(door http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "error code: 522", 522)
			return
		}
		door.ServeHTTP(w, r)
	})
}

// The carrier rides a WebSocket, which is the one upgrade every front end
// passes, and still reaches a relay that only speaks the raw upgrade.
func TestTheCarrierCrossesAWebSocketOnlyFrontAndAnOlderRelay(t *testing.T) {
	t.Run("websocket only", func(t *testing.T) {
		reachAndEcho(t, mounted(t, "/fabric", wsOnly(&Server{})))
	})
	t.Run("raw only", func(t *testing.T) {
		reachAndEcho(t, mounted(t, "/fabric", rawOnly(&Server{})))
	})
}

// An upstream proxy's body is not the relay's sentence. A relay that is down
// is unreachable; one that has no machine door says so; neither is printed as
// "error code: 522".
func TestUpstreamFailuresBecomeFacts(t *testing.T) {
	down := mounted(t, "/fabric", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "error code: 522", 522)
	}))
	_, err := Register(context.Background(), down, aKey(t))
	if !errors.Is(err, ErrUnreachable) || strings.Contains(err.Error(), "error code") {
		t.Fatalf("a relay answering 522: %v; want ErrUnreachable without the proxy's body", err)
	}

	noDoor := mounted(t, "/fabric", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"err":"not_found"}`))
	}))
	_, err = Register(context.Background(), noDoor, aKey(t))
	if !errors.Is(err, ErrNoDoor) || strings.Contains(err.Error(), "not_found") {
		t.Fatalf("a relay with no machine door: %v; want ErrNoDoor", err)
	}
}

// A machine at its stream ceiling is busy, not unreachable: the relay answers
// 429 and the surface hears that it is being turned away for now.
func TestAFullMachineIsBusyNotUnreachable(t *testing.T) {
	_, address := live(t)
	fullMachineIsBusy(t, address)
}

func fullMachineIsBusy(t *testing.T, address string) {
	t.Helper()
	registration, err := Register(context.Background(), address, aKey(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = registration.Close() }()
	for i := 0; i < MaxStreams; i++ {
		surface, err := Dial(context.Background(), address, registration.Name())
		if err != nil {
			t.Fatalf("surface %d: %v", i, err)
		}
		defer func() { _ = surface.Close() }()
	}
	if _, err := Dial(context.Background(), address, registration.Name()); !errors.Is(err, ErrTooMany) {
		t.Fatalf("a dial past %d streams gave %v; want ErrTooMany", MaxStreams, err)
	}
}
