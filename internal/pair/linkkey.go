package pair

// The words a person copies between two devices when one asks to join and
// another says yes: a short link, or the same thing typed. The shapes are the
// contract's (docs/ux-pairing-contract.md, section 2); this file reads and
// writes them, and seals the device name under the key that rides in them.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/nacl/secretbox"
)

// LinkOrigin is where a short link points. The page there hands a visitor to the
// installed app, or shows what to run.
const LinkOrigin = "https://codeaf.agentfield.ai"

// linkHost is the one host a pairing link may name; any other host is not a codeaf link.
const linkHost = "codeaf.agentfield.ai"

// linkKeySize is the secret in the link's fragment, which no server ever sees.
const linkKeySize = 16

var b64u = base64.RawURLEncoding

// codeShape is eight characters of Crockford base32.
var codeShape = regexp.MustCompile(`^[0-9a-hjkmnp-tv-z]{8}$`)

// legacyShape is the six-digit code with a nameplate, which keeps its own path.
// Anything made only of digits, spaces and dashes is the older code, so a code
// typed without its dashes keeps meaning what it always did.
var legacyShape = regexp.MustCompile(`^[\d\s-]+$`)

// LinkRef is what a link or a typed token names: the request, and the key that
// opens its device name. Key is nil when only the code was typed.
type LinkRef struct {
	Code string
	Key  []byte
}

// NewLinkKey makes the secret a new link carries.
func NewLinkKey() ([]byte, error) {
	key := make([]byte, linkKeySize)
	_, err := rand.Read(key)
	return key, err
}

// URL is the short link for this reference.
func (r LinkRef) URL() string {
	return LinkOrigin + "/p/" + r.Code + "#" + b64u.EncodeToString(r.Key)
}

// Token is the typed form, `<code>.<key>`.
func (r LinkRef) Token() string { return r.Code + "." + b64u.EncodeToString(r.Key) }

// IsLinkText says whether typed text is a link-pairing code or link, rather than
// the six-digit code of the older way. The shape decides, and the older shape is
// checked first so it never changes meaning.
func IsLinkText(typed string) bool {
	text := strings.TrimSpace(typed)
	if legacyShape.MatchString(text) {
		return false
	}
	_, err := ReadLink(text)
	return err == nil
}

// ReadLink takes a short link, an app link, `<code>.<key>` or a bare code.
func ReadLink(typed string) (LinkRef, error) {
	code, frag := splitLink(strings.TrimSpace(typed))
	code = strings.ToLower(code)
	if !codeShape.MatchString(code) {
		return LinkRef{}, ErrLinkShape
	}
	if frag == "" {
		return LinkRef{Code: code}, nil
	}
	key, err := b64u.DecodeString(frag)
	if err != nil || len(key) != linkKeySize {
		return LinkRef{}, ErrLinkShape
	}
	return LinkRef{Code: code, Key: key}, nil
}

// splitLink cuts text into the code and the key text, whichever form it is in.
func splitLink(text string) (code, key string) {
	if u, err := url.Parse(text); err == nil && u.Scheme != "" && u.Host != "" {
		return fromURL(u)
	}
	if i := strings.IndexAny(text, ".#"); i >= 0 {
		return text[:i], text[i+1:]
	}
	return text, ""
}

// fromURL reads https://codeaf.agentfield.ai/p/<code>#<key> and codeaf://pair?code=<code>#<key>.
func fromURL(u *url.URL) (code, key string) {
	if u.Scheme == "codeaf" {
		return u.Query().Get("code"), u.Fragment
	}
	if rest, ok := strings.CutPrefix(u.Path, "/p/"); ok && u.Host == linkHost {
		return strings.Trim(rest, "/"), u.Fragment
	}
	return "", ""
}

// nameKey widens the link key to the 32 bytes a secretbox takes.
func nameKey(link []byte) *[32]byte {
	sum := sha256.Sum256(append([]byte("codeaf/link-name/v1\n"), link...))
	return &sum
}

// SealDeviceName hides a device name under the link key, as b64u of nonce and box.
func SealDeviceName(linkKey []byte, name string) (string, error) {
	var nonce [24]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	plain := cutBytes(readableLabel(name), maxNameBytes)
	return b64u.EncodeToString(secretbox.Seal(nonce[:], []byte(plain), &nonce, nameKey(linkKey))), nil
}

// maxNameBytes is the most of a name the directory takes before it is sealed.
const maxNameBytes = 96

// cutBytes keeps whole characters up to n bytes.
func cutBytes(s string, n int) string {
	for len(s) > n {
		_, size := utf8.DecodeLastRuneInString(s)
		s = s[:len(s)-size]
	}
	return s
}

var errNameSealed = errors.New("pair: the device name could not be opened")

// OpenDeviceName reverses SealDeviceName. A wrong key or a changed byte is an error.
func OpenDeviceName(linkKey []byte, sealed string) (string, error) {
	raw, err := b64u.DecodeString(sealed)
	if err != nil || len(raw) < 24+secretbox.Overhead || len(linkKey) != linkKeySize {
		return "", errNameSealed
	}
	var nonce [24]byte
	copy(nonce[:], raw)
	plain, ok := secretbox.Open(nil, raw[24:], &nonce, nameKey(linkKey))
	if !ok {
		return "", errNameSealed
	}
	return readableLabel(string(plain)), nil
}
