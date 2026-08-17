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

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/aforge-v2/internal/config"
	"github.com/Agent-Field/aforge-v2/internal/guard"
	"github.com/Agent-Field/aforge-v2/internal/remote"
	"github.com/Agent-Field/aforge-v2/internal/session"
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
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("usage: aforge engine [--workspace path] [--session path]")
	}

	return remote.Serve(os.Stdin, os.Stdout, remote.Options{
		Boot: func(hello remote.Hello) (*remote.Engine, error) {
			return bootEngine(hello, *workspace, *file)
		},
	})
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

	launch, err := openV3Launch(v3Options{
		Door:      "engine",
		Workspace: workspace,
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

	agent, cfg, notice, err := openV3Agent(cfg, workspace)
	if err != nil {
		return nil, err
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
		Recent: func() []session.Summary {
			return session.Recent(launch.Bucket, v3RecentSessionSlots)
		},
	}, nil
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
