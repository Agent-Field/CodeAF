// Package clipboard puts one short piece of text on the clipboard of the
// machine this process runs on, and says only whether that worked.
//
// THE TEXT IS A SECRET (an invitation), so the package is built around never
// letting it out. It travels to the helper on the helper's standard input and
// nowhere else: never in an argument, an environment variable, a file or a
// shell line. Everything the helper prints is discarded, so a helper that echoes
// its input cannot put it in our output. An error is one of three sentinels with
// fixed words; no error wraps the helper's, its arguments or the text.
//
// NOTHING HERE IS ON BY DEFAULT. [Unavailable] is the production default and
// [NewSystem] is built only by a launcher that has verified it is the person's
// own native session and says so (localGUI). The operating system, a DISPLAY
// variable or an installed helper are never evidence of that: a shell over ssh
// inherits all three and its clipboard is the other machine's.
package clipboard

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// The three answers a copy can give. Their words are fixed.
var (
	// ErrUnavailable: there is no clipboard this process may write, or the
	// helper could not be started. Nothing was written.
	ErrUnavailable = errors.New("this machine cannot copy for you")
	// ErrFailed: the helper ran and said it did not copy.
	ErrFailed = errors.New("the clipboard did not take it")
	// ErrUnconfirmed: the helper did not finish answering, so the text may or
	// may not be on the clipboard.
	ErrUnconfirmed = errors.New("the clipboard did not confirm the copy")
)

const (
	// MaxText is the most a copy takes. A hostile answer cannot stream
	// megabytes into a helper.
	MaxText = 4096
	// helperTimeout bounds one copy.
	helperTimeout = 3 * time.Second
	// helperWaitDelay is how long a helper that exited but left a child holding
	// one of our pipes is waited for.
	helperWaitDelay = 500 * time.Millisecond
)

// Copier is a clipboard. Available is cheap and starts nothing; it says whether
// Copy could be tried now, not that it will work.
type Copier interface {
	Available() bool
	Copy(ctx context.Context, text string) error
}

// Unavailable is the clipboard of a process nobody has shown to be a person's
// own native session. It is the production default.
type Unavailable struct{}

func (Unavailable) Available() bool                    { return false }
func (Unavailable) Copy(context.Context, string) error { return ErrUnavailable }

// Command is one helper invocation, fully decided before anything starts. The
// text is not in it: Stdin is the only place it goes.
type Command struct {
	Path  string
	Args  []string
	Env   []string
	Stdin string
}

// System is the clipboard of a native desktop session, through that platform's
// own helper.
type System struct {
	localGUI bool
	goos     string
	getenv   func(string) string
	lookPath func(string) (string, error)
	stat     func(string) error
	run      func(context.Context, Command) error
}

// NewSystem is the system clipboard. localGUI is asserted by a launcher that
// has verified this process was started by the person's own native app on the
// machine they are sitting at; false makes every method answer unavailable.
func NewSystem(localGUI bool) *System {
	return &System{
		localGUI: localGUI, goos: goos(), getenv: os.Getenv, lookPath: exec.LookPath,
		stat: func(p string) error { _, err := os.Stat(p); return err },
		run:  run,
	}
}

// pick is the helper to use, or false. It reads no secret.
func (s *System) pick() (path string, args []string, ok bool) {
	if !s.localGUI {
		return "", nil, false
	}
	switch s.goos {
	case "darwin":
		const pbcopy = "/usr/bin/pbcopy"
		if s.stat(pbcopy) != nil {
			return "", nil, false
		}
		return pbcopy, nil, true
	case "linux":
		// A remote shell's clipboard is the remote machine's.
		for _, v := range []string{"SSH_CONNECTION", "SSH_CLIENT", "SSH_TTY"} {
			if s.getenv(v) != "" {
				return "", nil, false
			}
		}
		type helper struct {
			display string
			name    string
			args    []string
		}
		for _, h := range []helper{
			{"WAYLAND_DISPLAY", "wl-copy", []string{"--type", "text/plain"}},
			{"DISPLAY", "xclip", []string{"-selection", "clipboard", "-in"}},
			{"DISPLAY", "xsel", []string{"--clipboard", "--input"}},
		} {
			if s.getenv(h.display) == "" {
				continue
			}
			p, err := s.lookPath(h.name)
			// A relative result would be resolved against the current
			// directory, which is not where a trusted helper lives.
			if err != nil || !filepath.IsAbs(p) {
				continue
			}
			return p, h.args, true
		}
	}
	return "", nil, false
}

// Available is true when a helper for this session exists now.
func (s *System) Available() bool {
	_, _, ok := s.pick()
	return ok
}

// helperEnv is the helper's whole environment: the few variables a clipboard
// helper needs to find the display, and nothing else. A provider key, a GitHub
// token or any other secret in this process's environment is not passed on.
func (s *System) helperEnv() []string {
	var env []string
	for _, k := range []string{"PATH", "DISPLAY", "WAYLAND_DISPLAY", "XAUTHORITY", "XDG_RUNTIME_DIR", "DBUS_SESSION_BUS_ADDRESS", "LANG", "LC_ALL", "LC_CTYPE"} {
		if v := s.getenv(k); v != "" {
			env = append(env, k+"="+v)
		}
	}
	return env
}

// Copy writes text to the clipboard through the helper's standard input.
func (s *System) Copy(ctx context.Context, text string) error {
	if text == "" || len(text) > MaxText || strings.ContainsRune(text, 0) {
		return ErrFailed
	}
	path, args, ok := s.pick()
	if !ok {
		return ErrUnavailable
	}
	return s.run(ctx, Command{Path: path, Args: args, Env: s.helperEnv(), Stdin: text})
}
