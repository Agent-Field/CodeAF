package directory_test

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/directory/directorytest"
	"github.com/Agent-Field/codeaf/internal/wireauth"
)

const (
	devA  = "dev_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	devB  = "dev_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	cellX = "01J0000000000000000000000A"
	head1 = "1111111111111111111111111111111111111111111111111111111111111111"
	head2 = "2222222222222222222222222222222222222222222222222222222222222222"
)

// The fake pair stands in for the signing package: sign names the sender in
// two headers and authenticate reads them back.
func fakeSign(identity, device string) wireauth.Sign {
	return func(r *http.Request, _ []byte) {
		r.Header.Set("X-Test-Identity", identity)
		r.Header.Set("X-Test-Device", device)
	}
}

func fakeAuth(r *http.Request, _ []byte) (string, string, error) {
	id, dev := r.Header.Get("X-Test-Identity"), r.Header.Get("X-Test-Device")
	if id == "" || dev == "" {
		return "", "", wireauth.ErrUnauthorized
	}
	return id, dev, nil
}

// namespaces opens one Memory per identity, as the relay opens one per namespace.
type namespaces struct {
	mu    sync.Mutex
	clock *directorytest.FakeClock
	dirs  map[string]*directory.Memory
}

func newNamespaces(clock *directorytest.FakeClock) *namespaces {
	return &namespaces{clock: clock, dirs: map[string]*directory.Memory{}}
}

func (n *namespaces) open(identity string) (directory.Directory, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.dirs[identity] == nil {
		n.dirs[identity] = directory.NewMemory(n.clock.Now)
	}
	return n.dirs[identity], nil
}

type rig struct {
	srv   *httptest.Server
	clock *directorytest.FakeClock
}

func newRig(t *testing.T, clock *directorytest.FakeClock, auth wireauth.Authenticate) rig {
	t.Helper()
	srv := httptest.NewServer(directory.Handler(auth, newNamespaces(clock).open))
	t.Cleanup(srv.Close)
	return rig{srv: srv, clock: clock}
}

func (g rig) as(identity, device string) directory.Client {
	return directory.NewHTTP(g.srv.URL, fakeSign(identity, device), g.srv.Client())
}

func TestHTTPConformance(t *testing.T) {
	directorytest.Run(t, func(t *testing.T, clock *directorytest.FakeClock) directorytest.Devices {
		g := newRig(t, clock, fakeAuth)
		return func(device string) directory.Client { return g.as("id_one", device) }
	})
}

func TestHandlerTakesDeviceFromSignature(t *testing.T) {
	g := newRig(t, directorytest.NewFakeClock(), fakeAuth)
	ctx := context.Background()
	a, b := g.as("id_one", devA), g.as("id_one", devB)
	if _, err := a.Create(ctx, cellX, directory.CellInit{Head: head1, Class: "chat"}); err != nil {
		t.Fatal(err)
	}

	// B holds A's fence and even smuggles a device into the body, yet acts as B.
	_, err := b.Heartbeat(ctx, cellX, directory.Beat{Fence: 1})
	if !errors.Is(err, directory.ErrFenceStale) {
		t.Fatalf("B heartbeat with A's fence = %v", err)
	}
	req, _ := http.NewRequest("POST", g.srv.URL+"/v1/dir/cells/"+cellX+"/heartbeat",
		strings.NewReader(`{"fence":1,"device":"`+devA+`"}`))
	fakeSign("id_one", devB)(req, nil)
	resp, err := g.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("smuggled device: status %d", resp.StatusCode)
	}
}

func TestHandlerRefusesAnotherDevicesRecord(t *testing.T) {
	g := newRig(t, directorytest.NewFakeClock(), fakeAuth)
	ctx := context.Background()
	a, b := g.as("id_one", devA), g.as("id_one", devB)

	err := a.PutDevice(ctx, devB, directory.Device{V: 1, Name: "forged"})
	if !errors.Is(err, directory.ErrUnauthorized) {
		t.Fatalf("A writing B's record = %v, want ErrUnauthorized", err)
	}
	l, err := b.List(ctx)
	if err != nil || len(l.Devices) != 0 {
		t.Fatalf("list = %+v, %v; nothing may have been written", l, err)
	}
}

func TestHTTPNamespacesAreIsolated(t *testing.T) {
	g := newRig(t, directorytest.NewFakeClock(), fakeAuth)
	ctx := context.Background()
	x, y := g.as("id_x", devA), g.as("id_y", devA)
	if _, err := x.Create(ctx, cellX, directory.CellInit{Head: head1, Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	if _, err := y.Cell(ctx, cellX); !errors.Is(err, directory.ErrNotFound) {
		t.Fatalf("other identity sees the cell: %v", err)
	}
	if _, err := y.Create(ctx, cellX, directory.CellInit{Head: head2, Class: "chat"}); err != nil {
		t.Fatalf("same id in another namespace: %v", err)
	}
	l, _ := x.List(ctx)
	if l.Cells[cellX].Head != head1 {
		t.Fatalf("x's cell changed: %+v", l.Cells[cellX])
	}
}

// failing is a Directory whose every write returns err.
type failing struct{ err error }

func (f failing) For(string) directory.Client { return failClient{err: f.err} }

type failClient struct {
	directory.Client
	err error
}

func (f failClient) Acquire(context.Context, string) (directory.CellView, error) {
	return directory.CellView{}, f.err
}

func TestHTTPErrorsRoundTrip(t *testing.T) {
	for _, want := range []error{
		directory.ErrNotFound, directory.ErrExists, directory.ErrLeaseHeld,
		directory.ErrFenceStale, directory.ErrHeadMoved, directory.ErrCAS,
		directory.ErrUnauthorized, directory.ErrRevoked,
	} {
		t.Run(want.Error(), func(t *testing.T) {
			srv := httptest.NewServer(directory.Handler(fakeAuth, func(string) (directory.Directory, error) {
				return failing{want}, nil
			}))
			defer srv.Close()
			c := directory.NewHTTP(srv.URL, fakeSign("id_one", devA), srv.Client())
			if _, err := c.Acquire(context.Background(), cellX); !errors.Is(err, want) {
				t.Fatalf("got %v, want %v", err, want)
			}
		})
	}
}

func TestHandlerUnknownErrorIsServerFault(t *testing.T) {
	srv := httptest.NewServer(directory.Handler(fakeAuth, func(string) (directory.Directory, error) {
		return failing{errors.New("disk on fire")}, nil
	}))
	defer srv.Close()
	c := directory.NewHTTP(srv.URL, fakeSign("id_one", devA), srv.Client())
	_, err := c.Acquire(context.Background(), cellX)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v, want a 500", err)
	}
}

func TestHTTPClientSurfacesSkew(t *testing.T) {
	skewed := func(*http.Request, []byte) (string, string, error) { return "", "", wireauth.ErrSkew }
	g := newRig(t, directorytest.NewFakeClock(), skewed)
	_, err := g.as("id_one", devA).List(context.Background())
	if !errors.Is(err, wireauth.ErrSkew) {
		t.Fatalf("err = %v, want wireauth.ErrSkew", err)
	}
}

func TestHTTPRefusalWithoutProofIsUnauthorized(t *testing.T) {
	g := newRig(t, directorytest.NewFakeClock(), fakeAuth)
	c := directory.NewHTTP(g.srv.URL, func(*http.Request, []byte) {}, g.srv.Client())
	if _, err := c.List(context.Background()); !errors.Is(err, directory.ErrUnauthorized) {
		t.Fatalf("err = %v, want ErrUnauthorized", err)
	}
}

func TestHTTPTransportFailureIsUnreachable(t *testing.T) {
	g := newRig(t, directorytest.NewFakeClock(), fakeAuth)
	c := g.as("id_one", devA)
	g.srv.Close()
	if _, err := c.List(context.Background()); !errors.Is(err, directory.ErrUnreachable) {
		t.Fatalf("err = %v, want ErrUnreachable", err)
	}
}

func TestHandlerLimitsBody(t *testing.T) {
	g := newRig(t, directorytest.NewFakeClock(), fakeAuth)
	big := bytes.Repeat([]byte("a"), directory.MaxBody+1)
	req, _ := http.NewRequest("POST", g.srv.URL+"/v1/dir/cells/"+cellX, bytes.NewReader(big))
	fakeSign("id_one", devA)(req, big)
	resp, err := g.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", resp.StatusCode)
	}
	if _, err := g.as("id_one", devA).Cell(context.Background(), cellX); !errors.Is(err, directory.ErrNotFound) {
		t.Fatalf("an oversized create must leave no cell: %v", err)
	}
}

func TestHandlerRejectsMalformedBody(t *testing.T) {
	g := newRig(t, directorytest.NewFakeClock(), fakeAuth)
	req, _ := http.NewRequest("POST", g.srv.URL+"/v1/dir/cells/"+cellX, strings.NewReader("{nope"))
	fakeSign("id_one", devA)(req, nil)
	resp, err := g.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestReplayedPublishIsRefusedByFence(t *testing.T) {
	g := newRig(t, directorytest.NewFakeClock(), fakeAuth)
	ctx := context.Background()
	a, b := g.as("id_one", devA), g.as("id_one", devB)
	if _, err := a.Create(ctx, cellX, directory.CellInit{Head: head1, Class: "chat"}); err != nil {
		t.Fatal(err)
	}
	g.clock.Advance(directory.LeaseTTL + 1)
	if _, err := b.Acquire(ctx, cellX); err != nil {
		t.Fatal(err)
	}
	// A replays the publish it sealed before B took over; the fence refuses it.
	_, err := a.Publish(ctx, cellX, directory.Publish{Fence: 1, OldHead: head1, Head: head2, Class: "chat"})
	if !errors.Is(err, directory.ErrFenceStale) {
		t.Fatalf("replayed publish = %v, want ErrFenceStale", err)
	}
}
