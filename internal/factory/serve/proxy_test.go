package serve

import (
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// proxyRequest is one request from peer carrying headers.
func proxyRequest(method, target, peer string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(url.Values{"token": {"test-secret"}}.Encode()))
	r.RemoteAddr = peer
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

func TestForwardedHeadersAreBelievedOnlyFromTrustedProxies(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("127.0.0.1/32")}
	for _, tc := range []struct {
		name, peer, xff, proto string
		addr                   string
		secure, local          bool
	}{
		{"direct caller", "192.0.2.7:1", "", "", "192.0.2.7", false, false},
		{"direct loopback", "127.0.0.1:1", "", "", "127.0.0.1", false, true},
		{"untrusted peer lies", "192.0.2.7:1", "127.0.0.1", "https", "192.0.2.7", false, false},
		{"trusted proxy", "10.1.2.3:1", "198.51.100.4", "https", "198.51.100.4", true, false},
		{"caller prepends a lie", "10.1.2.3:1", "127.0.0.1, 198.51.100.4", "https", "198.51.100.4", true, false},
		{"two trusted hops", "10.1.2.3:1", "198.51.100.4, 10.9.9.9", "http", "198.51.100.4", false, false},
		{"trusted loopback proxy for a remote caller", "127.0.0.1:1", "203.0.113.9", "https", "203.0.113.9", true, false},
		{"trusted proxy for a caller on this machine", "127.0.0.1:1", "127.0.0.1", "", "127.0.0.1", false, true},
		{"garbage hop keeps the peer", "10.1.2.3:1", "not-an-ip", "https", "10.1.2.3", true, false},
		{"trusted local proxy naming nobody", "127.0.0.1:1", "", "https", "127.0.0.1", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			headers := map[string]string{}
			if tc.xff != "" {
				headers["X-Forwarded-For"] = tc.xff
			}
			if tc.proto != "" {
				headers["X-Forwarded-Proto"] = tc.proto
			}
			c := resolveClient(proxyRequest("GET", "/", tc.peer, headers), trusted)
			if c.addr.String() != tc.addr || c.secure != tc.secure || c.onThisMachine() != tc.local {
				t.Fatalf("client = %v secure %v local %v, want %s secure %v local %v",
					c.addr, c.secure, c.onThisMachine(), tc.addr, tc.secure, tc.local)
			}
		})
	}
}

// A PROXY ON THIS MACHINE THAT NOBODY LISTED MUST NOT OPEN THE FLOOR. Its
// requests arrive from loopback, which alone would be the open local grant.
func TestAnUnlistedLocalProxyDoesNotOpenTheLoopbackFloor(t *testing.T) {
	h := New(factory.FixtureSeam(time.Now()), Config{AccessToken: "test-secret"})
	defer h.Close()
	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    int
	}{
		{"local process", nil, http.StatusOK},
		{"forwarded through an unlisted proxy", map[string]string{"X-Forwarded-For": "203.0.113.9"}, http.StatusUnauthorized},
		{"Forwarded header", map[string]string{"Forwarded": "for=203.0.113.9"}, http.StatusUnauthorized},
	} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, proxyRequest("GET", "/v1/queue", "127.0.0.1:5555", tc.headers))
		if w.Code != tc.want {
			t.Fatalf("%s: /v1/queue = %d, want %d", tc.name, w.Code, tc.want)
		}
	}
}

func TestSessionCookieAndHSTSFollowTheCallersScheme(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.5/32")}
	for _, tc := range []struct {
		name    string
		peer    string
		headers map[string]string
		tls     bool
		secure  bool
	}{
		{"plain", "192.0.2.1:1", nil, false, false},
		{"tls here", "192.0.2.1:1", nil, true, true},
		{"trusted proxy https", "10.0.0.5:1", map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-For": "192.0.2.1"}, false, true},
		{"untrusted proxy claims https", "10.0.0.6:1", map[string]string{"X-Forwarded-Proto": "https", "X-Forwarded-For": "192.0.2.1"}, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := New(factory.FixtureSeam(time.Now()), Config{AccessToken: "test-secret", TrustedProxies: trusted})
			defer h.Close()
			r := proxyRequest("POST", "/access", tc.peer, tc.headers)
			if tc.tls {
				r.TLS = &tls.ConnectionState{}
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			cookies := w.Result().Cookies()
			if w.Code != http.StatusSeeOther || len(cookies) != 1 {
				t.Fatalf("login = %d, cookies %v", w.Code, cookies)
			}
			c := cookies[0]
			if c.Secure != tc.secure || !c.HttpOnly || c.SameSite != http.SameSiteStrictMode {
				t.Fatalf("cookie Secure %v HttpOnly %v SameSite %v, want Secure %v, HttpOnly, Strict", c.Secure, c.HttpOnly, c.SameSite, tc.secure)
			}
			if got := w.Header().Get("Strict-Transport-Security"); (got != "") != tc.secure {
				t.Fatalf("HSTS = %q with secure %v", got, tc.secure)
			}
			// The redirect after sign-in stays relative, so a proxy's public
			// name is kept rather than replaced with this listener's.
			if w.Header().Get("Location") != "/" {
				t.Fatalf("redirect = %q", w.Header().Get("Location"))
			}
		})
	}
}

func TestAboutReportsTheSchemeTheCallerUsed(t *testing.T) {
	h := New(factory.FixtureSeam(time.Now()), Config{AccessToken: "test-secret", Address: "https://factory.example.com",
		TrustedProxies: []netip.Prefix{netip.MustParsePrefix("10.0.0.5/32")}})
	defer h.Close()
	for _, tc := range []struct {
		name    string
		peer    string
		headers map[string]string
		tls     bool
		want    string
	}{
		{"plain", "192.0.2.1:1", nil, false, "http"},
		{"tls", "192.0.2.1:1", nil, true, "https"},
		{"proxy", "10.0.0.5:1", map[string]string{"X-Forwarded-Proto": "https"}, false, "https"},
	} {
		r := proxyRequest("GET", "/v1/about", tc.peer, tc.headers)
		if tc.tls {
			r.TLS = &tls.ConnectionState{}
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var about struct {
			Scheme  string `json:"scheme"`
			Factory struct {
				Address string `json:"address"`
			} `json:"factory"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &about); err != nil || about.Scheme != tc.want || about.Factory.Address != "https://factory.example.com" {
			t.Fatalf("%s: about = %s (%v)", tc.name, w.Body.String(), err)
		}
	}
}

func TestEventStreamAsksProxiesNotToBuffer(t *testing.T) {
	h := New(factory.FixtureSeam(time.Now()), Config{AccessToken: "test-secret"})
	defer h.Close()
	server := httptest.NewTLSServer(h)
	defer server.Close()
	req, _ := http.NewRequest("GET", server.URL+"/v1/events", nil)
	req.Header.Set("Authorization", "Bearer test-secret")
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != "text/event-stream" ||
		resp.Header.Get("X-Accel-Buffering") != "no" || resp.Header.Get("Strict-Transport-Security") == "" {
		t.Fatalf("events over TLS = %d %v", resp.StatusCode, resp.Header)
	}
}
