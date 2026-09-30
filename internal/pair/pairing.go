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
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Agent-Field/codeaf/internal/pair/cpace"
	"github.com/Agent-Field/codeaf/internal/relay"
	"github.com/flynn/noise"
)

// suite is the one cipher suite in this package: X25519, ChaCha20-Poly1305,
// BLAKE2s. It is Noise's own default trio and it is stated once.
var suite = noise.NewCipherSuite(noise.DH25519, noise.CipherChaChaPoly, noise.HashBLAKE2s)

// The two identities the PAKE's context is built from. They are fixed strings
// because the two ends have to agree on them exactly, and a machine name is the
// only part that varies.
const (
	// These are ON-THE-WIRE PAKE identities, not product prose. They remain
	// stable so the two ends derive the same key across an upgrade.
	whoSurface = "aforge device"  // legacy-name
	whoMachine = "aforge machine" // legacy-name
)

// machineScheme binds the exchange to THIS machine and THIS protocol, so that a
// transcript from one pairing cannot be replayed into another.
func machineScheme(machineName string) scheme {
	return scheme{
		protocol: protocol,
		pake:     cpace.NewContextInfo(whoSurface, whoMachine+" "+machineName, []byte(protocol)),
		prologue: protocol + " pairing " + machineName,
	}
}

// pairAsSurface runs the introduction from the device that wants in.
//
// It is handed the machine name the person typed and the code they read off
// that machine's screen, and it answers with what this device should remember.
// seen is told the join words as soon as they exist, which is before the machine
// has answered: they are what the person compares while they wait.
func pairAsSurface(conn io.ReadWriteCloser, service, machineName, code, label string, me Device, now time.Time, seen func(words string)) (Known, error) {
	if _, err := conn.Write([]byte{intentPair}); err != nil {
		return Known{}, err
	}
	offer := append(append([]byte{}, me.Public()...), label...)
	intro, err := begin(streamLink{conn}, machineScheme(machineName), code, offer)
	if err != nil {
		return Known{}, err
	}
	seen(intro.Words)
	said, err := intro.verdict()
	if err != nil {
		return Known{}, err
	}
	machineKey, err := hearMachine(said)
	if err != nil {
		return Known{}, err
	}

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

// hearMachine reads the machine's answer: its key, in the shape every build has
// sent, or a short refusal. A refusal is told apart by its length, because a
// key is always 32 bytes and a verdict never is.
func hearMachine(said []byte) ([]byte, error) {
	if len(said) >= 32 {
		return said[:32], nil
	}
	if len(said) > 0 && said[0] == verdictNo {
		return nil, ErrRefused
	}
	return nil, errors.New("that machine answered with something this build cannot read")
}

// pairAsMachine runs the introduction from the machine that showed the code.
//
// approve is handed the device's name and the join words and answers nil only
// when a person, looking at the other device's screen, said yes. admit is handed
// the device the introduction produced, and is this machine writing it into its
// own book. Neither is ever nil: every caller has a book and a person, and the
// whole point of the arguments is that there is no way to run this exchange
// without either.
func pairAsMachine(conn io.ReadWriteCloser, machineName, code string, me Device, now time.Time, approve func(label, words string) error, admit func(Paired) error) (Paired, error) {
	intro, err := answer(streamLink{conn}, machineScheme(machineName), code)
	if err != nil {
		// THE WRONG CODE LANDS HERE ON THIS SIDE, and it is the only thing that
		// can land here: the record was written by somebody who derived a
		// different key from a different six digits.
		return Paired{}, err
	}
	if len(intro.Offer) < 32 {
		return Paired{}, errors.New("that device offered something this build cannot read")
	}
	deviceKey := append([]byte{}, intro.Offer[:32]...)
	one := Paired{
		Label: readableLabel(string(intro.Offer[32:])),
		Key:   base64.RawURLEncoding.EncodeToString(deviceKey),
		Since: now,
	}

	// A PERSON LOOKS BEFORE ANYTHING IS WRITTEN. The join words are on both
	// screens by now; a refusal is a short verdict inside the encryption, so the
	// device that asked can say it was refused and not that a code was wrong.
	if err := approve(one.Label, intro.Words); err != nil {
		_ = intro.reply(sayVerdict("refused"))
		return Paired{}, ErrRefused
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
	// its name, and it is the same shape every build of codeaf has ever sent — so
	// the refusal is the silence of a machine that hangs up, exactly as this
	// machine already does when the six digits were wrong. The device is left
	// knowing the pairing did not hold, which is the fact that matters to it, and
	// the reason is kept where it can be acted on: this machine's own screen.
	if err := admit(one); err != nil {
		return Paired{}, notWrittenDown{err}
	}
	if err := intro.reply(append(append([]byte{}, me.Public()...), machineName...)); err != nil {
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
