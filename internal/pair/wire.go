package pair

// The bytes on a relay stream, before any of them mean anything.
//
// EVERY MESSAGE IS LENGTH-PREFIXED, because the relay carries a byte stream and
// a handshake message has to be handed to the crypto library whole. Two bytes
// of length is the largest a Noise message may be anyway, so the prefix and the
// ceiling are the same number and there is nothing to keep in step.

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// The first byte a surface writes on a fresh stream, saying what it came for.
// It is one byte in the clear, and it is the ONLY byte in the clear: a relay
// watching this stream learns that somebody wanted to pair or wanted to
// connect, which it could have inferred from the timing anyway.
const (
	intentPair    byte = 'P'
	intentConnect byte = 'C'

	// IntentFiles is the first byte a files-door or task-door connection writes,
	// matching the original connect byte so older builds and newer builds speak
	// the same wire without negotiation.
	IntentFiles byte = intentConnect
	// IntentLive is the first byte a live-view stream writes on the files-door
	// registration so the host can separate live views from file transfers before
	// starting Noise.
	IntentLive byte = 'L'
	// IntentDirect is the first byte a viewer writes on a peer-to-peer data
	// channel, where the files door's own prologue map selects the direct road's
	// prologue. It is a different byte from IntentLive on purpose: a stream
	// made for the relay can never complete against the direct road, and the
	// host decides the road from this byte, never from anything the viewer says.
	IntentDirect byte = 'D'
)

// maxRecord is a Noise message's own ceiling, and therefore this framing's.
const maxRecord = 65535

// maxPlain is the largest plaintext one record can carry: the ceiling less the
// AEAD tag.
const maxPlain = maxRecord - 16

// writeRecord sends one length-prefixed record in ONE write.
//
// THE PREFIX AND THE BODY GO TOGETHER because the stream underneath may be
// message-oriented: a relay carrier bills a message per write and a peer-to-peer
// data channel sends one message per write, so the old two writes doubled the
// message count of every record on both. The bytes on the wire are exactly the
// ones the two writes produced, so a build that still writes two reads this
// one and the other way round.
func writeRecord(w io.Writer, body []byte) error {
	if len(body) > maxRecord {
		return fmt.Errorf("pair: a %d-byte record is over the %d-byte limit", len(body), maxRecord)
	}
	record := make([]byte, 2+len(body))
	binary.BigEndian.PutUint16(record, uint16(len(body)))
	copy(record[2:], body)
	_, err := w.Write(record)
	return err
}

func readRecord(r io.Reader) ([]byte, error) {
	var prefix [2]byte
	if _, err := io.ReadFull(r, prefix[:]); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint16(prefix[:])
	if length == 0 {
		return nil, nil
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	return body, nil
}

// The verdict a machine puts in the last handshake message. It is inside the
// encryption, so a person's refusal sentence is not something the relay gets to
// read, change, or invent.
const (
	verdictWelcome byte = 1
	verdictNo      byte = 2
)

func sayVerdict(reason string) []byte {
	if reason == "" {
		return []byte{verdictWelcome}
	}
	return append([]byte{verdictNo}, reason...)
}

func hearVerdict(said []byte) error {
	if len(said) == 0 {
		return errors.New("that machine answered with nothing")
	}
	if said[0] == verdictWelcome {
		return nil
	}
	if len(said) == 1 {
		return errors.New("that machine refused this device")
	}
	return errors.New(string(said[1:]))
}
