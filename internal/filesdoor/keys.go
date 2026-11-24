// Package filesdoor moves already-encrypted chat objects between the computers
// of one person while the relay stores nothing.
//
// A computer that holds a chat runs a files door: it registers under a name
// derived from the person's cell key and answers read-only requests for the
// objects and frames of its local store. A computer that wants the chat dials
// that name through the relay's blind pipe and reads what it needs.
//
// THE RELAY SEES CIPHERTEXT AND NOTHING ELSE. Objects are sealed before they
// reach any store, the tunnel adds a second layer on top, and the relay only
// forwards the bytes. Who may ask is decided by the door itself: a device
// certificate signed by the person's identity, bound to the tunnel, and a
// roster that says the device has not been removed.
//
// The relay's pipe has no flow control, so everything here is built to bound
// what it can be asked to buffer: a request is small, a response is at most
// [MaxResponseBytes] of payload, and the client sends the next request only
// after reading the whole of the last answer.
package filesdoor

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relay"
)

const (
	// Version is the wire version, checked in the hello before any object moves.
	Version = 1

	// MaxResponseBytes is the most payload one response carries. It is the
	// bound the relay is held to: with no flow control in its Worker, the
	// receiver pulling one bounded response at a time is what limits how many
	// bytes the relay can be made to hold for a stream.
	MaxResponseBytes = 8 << 20

	// ResponseSlack is the room a response's own header may take beyond its
	// payload. The biggest header is a locate answer for a thousand objects.
	ResponseSlack = 64 << 10

	// MaxRequestBytes bounds one request message. Ids travel as raw bytes, so
	// the largest request (a locate of a thousand ids) is about half of this.
	MaxRequestBytes = 64 << 10

	// MaxRidsPerRequest bounds the ids in one has or get-many request, which is
	// what a get-many answers for in a single store call.
	MaxRidsPerRequest = blobstore.MaxGetMany

	// DefaultRosterFor is how long one roster answer may be trusted. A device
	// that is removed is cut off within this long, even in the middle of a take.
	DefaultRosterFor = 10 * time.Second

	// DefaultRetryWithin is how long a client keeps redialling a door that is
	// not there before it says the computer is off. It covers a door changing
	// from one process to another on the same computer.
	DefaultRetryWithin = 10 * time.Second

	// DefaultNoMachineWithin is how long a client keeps redialling when every
	// answer so far has been the relay saying that nothing is registered under
	// the door's name. A computer that is switched off answers that way for as
	// long as it is off, so waiting out [DefaultRetryWithin] for it only delays
	// the sentence that says so. A door that is moving between two codeaf
	// processes of one computer is back within one lock poll and one
	// registration, which this covers with room to spare. Any other failure, a
	// relay that is busy or unreachable for instance, keeps the longer window.
	DefaultNoMachineWithin = 2 * time.Second

	// DefaultLockPoll is how often a process that does not hold the home's door
	// lock looks again. It bounds how long a door can be missing after the
	// process that held it ends.
	DefaultLockPoll = 250 * time.Millisecond

	// FilesTunnelPrologue is mixed into the handshake, so a tunnel made for the
	// files door can never complete against a task door, even though both are
	// derived from the same cell key.
	FilesTunnelPrologue = "codeaf files door v1 connect"

	// LiveTunnelPrologue is mixed into the handshake of a live-view connection
	// on the files-door registration, so a stream made for live viewing and a
	// stream made for moving files can never complete against each other.
	LiveTunnelPrologue = "codeaf live door v1 connect"

	// DirectTunnelPrologue is mixed into the handshake of a live view that
	// travels over a peer-to-peer data channel. It is deliberately not the live
	// prologue: the same static door key serves every road, so this string is
	// what makes a stream made for the relay unable to complete against the
	// direct road, and one made for the direct road unable to complete against
	// the relay's live door or the files door.
	DirectTunnelPrologue = "codeaf live door v1 direct"

	// MaxDirect is how many direct streams one door admits at once, counted
	// before Noise so a flood costs a counter. A direct stream holds no relay
	// stream, so the number is its own rather than a share of the relay's live
	// streams; livedoor.MaxDirect is the same number for the router's side, and
	// a syncsetup test pins that the two agree.
	MaxDirect = 14

	doorKeySalt = "codeaf files door v1"
)

// The timings below are variables so a test can watch a bound do its job
// without waiting out the real one. Nothing else changes them.
var (
	// handshakeWithin bounds a connection from its first byte to the end of
	// the hello, so a peer that goes quiet cannot hold a stream open.
	handshakeWithin = pair.HandshakeWithin
	// idleAfter closes a connection that sends no request for this long, and
	// one that stops accepting a response for this long.
	idleAfter = 2 * time.Minute
	// silentAfter is how long a client waits without a single byte of an answer
	// before it treats the tunnel as broken.
	silentAfter = 2 * time.Minute
	// dialWithin bounds one dial attempt: relay, handshake and hello together.
	dialWithin = 10 * time.Second
	// liveRecheck is how often an open live tunnel asks the roster whether its
	// device is still allowed.
	liveRecheck = DefaultRosterFor
)

// DoorKey derives a device's files-door key from the person's cell key. Nothing
// extra is published for it, and a cell-key rotation changes every door. The
// salt is this door's own, so the key is never the task door's key for the
// same device.
func DoorKey(id identity.Identity, deviceID string) (*ecdh.PrivateKey, error) {
	raw, err := hkdf.Key(sha256.New, id.CellKey(), []byte(doorKeySalt), deviceID, 32)
	if err != nil {
		return nil, err
	}
	return ecdh.X25519().NewPrivateKey(raw)
}

// DoorName is the relay rendezvous name of the device's files-door key.
func DoorName(id identity.Identity, deviceID string) (string, error) {
	key, err := DoorKey(id, deviceID)
	if err != nil {
		return "", err
	}
	return relay.NameFor(key.PublicKey().Bytes()), nil
}
