package pairbox

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// fencing is a relay that names each opening of one nameplate, as the hosted
// relay does, and answers 404 gone to a caller that carries another opening.
type fencing struct {
	mu   sync.Mutex
	gen  string
	seen []string // the generation each request carried, "" for none
}

func (f *fencing) reopen(gen string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gen = gen
}

func (f *fencing) carried() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seen...)
}

func (f *fencing) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	carried := r.Header.Get(GenHeader)
	f.seen = append(f.seen, carried)
	if r.Method == http.MethodPost && r.URL.Path == Path {
		w.Header().Set(GenHeader, f.gen)
		reply(w, http.StatusCreated, createdBody{Nameplate: "42", ExpiresMS: 1000})
		return
	}
	if carried != "" && carried != f.gen {
		fail(w, ErrGone)
		return
	}
	w.Header().Set(GenHeader, f.gen)
	w.WriteHeader(http.StatusNoContent)
}

func fencedClient(t *testing.T) (*HTTP, *fencing) {
	t.Helper()
	relay := &fencing{gen: "first"}
	srv := httptest.NewServer(relay)
	t.Cleanup(srv.Close)
	return NewHTTP(srv.URL, srv.Client()), relay
}

func poll(h *HTTP) error {
	_, err := h.Poll(context.Background(), "42", SideB, 0, 0)
	return err
}

func TestCreatorCarriesTheOpeningItWasTold(t *testing.T) {
	h, relay := fencedClient(t)
	key, _ := NewKey()
	if _, err := h.Create(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if err := poll(h); err != nil {
		t.Fatal(err)
	}
	if got := relay.carried(); len(got) != 2 || got[0] != "" || got[1] != "first" {
		t.Fatalf("the create and the poll carried %q, want none and the opening it was told", got)
	}
}

func TestPollOfAnEarlierOpeningIsGoneAndIsForgotten(t *testing.T) {
	h, relay := fencedClient(t)
	key, _ := NewKey()
	if _, err := h.Create(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	relay.reopen("second") // the plate is drawn again for someone else
	if err := poll(h); !errors.Is(err, ErrGone) {
		t.Fatalf("poll of the earlier opening = %v, want ErrGone", err)
	}
	if h.opening("42") != "" {
		t.Error("the client still holds the opening of a mailbox that is gone")
	}
}

func TestJoinerLearnsTheOpeningFromItsFirstAnswer(t *testing.T) {
	h, relay := fencedClient(t)
	if err := poll(h); err != nil {
		t.Fatal(err)
	}
	relay.reopen("second")
	if err := poll(h); !errors.Is(err, ErrGone) {
		t.Fatalf("a joiner's poll after the plate was reused = %v, want ErrGone", err)
	}
	if got := relay.carried(); got[0] != "" || got[1] != "first" {
		t.Fatalf("the joiner carried %q, want none and then the opening it learned", got)
	}
}

// A relay that never names an opening, as the self-hosted one does not, is
// served as before: the client sends no header and nothing is fenced.
func TestRelayWithoutOpeningsIsServedAsBefore(t *testing.T) {
	var mu sync.Mutex
	var carried []string
	inner := Handler(NewMemory(DefaultLimits, nil), SocketPeer)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		carried = append(carried, r.Header.Get(GenHeader))
		mu.Unlock()
		inner.ServeHTTP(w, r)
	}))
	defer srv.Close()
	h, ctx := NewHTTP(srv.URL, srv.Client()), context.Background()
	key, _ := NewKey()
	made, err := h.Create(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.Post(ctx, made.Nameplate, SideA, key, []byte("hi")); err != nil {
		t.Fatal(err)
	}
	got, err := h.Poll(ctx, made.Nameplate, SideA, 0, time.Millisecond)
	if err != nil || len(got.Msgs) != 1 {
		t.Fatalf("poll = %v, %v", got, err)
	}
	if err := h.Delete(ctx, made.Nameplate, key); err != nil {
		t.Fatal(err)
	}
	for _, c := range carried {
		if c != "" {
			t.Fatalf("a relay that names no opening was sent %q", c)
		}
	}
}
