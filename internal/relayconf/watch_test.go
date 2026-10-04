//go:build relayurl

package relayconf

import (
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// The watch cases against any live relay, which keeps its own clock and its
// default cap of a thousand watchers per identity.
func TestWatchConformance(t *testing.T) {
	base := baseURL(t)
	directorytest.RunWatch(t, func(t *testing.T) directorytest.WatchRig {
		acct := newAccount(t)
		return directorytest.WatchRig{
			Rig: directorytest.Rig{
				Clock: relayClock{t, base},
				Devices: func(name string) directory.Client {
					return directory.NewHTTP(base, acct.sign(name, wall), httpClient)
				},
				ID: acct.deviceID,
			},
			Base:     base,
			Sign:     func(name string) wireauth.Sign { return acct.sign(name, wall) },
			Stranger: func() wireauth.Sign { return newAccount(t).sign("a", wall) },
		}
	})
}

// The lease cases against any live relay. They wait on the relay's own clock,
// so each takes about as long as two lease TTLs.
func TestLeaseConformance(t *testing.T) {
	base := baseURL(t)
	directorytest.RunLease(t, func(t *testing.T) directorytest.WatchRig {
		acct := newAccount(t)
		return directorytest.WatchRig{
			Rig: directorytest.Rig{
				Clock: relayClock{t, base},
				Devices: func(name string) directory.Client {
					return directory.NewHTTP(base, acct.sign(name, wall), httpClient)
				},
				ID: acct.deviceID,
			},
			Base:     base,
			Sign:     func(name string) wireauth.Sign { return acct.sign(name, wall) },
			Stranger: func() wireauth.Sign { return newAccount(t).sign("a", wall) },
		}
	})
}

// The presence cases against any live relay. They declare a beat of two
// seconds, so each waits the five-second window on the relay's own clock.
func TestPresenceConformance(t *testing.T) {
	base := baseURL(t)
	directorytest.RunPresence(t, func(t *testing.T) directorytest.WatchRig {
		acct := newAccount(t)
		return directorytest.WatchRig{
			Rig: directorytest.Rig{
				Clock: relayClock{t, base},
				Devices: func(name string) directory.Client {
					return directory.NewHTTP(base, acct.sign(name, wall), httpClient)
				},
				ID: acct.deviceID,
			},
			Base:     base,
			Sign:     func(name string) wireauth.Sign { return acct.sign(name, wall) },
			Stranger: func() wireauth.Sign { return newAccount(t).sign("a", wall) },
		}
	})
}
