package pair

// The mailbox as a carrier: the same four messages as the relay's pipe, left in
// a place both devices can reach and read in turn.

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/Agent-Field/codeaf/internal/pair/cpace"
	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/syncwords"
)

// chatProtocol labels the wire that carries a person's chats. It is mixed into
// the PAKE, the Noise prologue and the key derivation, so a build that would
// read any byte differently cannot finish a handshake and quietly mean
// something else.
const chatProtocol = "codeaf-pair/2"

// pollFor is how long one poll is held open. It is under the mailbox's cap so
// the relay ends the wait and the caller never has to guess at its timeout.
const pollFor = 20 * time.Second

// chatScheme binds an introduction to one nameplate and to this wire, so a
// message from one mailbox cannot be replayed into another.
func chatScheme(plate string) scheme {
	return scheme{
		protocol: chatProtocol,
		pake:     cpace.NewContextInfo("codeaf device", "codeaf home "+plate, []byte(chatProtocol)),
		prologue: chatProtocol + " pair " + plate,
	}
}

// Mailbox is the relay service a pairing runs through and how to name it to a person.
type Mailbox struct {
	// Box is the mailbox service.
	Box pairbox.Box
	// URL is the address of the relay, which a pairing hands to the other device
	// so it lands on the same one.
	URL string
	// Host is named in the sentence for a relay that does not answer.
	Host string
	// Shown is the address the other device is told to pass as --via, and is
	// empty for the default relay, which needs no telling.
	Shown string
}

// boxLink is one device's end of one mailbox. It writes to its own side, reads
// the other, and owns what a vanished mailbox means to it.
type boxLink struct {
	ctx    context.Context
	box    pairbox.Box
	plate  string
	mine   pairbox.Side
	theirs pairbox.Side
	key    pairbox.Key
	// seen is how many of the other side's messages have been read.
	seen int
	// gone is what a mailbox that vanished means at each stage of waiting, and
	// sendGone is what it means when a write finds it missing.
	gone     map[stage]error
	sendGone error
}

func (l *boxLink) send(msg []byte) error {
	_, err := l.box.Post(l.ctx, l.plate, l.mine, l.key, msg)
	return l.explain(err, l.sendGone)
}

// recv waits for exactly the next message from the other side. A batch that
// holds anything else is a relay replaying, dropping or reordering, and the
// exchange stops there rather than guess.
func (l *boxLink) recv(waiting stage) ([]byte, error) {
	for {
		got, err := l.box.Poll(l.ctx, l.plate, l.theirs, l.seen, pollFor)
		if err != nil {
			return nil, l.explain(err, l.gone[waiting])
		}
		if len(got.Msgs) == 0 {
			continue
		}
		if len(got.Msgs) != 1 || got.Next != l.seen+1 {
			return nil, ErrWrongCode
		}
		l.seen++
		return got.Msgs[0], nil
	}
}

// ackWithin is how long a device that has sent its last message waits to hear
// it was kept. The wait ends at once when the word comes; a joining device that
// refused, or died, costs the other one this long, once.
const ackWithin = 10 * time.Second

// acknowledge posts the joining device's word that it keeps the answer. The
// word is said after the exchange is over, once this device has decided and
// before it writes anything, so it has a context of its own: the exchange's
// may have ended, and the word must still go out. It answers what the mailbox
// said, so a mailbox the other device already deleted stops the joining one.
//
// THE WORD IS ONE PLAIN BYTE, AND ONLY ITS PRESENCE MEANS ANYTHING. A device
// that will not keep the answer sends nothing, so a relay has no refusal to turn
// into a yes; and every build has sent this byte, so an older device that
// shows a code still hears a newer one that joins.
func (l *boxLink) acknowledge() error {
	ctx, stop := context.WithTimeout(context.WithoutCancel(l.ctx), ackWithin)
	defer stop()
	_, err := l.box.Post(ctx, l.plate, l.mine, l.key, []byte{1})
	return err
}

// settle waits, briefly, for that word. Without it the answer may not have been
// kept, or may never have arrived, and the device that sent it must not say the
// pairing finished. Short of a person stopping it, every way the wait can end
// without the word is [ErrNotConfirmed]: the answer had already gone out, so a
// sentence that said nothing was shared could be false.
func (l *boxLink) settle() error {
	quick := *l
	var stop context.CancelFunc
	quick.ctx, stop = context.WithTimeout(l.ctx, ackWithin)
	defer stop()
	_, err := quick.recv(awaitAck)
	if err == nil || errors.Is(err, context.Canceled) {
		return err
	}
	return ErrNotConfirmed
}

// explain gives a mailbox refusal the sentence a person reads. vanished is what
// this device says when the mailbox is gone; every other fact reads the same on
// both devices. A vanished mailbox is never a success, so a stage that names no
// sentence for it says the pairing did not finish.
func (l *boxLink) explain(err, vanished error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pairbox.ErrGone):
		if vanished == nil {
			return ErrDidNotFinish
		}
		return vanished
	}
	return phraseBox(err, "")
}

// phraseBox turns what a mailbox said into one sentence: a network that has
// asked for too much, a relay that is full, a relay that does not answer, a
// pairing that ran out of time. Anything else is already a sentence.
func phraseBox(err error, host string) error {
	var limited pairbox.RateLimited
	var netErr net.Error
	switch {
	case errors.As(err, &limited):
		return TooManyFor(limited.RetryAfter)
	case errors.Is(err, pairbox.ErrForbidden):
		return ErrSomeoneElse
	case errors.Is(err, pairbox.ErrRelayFull):
		return ErrRelayBusy
	case errors.Is(err, pairbox.ErrTooOld):
		return ErrRelayTooOld
	case errors.Is(err, context.DeadlineExceeded):
		return ErrDidNotFinish
	case errors.Is(err, context.Canceled):
		return err
	case errors.As(err, &netErr):
		return cannotReachError{syncwords.Explain(err, host).Error()}
	}
	return syncwords.Explain(err, host)
}
