package appapi

import (
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func request(t *testing.T, handler http.Handler, method, path, body string, headers http.Header, host string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = host
	if headers != nil {
		r.Header = headers
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	return w
}

func bearer() http.Header { return http.Header{"Authorization": {"Bearer " + testToken}} }

func TestEveryRouteHasTheSameSecurityBoundary(t *testing.T) {
	for _, route := range routes {
		t.Run(route.path, func(t *testing.T) {
			for _, test := range []struct {
				name   string
				host   string
				origin []string
				auth   []string
				status int
			}{
				{"no token", "127.0.0.1:4000", nil, nil, 401},
				{"wrong token", "127.0.0.1:4000", nil, []string{"Bearer wrong"}, 401},
				{"bare token", "127.0.0.1:4000", nil, []string{testToken}, 401},
				{"lowercase scheme", "127.0.0.1:4000", nil, []string{"bearer " + testToken}, 401},
				{"two credentials", "127.0.0.1:4000", nil, []string{"Bearer " + testToken, "Bearer " + testToken}, 401},
				{"foreign origin", "127.0.0.1:4000", []string{"https://evil.example"}, []string{"Bearer " + testToken}, 403},
				{"origin before token", "127.0.0.1:4000", []string{"https://evil.example"}, nil, 403},
				{"null origin", "127.0.0.1:4000", []string{"null"}, nil, 403},
				{"empty origin", "127.0.0.1:4000", []string{""}, nil, 403},
				{"two origins", "127.0.0.1:4000", []string{"tauri://localhost", "tauri://localhost"}, nil, 403},
				{"origin lookalike", "127.0.0.1:4000", []string{"tauri://localhost.evil"}, nil, 403},
				{"wrong dev port", "127.0.0.1:4000", []string{"http://localhost:1421"}, nil, 403},
				{"origin slash", "127.0.0.1:4000", []string{"http://localhost:1420/"}, nil, 403},
				{"foreign host", "evil.example:4000", nil, []string{"Bearer " + testToken}, 421},
				{"host before token", "evil.example:4000", nil, nil, 421},
				{"private host", "10.0.0.5:4000", nil, []string{"Bearer " + testToken}, 421},
				{"host lookalike", "localhost.evil.example:4000", nil, nil, 421},
				{"missing port", "127.0.0.1", nil, nil, 421},
				{"invalid port", "127.0.0.1:x", nil, nil, 421},
				{"zero port", "127.0.0.1:0", nil, nil, 421},
				{"large port", "127.0.0.1:65536", nil, nil, 421},
				{"empty host", "", nil, nil, 421},
			} {
				t.Run(test.name, func(t *testing.T) {
					called := false
					s := New(testToken, Options{Shutdown: func() { called = true }})
					headers := http.Header{}
					if test.origin != nil {
						headers["Origin"] = test.origin
					}
					if test.auth != nil {
						headers["Authorization"] = test.auth
					}
					w := request(t, s, route.method, route.path, `{}`, headers, test.host)
					if w.Code != test.status || called {
						t.Fatalf("got %d, shutdown=%v: %s", w.Code, called, w.Body)
					}
					var failure map[string]string
					if err := json.Unmarshal(w.Body.Bytes(), &failure); err != nil || failure["error"] == "" || failure["request_id"] == "" {
						t.Fatalf("missing JSON error envelope: %s (%v)", w.Body, err)
					}
					if test.status != 401 && w.Header().Get("Access-Control-Allow-Origin") != "" {
						t.Fatal("refused origin or host received CORS access")
					}
				})
			}
		})
	}
}

func TestNativeAndDevOriginsAllowAuthenticatedRequestsAndPreflight(t *testing.T) {
	for _, origin := range []string{"", "tauri://localhost", "http://tauri.localhost", "http://localhost:1420", "http://127.0.0.1:1420"} {
		for _, host := range []string{"127.0.0.1:4000", "127.0.0.2:4000", "[::1]:4000", "[::ffff:127.0.0.1]:4000", "localhost:4000"} {
			headers := bearer()
			if origin != "" {
				headers.Set("Origin", origin)
			}
			w := request(t, New(testToken, Options{}), "GET", "/app/v1/health", "", headers, host)
			if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Access-Control-Allow-Origin") != origin {
				t.Fatalf("origin=%q host=%q: %d %s", origin, host, w.Code, w.Body)
			}
		}
		if origin == "" {
			continue
		}
		headers := http.Header{"Origin": {origin}, "Access-Control-Request-Method": {"POST"}}
		w := request(t, New(testToken, Options{Shutdown: func() { t.Fatal("preflight called shutdown") }}), "OPTIONS", "/app/v1/shutdown", "", headers, "127.0.0.1:4000")
		if w.Code != 204 || w.Body.Len() != 0 || !strings.Contains(w.Header().Get("Access-Control-Allow-Headers"), "Authorization") {
			t.Fatalf("preflight from %q: %d %s", origin, w.Code, w.Body)
		}
	}
	for _, host := range []string{"127.0.0.1:4000", "evil.example:4000"} {
		w := request(t, New(testToken, Options{}), "OPTIONS", "/app/v1/shutdown", "", http.Header{"Origin": {"https://evil.example"}}, host)
		if w.Code != 403 || w.Header().Get("Access-Control-Allow-Origin") != "" {
			t.Fatal("foreign preflight passed the guard")
		}
	}
}

func TestQueryCredentialsAndEmptyServerTokenNeverAuthenticate(t *testing.T) {
	for _, path := range []string{"/app/v1/health", "/app/v1/shutdown", "/app/v1/events", "/v1/about", "/unknown"} {
		w := request(t, New(testToken, Options{}), "GET", path+"?access="+testToken, "", nil, "127.0.0.1:4000")
		if w.Code != 401 {
			t.Fatalf("query credential authenticated %s: %d", path, w.Code)
		}
	}
	w := request(t, New("", Options{}), "GET", "/app/v1/health", "", http.Header{"Authorization": {"Bearer "}}, "127.0.0.1:4000")
	if w.Code != 401 {
		t.Fatal("empty server token authenticated a request")
	}
}

func TestLaunchTokensAreFresh32ByteHexCredentials(t *testing.T) {
	seen := map[string]bool{}
	for range 20 {
		token, err := Token()
		if err != nil {
			t.Fatal(err)
		}
		bytes, err := hex.DecodeString(token)
		if err != nil || len(bytes) != 32 || seen[token] {
			t.Fatalf("invalid or repeated launch token %q", token)
		}
		seen[token] = true
	}
}

func TestListenerRefusesAnythingOutsideNumericLoopback(t *testing.T) {
	for _, address := range []string{"0.0.0.0:0", "[::]:0", ":0", "localhost:0", "evil.example:0", "10.0.0.1:0", "127.0.0.1", "127.0.0.1:x"} {
		listener, err := Listen(address)
		if err == nil {
			listener.Close()
			t.Fatalf("opened disallowed address %q", address)
		}
	}
	listener, err := Listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	address := listener.Addr().(*net.TCPAddr)
	if address.Port == 0 || !address.IP.IsLoopback() {
		t.Fatalf("OS did not allocate a loopback port: %s", address)
	}
}
