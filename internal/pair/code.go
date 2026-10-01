package pair

// The pairing code: six digits, shown on the machine that owns the work, typed
// into the machine that wants in.
//
// SIX DIGITS IS THE WHOLE SECURITY BUDGET FOR THE INTRODUCTION, and it is
// enough only because of what surrounds it. The exchange is a PAKE, so a
// listener — including the relay — learns nothing about the code and cannot
// test guesses offline; an attacker gets one online guess per attempt; and the
// machine that minted the code throws it away after [CodeAttempts] wrong ones
// and after [CodeValidFor]. A million codes, five guesses, ten minutes.
//
// IT IS READ OFF ONE SCREEN AND TYPED INTO ANOTHER, sometimes over a phone
// call, so it is shown with a space in the middle — `715 302` — and accepted
// with or without it.

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"
)

// Code is one live pairing code. A code that pairs chats also carries the
// public nameplate of the mailbox it was made in; a code that pairs a machine is
// found by the machine's name instead, so its plate is empty.
type Code struct {
	digits string
	plate  string
	born   time.Time
	left   int
}

// NewCode mints one. The digits come from crypto/rand and not from the clock,
// a counter, or anything else a watcher could follow.
func NewCode(now time.Time) (*Code, error) {
	limit := big.NewInt(1_000_000)
	drawn, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, err
	}
	return &Code{digits: fmt.Sprintf("%06d", drawn.Int64()), born: now, left: CodeAttempts}, nil
}

// Shown is the code as a person reads it off the screen: `715 302`, and with a
// nameplate `42-715-302`, which is the whole of what the other device types.
func (c *Code) Shown() string {
	spaced := c.digits[:3] + " " + c.digits[3:]
	if c.plate == "" {
		return spaced
	}
	return c.plate + "-" + c.digits[:3] + "-" + c.digits[3:]
}

// Plate is the nameplate of the mailbox this code lives in, or empty.
func (c *Code) Plate() string { return c.plate }

// Born is when this code was made, by the clock it was made with.
func (c *Code) Born() time.Time { return c.born }

// WithPlate is this code placed in the mailbox that was made for it. The digits
// are drawn before the mailbox exists and the plate is assigned by the relay, so
// the two meet here.
func (c *Code) WithPlate(plate string) *Code {
	placed := *c
	placed.plate = plate
	return &placed
}

// secret is the code as the exchange uses it: the six digits, no space.
func (c *Code) secret() string { return c.digits }

// errSixDigits is the sentence for a machine code that is not six digits.
var errSixDigits = errors.New("a pairing code is six digits, like 715 302")

// ReadCode takes what a person typed and answers the six digits, or says what
// is wrong with it.
//
// SPACES AND DASHES ARE THROWN AWAY, because a code read aloud gets written
// down with whichever separator the listener prefers and neither of them is
// wrong.
func ReadCode(typed string) (string, error) {
	digits, ok := digitsOf(typed)
	if !ok || len(digits) != 6 {
		return "", errSixDigits
	}
	return digits, nil
}

// digitsOf is the digits of what was typed with separators dropped, and false
// when anything else was in it.
func digitsOf(typed string) (string, bool) {
	var digits strings.Builder
	for _, r := range typed {
		switch {
		case r >= '0' && r <= '9':
			digits.WriteRune(r)
		case r == ' ' || r == '-' || r == '\t':
		default:
			return "", false
		}
	}
	return digits.String(), true
}

// JoinCode is what a person typed to join a device's chats: the nameplate of
// the mailbox and the six secret digits.
type JoinCode struct{ Plate, Digits string }

// ReadJoinCode takes `42-715-302`, spaces and dashes optional, and separates the
// nameplate from the six digits. It refuses any shape that could not be a code
// before a single request is made: seven to ten digits, the last six the secret.
func ReadJoinCode(typed string) (JoinCode, error) {
	all, ok := digitsOf(typed)
	if !ok || len(all) < 7 || len(all) > 10 {
		return JoinCode{}, ErrCodeShape
	}
	cut := len(all) - 6
	return JoinCode{Plate: all[:cut], Digits: all[cut:]}, nil
}

// Desk is the engine machine's live pairing code: minting it, showing it,
// spending its attempts, and throwing it away.
//
// THERE IS ONE CODE AT A TIME. Two live codes would mean two independent guess
// budgets against the same machine, and a person only ever reads one number off
// the screen anyway.
type Desk struct {
	// Now is the clock, swapped by tests.
	Now func() time.Time

	mu      sync.Mutex
	current *Code
}

func (d *Desk) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// Offer is the code to show right now, minting a fresh one when the last has
// expired or been spent.
func (d *Desk) Offer() (*Code, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current != nil && d.alive(d.current) {
		return d.current, nil
	}
	fresh, err := NewCode(d.now())
	if err != nil {
		return nil, err
	}
	d.current = fresh
	return fresh, nil
}

// Retire throws the current code away — what a successful pairing does, so that
// one code pairs one device.
func (d *Desk) Retire() {
	d.mu.Lock()
	d.current = nil
	d.mu.Unlock()
}

// Spend takes one attempt off the live code and answers what the exchange
// should be run against. A code with nothing left is gone, and the sentence
// says so rather than letting somebody keep guessing.
func (d *Desk) Spend() (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.current == nil || !d.alive(d.current) {
		d.current = nil
		return "", errors.New("there is no pairing code on that machine right now — run `codeaf serve` there and read the new one")
	}
	d.current.left--
	secret := d.current.secret()
	if d.current.left <= 0 {
		// THE LAST ATTEMPT IS SPENT WHETHER OR NOT IT IS RIGHT. A code that
		// survived its own budget so long as the guesses kept being wrong would
		// be a code with no budget at all.
		d.current = nil
	}
	return secret, nil
}

func (d *Desk) alive(c *Code) bool {
	return c.left > 0 && d.now().Sub(c.born) < CodeValidFor
}

// Lines is what `codeaf serve` prints, exactly.
//
// The wording and the spacing are the design's own, and the validity is
// interpolated from [CodeValidFor] rather than typed, because a number that
// appears in two places drifts. Its second line names what the code grants, as
// the pairing screen for chats does in its first: one concept, two grants, and
// each says which one it is.
func Lines(name string, code *Code) string {
	return codeLines("this machine is reachable as  "+name, "let a device use this machine with code   "+code.Shown())
}

// codeLines is the two-line layout every code is shown in: what the code is for
// on the first line, what to do with it on the second, and how long it lasts.
func codeLines(what, how string) string {
	return fmt.Sprintf("  %s\n  %s   (valid %d minutes)\n", what, how, int(CodeValidFor/time.Minute))
}

// ChatLines is what a device that is sharing its chats shows: the grant on the
// line above the code, then the command that takes it. A relay that is not the
// default is named in the command, so the other device never has to guess it.
func ChatLines(code *Code, relay string) string {
	command := "codeaf pair " + code.Shown()
	if relay != "" {
		command += " --via " + relay
	}
	return codeLines("this shares your chats with the device you pair", "on it run  "+command)
}
