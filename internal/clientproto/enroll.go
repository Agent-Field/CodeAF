package clientproto

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// enrollLabel is the first line of the text an enrollment is signed over.
const enrollLabel = "codeaf-enroll-v1\n"

var (
	enrollName   = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)
	enrollDevice = regexp.MustCompile(`^dev_[0-9a-f]{32}$`)
)

// ErrEnrollment says an enrollment is malformed or its signature does not verify.
var ErrEnrollment = errors.New("clientproto: enrollment does not verify")

// Enrollment is what the owner's identity signs to admit one phone: its two
// public keys and what it may do. Every computer reads it from the relay and
// checks it again before it publishes anything to the phone. JSON byte fields
// are standard padded base64, as everywhere on the client wire.
type Enrollment struct {
	V       int      `json:"v"`
	Device  string   `json:"device"`
	SignPub []byte   `json:"sign_pub"`
	BoxPub  []byte   `json:"box_pub"`
	Methods []string `json:"methods"`
	Reads   []string `json:"reads"`
	Ver     uint64   `json:"ver"`
	Sig     []byte   `json:"sig"`
}

// DeviceIDOf is "dev_" and 32 hex characters of the hash of the key, the id
// identity.Cert.DeviceID gives the same key.
func DeviceIDOf(pub []byte) string {
	sum := sha256.Sum256(pub)
	return "dev_" + hex.EncodeToString(sum[:16])
}

// SignText is the exact text the signature covers. Methods and reads are
// sorted by byte so the order a signer listed them in cannot change it, and no
// JSON canonicalization is involved: Go and TypeScript disagree on key order
// and number spelling, and agree on this.
func (e Enrollment) SignText() []byte {
	methods, reads := slices.Clone(e.Methods), slices.Clone(e.Reads)
	slices.Sort(methods)
	slices.Sort(reads)
	return []byte(enrollLabel + e.Device + "\n" + hex.EncodeToString(e.SignPub) + "\n" + hex.EncodeToString(e.BoxPub) +
		"\n" + strings.Join(methods, ",") + "\n" + strings.Join(reads, ",") + "\n" + strconv.FormatUint(e.Ver, 10))
}

// Sign signs e with the identity key and answers the signed copy. Empty lists
// become empty arrays, so the stored JSON never carries a null.
func (e Enrollment) Sign(identity ed25519.PrivateKey) Enrollment {
	if e.Methods == nil {
		e.Methods = []string{}
	}
	if e.Reads == nil {
		e.Reads = []string{}
	}
	e.Sig = ed25519.Sign(identity, e.SignText())
	return e
}

// Validate checks the shape without the signature: the version, the id bound
// to the signing key, key and signature lengths, and the two name lists.
func (e Enrollment) Validate() error {
	if e.V != 1 || e.Ver == 0 || !enrollDevice.MatchString(e.Device) ||
		len(e.SignPub) != ed25519.PublicKeySize || len(e.BoxPub) != 32 || len(e.Sig) != ed25519.SignatureSize ||
		e.Device != DeviceIDOf(e.SignPub) || !names(e.Methods) || !names(e.Reads) {
		return ErrEnrollment
	}
	return nil
}

// names says a list is short, made of well-formed names, and has no repeat.
func names(list []string) bool {
	if len(list) > MaxEnrollNames {
		return false
	}
	seen := map[string]bool{}
	for _, n := range list {
		if !enrollName.MatchString(n) || seen[n] {
			return false
		}
		seen[n] = true
	}
	return true
}

// Verify checks the shape and then the identity's signature.
func (e Enrollment) Verify(identity ed25519.PublicKey) error {
	if len(identity) != ed25519.PublicKeySize || e.Validate() != nil || !ed25519.Verify(identity, e.SignText(), e.Sig) {
		return ErrEnrollment
	}
	return nil
}

// Same says two enrollments are the same statement: every field equal, with the
// two lists compared as sets because the signature covers them sorted.
func (e Enrollment) Same(o Enrollment) bool {
	return e.V == o.V && e.Device == o.Device && e.Ver == o.Ver && string(e.SignPub) == string(o.SignPub) &&
		string(e.BoxPub) == string(o.BoxPub) && string(e.Sig) == string(o.Sig) &&
		sameSet(e.Methods, o.Methods) && sameSet(e.Reads, o.Reads)
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
