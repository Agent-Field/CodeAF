package desktopbridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	faviconTimeout  = 3 * time.Second
	faviconMaxBytes = 64 << 10
)

var urlInText = regexp.MustCompile(`https?://[^\s"'<>)\]\\]+`)

// faviconCache remembers one answer per domain for the bridge's lifetime; an
// empty string is a remembered miss, so a dead site is asked about once.
type faviconCache struct {
	mu    sync.Mutex
	items map[string]string
	// fetch is replaced in tests; the default talks to the public internet only.
	fetch func(ctx context.Context, domain string) (dataURL string)
}

func newFaviconCache() *faviconCache {
	return &faviconCache{items: map[string]string{}, fetch: fetchFavicon}
}

func (c *faviconCache) lookup(domain string) string {
	c.mu.Lock()
	cached, ok := c.items[domain]
	c.mu.Unlock()
	if ok {
		return cached
	}
	ctx, cancel := context.WithTimeout(context.Background(), faviconTimeout)
	defer cancel()
	got := c.fetch(ctx, domain)
	c.mu.Lock()
	c.items[domain] = got
	c.mu.Unlock()
	return got
}

// contactedDomains is every host this conversation's own web_fetch and
// web_search calls named in their arguments or showed in their results. The
// renderer may ask about these and nothing else.
func (s *conversation) contactedDomains() map[string]bool {
	hosts := map[string]bool{}
	for _, entry := range s.conn.Agent.Transcript() {
		if entry.Role != "tool" || (entry.Tool != "web_fetch" && entry.Tool != "web_search") {
			continue
		}
		var args struct {
			URL string `json:"url"`
		}
		if json.Unmarshal([]byte(entry.Args), &args) == nil {
			addHost(hosts, args.URL)
		}
		for _, found := range urlInText.FindAllString(entry.Output, -1) {
			addHost(hosts, found)
		}
	}
	return hosts
}

func addHost(hosts map[string]bool, raw string) {
	if u, err := url.Parse(strings.TrimSpace(raw)); err == nil && u.Hostname() != "" {
		hosts[strings.ToLower(u.Hostname())] = true
	}
}

// plainDomain is a registrable-looking name: dotted, no port, path or address.
func plainDomain(domain string) bool {
	if domain == "" || len(domain) > 253 || !strings.Contains(domain, ".") {
		return false
	}
	if _, err := netip.ParseAddr(domain); err == nil {
		return false
	}
	for _, r := range domain {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}

func (s *conversation) favicon(w http.ResponseWriter, r *http.Request) {
	if !needGet(w, r) {
		return
	}
	domain := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("domain")))
	if !plainDomain(domain) || !s.contactedDomains()[domain] {
		write(w, map[string]string{})
		return
	}
	if dataURL := s.icons.lookup(domain); dataURL != "" {
		write(w, map[string]string{"dataUrl": dataURL})
		return
	}
	write(w, map[string]string{})
}

// publicOnly refuses to connect to loopback, private or link-local addresses,
// so a domain that resolves inward is never fetched.
func publicOnly(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
		return errors.New("address is not public")
	}
	return nil
}

func fetchFavicon(ctx context.Context, domain string) string {
	dialer := &net.Dialer{Timeout: faviconTimeout, Control: publicOnly}
	client := &http.Client{
		Timeout:   faviconTimeout,
		Transport: &http.Transport{DialContext: dialer.DialContext, DisableKeepAlives: true},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > 3 || !strings.EqualFold(req.URL.Hostname(), domain) || req.URL.Scheme != "https" {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+domain+"/favicon.ico", nil)
	if err != nil {
		return ""
	}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	return iconDataURL(resp)
}

// iconDataURL turns a response into a data URL when it is a small, real picture.
func iconDataURL(resp *http.Response) string {
	kind := strings.ToLower(strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]))
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(kind, "image/") || strings.Contains(kind, "svg") {
		return ""
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, faviconMaxBytes+1))
	if err != nil || len(data) == 0 || len(data) > faviconMaxBytes {
		return ""
	}
	return "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(data)
}
