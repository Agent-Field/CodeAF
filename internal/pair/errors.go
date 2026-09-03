package pair

// ONE SENTENCE, NAMING THE THING THAT IS WRONG. Never a stack trace, never a
// hang, and never a sentence that covers two different causes.
//
// This matters more here than almost anywhere else in aforge, because `--at`
// has four completely different ways to not work and a person cannot tell them
// apart by looking: there is no relay set up; the relay is not answering; the
// machine is not connected to it; this device was never let in. Each one has a
// different next step, and a shrug that covered all four would send somebody
// to check their wifi when the answer was `aforge serve`.
//
// THE RELAY SERVICE IS NOT DEPLOYED YET, and the first of these is therefore
// the sentence most people will meet. It says what to do about it rather than
// apologising: `--host` over ssh works today and is not going anywhere.

import (
	"errors"
	"fmt"
	"time"
)

// The facts, so a caller can tell them apart before phrasing them.
var (
	// ErrNoRelay is this machine having no relay address at all.
	ErrNoRelay = errors.New("pair: no relay is configured")
	// ErrNotPaired is this device never having been let in to that machine.
	ErrNotPaired = errors.New("pair: this device is not paired with that machine")
	// ErrWrongCode is the pairing code not matching.
	ErrWrongCode = errors.New("pair: that pairing code is not the one shown there")
	// ErrNotThatMachine is a completed dial to something that does not hold
	// the key this device pinned.
	ErrNotThatMachine = errors.New("pair: that is not the machine this device paired with")
	// ErrNoAnswer is a handshake that got no reply at all.
	ErrNoAnswer = errors.New("pair: that machine did not answer the handshake")
)

// NoRelay is the first sentence: nothing is configured.
func NoRelay(name string) error {
	return fmt.Errorf("no relay is set up on this machine, so --at has nowhere to look for %s — set %s to a relay's address, or reach that machine with --host over ssh", name, RelayEnv)
}

// Unreachable is the second: something is configured and it is not answering.
func Unreachable(service string) error {
	return fmt.Errorf("the relay at %s cannot be reached from here — check this machine's network, or reach that machine with --host over ssh", service)
}

// NotConnected is the third: the relay is fine and that machine is not there.
func NotConnected(name string) error {
	return fmt.Errorf("%s is not connected to the relay right now — run `aforge serve` on that machine", name)
}

// NotPaired is the fourth: this device has never been let in.
func NotPaired(name string) error {
	return fmt.Errorf("this device is not paired with %s — run `aforge serve` on that machine, then run this command again and type the code it shows", name)
}

// WrongCode is the pairing code being wrong, said with the fact that makes it
// worth trying again.
func WrongCode(name string) error {
	return fmt.Errorf("that is not the code shown on %s — read it again, and note that it is only good for %d minutes", name, int(CodeValidFor/time.Minute))
}

// NotThatMachine is the pinned key not matching. It is the sentence that says
// the safety property held: the connection stopped rather than opening.
func NotThatMachine(name string) error {
	return fmt.Errorf("whatever is answering to %s is not the machine this device paired with, so nothing was sent — pair again from that machine if it was rebuilt", name)
}

// NoAnswer is the other shape the same protection takes.
//
// A MACHINE THAT IS NOT THE ONE THIS DEVICE PAIRED WITH CANNOT EVEN READ THE
// FIRST MESSAGE, so it does not answer at all — and neither does a connection
// that dropped. The two are one event from this side and this sentence covers
// both truthfully, because the fact that matters is the same either way:
// nothing of the conversation left this device.
func NoAnswer(name string) error {
	return fmt.Errorf("%s did not answer this device's handshake, so nothing was sent — if that machine was rebuilt it has a new key and this device has to pair with it again", name)
}

// Busy is the relay turning connections away.
func Busy() error {
	return errors.New("the relay is turning connections away right now — try again in a minute")
}

// Stopped is what a machine says to a device it no longer lets in. It travels
// inside the encryption, so it is this machine's own words and not the relay's.
func Stopped() string {
	return "this device has been stopped on that machine — pair it again from there"
}

// notWrittenDown marks the one pairing failure that is this machine's own disk
// rather than the digits somebody typed.
//
// IT NEVER LEAVES THIS MACHINE. The last message of the pairing carries a key
// and a name and has carried nothing else in any build, so there is no sentence
// a machine can put on the wire here without changing what that message is; a
// machine that cannot write the pairing down therefore hangs up, exactly as it
// does for a wrong code. What this type is for is the other end of that: the
// line printed on the screen of the machine somebody is actually sitting at,
// which is where a full disk or a bad path can be acted on, and which would
// otherwise have said the person mistyped six digits.
type notWrittenDown struct{ err error }

func (n notWrittenDown) Error() string { return n.err.Error() }
func (n notWrittenDown) Unwrap() error { return n.err }
