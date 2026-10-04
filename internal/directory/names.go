package directory

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"unicode/utf8"

	"golang.org/x/crypto/chacha20poly1305"
)

// MaxTitleBytes is how much of a title is kept before it is sealed.
const MaxTitleBytes = 120

var (
	b64      = base64.RawURLEncoding
	nameAAD  = []byte("codeaf:name:v1")
	metaInfo = []byte("codeaf:metadata-key:v1")

	errSealedShort = errors.New("directory: sealed name too short")
)

// MetadataKey derives the key that seals names from a cell key.
func MetadataKey(cellKey []byte) []byte {
	mac := hmac.New(sha256.New, cellKey)
	mac.Write(metaInfo)
	return mac.Sum(nil)
}

// SealName encrypts plain under key with a random nonce and returns
// b64(nonce ‖ ciphertext). The directory never sees the name.
func SealName(key []byte, plain string) (string, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return b64.EncodeToString(aead.Seal(nonce, nonce, []byte(plain), nameAAD)), nil
}

// OpenName reverses SealName. A wrong key or a changed byte is an error.
func OpenName(key []byte, sealed string) (string, error) {
	aead, err := chacha20poly1305.NewX(key)
	if err != nil {
		return "", err
	}
	raw, err := b64.DecodeString(sealed)
	if err != nil {
		return "", err
	}
	if len(raw) < aead.NonceSize() {
		return "", errSealedShort
	}
	nonce, body := raw[:aead.NonceSize()], raw[aead.NonceSize():]
	plain, err := aead.Open(nil, nonce, body, nameAAD)
	return string(plain), err
}

// SealTitle cuts a title to MaxTitleBytes, never inside a character, and seals it.
func SealTitle(key []byte, title string) (string, error) {
	return SealName(key, cutBytes(title, MaxTitleBytes))
}

func cutBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	for max > 0 && !utf8.RuneStart(s[max]) {
		max--
	}
	return s[:max]
}
