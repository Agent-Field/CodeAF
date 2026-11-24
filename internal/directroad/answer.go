package directroad

import (
	"context"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/pion/webrtc/v4"
)

// Host manages incoming direct-road offers on the host computer, enforces
// per-device superseding and the host-wide livedoor.PendingOffers cap, and
// hands opened byte streams to the installed acceptor.
type Host struct {
	cfg Config

	mu              sync.Mutex
	closed          bool
	acceptCtx       context.Context
	accept          func(ctx context.Context, stream io.ReadWriteCloser)
	stopCtxWatch    chan struct{}
	pending         map[*hostSession]struct{}
	pendingByDevice map[string]*hostSession
	streams         map[*byteStream]struct{}

	pendingWG sync.WaitGroup
}

// New creates a Host with the given configuration.
func New(cfg Config) *Host {
	return &Host{
		cfg:             cfg,
		pending:         make(map[*hostSession]struct{}),
		pendingByDevice: make(map[string]*hostSession),
		streams:         make(map[*byteStream]struct{}),
	}
}

// NewHost returns a new Host as a livedoor.DirectHost.
func NewHost(cfg Config) livedoor.DirectHost {
	return New(cfg)
}

// SetAcceptor installs the callback that receives each opened direct-road byte
// stream. When ctx ends, the host closes every pending session and every open
// stream.
func (h *Host) SetAcceptor(ctx context.Context, accept func(ctx context.Context, stream io.ReadWriteCloser)) {
	if ctx == nil {
		ctx = context.Background()
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	if h.stopCtxWatch != nil {
		close(h.stopCtxWatch)
		h.stopCtxWatch = nil
	}
	h.acceptCtx = ctx
	h.accept = accept
	stopWatch := make(chan struct{})
	h.stopCtxWatch = stopWatch
	h.mu.Unlock()

	guard.Go("directroad.host.ctx", func() {
		select {
		case <-ctx.Done():
			h.Close()
		case <-stopWatch:
		}
	})
}

// Offered validates an incoming viewer offer synchronously on the relay link's
// reader goroutine, reserves a pending slot (superseding any earlier pending
// attempt from the same peer.Device), and spawns the WebRTC answer work on a
// guarded goroutine. Returning nil tells the caller to send a refuse signal.
func (h *Host) Offered(ctx context.Context, peer livedoor.Peer, offer []byte, s livedoor.Signaller) livedoor.DirectSession {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil
	}

	sig, err := livedoor.DecodeSignal(offer)
	if err != nil || sig.T != livedoor.SignalOffer || sig.ID == "" || sig.SDP == "" {
		return nil
	}

	h.mu.Lock()
	if h.closed || h.accept == nil || h.acceptCtx == nil || h.acceptCtx.Err() != nil {
		h.mu.Unlock()
		return nil
	}

	old := h.pendingByDevice[peer.Device]
	if old == nil && len(h.pending) >= livedoor.PendingOffers {
		h.mu.Unlock()
		return nil
	}
	if old != nil {
		delete(h.pending, old)
		delete(h.pendingByDevice, peer.Device)
	}

	hs := &hostSession{
		host:     h,
		ctx:      ctx,
		hostCtx:  h.acceptCtx,
		device:   peer.Device,
		offer:    sig,
		s:        s,
		inbox:    make(chan []byte, livedoor.SignalRate),
		cancelCh: make(chan struct{}),
	}
	h.pending[hs] = struct{}{}
	h.pendingByDevice[peer.Device] = hs
	h.pendingWG.Add(1)
	h.mu.Unlock()

	if old != nil {
		old.Close()
	}

	guard.Go("directroad.host.answer", func() {
		defer h.pendingWG.Done()
		hs.run()
	})

	return hs
}

// Close closes the host, every pending session, and every open stream, and
// waits for in-flight answer goroutines to finish.
func (h *Host) Close() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		h.pendingWG.Wait()
		return
	}
	h.closed = true
	if h.stopCtxWatch != nil {
		close(h.stopCtxWatch)
		h.stopCtxWatch = nil
	}

	pendingList := make([]*hostSession, 0, len(h.pending))
	for hs := range h.pending {
		pendingList = append(pendingList, hs)
	}
	clear(h.pending)
	clear(h.pendingByDevice)

	streamList := make([]*byteStream, 0, len(h.streams))
	for st := range h.streams {
		streamList = append(streamList, st)
	}
	clear(h.streams)
	h.mu.Unlock()

	for _, hs := range pendingList {
		hs.Close()
	}
	for _, st := range streamList {
		_ = st.Close()
	}

	h.pendingWG.Wait()
}

// releasePending removes hs from the host's pending tables if it is still
// registered there.
func (h *Host) releasePending(hs *hostSession) {
	h.mu.Lock()
	delete(h.pending, hs)
	if h.pendingByDevice[hs.device] == hs {
		delete(h.pendingByDevice, hs.device)
	}
	h.mu.Unlock()
}

// acceptStream registers an opened stream with the host and runs the installed
// acceptor on its own guarded goroutine, closing the stream when accept
// returns.
func (h *Host) acceptStream(st *byteStream) {
	h.mu.Lock()
	if h.closed || h.accept == nil || h.acceptCtx == nil || h.acceptCtx.Err() != nil {
		h.mu.Unlock()
		_ = st.Close()
		return
	}
	acceptFn := h.accept
	acceptCtx := h.acceptCtx
	h.streams[st] = struct{}{}
	h.mu.Unlock()

	guard.Go("directroad.host.accept", func() {
		defer func() {
			_ = st.Close()
			h.mu.Lock()
			delete(h.streams, st)
			h.mu.Unlock()
		}()
		acceptFn(acceptCtx, st)
	})
}

// hostSession is one pending direct-road attempt on the host.
type hostSession struct {
	host    *Host
	ctx     context.Context
	hostCtx context.Context
	device  string
	offer   livedoor.Signal
	s       livedoor.Signaller

	inbox    chan []byte
	cancelCh chan struct{}

	mu         sync.Mutex
	sess       *pcSession
	handedOver bool

	closed    atomic.Bool
	closeOnce sync.Once
}

// Deliver enqueues a signalling payload from the relay reader goroutine
// without blocking; overflowing the bounded queue ends the attempt.
func (hs *hostSession) Deliver(payload []byte) {
	if hs.closed.Load() {
		return
	}
	cp := append([]byte(nil), payload...)
	select {
	case hs.inbox <- cp:
	default:
		// A viewer that floods signalling frames beyond the bounded queue
		// loses its pending attempt so the relay reader goroutine never blocks.
		hs.Close()
	}
}

// Close releases the pending slot and tears down the peer connection if no
// stream has been handed over to the host acceptor yet.
func (hs *hostSession) Close() {
	hs.closeOnce.Do(func() {
		hs.closed.Store(true)
		close(hs.cancelCh)
		hs.host.releasePending(hs)

		hs.mu.Lock()
		sess := hs.sess
		handedOver := hs.handedOver
		hs.mu.Unlock()

		if sess != nil && !handedOver {
			sess.closePC()
		}
	})
}

// markHandedOver records that the session's data channel opened before the
// session was closed, transferring peer-connection ownership to the stream.
func (hs *hostSession) markHandedOver() bool {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	if hs.closed.Load() {
		return false
	}
	hs.handedOver = true
	return true
}

// run executes the pion peer-connection setup, answer generation, candidate
// trickling, and OfferTTL wait off the relay reader goroutine.
func (hs *hostSession) run() {
	defer hs.host.releasePending(hs)

	ttl := livedoor.OfferTTL
	if ttl <= 0 {
		ttl = time.Millisecond
	}
	ttlTimer := time.NewTimer(ttl)
	defer ttlTimer.Stop()

	if hs.closed.Load() || hs.ctx.Err() != nil || hs.hostCtx.Err() != nil {
		return
	}

	sess, err := newPCSession(hs.offer.ID, hs.host.cfg, hs.s)
	if err != nil {
		return
	}

	hs.mu.Lock()
	if hs.closed.Load() {
		hs.mu.Unlock()
		sess.abort()
		return
	}
	hs.sess = sess
	hs.mu.Unlock()

	failSession := func(sendEndFrame bool) {
		if sendEndFrame {
			_ = sendEnd(hs.s, hs.offer.ID)
		}
		sess.abort()
	}

	err = sess.setRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  hs.offer.SDP,
	})
	if err != nil {
		failSession(true)
		return
	}

	answerDesc, err := sess.pc.CreateAnswer(nil)
	if err != nil {
		failSession(true)
		return
	}
	if err := sess.pc.SetLocalDescription(answerDesc); err != nil {
		failSession(true)
		return
	}
	if err := sendAnswer(hs.s, hs.offer.ID, answerDesc.SDP); err != nil {
		failSession(false)
		return
	}

	for {
		select {
		case rawStream := <-sess.openCh:
			st := rawStream.(*byteStream)
			if !hs.markHandedOver() {
				_ = st.Close()
				sess.abort()
				return
			}
			hs.host.releasePending(hs)
			hs.host.acceptStream(st)
			return

		case <-sess.errCh:
			failSession(true)
			return

		case <-ttlTimer.C:
			failSession(true)
			return

		case <-hs.cancelCh:
			failSession(false)
			return

		case <-hs.ctx.Done():
			failSession(true)
			return

		case <-hs.hostCtx.Done():
			failSession(true)
			return

		case payload := <-hs.inbox:
			sig, err := livedoor.DecodeSignal(payload)
			if err != nil || sig.ID != hs.offer.ID {
				continue
			}
			switch sig.T {
			case livedoor.SignalCandidate:
				if err := sess.addCandidate(sig); err != nil {
					failSession(true)
					return
				}
			case livedoor.SignalEnd:
				failSession(false)
				return
			}
		}
	}
}
