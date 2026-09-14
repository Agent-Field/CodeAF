package provider

import (
	"crypto/tls"
	"net/http/httptrace"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/calllog"
)

// ── WHAT THE CONNECTION ITSELF COST ─────────────────────────────────────────
//
// A COLD POOL AND A SLOW MODEL LOOK THE SAME IN `ttft_ms`, and this file is
// what tells them apart. The wait a person sees before the first word includes
// everything the transport had to do before a byte of the request left this
// machine: resolve openrouter.ai, open the socket, finish the handshake. On a
// warm pool that is nothing at all. On a cold one it is a hundred to four
// hundred milliseconds, every attempt, and for ten days the log recorded it as
// the endpoint being slow.
//
// IT IS FREE. `net/http/httptrace` is a set of function pointers the transport
// already carries a nil check for; the hooks below record instants and do no
// work. The one cost is a mutex, and it is there because the hooks do NOT all
// run on the caller's goroutine — a dial and its handshake run on the
// transport's own, and Happy Eyeballs may run two at once — while the row that
// reads them is often written by the stream loop on a third.

// connFacts is one attempt's connection, filed under the attempt it belongs to.
//
// It is per-ATTEMPT rather than per-call for [callTrace.attemptID]'s reason: a
// call that was paced and sent again opened its second connection under
// different conditions, and a row about attempt two carrying attempt one's
// handshake would be a measurement of the wrong socket.
type connFacts struct {
	mu sync.Mutex
	// id is the attempt these facts are about. A closing row is written about
	// an attempt that has already been overtaken, so the row and the facts are
	// paired by this rather than by whichever attempt is current.
	id string
	// seen is whether the transport ever reached a connection at all. A request
	// refused before the dial — a context already expired, a limiter that would
	// not let go — has nothing to say and says nothing.
	seen   bool
	reused bool

	dnsBegan, connectBegan, tlsBegan time.Time
	dns, connect, handshake          time.Duration
}

// newConnFacts opens the measurement for one attempt.
func newConnFacts(attemptID string) *connFacts { return &connFacts{id: attemptID} }

// hooks is the trace this attempt rides. sent is the caller's own flag for
// whether the request bytes ever left — it is a separate fact from the
// connection and is kept where it was, on the one hook that answers it.
func (f *connFacts) hooks(sent *atomic.Bool) *httptrace.ClientTrace {
	return &httptrace.ClientTrace{
		WroteRequest: func(httptrace.WroteRequestInfo) { sent.Store(true) },
		DNSStart: func(httptrace.DNSStartInfo) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.dnsBegan = time.Now()
		},
		DNSDone: func(httptrace.DNSDoneInfo) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if !f.dnsBegan.IsZero() {
				f.dns = time.Since(f.dnsBegan)
			}
		},
		ConnectStart: func(string, string) {
			f.mu.Lock()
			defer f.mu.Unlock()
			// THE FIRST DIAL IS THE ONE THAT IS TIMED. Happy Eyeballs opens a
			// second one to the other address family a fraction of a second
			// later, and restarting the clock on it would report the loser's
			// head start as the whole cost of connecting.
			if f.connectBegan.IsZero() {
				f.connectBegan = time.Now()
			}
		},
		ConnectDone: func(_, _ string, err error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			// A dial that failed is not a cost anybody paid for an answer: the
			// winning dial writes the figure, and a row where every dial failed
			// carries no connect time because no connection was made.
			if err != nil || f.connectBegan.IsZero() || f.connect != 0 {
				return
			}
			f.connect = time.Since(f.connectBegan)
		},
		TLSHandshakeStart: func() {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.tlsBegan = time.Now()
		},
		TLSHandshakeDone: func(_ tls.ConnectionState, err error) {
			f.mu.Lock()
			defer f.mu.Unlock()
			if err != nil || f.tlsBegan.IsZero() {
				return
			}
			f.handshake = time.Since(f.tlsBegan)
		},
		GotConn: func(info httptrace.GotConnInfo) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.seen, f.reused = true, info.Reused
		},
	}
}

// stamp writes what was measured onto a row.
//
// FALSE IS WRITTEN OUT AND ZERO IS NOT. A connection that was opened fresh is
// the finding rather than the absence of one, so the row says `conn_reused` is
// false; the three parts of opening it are left off a reused connection,
// because on a reused connection none of them happened. A row whose attempt
// never reached a connection carries none of the four.
func (f *connFacts) stamp(record *calllog.Record) {
	if f == nil || record == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.seen {
		return
	}
	reused := f.reused
	record.ConnReused = &reused
	if reused {
		return
	}
	record.DNSms = f.dns.Milliseconds()
	record.ConnectMs = f.connect.Milliseconds()
	record.TLSms = f.handshake.Milliseconds()
}

// stampConn puts the CURRENT attempt's connection on a row, and only when the
// row is about that attempt. The pairing is the id, for the reason the trace
// keeps one at all ([recordFacts.id]).
func (t *callTrace) stampConn(record *calllog.Record) {
	if t == nil || t.conn == nil || record == nil {
		return
	}
	if t.conn.id != record.ID {
		return
	}
	t.conn.stamp(record)
}

// openConn starts the measurement for the attempt that is about to go out, and
// hands back the trace it rides. A trace-less caller — a probe, which is
// deliberately the leanest request this package makes — gets a trace that still
// reports whether the bytes were written, and nothing is filed.
func (t *callTrace) openConn(sent *atomic.Bool) *httptrace.ClientTrace {
	if t == nil {
		return &httptrace.ClientTrace{
			WroteRequest: func(httptrace.WroteRequestInfo) { sent.Store(true) },
		}
	}
	t.conn = newConnFacts(t.attemptID)
	return t.conn.hooks(sent)
}
