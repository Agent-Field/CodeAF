package directroad

import (
	"errors"
	"io"
	"sync"

	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/pion/webrtc/v4"
)

// errConnectionFailed is reported when the peer connection reaches Failed or
// Closed before or after the data channel opens.
var errConnectionFailed = errors.New("directroad: peer connection failed")

// pcSession wraps a pion PeerConnection and its single pre-negotiated data
// channel for either the viewer offerer or the host answerer. It queues remote
// ICE candidates that arrive before the remote description is applied and
// guarantees idempotent teardown of both the stream and the peer connection.
type pcSession struct {
	id string
	pc *webrtc.PeerConnection
	dc *webrtc.DataChannel
	s  livedoor.Signaller

	openCh chan io.ReadWriteCloser
	errCh  chan error

	mu          sync.Mutex
	remoteReady bool
	pendingCand []webrtc.ICECandidateInit
	stream      *byteStream
	closed      bool
	closeOnce   sync.Once
}

// newPCSession constructs the peer connection and registers trickle-ICE,
// connection-state, and data-channel open callbacks.
func newPCSession(id string, cfg Config, s livedoor.Signaller) (*pcSession, error) {
	pc, dc, err := newPeerConnection(cfg)
	if err != nil {
		return nil, err
	}

	sess := &pcSession{
		id:     id,
		pc:     pc,
		dc:     dc,
		s:      s,
		openCh: make(chan io.ReadWriteCloser, 1),
		errCh:  make(chan error, 1),
	}

	pc.OnICECandidate(sess.onLocalCandidate)
	pc.OnConnectionStateChange(sess.onConnectionState)
	dc.OnOpen(sess.onChannelOpen)

	return sess, nil
}

// onLocalCandidate trickles each gathered local candidate to the peer as soon
// as pion produces it; a send failure ends the attempt.
func (p *pcSession) onLocalCandidate(cand *webrtc.ICECandidate) {
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if closed {
		return
	}
	if err := sendCandidate(p.s, p.id, cand); err != nil {
		p.fail(err)
	}
}

// onConnectionState closes the byte stream or fails the pending attempt when
// the peer connection reaches Failed or Closed. Disconnected is ignored
// because a briefly interrupted path can recover before the failed timeout.
func (p *pcSession) onConnectionState(state webrtc.PeerConnectionState) {
	if state != webrtc.PeerConnectionStateFailed && state != webrtc.PeerConnectionStateClosed {
		return
	}
	p.mu.Lock()
	st := p.stream
	p.mu.Unlock()
	if st != nil {
		_ = st.Close()
	}
	p.fail(errConnectionFailed)
}

// onChannelOpen detaches the opened data channel, wraps it in a byteStream,
// and delivers it on openCh at most once.
func (p *pcSession) onChannelOpen() {
	raw, err := p.dc.Detach()
	if err != nil {
		p.fail(err)
		return
	}

	st := newByteStream(raw, p.closePC)

	p.mu.Lock()
	if p.closed || p.stream != nil {
		p.mu.Unlock()
		_ = st.Close()
		return
	}
	p.stream = st
	p.mu.Unlock()

	if state := p.pc.ConnectionState(); state == webrtc.PeerConnectionStateFailed || state == webrtc.PeerConnectionStateClosed {
		_ = st.Close()
		p.fail(errConnectionFailed)
		return
	}

	select {
	case p.openCh <- st:
	default:
	}
}

// setRemoteDescription applies desc to the peer connection and flushes any
// trickled remote candidates that arrived ahead of it, in order.
func (p *pcSession) setRemoteDescription(desc webrtc.SessionDescription) error {
	if err := p.pc.SetRemoteDescription(desc); err != nil {
		return err
	}

	p.mu.Lock()
	p.remoteReady = true
	queued := p.pendingCand
	p.pendingCand = nil
	p.mu.Unlock()

	for _, cand := range queued {
		if err := p.pc.AddICECandidate(cand); err != nil {
			return err
		}
	}
	return nil
}

// addCandidate either queues a remote candidate that arrived before the remote
// description or adds it directly to the peer connection.
func (p *pcSession) addCandidate(sig livedoor.Signal) error {
	init, ok := candidateInitFromSignal(sig)
	if !ok {
		return nil
	}

	p.mu.Lock()
	if !p.remoteReady {
		p.pendingCand = append(p.pendingCand, init)
		p.mu.Unlock()
		return nil
	}
	p.mu.Unlock()

	return p.pc.AddICECandidate(init)
}

// fail records the first fatal error for a connecting attempt.
func (p *pcSession) fail(err error) {
	select {
	case p.errCh <- err:
	default:
	}
}

// closePC closes the underlying pion PeerConnection at most once.
func (p *pcSession) closePC() {
	p.closeOnce.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.mu.Unlock()
		_ = p.pc.Close()
	})
}

// abort tears down a connecting attempt and waits for pion's internal worker
// goroutines to exit so failed attempts never leak resources.
func (p *pcSession) abort() {
	p.mu.Lock()
	p.closed = true
	st := p.stream
	p.mu.Unlock()

	if st != nil {
		_ = st.Close()
	}
	p.closePC()
	_ = p.pc.GracefulClose()
}
