package main

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/home"
)

func TestFactoryTLSConfigRefusesInPlainWords(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name   string
		cfg    factoryHostConfig
		listen string
		mode   string
		err    string
	}{
		{"loopback defaults to off", factoryHostConfig{}, "127.0.0.1:7420", factoryTLSOff, ""},
		{"localhost defaults to off", factoryHostConfig{}, "localhost:7420", factoryTLSOff, ""},
		{"off loopback defaults to auto", factoryHostConfig{Domain: "factory.example.com"}, "0.0.0.0:443", factoryTLSAuto, ""},
		{"every interface is not loopback", factoryHostConfig{}, ":7420", "", "tls is auto because :7420 is not a loopback address, and auto needs domain"},
		{"a certificate implies files", factoryHostConfig{CertFile: "c.pem", KeyFile: "k.pem"}, "0.0.0.0:443", factoryTLSFiles, ""},
		{"explicit off is allowed anywhere", factoryHostConfig{TLS: "off"}, "0.0.0.0:7420", factoryTLSOff, ""},
		{"unknown word", factoryHostConfig{TLS: "yes"}, "127.0.0.1:1", "", `tls must be auto, files or off, not "yes"`},
		{"auto without domain", factoryHostConfig{TLS: "auto"}, "0.0.0.0:443", "", "tls is auto, and auto needs domain"},
		{"auto on loopback", factoryHostConfig{TLS: "auto", Domain: "factory.example.com"}, "127.0.0.1:443", "", "a loopback address cannot be reached"},
		{"domain with scheme", factoryHostConfig{Domain: "https://factory.example.com"}, "0.0.0.0:443", "", "domain must be a host name"},
		{"domain with port", factoryHostConfig{Domain: "factory.example.com:443"}, "0.0.0.0:443", "", "domain must be a host name"},
		{"auto for an address", factoryHostConfig{TLS: "auto", Domain: "192.0.2.1"}, "0.0.0.0:443", "", "will not issue one for an IP address"},
		{"files without files", factoryHostConfig{TLS: "files"}, "0.0.0.0:443", "", "tls: files needs cert_file and key_file"},
		{"half a pair", factoryHostConfig{CertFile: "c.pem"}, "0.0.0.0:443", "", "cert_file and key_file go together"},
		{"certificate with auto", factoryHostConfig{TLS: "auto", Domain: "factory.example.com", CertFile: "c", KeyFile: "k"}, "0.0.0.0:443", "", "read only when tls is files, and tls is auto"},
		{"cache_dir with off", factoryHostConfig{TLS: "off", CacheDir: "tls"}, "0.0.0.0:443", "", "cache_dir is read only when tls is auto"},
		{"bad proxy", factoryHostConfig{TrustedProxies: []string{"proxy.local"}}, "127.0.0.1:1", "", `trusted_proxies: "proxy.local" is not an address or a range`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveFactoryTLS(tc.cfg, tc.listen, dir)
			if tc.err != "" {
				if err == nil || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("err = %v, want it to say %q", err, tc.err)
				}
				return
			}
			if err != nil || got.mode != tc.mode {
				t.Fatalf("mode = %q, err %v; want %q", got.mode, err, tc.mode)
			}
		})
	}
}

func TestFactoryTLSResolvesPathsAndProxies(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveFactoryTLS(factoryHostConfig{Domain: "Factory.Example.com",
		TrustedProxies: []string{"10.0.0.0/8", "127.0.0.1", "::1"}}, "0.0.0.0:8443", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.domain != "factory.example.com" || got.cacheDir != filepath.Join(dir, "tls") || got.redirect != "0.0.0.0:80" || len(got.proxies) != 3 {
		t.Fatalf("resolved = %+v", got)
	}
	if url := got.publicURL("0.0.0.0:8443"); url != "https://factory.example.com:8443" {
		t.Fatalf("public URL = %q", url)
	}
	if url := got.publicURL("[::]:443"); url != "https://factory.example.com" {
		t.Fatalf("public URL on 443 = %q", url)
	}
	files, err := resolveFactoryTLS(factoryHostConfig{CertFile: "c.pem", KeyFile: "/abs/k.pem"}, "0.0.0.0:8443", dir)
	if err != nil || files.certFile != filepath.Join(dir, "c.pem") || files.keyFile != "/abs/k.pem" || files.redirect != "" {
		t.Fatalf("files = %+v, %v", files, err)
	}
	proxied := factoryTLS{mode: factoryTLSOff, domain: "factory.example.com"}
	if url := proxied.publicURL("127.0.0.1:7420"); url != "https://factory.example.com" {
		t.Fatalf("behind a proxy the public URL is the proxy's: %q", url)
	}
}

// A TOKEN ROTATION REWRITES THE CONFIG, and must not drop the HTTPS keys.
func TestFactoryTLSKeysSurviveATokenRotation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	body := "listen: 0.0.0.0:443\naccess_token: a\ntls: auto\ndomain: factory.example.com\ncache_dir: certs\ntrusted_proxies: [10.0.0.1]\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := factoryHostConfigAt(path, true); err != nil {
		t.Fatal(err)
	}
	cfg, err := readFactoryHostConfig(path)
	if err != nil || cfg.AccessToken == "a" || cfg.TLS != "auto" || cfg.Domain != "factory.example.com" ||
		cfg.CacheDir != "certs" || !slices.Equal(cfg.TrustedProxies, []string{"10.0.0.1"}) {
		t.Fatalf("after rotation = %+v, %v", cfg, err)
	}
	if err := os.WriteFile(path, []byte("listen: 127.0.0.1:1\naccess_token: a\ntls: maybe\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readFactoryHostConfig(path); err == nil || !strings.Contains(err.Error(), "tls must be auto, files or off") {
		t.Fatalf("a bad tls word is read without complaint: %v", err)
	}
}

// writeSelfSigned writes a certificate for names and its key into dir.
func writeSelfSigned(t *testing.T, dir string, serial int64, names ...string) (certFile, keyFile string, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: names[0]},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), DNSNames: names,
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, IsCA: true, BasicConstraintsValid: true,
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, _ := x509.ParseCertificate(der)
	pool = x509.NewCertPool()
	pool.AddCert(parsed)
	return certFile, keyFile, pool
}

// THE SERVICE SERVES HTTPS IN FILES MODE: the public listener speaks TLS with
// the named certificate and HSTS, /v1/about says https, the event stream
// flows through TLS, and serve.json still names plain loopback for this
// machine's own clients.
func TestFactoryServesHTTPSFromCertificateFiles(t *testing.T) {
	t.Setenv(home.EnvVar, t.TempDir())
	dir := t.TempDir()
	certFile, keyFile, pool := writeSelfSigned(t, dir, 1, "factory.test")
	st, err := store.Open(filepath.Join(dir, "store"))
	if err != nil {
		t.Fatal(err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	secure := factoryTLS{mode: factoryTLSFiles, certFile: certFile, keyFile: keyFile}
	s, err := startFactoryService(context.Background(), st, factory.LocalSeam(st, time.Now()), l, "floor-secret", "",
		factoryHostConfig{secure: &secure})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.close)
	if !strings.HasPrefix(s.public, "https://127.0.0.1:") || !strings.HasPrefix(s.url, "http://127.0.0.1:") || s.url == "http://"+l.Addr().String() {
		t.Fatalf("public %q, local %q", s.public, s.url)
	}
	var receipt factoryDiscovery
	body, _ := os.ReadFile(filepath.Join(st.Root(), "serve.json"))
	if json.Unmarshal(body, &receipt) != nil || receipt.URL != s.url {
		t.Fatalf("serve.json = %s, want the loopback address %s", body, s.url)
	}
	client := &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "factory.test"}, ForceAttemptHTTP2: true}}
	resp, err := client.Get(s.public + "/v1/about")
	if err != nil {
		t.Fatal(err)
	}
	var about struct {
		Scheme  string `json:"scheme"`
		Factory struct {
			Address string `json:"address"`
		} `json:"factory"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&about)
	resp.Body.Close()
	if about.Scheme != "https" || about.Factory.Address != s.public || resp.Header.Get("Strict-Transport-Security") == "" {
		t.Fatalf("about over TLS = %+v, headers %v", about, resp.Header)
	}
	// The event stream answers its headers and a first event through TLS.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", s.public+"/v1/events", nil)
	req.Header.Set("Authorization", "Bearer floor-secret")
	req.Header.Set("Last-Event-ID", "unknown")
	resp, err = client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Content-Type") != "text/event-stream" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("events over TLS = %d %v", resp.StatusCode, resp.Header)
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil || line != "event: changed\n" {
		t.Fatalf("first event line = %q, %v", line, err)
	}
	// The local door is plain HTTP, and carries no HSTS.
	local, err := http.Get(s.url + "/v1/about")
	if err != nil {
		t.Fatal(err)
	}
	local.Body.Close()
	if local.StatusCode != http.StatusOK || local.Header.Get("Strict-Transport-Security") != "" {
		t.Fatalf("local door = %d %v", local.StatusCode, local.Header)
	}
}

func TestFactoryCertificateFilesReloadWhenTheyChange(t *testing.T) {
	dir := t.TempDir()
	certFile, keyFile, _ := writeSelfSigned(t, dir, 1, "factory.test")
	r := &factoryCertReloader{certFile: certFile, keyFile: keyFile}
	first, err := r.certificate()
	if err != nil {
		t.Fatal(err)
	}
	writeSelfSigned(t, dir, 2, "factory.test")
	later := time.Now().Add(time.Minute)
	for _, f := range []string{certFile, keyFile} {
		if err := os.Chtimes(f, later, later); err != nil {
			t.Fatal(err)
		}
	}
	second, err := r.certificate()
	if err != nil || second == first {
		t.Fatalf("a renewed pair was not read: %v", err)
	}
	// A half-written renewal keeps the last good certificate in service.
	if err := os.WriteFile(certFile, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(certFile, later.Add(time.Minute), later.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if kept, err := r.certificate(); err != nil || kept != second {
		t.Fatalf("a broken renewal replaced the certificate: %v", err)
	}
	missing := &factoryCertReloader{certFile: filepath.Join(dir, "none.pem"), keyFile: keyFile}
	if _, err := missing.certificate(); err == nil || !strings.Contains(err.Error(), "cannot load cert_file") {
		t.Fatalf("a missing certificate = %v", err)
	}
}

// THE PLAIN-HTTP DOOR REDIRECTS EVERYTHING BUT THE CHALLENGE, and only ever to
// the configured name.
func TestFactoryHTTPDoorRedirectsToHTTPSExceptTheChallenge(t *testing.T) {
	prior := factoryACMEDirectory
	factoryACMEDirectory = "https://acme.invalid/directory"
	t.Cleanup(func() { factoryACMEDirectory = prior })
	cache := filepath.Join(t.TempDir(), "tls")
	auto := factoryTLS{mode: factoryTLSAuto, domain: "factory.example.com", cacheDir: cache}
	config, door, err := auto.serverTLS("443")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(config.NextProtos, "acme-tls/1") || config.GetCertificate == nil {
		t.Fatalf("auto TLS does not answer TLS-ALPN-01: %v", config.NextProtos)
	}
	if info, err := os.Stat(cache); err != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("certificate cache = %v, %v", info, err)
	}
	// A name the policy does not cover is refused before any authority is asked.
	if _, err := config.GetCertificate(&tls.ClientHelloInfo{ServerName: "other.example.com"}); err == nil {
		t.Fatal("a certificate was offered for a name that is not the domain")
	}
	for _, tc := range []struct{ host, path, want string }{
		{"factory.example.com", "/v1/queue?x=1", "https://factory.example.com/v1/queue?x=1"},
		{"evil.example.net", "/", "https://factory.example.com/"},
	} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "http://"+tc.host+tc.path, nil)
		door.ServeHTTP(w, r)
		if w.Code != http.StatusPermanentRedirect || w.Header().Get("Location") != tc.want {
			t.Fatalf("door %s%s = %d %q", tc.host, tc.path, w.Code, w.Header().Get("Location"))
		}
	}
	w := httptest.NewRecorder()
	door.ServeHTTP(w, httptest.NewRequest("GET", "http://factory.example.com/.well-known/acme-challenge/token", nil))
	if w.Code == http.StatusPermanentRedirect || w.Code/100 == 3 {
		t.Fatalf("the challenge path was redirected: %d", w.Code)
	}
	other := factoryHTTPSRedirect("factory.example.com", "8443")
	w = httptest.NewRecorder()
	other.ServeHTTP(w, httptest.NewRequest("POST", "http://factory.example.com/access", nil))
	if w.Header().Get("Location") != "https://factory.example.com:8443/access" {
		t.Fatalf("redirect off 443 = %q", w.Header().Get("Location"))
	}
}
