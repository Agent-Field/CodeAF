// Package poolkey is the one place the binary's trusted pool key lives.
//
// THE KEY HAS ONE HOME AND IT IS HERE. A fetched index's detached signature is
// checked under an ed25519 public key the build carries, and the same key
// appears in the relay's wrangler.toml and in the mirror workflow — but in Go
// it is spelled exactly once, in this package, and every reader of it
// (cmd/codeaf's verbs and the seed generator beside the index) imports this
// rather than repeating the base64. A second spelling is a key two readers can
// drift apart on, and a signature one of them cannot check.
//
// The key is the index signer's public half, and it is public: the private seed
// is the relay's POOL_SIGNING_KEY secret and is never stored in the repository.
package poolkey

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
)

// encoded is the index signer's ed25519 public key, standard base64. It is the
// same key the relay publishes as POOL_PUBLIC_KEY (relay/wrangler.toml) and the
// mirror carries in its workflow env.
const encoded = "WOAo+g/oKxAV9vVqv2Q14w1TyyyiouwFO2fC0zgcps0="

// keys is the decoded key, done once at init: a literal that could not decode
// would verify nothing, so the failure is loud and immediate rather than a
// signature check that quietly refuses every document.
var keys = mustDecode(encoded)

// Keys answers with the built-in trusted keys, as a COPY: the list is read by
// many callers and handed into a puller, and neither the slice nor a key's
// bytes are the caller's to change.
func Keys() []ed25519.PublicKey {
	out := make([]ed25519.PublicKey, len(keys))
	for i, k := range keys {
		out[i] = append(ed25519.PublicKey(nil), k...)
	}
	return out
}

// Encoded answers with the base64 text the key was built from, for a caller
// that has to say the key rather than check under it.
func Encoded() string { return encoded }

// mustDecode decodes the built-in literal, or panics. It runs at init beside
// the variable it fills, so a build whose literal is not a 32-byte ed25519
// public key fails on start rather than at the first fetch.
func mustDecode(text string) []ed25519.PublicKey {
	raw, err := base64.StdEncoding.DecodeString(text)
	if err != nil || len(raw) != ed25519.PublicKeySize {
		panic(fmt.Sprintf("poolkey: the built-in public key does not decode: %q", text))
	}
	return []ed25519.PublicKey{ed25519.PublicKey(raw)}
}
