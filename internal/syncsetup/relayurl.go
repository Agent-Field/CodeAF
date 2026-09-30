package syncsetup

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
)

// HostedRelayURL is the central relay every computer syncs through when nobody
// has said otherwise. It is the one source of truth for that address: nothing
// else in the program spells it. Empty means there is no default relay, so sync
// stays off until a relay is named, exactly as it was before a hosted one
// existed. Making the hosted relay the default is this one line.
const HostedRelayURL = ""

// hostedRelay is what the resolver reads, so a test can stand in a hosted relay
// without the constant changing. Nothing but tests assigns it.
var hostedRelay = HostedRelayURL

// savedURLFile is where a computer keeps the relay another device told it to
// sync through when they were paired. It is a file beside the identity and not a
// settings row, because CODEAF_SYNC_URL is deliberately never a row: a persisted
// value would point a machine at a relay it was never told about, and this one
// was told, once, by the device the person had just approved.
const savedURLFile = "sync-url"

// offWord is the value of CODEAF_SYNC_URL that turns sync off.
const offWord = "off"

// Relay is the answer to "where does this computer sync?". It is one value so
// that sync, pairing and the first-run line cannot disagree about it.
type Relay struct {
	URL    string // the address, empty when there is none
	Off    bool   // the person turned sync off
	Hosted bool   // URL is the hosted default and nobody chose it
}

// On reports whether there is a relay to talk to.
func (r Relay) On() bool { return !r.Off && r.URL != "" }

// source is one place a relay can be named. It answers false when it has
// nothing to say, so the next one is asked.
type source func(home string) (Relay, bool)

// sources runs from the most deliberate word to the least: the person's
// variable, the relay a pairing saved, the hosted default. The last one always
// answers, so Resolve always has a value.
var sources = []source{fromVariable, fromSavedFile, fromHosted}

// Resolve is the relay this computer syncs through.
func Resolve(home string) Relay {
	for _, from := range sources {
		if relay, ok := from(home); ok {
			return relay
		}
	}
	return Relay{}
}

// Named is url when it is an address someone chose, and empty when it is the
// hosted default, which needs no naming to another computer.
func Named(url string) string {
	if url == hostedRelay {
		return ""
	}
	return url
}

func fromVariable(string) (Relay, bool) {
	set := env.Get(URLVar)
	if set == "" {
		return Relay{}, false
	}
	return Relay{URL: set, Off: strings.EqualFold(set, offWord)}, true
}

func fromSavedFile(home string) (Relay, bool) {
	raw, err := os.ReadFile(filepath.Join(home, savedURLFile))
	saved := strings.TrimSpace(string(raw))
	return Relay{URL: saved}, err == nil && saved != ""
}

func fromHosted(string) (Relay, bool) {
	return Relay{URL: hostedRelay, Hosted: hostedRelay != ""}, true
}

// SaveRelayURL remembers the relay a pairing named, for every later run that has
// no variable of its own.
func SaveRelayURL(home, url string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(home, savedURLFile), []byte(url+"\n"), 0o600)
}
