package syncsetup

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/pair"
)

const (
	// ProductionRelayURL is the central sync service (the fabric) that stable
	// releases and release candidates sync through when nobody has said
	// otherwise. The address has a path, which every client keeps (see
	// wireauth.Endpoint).
	ProductionRelayURL = "https://codeaf.agentfield.ai/fabric"

	// StagingRelayURL is the central sync service that staging builds sync
	// through when nobody has said otherwise.
	StagingRelayURL = "https://caf-fabric-staging.instrument-santosh.workers.dev"

	// DevRelayURL is the central sync service that dev builds, local builds from
	// a checkout, and test binaries sync through when nobody has said otherwise,
	// so a build nobody tagged can never write to production.
	DevRelayURL = "https://caf-fabric-dev.instrument-santosh.workers.dev"
)

// HostedRelayFor names the default sync service for a binary stamped with
// revision. A stable or release-candidate tag gets production; a staging tag
// gets staging; every other revision gets dev. The three addresses are spelled
// in this file and nowhere else in the program.
func HostedRelayFor(revision string) string {
	switch buildinfo.Channel(revision) {
	case "stable", "rc":
		return ProductionRelayURL
	case "staging":
		return StagingRelayURL
	default:
		return DevRelayURL
	}
}

// LinkOriginOf is the scheme and host where a hosted sync service serves its
// short pairing pages (`/p/<code>`), because every hosted Worker serves `/p/`
// at the root of its origin before stripping any base path.
func LinkOriginOf(relayURL string) string {
	u, err := url.Parse(strings.TrimSpace(relayURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// HostedLinkOriginFor names the pairing-link origin for a binary stamped with
// revision.
func HostedLinkOriginFor(revision string) string {
	return LinkOriginOf(HostedRelayFor(revision))
}

// HostedLinkHosts lists the hostnames of every channel's hosted sync service,
// so a pairing surface can recognise a short link from any codeaf channel
// without spelling the addresses outside this file.
func HostedLinkHosts() []string {
	hosts := make([]string, 0, 3)
	for _, raw := range []string{ProductionRelayURL, StagingRelayURL, DevRelayURL} {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			hosts = append(hosts, u.Host)
		}
	}
	return hosts
}

// HostedRelayURL is the central sync service this binary syncs through when
// nobody has said otherwise, decided once from the binary's release stamp.
// Empty means there is no default, so sync stays off until an address is named.
var HostedRelayURL = HostedRelayFor(buildinfo.Revision())

// hostedRelay is what the resolver reads, so a test can stand in a hosted relay
// without HostedRelayURL changing. Nothing but tests assigns it.
var hostedRelay = HostedRelayURL

func init() {
	pair.LinkOrigin = LinkOriginOf(HostedRelayURL)
}

// SavedURLFile is where a computer keeps the relay another device told it to
// sync through when they were paired. It is a file beside the identity and not a
// settings row, because CODEAF_SYNC_URL is deliberately never a row: a persisted
// value would point a machine at a relay it was never told about, and this one
// was told, once, by the device the person had just approved. It is exported
// because a dev box's boot script writes it before its first join (the laptop
// that created the box told it, through Terraform), and the devbox package
// holds that script to this one spelling.
const SavedURLFile = "sync-url"

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

// ResolveUnnamed is the relay this computer syncs through when the process
// asking names none of its own: the address a pairing saved, else the hosted
// default. CODEAF_SYNC_URL is a word said in ONE shell, and a process started
// somewhere that never said it — `codeaf engine` under ssh for --host, most of
// all — has not chosen a different relay by staying quiet. The session host
// compares such a process against this answer and never against the variable
// the terminal that started it happened to carry (internal/enginehost).
func ResolveUnnamed(home string) Relay {
	for _, from := range sources[1:] {
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
	raw, err := os.ReadFile(filepath.Join(home, SavedURLFile))
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
	return os.WriteFile(filepath.Join(home, SavedURLFile), []byte(url+"\n"), 0o600)
}
