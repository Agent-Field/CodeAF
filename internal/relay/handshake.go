package relay

// THE RELAY CHECKS ONE THING AND ONE THING ONLY: that whoever is claiming a
// name holds the private half of the key that name is derived from. That is
// what stops a name being taken by anybody who has merely SEEN a machine's
// public key, and it is the whole of the relay's cryptography. It never
// authenticates a surface, because a surface has nothing the relay could check
// against — the surface is authenticated end to end by the machine it dials, by
// a handshake this package cannot read.
//
// THE PROOF IS A DIFFIE-HELLMAN AND NOT A SIGNATURE, because a machine's
// long-term key is an X25519 key — Noise's key, the key the name is derived
// from, the key a surface pins — and an X25519 key cannot sign. So the relay
// sends an ephemeral public key and a nonce, and the machine answers with a MAC
// under the shared secret. Only the holder of the private half can compute it.
//
// THE LABEL BELOW IS LOAD-BEARING. The same static key is used by Noise above
// this package, and reusing a key across two protocols is only safe when the
// two can never be made to produce the same bytes. Every derivation from this
// key in this tree carries a distinct label saying what it is for, and this one
// says "relay registration" and nothing else.

import (
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
)

// registrationLabel domain-separates this proof from every other use of a
// machine's long-term key.
const registrationLabel = "aforge relay registration v1"

// challengeBytes is the relay's ephemeral public key followed by a fresh nonce.
const challengeBytes = 32 + 32

// proofBytes is the MAC the machine answers with.
const proofBytes = 32

// The one-byte verdicts the relay writes once it has read a proof. A verdict
// rather than an HTTP status because the upgrade has already happened by then,
// and a person watching `aforge serve` needs a sentence rather than a number.
const (
	verdictReady   byte = 1
	verdictRefused byte = 2
)

// EncodeKey and DecodeKey are how a public key travels in a header. Raw URL
// base64 because a header is a header: no padding to be stripped by a proxy, no
// slash to be read as a path.
func EncodeKey(key []byte) string { return base64.RawURLEncoding.EncodeToString(key) }

func DecodeKey(text string) ([]byte, error) {
	key, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return nil, errors.New("that key is not readable")
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("a machine key is 32 bytes and that one is %d", len(key))
	}
	return key, nil
}

// challengeMachine is the relay's half: write a challenge, read a proof, say
// whether it held. It returns nil when the machine proved it holds the key.
func challengeMachine(conn io.ReadWriter, name string, publicKey []byte) error {
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	challenge := make([]byte, 0, challengeBytes)
	challenge = append(challenge, ephemeral.PublicKey().Bytes()...)
	challenge = append(challenge, nonce...)
	if _, err := conn.Write(challenge); err != nil {
		return err
	}

	offered, err := ecdh.X25519().NewPublicKey(publicKey)
	if err != nil {
		return errors.New("that key is not a usable machine key")
	}
	shared, err := ephemeral.ECDH(offered)
	if err != nil {
		return errors.New("that key is not a usable machine key")
	}
	want, err := registrationProof(shared, nonce, name, publicKey)
	if err != nil {
		return err
	}

	got := make([]byte, proofBytes)
	if _, err := io.ReadFull(conn, got); err != nil {
		return err
	}
	if subtle.ConstantTimeCompare(want, got) != 1 {
		return errors.New("that machine did not prove it holds the key its name is derived from")
	}
	return nil
}

// proveToRelay is the machine's half: read the challenge, answer it, read the
// verdict.
func proveToRelay(conn io.ReadWriter, name string, private *ecdh.PrivateKey) error {
	challenge := make([]byte, challengeBytes)
	if _, err := io.ReadFull(conn, challenge); err != nil {
		return err
	}
	ephemeral, err := ecdh.X25519().NewPublicKey(challenge[:32])
	if err != nil {
		return errors.New("the relay sent a challenge this build cannot answer")
	}
	shared, err := private.ECDH(ephemeral)
	if err != nil {
		return errors.New("the relay sent a challenge this build cannot answer")
	}
	proof, err := registrationProof(shared, challenge[32:], name, private.PublicKey().Bytes())
	if err != nil {
		return err
	}
	if _, err := conn.Write(proof); err != nil {
		return err
	}
	return readVerdict(conn)
}

// registrationProof binds the shared secret to the nonce, the name and the key,
// so that a proof captured on one registration cannot be replayed onto another.
func registrationProof(shared, nonce []byte, name string, publicKey []byte) ([]byte, error) {
	key, err := hkdf.Key(sha256.New, shared, nonce, registrationLabel, 32)
	if err != nil {
		return nil, err
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(name))
	mac.Write(publicKey)
	return mac.Sum(nil), nil
}

// writeVerdict is the relay's last word on a registration.
func writeVerdict(w io.Writer, reason string) error {
	if reason == "" {
		_, err := w.Write([]byte{verdictReady, 0})
		return err
	}
	if len(reason) > 255 {
		reason = reason[:255]
	}
	if _, err := w.Write([]byte{verdictRefused, byte(len(reason))}); err != nil {
		return err
	}
	_, err := w.Write([]byte(reason))
	return err
}

// readVerdict is the machine reading it. A refusal arrives as the relay's own
// sentence, which is always truer than anything this side could infer.
func readVerdict(r io.Reader) error {
	var head [2]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return err
	}
	if head[0] == verdictReady {
		return nil
	}
	reason := make([]byte, head[1])
	if _, err := io.ReadFull(r, reason); err != nil {
		return err
	}
	if len(reason) == 0 {
		return errors.New("the relay refused this machine's registration")
	}
	return errors.New(string(reason))
}
