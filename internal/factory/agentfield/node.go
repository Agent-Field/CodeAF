package agentfield

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory/inbox"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/redact"
)

// ── THE NODE: WHAT RUNS WHILE A FACTORY IS CONNECTED ────────────────────────
//
// While connected, and only then, the node:
//
//   - registers with the control plane (POST /api/v1/nodes) as a fresh
//     instance — EVERY START IS A NEW instance_id, so the control plane knows
//     a restarted factory from a running one — registering again when the
//     control plane has forgotten it;
//   - OPENS THE LINK (link.go): one WebSocket it dials out and keeps, with
//     reconnect and backoff. Dispatches and cancels come down it, so a
//     factory behind NAT needs NO listening port. With no base_url the node
//     binds nothing;
//   - with a base_url, also listens and answers the control plane's HTTP
//     calls behind the internal-token bearer (reasoners.go), for a control
//     plane that offers no link;
//   - keeps the lease: a heartbeat frame down the link while it is up, the
//     lease PATCH otherwise, and EITHER WAY it lists the open executions
//     (`inflight`), which the control plane touches so the stale reaper never
//     times out a job the node still holds. There is no retelling of quiet
//     executions any more: the lease keeps them alive;
//   - reports the job behind every open execution (sink.go): it is a sink
//     on the factory's event bus ([inbox.RegisterSink]), so each of the
//     job's moments reaches its execution as it happens, up the link when it
//     is up and over HTTP when it is not. Each lease tick also catches up an
//     execution whose last report did not land.
//
// NOT CONNECTED, NOTHING RUNS: no listener, no link, no registration, no
// lease. A disconnect tells the control plane the node is offline on its way
// out.

// SinkName is the node's name on the event bus.
const SinkName = "agentfield"

// ErrNotConnected is a start with nothing kept.
var ErrNotConnected = errors.New("this factory is not connected to AgentField")

// Health is what the node did last, as a person reads it. It never carries a
// secret.
type Health struct {
	// Listening is the address the node's listener is bound to; empty while
	// it is not running, and always empty for a link-only node.
	Listening string
	// Link is the link's state: "up", "connecting", "not offered" (the
	// control plane has no link route) or "" while the node is stopped.
	// LinkSince is when it last changed and LinkUps how many times it has
	// come up since the node started.
	Link      string
	LinkSince time.Time
	LinkUps   int
	// LastEventVia is the carrier the last call came by: "link" or "http".
	LastEventVia string
	// Registered is when the control plane last took the registration.
	Registered time.Time
	// LeaseSeconds and LeaseUntil are the last lease the control plane gave.
	LeaseSeconds int
	LeaseUntil   time.Time
	// LastEvent is when the last call reached a reasoner, LastEventSource the
	// source it came from and LastEventWhat what became of it.
	LastEvent       time.Time
	LastEventSource string
	LastEventWhat   string
	// LastError is the last thing that went wrong, cleared by the next
	// registration or lease that works.
	LastError   string
	LastErrorAt time.Time
}

// Node is one factory's node. Conn and Store are required; every hook is
// optional and its absence is stated where it matters.
type Node struct {
	Conn  *Connection
	Store *store.Store
	// After is the factory's intake pass (routing and start rules), run after
	// an entry is filed so a routed job starts at once. nil files the job and
	// leaves it for the factory's next pass.
	After func(context.Context) error
	// Options are the inbox options an entry is filed with (the repository's
	// recipe).
	Options func() []inbox.Option
	// Nudge asks the factory's own GitHub read to read now. nil makes a
	// github event a no-op.
	Nudge func()
	// Cancel stops a job, the way a person's Stop does, when the control
	// plane cancels the execution behind it. nil leaves the job running and
	// only closes the execution here.
	Cancel func(ctx context.Context, job int) error
	// Filed is told the job an entry filed, once on filing and once after
	// the intake pass, so a page showing the queue reads it again now. nil
	// tells nobody.
	Filed func(job int)
	// Version is the node contract the node registers as ([NodeVersion]).
	Version string
	// Build is the codeaf build, kept in the registration's metadata.
	Build string
	// Clock replaces time.Now; tests only.
	Clock func() time.Time

	mu       sync.Mutex
	health   Health
	running  *running
	x        executions
	xOnce    sync.Once
	reportMu sync.Mutex
}

type running struct {
	ctx      context.Context
	cancel   context.CancelFunc
	server   *http.Server
	mux      http.Handler
	done     chan struct{}
	linkDone chan struct{}
	settings Settings
	secrets  Secrets
	client   *Client
	instance string
	began    time.Time
	// registered is closed by the first registration that worked; the link
	// waits for it, since the control plane refuses a link for a node it
	// does not know.
	registered     chan struct{}
	registeredOnce sync.Once
	// link is the live link, nil while it is down (guarded by Node.mu).
	link *linkSession
	seen *seenSet
}

func (n *Node) now() time.Time {
	if n.Clock != nil {
		return n.Clock()
	}
	return time.Now()
}

func (n *Node) table() *executions {
	n.xOnce.Do(func() { n.x.root = n.Store.Root() })
	return &n.x
}

// Health is the node's health now.
func (n *Node) Health() Health {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.health
}

// Running says the node is started.
func (n *Node) Running() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.running != nil
}

// Start starts the node over the kept connection: it binds the listener
// (and answers the error when it cannot, before anything else happens),
// then registers and keeps the lease in the background. A node already
// running is stopped first, so a connect with new settings takes effect.
func (n *Node) Start(ctx context.Context) error {
	n.Stop()
	s, sec, ok := n.Conn.Kept()
	if !ok {
		return ErrNotConnected
	}
	// LINK ONLY (no base_url): nothing is bound, at all.
	var listener net.Listener
	if s.BaseURL != "" {
		var err error
		if listener, err = net.Listen("tcp", s.Listen); err != nil {
			return errors.New("the factory could not listen on " + s.Listen + " for AgentField: " + redact.Secrets(err.Error()))
		}
	}
	instance, err := newInstance()
	if err != nil {
		if listener != nil {
			listener.Close()
		}
		return err
	}
	ctx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	r := &running{ctx: ctx, cancel: cancel, done: make(chan struct{}), linkDone: make(chan struct{}), settings: s, secrets: sec, client: &Client{URL: s.URL, Key: sec.Key, HTTP: n.Conn.HTTP}, instance: instance, began: n.now(), registered: make(chan struct{}), seen: newSeenSet(10000)}
	r.mux = n.routes(r)
	if listener != nil {
		r.server = &http.Server{Handler: Guarded(sec.Token, sec.Legacy, viaHTTP(n, r.mux)), ReadHeaderTimeout: 10 * time.Second, BaseContext: func(net.Listener) context.Context { return ctx }}
	}
	n.mu.Lock()
	n.running = r
	n.health = Health{Link: "connecting", LinkSince: n.now()}
	if listener != nil {
		n.health.Listening = listener.Addr().String()
	}
	n.mu.Unlock()
	inbox.RegisterSink(SinkName, sink{n})
	if r.server != nil {
		guard.Go("factory/agentfield listen", func() { _ = r.server.Serve(listener) })
	}
	guard.Go("factory/agentfield lease", func() {
		defer close(r.done)
		n.keep(ctx, r)
	})
	guard.Go("factory/agentfield link", func() {
		defer close(r.linkDone)
		n.linkLoop(ctx, r)
	})
	return nil
}

// Stop tells the control plane the node is going offline (best effort, a few
// seconds at most) and stops the listener and the lease.
func (n *Node) Stop() {
	n.mu.Lock()
	r := n.running
	n.running = nil
	n.health.Listening = ""
	n.mu.Unlock()
	if r == nil {
		return
	}
	inbox.RegisterSink(SinkName, nil)
	r.cancel()
	<-r.done
	<-r.linkDone
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _ = r.client.Renew(ctx, r.settings.NodeID, "offline", n.Version, nil)
	if r.server != nil {
		_ = r.server.Shutdown(ctx)
		_ = r.server.Close()
	}
	n.mu.Lock()
	n.health.Link = ""
	n.mu.Unlock()
}

// linkHeartbeat is how often the lease is renewed down the link.
const linkHeartbeat = time.Minute

// keep registers and renews the lease until ctx ends: a heartbeat frame down
// the link while it is up, the lease PATCH otherwise, each listing the open
// executions. Each renewal then catches up any execution whose last report
// did not land; every other report is a moment on the bus (sink.go).
func (n *Node) keep(ctx context.Context, r *running) {
	registered := false
	wait := time.Duration(0)
	failed := 0
	nextLease := time.Time{}
	for {
		if !registered {
			if err := n.register(ctx, r); err != nil {
				failed++
				n.trouble("registering with AgentField: " + err.Error())
				wait = backoff(failed)
			} else {
				registered, failed, wait = true, 0, 0
				r.registeredOnce.Do(func() { close(r.registered) })
			}
		}
		if registered && !n.now().Before(nextLease) {
			inflight := n.inflight()
			renewed := false
			if ls := n.liveLink(); ls != nil {
				ok, err := ls.call(ctx, frame{T: tHeartbeat}, heartbeat{Inflight: inflight})
				switch {
				case err == nil && ok.Code == http.StatusNotFound:
					registered = false
					continue
				case err == nil && ok.Code < 300:
					nextLease = n.now().Add(linkHeartbeat)
					n.mu.Lock()
					n.health.LeaseSeconds = 0
					n.health.LeaseUntil = time.Time{}
					n.health.LastError, n.health.LastErrorAt = "", time.Time{}
					n.mu.Unlock()
					n.catchUp(ctx, r)
					renewed = true
				}
				// Otherwise the link could not carry it: renew over HTTP.
			}
			var lease Lease
			var err error
			if !renewed {
				lease, err = r.client.Renew(ctx, r.settings.NodeID, "ready", n.Version, inflight)
			}
			switch {
			case renewed:
			case err == nil:
				every := leaseEvery(lease.Seconds)
				nextLease = n.now().Add(every)
				n.mu.Lock()
				n.health.LeaseSeconds = lease.Seconds
				n.health.LeaseUntil = n.now().Add(time.Duration(lease.Seconds) * time.Second)
				n.health.LastError, n.health.LastErrorAt = "", time.Time{}
				n.mu.Unlock()
				n.catchUp(ctx, r)
			case RefusedAs(err, RefusedMissing):
				// THE CONTROL PLANE FORGOT THE NODE (it restarted on a fresh
				// database, or someone deleted it): register again at once.
				registered = false
				continue
			default:
				n.trouble("renewing the AgentField lease: " + err.Error())
				nextLease = n.now().Add(backoff(1))
			}
		}
		if wait == 0 {
			wait = time.Until(nextLease)
			if wait <= 0 {
				wait = time.Second
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		wait = 0
	}
}

// register sends the registration for this start's instance.
func (n *Node) register(ctx context.Context, r *running) error {
	if err := r.client.Register(ctx, RegistrationFor(r.settings, r.instance, n.Version, n.Build)); err != nil {
		return err
	}
	n.mu.Lock()
	n.health.Registered = n.now()
	n.health.LastError, n.health.LastErrorAt = "", time.Time{}
	n.mu.Unlock()
	return nil
}

// trouble records what went wrong, redacted.
func (n *Node) trouble(what string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.health.LastError = redact.Secrets(what)
	n.health.LastErrorAt = n.now()
}

// inflight is every open execution's id, for the lease.
func (n *Node) inflight() []string {
	var out []string
	for _, e := range n.table().open() {
		out = append(out, e.ID)
	}
	return out
}

// heard records a call that reached a reasoner.
func (n *Node) heard(source, what string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.health.LastEvent = n.now()
	n.health.LastEventSource = source
	n.health.LastEventWhat = redact.Secrets(what)
}

// leaseEvery is how often a lease of seconds is renewed: a third of it,
// between ten seconds and two minutes, and a minute when the control plane
// named none.
func leaseEvery(seconds int) time.Duration {
	if seconds <= 0 {
		return time.Minute
	}
	return min(max(time.Duration(seconds)*time.Second/3, 10*time.Second), 2*time.Minute)
}

// backoff is the wait after failed tries in a row: five seconds, doubling, at
// most two minutes.
func backoff(failed int) time.Duration {
	d := 5 * time.Second
	for i := 1; i < failed && d < 2*time.Minute; i++ {
		d *= 2
	}
	return min(d, 2*time.Minute)
}

func newInstance() (string, error) {
	raw := make([]byte, 8)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	host, _ := os.Hostname()
	host = strings.Map(func(r rune) rune {
		if r == '.' || r == '-' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			return r
		}
		return -1
	}, strings.ToLower(host))
	if host == "" {
		host = "codeaf"
	}
	return host + "-" + hex.EncodeToString(raw), nil
}

// Forget takes the node off the event bus (a disconnect). [Node.Stop] does
// it too; Forget is for a node that is not running.
func (n *Node) Forget() { inbox.RegisterSink(SinkName, nil) }
