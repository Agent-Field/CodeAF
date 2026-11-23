package relay

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
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

// Contract item 6 (part 1): WebSocket upgrades honour HTTP_PROXY and NO_PROXY.
func TestWebSocketUpgradeHonoursProxyEnvironment(t *testing.T) {
	upstream := httptest.NewServer(http.StripPrefix("/fabric", wsOnly(&Server{})))
	t.Cleanup(upstream.Close)
	t.Cleanup(upstream.CloseClientConnections)
	upstreamHost := strings.TrimPrefix(upstream.URL, "http://")

	var proxied atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxied.Add(1)
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijack", http.StatusInternalServerError)
			return
		}
		backend, err := net.Dial("tcp", upstreamHost)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		clientConn, buffered, err := hijacker.Hijack()
		if err != nil {
			_ = backend.Close()
			return
		}
		out := r.Clone(r.Context())
		out.RequestURI = ""
		out.URL.Scheme = ""
		out.URL.Host = ""
		if err := out.Write(backend); err != nil {
			_ = backend.Close()
			_ = clientConn.Close()
			return
		}
		go func() {
			if buffered.Reader.Buffered() > 0 {
				_, _ = io.CopyN(backend, buffered, int64(buffered.Reader.Buffered()))
			}
			_, _ = io.Copy(backend, clientConn)
			_ = backend.Close()
		}()
		go func() {
			_, _ = io.Copy(clientConn, backend)
			_ = clientConn.Close()
		}()
	}))
	t.Cleanup(proxy.Close)
	t.Cleanup(proxy.CloseClientConnections)

	t.Setenv("HTTP_PROXY", proxy.URL)
	t.Setenv("NO_PROXY", "")
	// Non-loopback host name so httpproxy does not apply its loopback bypass.
	reachAndEcho(t, "http://relay.example.test/fabric")
	if got := proxied.Load(); got < 2 {
		t.Fatalf("proxy saw %d requests; want both Register and Dial routed through HTTP_PROXY", got)
	}

	// NO_PROXY bypasses the proxy for the named host.
	before := proxied.Load()
	t.Setenv("NO_PROXY", "unproxied.invalid")
	previous := dialWithin
	dialWithin = 50 * time.Millisecond
	t.Cleanup(func() { dialWithin = previous })
	_, _ = Dial(context.Background(), "http://unproxied.invalid/fabric", "otter-lamp-42")
	if proxied.Load() != before {
		t.Fatalf("NO_PROXY host still hit HTTP_PROXY (%d -> %d)", before, proxied.Load())
	}
}

// trackedListener counts every accepted TCP socket — hijacked or plain HTTP —
// until its Close method runs, because http.Server.ConnState stops reporting
// once a connection transitions to StateHijacked.
type trackedListener struct {
	net.Listener
	open atomic.Int32
}

type trackedServerConn struct {
	net.Conn
	once sync.Once
	open *atomic.Int32
}

func (l *trackedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	l.open.Add(1)
	return &trackedServerConn{Conn: c, open: &l.open}, nil
}

func (c *trackedServerConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { c.open.Add(-1) })
	return err
}

// Contract item 6 (part 2): 20 attempts over the 426 fallback leave no open
// connections on the server and leak no transport goroutines on the client.
func TestFallback426DoesNotGrowOpenConnectionsOverTwentyAttempts(t *testing.T) {
	service := &Server{}
	server := httptest.NewUnstartedServer(rawOnly(service))
	tracked := &trackedListener{Listener: server.Listener}
	server.Listener = tracked
	server.Start()
	t.Cleanup(server.Close)
	t.Cleanup(server.CloseClientConnections)

	countOpen := func() int {
		return int(tracked.open.Load())
	}

	registration, err := Register(context.Background(), server.URL, aKey(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = registration.Close() }()

	// Warm one dial so baseline goroutines and the one held registration settle.
	warm, err := Dial(context.Background(), server.URL, registration.Name())
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := registration.Accept()
	if err != nil {
		t.Fatal(err)
	}
	_ = warm.Close()
	_ = accepted.Close()
	baselineGoroutines := runtime.NumGoroutine()

	const attempts = 20
	for i := 0; i < attempts; i++ {
		surface, err := Dial(context.Background(), server.URL, registration.Name())
		if err != nil {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
		engine, err := registration.Accept()
		if err != nil {
			t.Fatalf("attempt %d accept: %v", i+1, err)
		}
		_ = surface.Close()
		_ = engine.Close()
	}

	// Only the single held registration connection may remain open once the 20
	// 426-fallback dials have closed.
	deadline := time.Now().Add(2 * time.Second)
	for countOpen() > 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := countOpen(); got > 1 {
		t.Fatalf("after %d 426 fallbacks, %d server connections remained open; want at most 1 (the held registration)", attempts, got)
	}
	if grew := runtime.NumGoroutine() - baselineGoroutines; grew > 6 {
		t.Fatalf("goroutines grew by %d across %d 426 fallbacks (baseline %d)", grew, attempts, baselineGoroutines)
	}
}

// Contract item 7 (Findings 7 & 8): upstream HTML/non-JSON bodies never leak
// into errors, wrong-prefix 404s are not misreported as ErrNoMachine, the base
// path is preserved in refusal messages, and an older relay's 503 stream
// ceiling maps to ErrTooMany.
func TestUpstreamHTMLAndLegacy503BecomeCleanFactsWithBasePath(t *testing.T) {
	t.Run("upstream 403 HTML becomes one sentence with base path and no HTML", func(t *testing.T) {
		waf := mounted(t, "/wrong-prefix", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte("<!DOCTYPE html><html><body><h1>403 Forbidden</h1>\nAccess denied</body></html>"))
		}))
		_, err := Dial(context.Background(), waf, "otter-lamp-42")
		if err == nil {
			t.Fatal("403 succeeded")
		}
		said := err.Error()
		if strings.Contains(said, "<!DOCTYPE") || strings.Contains(said, "Forbidden") || strings.Contains(said, "\n") {
			t.Fatalf("raw upstream HTML leaked into error: %q", said)
		}
		if !strings.Contains(said, waf) || !strings.Contains(said, "(403)") {
			t.Fatalf("refusal %q does not name full relay address %q and status", said, waf)
		}
	})

	t.Run("upstream HTML 404 at bad base path is not ErrNoMachine", func(t *testing.T) {
		nginx := mounted(t, "/wrong-prefix", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("<html><head><title>404 Not Found</title></head></html>"))
		}))
		_, err := Dial(context.Background(), nginx, "otter-lamp-42")
		if err == nil || errors.Is(err, ErrNoMachine) {
			t.Fatalf("HTML 404 at bad base path returned %v; must not report ErrNoMachine", err)
		}
		if strings.Contains(err.Error(), "<html") || !strings.Contains(err.Error(), nginx) {
			t.Fatalf("HTML 404 returned %q; want clean sentence naming %q", err.Error(), nginx)
		}
	})

	t.Run("empty 401 preserves the base path", func(t *testing.T) {
		gate := mounted(t, "/custom/base", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnauthorized)
		}))
		_, err := Register(context.Background(), gate, aKey(t))
		if err == nil || !strings.Contains(err.Error(), gate) {
			t.Fatalf("empty 401 returned %v; want full address %q", err, gate)
		}
	})

	t.Run("older relay 503 stream ceiling maps to ErrTooMany", func(t *testing.T) {
		legacy := mounted(t, "/fabric", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "that machine already has 16 connections open, which is as many as the relay carries at once", http.StatusServiceUnavailable)
		}))
		_, err := Dial(context.Background(), legacy, "otter-lamp-42")
		if !errors.Is(err, ErrTooMany) {
			t.Fatalf("legacy 503 stream ceiling returned %v; want ErrTooMany", err)
		}
	})
}

// Contract item 7 (Finding 9): WebSocket carrier verifies the negotiated
// subprotocol, enforces SetReadLimit(messageLimit) after NetConn, and splits
// Dial writes larger than 1 MiB across relay-sized messages.
func TestWebSocketSubprotocolReadLimitAndLargeWriteSplitting(t *testing.T) {
	t.Run("101 without negotiated subprotocol is refused as ErrNoDoor", func(t *testing.T) {
		genericWS := mounted(t, "/fabric", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Accepts WebSocket without configuring Subprotocols, so Sec-WebSocket-Protocol is omitted.
			socket, err := websocket.Accept(w, r, nil)
			if err != nil {
				return
			}
			_ = socket.Close(websocket.StatusNormalClosure, "bye")
		}))
		_, err := Dial(context.Background(), genericWS, "otter-lamp-42")
		if !errors.Is(err, ErrNoDoor) {
			t.Fatalf("WebSocket without %s subprotocol returned %v; want ErrNoDoor", Protocol, err)
		}
	})

	t.Run("oversized incoming WebSocket frame is rejected by SetReadLimit", func(t *testing.T) {
		// coder/websocket adds 1 byte internally in SetReadLimit for its FIN
		// probe, so messageLimit+2 forces NetConn's second chunk read to trip
		// ErrMessageTooBig instead of streaming unbounded messages.
		oversized := mounted(t, "/fabric", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			socket, err := websocket.Accept(w, r, &websocket.AcceptOptions{
				Subprotocols:    []string{Protocol},
				CompressionMode: websocket.CompressionDisabled,
			})
			if err != nil {
				return
			}
			defer func() { _ = socket.CloseNow() }()
			_ = socket.Write(r.Context(), websocket.MessageBinary, make([]byte, messageLimit+2))
		}))
		surface, err := Dial(context.Background(), oversized, "otter-lamp-42")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = surface.Close() }()
		buf := make([]byte, messageLimit+2)
		if _, err := io.ReadFull(surface, buf); !errors.Is(err, websocket.ErrMessageTooBig) {
			t.Fatalf("reading a %d-byte WebSocket message past the %d-byte limit returned %v; want ErrMessageTooBig", len(buf), messageLimit, err)
		}
	})

	t.Run("writes larger than 1 MiB on Dial split cleanly over WebSocket", func(t *testing.T) {
		address := mounted(t, "/fabric", wsOnly(&Server{}))
		registration, err := Register(context.Background(), address, aKey(t))
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = registration.Close() }()

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

		// 1.5 MiB + odd tail exceeds both MaxPayload (128 KiB) and Cloudflare's 1 MiB frame ceiling.
		huge := make([]byte, 12*MaxPayload+321)
		_, _ = rand.Read(huge)
		go func() {
			_, _ = io.Copy(engine, engine)
		}()
		errCh := make(chan error, 1)
		go func() {
			_, writeErr := surface.Write(huge)
			errCh <- writeErr
		}()
		got := make([]byte, len(huge))
		if _, err := io.ReadFull(surface, got); err != nil {
			t.Fatalf("reading echoed 1.5 MiB payload: %v", err)
		}
		if err := <-errCh; err != nil {
			t.Fatalf("writing 1.5 MiB payload: %v", err)
		}
		if !bytes.Equal(got, huge) {
			t.Fatal("echoed 1.5 MiB payload did not match what was written")
		}
	})
}
