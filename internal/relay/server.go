package relay

// The service. It is an [http.Handler] so that a relay operator can put it
// behind whatever already terminates TLS for them, and so that this package's
// own tests can drive it through httptest with no port and no certificate.
//
// WHAT IT HOLDS is a table of names that are connected right now, and nothing
// else. There is no account, no password, no database, no record of who dialled
// what — a registration exists while its connection does and is forgotten the
// moment that connection ends. THIS IS THE PRODUCT DECISION, not an unfinished
// one: a relay that remembered would be a relay somebody could subpoena, and
// the only thing it could ever tell them is which made-up machine names were
// online, which is exactly why it is not worth keeping.
//
// WHAT IT CAN SEE, stated so that nobody has to guess: the name, the times, and
// the byte counts. [Ledger] is that list made into a type — it is the whole of
// what this service could report about a session even if it wanted to, and a
// test asserts the payloads never appear in it.

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Ledger is everything the relay knows about one connected machine. IT IS THE
// COMPLETE LIST BY CONSTRUCTION: the relay's only other data is the socket.
type Ledger struct {
	// Name is the machine's key-derived name.
	Name string
	// Since is when it registered.
	Since time.Time
	// Streams is how many surfaces are attached right now.
	Streams int
	// Up and Down are bytes carried, surface→machine and machine→surface.
	// They are counts. The relay has never seen what any of them were.
	Up, Down int64
}

// Server is the relay.
//
// The zero value works: an empty table, the wall clock, and no logging. Every
// limit it enforces comes from the constants in relay.go, so an operator who
// changes one changes it in the one place the sentences quote.
type Server struct {
	// Now is the clock, swapped by tests. Nil is [time.Now].
	Now func() time.Time
	// Note is where a relay operator's log line goes, if they want one. It is
	// handed the name and a short reason and NEVER a payload, because there is
	// no payload in this package to hand it.
	Note func(name, what string)

	mu    sync.Mutex
	live  map[string]*registration
	dials map[string]*bucket
	regs  map[string]*bucket
}

// registration is one machine's held carrier.
type registration struct {
	name    string
	key     []byte
	carrier *carrier
	since   time.Time

	mu       sync.Mutex
	up, down int64
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) note(name, what string) {
	if s.Note != nil {
		s.Note(name, what)
	}
}

// Ledgers is what the relay currently knows, for an operator's status page and
// for this package's own tests.
func (s *Server) Ledgers() []Ledger {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Ledger, 0, len(s.live))
	for _, reg := range s.live {
		reg.mu.Lock()
		up, down := reg.up, reg.down
		reg.mu.Unlock()
		reg.carrier.mu.Lock()
		streams := len(reg.carrier.streams)
		reg.carrier.mu.Unlock()
		out = append(out, Ledger{Name: reg.name, Since: reg.since, Streams: streams, Up: up, Down: down})
	}
	return out
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == EnginePath:
		s.serveEngine(w, r)
	case strings.HasPrefix(r.URL.Path, DialPrefix):
		s.serveDial(w, r, strings.TrimPrefix(r.URL.Path, DialPrefix))
	default:
		// A RELAY HAS NO WEBSITE. Anything that is not one of the two doors is
		// not a page that is missing, it is a request that was never going to
		// be answered.
		http.Error(w, "not a relay door", http.StatusNotFound)
	}
}

// serveEngine takes a machine's registration and then holds its carrier.
func (s *Server) serveEngine(w http.ResponseWriter, r *http.Request) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), Protocol) {
		http.Error(w, "this door speaks "+Protocol, http.StatusUpgradeRequired)
		return
	}
	from := callerAddress(r)
	if !s.allow(&s.regs, from, RegistrationsPerMinute) {
		http.Error(w, "too many registrations from here — wait a minute", http.StatusTooManyRequests)
		return
	}

	name := r.Header.Get(HeaderName)
	key, err := DecodeKey(r.Header.Get(HeaderKey))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// RULE ONE OF [Names]: the name must be the one this key derives. The
	// relay recomputes it rather than trusting the header, so a machine cannot
	// register under a name it has no key for.
	if want := NameFor(key); name != want {
		http.Error(w, "that name does not belong to that key", http.StatusBadRequest)
		return
	}

	conn, err := hijack(w)
	if err != nil {
		http.Error(w, "this server cannot hold a connection open", http.StatusInternalServerError)
		return
	}
	defer func() { _ = conn.Close() }()
	if err := writeUpgraded(conn); err != nil {
		return
	}

	// RULE TWO: possession is proved. A machine that cannot answer the
	// challenge never reaches the table.
	_ = conn.SetDeadline(s.now().Add(HandshakeWithin))
	if err := challengeMachine(conn, name, key); err != nil {
		_ = writeVerdict(conn, err.Error())
		s.note(name, "registration refused")
		return
	}
	_ = conn.SetDeadline(time.Time{})

	carrier := newCarrier(conn, false)
	reg := &registration{name: name, key: key, carrier: carrier, since: s.now()}

	// RULE THREE: a live registration is never taken over. Whoever is
	// connected under this name stays connected under it.
	s.mu.Lock()
	if s.live == nil {
		s.live = map[string]*registration{}
	}
	if _, taken := s.live[name]; taken {
		s.mu.Unlock()
		_ = writeVerdict(conn, "that machine is already connected to the relay under this name")
		s.note(name, "registration refused, name already connected")
		return
	}
	s.live[name] = reg
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		if s.live[name] == reg {
			delete(s.live, name)
		}
		s.mu.Unlock()
		carrier.shutdown(nil)
		s.note(name, "left")
	}()

	if err := writeVerdict(conn, ""); err != nil {
		return
	}
	s.note(name, "connected")

	// THE CARRIER IS ONLY ALIVE WHILE IT IS SAYING SOMETHING. The relay pings a
	// silent one every [PingEvery] and the machine answers; the deadline is
	// pushed out by an ARRIVING frame, so a connection that has stopped
	// answering — a NAT that forgot the mapping, a machine that was unplugged —
	// ends at [IdleAfter] instead of being held for ever.
	carrier.onFrame = func() { _ = conn.SetReadDeadline(s.now().Add(IdleAfter)) }
	_ = conn.SetReadDeadline(s.now().Add(IdleAfter))
	go carrier.keepAlive()
	carrier.pump()
}

// serveDial matches a surface to a machine and then gets out of the way.
func (s *Server) serveDial(w http.ResponseWriter, r *http.Request, name string) {
	if !strings.EqualFold(r.Header.Get("Upgrade"), Protocol) {
		http.Error(w, "this door speaks "+Protocol, http.StatusUpgradeRequired)
		return
	}
	if !s.allow(&s.dials, callerAddress(r), DialsPerMinute) {
		http.Error(w, "too many connections from here — wait a minute", http.StatusTooManyRequests)
		return
	}
	if !ValidName(name) {
		http.Error(w, "that is not a machine name", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	reg := s.live[name]
	s.mu.Unlock()
	if reg == nil {
		// THE ONE SENTENCE THE SURFACE TURNS INTO ITS OWN. It is a 404 because
		// that is what it is: nothing is registered under that name right now.
		// The surface never shows this text — it says the machine is not
		// connected, in its own words (internal/pair's errors.go).
		http.Error(w, "no machine is connected under that name", http.StatusNotFound)
		return
	}

	stream, err := reg.carrier.open()
	if err != nil {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	defer func() { _ = stream.Close() }()

	conn, err := hijack(w)
	if err != nil {
		http.Error(w, "this server cannot hold a connection open", http.StatusInternalServerError)
		return
	}
	defer func() { _ = conn.Close() }()
	if err := writeUpgraded(conn); err != nil {
		return
	}
	s.note(name, "a surface arrived")
	defer s.note(name, "a surface left")

	// THE SPLICE, WHICH IS THE WHOLE SERVICE. Bytes from the surface go to the
	// machine and bytes from the machine go to the surface, and this function
	// counts them and looks at nothing. There is no branch here on what a byte
	// is, because there is nothing in this package that could tell.
	done := make(chan struct{}, 2)
	go func() {
		n, _ := io.Copy(stream, conn)
		reg.add(n, 0)
		_ = stream.Close()
		done <- struct{}{}
	}()
	go func() {
		n, _ := io.Copy(conn, stream)
		reg.add(0, n)
		_ = conn.Close()
		done <- struct{}{}
	}()
	<-done
}

func (r *registration) add(up, down int64) {
	r.mu.Lock()
	r.up += up
	r.down += down
	r.mu.Unlock()
}

// hijack takes the raw connection out from under the HTTP server.
func hijack(w http.ResponseWriter) (net.Conn, error) {
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		return nil, errors.New("relay: this server does not hand out connections")
	}
	conn, buffered, err := hijacker.Hijack()
	if err != nil {
		return nil, err
	}
	// A CLIENT MUST NOT WRITE BEFORE IT HAS READ THE 101, and both of this
	// package's clients obey that — so anything already buffered here is a
	// client that did not, and continuing would mean silently dropping its
	// first bytes.
	if buffered.Reader.Buffered() > 0 {
		_ = conn.Close()
		return nil, errors.New("relay: that client wrote before the upgrade was answered")
	}
	return conn, nil
}

// writeUpgraded is the 101 the two clients wait for, written by hand because
// the connection is no longer the HTTP server's to answer on.
func writeUpgraded(conn net.Conn) error {
	_, err := io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: "+Protocol+"\r\nConnection: Upgrade\r\n\r\n")
	return err
}

// callerAddress is who a rate limit is counted against. It reads
// X-Forwarded-For only when it is there, because a relay in production sits
// behind something that terminates TLS, and counting every request against the
// load balancer would rate-limit the world as one caller.
func callerAddress(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if comma := strings.IndexByte(forwarded, ','); comma >= 0 {
			forwarded = forwarded[:comma]
		}
		return strings.TrimSpace(forwarded)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// bucket is one caller's allowance, refilled by the minute. It is a plain
// counter rather than a token bucket because the thing being limited is a
// person opening a connection, and per-minute is the granularity a person can
// feel.
type bucket struct {
	minute time.Time
	count  int
}

func (s *Server) allow(table *map[string]*bucket, caller string, perMinute int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if *table == nil {
		*table = map[string]*bucket{}
	}
	minute := s.now().Truncate(time.Minute)
	entry := (*table)[caller]
	if entry == nil || !entry.minute.Equal(minute) {
		entry = &bucket{minute: minute}
		(*table)[caller] = entry
		// The tables are swept whenever one of them turns over, so a relay that
		// has been dialled by ten thousand addresses over a day holds the ones
		// that dialled it this minute.
		sweep(*table, minute)
	}
	if entry.count >= perMinute {
		return false
	}
	entry.count++
	return true
}

func sweep(table map[string]*bucket, minute time.Time) {
	for caller, entry := range table {
		if !entry.minute.Equal(minute) {
			delete(table, caller)
		}
	}
}

// Refuse is the sentence a relay operator's front end should hand back when it
// is turning connections away for a reason of its own, so that the surface has
// something true to print rather than an HTML error page.
func Refuse(reason string) string {
	return fmt.Sprintf("the relay refused this connection: %s", reason)
}
