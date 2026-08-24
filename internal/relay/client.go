package relay

// The two client halves. Both dial OUT and neither listens, which is the whole
// reason this lane exists: a machine behind a router with no forwarded port and
// a laptop on a café network can reach each other because they both walked out
// to the same door.
//
// THE ERRORS HERE ARE SENTINELS AND NOT SENTENCES. This package knows that
// nothing is connected under a name; it does not know that the person typed
// `--at otter-lamp-42` and needs to be told which of four different things went
// wrong in one line. internal/pair does that, because it is the layer that
// knows whether this device is even paired with that machine. What crosses the
// boundary is [ErrNoMachine], [ErrUnreachable], [ErrNameTaken] and
// [ErrTooMany] — facts, which the door above turns into the person's sentence.

import (
	"bufio"
	"context"
	"crypto/ecdh"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// The facts a caller above has to be able to tell apart.
var (
	// ErrUnreachable is the relay itself not answering.
	ErrUnreachable = errors.New("relay: cannot be reached")
	// ErrNoMachine is the relay answering that nothing is registered under
	// that name right now.
	ErrNoMachine = errors.New("relay: no machine connected under that name")
	// ErrNameTaken is a registration refused because that name is already
	// connected — rule three of [Names].
	ErrNameTaken = errors.New("relay: that name is already connected")
	// ErrTooMany is a rate limit, either door.
	ErrTooMany = errors.New("relay: too many connections from here")
)

// Registration is a machine's held connection to the relay and the surfaces
// arriving on it.
type Registration struct {
	name    string
	carrier *carrier
	conn    net.Conn
}

// Register holds this machine's outbound connection open under the name its key
// derives.
//
// It returns once the relay has accepted the registration, so a caller that
// gets no error can print the name and know it is real. Everything after that
// happens on [Registration.Accept].
func Register(ctx context.Context, service string, private *ecdh.PrivateKey) (*Registration, error) {
	name := NameFor(private.PublicKey().Bytes())
	header := http.Header{}
	header.Set(HeaderName, name)
	header.Set(HeaderKey, EncodeKey(private.PublicKey().Bytes()))

	conn, err := upgrade(ctx, service, EnginePath, header)
	if err != nil {
		return nil, err
	}
	// The registration exchange is bounded on this side too, so a relay that
	// accepts the upgrade and then says nothing is a failed command rather than
	// a process that never comes back.
	_ = conn.SetDeadline(time.Now().Add(HandshakeWithin))
	if err := proveToRelay(conn, name, private); err != nil {
		_ = conn.Close()
		if strings.Contains(err.Error(), "already connected") {
			return nil, fmt.Errorf("%w: %s", ErrNameTaken, err.Error())
		}
		return nil, err
	}
	_ = conn.SetDeadline(time.Time{})

	carrier := newCarrier(conn, true)
	// THE MACHINE WATCHES THE RELAY THE WAY THE RELAY WATCHES THE MACHINE. If
	// the relay disappears without closing — the far side of a NAT, a load
	// balancer that dropped the connection — nothing arrives, the deadline is
	// never pushed out, and the read fails instead of hanging for ever. The
	// relay's own pings are what keep an idle-but-alive carrier fresh.
	carrier.onFrame = func() { _ = conn.SetReadDeadline(time.Now().Add(IdleAfter)) }
	_ = conn.SetReadDeadline(time.Now().Add(IdleAfter))
	go carrier.pump()
	return &Registration{name: name, carrier: carrier, conn: conn}, nil
}

// Name is what a person types to reach this machine.
func (r *Registration) Name() string { return r.name }

// Accept hands back the next surface that dialled this machine. It blocks; a
// closed registration returns [io.EOF], which is the ordinary way the loop
// above it ends.
func (r *Registration) Accept() (io.ReadWriteCloser, error) {
	select {
	case stream, ok := <-r.carrier.accepted:
		if !ok {
			return nil, io.EOF
		}
		return stream, nil
	case <-r.carrier.done:
		if r.carrier.err != nil {
			return nil, r.carrier.err
		}
		return nil, io.EOF
	}
}

// Close gives the name up. Every surface on it goes with it, which is right:
// the machine has stopped being reachable.
func (r *Registration) Close() error {
	r.carrier.shutdown(nil)
	return nil
}

// Done is closed when this registration has ended, so a caller can redial.
func (r *Registration) Done() <-chan struct{} { return r.carrier.done }

// Dial reaches a machine by name, and hands back the raw byte pipe to it.
//
// WHAT COMES BACK IS NOT AUTHENTICATED AND NOT ENCRYPTED. It is a pipe to
// whoever is registered under that name, brokered by a relay this package does
// not ask anybody to trust. internal/pair is what turns it into a connection —
// a Noise handshake against a pinned key — and nothing else in this tree may
// put a session frame on this pipe.
func Dial(ctx context.Context, service, name string) (io.ReadWriteCloser, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("%q is not the shape of a machine name — they look like otter-lamp-42", name)
	}
	return upgrade(ctx, service, DialPrefix+name, http.Header{})
}

// upgrade dials the service and takes the connection over.
func upgrade(ctx context.Context, service, path string, header http.Header) (net.Conn, error) {
	address, err := url.Parse(strings.TrimSpace(service))
	if err != nil || address.Host == "" || (address.Scheme != "http" && address.Scheme != "https") {
		return nil, fmt.Errorf("%q is not a relay address — it looks like https://relay.example.com", service)
	}

	dialer := &net.Dialer{Timeout: HandshakeWithin}
	conn, err := dialer.DialContext(ctx, "tcp", hostPort(address))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrUnreachable, address.Host)
	}
	if address.Scheme == "https" {
		secure := tls.Client(conn, &tls.Config{ServerName: address.Hostname()})
		if err := secure.HandshakeContext(ctx); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("%w: %s", ErrUnreachable, address.Host)
		}
		conn = secure
	}

	request := &http.Request{
		Method: http.MethodGet,
		URL:    &url.URL{Path: path},
		Host:   address.Host,
		Header: header.Clone(),
		Proto:  "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
	}
	request.Header.Set("Upgrade", Protocol)
	request.Header.Set("Connection", "Upgrade")

	_ = conn.SetDeadline(time.Now().Add(HandshakeWithin))
	if err := request.Write(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %s", ErrUnreachable, address.Host)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("%w: %s", ErrUnreachable, address.Host)
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		said := strings.TrimSpace(readBounded(response.Body))
		_ = conn.Close()
		switch response.StatusCode {
		case http.StatusNotFound:
			return nil, ErrNoMachine
		case http.StatusTooManyRequests:
			return nil, ErrTooMany
		default:
			if said == "" {
				said = response.Status
			}
			return nil, errors.New(said)
		}
	}
	_ = conn.SetDeadline(time.Time{})
	// A 101 has no body, so there should be nothing left in the reader — but
	// "should" is not a thing to build a byte stream on, and a server that
	// packed its first frame into the same segment would otherwise lose it.
	if reader.Buffered() > 0 {
		return &joinedConn{Conn: conn, head: io.MultiReader(io.LimitReader(reader, int64(reader.Buffered())), conn)}, nil
	}
	return conn, nil
}

// joinedConn is a connection whose first bytes were already read into a buffer.
type joinedConn struct {
	net.Conn
	head io.Reader
}

func (j *joinedConn) Read(b []byte) (int, error) { return j.head.Read(b) }

func hostPort(address *url.URL) string {
	if address.Port() != "" {
		return address.Host
	}
	if address.Scheme == "https" {
		return net.JoinHostPort(address.Hostname(), "443")
	}
	return net.JoinHostPort(address.Hostname(), "80")
}

// readBounded takes the relay's own sentence off a refusal, and no more of it
// than a sentence.
func readBounded(body io.ReadCloser) string {
	defer func() { _ = body.Close() }()
	said, _ := io.ReadAll(io.LimitReader(body, 512))
	return string(said)
}
