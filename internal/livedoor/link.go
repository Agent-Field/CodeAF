package livedoor

import (
	"context"
	"errors"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relay"
)

const (
	reconnectBase = 1 * time.Second
	reconnectCap  = 15 * time.Second
)

// LinkConfig configures a viewer-side multiplexed Link to one host device.
// Dial returns an io.ReadWriteCloser that has already completed the tunnel
// handshake and device hello.
type LinkConfig struct {
	Home     string
	Dial     func(context.Context) (io.ReadWriteCloser, error)
	Identity string
	Host     string
	Clock    Clock
	Limits   Limits
	Active   func() bool
	OnState  func(State)
	HostName string
}

type trackedChannel struct {
	ch          *Channel
	chat        string
	endOnce     sync.Once
	mu          sync.Mutex
	localReason string
}

func (tc *trackedChannel) setLocalReason(reason string) {
	tc.mu.Lock()
	if tc.localReason == "" {
		tc.localReason = reason
	}
	tc.mu.Unlock()
}

func (tc *trackedChannel) reason() string {
	tc.mu.Lock()
	r := tc.localReason
	tc.mu.Unlock()
	if r != "" {
		return r
	}
	return tc.ch.Reason()
}

type linkChannel struct {
	*Channel
	tc *trackedChannel
}

func (lc *linkChannel) Reason() string {
	return lc.tc.reason()
}

func (lc *linkChannel) Read(p []byte) (int, error) {
	n, err := lc.Channel.Read(p)
	if err != nil {
		if r := lc.tc.reason(); r != "" && errors.Is(err, ErrLinkClosed) {
			return n, &ChannelError{Op: "close", Why: r, Err: io.EOF}
		}
	}
	return n, err
}

func (lc *linkChannel) Write(p []byte) (int, error) {
	n, err := lc.Channel.Write(p)
	if err != nil {
		if r := lc.tc.reason(); r != "" && errors.Is(err, ErrLinkClosed) {
			return n, &ChannelError{Op: "close", Why: r, Err: io.ErrClosedPipe}
		}
	}
	return n, err
}

// Link maintains at most one multiplexed viewer tunnel to a host computer,
// opening per-chat channels over it, sending liveness pings only while active,
// and detaching cleanly after IdleDetach of quiet.
type Link struct {
	cfg    LinkConfig
	clock  Clock
	limits Limits

	dialMu        sync.Mutex
	framesWritten atomic.Int64

	mu              sync.Mutex
	state           State
	permit          pair.Permit
	road            string
	mux             *Mux
	channels        map[uint32]*trackedChannel
	onChannelEnd    func(chat, reason string)
	lastHeard       time.Time
	lastTouch       time.Time
	lastActive      time.Time
	idleSince       time.Time
	lastPingSent    time.Time
	probeSentAt     time.Time
	probeDeadline   time.Time
	pingSeq         uint32
	livenessTimer   Timer
	livenessAt      time.Time
	redialTimer     Timer
	redialAt        time.Time
	retryCount      int
	baseWait        time.Duration
	offlineCap      bool
	immediateRedial bool
	budgetExhausted bool
	revoked         bool
	lastReason      string
	closed          bool

	loopOnce sync.Once
	wakeCh   chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once
}

// NewLink constructs a Link for one host device without starting any goroutine
// or dialling until the first Opener or reconnect Touch call.
func NewLink(cfg LinkConfig) *Link {
	clk := cfg.Clock
	if clk == nil {
		clk = RealClock()
	}
	lim := cfg.Limits.withDefaults()
	return &Link{
		cfg:      cfg,
		clock:    clk,
		limits:   lim,
		state:    State{Kind: Connecting, Since: clk.Now()},
		channels: make(map[uint32]*trackedChannel),
		baseWait: reconnectBase,
		wakeCh:   make(chan struct{}, 1),
		stopCh:   make(chan struct{}),
	}
}

// OnChannelEnd registers a callback invoked once per ended channel with its
// closed-set reason string.
func (l *Link) OnChannelEnd(fn func(chat, reason string)) {
	l.mu.Lock()
	l.onChannelEnd = fn
	l.mu.Unlock()
}

// State returns the link's current State snapshot.
func (l *Link) State() State {
	l.mu.Lock()
	mux := l.mux
	l.mu.Unlock()
	if mux != nil {
		select {
		case <-mux.Done():
			l.handleTunnelDone(mux)
		default:
		}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.state
}

// Close permanently shuts down the link, stops any pending liveness or redial
// timers, and closes the active tunnel if one is open.
func (l *Link) Close() {
	var mux *Mux
	l.mu.Lock()
	if !l.closed {
		l.closed = true
		l.stopLivenessTimerLocked()
		l.stopRedialTimerLocked()
		mux = l.mux
		l.mux = nil
		l.channels = make(map[uint32]*trackedChannel)
	}
	l.mu.Unlock()

	l.stopOnce.Do(func() {
		close(l.stopCh)
	})
	if mux != nil {
		_ = mux.Close()
	}
}

// Touch records a person action (key, scroll, or send), reconnecting the link
// if it had detached to Idle, or sending a single liveness probe ping if the
// link was connected and quiet for at least PingEvery.
func (l *Link) Touch() {
	now := l.clock.Now()
	l.mu.Lock()
	if l.closed || l.revoked {
		l.mu.Unlock()
		return
	}
	wasIdle := l.state.Kind == Idle
	mux := l.mux
	wasActive := l.isActiveLocked(now)
	l.lastTouch = now
	if now.After(l.idleSince) {
		l.idleSince = now
	}

	var (
		sentProbe    bool
		beforeWrites int64
	)
	if mux != nil && !wasActive {
		if now.Sub(l.lastHeard) >= l.limits.PingEvery {
			l.pingSeq++
			seq := l.pingSeq
			l.lastPingSent = now
			l.probeSentAt = now
			l.probeDeadline = now.Add(l.limits.DeadAfter)
			beforeWrites = l.framesWritten.Load()
			_ = mux.SendPing(seq)
			sentProbe = true
		}
	}
	if mux != nil {
		l.resetLivenessTimerLocked(now)
	}
	l.mu.Unlock()
	l.wake()

	if sentProbe && mux != nil {
		// Wait briefly for the mux writer goroutine to complete writeFrame so
		// the probe ping is on the wire before Touch returns.
		deadline := time.Now().Add(50 * time.Millisecond)
		for l.framesWritten.Load() == beforeWrites {
			select {
			case <-mux.Done():
				goto probeDone
			default:
			}
			if time.Now().After(deadline) {
				break
			}
			runtime.Gosched()
		}
	probeDone:
	}

	if wasIdle && mux == nil {
		l.startLoop()
		_, _ = l.ensureConnected(context.Background())
	}
}

// Opener returns a dialer for chat matching remote.Roaming.Dial.
func (l *Link) Opener(chat string) func() (io.ReadWriteCloser, error) {
	openCtx := l.OpenerContext(chat)
	return func() (io.ReadWriteCloser, error) {
		return openCtx(context.Background())
	}
}

// OpenerContext returns a context-aware channel opener for chat that connects
// the underlying host tunnel on first use or after detach and opens a new mux
// channel for chat.
func (l *Link) OpenerContext(chat string) func(context.Context) (io.ReadWriteCloser, error) {
	return func(ctx context.Context) (io.ReadWriteCloser, error) {
		if ctx == nil {
			ctx = context.Background()
		}
		l.startLoop()

		mux, err := l.ensureConnected(ctx)
		if err != nil {
			return nil, l.wrapReasonError(err)
		}

		ch, err := mux.Open(ctx, chat)
		if err != nil {
			var ref *RefusedError
			if errors.As(err, &ref) && ref != nil {
				l.noteOpenRefusal(ref.Reason)
				return nil, &ReasonError{
					Reason: ref.Reason,
					Host:   l.cfg.HostName,
					Err:    err,
				}
			}
			select {
			case <-mux.Done():
				l.handleTunnelDone(mux)
			default:
			}
			return nil, l.wrapReasonError(err)
		}

		tc := &trackedChannel{
			ch:   ch,
			chat: chat,
		}
		l.mu.Lock()
		if l.mux == mux && !l.closed {
			now := l.clock.Now()
			if len(l.channels) == 0 && now.After(l.idleSince) {
				l.idleSince = now
			}
			l.channels[ch.ID()] = tc
			l.resetLivenessTimerLocked(now)
		}
		l.mu.Unlock()
		l.wake()

		guard.Go("livedoor.link.channel", func() {
			select {
			case <-ch.Done():
				l.handleChannelDone(mux, tc)
			case <-l.stopCh:
			}
		})

		return &linkChannel{Channel: ch, tc: tc}, nil
	}
}

func (l *Link) startLoop() {
	l.loopOnce.Do(func() {
		guard.Go("livedoor.link.loop", l.loop)
	})
}

func (l *Link) wake() {
	select {
	case l.wakeCh <- struct{}{}:
	default:
	}
}

func (l *Link) wrapReasonError(err error) error {
	if err == nil {
		return nil
	}
	var re *ReasonError
	if errors.As(err, &re) {
		return err
	}
	if reason, ok := ReasonOf(err); ok {
		return &ReasonError{
			Reason: reason,
			Host:   l.cfg.HostName,
			Err:    err,
		}
	}
	l.mu.Lock()
	reason := l.lastReason
	if l.state.Reason != "" {
		reason = l.state.Reason
	}
	l.mu.Unlock()
	return &ReasonError{
		Reason: reason,
		Host:   l.cfg.HostName,
		Err:    err,
	}
}

func (l *Link) noteOpenRefusal(reason string) {
	now := l.clock.Now()
	var notify *State
	l.mu.Lock()
	switch reason {
	case ReasonRemoved:
		l.revoked = true
		l.lastReason = ReasonRemoved
		l.stopLivenessTimerLocked()
		l.stopRedialTimerLocked()
		notify = l.setStateLocked(State{
			Kind:   Revoked,
			Reason: ReasonRemoved,
			Since:  now,
			Permit: pair.Permit{},
			Road:   l.road,
		})
	case ReasonBudget:
		l.budgetExhausted = true
		l.permit.Drive = false
		l.lastReason = ReasonBudget
		notify = l.setStateLocked(State{
			Kind:   StateBudget,
			Reason: ReasonBudget,
			Since:  now,
			Permit: l.permit,
			Road:   l.road,
		})
	}
	l.mu.Unlock()
	if notify != nil && l.cfg.OnState != nil {
		l.cfg.OnState(*notify)
	}
}

func (l *Link) ensureConnected(ctx context.Context) (*Mux, error) {
	l.dialMu.Lock()
	defer l.dialMu.Unlock()

	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, ErrLinkClosed
	}
	if l.revoked {
		reason := l.state.Reason
		if reason == "" {
			reason = l.lastReason
		}
		if reason == "" {
			reason = ReasonNotAllowed
		}
		l.mu.Unlock()
		return nil, &ReasonError{Reason: reason, Host: l.cfg.HostName}
	}
	if mux := l.mux; mux != nil {
		select {
		case <-mux.Done():
			l.mu.Unlock()
			l.handleTunnelDone(mux)
		default:
			l.mu.Unlock()
			return mux, nil
		}
	} else {
		l.mu.Unlock()
	}

	return l.dialAndHandshake(ctx)
}

func (l *Link) dialAndHandshake(ctx context.Context) (*Mux, error) {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return nil, ErrLinkClosed
	}
	if l.revoked {
		reason := l.state.Reason
		if reason == "" {
			reason = ReasonNotAllowed
		}
		l.mu.Unlock()
		return nil, &ReasonError{Reason: reason, Host: l.cfg.HostName}
	}
	l.stopRedialTimerLocked()
	l.mu.Unlock()

	if l.cfg.Dial == nil {
		return nil, &ReasonError{Host: l.cfg.HostName, Err: errors.New("livedoor: missing Dial function")}
	}

	rwc, err := l.cfg.Dial(ctx)
	if err != nil {
		l.handleDialFailure(err)
		return nil, l.wrapReasonError(err)
	}

	now := l.clock.Now()
	l.mu.Lock()
	l.lastHeard = now
	l.mu.Unlock()

	mux := NewMux(rwc, Viewer, MuxConfig{
		Clock:          l.clock,
		Limits:         l.limits,
		OnPong:         func(uint32) { l.recordHeard() },
		OnFrameRead:    func(int) { l.recordHeard() },
		OnFrameWritten: func(int) { l.framesWritten.Add(1) },
	})

	helloWait := l.limits.DeadAfter
	if helloWait <= 0 {
		helloWait = DeadAfter
	}
	helloTimer := l.clock.NewTimer(helloWait)
	var (
		ack    HelloAck
		ackErr error
	)
	select {
	case <-mux.helloAckCh:
		ack, _ = mux.HelloAck()
	case <-mux.Done():
		if a, ok := mux.HelloAck(); ok {
			ack = a
		} else {
			ackErr = ErrLinkClosed
		}
	case <-helloTimer.C():
		ackErr = ErrLinkClosed
	case <-ctx.Done():
		ackErr = ctx.Err()
	case <-l.stopCh:
		ackErr = ErrLinkClosed
	}
	helloTimer.Stop()

	if ackErr != nil {
		_ = mux.Close()
		l.handleDialFailure(ackErr)
		return nil, l.wrapReasonError(ackErr)
	}

	now = l.clock.Now()
	permit := pair.Permit{Watch: ack.Watch, Drive: ack.Drive}
	road := ack.Road
	if road == "" {
		road = defaultRoad
	}

	if !ack.Watch {
		_ = mux.Close()
		var notify *State
		l.mu.Lock()
		l.revoked = true
		l.lastReason = ReasonNotAllowed
		l.permit = permit
		l.road = road
		l.stopLivenessTimerLocked()
		l.stopRedialTimerLocked()
		notify = l.setStateLocked(State{
			Kind:   Revoked,
			Reason: ReasonNotAllowed,
			Since:  now,
			Permit: permit,
			Road:   road,
		})
		l.mu.Unlock()
		l.wake()
		if notify != nil && l.cfg.OnState != nil {
			l.cfg.OnState(*notify)
		}
		return nil, &ReasonError{Reason: ReasonNotAllowed, Host: l.cfg.HostName}
	}

	var notify *State
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		_ = mux.Close()
		return nil, ErrLinkClosed
	}
	l.mux = mux
	l.permit = permit
	l.road = road
	l.lastHeard = now
	l.lastPingSent = now
	l.probeSentAt = time.Time{}
	l.probeDeadline = time.Time{}
	if l.idleSince.IsZero() || now.After(l.idleSince) {
		l.idleSince = now
	}
	l.retryCount = 0
	l.baseWait = reconnectBase
	l.offlineCap = false
	l.immediateRedial = false
	l.budgetExhausted = false
	l.lastReason = ""

	kind := Live
	if !ack.Drive {
		kind = WatchOnly
	}
	notify = l.setStateLocked(State{
		Kind:   kind,
		Since:  now,
		Permit: permit,
		Road:   road,
	})
	l.resetLivenessTimerLocked(now)
	l.mu.Unlock()

	l.wake()
	if notify != nil && l.cfg.OnState != nil {
		l.cfg.OnState(*notify)
	}
	return mux, nil
}

func (l *Link) handleDialFailure(err error) {
	now := l.clock.Now()
	var notify *State
	l.mu.Lock()
	if l.closed || l.revoked {
		l.mu.Unlock()
		return
	}
	if reason, ok := ReasonOf(err); ok && (reason == ReasonRemoved || reason == ReasonNotAllowed) {
		l.revoked = true
		l.lastReason = reason
		l.stopLivenessTimerLocked()
		l.stopRedialTimerLocked()
		notify = l.setStateLocked(State{
			Kind:   Revoked,
			Reason: reason,
			Since:  now,
			Permit: pair.Permit{},
			Road:   l.road,
		})
		l.mu.Unlock()
		l.wake()
		if notify != nil && l.cfg.OnState != nil {
			l.cfg.OnState(*notify)
		}
		return
	}

	if errors.Is(err, ErrHostOffline) {
		l.offlineCap = true
		notify = l.setStateLocked(State{
			Kind:   WentOffline,
			Since:  now,
			Permit: l.permit,
			Road:   l.road,
		})
	} else if l.state.Kind != WentOffline {
		notify = l.setStateLocked(State{
			Kind:   Reconnecting,
			Since:  now,
			Permit: l.permit,
			Road:   l.road,
		})
	}
	l.scheduleRedialLocked(now)
	l.mu.Unlock()

	l.wake()
	if notify != nil && l.cfg.OnState != nil {
		l.cfg.OnState(*notify)
	}
}

func (l *Link) recordHeard() {
	now := l.clock.Now()
	var woke bool
	l.mu.Lock()
	l.lastHeard = now
	if !l.probeDeadline.IsZero() && !now.Before(l.probeSentAt) {
		l.probeDeadline = time.Time{}
	}
	if l.mux != nil && l.cfg.Active != nil && l.cfg.Active() {
		l.lastActive = now
		if now.After(l.idleSince) {
			l.idleSince = now
		}
		// If the link transitioned from idle to active because a turn started
		// streaming frames, switch the timer to active ping mode immediately.
		if l.livenessTimer == nil || l.livenessAt.After(now.Add(l.limits.PingEvery)) {
			l.resetLivenessTimerLocked(now)
			woke = true
		}
	}
	l.mu.Unlock()
	if woke {
		l.wake()
	}
}

func (l *Link) isActiveLocked(now time.Time) bool {
	cfgActive := l.cfg.Active != nil && l.cfg.Active()
	if cfgActive {
		l.lastActive = now
		if now.After(l.idleSince) {
			l.idleSince = now
		}
	}
	touchActive := !l.lastTouch.IsZero() && now.Sub(l.lastTouch) < l.limits.ActiveWindow
	return cfgActive || touchActive
}

func (l *Link) setStateLocked(next State) *State {
	if l.state.Kind == next.Kind &&
		l.state.Reason == next.Reason &&
		l.state.Permit == next.Permit &&
		l.state.Road == next.Road {
		return nil
	}
	l.state = next
	cp := next
	return &cp
}

func (l *Link) stopLivenessTimerLocked() {
	if l.livenessTimer != nil {
		l.livenessTimer.Stop()
		l.livenessTimer = nil
		l.livenessAt = time.Time{}
	}
}

func (l *Link) stopRedialTimerLocked() {
	if l.redialTimer != nil {
		l.redialTimer.Stop()
		l.redialTimer = nil
		l.redialAt = time.Time{}
	}
}

func (l *Link) setLivenessTimerAtLocked(now, target time.Time) {
	if l.livenessTimer != nil && l.livenessAt.Equal(target) {
		return
	}
	l.stopLivenessTimerLocked()
	wait := target.Sub(now)
	if wait <= 0 {
		wait = time.Nanosecond
	}
	l.livenessAt = target
	l.livenessTimer = l.clock.NewTimer(wait)
}

func (l *Link) resetLivenessTimerLocked(now time.Time) {
	if l.closed || l.revoked || l.mux == nil {
		l.stopLivenessTimerLocked()
		return
	}

	if l.isActiveLocked(now) {
		deadAt := l.lastHeard.Add(l.limits.PingEvery + l.limits.DeadAfter)
		if !l.probeDeadline.IsZero() {
			if l.lastHeard.Before(l.probeSentAt) {
				deadAt = l.probeDeadline
			} else {
				l.probeDeadline = time.Time{}
			}
		}

		pingAt := l.lastPingSent.Add(l.limits.PingEvery)
		nextAt := pingAt
		if deadAt.Before(nextAt) {
			nextAt = deadAt
		}
		if (l.cfg.Active == nil || !l.cfg.Active()) && !l.lastTouch.IsZero() {
			activeEnd := l.lastTouch.Add(l.limits.ActiveWindow)
			if activeEnd.After(now) && activeEnd.Before(nextAt) {
				nextAt = activeEnd
			}
		}
		l.setLivenessTimerAtLocked(now, nextAt)
		return
	}

	// While NOT active, never schedule a repeating ping tick. If no channels
	// are open, schedule zero timers; otherwise arm only the single one-shot
	// IdleDetach timer.
	if len(l.channels) == 0 {
		l.stopLivenessTimerLocked()
		return
	}
	if l.idleSince.IsZero() {
		l.idleSince = now
	}
	detachAt := l.idleSince.Add(l.limits.IdleDetach)
	l.setLivenessTimerAtLocked(now, detachAt)
}

func (l *Link) scheduleRedialLocked(now time.Time) {
	if l.closed || l.revoked || l.redialTimer != nil {
		return
	}
	var wait time.Duration
	switch {
	case l.immediateRedial:
		l.immediateRedial = false
		wait = time.Nanosecond
	case l.offlineCap:
		l.offlineCap = false
		l.baseWait = reconnectCap
		if l.retryCount < 4 {
			l.retryCount = 4
		}
		wait = relay.Jitter(reconnectCap)
	case l.retryCount == 0:
		l.baseWait = reconnectBase
		l.retryCount = 1
		wait = relay.FirstRetry()
	default:
		l.baseWait *= 2
		if l.baseWait > reconnectCap {
			l.baseWait = reconnectCap
		}
		l.retryCount++
		wait = relay.Jitter(l.baseWait)
	}
	if wait <= 0 {
		wait = time.Nanosecond
	}
	l.redialAt = now.Add(wait)
	l.redialTimer = l.clock.NewTimer(wait)
}

func (l *Link) handleLivenessTick() {
	now := l.clock.Now()
	l.mu.Lock()
	mux := l.mux
	l.livenessTimer = nil
	l.livenessAt = time.Time{}
	if l.closed || l.revoked || mux == nil {
		l.mu.Unlock()
		return
	}

	if l.isActiveLocked(now) {
		deadAt := l.lastHeard.Add(l.limits.PingEvery + l.limits.DeadAfter)
		if !l.probeDeadline.IsZero() {
			if l.lastHeard.Before(l.probeSentAt) {
				deadAt = l.probeDeadline
			} else {
				l.probeDeadline = time.Time{}
			}
		}

		if !now.Before(deadAt) {
			// Active host liveness deadline exceeded: close the tunnel, mark the
			// host WentOffline, and schedule a jittered redial.
			l.mux = nil
			l.channels = make(map[uint32]*trackedChannel)
			l.retryCount = 0
			l.baseWait = reconnectBase
			notify := l.setStateLocked(State{
				Kind:   WentOffline,
				Since:  now,
				Permit: l.permit,
				Road:   l.road,
			})
			l.scheduleRedialLocked(now)
			l.mu.Unlock()

			_ = mux.Close()
			if notify != nil && l.cfg.OnState != nil {
				l.cfg.OnState(*notify)
			}
			return
		}

		pingAt := l.lastPingSent.Add(l.limits.PingEvery)
		if !now.Before(pingAt) {
			l.pingSeq++
			seq := l.pingSeq
			l.lastPingSent = now
			_ = mux.SendPing(seq)
		}
		l.resetLivenessTimerLocked(now)
		l.mu.Unlock()
		return
	}

	// Not active: check whether IdleDetach has elapsed.
	if len(l.channels) == 0 {
		l.mu.Unlock()
		return
	}
	detachAt := l.idleSince.Add(l.limits.IdleDetach)
	if !now.Before(detachAt) {
		l.detachIdleLocked(now, mux)
		return
	}
	l.resetLivenessTimerLocked(now)
	l.mu.Unlock()
}

func (l *Link) detachIdleLocked(now time.Time, mux *Mux) {
	l.mux = nil
	l.stopLivenessTimerLocked()
	l.stopRedialTimerLocked()

	ended := make([]*trackedChannel, 0, len(l.channels))
	for _, tc := range l.channels {
		tc.setLocalReason(ReasonIdle)
		ended = append(ended, tc)
	}
	l.channels = make(map[uint32]*trackedChannel)
	onEnd := l.onChannelEnd

	notify := l.setStateLocked(State{
		Kind:   Idle,
		Reason: ReasonIdle,
		Since:  now,
		Permit: l.permit,
		Road:   l.road,
	})
	l.mu.Unlock()

	// Stamp the mux channels locally under mux.mu before closing the mux so no
	// frame is written to the wire and Channel.Reason() reports "idle".
	mux.mu.Lock()
	for _, ch := range mux.channels {
		if ch.reason == "" {
			ch.reason = ReasonIdle
		}
		ch.ended = true
	}
	mux.mu.Unlock()

	_ = mux.Close()

	if onEnd != nil {
		for _, tc := range ended {
			tc := tc
			tc.endOnce.Do(func() {
				onEnd(tc.chat, ReasonIdle)
			})
		}
	}
	if notify != nil && l.cfg.OnState != nil {
		l.cfg.OnState(*notify)
	}
}

func (l *Link) handleChannelDone(mux *Mux, tc *trackedChannel) {
	reason := tc.reason()
	now := l.clock.Now()

	var (
		notify *State
		onEnd  func(chat, reason string)
	)
	l.mu.Lock()
	if cur, ok := l.channels[tc.ch.ID()]; ok && cur == tc {
		delete(l.channels, tc.ch.ID())
	}
	onEnd = l.onChannelEnd

	switch reason {
	case ReasonRemoved:
		l.revoked = true
		l.lastReason = ReasonRemoved
		l.stopLivenessTimerLocked()
		l.stopRedialTimerLocked()
		notify = l.setStateLocked(State{
			Kind:   Revoked,
			Reason: ReasonRemoved,
			Since:  now,
			Permit: pair.Permit{},
			Road:   l.road,
		})
	case ReasonBudget:
		l.budgetExhausted = true
		l.permit.Drive = false
		l.lastReason = ReasonBudget
		if l.mux == mux && !l.revoked {
			notify = l.setStateLocked(State{
				Kind:   StateBudget,
				Reason: ReasonBudget,
				Since:  now,
				Permit: l.permit,
				Road:   l.road,
			})
		}
	case ReasonPermitChanged:
		l.lastReason = ReasonPermitChanged
		l.immediateRedial = true
		l.permit.Drive = false
		if l.mux == mux && !l.revoked && (l.state.Kind == Live || l.state.Kind == WatchOnly) {
			notify = l.setStateLocked(State{
				Kind:   WatchOnly,
				Reason: ReasonPermitChanged,
				Since:  now,
				Permit: l.permit,
				Road:   l.road,
			})
		}
	case ReasonLagging:
		l.immediateRedial = true
	}

	if l.mux == mux && !l.closed && !l.revoked {
		l.resetLivenessTimerLocked(now)
	}
	l.mu.Unlock()
	l.wake()

	if reason != "" && onEnd != nil {
		tc.endOnce.Do(func() {
			onEnd(tc.chat, reason)
		})
	}
	if notify != nil && l.cfg.OnState != nil {
		l.cfg.OnState(*notify)
	}
}

func (l *Link) handleTunnelDone(mux *Mux) {
	now := l.clock.Now()
	var (
		notify *State
		ended  []*trackedChannel
		onEnd  func(chat, reason string)
	)

	l.mu.Lock()
	if l.mux != mux {
		l.mu.Unlock()
		return
	}
	l.mux = nil
	l.stopLivenessTimerLocked()
	onEnd = l.onChannelEnd

	for _, tc := range l.channels {
		ended = append(ended, tc)
		switch tc.reason() {
		case ReasonRemoved:
			l.revoked = true
			l.lastReason = ReasonRemoved
		case ReasonPermitChanged:
			l.immediateRedial = true
			l.lastReason = ReasonPermitChanged
		case ReasonLagging:
			l.immediateRedial = true
		case ReasonBudget:
			l.budgetExhausted = true
			l.lastReason = ReasonBudget
		}
	}
	l.channels = make(map[uint32]*trackedChannel)

	if !l.closed {
		if l.revoked {
			l.stopRedialTimerLocked()
			reason := l.lastReason
			if reason == "" {
				reason = ReasonRemoved
			}
			notify = l.setStateLocked(State{
				Kind:   Revoked,
				Reason: reason,
				Since:  now,
				Permit: pair.Permit{},
				Road:   l.road,
			})
		} else {
			kind := Reconnecting
			if !l.isActiveLocked(now) {
				kind = WentOffline
			}
			notify = l.setStateLocked(State{
				Kind:   kind,
				Reason: l.lastReason,
				Since:  now,
				Permit: l.permit,
				Road:   l.road,
			})
			l.retryCount = 0
			l.baseWait = reconnectBase
			l.scheduleRedialLocked(now)
		}
	}
	l.mu.Unlock()
	l.wake()

	if onEnd != nil {
		for _, tc := range ended {
			tc := tc
			if r := tc.reason(); r != "" {
				tc.endOnce.Do(func() {
					onEnd(tc.chat, r)
				})
			}
		}
	}
	if notify != nil && l.cfg.OnState != nil {
		l.cfg.OnState(*notify)
	}
}

func (l *Link) handleRedialTick() {
	l.mu.Lock()
	l.redialTimer = nil
	l.redialAt = time.Time{}
	if l.closed || l.revoked || l.mux != nil {
		l.mu.Unlock()
		return
	}
	l.mu.Unlock()

	_, _ = l.ensureConnected(context.Background())
}

func (l *Link) loop() {
	for {
		l.mu.Lock()
		if l.closed {
			l.mu.Unlock()
			return
		}
		var (
			livenessC <-chan time.Time
			redialC   <-chan time.Time
			muxDoneC  <-chan struct{}
			curMux    = l.mux
		)
		if l.livenessTimer != nil {
			livenessC = l.livenessTimer.C()
		}
		if l.redialTimer != nil {
			redialC = l.redialTimer.C()
		}
		if curMux != nil {
			muxDoneC = curMux.Done()
		}
		l.mu.Unlock()

		select {
		case <-l.stopCh:
			return
		case <-l.wakeCh:
		case <-muxDoneC:
			l.handleTunnelDone(curMux)
		case <-livenessC:
			l.handleLivenessTick()
		case <-redialC:
			l.handleRedialTick()
		}
	}
}
