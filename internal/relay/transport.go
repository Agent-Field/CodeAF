package relay

// The WebSocket carrier. It carries exactly the bytes the raw upgrade carries —
// the registration proof, then the frames of mux.go — so nothing above the
// connection knows which one it got. Each frame is one message, because
// [writeFrame] writes a frame in one call; that lets a relay that sees
// messages and not a byte stream, such as the hosted one, read a frame without
// buffering half of one across a wake-up.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/net/http/httpproxy"
)

// errRawOnly is a relay that answered a WebSocket with 426: one from before
// this door, which only speaks the raw upgrade.
var errRawOnly = errors.New("relay: that relay only speaks the raw upgrade")

// messageLimit is the largest message either end accepts: one full frame.
const messageLimit = frameHeader + MaxPayload

// wsConn is a WebSocket presented as the byte stream the carrier reads, with
// the protocol-level ping a carrier uses to keep a quiet connection proven.
type wsConn struct {
	net.Conn
	socket   *websocket.Conn
	maxWrite int
}

// Write cuts surface writes larger than [MaxPayload] into relay-sized WebSocket
// messages so a caller above [Dial] cannot trip a front end's per-message cap
// or the peer's [messageLimit]. Carrier frames on [EnginePath] set maxWrite to
// [messageLimit] so each frame stays one message.
func (w *wsConn) Write(b []byte) (int, error) {
	if w.maxWrite <= 0 || len(b) <= w.maxWrite {
		return w.Conn.Write(b)
	}
	written := 0
	for len(b) > 0 {
		chunk := b
		if len(chunk) > w.maxWrite {
			chunk = chunk[:w.maxWrite]
		}
		n, err := w.Conn.Write(chunk)
		written += n
		if err != nil {
			return written, err
		}
		b = b[n:]
	}
	return written, nil
}

// ping is answered by the far end's WebSocket layer, never by the relay's own
// code, so a hosted relay that is asleep answers it without waking.
func (w *wsConn) ping(ctx context.Context) error { return w.socket.Ping(ctx) }

// pinger is a carrier connection that can prove itself alive from this end.
type pinger interface {
	ping(ctx context.Context) error
}

// proxyFromEnvironment resolves HTTP_PROXY, HTTPS_PROXY and NO_PROXY with the
// same rules [http.ProxyFromEnvironment] uses, reading the current environment
// so process-lifetime changes and tests are honoured.
func proxyFromEnvironment(req *http.Request) (*url.URL, error) {
	return httpproxy.FromEnvironment().ProxyFunc()(req.URL)
}

func upgradeWebSocket(ctx context.Context, address *url.URL, base, path string, header http.Header) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: dialWithin}
	transport := &http.Transport{
		Proxy:             proxyFromEnvironment,
		DialContext:       dialer.DialContext,
		ForceAttemptHTTP2: false,
		DisableKeepAlives: true,
	}
	upgraded := false
	defer func() {
		// Any non-101 response (including every 426 fallback to an older
		// self-hosted relay) must release its socket and transport goroutines
		// before returning rather than leaving idle state behind.
		if !upgraded {
			transport.CloseIdleConnections()
		}
	}()
	client := &http.Client{Transport: transport}
	handshake, cancel := context.WithTimeout(ctx, HandshakeWithin)
	defer cancel()
	socket, response, err := websocket.Dial(handshake, address.String(), &websocket.DialOptions{
		HTTPClient:      client,
		HTTPHeader:      header.Clone(),
		Subprotocols:    []string{Protocol},
		CompressionMode: websocket.CompressionDisabled, // the payloads are ciphertext
	})
	if err != nil {
		// Always drain and close a non-101 body first — including on 426
		// fallbacks — so net/http closes the underlying TCP socket instead of
		// holding an active response open.
		said := ""
		if response != nil && response.Body != nil {
			said = strings.TrimSpace(readBounded(response.Body))
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if response == nil {
			return nil, fmt.Errorf("%w: %s", ErrUnreachable, base)
		}
		if response.StatusCode == http.StatusUpgradeRequired {
			return nil, errRawOnly
		}
		if response.StatusCode == http.StatusSwitchingProtocols {
			return nil, fmt.Errorf("%w: %s", ErrUnreachable, base)
		}
		return nil, refusal(base, path, response.StatusCode, said)
	}
	// coder/websocket permits a server that omitted Sec-WebSocket-Protocol; a
	// generic WebSocket endpoint is not a relay door and must fail closed here.
	if socket.Subprotocol() != Protocol {
		_ = socket.CloseNow()
		return nil, fmt.Errorf("%w: %s", ErrNoDoor, base)
	}
	upgraded = true
	// The connection outlives the handshake's context: it ends when the
	// carrier closes it, not when the dial's caller returns. SetReadLimit must
	// follow websocket.NetConn because NetConn resets the limit to -1 on entry.
	nc := websocket.NetConn(context.Background(), socket, websocket.MessageBinary)
	socket.SetReadLimit(messageLimit)
	maxWrite := MaxPayload
	if path == EnginePath {
		maxWrite = messageLimit
	}
	return &wsConn{Conn: nc, socket: socket, maxWrite: maxWrite}, nil
}

// proveAlive pings a carrier from this end every [PingEvery] and counts an
// answer as an arrival. A relay that never sends its own pings — the hosted one
// stays asleep rather than wake to send them — is then still a carrier whose
// silence is tested.
func proveAlive(c *carrier, p pinger) {
	ticker := time.NewTicker(PingEvery)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), HandshakeWithin)
			err := p.ping(ctx)
			cancel()
			if err != nil {
				c.shutdown(errCarrierGone)
				return
			}
			if c.onFrame != nil {
				c.onFrame()
			}
		}
	}
}
