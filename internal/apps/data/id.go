package data

import (
	"crypto/rand"
	"regexp"
	"time"
)

// crockford is the ULID alphabet: sortable, no I L O U.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

var idShape = regexp.MustCompile(`^[0-7][0-9A-HJKMNP-TV-Z]{25}$`)

// validID reports whether id has the shape of a ULID. A record id that comes
// from a call is checked with it before it is used anywhere.
func validID(id string) bool { return ValidRecordID(id) }

// ValidRecordID reports whether an app reference has the required record ID shape.
func ValidRecordID(id string) bool { return idShape.MatchString(id) }

// idGen makes ULIDs that also sort in the order they were made within one
// millisecond: the random part of a second id in the same millisecond is the
// first one's plus one. Ids are a record's default order, so two records
// created by one batch keep the order they were given in. It is used only by
// the writer goroutine and needs no lock.
type idGen struct {
	lastMS uint64
	last   [16]byte
}

func (g *idGen) next(now time.Time) string {
	ms := uint64(now.UnixMilli())
	var raw [16]byte
	if ms <= g.lastMS {
		raw = g.last
		for i := 15; i >= 6; i-- {
			raw[i]++
			if raw[i] != 0 {
				break
			}
		}
	} else {
		for i := 0; i < 6; i++ {
			raw[i] = byte(ms >> (8 * (5 - i)))
		}
		_, _ = rand.Read(raw[6:])
		// Leave headroom so the increments of one busy millisecond cannot
		// carry into the timestamp.
		raw[6] &= 0x7f
		g.lastMS = ms
	}
	g.last = raw
	return encode128(raw)
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
