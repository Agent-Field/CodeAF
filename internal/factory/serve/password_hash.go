package serve

// ── PASSWORD HASHING ────────────────────────────────────────────────────────
//
// A password is kept ONLY as an argon2id hash in a PHC string,
// $argon2id$v=19$m=<KiB>,t=<passes>,p=<lanes>$<salt>$<key>, salt and key in
// unpadded standard base64. The parameters ride in the string so they can rise
// later: a sign-in that verifies against weaker stored parameters is rehashed
// at the current ones without the person doing anything.
//
// THE CRYPTO IS golang.org/x/crypto/argon2's. What is written here is only the
// PHC encode and decode around it, which a fuzz test holds to round-tripping
// and to never panicking on hostile input.
//
// HASHING IS HEAVY BY DESIGN (46 MiB and a pass of CPU each), so a server-wide
// semaphore admits a few at a time; a burst of sign-ins waits briefly for a
// slot and is then told to try again, rather than taking the machine's memory.
//
// THE PASSWORD IS NFKC-NORMALIZED before it is hashed, so one passphrase typed
// on two systems is one password. Nothing is truncated; a password over
// passwordMaxBytes after normalization is refused before any hashing.

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/text/unicode/norm"
)

// argonParams are one argon2id cost. The current ones are OWASP's 2025
// argon2id floor: 46 MiB, one pass, one lane.
type argonParams struct {
	memory  uint32 // KiB
	time    uint32
	threads uint8
}

const (
	argonKeyLen  = 32
	argonSaltLen = 16
	// passwordMinChars and passwordMaxBytes are the length rules a person is
	// told; the first counts characters after normalization, the second bytes.
	passwordMinChars = 12
	passwordMaxBytes = 256
	// hashSlotsMost is how many hashes may run at once on the whole server,
	// and hashSlotWait how long a request waits for one before it is refused.
	hashSlotsMost = 4
	hashSlotWait  = 3 * time.Second
)

// owaspArgon is OWASP's 2025 argon2id floor. currentArgon is what a new hash
// is made with: owaspArgon, which only this package's tests lower (so a suite
// of sign-ins does not spend 46 MiB each); production never changes it.
var owaspArgon = argonParams{memory: 47104, time: 1, threads: 1}
var currentArgon = owaspArgon

// argonRuns counts argon2 computations, so a test can prove an unknown
// username pays the same one a known one does.
var argonRuns atomic.Int64

var hashSlots = make(chan struct{}, hashSlotsMost)

// errHashBusy is a full semaphore: the caller answers 429.
var errHashBusy = errors.New("password hashing is busy")

// errBadPHC is a stored hash this build cannot read. Its words name no part
// of the string.
var errBadPHC = errors.New("stored password hash is not readable")

func withHashSlot(run func()) error {
	timer := time.NewTimer(hashSlotWait)
	defer timer.Stop()
	select {
	case hashSlots <- struct{}{}:
	case <-timer.C:
		return errHashBusy
	}
	defer func() { <-hashSlots }()
	argonRuns.Add(1)
	run()
	return nil
}

// normalPassword is the password as it is hashed and judged.
func normalPassword(password string) string { return norm.NFKC.String(password) }

// hashPassword makes a new PHC string for password, already normalized.
func hashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	p := currentArgon
	var key []byte
	if err := withHashSlot(func() { key = argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, argonKeyLen) }); err != nil {
		return "", err
	}
	return encodePHC(p, salt, key), nil
}

// verifyPassword is whether password, already normalized, is the one phc was
// made from, and whether phc's parameters are weaker than today's. The key is
// compared in constant time.
func verifyPassword(password, phc string) (ok, weaker bool, err error) {
	p, salt, want, err := decodePHC(phc)
	if err != nil {
		return false, false, err
	}
	var got []byte
	if err := withHashSlot(func() { got = argon2.IDKey([]byte(password), salt, p.time, p.memory, p.threads, uint32(len(want))) }); err != nil {
		return false, false, err
	}
	ok = subtle.ConstantTimeCompare(got, want) == 1
	weaker = p.memory < currentArgon.memory || p.time < currentArgon.time || p.threads < currentArgon.threads
	return ok, weaker, nil
}

var (
	dummyOnce sync.Once
	dummyPHC  string
)

// dummyHash is a fixed hash at today's parameters of a random password nobody
// knows. A sign-in for a username that does not exist verifies against it, so
// it pays the same argon2 cost as one that does and its time says nothing.
func dummyHash() string {
	dummyOnce.Do(func() {
		secret := make([]byte, 32)
		_, _ = rand.Read(secret)
		salt := make([]byte, argonSaltLen)
		_, _ = rand.Read(salt)
		p := currentArgon
		dummyPHC = encodePHC(p, salt, argon2.IDKey(secret, salt, p.time, p.memory, p.threads, argonKeyLen))
	})
	return dummyPHC
}

var phcB64 = base64.RawStdEncoding

func encodePHC(p argonParams, salt, key []byte) string {
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, p.memory, p.time, p.threads,
		phcB64.EncodeToString(salt), phcB64.EncodeToString(key))
}

// decodePHC reads a PHC argon2id string. Every bound is checked before any
// allocation the string could size: a hostile or corrupt row cannot make the
// verify ask for gigabytes or spin for minutes.
func decodePHC(phc string) (argonParams, []byte, []byte, error) {
	parts := strings.Split(phc, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return argonParams{}, nil, nil, errBadPHC
	}
	var p argonParams
	seen := map[string]bool{}
	for _, kv := range strings.Split(parts[3], ",") {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || seen[k] {
			return argonParams{}, nil, nil, errBadPHC
		}
		seen[k] = true
		n, err := strconv.ParseUint(v, 10, 32)
		if err != nil || n == 0 || (len(v) > 1 && v[0] == '0') {
			return argonParams{}, nil, nil, errBadPHC
		}
		switch k {
		case "m":
			p.memory = uint32(n)
		case "t":
			p.time = uint32(n)
		case "p":
			if n > 255 {
				return argonParams{}, nil, nil, errBadPHC
			}
			p.threads = uint8(n)
		default:
			return argonParams{}, nil, nil, errBadPHC
		}
	}
	// 1 GiB, 16 passes and 16 lanes are far past any cost this server sets.
	if len(seen) != 3 || p.memory < 8*uint32(p.threads) || p.memory > 1<<20 || p.time > 16 || p.threads > 16 {
		return argonParams{}, nil, nil, errBadPHC
	}
	salt, err := phcB64.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return argonParams{}, nil, nil, errBadPHC
	}
	key, err := phcB64.Strict().DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return argonParams{}, nil, nil, errBadPHC
	}
	return p, salt, key, nil
}
