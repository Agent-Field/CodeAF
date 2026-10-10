package desktopbridge

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"
)

const webFaviconTTL = time.Hour

// webIconCaches keeps one hour-long answer per host on each bridge. The cache
// is not a field of Bridge because the route table that mounts webFavicon is
// owned by a later integration; this file has to stand on its own until then.
var webIconCaches sync.Map

// webLookupIP and webDial are the resolution and connect steps behind the
// public-address rule. Tests replace them. Production always checks the
// resolved addresses before webDial, and webDial checks the socket again.
var (
	webLookupIP = defaultWebLookupIP
	webDial     = defaultWebDial
)

// A web favicon is the icon for a page the person opened, not for a host the
// conversation happened to mention. GET /favicon?url= answers {dataUrl} when a
// small public image was fetched, and {} when it was not, so the tab draws its
// monogram. The integrator mounts this on ServeHTTP after the bridge token
// check; the handler checks the token itself so the file is safe to call first.
func (b *Bridge) webFavicon(w http.ResponseWriter, r *http.Request) {
	if !webFaviconToken(b, r) {
		fail(w, http.StatusUnauthorized, "engine connection required")
		return
	}
	if !needGet(w, r) {
		return
	}
	host, ok := webFaviconHost(r.URL.Query().Get("url"))
	if !ok {
		write(w, map[string]string{})
		return
	}
	if dataURL := b.webIconCache().lookup(host, r.URL.Query().Get("url")); dataURL != "" {
		write(w, map[string]string{"dataUrl": dataURL})
		return
	}
	write(w, map[string]string{})
}

func webFaviconToken(b *Bridge, r *http.Request) bool {
	if b == nil || b.token == "" {
		return false
	}
	got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	return subtle.ConstantTimeCompare([]byte(got), []byte(b.token)) == 1
}

func (b *Bridge) webIconCache() *webIconCache {
	if v, ok := webIconCaches.Load(b); ok {
		return v.(*webIconCache)
	}
	c := newWebIconCache()
	actual, loaded := webIconCaches.LoadOrStore(b, c)
	if loaded {
		return actual.(*webIconCache)
	}
	return c
}

type webIconHit struct {
	data string
	at   time.Time
}

type webIconCache struct {
	mu      sync.Mutex
	items   map[string]webIconHit
	now     func() time.Time
	ttl     time.Duration
	timeout time.Duration
	// client replaces the public dialer in tests. Production leaves it nil.
	client *http.Client
	fetch  func(ctx context.Context, raw string, client *http.Client) string
}

func newWebIconCache() *webIconCache {
	return &webIconCache{
		items:   map[string]webIconHit{},
		now:     time.Now,
		ttl:     webFaviconTTL,
		timeout: faviconTimeout,
		fetch:   fetchWebFavicon,
	}
}

func (c *webIconCache) lookup(host, raw string) string {
	c.mu.Lock()
	if e, ok := c.items[host]; ok && c.now().Before(e.at.Add(c.ttl)) {
		data := e.data
		c.mu.Unlock()
		return data
	}
	c.mu.Unlock()
	timeout := c.timeout
	if timeout <= 0 {
		timeout = faviconTimeout
	}
	client := c.client
	if client == nil {
		client = webIconClient(timeout)
	}
	fetch := c.fetch
	if fetch == nil {
		fetch = fetchWebFavicon
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	got := fetch(ctx, raw, client)
	c.mu.Lock()
	c.items[host] = webIconHit{data: got, at: c.now()}
	c.mu.Unlock()
	return got
}

func webFaviconHost(raw string) (string, bool) {
	u, ok := parseWebURL(raw)
	if !ok {
		return "", false
	}
	return canonicalHost(u.Hostname()), true
}

func canonicalHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
}

func parseWebURL(raw string) (*url.URL, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil {
		return nil, false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, false
	}
	host := canonicalHost(u.Hostname())
	if host == "" || len(host) > 253 {
		return nil, false
	}
	return u, true
}

// fetchWebFavicon tries https://host/favicon.ico first. Only a miss continues
// to the page, and then only far enough to read the head.
func fetchWebFavicon(ctx context.Context, raw string, client *http.Client) string {
	u, ok := parseWebURL(raw)
	if !ok || client == nil {
		return ""
	}
	host := canonicalHost(u.Hostname())
	if data := getIcon(ctx, client, faviconIcoURL(host)); data != "" {
		return data
	}
	if ctx.Err() != nil {
		return ""
	}
	return iconFromPage(ctx, client, raw)
}

func faviconIcoURL(host string) string {
	if strings.Contains(host, ":") {
		return "https://[" + host + "]/favicon.ico"
	}
	return "https://" + host + "/favicon.ico"
}

func getIcon(ctx context.Context, client *http.Client, raw string) string {
	if _, ok := parseWebURL(raw); !ok {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "codeaf")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	return iconDataURL(resp)
}

// iconFromPage reads the document head only. A linked icon is accepted after
// HEAD says it is a small image; the GET that follows still applies the same
// byte cap, so a lying Content-Length cannot land a larger picture.
func iconFromPage(ctx context.Context, client *http.Client, pageURL string) string {
	head, base := readHTMLHead(ctx, client, pageURL)
	if head == "" || ctx.Err() != nil {
		return ""
	}
	ref := firstIconRef(head, base)
	if ref == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(ref), "data:") {
		return dataIcon(ref)
	}
	if !headAllowsIcon(ctx, client, ref) {
		return ""
	}
	return getIcon(ctx, client, ref)
}

func readHTMLHead(ctx context.Context, client *http.Client, pageURL string) (string, string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return "", ""
	}
	req.Header.Set("User-Agent", "codeaf")
	resp, err := client.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ""
	}
	kind := mediaType(resp)
	if kind != "" && !strings.Contains(kind, "html") && !strings.Contains(kind, "xml") {
		return "", ""
	}
	// Stop at the head. The rest of a page is not an icon and is not read.
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 512)
	for len(buf) < faviconMaxBytes {
		n, err := resp.Body.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if bytes.Contains(bytes.ToLower(buf), []byte("</head>")) {
				break
			}
		}
		if err != nil {
			break
		}
	}
	base := pageURL
	if resp.Request != nil && resp.Request.URL != nil {
		base = resp.Request.URL.String()
	}
	return string(buf), base
}

func headAllowsIcon(ctx context.Context, client *http.Client, raw string) bool {
	if _, ok := parseWebURL(raw); !ok {
		return false
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, raw, nil)
	if err != nil {
		return false
	}
	req.Header.Set("User-Agent", "codeaf")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	// Some sites have no HEAD. The GET cap still applies.
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		return true
	}
	if resp.StatusCode != http.StatusOK {
		return false
	}
	if resp.ContentLength > faviconMaxBytes {
		return false
	}
	kind := mediaType(resp)
	if kind == "" {
		return true
	}
	return strings.HasPrefix(kind, "image/") && !strings.Contains(kind, "svg")
}

func mediaType(resp *http.Response) string {
	return strings.ToLower(strings.TrimSpace(strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]))
}

func firstIconRef(head, base string) string {
	s := head
	for {
		low := strings.ToLower(s)
		i := strings.Index(low, "<link")
		if i < 0 {
			return ""
		}
		s = s[i:]
		end := tagEnd(s)
		if end < 0 {
			return ""
		}
		tag := s[:end+1]
		s = s[end+1:]
		if !relIncludesIcon(tag) {
			continue
		}
		href := strings.TrimSpace(htmlAttr(tag, "href"))
		if href == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(href), "data:") {
			return href
		}
		abs, ok := resolveIconRef(base, href)
		if !ok {
			continue
		}
		return abs
	}
}

func relIncludesIcon(tag string) bool {
	for _, tok := range strings.Fields(strings.ToLower(htmlAttr(tag, "rel"))) {
		if tok == "icon" {
			return true
		}
	}
	return false
}

func resolveIconRef(base, href string) (string, bool) {
	bu, err := url.Parse(base)
	if err != nil {
		return "", false
	}
	ref, err := url.Parse(href)
	if err != nil {
		return "", false
	}
	abs := bu.ResolveReference(ref)
	if _, ok := parseWebURL(abs.String()); !ok {
		return "", false
	}
	return abs.String(), true
}

func tagEnd(s string) int {
	quote := byte(0)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '"' || c == '\'' {
			quote = c
			continue
		}
		if c == '>' {
			return i
		}
	}
	return -1
}

func htmlAttr(tag, name string) string {
	low := strings.ToLower(tag)
	key := strings.ToLower(name) + "="
	i := 0
	for i < len(low) {
		j := strings.Index(low[i:], key)
		if j < 0 {
			return ""
		}
		j += i
		if j > 0 {
			prev := low[j-1]
			if prev != ' ' && prev != '\t' && prev != '\n' && prev != '\r' && prev != '/' && prev != '<' {
				i = j + len(key)
				continue
			}
		}
		rest := tag[j+len(key):]
		if rest == "" {
			return ""
		}
		switch rest[0] {
		case '"', '\'':
			q := rest[0]
			rest = rest[1:]
			k := strings.IndexByte(rest, q)
			if k < 0 {
				return ""
			}
			return rest[:k]
		default:
			k := strings.IndexAny(rest, " \t\n\r>")
			if k < 0 {
				return rest
			}
			return rest[:k]
		}
	}
	return ""
}

// dataIcon keeps an inline image only when it is a non-SVG picture inside the
// same byte cap as a fetched file. The bytes already arrived in the head.
func dataIcon(href string) string {
	comma := strings.Index(href, ",")
	if comma < 0 {
		return ""
	}
	meta := strings.ToLower(href[:comma])
	if !strings.HasPrefix(meta, "data:image/") || strings.Contains(meta, "svg") {
		return ""
	}
	payload := href[comma+1:]
	var raw []byte
	var err error
	if strings.Contains(meta, ";base64") {
		raw, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			raw, err = base64.RawStdEncoding.DecodeString(payload)
		}
	} else {
		var decoded string
		decoded, err = url.PathUnescape(payload)
		raw = []byte(decoded)
	}
	if err != nil || len(raw) == 0 || len(raw) > faviconMaxBytes {
		return ""
	}
	kind := strings.TrimPrefix(strings.Split(meta, ";")[0], "data:")
	return "data:" + kind + ";base64," + base64.StdEncoding.EncodeToString(raw)
}

func webIconClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		CheckRedirect: webIconRedirect,
		Transport: &http.Transport{
			// A proxy would connect to the target for us and skip the dialer
			// check. This fetch never uses the environment proxy.
			Proxy:                  nil,
			DialContext:            dialWebIcon,
			DisableKeepAlives:      true,
			ResponseHeaderTimeout:  timeout,
			TLSHandshakeTimeout:    timeout,
			MaxResponseHeaderBytes: faviconMaxBytes,
			ForceAttemptHTTP2:      false,
		},
	}
}

func webIconRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > 3 || req.URL == nil || len(via) == 0 || via[0].URL == nil {
		return http.ErrUseLastResponse
	}
	if req.URL.User != nil || (req.URL.Scheme != "http" && req.URL.Scheme != "https") {
		return http.ErrUseLastResponse
	}
	// A redirect onto another host is a different site. The dialer would still
	// refuse a private address, and we do not follow the redirect either.
	if !strings.EqualFold(canonicalHost(req.URL.Hostname()), canonicalHost(via[0].URL.Hostname())) {
		return http.ErrUseLastResponse
	}
	return nil
}

func defaultWebLookupIP(ctx context.Context, host string) ([]netip.Addr, error) {
	host = canonicalHost(host)
	if ip, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{ip}, nil
	}
	return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
}

func defaultWebDial(ctx context.Context, network, address string) (net.Conn, error) {
	d := &net.Dialer{Timeout: faviconTimeout, Control: publicOnly}
	return d.DialContext(ctx, network, address)
}

// dialWebIcon resolves the name, refuses the whole answer when any address is
// not public, and dials the address it checked. Checking the name alone would
// miss a public name that resolves inward.
func dialWebIcon(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := webLookupIP(ctx, host)
	if err != nil || len(ips) == 0 {
		if err == nil {
			err = errors.New("address is not public")
		}
		return nil, err
	}
	for _, ip := range ips {
		if err := refusePrivate(ip, port); err != nil {
			return nil, err
		}
	}
	return webDial(ctx, network, net.JoinHostPort(ips[0].String(), port))
}

func refusePrivate(ip netip.Addr, port string) error {
	if ip.Is4In6() {
		ip = ip.Unmap()
	}
	return publicOnly("tcp", net.JoinHostPort(ip.String(), port), nil)
}
