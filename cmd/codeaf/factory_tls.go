package main

import (
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/acme"
	"golang.org/x/crypto/acme/autocert"
)

// The three ways `codeaf factory serve` answers the network. The words are
// the host config's own (`tls:`), and the manual quotes them.
const (
	factoryTLSAuto  = "auto"
	factoryTLSFiles = "files"
	factoryTLSOff   = "off"
)

// factoryACMEDirectory is where auto asks for certificates. It is a variable
// only so a test can prove the wiring without reaching a real authority.
var factoryACMEDirectory = acme.LetsEncryptURL

// factoryTLS is the host config's HTTPS keys after the listen address is
// final: one mode, and everything that mode needs, already checked.
type factoryTLS struct {
	mode     string
	domain   string
	certFile string
	keyFile  string
	cacheDir string
	proxies  []netip.Prefix
	// redirect is where the plain-HTTP door that answers certificate
	// challenges and sends everyone else to HTTPS listens. Empty is no door.
	redirect string
}

// on says this process terminates TLS itself.
func (t factoryTLS) on() bool { return t.mode == factoryTLSAuto || t.mode == factoryTLSFiles }

// checkFactoryTLSConfig is everything about the HTTPS keys that can be judged
// without the listen address, so a typo fails `factory token` as loudly as it
// fails `factory serve`.
func checkFactoryTLSConfig(cfg factoryHostConfig) error {
	switch factoryTLSWord(cfg.TLS) {
	case "", factoryTLSAuto, factoryTLSFiles, factoryTLSOff:
	default:
		return fmt.Errorf("tls must be auto, files or off, not %q", cfg.TLS)
	}
	if domain := strings.TrimSpace(cfg.Domain); domain != "" && !factoryDomainOK(domain) {
		return fmt.Errorf("domain must be a host name such as factory.example.com, without https://, a path or a port; %q is not", cfg.Domain)
	}
	if (strings.TrimSpace(cfg.CertFile) == "") != (strings.TrimSpace(cfg.KeyFile) == "") {
		return errors.New("cert_file and key_file go together: name both, or neither")
	}
	_, err := factoryProxies(cfg.TrustedProxies)
	return err
}

func factoryTLSWord(word string) string { return strings.ToLower(strings.TrimSpace(word)) }

// resolveFactoryTLS settles the mode for this listen address. An unset tls is
// files when a certificate is named, off on loopback, and auto everywhere
// else, BECAUSE A FACTORY OFF THIS MACHINE CARRIES ACCESS TOKENS AND SIGN-IN
// SESSIONS, and plain HTTP hands both to anyone on the path.
func resolveFactoryTLS(cfg factoryHostConfig, listen, configDir string) (factoryTLS, error) {
	if err := checkFactoryTLSConfig(cfg); err != nil {
		return factoryTLS{}, err
	}
	proxies, _ := factoryProxies(cfg.TrustedProxies)
	t := factoryTLS{mode: factoryTLSWord(cfg.TLS), domain: strings.ToLower(strings.TrimSpace(cfg.Domain)), proxies: proxies,
		certFile: factoryConfigPath(configDir, cfg.CertFile), keyFile: factoryConfigPath(configDir, cfg.KeyFile)}
	implicit := t.mode == ""
	loopback := factoryLoopbackListen(listen)
	switch {
	case !implicit:
	case t.certFile != "":
		t.mode = factoryTLSFiles
	case loopback:
		t.mode = factoryTLSOff
	default:
		t.mode = factoryTLSAuto
	}
	if t.mode != factoryTLSFiles && t.certFile != "" {
		return factoryTLS{}, fmt.Errorf("cert_file and key_file are read only when tls is files, and tls is %s", t.mode)
	}
	if t.mode != factoryTLSAuto && strings.TrimSpace(cfg.CacheDir) != "" {
		return factoryTLS{}, fmt.Errorf("cache_dir is read only when tls is auto, and tls is %s", t.mode)
	}
	switch t.mode {
	case factoryTLSAuto:
		if t.domain == "" {
			why := "tls is auto"
			if implicit {
				why = "tls is auto because " + listen + " is not a loopback address"
			}
			return factoryTLS{}, fmt.Errorf("%s, and auto needs domain, the public name its certificate is for. "+
				"Set domain, or set tls: files with cert_file and key_file, or set tls: off when a reverse proxy in front serves HTTPS", why)
		}
		if net.ParseIP(t.domain) != nil {
			return factoryTLS{}, errors.New("tls: auto needs a host name in domain; a certificate authority will not issue one for an IP address")
		}
		if loopback {
			return factoryTLS{}, fmt.Errorf("tls: auto cannot work on %s: the certificate authority has to reach this machine, and a loopback address cannot be reached", listen)
		}
		t.cacheDir = factoryConfigPath(configDir, cfg.CacheDir)
		if t.cacheDir == "" {
			t.cacheDir = filepath.Join(configDir, "tls")
		}
		t.redirect = factoryRedirectAddr(listen)
	case factoryTLSFiles:
		if t.certFile == "" {
			return factoryTLS{}, errors.New("tls: files needs cert_file and key_file, the certificate and its private key")
		}
		// The challenge door has nothing to answer here, so it opens only
		// where HTTPS is on its usual port and :80 is the natural way in.
		if _, port, err := net.SplitHostPort(listen); err == nil && port == "443" {
			t.redirect = factoryRedirectAddr(listen)
		}
	}
	return t, nil
}

// factoryConfigPath reads a path from the host config: relative paths are
// beside the config file, so the file means the same thing from any directory.
func factoryConfigPath(configDir, p string) string {
	p = strings.TrimSpace(p)
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(configDir, p)
}

// factoryLoopbackListen says listen is only reachable from this machine. An
// empty host (":7420") is every interface, which is not loopback.
func factoryLoopbackListen(listen string) bool {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// factoryRedirectAddr is port 80 on the interface the factory listens on.
func factoryRedirectAddr(listen string) string {
	host, _, err := net.SplitHostPort(listen)
	if err != nil {
		return ""
	}
	return net.JoinHostPort(host, "80")
}

// factoryDomainOK accepts a DNS host name: dot-separated labels of letters,
// digits and hyphens, no scheme, path or port.
func factoryDomainOK(domain string) bool {
	if len(domain) > 253 || !strings.Contains(domain, ".") {
		return false
	}
	for _, label := range strings.Split(strings.ToLower(domain), ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

// factoryProxies reads trusted_proxies: single addresses or CIDR ranges.
func factoryProxies(list []string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, raw := range list {
		raw = strings.TrimSpace(raw)
		if p, err := netip.ParsePrefix(raw); err == nil {
			out = append(out, p.Masked())
			continue
		}
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			return nil, fmt.Errorf("trusted_proxies: %q is not an address or a range such as 10.0.0.0/8", raw)
		}
		addr = addr.Unmap()
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}

// serverTLS is the TLS configuration for the public listener, and the handler
// the plain-HTTP door serves: the certificate challenge when auto, and a
// redirect to HTTPS for everything else.
func (t factoryTLS) serverTLS(publicPort string) (*tls.Config, http.Handler, error) {
	redirect := factoryHTTPSRedirect(t.domain, publicPort)
	switch t.mode {
	case factoryTLSAuto:
		// The cache holds the account key and every certificate's private
		// key; autocert writes its files 0600 inside this 0700 folder.
		if err := os.MkdirAll(t.cacheDir, 0o700); err != nil {
			return nil, nil, fmt.Errorf("cannot make the certificate cache %s: %w", t.cacheDir, err)
		}
		m := &autocert.Manager{
			Prompt:     autocert.AcceptTOS,
			HostPolicy: autocert.HostWhitelist(t.domain),
			Cache:      autocert.DirCache(t.cacheDir),
			Client:     &acme.Client{DirectoryURL: factoryACMEDirectory},
		}
		// TLSConfig answers TLS-ALPN-01 on the public listener; HTTPHandler
		// answers HTTP-01 on the door and redirects every other request.
		// Renewal is the manager's: it renews ahead of expiry on its own.
		return m.TLSConfig(), m.HTTPHandler(redirect), nil
	case factoryTLSFiles:
		r := &factoryCertReloader{certFile: t.certFile, keyFile: t.keyFile}
		if _, err := r.certificate(); err != nil {
			return nil, nil, err
		}
		return &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"h2", "http/1.1"},
			GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) { return r.certificate() }}, redirect, nil
	}
	return nil, nil, nil
}

// factoryHTTPSRedirect sends a plain-HTTP request to the same path over
// HTTPS. With a domain the target is always that name, so the door never
// redirects anywhere the host did not choose.
func factoryHTTPSRedirect(domain, port string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := domain
		if host == "" {
			host = r.Host
			if h, _, err := net.SplitHostPort(r.Host); err == nil {
				host = h
			}
		}
		if port != "" && port != "443" {
			host = net.JoinHostPort(host, port)
		}
		http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusPermanentRedirect)
	})
}

// publicURL is the address a person types: the domain when there is one,
// without the port when it is HTTPS's own.
func (t factoryTLS) publicURL(listenAddr string) string {
	_, port, _ := net.SplitHostPort(listenAddr)
	switch {
	case t.on() && t.domain != "":
		if port == "443" {
			return "https://" + t.domain
		}
		return "https://" + net.JoinHostPort(t.domain, port)
	case t.on():
		return "https://" + listenAddr
	case t.domain != "":
		// Off with a domain is the reverse-proxy shape: the proxy serves
		// HTTPS on the name, and this process listens behind it.
		return "https://" + t.domain
	}
	return "http://" + listenAddr
}

// factoryCertReloader serves cert_file and key_file, and reads them again when
// either changes on disk, so a renewal by another tool needs no restart. A
// pair that fails to load keeps the last good certificate in service.
type factoryCertReloader struct {
	certFile, keyFile string

	mu      sync.Mutex
	current *tls.Certificate
	certMod time.Time
	keyMod  time.Time
}

func (r *factoryCertReloader) certificate() (*tls.Certificate, error) {
	certInfo, certErr := os.Stat(r.certFile)
	keyInfo, keyErr := os.Stat(r.keyFile)
	r.mu.Lock()
	defer r.mu.Unlock()
	if certErr == nil && keyErr == nil && r.current != nil &&
		certInfo.ModTime().Equal(r.certMod) && keyInfo.ModTime().Equal(r.keyMod) {
		return r.current, nil
	}
	pair, err := tls.LoadX509KeyPair(r.certFile, r.keyFile)
	if err != nil {
		if r.current != nil {
			return r.current, nil
		}
		return nil, fmt.Errorf("cannot load cert_file %s and key_file %s: %w", r.certFile, r.keyFile, err)
	}
	r.current = &pair
	if certErr == nil && keyErr == nil {
		r.certMod, r.keyMod = certInfo.ModTime(), keyInfo.ModTime()
	}
	return r.current, nil
}
