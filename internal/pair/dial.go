package pair

// `aforge chat --at otter-lamp-42`: the surface side.
//
// THE FIRST TIME IT PAIRS AND EVERY TIME AFTER THAT IT JUST OPENS. That is the
// whole shape of the door, and the reason the pairing prompt lives here rather
// than in a separate `aforge pair` command: a person types the command they
// want to use, and pairing is a question that comes up on the way, once.
//
// EVERYTHING THAT MIGHT ASK A QUESTION HAPPENS BEFORE THE SURFACE TAKES THE
// SCREEN. That is the ssh door's own law (cmd/aforge's chatv3_host.go) and it
// applies here for the same reason: a code typed into a full-screen surface
// would be a code typed at a frame drawn over the top of it.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/relay"
)

// The three lines a pairing shows, kept here so that the code that prints them
// and the manual page that quotes them cannot drift apart.

// PairingWeight is what a person reads BEFORE they type a code. It is the whole
// security posture of this feature in one sentence, in a person's words.
//
// A PAIRED DEVICE IS A HAND ON THAT MACHINE'S TOOLS. Saying so at the moment of
// the decision is not a disclaimer; it is the only moment at which the sentence
// can do any good.
const PairingWeight = "a paired device is a key to that machine: it opens conversations there, runs whatever that machine allows to run, and spends that machine's model key. it is the same weight as an ssh key."

// PairingPreamble names what is about to happen.
func PairingPreamble(name string) string {
	return "pairing with " + name
}

// PairingPrompt is the line the code is typed after.
func PairingPrompt(name string) string {
	return "enter the code shown on " + name + ": "
}

// PairedLine is what a person reads when it worked.
func PairedLine(name string) string {
	return "paired. this device is now a key to " + name + "."
}

// Reach is one attempt to open a connection to a named machine.
type Reach struct {
	// Name is what the person typed after --at.
	Name string
	// Device is this device's key.
	Device Device
	// Machines is this device's book of machines it has paired with.
	Machines *Book
	// Label is what this device calls itself; see [ThisMachineLabel].
	Label string
	// AskCode is asked for the pairing code when this device is not yet paired
	// with the machine. NIL MEANS THIS DOOR CANNOT ASK — a headless run, a
	// script — and an unpaired machine is then refused with [NotPaired]
	// rather than hanging on a prompt nobody is there to answer.
	AskCode func(name string) (string, error)
	// Say is where the pairing's own lines go. Nil says nothing.
	Say func(string)
	// Now is the clock, swapped by tests.
	Now func() time.Time
}

func (r Reach) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r Reach) say(line string) {
	if r.Say != nil {
		r.Say(line)
	}
}

// Open answers a live tunnel to the machine, pairing first if it has to.
//
// EVERY ROAD OUT OF HERE IS ONE SENTENCE NAMING WHAT IS WRONG. There are four
// ways this fails that a person cannot tell apart by looking, and errors.go
// keeps one sentence for each.
func (r Reach) Open(ctx context.Context) (*Tunnel, error) {
	if !relay.ValidName(r.Name) {
		return nil, fmt.Errorf("%q is not the shape of a machine name — they look like otter-lamp-42, and `aforge serve` prints the one for a machine", r.Name)
	}
	known, found, err := r.Machines.Machine(r.Name)
	if err != nil {
		return nil, err
	}
	service := RelayFor(known, found)
	if strings.TrimSpace(service) == "" {
		return nil, NoRelay(r.Name)
	}

	if !found {
		if r.AskCode == nil {
			return nil, NotPaired(r.Name)
		}
		paired, err := r.pair(ctx, service)
		if err != nil {
			return nil, err
		}
		known = paired
	}

	machineKey, err := decodeStoredKey(known.Key)
	if err != nil {
		return nil, fmt.Errorf("what this device remembers about %s is not readable any more — pair with it again", r.Name)
	}

	stream, err := r.dial(ctx, service)
	if err != nil {
		return nil, err
	}
	withDeadline(stream, r.now().Add(HandshakeWithin))
	tunnel, err := connectAsSurface(stream, machineKey, r.Device, r.Label)
	if err != nil {
		_ = stream.Close()
		if errors.Is(err, ErrNotThatMachine) {
			return nil, NotThatMachine(r.Name)
		}
		if errors.Is(err, ErrNoAnswer) {
			return nil, NoAnswer(r.Name)
		}
		return nil, err
	}
	withDeadline(stream, time.Time{})
	return tunnel, nil
}

// pair runs the introduction and writes what it learned into this device's book.
func (r Reach) pair(ctx context.Context, service string) (Known, error) {
	stream, err := r.dial(ctx, service)
	if err != nil {
		return Known{}, err
	}
	defer func() { _ = stream.Close() }()

	r.say(PairingPreamble(r.Name))
	r.say(PairingWeight)
	typed, err := r.AskCode(r.Name)
	if err != nil {
		return Known{}, err
	}
	code, err := ReadCode(typed)
	if err != nil {
		return Known{}, err
	}

	withDeadline(stream, r.now().Add(HandshakeWithin))
	learned, err := pairAsSurface(stream, service, r.Name, code, r.Label, r.Device, r.now())
	if err != nil {
		if errors.Is(err, ErrWrongCode) {
			return Known{}, WrongCode(r.Name)
		}
		return Known{}, err
	}
	if err := r.Machines.Remember(learned); err != nil {
		return Known{}, err
	}
	r.say(PairedLine(r.Name))
	return learned, nil
}

// dial reaches the relay and turns its facts into this package's sentences.
func (r Reach) dial(ctx context.Context, service string) (io.ReadWriteCloser, error) {
	stream, err := relay.Dial(ctx, service, r.Name)
	switch {
	case err == nil:
		return stream, nil
	case errors.Is(err, relay.ErrNoMachine):
		return nil, NotConnected(r.Name)
	case errors.Is(err, relay.ErrUnreachable):
		return nil, Unreachable(service)
	case errors.Is(err, relay.ErrTooMany):
		return nil, Busy()
	default:
		return nil, err
	}
}

// Machines is every machine this device can reach, for `aforge devices` on a
// surface and for a door that wants to say which names it knows.
func Machines() ([]Known, error) { return MachineBook().Machines() }
