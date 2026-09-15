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

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/lane/control"
)

// Connection recovery is a reachability check, never an inference. One shared
// check serves this client's conversations and workers, so losing Wi-Fi does
// not spend every call's retry ladder against the same unreachable router.
const (
	connectionRecoveryWindow = 2 * time.Minute
	connectionProbeTimeout   = 2 * time.Second
	connectionProbeInterval  = time.Second
)

// connectionRetry is one media call's recovery state. Completion calls keep
// this state inside the one dispatcher, where [control.Plan.Deadline] is their
// only budget; media has no plan or completion ladder and keeps the caller's
// shorter deadline or [connectionRecoveryWindow]. The pass count chooses a
// backoff rung and never ends the call.
type connectionRetry struct {
	ctx    context.Context
	cancel context.CancelFunc
	passes int
	owed   time.Duration
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

// recoverBeforeSend waits for the origin on behalf of one media send that
// failed before it left, and reports whether the caller may try again. A nil
// error means send; anything else is this call's ending. A DNS or dial failure
// precedes accepted generation, so what is waited for is the ORIGIN and never
// another provider behind that same origin.
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
	deadline, bounded := recovery.ctx.Deadline()
	spentAt := func() time.Time { return time.Now().Add(recovery.owed) }
	if bounded && !spentAt().Before(deadline) {
		return &ConnectionUnavailableError{}
	}
	recovery.passes++
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
		delay := backoffFor(recovery.passes-1, 0)
		if bounded {
			left := deadline.Sub(spentAt())
			if left <= 0 {
				return &ConnectionUnavailableError{}
			}
			if delay > left {
				delay = left
			}
		}
		began := time.Now()
		if err := c.wait(recovery.ctx, delay); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if recovery.ctx.Err() != nil {
				return &ConnectionUnavailableError{}
			}
			return err
		}
		if took := time.Since(began); took < delay {
			recovery.owed += delay - took
		}
		if bounded && !spentAt().Before(deadline) {
			return &ConnectionUnavailableError{}
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
	w, waiting := c.joinConnection(target, start)
	if !waiting {
		return false, nil
	}
	g := &c.connection
	defer func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		w.waiters--
		if w.waiters == 0 {
			w.cancel()
			if g.active == w {
				g.active = nil
			}
		}
	}()
	watch := streamWatchFrom(ctx)
	watch.pauseConnection()
	defer watch.resumeConnection()
	// THE DEADLINE ON THIS WAIT IS REAL AND WAS BEING THROWN AWAY. A connection
	// wait ends on its own at [connectionRecoveryWindow] past the moment it
	// started, with a [ConnectionUnavailableError] the ladder above will not
	// spend another window on — so there IS a moment at which this build acts,
	// and a countdown drawn to it is a countdown that means something. It used
	// to post a zero here, which the emptiness law correctly draws as nothing,
	// and the person watching a dead Wi-Fi was told the wait was real but never
	// how long this build would give it.
	until := w.since.Add(connectionRecoveryWindow)
	announce := func() {
		if watch.speaking() {
			notePhase(ctx, model, PhaseConnectionLost, "", w.since, until, "")
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

// joinConnection is [Client.waitConnection]'s locked half: the existing wait
// joined, or one started when start asks for it and none is running. Starting
// the probe inside the same critical section is what keeps two callers from
// each opening their own recovery for one outage.
func (c *Client) joinConnection(target string, start bool) (*connectionWait, bool) {
	g := &c.connection
	g.mu.Lock()
	defer g.mu.Unlock()
	w := g.active
	if w == nil && start {
		probeCtx, cancel := context.WithTimeout(context.Background(), connectionRecoveryWindow)
		w = &connectionWait{done: make(chan struct{}), cancel: cancel, since: time.Now()}
		g.active = w
		guard.Go("provider connection recovery", func() { c.recoverConnection(probeCtx, target, w) })
	}
	if w == nil {
		return nil, false
	}
	w.waiters++
	return w, true
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
	// The suspend is its own locked half: [hedgeRace.rearm] signals the beat,
	// which reads this watch's deadline under this same lock.
	w.suspend()
	w.race.rearm()
}

// suspend marks this watch as paused on a dead connection and drops its
// deadline, holding the lock from a defer for the whole of it.
func (w *streamWatch) suspend() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.recovering, w.recovered = true, true
	w.deadline = time.Time{}
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
	// The fresh controller is installed in its own locked half, for
	// [streamWatch.attempt]'s reason: the re-arm must not be sent from inside
	// the lock it wakes readers of.
	w.rebuildAfterRecovery(plan)
	w.race.rearm()
}

// rebuildAfterRecovery installs a controller dated from the reconnection and
// reopens the watch's per-attempt facts, holding the lock from a defer.
func (w *streamWatch) rebuildAfterRecovery(plan control.Plan) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.control = w.race.build(plan)
	w.deadline = w.control.Deadline()
	w.began = plan.Began
	w.acted, w.silence, w.fault = control.Act{}, 0, false
	w.recovering = false
}
