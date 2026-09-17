// Package poolcfg resolves how the measurement pool behaves from two stored
// settings — the mode word and the trusted key — and an environment handed in
// as a function. Nothing here touches the disk, the network, the clock or the
// process environment, so Resolve is a table of inputs and outputs and every
// case is reachable from a test.
package poolcfg

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Addresses held when the environment offers nothing readable. The relay is
// the one base the resolver holds: the index and the submit addresses are
// read from beside it, and each keeps its own override.
const (
	DefaultRelayURL  = "https://codeaf.agentfield.ai/pool"
	DefaultIndexURL  = DefaultRelayURL + indexPath
	DefaultSubmitURL = DefaultRelayURL + submitPath
)

// The two addresses a relay serves, under its base.
const (
	indexPath  = "/index.json"
	submitPath = "/v1/rows"
)

// Mode says what the pool may do: send and read, only read, or neither.
type Mode int

const (
	On Mode = iota
	Read
	Off
)

// String spells the mode the way the setting and the environment spell it.
func (m Mode) String() string {
	switch m {
	case On:
		return "on"
	case Read:
		return "read"
	case Off:
		return "off"
	default:
		return fmt.Sprintf("Mode(%d)", int(m))
	}
}

// Sources records where each resolved value came from. Every field holds one
// of four words: "default", "setting", "env" or "ci".
type Sources struct {
	Mode      string
	RelayURL  string
	IndexURL  string
	SubmitURL string
	PublicKey string
	TTL       string
}

// Config is the resolved pool configuration. Source names the origin of each
// field, and every field is resolved on its own: the mode does not change how
// the addresses or the TTL are read, and an unreadable URL does not disturb
// the TTL.
//
// The relay is the base the index and submit addresses derive from: each is
// RelayURL with its own path appended, unless its own environment name
// overrides it. An empty PublicKey is the ordinary answer — it means the key
// the binary carries.
type Config struct {
	Mode      Mode
	RelayURL  string
	IndexURL  string
	SubmitURL string
	PublicKey string
	TTL       time.Duration
	Source    Sources
}

// The seven names Resolve reads, and no others.
const (
	envMode   = "CODEAF_MODEL_POOL"
	envCI     = "CI"
	envRelay  = "CODEAF_MODEL_POOL_RELAY_URL"
	envIndex  = "CODEAF_MODEL_POOL_URL"
	envSubmit = "CODEAF_MODEL_POOL_SUBMIT_URL"
	envTTL    = "CODEAF_MODEL_POOL_TTL"
	envKey    = "CODEAF_MODEL_POOL_PUBLIC_KEY"
)

// Where a resolved value came from.
const (
	srcDefault = "default"
	srcSetting = "setting"
	srcEnv     = "env"
	srcCI      = "ci"
)

// TTL bounds, and the value held when no readable TTL is set.
const (
	ttlFloor   = time.Minute
	ttlCeiling = 7 * 24 * time.Hour
	defaultTTL = 24 * time.Hour
)

// Resolve resolves the pool configuration from the stored mode word, the
// stored public key and the environment behind lookup, which answers with a
// value and whether the name was set at all — a name that is set and empty is
// not the same as a name nobody set. A nil lookup is an environment in which
// nothing is set.
//
// Every value is read with the space around it trimmed, and words are matched
// without regard to case. Resolve is pure: the same inputs give the same
// Config, it keeps nothing between calls, and many goroutines may call it at
// once.
func Resolve(setting, publicKey string, lookup func(name string) (value string, set bool)) Config {
	// One injection for the whole environment: the seven names are read once,
	// here, in this order, and each value is trimmed as it is read.
	get := func(name string) (string, bool) {
		if lookup == nil {
			return "", false
		}
		value, set := lookup(name)
		if !set {
			return "", false
		}
		return strings.TrimSpace(value), true
	}
	modeWord, _ := get(envMode)
	ciWord, _ := get(envCI)
	relayWord, _ := get(envRelay)
	indexWord, _ := get(envIndex)
	submitWord, submitSet := get(envSubmit)
	ttlWord, _ := get(envTTL)
	keyWord, _ := get(envKey)

	// The mode is the first of these that answers, and that answer is its
	// source: the environment, the stored setting, CI, then the default. A
	// word neither the environment nor the setting knows is simply not an
	// answer and the next line gets its turn.
	mode, modeSrc := On, srcDefault
	if m, ok := parseMode(modeWord); ok {
		mode, modeSrc = m, srcEnv
	} else if m, ok := parseMode(strings.TrimSpace(setting)); ok {
		mode, modeSrc = m, srcSetting
	} else if ciSaysYes(ciWord) {
		mode, modeSrc = Read, srcCI
	}

	// The relay is the base the other two addresses derive from, and its
	// source is theirs: a relay read from the environment hands down both of
	// its derived addresses, each of which an override of its own still wins.
	// A base is held with no trailing slash, whatever the name carried.
	relayURL, relaySrc := DefaultRelayURL, srcDefault
	if u, ok := acceptURL(relayWord); ok {
		relayURL, relaySrc = strings.TrimRight(u, "/"), srcEnv
	}

	indexURL, indexSrc := relayURL+indexPath, relaySrc
	if u, ok := acceptURL(indexWord); ok {
		indexURL, indexSrc = u, srcEnv
	}

	// An empty submit address means send nowhere, and that is an answer; a
	// name nobody set, or one holding neither an address nor emptiness, is
	// the address derived from the relay.
	submitURL, submitSrc := relayURL+submitPath, relaySrc
	if submitSet {
		if submitWord == "" {
			submitURL, submitSrc = "", srcEnv
		} else if u, ok := acceptURL(submitWord); ok {
			submitURL, submitSrc = u, srcEnv
		}
	}

	// A readable TTL is held between the floor and the ceiling; a name nobody
	// set, or one no duration can be read from, is the default.
	ttl, ttlSrc := defaultTTL, srcDefault
	if d, err := time.ParseDuration(ttlWord); err == nil {
		ttl, ttlSrc = clampTTL(d), srcEnv
	}

	// The trusted key is the first of these that answers: the environment,
	// then the stored row, then none. An empty key is the ordinary answer —
	// it means the key the binary carries.
	storedKey := strings.TrimSpace(publicKey)
	publicKey, keySrc := "", srcDefault
	if keyWord != "" {
		publicKey, keySrc = keyWord, srcEnv
	} else if storedKey != "" {
		publicKey, keySrc = storedKey, srcSetting
	}

	return Config{
		Mode:      mode,
		RelayURL:  relayURL,
		IndexURL:  indexURL,
		SubmitURL: submitURL,
		PublicKey: publicKey,
		TTL:       ttl,
		Source: Sources{
			Mode:      modeSrc,
			RelayURL:  relaySrc,
			IndexURL:  indexSrc,
			SubmitURL: submitSrc,
			PublicKey: keySrc,
			TTL:       ttlSrc,
		},
	}
}

// CanSend reports whether the pool may send measurements: only when the mode
// is on and there is a submit address to send to.
func (c Config) CanSend() bool {
	return c.Mode == On && c.SubmitURL != ""
}

// CanRead reports whether the pool may read measurements: whenever the mode
// is not off.
func (c Config) CanRead() bool {
	return c.Mode != Off
}

// parseMode reads one of the three mode words, without regard to case.
// Anything else is not an answer.
func parseMode(word string) (Mode, bool) {
	switch strings.ToLower(word) {
	case "on":
		return On, true
	case "read":
		return Read, true
	case "off":
		return Off, true
	}
	return On, false
}

// ciSaysYes reads the CI word: one of true, 1 or yes, without regard to case.
func ciSaysYes(word string) bool {
	switch strings.ToLower(word) {
	case "true", "1", "yes":
		return true
	}
	return false
}

// acceptURL reads one of the four shapes a pool address may take, and nothing
// else: an "https://" URL, an "http://" URL whose host is localhost or
// 127.0.0.1 with or without a port, a "file://" URL, or an absolute filesystem
// path, which is one beginning "/". Plain http anywhere else is not accepted.
func acceptURL(value string) (string, bool) {
	if strings.HasPrefix(value, "/") {
		return value, true
	}
	lower := strings.ToLower(value)
	switch {
	case strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "file://"):
		if _, err := url.Parse(value); err != nil {
			return "", false
		}
		return value, true
	case strings.HasPrefix(lower, "http://"):
		u, err := url.Parse(value)
		if err != nil {
			return "", false
		}
		host := u.Hostname()
		return value, strings.EqualFold(host, "localhost") || host == "127.0.0.1"
	}
	return "", false
}

// clampTTL holds d between the TTL floor and ceiling.
func clampTTL(d time.Duration) time.Duration {
	if d < ttlFloor {
		return ttlFloor
	}
	if d > ttlCeiling {
		return ttlCeiling
	}
	return d
}
