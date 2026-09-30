package relayserve

import (
	"context"
	"io"
	"net/http"
	"sync/atomic"

	"github.com/Agent-Field/codeaf/internal/wireauth"
)

// WHAT THE RELAY LOGS, AND WHY THAT LIST IS SHORT. A line carries the first
// eight characters of the verified identity id, the route pattern (the verb
// with its placeholders, never the values that filled them), the status code
// and two byte counts. There is no argument in this file that could carry a
// body, a header value or a path parameter, so none can be logged by accident.

// call is what one request leaves behind for its log line.
type call struct {
	identity atomic.Value // string: set by the authenticator once the sender is proven
	in, out  atomic.Int64
	status   int
}

type callKey struct{}

// noting wraps an authenticator so a verified identity is recorded for the
// log line. Only a proven id is ever recorded; a refused caller logs as "-".
func noting(auth wireauth.Authenticate) wireauth.Authenticate {
	return func(r *http.Request, body []byte) (string, string, error) {
		id, dev, err := auth(r, body)
		if c, ok := r.Context().Value(callKey{}).(*call); ok && err == nil {
			c.identity.Store(id)
		}
		return id, dev, err
	}
}

// recorder counts what a handler writes and remembers the status it chose.
type recorder struct {
	http.ResponseWriter
	c *call
}

// Unwrap lets the upgrade to a WebSocket reach the real connection.
func (w *recorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *recorder) WriteHeader(status int) {
	w.c.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *recorder) Write(b []byte) (int, error) {
	if w.c.status == 0 {
		w.c.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.c.out.Add(int64(n))
	return n, err
}

// counted counts the request body bytes the handler actually read.
type counted struct {
	body io.ReadCloser
	n    *atomic.Int64
}

func (b counted) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	b.n.Add(int64(n))
	return n, err
}
func (b counted) Close() error { return b.body.Close() }

// logged wraps a handler so each answer leaves one line through logf.
func logged(logf func(string, ...any), next http.Handler) http.Handler {
	if logf == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := &call{}
		r = r.WithContext(context.WithValue(r.Context(), callKey{}, c))
		r.Body = counted{body: r.Body, n: &c.in}
		next.ServeHTTP(&recorder{ResponseWriter: w, c: c}, r)
		logf("%s %s %d in=%d out=%d", who(c), pattern(r), c.status, c.in.Load(), c.out.Load())
	})
}

func who(c *call) string {
	id, _ := c.identity.Load().(string)
	if len(id) < 11 { // "id_" plus eight characters
		return "-"
	}
	return id[:11]
}

func pattern(r *http.Request) string {
	if r.Pattern == "" {
		return "unrouted"
	}
	return r.Pattern
}
