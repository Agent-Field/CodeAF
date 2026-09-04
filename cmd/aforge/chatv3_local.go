package main

// ── `aforge chat` on a conversation this machine is already holding ─────────
//
// This is the third dialer and the first one with no machine in it. The surface
// runs here, the session runs in THIS workspace's session host
// (internal/enginehost), and the pipe between them is a unix socket rather than
// an ssh child or a relay tunnel.
//
// WHY THERE IS A DOOR AT ALL. A conversation held by a host used to be
// unreachable from the terminal it was started in: `aforge chat` opened the
// session in-process, met the journal's flock, and quietly started a NEW
// conversation somewhere else. Nothing on the screen said the chat the person
// was looking for was still running a few inches away. The road to it already
// existed in every part but one — the host attaches or spawns under a flock
// ([enginehost.Attach]), the room has one keyboard that the newest window takes
// (internal/remote's driver.go), and the surface already knows how to be a
// watcher (internal/tui3's watching.go). What was missing was a local dial.
//
// A HOSTED CONVERSATION IS NO LONGER A LESSER ONE. It used to be built with the
// designer nilled, the intake cards off and the adaptive runner unwired, because
// the wire had no door for the standing lanes those three raise their cards on;
// it carries them now (internal/remote's standinglane.go), and both roads are
// shaped by the same statement (chatv3_lanes.go). What still decides whether
// this door is taken is [v3HostRoad] below.
//
// EVERYTHING BELOW THE DIAL IS THE ssh DOOR'S OWN CODE. The client, the
// welcome, the surface's options and the headless run are [openChatV3Host]'s,
// called with an empty machine name — which is the seam this build spells as
// "linked, local": [tui3.Options.Link] filled and [tui3.Options.Host] empty.

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/enginehost"
	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// localLaunch is one launch that goes through this machine's own session host.
//
// It carries no target: the machine is this one, and the workspace has already
// been resolved by the rule that decided to come here ([v3HostRoad]) rather
// than parsed out of a flag.
type localLaunch struct {
	// workspace is the directory whose host holds this conversation, absolute
	// and resolved — the same answer [openV3Launch] would have reached, so both
	// doors agree about which project this terminal is in.
	workspace string
	// session is --session, meaning here exactly what it means to the engine:
	// the transcript to open, and empty is this workspace's latest.
	session string
	// model and level are --model and --reasoning, carried in the hello so the
	// ENGINE opens the session on them — the same law the ssh door states.
	model string
	level string
	// once is --once: one message, printed, no terminal ownership.
	once string
	// pick is `aforge resume`: the same surface, opened on the session picker.
	pick bool
}

// localLink is the dialer for a conversation on this machine.
//
// IT IS A [remote.Dialer] AND NOTHING MORE, which is what makes the rest of the
// road free: internal/remote speaks its protocol over any io.ReadWriteCloser,
// and a unix socket is one. There is no process to hold and no stderr to read —
// the host is nobody's child, by construction ([enginehost.Spawn] detaches it) —
// so this type is a workspace and a method.
type localLink struct{ workspace string }

// dial hands back a connection to this workspace's host, starting one when
// nothing answers. It is called again by the redial loop, which is exactly what
// it is for: a host that retired under a surface is replaced by a fresh one on
// the next attempt rather than ending the window.
//
// THE STALE HOST IS ASKED ABOUT FIRST, on the same terms `aforge engine` asks
// (engine.go's [clearStaleEngineHost]): a host outlives the binary that started
// it, so a rebuilt aforge can meet an older one still holding the socket. It
// retires if it is holding nothing, and otherwise the person is told in words
// which command lets go of it — one sentence, already written, and not a splice
// onto a build that speaks a different protocol.
func (l *localLink) dial() (io.ReadWriteCloser, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if err := clearStaleEngineHost(l.workspace); err != nil {
		return nil, err
	}
	return enginehost.Attach(l.workspace, func() error {
		return enginehost.Spawn(self, "engine", "--daemon", "--workspace", l.workspace)
	})
}

// openChatV3Local is the launch.
func openChatV3Local(launch localLaunch) error {
	if launch.pick && launch.once != "" {
		return fmt.Errorf(`aforge resume opens the session picker; for one headless message use: aforge chat --once "text"`)
	}
	link := &localLink{workspace: launch.workspace}
	// THE MACHINE NAME IS EMPTY AND THAT IS THE WHOLE SIGNAL. internal/remote
	// already says "the engine" where it would say a machine's name (its
	// [Client.where]), and the surface reads an empty [tui3.Options.Host] as a
	// conversation with no far machine in it — no `via` segment, no machine in
	// front of a path, and `another window` where a connection would name a
	// host.
	client, err := remote.Roam("", remote.Hello{
		Workspace: launch.workspace,
		Session:   launch.session,
		Model:     launch.model,
		Level:     launch.level,
	}, remote.Roaming{Dial: link.dial})
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	agent := client.Agent()
	welcome := client.Welcome()
	correctHostChoices(agent, hostLaunch{model: launch.model, level: launch.level}, welcome)

	if launch.once != "" {
		return runHostOnce(agent, launch.once)
	}
	options := hostOptions(client, agent, "", welcome, launch.pick)
	// The draft this terminal has half-typed is keyed by the workspace and NOT
	// by a machine, because there is no machine: a person who takes the host
	// road today and the in-process door tomorrow is in the same place both
	// times, and a key with an empty machine glued to the front of it would
	// hide their own unsent words from them.
	if dir, err := v3Dir(); err == nil {
		options.DraftFile = tui3.DraftFile(dir, welcome.Workspace)
	}
	return runSurface(context.Background(), options)
}

// ── THE WHEN-TO-TAKE RULE ───────────────────────────────────────────────────

// v3HostChoice is everything the rule is decided on, gathered at the flag
// parser so the decision itself reads as one sentence and can be tested without
// a terminal.
type v3HostChoice struct {
	// noHost is --no-host: the escape hatch, and the reason it exists is that a
	// fallback nobody can ask for is a fallback nobody can use when the host is
	// the thing that is wrong. It means here exactly what it means on `aforge
	// engine` — serve this conversation in this process and never dial a socket.
	noHost bool
	// shaped says a flag that describes HOW THE SESSION IS BUILT was named:
	// --yolo, --no-compact, --one-model, --max-hours or --max-cost. None of
	// them can travel a wire — the ssh door refuses all of them by name
	// ([hostLaunch.check]) — and a launch that carried one down this road would
	// drop it in silence, which is the one outcome worse than a refusal. So the
	// flag keeps this launch in its own process, where the flag is real.
	shaped bool
}

// v3HostRoad answers whether `aforge chat` opens its conversation through this
// workspace's session host, and names the workspace either way.
//
// THE RULE IS ONE FACT: A HOST FOR THIS WORKSPACE ALREADY ANSWERS. Something is
// open in it — a `--host` or `--at` connection into this machine, or somebody's
// `aforge engine` — and a terminal that opened its own conversation instead
// would leave a live one unreachable from the machine it is running on. Joining
// it is the room internal/remote's driver.go already runs: one keyboard, the
// newest window holding it.
//
// A PLAIN LOCAL LAUNCH NEVER STARTS A HOST, and that is deliberate rather than
// unfinished. A host cannot open a journal an in-process window is holding —
// the flock is the flock — so spawning one to reach a locked conversation buys
// a slower version of the same refusal and a process that lingers for its idle
// span. What a person wants there is the conversation MOVED to this terminal,
// which is the hand-off in issue #71 and needs no host at all. Making the host
// the local default is issue #66, gated on the wire growing doors for the
// standing lanes (docs/design/multi-attach/PLAN.md).
//
// OTHERWISE THE IN-PROCESS DOOR RUNS EXACTLY AS IT ALWAYS HAS, and it is no
// longer the door with more in it: the two roads are shaped by one statement
// (chatv3_lanes.go), so joining a host costs a person no capability.
//
// A WORKSPACE THAT CANNOT BE RESOLVED IS NO ROAD. Everything here answers
// "in-process" when it cannot tell, because the in-process door is the floor
// and reaching a host is the feature.
func v3HostRoad(choice v3HostChoice) (string, bool) {
	workspace, _ := v3Workspace(v3LaunchDir(), "")
	if workspace == "" {
		return "", false
	}
	return workspace, v3TakeHostRoad(workspace, choice)
}

// v3TakeHostRoad is the rule itself, asked about one workspace. It is separate
// from the resolution above because where the person is standing is answered
// once per process ([v3LaunchDir] is a sync.Once, by design) and the rule is a
// question about any directory.
func v3TakeHostRoad(workspace string, choice v3HostChoice) bool {
	if strings.TrimSpace(workspace) == "" || choice.noHost || choice.shaped {
		return false
	}
	// A host that answers is asked nothing here. Which build it is, and what to
	// do about an older one, is [localLink.dial]'s question a moment later and
	// is asked once — this connection is spent on the question "is anybody
	// there" and closed. Nothing is ever STARTED from this line: [Attach] can
	// spawn, and it is only ever reached because a host already answered.
	conn, err := enginehost.Dial(workspace)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}
