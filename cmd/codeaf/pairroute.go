package main

// The three things every pairing door needs before it can say a word: which
// relay to go through, the identity to hand over, and where to keep a relay the
// other device named. The chat's `/pair` and the terminal's `codeaf pair` both
// take them from here, so the two doors cannot disagree about where a code goes.

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/syncsetup"
)

// syncOffWord is the value of CODEAF_SYNC_URL that turns the relay off.
const syncOffWord = "off"

// pairPollTimeout outlasts one held poll (pairbox.MaxWait) so the relay ends the
// wait and this client never cuts a healthy one short.
const pairPollTimeout = pairbox.MaxWait + 10*time.Second

// pairMailbox is the relay a pairing runs through: the --relay flag, else the
// relay this computer syncs through. A relay named by flag is the one the other
// device is told to pass, so it is `Shown`; the default needs no telling.
func pairMailbox(relayFlag string) (pair.Mailbox, error) {
	flag := strings.TrimSpace(relayFlag)
	base := flag
	if base == "" {
		base = syncsetup.RelayURL(home.Dir())
	}
	if base == "" {
		return pair.Mailbox{}, pair.ErrNoSync
	}
	if strings.EqualFold(base, syncOffWord) && flag == "" {
		return pair.Mailbox{}, pair.ErrSyncOff
	}
	u, err := url.Parse(base)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return pair.Mailbox{}, pair.ErrNoSync
	}
	return pair.Mailbox{
		Box:   pairbox.NewHTTP(base, &http.Client{Timeout: pairPollTimeout}),
		URL:   base,
		Host:  u.Host,
		Shown: flag,
	}, nil
}

// pairGrant is what this computer hands to the device it pairs: the identity it
// has, made on first use, and the relay the pairing went through, so the other
// device syncs through the same one. There is no built-in default relay yet, so
// every relay is worth naming; the day one exists this is where it is left out.
func pairGrant(route pair.Mailbox) (pair.Grant, error) {
	id, err := identity.Ensure(home.Dir())
	if err != nil {
		return pair.Grant{}, err
	}
	return pair.Grant{Identity: id, SyncURL: route.URL}, nil
}

// pairJoining is this computer as the device that types the code.
func pairJoining(replace bool) pair.Joining {
	dir := home.Dir()
	return pair.Joining{
		Home:        dir,
		Label:       pair.ThisMachineLabel(),
		Replace:     replace,
		SaveSyncURL: func(url string) error { return syncsetup.SaveRelayURL(dir, url) },
	}
}
