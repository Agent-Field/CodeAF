package main

// engine.go is the far half of `aforge chat --host devbox`: the process that
// ssh starts on the other machine, holding the real conversation and answering
// frames about it on its own stdin and stdout (internal/remote).
//
// IT IS MACHINERY, NOT A COMMAND. It is deliberately absent from the usage text
// because there is nothing a person accomplishes by typing it — it draws
// nothing, reads no keys, and speaks a protocol. A surface dials it; that is the
// whole of its audience.
//
// STDOUT IS THE PROTOCOL AND NOTHING ELSE. One stray line of chatter there is a
// frame the surface cannot parse, which is the end of the session rather than a
// cosmetic fault, so everything this door has to say to a human goes to stderr
// and only when it is fatal.
//
// ── IT IS ALSO, NOW, A DOOR ONTO SOMETHING ALREADY RUNNING ───────────────────
//
// Version 2 of the wire separated a conversation's life from a connection's, so
// this door has two shapes and tries them in one order:
//
//  1. ATTACH. Dial this workspace's session host (internal/enginehost) and
//     splice the ssh pipes to its socket. The conversation is already there,
//     possibly mid-turn, and closing the lid does not end it. If no host is
//     running, one is started and this connection waits a moment for it.
//  2. THE PIPE. If a host cannot be reached or started for ANY reason — no
//     socket directory, a path too long for a unix socket, a spawn that failed,
//     a machine that refuses all of it — this process serves the conversation
//     itself, exactly as version 1 did, and the welcome says
//     [remote.Welcome.Persistent] is false so no surface promises a lifetime
//     this shape does not have.
//
// THE SECOND IS NOT A DEGRADED MODE, IT IS THE FLOOR. A machine where the host
// cannot work must still take a remote session.

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/enginehost"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
)

// A session agent is what the wire serves, and this is where the two are held
// against each other. A method added to [remote.WrappedAgent] that the session
// does not have fails HERE, at the door that wires them together, rather than
// as a mysterious refusal on somebody's laptop.
var _ remote.WrappedAgent = (*session.Agent)(nil)

// The name says "remote" because this tree has another engine: the swepro
// sentinel turns the same binary into a different program entirely (swepro.go),
// and two doors called runEngine in one package would be a coin toss for
// whoever reads the switch next.
func runRemoteEngine(args []string) error {
	flags := flag.NewFlagSet("engine", flag.ContinueOnError)
	workspace := flags.String("workspace", "", "directory to work in; relative paths are relative to the home directory, empty is the home directory")
	file := flags.String("session", "", "session transcript to open; empty opens this workspace's most recent")
	// --daemon is this process BEING the host rather than talking to one. It is
	// machinery of the machinery: nothing types it, [enginehost.Spawn] does.
	daemon := flags.Bool("daemon", false, "hold this workspace's conversations and answer surfaces on a socket")
	// --no-host is the escape hatch, and it exists because a fallback nobody can
	// ask for is a fallback nobody can use when the host is the thing that is
	// wrong. It serves the conversation on this pipe and never dials a socket.
	alone := flags.Bool("no-host", false, "serve this conversation on the pipe instead of attaching to a session host")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		// --daemon is deliberately not named here. It is how a host is started
		// and nothing a person accomplishes by typing it, so the usage line
		// offers the two flags somebody might mean and stays quiet about the
		// one they would only ever mean by accident.
		return fmt.Errorf("usage: aforge engine [--workspace path] [--session path] [--no-host]")
	}

	if *daemon {
		return runEngineHost(*workspace, *file)
	}
	if !*alone {
		if conn, err := attachEngineHost(*workspace); err == nil {
			// From here this process reads and writes nothing but bytes: the
			// handshake, the frames and every decision in them are between the
			// surface and the engine on the other side of that socket.
			return enginehost.Splice(os.Stdin, os.Stdout, conn)
		}
	}
	return remote.Serve(os.Stdin, os.Stdout, remote.Options{
		Boot: func(hello remote.Hello) (*remote.Engine, error) {
			return bootEngine(hello, *workspace, *file)
		},
	})
}

// attachEngineHost is step one: a connection to this workspace's host, starting
// one if nothing answers.
//
// THE WORKSPACE IS RESOLVED BEFORE THE HELLO IS READ, and it can be, because
// the surface puts it on the ssh command line as well as in the frame
// (chatv3_host.go's dialEngine) — which was already true and is what makes
// routing to a per-workspace socket possible at all without parsing a single
// frame here. A hand-run `aforge engine` with no --workspace resolves to the
// home directory, which is exactly what its hello would have meant.
//
// EVERY FAILURE ON THIS PATH IS ANSWERED THE SAME WAY, by the caller, with the
// pipe. Nothing here is worth a sentence on stderr: a machine with no host is
// not a machine with a problem.
func attachEngineHost(workspaceFlag string) (net.Conn, error) {
	workspace, err := engineWorkspace(workspaceFlag)
	if err != nil {
		return nil, err
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return enginehost.Attach(workspace, func() error {
		return enginehost.Spawn(self, "engine", "--daemon", "--workspace", workspace)
	})
}

// runEngineHost is this process being the host: it moves into the workspace
// once, the way every engine does, and then holds that workspace's
// conversations until the idle policy retires it.
//
// A HOST THAT FINDS ANOTHER HOST EXITS WITHOUT A WORD. That is not a failure —
// the machine is in exactly the state that was asked for — and this process was
// started by another one that is about to dial the socket either way.
func runEngineHost(workspaceFlag, sessionFlag string) error {
	workspace, err := engineWorkspace(workspaceFlag)
	if err != nil {
		return err
	}
	if err := os.Chdir(workspace); err != nil {
		return fmt.Errorf("open %s: %w", workspace, err)
	}
	err = enginehost.Run(workspace, enginehost.Options{
		Boot: func(hello remote.Hello) (*remote.Engine, error) {
			return bootEngine(hello, workspace, sessionFlag)
		},
		// WHICH CONVERSATION A HELLO WANTS is the session file it named, and
		// naming none is this workspace's latest-or-new — the same meaning
		// --session has everywhere else. So two surfaces that both say nothing
		// are asking for the same conversation, which is the whole of "sit down
		// somewhere else and be in it".
		Key: func(hello remote.Hello) string {
			return firstEngineWord(hello.Session, sessionFlag)
		},
	})
	if errors.Is(err, enginehost.ErrHostRunning) {
		return nil
	}
	return err
}

// bootEngine opens the conversation the hello asked for.
//
// THE HELLO WINS over the flags when it names anything, and the flags are what a
// person running this by hand can say. Both exist because both are true: the
// surface puts the workspace on the ssh command line so the engine starts in the
// right place even if the handshake never happens, and it puts it in the hello
// because the hello is the frame that gets an answer.
func bootEngine(hello remote.Hello, workspaceFlag, sessionFlag string) (*remote.Engine, error) {
	workspace, err := engineWorkspace(firstEngineWord(hello.Workspace, workspaceFlag))
	if err != nil {
		return nil, err
	}
	// The engine moves INTO the workspace before it assembles anything, so that
	// every tool the session runs — a bash command, a relative path in an edit —
	// happens where the work is. A session config carries the directory too, and
	// this is the other half of the same statement: the process's own idea of
	// where it is has to agree with it.
	if err := os.Chdir(workspace); err != nil {
		return nil, fmt.Errorf("open %s: %w", workspace, err)
	}

	// ONE PROCESS, ONE WORKSPACE — and, when this process is a host, SEVERAL
	// CONVERSATIONS IN IT. The process half of a launch is opened once and
	// shared ([openEngineProcess]): the profile, the model catalog, the harness
	// registry and the recall store are properties of the machine and the
	// directory, not of the conversation, and opening a second set of them
	// would be the second assembly this tree keeps refusing. The chdir above is
	// still the only chdir in the tree (chatv3_process.go), which is exactly
	// why a host holds one workspace and not two.
	proc, err := openEngineProcess()
	if err != nil {
		return nil, err
	}
	// THE MODEL IS BUILT INTO THE SESSION AND NOT SET ON IT A MOMENT LATER,
	// which is the whole of what carrying it in the hello bought. The surface
	// used to open the conversation and then switch it (chatv3_host.go's old
	// applyHostChoices), and every turn a person could type rode the model they
	// asked for — but the session file's first line named the model the session
	// was BORN on, which was the wrong one. Nobody on the screen could see the
	// difference. The journal could, and the journal is the record.
	launch, err := openV3Launch(proc, v3Options{
		Workspace: workspace,
		Model:     strings.TrimSpace(hello.Model),
		Session:   firstEngineWord(hello.Session, sessionFlag),
	})
	if err != nil {
		return nil, err
	}
	// There IS a surface answering the consent cards; it is simply on another
	// machine. This is the same line `aforge chat` sets and for the same reason,
	// and it is the one fact the shared assembly cannot know for itself.
	cfg := launch.Config
	cfg.AskConsent = true

	// BUILDING A HARNESS IS STILL OFF OVER A CONNECTION, AND THE REASON HAS
	// CHANGED. It used to be "the card would arrive in an empty room", and a
	// persistent engine retired that sentence: a question raised with nobody
	// attached now WAITS and is handed to the next surface that arrives
	// (internal/remote's held.go). That is a real change and it is not enough
	// here, because the design card never reaches this wire at all.
	//
	// THE DESIGN LANE IS A SUBSCRIPTION AND THIS PROTOCOL HAS NO DOOR FOR ONE.
	// A design outlives the turn that asked for it, so its card is not emitted
	// on any turn's stream — internal/session's emitHarness sends only to the
	// watchers of [session.Agent.HarnessDesigns], which is a method
	// [remote.WrappedAgent] does not carry and a remote handle does not have.
	// Nothing crosses, so there is nothing to hold; the waiting room can only
	// keep a question that arrived. A design started here would still run two
	// model calls and end with a page nobody is ever shown.
	//
	// Nil is the honest way to say so rather than a special case: session.Config
	// already states that A NIL STORE IS BUILDING OFF, on the same terms a nil
	// RunHarness is detection off, so the designer simply is not among the
	// things this conversation can do and the model says as much instead of
	// starting work nobody will ever be shown. RUNNING a harness that already
	// exists is untouched — that rides Harnesses and RunHarness, which the
	// shared assembly still fills, and it works over a connection today. What
	// would light this up is a wire door for the standing lanes, which is a lane
	// of its own and not a line in this file.
	cfg.HarnessStore = nil

	// AND FOR THE SAME REASON, cfg.HarnessCards IS LEFT FALSE — the one line on
	// this list that is a silence rather than a statement. It is the door saying
	// "a surface here holds the harness lane", and no surface here does; so chat
	// is not given the verb that offers a saved program with an intake card
	// (internal/session's canProposeSubharness), because that card travels the
	// same subscription the design card does and reaches this wire no more than
	// it does. Its answer would not fit either: ResolveSubharness is not on
	// [remote.WrappedAgent], so even a card that crossed would be a key that
	// pressed nothing — which is why internal/remote's held.go deliberately
	// holds four kinds of question and not five.
	// RUNNING a saved program is untouched: `/subharness` is a surface door, and
	// the surface on the far end of this wire has no registry to open either.

	// AND THE AMBIENT SIDE IS ON, which is the one capability on this list that
	// a connection does not take away. It arrives already filled, from the
	// shared assembly every v3 door goes through (chatv3.go's [openV3Launch]
	// sets Config.Standing and starts this process ticking), and it is left
	// alone here rather than rebuilt — one source of truth about where the store
	// lives and what a pass may do.
	//
	// IT IS SAFE BECAUSE THE ENGINE IS THE MACHINE. Everything the two
	// capabilities above lack is present here: the store is a directory under
	// THIS machine's AFORGE_HOME ([v3StandingRoot]), a firing runs under THIS
	// machine's profile rules (chatv3_standing.go's header states that law), the
	// OS timer a first yes offers to install is THIS machine's timer, and the
	// work an item does happens where the workspace is. And the card travels a
	// road the two above do not: the standing proposal crosses as an ordinary
	// event on the turn's own stream (internal/remote's EventWire) and the
	// answer crosses back as ResolveStanding, so the person sitting on the other
	// end of this wire is the person who says yes. Nobody being there at that
	// moment no longer loses it either — a proposal raised with no surface
	// attached is held and handed to the next one (internal/remote's held.go).
	// A session that could leave nothing behind over --host would have made the
	// ambient side a property of which terminal somebody happened to open.

	// AN ADAPTIVE RUN IS OFF OVER A CONNECTION, for the same reason and by the
	// same road, and its reason has been re-checked rather than inherited. A
	// run's notes, its gauge and — the one that matters — its FUEL GATE all
	// arrive on a standing subscription the surface opens on the agent
	// (internal/session's Orchestrations, asserted by internal/tui3's runAgent),
	// and a remote handle has no such method. The waiting room does not reach
	// this one either: the gate's question is
	// [session.EventOrchestratePause], answered through ResolveOrchestrate, and
	// neither the event nor the answer has a door on this wire — so a question
	// held for it would be one nobody could ever say yes to. A run started here
	// would spend the person's money and stop at its cap in silence, four hours
	// from now, with nothing on any screen. So this session is built by
	// session.New rather than by
	// [v3OpenSession]: nothing fills Config.OrchestrateRunner, which session
	// already states is orchestration off, and the model is simply not handed the
	// verb (its tools_harness.go).
	agent, cfg, notice, err := openV3Agent(cfg, workspace, session.New)
	if err != nil {
		return nil, err
	}
	// The boot override for how hard this session's model is asked to think,
	// landed the same way every local door lands it (chatv3.go's SetReasoning)
	// and on the same model — the one this session opened on. A level the
	// person did not name leaves the session on whatever the profile says,
	// which is what an empty string already means everywhere else.
	if level := strings.TrimSpace(hello.Level); level != "" {
		agent.SetReasoning(level)
	}
	transcript, resumed := launch.SessionFile, launch.Resumed
	if notice != "" {
		// The session file moved under us, so the welcome has to name the new
		// one — everything the surface prints about this conversation comes off
		// that frame.
		transcript, resumed = cfg.SessionFile, false
	}

	guard.Go("engine/models", func() { warmV3Models(launch.Models, agent, launch.Model) })

	return &remote.Engine{
		Agent:       agent,
		Workspace:   workspace,
		SessionFile: transcript,
		Resumed:     resumed,
		Note:        notice,
		// Where a picture arriving on the wire lands: the engine's own session
		// folder, the same answer the local launch assembly gives its session.
		Place: cfg.Place,
		// The far half of a remote YOLO badge: this machine's own tool-approval
		// row, read the same way the local surface reads its own
		// (internal/tui3's readApproval). Empty when there is no profile
		// directory to read, which the welcome's omitempty and the badge's
		// emptiness law both already handle.
		ApprovalMode: config.ToolApprovalModeAt(launch.Settings.ProfileDir),
		// The three doors a remote surface reaches through, and every one of
		// them is a closure the local surface already has by another name: /new,
		// the resume picker, and the welcome box's list of recent conversations.
		// They are built on THIS config, because the model, the gate, the roles
		// and the rail are properties of the launch and a conversation opened
		// from the picker is the same launch.
		Fresh: func() (remote.WrappedAgent, string, error) {
			place, err := v3NextSession(cfg.Place, workspace)
			if err != nil {
				return nil, "", err
			}
			fresh, err := v3PointAt(cfg, place)
			if err != nil {
				return nil, "", err
			}
			replacement, err := session.New(fresh)
			if err != nil {
				return nil, "", err
			}
			return replacement, fresh.SessionFile, nil
		},
		Open: func(name string) (remote.WrappedAgent, bool, error) {
			path, err := engineSessionPath(name)
			if err != nil {
				return nil, false, err
			}
			earlier, err := v3Reopen(cfg, path, workspace)
			if err != nil {
				return nil, false, err
			}
			// Whether the file was found is asked BEFORE it is opened, because
			// opening it creates it: a path nobody has written yet is a new
			// conversation, and the surface says so on its first line.
			_, statErr := os.Stat(path)
			replacement, err := session.New(earlier)
			if err != nil {
				// Returned unwrapped, the way the local picker returns it: a
				// locked file's error names the file, and the surface prints
				// exactly that.
				return nil, false, err
			}
			return replacement, statErr == nil, nil
		},
		// The engine machine's ambient side, as a remote surface reads it, off
		// the SAME store this session proposes into. What a surface does with
		// them is the surface's business and is stated where it wires them
		// (chatv3_host.go's [hostStanding]: over --host the live reader is the
		// status line's `keeping an eye on` segment, because home does not open
		// on a remote session at all). They are closures on the store rather
		// than the store itself for [tui3.StandingSeam]'s own reason — the door
		// owns where it lives and how it is opened — and they are absent
		// entirely when the ambient side could not be built, which the surface
		// reads as nothing to show rather than as an empty list.
		StandingItems: engineStandingItems(cfg.Standing),
		StandingSave:  engineStandingSave(cfg.Standing),
		Recent: func() []session.Summary {
			// Both shapes, exactly as the local list reads them
			// ([v3RecentSessions]): the far machine's disk is under the same
			// decision as this one's, and a list that answered differently
			// over a connection would be a second law about one layout.
			return session.RecentSessions(launch.Bucket, v3RecentSessionSlots)
		},
	}, nil
}

// engineProcess is the once-per-process half of a v3 launch, opened on the
// first conversation this process serves and shared by every one after it.
//
// IT IS A MEMO BECAUSE A HOST OPENS SEVERAL CONVERSATIONS AND A PIPE OPENS ONE.
// [openV3Process] resolves the profile, the model catalog, the sub-harness
// registry and the recall store — every one of them a fact about the MACHINE
// and the directory rather than about a conversation — and a second copy would
// be a second set of governance rows and a second handle on the same store.
// For the pipe engine this changes nothing at all: one conversation calls it
// once, exactly as before.
var engineProcess struct {
	once sync.Once
	proc *v3Process
	err  error
}

func openEngineProcess() (*v3Process, error) {
	engineProcess.once.Do(func() {
		engineProcess.proc, engineProcess.err = openV3Process("engine")
	})
	return engineProcess.proc, engineProcess.err
}

// engineStandingItems and engineStandingSave are the two standing doors, or nil.
//
// NIL IS THE AMBIENT SIDE OFF AND IT IS NEVER A CLOSURE THAT FAILS, which is
// the same reading [v3Standing] already asks every caller for: an engine with no
// store hands the surface nothing, the surface draws no band, and the model
// never had the `stand` verb either. A pair of closures that answered an error
// on every call would be a capability that is present and broken.
func engineStandingItems(seam *session.Standing) func(string) ([]standing.Item, error) {
	if seam == nil || seam.Store == nil {
		return nil
	}
	return seam.Store.ForWorkspace
}

func engineStandingSave(seam *session.Standing) func(standing.Item) error {
	if seam == nil || seam.Store == nil {
		return nil
	}
	return seam.Store.Save
}

// engineWorkspace resolves the directory the surface asked for.
//
// EMPTY IS THE HOME DIRECTORY and a relative path is relative to it — NOT to the
// process's own directory. That is not a convenience, it is what the person
// typed: `aforge chat --host devbox:work/api` is read by whoever is holding the
// ssh session, and an ssh command starts in the home directory. Resolving
// "work/api" against wherever sshd happened to leave the process would make the
// same words mean different places on different machines.
//
// A directory that is not there is refused at the door. The alternative is a
// session assembled against a path that does not exist, which fails later, in a
// tool call, with a message about a file.
func engineWorkspace(path string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return home, nil
	}
	expanded, err := expandHome(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(expanded) {
		expanded = filepath.Join(home, expanded)
	}
	expanded = filepath.Clean(expanded)
	info, err := os.Stat(expanded)
	if err != nil {
		return "", fmt.Errorf("open %s: %w", expanded, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("open %s: not a directory", expanded)
	}
	return expanded, nil
}

// engineSessionPath is the same reading for a transcript the surface named. A
// session file comes off the engine's own listing in practice, so it is already
// absolute; a relative one is read against the home directory for the reason the
// workspace is.
func engineSessionPath(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("open session: no file was named")
	}
	path, err := expandHome(name)
	if err != nil {
		return "", err
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Clean(filepath.Join(home, path)), nil
}

func firstEngineWord(words ...string) string {
	for _, word := range words {
		if trimmed := strings.TrimSpace(word); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
