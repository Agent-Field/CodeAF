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
	socket *websocket.Conn
}

// ping is answered by the far end's WebSocket layer, never by the relay's own
// code, so a hosted relay that is asleep answers it without waking.
func (w *wsConn) ping(ctx context.Context) error { return w.socket.Ping(ctx) }

// pinger is a carrier connection that can prove itself alive from this end.
type pinger interface {
	ping(ctx context.Context) error
}

func upgradeWebSocket(ctx context.Context, address *url.URL, path string, header http.Header) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: dialWithin}
	client := &http.Client{Transport: &http.Transport{DialContext: dialer.DialContext, ForceAttemptHTTP2: false}}
	handshake, cancel := context.WithTimeout(ctx, HandshakeWithin)
	defer cancel()
	socket, response, err := websocket.Dial(handshake, address.String(), &websocket.DialOptions{
		HTTPClient:      client,
		HTTPHeader:      header.Clone(),
		Subprotocols:    []string{Protocol},
		CompressionMode: websocket.CompressionDisabled, // the payloads are ciphertext
	})
	if err != nil {
		if response == nil {
			return nil, fmt.Errorf("%w: %s", ErrUnreachable, address.Host)
		}
		if response.StatusCode == http.StatusUpgradeRequired {
			return nil, errRawOnly
		}
		said := ""
		if response.Body != nil {
			said = strings.TrimSpace(readBounded(response.Body))
		}
		if response.StatusCode == http.StatusSwitchingProtocols {
			return nil, fmt.Errorf("%w: %s", ErrUnreachable, address.Host)
		}
		return nil, refusal(address.Host, path, response.StatusCode, said)
	}
	socket.SetReadLimit(messageLimit)
	// The connection outlives the handshake's context: it ends when the
	// carrier closes it, not when the dial's caller returns.
	return &wsConn{Conn: websocket.NetConn(context.Background(), socket, websocket.MessageBinary), socket: socket}, nil
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
