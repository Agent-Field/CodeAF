package provider

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

// How an outbound call is bounded, and why a stream is bounded differently.
//
// http.Client.Timeout is a deadline on the *whole* exchange, body reads
// included. For a request/response completion that is exactly right: the answer
// arrives in one piece and a call still open after the adaptive budget is a
// wedged call. For a stream it is a bug with a stopwatch on it — every stream
// died at the timeout no matter how healthily it was delivering, which at the
// default 32k max tokens meant a hard stop at eight and a half minutes,
// mid-sentence, with no resume.
//
// So a stream is bounded by silence instead. Two things replace the total
// deadline, and between them a dead connection is still noticed in minutes:
//
//   - responseHeaderTimeout, on the streaming transport, catches a provider that
//     accepts the connection and never answers. It applies only to the streaming
//     transport, because for a non-streamed completion the response headers do
//     not arrive until the whole answer has been generated — putting a header
//     deadline there would reintroduce the very cutoff being removed.
//   - streamIdleTimeout, an idle watchdog on the body, catches a stream that
//     stops mid-answer. Any byte counts as progress, including the SSE keepalive
//     comments providers send while a reasoning model is still thinking, so a
//     long silent think is only a stall if nothing at all comes down the wire.
const (
	responseHeaderTimeout = 2 * time.Minute
	streamIdleTimeout     = 2 * time.Minute
)

// ── THE POOL'S OWN TABLE: ONE VALUE, ONE REASON ─────────────────────────────
//
// EVERY NUMBER HERE IS THIS BUILD'S ANSWER AND NOT THE STANDARD LIBRARY'S.
// `http.DefaultTransport` is a sensible default for a program nobody measured;
// it was measured here, and one of its answers was costing a person a TLS
// handshake every time they stopped to read. A value inherited silently is a
// decision nobody took, so each of the four below is set on purpose and says
// why, and [TestTheSharedTransportStatesEveryValueItRelieson] fails the build
// if one of them goes back to being inherited.
//
//   - idleConnTimeout is how long THIS END keeps a connection nobody is using.
//     The default is ninety seconds, which is shorter than a person reads for.
//     Someone who takes two minutes over an answer and then types again found
//     an empty pool and paid DNS, TCP and TLS — a hundred to four hundred
//     milliseconds — inside the next request's own `httpClient.Do`, invisibly,
//     because `ttft_ms` cannot tell a handshake from a slow model (conntrace.go
//     is what tells them apart now). The figure below is not a guess: the far
//     end's own idle window is the real bound, and it was measured against the
//     live router on 2026-09-11 by holding connections idle for 30 s, 2, 5, 10
//     and 15 minutes and asking `httptrace` whether the next request rode the
//     same socket. THE MEASUREMENT IS RECORDED IN docs/changes/unreleased.
//     Ours sits under it, because a socket this end believes in and the far end
//     has already closed is a request that fails and is sent again — the
//     expensive way to discover the same thing.
//   - forceAttemptHTTP2 is why a warm pool is cheap at all against the router:
//     one connection carries every request this process makes, so the pool that
//     matters is one socket rather than [limiterCeiling] of them. It is on in
//     the standard library's default and off in a transport somebody built by
//     hand, which is exactly the accident this states out loud.
//   - tlsHandshakeTimeout bounds the part of a cold connection this file exists
//     to stop paying for. Ten seconds is the standard library's figure and it
//     is far too long for a handshake that is going to work: a router lane that
//     has not finished one in this long is a path to somewhere else, and the
//     ladder above has somewhere else to go (dispatch.go).
//   - expectContinueTimeout is stated to say that it never applies: nothing in
//     this package sends `Expect: 100-continue`, so the wait it bounds is a
//     wait no request of ours takes. It is kept at a second rather than
//     disabled because a zero here means "wait forever" for the one request
//     that might one day carry the header through a proxy at AFORGE_BASE_URL.
const (
	idleConnTimeout       = 4 * time.Minute
	tlsHandshakeTimeout   = 10 * time.Second
	expectContinueTimeout = time.Second
)

var (
	transportOnce sync.Once
	shared        *http.Transport
	streaming     *http.Transport
)

func buildTransports() {
	// The standard library's default is an *http.Transport, but this is a
	// process-global another package could have replaced, and a failed
	// assertion inside a sync.Once would take the whole binary with it.
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{}
	}
	shared = base.Clone()
	// The default is two idle connections per host, and the limiter's ceiling is
	// sixty-four. Every request past the second was therefore closing its
	// connection on completion and paying a fresh handshake on the next one —
	// invisible against HTTP/2 to OpenRouter, which multiplexes over one
	// connection, and expensive against an h1 endpoint behind AFORGE_BASE_URL.
	shared.MaxIdleConnsPerHost = limiterCeiling
	if shared.MaxIdleConns < 2*limiterCeiling {
		shared.MaxIdleConns = 2 * limiterCeiling
	}
	// And the four the table above states. They are assigned here rather than
	// left to the clone so that a base transport somebody replaces — the
	// process-global this function is careful about above — cannot quietly
	// change any of them.
	shared.IdleConnTimeout = idleConnTimeout
	shared.ForceAttemptHTTP2 = true
	shared.TLSHandshakeTimeout = tlsHandshakeTimeout
	shared.ExpectContinueTimeout = expectContinueTimeout

	streaming = shared.Clone()
	streaming.ResponseHeaderTimeout = responseHeaderTimeout
}

// SharedTransport is the process-wide connection pool for provider traffic.
// One pool rather than one per client: the clients (talk, work, boost, media,
// vision) all address the same account at the same host, and a pool each would
// mean a handshake each.
func SharedTransport() *http.Transport {
	transportOnce.Do(buildTransports)
	return shared
}

// streamTransport is SharedTransport plus a header deadline. It keeps its own
// pool because ResponseHeaderTimeout is a transport-wide setting and a
// non-streamed completion must not inherit it.
func streamTransport() *http.Transport {
	transportOnce.Do(buildTransports)
	return streaming
}

// idleWatchdog is the stream's real deadline: it cancels the request when the
// wire goes quiet for too long, and never because the answer is long.
//
// Cancelling the request context rather than closing the body is deliberate —
// the reader is parked inside Read on a socket, and only the transport can
// unblock it. Close is idempotent and always cancels, so the request goroutine
// is released whether the stream ended, stalled, or was abandoned by its caller.
type idleWatchdog struct {
	body   io.ReadCloser
	timer  *time.Timer
	cancel context.CancelFunc
	idle   time.Duration
	once   sync.Once
}

func newIdleWatchdog(body io.ReadCloser, idle time.Duration, cancel context.CancelFunc) *idleWatchdog {
	return &idleWatchdog{
		body:   body,
		timer:  time.AfterFunc(idle, cancel),
		cancel: cancel,
		idle:   idle,
	}
}

func (w *idleWatchdog) Read(p []byte) (int, error) {
	count, err := w.body.Read(p)
	if count > 0 {
		w.timer.Reset(w.idle)
	}
	return count, err
}

func (w *idleWatchdog) Close() error {
	err := w.body.Close()
	w.once.Do(func() {
		w.timer.Stop()
		w.cancel()
	})
	return err
}
