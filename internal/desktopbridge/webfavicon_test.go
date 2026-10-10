package desktopbridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestWebFaviconRefusesLoopbackAndPrivateHosts(t *testing.T) {
	prevLookup, prevDial := webLookupIP, webDial
	t.Cleanup(func() {
		webLookupIP = prevLookup
		webDial = prevDial
	})
	webLookupIP = func(_ context.Context, host string) ([]netip.Addr, error) {
		host = canonicalHost(host)
		switch host {
		case "localhost":
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		case "rebind.example":
			return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
		case "split.example":
			return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("10.0.0.1")}, nil
		case "ok.example":
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		case "link.example":
			return []netip.Addr{netip.MustParseAddr("169.254.169.254")}, nil
		case "ula.example":
			return []netip.Addr{netip.MustParseAddr("fd00::1")}, nil
		case "mapped.example":
			return []netip.Addr{netip.MustParseAddr("::ffff:127.0.0.1")}, nil
		default:
			if ip, err := netip.ParseAddr(host); err == nil {
				return []netip.Addr{ip}, nil
			}
			return nil, errors.New("no such host")
		}
	}
	var mu sync.Mutex
	var dialed []string
	webDial = func(_ context.Context, _, address string) (net.Conn, error) {
		mu.Lock()
		dialed = append(dialed, address)
		mu.Unlock()
		return nil, errors.New("stopped before connect")
	}
	b := New(testToken, nil)
	t.Cleanup(b.Close)

	refused := []string{
		"http://127.0.0.1/secret",
		"http://127.0.0.1:9/secret",
		"http://[::1]/secret",
		"http://localhost/secret",
		"http://10.1.2.3/secret",
		"http://192.168.1.9/secret",
		"http://172.20.1.1/secret",
		"http://169.254.169.254/latest",
		"http://0.0.0.0/",
		"http://[fe80::1]/",
		"http://224.0.0.1/",
		"https://rebind.example/page",
		"https://split.example/page",
		"https://link.example/",
		"https://ula.example/",
		"https://mapped.example/",
		"https://user:pass@ok.example/secret",
		"file:///etc/passwd",
		"http://",
	}
	for _, raw := range refused {
		mu.Lock()
		dialed = nil
		mu.Unlock()
		w := callWebFavicon(b, testToken, raw)
		mu.Lock()
		got := append([]string(nil), dialed...)
		mu.Unlock()
		if w.Code != 200 || strings.Contains(w.Body.String(), "dataUrl") || len(got) != 0 {
			t.Fatalf("%s dialed %v body %s", raw, got, w.Body.String())
		}
	}

	mu.Lock()
	dialed = nil
	mu.Unlock()
	w := callWebFavicon(b, testToken, "https://ok.example/page")
	mu.Lock()
	got := append([]string(nil), dialed...)
	mu.Unlock()
	if len(got) == 0 {
		t.Fatal("a public address was not dialed")
	}
	for _, address := range got {
		if err := publicOnly("tcp", address, nil); err != nil {
			t.Fatalf("dialed non-public %s: %v", address, err)
		}
		if !strings.HasPrefix(address, "93.184.216.34:") {
			t.Fatalf("dialed the name instead of the checked address: %s", address)
		}
	}
	if strings.Contains(w.Body.String(), "dataUrl") {
		t.Fatalf("a failed public dial still returned an icon: %s", w.Body.String())
	}

	via := []*http.Request{{URL: mustURL("https://ok.example/favicon.ico")}}
	if err := webIconRedirect(&http.Request{URL: mustURL("http://127.0.0.1/secret")}, via); err == nil {
		t.Fatal("a redirect onto another host was followed")
	}
	if err := webIconRedirect(&http.Request{URL: mustURL("https://ok.example/icon.png")}, via); err != nil {
		t.Fatal(err)
	}
}

func TestWebFaviconCapsSizeAndTime(t *testing.T) {
	if faviconTimeout != 3*time.Second || faviconMaxBytes != 64<<10 {
		t.Fatalf("caps are %s and %d", faviconTimeout, faviconMaxBytes)
	}
	if newWebIconCache().timeout != faviconTimeout || newWebIconCache().ttl != time.Hour {
		t.Fatal("web favicon cache does not use the 3s and 1h caps")
	}
	png := []byte("png-bytes")

	t.Run("oversize", func(t *testing.T) {
		srv := iconServer(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/favicon.ico" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(bytesRepeat(faviconMaxBytes + 1))
		})
		w := callWebFavicon(iconBridge(t, srv, faviconTimeout), testToken, "https://big.example/page")
		if w.Code != 200 || strings.Contains(w.Body.String(), "dataUrl") {
			t.Fatal(w.Code, w.Body.String())
		}
	})

	t.Run("exact", func(t *testing.T) {
		body := bytesRepeat(faviconMaxBytes)
		srv := iconServer(t, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(body)
		})
		w := callWebFavicon(iconBridge(t, srv, faviconTimeout), testToken, "https://exact.example/")
		got := iconBody(t, w)
		raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(got, "data:image/png;base64,"))
		if err != nil || len(raw) != faviconMaxBytes {
			t.Fatalf("exact icon: %d %v %q", len(raw), err, got[:min(40, len(got))])
		}
	})

	t.Run("not an image", func(t *testing.T) {
		for _, kind := range []string{"text/html", "image/svg+xml"} {
			srv := iconServer(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", kind)
				_, _ = io.WriteString(w, "<svg></svg>")
			})
			w := callWebFavicon(iconBridge(t, srv, faviconTimeout), testToken, "https://kind.example/")
			if strings.Contains(w.Body.String(), "dataUrl") {
				t.Fatalf("%s was kept: %s", kind, w.Body.String())
			}
		}
	})

	t.Run("time", func(t *testing.T) {
		srv := iconServer(t, func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(2 * time.Second):
			}
		})
		b := iconBridge(t, srv, 80*time.Millisecond)
		start := time.Now()
		w := callWebFavicon(b, testToken, "https://slow.example/page")
		if elapsed := time.Since(start); elapsed > time.Second {
			t.Fatalf("fetch ignored the time cap: %s", elapsed)
		}
		if w.Code != 200 || strings.Contains(w.Body.String(), "dataUrl") {
			t.Fatal(w.Code, w.Body.String())
		}
	})

	t.Run("head link", func(t *testing.T) {
		var apple, icon int
		srv := iconServer(t, func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/favicon.ico":
				http.NotFound(w, r)
			case "/apple.png":
				apple++
				http.NotFound(w, r)
			case "/icon.png":
				icon++
				if r.Method == http.MethodHead {
					w.Header().Set("Content-Type", "image/png")
					w.Header().Set("Content-Length", "9")
					return
				}
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write(png)
			default:
				w.Header().Set("Content-Type", "text/html")
				_, _ = io.WriteString(w, `<!doctype html><html><head><link rel="apple-touch-icon" href="/apple.png"><link rel="shortcut icon" href="/icon.png"></head><body>`)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
				select {
				case <-r.Context().Done():
				case <-time.After(2 * time.Second):
				}
			}
		})
		b := iconBridge(t, srv, faviconTimeout)
		start := time.Now()
		w := callWebFavicon(b, testToken, "https://linked.example/docs")
		if time.Since(start) > time.Second {
			t.Fatal("the page body was read past the head")
		}
		if !strings.HasPrefix(iconBody(t, w), "data:image/png;base64,") || apple != 0 || icon < 1 {
			t.Fatalf("apple %d icon %d body %s", apple, icon, w.Body.String())
		}
	})

	t.Run("head refuses a large link", func(t *testing.T) {
		var gets int
		srv := iconServer(t, func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.URL.Path == "/favicon.ico":
				http.NotFound(w, r)
			case r.URL.Path == "/huge.png" && r.Method == http.MethodHead:
				w.Header().Set("Content-Type", "image/png")
				w.Header().Set("Content-Length", "999999")
			case r.URL.Path == "/huge.png":
				gets++
				w.Header().Set("Content-Type", "image/png")
				_, _ = w.Write(png)
			default:
				w.Header().Set("Content-Type", "text/html")
				_, _ = io.WriteString(w, `<html><head><link rel="icon" href="/huge.png"></head></html>`)
			}
		})
		w := callWebFavicon(iconBridge(t, srv, faviconTimeout), testToken, "https://huge.example/")
		if gets != 0 || strings.Contains(w.Body.String(), "dataUrl") {
			t.Fatalf("gets %d body %s", gets, w.Body.String())
		}
	})

	if dataIcon("data:image/svg+xml;base64,PHN2Zy8+") != "" {
		t.Fatal("svg data icon was kept")
	}
	big := "data:image/png;base64," + base64.StdEncoding.EncodeToString(bytesRepeat(faviconMaxBytes+1))
	if dataIcon(big) != "" {
		t.Fatal("oversize data icon was kept")
	}
	srv := iconServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/favicon.ico" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, `<html><head><link rel="icon" href="data:image/png;base64,cG5nLWJ5dGVz"></head></html>`)
	})
	w := callWebFavicon(iconBridge(t, srv, faviconTimeout), testToken, "https://inline.example/")
	if iconBody(t, w) != "data:image/png;base64,"+base64.StdEncoding.EncodeToString(png) {
		t.Fatal(w.Body.String())
	}
}

func TestWebFaviconCachesPerHost(t *testing.T) {
	var hits int
	srv := iconServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "image/png")
		_, _ = io.WriteString(w, "png")
	})
	b := iconBridge(t, srv, faviconTimeout)
	c := b.webIconCache()
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }

	first := iconBody(t, callWebFavicon(b, testToken, "https://cache.example/one"))
	second := iconBody(t, callWebFavicon(b, testToken, "https://cache.example/two?q=1"))
	if first == "" || first != second || hits != 1 {
		t.Fatalf("hits %d first %q second %q", hits, first, second)
	}
	now = now.Add(time.Hour - time.Second)
	_ = callWebFavicon(b, testToken, "https://cache.example/later")
	if hits != 1 {
		t.Fatalf("refetched inside the hour: %d", hits)
	}
	now = now.Add(time.Second)
	_ = callWebFavicon(b, testToken, "https://cache.example/expired")
	if hits != 2 {
		t.Fatalf("a host older than an hour was not fetched again: %d", hits)
	}
	_ = callWebFavicon(b, testToken, "https://other.example/")
	if hits != 3 {
		t.Fatalf("a second host reused the first host's fetch: %d", hits)
	}

	var misses int
	missSrv := iconServer(t, func(w http.ResponseWriter, r *http.Request) {
		misses++
		http.NotFound(w, r)
	})
	mb := iconBridge(t, missSrv, faviconTimeout)
	if strings.Contains(callWebFavicon(mb, testToken, "https://miss.example/a").Body.String(), "dataUrl") {
		t.Fatal("a miss returned an icon")
	}
	after := misses
	_ = callWebFavicon(mb, testToken, "https://miss.example/b")
	if misses != after {
		t.Fatalf("a remembered miss was fetched again: %d then %d", after, misses)
	}
}

func TestWebFaviconNeedsTheToken(t *testing.T) {
	b := New(testToken, nil)
	t.Cleanup(b.Close)
	var fetches int
	b.webIconCache().fetch = func(context.Context, string, *http.Client) string {
		fetches++
		return "data:image/png;base64,YQ=="
	}
	for _, token := range []string{"", "wrong", "Bearer " + testToken} {
		w := callWebFavicon(b, token, "https://example.com/page")
		if w.Code != 401 || fetches != 0 {
			t.Fatalf("token %q: %d fetches %d %s", token, w.Code, fetches, w.Body.String())
		}
	}
	bare := New("", nil)
	t.Cleanup(bare.Close)
	bare.webIconCache().fetch = b.webIconCache().fetch
	if w := callWebFavicon(bare, testToken, "https://example.com/"); w.Code != 401 || fetches != 0 {
		t.Fatalf("empty bridge token: %d fetches %d", w.Code, fetches)
	}

	w := callWebFavicon(b, testToken, "https://example.com/page")
	if w.Code != 200 || iconBody(t, w) != "data:image/png;base64,YQ==" || fetches != 1 {
		t.Fatalf("with the token: %d fetches %d %s", w.Code, fetches, w.Body.String())
	}
	r := httptest.NewRequest(http.MethodPost, "/favicon?url=https://example.com/", nil)
	r.Header.Set("Authorization", "Bearer "+testToken)
	rec := httptest.NewRecorder()
	b.webFavicon(rec, r)
	if rec.Code != 405 || fetches != 1 {
		t.Fatalf("POST: %d fetches %d", rec.Code, fetches)
	}
}

func callWebFavicon(b *Bridge, token, raw string) *httptest.ResponseRecorder {
	q := url.Values{}
	q.Set("url", raw)
	r := httptest.NewRequest(http.MethodGet, "/favicon?"+q.Encode(), nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	b.webFavicon(w, r)
	return w
}

func iconBody(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	if w.Code != 200 {
		t.Fatalf("status %d %s", w.Code, w.Body.String())
	}
	var body struct {
		DataURL string `json:"dataUrl"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body.DataURL
}

func iconBridge(t *testing.T, srv *httptest.Server, timeout time.Duration) *Bridge {
	t.Helper()
	b := New(testToken, nil)
	t.Cleanup(b.Close)
	c := b.webIconCache()
	c.timeout = timeout
	c.client = &http.Client{
		Timeout:       timeout,
		CheckRedirect: webIconRedirect,
		Transport:     &rewriteIconTransport{base: srv.Client().Transport, dest: srv.URL},
	}
	return b
}

func iconServer(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

type rewriteIconTransport struct {
	base http.RoundTripper
	dest string
}

func (t *rewriteIconTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	dest, err := url.Parse(t.dest)
	if err != nil {
		return nil, err
	}
	u := *req.URL
	u.Scheme = dest.Scheme
	u.Host = dest.Host
	clone := req.Clone(req.Context())
	clone.URL = &u
	clone.Host = dest.Host
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

func mustURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil {
		panic(err)
	}
	return u
}

func bytesRepeat(n int) []byte {
	return []byte(strings.Repeat("x", n))
}
