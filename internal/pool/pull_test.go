package pull

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// makeKey makes a fresh ed25519 key pair.
func makeKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

// signDoc signs doc with priv and returns base64-encoded standard encoding.
func signDoc(t *testing.T, priv ed25519.PrivateKey, doc []byte) []byte {
	t.Helper()
	sig := ed25519.Sign(priv, doc)
	return []byte(base64.StdEncoding.EncodeToString(sig))
}

// jsonObject builds a JSON object with a version field and whatever extra
// fields are given.
func jsonObject(version int64, extra map[string]any) []byte {
	obj := map[string]any{"version": version}
	for k, v := range extra {
		obj[k] = v
	}
	b, _ := json.Marshal(obj)
	return b
}

// writeFile is the test shorthand that makes a 0600 file.
func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// -----------------------------------------------------------------------------

// Verify reads a base64 standard-encoding signature and answers whether it
// is a detached ed25519 signature over doc under any one of the keys. No keys,
// unreadable base64, the wrong length, or a signature nobody's key made is
// false.
func TestVerifyAnswersTrueForAnyTrustedKey(t *testing.T) {
	pub1, priv1 := makeKey(t)
	pub2, priv2 := makeKey(t)
	doc := []byte(`{"version":3}`)
	sig1 := signDoc(t, priv1, doc)
	sig2 := signDoc(t, priv2, doc)

	if !Verify(doc, sig1, []ed25519.PublicKey{pub1, pub2}) {
		t.Fatal("signature by key 1 did not verify")
	}
	if !Verify(doc, sig2, []ed25519.PublicKey{pub1, pub2}) {
		t.Fatal("signature by key 2 did not verify")
	}
}

// A caller who trusts nobody trusts nothing.
func TestVerifyWithNoKeysIsFalse(t *testing.T) {
	_, priv := makeKey(t)
	doc := []byte(`{"version":1}`)
	sig := signDoc(t, priv, doc)
	if Verify(doc, sig, nil) {
		t.Fatal("a signature verified with no keys")
	}
}

// Unreadable base64 is false, not an error.
func TestVerifyWithGarbageSignatureIsFalse(t *testing.T) {
	pub, _ := makeKey(t)
	doc := []byte(`{"version":1}`)
	if Verify(doc, []byte("!!!not-base64!!!"), []ed25519.PublicKey{pub}) {
		t.Fatal("garbage verified")
	}
}

// A signature of the wrong length is false.
func TestVerifyWithWrongLengthSignatureIsFalse(t *testing.T) {
	pub, _ := makeKey(t)
	doc := []byte(`{"version":1}`)
	short := base64.StdEncoding.EncodeToString([]byte("too short"))
	if Verify(doc, []byte(short), []ed25519.PublicKey{pub}) {
		t.Fatal("a short signature verified")
	}
}

// Space around the signature is ignored.
func TestVerifyIgnoresSpaceAroundSignature(t *testing.T) {
	pub, priv := makeKey(t)
	doc := []byte(`{"version":1}`)
	sig := signDoc(t, priv, doc)
	padded := []byte("  \n" + string(sig) + "\t ")
	if !Verify(doc, padded, []ed25519.PublicKey{pub}) {
		t.Fatal("padded signature did not verify")
	}
}

// A signature over the wrong bytes is false.
func TestVerifyWithWrongMessageIsFalse(t *testing.T) {
	pub, priv := makeKey(t)
	sig := signDoc(t, priv, []byte(`{"version":1}`))
	if Verify([]byte(`{"version":2}`), sig, []ed25519.PublicKey{pub}) {
		t.Fatal("a signature verified over the wrong message")
	}
}

// A wrong-length key is skipped rather than crashing the caller.
func TestVerifySkipsWrongLengthKeys(t *testing.T) {
	pub, priv := makeKey(t)
	doc := []byte(`{"version":1}`)
	sig := signDoc(t, priv, doc)
	bad := ed25519.PublicKey(make([]byte, 10)) // not PublicKeySize
	if !Verify(doc, sig, []ed25519.PublicKey{bad, pub}) {
		t.Fatal("a good key behind a bad one did not verify")
	}
}

// -----------------------------------------------------------------------------

// A good file-source fetch returns the document with FromCache false and
// Changed true when nothing was cached.
func TestPullFromFileReturnsFreshDocument(t *testing.T) {
	pub, priv := makeKey(t)
	dir := t.TempDir()
	doc := jsonObject(5, nil)
	sig := signDoc(t, priv, doc)
	writeFile(t, filepath.Join(dir, "doc.json"), doc)
	writeFile(t, filepath.Join(dir, "doc.json.sig"), sig)

	p := &Puller{
		URL:      filepath.Join(dir, "doc.json"),
		Keys:     []ed25519.PublicKey{pub},
		CacheDir: t.TempDir(),
	}
	res, err := p.Pull(t.Context())
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}
	if res.FromCache {
		t.Fatal("a fresh fetch reported FromCache")
	}
	if !res.Changed {
		t.Fatal("a first fetch reported Changed false")
	}
	if res.Version != 5 {
		t.Fatalf("Version = %d, want 5", res.Version)
	}
	if !bytesEqual(res.Doc, doc) {
		t.Fatal("Doc does not match the source")
	}
}

// A file:// URL is the same fetch as a plain path.
func TestPullFromFileURLScheme(t *testing.T) {
	pub, priv := makeKey(t)
	dir := t.TempDir()
	doc := jsonObject(1, nil)
	sig := signDoc(t, priv, doc)
	path := filepath.Join(dir, "doc.json")
	writeFile(t, path, doc)
	writeFile(t, path+".sig", sig)

	p := &Puller{
		URL:      "file://" + path,
		Keys:     []ed25519.PublicKey{pub},
		CacheDir: t.TempDir(),
	}
	res, err := p.Pull(t.Context())
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}
	if res.Version != 1 {
		t.Fatalf("Version = %d, want 1", res.Version)
	}
}

// -----------------------------------------------------------------------------

// A fetchServer serves a document and its signature and remembers how many
// times each was asked for.
type fetchServer struct {
	t         *testing.T
	pub       ed25519.PublicKey
	priv      ed25519.PrivateKey
	docBody   []byte
	sigBody   []byte
	etag      string
	docHits   int
	sigHits   int
	statusDoc int
	statusSig int
	delay     time.Duration
	modified  bool
}

func newFetchServer(t *testing.T) (*fetchServer, *httptest.Server) {
	t.Helper()
	pub, priv := makeKey(t)
	fs := &fetchServer{
		t:         t,
		pub:       pub,
		priv:      priv,
		docBody:   jsonObject(1, nil),
		statusDoc: http.StatusOK,
		statusSig: http.StatusOK,
		etag:      "v1",
	}
	fs.sigBody = signDoc(t, priv, fs.docBody)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fs.delay > 0 {
			time.Sleep(fs.delay)
		}
		if strings.HasSuffix(r.URL.Path, ".sig") {
			fs.sigHits++
			w.Header().Set("Content-Type", "text/plain")
			if fs.statusSig != http.StatusOK {
				w.WriteHeader(fs.statusSig)
				return
			}
			_, _ = w.Write(fs.sigBody)
			return
		}
		fs.docHits++
		if r.Header.Get("If-None-Match") == fs.etag && !fs.modified {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", fs.etag)
		if fs.statusDoc != http.StatusOK {
			w.WriteHeader(fs.statusDoc)
			return
		}
		_, _ = w.Write(fs.docBody)
	}))
	t.Cleanup(srv.Close)
	return fs, srv
}

func (fs *fetchServer) setVersion(v int64) {
	fs.docBody = jsonObject(v, nil)
	fs.sigBody = signDoc(fs.t, fs.priv, fs.docBody)
	fs.etag = "v" + strconv.FormatInt(v, 10)
	fs.modified = true
}

// -----------------------------------------------------------------------------

// A good http fetch returns the document fresh.
func TestPullOverHTTPReturnsFreshDocument(t *testing.T) {
	fs, srv := newFetchServer(t)
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: t.TempDir(),
	}
	res, err := p.Pull(t.Context())
	if err != nil {
		t.Fatalf("Pull failed: %v", err)
	}
	if res.FromCache {
		t.Fatal("fresh fetch reported FromCache")
	}
	if !res.Changed {
		t.Fatal("first fetch reported Changed false")
	}
	if fs.docHits != 1 {
		t.Fatalf("doc requested %d times, want 1", fs.docHits)
	}
}

// THE NETWORK IS SKIPPED WHEN THE CACHE IS YOUNG. A good copy cached less than
// TTL ago answers from the cache, and not one request is made.
func TestCacheYoungSkipsNetworkEntirely(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	clock := time.Now()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
		TTL:      10 * time.Minute,
		Now:      func() time.Time { return clock },
	}
	// First fetch hits the network and caches.
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fs.docHits != 1 {
		t.Fatalf("first pull hit the doc %d times, want 1", fs.docHits)
	}
	// Second fetch is inside the TTL: no request at all.
	clock = clock.Add(5 * time.Minute)
	res, err := p.Pull(t.Context())
	if err != nil {
		t.Fatalf("second pull failed: %v", err)
	}
	if !res.FromCache {
		t.Fatal("a fetch inside TTL did not come from cache")
	}
	if res.Changed {
		t.Fatal("a fetch inside TTL reported Changed")
	}
	if fs.docHits != 1 {
		t.Fatalf("second pull hit the doc %d times, want 1", fs.docHits)
	}
}

// A TTL of zero makes no cached copy young: the source is always consulted.
func TestZeroTTLAlwaysConsultsSource(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
		TTL:      0,
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	if fs.docHits != 2 {
		t.Fatalf("two pulls with TTL 0 hit the doc %d times, want 2", fs.docHits)
	}
}

// -----------------------------------------------------------------------------

// Over http, a cached copy the source still has unchanged is asked for with
// If-None-Match; a 304 means the cached copy is the answer.
func TestHTTP304MeansCachedCopyIsAnswer(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
		TTL:      0, // force a network consult
	}
	// First fetch caches with an ETag.
	first, err := p.Pull(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if first.Changed != true {
		t.Fatal("first fetch should be Changed")
	}
	// Second fetch: the source answers 304 for the same ETag.
	fs.modified = false
	res, err := p.Pull(t.Context())
	if err != nil {
		t.Fatalf("second pull failed: %v", err)
	}
	if !res.FromCache {
		t.Fatal("a 304 did not produce FromCache")
	}
	if res.Changed {
		t.Fatal("a 304 reported Changed")
	}
	if fs.docHits != 2 {
		t.Fatalf("doc requested %d times, want 2 (both the GET and the 304)", fs.docHits)
	}
}

// A higher version is Changed true; an equal version is Changed false.
func TestChangedFlagReflectsVersion(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
		TTL:      0,
	}
	if res, _ := p.Pull(t.Context()); !res.Changed {
		t.Fatal("first fetch should be Changed")
	}
	// Same version again: Changed false, still a fetch (not 304, no etag match
	// because modified flag default makes the server return 200).
	fs.modified = false
	if res, _ := p.Pull(t.Context()); res.Changed {
		t.Fatal("same version reported Changed")
	}
	// Higher version: Changed true.
	fs.setVersion(2)
	fs.modified = true
	res, _ := p.Pull(t.Context())
	if !res.Changed {
		t.Fatal("higher version reported Changed false")
	}
	if res.Version != 2 {
		t.Fatalf("Version = %d, want 2", res.Version)
	}
}

// -----------------------------------------------------------------------------

// A document larger than MaxDoc is refused rather than read to the end.
func TestDocumentLargerThanMaxDocIsRefused(t *testing.T) {
	// Override MaxDoc locally by testing the reader directly, since the real
	// MaxDoc is 4 MiB and writing 5 MiB of test data is wasteful.
	data := make([]byte, MaxDoc+1)
	for i := range data {
		data[i] = 'a'
	}
	_, err := readLimited(strings.NewReader(string(data)), int64(len(data)))
	if err == nil {
		t.Fatal("a document larger than MaxDoc was read without error")
	}
	if !strings.Contains(err.Error(), "exceeds max") {
		t.Fatalf("error does not name the size: %v", err)
	}
}

// ContentLength above MaxDoc is refused before a byte is read.
func TestContentLengthAboveMaxDocIsRefusedBeforeRead(t *testing.T) {
	called := false
	r := readerFunc{read: func(b []byte) (int, error) {
		called = true
		return 0, nil
	}}
	_, err := readLimited(r, int64(MaxDoc+1))
	if err == nil {
		t.Fatal("a ContentLength above MaxDoc was not refused")
	}
	if called {
		t.Fatal("the body was read despite ContentLength above MaxDoc")
	}
}

type readerFunc struct {
	read func([]byte) (int, error)
}

func (r readerFunc) Read(b []byte) (int, error) { return r.read(b) }

// -----------------------------------------------------------------------------

// A bad signature falls back to the cache.
func TestBadSignatureFallsBackToCache(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
		TTL:      0,
	}
	// First fetch caches.
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Now serve a document signed by a different key.
	_, otherPriv := makeKey(t)
	fs.docBody = jsonObject(2, nil)
	fs.sigBody = signDoc(t, otherPriv, fs.docBody)
	fs.modified = true
	res, err := p.Pull(t.Context())
	if err == nil {
		t.Fatal("a bad signature returned no error")
	}
	if !res.FromCache {
		t.Fatal("a bad signature did not fall back to cache")
	}
	if res.Version != 1 {
		t.Fatalf("fallback Version = %d, want 1", res.Version)
	}
}

// A rollback — a version lower than the cached one — falls back to the cache.
func TestRollbackFallsBackToCache(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
		TTL:      0,
	}
	// Cache version 5.
	fs.setVersion(5)
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Serve version 3, still signed correctly.
	fs.setVersion(3)
	fs.modified = true
	res, err := p.Pull(t.Context())
	if err == nil {
		t.Fatal("a rollback returned no error")
	}
	if !res.FromCache {
		t.Fatal("a rollback did not fall back to cache")
	}
	if res.Version != 5 {
		t.Fatalf("fallback Version = %d, want 5", res.Version)
	}
}

// A dead source falls back to the cache.
func TestDeadSourceFallsBackToCache(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
		TTL:      0,
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Kill the server.
	srv.Close()
	res, err := p.Pull(t.Context())
	if err == nil {
		t.Fatal("a dead source returned no error")
	}
	if !res.FromCache {
		t.Fatal("a dead source did not fall back to cache")
	}
}

// With no cached copy, a dead source returns a non-nil error and Doc nil.
func TestDeadSourceWithNoCacheReturnsErrorAndNilDoc(t *testing.T) {
	pub, _ := makeKey(t)
	p := &Puller{
		URL:      "http://127.0.0.1:1/doc.json", // nothing listening
		Keys:     []ed25519.PublicKey{pub},
		CacheDir: t.TempDir(),
		Budget:   200 * time.Millisecond,
	}
	res, err := p.Pull(t.Context())
	if err == nil {
		t.Fatal("a dead source with no cache returned no error")
	}
	if res.Doc != nil {
		t.Fatal("Doc should be nil with no cache")
	}
	if res.FromCache {
		t.Fatal("FromCache should be false with no cache")
	}
}

// A document that is not JSON falls back to the cache.
func TestNonJSONDocumentFallsBackToCache(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
		TTL:      0,
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Serve non-JSON but sign it so it gets past the signature check.
	bad := []byte("not json at all")
	fs.docBody = bad
	fs.sigBody = signDoc(t, fs.priv, bad)
	fs.modified = true
	res, err := p.Pull(t.Context())
	if err == nil {
		t.Fatal("non-JSON returned no error")
	}
	if !res.FromCache {
		t.Fatal("non-JSON did not fall back to cache")
	}
}

// A document that is not an object (an array) falls back to the cache.
func TestNonObjectDocumentFallsBackToCache(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
		TTL:      0,
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	arr := []byte(`[1,2,3]`)
	fs.docBody = arr
	fs.sigBody = signDoc(t, fs.priv, arr)
	fs.modified = true
	res, err := p.Pull(t.Context())
	if err == nil {
		t.Fatal("non-object returned no error")
	}
	if !res.FromCache {
		t.Fatal("non-object did not fall back to cache")
	}
}

// A document with no version falls back to the cache.
func TestDocumentWithNoVersionFallsBackToCache(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
		TTL:      0,
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	obj := []byte(`{"notversion":1}`)
	fs.docBody = obj
	fs.sigBody = signDoc(t, fs.priv, obj)
	fs.modified = true
	res, err := p.Pull(t.Context())
	if err == nil {
		t.Fatal("no-version returned no error")
	}
	if !res.FromCache {
		t.Fatal("no-version did not fall back to cache")
	}
}

// A document with a non-integer version falls back to the cache.
func TestDocumentWithNonIntegerVersionFallsBackToCache(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
		TTL:      0,
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	obj := []byte(`{"version":"five"}`)
	fs.docBody = obj
	fs.sigBody = signDoc(t, fs.priv, obj)
	fs.modified = true
	res, err := p.Pull(t.Context())
	if err == nil {
		t.Fatal("non-integer version returned no error")
	}
	if !res.FromCache {
		t.Fatal("non-integer version did not fall back to cache")
	}
}

// Extra top-level fields are none of this package's business.
func TestExtraFieldsArePassedThrough(t *testing.T) {
	fs, srv := newFetchServer(t)
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: t.TempDir(),
	}
	extra := map[string]any{"min_client": "1.0", "submit": "https://example.com"}
	fs.docBody = jsonObject(1, extra)
	fs.sigBody = signDoc(t, fs.priv, fs.docBody)
	fs.modified = true
	res, err := p.Pull(t.Context())
	if err != nil {
		t.Fatalf("extra fields caused an error: %v", err)
	}
	// The bytes handed back carry the extra fields.
	var got map[string]any
	if err := json.Unmarshal(res.Doc, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["min_client"]; !ok {
		t.Fatal("extra field min_client was stripped")
	}
}

// -----------------------------------------------------------------------------

// The cache lives in CacheDir and is written atomically.
func TestGoodFetchIsCachedOnDisk(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	doc, err := os.ReadFile(filepath.Join(cacheDir, "doc.json"))
	if err != nil {
		t.Fatalf("doc.json not cached: %v", err)
	}
	if !bytesEqual(doc, fs.docBody) {
		t.Fatal("cached doc does not match source")
	}
	if _, err := os.ReadFile(filepath.Join(cacheDir, "doc.sig")); err != nil {
		t.Fatalf("doc.sig not cached: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join(cacheDir, "meta.json")); err != nil {
		t.Fatalf("meta.json not cached: %v", err)
	}
}

// Cached files have mode 0600.
func TestCacheFilesHaveMode0600(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"doc.json", "doc.sig", "meta.json"} {
		info, err := os.Stat(filepath.Join(cacheDir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s has mode %o, want 0600", name, info.Mode().Perm())
		}
	}
}

// CacheDir is made if it is missing.
func TestCacheDirIsMadeIfMissing(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := filepath.Join(t.TempDir(), "nested", "cache")
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "doc.json")); err != nil {
		t.Fatalf("nested CacheDir was not made: %v", err)
	}
}

// A cache that does not read is no cache: corrupt bytes are treated as nothing
// cached, and the next good fetch replaces them.
func TestCorruptCacheIsTreatedAsEmpty(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	// Write garbage into the cache.
	writeFile(t, filepath.Join(cacheDir, "doc.json"), []byte("garbage"))
	writeFile(t, filepath.Join(cacheDir, "doc.sig"), []byte("garbage"))
	writeFile(t, filepath.Join(cacheDir, "meta.json"), []byte("{}"))
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
	}
	res, err := p.Pull(t.Context())
	if err != nil {
		t.Fatalf("a corrupt cache should not surface its own error: %v", err)
	}
	if res.FromCache {
		t.Fatal("a corrupt cache was served as a cache hit")
	}
	if !res.Changed {
		t.Fatal("replacing a corrupt cache should be Changed")
	}
}

// A cache whose signature no longer verifies under the current keys is no cache.
func TestCacheWithUntrustedSignatureIsTreatedAsEmpty(t *testing.T) {
	_, oldPriv := makeKey(t)
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	// Cache a document signed by the old key.
	doc := jsonObject(1, nil)
	sig := signDoc(t, oldPriv, doc)
	writeFile(t, filepath.Join(cacheDir, "doc.json"), doc)
	writeFile(t, filepath.Join(cacheDir, "doc.sig"), sig)
	writeFile(t, filepath.Join(cacheDir, "meta.json"), []byte(`{"version":1,"fetched_at":"2020-01-01T00:00:00Z"}`))
	// Pull with a different (untrusted) key set — the old signature must not
	// verify, and the source is fetched fresh.
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
	}
	res, err := p.Pull(t.Context())
	if err != nil {
		t.Fatalf("untrusted cache should fall through silently: %v", err)
	}
	if res.FromCache {
		t.Fatal("an untrusted cache was served")
	}
}

// -----------------------------------------------------------------------------

// An empty URL is an error.
func TestEmptyURLError(t *testing.T) {
	p := &Puller{CacheDir: t.TempDir()}
	_, err := p.Pull(t.Context())
	if err == nil {
		t.Fatal("empty URL returned no error")
	}
}

// The whole call lives inside Budget.
func TestBudgetTimesOut(t *testing.T) {
	fs, srv := newFetchServer(t)
	fs.delay = 500 * time.Millisecond
	p := &Puller{
		URL:    srv.URL + "/doc.json",
		Keys:   []ed25519.PublicKey{fs.pub},
		Budget: 100 * time.Millisecond,
		// no CacheDir so there is nothing to fall back to
	}
	_, err := p.Pull(t.Context())
	if err == nil {
		t.Fatal("a call over budget returned no error")
	}
}

// The call also lives inside ctx, whichever ends first.
func TestContextCancelEndsCall(t *testing.T) {
	fs, srv := newFetchServer(t)
	fs.delay = 500 * time.Millisecond
	ctx, cancel := contextWithTimeout(50 * time.Millisecond)
	defer cancel()
	p := &Puller{
		URL:    srv.URL + "/doc.json",
		Keys:   []ed25519.PublicKey{fs.pub},
		Budget: 10 * time.Second,
	}
	_, err := p.Pull(ctx)
	if err == nil {
		t.Fatal("a cancelled context returned no error")
	}
}

// Pull never panics, even on a wrong-length key.
func TestPullNeverPanicsOnBadKey(t *testing.T) {
	pub, priv := makeKey(t)
	dir := t.TempDir()
	doc := jsonObject(1, nil)
	sig := signDoc(t, priv, doc)
	writeFile(t, filepath.Join(dir, "doc.json"), doc)
	writeFile(t, filepath.Join(dir, "doc.json.sig"), sig)
	bad := ed25519.PublicKey(make([]byte, 5))
	p := &Puller{
		URL:      filepath.Join(dir, "doc.json"),
		Keys:     []ed25519.PublicKey{bad, pub},
		CacheDir: t.TempDir(),
	}
	// Must not panic; the bad key is skipped and the good one verifies.
	res, err := p.Pull(t.Context())
	if err != nil {
		t.Fatalf("Pull with a bad key failed: %v", err)
	}
	if res.Version != 1 {
		t.Fatalf("Version = %d, want 1", res.Version)
	}
}

// -----------------------------------------------------------------------------

// A 304 with no cached copy is a failure: the source claimed not-modified for
// something we never had.
func Test304WithNoCacheIsAnError(t *testing.T) {
	pub, priv := makeKey(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sig") {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(signDoc(t, priv, []byte{}))
			return
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	t.Cleanup(srv.Close)
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{pub},
		CacheDir: t.TempDir(),
	}
	_, err := p.Pull(t.Context())
	if err == nil {
		t.Fatal("a 304 with no cache returned no error")
	}
}

// A non-200 status from the source falls back to the cache.
func TestNon200StatusFallsBackToCache(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
		TTL:      0,
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	fs.statusDoc = http.StatusInternalServerError
	fs.modified = true
	res, err := p.Pull(t.Context())
	if err == nil {
		t.Fatal("a 500 returned no error")
	}
	if !res.FromCache {
		t.Fatal("a 500 did not fall back to cache")
	}
}

// A signature fetch that fails falls back to the cache.
func TestSignatureFetchFailureFallsBackToCache(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
		TTL:      0,
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	fs.setVersion(2)
	fs.modified = true
	fs.statusSig = http.StatusNotFound
	res, err := p.Pull(t.Context())
	if err == nil {
		t.Fatal("a missing signature returned no error")
	}
	if !res.FromCache {
		t.Fatal("a missing signature did not fall back to cache")
	}
}

// The cache stores the ETag and uses it on the next fetch.
func TestCacheStoresETag(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
		TTL:      0,
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	meta, err := os.ReadFile(filepath.Join(cacheDir, "meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(meta), "v1") {
		t.Fatalf("meta.json does not contain the etag: %s", meta)
	}
}

// No temp files are left behind after a good cache write.
func TestAtomicWriteLeavesNoTempFiles(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}

// A cache whose meta.json is missing is treated as empty.
func TestCacheWithMissingMetaIsTreatedAsEmpty(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	// Write doc and sig but no meta.
	doc := jsonObject(1, nil)
	sig := signDoc(t, fs.priv, doc)
	writeFile(t, filepath.Join(cacheDir, "doc.json"), doc)
	writeFile(t, filepath.Join(cacheDir, "doc.sig"), sig)
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
	}
	res, err := p.Pull(t.Context())
	if err != nil {
		t.Fatalf("missing meta surfaced an error: %v", err)
	}
	if res.FromCache {
		t.Fatal("a cache missing meta.json was served from cache")
	}
}

// A file source whose document is missing returns an error with no cache.
func TestFileSourceMissingReturnsError(t *testing.T) {
	pub, _ := makeKey(t)
	p := &Puller{
		URL:      filepath.Join(t.TempDir(), "nope.json"),
		Keys:     []ed25519.PublicKey{pub},
		CacheDir: t.TempDir(),
	}
	_, err := p.Pull(t.Context())
	if err == nil {
		t.Fatal("a missing file source returned no error")
	}
}

// A file source falls back to cache when the file goes away.
func TestFileSourceFallsBackWhenFileDisappears(t *testing.T) {
	pub, priv := makeKey(t)
	dir := t.TempDir()
	doc := jsonObject(3, nil)
	sig := signDoc(t, priv, doc)
	path := filepath.Join(dir, "doc.json")
	writeFile(t, path, doc)
	writeFile(t, path+".sig", sig)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      path,
		Keys:     []ed25519.PublicKey{pub},
		CacheDir: cacheDir,
		TTL:      0,
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	os.Remove(path)
	res, err := p.Pull(t.Context())
	if err == nil {
		t.Fatal("a missing file returned no error")
	}
	if !res.FromCache {
		t.Fatal("a missing file did not fall back to cache")
	}
	if res.Version != 3 {
		t.Fatalf("fallback Version = %d, want 3", res.Version)
	}
}

// A version equal to the cached one is good and Changed is false.
func TestEqualVersionIsGoodAndNotChanged(t *testing.T) {
	fs, srv := newFetchServer(t)
	cacheDir := t.TempDir()
	p := &Puller{
		URL:      srv.URL + "/doc.json",
		Keys:     []ed25519.PublicKey{fs.pub},
		CacheDir: cacheDir,
		TTL:      0,
	}
	if _, err := p.Pull(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Serve the same version again with a fresh body (different object, same
	// version number) so the 304 path is not taken.
	fs.docBody = jsonObject(1, map[string]any{"note": "refreshed"})
	fs.sigBody = signDoc(t, fs.priv, fs.docBody)
	fs.modified = true
	res, err := p.Pull(t.Context())
	if err != nil {
		t.Fatalf("equal version should be good: %v", err)
	}
	if res.Changed {
		t.Fatal("equal version reported Changed true")
	}
}

// -----------------------------------------------------------------------------

// helpers

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// contextWithTimeout makes a context with a timeout, because t.Context() does
// not take one.
func contextWithTimeout(d time.Duration) (context.Context, func()) {
	return context.WithTimeout(context.Background(), d)
}
