package relayserve_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/relayserve"
)

func TestTrustProxyAppliesToEveryRelayDoor(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEAF_TELEMETRY", "off")
	for _, store := range []bool{false, true} {
		for _, trust := range []bool{false, true} {
			for _, door := range []struct {
				name, path, method string
				limit              int
			}{
				{"registration", relay.EnginePath, http.MethodGet, relay.RegistrationsPerMinute},
				{"dial", relay.DialPrefix + "otter-lamp-42", http.MethodGet, relay.DialsPerMinute},
				{"mailbox", pairbox.Path, http.MethodPost, pairbox.DefaultLimits.CreatePerHour},
				{"link", directory.LinkPath + "/requests/AAAAAAAA", http.MethodGet, directory.DefaultLinkLimits.MissPerMinute},
			} {
				if !store && door.name == "link" {
					continue
				}
				t.Run(fmt.Sprintf("store=%v/trust=%v/%s", store, trust, door.name), func(t *testing.T) {
					path := ""
					if store {
						path = t.TempDir()
					}
					svc := relayserve.New(relayserve.Config{Store: path, TrustProxy: trust, Now: func() time.Time { return time.Unix(100000, 0) }})
					defer svc.Close()
					request := func(header string) int {
						req := httptest.NewRequest(door.method, door.path, nil)
						req.RemoteAddr = "192.0.2.1:1234"
						req.Header.Set("X-Forwarded-For", header)
						req.Header.Set("Upgrade", relay.Protocol)
						if door.name == "mailbox" {
							key, err := pairbox.NewKey()
							if err != nil {
								t.Fatal(err)
							}
							req.Header.Set(pairbox.KeyHeader, key.String())
						}
						out := httptest.NewRecorder()
						svc.Handler.ServeHTTP(out, req)
						return out.Code
					}
					// Changing only the second entry never changes the counted network.
					for i := 0; i < door.limit; i++ {
						if code := request(fmt.Sprintf(" 198.51.100.1 , 203.0.113.%d", i)); code == http.StatusTooManyRequests {
							t.Fatalf("early limit at %d", i)
						}
					}
					if code := request("198.51.100.1, 203.0.113.99"); code != http.StatusTooManyRequests {
						t.Fatalf("same first address: %d", code)
					}
					code := request("198.51.100.2, 198.51.100.1")
					if (code != http.StatusTooManyRequests) != trust {
						t.Fatalf("new forwarded address: trust=%v status=%d", trust, code)
					}
					if trust {
						// Absent and blank headers fall back to the same socket allowance.
						for i := 0; i < door.limit; i++ {
							header := ""
							if i%2 == 1 {
								header = " , 203.0.113.1"
							}
							if code := request(header); code == http.StatusTooManyRequests {
								t.Fatalf("early fallback limit at %d", i)
							}
						}
						if code := request(" "); code != http.StatusTooManyRequests {
							t.Fatalf("blank did not share fallback allowance: %d", code)
						}
					}
				})
			}
		}
	}
}
