package clientproto

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// A phone's pairing request carries a MAC over its two public keys, keyed by
// the link key that only the phone and the approving computer hold (the part of
// the token after the dot, which the relay never sees). Both ends compute it
// locally, so a relay that substitutes the phone's keys cannot produce a
// matching MAC, and cannot grind the four digits either: it has nothing to test
// a candidate against. The number the relay supplies is never the one compared.

// BindKey widens the link key into the MAC key, in the shape of the link name
// key (internal/pair/linkkey.go nameKey) with its own label.
func BindKey(link []byte) [32]byte {
	return sha256.Sum256(append([]byte("codeaf/link-bind/v1\n"), link...))
}

// BindMAC is HMAC-SHA256 under BindKey over "client\n" and the two raw 32-byte keys.
func BindMAC(link, signPub, boxPub []byte) [32]byte {
	key := BindKey(link)
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(ClientLinkClass + "\n"))
	mac.Write(signPub)
	mac.Write(boxPub)
	var out [32]byte
	copy(out[:], mac.Sum(nil))
	return out
}

// Bind is the 16 bytes of the MAC the request carries, as the relay stores them.
func Bind(mac [32]byte) []byte { return append([]byte(nil), mac[:ClientLinkBindLen]...) }

// CheckDigits is the four digits both screens show: the next two bytes of the
// MAC as a big-endian integer, modulo 10,000, zero padded.
func CheckDigits(mac [32]byte) string {
	return fmt.Sprintf("%04d", int(binary.BigEndian.Uint16(mac[ClientLinkBindLen:ClientLinkBindLen+2]))%10000)
}
