package pair

// `codeaf serve`: the machine that owns the work, holding one outbound
// connection open and answering the devices that arrive on it.
//
// IT LISTENS FOR NOTHING. There is no port, no inbound rule, no certificate and
// no address to tell anybody. The machine walks out to the relay and stays
// there, which is why this works from behind a home router, a corporate
// firewall or a café network with no configuration at all.
//
// EVERY DECISION IS MADE HERE AND NOT AT THE RELAY: whether a pairing code is
// live, whether a device is let in, whether a device is stopped. The relay's
// part is to put two byte streams next to each other.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/relay"
)

// Host is the machine side.
type Host struct {
	// Service is the relay's address.
	Service string
	// Device is this machine's key.
	Device Device
	// Devices is the book of devices this machine lets in.
	Devices *Book
	// Desk holds the live pairing code.
	Desk *Desk
	// Approve is asked, with the joining device's name and the join words, before
	// this machine lets a device in, and answers whether the person looking at
	// that device's screen said yes. NIL IS A NO: a machine with nobody to ask
	// admits nobody, because admitting a shell without a look at the other
	// device is the weaker door.
	Approve func(label, words string) bool
	// Say is where a person-facing line goes — the name, the code, and one
	// line per device that arrives or is turned away. Nil says nothing.
	Say func(string)
	// Open is handed a plaintext pipe for each connection that was let in, and
	// owns it from then on: it runs the conversation and closes the pipe. THE
	// TUNNEL IS AN io.ReadWriteCloser AND NOTHING MORE, which is what lets the
	// caller hand it straight to internal/remote.
	Open func(io.ReadWriteCloser)
	// Now is the clock, swapped by tests.
	Now func() time.Time
}

func (h *Host) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *Host) say(line string) {
	if h.Say != nil {
		h.Say(line)
	}
}

// Run holds the registration until the context is cancelled, redialling when
// the connection to the relay is lost.
//
// A LOST RELAY IS NOT A LOST MACHINE. Wifi drops, a relay is restarted, a NAT
// forgets — none of those are reasons for the conversations on this machine to
// end, so this loop backs off and comes back rather than returning. It returns
// only when the context ends, or when the relay refuses in a way that trying
// again cannot fix.
func (h *Host) Run(ctx context.Context) error {
	if strings.TrimSpace(h.Service) == "" {
		return errors.New("no sync address is set up on this machine, so there is nowhere to be reachable from — set " + RelayEnv + " to your sync address")
	}
	wait := time.Second
	announced := false
	for {
		registration, err := relay.Register(ctx, h.Service, h.Device.Private())
		switch {
		case err == nil:
		case errors.Is(err, relay.ErrNameTaken):
			// RULE THREE OF THE NAME POLICY, MET FROM THIS SIDE. Something is
			// already connected under this machine's name, which in practice
			// means a second `codeaf serve` on this same machine. Trying again
			// forever would be two processes fighting over one name.
			return fmt.Errorf("this machine is already connected to sync as %s — there is only one of it, so close the other `codeaf serve`", h.Device.Name())
		case errors.Is(err, relay.ErrUnreachable):
			if !announced {
				return Unreachable(h.Service)
			}
			h.say("lost sync — trying again")
		default:
			if !announced {
				return err
			}
			h.say("sync refused this machine: " + err.Error())
		}

		if err == nil {
			wait = time.Second
			if !announced {
				announced = true
			}
			if code, codeErr := h.Desk.Offer(); codeErr == nil {
				h.say(strings.TrimRight(Lines(registration.Name(), code), "\n"))
			} else {
				h.say("  this machine is reachable as  " + registration.Name())
			}
			h.hold(ctx, registration)
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		if wait < time.Minute {
			wait *= 2
		}
	}
}

// hold answers arrivals until the registration ends.
func (h *Host) hold(ctx context.Context, registration *relay.Registration) {
	go func() {
		<-ctx.Done()
		_ = registration.Close()
	}()
	for {
		stream, err := registration.Accept()
		if err != nil {
			return
		}
		go h.answer(stream)
	}
}

// answer takes one arriving stream through whichever of the two exchanges it
// asked for.
func (h *Host) answer(stream io.ReadWriteCloser) {
	withDeadline(stream, h.now().Add(HandshakeWithin))
	var intent [1]byte
	if _, err := io.ReadFull(stream, intent[:]); err != nil {
		_ = stream.Close()
		return
	}
	switch intent[0] {
	case intentPair:
		h.pair(stream)
	case intentConnect:
		h.connect(stream)
	default:
		_ = stream.Close()
	}
}

// pair runs the introduction and, if it held, writes the device into this
// machine's book.
func (h *Host) pair(stream io.ReadWriteCloser) {
	defer func() { _ = stream.Close() }()
	// ONE ATTEMPT, SPENT BEFORE THE EXCHANGE RUNS. That is what makes a
	// six-digit code safe: the budget is taken whether the guess turns out
	// right or wrong, so nobody can grind by failing cheaply.
	code, err := h.Desk.Spend()
	if err != nil {
		h.say("a device tried to pair and there was no code to pair with")
		return
	}
	// THE BOOK IS WRITTEN INSIDE THE EXCHANGE, before the reply that tells the
	// device it is paired — see pairAsMachine. It is passed in rather than done
	// here for exactly that reason: done here it would be done one reply too
	// late, and the device's own next connection could find the book empty.
	admitted, err := pairAsMachine(stream, h.Device.Name(), code, h.Device, h.now(), h.approver(stream), h.Devices.Admit)
	h.say(h.outcome(admitted, err))
	// Every attempt spends the code, so every attempt is followed by the next
	// one: a person who watched a code burn must not have to ask for another.
	h.Desk.Retire()
	if next, err := h.Desk.Offer(); err == nil {
		h.say(strings.TrimRight(Lines(h.Device.Name(), next), "\n"))
	}
}

// approver asks the person, with the connection's deadline held open for as long
// as they are given and put back afterwards. Silence is a no, so a machine
// nobody is sitting at never admits anything.
func (h *Host) approver(stream io.ReadWriteCloser) func(label, words string) error {
	return func(label, words string) error {
		withDeadline(stream, h.now().Add(ConfirmWithin+HandshakeWithin))
		defer withDeadline(stream, h.now().Add(HandshakeWithin))
		if h.Approve == nil || !h.Approve(label, words) {
			return ErrRefused
		}
		return nil
	}
}

// outcomes is what this machine says about how an attempt ended, first match
// wins. The last row matches everything, so an attempt always says something.
var outcomes = []struct {
	is   func(error) bool
	line func(h *Host, one Paired, err error) string
}{
	{func(err error) bool { return err == nil }, func(h *Host, one Paired, _ error) string {
		return fmt.Sprintf("%s is now paired with this machine — it can open conversations here and run what this machine allows", one.Label)
	}},
	{func(err error) bool { return errors.Is(err, ErrRefused) }, func(*Host, Paired, error) string {
		return "a device asked to pair and was not let in"
	}},
	{func(err error) bool { var w notWrittenDown; return errors.As(err, &w) }, func(_ *Host, _ Paired, err error) string {
		return "could not write down that pairing: " + err.Error()
	}},
	{func(error) bool { return true }, func(*Host, Paired, error) string {
		return "someone typed a wrong code, so that code is no longer good"
	}},
}

func (h *Host) outcome(one Paired, err error) string {
	for _, row := range outcomes {
		if row.is(err) {
			return row.line(h, one, err)
		}
	}
	return ""
}

// connect answers a device that has already been paired.
func (h *Host) connect(stream io.ReadWriteCloser) {
	var who Paired
	tunnel, key, err := acceptAsMachine(stream, h.Device, func(key []byte, label string) error {
		found, ok, err := h.Devices.Allows(key)
		if err != nil {
			return errors.New("this machine could not read its own list of devices")
		}
		if !ok {
			// THE SAME SENTENCE FOR A DEVICE THAT WAS STOPPED AND ONE THAT WAS
			// NEVER LET IN, because from this machine's side they are the same
			// fact — it does not know this key — and inventing a difference
			// would mean keeping a list of devices it has forgotten.
			return errors.New(Stopped())
		}
		who = found
		return nil
	})
	if err != nil {
		_ = stream.Close()
		if key != nil {
			h.say("a device this machine does not know tried to connect")
		}
		return
	}
	withDeadline(stream, time.Time{})
	h.Devices.Touch(key, h.now())
	h.say(who.Label + " connected")
	if h.Open == nil {
		_ = tunnel.Close()
		return
	}
	h.Open(tunnel)
	h.say(who.Label + " left")
}

// ThisMachineLabel is what this device calls itself when it pairs. It is the
// host name, because that is the word a person already uses for their laptop,
// and it is only ever a label — a connection is checked against a key.
func ThisMachineLabel() string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return "a device"
	}
	// A host name with a domain on it reads badly in a list of two laptops.
	if dot := strings.IndexByte(name, '.'); dot > 0 {
		name = name[:dot]
	}
	return readableLabel(name)
}
