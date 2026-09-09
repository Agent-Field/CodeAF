package provider

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/lane/control"
)

// Connection recovery is a reachability check, never an inference. One shared
// check serves this client's conversations and workers, so losing Wi-Fi does
// not spend every call's retry ladder against the same unreachable router.
const (
	connectionRecoveryWindow = 2 * time.Minute
	connectionProbeTimeout   = 2 * time.Second
	connectionProbeInterval  = time.Second
	// connectionRetryPasses bounds how many times ONE request may be sent again
	// after a reachability check answered. It is the arithmetic backstop and not
	// the policy: connectionRecoveryWindow is what ends a real outage, and the
	// growing pause below is what a portal meets. It exists for the degenerate
	// case the window cannot bound — an origin that answers a check in microseconds
	// while refusing every send, where the wall clock barely moves and only a count
	// is finite. Twelve passes of backoffFor already exceed the window, so this
	// changes nothing about an ordinary recovery.
	connectionRetryPasses = 12
)

// connectionRetry is one call's whole recovery: the bounded window it may spend
// and how many times it has come back for another send. It is a value on the
// caller's stack because the bound belongs to the CALL — two conversations
// losing the same router share the probe, never the patience.
type connectionRetry struct {
	ctx    context.Context
	cancel context.CancelFunc
	passes int
}

func (r *connectionRetry) release() {
	if r.cancel != nil {
		r.cancel()
	}
}

// ConnectionUnavailableError ends automatic recovery without inviting a model
// or endpoint ladder to spend another window on the same unreachable origin.
type ConnectionUnavailableError struct{}

func (*ConnectionUnavailableError) Error() string {
	return "connection is still unavailable; try again when connected"
}

// IsConnectionUnavailable distinguishes a spent connection wait from a model
// failure. There is no underlying provider error for a fallback to repair.
func IsConnectionUnavailable(err error) bool {
	var unavailable *ConnectionUnavailableError
	return errors.As(err, &unavailable)
}

// recoverBeforeSend waits for the origin on behalf of one send that failed
// before it left, and reports whether the caller may try again. A nil error
// means send; anything else is this call's ending. A DNS or dial failure
// precedes accepted generation, so what is waited for is the ORIGIN and never
// another provider behind that same origin, and the whole of it stays bounded
// even while connectivity keeps flapping.
//
// THE PAUSE IS THE WHOLE POINT. A check that answers while the send keeps
// failing — a captive portal, a transparent proxy — is a fault and not a
// recovery, and a fault backs off. The first recovered pass is still immediate
// because a connection that genuinely came back should not wait out an old
// delay; every pass after it waits backoffFor its own count, which is the same
// ladder every other fault on this client climbs.
func (c *Client) recoverBeforeSend(ctx context.Context, recovery *connectionRetry, model, target string) error {
	if recovery.ctx == nil {
		recovery.ctx, recovery.cancel = context.WithTimeout(ctx, connectionRecoveryWindow)
	}
	recovery.passes++
	if recovery.passes > connectionRetryPasses {
		return &ConnectionUnavailableError{}
	}
	if _, err := c.waitConnection(recovery.ctx, model, target, true); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if recovery.ctx.Err() != nil {
			return &ConnectionUnavailableError{}
		}
		return err
	}
	if recovery.passes > 1 {
		if err := c.wait(recovery.ctx, backoffFor(recovery.passes-1, 0)); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if recovery.ctx.Err() != nil {
				return &ConnectionUnavailableError{}
			}
			return err
		}
	}
	return nil
}

// connectionFailure only admits failures before an HTTP exchange. A stream
// reset or a response timeout may follow accepted generation and stays on the
// existing stream recovery path; a TLS certificate error needs correction.
func connectionFailure(err error) bool {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		return true
	}
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "dial" {
		return true
	}
	return errors.Is(err, syscall.ENETDOWN) || errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH)
}

type connectionGate struct {
	mu     sync.Mutex
	active *connectionWait
}

// connectionWaiting lets optional paid lane measurements stand aside while
// the connection itself is being recovered.
func (c *Client) connectionWaiting() bool {
	c.connection.mu.Lock()
	defer c.connection.mu.Unlock()
	return c.connection.active != nil
}

type connectionWait struct {
	done    chan struct{}
	cancel  context.CancelFunc
	waiters int
	err     error
	since   time.Time
}

// waitConnection joins an existing wait on the healthy path and starts one
// only after a concrete connection failure. Healthy calls perform no probe.
// The recovery context belongs to its subscribers: one canceled title must
// not cancel a conversation, and the last subscriber leaves no probe behind.
func (c *Client) waitConnection(ctx context.Context, model, target string, start bool) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	g := &c.connection
	g.mu.Lock()
	w := g.active
	if w == nil && start {
		probeCtx, cancel := context.WithTimeout(context.Background(), connectionRecoveryWindow)
		w = &connectionWait{done: make(chan struct{}), cancel: cancel, since: time.Now()}
		g.active = w
		guard.Go("provider connection recovery", func() { c.recoverConnection(probeCtx, target, w) })
	}
	if w == nil {
		g.mu.Unlock()
		return false, nil
	}
	w.waiters++
	g.mu.Unlock()
	defer func() {
		g.mu.Lock()
		w.waiters--
		if w.waiters == 0 {
			w.cancel()
			if g.active == w {
				g.active = nil
			}
		}
		g.mu.Unlock()
	}()
	watch := streamWatchFrom(ctx)
	watch.pauseConnection()
	defer watch.resumeConnection()
	announce := func() {
		if watch.speaking() {
			notePhase(ctx, model, PhaseConnectionLost, "", w.since, time.Time{}, "")
		}
	}
	announce()
	beat := time.NewTicker(phaseBeat)
	defer beat.Stop()
	for {
		select {
		case <-ctx.Done():
			return true, ctx.Err()
		case <-w.done:
			if ctx.Err() != nil {
				return true, ctx.Err()
			}
			return true, w.err
		case <-beat.C:
			announce()
		}
	}
}

func (c *Client) recoverConnection(ctx context.Context, target string, w *connectionWait) {
	defer w.cancel()
	w.err = &ConnectionUnavailableError{}
	// Even a failing custom transport must wake its subscribers. Publication
	// closes the channel only after the final error has been written.
	defer func() {
		c.connection.mu.Lock()
		defer c.connection.mu.Unlock()
		if c.connection.active == w {
			c.connection.active = nil
		}
		close(w.done)
	}()
	probe := c.connectionProbe
	if probe == nil {
		probe = c.probeConnection
	}
	for {
		if ctx.Err() != nil {
			w.err = &ConnectionUnavailableError{}
			break
		}
		err := probe(ctx, target)
		if err == nil {
			w.err = nil
			break
		}
		var certificate *tls.CertificateVerificationError
		var unknownAuthority x509.UnknownAuthorityError
		if errors.As(err, &certificate) || errors.As(err, &unknownAuthority) {
			w.err = err
			break
		}
		// A fixed short cadence keeps recovery responsive even after a long
		// outage. Jitter avoids synchronizing separate application processes.
		delay := connectionProbeInterval + time.Duration(rand.Int63n(int64(connectionProbeInterval/2)))
		if err := c.wait(ctx, delay); err != nil {
			w.err = &ConnectionUnavailableError{}
			break
		}
	}
}

// probeConnection checks the configured origin through the same HTTP transport
// and proxy. Any HTTP response proves reachability, including 401 or 405. It
// sends neither credentials nor a prompt, follows no redirect and requests no
// generation, so recovery cannot create duplicate paid work.
func (c *Client) probeConnection(ctx context.Context, target string) error {
	u, err := url.Parse(target)
	if err != nil {
		return err
	}
	u.Path, u.RawPath, u.RawQuery, u.Fragment, u.User = "/", "", "", "", nil
	probeCtx, cancel := context.WithTimeout(ctx, connectionProbeTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodHead, u.String(), nil)
	if err != nil {
		return err
	}
	client := &http.Client{Transport: c.http.Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if response != nil {
		response.Body.Close()
	}
	return err
}

// A local outage pauses routing deadlines rather than spending a rescue on an
// unreachable origin. A recovered request gets a fresh controller clock, and
// its timing is excluded from learning because the outage was not provider work.
func (w *streamWatch) pauseConnection() {
	if w == nil {
		return
	}
	w.mu.Lock()
	w.recovering, w.recovered = true, true
	w.deadline = time.Time{}
	w.mu.Unlock()
	w.race.rearm()
}

func (w *streamWatch) resumeConnection() {
	if w == nil {
		return
	}
	plan := w.race.plan
	plan.Began = waitNow()
	if arm := w.race.armAt(w.arm); arm != nil && arm.lane != "" {
		plan.Lane, plan.Pinned = arm.lane, false
	}
	plan.Alts = w.race.untriedAlts()
	w.mu.Lock()
	w.control = w.race.build(plan)
	w.deadline = w.control.Deadline()
	w.began = plan.Began
	w.acted, w.silence, w.fault = control.Act{}, 0, false
	w.recovering = false
	w.mu.Unlock()
	w.race.rearm()
}
