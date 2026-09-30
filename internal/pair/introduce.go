package pair

// The introduction, once. Two doors use it: `codeaf serve` pairs a device with a
// machine through the relay's pipe, and `/pair` pairs a device with a person's
// chats through the relay's mailbox. What each door carries in the messages
// differs; how the two devices come to trust the channel does not, so that is
// written here one time and both doors are held to it.
//
// FOUR MESSAGES, ALWAYS IN THIS ORDER:
//
//  1. the joining device starts the PAKE over the code;
//  2. the device that showed the code answers it;
//  3. the joining device sends the first Noise message (pre-shared key from the
//     PAKE), carrying what it offers;
//  4. the device that showed the code replies inside Noise, carrying its answer.
//
// A WRONG CODE FAILS AT MESSAGE 3, on the device that showed the code, before it
// has shown a person anything. Both ends reached "a key" at message 2 whatever
// they typed; only equal codes decrypt.

import (
	"crypto/hkdf"
	"crypto/sha256"
	"io"

	"github.com/Agent-Field/codeaf/internal/pair/cpace"
	"github.com/flynn/noise"
)

// scheme is what two builds must agree on before an introduction can begin. It
// binds a transcript to one place and one wire, so a message from one pairing
// cannot be replayed into another.
type scheme struct {
	// protocol labels the wire and the key derivation.
	protocol string
	// pake is the PAKE's context: both identities and the wire label.
	pake *cpace.ContextInfo
	// prologue is what Noise mixes in before the first message.
	prologue string
}

// psk stretches the PAKE's key into the 32 bytes Noise wants. cpace's own
// documentation says its output is for feeding to HKDF, and this is that, with
// a label of its own.
func (s scheme) psk(agreed []byte) ([]byte, error) {
	return hkdf.Expand(sha256.New, agreed, s.protocol+" pairing psk", 32)
}

// handshake is the Noise state for one side. Placement 0 mixes the pre-shared
// key in before anything else, so the very first message is already protected
// by the code.
func (s scheme) handshake(initiator bool, agreed []byte) (*noise.HandshakeState, error) {
	psk, err := s.psk(agreed)
	if err != nil {
		return nil, err
	}
	return noise.NewHandshakeState(noise.Config{
		CipherSuite:           suite,
		Pattern:               noise.HandshakeNN,
		Initiator:             initiator,
		Prologue:              []byte(s.prologue),
		PresharedKey:          psk,
		PresharedKeyPlacement: 0,
	})
}

// stage says what a device is waiting for, so a link can say what the other
// device vanishing means at that moment.
type stage int

const (
	// awaitStart is the device that showed the code waiting for message 1.
	awaitStart stage = iota
	// awaitAnswer is the joining device waiting for message 2.
	awaitAnswer
	// awaitOffer is the device that showed the code waiting for message 3.
	awaitOffer
	// awaitVerdict is the joining device waiting for message 4.
	awaitVerdict
	// awaitAck is the device that sent message 4 waiting to hear it was read.
	awaitAck
)

// link carries messages between the two devices, one at a time and in order. It
// owns what a vanished or misbehaving counterpart means, because that differs by
// carrier: a hung-up stream and a deleted mailbox are not the same sentence.
type link interface {
	send(msg []byte) error
	recv(waiting stage) ([]byte, error)
	// acknowledge tells the other device the last message was read, and settle
	// waits for that word after sending one, answering an error when it never
	// came. A carrier that keeps messages in a place that is deleted when the
	// exchange ends needs them, or the deletion can land under the read that was
	// about to happen; a stream does not.
	acknowledge()
	settle() error
}

// streamLink is a link over a relay byte stream, one length-prefixed record per
// message.
type streamLink struct{ rw io.ReadWriter }

func (l streamLink) send(msg []byte) error { return writeRecord(l.rw, msg) }

// recv reads one record. A far end that hung up while the joining device waited
// is the shape a thrown-away code takes on the wire, so it is reported as the
// code and not as the network.
func (l streamLink) recv(waiting stage) ([]byte, error) {
	msg, err := readRecord(l.rw)
	if waiting == awaitAnswer || waiting == awaitVerdict {
		err = wrongCodeOrGone(err)
	}
	return msg, err
}

func (streamLink) acknowledge()  {}
func (streamLink) settle() error { return nil }

// introduction is the joining device's half, after it has sent message 3.
type introduction struct {
	hs *noise.HandshakeState
	ln link
	// Words are what to show a person while the other device asks them.
	Words string
}

// begin runs messages 1 to 3 from the joining device. offer travels inside
// message 3.
func begin(ln link, s scheme, secret string, offer []byte) (*introduction, error) {
	first, state, err := cpace.Start(secret, s.pake)
	if err != nil {
		return nil, err
	}
	if err := ln.send(first); err != nil {
		return nil, err
	}
	second, err := ln.recv(awaitAnswer)
	if err != nil {
		return nil, err
	}
	agreed, err := state.Finish(second)
	if err != nil {
		return nil, ErrWrongCode
	}
	hs, err := s.handshake(true, agreed)
	if err != nil {
		return nil, err
	}
	message, _, _, err := hs.WriteMessage(nil, offer)
	if err != nil {
		return nil, err
	}
	if err := ln.send(message); err != nil {
		return nil, err
	}
	return &introduction{hs: hs, ln: ln, Words: joinWords(hs.ChannelBinding())}, nil
}

// verdict reads message 4 and answers what the other device said inside it.
func (i *introduction) verdict() ([]byte, error) {
	reply, err := i.ln.recv(awaitVerdict)
	if err != nil {
		return nil, err
	}
	said, _, _, err := i.hs.ReadMessage(nil, reply)
	if err != nil {
		return nil, ErrWrongCode
	}
	// THE WORD THAT THE ANSWER WAS READ IS SENT ONLY ONCE IT HAS OPENED. A reply
	// that a relay changed on the way was not read, and telling the other device
	// it was would have that device report a pairing that never happened here.
	i.ln.acknowledge()
	return said, nil
}

// offered is the other half, after it has read message 3 and opened it.
type offered struct {
	hs *noise.HandshakeState
	ln link
	// Offer is what the joining device sent, still to be read by the caller.
	Offer []byte
	// Words are what to show a person so they can compare with the joining
	// device's screen.
	Words string
}

// answer runs messages 1 to 3 from the device that showed the code. Anything
// that does not open under the code is ErrWrongCode: a message from a device
// that typed different digits is indistinguishable from noise, and both are
// spent the same way.
func answer(ln link, s scheme, secret string) (*offered, error) {
	first, err := ln.recv(awaitStart)
	if err != nil {
		return nil, err
	}
	second, agreed, err := cpace.Exchange(secret, s.pake, first)
	if err != nil {
		return nil, ErrWrongCode
	}
	if err := ln.send(second); err != nil {
		return nil, err
	}
	hs, err := s.handshake(false, agreed)
	if err != nil {
		return nil, err
	}
	message, err := ln.recv(awaitOffer)
	if err != nil {
		return nil, err
	}
	offer, _, _, err := hs.ReadMessage(nil, message)
	if err != nil {
		return nil, ErrWrongCode
	}
	return &offered{hs: hs, ln: ln, Offer: offer, Words: joinWords(hs.ChannelBinding())}, nil
}

// reply sends message 4.
func (o *offered) reply(payload []byte) error {
	message, _, _, err := o.hs.WriteMessage(nil, payload)
	if err != nil {
		return err
	}
	if err := o.ln.send(message); err != nil {
		return err
	}
	return o.ln.settle()
}
