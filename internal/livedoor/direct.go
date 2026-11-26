package livedoor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// The two roads a live view can travel. The HOST decides which one a tunnel is
// from the byte that opened it and the prologue it completed, never from
// anything a viewer says, and both sides report it in the road word a person
// sees.
const (
	RoadRelay  = "relay"
	RoadDirect = "direct"
)

// SignalVersion is the only version of the signalling payload there is.
const SignalVersion = 1

// What a Signal says. An offer, an answer and the candidates that follow them
// are the whole of how two computers find a path to each other; `end` abandons
// an attempt; `refuse` is the host saying no before it has allocated anything.
const (
	SignalOffer     = "offer"
	SignalAnswer    = "answer"
	SignalCandidate = "cand"
	SignalEnd       = "end"
	SignalRefuse    = "refuse"
)

// Why a host refuses an offer. They are closed words and nobody speaks them to
// a person: a refused offer leaves the view on the relay with no sentence.
const (
	RefuseRelayOnly = "relay-only"
	RefuseBusy      = "busy"
	RefuseOff       = "off"
)

// Signal is the JSON payload of one OpSignal frame. It travels on channel 0 of
// a relay tunnel that is already open, so the relay carries it as ciphertext
// inside the Noise records of that tunnel and learns only that a few records
// crossed.
//
// ID is a random attempt id that both sides echo, so a frame that belongs to an
// attempt that has already ended is dropped instead of being fed to a new one.
// A candidate carries its address line in C, its media id in Mid and its index
// in Idx; a candidate with End set and an empty C says there are no more.
type Signal struct {
	V      int    `json:"v"`
	T      string `json:"t"`
	ID     string `json:"id,omitempty"`
	SDP    string `json:"sdp,omitempty"`
	C      string `json:"c,omitempty"`
	Mid    string `json:"mid,omitempty"`
	Idx    int    `json:"idx,omitempty"`
	End    bool   `json:"end,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// maxSignalIDBytes bounds an attempt id: sixteen hex digits is what a sender
// makes, and this leaves room for a longer one without letting a peer park a
// large string in every frame.
const maxSignalIDBytes = 64

// ErrSignal reports a signalling payload that cannot be understood or that is
// over its size limit.
var ErrSignal = errors.New("livedoor: unusable signal")

// EncodeSignal writes a signal in the form an OpSignal frame carries. A signal
// over SignalMax is an error, because the receiver would close the link on it.
func EncodeSignal(s Signal) ([]byte, error) {
	if s.V == 0 {
		s.V = SignalVersion
	}
	payload, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	if len(payload) > SignalMax {
		return nil, fmt.Errorf("%w: %d bytes is over the %d-byte limit", ErrSignal, len(payload), SignalMax)
	}
	return payload, nil
}

// DecodeSignal reads one OpSignal payload. It refuses a payload over SignalMax,
// one that is not a single JSON object, an unknown version and an unknown kind,
// so everything past this point sees only a well-formed signal.
func DecodeSignal(payload []byte) (Signal, error) {
	var s Signal
	if len(payload) > SignalMax {
		return s, fmt.Errorf("%w: %d bytes is over the %d-byte limit", ErrSignal, len(payload), SignalMax)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&s); err != nil {
		return Signal{}, fmt.Errorf("%w: %v", ErrSignal, err)
	}
	if decoder.More() {
		return Signal{}, fmt.Errorf("%w: more than one value", ErrSignal)
	}
	if s.V != SignalVersion {
		return Signal{}, fmt.Errorf("%w: version %d", ErrSignal, s.V)
	}
	switch s.T {
	case SignalOffer, SignalAnswer, SignalCandidate, SignalEnd, SignalRefuse:
	default:
		return Signal{}, fmt.Errorf("%w: kind %q", ErrSignal, s.T)
	}
	if len(s.ID) > maxSignalIDBytes {
		return Signal{}, fmt.Errorf("%w: attempt id too long", ErrSignal)
	}
	return s, nil
}

// Signaller sends one OpSignal frame on channel 0 of THE relay link it belongs
// to. It is how a direct-road implementation talks to the other computer
// before a direct path exists.
type Signaller interface {
	Send(payload []byte) error
}

// DirectHost is the host half of the direct road, implemented outside this
// package (by internal/directroad, through a seam syncsetup installs) so this
// package imports no peer-to-peer library.
//
// Offered is called with the raw offer payload of a viewer the door has already
// authenticated. It returns the session that will carry the rest of that
// attempt, or nil to say no, which the router answers with a `refuse` signal
// before anything has been allocated for the peer.
type DirectHost interface {
	Offered(ctx context.Context, peer Peer, offer []byte, s Signaller) DirectSession
}

// DirectSession is one pending attempt on the host. Deliver is handed each
// later signal payload of the same attempt in the order it arrived, and Close
// ends the attempt and releases everything it holds. Both must be safe to call
// after the attempt has finished by itself.
type DirectSession interface {
	Deliver(payload []byte)
	Close()
}

// DirectConfig is what a viewer link needs to try the direct road. Dial builds
// the peer-to-peer connection (signalling through s), finishes the tunnel's
// handshake and device hello, and returns a READY tunnel: the same contract a
// link's own Dial has for the relay road.
type DirectConfig struct {
	Dial func(ctx context.Context, s Signaller) (io.ReadWriteCloser, error)
}
