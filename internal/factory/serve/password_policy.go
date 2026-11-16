package serve

// ── WHAT A USERNAME AND A PASSWORD MAY BE, AND HOW OFTEN ONE MAY BE TRIED ───
//
// THE POLICY IS NIST SP 800-63B's: a length floor, a length ceiling, any
// characters (spaces and every script included), no composition rules, no
// forced rotation, and a refusal of passwords known to be common.
//
// THE COMMON LIST is every password in SecLists' "100k most used passwords
// (NCSC)" and "xato-net 10 million passwords, top 1,000,000" (MIT licence;
// see common_passwords.LICENSE), plus a handful of passphrases famous enough
// to be guessed first ("correct horse battery staple"), each squashed: NFKC,
// lower-cased, and only its letters and digits kept. A password is squashed
// the same way before it is looked up, so "P.a.s.s.w.o.r.d-1234" is
// "password1234" and is refused. Entries shorter than commonSquashedMost
// letters and digits are left out: they cost the most space and a password
// that squashes that short is a 12-character string of mostly punctuation.
// The list is about 486,000 entries in 1.9 MiB, read once, on first use, into
// one sorted string searched by halves.
//
// How often a password may be tried is password_throttle.go's.

import (
	"bytes"
	"compress/gzip"
	_ "embed"
	"io"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

//go:embed common_passwords.txt.gz
var commonPasswordsGz []byte

// commonSquashedMost is the shortest squashed entry the list keeps.
const commonSquashedMost = 8

var (
	commonOnce sync.Once
	// commonBlob is the whole list, sorted, one entry per line; commonAt is
	// where each entry starts.
	commonBlob string
	commonAt   []int32
)

func loadCommon() {
	zr, err := gzip.NewReader(bytes.NewReader(commonPasswordsGz))
	if err != nil {
		panic("the embedded common-password list is not readable: " + err.Error())
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		panic("the embedded common-password list is not readable: " + err.Error())
	}
	commonBlob = string(raw)
	for i := 0; i < len(commonBlob); {
		end := strings.IndexByte(commonBlob[i:], '\n')
		if end < 0 {
			end = len(commonBlob) - i
		}
		if end > 0 {
			commonAt = append(commonAt, int32(i))
		}
		i += end + 1
	}
}

func commonEntry(k int) string {
	start := int(commonAt[k])
	end := strings.IndexByte(commonBlob[start:], '\n')
	if end < 0 {
		return commonBlob[start:]
	}
	return commonBlob[start : start+end]
}

// squash is a password as the common list keeps it: lower-cased letters and
// digits only. normal is already NFKC.
func squash(normal string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(normal) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// commonPassword is whether a password is one a guesser tries early: on the
// list (by its letters and digits, or, when it has too few of those to be
// listed that way, as it is typed, lower-cased), a few characters repeated,
// or a run along one row of a keyboard. normal is already NFKC.
func commonPassword(normal string) bool {
	commonOnce.Do(loadCommon)
	lower := strings.ToLower(normal)
	if fewCharacters(lower) || keyboardRun(lower) {
		return true
	}
	key := squash(normal)
	if len(key) < commonSquashedMost {
		// Mostly punctuation: the list keeps such entries as typed.
		key = lower
	}
	k := sort.Search(len(commonAt), func(i int) bool { return commonEntry(i) >= key })
	return k < len(commonAt) && commonEntry(k) == key
}

// fewCharacters is a password made of fewer than four different characters,
// such as "!!!!!!!!!!!!" or "abababababab": NIST's "repetitive characters".
func fewCharacters(s string) bool {
	seen := map[rune]bool{}
	for _, r := range s {
		seen[r] = true
		if len(seen) >= 4 {
			return false
		}
	}
	return true
}

// keyboardRows are a US keyboard's rows, unshifted and shifted.
var keyboardRows = []string{"`1234567890-=", "qwertyuiop[]\\", "asdfghjkl;'", "zxcvbnm,./",
	"~!@#$%^&*()_+", "qwertyuiop{}|", "asdfghjkl:\"", "zxcvbnm<>?"}

// keyboardRun is a password that is a run along one keyboard row, forwards
// or backwards, however many times round: "!@#$%^&*()_+", "1234567890-=".
func keyboardRun(s string) bool {
	backwards := []rune(s)
	slices.Reverse(backwards)
	for _, row := range keyboardRows {
		round := strings.Repeat(row, len(s)/len(row)+2)
		if strings.Contains(round, s) || strings.Contains(round, string(backwards)) {
			return true
		}
	}
	return false
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
	// reasonGitHubLink is a sign-up through a link made for one GitHub
	// login, which only that GitHub person can use.
	reasonGitHubLink = "github_link"
)

var refusalWords = map[string]string{
	reasonUsernameTaken:   "That username is taken. Choose another.",
	reasonUsernameInvalid: "A username is 2 to 39 lowercase letters, numbers or dashes, and does not start or end with a dash.",
	reasonPasswordShort:   "A password needs at least 12 characters.",
	reasonPasswordLong:    "A password can be at most 256 bytes long.",
	reasonPasswordCommon:  "That password is on a list of common passwords. Choose another.",
	reasonPasswordName:    "A password cannot contain your username.",
	reasonNameInvalid:     "A name is one line of at most 80 characters.",
	reasonGitHubLink:      "This link is for a GitHub account; sign in with GitHub.",
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
	case containsUsername(normal, username):
		return reasonPasswordName
	case commonPassword(normal):
		return reasonPasswordCommon
	}
	return ""
}

// cleanDisplayName is a person's chosen name, or false. Format characters
// are dropped (see plainText) and a control character refuses the name.
func cleanDisplayName(raw string) (string, bool) {
	if !utf8.ValidString(raw) {
		return "", false
	}
	name := strings.Join(strings.Fields(strings.Map(dropFormat, raw)), " ")
	if utf8.RuneCountInString(name) > 80 {
		return "", false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", false
		}
	}
	return name, true
}

// plainText is s without control or format characters. A name someone
// else chose is shown inside the server's own sentences, and a right-to-left
// override (U+202E) or an isolate (U+2066–U+2069) left open in it would
// reorder those sentences; zero-width and C1 characters hide what it says.
func plainText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return dropFormat(r)
	}, strings.ToValidUTF8(s, ""))
}

// dropFormat drops a Unicode format character (category Cf: bidi
// controls, zero-width marks and the like).
func dropFormat(r rune) rune {
	if unicode.Is(unicode.Cf, r) {
		return -1
	}
	return r
}

// containsUsername is whether the password holds the username, compared
// squashed (so "S.a.n.t.o.s.h" holds santosh). A username of one or two
// letters and digits is left out: nearly every password holds two letters.
func containsUsername(normal, username string) bool {
	name := squash(username)
	return len(name) >= 3 && strings.Contains(squash(normal), name)
}
