package blobstore_test

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// pathCounter records the path of every request that leaves the client, so a
// test can say what the relay was asked and, as importantly, that it was not.
type pathCounter struct {
	inner http.RoundTripper
	mu    sync.Mutex
	paths []string
}

func (p *pathCounter) RoundTrip(r *http.Request) (*http.Response, error) {
	p.mu.Lock()
	p.paths = append(p.paths, r.URL.Path)
	p.mu.Unlock()
	return p.inner.RoundTrip(r)
}

func (p *pathCounter) seen() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.paths...)
}

// C5.2: while the store may not be written, the question that only ever comes
// before an upload is not worth a request, so Has asks nothing and answers what
// a put would answer.
func TestHasAsksNothingWhileStorageIsOff(t *testing.T) {
	g := newRig(t)
	counter := &pathCounter{inner: g.srv.Client().Transport}
	if counter.inner == nil {
		counter.inner = http.DefaultTransport
	}
	c := blobstore.NewHTTP(g.srv.URL, fakeSign("alice"), &http.Client{Transport: counter})
	o, f := frame(t, "held")
	if _, err := c.PutFrame(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	before := len(counter.seen())

	// With the gate open, or no gate at all, Has answers as it always did.
	for _, gate := range []func() bool{nil, func() bool { return true }} {
		c.CanStore = gate
		have, err := c.Has(context.Background(), []string{o.RID})
		if err != nil || len(have) != 1 || !have[0] {
			t.Fatalf("Has with storage allowed = %v, %v; want [true]", have, err)
		}
	}
	if got := len(counter.seen()) - before; got != 2 {
		t.Fatalf("Has with storage allowed made %d requests, want 2 (one each)", got)
	}

	// With the gate shut, Has asks nothing and says storage is off.
	c.CanStore = func() bool { return false }
	before = len(counter.seen())
	have, err := c.Has(context.Background(), []string{o.RID})
	if !errors.Is(err, wireauth.ErrStorageOff) || have != nil {
		t.Fatalf("Has with storage off = %v, %v; want nil and ErrStorageOff", have, err)
	}
	if got := counter.seen()[before:]; len(got) != 0 {
		t.Fatalf("Has with storage off asked the relay: %v", got)
	}

	// Reads stay open: earlier frames remain readable when storage closes, and a
	// take reads them. Only the pre-upload question is closed.
	if got, err := c.Get(context.Background(), o.RID); err != nil || string(got) != string(o.Bytes) {
		t.Fatalf("Get with storage off = %q, %v; reads must still work", got, err)
	}
	if at, err := c.Locate(context.Background(), []string{o.RID}); err != nil || len(at) != 1 {
		t.Fatalf("Locate with storage off = %v, %v; reads must still work", at, err)
	}
}

// A store says whether it may take new objects without asking anyone, so a
// caller can refuse work whose last step is an upload before it begins.
func TestStoringAnswersFromTheLocalGate(t *testing.T) {
	c := blobstore.NewHTTP("http://127.0.0.1:1", fakeSign("alice"), nil)
	if !c.Storing() {
		t.Fatal("a store with no gate may store")
	}
	c.CanStore = func() bool { return false }
	if c.Storing() {
		t.Fatal("a store whose gate says no may not store")
	}
	c.CanStore = func() bool { return true }
	if !c.Storing() {
		t.Fatal("a store whose gate says yes may store")
	}
}
