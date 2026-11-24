package livedoor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/guard"
)

// Role distinguishes the viewer that dials and opens odd-numbered channels from
// the host that accepts them.
type Role int

const (
	Viewer Role = iota
	Host
)

// ErrLinkClosed reports that the underlying multiplexer link has been closed.
var ErrLinkClosed = errors.New("livedoor: link closed")

// ErrProtocol reports that the peer violated the multiplexer wire contract.
var ErrProtocol = errors.New("livedoor: protocol error")

// ErrSignalTooLarge reports a signalling payload over SignalMax, which the
// sender refuses to send and the receiver refuses to read.
var ErrSignalTooLarge = fmt.Errorf("%w: signal over SignalMax", ErrProtocol)

// ErrTooManySignals reports that a link carried more than SignalRate signalling
// frames in a minute. The sender refuses to send the one that would be over, and
// a receiver closes the link on it.
var ErrTooManySignals = fmt.Errorf("%w: too many signalling frames", ErrProtocol)

// ErrInvalidRole reports that an operation belongs to the opposite mux role.
var ErrInvalidRole = errors.New("livedoor: operation not valid for role")

// RefusedError is returned by Open when the host or local channel cap refuses
// a channel request with a closed-vocabulary reason.
type RefusedError struct {
	Reason string
}

func (e *RefusedError) Error() string {
	return "livedoor: channel refused: " + e.Reason
}

// ChannelError is returned by Channel Read or Write when a channel ends with a
// remote or local reset or reason-bearing close.
type ChannelError struct {
	Op  string
	Why string
	Err error
}

func (e *ChannelError) Error() string {
	if e.Why == "" {
		return "livedoor: channel " + e.Op
	}
	return "livedoor: channel " + e.Op + ": " + e.Why
}

// Reason returns the closed-enum reason string that ended the channel.
func (e *ChannelError) Reason() string {
	return e.Why
}

// PlannedRedial reports whether the channel ended on purpose for an immediate
// viewer road switch so remote.Roam skips its initial redial pause.
func (e *ChannelError) PlannedRedial() bool {
	return e != nil && e.Why == ReasonRoadChange
}

// EndsView reports that the host ended this view on purpose and said why, so
// that dialling it again cannot change anything: remote.Roam buries a client
// whose link ended this way and says the host's sentence, where an ordinary
// drop is redialled for the whole of its window. The set is closed and has one
// member, because every other reason either has a redial that works (a quiet
// spell, a lagging view, a changed permit) or already stops one (a removal,
// which the link refuses on every dial).
func (e *ChannelError) EndsView() bool {
	return e != nil && e.Why == ReasonRemoteControlOff
}

func (e *ChannelError) Unwrap() error {
	return e.Err
}

// OpenRequest is the payload carried by OpOpen on a newly opened channel.
type OpenRequest struct {
	Chat string `json:"chat"`
	V    int    `json:"v"`
}

// HelloAck is the payload carried by OpHelloAck on channel 0 after the
// host checks the viewer's permit.
type HelloAck struct {
	V      int    `json:"v"`
	Watch  bool   `json:"watch"`
	Drive  bool   `json:"drive"`
	Road   string `json:"road"`
	Limits Limits `json:"limits"`
	// Direct says the host will take an offer for a direct road on this link:
	// its remote control is on, its road is not forced to the relay, and it has
	// room for another direct tunnel. ADDITIVE: an older host never sets it and
	// an older viewer ignores it, so nobody offers to a host that cannot answer.
	Direct bool `json:"direct,omitempty"`
	// Off says that Watch is false BECAUSE the person turned remote control off on
	// the host, not because this computer's permit has no watching in it. It is
	// what a refused dial can say, since a refusal that never opened a channel
	// has no channel reset to carry [ReasonRemoteControlOff]. ADDITIVE for the
	// same reason Direct is: an older host never sets it, and an older viewer
	// ignores it and reads the Watch that is false as it always did.
	Off bool `json:"off,omitempty"`
}

// MuxConfig configures a Mux over an io.ReadWriteCloser.
type MuxConfig struct {
	Clock  Clock
	Limits Limits
	OnPong func(seq uint32)
	// OnSignal is handed each signalling payload that arrives, in order. It runs
	// on the link's reader goroutine, so it must not block; a handler that has
	// work to do queues it.
	OnSignal       func(payload []byte)
	OnFrameWritten func(n int)
	OnFrameRead    func(n int)
}

type acceptItem struct {
	ch  *Channel
	req OpenRequest
}

type outChunk struct {
	data []byte
	done chan error
}

// Mux multiplexes flow-controlled channels over a single io.ReadWriteCloser.
type Mux struct {
	rwc    io.ReadWriteCloser
	role   Role
	cfg    MuxConfig
	clock  Clock
	limits Limits

	mu                  sync.Mutex
	cond                *sync.Cond
	closed              bool
	closeErr            error
	nextChanID          uint32
	channels            map[uint32]*Channel
	channelOrder        []uint32
	rrIndex             int
	ctrlQueue           []frame
	linkSendCredit      int
	linkRecvAllowance   int
	linkUnreturnedBytes int
	totalBufferedBytes  int

	tokens     float64
	lastRefill time.Time

	// signalsIn and signalsOut are the times of the signalling frames this link
	// received and sent in the last minute, kept to hold both to SignalRate.
	signalsIn  []time.Time
	signalsOut []time.Time

	helloAck     HelloAck
	hasHelloAck  bool
	helloAckOnce sync.Once
	helloAckCh   chan struct{}

	acceptCh  chan acceptItem
	writeWake chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

// NewMux starts the reader and writer goroutines over rwc under guard.Go.
func NewMux(rwc io.ReadWriteCloser, role Role, cfg MuxConfig) *Mux {
	clk := cfg.Clock
	if clk == nil {
		clk = RealClock()
	}
	lim := cfg.Limits.withDefaults()
	m := &Mux{
		rwc:               rwc,
		role:              role,
		cfg:               cfg,
		clock:             clk,
		limits:            lim,
		channels:          make(map[uint32]*Channel),
		linkSendCredit:    lim.LinkWindow,
		linkRecvAllowance: lim.LinkWindow,
		tokens:            float64(lim.RateBurst),
		lastRefill:        clk.Now(),
		helloAckCh:        make(chan struct{}),
		acceptCh:          make(chan acceptItem, lim.MaxChannelsPerLink),
		writeWake:         make(chan struct{}, 1),
		done:              make(chan struct{}),
	}
	m.cond = sync.NewCond(&m.mu)
	guard.Go("livedoor.mux.reader", m.readerLoop)
	guard.Go("livedoor.mux.writer", m.writerLoop)
	return m
}

// Done returns a channel that closes when the multiplexer link closes.
func (m *Mux) Done() <-chan struct{} {
	return m.done
}

// Close closes the multiplexer and underlying transport once, waking every
// blocked Read, Write, Open, Accept, and WaitHelloAck.
func (m *Mux) Close() error {
	return m.shutdown(ErrLinkClosed)
}

func (m *Mux) fail(err error) {
	if err == nil {
		err = ErrProtocol
	}
	_ = m.shutdown(err)
}

func (m *Mux) shutdown(cause error) error {
	var rwcErr error
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		if m.closeErr == nil {
			m.closeErr = cause
		}
		for _, ch := range m.channels {
			ch.failLocked(ErrLinkClosed)
		}
		m.cond.Broadcast()
		m.mu.Unlock()

		close(m.done)
		m.wakeWriter()
		rwcErr = m.rwc.Close()
	})
	return rwcErr
}

func (m *Mux) wakeWriter() {
	select {
	case m.writeWake <- struct{}{}:
	default:
	}
}

func (m *Mux) enqueueControlLocked(f frame) {
	if m.closed {
		return
	}
	m.ctrlQueue = append(m.ctrlQueue, f)
	m.wakeWriter()
}

func (m *Mux) removeChannelLocked(ch *Channel) {
	if _, ok := m.channels[ch.id]; !ok {
		return
	}
	delete(m.channels, ch.id)
	for i, id := range m.channelOrder {
		if id == ch.id {
			m.channelOrder = append(m.channelOrder[:i], m.channelOrder[i+1:]...)
			if m.rrIndex > i {
				m.rrIndex--
			}
			break
		}
	}
	// Release any unread bytes that had already deducted from the link receive
	// allowance so closing or resetting a channel never leaks link credit.
	bufLen := len(ch.readBuf)
	unread := bufLen + ch.unreturnedBytes
	ch.readBuf = nil
	ch.unreturnedBytes = 0
	if unread > 0 {
		m.totalBufferedBytes -= bufLen
		if m.totalBufferedBytes < 0 {
			m.totalBufferedBytes = 0
		}
		m.linkUnreturnedBytes += unread
		m.maybeSendLinkCreditLocked()
	}
	// Refund any unsent data chunks that had reserved link send credit.
	for _, chunk := range ch.outQueue {
		m.linkSendCredit += len(chunk.data)
		if chunk.done != nil {
			chunk.done <- ch.writeErrLocked()
			close(chunk.done)
		}
	}
	ch.outQueue = nil
	m.cond.Broadcast()
}

func (m *Mux) maybeSendLinkCreditLocked() {
	if m.closed || m.linkUnreturnedBytes <= 0 {
		return
	}
	if m.linkUnreturnedBytes >= m.limits.LinkWindow/4 || m.totalBufferedBytes == 0 {
		grant := m.linkUnreturnedBytes
		m.linkUnreturnedBytes = 0
		m.linkRecvAllowance += grant
		m.enqueueControlLocked(frame{
			op:      OpCredit,
			channel: 0,
			payload: encodeUint32(uint32(grant)),
		})
	}
}

// Open opens a new odd-numbered channel from the viewer side and waits for the
// host to reply with OpOpened or OpRefused.
func (m *Mux) Open(ctx context.Context, chat string) (*Channel, error) {
	if m.role != Viewer {
		return nil, ErrInvalidRole
	}
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, ErrLinkClosed
	}
	if len(m.channels) >= m.limits.MaxChannelsPerLink {
		m.mu.Unlock()
		return nil, &RefusedError{Reason: ReasonTooMany}
	}
	if m.nextChanID == 0 {
		m.nextChanID = 1
	} else {
		m.nextChanID += 2
	}
	id := m.nextChanID
	ch := newChannel(m, id)
	m.channels[id] = ch
	m.channelOrder = append(m.channelOrder, id)

	payload, err := json.Marshal(OpenRequest{Chat: chat, V: 1})
	if err != nil {
		delete(m.channels, id)
		m.mu.Unlock()
		return nil, err
	}
	m.enqueueControlLocked(frame{
		op:      OpOpen,
		channel: id,
		payload: payload,
	})
	m.mu.Unlock()

	select {
	case <-ch.openedCh:
		m.mu.Lock()
		refused := ch.refused
		reason := ch.reason
		closed := m.closed
		m.mu.Unlock()
		if refused {
			return nil, &RefusedError{Reason: reason}
		}
		if closed {
			return nil, ErrLinkClosed
		}
		return ch, nil
	case <-m.done:
		m.mu.Lock()
		refused, reason := ch.refused, ch.reason
		m.mu.Unlock()
		if refused {
			return nil, &RefusedError{Reason: reason}
		}
		return nil, ErrLinkClosed
	case <-ctx.Done():
		m.mu.Lock()
		if !ch.opened && !ch.ended {
			ch.ended = true
			ch.closeDone()
			m.removeChannelLocked(ch)
			m.enqueueControlLocked(frame{
				op:      OpClose,
				channel: id,
			})
		}
		m.mu.Unlock()
		return nil, ctx.Err()
	}
}

// Accept waits on the host side for the next incoming channel open request.
// The caller must then call ch.Accept() or ch.Refuse(reason).
func (m *Mux) Accept(ctx context.Context) (*Channel, OpenRequest, error) {
	if m.role != Host {
		return nil, OpenRequest{}, ErrInvalidRole
	}
	select {
	case item := <-m.acceptCh:
		return item.ch, item.req, nil
	case <-m.done:
		return nil, OpenRequest{}, ErrLinkClosed
	case <-ctx.Done():
		return nil, OpenRequest{}, ctx.Err()
	}
}

// SendPing sends an OpPing frame on channel 0 with seq.
func (m *Mux) SendPing(seq uint32) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrLinkClosed
	}
	m.enqueueControlLocked(frame{
		op:      OpPing,
		channel: 0,
		payload: encodeUint32(seq),
	})
	return nil
}

// SendSignal sends one signalling payload as an OpSignal frame on channel 0. It
// refuses a payload over SignalMax and refuses to send more than SignalRate
// frames in a minute, because the far end closes a link that does either.
func (m *Mux) SendSignal(payload []byte) error {
	if len(payload) > SignalMax {
		return ErrSignalTooLarge
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrLinkClosed
	}
	if !m.noteSignalLocked(&m.signalsOut) {
		return ErrTooManySignals
	}
	m.enqueueControlLocked(frame{
		op:      OpSignal,
		channel: 0,
		payload: append([]byte(nil), payload...),
	})
	return nil
}

// noteSignalLocked records one signalling frame against the per-minute rate and
// reports whether it is within it.
func (m *Mux) noteSignalLocked(window *[]time.Time) bool {
	now := m.clock.Now()
	kept := (*window)[:0]
	for _, at := range *window {
		if now.Sub(at) < time.Minute {
			kept = append(kept, at)
		}
	}
	*window = kept
	if len(*window) >= SignalRate {
		return false
	}
	*window = append(*window, now)
	return true
}

// SendHelloAck sends an OpHelloAck frame on channel 0 from the host.
func (m *Mux) SendHelloAck(ack HelloAck) error {
	if m.role != Host {
		return ErrInvalidRole
	}
	if ack.V == 0 {
		ack.V = 1
	}
	payload, err := json.Marshal(ack)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrLinkClosed
	}
	m.enqueueControlLocked(frame{
		op:      OpHelloAck,
		channel: 0,
		payload: payload,
	})
	return nil
}

// HelloAck returns the received HelloAck if one has arrived on the link.
func (m *Mux) HelloAck() (HelloAck, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.helloAck, m.hasHelloAck
}

// WaitHelloAck waits until an OpHelloAck frame arrives, the context is cancelled,
// or the link closes.
func (m *Mux) WaitHelloAck(ctx context.Context) (HelloAck, error) {
	select {
	case <-m.helloAckCh:
		m.mu.Lock()
		ack := m.helloAck
		m.mu.Unlock()
		return ack, nil
	case <-m.done:
		m.mu.Lock()
		ack, ok := m.helloAck, m.hasHelloAck
		m.mu.Unlock()
		if ok {
			return ack, nil
		}
		return HelloAck{}, ErrLinkClosed
	case <-ctx.Done():
		return HelloAck{}, ctx.Err()
	}
}

func (m *Mux) readerLoop() {
	for {
		f, err := readFrame(m.rwc, m.limits.ChunkBytes, m.cfg.OnFrameRead)
		if err != nil {
			m.fail(err)
			return
		}
		if err := m.handleFrame(f); err != nil {
			m.fail(err)
			return
		}
	}
}

func (m *Mux) handleFrame(f frame) error {
	switch f.op {
	case OpOpen:
		if m.role != Host || f.channel == 0 || f.channel%2 == 0 {
			return ErrProtocol
		}
		var req OpenRequest
		if err := json.Unmarshal(f.payload, &req); err != nil {
			return ErrProtocol
		}
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return nil
		}
		if _, exists := m.channels[f.channel]; exists {
			m.mu.Unlock()
			return ErrProtocol
		}
		if len(m.channels) >= m.limits.MaxChannelsPerLink {
			m.enqueueControlLocked(frame{
				op:      OpRefused,
				channel: f.channel,
				payload: []byte(ReasonTooMany),
			})
			m.mu.Unlock()
			return nil
		}
		ch := newChannel(m, f.channel)
		ch.pendingAccept = true
		m.channels[f.channel] = ch
		m.channelOrder = append(m.channelOrder, f.channel)
		m.mu.Unlock()

		select {
		case m.acceptCh <- acceptItem{ch: ch, req: req}:
		case <-m.done:
		}
		return nil

	case OpOpened:
		if m.role != Viewer || f.channel == 0 {
			return ErrProtocol
		}
		m.mu.Lock()
		ch, ok := m.channels[f.channel]
		if !ok || ch.opened {
			m.mu.Unlock()
			return ErrProtocol
		}
		ch.opened = true
		ch.signalOpened()
		m.mu.Unlock()
		return nil

	case OpRefused:
		if m.role != Viewer || f.channel == 0 {
			return ErrProtocol
		}
		reason := string(f.payload)
		m.mu.Lock()
		ch, ok := m.channels[f.channel]
		if !ok || ch.opened {
			m.mu.Unlock()
			return ErrProtocol
		}
		ch.refused = true
		ch.ended = true
		ch.reason = reason
		ch.closeDone()
		m.removeChannelLocked(ch)
		ch.signalOpened()
		m.mu.Unlock()
		return nil

	case OpData:
		if f.channel == 0 {
			return ErrProtocol
		}
		n := len(f.payload)
		m.mu.Lock()
		if n > m.linkRecvAllowance {
			m.mu.Unlock()
			return fmt.Errorf("%w: link credit overrun (%d > %d)", ErrProtocol, n, m.linkRecvAllowance)
		}
		ch, ok := m.channels[f.channel]
		if !ok {
			// If the channel ID was never valid or never opened, fail the link;
			// if it was recently closed locally while data was in flight, return
			// the link credit so the remaining channels do not lose link window.
			if f.channel%2 == 0 || (m.role == Viewer && f.channel > m.nextChanID) {
				m.mu.Unlock()
				return ErrProtocol
			}
			m.linkRecvAllowance -= n
			m.linkUnreturnedBytes += n
			m.maybeSendLinkCreditLocked()
			m.mu.Unlock()
			return nil
		}
		if n > ch.recvAllowance {
			m.mu.Unlock()
			return fmt.Errorf("%w: channel %d credit overrun (%d > %d)", ErrProtocol, f.channel, n, ch.recvAllowance)
		}
		ch.recvAllowance -= n
		m.linkRecvAllowance -= n
		if ch.ended {
			m.linkUnreturnedBytes += n
			m.maybeSendLinkCreditLocked()
			m.mu.Unlock()
			return nil
		}
		ch.readBuf = append(ch.readBuf, f.payload...)
		m.totalBufferedBytes += n
		m.cond.Broadcast()
		m.mu.Unlock()
		return nil

	case OpCredit:
		inc, ok := decodeUint32(f.payload)
		if !ok {
			return ErrProtocol
		}
		m.mu.Lock()
		if f.channel == 0 {
			m.linkSendCredit += int(inc)
		} else {
			if ch, exists := m.channels[f.channel]; exists {
				ch.sendCredit += int(inc)
			}
		}
		m.cond.Broadcast()
		m.mu.Unlock()
		return nil

	case OpClose:
		if f.channel == 0 {
			return ErrProtocol
		}
		reason := string(f.payload)
		m.mu.Lock()
		if ch, ok := m.channels[f.channel]; ok {
			if !ch.ended {
				ch.ended = true
				ch.reason = reason
			}
			ch.remoteClosed = true
			ch.closeDone()
			// Free any pending outbound chunks, and remove the channel once its
			// read buffer has been drained.
			for _, chunk := range ch.outQueue {
				m.linkSendCredit += len(chunk.data)
				if chunk.done != nil {
					chunk.done <- ch.writeErrLocked()
					close(chunk.done)
				}
			}
			ch.outQueue = nil
			if len(ch.readBuf) == 0 {
				m.removeChannelLocked(ch)
			}
			m.cond.Broadcast()
		}
		m.mu.Unlock()
		return nil

	case OpReset:
		if f.channel == 0 {
			return ErrProtocol
		}
		reason := string(f.payload)
		m.mu.Lock()
		if ch, ok := m.channels[f.channel]; ok {
			ch.ended = true
			ch.remoteReset = true
			ch.reason = reason
			ch.closeDone()
			m.removeChannelLocked(ch)
			m.cond.Broadcast()
		}
		m.mu.Unlock()
		return nil

	case OpPing:
		seq, ok := decodeUint32(f.payload)
		if !ok || f.channel != 0 {
			return ErrProtocol
		}
		m.mu.Lock()
		m.enqueueControlLocked(frame{
			op:      OpPong,
			channel: 0,
			payload: encodeUint32(seq),
		})
		m.mu.Unlock()
		return nil

	case OpPong:
		seq, ok := decodeUint32(f.payload)
		if !ok || f.channel != 0 {
			return ErrProtocol
		}
		if m.cfg.OnPong != nil {
			m.cfg.OnPong(seq)
		}
		return nil

	case OpSignal:
		if f.channel != 0 || len(f.payload) > SignalMax {
			return ErrProtocol
		}
		m.mu.Lock()
		within := m.noteSignalLocked(&m.signalsIn)
		m.mu.Unlock()
		if !within {
			return ErrTooManySignals
		}
		if m.cfg.OnSignal != nil {
			m.cfg.OnSignal(f.payload)
		}
		return nil

	case OpHelloAck:
		if f.channel != 0 {
			return ErrProtocol
		}
		var ack HelloAck
		if err := json.Unmarshal(f.payload, &ack); err != nil {
			return ErrProtocol
		}
		m.mu.Lock()
		m.helloAck = ack
		m.hasHelloAck = true
		m.mu.Unlock()
		m.helloAckOnce.Do(func() {
			close(m.helloAckCh)
		})
		return nil
	}
	return nil
}

func (m *Mux) refillTokensLocked() {
	now := m.clock.Now()
	elapsed := now.Sub(m.lastRefill)
	if elapsed > 0 {
		m.tokens += elapsed.Seconds() * float64(m.limits.Rate)
		if m.tokens > float64(m.limits.RateBurst) {
			m.tokens = float64(m.limits.RateBurst)
		}
		m.lastRefill = now
	}
}

func (m *Mux) peekNextDataBytesLocked() int {
	n := len(m.channelOrder)
	if n == 0 {
		return 0
	}
	if m.rrIndex >= n || m.rrIndex < 0 {
		m.rrIndex = 0
	}
	for i := 0; i < n; i++ {
		idx := (m.rrIndex + i) % n
		id := m.channelOrder[idx]
		ch := m.channels[id]
		if ch != nil && len(ch.outQueue) > 0 {
			return len(ch.outQueue[0].data)
		}
	}
	return 0
}

func (m *Mux) popNextDataChunkLocked() (uint32, *outChunk, bool) {
	n := len(m.channelOrder)
	if n == 0 {
		return 0, nil, false
	}
	if m.rrIndex >= n || m.rrIndex < 0 {
		m.rrIndex = 0
	}
	for i := 0; i < n; i++ {
		idx := (m.rrIndex + i) % n
		id := m.channelOrder[idx]
		ch := m.channels[id]
		if ch != nil && len(ch.outQueue) > 0 {
			chunk := ch.outQueue[0]
			ch.outQueue = ch.outQueue[1:]
			m.rrIndex = (idx + 1) % n
			return id, chunk, true
		}
	}
	return 0, nil, false
}

func (m *Mux) writerLoop() {
	var paceTimer Timer
	stopPaceTimer := func() {
		if paceTimer != nil {
			paceTimer.Stop()
			paceTimer = nil
		}
	}
	defer stopPaceTimer()

	for {
		m.mu.Lock()
		if m.closed {
			m.mu.Unlock()
			return
		}

		// Control frames always jump ahead of queued data frames and are never
		// delayed by the data token bucket.
		if len(m.ctrlQueue) > 0 {
			f := m.ctrlQueue[0]
			m.ctrlQueue = m.ctrlQueue[1:]
			m.mu.Unlock()

			n, err := writeFrame(m.rwc, f, m.limits.ChunkBytes)
			if err != nil {
				m.fail(err)
				return
			}
			if m.cfg.OnFrameWritten != nil {
				m.cfg.OnFrameWritten(n)
			}
			continue
		}

		need := m.peekNextDataBytesLocked()
		if need > 0 {
			m.refillTokensLocked()
			if m.tokens >= float64(need) {
				stopPaceTimer()
				chID, chunk, ok := m.popNextDataChunkLocked()
				if ok {
					m.tokens -= float64(len(chunk.data))
					m.mu.Unlock()

					n, err := writeFrame(m.rwc, frame{
						op:      OpData,
						channel: chID,
						payload: chunk.data,
					}, m.limits.ChunkBytes)
					if chunk.done != nil {
						chunk.done <- err
						close(chunk.done)
					}
					if err != nil {
						m.fail(err)
						return
					}
					if m.cfg.OnFrameWritten != nil {
						m.cfg.OnFrameWritten(n)
					}
					continue
				}
			}

			// Not enough tokens for the next queued data chunk; start a pacing
			// timer on Clock if one is not already running.
			if paceTimer == nil {
				deficit := float64(need) - m.tokens
				wait := time.Duration(math.Ceil(deficit / float64(m.limits.Rate) * float64(time.Second)))
				if wait < time.Nanosecond {
					wait = time.Nanosecond
				}
				paceTimer = m.clock.NewTimer(wait)
			}
			timerC := paceTimer.C()
			m.mu.Unlock()

			select {
			case <-timerC:
				stopPaceTimer()
			case <-m.writeWake:
			case <-m.done:
				return
			}
			continue
		}

		stopPaceTimer()
		m.mu.Unlock()

		select {
		case <-m.writeWake:
		case <-m.done:
			return
		}
	}
}

// Channel is a single flow-controlled stream multiplexed over a Mux.
type Channel struct {
	mux *Mux
	id  uint32

	opened        bool
	refused       bool
	pendingAccept bool
	openedOnce    sync.Once
	openedCh      chan struct{}

	sendCredit      int
	recvAllowance   int
	unreturnedBytes int
	readBuf         []byte
	outQueue        []*outChunk

	ended        bool
	remoteClosed bool
	remoteReset  bool
	failErr      error
	reason       string

	doneOnce sync.Once
	done     chan struct{}
}

func newChannel(m *Mux, id uint32) *Channel {
	return &Channel{
		mux:           m,
		id:            id,
		openedCh:      make(chan struct{}),
		sendCredit:    m.limits.ChannelWindow,
		recvAllowance: m.limits.ChannelWindow,
		done:          make(chan struct{}),
	}
}

func (c *Channel) signalOpened() {
	c.openedOnce.Do(func() {
		close(c.openedCh)
	})
}

func (c *Channel) closeDone() {
	c.doneOnce.Do(func() {
		close(c.done)
	})
}

func (c *Channel) failLocked(err error) {
	if !c.ended {
		c.ended = true
	}
	if c.failErr == nil {
		c.failErr = err
	}
	for _, chunk := range c.outQueue {
		if chunk.done != nil {
			chunk.done <- err
			close(chunk.done)
		}
	}
	c.outQueue = nil
	c.closeDone()
	c.signalOpened()
}

// ID returns the channel's odd wire identifier.
func (c *Channel) ID() uint32 {
	return c.id
}

// Done returns a channel that closes when the channel ends for any reason.
func (c *Channel) Done() <-chan struct{} {
	return c.done
}

// Reason returns the closed-enum reason string that ended the channel, or ""
// for a clean local Close.
func (c *Channel) Reason() string {
	c.mux.mu.Lock()
	defer c.mux.mu.Unlock()
	return c.reason
}

// Accept confirms a pending host channel returned by Mux.Accept and sends
// OpOpened to the viewer.
func (c *Channel) Accept() error {
	m := c.mux
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrLinkClosed
	}
	if !c.pendingAccept {
		return ErrProtocol
	}
	c.pendingAccept = false
	c.opened = true
	m.enqueueControlLocked(frame{
		op:      OpOpened,
		channel: c.id,
	})
	return nil
}

// Refuse rejects a pending host channel returned by Mux.Accept and sends
// OpRefused with reason to the viewer.
func (c *Channel) Refuse(reason string) error {
	m := c.mux
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return ErrLinkClosed
	}
	if !c.pendingAccept {
		return ErrProtocol
	}
	c.pendingAccept = false
	c.ended = true
	c.reason = reason
	c.closeDone()
	m.removeChannelLocked(c)
	m.enqueueControlLocked(frame{
		op:      OpRefused,
		channel: c.id,
		payload: []byte(reason),
	})
	return nil
}

// Read reads buffered data from the channel and grants channel and link credit
// back to the peer when unreturned consumed bytes reach a quarter window or the
// read buffer drains empty.
func (c *Channel) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	m := c.mux
	m.mu.Lock()
	for len(c.readBuf) == 0 {
		if c.remoteReset {
			reason := c.reason
			m.mu.Unlock()
			return 0, &ChannelError{Op: "reset", Why: reason}
		}
		if c.remoteClosed {
			reason := c.reason
			m.removeChannelLocked(c)
			m.mu.Unlock()
			if reason != "" {
				return 0, &ChannelError{Op: "close", Why: reason, Err: io.EOF}
			}
			return 0, io.EOF
		}
		if m.closed || c.failErr != nil {
			m.mu.Unlock()
			return 0, ErrLinkClosed
		}
		if c.ended {
			reason := c.reason
			m.mu.Unlock()
			if reason != "" {
				return 0, &ChannelError{Op: "close", Why: reason, Err: io.EOF}
			}
			return 0, io.EOF
		}
		m.cond.Wait()
	}

	if c.remoteReset {
		reason := c.reason
		m.mu.Unlock()
		return 0, &ChannelError{Op: "reset", Why: reason}
	}

	n := copy(p, c.readBuf)
	c.readBuf = c.readBuf[n:]
	if len(c.readBuf) == 0 {
		c.readBuf = nil
	}
	m.totalBufferedBytes -= n
	if m.totalBufferedBytes < 0 {
		m.totalBufferedBytes = 0
	}

	if !m.closed && !c.ended {
		c.unreturnedBytes += n
		if c.unreturnedBytes >= m.limits.ChannelWindow/4 || len(c.readBuf) == 0 {
			grant := c.unreturnedBytes
			c.unreturnedBytes = 0
			c.recvAllowance += grant
			m.enqueueControlLocked(frame{
				op:      OpCredit,
				channel: c.id,
				payload: encodeUint32(uint32(grant)),
			})
		}
	}
	if !m.closed {
		m.linkUnreturnedBytes += n
		m.maybeSendLinkCreditLocked()
	}
	if len(c.readBuf) == 0 && c.remoteClosed {
		m.removeChannelLocked(c)
	}
	m.mu.Unlock()
	return n, nil
}

func (c *Channel) writeErrLocked() error {
	if c.remoteReset {
		return &ChannelError{Op: "reset", Why: c.reason}
	}
	if c.mux.closed || c.failErr != nil {
		return ErrLinkClosed
	}
	if c.reason != "" {
		return &ChannelError{Op: "close", Why: c.reason, Err: io.ErrClosedPipe}
	}
	return io.ErrClosedPipe
}

// Write cuts p into ChunkBytes slices, blocks while out of channel or link
// credit, and queues data frames for the round-robin writer goroutine.
func (c *Channel) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	m := c.mux
	total := 0
	remaining := p
	var waiters []chan error

	for len(remaining) > 0 {
		m.mu.Lock()
		for (c.sendCredit <= 0 || m.linkSendCredit <= 0) && !c.ended && !m.closed {
			m.cond.Wait()
		}
		if c.ended || m.closed {
			err := c.writeErrLocked()
			m.mu.Unlock()
			for _, w := range waiters {
				if wErr := <-w; wErr != nil {
					return total, wErr
				}
			}
			return total, err
		}

		for len(remaining) > 0 && c.sendCredit > 0 && m.linkSendCredit > 0 {
			take := len(remaining)
			if take > m.limits.ChunkBytes {
				take = m.limits.ChunkBytes
			}
			if take > c.sendCredit {
				take = c.sendCredit
			}
			if take > m.linkSendCredit {
				take = m.linkSendCredit
			}
			c.sendCredit -= take
			m.linkSendCredit -= take

			buf := make([]byte, take)
			copy(buf, remaining[:take])
			remaining = remaining[take:]
			total += take

			var doneCh chan error
			if len(remaining) == 0 {
				doneCh = make(chan error, 1)
				waiters = append(waiters, doneCh)
			}
			c.outQueue = append(c.outQueue, &outChunk{
				data: buf,
				done: doneCh,
			})
		}
		m.wakeWriter()
		m.mu.Unlock()
	}

	for _, w := range waiters {
		if err := <-w; err != nil {
			return total, err
		}
	}
	return total, nil
}

// Close closes the channel cleanly with an empty reason.
func (c *Channel) Close() error {
	return c.CloseWithReason("")
}

// CloseWithReason closes the channel, sends OpClose with reason, and releases
// its credit state.
func (c *Channel) CloseWithReason(reason string) error {
	m := c.mux
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.ended {
		return nil
	}
	c.ended = true
	c.reason = reason
	c.closeDone()
	m.removeChannelLocked(c)
	m.enqueueControlLocked(frame{
		op:      OpClose,
		channel: c.id,
		payload: []byte(reason),
	})
	return nil
}

// Reset aborts the channel, sends OpReset with reason, and wakes any blocked
// reader or writer immediately.
func (c *Channel) Reset(reason string) error {
	m := c.mux
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.ended {
		return nil
	}
	c.ended = true
	c.remoteReset = true
	c.reason = reason
	c.closeDone()
	m.removeChannelLocked(c)
	m.enqueueControlLocked(frame{
		op:      OpReset,
		channel: c.id,
		payload: []byte(reason),
	})
	return nil
}
