// Package pair is how two machines come to trust each other without ssh, and
// the encrypted tunnel they talk through afterwards.
//
// THE WHOLE STORY IN SIX LINES:
//
//	big-machine$ aforge serve
//	  this machine is reachable as  otter-lamp-42
//	  pair a new device with code   715 302   (valid 10 minutes)
//
//	laptop$ aforge chat --at otter-lamp-42
//	  pairing with otter-lamp-42 — enter the code shown there: ______
//	  paired. this laptop is now a key to otter-lamp-42.
//
// After that, `aforge chat --at otter-lamp-42` from that laptop just opens.
//
// ── the three facts this package is built on ────────────────────────────────
//
// FIRST: THE RELAY BROKERS THE INTRODUCTION AND IS NEVER TRUSTED WITH IT. The
// pairing runs a PAKE — a password-authenticated key exchange — over the six
// digits shown on the engine machine's own screen. Somebody in the middle who
// does not know those digits cannot learn the shared key, cannot guess at it
// offline, and gets exactly ONE guess per attempt, which the engine machine
// counts and cuts off. That is the property a plain shared secret over a
// brokered channel would not have, and it is why the code can be six digits
// instead of a paragraph.
//
// SECOND: THE PAIRING PINS KEYS, AND EVERY LATER CONNECTION IS BETWEEN PINNED
// KEYS. The PAKE is used once, to carry two long-term public keys past the
// relay safely. From then on the surface dials with a Noise handshake against
// the key it pinned, and the machine answers only devices whose key is in its
// own book. The code is never used again and cannot be replayed.
//
// THIRD: A PAIRED DEVICE IS A HAND ON THAT MACHINE'S TOOLS. It is not a
// read-only window and it is not a login to a website. It opens conversations
// on that machine, runs the tools that machine's gate allows, and spends that
// machine's key — the same weight as an ssh key, and the pairing screen says so
// in those words before anybody types a code.
//
// ── what is NOT here, said plainly ──────────────────────────────────────────
//
// THE DEVICE KEY IS A FILE, NOT A KEYCHAIN ENTRY, AND THERE IS NO TOUCH ID.
// The design for this lane puts the device key in the OS keychain where there
// is one, so that the platform can demand a fingerprint before releasing it and
// aforge never sees a biometric. That is the right design and it is not built.
// What is built is [Keeper] — the seam it goes behind — with one implementation:
// a file with owner-only permissions under the aforge home directory. Every
// person-facing sentence in this package says "a file on this machine" because
// that is what it is, and the manual page says the same. A CAPABILITY THAT
// CANNOT WORK IS ABSENT, NOT BROKEN.
package pair

import "time"

// The protocol label. It is mixed into the PAKE's context, into the Noise
// prologue and into the tunnel's own greeting, so that two builds that would
// disagree about any byte below cannot complete a handshake and quietly mean
// different things.
const protocol = "aforge-pair/1"

// CodeValidFor is how long a pairing code shown by `aforge serve` is good for.
// It is quoted in the person-facing line, and there is exactly one of it.
const CodeValidFor = 10 * time.Minute

// CodeAttempts is how many wrong codes a single code will absorb before it is
// thrown away.
//
// IT IS THE OTHER HALF OF WHAT MAKES SIX DIGITS ENOUGH. A PAKE gives an
// attacker one guess per exchange and no way to test a guess offline; a small
// cap on exchanges is what turns "one guess at a time" into "five guesses,
// ever". A million codes and five guesses is the whole of the arithmetic.
const CodeAttempts = 5

// HandshakeWithin bounds every exchange in this package. A connection that has
// arrived but not finished proving who it is holds a slot, and a slot held for
// ever is the cheapest denial of service there is.
const HandshakeWithin = 30 * time.Second
