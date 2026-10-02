package relayserve

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/relay"
	"github.com/Agent-Field/codeaf/internal/reqsign"
)

// Config is everything a relay is told. The zero Store is the blind pipe.
type Config struct {
	Store  string           // directory holding directory/ and blobs/; empty serves the pipe only
	Status bool             // answer GET /status with what is connected right now
	Now    func() time.Time // the relay's clock; nil is the wall clock
	Logf   func(string, ...any)
	Note   func(name, what string) // the pipe's arrival and departure line; nil says nothing

	// TrustProxy makes the pairing mailbox's per-network limits count the
	// address in X-Forwarded-For. Only a relay behind a proxy that sets it may
	// say so; anywhere else a caller could name any network it liked.
	TrustProxy bool
	// Pair is what the pairing mailbox enforces; the zero value is
	// pairbox.DefaultLimits.
	Pair pairbox.Limits

	// Grace is what a rotation may ask for as its grace period; the zero value
	// is one hour to thirty days, a week unless told. A relay a test drives
	// shortens it.
	Grace directory.GraceBounds
	// SweepEvery is how often identities whose retirement is due are deleted;
	// zero is hourly.
	SweepEvery time.Duration
	// MaxWatchers is how many directory watch sockets one identity may hold; the
	// zero value is directory.MaxWatchers. A relay a test drives lowers it.
	MaxWatchers int
	// After is the timers presence runs on; nil is the wall clock's. A relay a
	// test drives on a fake clock hands timers that move with it.
	After directory.After
}

// Service is a built relay: one handler and the files it holds open.
type Service struct {
	Handler http.Handler
	ns      *namespaces
	stop    chan struct{}
	swept   sync.WaitGroup
}

// Close stops the sweep and releases the directory files. It is safe on a
// pipe-only service.
func (s *Service) Close() error {
	if s.ns == nil {
		return nil
	}
	close(s.stop)
	s.swept.Wait()
	return s.ns.Close()
}

// Sweep deletes the identities whose retirement is due now. The relay runs it on
// its own every SweepEvery; it is exported for a caller that keeps its own time.
func (s *Service) Sweep() error {
	if s.ns == nil {
		return nil
	}
	return s.ns.sweep()
}

// sweepLoop sweeps once at start, so a relay that was down past a deadline
// catches up, and then every interval.
func (s *Service) sweepLoop(every time.Duration, logf func(string, ...any)) {
	defer s.swept.Done()
	for {
		if err := s.Sweep(); err != nil && logf != nil {
			logf("sweep: %v", err)
		}
		select {
		case <-s.stop:
			return
		case <-time.After(every):
		}
	}
}

// New builds the relay. Every relay is the blind pipe and the pairing mailbox,
// which needs no disk and no identity. Given a store it also serves the
// directory and store wires, every request authenticated by the real signature
// check against the relay's own clock.
func New(cfg Config) *Service {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	mux := pipeMux(&relay.Server{Note: cfg.Note}, cfg.Status)
	mountPairing(mux, cfg, now)
	svc := &Service{Handler: stamped(now, mux)}
	if cfg.Store == "" {
		return svc
	}
	links := directory.NewLinks(now, directory.DefaultLinkLimits)
	svc.ns = newNamespaces(cfg.Store, now, cfg.Grace, cfg.MaxWatchers)
	svc.ns.after = cfg.After
	svc.ns.links = links
	auth := noting(turningRevokedAway(reqsign.AuthenticateAt(now), svc.ns.revoked))
	dirs := logged(cfg.Logf, directory.Handler(auth, svc.ns.directory))
	mux.Handle("/v1/dir/", dirs)
	mux.Handle("/v1/identity/", dirs)
	mux.Handle(directory.LinkPath+"/", logged(cfg.Logf, directory.LinkHandler(links, peerOf(cfg))))
	mux.Handle("/v1/store/", logged(cfg.Logf, blobstore.HandlerAt(now, auth, svc.ns.blobs)))
	svc.stop = make(chan struct{})
	svc.swept.Add(1)
	go svc.sweepLoop(sweepInterval(cfg.SweepEvery), cfg.Logf)
	return svc
}

// peerOf names the network of a link-pairing request: the proxy's word when
// the relay sits behind one, the socket otherwise.
func peerOf(cfg Config) directory.PeerFunc {
	if cfg.TrustProxy {
		return pairbox.ForwardedPeer
	}
	return pairbox.SocketPeer
}

func sweepInterval(d time.Duration) time.Duration {
	if d <= 0 {
		return time.Hour
	}
	return d
}

// mountPairing serves the pairing mailbox outside request signing, because the
// device that is pairing has no identity yet. Its log line is the verb and the
// status and nothing else: the key travels in a header and the messages in a
// body, and neither is ever handed to the logger.
func mountPairing(mux *http.ServeMux, cfg Config, now func() time.Time) {
	limits := cfg.Pair
	if limits == (pairbox.Limits{}) {
		limits = pairbox.DefaultLimits
	}
	peer := pairbox.SocketPeer
	if cfg.TrustProxy {
		peer = pairbox.ForwardedPeer
	}
	h := logged(cfg.Logf, pairbox.Handler(pairbox.NewMemory(limits, now), peer))
	mux.Handle(pairbox.Path, h)
	mux.Handle(pairbox.Path+"/", h)
}

// stamped puts the relay's clock on every answer, refusals included, because a
// device refused for skew needs the time most of all, and a conformance run
// reads the relay's time from any answer at all. It sets a header and
// wraps nothing, so the pipe's hijacked connections are untouched.
func stamped(now func() time.Time, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Codeaf-Now", strconv.FormatInt(now().UnixMilli(), 10))
		next.ServeHTTP(w, r)
	})
}

// pipeMux is the blind pipe's routes, as cmd/relay always registered them.
func pipeMux(service *relay.Server, status bool) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle(relay.EnginePath, service)
	mux.Handle(relay.DialPrefix, service)
	if status {
		mux.HandleFunc("/status", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(service.Ledgers())
		})
	}
	return mux
}

// drainWithin is how long in-flight requests get to finish after SIGTERM.
const drainWithin = 30 * time.Second

// Serve answers on ln until ctx ends, then lets in-flight requests finish.
//
// READ AND WRITE TIMEOUTS ARE DELIBERATELY ABSENT. Every pipe connection is
// meant to be held open for hours, and a write timeout on a hijacked
// connection is a conversation cut in half. The bounds that matter are in the
// relay package: a registration must finish its handshake within
// relay.HandshakeWithin, and a carrier that stops answering pings is dropped.
func Serve(ctx context.Context, ln net.Listener, h http.Handler) error {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 20 * time.Second}
	done := make(chan error, 1)
	go func() {
		<-ctx.Done()
		grace, cancel := context.WithTimeout(context.Background(), drainWithin)
		defer cancel()
		done <- srv.Shutdown(grace)
	}()
	if err := srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return <-done
}
