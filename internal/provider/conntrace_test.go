package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestTheRowSaysWhetherThePoolWasWarm is the fix for the blind spot this wave
// was written about: `ttft_ms` folds the handshake into the model's wait, so a
// person who paused to read and then typed again looked like a provider that
// had slowed down. The first call opens a connection and says so; the second
// rides the one the pool kept and says that.
func TestTheRowSaysWhetherThePoolWasWarm(t *testing.T) {
	read := loggingTo(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
	}))
	t.Cleanup(server.Close)

	// A POOL OF ITS OWN, so the two calls below are the only two things that
	// have ever used it and the first one is certainly cold.
	transport := &http.Transport{IdleConnTimeout: idleConnTimeout}
	t.Cleanup(transport.CloseIdleConnections)
	client, err := NewClient(Config{
		APIKey:     "test-key",
		BaseURL:    server.URL,
		Model:      "sim/model",
		HTTPClient: &http.Client{Transport: transport, Timeout: 10 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	forgetLanes(t)

	for range 2 {
		if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
			t.Fatal(err)
		}
	}

	done := ended(read())
	if len(done) != 2 {
		t.Fatalf("two calls should leave two finished rows; got %d", len(done))
	}
	cold, warm := done[0], done[1]
	if cold.ConnReused == nil {
		t.Fatal("the first row says nothing about its connection")
	}
	if *cold.ConnReused {
		t.Error("the first call of a fresh pool cannot have ridden a connection the pool already had")
	}
	if cold.ConnectMs < 0 {
		t.Errorf("connect_ms = %d", cold.ConnectMs)
	}
	if warm.ConnReused == nil || !*warm.ConnReused {
		t.Fatalf("the second call should have ridden the pool: %+v", warm)
	}
	// A REUSED CONNECTION PAID FOR NONE OF THE THREE, so the row carries none
	// of them rather than three honest zeroes (the emptiness law).
	if warm.DNSms != 0 || warm.ConnectMs != 0 || warm.TLSms != 0 {
		t.Errorf("a warm row should carry no handshake: %+v", warm)
	}
}

// TestAThinkPauseKeepsThePool is the whole point of [idleConnTimeout], stated
// as the thing a person does: read an answer, think, type again. The pause here
// is short because the law under test is that the pool's own timeout is not
// what closes the connection — the transport's figure is minutes, and a pool
// that dropped the socket in between would fail this at any pause at all.
func TestAThinkPauseKeepsThePool(t *testing.T) {
	read := loggingTo(t)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"model":"sim/model","choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":10,"completion_tokens":2,"total_tokens":12}}`))
	}))
	t.Cleanup(server.Close)

	transport := SharedTransport().Clone()
	t.Cleanup(transport.CloseIdleConnections)
	client, err := NewClient(Config{
		APIKey:     "test-key",
		BaseURL:    server.URL,
		Model:      "sim/model",
		HTTPClient: &http.Client{Transport: transport, Timeout: 10 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	forgetLanes(t)

	if _, err := client.CompleteWithMessages(context.Background(), userMessages("hello")); err != nil {
		t.Fatal(err)
	}
	// A pause, short enough to be free and long enough that a transport with
	// no idle timeout of its own would already have reaped the connection.
	time.Sleep(150 * time.Millisecond)
	if _, err := client.CompleteWithMessages(context.Background(), userMessages("again")); err != nil {
		t.Fatal(err)
	}

	done := ended(read())
	if len(done) != 2 {
		t.Fatalf("two calls should leave two finished rows; got %d", len(done))
	}
	if done[1].ConnReused == nil || !*done[1].ConnReused {
		t.Fatalf("the turn after a pause paid a fresh handshake: %+v", done[1])
	}
}

// TestTheSharedTransportStatesEveryValueItRelieson refuses an inherited
// default. Each of these is a number this build has a reason for
// (transport.go's table), and a zero here is the standard library's answer to a
// question nobody asked.
func TestTheSharedTransportStatesEveryValueItRelieson(t *testing.T) {
	shared := SharedTransport()
	if shared.IdleConnTimeout != idleConnTimeout {
		t.Errorf("IdleConnTimeout = %v, want %v", shared.IdleConnTimeout, idleConnTimeout)
	}
	if !shared.ForceAttemptHTTP2 {
		t.Error("ForceAttemptHTTP2 is off: one multiplexed connection is the whole reason a warm pool is cheap here")
	}
	if shared.TLSHandshakeTimeout != tlsHandshakeTimeout {
		t.Errorf("TLSHandshakeTimeout = %v, want %v", shared.TLSHandshakeTimeout, tlsHandshakeTimeout)
	}
	if shared.ExpectContinueTimeout != expectContinueTimeout {
		t.Errorf("ExpectContinueTimeout = %v, want %v", shared.ExpectContinueTimeout, expectContinueTimeout)
	}
	if shared.HTTP2 == nil {
		t.Fatal("no keep-alive: the far end closes an idle connection between six and seven minutes, and a pool that only waited out its own would hand a request a socket the router had already dropped")
	}
	if shared.HTTP2.SendPingTimeout != http2KeepAlive {
		t.Errorf("SendPingTimeout = %v, want %v", shared.HTTP2.SendPingTimeout, http2KeepAlive)
	}
	if shared.HTTP2.PingTimeout != pingAnswerTimeout {
		t.Errorf("PingTimeout = %v, want %v", shared.HTTP2.PingTimeout, pingAnswerTimeout)
	}
	// AT LEAST TWO PINGS MUST LAND INSIDE THE WINDOW THE MEASUREMENT PROVES,
	// which is what makes one lost ping survivable and is the whole derivation
	// of the period (transport.go's table). The pool's own timeout is the far
	// end's measured figure, so the same inequality states both.
	if http2KeepAlive*2 > idleConnTimeout {
		t.Errorf("a keep-alive of %v inside a window of %v leaves fewer than two pings", http2KeepAlive, idleConnTimeout)
	}
	if shared.MaxIdleConnsPerHost != limiterCeiling {
		t.Errorf("MaxIdleConnsPerHost = %d, want the limiter's ceiling %d", shared.MaxIdleConnsPerHost, limiterCeiling)
	}
	// AND THE STREAMING POOL IS THE SAME POOL PLUS ONE DEADLINE. A second set
	// of values would drift from this one the first time somebody changed one.
	streaming := streamTransport()
	if streaming.IdleConnTimeout != shared.IdleConnTimeout || streaming.ForceAttemptHTTP2 != shared.ForceAttemptHTTP2 {
		t.Error("the streaming pool no longer inherits the shared pool's settings")
	}
	if streaming.ResponseHeaderTimeout != responseHeaderTimeout {
		t.Errorf("ResponseHeaderTimeout = %v, want %v", streaming.ResponseHeaderTimeout, responseHeaderTimeout)
	}
	if shared.ResponseHeaderTimeout != 0 {
		t.Error("a non-streamed completion must not inherit a header deadline: its headers do not arrive until the answer is generated")
	}
}
