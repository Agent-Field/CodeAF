package relayserve

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/blobstore"
	"github.com/Agent-Field/codeaf/internal/clusterctl"
	"github.com/Agent-Field/codeaf/internal/clusterstate"
	"github.com/Agent-Field/codeaf/internal/directory"
	"github.com/Agent-Field/codeaf/internal/pairbox"
	"github.com/Agent-Field/codeaf/internal/rafttransport"
	"github.com/Agent-Field/codeaf/internal/relay"
)

// Config is everything a relay is told. The zero Store is the blind pipe.
type Config struct {
	// Cluster is supplied by the persistent member host after loading its
	// committed enrollment view. Its handler is an internal assembly seam, not a
	// rollout switch or an alternative directory writer.
	Cluster http.Handler

	Store  string           // directory holding directory/ and blobs/; empty serves the pipe only
	Status bool             // answer GET /status with what is connected right now
	Now    func() time.Time // the relay's clock; nil is the wall clock
	Logf   func(string, ...any)
	Note   func(name, what string) // the pipe's arrival and departure line; nil says nothing

	// TrustProxy makes registration, dial and pairing limits count the
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

	// StorageOff keeps the directory and refuses every chat upload, the way the hosted relay does for an account it has not
	// approved to store chats (blobstore.StorageOff). It changes nothing without a Store.
	StorageOff bool
}

// Service is a built relay: one handler and the files it holds open.
type Service struct {
	Handler http.Handler
	ns      *namespaces
	fabric  *fabric
	stop    chan struct{}
	swept   sync.WaitGroup
	// drain is closed when the relay starts shutting down, so a request held open
	// on purpose ends at once instead of holding the shutdown for its full wait.
	drain     chan struct{}
	drainOnce sync.Once
}

// Drain ends every request the relay is holding open on purpose: a slot wait, a
// pairing poll and a directory watch socket. [Serve] calls it when its server
// begins to shut down; it is safe to call more than once, and on a pipe-only
// service.
//
// THE SHUTDOWN CANNOT DO THIS ITSELF. http.Server.Shutdown waits for every
// request still being answered, and a held wait is one: up to 25 seconds for a
// slot wait or a pairing poll, so a phone with a wait parked kept the relay
// alive past a SIGTERM until it was killed. Shutdown also ignores a hijacked
// connection such as a watch socket, which it is up to the caller to tell.
// Requests that are doing work, an upload or a write to the directory, are not
// touched: they still finish, as Serve promises.
func (s *Service) Drain() { s.drainOnce.Do(func() { close(s.drain) }) }

// drainable is the handler of a built relay: the same routes, plus the Drain
// that [Serve] looks for.
type drainable struct {
	http.Handler
	drain func()
}

func (d drainable) Drain() { d.drain() }

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
	s.fabric.mu.Lock()
	defer s.fabric.mu.Unlock()
	if err := s.ns.sweep(); err != nil {
		return err
	}
	return s.fabric.cleanRevoked()
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
// directory and store wires through three persistent local anchors per identity.
// First use migrates the source; every request proves its original signature.
func New(cfg Config) *Service {
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	mux := pipeMux(&relay.Server{Note: cfg.Note, Peer: peerOf(cfg)}, cfg.Status)
	svc := &Service{drain: make(chan struct{})}
	mountPairing(mux, cfg, now, svc.drain)
	mountCluster(mux, cfg.Cluster)
	mux.HandleFunc("/p/", servePairPage)
	svc.Handler = drainable{Handler: stamped(now, mux), drain: svc.Drain}
	if cfg.Store == "" {
		return svc
	}
	links := directory.NewLinks(now, directory.DefaultLinkLimits)
	svc.ns = newNamespaces(cfg.Store, now, cfg.Grace, cfg.MaxWatchers)
	svc.ns.logf = cfg.Logf
	svc.ns.after = cfg.After
	svc.ns.links = links
	mux.HandleFunc(clusterstate.ClusterAPIPath, svc.ns.clusterAPI)
	mux.HandleFunc(clusterstate.PersonalAdmissionPath, svc.ns.enrollPersonal)
	if cfg.Cluster == nil {
		mux.HandleFunc(rafttransport.ExchangePath, svc.ns.exchangePersonal)
		mux.HandleFunc(rafttransport.SocketPath, svc.ns.exchangePersonal)
		mux.HandleFunc(clusterctl.ForwardPath, svc.ns.exchangePersonal)
	}
	mux.Handle(clusterctl.Path, clusterctl.Handler(svc.ns.controlAuthenticate, svc.ns.controlBackend))
	auth := noting(svc.ns.authenticate)
	svc.stop = make(chan struct{})
	fabric := &fabric{ns: svc.ns, auth: noting(svc.ns.authenticateFabric), stop: svc.stop, drain: svc.drain}
	svc.fabric = fabric
	svc.ns.fabric = fabric
	dirs := endOnDrain(svc.drain, isWatchSocket, logged(cfg.Logf, fabric.directoryHandler(directory.Handler(auth, svc.ns.directory))))
	clients := logged(cfg.Logf, fabric.handler())
	mux.Handle("/v1/clients", clients)
	mux.Handle("/v1/clients/", clients)
	mux.Handle("POST /v1/dir/devices/{device}/revoke", logged(cfg.Logf, fabric.serve(revokeClient)))
	mux.Handle("/v1/dir/", dirs)
	mux.Handle("/v1/identity/", dirs)
	mux.Handle(directory.LinkPath+"/", logged(cfg.Logf, directory.LinkHandler(links, peerOf(cfg))))
	blobs := blobstore.HandlerAt(now, auth, svc.ns.blobs)
	if cfg.StorageOff {
		blobs = blobstore.StorageOff(blobs)
	}
	mux.Handle("/v1/store/", logged(cfg.Logf, blobs))
	svc.swept.Add(1)
	go svc.sweepLoop(sweepInterval(cfg.SweepEvery), cfg.Logf)
	return svc
}

// peerOf keeps registrations, dials and both pairing paths on one explicit
// proxy policy. An untrusted forwarding header can never select an allowance.
func peerOf(cfg Config) func(*http.Request) string {
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
func mountPairing(mux *http.ServeMux, cfg Config, now func() time.Time, drain <-chan struct{}) {
	limits := cfg.Pair
	if limits == (pairbox.Limits{}) {
		limits = pairbox.DefaultLimits
	}
	peer := peerOf(cfg)
	h := logged(cfg.Logf, pairbox.Handler(pairbox.NewMemory(limits, now), peer))
	h = endOnDrain(drain, isPairingPoll, h)
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

// endOnDrain cancels the context of each request that held says is held open on
// purpose, when drain closes. A handler that waits on its request's context ends
// then: a pairing poll answers with nothing, and a watch socket closes. Every
// other request passes through untouched and is not cancelled.
func endOnDrain(drain <-chan struct{}, held func(*http.Request) bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !held(r) {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go func() {
			select {
			case <-drain:
				cancel()
			case <-ctx.Done():
			}
		}()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// isWatchSocket is a directory watch: the one request on the directory's
// routes that stays open until a peer or the relay ends it.
func isWatchSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

// isPairingPoll is a read of the pairing mailbox, which parks for up to
// pairbox.MaxWait when nothing has been written yet.
func isPairingPoll(r *http.Request) bool { return r.Method == http.MethodGet }

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
	// A handler that holds requests open on purpose says so by having a Drain
	// (a built relay's does): it is told the moment the shutdown begins, since
	// Shutdown would otherwise wait out every held wait and ignore every socket.
	if d, ok := h.(interface{ Drain() }); ok {
		srv.RegisterOnShutdown(d.Drain)
	}
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
