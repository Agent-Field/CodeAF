package pairbox

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"
)

// Side names one end of a mailbox. A is the device that showed the code.
type Side string

// The two sides of a mailbox.
const (
	SideA Side = "a"
	SideB Side = "b"
)

// Valid reports whether s names a side that exists.
func (s Side) Valid() bool { return s == SideA || s == SideB }

// index is where a side's state lives in a two-element array.
func (s Side) index() int {
	if s == SideA {
		return 0
	}
	return 1
}

// KeySize is the length of the key a side chooses.
const KeySize = 16

// Key is the secret a side chooses when it first writes. It authorises later
// writes from the same side and nothing else; the mailbox keeps its digest.
type Key [KeySize]byte

// NewKey draws a side key from the system's random source.
func NewKey() (Key, error) {
	var k Key
	_, err := rand.Read(k[:])
	return k, err
}

// String is the wire form: base64url without padding.
func (k Key) String() string { return base64.RawURLEncoding.EncodeToString(k[:]) }

// ParseKey reads the wire form, refusing anything that is not exactly a key.
func ParseKey(text string) (Key, error) {
	raw, err := base64.RawURLEncoding.DecodeString(text)
	var k Key
	if err != nil || len(raw) != KeySize {
		return k, errors.New("pairbox: a side key is 16 bytes, base64url")
	}
	copy(k[:], raw)
	return k, nil
}

// digest is what a mailbox keeps of a key.
func (k Key) digest() [sha256.Size]byte { return sha256.Sum256(k[:]) }

// Created answers a new mailbox: its public name and how long it will live,
// counted by the mailbox's own clock so a device's skew cannot matter.
type Created struct {
	Nameplate string
	ExpiresIn time.Duration
}

// Batch is what a side has written since a position. An empty Msgs is the
// answer of a wait that ended with nothing new; Next is then the position asked
// from, so the caller asks again from the same place.
type Batch struct {
	Msgs [][]byte
	Next int
}

// Box is the mailbox as a device sees it. Memory implements it in process and
// HTTP reaches one over the wire, and both answer with the same errors.
type Box interface {
	// Limits are the numbers this mailbox enforces, so a device and a
	// conformance run test what the mailbox says and not what they assume.
	Limits(ctx context.Context) (Limits, error)
	// Create opens a mailbox and claims side a for key.
	Create(ctx context.Context, key Key) (Created, error)
	// Post appends msg to a side; the first write to b claims it. It answers
	// the message's index on that side.
	Post(ctx context.Context, nameplate string, side Side, key Key, msg []byte) (int, error)
	// Poll answers what side has written after position after, waiting up to
	// wait (capped at MaxWait) when there is nothing yet. It needs no key: the
	// messages are ciphertext.
	Poll(ctx context.Context, nameplate string, side Side, after int, wait time.Duration) (Batch, error)
	// Delete removes a mailbox for the holder of either side's key and wakes
	// every poll on it.
	Delete(ctx context.Context, nameplate string, key Key) error
}

// MaxWait is the longest a poll is held, whatever it asks for.
const MaxWait = 25 * time.Second

// Limits is the one table both relays read (contract 18.4). The first five
// fields are what GET /v1/pair/limits answers; the rest are enforced but not
// advertised.
type Limits struct {
	TTL            time.Duration `json:"-"`
	MaxMsg         int           `json:"max_msg"`
	MaxMsgsPerSide int           `json:"max_msgs_per_side"`
	CreatePerHour  int           `json:"create_per_hour"`
	WritePerMinute int           `json:"write_per_minute"`

	MaxBoxes   int `json:"-"`
	MaxBytes   int `json:"-"`
	PollsPerIP int `json:"-"`
}

// DefaultLimits are the numbers of contract 18.4.
var DefaultLimits = Limits{
	TTL:            10 * time.Minute,
	MaxMsg:         4 << 10,
	MaxMsgsPerSide: 4,
	CreatePerHour:  10,
	WritePerMinute: 30,
	MaxBoxes:       2000,
	MaxBytes:       64 << 20,
	PollsPerIP:     4,
}

// The reasons a mailbox says no. Each is one fact a device can act on.
var (
	// ErrForbidden is a key that does not own the side, or a side someone else
	// already claimed.
	ErrForbidden = errors.New("pairbox: that side belongs to someone else")
	// ErrGone is a mailbox that never existed, expired or was deleted.
	ErrGone = errors.New("pairbox: no pairing is waiting there")
	// ErrSideFull is a side that already holds its share of messages.
	ErrSideFull = errors.New("pairbox: that side is full")
	// ErrTooBig is a message over the size limit.
	ErrTooBig = errors.New("pairbox: message too big")
	// ErrRelayFull is a mailbox service at one of its global caps.
	ErrRelayFull = errors.New("pairbox: the relay is full")
	// ErrRateLimited is any per-network limit; the concrete error is RateLimited.
	ErrRateLimited = errors.New("pairbox: too many requests from this network")
)

// RateLimited is a per-network limit, with how long until it lifts.
type RateLimited struct{ RetryAfter time.Duration }

func (r RateLimited) Error() string {
	return fmt.Sprintf("%v; retry in %v", ErrRateLimited, r.RetryAfter.Round(time.Second))
}

// Is lets errors.Is(err, ErrRateLimited) match a RateLimited.
func (r RateLimited) Is(target error) bool { return target == ErrRateLimited }

// peerKey is the context key under which the caller's network address travels.
type peerKey struct{}

// WithPeer marks ctx as coming from a network address, which is what the
// per-network limits count. A handler sets it from the request; a test sets it
// to be a network of its own.
func WithPeer(ctx context.Context, peer string) context.Context {
	return context.WithValue(ctx, peerKey{}, peer)
}

func peerOf(ctx context.Context) string {
	if p, ok := ctx.Value(peerKey{}).(string); ok && p != "" {
		return p
	}
	return "local"
}
