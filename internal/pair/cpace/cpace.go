// Copyright 2020 Google LLC
//
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file or at
// https://developers.google.com/open-source/licenses/bsd

// VENDORED, DELIBERATELY, AND THIS PARAGRAPH IS THE REASON.
//
// This file is filippo.io/cpace at v0.0.0-20210101143347-24d601e2e469, copied
// here byte for byte apart from this comment. LICENSE beside it is Google's own
// and is kept because the licence requires it.
//
// WHY A COPY RATHER THAN A DEPENDENCY: the upstream module has never been
// tagged. It is not abandoned in the sense of being broken — it is a clean,
// small implementation by a well-known cryptographer — but nobody has promised
// to maintain it, so depending on it meant depending on a 2021 snapshot with no
// release behind it. Two hundred lines is small enough that one person can read
// the whole thing and understand it, which is more assurance than an unowned
// dependency was ever giving us. THE PRICE IS THAT THE BUG IS OURS NOW, and
// that is the trade being made on purpose rather than by drift.
//
// WHAT WAS FOUND READING IT, so that the next person does not have to start
// cold. Nothing here is a defect; they are the things worth knowing:
//
//   - ContextInfo.serialize length-prefixes every field, so two different
//     (idA, idB, ad) triples cannot serialize to the same bytes. That is what
//     stops one party's identity bleeding into another's.
//   - deriveKey checks for the identity element even though the comment
//     explains it is unnecessary for a prime-order group with a checking
//     decode. It is cheap, and it is the kind of check whose absence ages
//     badly.
//   - Start and Exchange do NOT report a wrong password. They cannot: a PAKE
//     that told you the guess was wrong would be an oracle for guessing it. The
//     two sides simply derive different keys, and the caller learns it when the
//     NEXT handshake fails to decrypt (internal/pair runs Noise NNpsk0 over
//     this key, which is exactly that check). A caller that expected an error
//     here would silently accept a stranger.
//   - secretGenerator ignores hkdf's Read error. It cannot fail for 64 bytes
//     out of SHA-256, whose limit is 8160.
//   - State.Finish appends msgB into a transcript slice with spare capacity, so
//     calling Finish twice on one State would write over the first call's
//     transcript. Our flow finishes once; a caller that retried would need a
//     fresh Start, which is also what the protocol wants.
//
// To compare against upstream later:
//
//	go mod download -x filippo.io/cpace@v0.0.0-20210101143347-24d601e2e469

// Package cpace implements the CPace password authenticated key exchange (PAKE)
// instantiated with the ristretto255 group.
//
// PAKEs allow two peers to establish a shared secret key if they agree on a
// password or similar low-entropy value, without letting eavesdropping or
// machine-in-the-middle attackers make multiple attempts at guessing the
// password value. CPace is a balanced PAKE, meaning that both peers need to
// know the password plaintext.
//
// This implementation is loosely based on draft-haase-cpace-01.
package cpace

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"

	"github.com/gtank/ristretto255"
	"golang.org/x/crypto/cryptobyte"
	"golang.org/x/crypto/hkdf"
)

// ContextInfo captures the additional connection information that the two peers
// need to agree on for the key to be the same.
type ContextInfo struct {
	idA, idB string
	ad       []byte
}

// NewContextInfo returns a ContextInfo for use with Start or Exchange.
//
// idA represents the identity of the party that uses Start, idB of the party
// that uses Exchange. Identities could be MAC addresses, or IPs and ports.
//
// ad is any additional context the two parties share, and can be nil. Examples
// of values that could be included in ad to protect against protocol downgrade
// and mismatch attacks are the name and transcript of the higher level
// protocol, including any negotiation inputs that led to the use of this PAKE.
func NewContextInfo(idA, idB string, ad []byte) *ContextInfo {
	return &ContextInfo{
		idA: idA, idB: idB, ad: ad,
	}
}

func (c *ContextInfo) validate() error {
	switch {
	case c == nil:
		return errors.New("cpace: ContextInfo can't be nil")
	case len(c.idA) >= 1<<16:
		return errors.New("cpace: idA too long")
	case len(c.idB) >= 1<<16:
		return errors.New("cpace: idB too long")
	case len(c.ad) >= 1<<16:
		return errors.New("cpace: additional data too long")
	default:
		return nil
	}
}

const label = "cpace-r255"

func (c *ContextInfo) serialize() []byte {
	b := &cryptobyte.Builder{}
	for _, in := range [][]byte{
		[]byte(label), []byte(c.idA), []byte(c.idB), c.ad,
	} {
		b.AddUint16LengthPrefixed(func(b *cryptobyte.Builder) {
			b.AddBytes(in)
		})
	}
	return b.BytesOrPanic()
}

func secretGenerator(password string, salt []byte, c *ContextInfo) *ristretto255.Element {
	h := hkdf.New(sha256.New, []byte(password), salt, c.serialize())
	b := make([]byte, 64)
	h.Read(b)
	return ristretto255.NewElement().FromUniformBytes(b)
}

func randomScalar() (*ristretto255.Scalar, error) {
	b := make([]byte, 64)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return ristretto255.NewScalar().FromUniformBytes(b), nil
}

// State is a PAKE session in progress, where the initiating party is waiting
// for the peer response.
type State struct {
	transcript []byte
	secret     *ristretto255.Scalar
}

// Start initiates a new PAKE exchange authenticated by password. msgA should be
// sent to the peer, to be processed by Exchange, and s used to process the
// peer's response.
func Start(password string, c *ContextInfo) (msgA []byte, s *State, err error) {
	if err := c.validate(); err != nil {
		return nil, nil, err
	}

	s = &State{}

	salt := make([]byte, 16, 16+32)
	if _, err := rand.Read(salt); err != nil {
		return nil, nil, err
	}

	s.secret, err = randomScalar()
	if err != nil {
		return nil, nil, err
	}

	A := secretGenerator(password, salt, c)
	A.ScalarMult(s.secret, A)

	msgA = A.Encode(salt)

	s.transcript = make([]byte, 16+32, 16+32+32)
	copy(s.transcript, msgA)

	return msgA, s, nil
}

var identity = ristretto255.NewElement()

func deriveKey(peerElement, transcript []byte, secret *ristretto255.Scalar) ([]byte, error) {
	K := ristretto255.NewElement()
	if err := K.Decode(peerElement); err != nil {
		return nil, errors.New("cpace: invalid peer message")
	}
	K.ScalarMult(secret, K)

	// draft-haase-cpace-01 requires checking for the identity element at this
	// stage, but also overloads the identity as the output for invalid peer
	// points (for curves where that check is necessary like P-256) and for
	// degenerate scalar multiplications (for curves with cofactors). In a safe
	// prime order group with an error-checking decode function such as
	// ristretto255, this should not be necessary, as the transcript hash
	// guarantees contributory behavior, but it's cheap so we do it anyway.
	if K.Equal(identity) == 1 {
		return nil, errors.New("cpace: invalid peer message")
	}

	h := hmac.New(sha256.New, transcript)
	h.Write(K.Encode(nil))
	return h.Sum(nil), nil
}

// Finish processes the peer's response, generated by Exchange, and returns the
// shared secret key.
//
// If the two peers agree on the password and ContextInfo, they will derive the
// same key. Note that an error is NOT returned otherwise: the two peers will
// simply derive different keys.
//
// The returned key is suitable to be passed to hkdf.Expand.
func (s *State) Finish(msgB []byte) (key []byte, err error) {
	if len(msgB) != 32 {
		return nil, errors.New("cpace: invalid peer message")
	}

	transcript := append(s.transcript, msgB...)
	return deriveKey(msgB, transcript, s.secret)
}

// Exchange executes a PAKE exchange authenticated by password, processing msgA
// generated by a peer with Start, and returns the shared secret key and msgB.
// msgB should be sent to the peer, to be processed by (*State).Finish.
//
// If the two peers agree on the password and ContextInfo, they will derive the
// same key. Note that an error is NOT returned otherwise: the two peers will
// simply derive different keys.
//
// The returned key is suitable to be passed to hkdf.Expand.
func Exchange(password string, c *ContextInfo, msgA []byte) (msgB, key []byte, err error) {
	if err := c.validate(); err != nil {
		return nil, nil, err
	}

	if len(msgA) != 16+32 {
		return nil, nil, errors.New("cpace: invalid peer message")
	}
	salt := msgA[:16]
	encodedA := msgA[16:]

	secret, err := randomScalar()
	if err != nil {
		return nil, nil, err
	}

	x := secretGenerator(password, salt, c)
	x.ScalarMult(secret, x)

	msgB = make([]byte, 0, 32)
	msgB = x.Encode(msgB)

	transcript := make([]byte, 0, 16+32+32)
	transcript = append(transcript, msgA...)
	transcript = append(transcript, msgB...)

	key, err = deriveKey(encodedA, transcript, secret)
	if err != nil {
		return nil, nil, err
	}

	return msgB, key, nil
}
