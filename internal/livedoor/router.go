package livedoor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/remote"
)

const (
	defaultRoad             = "relay"
	hostBackstopExtra       = time.Minute
	opFlushClose      uint8 = 0xff
)

// Peer identifies the authenticated viewer computer at the other end of a live
// door tunnel along with its permit at connection time.
type Peer struct {
	Device string
	Label  string
	Permit pair.Permit
	Class  pair.PermitClass
}

// ServeConfig configures a host-side live door router over one viewer tunnel.
type ServeConfig struct {
	Resolver Resolver
	Registry *Registry
	Budget   *Budget
	Permit   func(device string) pair.Permit
	Clock    Clock
	Limits   Limits
	Host     string
	Road     string
}

type tunnelRWC struct {
	rwc io.ReadWriteCloser
}

func (t *tunnelRWC) Read(p []byte) (int, error) {
	return t.rwc.Read(p)
}

func (t *tunnelRWC) Write(p []byte) (int, error) {
	if len(p) >= frameHeaderBytes && p[0] == opFlushClose {
		return 0, ErrLinkClosed
	}
	return t.rwc.Write(p)
}

func (t *tunnelRWC) Close() error {
	return t.rwc.Close()
}

type serverTunnel struct {
	peer   Peer
	cfg    ServeConfig
	clock  Clock
	limits Limits
	road   string
	mux    *Mux

	mu            sync.Mutex
	lastRead      time.Time
	lastPermit    pair.Permit
	chans         map[uint32]*activeChan
	closing       bool
	droppedFrames atomic.Int64
}

type activeChan struct {
	ch   *Channel
	outQ *chanOutQueue

	mu         sync.Mutex
	holderConn net.Conn
	drive      bool
	closed     bool
}

func (a *activeChan) setDrive(drive bool) {
	a.mu.Lock()
	a.drive = drive
	a.mu.Unlock()
}

func (a *activeChan) isDrive() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.drive
}

func (a *activeChan) setOutQueue(q *chanOutQueue) {
	a.mu.Lock()
	closed := a.closed
	a.outQ = q
	a.mu.Unlock()
	if closed {
		q.abort()
	}
}

func (a *activeChan) closeHolderAndQueue() {
	a.mu.Lock()
	a.closed = true
	conn := a.holderConn
	q := a.outQ
	a.mu.Unlock()

	if q != nil {
		q.abort()
	}
	if conn != nil {
		_ = conn.Close()
	}
}

type chanOutQueue struct {
	mu         sync.Mutex
	cond       *sync.Cond
	lines      [][]byte
	buffered   int
	limit      int
	aborted    bool
	overflowed bool
	eof        bool
}

func newChanOutQueue(limit int) *chanOutQueue {
	if limit <= 0 {
		limit = QueueMax
	}
	q := &chanOutQueue{limit: limit}
	q.cond = sync.NewCond(&q.mu)
	return q
}

func (q *chanOutQueue) enqueue(line []byte) bool {
	q.mu.Lock()
	if q.aborted || q.eof {
		q.mu.Unlock()
		return false
	}
	if q.buffered+len(line) > q.limit {
		q.aborted = true
		q.overflowed = true
		q.lines = nil
		q.buffered = 0
		q.cond.Broadcast()
		q.mu.Unlock()
		return false
	}
	cp := append([]byte(nil), line...)
	q.lines = append(q.lines, cp)
	q.buffered += len(cp)
	q.cond.Signal()
	q.mu.Unlock()
	return true
}

func (q *chanOutQueue) didOverflow() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.overflowed
}

func (q *chanOutQueue) finishHolderEOF() {
	q.mu.Lock()
	if !q.aborted {
		q.eof = true
		q.cond.Broadcast()
	}
	q.mu.Unlock()
}

func (q *chanOutQueue) abort() {
	q.mu.Lock()
	if !q.aborted {
		q.aborted = true
		q.lines = nil
		q.buffered = 0
		q.cond.Broadcast()
	}
	q.mu.Unlock()
}

func (q *chanOutQueue) pop() (line []byte, eof bool, ok bool) {
	q.mu.Lock()
	for len(q.lines) == 0 && !q.aborted && !q.eof {
		q.cond.Wait()
	}
	if q.aborted {
		q.mu.Unlock()
		return nil, false, false
	}
	if len(q.lines) > 0 {
		line = q.lines[0]
		q.lines[0] = nil
		q.lines = q.lines[1:]
		q.buffered -= len(line)
		if q.buffered < 0 {
			q.buffered = 0
		}
		eof = len(q.lines) == 0 && q.eof
		q.mu.Unlock()
		return line, eof, true
	}
	if q.eof {
		q.aborted = true
		q.mu.Unlock()
		return nil, true, true
	}
	q.mu.Unlock()
	return nil, false, false
}

// Serve runs a Host-role multiplexer over tun, registers the tunnel for
// revocation and permit updates, sends the initial HELLO_ACK reflecting the
// peer's current permit, and routes accepted channels to local holders.
func Serve(ctx context.Context, tun io.ReadWriteCloser, peer Peer, cfg ServeConfig) error {
	if ctx == nil {
		ctx = context.Background()
	}
	clk := cfg.Clock
	if clk == nil {
		clk = RealClock()
	}
	lim := cfg.Limits.withDefaults()
	road := cfg.Road
	if road == "" {
		road = defaultRoad
	}

	st := &serverTunnel{
		peer:     peer,
		cfg:      cfg,
		clock:    clk,
		limits:   lim,
		road:     road,
		lastRead: clk.Now(),
		chans:    make(map[uint32]*activeChan),
	}
	curPermit := st.readPermit()
	st.lastPermit = curPermit

	wrapped := &tunnelRWC{rwc: tun}
	st.mux = NewMux(wrapped, Host, MuxConfig{
		Clock:          clk,
		Limits:         lim,
		OnFrameRead:    st.onFrameRead,
		OnFrameWritten: st.onFrameWritten,
	})

	if !curPermit.Watch {
		_ = st.mux.SendHelloAck(HelloAck{
			V:      1,
			Watch:  false,
			Drive:  false,
			Road:   road,
			Limits: lim,
		})
		st.flushAndClose()
		return nil
	}

	if cfg.Registry != nil {
		removeReg := cfg.Registry.Add(peer.Device, st)
		defer removeReg()
	}
	if cfg.Budget != nil {
		removeBudget := cfg.Budget.addOnExhausted(func() {
			st.resetAllChannels(ReasonBudget)
		})
		defer removeBudget()
		if cfg.Registry != nil {
			removeRegBudget := cfg.Budget.addOnExhausted(cfg.Registry.onBudgetExhausted)
			defer removeRegBudget()
		}
	}

	curPermit = st.readPermit()
	st.mu.Lock()
	st.lastPermit = curPermit
	closing := st.closing
	st.mu.Unlock()
	if closing {
		st.flushAndClose()
		return nil
	}
	if !curPermit.Watch {
		_ = st.mux.SendHelloAck(HelloAck{
			V:      1,
			Watch:  false,
			Drive:  false,
			Road:   road,
			Limits: lim,
		})
		st.flushAndClose()
		return nil
	}

	drive := curPermit.Drive && (cfg.Budget == nil || !cfg.Budget.Exhausted())
	if err := st.mux.SendHelloAck(HelloAck{
		V:      1,
		Watch:  true,
		Drive:  drive,
		Road:   road,
		Limits: lim,
	}); err != nil {
		_ = st.mux.Close()
		return nil
	}

	backstopWait := lim.IdleDetach + hostBackstopExtra
	initialTimer := clk.NewTimer(backstopWait)
	stopBackstop := make(chan struct{})
	defer close(stopBackstop)

	guard.Go("livedoor.router.backstop", func() {
		timer := initialTimer
		defer func() {
			if timer != nil {
				timer.Stop()
			}
		}()
		for {
			select {
			case <-timer.C():
				st.mu.Lock()
				elapsed := clk.Now().Sub(st.lastRead)
				st.mu.Unlock()
				if elapsed >= backstopWait {
					_ = st.mux.Close()
					return
				}
				rem := backstopWait - elapsed
				if rem <= 0 {
					rem = time.Nanosecond
				}
				timer = clk.NewTimer(rem)
			case <-st.mux.Done():
				return
			case <-stopBackstop:
				return
			case <-ctx.Done():
				_ = st.mux.Close()
				return
			}
		}
	})

	var wg sync.WaitGroup
	for {
		ch, req, err := st.mux.Accept(ctx)
		if err != nil {
			break
		}
		wg.Add(1)
		guard.Go("livedoor.router.channel", func() {
			defer wg.Done()
			st.serveChannel(ctx, ch, req)
		})
	}

	_ = st.mux.Close()
	wg.Wait()
	return nil
}

func (st *serverTunnel) maxTunnelsPerDevice() int {
	return st.limits.MaxTunnelsPerDevice
}

func (st *serverTunnel) readPermit() pair.Permit {
	if st.cfg.Permit != nil {
		return st.cfg.Permit(st.peer.Device)
	}
	return st.peer.Permit
}

func (st *serverTunnel) onFrameRead(n int) {
	st.mu.Lock()
	st.lastRead = st.clock.Now()
	st.mu.Unlock()
	if st.cfg.Budget != nil && n > 0 {
		st.cfg.Budget.Charge(int64(n))
	}
}

func (st *serverTunnel) onFrameWritten(n int) {
	if st.cfg.Budget != nil && n > 0 {
		st.cfg.Budget.Charge(int64(n))
	}
}

func (st *serverTunnel) flushAndClose() {
	m := st.mux
	if m == nil {
		return
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.enqueueControlLocked(frame{op: opFlushClose})
	m.mu.Unlock()

	select {
	case <-m.Done():
	case <-time.After(50 * time.Millisecond):
		_ = m.Close()
	}
}

func (st *serverTunnel) CloseWithReason(reason string) {
	st.mu.Lock()
	st.closing = true
	active := make([]*activeChan, 0, len(st.chans))
	for _, ac := range st.chans {
		active = append(active, ac)
	}
	st.mu.Unlock()

	for _, ac := range active {
		_ = ac.ch.Reset(reason)
	}

	if m := st.mux; m != nil {
		m.mu.Lock()
		muxChans := make([]*Channel, 0, len(m.channels))
		for _, ch := range m.channels {
			muxChans = append(muxChans, ch)
		}
		m.mu.Unlock()

		for _, ch := range muxChans {
			m.mu.Lock()
			pending := ch.pendingAccept
			m.mu.Unlock()
			if pending {
				_ = ch.Refuse(reason)
			} else {
				_ = ch.Reset(reason)
			}
		}
	}

	for _, ac := range active {
		ac.closeHolderAndQueue()
	}

	st.flushAndClose()
}

func (st *serverTunnel) PermitsChanged() {
	cur := st.readPermit()
	if !cur.Watch {
		st.CloseWithReason(ReasonPermitChanged)
		return
	}
	st.mu.Lock()
	wasDrive := st.lastPermit.Drive
	st.lastPermit = cur
	active := make([]*activeChan, 0, len(st.chans))
	for _, ac := range st.chans {
		active = append(active, ac)
	}
	st.mu.Unlock()

	if !cur.Drive {
		for _, ac := range active {
			if wasDrive || ac.isDrive() {
				_ = ac.ch.Reset(ReasonPermitChanged)
				ac.closeHolderAndQueue()
			}
		}
	}
}

func (st *serverTunnel) resetAllChannels(reason string) {
	st.mu.Lock()
	active := make([]*activeChan, 0, len(st.chans))
	for _, ac := range st.chans {
		active = append(active, ac)
	}
	st.mu.Unlock()

	for _, ac := range active {
		_ = ac.ch.Reset(reason)
		ac.closeHolderAndQueue()
	}

	if m := st.mux; m != nil {
		m.mu.Lock()
		muxChans := make([]*Channel, 0, len(m.channels))
		for _, ch := range m.channels {
			muxChans = append(muxChans, ch)
		}
		m.mu.Unlock()

		for _, ch := range muxChans {
			m.mu.Lock()
			pending := ch.pendingAccept
			m.mu.Unlock()
			if pending {
				_ = ch.Refuse(reason)
			} else {
				_ = ch.Reset(reason)
			}
		}
	}
}

func (st *serverTunnel) serveChannel(ctx context.Context, ch *Channel, req OpenRequest) {
	// 2a. Resolve the running chat holder; THE LIVE DOOR NEVER STARTS A CHAT.
	if st.cfg.Resolver == nil {
		_ = ch.Refuse(ReasonNotRunning)
		return
	}
	holderConn, route, err := st.cfg.Resolver.Resolve(ctx, req.Chat)
	if err != nil || holderConn == nil {
		if holderConn != nil {
			_ = holderConn.Close()
		}
		_ = ch.Refuse(ReasonNotRunning)
		return
	}

	curPermit := st.readPermit()
	ac := &activeChan{
		ch:         ch,
		holderConn: holderConn,
		drive:      curPermit.Drive,
	}
	st.mu.Lock()
	if st.closing {
		st.mu.Unlock()
		_ = holderConn.Close()
		_ = ch.Refuse(ReasonHostClosing)
		return
	}
	st.chans[ch.ID()] = ac
	st.mu.Unlock()

	spliceDone := make(chan struct{})
	defer func() {
		close(spliceDone)
		st.mu.Lock()
		delete(st.chans, ch.ID())
		st.mu.Unlock()
		ac.closeHolderAndQueue()
	}()

	// 2b. Accept the channel and read the viewer's initial hello line bounded by FirstLineMax.
	if err := ch.Accept(); err != nil {
		return
	}

	guard.Go("livedoor.router.chanWatch", func() {
		select {
		case <-ch.Done():
			ac.closeHolderAndQueue()
		case <-ctx.Done():
			ac.closeHolderAndQueue()
		case <-spliceDone:
		}
	})

	viewerScan := newLineScanner(ch, st.limits.MaxLine)
	firstLine, err := viewerScan.ReadLine(st.limits.FirstLineMax)
	if err != nil {
		if errors.Is(err, errLineTooLong) || errors.Is(err, errNoNewline) {
			_ = ch.Reset(ReasonFrameTooLarge)
		}
		return
	}

	var firstFrame remote.Frame
	if err := json.Unmarshal(bytes.TrimRight(firstLine, "\r\n"), &firstFrame); err != nil || firstFrame.Kind != "hello" {
		_ = ch.Reset(ReasonNotAllowed)
		return
	}
	var viewerHello remote.Hello
	if len(firstFrame.Payload) > 0 {
		if err := json.Unmarshal(firstFrame.Payload, &viewerHello); err != nil {
			_ = ch.Reset(ReasonNotAllowed)
			return
		}
	}

	// 2c. Rewrite Hello from the authenticated peer state, discarding any viewer
	// claim over Session, Workspace, Join, New, Watch, Back, or Remote.
	curPermit = st.readPermit()
	if !curPermit.Watch {
		_ = ch.Reset(ReasonPermitChanged)
		return
	}
	exhausted := st.cfg.Budget != nil && st.cfg.Budget.Exhausted()
	drive := curPermit.Drive && !exhausted
	ac.setDrive(drive)

	stamp := remote.RemoteViewer{
		Device: st.peer.Device,
		Label:  st.peer.Label,
		Watch:  true,
		Drive:  drive,
		Road:   st.road,
	}
	rewrittenHello := remote.Hello{
		Version:         viewerHello.Version,
		Workspace:       route.Workspace,
		Session:         route.Session,
		SessionInstance: viewerHello.SessionInstance,
		Encodings:       viewerHello.Encodings,
		Resume:          viewerHello.Resume,
		Surface:         viewerHello.Surface,
		ClientID:        viewerHello.ClientID,
		Back:            true,
		Join:            true,
		New:             false,
		Watch:           !drive,
		Remote:          &stamp,
	}
	rewrittenPayload, err := json.Marshal(rewrittenHello)
	if err != nil {
		_ = ch.Reset(ReasonNotRunning)
		return
	}
	rewrittenLine, err := json.Marshal(remote.Frame{
		Kind:    "hello",
		Payload: rewrittenPayload,
	})
	if err != nil {
		_ = ch.Reset(ReasonNotRunning)
		return
	}
	if _, err := holderConn.Write(append(rewrittenLine, '\n')); err != nil {
		if !channelEnded(ch) {
			_ = ch.CloseWithReason(ReasonHolderGone)
		}
		return
	}

	// 2d. D10: Read and verify the holder's stamped welcome BEFORE any viewer
	// byte after the hello is read or forwarded to the holder.
	welcomeCap := remote.DefaultMaxLine
	if st.limits.MaxLine > 0 && st.limits.MaxLine < welcomeCap {
		welcomeCap = st.limits.MaxLine
	}
	holderScanCap := st.limits.MaxLine
	if holderScanCap < welcomeCap {
		holderScanCap = welcomeCap
	}
	holderScan := newLineScanner(holderConn, holderScanCap)
	replyLine, err := holderScan.ReadLine(welcomeCap)
	if err != nil {
		if errors.Is(err, errLineTooLong) {
			_ = ch.Reset(ReasonFrameTooLarge)
		} else if !channelEnded(ch) {
			_ = ch.Reset(ReasonNotRunning)
		}
		return
	}

	var replyFrame remote.Frame
	if err := json.Unmarshal(bytes.TrimRight(replyLine, "\r\n"), &replyFrame); err != nil {
		_ = ch.Reset(ReasonNotRunning)
		return
	}
	if replyFrame.Kind != "welcome" {
		_ = ch.Reset(ReasonNotRunning)
		return
	}
	var welcome remote.Welcome
	if err := json.Unmarshal(replyFrame.Payload, &welcome); err != nil {
		_ = ch.Reset(ReasonNotRunning)
		return
	}
	if welcome.Remote == nil || *welcome.Remote != stamp {
		_ = ch.Reset(ReasonHolderOld)
		return
	}

	// 2e & 3. Forward the verified welcome and splice in both directions through
	// the policy scanners and bounded outbound queue.
	outQ := newChanOutQueue(st.limits.QueueMax)
	ac.setOutQueue(outQ)
	if !outQ.enqueue(replyLine) {
		if outQ.didOverflow() && !channelEnded(ch) {
			_ = ch.Reset(ReasonLagging)
		}
		return
	}

	var childWG sync.WaitGroup
	childWG.Add(2)

	// Single writer goroutine draining outQ into ch so the holder reader never
	// blocks on viewer consumption.
	guard.Go("livedoor.router.chanWriter", func() {
		defer childWG.Done()
		for {
			line, eof, ok := outQ.pop()
			if !ok {
				return
			}
			if len(line) > 0 {
				if _, wErr := ch.Write(line); wErr != nil {
					ac.closeHolderAndQueue()
					return
				}
			}
			if eof {
				if !channelEnded(ch) {
					_ = ch.CloseWithReason(ReasonHolderGone)
				}
				ac.closeHolderAndQueue()
				return
			}
		}
	})

	// Viewer -> holder policy scanner.
	guard.Go("livedoor.router.viewerToHolder", func() {
		defer childWG.Done()
		for {
			vLine, rErr := viewerScan.ReadLine(st.limits.MaxLine)
			if rErr != nil {
				if errors.Is(rErr, errLineTooLong) || errors.Is(rErr, errNoNewline) {
					outQ.abort()
					_ = ch.Reset(ReasonFrameTooLarge)
				} else {
					outQ.abort()
				}
				_ = holderConn.Close()
				return
			}
			act, refusalLine := classifyViewerLine(vLine, st.cfg.Host, drive)
			switch act {
			case viewerLineForward:
				if _, wErr := holderConn.Write(vLine); wErr != nil {
					return
				}
			case viewerLineRefuse:
				if !outQ.enqueue(refusalLine) {
					if outQ.didOverflow() && !channelEnded(ch) {
						_ = ch.Reset(ReasonLagging)
					}
					ac.closeHolderAndQueue()
					return
				}
			case viewerLineDrop:
				st.droppedFrames.Add(1)
			}
		}
	})

	// Holder -> viewer bounded line scanner.
	for {
		hLine, rErr := holderScan.ReadLine(st.limits.MaxLine)
		if rErr != nil {
			if errors.Is(rErr, errLineTooLong) {
				outQ.abort()
				_ = ch.Reset(ReasonFrameTooLarge)
				_ = holderConn.Close()
				break
			}
			if !channelEnded(ch) {
				outQ.finishHolderEOF()
			} else {
				outQ.abort()
			}
			break
		}
		if !outQ.enqueue(hLine) {
			if outQ.didOverflow() && !channelEnded(ch) {
				_ = ch.Reset(ReasonLagging)
			}
			ac.closeHolderAndQueue()
			break
		}
	}

	childWG.Wait()
}

func channelEnded(ch *Channel) bool {
	select {
	case <-ch.Done():
		return true
	default:
		return false
	}
}
