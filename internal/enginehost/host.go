package enginehost

// host.go is the host itself: the process that holds the conversations.
//
// It is a small thing on purpose. It owns a socket, a lock, a map of open
// sessions and a clock; everything about what a conversation IS belongs to
// internal/remote and internal/session, and everything about how one is
// ASSEMBLED belongs to the door that calls [Run]. A host that knew how to build
// an agent would be the second assembly this tree has already refused twice
// (cmd/aforge's shared assembly states why).

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/filelock"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/remote"
)

// ErrHostRunning is [Run] finding that this workspace already has a host. It is
// not a failure and the caller must not print it: the machine is in exactly the
// state that was asked for, by another process.
var ErrHostRunning = errors.New("engine host: another host already holds this workspace")

// The idle policy, in three numbers.
//
// A CONVERSATION IS KEPT WHILE IT IS DOING ANYTHING AT ALL. Nobody attached is
// not idle: a turn still running, or a question waiting for somebody to come
// back and answer it, is the entire point of this process
// ([remote.Session.IdleSince] draws that line). What is idle is a conversation
// with no surface, no turn and no question — somebody finished and walked away
// — and it is kept for [sessionIdle] anyway, because "come back later and be in
// it" is the product, and reopening from the journal a minute after closing a
// laptop lid would be the version-1 experience with extra steps.
//
// AND THEN THE HOST LEAVES. With no conversations left and nobody attached
// there is nothing to be the host of, so it exits and the next `aforge engine`
// starts a new one — which costs one process spawn and is invisible.
//
// THIS DOES NOT PUT THE AMBIENT SIDE TO SLEEP, and that is the one interaction
// worth stating. Standing items are not held by this process: a firing runs
// inside whichever process holds the store's tick lock — any live window, or
// the operating system's timer running `aforge tick` with nobody sitting
// anywhere (cmd/aforge's chatv3_standing.go states that law). A host that exits
// hands the tick back to that timer exactly as a closed terminal does. So the
// two lifetimes are deliberately NOT married: standing work already keeps a
// machine warm on its own terms, and a host that stayed up forever to guard it
// would be a second answer to a question that already has one.
const (
	sessionIdle = 30 * time.Minute
	hostIdle    = 2 * time.Minute
	sweepEvery  = 30 * time.Second
)

// Options is what a host needs from the door that starts it, which is the same
// two things every version of this has needed: how to open a conversation, and
// which conversation a hello is asking for.
type Options struct {
	// Boot opens one conversation for a hello — cmd/aforge's bootEngine, the
	// same closure the pipe engine hands to [remote.Serve].
	//
	// It is called with the host's own lock held, so ONE CONVERSATION IS OPENED
	// AT A TIME. That is not for speed, it is for the session file: two
	// connections asking for the same conversation at the same moment must not
	// both open it, because the second would find the journal locked and mint a
	// second session under a person who asked for one.
	Boot func(remote.Hello) (*remote.Engine, error)

	// Key says which conversation a hello wants, so that two surfaces asking
	// for the same one are handed the same one. Empty from a nil Key is the
	// workspace's latest-or-new, which is what a hello naming no session means
	// everywhere else on this wire.
	Key func(remote.Hello) string
}

// Host is one workspace's conversations, and the socket they are reached
// through.
type Host struct {
	workspace string
	dir       string
	opts      Options

	listener net.Listener
	lock     *os.File

	mu       sync.Mutex
	sessions map[string]*remote.Session
	live     int
	// quiet is when the host last had nothing to do, and zero while it has
	// something. It is what [hostIdle] is measured against.
	quiet  time.Time
	closed bool

	done chan struct{}
}

// Run becomes this workspace's host and does not return until it is done: the
// idle policy retires it, a signal asks it to stop, or the listener breaks.
//
// It answers [ErrHostRunning] when another host already holds this workspace,
// which the caller treats as success — somebody else did the job.
func Run(workspace string, opts Options) error {
	if opts.Boot == nil {
		return errors.New("engine host: a host with no way to open a conversation")
	}
	dir, err := Dir(workspace)
	if err != nil {
		return err
	}
	socket, err := SocketPath(workspace)
	if err != nil {
		return err
	}

	// THE LOCK IS THE HOST'S LIFE AND NOT A MOMENT OF IT. It is taken before
	// the socket exists and released only when this process ends, so "is there
	// a host" is a question the operating system answers rather than a file
	// somebody has to remember to delete.
	lock, err := takeLock(filepath.Join(dir, lockName))
	if err != nil {
		return ErrHostRunning
	}

	// Whatever socket file is lying there belongs to a host that is gone: the
	// lock we just took proves it. A unix socket is not removed by the process
	// that made it dying, so somebody has to, and the only safe moment to do it
	// is with the lock in hand.
	_ = os.Remove(socket)
	listener, err := net.Listen("unix", socket)
	if err != nil {
		_ = releaseLock(lock)
		return fmt.Errorf("engine host: %w", err)
	}
	// The readable half of the directory's name, for a person looking at
	// ~/.aforge/v3/hosts and wondering which of these is which.
	_ = os.WriteFile(filepath.Join(dir, placeName), []byte(workspace+"\n"), 0o600)

	h := &Host{
		workspace: workspace,
		dir:       dir,
		opts:      opts,
		listener:  listener,
		lock:      lock,
		sessions:  map[string]*remote.Session{},
		quiet:     time.Now(),
		done:      make(chan struct{}),
	}
	return h.serve()
}

func (h *Host) serve() error {
	// A machine going down should flush journals rather than lose the last
	// turn, so the two signals a shutdown actually arrives as are answered the
	// same way a retirement is.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(signals)
	go func() {
		select {
		case <-signals:
			h.stop()
		case <-h.done:
		}
	}()

	guard.Go("enginehost/sweep", h.sweep)

	for {
		conn, err := h.listener.Accept()
		if err != nil {
			if h.stopped() {
				break
			}
			// A listener that broke while the host still wanted it is the end
			// of this host: there is no way back to accepting, and everything
			// attached is about to lose its socket anyway.
			h.note("stopped accepting: " + err.Error())
			break
		}
		h.mu.Lock()
		h.live++
		h.quiet = time.Time{}
		h.mu.Unlock()
		guard.Go("enginehost/connection", func() { h.attach(conn) })
	}
	h.shutdown()
	return nil
}

// attach serves one connection. The frames are internal/remote's from the first
// byte — this side contributes only the answer to "which conversation".
func (h *Host) attach(conn net.Conn) {
	defer func() {
		_ = conn.Close()
		h.mu.Lock()
		h.live--
		if h.live == 0 && len(h.sessions) == 0 {
			h.quiet = time.Now()
		}
		h.mu.Unlock()
	}()
	if err := remote.ServeAttach(conn, conn, remote.AttachOptions{Open: h.open}); err != nil {
		// A host has no terminal and no person to tell, so a broken connection
		// goes to a file an operator can read afterwards — the same destination
		// and the same reason the standing pass's failures have.
		h.note("connection: " + err.Error())
	}
}

// open answers which conversation a hello is attaching to: the one already
// running under that key, or a new one.
func (h *Host) open(hello remote.Hello) (*remote.Session, error) {
	key := strings.TrimSpace(hello.Session)
	if h.opts.Key != nil {
		key = h.opts.Key(hello)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, errors.New("engine host: this host is shutting down")
	}
	if existing := h.sessions[key]; existing != nil && !existing.Ended() {
		// THE WHOLE PRODUCT IS THIS LINE: the conversation was already running,
		// possibly mid-turn, and the surface is joining it rather than starting
		// anything.
		return existing, nil
	}
	engine, err := h.opts.Boot(hello)
	if err != nil {
		return nil, err
	}
	if engine == nil || engine.Agent == nil {
		return nil, errors.New("engine host: the workspace opened no conversation")
	}
	sess := remote.NewSession(engine, true)
	h.sessions[key] = sess
	h.quiet = time.Time{}
	return sess, nil
}

// sweep is the idle policy, run on a clock: retire the conversations nobody
// wants any more, and then retire the host when there is nothing left to hold.
func (h *Host) sweep() {
	ticker := time.NewTicker(sweepEvery)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
		}
		var retiring []*remote.Session
		h.mu.Lock()
		for key, sess := range h.sessions {
			idle := sess.IdleSince()
			if sess.Ended() || (!idle.IsZero() && time.Since(idle) > sessionIdle) {
				delete(h.sessions, key)
				retiring = append(retiring, sess)
			}
		}
		if h.live == 0 && len(h.sessions) == 0 && h.quiet.IsZero() {
			h.quiet = time.Now()
		}
		leaving := h.live == 0 && len(h.sessions) == 0 &&
			!h.quiet.IsZero() && time.Since(h.quiet) > hostIdle
		h.mu.Unlock()

		for _, sess := range retiring {
			// Closing flushes the journal, which is the only thing that has to
			// happen before a conversation is let go of.
			_ = sess.Close()
		}
		if leaving {
			h.stop()
			return
		}
	}
}

// stop asks the host to end. It is idempotent because the signal handler, the
// sweep and a caller may all reach it.
func (h *Host) stop() {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	h.mu.Unlock()
	close(h.done)
	// Closing the listener is what wakes the accept loop, which is where the
	// shutdown actually happens.
	_ = h.listener.Close()
}

func (h *Host) stopped() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.closed
}

// shutdown is the exit: every conversation is closed, which flushes every
// journal, and the socket and the lock go last.
func (h *Host) shutdown() {
	h.mu.Lock()
	h.closed = true
	sessions := make([]*remote.Session, 0, len(h.sessions))
	for key, sess := range h.sessions {
		sessions = append(sessions, sess)
		delete(h.sessions, key)
	}
	h.mu.Unlock()
	for _, sess := range sessions {
		_ = sess.Close()
	}
	socket := filepath.Join(h.dir, socketName)
	_ = h.listener.Close()
	_ = os.Remove(socket)
	_ = releaseLock(h.lock)
}

// note appends one line to the host's log. It is best-effort by construction:
// a host that could not write its log is still a host, and there is nobody to
// tell about the failure to tell somebody.
func (h *Host) note(line string) {
	file, err := os.OpenFile(filepath.Join(h.dir, logName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "%s %s\n", time.Now().Format(time.RFC3339), line)
}

// ── the lock ────────────────────────────────────────────────────────────────

// takeLock is the tree's own advisory lock, taken without blocking: a busy lock
// is an answer ("somebody else has this") and never a wait. internal/filelock
// is the one primitive for this in the tree and nothing here invents a second.
func takeLock(path string) (*os.File, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(file, true, true); err != nil {
		_ = file.Close()
		return nil, err
	}
	return file, nil
}

func releaseLock(file *os.File) error {
	if file == nil {
		return nil
	}
	err := filelock.Unlock(file)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	return err
}
