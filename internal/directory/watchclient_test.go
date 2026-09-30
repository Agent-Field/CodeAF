package directory_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/dirwatch"
)

// signedHeaders are the four headers a signed request carries.
var signedHeaders = []string{"Codeaf-Identity", "Codeaf-Cert", "Codeaf-Time", "Codeaf-Sig"}

// stampAll stands in for the signer: it sets the four headers and notes the body.
func stampAll(r *http.Request, _ []byte) {
	for _, h := range signedHeaders {
		r.Header.Set(h, "x")
	}
}

func watchClient(srv *httptest.Server) *directory.HTTP {
	return directory.NewHTTP(srv.URL, stampAll, srv.Client())
}

// serve runs handler as the watch route and returns the client dialled at it.
func serve(t *testing.T, handler http.HandlerFunc) *directory.HTTP {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return watchClient(srv)
}

func accept(t *testing.T, w http.ResponseWriter, r *http.Request) *websocket.Conn {
	t.Helper()
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		t.Errorf("accept: %v", err)
	}
	return c
}

func TestWatchUpgradeIsSigned(t *testing.T) {
	seen := make(chan *http.Request, 1)
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		seen <- r
		conn := accept(t, w, r)
		conn.Close(websocket.StatusNormalClosure, "")
	})
	s, err := c.Watch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	r := <-seen
	if r.URL.Path != "/v1/dir/watch" {
		t.Fatalf("path %q", r.URL.Path)
	}
	for _, h := range signedHeaders {
		if r.Header.Get(h) == "" {
			t.Errorf("missing %s", h)
		}
	}
	if b, _ := io.ReadAll(r.Body); len(b) != 0 {
		t.Errorf("body %q", b)
	}
}

func TestWatchDecodesVersionAndPong(t *testing.T) {
	got := make(chan string, 1)
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		conn := accept(t, w, r)
		ctx := r.Context()
		conn.Write(ctx, websocket.MessageText, []byte(`{"v":7,"extra":true}`))
		_, m, _ := conn.Read(ctx)
		got <- string(m)
		conn.Write(ctx, websocket.MessageText, []byte("pong"))
		conn.Read(ctx)
	})
	ctx := context.Background()
	s, err := c.Watch(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if f, err := s.Next(ctx); err != nil || f.Version != 7 || f.Pong {
		t.Fatalf("frame %+v, %v", f, err)
	}
	if err := s.Ping(ctx); err != nil {
		t.Fatal(err)
	}
	if m := <-got; m != "ping" {
		t.Fatalf("server read %q", m)
	}
	if f, err := s.Next(ctx); err != nil || !f.Pong {
		t.Fatalf("frame %+v, %v", f, err)
	}
}

func TestWatchUndecodableFrameIsAnError(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		conn := accept(t, w, r)
		conn.Write(r.Context(), websocket.MessageText, []byte("nonsense"))
		conn.Read(r.Context())
	})
	s, err := c.Watch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err := s.Next(context.Background()); err == nil {
		t.Fatal("want an error")
	}
}

func TestWatchRefusedUpgrade(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"no route", 404, "", dirwatch.ErrNoRoute},
		{"revoked", 401, `{"err":"revoked"}`, dirwatch.ErrRevoked},
		{"revoked keeps the directory sentinel", 401, `{"err":"revoked"}`, directory.ErrRevoked},
		{"rotated", 410, `{"err":"rotated"}`, dirwatch.ErrRotated},
		{"rotated keeps the directory sentinel", 410, `{"err":"rotated"}`, directory.ErrRotated},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := serve(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				io.Copy(w, strings.NewReader(tc.body))
			})
			if _, err := c.Watch(context.Background()); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want %v", err, tc.want)
			}
		})
	}
}

func TestWatchUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	c := watchClient(srv)
	srv.Close()
	if _, err := c.Watch(context.Background()); !errors.Is(err, directory.ErrUnreachable) {
		t.Fatalf("got %v", err)
	}
}

func TestWatchCloseCodesAreSentinels(t *testing.T) {
	for code, want := range map[websocket.StatusCode]error{
		dirwatch.CloseRevoked: dirwatch.ErrRevoked,
		dirwatch.CloseRotated: dirwatch.ErrRotated,
	} {
		c := serve(t, func(w http.ResponseWriter, r *http.Request) {
			accept(t, w, r).Close(code, "")
		})
		s, err := c.Watch(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.Next(context.Background()); !errors.Is(err, want) {
			t.Errorf("close %d: got %v, want %v", code, err, want)
		}
		s.Close()
	}
}

func TestListReadsTheVersionHeader(t *testing.T) {
	for header, want := range map[string]uint64{"42": 42, "": 0, "soon": 0} {
		c := serve(t, func(w http.ResponseWriter, r *http.Request) {
			if header != "" {
				w.Header().Set(directory.VersionHeader, header)
			}
			io.WriteString(w, `{}`)
		})
		if l, err := c.List(context.Background()); err != nil || l.Version != want {
			t.Errorf("header %q: version %d, %v; want %d", header, l.Version, err, want)
		}
	}
}
