package livedoor

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

// This file is the viewer's direct road. A Link that was given a DirectConfig
// keeps the relay view exactly as phase 1 does, and after the first channel is
// open it tries, quietly and on a ladder of waits, to build a second tunnel
// straight to the host. The second tunnel is a NESTED Link of the same type
// (so it has its own mux, its own liveness and its own state) whose every
// event is private to the outer link. When the nested link is Live the outer
// link swaps roads with a planned redial, and when it dies the outer link falls
// back the same way. A person sees only the road word change.
//
// THE OUTER LINK OWNS EVERYTHING THE PERSON SEES. The nested link never raises
// an OnState of the outer link by itself, never redials by itself, and is dialled
// only from an attempt.

// outerSignaller lets the direct-road implementation signal through the outer
// link's CURRENT relay tunnel, so an attempt that outlives a reconnect talks on
// the tunnel that exists now.
type outerSignaller struct {
	l *Link
}

func (s outerSignaller) Send(payload []byte) error {
	return s.l.SendSignal(payload)
}

// directRun is one attempt to build the direct road. Its token says whether the
// attempt is still the current one, and its context is cancelled by the
// connect deadline, by closing the link, by an idle detach, by a revocation and
// by the end of the relay tunnel it started on.
type directRun struct {
	tok    uint64
	nested *Link
	relay  *Mux
	ctx    context.Context
	cancel context.CancelFunc
}

// directChannel is a channel opened on the nested link's tunnel, tracked and
// wrapped by the OUTER link so that the way it ends is the outer link's to
// describe: a tunnel that died under it is a planned redial, a reason the host
// spoke is passed through unchanged.
type directChannel struct {
	l    *Link
	ch   *Channel
	mux  *Mux
	chat string

	closedLocally atomic.Bool
	endOnce       sync.Once

	mu          sync.Mutex
	localReason string
}

func (dc *directChannel) setLocalReason(reason string) {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	if dc.localReason == "" {
		dc.localReason = reason
	}
}

func (dc *directChannel) local() string {
	dc.mu.Lock()
	defer dc.mu.Unlock()
	return dc.localReason
}

// reason is the reason this channel ended with: the one the outer link gave it,
// else the one the host spoke.
func (dc *directChannel) reason() string {
	if r := dc.local(); r != "" {
		return r
	}
	return dc.ch.Reason()
}

// Reason returns the closed-set reason the channel ended with, or "".
func (dc *directChannel) Reason() string { return dc.reason() }

// ID returns the channel's wire identifier on the nested tunnel.
func (dc *directChannel) ID() uint32 { return dc.ch.ID() }

// Done is closed when the channel ends for any reason.
func (dc *directChannel) Done() <-chan struct{} { return dc.ch.Done() }

// Close closes the channel because the person asked to, which is never a road
// change.
func (dc *directChannel) Close() error {
	dc.closedLocally.Store(true)
	return dc.ch.Close()
}

func (dc *directChannel) Read(p []byte) (int, error) {
	n, err := dc.ch.Read(p)
	if err != nil {
		return n, dc.endError(err, io.EOF)
	}
	return n, nil
}

func (dc *directChannel) Write(p []byte) (int, error) {
	n, err := dc.ch.Write(p)
	if err != nil {
		return n, dc.endError(err, io.ErrClosedPipe)
	}
	return n, nil
}

// endError says how a direct channel ended. ANY end that is not a reason the
// host spoke and not the person closing it is a planned redial, because the
// only things that can end it are the direct tunnel dying and the outer link
// moving the view, and both are answered the same way: open the channel again
// on the road that carries now.
func (dc *directChannel) endError(err, plain error) error {
	if r := dc.local(); r != "" {
		return &ChannelError{Op: "close", Why: r, Err: plain}
	}
	var ce *ChannelError
	if errors.As(err, &ce) && ce != nil && ce.Why != "" {
		return err
	}
	if dc.closedLocally.Load() || dc.l.isClosed() {
		return err
	}
	if dc.ch.Reason() != "" {
		return err
	}
	return &ChannelError{Op: "close", Why: ReasonRoadChange, Err: plain}
}

func (l *Link) isClosed() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.closed
}

// stampMuxChannels gives every channel of mux a local reason and marks it
// ended, so that no frame is written for it and Channel.Reason() reports the
// reason. It is how an idle detach ends channels "with nothing sent first".
func stampMuxChannels(mux *Mux, reason string) {
	mux.mu.Lock()
	defer mux.mu.Unlock()
	for _, ch := range mux.channels {
		if ch.reason == "" {
			ch.reason = reason
		}
		ch.ended = true
	}
}

// detachWithReason closes a link for good with a reason, sending nothing and
// telling nobody: it stamps the mux channels, closes the mux and stops the
// loop. The outer link uses it on its nested direct link when it goes idle.
func (l *Link) detachWithReason(reason string) {
	var mux *Mux
	func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.closed {
			return
		}
		l.closed = true
		l.stopLivenessTimerLocked()
		l.stopRedialTimerLocked()
		mux = l.mux
		l.mux = nil
		l.channels = make(map[uint32]*trackedChannel)
	}()
	l.stopOnce.Do(func() {
		close(l.stopCh)
	})
	if mux != nil {
		stampMuxChannels(mux, reason)
		_ = mux.Close()
	}
}

// heardAt is when the link last heard anything from its host. The outer link
// reads it from the nested link to see the first missed pong.
func (l *Link) heardAt() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastHeard
}

// snapshotState is the link's state without the side effects State() has, so
// the outer link can read the nested one while holding its own lock.
func (l *Link) snapshotState() State {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state
}

// bumpRoadGenLocked counts an idle detach as a change of road, but only on a
// link that has a direct road to change: a link without one behaves exactly as
// it did before there was one.
func (l *Link) bumpRoadGenLocked() {
	if l.cfg.Direct != nil {
		l.roadGen++
	}
}

// relayEstablishedLocked records what a new relay tunnel says about the host
// and starts the ladder over. A new tunnel is a new host session, so an
// attempt that began on the old one is abandoned and the waits begin again.
func (l *Link) relayEstablishedLocked(hostDirect bool) {
	l.hostDirect = hostDirect
	l.abortAttemptLocked()
	l.dfails = 0
	l.sbProbeAt = time.Time{}
	l.releaseStandbyWaitLocked()
}

// relayLostLocked records that the relay tunnel is gone for a reason that ends
// the view's use of it: whatever attempt was running on it cannot succeed.
func (l *Link) relayLostLocked() {
	l.hostDirect = false
	l.abortAttemptLocked()
	l.sbProbeAt = time.Time{}
	l.releaseStandbyWaitLocked()
}

// abortAttemptLocked abandons the attempt that is running, if any, and the
// timer that would start the next one. The attempt's goroutine closes the nested
// link it was building, outside the lock, when its dial returns.
func (l *Link) abortAttemptLocked() {
	l.attemptTok++
	if l.dtimer != nil {
		l.dtimer.Stop()
		l.dtimer = nil
	}
	if run := l.drun; run != nil {
		run.cancel()
		l.drun = nil
		if l.direct == run.nested && l.carrying != RoadDirect {
			l.direct = nil
		}
	}
}

func (l *Link) stopProbeTimerLocked() {
	if l.ptimer != nil {
		l.ptimer.Stop()
		l.ptimer = nil
	}
}

// dropDirectLocked takes the direct road away entirely: no attempt, no timer,
// no nested link, nothing carrying. It returns the nested link, which the
// caller closes after releasing the lock.
func (l *Link) dropDirectLocked() *Link {
	nested := l.direct
	l.abortAttemptLocked()
	l.stopProbeTimerLocked()
	l.sbProbeAt = time.Time{}
	l.releaseStandbyWaitLocked()
	l.direct = nil
	l.directMux = nil
	l.carrying = RoadRelay
	l.dchannels = make(map[*directChannel]struct{})
	return nested
}

// directShownLocked updates what State() reports from a direct posture and
// returns the one state to announce, or nil. Road is always "direct" here.
func (l *Link) directShownLocked(next State) *State {
	next.Road = RoadDirect
	if sameShown(l.shown, next) {
		return nil
	}
	l.shown = next
	cp := next
	return &cp
}

// showRelayLocked makes State() report the relay's own state again and returns
// the one state to announce, or nil.
func (l *Link) showRelayLocked() *State {
	prev := l.shown
	l.shown = l.state
	if sameShown(prev, l.state) {
		return nil
	}
	cp := l.state
	return &cp
}

// newNestedLink builds the link that will carry the direct road. Its Dial is the
// direct implementation's, signalling through this link's relay tunnel; its
// limits are the direct road's (a one-second ping and a three-second death); it
// is always active, because that ping IS its liveness and costs the relay
// nothing; and its OnState is private to this link.
func (l *Link) newNestedLink() *Link {
	var nested *Link
	nested = NewLink(LinkConfig{
		Home:     l.cfg.Home,
		Identity: l.cfg.Identity,
		Host:     l.cfg.Host,
		HostName: l.cfg.HostName,
		Clock:    l.clock,
		Limits:   DirectLimits(),
		Active:   func() bool { return true },
		Dial: func(ctx context.Context) (io.ReadWriteCloser, error) {
			if l.cfg.Direct == nil || l.cfg.Direct.Dial == nil {
				return nil, &ReasonError{Host: l.cfg.HostName, Err: errors.New("livedoor: missing direct Dial function")}
			}
			return l.cfg.Direct.Dial(ctx, outerSignaller{l})
		},
		OnState: func(s State) { l.directState(nested, s) },
	})
	nested.noAutoRedial = true
	return nested
}

// directEligibleLocked says whether an attempt may start now: the host offered
// a direct road on THIS relay tunnel, the relay carries and is alive, a view is
// open, the link is not idle, revoked or closed, and no attempt is running.
func (l *Link) directEligibleLocked() bool {
	if l.cfg.Direct == nil || l.cfg.Direct.Dial == nil || !l.hostDirect {
		return false
	}
	if l.carrying == RoadDirect || l.closed || l.revoked || l.drun != nil {
		return false
	}
	if l.mux == nil || len(l.channels) == 0 || l.state.Kind == Idle {
		return false
	}
	select {
	case <-l.mux.Done():
		return false
	default:
	}
	return true
}

// maybeScheduleDirectAttempt arms the one timer that starts the next attempt,
// if an attempt is allowed and none is already waiting. The first attempt of a
// relay tunnel waits DirectSettle after the first channel opens, so the first
// paint never waits on the path finding; later ones wait DirectBackoff after a
// failure.
func (l *Link) maybeScheduleDirectAttempt() {
	if l.cfg.Direct == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.armDirectAttemptLocked()
}

func (l *Link) armDirectAttemptLocked() {
	if l.dtimer != nil || !l.directEligibleLocked() {
		return
	}
	wait := DirectSettle
	if l.dfails > 0 && len(DirectBackoff) > 0 {
		idx := l.dfails - 1
		if idx >= len(DirectBackoff) {
			idx = len(DirectBackoff) - 1
		}
		wait = DirectBackoff[idx]
	}
	l.dtimer = l.clock.NewTimer(wait)
	l.wake()
}

// handleAttemptTick starts an attempt when the attempt timer fires and an
// attempt is still allowed. It runs on the link's loop, so the attempt itself
// runs on its own goroutine.
func (l *Link) handleAttemptTick(fired Timer) {
	var run *directRun
	func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.dtimer == nil || l.dtimer != fired {
			return
		}
		l.dtimer = nil
		if !l.directEligibleLocked() {
			return
		}
		l.attemptTok++
		ctx, cancel := context.WithCancel(context.Background())
		run = &directRun{
			tok:    l.attemptTok,
			nested: l.newNestedLink(),
			relay:  l.mux,
			ctx:    ctx,
			cancel: cancel,
		}
		l.drun = run
		l.direct = run.nested
	}()
	if run == nil {
		return
	}
	guard.Go("livedoor.link.direct-attempt", func() {
		l.runDirectAttempt(run)
	})
}

// runDirectAttempt dials the nested link, the one place it is ever dialled, and
// then either swaps roads or fails silently. The attempt gets DirectConnectWithin
// on the link's own clock, and it ends early when the link closes or the relay
// tunnel it started on ends.
func (l *Link) runDirectAttempt(run *directRun) {
	deadline := l.clock.NewTimer(DirectConnectWithin)
	finished := make(chan struct{})
	guard.Go("livedoor.link.direct-deadline", func() {
		defer deadline.Stop()
		select {
		case <-deadline.C():
			run.cancel()
		case <-run.relay.Done():
			run.cancel()
		case <-l.stopCh:
			run.cancel()
		case <-finished:
		}
	})

	run.nested.startLoop()
	nmux, err := run.nested.ensureConnected(run.ctx)
	close(finished)
	// The context is cancelled only when the attempt fails: a direct tunnel that
	// was built with it must outlive the attempt, and an implementation may tie
	// the connection's lifetime to the context it was dialled with.
	if err != nil {
		l.directAttemptFailed(run, err)
		return
	}
	l.directBecameLive(run, nmux)
}

// directAttemptFailed ends an attempt without a word to anyone: no state, no
// notification, no banner. It counts the failure and arms the next wait, unless
// the attempt was abandoned on purpose (a stale token), in which case whatever
// abandoned it already decided what happens next.
func (l *Link) directAttemptFailed(run *directRun, err error) {
	run.cancel()
	func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.drun == run {
			l.drun = nil
		}
		if l.direct == run.nested && l.carrying != RoadDirect {
			l.direct = nil
		}
		if l.attemptTok == run.tok && !l.closed {
			l.dfails++
			l.armDirectAttemptLocked()
		}
	}()
	run.nested.Close()
	if reason, ok := ReasonOf(err); ok && reason == ReasonRemoved {
		// The host said the removal itself on the direct road, which is the same
		// news as on the relay: the whole link is revoked.
		l.noteOpenRefusal(ReasonRemoved)
	}
}

// directBecameLive swaps the carrying road to the direct one.
//
// THE ORDER IS A LAW, and it is what makes a swap safe: (i) the direct road is
// recorded as the one that carries and the single state change is computed, so
// every open from this moment on goes direct; (ii) every channel the relay holds
// is given the local reason road-change and closed with a normal CLOSE, which
// ends its next Read or Write with a planned redial; (iii) the state change is
// delivered only after the locks are released. The relay tunnel stays attached
// and SILENT: no ping and no frame but those CLOSEs.
func (l *Link) directBecameLive(run *directRun, nmux *Mux) {
	var (
		notify  *State
		swapped []*trackedChannel
		ok      bool
	)
	func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		snap := run.nested.snapshotState()
		if l.closed || l.revoked || l.attemptTok != run.tok || l.carrying == RoadDirect ||
			l.direct != run.nested || l.mux == nil || l.mux != run.relay ||
			l.state.Kind == Idle || len(l.channels) == 0 ||
			(snap.Kind != Live && snap.Kind != WatchOnly) {
			return
		}
		select {
		case <-nmux.Done():
			return
		default:
		}
		ok = true
		now := l.clock.Now()
		l.drun = nil

		l.carrying = RoadDirect
		l.roadGen++
		l.directMux = nmux
		l.directSince = now
		l.sbProbed = false
		l.sbProbeAt = time.Time{}
		l.releaseStandbyWaitLocked()
		notify = l.directShownLocked(snap)
		l.stopLivenessTimerLocked()

		for _, tc := range l.channels {
			tc.setLocalReason(ReasonRoadChange)
			swapped = append(swapped, tc)
		}
		l.channels = make(map[uint32]*trackedChannel)
		l.resetLivenessTimerLocked(now)
		l.armProbeLocked()
	}()
	if !ok {
		l.directAttemptFailed(run, nil)
		return
	}

	for _, tc := range swapped {
		// OnChannelEnd is never told about a road change: the channel's end is the
		// link's own doing and not a thing the host said.
		tc.endOnce.Do(func() {})
		_ = tc.ch.Close()
	}
	nested := run.nested
	guard.Go("livedoor.link.direct-watch", func() {
		select {
		case <-nmux.Done():
			l.directWentDown(nested, nmux)
		case <-l.stopCh:
		}
	})
	l.wake()
	if notify != nil && l.cfg.OnState != nil {
		l.cfg.OnState(*notify)
	}
}

// directState consumes the nested link's OnState, which is private to this
// link. It only matters while the direct road carries: a nested link that is no
// longer Live ends the direct road, and one that changed permit updates what
// the person sees. A callback from a nested link that is not the current one is
// ignored, so an old tunnel's death cannot demote its replacement.
func (l *Link) directState(nested *Link, s State) {
	var (
		notify *State
		down   bool
	)
	func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.direct != nested || l.carrying != RoadDirect {
			return
		}
		switch s.Kind {
		case Live, WatchOnly:
			notify = l.directShownLocked(s)
		default:
			down = true
		}
	}()
	if notify != nil && l.cfg.OnState != nil {
		l.cfg.OnState(*notify)
	}
	if down {
		l.directWentDown(nested, nil)
	}
}

// directWentDown is the fallback: the direct road is gone and the relay carries
// again. nested and mux say which tunnel went down (mux nil means any), and a
// report about a tunnel that is not the current one is ignored.
//
// THE ORDER IS A LAW: (i) the relay is recorded as carrying and the one state
// change is computed; (ii) every direct channel is given the local reason
// road-change, so its next Read or Write reports a planned redial, and the
// nested link is closed after the lock is released; (iii) the ladder counts a
// failure unless the direct road had been up for DirectStableAfter; (iv) the
// relay standby is made usable: kept if it answered the probe, retired if it
// did not, and dialled on demand when there is none.
func (l *Link) directWentDown(nested *Link, mux *Mux) {
	var (
		notify    *State
		retire    *Mux
		ping      *Mux
		pingSeq   uint32
		dialRelay bool
	)
	func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.closed || l.carrying != RoadDirect || l.direct != nested || (mux != nil && l.directMux != mux) {
			return
		}
		now := l.clock.Now()
		stable := now.Sub(l.directSince) >= DirectStableAfter

		l.carrying = RoadRelay
		l.roadGen++
		l.direct = nil
		l.directMux = nil
		l.stopProbeTimerLocked()
		notify = l.showRelayLocked()

		for dc := range l.dchannels {
			if dc.ch.Reason() == "" {
				dc.setLocalReason(ReasonRoadChange)
			}
		}
		l.dchannels = make(map[*directChannel]struct{})

		if stable {
			l.dfails = 0
		} else {
			l.dfails++
		}

		standby := l.mux
		if standby != nil {
			select {
			case <-standby.Done():
				l.mux = nil
				standby = nil
			default:
			}
		}
		switch {
		case standby == nil:
			// Nothing to do here: the next open dials the relay, with phase 1's own
			// dial, backoff and states.
		case !l.sbProbeAt.IsZero():
			if !now.Before(l.sbProbeAt.Add(DirectPingEvery)) {
				retire = standby
				l.mux = nil
				l.sbProbeAt = time.Time{}
				l.releaseStandbyWaitLocked()
				l.relayLostLocked()
				dialRelay = true
			}
		case !l.sbProbed:
			// The direct tunnel ended abruptly, before the first missed pong, so the
			// standby has not been asked yet. Ask it now; it gets DirectPingEvery
			// to answer before it is retired.
			l.sbProbed = true
			l.pingSeq++
			pingSeq = l.pingSeq
			l.sbProbeAt = now
			l.sbWait = make(chan struct{})
			ping = standby
		}
		l.armDirectAttemptLocked()
	}()
	// Closing the nested link is right when it was the current one, and harmless
	// when the report was stale: either way it is a tunnel nobody uses.
	nested.Close()
	if retire != nil {
		_ = retire.Close()
	}
	l.wake()
	if notify != nil && l.cfg.OnState != nil {
		l.cfg.OnState(*notify)
	}
	if ping != nil {
		_ = ping.SendPing(pingSeq)
		guard.Go("livedoor.link.standby", func() {
			if l.settleStandby(context.Background()) {
				_, _ = l.ensureConnected(context.Background())
			}
		})
	}
	if dialRelay {
		guard.Go("livedoor.link.standby-dial", func() {
			_, _ = l.ensureConnected(context.Background())
		})
	}
}

// armProbeLocked arms the ticker that watches the direct tunnel for its first
// missed pong. It exists only while the direct road carries.
func (l *Link) armProbeLocked() {
	if l.ptimer != nil || l.carrying != RoadDirect || l.closed || l.revoked {
		return
	}
	l.ptimer = l.clock.NewTimer(DirectPingEvery)
	l.wake()
}

// handleDirectProbeTick is the standby probe. The nested link pings every
// second and is dead three seconds after it last heard anything. At the FIRST
// missed pong, when it has heard nothing for more than one ping period, this
// asks the relay standby once whether it is alive, or starts dialling the relay
// at once if there is no standby, so the relay is up by the time the direct road
// is declared dead. The relay is pinged at most once per outage, and never
// otherwise while the direct road carries.
func (l *Link) handleDirectProbeTick(fired Timer) {
	var (
		ping    *Mux
		pingSeq uint32
		dial    bool
	)
	func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.ptimer == nil || l.ptimer != fired {
			return
		}
		l.ptimer = nil
		if l.carrying != RoadDirect || l.direct == nil || l.closed || l.revoked {
			return
		}
		l.armProbeLocked()
		now := l.clock.Now()
		if l.sbProbed || now.Sub(l.direct.heardAt()) <= DirectPingEvery {
			return
		}
		l.sbProbed = true
		standby := l.mux
		if standby != nil {
			select {
			case <-standby.Done():
				standby = nil
			default:
			}
		}
		if standby == nil {
			dial = true
			return
		}
		l.pingSeq++
		pingSeq = l.pingSeq
		l.sbProbeAt = now
		l.sbWait = make(chan struct{})
		ping = standby
	}()
	if ping != nil {
		_ = ping.SendPing(pingSeq)
	}
	if dial {
		guard.Go("livedoor.link.standby-dial", func() {
			_, _ = l.ensureConnected(context.Background())
		})
	}
}

// standbyAnsweredLocked records that the relay standby said something, which is
// the answer to the probe: the relay sends nothing on its own while the direct
// road carries.
func (l *Link) standbyAnsweredLocked(now time.Time) {
	if l.sbProbeAt.IsZero() || now.Before(l.sbProbeAt) {
		return
	}
	l.sbProbeAt = time.Time{}
	l.releaseStandbyWaitLocked()
}

func (l *Link) releaseStandbyWaitLocked() {
	if l.sbWait != nil {
		close(l.sbWait)
		l.sbWait = nil
	}
}

// settleStandby waits for the answer to an outstanding standby probe and
// retires the standby if there is none within DirectPingEvery of the probe. It
// reports whether it retired one, in which case the caller dials the relay
// through ensureConnected (which serialises dials, so there is only ever one).
// With no probe outstanding it returns at once.
func (l *Link) settleStandby(ctx context.Context) bool {
	if l.cfg.Direct == nil {
		return false
	}
	for {
		var (
			retired *Mux
			wait    chan struct{}
			until   time.Time
		)
		func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.carrying == RoadDirect || l.closed || l.sbProbeAt.IsZero() || l.mux == nil {
				return
			}
			deadline := l.sbProbeAt.Add(DirectPingEvery)
			if !l.clock.Now().Before(deadline) {
				retired = l.mux
				l.mux = nil
				l.stopLivenessTimerLocked()
				l.relayLostLocked()
				return
			}
			wait = l.sbWait
			until = deadline
		}()
		if retired != nil {
			_ = retired.Close()
			return true
		}
		if wait == nil {
			return false
		}
		t := l.clock.NewTimer(until.Sub(l.clock.Now()))
		select {
		case <-wait:
			t.Stop()
		case <-t.C():
		case <-ctx.Done():
			t.Stop()
			return false
		case <-l.stopCh:
			t.Stop()
			return false
		}
	}
}

// openDirect opens the chat on the direct road when it carries. It reports
// handled == false when the caller should go on to the relay: the direct road
// does not carry, or its tunnel died under the open (in which case the road is
// marked down first, so the relay path finds the relay carrying). A reason the
// host spoke is returned unchanged, because the relay would give the same
// answer.
func (l *Link) openDirect(ctx context.Context, chat string) (rwc io.ReadWriteCloser, handled bool, err error) {
	var (
		nested *Link
		mux    *Mux
		gen    uint64
	)
	func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.carrying != RoadDirect || l.closed || l.revoked || l.direct == nil || l.directMux == nil {
			return
		}
		nested, mux, gen = l.direct, l.directMux, l.roadGen
	}()
	if mux == nil {
		return nil, false, nil
	}
	select {
	case <-mux.Done():
		l.directWentDown(nested, mux)
		return nil, false, nil
	default:
	}

	ch, oerr := mux.Open(ctx, chat)
	if oerr != nil {
		var ref *RefusedError
		if errors.As(oerr, &ref) && ref != nil {
			l.noteOpenRefusal(ref.Reason)
			return nil, true, &ReasonError{
				Reason: ref.Reason,
				Host:   l.cfg.HostName,
				Err:    oerr,
			}
		}
		if ctx.Err() != nil {
			return nil, true, l.wrapReasonError(oerr)
		}
		l.directWentDown(nested, mux)
		return nil, false, nil
	}

	dc := &directChannel{l: l, ch: ch, mux: mux, chat: chat}
	var (
		stale  bool
		closed bool
	)
	func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.closed {
			closed = true
			return
		}
		if l.roadGen != gen || l.carrying != RoadDirect || l.direct != nested {
			stale = true
			return
		}
		now := l.clock.Now()
		if l.channelCountLocked() == 0 && now.After(l.idleSince) {
			l.idleSince = now
		}
		l.dchannels[dc] = struct{}{}
		l.resetLivenessTimerLocked(now)
	}()
	if stale {
		// A fallback committed while this channel was opening and so could not
		// have seen it. It is closed outside the lock and the open is repeated.
		_ = ch.Close()
		return nil, true, errRoadMoved
	}
	if closed {
		return dc, true, nil
	}
	l.wake()
	guard.Go("livedoor.link.dchannel", func() {
		select {
		case <-ch.Done():
			l.handleDirectChannelDone(dc)
		case <-l.stopCh:
		}
	})
	return dc, true, nil
}

// handleDirectChannelDone is handleChannelDone for a channel of the direct road:
// a reason the host spoke has the same effect on the link it has on the relay,
// and a road change is never reported as the end of a channel.
func (l *Link) handleDirectChannelDone(dc *directChannel) {
	reason := dc.reason()
	now := l.clock.Now()
	var (
		notify *State
		onEnd  func(chat, reason string)
	)
	func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		delete(l.dchannels, dc)
		onEnd = l.onChannelEnd
		current := l.carrying == RoadDirect && l.directMux == dc.mux
		notify = l.applyChannelReasonLocked(reason, now, current, true)
		if current && !l.closed && !l.revoked {
			l.resetLivenessTimerLocked(now)
		}
	}()
	l.wake()

	if reason != "" && reason != ReasonRoadChange && onEnd != nil {
		dc.endOnce.Do(func() {
			onEnd(dc.chat, reason)
		})
	}
	if notify != nil && l.cfg.OnState != nil {
		l.cfg.OnState(*notify)
	}
	l.settleRevocation()
}

// settleRevocation finishes a revocation that was recorded as a relay write:
// when the host has removed this viewer, no direct road may carry, be built or
// be attempted. It closes the nested link, ends the direct channels with the
// reason, and announces the revoked state if a direct road was what a person
// was looking at. It does nothing on a link that has no direct road.
func (l *Link) settleRevocation() {
	if l.cfg.Direct == nil {
		return
	}
	var (
		nested *Link
		notify *State
		ended  []*directChannel
		onEnd  func(chat, reason string)
		reason string
	)
	func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if !l.revoked {
			return
		}
		if l.direct == nil && l.carrying != RoadDirect && l.drun == nil && l.dtimer == nil &&
			l.ptimer == nil && len(l.dchannels) == 0 {
			return
		}
		reason = l.lastReason
		if reason == "" {
			reason = ReasonRemoved
		}
		onEnd = l.onChannelEnd
		for dc := range l.dchannels {
			dc.setLocalReason(reason)
			ended = append(ended, dc)
		}
		wasDirect := l.carrying == RoadDirect
		nested = l.dropDirectLocked()
		if wasDirect {
			l.roadGen++
			notify = l.showRelayLocked()
		}
	}()
	if nested != nil {
		nested.Close()
	}
	if onEnd != nil {
		for _, dc := range ended {
			dc := dc
			dc.endOnce.Do(func() {
				onEnd(dc.chat, reason)
			})
		}
	}
	if notify != nil && l.cfg.OnState != nil {
		l.cfg.OnState(*notify)
	}
}

// resetDirectIdleTimerLocked arms the one timer idleness needs while the direct
// road carries. There is no repeating relay ping and no relay liveness timer
// here: only a one-shot at the moment IdleDetach would elapse, or, while the
// person is active, a local re-check, which sends nothing to anyone.
func (l *Link) resetDirectIdleTimerLocked(now time.Time) {
	if l.channelCountLocked() == 0 {
		l.stopLivenessTimerLocked()
		return
	}
	if l.isActiveLocked(now) {
		next := now.Add(l.limits.PingEvery)
		if l.cfg.Active == nil || !l.cfg.Active() {
			if end := l.lastTouch.Add(l.limits.ActiveWindow); end.After(now) {
				next = end
			}
		}
		l.setLivenessTimerAtLocked(now, next)
		return
	}
	if l.idleSince.IsZero() {
		l.idleSince = now
	}
	l.setLivenessTimerAtLocked(now, l.idleSince.Add(l.limits.IdleDetach))
}

// directDetach is everything an idle detach of the direct road must do after the
// lock is released.
type directDetach struct {
	relay   *Mux
	nested  *Link
	relayCh []*trackedChannel
	directC []*directChannel
	onEnd   func(chat, reason string)
	notify  *State
}

// handleDirectIdleTick decides idleness while the direct road carries. Idle is
// decided over relay and direct channels alike, and it belongs to this link, not
// to the nested link, whose ping would otherwise keep a view alive forever.
func (l *Link) handleDirectIdleTick(now time.Time) {
	var detach *directDetach
	func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.carrying != RoadDirect || l.closed || l.revoked || l.channelCountLocked() == 0 {
			return
		}
		if l.isActiveLocked(now) || now.Before(l.idleSince.Add(l.limits.IdleDetach)) {
			l.resetDirectIdleTimerLocked(now)
			return
		}
		detach = l.detachIdleDirectLocked(now)
	}()
	if detach != nil {
		l.finishDirectDetach(detach)
	}
}

// detachIdleDirectLocked records an idle detach of BOTH roads: the relay mux
// and the nested link, one close each, nothing sent first. The outer state
// becomes Idle on the relay road, and the ladder and any attempt are cancelled.
func (l *Link) detachIdleDirectLocked(now time.Time) *directDetach {
	d := &directDetach{relay: l.mux, nested: l.direct, onEnd: l.onChannelEnd}
	l.mux = nil
	l.carrying = RoadRelay
	l.roadGen++
	l.direct = nil
	l.directMux = nil
	l.stopLivenessTimerLocked()
	l.stopRedialTimerLocked()
	l.stopProbeTimerLocked()
	l.relayLostLocked()
	l.dfails = 0

	for _, tc := range l.channels {
		tc.setLocalReason(ReasonIdle)
		d.relayCh = append(d.relayCh, tc)
	}
	l.channels = make(map[uint32]*trackedChannel)
	for dc := range l.dchannels {
		dc.setLocalReason(ReasonIdle)
		d.directC = append(d.directC, dc)
	}
	l.dchannels = make(map[*directChannel]struct{})

	d.notify = l.setStateLocked(State{
		Kind:   Idle,
		Reason: ReasonIdle,
		Since:  now,
		Permit: l.permit,
		Road:   l.road,
	})
	return d
}

func (l *Link) finishDirectDetach(d *directDetach) {
	if d.relay != nil {
		stampMuxChannels(d.relay, ReasonIdle)
		_ = d.relay.Close()
	}
	if d.nested != nil {
		d.nested.detachWithReason(ReasonIdle)
	}
	if d.onEnd != nil {
		for _, tc := range d.relayCh {
			tc := tc
			tc.endOnce.Do(func() {
				d.onEnd(tc.chat, ReasonIdle)
			})
		}
		for _, dc := range d.directC {
			dc := dc
			dc.endOnce.Do(func() {
				d.onEnd(dc.chat, ReasonIdle)
			})
		}
	}
	l.wake()
	if d.notify != nil && l.cfg.OnState != nil {
		l.cfg.OnState(*d.notify)
	}
}
