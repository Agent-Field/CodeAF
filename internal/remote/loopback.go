package remote

// loopback.go is this protocol's TEST DOUBLE FOR THE TRANSPORT, and it is a
// normal file rather than a _test.go one on purpose: five packages outside this
// one need to drive a real client against a real engine, and a helper that only
// existed inside `go test ./internal/remote` would have been copied five times.
//
// WHAT IT REPLACES IS ssh AND NOTHING ELSE. The client is the real client, the
// server is the real server, the frames are the real frames — the only thing
// faked is the pipe between them, which becomes a pair of in-memory halves
// instead of a child process's stdin and stdout. That is the whole reason
// [Dial] and [Serve] were written against io interfaces rather than against
// exec.Cmd, and this file is that decision being spent.
//
// IT IS ALSO THE LANE CONTRACT. Several lanes of the remote-access wave build
// against the version-2 wire at once — the session host, the surface's resume
// loop, attachments, the relay tunnel — and none of them can wait for the
// others to exist. Each one drives [Loopback] with its own fake half and knows
// its bytes are the bytes the real other half will send, because there is only
// one encoder in the tree and both ends of this pipe use it.

import (
	"io"
	"net"
	"sync"
)

// Pipe is two connected halves of one connection: what the surface would have
// got from an ssh child's pipes, without the ssh child.
//
// It is net.Pipe rather than an os.Pipe pair because net.Pipe is synchronous
// and in-memory — no file descriptors, no buffering to make a test's timing
// lie, and a close on either half is seen immediately by the other, which is
// exactly the event this protocol's dead-connection handling turns on.
func Pipe() (surface, engine io.ReadWriteCloser) {
	a, b := net.Pipe()
	return a, b
}

// Loop is a live client and the engine it is talking to, both in this process.
type Loop struct {
	// Client is the surface's half — the real [Client], dialled and handshaken.
	Client *Client
	// Served is the error [Serve] finished with, readable after Close. It is a
	// channel rather than a field because the engine goroutine outlives the
	// call that started it, and a test that wants to know how the far end died
	// has to be able to wait for it.
	Served <-chan error

	closeOnce sync.Once
	surface   io.ReadWriteCloser
}

// Loopback dials a real client against a real engine over an in-memory pipe.
//
// The engine runs on its own goroutine, exactly as it does under ssh, so
// everything about the ordering — a call answered while a turn's events are
// arriving, a stream that outlives the call that opened it — behaves the way it
// does on a real link. What it does not reproduce is LATENCY, and no test
// should read a pass here as evidence about a slow connection; the deadline
// laws in client.go are about a link that has stopped answering, and a test for
// those hands [Dial] a half that never writes.
func Loopback(hello Hello, opts Options) (*Loop, error) {
	surface, engine := Pipe()
	served := make(chan error, 1)
	go func() {
		// The engine reads the pipe and writes the pipe, which is Serve's own
		// shape: one reader, one writer, no idea what is on the other side.
		served <- Serve(engine, engine, opts)
		_ = engine.Close()
	}()
	client, err := Dial(surface, "loopback", hello)
	if err != nil {
		_ = surface.Close()
		return nil, err
	}
	return &Loop{Client: client, Served: served, surface: surface}, nil
}

// Close ends the connection from the surface's side, which is the ordinary way
// a link dies: the pipe shuts and the engine's reader sees EOF. It is idempotent
// because a test that closes in a defer and again on the happy path is a test
// that should not have to care.
func (l *Loop) Close() error {
	var err error
	l.closeOnce.Do(func() {
		if l.Client != nil {
			err = l.Client.Close()
			return
		}
		err = l.surface.Close()
	})
	return err
}

// Cut kills the link WITHOUT the surface saying goodbye, which is the event
// every reconnect story is actually about: a laptop that slept, a network that
// went away, an ssh that was killed.
//
// IT IS DELIBERATELY NOT [Loop.Close]. Close sets the client's own `closing`
// flag, so the EOF that follows is read as a door being shut and the surface
// says "this connection is closed". Cut sets nothing, so the same EOF is read
// as a connection that was lost and the surface says the sentence a person
// needs — which is the difference the version-2 detach frame exists to make
// visible, and therefore the difference a test of it must be able to stage.
func (l *Loop) Cut() error {
	return l.surface.Close()
}
