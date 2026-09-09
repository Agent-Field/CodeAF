// Package enginehost is the process that keeps a conversation alive between
// connections.
//
// `aforge chat --host devbox` runs `ssh devbox aforge engine` and speaks
// internal/remote's protocol over the pipes. In version 1 that process WAS the
// conversation: it opened the session, answered frames, and died with the pipe,
// so a closed laptop lid and a dropped wifi both ended a running turn. This
// package is the other half of version 2's answer — a host that outlives the
// pipe, holding the engine, while `aforge engine` becomes a splice between the
// ssh pipes and a unix socket.
//
// ── THE GRAIN IS ONE HOST PER WORKSPACE, AND THE CHDIR LAW DECIDES IT ────────
//
// A host holds one engine per open session, but every session it holds is about
// the SAME directory, and that is not an arbitrary carving. `aforge engine`
// moves the process into the workspace before it assembles anything (cmd/aforge
// engine.go), and that chdir is the only chdir in the tree — the process's own
// idea of where it is has to agree with the session config's. A host holding
// two workspaces would have to break that or lie about it. So the socket lives
// under a directory named for the workspace, a host is born in it, and a hello
// asking for a different one reaches a different host through a different
// socket. Nothing about the protocol changes; the routing happens before the
// first frame.
//
// ── NOTHING HERE IS REQUIRED ─────────────────────────────────────────────────
//
// THE FALLBACK IS NOT OPTIONAL. A machine where the socket directory cannot be
// made, where the path is too long for a unix socket, where the lock cannot be
// taken, or where the host simply refuses to start must still take a remote
// session — as the version-1 engine did, on the pipe, with the welcome saying
// [remote.Welcome.Persistent] is false. Every door in this package is written
// to be allowed to fail: the caller reads the error as "not today" and serves
// the connection itself.
package enginehost

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/remote"
)

// socketName, lockName and logName are the three files a host keeps beside each
// other, and the whole of what it writes.
const (
	socketName = "host.sock"
	lockName   = "host.lock"
	logName    = "host.log"
	// placeName records the workspace this directory is the host of, in plain
	// text, because the directory itself is named by a hash and a person
	// looking at ~/.aforge/v3/hosts deserves to be able to tell which is which.
	placeName = "workspace"
)

// SocketLimit is the most bytes a unix socket path may weigh.
//
// It is 104 rather than Linux's own 108 because THE SMALLEST LIMIT IS THE ONE
// THAT TRAVELS: macOS stops at 104, the same aforge home can be shared over a
// network mount, and a host that worked on one machine and refused on another
// for a reason nobody could see would be worse than one honest refusal
// everywhere. Exceeding it is not a fault — AFORGE_HOME can be anywhere — so it
// is answered as "no host today" and the caller falls back to the pipe.
const SocketLimit = 104

// ErrSocketPathTooLong is a state root deeper than a unix socket may be named
// in, and it is the one failure on this road that is settled BEFORE anything
// is started: no lock, no process, no wait. The caller can therefore name this
// refusal separately from a host that had somewhere to listen but did not come
// up.
var ErrSocketPathTooLong = errors.New("engine host: the state path is too long for a socket")

// ErrNoHostAnswered is a host that had somewhere to listen but did not answer
// before the birth wait ran out. It stays distinct from [ErrSocketPathTooLong]
// so the caller can tell a failed start from one that was never possible.
var ErrNoHostAnswered = errors.New("engine host: no host answered")

// SocketPathFits exposes the shared Unix-socket ceiling to other doors that
// place a socket under the aforge state root. Keeping the number here prevents
// ssh control sockets and engine-host sockets from drifting across platforms.
func SocketPathFits(path string) bool { return len(path) <= SocketLimit }

// Dir is where the host for one workspace keeps its socket: a directory under
// ~/.aforge/v3/hosts, resolved through internal/home so AFORGE_HOME moves it
// with everything else.
//
// THE DIRECTORY IS NAMED BY A HASH AND NOT BY THE PATH, which is the one place
// this parts from the session layout's own encoding (chatv3_layout.go turns
// separators into dashes and keeps the name readable). A socket path has a hard
// ceiling of about a hundred bytes, and a workspace six directories deep would
// spend all of it — so the name is short by construction and the readable
// answer is written INSIDE the directory instead ([placeName]).
func Dir(workspace string) (string, error) {
	dir, err := where(workspace)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("engine host: %w", err)
	}
	return dir, nil
}

// where is the same answer WITHOUT making the directory, and the split is not a
// tidy-up: ASKING WHETHER SOMEBODY IS THERE MUST NOT BUILD THEM A HOUSE. Every
// plain `aforge chat` now puts one question to [Dial] before it opens anything
// (cmd/aforge's v3HostRoad), and a Dial that created a directory would leave one
// behind under every workspace anybody ever ran aforge in — litter proving only
// that a question was asked. The doors that are about to WRITE something — the
// host's own listener, the spawn lock, the wait for a host to go — call [Dir]
// and make it themselves.
func where(workspace string) (string, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return "", errors.New("engine host: no workspace to hold")
	}
	sum := sha256.Sum256([]byte(filepath.Clean(workspace)))
	return home.Join("v3", "hosts", hex.EncodeToString(sum[:8])), nil
}

// SocketPath is the socket one workspace's host listens on, refused up front
// when the path is longer than a unix socket may be. It makes nothing — see
// [where] — so a caller that is going to listen on it takes [Dir] as well.
func SocketPath(workspace string) (string, error) {
	dir, err := where(workspace)
	if err != nil {
		return "", err
	}
	socket := filepath.Join(dir, socketName)
	if !SocketPathFits(socket) {
		return "", fmt.Errorf("%s is too long a path for a socket: %w", socket, ErrSocketPathTooLong)
	}
	return socket, nil
}

// Dial connects to a host that is already running, and fails when none is. It
// never starts one — see [Attach] for that.
func Dial(workspace string) (net.Conn, error) {
	socket, err := SocketPath(workspace)
	if err != nil {
		return nil, err
	}
	return net.DialTimeout("unix", socket, dialTimeout)
}

// dialTimeout is how long one connect attempt may take. A unix socket connects
// instantly or not at all; this exists so a socket file whose host is wedged
// cannot hang the surface's whole launch.
const dialTimeout = 2 * time.Second

// spawnWait is how long [Attach] waits for a host it just asked for. Booting
// one means loading the profile, the catalog and a session, so it is seconds
// rather than milliseconds — and when the wait runs out nothing is broken, the
// caller simply serves the connection itself.
const spawnWait = 10 * time.Second

// Attach is the door `aforge engine` knocks on: a connection to this
// workspace's host, starting one if nothing answers.
//
// THE SPAWN RACE IS GUARDED BY THE LOCK THE HOST ITSELF HOLDS, which is the
// discipline this tree already uses for anything one machine may only do once
// (internal/filelock, and the standing tick's own lock). Whoever takes the lock
// spawns; whoever finds it busy knows a host is alive or being born and simply
// waits for the socket. Two spawns that race are not a fault either — the
// second host to start finds the lock held and exits without a word — so the
// worst case here is one wasted process, never two hosts on one workspace.
//
// Every failure answers the same way: no connection and a reason, which the
// caller reads as "serve this one on the pipe".
func Attach(workspace string, spawn func() error) (net.Conn, error) {
	// A PATH A SOCKET CAN NEVER BE NAMED IN IS ANSWERED BEFORE ANYTHING IS
	// STARTED. Every other failure here is worth a spawn and a wait, because a
	// host that is not there yet may be there in a second; this one cannot be —
	// the host would be born unable to listen and die into its own log while
	// this launch paid the whole [spawnWait] for a question already answered.
	if _, err := SocketPath(workspace); err != nil {
		return nil, err
	}
	if conn, err := Dial(workspace); err == nil {
		return conn, nil
	}
	if spawn == nil {
		return nil, errors.New("engine host: nothing to start a host with")
	}
	dir, err := Dir(workspace)
	if err != nil {
		return nil, err
	}
	// A lock that is BUSY is an answer and not a failure: somebody else is
	// already the host or is starting one, and waiting for their socket is all
	// this connection has left to do.
	if held, err := takeLock(filepath.Join(dir, lockName)); err == nil {
		// The lock is released BEFORE the spawn rather than after it, because
		// the host we are about to start wants this very lock for its own life.
		// The window that opens is the one described above, and it costs at
		// most a second process that finds the lock taken and exits.
		_ = releaseLock(held)
		if err := spawn(); err != nil {
			return nil, err
		}
	}
	return waitForHost(workspace, spawnWait)
}

func waitForHost(workspace string, within time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(within)
	for {
		if conn, err := Dial(workspace); err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, ErrNoHostAnswered
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// ── asking the host which build it is ───────────────────────────────────────

// askTimeout is how long the version exchange may take. It is a local unix
// socket answering one question with no work behind it, so this is a ceiling on
// a WEDGED host rather than a wait anybody should ever notice: a surface's
// launch must not hang on a process that stopped reading.
const askTimeout = 3 * time.Second

// goneWait is how long a host that agreed to retire is given to finish. It has
// journals to flush, which is the only thing it owes anybody on the way out.
const goneWait = 10 * time.Second

// ErrHostBusy is a host that will not retire because it is holding work: a
// surface attached, a turn running, or a question waiting for an answer. It is
// not a fault and the caller must not treat it as one — the machine is doing
// exactly what somebody asked it to.
var ErrHostBusy = errors.New("engine host: this host is holding work in flight")

// Ask puts internal/remote's version exchange to this workspace's host on a
// connection of its own, and fails when no host answers.
//
// THE CONNECTION IS SPENT ON THE QUESTION AND NEVER BECOMES A SURFACE. A hello
// would open a conversation — the expensive, journal-locking act this whole
// exchange exists to keep from happening against the wrong build — so the
// question travels alone and the caller dials again for the real thing.
func Ask(workspace string, ask remote.WhoIs) (remote.HostSelf, error) {
	conn, err := Dial(workspace)
	if err != nil {
		return remote.HostSelf{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(askTimeout))
	return remote.AskHost(conn, ask)
}

// Retire asks this workspace's host to go, and does not return until it has.
//
// IT IS THE HOST THAT SAYS WHETHER IT MAY. A host holding a turn, a surface or
// an unanswered question answers [ErrHostBusy] and stays where it is; anyway
// asks for it regardless, which is one person's own `aforge engine --stop` and
// nothing else. Either way the ending is the host's own shutdown — every
// conversation closed, every journal flushed — and never a signal from outside.
func Retire(workspace string, anyway bool) error {
	self, err := Ask(workspace, remote.WhoIs{StandDown: true, Anyway: anyway})
	if err != nil {
		return err
	}
	if !self.Retiring {
		if self.Busy {
			return ErrHostBusy
		}
		return errors.New("engine host: the host did not agree to go")
	}
	return waitForGone(workspace, goneWait)
}

// waitForGone waits until nothing is holding this workspace: the socket answers
// nobody AND the lock can be taken. BOTH HALVES ARE THE QUESTION — a host on
// its way down has already dropped its listener and still holds the lock for a
// moment, and a caller that spawned into that moment would watch its new host
// find the lock busy and exit without a word.
func waitForGone(workspace string, within time.Duration) error {
	dir, err := Dir(workspace)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(within)
	for {
		conn, err := Dial(workspace)
		if err == nil {
			_ = conn.Close()
		} else if held, err := takeLock(filepath.Join(dir, lockName)); err == nil {
			_ = releaseLock(held)
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("engine host: the host is still holding this workspace")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Stop is `aforge engine --stop`: whatever is holding this workspace on this
// machine, stopped, whichever build it is.
//
// IT HAS TO WORK ON A BUILD THAT PREDATES THE EXCHANGE, because that is the
// only build a person ever needs it for — the stale half in the middle, still
// answering a socket after the binary under it was replaced. So a host that
// cannot be asked anything is ended the one way the operating system offers:
// the kernel names the process on the other end of the socket ([peerPID]) and
// it is sent the signal every build of the host has always answered by flushing
// its journals and exiting.
//
// It answers false when nothing was holding this workspace, which is not a
// failure and is the ordinary case.
func Stop(workspace string) (bool, error) {
	conn, err := Dial(workspace)
	if err != nil {
		return false, nil
	}
	// The kernel is asked BEFORE the question is, because the answer to the
	// question may be that this process cannot answer questions.
	pid, pidErr := peerPID(conn)
	_ = conn.SetDeadline(time.Now().Add(askTimeout))
	self, askErr := remote.AskHost(conn, remote.WhoIs{StandDown: true, Anyway: true})
	_ = conn.Close()

	if askErr == nil && self.Retiring {
		return true, waitForGone(workspace, goneWait)
	}
	if pidErr != nil {
		if askErr != nil {
			return false, fmt.Errorf("engine host: %w", askErr)
		}
		return false, errors.New("engine host: the host did not agree to go")
	}
	if err := signalHost(pid); err != nil {
		return false, fmt.Errorf("engine host: %w", err)
	}
	return true, waitForGone(workspace, goneWait)
}

// Spawn starts a host process and lets go of it.
//
// IT IS DETACHED ON PURPOSE AND THAT IS THE POINT OF THE WHOLE LANE. The
// process asking for it is `aforge engine` under sshd, and when the connection
// drops sshd takes down everything in that session's process group — which is
// exactly the death this package exists to survive. So the child gets a session
// of its own ([detach]) and none of the parent's input or output: its stdout is
// the one thing that must never carry a stray byte, because a host's stdout is
// nothing at all and its parent's is the protocol.
//
// STDERR IS THE ONE PIPE THAT LEAVES LAST WORDS BEHIND. A host that dies at
// birth is the one process that knew the reason, and sending that reason to
// /dev/null made the failure unknowable; it appends to the same host log that a
// running [Host] writes, with /dev/null as the best-effort fallback when that
// log cannot be opened.
func Spawn(workspace, name string, args ...string) error {
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("engine host: %w", err)
	}
	defer null.Close()
	stderr := io.Writer(null)
	if dir, err := where(workspace); err == nil {
		if log, err := os.OpenFile(filepath.Join(dir, logName), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			defer log.Close()
			stderr = log
		}
	}
	command := exec.Command(name, args...)
	command.Stdin, command.Stdout, command.Stderr = null, null, stderr
	detach(command)
	if err := command.Start(); err != nil {
		return fmt.Errorf("engine host: %w", err)
	}
	// The child is nobody's to wait for. Releasing it here is what keeps this
	// process from holding a zombie for a host that is meant to outlive it.
	return command.Process.Release()
}

// Splice is `aforge engine` once it has a host: everything the surface says
// goes to the socket, everything the host says goes back, and nothing in
// between is read.
//
// THE PROXY UNDERSTANDS NO FRAMES, and that is deliberate. The handshake, the
// resume cursors, the held questions and the stream sequence numbers are all
// between the surface and the engine; a splice that parsed them would be a
// third opinion about a protocol with two ends. It copies bytes.
//
// EITHER SIDE ENDING ENDS BOTH. Stdin closing is the ssh connection going away,
// and the host must see that as the torn pipe it is — so the socket is closed
// rather than left half-open, and the host reads it as a surface that will be
// back (internal/remote's server.leave states what it does with that).
func Splice(in io.Reader, out io.Writer, conn net.Conn) error {
	done := make(chan error, 2)
	go func() {
		_, err := io.Copy(out, conn)
		done <- err
	}()
	go func() {
		_, err := io.Copy(conn, in)
		done <- err
	}()
	err := <-done
	_ = conn.Close()
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}
