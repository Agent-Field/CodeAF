package pair

// The tunnel: an encrypted connection between two keys that have already been
// pinned, presenting as an ordinary [io.ReadWriteCloser].
//
// THAT INTERFACE IS THE ENTIRE POINT. internal/remote's Dial takes an
// io.ReadWriteCloser and its Serve takes a reader and a writer, and neither
// knows what is underneath — which is why the session protocol needed no change
// at all to travel this way. `remote.Dial(tunnel, name, hello)` is the same
// call the ssh door makes with an ssh child's pipes.
//
// THE PATTERN IS Noise_IK. The surface knows the machine's static key, because
// it pinned it when it paired; the machine learns the surface's static key from
// the first message and looks it up in its own book. Two consequences worth
// stating:
//
//   - The surface authenticates the machine BEFORE it sends anything of the
//     conversation. A relay that pointed the dial at a different machine
//     produces a handshake that does not complete, not a session that opens.
//   - The surface's identity is never in the clear. IK encrypts the initiator's
//     static key to the responder, so the relay sees a stream of bytes between
//     an address and a name, and not which of somebody's devices it is.
//
// WHAT THE RELAY CAN STILL SEE, and this package will not pretend otherwise:
// the machine name, when a connection starts and ends, and how many bytes went
// each way. Frame boundaries survive too, because a record here is a record on
// the wire — so the relay can see the SHAPE of a conversation's traffic. It
// cannot see a word of it.

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/flynn/noise"
)

// Tunnel is one encrypted connection.
type Tunnel struct {
	conn io.ReadWriteCloser
	// peer is the other end's long-term public key, as the completed handshake
	// proved it — not as anybody claimed it.
	peer []byte

	sendMu sync.Mutex
	send   *noise.CipherState

	recvMu sync.Mutex
	recv   *noise.CipherState
	rest   []byte

	closeOnce sync.Once
}

// Peer is the key on the other end, proved by the handshake.
func (t *Tunnel) Peer() []byte { return t.peer }

// Read hands back plaintext. A record arrives whole and is drained across as
// many Reads as the caller needs, which is what makes this an ordinary stream
// rather than a message queue.
func (t *Tunnel) Read(b []byte) (int, error) {
	t.recvMu.Lock()
	defer t.recvMu.Unlock()
	for len(t.rest) == 0 {
		sealed, err := readRecord(t.conn)
		if err != nil {
			return 0, err
		}
		plain, err := t.recv.Decrypt(nil, nil, sealed)
		if err != nil {
			// A RECORD THAT DOES NOT OPEN ENDS THE CONNECTION. There is no
			// benign reason for one: the keys are right or the stream has been
			// tampered with, and carrying on past it would be carrying on past
			// the only thing this layer is for.
			_ = t.Close()
			return 0, errors.New("something on this connection has been changed in transit — it has been closed")
		}
		t.rest = plain
	}
	n := copy(b, t.rest)
	t.rest = t.rest[n:]
	return n, nil
}

// Write encrypts and sends. A write longer than one record is cut up, because
// the caller above knows nothing about records and must not have to.
func (t *Tunnel) Write(b []byte) (int, error) {
	t.sendMu.Lock()
	defer t.sendMu.Unlock()
	written := 0
	for len(b) > 0 {
		chunk := b
		if len(chunk) > maxPlain {
			chunk = chunk[:maxPlain]
		}
		sealed, err := t.send.Encrypt(nil, nil, chunk)
		if err != nil {
			return written, err
		}
		if err := writeRecord(t.conn, sealed); err != nil {
			return written, err
		}
		written += len(chunk)
		b = b[len(chunk):]
	}
	return written, nil
}

func (t *Tunnel) Close() error {
	var err error
	t.closeOnce.Do(func() { err = t.conn.Close() })
	return err
}

// connectAsSurface dials a machine whose key this device already pinned.
func connectAsSurface(conn io.ReadWriteCloser, machineKey []byte, me Device, label string) (*Tunnel, error) {
	if _, err := conn.Write([]byte{intentConnect}); err != nil {
		return nil, err
	}
	handshake, err := noise.NewHandshakeState(noise.Config{
		CipherSuite:   suite,
		Pattern:       noise.HandshakeIK,
		Initiator:     true,
		Prologue:      []byte(protocol + " connect"),
		StaticKeypair: me.noiseKey(),
		PeerStatic:    machineKey,
	})
	if err != nil {
		return nil, err
	}
	// THE PAYLOAD IS WHAT PROVES THIS DEVICE HOLDS ITS KEY. In IK the first
	// message carries this device's static key and a static-static exchange,
	// and the tag over this payload is under a key that exchange produced — so
	// a machine that opens it has proof, and a device replaying somebody else's
	// captured static key produces a message that does not open.
	message, _, _, err := handshake.WriteMessage(nil, []byte(label))
	if err != nil {
		return nil, err
	}
	if err := writeRecord(conn, message); err != nil {
		return nil, err
	}
	reply, err := readRecord(conn)
	if err != nil {
		// NOTHING CAME BACK, which is what a machine that could not read the
		// first message looks like from here: IK encrypts this device's own
		// static key to the key it pinned, so a machine holding a different key
		// cannot open the message and has nothing to answer with.
		return nil, ErrNoAnswer
	}
	said, first, second, err := handshake.ReadMessage(nil, reply)
	if err != nil {
		return nil, ErrNotThatMachine
	}
	if first == nil || second == nil {
		return nil, errors.New("that machine answered with a handshake this build cannot finish")
	}
	if err := hearVerdict(said); err != nil {
		return nil, err
	}
	return &Tunnel{conn: conn, peer: machineKey, send: first, recv: second}, nil
}

// acceptAsMachine answers a connection, and asks `allow` whether the device on
// the other end may open a conversation here.
//
// THE VERDICT TRAVELS INSIDE THE ENCRYPTION. A device that has been stopped
// gets a sentence saying so, and it gets it from the machine itself — the relay
// neither writes it nor reads it.
func acceptAsMachine(conn io.ReadWriteCloser, me Device, allow func(key []byte, label string) error) (*Tunnel, []byte, error) {
	handshake, err := noise.NewHandshakeState(noise.Config{
		CipherSuite:   suite,
		Pattern:       noise.HandshakeIK,
		Initiator:     false,
		Prologue:      []byte(protocol + " connect"),
		StaticKeypair: me.noiseKey(),
	})
	if err != nil {
		return nil, nil, err
	}
	message, err := readRecord(conn)
	if err != nil {
		return nil, nil, err
	}
	said, _, _, err := handshake.ReadMessage(nil, message)
	if err != nil {
		return nil, nil, errors.New("a connection arrived that this machine could not make sense of")
	}
	deviceKey := append([]byte{}, handshake.PeerStatic()...)
	verdict := ""
	if err := allow(deviceKey, readableLabel(string(said))); err != nil {
		verdict = err.Error()
	}
	reply, first, second, err := handshake.WriteMessage(nil, sayVerdict(verdict))
	if err != nil {
		return nil, deviceKey, err
	}
	if err := writeRecord(conn, reply); err != nil {
		return nil, deviceKey, err
	}
	if verdict != "" {
		return nil, deviceKey, errors.New(verdict)
	}
	// The responder writes with the second cipher state and reads with the
	// first, which is Noise's own ordering: the first is the initiator's.
	return &Tunnel{conn: conn, peer: deviceKey, send: second, recv: first}, deviceKey, nil
}

// withDeadline puts a bound on a handshake when the thing underneath can carry
// one. A relay stream cannot — it is a queue, not a socket — so this is a
// courtesy on the transports that can and a silence on the ones that cannot.
type deadliner interface{ SetDeadline(time.Time) error }

func withDeadline(conn io.ReadWriteCloser, when time.Time) {
	if able, ok := conn.(deadliner); ok {
		_ = able.SetDeadline(when)
	}
}
