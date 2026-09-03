package pair

// The introduction: a PAKE over six digits, and two long-term keys carried past
// the relay under it.
//
// WHY A PAKE AND NOT A SHARED SECRET. The relay brokered this meeting, so it is
// exactly the party that could sit in the middle of it. A plain "both ends hash
// the code" scheme would hand a listener a transcript to grind six digits
// against offline, and six digits do not survive that for a second. A PAKE does
// not leak the code to a listener at all: the only way to test a guess is to
// run a whole exchange against the machine that minted it, which counts them
// ([Desk], [CodeAttempts]).
//
// THE PRIMITIVE IS CPace, from filippo.io/cpace, over ristretto255. It is a
// real, reviewed implementation and NOT ONE OF OURS. Nothing in this package
// implements a PAKE, an AEAD, a handshake pattern or a key schedule; the
// primitives are CPace for the introduction and github.com/flynn/noise for
// everything after it, and this file is the wiring between them.
//
// WHAT THE PAKE'S OUTPUT IS USED FOR is one thing only: it is the pre-shared
// key of a Noise NNpsk0 handshake. That is what turns "we agree on a key" into
// a confirmed, authenticated channel — the second message cannot be decrypted
// by anybody who derived a different key, so a wrong code fails as a refused
// handshake and never as a connection that quietly means something else.

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/pair/cpace"
	"github.com/Agent-Field/aforge-v2/internal/relay"
	"github.com/flynn/noise"
)

// suite is the one cipher suite in this package: X25519, ChaCha20-Poly1305,
// BLAKE2s. It is Noise's own default trio and it is stated once.
var suite = noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2s)

// The two identities the PAKE's context is built from. They are fixed strings
// because the two ends have to agree on them exactly, and a machine name is the
// only part that varies.
const (
	whoSurface = "aforge device"
	whoMachine = "aforge machine"
)

// pakeContext binds the exchange to THIS machine and THIS protocol, so that a
// transcript from one pairing cannot be replayed into another.
func pakeContext(machineName string) *cpace.ContextInfo {
	return cpace.NewContextInfo(whoSurface, whoMachine+" "+machineName, []byte(protocol))
}

// pskFrom stretches the PAKE's key into the 32 bytes Noise wants for a
// pre-shared key. cpace's own documentation says its output is for feeding to
// HKDF, and this is that, with a label of its own.
func pskFrom(key []byte) ([]byte, error) {
	return hkdf.Expand(sha256.New, key, protocol+" pairing psk", 32)
}

// pairAsSurface runs the introduction from the device that wants in.
//
// It is handed the machine name the person typed and the code they read off
// that machine's screen, and it answers with what this device should remember.
func pairAsSurface(conn io.ReadWriteCloser, service, machineName, code, label string, me Device, now time.Time) (Known, error) {
	if _, err := conn.Write([]byte{intentPair}); err != nil {
		return Known{}, err
	}

	// ── the PAKE ────────────────────────────────────────────────────────────
	first, state, err := cpace.Start(code, pakeContext(machineName))
	if err != nil {
		return Known{}, err
	}
	if err := writeRecord(conn, first); err != nil {
		return Known{}, err
	}
	second, err := readRecord(conn)
	if err != nil {
		return Known{}, wrongCodeOrGone(err)
	}
	agreed, err := state.Finish(second)
	if err != nil {
		return Known{}, ErrWrongCode
	}
	psk, err := pskFrom(agreed)
	if err != nil {
		return Known{}, err
	}

	// ── the keys, carried under it ──────────────────────────────────────────
	//
	// A WRONG CODE FAILS HERE AND NOWHERE ELSE. Both ends reached "a key" above
	// whatever they typed; only a matching one decrypts the machine's reply.
	handshake, err := noise.NewHandshakeState(noise.Config{
		CipherSuite:  suite,
		Pattern:      noise.HandshakeNN,
		Initiator:    true,
		Prologue:     []byte(protocol + " pairing " + machineName),
		PresharedKey: psk,
		// Placement 0 mixes the pre-shared key in before anything else, so the
		// very first message is already protected by the code.
		PresharedKeyPlacement: 0,
	})
	if err != nil {
		return Known{}, err
	}
	offer := append(append([]byte{}, me.Public()...), label...)
	message, _, _, err := handshake.WriteMessage(nil, offer)
	if err != nil {
		return Known{}, err
	}
	if err := writeRecord(conn, message); err != nil {
		return Known{}, err
	}
	reply, err := readRecord(conn)
	if err != nil {
		return Known{}, wrongCodeOrGone(err)
	}
	said, _, _, err := handshake.ReadMessage(nil, reply)
	if err != nil {
		return Known{}, ErrWrongCode
	}
	if len(said) < 32 {
		return Known{}, errors.New("that machine answered with something this build cannot read")
	}
	machineKey := said[:32]

	// THE NAME MUST BE THE ONE THIS KEY DERIVES. The relay already checks this
	// before it lets a machine register, but a surface that took the relay's
	// word for it would be trusting the relay with exactly the thing this whole
	// handshake exists to take away from it.
	if got := relay.NameFor(machineKey); got != machineName {
		return Known{}, fmt.Errorf("the machine that answered is %s, not %s — nothing was paired", got, machineName)
	}

	return Known{
		Name:    machineName,
		Key:     base64.RawURLEncoding.EncodeToString(machineKey),
		Service: service,
		Since:   now,
	}, nil
}

// pairAsMachine runs the introduction from the machine that showed the code.
//
// admit is handed the device the introduction produced, and is this machine
// writing it into its own book. It is asked BEFORE the reply that tells the
// surface the pairing held, and it is never nil: every caller has a book, and
// the whole point of the argument is that there is no way to run this exchange
// without one.
func pairAsMachine(conn io.ReadWriteCloser, machineName, code string, me Device, now time.Time, admit func(Paired) error) (Paired, error) {
	first, err := readRecord(conn)
	if err != nil {
		return Paired{}, err
	}
	second, agreed, err := cpace.Exchange(code, pakeContext(machineName), first)
	if err != nil {
		return Paired{}, err
	}
	if err := writeRecord(conn, second); err != nil {
		return Paired{}, err
	}
	psk, err := pskFrom(agreed)
	if err != nil {
		return Paired{}, err
	}

	handshake, err := noise.NewHandshakeState(noise.Config{
		CipherSuite:           suite,
		Pattern:               noise.HandshakeNN,
		Initiator:             false,
		Prologue:              []byte(protocol + " pairing " + machineName),
		PresharedKey:          psk,
		PresharedKeyPlacement: 0,
	})
	if err != nil {
		return Paired{}, err
	}
	message, err := readRecord(conn)
	if err != nil {
		return Paired{}, err
	}
	offer, _, _, err := handshake.ReadMessage(nil, message)
	if err != nil {
		// THE WRONG CODE LANDS HERE ON THIS SIDE, and it is the only thing that
		// can land here: the record was written by somebody who derived a
		// different key from a different six digits.
		return Paired{}, ErrWrongCode
	}
	if len(offer) < 32 {
		return Paired{}, errors.New("that device offered something this build cannot read")
	}
	deviceKey := append([]byte{}, offer[:32]...)
	one := Paired{
		Label: readableLabel(string(offer[32:])),
		Key:   base64.RawURLEncoding.EncodeToString(deviceKey),
		Since: now,
	}

	// THE MACHINE WRITES THE DEVICE DOWN BEFORE IT SAYS THE PAIRING HELD.
	//
	// The reply below is the whole of what the surface has to go on: it reads it,
	// says "paired", and dials straight back. A book written after that reply went
	// out is a book the very next connection can find empty — and a device that was
	// let in one millisecond ago is then told it has been stopped, which is the
	// sentence for a device somebody deliberately revoked. So the book is written
	// here, ahead of the reply. This is the shape acceptAsMachine already has,
	// where `allow` is asked before the last message is composed.
	//
	// A WRITE THAT FAILS SENDS NOTHING AT ALL. There is no room in this message
	// for a machine to say why it stopped — its shape is the machine's key and
	// its name, and it is the same shape every build of aforge has ever sent — so
	// the refusal is the silence of a machine that hangs up, which is exactly what
	// this machine already does when the six digits were wrong. The device is left
	// knowing the pairing did not hold, which is the fact that matters to it, and
	// the reason is kept where it can be acted on: this machine's own screen.
	if err := admit(one); err != nil {
		return Paired{}, notWrittenDown{err}
	}

	answer := append(append([]byte{}, me.Public()...), machineName...)
	reply, _, _, err := handshake.WriteMessage(nil, answer)
	if err != nil {
		return Paired{}, err
	}
	if err := writeRecord(conn, reply); err != nil {
		return Paired{}, err
	}

	return one, nil
}

// readableLabel keeps a device's own name for itself down to something that can
// sit in a list without wrecking it. A device gets to say what it is called and
// does not get to say it in three hundred characters or in control codes.
func readableLabel(said string) string {
	const most = 32
	kept := make([]rune, 0, most)
	for _, r := range said {
		if r < ' ' || r == 0x7f {
			continue
		}
		kept = append(kept, r)
		if len(kept) == most {
			break
		}
	}
	if len(kept) == 0 {
		return "a device"
	}
	return string(kept)
}

// wrongCodeOrGone reads a torn exchange the honest way. A machine that hung up
// mid-pairing is the shape a spent code takes on the wire — the far end threw
// the code away and closed — so an EOF here is reported as the code rather than
// as the network, and anything else is reported as itself.
func wrongCodeOrGone(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return ErrWrongCode
	}
	return err
}
