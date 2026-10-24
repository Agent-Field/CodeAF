//go:build darwin

package executor

// jail_darwin.go confines a child with the system's seatbelt (sandbox-exec):
// the command is wrapped, and the profile (seatbelt.go) is passed inline, so
// nothing is written to disk and nothing needs cleaning up.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

const sandboxExec = "/usr/bin/sandbox-exec"

// SeatbeltJail is the macOS Jail. Hidden names extra directories no call may
// read or write, such as the engine store; the workspace's .cell/ is always
// hidden. The zero value is ready to use.
type SeatbeltJail struct {
	Hidden []string
}

// Confine rewrites cmd to run under sandbox-exec.
func (j SeatbeltJail) Confine(cmd *exec.Cmd, req ExecRequest) error {
	if cmd.Err != nil {
		return cmd.Err
	}
	if !seatbeltWorks() {
		return refuseWithout(req, "sandbox-exec is unavailable; declare the workspace host-bound")
	}
	sc, err := j.scopeFor(cmd, req)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(sc.tmp, 0o700); err != nil {
		return err
	}
	// TODO(allowlist): a host allowlist is treated as open until proxying exists.
	cmd.Args = append([]string{sandboxExec, "-p", sc.profile(), cmd.Path}, cmd.Args[1:]...)
	cmd.Path = sandboxExec
	cmd.Env = withTmpdir(cmd.Env, sc.tmp)
	return nil
}

// Degraded reports that sandbox-exec cannot apply, so the call ran unconfined.
func (SeatbeltJail) Degraded(ExecRequest) bool { return !seatbeltWorks() }

// scopeFor builds the call's scope from canonical paths: the profile matches
// real paths, and /var and /tmp are links.
func (j SeatbeltJail) scopeFor(cmd *exec.Cmd, req ExecRequest) (scope, error) {
	root, err := canonical(cmd.Dir)
	if err != nil {
		return scope{}, err
	}
	// The private tmp is a directory of its own, reused by every call in this
	// workspace, so it is never the workspace and needs no removal hook.
	tmp, err := canonical(os.TempDir())
	if err != nil {
		return scope{}, err
	}
	hidden, err := canonicals(append([]string{filepath.Join(root, ".cell")}, j.Hidden...))
	if err != nil {
		return scope{}, err
	}
	return scope{root: root, tmp: filepath.Join(tmp, "codeaf-jail-"+digest(root)), hidden: hidden, net: req.Net}, nil
}

// canonical is an absolute path with links resolved; a path that does not
// exist yet keeps its cleaned form.
func canonical(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		return real, nil
	}
	return abs, nil
}

func canonicals(paths []string) ([]string, error) {
	out := make([]string, len(paths))
	for i, p := range paths {
		c, err := canonical(p)
		if err != nil {
			return nil, err
		}
		out[i] = c
	}
	return out, nil
}

func digest(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}

// withTmpdir points the child's temporary directory at the private one; a nil
// environment inherits the host's first.
func withTmpdir(env []string, tmp string) []string {
	if env == nil {
		env = os.Environ()
	}
	kept := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "TMPDIR=") {
			kept = append(kept, kv)
		}
	}
	return append(kept, "TMPDIR="+tmp)
}

var (
	probeOnce sync.Once
	probed    bool
)

// seatbeltWorks measures once whether a profile applies: a no-op runs under a
// minimal one.
func seatbeltWorks() bool {
	probeOnce.Do(func() {
		probe := exec.Command(sandboxExec, "-p", "(version 1)(allow default)", "/usr/bin/true") //codeaf:plumbing capability probe of the jail itself
		probed = probe.Run() == nil
	})
	return probed
}

// DefaultJail is the jail this system provides.
func DefaultJail() Jail { return SeatbeltJail{} }
