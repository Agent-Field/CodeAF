package trace

import (
	"regexp"
	"strings"
	"sync"
)

// THE RECORD IS A PERSON'S OWN DATA AND A KEY IS NOT IN IT.
//
// The bodies written here are the JSON that travelled, and headers are never
// captured at all — so in the ordinary case there is nothing to redact. Scrub
// is defensive rather than corrective: a provider that echoes its own
// authorization back in an error object, a tool result that quotes a curl
// command, and a request an adapter one day builds with its credentials inside
// the body are all things a person would only discover by finding their key in
// a file they were about to attach to a bug report.
//
// So every string and every body written by this package passes through here
// first, and what it finds becomes the word "[redacted]" — which is a word
// somebody reading the record can search for, unlike an elision.
const redacted = "[redacted]"

var (
	// headerLike catches a credential written as a JSON field, whatever the
	// field is spelled: authorization, x-api-key, api-key, api_key. THE FIELD'S
	// NAME GOES WITH ITS VALUE — the record is greppable evidence a person
	// attaches to a bug report, and `grep -i authorization` over it has to come
	// back empty, which a redacted value under its own name does not manage.
	headerLike = regexp.MustCompile(`(?i)"(authorization|x-api-key|api[-_]key)"\s*:\s*"[^"]*"`)
	// bearerLike catches the scheme wherever it appears — a header echoed into
	// a message, a curl line in a tool result — with the token after it.
	bearerLike = regexp.MustCompile(`(?i)\bbearer\s+[A-Za-z0-9._\-]{8,}`)
	// keyLike catches the shape the keys this program uses are written in:
	// OpenRouter's `sk-or-v1-…` and every other `sk-…` any provider mints.
	keyLike = regexp.MustCompile(`\bsk-[A-Za-z0-9._\-]{8,}`)
)

// secrets are the exact values this process knows to be credentials — the
// configured key, handed over by the door that loaded it. They are matched
// literally, which is the only way to catch a key whose shape this file has
// never seen.
var secrets struct {
	mutex sync.Mutex
	list  []string
}

// Secret registers a value that must never appear in the record. A door calls
// it with the configured key; anything shorter than a credential is ignored, so
// an empty or placeholder key cannot turn every record into redactions.
func Secret(value string) {
	value = strings.TrimSpace(value)
	if len(value) < 8 {
		return
	}
	secrets.mutex.Lock()
	defer secrets.mutex.Unlock()
	for _, known := range secrets.list {
		if known == value {
			return
		}
	}
	secrets.list = append(secrets.list, value)
}

// Scrub returns the bytes with every credential it can recognize replaced. It
// returns the input unchanged when there is nothing to find, so the common case
// costs one pass and no allocation.
func Scrub(body []byte) []byte {
	if len(body) == 0 {
		return body
	}
	out := headerLike.ReplaceAll(body, []byte(`"credential":"`+redacted+`"`))
	out = bearerLike.ReplaceAll(out, []byte("Bearer "+redacted))
	out = keyLike.ReplaceAll(out, []byte(redacted))
	secrets.mutex.Lock()
	known := secrets.list
	secrets.mutex.Unlock()
	for _, secret := range known {
		out = []byte(strings.ReplaceAll(string(out), secret, redacted))
	}
	return out
}
