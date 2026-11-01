package taskdoor

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Agent-Field/codeaf/internal/identity"
	"github.com/Agent-Field/codeaf/internal/pair"
	"github.com/Agent-Field/codeaf/internal/relay"
)

// Dial reaches the chosen device within one bound covering relay, Noise and
// hello. The returned connection lives until it or the caller's context closes.
func Dial(ctx context.Context, service string, id identity.Identity, dev identity.Dev, targetDeviceID, targetName string) (*Conn, error) {
	return dial(ctx, service, id, dev, targetDeviceID, targetName, wallClock{})
}

// DialWithClock supplies watch wakeups as well as time readings for callers
// that need a deterministic clock. Dial uses the wall clock.
func DialWithClock(ctx context.Context, service string, id identity.Identity, dev identity.Dev, targetDeviceID, targetName string, clock Clock) (*Conn, error) {
	return dial(ctx, service, id, dev, targetDeviceID, targetName, clockOrWall(clock))
}
func dial(ctx context.Context, service string, id identity.Identity, dev identity.Dev, targetDeviceID, targetName string, clock Clock) (*Conn, error) {
	timeout, cancel := context.WithTimeout(ctx, DialWithin)
	defer cancel()
	key, err := DoorKey(id, dev.ID())
	if err != nil {
		return nil, err
	}
	target, err := DoorKey(id, targetDeviceID)
	if err != nil {
		return nil, err
	}
	stream, err := relay.Dial(timeout, service, relay.NameFor(target.PublicKey().Bytes()))
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New(NotTakingTasks(targetName))
	}
	failed := true
	defer func() {
		if failed {
			_ = stream.Close()
		}
	}()
	stop := context.AfterFunc(timeout, func() { _ = stream.Close() })
	defer stop()
	deadline, _ := timeout.Deadline()
	setDeadline(stream, deadline)
	tunnel, err := pair.ConnectWithKey(stream, target.PublicKey().Bytes(), key)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if errors.Is(err, pair.ErrNotThatMachine) {
			return nil, errors.New(CouldNotConfirm(targetName))
		}
		return nil, errors.New(NotTakingTasks(targetName))
	}
	if err := sendHello(tunnel, dev); err != nil {
		return nil, errors.New(NotTakingTasks(targetName))
	}
	frame, err := ReadFrame(tunnel)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New(NotTakingTasks(targetName))
	}
	if frame.Type == "refused" {
		var no Refused
		if frame.Decode(&no) != nil || no.Reason == "" {
			return nil, errors.New(CouldNotConfirm(targetName))
		}
		return nil, errors.New(no.Reason)
	}
	var hello Hello
	reason := ""
	switch {
	case frame.Type != "hello" || frame.Decode(&hello) != nil:
		reason = CouldNotConfirm(targetName)
	case hello.V != Version:
		reason = DifferentVersion(targetName)
	case hello.Cert.DeviceID() != targetDeviceID:
		reason = CouldNotConfirm(targetName)
	default:
		if err := checkHello(id, tunnel, hello); err != nil {
			reason = CouldNotConfirm(targetName)
		}
	}
	if reason != "" {
		_ = refuse(tunnel, reason)
		return nil, errors.New(reason)
	}
	// Stop the handshake cancellation callback before handing ownership on. A
	// callback already running has won the timeout and cannot yield a live Conn.
	if !stop() || timeout.Err() != nil {
		return nil, errors.New(NotTakingTasks(targetName))
	}
	setDeadline(stream, time.Time{})
	failed = false
	return newConn(ctx, tunnel, clock), nil
}
func setDeadline(stream io.ReadWriteCloser, when time.Time) {
	if conn, ok := stream.(interface{ SetDeadline(time.Time) error }); ok {
		_ = conn.SetDeadline(when)
	}
}

type readResult struct {
	frame Frame
	err   error
}

// Conn serializes writes across replies, watches and keepalive. A single reader
// answers pings even when the application is busy consuming earlier events.
type Conn struct {
	tunnel        *pair.Tunnel
	clock         Clock
	sendMu        sync.Mutex
	incoming      chan readResult
	done          chan struct{}
	once          sync.Once
	stopContext   func() bool
	watching      atomic.Bool
	lastFrame     atomic.Int64
	heartbeatOnce sync.Once
	ping          chan struct{}
	pong          chan struct{}
	activity      chan struct{}
}

func newConn(ctx context.Context, tunnel *pair.Tunnel, clock Clock) *Conn {
	c := &Conn{tunnel: tunnel, clock: clock, incoming: make(chan readResult, 32), done: make(chan struct{}), activity: make(chan struct{}, 1)}
	c.lastFrame.Store(clock.Now().UnixNano())
	// Installing this callback before starting the reader avoids a concurrent
	// Close observing a half-constructed connection.
	c.stopContext = context.AfterFunc(ctx, func() { c.closeTransport() })
	go c.read()
	return c
}
func (c *Conn) closeTransport() { c.once.Do(func() { close(c.done); _ = c.tunnel.Close() }) }
func (c *Conn) Close() error    { c.stopContext(); c.closeTransport(); return nil }
func (c *Conn) read() {
	defer close(c.incoming)
	defer c.closeTransport()
	for {
		frame, err := ReadFrame(c.tunnel)
		if err != nil {
			select {
			case c.incoming <- readResult{err: err}:
			case <-c.done:
			}
			return
		}
		c.lastFrame.Store(c.clock.Now().UnixNano())
		c.noteActivity()
		switch frame.Type {
		case "ping":
			// A blocked writer must not prevent the reader noticing other frames or
			// the watchdog closing a silent peer. One queued pong is enough.
			c.startHeartbeat()
			c.queuePong()
			continue
		case "pong":
			continue
		case "finished", "unknown":
			c.watching.Store(false)
		}
		select {
		case c.incoming <- readResult{frame: frame}:
		case <-c.done:
			return
		}
	}
}
func (c *Conn) Receive() (Frame, error) {
	result, ok := <-c.incoming
	if !ok {
		return Frame{}, io.EOF
	}
	return result.frame, result.err
}
func (c *Conn) sendFrame(frame Frame) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	select {
	case <-c.done:
		return io.ErrClosedPipe
	default:
	}
	if err := WriteFrame(c.tunnel, frame); err != nil {
		c.closeTransport()
		return err
	}
	return nil
}
func (c *Conn) Send(attempt string, payload any) error {
	frame, err := NewFrame(attempt, payload)
	if err != nil {
		return err
	}
	if frame.Type == "watch" {
		c.startWatch()
	}
	return c.sendFrame(frame)
}

// LastActivity includes keepalive replies, so the origin distinguishes a quiet
// worker from a computer whose connection stopped answering.
func (c *Conn) LastActivity() time.Time { return time.Unix(0, c.lastFrame.Load()) }
