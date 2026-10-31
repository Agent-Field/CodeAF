package agentdid

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
)

// Credentials hold private keys only in memory. JSON and formatting expose no
// key material; persistence goes through Document.Protect exclusively.
type Credentials struct {
	signing    [ed25519.SeedSize]byte
	encryption [32]byte
	hasSigning bool
}

// String prevents ordinary diagnostic formatting from revealing key material.
func (Credentials) String() string { return "agent credentials (private)" }

// GoString prevents detailed diagnostic formatting from revealing key material.
func (c Credentials) GoString() string { return c.String() }

// Generate mints both keys without writing anything to disk.
func Generate(session string) (Document, *Credentials, error) {
	c := &Credentials{hasSigning: true}
	if _, err := rand.Read(c.signing[:]); err != nil {
		return Document{}, nil, err
	}
	d := Document{V: Version, Session: session, pending: c, DID: FromPublicKey(c.public())}
	if err := c.addEncryption(&d); err != nil {
		return Document{}, nil, err
	}
	d.Signing = &Key{Type: "Ed25519", Public: encode(c.public())}
	return d, c, nil
}

// Upgrade preserves a public-only DID whose discarded private key cannot be
// recovered. It adds encryption and states the lack of signing custody honestly.
func Upgrade(d Document) (Document, *Credentials, error) {
	if d.V != "" || !Valid(d.DID) {
		return Document{}, nil, ErrDocument
	}
	c := &Credentials{}
	d.V, d.SigningUnavailable = Version, true
	d.Signing = &Key{Type: "Ed25519", Public: encode(publicKey(d.DID))}
	if err := c.addEncryption(&d); err != nil {
		return Document{}, nil, err
	}
	return d, c, nil
}

func (c *Credentials) addEncryption(d *Document) error {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	copy(c.encryption[:], key.Bytes())
	d.Encryption = &Key{Type: "X25519", Public: encode(key.PublicKey().Bytes())}
	return nil
}

func (c *Credentials) public() ed25519.PublicKey {
	return ed25519.NewKeyFromSeed(c.signing[:]).Public().(ed25519.PublicKey)
}

func encode(b []byte) string { return base64.RawURLEncoding.EncodeToString(b) }

func publicKey(did string) ed25519.PublicKey {
	if !Valid(did) {
		return nil
	}
	raw, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(did, prefix))
	return ed25519.PublicKey(raw[len(multicodec):])
}

// purposeBody frames each component so a purpose or payload cannot be confused
// with an adjacent field. Every application purpose carries its own version.
func purposeBody(label, did, purpose string, payload []byte) ([]byte, error) {
	separator := strings.LastIndex(purpose, "/v")
	if separator <= 0 {
		return nil, errors.New("agent identity: purpose must end in /v<number>")
	}
	version := purpose[separator+2:]
	if version == "" || version[0] == '0' {
		return nil, errors.New("agent identity: purpose must end in /v<number>")
	}
	for _, ch := range version {
		if ch < '0' || ch > '9' {
			return nil, errors.New("agent identity: invalid purpose version")
		}
	}
	out := []byte(label)
	for _, value := range [][]byte{[]byte(did), []byte(purpose), payload} {
		out = binary.BigEndian.AppendUint64(out, uint64(len(value)))
		out = append(out, value...)
	}
	return out, nil
}

// Sign signs a purpose-framed statement only as the DID owned by these keys.
func (c *Credentials) Sign(did, purpose string, payload []byte) ([]byte, error) {
	if c == nil || !c.hasSigning {
		return nil, ErrUnavailable
	}
	if did != FromPublicKey(c.public()) {
		return nil, ErrDocument
	}
	body, err := purposeBody("codeaf/agent-signature/v1", did, purpose, payload)
	if err != nil {
		return nil, err
	}
	return ed25519.Sign(ed25519.NewKeyFromSeed(c.signing[:]), body), nil
}

// Verify needs only the public DID, never a cell, identity document or home key.
func Verify(did, purpose string, payload, signature []byte) bool {
	public := publicKey(did)
	if public == nil {
		return false
	}
	body, err := purposeBody("codeaf/agent-signature/v1", did, purpose, payload)
	return err == nil && ed25519.Verify(public, body, signature)
}

func aeadFor(key []byte, label string) (cipher.AEAD, error) {
	derived, err := hkdf.Key(sha256.New, key, nil, label, 32)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(derived)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func encrypt(key, plain, aad []byte, label string) ([]byte, error) {
	aead, err := aeadFor(key, label)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return aead.Seal(nonce, nonce, plain, aad), nil
}

func decrypt(key, box, aad []byte, label string) ([]byte, error) {
	aead, err := aeadFor(key, label)
	if err != nil {
		return nil, err
	}
	if len(box) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrDocument
	}
	return aead.Open(nil, box[:aead.NonceSize()], box[aead.NonceSize():], aad)
}

func (d Document) binding() []byte {
	body, _ := purposeBody("codeaf/agent-custody/v1", d.DID, "custody/v1", []byte(d.Session))
	return body
}

// Protect encrypts both private keys under the profile's cell key. The nonce
// and ciphertext travel inside the cell; the wrapping key never does.
func (d *Document) Protect(c *Credentials, cellKey []byte) error {
	if len(cellKey) != 32 || d.V != Version || c == nil {
		return ErrDocument
	}
	if err := c.matches(*d); err != nil {
		return err
	}
	plain := make([]byte, 65)
	if c.hasSigning {
		plain[0] = 1
	}
	copy(plain[1:33], c.signing[:])
	copy(plain[33:], c.encryption[:])
	defer clear(plain)
	box, err := encrypt(cellKey, plain, d.binding(), "codeaf/agent-custody/v1")
	if err == nil {
		d.Custody = box
		d.pending = nil
	}
	return err
}

// Unlock refuses transplanted, corrupt and unsupported custody without
// exposing private material in errors.
func (d Document) Unlock(cellKey []byte) (*Credentials, error) {
	if d.V == Version && len(d.Custody) == 0 && d.pending != nil {
		return d.pending, d.pending.matches(d)
	}
	if d.V != Version || len(cellKey) != 32 {
		return nil, ErrDocument
	}
	plain, err := decrypt(cellKey, d.Custody, d.binding(), "codeaf/agent-custody/v1")
	if err != nil {
		return nil, fmt.Errorf("agent identity: custody could not be opened")
	}
	defer clear(plain)
	if len(plain) != 65 || plain[0] > 1 {
		return nil, ErrDocument
	}
	c := &Credentials{hasSigning: plain[0] == 1}
	copy(c.signing[:], plain[1:33])
	copy(c.encryption[:], plain[33:])
	return c, c.matches(d)
}

func (c *Credentials) matches(d Document) error {
	if d.Signing == nil || d.Encryption == nil || d.Signing.Type != "Ed25519" || d.Encryption.Type != "X25519" {
		return ErrDocument
	}
	if d.Signing.Public != encode(publicKey(d.DID)) || !Valid(d.DID) {
		return ErrDocument
	}
	if c.hasSigning == d.SigningUnavailable {
		return ErrDocument
	}
	if c.hasSigning && FromPublicKey(c.public()) != d.DID {
		return ErrDocument
	}
	key, err := ecdh.X25519().NewPrivateKey(c.encryption[:])
	if err != nil || d.Encryption.Public != encode(key.PublicKey().Bytes()) {
		return ErrDocument
	}
	return nil
}

// Seal encrypts material to the document's X25519 key, using a fresh ephemeral
// sender key. The recipient and purpose are authenticated with the ciphertext.
func Seal(d Document, purpose string, payload []byte) ([]byte, error) {
	if d.V != Version || d.Encryption == nil || d.Encryption.Type != "X25519" || !Valid(d.DID) {
		return nil, ErrDocument
	}
	aad, err := purposeBody("codeaf/agent-envelope/v1", d.DID, purpose, nil)
	if err != nil {
		return nil, err
	}
	raw, err := base64.RawURLEncoding.DecodeString(d.Encryption.Public)
	if err != nil {
		return nil, ErrDocument
	}
	public, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		return nil, err
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	shared, err := ephemeral.ECDH(public)
	if err != nil {
		return nil, err
	}
	defer clear(shared)
	box, err := encrypt(shared, payload, aad, "codeaf/agent-envelope/v1")
	if err != nil {
		return nil, err
	}
	return append(ephemeral.PublicKey().Bytes(), box...), nil
}

// Open decrypts material only with this document's private companion key.
func (c *Credentials) Open(d Document, purpose string, box []byte) ([]byte, error) {
	if c == nil || d.V != Version || len(box) < 32 {
		return nil, ErrDocument
	}
	if err := c.matches(d); err != nil {
		return nil, err
	}
	aad, err := purposeBody("codeaf/agent-envelope/v1", d.DID, purpose, nil)
	if err != nil {
		return nil, err
	}
	public, err := ecdh.X25519().NewPublicKey(box[:32])
	if err != nil {
		return nil, err
	}
	key, err := ecdh.X25519().NewPrivateKey(c.encryption[:])
	if err != nil {
		return nil, err
	}
	shared, err := key.ECDH(public)
	if err != nil {
		return nil, err
	}
	defer clear(shared)
	return decrypt(shared, box[32:], aad, "codeaf/agent-envelope/v1")
}
