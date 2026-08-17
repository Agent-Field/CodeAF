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

	"github.com/Agent-Field/aforge-v2/internal/catalog"
	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/history"
	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
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
	// model and level are --model and --reasoning. They are applied AFTER the
	// handshake rather than carried in it, because the wire's hello has no room
	// for them — see the STUB on [applyHostChoices].
	model string
	level string
	// once is --once: one message, printed, no terminal ownership.
	once string
	// pick is `aforge resume`: the same surface, opened on the session picker.
	pick bool
	// noCompact and yolo are refused rather than ignored — see [hostLaunch.check].
	noCompact bool
	yolo      bool
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
type engineLink struct {
	client *remote.Client
	// process is the ssh child. Closing the client's pipe is what ends it; this
	// is held so the door can wait for its exit code when the handshake failed
	// and the exit code is the only witness to why.
	process *exec.Cmd
	// stderr is the tail of what ssh and the far shell said, kept so a failed
	// handshake can be diagnosed in the person's own words rather than in a
	// pipe error. It is a TEE — everything in it was also printed as it arrived.
	stderr *tailWriter
	// done is closed when the process has been waited on, and wait guards it.
	wait sync.Once
	err  error
}

// dialEngine starts the engine on the far machine and completes the handshake.
func dialEngine(dest, workspace, sessionFile string) (*engineLink, error) {
	// The remote command, as the far machine's login shell will read it. The
	// workspace is quoted because a path with a space in it is a path, and an
	// unquoted one would arrive at `aforge engine` as two arguments.
	remoteCommand := "aforge engine"
	if workspace != "" {
		remoteCommand += " --workspace " + shellQuote(workspace)
	}
	// -T because there is nothing interactive on the far end: the engine reads
	// frames on stdin and writes them on stdout, and a pseudo-terminal in the
	// middle would turn a newline into a carriage return and a frame into
	// nonsense. ssh's OWN questions do not go through this — it asks them on
	// /dev/tty, which is still the person's terminal.
	process := exec.Command("ssh", "-T", dest, remoteCommand)
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
	link := &engineLink{process: process, stderr: tail}
	client, err := remote.Dial(pipePair{r: stdout, w: stdin}, dest, remote.Hello{
		Workspace: workspace,
		Session:   sessionFile,
	})
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
	said := l.stderr.String()
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

// close shuts the connection and reaps the process.
func (l *engineLink) close() error {
	if l.client != nil {
		_ = l.client.Close()
	}
	l.wait.Do(func() {
		if l.process != nil {
			l.err = l.process.Wait()
		}
	})
	return l.err
}

// exitCode is ssh's exit status once it has been waited on, or -1.
func (l *engineLink) exitCode() int {
	l.wait.Do(func() {
		if l.process != nil {
			l.err = l.process.Wait()
		}
	})
	if l.process == nil || l.process.ProcessState == nil {
		return -1
	}
	return l.process.ProcessState.ExitCode()
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
	link, err := dialEngine(dest, workspace, launch.session)
	if err != nil {
		return err
	}
	defer func() { _ = link.close() }()

	client := link.client
	agent := client.Agent()
	welcome := client.Welcome()
	applyHostChoices(agent, launch, welcome)

	if launch.once != "" {
		return runHostOnce(agent, launch.once)
	}
	options := hostOptions(client, agent, dest, welcome, launch.pick)
	// The byte meter, off unless a developer named a log file (wire.go). It
	// measures what this surface DRAWS and is therefore as local as the terminal
	// is — the same launch the local door makes.
	meter, closeMeter := v3Wire()
	defer closeMeter()
	options.Output = meter
	return tui3.Run(context.Background(), options)
}

// applyHostChoices lands --model and --reasoning on the session that just
// opened.
//
// STUB: they belong in the hello, and [remote.Hello] has no room for them
// (internal/remote's wire.go, which this lane does not own). Setting them
// immediately after the handshake is the closest faithful thing: the session has
// existed for a millisecond, nothing has been asked of it, and the first turn
// rides the model the person named. The difference a hello field would make is
// that the ENGINE would open the session on that model rather than switch it, so
// the session file's first line would name it — a fact nobody on this screen can
// see, but the right one for the journal.
func applyHostChoices(agent *remote.Agent, launch hostLaunch, welcome remote.Welcome) {
	model := welcome.Model
	if launch.model != "" {
		agent.SetModel(launch.model)
		model = launch.model
	}
	// The boot override lands on the model this session starts on, which is the
	// only model it can be about — the same law the local door states.
	if launch.level != "" && model != "" {
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

	options := tui3.Options{
		Agent:     agent,
		Host:      dest,
		Workspace: welcome.Workspace,
		// The engine's own journal path, shown with its machine in front of it
		// wherever the surface shows it (internal/tui3's host.go).
		SessionFile: welcome.SessionFile,
		Resumed:     welcome.Resumed,
		Notice:      welcome.Note,
		// The engine's own tool-approval posture, so the YOLO badge names the
		// machine that actually decides whether a tool runs unattended
		// (internal/tui3's app.approvalPosture).
		ApprovalMode:  welcome.ApprovalMode,
		ContextWindow: v3Window(models, welcome.Model),
		Models:        func() []tui3.Model { return v3Models(models) },
		ProfileDir:    profileDir,
		PickSession:   pick,
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
