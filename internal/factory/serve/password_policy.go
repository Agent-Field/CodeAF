package serve

// ── WHAT A USERNAME AND A PASSWORD MAY BE, AND HOW OFTEN ONE MAY BE TRIED ───
//
// THE POLICY IS NIST SP 800-63B's: a length floor, a length ceiling, any
// characters (spaces and every script included), no composition rules, no
// forced rotation, and a refusal of passwords known to be common. The common
// list is SecLists' "100k most used passwords (NCSC)" (MIT licence; see
// common_passwords.LICENSE), NFKC-normalized and lower-cased, keeping only the
// entries at least passwordMinChars long: a shorter one can never equal a
// password the length rule lets through, so it would only cost space.
//
// THE THROTTLE IS TWO SETS OF golang.org/x/time/rate BUCKETS: one per username
// (10 failures per 15 minutes) and one per client address (50 per 15 minutes),
// the address being the one the server's own trusted-proxy resolver settled
// (proxy.go), never a header read here. A username whose bucket is empty also
// backs off exponentially, 30 seconds doubling to at most 15 minutes, and that
// backoff decays once the name has been quiet for 15 minutes: an attacker can
// slow the real owner down, never lock them out for good. Each set is bounded;
// a full set first forgets the keys that have recovered, then the oldest.

import (
	"bufio"
	"bytes"
	"compress/gzip"
	_ "embed"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/time/rate"
)

//go:embed common_passwords.txt.gz
var commonPasswordsGz []byte

var (
	commonOnce sync.Once
	commonSet  map[string]struct{}
)

func commonPassword(normal string) bool {
	commonOnce.Do(func() {
		commonSet = map[string]struct{}{}
		zr, err := gzip.NewReader(bytes.NewReader(commonPasswordsGz))
		if err != nil {
			return
		}
		sc := bufio.NewScanner(zr)
		for sc.Scan() {
			if line := sc.Text(); line != "" {
				commonSet[line] = struct{}{}
			}
		}
	})
	_, found := commonSet[strings.ToLower(normal)]
	return found
}

// usernameShape is GitHub's: 2 to 39 of a-z, 0-9 and "-", not starting or
// ending with "-". It is a subset of every handle the server accepts.
var usernameShape = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,37}[a-z0-9]$`)

// The refusal reasons a sign-up, a change or a reset may carry, with the
// sentence each one reads as. These are the one spelling.
const (
	reasonUsernameTaken   = "username_taken"
	reasonUsernameInvalid = "username_invalid"
	reasonPasswordShort   = "password_short"
	reasonPasswordLong    = "password_long"
	reasonPasswordCommon  = "password_common"
	reasonPasswordName    = "password_contains_username"
	reasonNameInvalid     = "name_invalid"
)

var refusalWords = map[string]string{
	reasonUsernameTaken:   "That username is taken. Choose another.",
	reasonUsernameInvalid: "A username is 2 to 39 lowercase letters, numbers or dashes, and does not start or end with a dash.",
	reasonPasswordShort:   "A password needs at least 12 characters.",
	reasonPasswordLong:    "A password can be at most 256 bytes long.",
	reasonPasswordCommon:  "That password is on a list of common passwords. Choose another.",
	reasonPasswordName:    "A password cannot contain your username.",
	reasonNameInvalid:     "A name is one line of at most 80 characters.",
}

// The sentences every sign-in refusal shares, whatever the cause.
const (
	signInRefusedWords = "That username and password don't match."
	tooManyTriesWords  = "Too many tries. Wait a few minutes."
	passwordHTTPSWords = "Passwords are only sent to this server over https."
)

// cleanUsername is the username as it is kept, or "" when it is not one.
func cleanUsername(raw string) string {
	u := strings.ToLower(strings.TrimSpace(raw))
	if !usernameShape.MatchString(u) {
		return ""
	}
	return u
}

// passwordRefusal names the rule a new password breaks, or "". normal is
// already normalized; username is the person's own.
func passwordRefusal(normal, username string) string {
	switch {
	case len(normal) > passwordMaxBytes:
		return reasonPasswordLong
	case utf8.RuneCountInString(normal) < passwordMinChars:
		return reasonPasswordShort
	case username != "" && strings.Contains(strings.ToLower(normal), username):
		return reasonPasswordName
	case commonPassword(normal):
		return reasonPasswordCommon
	}
	return ""
}

// cleanDisplayName is a person's chosen name, or false.
func cleanDisplayName(raw string) (string, bool) {
	name := strings.Join(strings.Fields(raw), " ")
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) > 80 {
		return "", false
	}
	for _, r := range name {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	return name, true
}

const (
	throttleWindow  = 15 * time.Minute
	userFailures    = 10
	addrFailures    = 50
	throttleKeyMost = 10000
	backoffFirst    = 30 * time.Second
	backoffMost     = 15 * time.Minute
	backoffForgiven = 15 * time.Minute
)

type throttleEntry struct {
	lim     *rate.Limiter
	strikes int
	until   time.Time
	seen    time.Time
}

// throttle is one bounded set of buckets. backoff turns on the exponential
// wait for a key whose bucket is empty (the username set).
type throttle struct {
	mu      sync.Mutex
	keys    map[string]*throttleEntry
	burst   int
	every   time.Duration
	backoff bool
	most    int
}

func newThrottle(failures int, backoff bool) *throttle {
	return &throttle{keys: map[string]*throttleEntry{}, burst: failures,
		every: throttleWindow / time.Duration(failures), backoff: backoff, most: throttleKeyMost}
}

// open is whether key may be tried at now. Asking spends nothing.
func (t *throttle) open(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.keys[key]
	if e == nil {
		return true
	}
	return !now.Before(e.until) && e.lim.TokensAt(now) >= 1
}

// fail spends one try of key, and answers true the first time the key runs
// dry since it last recovered, which is when a summary is worth writing down.
func (t *throttle) fail(key string, now time.Time) (ranDry bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e := t.keys[key]
	if e == nil {
		if len(t.keys) >= t.most {
			t.evict(now)
		}
		e = &throttleEntry{lim: rate.NewLimiter(rate.Every(t.every), t.burst)}
		t.keys[key] = e
	}
	if now.Sub(e.seen) >= backoffForgiven {
		e.strikes = 0
	}
	e.seen = now
	wasOpen := e.lim.TokensAt(now) >= 1
	e.lim.AllowN(now, 1)
	dry := e.lim.TokensAt(now) < 1
	if dry && t.backoff {
		// Each time the name runs dry it waits twice as long as the last,
		// on top of the bucket's own refill, until it has been quiet a while.
		wait := backoffFirst << min(e.strikes, 10)
		e.until = now.Add(min(wait, backoffMost))
		e.strikes++
	}
	// Only the first run dry of a quiet spell is worth a line in the record.
	return dry && wasOpen && e.strikes <= 1
}

// evict makes room: keys that have fully recovered go first, then the one
// seen longest ago. Called with mu held.
func (t *throttle) evict(now time.Time) {
	var oldest string
	var oldestSeen time.Time
	for k, e := range t.keys {
		if !now.Before(e.until) && e.lim.TokensAt(now) >= float64(t.burst) {
			delete(t.keys, k)
			continue
		}
		if oldest == "" || e.seen.Before(oldestSeen) {
			oldest, oldestSeen = k, e.seen
		}
	}
	if len(t.keys) >= t.most && oldest != "" {
		delete(t.keys, oldest)
	}
}

// passwordGuard is the server's two throttles.
type passwordGuard struct {
	users *throttle
	addrs *throttle
}

func newPasswordGuard() *passwordGuard {
	return &passwordGuard{users: newThrottle(userFailures, true), addrs: newThrottle(addrFailures, false)}
}
