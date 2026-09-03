package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/enginehost"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/history"
	"github.com/Agent-Field/aforge-v2/internal/home"
	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
	"github.com/Agent-Field/aforge-v2/internal/standing"
	"github.com/Agent-Field/aforge-v2/internal/store"
	"github.com/Agent-Field/aforge-v2/internal/tui3"
)

// ── `aforge chat --host devbox` ─────────────────────────────────────────────
//
// The surface runs here and the session runs there. This file is the door: it
// parses the destination, starts `ssh <dest> aforge engine`, completes the
// handshake, and hands the connection to the same surface a local launch opens.
//
// THE PROMPT LAW, and it is the reason this whole path is shaped the way it is:
// EVERYTHING THAT MIGHT ASK THE PERSON A QUESTION HAPPENS BEFORE THE SURFACE
// TAKES THE SCREEN. ssh asks for a passphrase; ssh asks whether an unknown host
// key is really theirs; the engine may answer that it speaks a different version
// of the protocol. All three are plain text on a plain terminal, because they
// arrive while stderr is still the terminal's and tui3.Run has not been called.
// A TUI that came up first would either eat those questions or draw a frame over
// them, and a person would be looking at a hung screen with an invisible
// password prompt behind it.
//
// AND IT IS THE PERSON'S OWN SSH. No library, no key handling, no config file of
// ours: `ssh` is on their path, reads their ~/.ssh/config, knows their aliases
// and their agent. `--host devbox` works because `ssh devbox` already worked.
//
// WHAT THIS DOOR DOES NOT RESOLVE is everything the local door resolves and the
// far machine owns: the API key, the model catalog's credentials, the tool gate,
// the spend rail, the harness registry, the session files. Reading this
// machine's would be assembling a launch that never happens. What stays local is
// what belongs to the SURFACE — the input history, the unsent draft, the model
// picker's cached list — and that is stated once here and once in host.go.

// hostLaunch is one `--host` launch as the flag parser saw it.
type hostLaunch struct {
	// target is the flag as typed: host, user@host, host:path or host:/abs.
	target string
	// session is --session, passed through as the engine's own --session means it.
	session string
	// model and level are --model and --reasoning, carried in the hello itself
	// ([remote.Hello]'s Model and Level) so the ENGINE opens the session on them
	// rather than being switched a millisecond later — which is the difference
	// between a journal whose first line names the model the person asked for
	// and one that names whatever the machine happened to default to.
	model string
	level string
	// once is --once: one message, printed, no terminal ownership.
	once string
	// pick is `aforge resume`: the same surface, opened on the session picker.
	pick bool
	// noCompact and yolo are refused rather than ignored — see [hostLaunch.check].
	noCompact bool
	yolo      bool
	// budget is --max-hours / --max-cost, and it is refused for yolo's reason
	// and one more: what it bounds is a goal owner that lives in the session
	// (internal/session's principal.go), and the session is on the far machine.
	// A ceiling accepted here would bound nothing at all.
	budget bool
}

// check refuses the flags this door cannot honour.
//
// A FLAG THAT COULD NOT TRAVEL IS A REFUSAL AND NEVER A SHRUG. --no-compact and
// --yolo are properties of the SESSION, the session is built on the far machine
// by `aforge engine`, and the wire's hello carries neither. Accepting them and
// doing nothing would be the worst outcome available: a person types --yolo,
// watches the gate ask about every tool, and has no way to tell whether the flag
// or the gate is broken. So the door says which machine the setting lives on.
func (l hostLaunch) check() error {
	var named []string
	if l.noCompact {
		named = append(named, "--no-compact")
	}
	if l.yolo {
		named = append(named, "--yolo")
	}
	if l.budget {
		named = append(named, "--max-hours/--max-cost")
	}
	if len(named) == 0 {
		return nil
	}
	dest, _, _ := parseHostTarget(l.target)
	return fmt.Errorf("%s cannot travel over --host: the session is built on %s, so set it there — `ssh %s aforge chat %s` — or open the settings panel on that machine",
		strings.Join(named, " and "), dest, dest, strings.Join(named, " "))
}

// parseHostTarget splits the flag scp-style: everything before the FIRST colon
// is the ssh destination, everything after it is the workspace.
//
// THE WORKSPACE IS NOT RESOLVED HERE, and that is the whole point of "as typed".
// `--host devbox:code/app` means the directory `code/app` on devbox, relative to
// whatever devbox's home is — a directory this machine has never seen and must
// not have an opinion about. Making it absolute here would resolve it against
// THIS machine's cwd and send the far end a path out of somebody else's
// filesystem. The engine resolves it, and the welcome says what it resolved to.
//
// No colon is no workspace, which the engine reads as its own home. A trailing
// colon is the same thing said out loud.
func parseHostTarget(raw string) (dest, workspace string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("--host needs a machine: --host devbox, --host me@devbox, or --host devbox:code/app")
	}
	at := strings.Index(raw, ":")
	if at < 0 {
		return raw, "", nil
	}
	dest = strings.TrimSpace(raw[:at])
	workspace = strings.TrimSpace(raw[at+1:])
	if dest == "" {
		return "", "", fmt.Errorf("--host %q has no machine in front of the colon", raw)
	}
	return dest, workspace, nil
}

// engineLink is a live ssh process and the client speaking to it.
//
// THE PROCESS IS A SLOT AND NOT A CONSTANT, which is the whole of what roaming
// changed here: a dropped link is redialled by [remote.Roam], every redial is a
// FRESH ssh child, and this holds whichever one is current so the door can still
// wait on its exit code and read what it said on stderr.
type engineLink struct {
	client *remote.Client
	// dest and workspace are what every spawn needs, kept because the spawning
	// now happens again on a link that died and not only once at the door.
	dest      string
	workspace string

	mu sync.Mutex
	// process is the ssh child currently carrying the connection. Closing the
	// client's pipe is what ends it; this is held so the door can wait for its
	// exit code when the handshake failed and the exit code is the only witness
	// to why.
	process *exec.Cmd
	// stderr is the tail of what ssh and the far shell said, kept so a failed
	// handshake can be diagnosed in the person's own words rather than in a
	// pipe error. It is a TEE — everything in it was also printed as it arrived.
	stderr *tailWriter
	// reaped says this process has already been waited on, and err is what that
	// wait answered.
	reaped bool
	err    error
}

// spawn starts one `ssh <dest> aforge engine` and hands back its pipes. It is
// [remote.Dialer]: the door owns processes, the wire owns frames, and this is
// the one function the redial loop reaches back through when a link dies.
func (l *engineLink) spawn() (io.ReadWriteCloser, error) {
	// The remote command, as the far machine's login shell will read it. The
	// workspace is quoted because a path with a space in it is a path, and an
	// unquoted one would arrive at `aforge engine` as two arguments.
	remoteCommand := "aforge engine"
	if l.workspace != "" {
		remoteCommand += " --workspace " + shellQuote(l.workspace)
	}
	// -T because there is nothing interactive on the far end: the engine reads
	// frames on stdin and writes them on stdout, and a pseudo-terminal in the
	// middle would turn a newline into a carriage return and a frame into
	// nonsense. ssh's OWN questions do not go through this — it asks them on
	// /dev/tty, which is still the person's terminal.
	process := exec.Command("ssh", sshTransportArgs(l.dest, remoteCommand)...)
	stdin, err := process.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := process.StdoutPipe()
	if err != nil {
		return nil, err
	}
	// STDERR IS THE PERSON'S, AND ALSO OURS. It is printed as it arrives — that
	// is how a passphrase prompt and a host-key question reach the person — and
	// the tail is kept so that a handshake failure can name the likely cause.
	tail := &tailWriter{}
	process.Stderr = io.MultiWriter(os.Stderr, tail)
	if err := process.Start(); err != nil {
		if strings.Contains(err.Error(), "executable file not found") {
			return nil, fmt.Errorf("this machine has no ssh on its path, and --host is ssh")
		}
		return nil, fmt.Errorf("could not start ssh: %w", err)
	}
	l.hold(process, tail)
	return pipePair{r: stdout, w: stdin}, nil
}

// sshTransportArgs keeps the carrier's latency policy in one place. -T remains
// first because a pseudo-terminal changes bytes; the other options keep a warm
// connection available for redials, notice a machine that stopped answering,
// and keep interactive frames out of bulk queues on networks that distinguish
// them. The values come through config's registry, so a network that needs a
// different policy has a supported override rather than a private environment
// variable hidden from the settings sheet.
func sshTransportArgs(dest, remoteCommand string) []string {
	settings := config.SSHTransportAt(os.Getenv("AFORGE_PROFILE_DIR"))
	args := []string{
		"-T",
		"-o", fmt.Sprintf("ServerAliveInterval=%d", settings.ServerAliveSeconds),
		"-o", fmt.Sprintf("ServerAliveCountMax=%d", settings.ServerAliveMisses),
		"-o", "IPQoS=" + settings.IPQoS,
	}
	if control := sshControlPath(); control != "" {
		args = append(args,
			"-o", "ControlMaster=auto",
			"-o", "ControlPath="+control,
			"-o", fmt.Sprintf("ControlPersist=%d", settings.ControlPersistSeconds),
		)
	}
	return append(args, dest, remoteCommand)
}

// sshControlPath is short by construction and lives under the state root. %C
// lets OpenSSH hash the resolved host, port and user rather than us guessing at
// identities hidden in ~/.ssh/config. A state root too long for a unix socket
// loses multiplexing only: --host must still work, just as enginehost falls
// back to its pipe when its own socket cannot be made.
func sshControlPath() string {
	dir := home.Join("v3", "ssh")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return ""
	}
	path := filepath.Join(dir, "ctl-%C")
	// OpenSSH expands %C to a 40-character SHA-1 digest before bind(2), so the
	// expanded path is the one that must fit the shared macOS/Linux ceiling.
	expanded := strings.Replace(path, "%C", strings.Repeat("0", 40), 1)
	if !enginehost.SocketPathFits(expanded) {
		return ""
	}
	return path
}

// hold takes the new child and lets go of the old one. THE PREVIOUS SSH IS
// REAPED IN THE BACKGROUND, because a redial happens after its pipe died and a
// child nobody waits on is a zombie for as long as this terminal is open — five
// minutes of retries against a machine that is switched off would leave a row of
// them.
func (l *engineLink) hold(process *exec.Cmd, tail *tailWriter) {
	previous, reaped := l.swap(process, tail)
	if previous != nil && !reaped {
		guard.Go("chatv3/host-reap", func() { _ = previous.Wait() })
	}
}

// swap installs the new child and answers the old one and whether it was
// already reaped. It is its own method so the mutex is held from a defer while
// the wait on the previous child stays outside the lock.
func (l *engineLink) swap(process *exec.Cmd, tail *tailWriter) (previous *exec.Cmd, reaped bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	previous, reaped = l.process, l.reaped
	l.process, l.stderr, l.reaped, l.err = process, tail, false, nil
	return previous, reaped
}

// dialEngine starts the engine on the far machine, completes the handshake, and
// leaves the connection able to redial itself.
func dialEngine(dest, workspace string, launch hostLaunch) (*engineLink, error) {
	link := &engineLink{dest: dest, workspace: workspace}
	client, err := remote.Roam(dest, remote.Hello{
		Workspace: workspace,
		Session:   launch.session,
		// --model and --reasoning ride the frame that BUILDS the session, so the
		// engine opens on them rather than being switched afterwards.
		Model: launch.model,
		Level: launch.level,
	}, remote.Roaming{Dial: link.spawn})
	if err != nil {
		return nil, link.diagnose(dest, err)
	}
	link.client = client
	return link, nil
}

// diagnose turns a failed handshake into the truest sentence available.
//
// IT SNIFFS AND IT DOES NOT GUESS. A shell that could not find the command says
// so on stderr and exits 127; that is a fact, and "aforge is not installed on
// devbox" is what it means. ssh's own failures exit 255 and have already printed
// their own reason, which is better than anything this function could invent, so
// they are not paraphrased. Everything else falls back to what the handshake
// itself said. The one thing this never does is offer a cause it cannot see.
func (l *engineLink) diagnose(dest string, cause error) error {
	_ = l.close()
	code := l.exitCode()
	said := l.said()
	switch {
	case code == 127 || mentionsMissingCommand(said):
		return fmt.Errorf("aforge is not installed on %s — install it there, or put it on the PATH that a non-login ssh command sees", dest)
	case code == 255:
		// ssh has already said why, in its own words, above this line.
		return fmt.Errorf("ssh could not open a session on %s", dest)
	default:
		return cause
	}
}

// mentionsMissingCommand reads the far shell's own wording. The three spellings
// are bash/zsh, sh/dash and busybox, which is every shell an engine is likely to
// be started from.
func mentionsMissingCommand(said string) bool {
	said = strings.ToLower(said)
	return strings.Contains(said, "command not found") ||
		strings.Contains(said, "not found") && strings.Contains(said, "aforge") ||
		strings.Contains(said, "no such file or directory") && strings.Contains(said, "aforge")
}

// close shuts the connection and reaps the process. Closing the client also
// ends the redialling, so no further ssh child is started after this.
func (l *engineLink) close() error {
	if l.client != nil {
		_ = l.client.Close()
	}
	return l.reap()
}

// reap waits on the current ssh child, once.
func (l *engineLink) reap() error {
	process, err := l.claimReap()
	if process == nil {
		return err
	}
	err = process.Wait()
	l.noteReaped(err)
	return err
}

// claimReap marks the current child as being waited on and hands it back, or
// answers nil and the error already recorded when there is nothing to wait on.
// It is its own method so the mutex is held from a defer while the wait itself
// stays outside the lock.
func (l *engineLink) claimReap() (*exec.Cmd, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.process == nil || l.reaped {
		return nil, l.err
	}
	l.reaped = true
	return l.process, nil
}

// noteReaped records what the wait answered. It is its own method so the mutex
// is held from a defer.
func (l *engineLink) noteReaped(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.err = err
}

// exitCode is ssh's exit status once it has been waited on, or -1.
func (l *engineLink) exitCode() int {
	_ = l.reap()
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.process == nil || l.process.ProcessState == nil {
		return -1
	}
	return l.process.ProcessState.ExitCode()
}

// said is the tail of what ssh and the far shell printed on the current link.
func (l *engineLink) said() string {
	tail := l.tail()
	if tail == nil {
		return ""
	}
	return tail.String()
}

// tail is the current link's stderr keeper. It is its own method so the mutex
// is held from a defer while the read of the keeper, which takes a lock of its
// own, stays outside this one.
func (l *engineLink) tail() *tailWriter {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stderr
}

// pipePair is the ssh child's two halves as one [io.ReadWriteCloser], which is
// what internal/remote dials on. Closing it closes the WRITE half, which is what
// tells the engine there is nothing more coming and lets it exit cleanly; the
// read half then ends by itself.
type pipePair struct {
	r io.ReadCloser
	w io.WriteCloser
}

func (p pipePair) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p pipePair) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p pipePair) Close() error {
	err := p.w.Close()
	_ = p.r.Close()
	return err
}

// tailWriter keeps the last few kilobytes of what was written through it, so a
// failure can be diagnosed from what the far end actually said. It is bounded
// because a remote command can print for ever and a diagnosis needs a sentence.
type tailWriter struct {
	mu   sync.Mutex
	seen bytes.Buffer
}

const tailKept = 8 << 10

func (t *tailWriter) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.seen.Write(b)
	if t.seen.Len() > tailKept {
		kept := t.seen.Bytes()
		kept = kept[t.seen.Len()-tailKept:]
		t.seen.Reset()
		t.seen.Write(kept)
	}
	return len(b), nil
}

func (t *tailWriter) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.seen.String()
}

// shellQuote wraps a path for the far machine's shell, single quotes and all.
func shellQuote(word string) string {
	return "'" + strings.ReplaceAll(word, "'", `'\''`) + "'"
}

// openChatV3Host is the launch.
func openChatV3Host(launch hostLaunch) error {
	if err := launch.check(); err != nil {
		return err
	}
	dest, workspace, err := parseHostTarget(launch.target)
	if err != nil {
		return err
	}
	if launch.pick && launch.once != "" {
		return fmt.Errorf(`aforge resume opens the session picker; for one headless message use: aforge chat --host %s --once "text"`, dest)
	}
	link, err := dialEngine(dest, workspace, launch)
	if err != nil {
		return err
	}
	defer func() { _ = link.close() }()

	client := link.client
	agent := client.Agent()
	welcome := client.Welcome()
	correctHostChoices(agent, launch, welcome)

	if launch.once != "" {
		return runHostOnce(agent, launch.once)
	}
	options := hostOptions(client, agent, dest, welcome, launch.pick)
	// THE SAME WAY THE SURFACE IS RUN AT EVERY OTHER DOOR (chatv3_surface.go):
	// the byte meter and the logger redirect are the terminal's business rather
	// than this connection's, and a door does not state them for itself.
	return runSurface(context.Background(), options)
}

// correctHostChoices makes the session agree with --model and --reasoning when
// the welcome says it does not.
//
// THE HELLO IS WHERE THESE BELONG AND THE HELLO IS WHERE THEY GO NOW. The engine
// opens the session on them ([remote.Hello]'s Model and Level), so the journal's
// first line names the model the person asked for rather than the one the far
// machine happens to default to. This is not that work done twice: it is the
// check that the ask LANDED. The welcome states the model the engine actually
// opened on, so a disagreement with --model is a fact the surface can see, and a
// surface that saw it and drew the engine's answer anyway would be showing a
// person a model they did not choose.
//
// The level cannot be read off the welcome — there is no field for it — so when
// one was asked for it is read back with the one round trip that can answer it,
// and set only when it differs.
func correctHostChoices(agent *remote.Agent, launch hostLaunch, welcome remote.Welcome) {
	model := welcome.Model
	if launch.model != "" && launch.model != model {
		agent.SetModel(launch.model)
		model = launch.model
	}
	// The boot override lands on the model this session starts on, which is the
	// only model it can be about — the same law the local door states.
	if launch.level != "" && model != "" && agent.ReasoningFor(model) != launch.level {
		agent.SetReasoningFor(model, launch.level)
	}
}

// hostOptions assembles the surface. Everything the far machine knows comes off
// the welcome; everything this machine keeps on the person's behalf is resolved
// here, exactly as the local door resolves it.
func hostOptions(client *remote.Client, agent *remote.Agent, dest string, welcome remote.Welcome, pick bool) tui3.Options {
	// THE PICKER'S LIST IS RESOLVED WITHOUT CREDENTIALS. The catalog is opened
	// with whatever this machine happens to have — usually nothing, because the
	// key lives on the engine's machine — and that is fine: a catalog with no key
	// answers nil, the picker falls back to ~/.aforge/v3/models.json and then to
	// its built-ins, and the list is a list of NAMES rather than a claim about
	// what this machine can reach. What the model actually costs and whether the
	// switch took is the session's answer, and the session is over there.
	//
	// AND A MISSING KEY IS NOT AN ERROR ON THIS PATH, which is the one place this
	// door differs from the local one in a way a person would notice. The local
	// door stops with "aforge chat needs a model to talk with" because it is about
	// to build a session; this one is not — the session, and the key that pays for
	// it, are on the other machine. Refusing to open a remote conversation because
	// THIS laptop has no OPENROUTER_API_KEY would be asking for a credential
	// nothing is going to spend.
	settings, err := config.Load()
	profileDir := settings.ProfileDir
	if err != nil {
		settings = config.Config{BaseURL: config.DefaultBaseURL}
		profileDir = os.Getenv("AFORGE_PROFILE_DIR")
	}
	models := catalog.LoadLazy(context.Background(), catalog.Options{
		BaseURL: settings.BaseURL, APIKey: settings.APIKey, Dir: profileDir,
	})
	seams := newHostSeams(client)
	// EVERY BACKGROUND READING IS ARMED THROUGH ONE SEAM: the connection, and the
	// notice line a duty that fell over speaks to once (chatv3_host_duty.go). A
	// door with no client behind it arms duties that never start, rather than
	// goroutines that die on the wire.
	news := &hostNews{}
	seams.Notice = news.join(seams.Notice)
	far := hostFar{client: client, tell: news.say}
	stands := newHostStanding(far)
	// THE PLACES FOLLOW THE SESSION'S MACHINE. The world behind home, tasks,
	// standing, spend and search is asked of the ENGINE and kept warm here, and
	// it is asked once now so the first frame after launch already has it
	// ([hostWorld] holds both laws).
	world := newHostWorld(far)
	world.prime()
	ledger := newHostLedger(far)
	ledger.prime()
	memory := newHostMemory(far)
	memory.prime()

	options := tui3.Options{
		Agent:     agent,
		Build:     welcome.Build,
		Host:      dest,
		Workspace: welcome.Workspace,
		// The engine's own journal path, shown with its machine in front of it
		// wherever the surface shows it (internal/tui3's host.go).
		SessionFile: welcome.SessionFile,
		Resumed:     welcome.Resumed,
		Notice:      hostEntryNotice(welcome),
		// The engine's own tool-approval posture, so the YOLO badge names the
		// machine that actually decides whether a tool runs unattended
		// (internal/tui3's app.approvalPosture).
		ApprovalMode: welcome.ApprovalMode,
		// The engine's own foreground-command clock, for the same reason. The
		// laptop's profile may carry a different value and cannot describe the
		// timer that is already armed around a far process.
		BashBackgroundAfterSeconds: welcome.BashBackgroundAfterSeconds,
		ContextWindow:              v3Window(models, welcome.Model),
		Models:                     func() []tui3.Model { return v3Models(models) },
		// /export writes on THIS machine (host.go's honesty table), so its row
		// goes in this machine's index — the same one the local launch spells.
		ArtifactsIndex: artifactsIndexPath(),
		ProfileDir:     profileDir,
		PickSession:    pick,
		// The conversations the ENGINE's disk holds, and the door back into one of
		// them. Both go over the wire; neither reads a session file here.
		RecentSessions: func() []tui3.Session { return hostSessions(client) },
		Resume: func(file string) (tui3.Agent, error) {
			if _, err := client.OpenSession(file); err != nil {
				return nil, err
			}
			// THE SAME AGENT, and that is not a shortcut. This handle is a door
			// onto whichever session the engine currently has open, and the engine
			// has just swapped which one that is. The surface closed the agent it
			// was holding on the way in, which over the wire flushed the far
			// journal and left the connection standing (internal/remote's
			// Agent.Close states that difference).
			return agent, nil
		},
		Fresh: func() (tui3.Agent, string, error) {
			next, err := client.NewSession()
			if err != nil {
				return nil, "", err
			}
			return agent, next.SessionFile, nil
		},
		// ── THE PLACES, AS THE ENGINE MACHINE HOLDS THEM ────────────────────
		//
		// The seven places are a listing of one machine's disk, and until this
		// pair existed the surface listed its OWN: over --host the tasks place
		// walked this laptop's `~/.aforge/v3` and drew eight rows and $22.54 of
		// work under a conversation on a server that had run none of it. The
		// world crosses the wire now (internal/remote's Places.World), and the
		// root it was walked under travels with it so that the conversation this
		// window is sitting in can be put back into a walk taken before it
		// existed (internal/tui3's [app.worldRoot]).
		World:     world.world,
		WorldRoot: welcome.PlacesRoot,
		// AND ONE ROW OF THAT RECORD, WHEN SOMEBODY OPENS ITS CARD. It is a call
		// and not a cache, unlike the world above: the world is asked on every
		// place's open and on the beat, and this is asked once, on a press, about
		// one of four hundred rows. The surface only ever makes it off its loop
		// (internal/tui3's [app.readTaskTail]), so the wire's own deadline is the
		// only clock it needs.
		TaskRecord: client.TaskRecord,
		TaskRoom:   client.Agent().TaskRoom,
		TaskIndex: func() ([]session.TaskIndexEntry, bool) {
			far, known := world.world()
			if !known {
				return nil, false
			}
			for _, row := range far.Sessions() {
				if filepath.Clean(row.Transcript) == filepath.Clean(welcome.SessionFile) {
					return append([]session.TaskIndexEntry(nil), row.Tasks.Rows...), true
				}
			}
			return nil, true
		},
		Ledger:  ledger.read,
		Memory:  memory,
		Search:  client,
		Archive: client.Archive,
		// THE AMBIENT SIDE, AS THE ENGINE MACHINE HOLDS IT. The items belong to
		// the machine that runs them, so both halves go over the wire and
		// neither reads a store on this laptop — the far end answers about the
		// far end's own disk, keyed by a workspace path that means something
		// there ([standing.Item.Workspace] is always the engine's own path,
		// which is exactly what welcome.Workspace is too).
		//
		// WHAT THIS LIGHTS UP HERE is both home's item band and the status line's
		// `keeping an eye on` segment. Home reads the far world, so its project
		// paths are paths this far store can answer, while the segment asks about this
		// window's workspace — which over --host is the engine's own path, so
		// the count is about the right machine ([hostStanding] has the longer
		// version, and internal/tui3's host.go states it in the honesty table).
		//
		// Items therefore answers from a cache and refreshes behind itself,
		// which is not an optimization but the seam's stated law, because that
		// segment is asked on the frame. Save travels synchronously: it is a
		// keystroke, it is rare, and somebody is waiting for its answer.
		Standing: tui3.StandingSeam{
			Items: stands.list,
			Save:  stands.save,
			Watch: client.StandingWatch,
			// Running stays nil; Watch reads the engine's own scheduler over the
			// wire and therefore never substitutes this laptop's timer.
			//
			// Running: nothing on the far machine's disk says "firing at this
			// instant" — a run is in flight inside whichever process holds the
			// tick lock — so there is no question to put on the wire and no
			// answer a frame could carry. Nil answers no for everything, and a
			// home where no row ever wears `●` is the truth (cmd/aforge's
			// [v3StandingSeam] declines it for the same reason on this machine's
			// own store).
			//
		},
		// ── WHAT THE CONNECTION ITSELF SAYS ─────────────────────────────────
		//
		// The four facts only a connection has: the sentence to draw while a
		// dropped link is being redialled, the measured round trip, the one-off
		// news a redial discovered, and the questions this conversation raised
		// while nobody was attached.
		// Each lands somewhere different on the screen and internal/tui3's
		// hostlink.go says where; what this door owes is the answer, and the
		// client answers all four.
		Link: tui3.LinkSeam{
			Note:           seams.Link,
			Ping:           seams.Ping,
			Notice:         seams.Notice,
			Held:           hostHeld(seams),
			Driving:        hostDriving(seams),
			DrivingChanged: seams.DrivingChanged,
			Take:           seams.Take,
			Follow:         hostFollow(seams),
		},
		// ── WHAT IS DELIBERATELY NOT WIRED ──────────────────────────────────
		//
		// Connections: the accounts panel signs in through a browser HERE and
		// stores the result HERE, while the session reads the store THERE. A
		// panel wired to this machine's manager would offer rows that landed in
		// the wrong place, so none is handed over and /connect says so in one
		// sentence (internal/tui3's host.go).
		//
		// Harnesses: the registry the engine matches turns against is on the
		// engine's machine. Listing this machine's under /harness would be
		// offering to run harnesses that are not there.
		//
		// SaveApproval and SaveBashApproval: the consent card's "always" writes a
		// row into a profile, and the gate that reads it is the far machine's. Nil
		// is the honest wiring — the card keeps the answer for the session, over
		// the wire, and its row says "allowed" rather than "saved", which is
		// exactly what happened (internal/tui3's consent.go).
		//
		// StandingRoot: the local errand and exchange folder, which is where a
		// question asked from home leaves its short transcript. It is a path on
		// THIS machine and the exchange it hosts is a session this surface would
		// have to open here, so it stays unset over a connection — the same
		// posture the errand pair takes.
		//
		// SaveModel: the same rule for the same reason. The model this session
		// opens on next time is resolved where the session is built, which is over
		// there; writing the choice into THIS machine's profile would change which
		// model a local conversation starts on because somebody switched models on
		// a remote one. The switch itself still takes — it goes over the wire like
		// everything else — it simply does not outlive the session.
		//
		// AND THE FILE DOOR IS NOT A SEAM HERE EITHER, which is worth saying
		// because it looks like an omission and is not. Remote files — the links
		// in a reply, the browse page /files opens, the fetch behind opening one
		// — ride the CLIENT under this agent: the surface asserts `Client()` on
		// whatever it was handed and gets ListDir, StatPaths and FetchFile off it
		// (internal/tui3's remotefiles.go, on attach.go's narrow-interface
		// pattern). So the door this file already passes is the whole wiring, a
		// local session satisfies none of it and gets none of it, and there is no
		// closure here that could go stale around a conversation that was swapped.
	}
	// The two things this surface keeps on the person's behalf, resolved the way
	// the local door resolves them and keyed by the REMOTE workspace — which is
	// right: what a person typed while working on devbox:code/app belongs to that
	// place, not to whatever directory this terminal happens to be sitting in.
	if dir, err := v3Dir(); err == nil {
		store := history.New(filepath.Join(dir, "history.jsonl"))
		options.History = store
		options.DraftFile = tui3.DraftFile(dir, dest+":"+welcome.Workspace)
	}
	return options
}

// ── what the engine says about the room ─────────────────────────────────────

// hostEntryNotice is the sentence the surface shows once on the way in: what the
// engine said about opening the session, and who else is already in it.
//
// WHO ELSE IS HERE IS NOT A DETAIL. A session with another surface attached is a
// conversation somebody else can type into, and a screen that kept that quiet
// would be the one place aforge hid something about the room. The count is the
// engine's ([remote.Welcome]'s Attached), because only the machine holding the
// session can know it. Zero says nothing at all, by the emptiness law.
func hostEntryNotice(welcome remote.Welcome) string {
	said := strings.TrimSpace(welcome.Note)
	attached := hostAttachedNote(welcome.Attached, welcome.Driver.Yours)
	switch {
	case said == "":
		return attached
	case attached == "":
		return said
	default:
		return said + " · " + attached
	}
}

// hostAttachedNote is who else is here, and — since the room got one keyboard —
// what arriving did to them.
//
// THE SECOND HALF IS OWED TO THE OTHER WINDOW'S PERSON. Opening this
// conversation here has just taken the keyboard off whatever window was holding
// it (internal/remote's driver.go), and the person who did it is the only one
// who can be told why the screen over there changed. `typing is here now` is the
// same fact the watcher's own line says from the other side, in the same words.
//
// A window that arrived WITHOUT the keyboard — a link coming back to a
// conversation somebody has walked to since — says only who is here, because it
// took nothing.
func hostAttachedNote(attached int, driving bool) string {
	who := ""
	switch {
	case attached <= 0:
		return ""
	case attached == 1:
		who = "another window is on this conversation"
	default:
		who = fmt.Sprintf("%d other windows are on this conversation", attached)
	}
	if driving {
		who += " — typing is here now"
	}
	return who
}

// hostSeams is what a remote connection can tell the surface, in the wire's own
// shapes. Each field is a closure over [remote.Client], and each is named here
// rather than passed inline so that the door has ONE list of what a connection
// knows about itself and the option assembly has one line per fact.
//
// ALL FOUR ARE WIRED NOW. Three were written before internal/tui3 had anywhere
// to put them and sat unwired for a wave, which is why this type reads as a list
// of facts rather than as an argument: the surface has grown
// [tui3.LinkSeam] and hostOptions hands all four straight into it.
type hostSeams struct {
	// Link is the quiet sentence about the connection right now — empty
	// whenever there is nothing to say, which is what a status-line segment
	// draws as nothing at all. It reads `reconnecting to devbox — trying for up
	// to 5 minutes` while a dropped link is being redialled.
	Link func() string
	// Ping measures one empty call and back. The surface owns its cadence and
	// rolling estimate; this door only hands over the wire's typed method.
	Ping func() (time.Duration, error)
	// Notice is one sentence to show once and then forget: the two things a
	// redial can discover — the engine did not keep the turn, and the engine
	// came back with a different conversation open.
	Notice func() string
	// Held is the questions this session raised while nobody was attached. The
	// surface replays each one's event through the door it already draws live
	// cards with.
	Held func() ([]remote.HeldQuestion, error)
	// Driving is who holds the keyboard on this conversation, DrivingChanged is
	// closed the next time that moves, and Take asks for it back. They are the
	// three halves of one fact — a conversation with more than one window on it
	// has exactly one that can type (internal/remote's driver.go) — and they are
	// listed here beside the others because they are the same kind of thing: what
	// a connection can tell the surface about itself.
	Driving        func() remote.Driver
	DrivingChanged func() <-chan struct{}
	Take           func() error
	// Follow is the turns started by another window on this conversation, so a
	// window that is not typing is still a window onto the work.
	Follow func() <-chan remote.Following
}

func newHostSeams(client *remote.Client) hostSeams {
	return hostSeams{
		Link: client.LinkNote, Ping: client.Ping, Notice: client.TakeNotice, Held: client.HeldQuestions,
		Driving: client.Driver, DrivingChanged: client.DriverChanged, Take: client.Take,
		Follow: client.Follow,
	}
}

// hostDriving is who holds the keyboard in the SURFACE's shape, for
// [hostHeld]'s reason: internal/tui3 does not import internal/remote, so the
// one translation there is happens here, at the door, where both halves are
// already in scope.
// hostFollow is the same translation for the turns this window did not start.
// The channel itself is remade, one element at a time, because the surface's
// [tui3.Following] is its own type for [hostHeld]'s reason.
func hostFollow(seams hostSeams) func() <-chan tui3.Following {
	if seams.Follow == nil {
		return nil
	}
	var once sync.Once
	var out chan tui3.Following
	return func() <-chan tui3.Following {
		once.Do(func() {
			out = make(chan tui3.Following)
			guard.Go("chatv3/host-follow", func() {
				// A recovered panic must end the follow the way the seam's own
				// end does, or the surface waits on a channel nobody will close
				// again.
				defer close(out)
				for turn := range seams.Follow() {
					out <- tui3.Following{Said: turn.Said, Events: turn.Events}
				}
			})
		})
		return out
	}
}

func hostDriving(seams hostSeams) func() tui3.Driving {
	if seams.Driving == nil {
		return nil
	}
	return func() tui3.Driving {
		note := seams.Driving()
		return tui3.Driving{Yours: note.Yours, Machine: note.Machine, Here: note.Here}
	}
}

// hostHeld is the waiting room in the SURFACE's shape.
//
// It exists because internal/tui3 does not import internal/remote and must not:
// the package that draws a screen has no business compiling against a protocol,
// which is why every other thing that crosses this door crosses as a closure or
// as one of internal/session's own types. So the one translation there is —
// unwrapping the event, which is the field JSON could not carry
// ([remote.EventWire]) — happens HERE, at the door, where both halves are
// already in scope.
//
// THE ERROR TRAVELS rather than becoming an empty list, on the terms
// [remote.Client.HeldQuestions] states: "nothing is waiting" and "the far end
// did not answer" are different facts, and a surface handed the second as the
// first would quietly tell a person there is nothing to answer.
func hostHeld(seams hostSeams) func() ([]tui3.HeldQuestion, error) {
	if seams.Held == nil {
		return nil
	}
	return func() ([]tui3.HeldQuestion, error) {
		held, err := seams.Held()
		if err != nil {
			return nil, err
		}
		out := make([]tui3.HeldQuestion, 0, len(held))
		for _, q := range held {
			// THE KIND IS CARRIED ACROSS UNREAD. Deciding here which kinds this
			// build can draw would put the skip rule in two places, and the
			// surface is where the cards are — see internal/tui3's
			// [app.replayHeld], which leaves an unrecognised one waiting.
			//
			// [remote.HeldQuestion.Stream] is dropped, and dropped rather than
			// carried unread: the surface has one conversation and one place a
			// card goes, so there is nothing for it to name (tui3's
			// [HeldQuestion] states the same thing from the other side).
			out = append(out, tui3.HeldQuestion{Kind: q.Kind, Event: q.Event.Unwire(), Since: q.Since})
		}
		return out, nil
	}
}

// ── the ambient side over a connection ──────────────────────────────────────

// hostStandingEvery is how stale a cached list of items is allowed to be before
// the next reading kicks a fresh one. It is deliberately LOOSER than the
// surface's own three-second beat (internal/tui3's homeEvery): every refresh
// here is a round trip down an ssh pipe rather than a directory read, and
// nothing standing changes faster than this — an item's cadence is measured in
// minutes ([standing.Interval] is five of them), so a list a few seconds old is
// a list that is right.
const hostStandingEvery = 5 * time.Second

// hostStanding is the engine machine's items, kept here so the surface can have
// them without waiting.
//
// WHO ACTUALLY READS THIS OVER A CONNECTION, because it is not the reader the
// seam's own doc comment leads with: /home does not open over --host at all
// (internal/tui3's homeRemoteWord), so home's item band and its `p` and `s` keys
// are unreachable here and homestanding.go's per-project standItems is never
// called. The live reader is THE STATUS LINE — [app.keepingCount] feeds the
// `keeping an eye on 2` segment, and it asks about the window's own workspace,
// which over --host is the ENGINE's path. That makes the count a true sentence
// about the right machine, and it is a real thing to have: a remote window says
// how many things are keeping an eye on the project it is sitting in.
//
// AND IT EXISTS FOR ONE LAW, which is [tui3.StandingSeam.Items]': it MUST NOT
// BLOCK. That segment is asked on EVERY FRAME and twice per frame while a turn
// runs. The surface already keeps its own three-second answer for it, so this is
// the second of two guards rather than the only one — and it is not redundant,
// because the one reading that does get through is on the draw path. Locally
// that reading is a directory of small documents. Over a connection it is a call
// with a ten-second deadline (internal/remote's callDeadline), so a surface that
// made one of those while drawing would be a terminal that stopped repainting
// for as long as the far machine took to answer — on a link that had just died,
// ten seconds per frame.
//
// So the reading and the fetching are pulled apart. THE READ ANSWERS FROM WHAT
// IS HELD, ALWAYS AND IMMEDIATELY; a list that has gone stale kicks ONE
// background fetch and still answers with what it had. The first read of a
// workspace answers nothing at all and starts the fetch, so the segment is
// absent for one beat and then true — the honest order, because a surface that
// guessed would have to guess wrong first.
//
// ONE FETCH AT A TIME PER WORKSPACE, never a pile: the surface is drawn many
// times a second and every one of those readings would otherwise start its own
// goroutine and its own frame on the wire, which is a queue of identical
// questions behind a link that is already slow.
type hostStanding struct {
	// ask and put are the two wire doors, held as CLOSURES rather than as the
	// client for [tui3.StandingSeam]'s own reason said one seam further along:
	// what this type does is a policy about staleness and blocking, and a test
	// of that policy should be able to hand it a slow answer without opening a
	// pipe.
	ask func(workspace string) ([]standing.Item, error)
	put func(item standing.Item) error

	// duty is the latch, the clock and the door, kept per workspace
	// (chatv3_host_duty.go).
	duty hostDuty

	mu sync.Mutex
	// items is the last list each workspace answered with.
	items map[string][]standing.Item
}

func newHostStanding(far hostFar) *hostStanding {
	h := &hostStanding{
		ask:   far.client.StandingItems,
		put:   far.client.SaveStanding,
		items: map[string][]standing.Item{},
	}
	far.arm(&h.duty, "reading the standing items")
	return h
}

// list is [tui3.StandingSeam.Items]: what is held, right now, with a refresh
// started behind it when what is held has aged.
func (h *hostStanding) list(workspace string) []standing.Item {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil
	}
	held := h.snapshot(workspace)
	if h.duty.due(workspace, hostStandingEvery) {
		h.duty.run("chatv3/host-standing", workspace, func() { h.fetch(workspace) })
	}
	return held
}

// snapshot is what is held for one workspace, under the lock held from a defer.
func (h *hostStanding) snapshot(workspace string) []standing.Item {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.items[workspace]
}

// fetch is the round trip, on a goroutine of its own.
//
// A FAILED CALL KEEPS THE LAST LIST rather than emptying the band. The two ways
// this fails are a link that has died and an engine with no ambient side at all;
// neither of them is the news "the things you set up are gone", and a band that
// blanked itself on a dropped connection would be the screen reporting a loss
// that did not happen. The clock is still stamped, so a link that is failing is
// asked again on the next beat and not on every frame.
func (h *hostStanding) fetch(workspace string) {
	items, err := h.ask(workspace)
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.items[workspace] = items
}

// save is [tui3.StandingSeam.Save], and it is ALLOWED TO BLOCK where the read is
// not: it is a keystroke on one row — pause, stop — so it happens once and a
// person is waiting for the answer to it. The error travels back unchanged
// because home prints it: a row redrawn as paused over a store that refused the
// write would be this screen lying about the other machine's disk.
//
// NOTHING CAN PRESS THOSE KEYS OVER --host TODAY, and that is worth saying out
// loud rather than leaving for somebody to discover: the only callers are home's
// `p` and `s`, and home does not open over a connection. It is wired anyway
// because the alternative is a seam that is half absent for a reason that is not
// its own — the door and the wire are correct and proved, and the day home opens
// on a remote session the keys work rather than saying the change cannot be made
// here. A nil would have been a second thing to undo on that day, and a claim
// about this store that is not true.
//
// A WRITE THAT LANDED IS PUT STRAIGHT INTO WHAT IS HELD, and that is what keeps
// the row from redrawing stale in the beat before the next fetch returns: the
// engine accepted this exact document, so the held list is corrected with it
// rather than left showing the version the key was pressed on. The entry is
// aged out at the same time, so the next reading also asks the store what it
// really thinks.
func (h *hostStanding) save(item standing.Item) error {
	if err := h.put(item); err != nil {
		return err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	workspace := strings.TrimSpace(item.Workspace)
	held := h.items[workspace]
	for i, existing := range held {
		if existing.ID == item.ID {
			held[i] = item
			break
		}
	}
	h.items[workspace] = held
	h.duty.age(workspace)
	return nil
}

// hostSessions is [tui3.Options.RecentSessions] over the wire.
//
// IT IS ASKED ON THE KEYSTROKE, like the local one, and it is a round trip
// rather than a directory walk — which is the same order of work: the engine
// reads its own session directory (internal/session's peek.go bounds the reads)
// and one frame comes back. The surface's rule is that this must not BLOCK, and
// a call on a healthy link is milliseconds; a call on a link that is not healthy
// gives up on internal/remote's own deadline and answers with an empty list,
// which is what the local door answers for an unreadable directory.
func hostSessions(client *remote.Client) []tui3.Session {
	found := client.Recent()
	rows := make([]tui3.Session, 0, len(found))
	for _, summary := range found {
		rows = append(rows, tui3.Session{
			Title:   summary.Title,
			Opening: summary.Opening,
			Last:    summary.Last,
			File:    summary.File,
			At:      summary.At,
		})
	}
	return rows
}

// runHostOnce is --once over a connection: one message, the reply on stdout,
// everything else on stderr. It is [runChatV3Once] with a remote agent, and it
// is deliberately the same shape — a probe that compares stdout with the
// sentence it asked for must not have to know which machine answered.
func runHostOnce(agent *remote.Agent, text string) error {
	events, err := agent.Submit(context.Background(), text)
	if err != nil {
		return err
	}
	wrote := false
	newline := func() {
		if wrote {
			fmt.Println()
			wrote = false
		}
	}
	var failure error
	for event := range events {
		switch event.Kind {
		case session.EventTextDelta:
			if event.Text == "" {
				continue
			}
			fmt.Print(event.Text)
			wrote = !strings.HasSuffix(event.Text, "\n")
		case session.EventToolBegin:
			newline()
			fmt.Fprintln(os.Stderr, "tool: "+tui3.ToolGloss(event.Tool, event.Hint))
		case session.EventToolFailed:
			newline()
			reason := event.Hint
			if reason == "" && event.Err != nil {
				reason = event.Err.Error()
			}
			fmt.Fprintln(os.Stderr, "tool: "+event.Tool+" failed: "+reason)
		case session.EventCompacted:
			newline()
			fmt.Fprintln(os.Stderr, "compacted: "+event.Hint)
		case session.EventError:
			failure = event.Err
			if failure == nil {
				failure = fmt.Errorf("session: the turn failed without a reason")
			}
		}
	}
	newline()
	_ = agent.Close()
	return failure
}

// ── the places over a connection ────────────────────────────────────────────

// hostWorldEvery is how stale a held world is allowed to be before the next
// reading kicks a fresh one.
//
// IT IS SHORTER THAN [hostStandingEvery] AND LONGER THAN NOTHING. The surface
// re-reads its places every three seconds (internal/tui3's homeEvery), and a
// staleness of two means every one of those beats finds the held world old
// enough to refresh — so what a person looks at is at most one beat behind the
// far machine, which is the same lag a local window has against the next
// terminal on its own disk. Standing can afford five because an item's cadence
// is measured in minutes; a conversation in the next window over there answers
// in seconds, and home exists to show that happening.
const hostWorldEvery = 2 * time.Second

// hostWorld is the ENGINE machine's world, kept here so the places can have it
// without waiting.
//
// It is [hostStanding] applied to the one reading five of the seven places are
// built from, and it keeps that type's two laws for that type's two reasons:
//
//   - THE READ ANSWERS FROM WHAT IS HELD, ALWAYS AND IMMEDIATELY. A place may
//     read on its open and on its beat, and over a connection both of those are
//     still moments a person is waiting through — a call down an ssh pipe has a
//     ten-second deadline (internal/remote's callDeadline), so an open that made
//     one would be a terminal that stopped answering keys for as long as the far
//     machine took, and on a link that had just died, for ten seconds.
//   - ONE FETCH AT A TIME, never a pile. Seven places on a three-second beat,
//     each asking the same question, is a queue of identical frames behind a link
//     that is already slow.
//
// AND IT SAYS WHETHER IT HAS AN ANSWER, which [hostStanding] does not have to.
// An empty list of standing items and no answer yet are the same thing on a
// screen — the emptiness law draws both as nothing. An empty WORLD is not: it is
// a machine with no projects on it, and `nothing here yet — say something and
// this fills up` drawn over a server full of work is the one wrong sentence this
// screen can say about somebody else's disk. So the seam answers a second value
// and the surface draws nothing at all until the far machine has spoken once
// (internal/tui3's [app.worldKnown]).
type hostWorld struct {
	// ask is the wire door, held as a closure for [hostStanding.ask]'s reason:
	// what this type does is a policy about staleness and blocking, and a test of
	// that policy should be able to hand it a slow answer without opening a pipe.
	ask func() (session.World, error)

	// duty is the latch, the clock and the door (chatv3_host_duty.go).
	duty hostDuty

	mu sync.Mutex
	// held is the last world the engine answered with, and known says it has
	// answered at least once.
	held  session.World
	known bool
}

func newHostWorld(far hostFar) *hostWorld {
	h := &hostWorld{ask: far.client.World}
	far.arm(&h.duty, "keeping the places current")
	return h
}

// world is [tui3.Options.World]: what is held, right now, with a refresh started
// behind it when what is held has aged.
func (h *hostWorld) world() (session.World, bool) {
	held, known := h.snapshot()
	if h.duty.due("", hostWorldEvery) {
		h.duty.run("chatv3/host-world", "", h.fetch)
	}
	return held, known
}

// snapshot is what is held, under the lock held from a defer.
func (h *hostWorld) snapshot() (session.World, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.held, h.known
}

// fetch is the round trip, on a goroutine of its own.
//
// A FAILED CALL KEEPS THE LAST WORLD rather than emptying every place at once.
// The two ways this fails are a link that has died and an engine too old to
// answer this method; neither of them is the news "everything you have worked on
// is gone", and seven rooms that blanked themselves on a dropped connection
// would be the screen reporting a loss that did not happen. The clock is stamped
// either way, so a link that is failing is asked again on the next beat rather
// than on every frame — and `known` is only ever turned on, so a world that
// arrived once goes on being drawn while the connection is being redialled.
func (h *hostWorld) fetch() {
	world, err := h.ask()
	if err != nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.held, h.known = world, true
}

// prime asks once, in the background, at the moment the door is built.
//
// THE FIRST FRAME IS THE ONE THAT MATTERS. Without this the first reading a
// place takes is the one that starts the fetch and answers nothing, so a person
// who pressed space-space quickly would meet an empty frame and then a full one.
// Launch is far longer than one round trip, so asking here means the answer is
// almost always already there — and when it is not, the surface draws nothing
// rather than a guess, which is why this is a convenience rather than a
// correctness fix.
func (h *hostWorld) prime() {
	h.duty.run("chatv3/host-world-prime", "", h.fetch)
}

// hostLedger keeps the far ledger off the surface goroutine. A wider window
// replaces the held floor; paging forward then reads the same held slice.
type hostLedger struct {
	ask         func(time.Time) (remote.LedgerReading, error)
	duty        hostDuty
	mu          sync.Mutex
	lines       []session.UsageLine
	held, known bool
	floor       time.Time
}

func newHostLedger(far hostFar) *hostLedger {
	h := &hostLedger{ask: far.client.Ledger}
	far.arm(&h.duty, "reading what has been spent")
	return h
}
func (h *hostLedger) prime() { h.start(time.Now().AddDate(0, 0, -14)) }
func (h *hostLedger) read(since time.Time) ([]session.UsageLine, bool, bool) {
	lines, held, known, need := h.snapshot(since)
	if need {
		h.start(since)
	}
	return lines, held, known
}

// snapshot is what is held and whether a wider window is wanted, under the lock
// held from a defer.
func (h *hostLedger) snapshot(since time.Time) (lines []session.UsageLine, held, known, need bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	lines = append([]session.UsageLine(nil), h.lines...)
	held, known = h.held, h.known
	need = !known || since.Before(h.floor)
	return lines, held, known, need
}
func (h *hostLedger) start(since time.Time) {
	h.duty.run("chatv3/host-ledger", "", func() {
		reading, err := h.ask(since)
		if err != nil {
			return
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		h.lines, h.held, h.known, h.floor = reading.Lines, reading.Held, true, since
	})
}

// hostMemory caches the two readings used by drawing. The deliberate write and
// provenance methods remain synchronous because they are reached only by an
// explicit key and their answer decides what the surface may say happened.
type hostMemory struct {
	client         *remote.Client
	duty           hostDuty
	mu             sync.Mutex
	shelves        store.MemoryShelves
	learned, letGo int
	known          bool
}

func newHostMemory(far hostFar) *hostMemory {
	h := &hostMemory{client: far.client}
	far.arm(&h.duty, "reading what is remembered")
	return h
}
func (h *hostMemory) prime() { h.refresh(500, time.Time{}) }
func (h *hostMemory) refresh(limit int, at time.Time) {
	h.duty.run("chatv3/host-memory", "", func() {
		s, err := h.client.Snapshot(limit)
		learned, letGo, changedErr := h.client.ChangedSince(at)
		if err != nil || changedErr != nil {
			return
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		h.shelves, h.learned, h.letGo, h.known = s, learned, letGo, true
	})
}
func (h *hostMemory) Snapshot(limit int) (store.MemoryShelves, error) {
	s, _, _, known := h.snapshot()
	if !known {
		h.refresh(limit, time.Time{})
	}
	return s, nil
}
func (h *hostMemory) ChangedSince(at time.Time) (int, int, error) {
	_, learned, letGo, _ := h.snapshot()
	h.refresh(500, at)
	return learned, letGo, nil
}

// snapshot is what is held, under the lock held from a defer.
func (h *hostMemory) snapshot() (shelves store.MemoryShelves, learned, letGo int, known bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.shelves, h.learned, h.letGo, h.known
}
func (h *hostMemory) ListMemories(scope string, limit int) ([]store.Memory, error) {
	return h.client.ListMemories(scope, limit)
}
func (h *hostMemory) UpdateMemory(id, title, text string, tags []string) error {
	err := h.client.UpdateMemory(id, title, text, tags)
	h.refresh(500, time.Time{})
	return err
}
func (h *hostMemory) ForgetMemory(id string) error {
	err := h.client.ForgetMemory(id)
	h.refresh(500, time.Time{})
	return err
}
func (h *hostMemory) RestoreMemory(id string) error {
	err := h.client.RestoreMemory(id)
	h.refresh(500, time.Time{})
	return err
}
func (h *hostMemory) MemoryProvenance(id string) (string, string, time.Time, error) {
	return h.client.MemoryProvenance(id)
}
