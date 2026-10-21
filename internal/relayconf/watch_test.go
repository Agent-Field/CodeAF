//go:build relayurl

package relayconf

import (
	"flag"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// The local proxy needs its own runtime for the thousand-socket teardown so
// closing that fixture cannot delay another identity's five-second dial.
var watchCapURL = flag.String("relay-watch-cap-url", "", "base URL of an isolated relay for CapIsOneThousand")

// The watch cases against any live relay, which keeps its own clock and its
// default cap of a thousand watchers per identity.
func TestWatchConformance(t *testing.T) {
	base := baseURL(t)
	directorytest.RunWatch(t, func(t *testing.T) directorytest.WatchRig {
		service := base
		if *watchCapURL != "" && strings.HasSuffix(t.Name(), "/CapIsOneThousand") {
			service = *watchCapURL
		}
		acct := newAccount(t)
		return directorytest.WatchRig{
			Rig: directorytest.Rig{
				Clock: relayClock{t, service},
				Devices: func(name string) directory.Client {
					return directory.NewHTTP(service, acct.sign(name, wall), httpClient)
				},
				ID: acct.deviceID,
			},
			Base:     service,
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
