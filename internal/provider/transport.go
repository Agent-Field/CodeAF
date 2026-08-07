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

var (
	transportOnce sync.Once
	shared        *http.Transport
	streaming     *http.Transport
)

func buildTransports() {
	shared = http.DefaultTransport.(*http.Transport).Clone()
	// The default is two idle connections per host, and the limiter's ceiling is
	// sixty-four. Every request past the second was therefore closing its
	// connection on completion and paying a fresh handshake on the next one —
	// invisible against HTTP/2 to OpenRouter, which multiplexes over one
	// connection, and expensive against an h1 endpoint behind AFORGE_BASE_URL.
	shared.MaxIdleConnsPerHost = limiterCeiling
	if shared.MaxIdleConns < 2*limiterCeiling {
		shared.MaxIdleConns = 2 * limiterCeiling
	}

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
