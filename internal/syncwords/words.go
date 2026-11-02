// Package syncwords owns the sentences shared by sync's terminal and chat doors.
package syncwords

import (
	"fmt"
	"net/url"
	"strings"
)

// HostedURL is this build's default hosted sync service. Its address differs by
// release channel and internal/syncsetup/relayurl.go is the ONE place that spells
// it, so that package hands it over at startup through [UseHosted]. Empty means
// no default is known, and the sentences below then name no address.
var HostedURL string

// hostedHosts is every release channel's hosted service, so a computer synced
// through another channel's service still calls it hosted rather than its own.
var hostedHosts []string

// UseHosted is called once, by internal/syncsetup, before any sentence is built.
func UseHosted(url string, hosts []string) {
	HostedURL, hostedHosts = url, append([]string(nil), hosts...)
}

// Host leaves protocol paths out of sentences a person reads.
func Host(address string) string {
	if u, err := url.Parse(address); err == nil && u.Host != "" {
		return u.Host
	}
	return strings.TrimRight(address, "/")
}

// Hosted says whether a resolved address is one of the hosted services.
func Hosted(address string) bool {
	if address = strings.TrimRight(address, "/"); address == "" {
		return false
	}
	if address == strings.TrimRight(HostedURL, "/") {
		return true
	}
	u, err := url.Parse(address)
	if err != nil || u.Host == "" {
		return false
	}
	for _, host := range hostedHosts {
		if strings.EqualFold(u.Host, host) {
			return true
		}
	}
	return false
}

// ServiceLine is shared with the device screens so all lists name the same service.
func ServiceLine(address string) string {
	kind := "your own"
	if Hosted(address) {
		kind = "hosted"
	}
	return fmt.Sprintf("sync service: %s (%s)", Host(address), kind)
}

// Unreachable distinguishes a personal service from the hosted internet service.
func Unreachable(address string) string {
	if Hosted(address) {
		return "cannot reach " + Host(address) + " — check this computer's internet connection"
	}
	return "nothing is answering at " + address + " — if that is your own sync service, start it there with codeaf relay"
}

// Struggling is a service that answered but could not finish a coordinated change.
func Struggling(address string) string {
	return "the sync service at " + Host(address) + " is up but cannot agree on changes right now; it usually recovers within a minute"
}

// Recovered is emitted only after a successful sync-service call.
const Recovered = "syncing again"

// UnknownOutage omits a service host when that fact is unavailable.
// LegacyOutage remains the wire spelling for surfaces that predate this vocabulary.
const LegacyOutage = "this chat is not syncing because other machines are unreachable; it keeps working here"

const outagePrefix = "the sync service at "
const outageSuffix = " is not answering; this chat keeps working here and catches up when it is back"

const UnknownOutage = "the sync service is not answering; this chat keeps working here and catches up when it is back"

// Outage is said once while local work continues through a network outage.
func Outage(address string) string {
	return outagePrefix + Host(address) + outageSuffix
}

// Asking is the delayed progress sentence for terminal commands.
func Asking(address string) string { return "asking the sync service at " + Host(address) + "…" }

// IsOutage recognizes both generations so mixed builds share one notice ledger.
func IsOutage(text string) bool {
	return text == UnknownOutage || text == LegacyOutage || strings.HasPrefix(text, outagePrefix) && strings.HasSuffix(text, outageSuffix)
}
