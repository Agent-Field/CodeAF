package github

import (
	"crypto/sha256"
	"errors"
	"net/http"
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
	k := keyOf(c.token, rawurl)
	row, known := cache.get(k)
	if known && readCacheNow().Sub(row.at) < stands {
		return row.body, row.etag, nil
	}
	body, tag, err := ask(row.etag)
	switch {
	case errors.Is(err, ErrNotModified) && known:
		cache.put(k, row)
		return row.body, row.etag, nil
	case err == nil && len(body) <= readCacheBody && (tag != "" || stands > 0):
		cache.put(k, readRow{body: body, etag: tag})
	case isStatusErr(err, http.StatusUnauthorized):
		cache.forgetToken(c.token)
	}
	return body, tag, err
}

func isStatusErr(err error, status int) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == status
}
