package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"sync"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/home"
	"github.com/Agent-Field/codeaf/internal/livedoor"
	"github.com/Agent-Field/codeaf/internal/remote"
)

// ── THE DESKTOP AS A HOLDER OF CHATS ─────────────────────────────────────────
//
// Another computer opens a chat THIS desktop is running by dialling the holder's
// own live door (livedoor.Listen: a socket beside a registry file under
// <home>/v3/live, named by this process's id), which hands it the very
// conversation the desktop's own window is looking at. For that the
// conversation has to be something a second surface can join, which a
// conversation opened through [remote.Loopback] is not: Loopback's session
// belongs to its one pipe, and a torn pipe ends it.
//
// SO A CHAT THE DESKTOP BUILDS WHILE REMOTE CONTROL IS ON GETS A PERSISTENT
// SESSION INSTEAD ([appDeskHost.open]), and nothing else about it changes. The
// desktop's own view is still one in-process attach over an in-memory pipe, the
// connection is still marked Assembled, and every road that ends the chat still
// ends in [appConn.Release]: the view lets go, the session is closed behind it,
// and the chat leaves the door. The persistent flag matters only to somebody
// ELSE arriving: a viewer's torn pipe keeps the conversation, because the
// desktop's window is still in the room.
//
// WITH REMOTE CONTROL OFF (the default) NONE OF THIS RUNS. No listener, no
// registry file, no socket, no persistent session: [appDeskHost.ready] answers
// false and the open is the one it was before this file existed.
//
// IT IS PROCESS-WIDE BECAUSE THE DOOR IS. One process has one pid, one socket
// and one registry file, however many backends a test makes, and the chat opener
// ([appOpenInProcess]) is a plain function with no backend to hang this on.

// appDeskHost is the desktop's holder door and the chats behind it.
type appDeskHost struct {
	// listen starts the door. It is a field so a test can stand in a different
	// one, never to change what the door does.
	listen func(homeDir string, open func(transcript string) (*remote.Session, bool), tr livedoor.Transport) (*livedoor.Listener, error)

	mu       sync.Mutex
	listener *livedoor.Listener
	// failed says starting the door failed once in this process (a state folder
	// whose path is too long for a socket, a read-only home). It is remembered,
	// so a desktop that cannot be a holder says so once and goes on opening its
	// chats the way it did, instead of retrying on every chat.
	failed   bool
	sessions map[string]*remote.Session
	// viewing is the connections other computers hold to each chat, by the chat's
	// key, so letting a chat go can end them ([appDeskHost.endViewers]).
	viewing map[string]map[*deskConn]struct{}
}

// locked runs f under the door's lock, which is released from a defer even if f
// panics (the guarded tree's law: a panic absorbed upstream must not become a
// deadlock here).
func (h *appDeskHost) locked(f func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	f()
}

// deskHost is this process's holder door.
var deskHost = newAppDeskHost()

func newAppDeskHost() *appDeskHost {
	return &appDeskHost{
		listen:   livedoor.Listen,
		sessions: map[string]*remote.Session{},
		viewing:  map[string]map[*deskConn]struct{}{},
	}
}

// appDeskKey is how a chat's transcript is looked up: the path the door names
// is the holder's own spelling of it, and the engine's may differ by a symlink
// (the macOS temporary folder is one), so both sides are spelled physically.
func appDeskKey(transcript string) string {
	if transcript == "" {
		return ""
	}
	path := filepath.Clean(transcript)
	if physical, err := filepath.EvalSymlinks(path); err == nil {
		return physical
	}
	if dir, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		return filepath.Join(dir, filepath.Base(path))
	}
	return path
}

// ready says the door is up for a chat about to be opened, starting it the
// first time. It is false with remote control off, and false when the door could
// not be started: those chats stay ordinary desktop chats.
func (h *appDeskHost) ready() bool {
	if !config.RemoteControlOn() {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.listener != nil {
		return true
	}
	if h.failed {
		return false
	}
	l, err := h.listen(home.Dir(), h.lookup, deskTransport{inner: livedoor.UnixTransport(), host: h})
	if err != nil {
		h.failed = true
		return false
	}
	h.listener = l
	return true
}

// lookup is the door's question: is this chat open here. A chat that has ended
// is not, and an answer "no" is the refusal the viewer reads as not-running.
func (h *appDeskHost) lookup(transcript string) (*remote.Session, bool) {
	// THE SETTING IS READ AGAIN HERE, so switching remote control off stops
	// handing chats out at once, without waiting for the chats to close.
	if !config.RemoteControlOn() {
		return nil, false
	}
	key := appDeskKey(transcript)
	var sess *remote.Session
	h.locked(func() { sess = h.sessions[key] })
	// THE SESSION IS ASKED OUTSIDE THIS LOCK. Its own lock can be held by a
	// path that closes a viewer's connection, and closing one takes this lock
	// ([deskConn.Close]); asking while holding it would be the other order.
	if sess == nil || sess.Ended() {
		return nil, false
	}
	return sess, true
}

// viewers is how many OTHER surfaces are watching the chat on this transcript:
// everybody attached except the desktop's own view. It is zero for a chat that
// is not held through the door.
func (h *appDeskHost) viewers(transcript string) int {
	if h == nil || transcript == "" {
		return 0
	}
	key := appDeskKey(transcript)
	var sess *remote.Session
	h.locked(func() { sess = h.sessions[key] })
	if sess == nil || sess.Ended() {
		return 0
	}
	return max(sess.Attached()-1, 0)
}

// closeListener takes the door down once the app has let every chat go. A chat
// still registered (a second backend in a test, nothing in production) keeps it.
func (h *appDeskHost) closeListener() {
	var l *livedoor.Listener
	h.locked(func() {
		l = h.listener
		if len(h.sessions) > 0 {
			l = nil
		} else {
			h.listener = nil
		}
	})
	if l != nil {
		_ = l.Close()
	}
}

// appHeld is one chat the door can reach: the persistent session behind it and
// the key it is filed under.
type appHeld struct {
	host *appDeskHost
	sess *remote.Session
	key  string
}

// drop takes the chat off the door, so nobody new can join it while it closes.
func (h *appHeld) drop() {
	if h == nil {
		return
	}
	h.host.locked(func() {
		if h.host.sessions[h.key] == h.sess {
			delete(h.host.sessions, h.key)
		}
	})
}

// endViewers tells everybody else watching the chat that it is over, by closing
// their connections.
func (h *appHeld) endViewers() {
	if h == nil {
		return
	}
	h.host.endViewers(h.key)
}

// close ends the conversation behind the door. The desktop's view has already
// let go, so a chat nobody else is watching closes through the same door it
// always did (a retirement), and one a viewer is still watching tells that
// viewer the engine stopped.
//
// THE RESULT IS NOT RECORDED, which is what the in-process open has always done
// with it: the final save's failure is carried by the conversation's own close
// notices, and [recordFinalSaveFailure] of the pipe's close (the only thing the
// loopback open records) is nil.
func (h *appHeld) close() {
	if h == nil {
		return
	}
	_ = h.sess.Close()
}

// open is [remote.Loopback] for a chat the door can reach. boot is the engine's
// own opener, called exactly once with the hello the engine receives from any
// loopback.
//
// IT RETURNS A NIL appHeld WHEN THE DOOR IS NOT UP, AND THE LOOPBACK IT RETURNS
// THEN IS THE ONE THE DESKTOP ALWAYS OPENED. The caller releases both the same
// way either time.
func (h *appDeskHost) open(hello remote.Hello, boot func(remote.Hello) (*remote.Engine, error)) (*remote.Loop, *appHeld, error) {
	if !h.ready() {
		loop, err := remote.Loopback(hello, remote.Options{Boot: boot})
		return loop, nil, err
	}
	var sess *remote.Session
	var key string
	loop, err := remote.LoopbackAttach(hello, remote.AttachOptions{
		Open: func(asked remote.Hello) (*remote.Session, error) {
			// The same refusal [remote.Serve] gives: this door opens its own
			// conversation, and a join names one that is already open.
			if asked.Join {
				return nil, errors.New("this engine opens one conversation for one connection, so there is none already open here to join")
			}
			engine, err := boot(asked)
			if err != nil {
				return nil, err
			}
			if engine == nil {
				return nil, nil
			}
			key = appDeskKey(engine.SessionFile)
			sess = remote.NewSession(engine, true)
			return sess, nil
		},
	})
	if err != nil {
		// A PERSISTENT SESSION SURVIVES A FAILED ATTACH, which Loopback's does
		// not: with nothing to close it the agent would stay open on a
		// conversation nobody holds.
		if sess != nil {
			_ = sess.Close()
		}
		return nil, nil, err
	}
	held := &appHeld{host: h, sess: sess, key: key}
	if key != "" {
		h.locked(func() { h.sessions[key] = sess })
	}
	return loop, held, nil
}

// deskViewed says another computer is watching the agent's chat through this
// desktop's door. It reads the transcript under the backend's lock and asks the
// door outside it, so the two locks are never held together.
func (b *appBackend) deskViewed(a *appAgent) bool {
	var transcript string
	b.locked(func() { transcript = a.transcript })
	return deskHost.viewers(transcript) > 0
}

// ── ENDING A CHAT'S VIEWERS ──────────────────────────────────────────────────
//
// A remote.Session that is closed tells its attached surfaces nothing and leaves
// their pipes open: the one door that ever closed a conversation with a window
// still in it (a goodbye) ends only the window that said it. A viewer of a chat
// the desktop lets go has to be told, and the way the live door's router tells a
// person is the holder's socket closing: it reads the end of the local
// connection as "the holder is gone", ends the channel with that reason, and the
// viewer's roaming client redials and finds the chat not running.
//
// SO THE DOOR KEEPS THE CONNECTIONS IT HAS HANDED TO EACH CHAT. The listener
// accepts them without knowing which chat each is for until its hello has been
// read, so the transport it is given wraps every accepted connection and reads
// the hello as it goes past (it is the first frame the viewer sends, it is
// passed on unchanged, and nothing else is looked at).

// deskTransport is the unix transport with every accepted connection watched.
type deskTransport struct {
	inner livedoor.Transport
	host  *appDeskHost
}

func (t deskTransport) Listen(addr string) (net.Listener, error) {
	ln, err := t.inner.Listen(addr)
	if err != nil {
		return nil, err
	}
	return deskListener{Listener: ln, host: t.host}, nil
}

func (t deskTransport) Dial(ctx context.Context, addr string) (net.Conn, error) {
	return t.inner.Dial(ctx, addr)
}

type deskListener struct {
	net.Listener
	host *appDeskHost
}

func (l deskListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &deskConn{Conn: conn, host: l.host}, nil
}

// deskConn is one viewer's connection to a chat of this desktop. Read is called
// by one goroutine, the server's reader, so the hello scan needs no lock.
type deskConn struct {
	net.Conn
	host    *appDeskHost
	head    []byte
	scanned bool
	// key is the chat this connection was opened for, set once its hello has
	// been read. It is written and read under the door's lock, because Close
	// can come from another goroutine than the reader.
	key string
	// ended says Close has run, so a hello read after it files nothing.
	ended bool
}

// appDeskHelloMost is how much of a connection's start is looked at for its
// hello. A first frame longer than this is not a hello the live door wrote
// (livedoor.MaxLine is the cap it holds a line to), and the connection is
// simply not tracked.
const appDeskHelloMost = 64 << 10

func (c *deskConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if !c.scanned && n > 0 {
		c.head = append(c.head, p[:n]...)
		for !c.scanned {
			end := bytes.IndexByte(c.head, '\n')
			if end < 0 {
				if len(c.head) > appDeskHelloMost {
					c.scanned = true
				}
				break
			}
			line := c.head[:end]
			c.head = c.head[end+1:]
			var frame remote.Frame
			if json.Unmarshal(line, &frame) == nil && frame.Kind == "hello" {
				c.scanned = true
				var hello remote.Hello
				if json.Unmarshal(frame.Payload, &hello) == nil && hello.Session != "" {
					c.host.watch(c, appDeskKey(hello.Session))
				}
			}
		}
		if c.scanned {
			c.head = nil
		}
	}
	return n, err
}

// Close takes the connection off the door's list as it closes.
func (c *deskConn) Close() error {
	c.host.unwatch(c)
	return c.Conn.Close()
}

func (h *appDeskHost) watch(c *deskConn, key string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c.ended {
		return
	}
	c.key = key
	if h.viewing[key] == nil {
		h.viewing[key] = map[*deskConn]struct{}{}
	}
	h.viewing[key][c] = struct{}{}
}

func (h *appDeskHost) unwatch(c *deskConn) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c.ended = true
	if set := h.viewing[c.key]; set != nil {
		delete(set, c)
		if len(set) == 0 {
			delete(h.viewing, c.key)
		}
	}
}

// endViewers closes every connection another computer holds to the chat on key.
func (h *appDeskHost) endViewers(key string) {
	var conns []*deskConn
	h.locked(func() {
		for c := range h.viewing[key] {
			conns = append(conns, c)
		}
	})
	for _, c := range conns {
		_ = c.Close()
	}
}
