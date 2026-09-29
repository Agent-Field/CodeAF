package cell

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
)

// crockford is the ULID alphabet: sortable, no I L O U.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var ulidShape = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)

// NewID returns a ULID: 48 bits of unix milliseconds then 80 random bits,
// encoded as 26 Crockford base32 characters. Ids sort by creation time.
func NewID(now time.Time) (string, error) {
	var raw [16]byte
	ms := uint64(now.UnixMilli())
	for i := 0; i < 6; i++ {
		raw[i] = byte(ms >> (8 * (5 - i)))
	}
	if _, err := rand.Read(raw[6:]); err != nil {
		return "", fmt.Errorf("cell id: %w", err)
	}
	return encode128(raw), nil
}

// encode128 writes 128 bits as 26 five-bit groups, most significant first;
// the first group carries only 3 bits.
func encode128(raw [16]byte) string {
	out := make([]byte, 26)
	var acc, bits uint
	pos := 25
	for i := 15; i >= 0; i-- {
		acc |= uint(raw[i]) << bits
		bits += 8
		for bits >= 5 && pos >= 0 {
			out[pos] = crockford[acc&31]
			pos--
			acc >>= 5
			bits -= 5
		}
	}
	if pos == 0 {
		out[0] = crockford[acc&31]
	}
	return string(out)
}

// ValidID reports whether id has the shape of a ULID. Open relies on it so an
// id can never name a path outside the cells directory.
func ValidID(id string) bool { return ulidShape.MatchString(id) }

// newKeyID is 16 random bytes as 32 hex characters (SCHEMAS.md §4).
func newKeyID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("cell key id: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
