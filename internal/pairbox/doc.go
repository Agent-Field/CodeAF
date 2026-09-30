// Package pairbox is the pairing mailbox: a place two devices that share only a
// short code can leave each other opaque messages for ten minutes.
//
// A device that shows a code creates a mailbox and gets a public nameplate; the
// device that types the code writes to the other side of it. The mailbox never
// makes a secret and never learns one: each side chooses its own random key,
// the mailbox keeps only a digest of it, and every message is ciphertext the
// two devices produced between themselves. That is why one implementation can
// serve a self-hosted relay and a hosted one alike, and why the exported suite
// in pairboxtest is the only definition of "a mailbox works".
//
// State is memory only. A restart drops every live pairing, and a pairing is
// ten minutes long anyway.
//
// The package may not import the packages that hold what a mailbox must never
// see (contract section 18.4); law_test.go reads that out of the source.
package pairbox
