// Package id ports src/id/id.ts:4-85 (swe-pro 3b25a1a). IDs encode a
// millisecond timestamp and per-timestamp counter in six big-endian bytes,
// followed by 14 modulo-biased base62 characters.
package id

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

const (
	encodedLength = 26
	randomLength  = encodedLength - 12
	base62        = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
)

var prefixes = map[string]string{
	"event":      "evt",
	"session":    "ses",
	"message":    "msg",
	"permission": "per",
	"question":   "que",
	"user":       "usr",
	"part":       "prt",
	"pty":        "pty",
	"tool":       "tool",
	"workspace":  "wrk",
	"entry":      "ent",
	"account":    "act",
}

// Direction selects the timestamp byte ordering used by Create.
type Direction string

const (
	AscendingDirection  Direction = "ascending"
	DescendingDirection Direction = "descending"
)

// Generator owns the monotonic counter and its injectable side effects.
type Generator struct {
	mu            sync.Mutex
	lastTimestamp int64
	counter       uint64
	now           func() int64
	random        io.Reader
}

// NewGenerator constructs an independent ID generator. Nil inputs use the
// wall clock and crypto/rand.
func NewGenerator(now func() int64, random io.Reader) *Generator {
	if now == nil {
		now = func() int64 { return time.Now().UnixMilli() }
	}
	if random == nil {
		random = rand.Reader
	}
	return &Generator{now: now, random: random}
}

var defaultGenerator = NewGenerator(nil, nil)

// Prefix returns the short prefix corresponding to a TS prefix key.
func Prefix(kind string) (string, bool) {
	value, ok := prefixes[kind]
	return value, ok
}

// SchemaAccepts mirrors schema(kind).safeParse(value).success.
func SchemaAccepts(kind, value string) bool {
	prefix, ok := Prefix(kind)
	return ok && strings.HasPrefix(value, prefix)
}

// Ascending mirrors Identifier.ascending. An absent or empty given value
// generates a new ID; any other value is prefix-checked and returned verbatim.
func Ascending(kind string, given ...string) (string, error) {
	return defaultGenerator.generateKind(kind, AscendingDirection, given...)
}

// Descending mirrors Identifier.descending.
func Descending(kind string, given ...string) (string, error) {
	return defaultGenerator.generateKind(kind, DescendingDirection, given...)
}

// Ascending generates with this Generator.
func (g *Generator) Ascending(kind string, given ...string) (string, error) {
	return g.generateKind(kind, AscendingDirection, given...)
}

// Descending generates with this Generator.
func (g *Generator) Descending(kind string, given ...string) (string, error) {
	return g.generateKind(kind, DescendingDirection, given...)
}

func (g *Generator) generateKind(kind string, direction Direction, given ...string) (string, error) {
	prefix, ok := Prefix(kind)
	if !ok {
		return "", fmt.Errorf("unknown ID prefix %s", kind)
	}
	if len(given) == 0 || given[0] == "" {
		return g.Create(prefix, direction)
	}
	if !strings.HasPrefix(given[0], prefix) {
		return "", fmt.Errorf("ID %s does not start with %s", given[0], prefix)
	}
	return given[0], nil
}

// Create uses the process-global generator and an optional explicit timestamp.
func Create(prefix string, direction Direction, timestamp ...int64) (string, error) {
	return defaultGenerator.Create(prefix, direction, timestamp...)
}

// Create emits an ID from this Generator. State resets only when the current
// timestamp differs from the previous one; moving the clock backwards is
// intentionally accepted, matching the TS.
func (g *Generator) Create(prefix string, direction Direction, timestamp ...int64) (string, error) {
	current := g.now()
	if len(timestamp) > 0 {
		current = timestamp[0]
	}

	g.mu.Lock()
	defer g.mu.Unlock()
	if current != g.lastTimestamp {
		g.lastTimestamp = current
		g.counter = 0
	}
	g.counter++

	// JS BigInt is unbounded, then the extraction loop observes only the low 48
	// bits. Converting to uint64 before the shifts gives the same two's-
	// complement low bits for negative timestamps and for `~now`.
	now := uint64(current)*0x1000 + g.counter
	if direction == DescendingDirection {
		now = ^now
	}
	var timeBytes [6]byte
	for i := range 6 {
		timeBytes[i] = byte(now >> uint(40-8*i))
	}
	random, err := g.randomBase62(randomLength)
	if err != nil {
		return "", err
	}
	return prefix + "_" + hex.EncodeToString(timeBytes[:]) + random, nil
}

func (g *Generator) randomBase62(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := io.ReadFull(g.random, bytes); err != nil {
		return "", err
	}
	out := make([]byte, length)
	for i, value := range bytes {
		out[i] = base62[int(value)%len(base62)]
	}
	return string(out), nil
}

// Timestamp extracts the millisecond timestamp from an ascending ID. Like the
// TS function, it does not invert descending IDs.
func Timestamp(value string) (int64, error) {
	prefix := strings.Split(value, "_")[0]
	start := len(prefix) + 1
	end := start + 12
	if start > len(value) {
		start = len(value)
	}
	if end > len(value) {
		end = len(value)
	}
	hexPart := value[start:end]
	if hexPart == "" {
		return 0, fmt.Errorf("invalid ID %q", value)
	}
	bytes, err := hex.DecodeString(hexPart)
	if err != nil || len(bytes) == 0 {
		if err == nil {
			err = fmt.Errorf("empty timestamp")
		}
		return 0, fmt.Errorf("invalid ID %q: %w", value, err)
	}
	var encoded uint64
	for _, value := range bytes {
		encoded = encoded<<8 | uint64(value)
	}
	return int64(encoded / 0x1000), nil
}
