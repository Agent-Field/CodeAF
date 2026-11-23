package github

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// identityFresh is how long who a token belongs to stands before GET /user
	// is asked again. A refusal of the token on any read forgets it sooner.
	identityFresh = 10 * time.Minute
	// readCacheRows bounds the answers kept, readCacheBody the size of one,
	// and readCacheBytes their sum; past a bound the cache starts over.
	readCacheRows  = 512
	readCacheBody  = 1 << 20
	readCacheBytes = 32 << 20
)

// readCacheNow is the cache's clock, so a test can hold it still.
var readCacheNow = time.Now

// cache is every client's memory of what GitHub last answered a read, in
// this process. An idle factory asked who its token is on nearly every
// request it served, and read lists that had not changed in full every
// minute. Who the token is now stands for [identityFresh]; any other kept
// answer is asked again with If-None-Match, and a 304, which GitHub does not
// count against the hourly limit, answers what was kept.
var cache = &readCache{rows: map[readKey]readRow{}, unreadable: map[readKey]time.Time{}}

// readKey is one URL as one token sees it; the token is kept only as a hash.
type readKey struct {
	token [32]byte
	url   string
}

type readRow struct {
	body []byte
	etag string
	at   time.Time
}

type readCache struct {
	mu    sync.Mutex
	rows  map[readKey]readRow
	bytes int
	// unreadable is when a token was last told it may not read a branch
	// policy on this plan ([PolicyUnavailable]), an answer that stands like
	// identity: the factory and its runner asked about it every minute.
	unreadable map[readKey]time.Time
}

// policyUnreadable says whether the token was told, less than
// [identityFresh] ago, that it may not read the policy at url.
func (r *readCache) policyUnreadable(token, url string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	at, ok := r.unreadable[keyOf(token, url)]
	return ok && readCacheNow().Sub(at) < identityFresh
}

func (r *readCache) notePolicyUnreadable(token, url string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.unreadable) >= readCacheRows {
		clear(r.unreadable)
	}
	r.unreadable[keyOf(token, url)] = readCacheNow()
}

func keyOf(token, url string) readKey { return readKey{sha256.Sum256([]byte(token)), url} }

func (r *readCache) get(k readKey) (readRow, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	row, ok := r.rows[k]
	return row, ok
}

func (r *readCache) put(k readKey, row readRow) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.rows) >= readCacheRows || r.bytes+len(row.body) > readCacheBytes {
		clear(r.rows)
		r.bytes = 0
	}
	row.at = readCacheNow()
	r.bytes += len(row.body) - len(r.rows[k].body)
	r.rows[k] = row
}

// forgetToken drops everything a token was told, once GitHub refused it.
func (r *readCache) forgetToken(token string) {
	h := sha256.Sum256([]byte(token))
	r.mu.Lock()
	defer r.mu.Unlock()
	for k := range r.rows {
		if k.token == h {
			r.bytes -= len(r.rows[k].body)
			delete(r.rows, k)
		}
	}
	for k := range r.unreadable {
		if k.token == h {
			delete(r.unreadable, k)
		}
	}
}

// cachedRead is [client.read] for a caller that holds no tag of its own: the
// last answer while it stands (zero for most reads), else a conditional ask
// that a 304 turns into that answer, else a fresh ask whose answer is kept.
func (c *client) cachedRead(rawurl string, stands time.Duration, ask func(etag string) ([]byte, string, error)) ([]byte, string, error) {
	body, tag, _, err := c.cachedReadChanged(rawurl, stands, ask)
	return body, tag, err
}

func (c *client) cachedReadChanged(rawurl string, stands time.Duration, ask func(string) ([]byte, string, error)) ([]byte, string, bool, error) {
	k := keyOf(c.token, rawurl)
	row, known := cache.get(k)
	if known && readCacheNow().Sub(row.at) < stands {
		return row.body, row.etag, false, nil
	}
	body, tag, err := ask(row.etag)
	switch {
	case errors.Is(err, ErrNotModified) && known:
		cache.put(k, row)
		return row.body, row.etag, false, nil
	case err == nil && len(body) <= readCacheBody && (tag != "" || stands > 0):
		cache.put(k, readRow{body: body, etag: tag})
	case isStatusErr(err, http.StatusUnauthorized):
		cache.forgetToken(c.token)
	}
	return body, tag, true, err
}

func isStatusErr(err error, status int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == status
}

// ReadHTTP reuses the bounded process read cache for the serving routes that
// need HTTP status and headers. Revision identifies immutable content already
// authorized by a fresh parent read (for example a pull request head SHA).
// Changed is false only when the body was reused, including a GitHub 304.
func ReadHTTP(client *http.Client, req *http.Request, revision string) (*http.Response, bool, error) {
	token := strings.TrimPrefix(req.Header.Get("Authorization"), "Bearer ")
	rawURL := req.URL.String()
	if revision != "" {
		rawURL += "#" + revision
	}
	key := keyOf(token, rawURL)
	row, known := cache.get(key)
	if known && revision != "" {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(row.body))}, false, nil
	}
	if known && row.etag != "" {
		req.Header.Set("If-None-Match", row.etag)
	}
	resp, err := client.Do(req)
	if err != nil {
		return resp, false, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		cache.forgetToken(token)
	}
	if resp.StatusCode == http.StatusNotModified && known {
		resp.Body.Close()
		resp.StatusCode = http.StatusOK
		resp.Body = io.NopCloser(bytes.NewReader(row.body))
		return resp, false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return resp, true, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, readCacheBody+1))
	if err != nil {
		resp.Body.Close()
		return nil, false, err
	}
	// Oversize bodies remain streamed to the caller's own bound, but are not kept.
	resp.Body = struct {
		io.Reader
		io.Closer
	}{io.MultiReader(bytes.NewReader(body), resp.Body), resp.Body}
	if len(body) <= readCacheBody && json.Valid(body) && (revision != "" || resp.Header.Get("ETag") != "") {
		cache.put(key, readRow{body: body, etag: resp.Header.Get("ETag")})
	}
	return resp, true, nil
}
