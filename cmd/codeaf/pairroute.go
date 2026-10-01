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

// pairPollTimeout outlasts one held poll (pairbox.MaxWait) so the relay ends the
// wait and this client never cuts a healthy one short.
const pairPollTimeout = pairbox.MaxWait + 10*time.Second

// pairMailbox is the relay a pairing runs through: the --relay flag, else the
// relay this computer syncs through. A relay named by flag is the one the other
// device is told to pass, so it is `Shown`; the default needs no telling.
func pairMailbox(relayFlag string) (pair.Mailbox, error) {
	flag := strings.TrimSpace(relayFlag)
	base := flag
	if flag == "" {
		relay := syncsetup.Resolve(home.Dir())
		if relay.Off {
			return pair.Mailbox{}, pair.ErrSyncOff
		}
		base = relay.URL
	}
	if base == "" {
		return pair.Mailbox{}, pair.ErrNoSync
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
// device syncs through the same one. The hosted default is left out: the other
// device reaches it by default, and a saved copy would pin it to an address the
// hosted relay may one day leave.
// It also names the identities this one replaced by rotation, so a computer
// still on one of them may follow instead of being refused.
func pairGrant(route pair.Mailbox) (pair.Grant, error) {
	id, err := identity.Ensure(home.Dir())
	if err != nil {
		return pair.Grant{}, err
	}
	if err := identity.MarkPaired(home.Dir()); err != nil {
		return pair.Grant{}, err
	}
	return pair.Grant{Identity: id, SyncURL: syncsetup.Named(route.URL), Replaces: identity.Predecessors(home.Dir())}, nil
}

// pairJoining is this computer as the device that types the code.
func pairJoining(replace bool) pair.Joining { return pairJoiningAt(home.Dir())(replace) }

// pairJoiningAt is the same for the codeaf home at dir.
func pairJoiningAt(dir string) func(replace bool) pair.Joining {
	return func(replace bool) pair.Joining {
		return pair.Joining{
			Home:        dir,
			Label:       pair.ThisMachineLabel(),
			Replace:     replace,
			SaveSyncURL: func(url string) error { return syncsetup.SaveRelayURL(dir, url) },
		}
	}
}
