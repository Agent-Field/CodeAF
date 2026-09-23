package main

// ── SEVERAL CONVERSATIONS, THROUGH ONE ENGINE ───────────────────────────────
//
// chatv3_beside.go is what makes `codeaf`, `codeaf chat`, `--host` and `--at`
// able to hold more than one live conversation at a time. Until this file
// existed they could not, and the surface said so in as many words —
// `closed · <name> — a connection holds one conversation at a time`.
//
// THE SENTENCE WAS TRUE AND THE ARRANGEMENT BEHIND IT WAS THE DEFECT. A
// [remote.Agent] holds no state: it is a handle on whichever conversation the
// engine currently has open on ONE connection, and the two seams this door used
// to wire — [tui3.Options.Fresh] and [tui3.Options.Resume] — both asked the
// engine to SWAP that connection onto another session, which interrupts and
// closes the one it replaced (internal/remote's Session.swap). So opening a
// second chat really did end the first one, and [tui3.Options.SharedAgent] was
// the surface correctly declining to pretend otherwise.
//
// ── WHAT IS DONE INSTEAD: ONE CONNECTION PER CONVERSATION ───────────────────
//
// The engine side was already built for this and had been for two versions.
// [remote.Session] is the CONVERSATION and [remote.server] is one CONNECTION;
// several connections may point at one session, and an [enginehost.Host] holds a
// MAP of sessions rather than one — its own comment says a host opens several
// conversations and a pipe opens one. What was missing was on this side: one
// surface had one connection, and one connection had one session.
//
// So a conversation opened beside gets a connection of its own. Its own
// [remote.Client], its own reader goroutine, its own stream pumps, its own facts
// replica, its own event ring, its own held questions, its own keyboard
// arbitration. NOTHING MUTABLE IS SHARED BETWEEN TWO CONVERSATIONS, which is the
// whole correctness argument and the reason this is a fleet of connections
// rather than a flag flipped on the old arrangement.
//
// THE ALTERNATIVE WAS A CONVERSATION ID ON EVERY FRAME — multiplexing the
// sessions down one pipe — and it was rejected as a protocol rewrite bought for
// one file descriptor on a unix socket. Over `--host` a second link is a second
// ssh child, which is a real cost, and it is paid on the same multiplexed
// transport the first one opened (`ControlMaster=auto`, chatv3_host.go).
//
// ── WHO GIVES A CONNECTION BACK ─────────────────────────────────────────────
//
// [besideAgent] is the whole of the lifecycle ownership. The surface already has
// exactly two roads off a conversation — `Close` when a person ends it and
// `Detach` when the window goes and the work does not (internal/tui3's
// keeper.go) — and both of them run through this wrapper, which does what the
// far side needs and then retires the transport. There is no third place a
// connection can leak from.
//
// ── AND THE BOOT CONNECTION IS THE WINDOW'S DOOR ONTO THE MACHINE ───────────
//
// The places, the ledger, the memory store, the search index, the recent list
// and the standing items are facts about the ENGINE'S MACHINE rather than about
// any conversation, but the protocol dispatches every call against a session. So
// they ride the boot connection, and THE BOOT CONNECTION IS NEVER RETIRED BY A
// CONVERSATION ENDING: closing the conversation on it ends that session and
// leaves the pipe standing, which is exactly what [remote.Agent.Close] has
// always done and what those readings need. It is given back once, by the door
// that opened it, when the window closes.

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/tui3"
)

// engineConn is one connection to an engine and the one way to give it back.
//
// shut is the DOOR's own teardown and not the client's: over `--host` there is
// an ssh child to reap behind the pipe, over the socket there is nothing but the
// pipe, and this package is the one that knows which. It is run at most once,
// because every road out of a conversation may reach it and a window quitting
// reaches it again.
type engineConn struct {
	client *remote.Client
	shut   func() error
	once   sync.Once
	err    error
}

func (c *engineConn) close() error {
	if c == nil {
		return nil
	}
	c.once.Do(func() {
		if c.shut != nil {
			c.err = c.shut()
			return
		}
		c.err = c.client.Close()
	})
	return c.err
}

// engineAsk is which conversation a new connection is being opened for.
//
// THE THREE FIELDS ARE THREE DIFFERENT INTENTIONS and the wire keeps them apart
// for the reason [remote.Hello.New] states: "open or create" is the wrong verb
// for a window that means one or the other.
type engineAsk struct {
	// workspace is the directory the conversation runs in, and empty is the one
	// this window is already in — which is what /new and the resume picker mean
	// when they name none.
	workspace string
	// session is a transcript to open, and empty with mint unset is this
	// workspace's latest-or-new.
	session string
	// mint says this is a conversation of its own: [remote.Hello.New], which
	// takes nothing that is already open and leaves the engine to choose the
	// file.
	mint bool
}

// engineDial opens ONE more connection to the same engine. It is the door's
// seam — this package owns ssh children and unix sockets, internal/remote owns
// frames — and nil is a door that cannot open a second one, which is the honest
// state of a test driving a loopback pipe.
type engineDial func(ask engineAsk) (*engineConn, error)

// engineFleet is one window's connections to one engine: the boot connection,
// and one per conversation opened beside it.
type engineFleet struct {
	// dest is the machine as the person typed it, empty for this one. It is the
	// same string [tui3.Options.Host] carries.
	dest string
	// workspace is the directory this window is in, as the ENGINE resolved it,
	// so a conversation opened with no workspace of its own asks for the path
	// that means something over there.
	workspace string
	dial      engineDial
	// boot is the connection the launch opened and the one every machine-wide
	// reading rides. It is in conns as well, because it is also a conversation.
	boot *engineConn

	// machine is what a conversation opened beside borrows from the door that
	// assembled the surface: the readings that are about the engine's machine
	// rather than about one conversation (hostOptions fills it).
	machine machineReadings

	mu    sync.Mutex
	conns []*engineConn
	// closers are what this window opened beside its connections and must join
	// when it lets them go — the model catalog [hostOptions] warms, whose warm
	// writes a cache when it lands (#1274). [engineFleet.closeAll] runs them.
	closers []func()
}

// own hands one close to [engineFleet.closeAll]. A nil fleet or a nil close is
// nothing to own.
func (f *engineFleet) own(shut func()) {
	if f == nil || shut == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closers = append(f.closers, shut)
}

// takeClosers hands over everything [engineFleet.own] was given and empties the
// list, so the joins run after the lock is let go.
func (f *engineFleet) takeClosers() []func() {
	f.mu.Lock()
	defer f.mu.Unlock()
	closers := f.closers
	f.closers = nil
	return closers
}

// machineReadings is the handful of per-window facts a beside conversation needs
// and must not re-derive: they are all about the engine's disk, they are already
// cached behind the surface, and a second copy per conversation would be a
// second walk of the far machine's places on every tab.
type machineReadings struct {
	// recent is this project's conversations, as [tui3.Options.RecentSessions].
	recent func() []tui3.Session
	// world is the far machine's places, cached, and whether it has answered
	// yet. It is what a conversation's own task rows are read out of.
	world func() (session.World, bool)
	// window is how many tokens a model accepts, resolved off this window's
	// catalog rather than the far machine's.
	window func(model string) int
	// draft is where a conversation in this workspace keeps its unsent
	// sentence, and empty is a window that keeps none.
	draft string
	// history is the recall store every conversation on this connection walks
	// with the up arrow. IT IS PER MACHINE AND NOT PER CONVERSATION — the store
	// is this laptop's `history.jsonl`, keyed by workspace — so a conversation
	// opened beside the first one has to be handed the same door the first one
	// got, or its box scrolls the transcript where it should recall the last
	// thing typed (internal/tui3's recall.go). It was left nil here until
	// 2026-09-09, and every conversation started from home lost the arrows.
	history tui3.History
}

// newEngineFleet is the fleet around a connection that has already said hello.
// A nil dial is a door that cannot open a second connection, and the surface is
// told so by getting no [tui3.Options.Start] and no [tui3.Options.Open].
func newEngineFleet(dest string, client *remote.Client, shut func() error, dial engineDial) *engineFleet {
	boot := &engineConn{client: client, shut: shut}
	return &engineFleet{
		dest:      strings.TrimSpace(dest),
		workspace: client.Welcome().Workspace,
		dial:      dial,
		boot:      boot,
		conns:     []*engineConn{boot},
	}
}

// client is the boot connection, which is what every machine-wide reading and
// every seam about the window rather than about one conversation is wired to.
func (f *engineFleet) client() *remote.Client { return f.boot.client }

// agent is the boot conversation's handle. It is a plain [remote.Agent] and NOT
// a [besideAgent], deliberately: closing this conversation must end the session
// and leave the connection standing, because the connection is this window's
// door onto the engine's machine (this file's header).
func (f *engineFleet) agent() *remote.Agent { return f.boot.client.Agent() }

// canBeside says this door can open a conversation beside the one it is
// showing. It is the one question [tui3.Options.SharedAgent] is the answer to:
// a door that cannot dial again really does hold one conversation at a time, and
// says so rather than letting somebody discover it.
func (f *engineFleet) canBeside() bool { return f != nil && f.dial != nil }

// start is [tui3.Options.Start]: a conversation of this window's own, in the
// workspace it names or in this one.
func (f *engineFleet) start(workspace string) (tui3.Conversation, error) {
	return f.take(engineAsk{workspace: workspace, mint: true})
}

// open is [tui3.Options.Open]: an earlier conversation, by transcript.
//
// A TRANSCRIPT THE ENGINE IS ALREADY RUNNING IS ATTACHED TO RATHER THAN REOPENED
// (internal/enginehost's open), so a conversation this window put down and picked
// up again is the same conversation with its work still in it — and one another
// window is holding is joined, with the keyboard arbitration that has always
// governed two surfaces in one room.
func (f *engineFleet) open(workspace, transcript string) (tui3.Conversation, error) {
	if strings.TrimSpace(transcript) == "" {
		return tui3.Conversation{}, fmt.Errorf("opening a conversation needs a transcript")
	}
	return f.take(engineAsk{workspace: workspace, session: transcript})
}

// take is the half both doors end in: one more connection, and the conversation
// bundle built around it.
//
// A DIAL THAT FAILED LEAVES NOTHING BEHIND. The surface's own contract is that a
// refused open costs nothing at all — the person stays exactly where they were,
// with a live conversation on screen (internal/tui3's [app.openBeside]) — so a
// connection that answered a welcome this door cannot use is retired here rather
// than left holding a session on the far machine.
func (f *engineFleet) take(ask engineAsk) (tui3.Conversation, error) {
	if !f.canBeside() {
		return tui3.Conversation{}, fmt.Errorf("this connection cannot open another conversation")
	}
	if strings.TrimSpace(ask.workspace) == "" {
		ask.workspace = f.workspace
	}
	conn, err := f.dial(ask)
	if err != nil {
		// A journal another window holds comes back over the socket as the
		// engine's sentence; it is handed to the surface as the lock it is, so
		// home asks that window for it (chatv3.go's [engineHeldRefusal]).
		return tui3.Conversation{}, asHeldRefusal(err)
	}
	welcome := conn.client.Welcome()
	if strings.TrimSpace(welcome.SessionFile) == "" {
		_ = conn.close()
		return tui3.Conversation{}, fmt.Errorf("%s opened no conversation", f.where())
	}
	f.hold(conn)
	return f.bundle(conn, welcome), nil
}

// where names the far end the way a sentence about it should, in the wire's own
// words: the machine the person typed, or "the engine" when they typed none.
func (f *engineFleet) where() string {
	if f.dest == "" {
		return "the engine"
	}
	return f.dest
}

func (f *engineFleet) hold(conn *engineConn) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.conns = append(f.conns, conn)
}

// forget takes one connection off the list and retires it. It is what
// [besideAgent] calls once its conversation is over on the far side.
func (f *engineFleet) forget(conn *engineConn) {
	f.drop(conn)
	_ = conn.close()
}

// drop takes one connection off the list. The retire runs in the caller after
// the lock is let go: closing a connection can take as long as an ssh child's
// reaping, and none of that belongs under the fleet's mutex.
func (f *engineFleet) drop(conn *engineConn) {
	f.mu.Lock()
	defer f.mu.Unlock()
	kept := f.conns[:0]
	for _, held := range f.conns {
		if held != conn {
			kept = append(kept, held)
		}
	}
	f.conns = kept
}

// closeAll retires every connection this window opened, the boot one last. It is
// the door's own defer and it is safe to call twice.
//
// THE SURFACE HAS USUALLY ALREADY DONE THE HALF THAT MATTERS by the time this
// runs: quitting detaches or closes every conversation it holds
// (internal/tui3's [app.leaveEverything]), which is what tells the engine
// whether the work outlives the window. This is the transport underneath that,
// and nothing here decides anything about anybody's work.
func (f *engineFleet) closeAll() {
	conns := f.takeAll()
	for _, conn := range conns {
		if conn != f.boot {
			_ = conn.close()
		}
	}
	_ = f.boot.close()
	// AND WHAT THE WINDOW OPENED BESIDE THEM, joined last: nothing it warmed may
	// write after the door has let go.
	for _, shut := range f.takeClosers() {
		shut()
	}
}

// takeAll hands over every connection this window opened and empties the list,
// so the retires — each as slow as the transport it reaps — run after the lock
// is let go rather than under it.
func (f *engineFleet) takeAll() []*engineConn {
	f.mu.Lock()
	defer f.mu.Unlock()
	conns := append([]*engineConn(nil), f.conns...)
	f.conns = nil
	return conns
}

// bundle is one conversation as the surface takes it: the agent that owns its
// connection, and every seam that is about THIS conversation rather than about
// the window.
func (f *engineFleet) bundle(conn *engineConn, welcome remote.Welcome) tui3.Conversation {
	agent := &besideAgent{countingAgent: &countingAgent{Agent: conn.client.Agent()}, conn: conn, fleet: f}
	link := hostLink(newHostSeams(conn.client))
	conv := tui3.Conversation{
		Agent:       agent,
		SessionFile: welcome.SessionFile,
		Workspace:   welcome.Workspace,
		Resumed:     welcome.Resumed,
		Notice:      hostEntryNotice(welcome),
		DraftFile:   f.machine.draft,
		History:     f.machine.history,
		Link:        &link,
		TaskRoom:    agent.TaskRoom,
	}
	if f.machine.window != nil {
		conv.ContextWindow = f.machine.window(welcome.Model)
	}
	if f.machine.recent != nil {
		conv.RecentSessions = f.machine.recent
	}
	if f.machine.world != nil {
		conv.TaskIndex = farTaskRows(f.machine.world, welcome.SessionFile)
	}
	return conv
}

// farTaskRows is [tui3.Options.TaskIndex] for ONE conversation: its own rows out
// of the far machine's world, matched on the transcript.
//
// IT IS A CLOSURE PER CONVERSATION AND THE WORLD IS SHARED, which is the split
// that matters: the walk is a reading of the engine's disk and is cached once
// behind the window, and which rows belong to whom is a question about the
// conversation asking.
func farTaskRows(world func() (session.World, bool), file string) func() ([]session.TaskIndexEntry, bool) {
	return func() ([]session.TaskIndexEntry, bool) {
		far, known := world()
		if !known {
			return nil, false
		}
		for _, row := range far.Sessions() {
			if filepath.Clean(row.Transcript) == filepath.Clean(file) {
				return append([]session.TaskIndexEntry(nil), row.Tasks.Rows...), true
			}
		}
		return nil, true
	}
}

// besideAgent is one conversation that OWNS ITS CONNECTION.
//
// It is [remote.Agent] with two methods replaced, and everything else — every
// optional door the surface type-asserts for, the standing lanes, the attach
// pair, the rewind pair, the file doors under `Client()` — is promoted
// unchanged, which is the whole reason it is an embedding and not a
// reimplementation. A method added to [remote.Agent] next year is a method this
// conversation has on the day it lands.
//
// THE TWO THAT CHANGE ARE THE TWO ROADS OFF A CONVERSATION, and they differ from
// each other exactly as they do on the far side: Close is a person saying the
// conversation is over, Detach is a window going while the work stays. Both end
// with this connection retired, because after either of them there is nothing
// left on it for this window to say.
type besideAgent struct {
	// The counting tee and not the bare remote agent: a conversation opened
	// beside runs in the engine like the one it was opened beside, and its
	// turns reach this process's tally the same way (telemetry_events.go).
	*countingAgent
	conn  *engineConn
	fleet *engineFleet
}

// Close ends the conversation on the far machine and then gives the connection
// back. The order is the point: the frame that ends the session has to be
// written before the pipe carrying it is closed.
func (b *besideAgent) Close() error {
	err := b.Agent.Close()
	b.fleet.forget(b.conn)
	return err
}

// Detach lets go of this VIEW and then gives the connection back — the window
// closed, and the turn, the tasks and the questions are the engine's to keep
// ([remote.Agent.Detach] holds the whole of what that means, including what it
// does instead against an engine whose whole life is one pipe).
func (b *besideAgent) Detach() error {
	err := b.Agent.Detach()
	b.fleet.forget(b.conn)
	return err
}

// hostLink is the connection seam in the SURFACE's shape, assembled from one
// client's own doors. It is a function rather than a literal at the door because
// EVERY conversation now needs one of these rather than the window needing one
// (internal/tui3's [Conversation.Link] says why the waiting questions make that
// correctness rather than tidiness).
func hostLink(seams hostSeams) tui3.LinkSeam {
	return tui3.LinkSeam{
		Note:           seams.Link,
		Ping:           seams.Ping,
		Notice:         seams.Notice,
		Held:           hostHeld(seams),
		Driving:        hostDriving(seams),
		DrivingChanged: seams.DrivingChanged,
		Take:           seams.Take,
		Follow:         hostFollow(seams),
		NewsSilent:     seams.NewsSilent,
	}
}
