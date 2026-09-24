package main

// engine.go is the far half of `codeaf chat --host devbox`: the process that
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
//  1a. AND THE HOST IS ASKED WHICH BUILD IT IS FIRST. A host outlives the
//     binary that started it, so `rm bin/codeaf && make build` on this machine
//     leaves the NEW codeaf answering `codeaf version` while the OLD one is
//     still holding the socket — and a splice that copied bytes handed the new
//     surface straight to it. What came back was the old host's own refusal
//     about protocol versions, telling the person to update a machine they had
//     just updated. So the socket is asked (internal/remote's whois.go) and a
//     host of another build is retired and replaced rather than attached to.
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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/buildinfo"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/enginehost"
	"github.com/Agent-Field/codeaf/internal/remote"
	"github.com/Agent-Field/codeaf/internal/session"
	"github.com/Agent-Field/codeaf/internal/standing"
	"github.com/Agent-Field/codeaf/internal/store"
)

// A session agent is what the wire serves, and this is where the two are held
// against each other. A method added to [remote.WrappedAgent] that the session
// does not have fails HERE, at the door that wires them together, rather than
// as a mysterious refusal on somebody's laptop.
var _ remote.WrappedAgent = (*session.Agent)(nil)

// The name says "remote" because what this door opens is the engine on the
// other end of the wire — the host this surface talks to — and a plain
// runEngine would read as the thing that runs work in this process.
func runRemoteEngine(args []string) error {
	flags := commandFlags("engine")
	workspace := flags.String("workspace", "", "directory to work in; relative paths are relative to the home directory, empty is the home directory")
	file := flags.String("session", "", "session transcript to open; empty opens this workspace's most recent")
	// --daemon is this process BEING the host rather than talking to one. It is
	// machinery of the machinery: nothing types it, [enginehost.Spawn] does.
	daemon := flags.Bool("daemon", false, "hold this workspace's conversations and answer surfaces on a socket")
	// --no-host is the escape hatch, and it exists because a fallback nobody can
	// ask for is a fallback nobody can use when the host is the thing that is
	// wrong. It serves the conversation on this pipe and never dials a socket.
	alone := flags.Bool("no-host", false, "serve this conversation on the pipe instead of attaching to a session host")
	// --stop is the one flag here a PERSON types, and it exists because the
	// refusal below sends them to it: something older is holding this
	// workspace and has to be let go of before a current build can hold it.
	stop := flags.Bool("stop", false, "stop whatever is holding this workspace's conversations on this machine")
	// --stop-all IS THE ONE A PERSON REACHES FOR WHEN THEY DO NOT KNOW WHICH
	// WORKSPACE IS THE PROBLEM, and that is the ordinary case: the refusal names
	// a machine, a person has run codeaf in six folders this month, and finding
	// the one that will not let go means reading a directory of hashes. It is
	// the same stand-down as --stop, asked of every workspace this machine has a
	// host directory for, one at a time and named as it goes.
	stopAll := flags.Bool("stop-all", false, "stop every engine this machine is holding, in every workspace")
	if err := parseCommandFlags(flags, args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		// --daemon is deliberately not named here. It is how a host is started
		// and nothing a person accomplishes by typing it, so the usage line
		// offers the two flags somebody might mean and stays quiet about the
		// one they would only ever mean by accident.
		return fmt.Errorf("usage: codeaf engine [--workspace path] [--session path] [--no-host] [--stop] [--stop-all]")
	}

	// AN ENGINE CAN OPEN A TEAM CONVERSATION NOBODY HOLDS, so team traffic can
	// wake it (team_resume.go). Only the two roads below that serve sessions
	// ever use the door; the splice between them serves none.
	armTeamResume()
	if *daemon {
		return runEngineHost(*workspace, *file)
	}
	if *stopAll {
		// THE TWO FLAGS ARE NOT COMBINED, they are ordered: --stop-all is a
		// superset of --stop, so a person who typed both meant the larger one
		// and being refused for saying it twice would be pedantry.
		return runEngineStopAll()
	}
	if *stop {
		return runEngineStop(*workspace)
	}
	if !*alone {
		conn, err := attachEngineHost(*workspace)
		if err == nil {
			// From here this process reads and writes nothing but bytes: the
			// handshake, the frames and every decision in them are between the
			// surface and the engine on the other side of that socket.
			return enginehost.Splice(os.Stdin, os.Stdout, conn)
		}
		var stale *staleHost
		if errors.As(err, &stale) {
			// THE ONE FAILURE ON THIS ROAD THAT IS NOT ANSWERED WITH THE PIPE,
			// and the reason is the session file. Something older is holding
			// this workspace's conversation, which means it is holding the
			// journal's lock; an engine that fell back to the pipe here would
			// try to open the same file, fail on that lock, and say so in a
			// sentence about a path — burying the one fact the person needs.
			// So the truth goes down the wire instead.
			return quietRefusal(remote.Refuse(os.Stdout, stale.reason))
		}
	}
	err := remote.Serve(os.Stdin, os.Stdout, remote.Options{
		Boot: func(hello remote.Hello) (*remote.Engine, error) {
			return bootEngine(hello, *workspace, *file)
		},
	})
	// THE USAGE WRITER A REMOTE TURN STARTED HAS AN OWNER ON THIS PATH TOO: the
	// fallback served the conversation on this pipe, and the ledger writer it
	// started belongs to this process exactly as the local launch belongs to
	// its own. The close is the same door [v3Process.closeAll] uses; it drains
	// the queue before joining, so the last row is on disk as well.
	session.CloseUsage()
	closeEngineProcess()
	return quietRefusal(err)
}

// quietRefusal is the door's half of [remote.Refusal]: a handshake this engine
// turned away has already had its reason written down the wire, and OVER SSH
// THIS PROCESS'S STDERR IS THE PERSON'S TERMINAL — the same terminal the
// surface is about to draw that reason on. Printing it here as well is how one
// refusal became two identical `error:` lines on `codeaf chat --host
// devbox:/nowhere`. The exit code stays 1, because the engine did fail.
func quietRefusal(err error) error {
	var refusal *remote.Refusal
	if errors.As(err, &refusal) {
		return exitStatus(1)
	}
	return err
}

// staleHost is an older codeaf still holding this workspace, and it is the ONE
// reason `codeaf engine` refuses instead of falling back to the pipe. The
// sentence has already been written for a person to read; the caller's whole
// job is to put it on the wire.
type staleHost struct{ reason string }

func (s *staleHost) Error() string { return s.reason }

// attachEngineHost is step one: a connection to this workspace's host, starting
// one if nothing answers — and, before any of that, a question about which
// build is already there.
//
// THE WORKSPACE IS RESOLVED BEFORE THE HELLO IS READ, and it can be, because
// the surface puts it on the ssh command line as well as in the frame
// (chatv3_host.go's dialEngine) — which was already true and is what makes
// routing to a per-workspace socket possible at all without parsing a single
// frame here. A hand-run `codeaf engine` with no --workspace resolves to the
// home directory, which is exactly what its hello would have meant.
//
// ── THE THREE THINGS THE QUESTION CAN FIND ──────────────────────────────────
//
// A host of THIS build is spliced onto, which is the ordinary answer and the
// only one that costs anything at all — one extra connection, on a unix socket,
// asking one question.
//
// A host of ANOTHER build is asked to go, and goes if it is holding nothing.
// The conversation it was holding is closed properly on the way out, its
// journal flushed, and the next line of this function starts a fresh host from
// the binary that is on disk now — so a rebuild simply works on the next
// connection instead of trapping somebody.
//
// A host that will not go, or one so old it cannot be asked, is REFUSED with a
// sentence naming the machine and the way out. Nothing here signals a process
// it could not ask, and nothing here decides on somebody else's behalf that
// their turn is over.
//
// EVERY OTHER FAILURE ON THIS PATH IS ANSWERED BY THE CALLER WITH THE PIPE, and
// none of them is worth a sentence: a machine with no host is not a machine
// with a problem.
func attachEngineHost(workspaceFlag string) (net.Conn, error) {
	workspace, err := engineWorkspace(workspaceFlag)
	if err != nil {
		return nil, err
	}
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if _, err := clearStaleEngineHost(workspace); err != nil {
		return nil, err
	}
	return enginehost.Attach(workspace, func() error {
		return enginehost.Spawn(workspace, self, "engine", "--daemon", "--workspace", workspace)
	})
}

// clearStaleEngineHost asks on behalf of the build that is running, which is the
// only caller there has ever been. The yardstick is a parameter one layer down
// because a test needs two builds of one source and a test binary is linked once.
func clearStaleEngineHost(workspace string) (string, error) {
	return clearStaleEngineHostFor(workspace, buildinfo.Identity())
}

// clearStaleEngineHostFor is the question and what is done with the answer. A nil
// error means "go ahead and attach": either nothing is holding this workspace,
// or what is holding it is this build, or what was holding it has gone — or it
// is an older build of the SAME WIRE that would not let go, which is the one
// case that answers with a sentence AND a nil error.
//
// WHAT MAKES TWO BUILDS THE SAME ONE IS THE SOURCE THEY WERE BUILT FROM, and
// [buildinfo.Identity] is where that is decided. Rebuilding a commit does not
// make an older codeaf, and while the moment of the build was part of the answer
// every window opened after a `make build` told somebody their own engine was
// behind (#730).
//
// ── A BUSY OLD HOST IS ATTACHED TO, NOT REFUSED ─────────────────────────────
//
// It used to be the third refusal here: a host on yesterday's binary, holding a
// turn or a task, was asked to go, said no, and the person was told to run
// `codeaf engine --stop` — which would have ENDED the very work they were trying
// to get back on screen. What they wanted was their running conversation, and it
// was one socket away.
//
// SO THE VERSION IS WHAT DECIDES AND THE BUILD IS NOT. A host answering the same
// [remote.Version] speaks every frame this binary speaks; the difference between
// the two builds is a difference in what happens NEXT TIME, and it settles
// itself — a host whose binary has been replaced retires the moment it is
// holding nothing (internal/enginehost's binary.go). So this attaches, and hands
// back one line for the entry notice saying which state the machine is in. A
// DIFFERENT wire version keeps the refusal it has always had, because there is
// no attaching to a peer whose frames this build cannot read.
//
// AND THE NOTICE IS OWED WHENEVER THE OLDER BUILD IS THE ONE ANSWERING, not only
// when it is holding work. It used to speak only for a busy host: a host whose
// conversation had gone quiet — the turn finished, the person stepped away — was
// asked to go, went, and the window opened on the fresh build WITHOUT A WORD, so
// the person whose rebuild had not yet reached the conversation they were
// reading was never told which build they had been talking to. That is the ghost
// this line exists to name: whatever the older build was holding, the person is
// told it was an older build, and told it is gone the moment it lets go.
func clearStaleEngineHostFor(workspace, thisBuild string) (string, error) {
	host, err := enginehost.Ask(workspace, remote.WhoIs{})
	switch {
	case errors.Is(err, remote.ErrNoHostThere):
		// The socket answered with a refusal, which is what EVERY BUILD FROM
		// BEFORE THE EXCHANGE says to a question it has never heard of. It
		// cannot be asked whether it is busy either, so it is never ended from
		// here — a person is told, in words, what is true and what to type.
		// NO ANSWER CARRIES NO WORKSPACE OF ITS OWN, so the one this process
		// asked about is the one named: it is the workspace whose socket just
		// refused, which is exactly what a person has to stop.
		return "", &staleHost{reason: staleEngineHostSentence(false, workspace)}
	case err != nil:
		// Nothing answered at all: no host, or one that has stopped reading.
		// Both are the ordinary road — Attach starts one.
		return "", nil
	case host.Version == remote.Version && host.Build == thisBuild:
		return "", nil
	}
	// Another build, and it is answering, so it can be asked to go.
	if err := enginehost.Retire(workspace, false); err != nil {
		if errors.Is(err, enginehost.ErrHostBusy) && host.Version == remote.Version {
			return busyEngineHostSentence(host.Busy), nil
		}
		if errors.Is(err, enginehost.ErrHostBusy) {
			return "", &staleHost{reason: staleEngineHostSentence(true, hostWorkspace(host, workspace))}
		}
		return "", &staleHost{reason: staleEngineHostSentence(false, hostWorkspace(host, workspace))}
	}
	// IT WENT, and it went without a fight: whatever it was holding, it was
	// holding nothing that could not be let go. The window opens on the fresh
	// host either way — but it was the OLDER build answering until this moment,
	// and a person whose rebuild had not reached the conversation they were
	// reading deserves to be told so. A DIFFERENT wire keeps its silence: there
	// the refusal above already said the machine was behind.
	if host.Version == remote.Version {
		return olderEngineHostSentence(host.Busy), nil
	}
	return "", nil
}

// staleEngineHostSentence is what the person reads, and it is written on the
// far machine because the far machine is the one with the problem.
//
// IT NAMES THE MACHINE AND NOT "THE OTHER END". This sentence is printed on a
// laptop by a surface that has three windows open onto three machines, and the
// old version of this refusal — "update the older one" — failed precisely by
// being unable to say WHICH half was old. The name is this machine's own
// hostname, the same one every window in a shared conversation is labelled with
// ([remote.MachineName]).
// IT NAMES THE WORKSPACE IN THE COMMAND, and that is the half this sentence was
// missing. `codeaf engine --stop` with NO `--workspace` resolves to the HOME
// directory ([engineWorkspace]), never to the workspace being complained about —
// so a person reading this line inside a checkout, and typing it exactly as
// written, stopped their healthy home host and left the offending one running.
// The line then came back on the next launch, forever, which is how a stale host
// on this machine outlived eight rebuilds and twenty-two hours.
// [sessionHeldElsewhereSentence] in chatv3.go already spells the flag for this
// exact reason and names this function as its voice; this is that voice saying
// the same thing.
//
// The workspace comes from the host's OWN answer ([remote.HostSelf.Workspace]),
// not from what this process thinks it opened: the sentence is about the machine
// that will not let go, so the words a person types have to name what IT is
// holding. An answer that carries no workspace keeps the bare command rather
// than inventing a path.
func staleEngineHostSentence(busy bool, workspace string) string {
	name := remote.MachineName()
	if strings.TrimSpace(name) == "" {
		name = "that machine"
	}
	stop := "codeaf engine --stop"
	if workspace = strings.TrimSpace(workspace); workspace != "" {
		stop += " --workspace " + workspace
	}
	if busy {
		return fmt.Sprintf("engine: %s is still running an older codeaf and something is still going in it — let that finish, or run %s on %s", name, stop, name)
	}
	return fmt.Sprintf("engine: %s is still holding this conversation on an older codeaf — run %s on %s", name, stop, name)
}

// hostWorkspace is what the host says it is holding, and what this process asked
// about when the host did not say. The host's own answer is preferred because
// the sentence is about the machine that will not let go — but a build old
// enough to leave the field empty must not cost a person the flag, and the
// workspace asked about is the same directory in every case that reaches here.
func hostWorkspace(host remote.HostSelf, asked string) string {
	if held := strings.TrimSpace(host.Workspace); held != "" {
		return held
	}
	return asked
}

// busyEngineHostSentence is the one line a person reads when their conversation
// comes back on a host that is one build behind and could not be let go of yet.
// It is [staleEngineHostSentence]'s voice and its opposite in every other way:
// nothing is wrong, nothing is owed, and the sentence exists so a surface never
// quietly runs against a binary that is not the one on disk.
//
// IT NAMES WHAT IS ACTUALLY HELD. busy is the host's own answer, and it is the
// difference between a turn still running and a conversation that is only being
// kept warm — the two are not the same ghost, and the person being told deserves
// to know which one is between them and the rebuild.
func busyEngineHostSentence(busy bool) string {
	name := remote.MachineName()
	if strings.TrimSpace(name) == "" {
		name = "this machine"
	}
	if busy {
		return fmt.Sprintf("the engine on %s is an older codeaf and is still holding work — it picks up this build the moment it goes quiet", name)
	}
	return fmt.Sprintf("the engine on %s is an older codeaf — it is holding this conversation and picks up this build the moment you leave it", name)
}

// olderEngineHostSentence is the same notice for the host that went quietly: an
// older build was answering until the moment this window arrived, and it has
// already stepped aside. Nothing is owed and nothing is still pinned — the
// sentence exists so a person whose rebuild had not yet reached the conversation
// they were reading is told which build they had been talking to, rather than
// finding it out by the fix not being there.
func olderEngineHostSentence(busy bool) string {
	name := remote.MachineName()
	if strings.TrimSpace(name) == "" {
		name = "this machine"
	}
	if busy {
		return fmt.Sprintf("the engine on %s was an older codeaf until just now — it has picked up this build", name)
	}
	return fmt.Sprintf("the engine on %s was an older codeaf holding this conversation — it has picked up this build", name)
}

// runEngineStop is `codeaf engine --stop`: whatever is holding this workspace
// on this machine, let go of.
//
// IT TALKS TO A PERSON, WHICH IS WHY IT IS THE ONE DOOR IN THIS FILE THAT
// PRINTS. Every other shape of `codeaf engine` owns stdout as the protocol and
// a stray line there is a frame the surface cannot parse; this one is nobody's
// engine, it is somebody typing on the machine itself and waiting to be told
// what happened.
func runEngineStop(workspaceFlag string) error {
	workspace, err := engineWorkspace(workspaceFlag)
	if err != nil {
		return err
	}
	stopped, err := enginehost.Stop(workspace)
	if err != nil {
		return err
	}
	if !stopped {
		fmt.Printf("nothing is holding %s here\n", workspace)
		return nil
	}
	fmt.Printf("stopped holding %s — the next connection starts fresh from this build\n", workspace)
	return nil
}

// runEngineStopAll is `codeaf engine --stop-all`: every engine this machine is
// holding, in every workspace, let go of.
//
// IT EXISTS BECAUSE THE REMEDY USED TO REQUIRE KNOWING THE ANSWER. A stale host
// announces itself by refusing a launch, and the fix is `--stop --workspace
// <path>` — but the person reading that has run codeaf in six folders and the
// state root names them by hash. On 2026-09-12 a host on an older wire held one
// checkout for twenty-two hours and eight rebuilds, and clearing it took reading
// a directory of hashes to find which one it was. This is that reading, done by
// the program.
//
// EVERY WORKSPACE IS NAMED AS IT GOES, and one that refuses does not stop the
// sweep: the whole point is the workspace you did not know about, so a failure
// on the third of five must not hide the fourth. The refusals are collected and
// reported together at the end, and the exit code says whether any of them
// happened.
//
// A DIRECTORY WHOSE HOST HAS GONE IS NOT A FAILURE. [enginehost.Stop] answers
// false for a socket nobody is listening on, which is the ordinary state of
// every workspace anybody has ever opened and closed, so those are counted and
// summarised rather than printed one by one — a sweep that listed thirteen
// "nothing there" lines would bury the one line that mattered.
func runEngineStopAll() error {
	held, err := enginehost.Held()
	if err != nil {
		return err
	}
	if len(held) == 0 {
		fmt.Println("nothing is holding any workspace here")
		return nil
	}
	var stopped, quiet int
	var refused []string
	for _, workspace := range held {
		went, err := enginehost.Stop(workspace)
		switch {
		case err != nil:
			refused = append(refused, fmt.Sprintf("%s: %v", workspace, err))
		case went:
			stopped++
			fmt.Printf("stopped holding %s\n", workspace)
		default:
			quiet++
		}
	}
	if stopped == 0 && len(refused) == 0 {
		fmt.Printf("nothing is holding any of the %s here\n", placesWord(quiet))
		return nil
	}
	if quiet > 0 {
		fmt.Printf("%s had nothing holding them\n", placesWord(quiet))
	}
	if len(refused) > 0 {
		return fmt.Errorf("could not stop %s:\n  %s", placesWord(len(refused)), strings.Join(refused, "\n  "))
	}
	fmt.Println("the next connection in any of them starts fresh from this build")
	return nil
}

// placesWord counts workspaces the way a sentence does, because "1 workspaces"
// is the kind of line that makes a person doubt the number beside it.
func placesWord(n int) string {
	if n == 1 {
		return "1 workspace"
	}
	return fmt.Sprintf("%d workspaces", n)
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
		}, // usage writer owner: see CloseUsage after the host returns
		// WHICH CONVERSATION A HELLO WANTS is the session file it named, and
		// naming none is this workspace's latest-or-new — the same meaning
		// --session has everywhere else. So two surfaces that both say nothing
		// are asking for the same conversation, which is the whole of "sit down
		// somewhere else and be in it".
		Key: func(hello remote.Hello) string {
			return engineHelloKey(hello, workspace, sessionFlag)
		},
	})
	// THE HOST IS GOING AWAY: stop and join the ledger writer a remote turn
	// started, the same door [v3Process.closeAll] uses. CloseUsage drains the
	// queue before joining, so the last row is on disk before this returns.
	// (The refusal path above never booted a conversation, so its registry is
	// empty and this close is a no-op there. It is one owner door, not two.)
	session.CloseUsage()
	closeEngineProcess()
	if errors.Is(err, enginehost.ErrHostRunning) {
		return nil
	}
	return err
}

// engineHelloKey is which conversation a hello is asking for, spelled as a
// TRANSCRIPT PATH and never as the empty string.
//
// A HELLO THAT NAMES NO SESSION IS RESOLVED HERE, THE SAME WAY THE BOOT WOULD
// RESOLVE IT, and that is the whole of what this function is for. It used to
// answer "" for such a hello and the host filed the conversation under that
// empty name — which worked exactly as long as the "" slot held the workspace's
// latest. The moment that conversation ended (it moved to another window, /new
// left it behind, it was closed) while the host went on holding a DIFFERENT one,
// the next plain launch found nothing under "", booted, and [bootEngine]
// resolved the very journal this host already holds the flock on. The host then
// refused its own conversation with "this conversation is open in another
// window", about itself.
//
// So the key is the answer [v3LatestTranscript] gives — one shared reading of
// the resume order, taken without touching the disk — and the host's lookup
// finds the conversation it is already holding before it boots anything.
//
// A NAMED SESSION IS SPELLED THE WAY THE BOOT WILL SPELL IT, through the same
// [engineSessionPath] every other door reads a --session with, so two surfaces
// that named one file two ways ("~/x", "/home/you/x") are asking for one
// conversation rather than two.
func engineHelloKey(hello remote.Hello, workspaceFlag, sessionFlag string) string {
	if named := firstEngineWord(hello.Session, sessionFlag); named != "" {
		path, err := engineSessionPath(named)
		if err != nil {
			return named
		}
		return path
	}
	// The hello's own workspace wins over the flag exactly as it does in
	// [bootEngine]: the two must resolve the same directory or they would be
	// answering about two different projects.
	workspace, err := engineWorkspace(firstEngineWord(hello.Workspace, workspaceFlag))
	if err != nil {
		return ""
	}
	return v3LatestTranscript(workspace)
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
	// EVERY HELLO GETS THE PROFILE AS IT STANDS NOW. The daemon outlives the
	// surface that may have connected a model service, so its process snapshot
	// is not evidence that the rows on disk have stood still.
	proc.refreshModelSources()
	// THE MODEL IS BUILT INTO THE SESSION AND NOT SET ON IT A MOMENT LATER,
	// which is the whole of what carrying it in the hello bought. The surface
	// used to open the conversation and then switch it (chatv3_host.go's old
	// applyHostChoices), and every turn a person could type rode the model they
	// asked for — but the session file's first line named the model the session
	// was BORN on, which was the wrong one. Nobody on the screen could see the
	// difference. The journal could, and the journal is the record.
	// The hello's launch shape lands the same way, and for the same reason: the
	// approval floor, the compaction posture and the one-model settlement are
	// built INTO the session rather than switched on after it has opened
	// ([engineLaunchOptions] holds the mapping).
	launchOptions := engineLaunchOptions(hello, workspace, sessionFlag)
	launch, err := openV3Launch(proc, launchOptions)
	if err != nil {
		return nil, err
	}
	// There IS a surface answering the consent cards; it is simply on another
	// machine. This is the same line `codeaf chat` sets and for the same reason,
	// and it is the one fact the shared assembly cannot know for itself.
	cfg := launch.Config
	cfg.AskConsent = !hello.Headless

	// A HELLO THAT ASKED FOR A CONVERSATION OF ITS OWN GETS A SIBLING FOLDER,
	// through the very pair [remote.Engine.Fresh] below is written from
	// ([v3NextSession] then [v3PointAt]). The launch above resolved this
	// workspace's LATEST conversation, which is the right answer for every other
	// hello and the wrong one for this: a window opening a second chat beside the
	// one it already has must not be handed the one it already has
	// ([remote.Hello.New] holds the whole of why the two intentions are two
	// flags).
	if hello.New {
		place, err := v3NextSession(cfg.Place, workspace)
		if err != nil {
			return nil, err
		}
		if cfg, err = v3PointAt(cfg, place); err != nil {
			return nil, err
		}
	}

	// THE THREE ROAD-DEPENDENT CAPABILITIES ARE DECIDED IN ONE PLACE, and this
	// door no longer keeps its own answer to any of them (chatv3_lanes.go). Each
	// is on exactly when the wire carries its lane AND the answer that closes it,
	// which internal/remote states about itself ([remote.StandingLanes]) rather
	// than this file guessing.
	//
	// TODAY THAT IS: the designer and the intake cards ON — version 11 carries
	// their subscription and both answers — and the ADAPTIVE RUNNER STILL OFF,
	// because the run page needs three more doors and the run lane replays
	// nothing, so a fuel gate raised while nobody was attached would be lost.
	// internal/remote's lanes.go holds that list; nothing here restates it.
	//
	// RUNNING a harness or a saved program was never on this list: that rides
	// Harnesses and RunHarness, properties of the machine that the shared
	// assembly fills for every door.
	cfg, open := v3Shape(cfg, v3LanesOverWire())

	// AND THE AMBIENT SIDE IS ON, which is the one capability on this list that
	// a connection does not take away. It arrives already filled, from the
	// shared assembly every v3 door goes through (chatv3.go's [openV3Launch]
	// sets Config.Standing and starts this process ticking), and it is left
	// alone here rather than rebuilt — one source of truth about where the store
	// lives and what a pass may do.
	//
	// IT IS SAFE BECAUSE THE ENGINE IS THE MACHINE. Everything the two
	// capabilities above lack is present here: the store is a directory under
	// THIS machine's CODEAF_HOME ([v3StandingRoot]), a firing runs under THIS
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

	// The builder came out of the shape above with the adaptive runner already
	// decided: "runs are on" and "build it through [v3OpenSession]" are one fact
	// (chatv3_lanes.go says why they cannot be two).
	agent, cfg, notice, err := openV3Agent(cfg, workspace, open)
	if err != nil {
		return nil, err
	}
	// AND THIS PROCESS HOLDS THE CONVERSATION IT JUST BUILT, the same way both
	// local doors hold theirs (chatv3.go's boot conversation and
	// chatv3_process.go's /new). It is what makes a service connected while
	// this conversation is open reach this conversation: the broadcast that
	// carries a freshly resolved profile walks the tracked agents
	// ([v3Process.setModelSources]), and a conversation missing from that list
	// keeps the account set it was BORN with. A model id naming a service the
	// agent has never heard of then falls through to the default one with its
	// prefix still on it, which is a 400 from a router that has no such model.
	// The pair is Closed below, and the pair is not optional — an engine
	// process outlives every conversation in it.
	proc.track(agent)
	// The boot override for how hard this session's model is asked to think,
	// landed the same way every local door lands it (chatv3.go's SetReasoning)
	// and on the same model — the one this session opened on. A level the
	// person did not name leaves the session on whatever the profile says,
	// which is what an empty string already means everywhere else.
	if level := strings.TrimSpace(hello.Level); level != "" {
		agent.SetReasoning(level)
	}
	transcript, resumed := launch.SessionFile, launch.Resumed
	if notice != "" || hello.New {
		// The session file moved under us, so the welcome has to name the new
		// one — everything the surface prints about this conversation comes off
		// that frame. A minted conversation is the same fact said on purpose: it
		// was never the launch's file and it was never resumed.
		transcript, resumed = cfg.SessionFile, false
	}

	proc.warmModels("engine/models", agent, launch.Model)

	return &remote.Engine{
		Headless: hello.Headless,
		Agent:    agent,
		// A model picked through a linked-local surface arrives on the existing
		// model-set call. Re-read the engine's profile immediately before it is
		// applied, so that model's address and key are live for the next turn.
		RefreshModelSources: proc.refreshModelSources,
		// And the pair of the tracking above: a conversation this engine closed
		// is one this process stops holding. Every road out reaches it — a
		// person's own goodbye, a torn pipe, an idle retirement, and the swap
		// /new and /resume make — so the list is the conversations that are
		// actually open rather than every conversation the daemon has ever
		// served. The type assertion is the seam's own shape: the wire holds a
		// remote.WrappedAgent and this process tracks what it built.
		Closed: func(closed remote.WrappedAgent) {
			if built, ok := closed.(*session.Agent); ok {
				proc.forget(built)
			}
		},
		// A linked-local surface writes a banked approval into this profile and
		// carries ConsentRule on the answer already crossing the wire. Rebuild
		// this conversation's gate at that door, using this conversation's own
		// workspace and launch posture, before the waiting call is released.
		RefreshApprovals: func() {
			refreshV3Policy(agent, workspace, proc.ProfileDir, launchOptions.Yolo)
		},
		ProfileDir:        proc.ProfileDir,
		UnreadProfileKeys: append([]string(nil), proc.UnreadProfileKeys...),
		Workspace:         workspace,
		SessionFile:       transcript,
		Resumed:           resumed,
		Note:              notice,
		// The shape this conversation ended up with, for the surface to compare
		// against what it asked for. It is the shape that was APPLIED, so a
		// hello that joined a conversation somebody else opened reads the other
		// person's shape here and can say so.
		Launch: hello.Launch,
		// Where a picture arriving on the wire lands: the engine's own session
		// folder, the same answer the local launch assembly gives its session.
		Place: cfg.Place,
		// The far half of a remote YOLO badge: this machine's own tool-approval
		// row, read the same way the local surface reads its own
		// (internal/tui3's readApproval). Empty when there is no profile
		// directory to read, which the welcome's omitempty and the badge's
		// emptiness law both already handle.
		ApprovalMode: config.ToolApprovalModeAt(launch.Settings.ProfileDir),
		// The countdown is an engine fact for the same reason: the far surface's
		// profile is a different machine's, and zero is a meaningful off posture.
		BashBackgroundAfterSeconds: cfg.BashBackgroundAfterSeconds,
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
			// THROUGH THE SAME BUILDER THE BOOT CONVERSATION WAS OPENED WITH, so
			// a conversation started by /new over a connection is not a lesser
			// one than the conversation it replaced: the adaptive runner is
			// bound to the agent that is actually open, which is the whole of
			// chatv3_orchestrate.go's late binding.
			replacement, err := open(fresh)
			if err != nil {
				return nil, "", err
			}
			// A CONVERSATION OPENED OVER THE WIRE IS TRACKED LIKE THE BOOT ONE.
			// Without this the defect above returns for anybody who reaches a
			// new conversation through /new rather than by relaunching, which
			// is the road a person takes precisely when they have just
			// connected something.
			proc.track(replacement)
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
			replacement, _, _, err := openV3Agent(earlier, workspace, open)
			if err != nil {
				// Returned unwrapped, the way the local picker returns it: a
				// locked file's error names the file, and the surface prints
				// exactly that.
				return nil, false, err
			}
			// And the picker's road, for the reason /new's is tracked.
			proc.track(replacement)
			return replacement, statErr == nil, nil
		},
		// The engine machine's ambient side, as a remote surface reads it, off
		// the SAME store this session proposes into. What a surface does with
		// them is the surface's business and is stated where it wires them
		// (chatv3_host.go's [hostStanding]: over --host the live reader is the
		// status line and home's far project bands). They are closures on the store rather
		// than the store itself for [tui3.StandingSeam]'s own reason — the door
		// owns where it lives and how it is opened — and they are absent
		// entirely when the ambient side could not be built, which the surface
		// reads as nothing to show rather than as an empty list.
		StandingItems: engineStandingItems(cfg.Standing),
		StandingSave:  engineStandingSave(cfg.Standing),
		StandingWatch: engineStandingWatch(cfg.Standing),
		// ── THE PLACES, AS THIS MACHINE HOLDS THEM ──────────────────────
		//
		// The world under THIS machine's state root, and the root it was walked
		// under. A surface over --host draws its seven places out of these two
		// facts, and before they existed it drew them out of the LAPTOP's copy
		// of the same directory — so the tasks place listed eight pieces of work
		// and $22.54 that had happened on a machine nobody in the conversation
		// had mentioned (internal/tui3's host.go).
		//
		// IT IS [session.ReadWorld] AND [session.PlacesRoot], WHICH IS WHAT THE
		// LOCAL SURFACE CALLS. The far machine's disk is under the same layout
		// decision as this one's, and a world assembled differently over a
		// connection would be a second law about one layout — the same argument
		// Recent's own comment makes two lines down.
		World: func() session.World {
			world := session.ReadWorld(session.PlacesRoot())
			world.Artifacts = session.ReadArtifacts(artifactsIndexPath())
			return world
		},
		Ledger: func(since time.Time) remote.LedgerReading {
			lines, _ := session.ReadUsage(session.UsageLedgerPath(), since)
			all, _ := session.ReadUsage(session.UsageLedgerPath(), time.Time{})
			held := false
			for _, line := range all {
				if line.USD > 0 {
					held = true
					break
				}
			}
			return remote.LedgerReading{Lines: lines, Held: held}
		},
		Search: func(terms string, limit int) ([]store.ConversationHit, error) {
			if proc.Memory == nil {
				return nil, errors.New("memory is off")
			}
			return proc.Memory.SearchConversations(terms, limit)
		},
		Memory:     v3MemorySeam(proc.Memory),
		Archive:    session.SetArchived,
		PlacesRoot: session.PlacesRoot(),
		// AND ONE ROW OF THAT RECORD, READ DEEPER THAN THE WALK READS IT. The
		// card behind a task row draws the last thing that piece of work said,
		// which is in the node's own journal and not in the index — and a surface
		// over --host has no way to open a journal on this disk. It reads it here
		// instead, under this machine's own places root, which is the boundary
		// [session.ReadTaskRecordUnder] applies rather than trusting the URI the
		// other end handed back.
		TaskRecord: func(uri string, tail int) (session.TaskRecord, error) {
			return session.ReadTaskRecordUnder(session.RecordRoots(), uri, tail)
		},
		Recent: func() []session.Summary {
			// Both shapes, exactly as the local list reads them
			// ([v3RecentSessions]): the far machine's disk is under the same
			// decision as this one's, and a list that answered differently
			// over a connection would be a second law about one layout.
			return session.RecentSessions(launch.Bucket, v3RecentSessionSlots)
		},
	}, nil
}

// engineLaunchOptions is the hello, as the shared assembly takes it.
//
// A NIL SHAPE IS THE ENGINE'S DEFAULTS, which is what every remote surface
// sends: --yolo and its neighbours are settings of the machine the session runs
// on, and cmd/codeaf refuses them over --host and --at by name. The local dial
// is the one caller that fills it (chatv3_local.go).
func engineLaunchOptions(hello remote.Hello, workspace, sessionFlag string) v3Options {
	opts := v3Options{
		Workspace:   workspace,
		Interactive: !hello.Headless,
		Model:       strings.TrimSpace(hello.Model),
		Session:     firstEngineWord(hello.Session, sessionFlag),
	}
	// A HELLO MINTING ITS OWN CONVERSATION NAMES NO TRANSCRIPT, and the host's
	// --session flag is not an answer for it either: that flag says which
	// conversation this daemon opens for a hello that did not choose, and this
	// hello chose "another one". The launch resolves the workspace's latest
	// anyway and [bootEngine] points the config at a sibling of it.
	if hello.New {
		opts.Session = ""
	}
	if shape := hello.Launch; shape != nil {
		opts.Yolo = shape.Yolo
		opts.NoCompact = shape.NoCompact
		opts.OneModel = shape.OneModel
		opts.Budget = chatBudget(shape.MaxHours, shape.MaxCost)
		opts.Interactive = shape.Interactive && !hello.Headless
	}
	return opts
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

// closeEngineProcess closes the once-per-process resources after the serving
// road has returned. In particular, closeAll cancels and joins model discovery
// before the engine process can exit or its profile can be reused.
func closeEngineProcess() {
	if engineProcess.proc != nil {
		engineProcess.proc.closeAll()
	}
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

func engineStandingWatch(seam *session.Standing) func() (standing.WatchStatus, bool) {
	if seam == nil || seam.Watch == nil {
		return nil
	}
	return func() (standing.WatchStatus, bool) {
		status, err := seam.Watch.Status()
		return status, err == nil
	}
}

// engineWorkspace resolves the directory the surface asked for.
//
// EMPTY IS THE HOME DIRECTORY and a relative path is relative to it — NOT to the
// process's own directory. That is not a convenience, it is what the person
// typed: `codeaf chat --host devbox:work/api` is read by whoever is holding the
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
