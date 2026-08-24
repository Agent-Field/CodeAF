// Package relay is the blind pipe between a machine that runs aforge and a
// machine somebody is sitting at.
//
// IT EXISTS BECAUSE THE ENGINE MACHINE ONLY EVER DIALS OUT. A home server, an
// office workstation, a laptop on a café network — none of them can be reached
// from outside, and telling a person to forward a port is the onboarding cliff
// this whole lane was written to remove. So the engine machine opens ONE
// outbound connection to the relay and holds it. NAT and firewalls become
// irrelevant, because nothing was opened.
//
// THE RELAY MUST NOT BE ABLE TO READ A FRAME, AND THAT IS A PROPERTY OF THE
// CODE AND NOT A PROMISE IN A DOCUMENT. This package imports neither
// internal/remote nor internal/pair; it has no key material of the
// conversation's, no decoder for the conversation's frames, and no type in it
// that could hold a plaintext. What it moves is a byte slice it never looks
// inside. Its total knowledge of a session is the machine's NAME, the TIMING,
// and the BYTE COUNTS — and a test in this package pins exactly that
// (relay_blind_test.go).
//
// WHAT IT IS NOT is a trust root. The name a machine registers under is derived
// from that machine's own key, and the relay checks possession of that key
// before it hands the name over — but a name is a rendezvous label, never an
// identity. Everything that makes a connection safe happens ABOVE this package,
// end to end, in internal/pair: a PAKE over a code shown on the engine machine's
// own screen, and thereafter a Noise handshake between keys both ends have
// pinned. A relay that lied about which machine is which produces a failed
// handshake, not a compromise.
//
// THE CARRIER IS AN HTTP UPGRADE OVER ONE TCP CONNECTION, in front of TLS in
// production. The design doc says "outbound websocket" and means the property
// rather than the framing: one long-lived connection the engine machine dials
// out on, over the port every network already lets out. A websocket's masking
// and its close protocol buy nothing here — both ends of this pipe are aforge,
// and the payloads are already ciphertext — while its framing would be a second
// framing on top of the one below, which this package needs anyway to carry
// several surfaces down one carrier.
package relay

import "time"

// Protocol is the upgrade token both ends spell, and the door that refuses a
// build that would misread the bytes. It is bumped when the rendezvous framing
// below changes, which is a different clock from internal/remote's Version —
// the relay carries session frames it cannot read, so the two versions move
// independently and neither may be inferred from the other.
const Protocol = "aforge-relay/1"

// The doors. Both are GET so that an ordinary HTTP front end, a load balancer
// or a corporate proxy sees a request shape it already knows how to pass.
const (
	// EnginePath is where a machine registers itself and holds the carrier.
	EnginePath = "/v1/engine"
	// DialPrefix is where a surface asks for a machine by name; the name is
	// the rest of the path.
	DialPrefix = "/v1/dial/"
)

// The headers of the registration request. They are headers rather than a first
// frame because a relay operator's logs, metrics and rate limiters all read
// headers, and a name that only appeared after the upgrade would be invisible
// to every one of them.
const (
	// HeaderName carries the machine name being claimed.
	HeaderName = "Aforge-Name"
	// HeaderKey carries the machine's long-term public key, base64 raw-url.
	HeaderKey = "Aforge-Key"
)

// The tuning, all in one place so that a relay operator changes a number here
// and every sentence in this package that quotes it changes with it. ONE SOURCE
// OF TRUTH: nothing below re-states a limit as a literal.
const (
	// MaxPayload is the largest body one framed message may carry. It is a
	// little over 64 KiB because the tunnel above frames its ciphertext at
	// Noise's own 65535-byte ceiling and adds a small header; a limit under
	// that would cut a legal message in half.
	MaxPayload = 1 << 17

	// MaxStreams is how many surfaces may be attached to one machine at once.
	// It is a limit on the relay's memory and on nothing else — the engine
	// machine has its own opinion about how many surfaces a session will have,
	// and this is only the number the pipe will carry.
	MaxStreams = 16

	// PingEvery is how often the relay pokes an idle carrier, and IdleAfter is
	// how long it will wait for any byte before it decides the machine is gone.
	// A NAT that has forgotten a mapping is silent rather than closed, so a
	// carrier with no traffic on it is a carrier that has to be tested.
	PingEvery = 45 * time.Second
	IdleAfter = 2 * PingEvery

	// HandshakeWithin bounds the registration exchange. A connection that has
	// claimed a name but not yet proved it holds the key is a connection that
	// could hold a name for free, so it is given one round trip's worth of time
	// and no more.
	HandshakeWithin = 20 * time.Second

	// DialsPerMinute and RegistrationsPerMinute are the per-address ceilings.
	// They are deliberately generous for a person and stingy for a script: a
	// person dials once and stays, and reconnects a handful of times a day.
	DialsPerMinute         = 30
	RegistrationsPerMinute = 10
)
