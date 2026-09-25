package enginehost

// host.go is the host itself: the process that holds the conversations.
//
// It is a small thing on purpose. It owns a socket, a lock, a map of open
// sessions and a clock; everything about what a conversation IS belongs to
// internal/remote and internal/session, and everything about how one is
// ASSEMBLED belongs to the door that calls [Run]. A host that knew how to build
// an agent would be the second assembly this tree has already refused twice
// (cmd/codeaf's shared assembly states why).

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

	"github.com/Agent-Field/codeaf/internal/filelock"
	"github.com/Agent-Field/codeaf/internal/guard"
	"github.com/Agent-Field/codeaf/internal/remote"
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
// there is nothing to be the host of, so it exits and the next `codeaf engine`
// starts a new one — which costs one process spawn and is invisible.
//
// THIS DOES NOT PUT THE AMBIENT SIDE TO SLEEP, and that is the one interaction
// worth stating. Standing items are not held by this process: a firing runs
// inside whichever process holds the store's tick lock — any live window, or
// the operating system's timer running `codeaf tick` with nobody sitting
// anywhere (cmd/codeaf's chatv3_standing.go states that law). A host that exits
// hands the tick back to that timer exactly as a closed terminal does. So the
// two lifetimes are deliberately NOT married: standing work already keeps a
// machine warm on its own terms, and a host that stayed up forever to guard it
// would be a second answer to a question that already has one.
// sessionIdle is a var rather than a const so a test can ask what the policy
// DECIDES without waiting half an hour to find out. Nothing in the product
// writes it.
var sessionIdle = 30 * time.Minute

const (
	hostIdle   = 2 * time.Minute
	sweepEvery = 30 * time.Second
)

// The two numbers a stand-down is measured in.
//
// A HOST THAT AGREED TO GO WRITES ITS ANSWER BEFORE IT GOES. The frame saying
// so has not left the process when the retirement is armed — the connection's
// own goroutine writes it on the way back out of the exchange — so the listener
// is closed only once every connection has hung up, and [standDownGrace] is the
// ceiling for one that never does. Two seconds because the only thing waiting
// on it is a local unix socket that is about to be dialled again.
const (
	standDownGrace = 2 * time.Second
	standDownPoll  = 20 * time.Millisecond
)

// Options is what a host needs from the door that starts it, which is the same
// two things every version of this has needed: how to open a conversation, and
// which conversation a hello is asking for.
type Options struct {
	// Boot opens one conversation for a hello — cmd/codeaf's bootEngine, the
	// same closure the pipe engine hands to [remote.Serve].
	//
	// It is called with the host's own lock held, so ONE CONVERSATION IS OPENED
	// AT A TIME. That is not for speed, it is for the session file: two
	// connections asking for the same conversation at the same moment must not
	// both open it, because the second would find the journal locked and mint a
	// second session under a person who asked for one.
	Boot func(remote.Hello) (*remote.Engine, error)

	// Key says which conversation a hello wants, so that two surfaces asking
	// for the same one are handed the same one.
	//
	// IT ANSWERS A TRANSCRIPT PATH, INCLUDING FOR A HELLO THAT NAMED NOTHING.
	// "The workspace's latest" is a question about this machine's disk, and the
	// door that starts the host is the half of this pair that can read it
	// (cmd/codeaf's [engineHelloKey]) — resolving it here, at the door, is what
	// lets the lookups below find a conversation this host is ALREADY holding
	// rather than booting a second agent onto its journal.
	//
	// A nil Key falls back to the hello's own session, which is what a test with
	// no door behind it means, and the empty string it answers for a hello that
	// named nothing is then the boot's problem rather than an identity.
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

	// binary is the file this host was started from, as it looked when it
	// started. A host whose own binary has been replaced retires the moment it
	// is holding nothing (binary.go states why).
	binary hostBinary
	// started is when this process began holding the workspace, for the one
	// person who asks `codeaf engine --status` how long it has been there.
	started time.Time

	mu       sync.Mutex
	sessions map[string]*remote.Session
	// latest is the transcript this host opened for the last hello that named
	// no conversation, and it is an ALIAS RATHER THAN AN IDENTITY: nothing is
	// ever filed under it, [Host.open] simply resolves the empty key through it
	// before it looks anything up. See the law at that lookup.
	latest string
	// minted counts the conversations opened by a hello that asked for one of
	// its own ([remote.Hello.New]) and could not be filed under their own
	// transcript. It is bookkeeping and never an identity: see [Host.freeKeyLocked].
	minted int
	live   int
	// probes is how many of those live connections turned out to be the version
	// exchange rather than a surface (whois.go). THE QUESTION MUST NOT COUNT AS
	// THE WORK: a connection asking "are you busy" is not what busy means, and
	// counting it would make every host answer yes to the one question the
	// answer matters for.
	probes int
	// retiring is a stand-down that has been agreed to and not yet finished. It
	// closes the door on new conversations, because a conversation opened into
	// a process that is leaving is one the person watches vanish.
	retiring bool
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
	// ~/.codeaf/v3/hosts and wondering which of these is which.
	_ = os.WriteFile(filepath.Join(dir, placeName), []byte(workspace+"\n"), 0o600)

	h := &Host{
		workspace: workspace,
		dir:       dir,
		opts:      opts,
		binary:    thisBinary(),
		started:   time.Now(),
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
	guard.Go("enginehost/doorstep", h.doorstep)

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
	// asked is this connection turning out to be the version exchange and not a
	// surface. It is written by the closure below and read after ServeAttach
	// has returned, both on THIS goroutine, which is the whole of why it needs
	// no lock of its own.
	asked := false
	defer func() {
		_ = conn.Close()
		h.mu.Lock()
		h.live--
		if asked {
			h.probes--
		}
		if h.live == 0 && len(h.sessions) == 0 {
			h.quiet = time.Now()
		}
		h.mu.Unlock()
	}()
	if err := remote.ServeAttach(conn, conn, remote.AttachOptions{
		Open: h.open,
		Host: func(ask remote.WhoIs) remote.HostSelf {
			asked = true
			return h.whois(ask)
		},
	}); err != nil {
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
	if h.closed || h.retiring {
		return nil, errors.New("engine host: this host is shutting down")
	}
	// A JOIN TAKES A CONVERSATION THAT IS ALREADY HERE AND NOTHING ELSE. It is
	// matched on the transcript rather than on the key, and it never reaches the
	// boot below — [remote.Hello.Join] says why both halves of that are the point.
	if hello.Join {
		return h.joinedLocked(hello.Session)
	}
	// AND A MINT TAKES NOTHING THAT IS ALREADY HERE. [remote.Hello.New] is a
	// window opening ANOTHER conversation beside the ones it has, so the lookup
	// below — which is what makes two surfaces saying nothing land in one
	// conversation — is exactly what it must not do. It boots, and it is keyed by
	// the transcript the engine chose rather than by the empty string the hello
	// carried, so a later window naming that file finds this conversation instead
	// of opening a second agent onto the same journal.
	if hello.New {
		return h.mintedLocked(hello)
	}
	// AND NO CONVERSATION IS EVER KEYED BY THE EMPTY STRING, which is the whole
	// of the collision this closes.
	//
	// "Nothing" is not a name: a hello that names no session is asking for this
	// workspace's LATEST, and the door resolves that to a transcript path before
	// it ever reaches here (cmd/codeaf's [engineHelloKey]). What is left for the
	// host is the one case a door cannot resolve — a workspace with no
	// conversation in it yet, and a host with no door at all — and there the
	// empty string is resolved through the transcript the last such hello landed
	// on rather than used as a slot to file under.
	//
	// It was a slot once, and it worked exactly as long as that slot held the
	// workspace's latest. The moment its conversation ended — moved to another
	// window, left behind by /new, closed — while this host went on holding a
	// different one, the next plain hello found nothing under "" and booted;
	// the boot resolved the workspace's latest, which is a journal THIS PROCESS
	// holds the flock on, and the host refused its own conversation with "this
	// conversation is open in another window".
	if key == "" {
		key = h.latest
	}
	if existing := h.sessions[key]; existing != nil && !existing.Ended() {
		// THE WHOLE PRODUCT IS THIS LINE: the conversation was already running,
		// possibly mid-turn, and the surface is joining it rather than starting
		// anything.
		return existing, nil
	}
	// AND A HELLO THAT NAMES A TRANSCRIPT THIS HOST IS ALREADY HOLDING IS THE
	// SAME LINE SAID ABOUT A DIFFERENT KEY. A conversation minted by the branch
	// above, or by an older hello that named no session, is keyed by something
	// this hello has no way to guess — so without this a person reopening that
	// chat by its own path would boot a second agent onto a journal the first one
	// holds the lock on, and be told their conversation was open in another
	// window by the process they were talking to.
	if open, found := h.openLocked(key); found {
		return open, nil
	}
	engine, err := h.opts.Boot(hello)
	if err != nil {
		return nil, err
	}
	if engine == nil || engine.Agent == nil {
		return nil, errors.New("engine host: the workspace opened no conversation")
	}
	sess := remote.NewSession(engine, true)
	// The conversation is filed under the JOURNAL THE BOOT ACTUALLY OPENED,
	// which is the one name every later hello can arrive at — by naming it, or
	// by resolving "this workspace's latest" to it.
	filed := h.freeKeyLocked(firstFilled(engine.SessionFile, key))
	h.sessions[filed] = sess
	if key == "" {
		// And this is what a hello that named nothing will mean next time. It
		// is the alias above, written where the answer is finally known — the
		// name this conversation was actually filed under, so that an engine
		// which answered no transcript at all is still found rather than booted
		// a second time.
		h.latest = filed
	}
	h.quiet = time.Time{}
	return sess, nil
}

// firstFilled is the first of its arguments with something in it, trimmed.
func firstFilled(words ...string) string {
	for _, word := range words {
		if trimmed := strings.TrimSpace(word); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// mintedLocked opens a conversation of this host's own choosing and files it
// under the transcript it opened.
//
// THE KEY IS THE ENGINE'S ANSWER AND NOT THE HELLO'S QUESTION, which is the one
// thing that makes this different from the ordinary road. A minting hello names
// no session on purpose — where a new conversation lands is a question about
// this machine's disk — so keying it by what the hello said would file every
// minted conversation under the empty string and have the second one replace the
// first.
//
// IT IS ALWAYS FILED, even when the transcript it wants is somehow taken and
// even when the engine answered no file at all. The map is not only the door a
// second window comes back through: it is what the sweep retires conversations
// out of and what [Host.idleLocked] counts, so a session left out of it would be
// a conversation this host holds, keeps alive and can never let go of.
func (h *Host) mintedLocked(hello remote.Hello) (*remote.Session, error) {
	engine, err := h.opts.Boot(hello)
	if err != nil {
		return nil, err
	}
	if engine == nil || engine.Agent == nil {
		return nil, errors.New("engine host: the workspace opened no conversation")
	}
	sess := remote.NewSession(engine, true)
	h.sessions[h.freeKeyLocked(engine.SessionFile)] = sess
	h.quiet = time.Time{}
	return sess, nil
}

// freeKeyLocked is where a minted conversation is filed: its own transcript when
// nothing live is under that name, and otherwise a key nothing can collide with.
// The fallback is not reachable by name and is not meant to be — it exists so
// that the bookkeeping above can never drop a conversation on the floor.
func (h *Host) freeKeyLocked(file string) string {
	key := strings.TrimSpace(file)
	if key != "" {
		if held := h.sessions[key]; held == nil || held.Ended() {
			return key
		}
	}
	h.minted++
	return fmt.Sprintf("\x00minted-%d", h.minted)
}

// openLocked is the live conversation writing one transcript, found by that
// transcript rather than by the key it was filed under. It is [Host.joinedLocked]
// without the refusal: a caller here has somewhere else to go when the answer is
// no, which is the boot.
func (h *Host) openLocked(file string) (*remote.Session, bool) {
	want := strings.TrimSpace(file)
	if want == "" {
		return nil, false
	}
	want = filepath.Clean(want)
	for _, sess := range h.sessions {
		if sess == nil || sess.Ended() {
			continue
		}
		if open := strings.TrimSpace(sess.File()); open != "" && filepath.Clean(open) == want {
			return sess, true
		}
	}
	return nil, false
}

// joinedLocked is the live conversation writing one transcript, and an error
// naming what was asked for when there is none.
//
// THE REFUSAL IS A SENTENCE AND NOT A BOOT. The caller is a surface that wants a
// second view onto work it believes is running; if that belief is stale — the
// window closed a second ago, the conversation ended — the honest answer is that
// it is not here, and the surface has a recovery state for exactly that. Starting
// a conversation would answer a question nobody asked with a model somebody pays
// for.
//
// The file is compared cleaned, because one side of this walked a directory and
// the other read a presence file, and neither promises the other's spelling.
func (h *Host) joinedLocked(file string) (*remote.Session, error) {
	want := strings.TrimSpace(file)
	if want == "" {
		return nil, errors.New("engine host: a join has to name a conversation")
	}
	if sess, found := h.openLocked(want); found {
		return sess, nil
	}
	return nil, fmt.Errorf("engine host: that conversation is not open here: %s", filepath.Clean(want))
}

// whois is the host answering what it is and what it will do about a request to
// go — the host's half of internal/remote's version exchange.
//
// THE HOST DECIDES, AND THAT IS THE WHOLE OF WHY THIS IS SAFE. The process
// asking cannot see a turn in flight, a surface in another window or a card
// waiting for an answer; this one can, and a host holding any of them says so
// and stays exactly where it is. Nothing on the asking side can end somebody
// else's work by accident, because nothing on the asking side does the ending.
func (h *Host) whois(ask remote.WhoIs) remote.HostSelf {
	h.mu.Lock()
	// This connection is the question and not the work — counted before
	// anything is measured, so that the measurement is right.
	h.probes++
	self := remote.HostSelf{Workspace: h.workspace, Busy: !h.idleLocked()}
	// AND WHAT THIS PROCESS IS, for `codeaf engine --status` and for the door
	// deciding which of two builds is the older one. The counts are read under
	// the same lock as busy, so the three never disagree with each other.
	self.PID = os.Getpid()
	self.Binary = h.binary.path
	self.Started = h.started
	self.BuiltAt = h.binary.builtAt()
	self.Surfaces = h.live - h.probes
	for _, sess := range h.sessions {
		if sess != nil && !sess.Ended() {
			self.Conversations++
		}
	}
	going := ask.StandDown && (ask.Anyway || !self.Busy)
	switch {
	case h.closed || h.retiring:
		// Already leaving, which is the answer the asker wanted either way.
		self.Retiring = true
		going = false
	case going:
		h.retiring, self.Retiring = true, true
	}
	h.mu.Unlock()
	if going {
		h.standDown()
	}
	return self
}

// idleLocked is this host holding nothing: no connection attached that is not
// the question itself, and no conversation with a surface, a running turn or a
// question waiting to be answered ([remote.Session.IdleSince] draws that last
// line and is the same reading the sweep uses).
//
// IT IS NOT THE IDLE POLICY. The clocks above are about a host that has been
// quiet for LONG ENOUGH to be worth retiring; this is about a host that could
// be retired RIGHT NOW without taking anything down with it, which is a
// different question with a different answer.
func (h *Host) idleLocked() bool {
	if h.live-h.probes > 0 {
		return false
	}
	for _, sess := range h.sessions {
		if sess.IdleSince().IsZero() {
			return false
		}
	}
	return true
}

// standDown ends the host once the connections it is talking to have gone.
//
// The wait is what makes the answer arrive: the frame agreeing to retire is
// written by the asking connection's own goroutine after this returns, so a
// listener closed here would close it under the sentence it was carrying.
func (h *Host) standDown() {
	guard.Go("enginehost/standdown", func() {
		deadline := time.Now().Add(standDownGrace)
		for {
			h.mu.Lock()
			empty := h.live == 0
			h.mu.Unlock()
			if empty || time.Now().After(deadline) {
				break
			}
			time.Sleep(standDownPoll)
		}
		h.stop()
	})
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
		if h.sweepOnce() {
			h.stop()
			return
		}
	}
}

// sweepOnce is one pass of the policy, and it answers whether this was the host's
// last one. It is a function rather than the body of the loop above so that a
// test can ask what the policy decides without waiting out a clock.
func (h *Host) sweepOnce() bool {
	// THE DECISION IS MADE OFF THIS HOST'S LOCK. Retiring a conversation flushes
	// its journal and may walk its graph, and doing that with the sessions map
	// held would stall every connection arriving meanwhile.
	type held struct {
		key  string
		sess *remote.Session
	}
	h.mu.Lock()
	holding := make([]held, 0, len(h.sessions))
	for key, sess := range h.sessions {
		holding = append(holding, held{key: key, sess: sess})
	}
	h.mu.Unlock()

	var gone []held
	for _, one := range holding {
		// RetireIfIdle takes its last look and ends the conversation under one
		// acquisition of the session's own lock, so a surface that arrived while
		// this pass was thinking cancels the retirement rather than losing the
		// conversation it just joined.
		if one.sess.Ended() || one.sess.RetireIfIdle(sessionIdle) {
			gone = append(gone, one)
		}
	}

	h.mu.Lock()
	for _, one := range gone {
		// Only if it is still the SAME conversation under that key: a hello may
		// have opened a fresh one there while this pass was retiring the old.
		if h.sessions[one.key] == one.sess {
			delete(h.sessions, one.key)
		}
	}
	if h.live == 0 && len(h.sessions) == 0 && h.quiet.IsZero() {
		h.quiet = time.Now()
	}
	leaving := h.live == 0 && len(h.sessions) == 0 &&
		!h.quiet.IsZero() && time.Since(h.quiet) > hostIdle
	replaced := !leaving && h.binary.replaced() && h.idleLocked()
	if replaced {
		// The door closes under the same lock that decided, so a surface
		// arriving in the moment between here and the stop is refused rather
		// than handed a conversation that is about to end.
		leaving, h.retiring = true, true
	}
	h.mu.Unlock()

	if replaced {
		h.note("the file this host was started from has been replaced; retiring so the next connection starts the current one")
	}
	return leaving
}

// takeoverDoorstep is how often the host looks for a conversation another
// window has asked for. The agent inside already looks at its own doorstep four
// times a second (internal/session's takeover.go); this is one lock and a walk
// of a small map, at half that rate, so a window waiting on the journal's flock
// sees it free within about a second of asking.
const takeoverDoorstep = 500 * time.Millisecond

// doorstep is the host honouring a move-it-here request for a conversation it
// holds (internal/remote's takeover.go says why the host has to be the one that
// looks): the conversation asked for is closed, which releases its journal to
// the window that asked.
func (h *Host) doorstep() {
	ticker := time.NewTicker(takeoverDoorstep)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
		}
		h.releaseAsked()
	}
}

// releaseAsked is one look. THE CLOSE HAPPENS OFF THE HOST'S LOCK, for the
// sweep's reason: closing a conversation flushes its journal, and every
// connection arriving meanwhile would stall behind it.
func (h *Host) releaseAsked() {
	type asked struct {
		key  string
		sess *remote.Session
	}
	h.mu.Lock()
	holding := make([]asked, 0, len(h.sessions))
	for key, sess := range h.sessions {
		if sess != nil {
			holding = append(holding, asked{key: key, sess: sess})
		}
	}
	h.mu.Unlock()
	// The agents are asked off the host's lock too: each answer takes the
	// agent's own lock, and nothing here may queue the host behind a turn.
	var found []asked
	for _, one := range holding {
		if one.sess.TakeoverAsked() {
			found = append(found, one)
		}
	}
	for _, one := range found {
		_ = one.sess.ReleaseForTakeover()
		h.note("let go of " + one.sess.File() + ": another window on this machine asked for it")
	}
	if len(found) == 0 {
		return
	}
	h.mu.Lock()
	for _, one := range found {
		if h.sessions[one.key] == one.sess {
			delete(h.sessions, one.key)
		}
	}
	if h.live == 0 && len(h.sessions) == 0 && h.quiet.IsZero() {
		h.quiet = time.Now()
	}
	h.mu.Unlock()
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
