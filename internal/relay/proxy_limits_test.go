package relay

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestForwardedAddressesCannotResetUntrustedLimits(t *testing.T) {
	for _, socket := range []string{"192.0.2.1:1234", "[2001:db8::1]:1234"} {
		for _, door := range []struct {
			path  string
			limit int
		}{{EnginePath, RegistrationsPerMinute}, {DialPrefix + "otter-lamp-42", DialsPerMinute}} {
			t.Run(socket+door.path, func(t *testing.T) {
				server := &Server{Now: func() time.Time { return time.Unix(100000, 0) }}
				for i := 0; i <= door.limit; i++ {
					req := httptest.NewRequest(http.MethodGet, door.path, nil)
					req.RemoteAddr = socket
					req.Header.Set("Upgrade", Protocol)
					switch i % 5 {
					case 0:
					case 1:
						req.Header.Set("X-Forwarded-For", "")
					case 2:
						req.Header.Set("X-Forwarded-For", "  ")
					case 3:
						req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i))
					case 4:
						req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d, 203.0.113.1", i))
					}
					out := httptest.NewRecorder()
					server.ServeHTTP(out, req)
					if (i == door.limit) != (out.Code == http.StatusTooManyRequests) {
						t.Fatalf("request %d status=%d, header=%q", i+1, out.Code, req.Header.Get("X-Forwarded-For"))
					}
				}
			})
		}
	}
}
