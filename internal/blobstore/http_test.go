package blobstore_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/blobstore/blobstoretest"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// The wire tests use the fake sign/authenticate pair of contract §16: the
// client writes who it is into a header and the server believes it. Real
// signing is reqsign's business and is bound only in the relay.
const identityHeader = "X-Test-Device"

func fakeSign(identity string) wireauth.Sign {
	return func(r *http.Request, _ []byte) { r.Header.Set(identityHeader, identity) }
}

// fakeAuth refuses a request with no identity, and reports skew for the
// identity named "skewed" so a test can watch the client meet it.
func fakeAuth(r *http.Request, _ []byte) (string, string, error) {
	switch id := r.Header.Get(identityHeader); id {
	case "":
		return "", "", wireauth.ErrUnauthorized
	case "skewed":
		return "", "", wireauth.ErrSkew
	default:
		return id, "dev_" + id, nil
	}
}

// rig is a store server over one Memory per identity.
type rig struct {
	srv *httptest.Server
	mu  sync.Mutex
	mem map[string]*blobstore.Memory
}

func newRig(t *testing.T) *rig {
	t.Helper()
	g := &rig{mem: map[string]*blobstore.Memory{}}
	g.srv = httptest.NewServer(blobstore.Handler(fakeAuth, func(id string) (blobstore.Store, error) {
		return g.memory(id), nil
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *rig) memory(id string) *blobstore.Memory {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.mem[id] == nil {
		g.mem[id] = blobstore.NewMemory()
	}
	return g.mem[id]
}

func (g *rig) client(id string) *blobstore.HTTP {
	return blobstore.NewHTTP(g.srv.URL, fakeSign(id), g.srv.Client())
}

// failingHTTP lets the conformance suite make the server's store fail, as it
// does for the fake, by reaching through to the Memory behind the wire.
type failingHTTP struct {
	*blobstore.HTTP
	mem *blobstore.Memory
}

func (f failingHTTP) FailAfter(n int, err error) { f.mem.FailAfter(n, err) }

func TestHTTPConformance(t *testing.T) {
	blobstoretest.Run(t, func(t *testing.T) blobstore.Store {
		g := newRig(t)
		return failingHTTP{g.client("alice"), g.memory("alice")}
	})
}

func frame(t *testing.T, name string) (blobstore.Object, []byte) {
	t.Helper()
	body := append([]byte("AGEO\x01"), name...)
	rid := strings.Repeat("0", 64-len(strconv.Itoa(len(name)))) + strconv.Itoa(len(name))
	o := blobstore.Object{RID: rid, Bytes: body}
	f, err := blobstore.Encode(strings.Repeat("a", 32), []blobstore.Object{o})
	if err != nil {
		t.Fatal(err)
	}
	return o, f
}

func TestHandlerStats(t *testing.T) {
	g := newRig(t)
	c := g.client("alice")
	ctx := context.Background()
	o, f := frame(t, "one")
	_, _ = c.PutFrame(ctx, f)
	_, _ = c.PutFrame(ctx, f)
	_, _ = c.Get(ctx, o.RID)
	_, _ = c.Get(ctx, strings.Repeat("f", 64)) // a miss still counts as a request
	_, _ = c.Has(ctx, []string{o.RID})

	var want blobstore.Counts
	for _, op := range g.memory("alice").Log() {
		switch op.Kind {
		case "put":
			want.Puts++
			want.BytesIn += op.Bytes
		case "get":
			want.Gets++
			want.BytesOut += op.Bytes
		case "has":
			want.Has++
		}
	}
	got, err := c.Stats(ctx)
	if err != nil || got != want {
		t.Fatalf("stats = %+v, %v; the server's request log says %+v", got, err, want)
	}
	if other, _ := g.client("bob").Stats(ctx); other != (blobstore.Counts{}) {
		t.Fatalf("another identity sees stats %+v, want zeros", other)
	}
}

// Identity X asking for identity Y's object gets 404, not 401: the answer is
// the same as for an object that never existed, so a caller cannot learn
// whether some other identity holds a given id. A 401 would confirm it does.
func TestHandlerNamespacesByIdentity(t *testing.T) {
	g := newRig(t)
	o, f := frame(t, "secret")
	ctx := context.Background()
	if _, err := g.client("alice").PutFrame(ctx, f); err != nil {
		t.Fatal(err)
	}
	if got, err := g.client("alice").Get(ctx, o.RID); err != nil || !bytes.Equal(got, o.Bytes) {
		t.Fatalf("owner Get = %q, %v", got, err)
	}
	if _, err := g.client("bob").Get(ctx, o.RID); !errors.Is(err, blobstore.ErrNotFound) {
		t.Fatalf("other identity Get = %v, want ErrNotFound", err)
	}
	if have, _ := g.client("bob").Has(ctx, []string{o.RID}); len(have) != 1 || have[0] {
		t.Fatalf("other identity Has = %v, want [false]", have)
	}
	if code := statusOf(t, g, "bob", http.MethodGet, "/v1/store/objects/"+o.RID, nil); code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (never 401)", code)
	}
}

func statusOf(t *testing.T, g *rig, id, method, path string, body []byte) int {
	t.Helper()
	req, _ := http.NewRequest(method, g.srv.URL+path, bytes.NewReader(body))
	fakeSign(id)(req, body)
	resp, err := g.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func TestHandlerFullIs507(t *testing.T) {
	g := newRig(t)
	_, f := frame(t, "x")
	g.memory("alice").FailAfter(0, blobstore.ErrFull)
	if code := statusOf(t, g, "alice", http.MethodPost, "/v1/store/frames", f); code != http.StatusInsufficientStorage {
		t.Fatalf("status = %d, want 507", code)
	}
}

// countingBody yields n zero bytes and remembers how many were taken.
type countingBody struct{ left, taken int }

func (b *countingBody) Read(p []byte) (int, error) {
	if b.left == 0 {
		return 0, io.EOF
	}
	n := min(len(p), b.left)
	b.left -= n
	b.taken += n
	return n, nil
}

// The cap must stop the read, not follow it: with a body twice the limit, the
// handler may take at most MaxFrame+1 bytes before it answers 413.
func TestHandlerBodyLimit(t *testing.T) {
	h := blobstore.Handler(fakeAuth, func(string) (blobstore.Store, error) { return blobstore.NewMemory(), nil })
	body := &countingBody{left: 2 * blobstore.MaxFrame}
	req := httptest.NewRequest(http.MethodPost, "/v1/store/frames", body)
	fakeSign("alice")(req, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}
	if body.taken > blobstore.MaxFrame+1 {
		t.Fatalf("handler read %d bytes before refusing, limit is %d", body.taken, blobstore.MaxFrame)
	}
}

func TestHandlerNowOnEveryAnswer(t *testing.T) {
	g := newRig(t)
	for _, id := range []string{"alice", "skewed", ""} {
		req, _ := http.NewRequest(http.MethodGet, g.srv.URL+"/v1/store/stats", nil)
		if id != "" {
			fakeSign(id)(req, nil)
		}
		resp, err := g.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if _, err := strconv.ParseInt(resp.Header.Get("Codeaf-Now"), 10, 64); err != nil {
			t.Errorf("identity %q: status %d without a numeric Codeaf-Now", id, resp.StatusCode)
		}
	}
}

func TestHTTPUnreachable(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	c := blobstore.NewHTTP(dead.URL, fakeSign("alice"), dead.Client())
	dead.Close()
	if _, err := c.Get(context.Background(), strings.Repeat("a", 64)); !errors.Is(err, blobstore.ErrUnreachable) {
		t.Fatalf("Get on a closed server = %v, want ErrUnreachable", err)
	}
	for _, status := range []int{500, 502, 503} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) }))
		c := blobstore.NewHTTP(srv.URL, fakeSign("alice"), srv.Client())
		if _, err := c.Has(context.Background(), nil); !errors.Is(err, blobstore.ErrUnreachable) {
			t.Errorf("status %d: Has = %v, want ErrUnreachable", status, err)
		}
		srv.Close()
	}
}

// Skew is surfaced, never retried: the person must be told once.
func TestHTTPClientSurfacesSkew(t *testing.T) {
	var asked int
	counting := func(r *http.Request, b []byte) (string, string, error) {
		asked++
		return fakeAuth(r, b)
	}
	srv := httptest.NewServer(blobstore.Handler(counting, func(string) (blobstore.Store, error) { return blobstore.NewMemory(), nil }))
	defer srv.Close()

	c := blobstore.NewHTTP(srv.URL, fakeSign("skewed"), srv.Client())
	if _, err := c.Has(context.Background(), nil); !errors.Is(err, wireauth.ErrSkew) {
		t.Fatalf("Has = %v, want wireauth.ErrSkew", err)
	}
	if asked != 1 {
		t.Fatalf("the server was asked %d times, want 1 (no silent retry)", asked)
	}
	unsigned := blobstore.NewHTTP(srv.URL, func(*http.Request, []byte) {}, srv.Client())
	if _, err := unsigned.Has(context.Background(), nil); !errors.Is(err, wireauth.ErrUnauthorized) || errors.Is(err, wireauth.ErrSkew) {
		t.Fatalf("unsigned Has = %v, want ErrUnauthorized and not skew", err)
	}
}

// Every §2.1 error the store can raise comes out of the client as itself.
func TestHTTPErrorsRoundTrip(t *testing.T) {
	all := []error{
		blobstore.ErrNotFound, blobstore.ErrBadFrame, blobstore.ErrBadRID, blobstore.ErrConflict,
		blobstore.ErrFull, blobstore.ErrUnreachable, blobstore.ErrTooMany, blobstore.ErrDamaged,
		wireauth.ErrRateLimited, wireauth.ErrTooManyIdentities,
	}
	rid := strings.Repeat("a", 64)
	for _, want := range all {
		t.Run(want.Error(), func(t *testing.T) {
			g := newRig(t)
			g.memory("alice").FailAfter(0, want)
			if _, err := g.client("alice").Get(context.Background(), rid); !errors.Is(err, want) {
				t.Fatalf("Get = %v, want %v", err, want)
			}
		})
	}
}

// A rate limit keeps the wait the relay named, so the client can honour it.
func TestHTTPRateLimitKeepsRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "42")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"err":"rate_limited"}`))
	}))
	defer srv.Close()
	c := blobstore.NewHTTP(srv.URL, fakeSign("alice"), srv.Client())
	_, err := c.Has(context.Background(), nil)
	if !errors.Is(err, wireauth.ErrRateLimited) || wireauth.After(err) != 42*time.Second {
		t.Fatalf("Has = %v (after %v), want ErrRateLimited after 42s", err, wireauth.After(err))
	}
}

// The relay's 507 is the client's ErrFull, with no wait attached.
func TestHTTPFullIs507WithoutAWait(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInsufficientStorage)
		_, _ = w.Write([]byte(`{"err":"full"}`))
	}))
	defer srv.Close()
	c := blobstore.NewHTTP(srv.URL, fakeSign("alice"), srv.Client())
	_, err := c.Has(context.Background(), nil)
	if !errors.Is(err, blobstore.ErrFull) || wireauth.After(err) != 0 {
		t.Fatalf("Has = %v (after %v), want ErrFull and no wait", err, wireauth.After(err))
	}
}
