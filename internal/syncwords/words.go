// Package syncwords owns the sentences shared by sync's terminal and chat doors.
package syncwords

import (
	"errors"
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
	return fmt.Sprintf("relay: %s (%s)", Host(address), kind)
}

// Unreachable distinguishes a personal service from the hosted internet service.
func Unreachable(address string) string {
	if Hosted(address) {
		return "cannot reach " + Host(address) + " — check this computer's internet connection"
	}
	return "nothing is answering at " + address + " — if that is your own relay, start it there with codeaf relay"
}

// Struggling is a service that answered but could not finish a coordinated change.
func Struggling(address string) string {
	return "the relay at " + Host(address) + " is up but cannot agree on changes right now; it usually recovers within a minute"
}

// Recovered is emitted only after a successful sync-service call.
const Recovered = "syncing again"

// UnknownOutage omits a service host when that fact is unavailable.
// LegacyOutage remains the wire spelling for surfaces that predate this vocabulary.
const LegacyOutage = "this chat is not syncing because other machines are unreachable; it keeps working here"

const outagePrefix = "the relay at "
const outageSuffix = " is not answering; this chat keeps working here and catches up when it is back"

const UnknownOutage = "the relay is not answering; this chat keeps working here and catches up when it is back"

// Outage is said once while local work continues through a network outage.
func Outage(address string) string {
	return outagePrefix + Host(address) + outageSuffix
}

// Asking is the delayed progress sentence for terminal commands.
func Asking(address string) string { return "asking the relay at " + Host(address) + "…" }

// IsOutage recognizes both generations so mixed builds share one notice ledger.
func IsOutage(text string) bool {
	return text == UnknownOutage || text == LegacyOutage || strings.HasPrefix(text, outagePrefix) && strings.HasSuffix(text, outageSuffix)
}

// SavedHerePending names the same pending upload at app quit and individual close.
const SavedHerePending = "saved here; it reaches your other devices when the relay at %s answers"

// SavedStorageOff is shared by app quit and the close of an individual chat.
const SavedStorageOff = "saved here; relay storage is off for this account, so it stays on this computer"

// ErrContinueStorageOff preserves local turns when the recorded history is missing here.
var ErrContinueStorageOff = errors.New("cannot move this chat here while relay storage is off because this computer is missing saved turns; your local turns stay here")

// ErrHolderOffline is a take that cannot finish because the computer that has
// the chat is not running codeaf, so nothing can send its bytes. The chat is
// safe on that computer; the person is told whose it is and what to do.
var ErrHolderOffline = errors.New("the computer that has this chat is not on right now")

// HolderOfflineError carries the name of the computer that has the chat.
// errors.Is(err, ErrHolderOffline) holds for it, and errors.As recovers the name.
type HolderOfflineError struct {
	// Name is the person-facing name of the computer, "another computer" when
	// the directory could not say.
	Name string
}

func (e *HolderOfflineError) Error() string { return HolderOffline(e.Name) }

// Is makes the typed error match [ErrHolderOffline] without wrapping anything
// that a fetch would classify as a transient failure and retry.
func (e *HolderOfflineError) Is(target error) bool { return target == ErrHolderOffline }

// HolderOffline is the sentence for a chat whose holder is not running codeaf.
// It is the one place the words live, so Home, the app and the manual agree.
func HolderOffline(name string) string {
	if name == "" {
		name = "another computer"
	}
	return name + " has this chat and is not on right now; open codeaf there, then move here"
}

// ErrLiveTransferOff is what continuing a chat says when the chat exists only on
// the computer that shared it by blind pass-through and this computer has that
// mode switched off: the record is in the list, but the only road to its frames
// is shut here.
var ErrLiveTransferOff = errors.New("this chat is only on the computer that shares it, and blind pass-through is switched off here; switch it back on (CODEAF_PASSTHROUGH) or turn on relay storage to move it here")

// SecretFilesLate is said when a chat has moved here but the files kept out of
// its saved history (.env and the like) did not come with it, because the
// computer that has them stopped answering. They are still on that computer.
func SecretFilesLate(name string) string {
	if name == "" {
		name = "the other computer"
	}
	return "its secret files (.env and the like) did not come with it, because " + name + " stopped answering; they are still there"
}

// ErrNotesWaitForStorage is what sending a message to a chat another computer
// holds says while sync storage is off for the account. Such a message would
// have to sit on the sync service until that computer took it, and the service
// keeps nothing of the kind until storage is approved, so the message is not
// sent: the chat has to be brought here to be written to.
var ErrNotesWaitForStorage = errors.New("a message to a chat on another computer waits for relay storage, which is off for this account; move the chat here to write to it")
