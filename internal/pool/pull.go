// Package pull fetches a signed JSON document, checks its signature against
// keys it was handed, and keeps the last good copy on disk so that the next run
// has something to work with when the fetch does not go through.
//
// THE SHAPE IT EXISTS FOR. A document this package serves changes rarely and is
// needed on every run; the source that publishes it is not always reachable.
// The whole point is that a caller can ask for the document on any run and get
// an answer: either a fresh one, or the last good one with an error saying why
// it is not fresh. The network is consulted only when the cache is older than
// the caller's patience, and every failure the source can throw — a dead
// host, a timeout, a bad signature, a rollback — falls back to the cache
// rather than leaving the caller empty.
//
// WHAT MAKES A DOCUMENT GOOD is written down in [Puller.good] and nowhere else,
// because it is the one contract a reader can spot on its own: the document is
// JSON, an object, and carries an integer "version"; its ".sig" verifies under
// one of the keys the caller trusts; and its version is not lower than the one
// the cache already holds, because a copy of yesterday served today is the one
// attack a reader can catch without trusting the source. A good document is
// cached and returned; a bad one returns whatever the cache had, together with
// the error that says why the fresh one was refused.
//
// THE CACHE IS NOT A TRUSTED STORE. Bytes in [Puller.CacheDir] that do not
// parse, or whose signature no longer verifies under Keys, are treated as if
// nothing were cached: no error of their own, no panic, and the next good
// fetch replaces them. The cache is a copy of a good fetch, never a source of
// truth in its own right.
package pull

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// MaxDoc is the largest document Pull will read. A document larger than
	// this is refused rather than read to the end, because a source that answers
	// with an unbounded body is a source that can exhaust the reader.
	MaxDoc = 4 << 20 // 4 MiB

	// DefaultBudget is what a Pull with a zero Budget spends. It is short on
	// purpose: this package serves a document that is needed on the path of a
	// keystroke, and a caller who can afford to wait longer sets its own.
	DefaultBudget = 2 * time.Second
)

// Result is what a single Pull returns. Doc is the document bytes, nil when
// there is nothing to give. Version is its "version". FromCache is true when
// the bytes came off disk, not off the wire. Changed is true when this version
// is newer than the one the cache held.
type Result struct {
	Doc       []byte
	Version   int64
	FromCache bool
	Changed   bool
}

// Puller fetches one document from one URL, verifies it against one set of
// keys, and keeps the last good copy in one directory. The zero value is not
// usable: URL, Keys and CacheDir must be set before Pull is called.
//
// Client and Now have nil defaults the package fills: a plain http.Client with
// no timeout of its own (the budget governs) and time.Now. A caller that wants
// deterministic clocks or a test transport sets both.
type Puller struct {
	URL      string
	Keys     []ed25519.PublicKey
	CacheDir string
	TTL      time.Duration
	Budget   time.Duration // 0 means DefaultBudget
	Client   *http.Client  // nil means a client the package makes
	Now      func() time.Time // nil means time.Now
}

// Verify answers whether sig is a detached ed25519 signature over doc under ANY
// ONE of keys. sig is read as base64 text in the standard encoding; space
// around it is ignored. No keys, an unreadable base64, a signature of the
// wrong length or a signature nobody's key made is false. A caller who trusts
// nobody trusts nothing.
//
// The key-length guard is here because ed25519.Verify panics on a wrong-length
// key, and a key the caller mis-hands is a misconfiguration, not a crash.
func Verify(doc, sig []byte, keys []ed25519.PublicKey) bool {
	trimmed := bytes.TrimSpace(sig)
	raw, err := base64.StdEncoding.DecodeString(string(trimmed))
	if err != nil {
		return false
	}
	if len(raw) != ed25519.SignatureSize {
		return false
	}
	for _, k := range keys {
		if len(k) != ed25519.PublicKeySize {
			continue
		}
		if ed25519.Verify(k, doc, raw) {
			return true
		}
	}
	return false
}

// Pull fetches the document and returns a Result and an error. It never
// panics. The whole call lives inside Budget (DefaultBudget when it is zero)
// and inside ctx, whichever ends first.
func (p *Puller) Pull(ctx context.Context) (Result, error) {
	if p.URL == "" {
		return Result{}, errors.New("pull: empty url")
	}
	budget := p.Budget
	if budget <= 0 {
		budget = DefaultBudget
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()

	now := p.now()
	cached, ok := p.loadCache()

	// THE NETWORK IS SKIPPED WHEN THE CACHE IS YOUNG. A good copy fetched less
	// than TTL ago answers straight from the cache, and not one request is
	// made. A TTL of zero makes no cached copy young.
	if ok && p.TTL > 0 && now.Sub(cached.fetchedAt) < p.TTL {
		return Result{
			Doc:       cached.doc,
			Version:   cached.version,
			FromCache: true,
			Changed:   false,
		}, nil
	}

	doc, sig, etag, err := p.fetch(ctx, cached)
	if err != nil {
		// EVERY FAILURE FALLS BACK. The cached good copy, if there is one, is
		// returned with the error that says why the fresh one was refused. The
		// cache is left exactly as it was.
		if ok {
			return Result{
				Doc:       cached.doc,
				Version:   cached.version,
				FromCache: true,
				Changed:   false,
			}, err
		}
		return Result{FromCache: false}, err
	}

	// WHAT MAKES A FETCHED DOCUMENT GOOD.
	version, err := p.good(doc, sig, cached)
	if err != nil {
		if ok {
			return Result{
				Doc:       cached.doc,
				Version:   cached.version,
				FromCache: true,
				Changed:   false,
			}, err
		}
		return Result{FromCache: false}, err
	}

	changed := true
	if ok && version == cached.version {
		changed = false
	}

	// A good document is cached and returned with FromCache false.
	p.storeCache(doc, sig, etag, version, now)
	return Result{
		Doc:       doc,
		Version:   version,
		FromCache: false,
		Changed:   changed,
	}, nil
}

// good answers whether the fetched bytes are a document this package will serve.
// The document must be JSON, an object, and carry an integer "version"; its
// signature must verify under one of Keys; and its version may not be lower
// than the one the cache already holds. It returns the version when it does.
func (p *Puller) good(doc, sig []byte, cached cache) (int64, error) {
	if !Verify(doc, sig, p.Keys) {
		return 0, errors.New("pull: signature does not verify")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(doc, &obj); err != nil {
		return 0, fmt.Errorf("pull: document is not json: %w", err)
	}
	if obj == nil {
		return 0, errors.New("pull: document is not a json object")
	}
	raw, has := obj["version"]
	if !has {
		return 0, errors.New("pull: document has no version")
	}
	version, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("pull: version is not an integer: %w", err)
	}
	if cached.ok && version < cached.version {
		return 0, fmt.Errorf("pull: version %d is lower than cached %d", version, cached.version)
	}
	return version, nil
}

func (p *Puller) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Puller) client() *http.Client {
	if p.Client != nil {
		return p.Client
	}
	return &http.Client{}
}

// sigURL names the signature location: the URL or path with ".sig" appended to
// its text. A URL gets ".sig" on the end of the URL; a path gets ".sig" on the
// end of the path.
func (p *Puller) sigLocation() string {
	return p.URL + ".sig"
}

// fetch reads the document and its signature from the source, and returns the
// document bytes, the raw signature bytes, the ETag (if any) and any error.
// Over http, a cached copy the source may still have unchanged is asked for
// with If-None-Match carrying the ETag that came with it; a 304 means the
// cached copy is the answer.
func (p *Puller) fetch(ctx context.Context, cached cache) ([]byte, []byte, string, error) {
	scheme := schemeOf(p.URL)
	switch scheme {
	case "http", "https":
		return p.fetchHTTP(ctx, cached)
	default:
		return p.fetchFile(ctx)
	}
}

// fetchHTTP reads the document and its signature over the network.
func (p *Puller) fetchHTTP(ctx context.Context, cached cache) ([]byte, []byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.URL, nil)
	if err != nil {
		return nil, nil, "", fmt.Errorf("pull: build request: %w", err)
	}
	if cached.ok && cached.etag != "" {
		req.Header.Set("If-None-Match", cached.etag)
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, nil, "", fmt.Errorf("pull: fetch: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		// A 304 means the cached copy is still the source's current one. We
		// answer with the cached bytes, signed by the cached signature, but
		// the caller already has those; what it needs is a nil error so the
		// good path runs. The signature is re-verified through cached.sig.
		return cached.doc, cached.sig, cached.etag, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, "", fmt.Errorf("pull: fetch: status %d", resp.StatusCode)
	}
	doc, err := readLimited(resp.Body, resp.ContentLength)
	if err != nil {
		return nil, nil, "", fmt.Errorf("pull: read document: %w", err)
	}
	sig, err := p.fetchSigHTTP(ctx)
	if err != nil {
		return nil, nil, "", err
	}
	return doc, sig, resp.Header.Get("ETag"), nil
}

func (p *Puller) fetchSigHTTP(ctx context.Context) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.sigLocation(), nil)
	if err != nil {
		return nil, fmt.Errorf("pull: build signature request: %w", err)
	}
	resp, err := p.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("pull: fetch signature: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pull: fetch signature: status %d", resp.StatusCode)
	}
	sig, err := readLimited(resp.Body, resp.ContentLength)
	if err != nil {
		return nil, fmt.Errorf("pull: read signature: %w", err)
	}
	return sig, nil
}

// fetchFile reads the document and its signature from the filesystem. A path
// may be written plain or as "file://" and then the path.
func (p *Puller) fetchFile(ctx context.Context) ([]byte, []byte, string, error) {
	path := stripFile(p.URL)
	doc, err := readLimitedFile(path)
	if err != nil {
		return nil, nil, "", fmt.Errorf("pull: read document: %w", err)
	}
	sig, err := readLimitedFile(p.sigLocation())
	if err != nil {
		return nil, nil, "", fmt.Errorf("pull: read signature: %w", err)
	}
	return doc, sig, "", nil
}

// readLimited reads from r up to MaxDoc bytes. A ContentLength above MaxDoc is
// refused before a byte is read; a body that exceeds MaxDoc mid-stream is cut
// off and refused rather than read to the end.
func readLimited(r io.Reader, contentLength int64) ([]byte, error) {
	if contentLength > MaxDoc {
		return nil, fmt.Errorf("size %d exceeds max %d", contentLength, MaxDoc)
	}
	// MaxDoc+1 so a body that fills the buffer once is caught as too large.
	limited := io.LimitReader(r, MaxDoc+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxDoc {
		return nil, fmt.Errorf("size exceeds max %d", MaxDoc)
	}
	return data, nil
}

// readLimitedFile reads a file up to MaxDoc bytes. A file larger than MaxDoc is
// refused before it is read to the end.
func readLimitedFile(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxDoc {
		return nil, fmt.Errorf("size %d exceeds max %d", info.Size(), MaxDoc)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	limited := io.LimitReader(f, MaxDoc+1)
	data, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if len(data) > MaxDoc {
		return nil, fmt.Errorf("size exceeds max %d", MaxDoc)
	}
	return data, nil
}

// schemeOf returns the scheme of a URL, or "" when the string is a plain
// filesystem path. A "file://" prefix is reported as "file".
func schemeOf(s string) string {
	if strings.HasPrefix(s, "file://") {
		return "file"
	}
	if u, err := url.Parse(s); err == nil && u.Scheme != "" {
		return u.Scheme
	}
	return ""
}

// stripFile removes a "file://" prefix, leaving the bare path.
func stripFile(s string) string {
	if strings.HasPrefix(s, "file://") {
		return strings.TrimPrefix(s, "file://")
	}
	return s
}
