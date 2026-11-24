package serve

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func boolPtr(value bool) *bool { return &value }

func TestContractC2AndC3LoopbackTrust(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mode   string
		trust  *bool
		status int
	}{
		{"derived team", "", nil, http.StatusUnauthorized},
		{"team", api.FactoryTeam, nil, http.StatusUnauthorized},
		{"team opt-in", api.FactoryTeam, boolPtr(true), http.StatusOK},
		{"solo", api.FactorySolo, nil, http.StatusOK},
		{"solo opt-out", api.FactorySolo, boolPtr(false), http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const token = "host-secret"
			h := New(factory.Seam{}, Config{AccessToken: token, Mode: tc.mode, LoopbackTrust: tc.trust})
			t.Cleanup(h.Close)
			request := func(path, credential string, cookie *http.Cookie, status int) *httptest.ResponseRecorder {
				t.Helper()
				r := httptest.NewRequest(http.MethodGet, path, nil)
				r.RemoteAddr = "127.0.0.1:1234"
				if credential != "" {
					r.Header.Set("Authorization", "Bearer "+credential)
				}
				if cookie != nil {
					r.AddCookie(cookie)
				}
				w := httptest.NewRecorder()
				h.ServeHTTP(w, r)
				if w.Code != status {
					t.Fatalf("%s: got %d, want %d. %s", path, w.Code, status, w.Body)
				}
				return w
			}
			request("/v1/about", "", nil, http.StatusOK)
			request("/access", "", nil, http.StatusOK)
			request("/v1/me", "", nil, tc.status)
			request("/v1/me", token, nil, http.StatusOK)
			signIn := request("/access?token="+token, "", nil, http.StatusSeeOther)
			request("/v1/me", "", signIn.Result().Cookies()[0], http.StatusOK)
		})
	}
}

func TestContractC4ForwardedLoopbackTrust(t *testing.T) {
	const ipv4 = "127.0.0.1:1234"
	for _, tc := range []struct {
		name, peer, header, value string
		trusted                   bool
		status                    int
	}{
		{"unlisted proxy", ipv4, "X-Forwarded-For", "203.0.113.9", false, 401},
		{"listed proxy remote", ipv4, "X-Forwarded-For", "203.0.113.9", true, 401},
		{"Forwarded", ipv4, "Forwarded", "for=203.0.113.9", true, 401},
		{"X-Real-Ip", ipv4, "X-Real-Ip", "203.0.113.9", true, 401},
		{"X-Forwarded-Proto", ipv4, "X-Forwarded-Proto", "https", true, 401},
		{"garbage", ipv4, "X-Forwarded-For", "garbage", true, 401},
		{"empty header", ipv4, "X-Forwarded-For", "", true, 401},
		{"IPv6 peer", "[::1]:1234", "X-Forwarded-For", "203.0.113.9", false, 401},
		{"mapped peer", "[::ffff:127.0.0.1]:1234", "X-Forwarded-For", "203.0.113.9", false, 401},
		{"listed proxy local", ipv4, "X-Forwarded-For", "127.0.0.1", true, 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Mode: api.FactoryTeam, LoopbackTrust: boolPtr(true)}
			if tc.trusted {
				cfg.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
			}
			h := New(factory.Seam{}, cfg)
			t.Cleanup(h.Close)
			r := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
			r.RemoteAddr = tc.peer
			r.Header.Set(tc.header, tc.value)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != tc.status {
				t.Fatalf("got %d, want %d. %s", w.Code, tc.status, w.Body)
			}
		})
	}
}
