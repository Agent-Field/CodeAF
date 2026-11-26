// Package relay is the blind pipe between a machine that runs codeaf and a
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
// and its close protocol buy nothing here — both ends of this pipe are codeaf,
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
// This is an ON-THE-WIRE identifier, not product prose. A product rename may
// not split old and new relay clients into different protocols.
const Protocol = "aforge-relay/1" // legacy-name

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
	HeaderName = "Aforge-Name" // legacy-name: persisted on the wire.
	// HeaderKey carries the machine's long-term public key, base64 raw-url.
	HeaderKey = "Aforge-Key" // legacy-name: persisted on the wire.
)

// HeaderDoor carries the class of a dial: what the surface asking for the machine
// means to do with the stream. It is a request header for the reason the
// registration's are, an operator's logs and limiters all read headers, and it
// is the one thing a dialer volunteers about itself. A relay that has never
// heard of it ignores it, and a dial that does not send it is an UNCLASSIFIED
// dial, which is every dial from a build older than the class.
const HeaderDoor = "Codeaf-Door"

// The classes a dial may claim in [HeaderDoor]. THE CLASS IS A CLAIM AND NOT AN
// AUTHENTICATION: a dialer can say "move" when it is not, which is why it only
// ever decides how the machine's streams are shared and never who may speak on
// one. Who may speak is settled above this package, by the handshake the host
// runs against a pinned key. A value that is neither of these is read as no
// class at all, so a later class does not turn an older relay's answer into a
// refusal.
const (
	// DoorLive is a surface watching or driving a chat as it happens, one
	// stream per viewing computer, held open for as long as the view is.
	DoorLive = "live"
	// DoorMove is a surface that has to be able to reach the machine whenever
	// anything else is attached: moving a chat to another computer must not be
	// refused because viewers filled the machine's streams.
	DoorMove = "move"
)

// The tuning, all in one place so that a relay operator changes a number here
// and every sentence in this package that quotes it changes with it. ONE SOURCE
// OF TRUTH: nothing below re-states a limit as a literal, and the hosted Worker
// mirrors these constants in relay/hosted/src/engine/carrier.js (and its
// front-door rate-limit bindings in relay/hosted/wrangler.toml).
const (
	// MaxPayload is the largest body one framed message may carry (MAX_PAYLOAD
	// in relay/hosted/src/engine/carrier.js). It is a little over 64 KiB
	// because the tunnel above frames its ciphertext at Noise's own 65535-byte
	// ceiling and adds a small header; a limit under that would cut a legal
	// message in half.
	MaxPayload = 1 << 17

	// MaxStreams is how many surfaces may be attached to one machine at once
	// (MAX_STREAMS in relay/hosted/src/engine/carrier.js). It is a limit on
	// the relay's memory and on nothing else — the engine machine has its own
	// opinion about how many surfaces a session will have, and this is only
	// the number the pipe will carry.
	MaxStreams = 16

	// LiveStreams is how many of those streams a [DoorLive] dial may leave
	// attached (LIVE_STREAMS in relay/hosted/src/engine/carrier.js). It is
	// compared with the machine's TOTAL stream count, whoever holds the
	// others, so live dials can never push the total past it and the last
	// MaxStreams-LiveStreams slots are always free for a [DoorMove] dial. A
	// dial that claims no class is held to MaxStreams exactly as before: the
	// reservation costs an older client nothing.
	LiveStreams = 14

	// BreakerRate and BreakerWindow are the ceiling on how fast one LIVE stream
	// may send, in either direction (BREAKER_BYTES_PER_SECOND and
	// BREAKER_WINDOW_MS in relay/hosted/src/engine/carrier.js): more than
	// BreakerRate sustained across BreakerWindow ends the stream. It is four
	// times the pace a correct host holds a live view to, so it never meets a
	// correct host, and it is an ABUSE control and nothing finer: the relay
	// cannot read what it carries, so it cannot batch or slow it, and cutting is
	// all it can do. It is counted when a message arrives and never from a
	// timer, which is what keeps an idle live view free on the hosted relay.
	BreakerRate   = 4 << 20
	BreakerWindow = 5 * time.Second

	// PingEvery is how often the relay pokes an idle carrier, and IdleAfter is
	// how long it will wait for any byte before it decides the machine is gone.
	// A NAT that has forgotten a mapping is silent rather than closed, so a
	// carrier with no traffic on it is a carrier that has to be tested.
	PingEvery = 45 * time.Second
	IdleAfter = 2 * PingEvery

	// HandshakeWithin bounds the registration exchange (HANDSHAKE_MS in
	// relay/hosted/src/engine/carrier.js). A connection that has claimed a name
	// but not yet proved it holds the key is a connection that could hold a
	// name for free, so it is given one round trip's worth of time and no more.
	HandshakeWithin = 20 * time.Second

	// ProbeWithin is how long a registration waits for an older carrier under
	// the same key to answer a ping before replacing it (PROBE_MS in
	// relay/hosted/src/engine/carrier.js). A machine whose network dropped can
	// come back before IdleAfter runs out; a live carrier answers in one round
	// trip and keeps its name.
	ProbeWithin = 5 * time.Second

	// DialsPerMinute and RegistrationsPerMinute are the per-address ceilings
	// (DIALS_PER_MINUTE and REGISTRATIONS_PER_MINUTE in
	// relay/hosted/src/engine/carrier.js, and MACHINE_DIAL_LIMIT /
	// MACHINE_REGISTER_LIMIT in relay/hosted/wrangler.toml). They are
	// deliberately generous for a person and stingy for a script: a person
	// dials once and stays, and reconnects a handful of times a day.
	DialsPerMinute         = 30
	RegistrationsPerMinute = 10
)
