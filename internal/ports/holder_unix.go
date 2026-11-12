//go:build !windows

package ports

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	// HelperBudget is the longest the lookup helper runs, HelperOutputCap
	// the most of its output ever held.
	HelperBudget    = 500 * time.Millisecond
	HelperOutputCap = 64 << 10
	maxNameLen      = 64
)

// lsofPaths are the only places the helper is looked for: absolute and
// fixed, so no PATH entry or user directory can stand in for it.
var lsofPaths = []string{"/usr/sbin/lsof", "/usr/bin/lsof"}

func addrInUse(error) bool { return false }

// lsofHolder asks the system lsof for the one listener on a TCP port. The
// argv is fixed apart from the port, which is an integer.
func lsofHolder(ctx context.Context, port int) *Holder {
	if port < MinPort || port > MaxPort {
		return nil
	}
	for _, path := range lsofPaths {
		if st, err := os.Stat(path); err != nil || st.IsDir() {
			continue
		}
		out, err := runCapped(ctx, path, []string{"-nP", "+c", "0", "-iTCP:" + strconv.Itoa(port), "-sTCP:LISTEN", "-F", "pc"}, HelperBudget, HelperOutputCap)
		if err != nil {
			// lsof exits 1 for "nothing found", and an error is not proof.
			return nil
		}
		return ParseLsof(out)
	}
	return nil
}

var errOverflow = errors.New("helper output over cap")

// cappedWriter keeps at most limit bytes and cancels the helper the moment
// one more arrives: output is never held without bound.
type cappedWriter struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if w.buf.Len()+len(p) > w.limit {
		w.overflow = true
		w.cancel()
		return 0, errOverflow
	}
	return w.buf.Write(p)
}

// runCapped runs path with argv and an empty environment, bounded in time and
// output, in its own process group so a helper that spawned anything is
// killed whole. A timeout, an overflow or a non-zero exit is an error.
func runCapped(ctx context.Context, path string, argv []string, budget time.Duration, limit int) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	w := &cappedWriter{limit: limit, cancel: cancel}
	cmd := exec.CommandContext(ctx, path, argv...)
	cmd.Env = []string{"LC_ALL=C", "PATH="}
	cmd.Stdout = w
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 200 * time.Millisecond
	err := cmd.Run()
	if w.overflow {
		return nil, errOverflow
	}
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return w.buf.Bytes(), nil
}

// ParseLsof reads lsof -F pc output: "p<pid>" then "c<name>" per process.
// It answers a holder only when the text is well formed and names exactly
// one process; two processes, a missing name, a bad pid or an unterminated
// last line are all nil, because a guessed holder is worse than none.
func ParseLsof(out []byte) *Holder {
	if len(out) == 0 || out[len(out)-1] != '\n' {
		return nil
	}
	type record struct {
		pid  int
		name string
	}
	var recs []record
	for _, line := range strings.Split(strings.TrimSuffix(string(out), "\n"), "\n") {
		if line == "" {
			return nil
		}
		switch line[0] {
		case 'p':
			n, err := strconv.Atoi(line[1:])
			if err != nil || n <= 0 {
				return nil
			}
			recs = append(recs, record{pid: n})
		case 'c':
			if len(recs) == 0 || recs[len(recs)-1].name != "" {
				return nil
			}
			name := cleanName(line[1:])
			if name == "" {
				return nil
			}
			recs[len(recs)-1].name = name
		default:
			return nil
		}
	}
	if len(recs) == 0 {
		return nil
	}
	for _, r := range recs {
		if r.name == "" || r.pid != recs[0].pid || r.name != recs[0].name {
			return nil
		}
	}
	return &Holder{PID: recs[0].pid, Name: recs[0].name}
}

// cleanName is the program's base name: no path, no control bytes, bounded.
func cleanName(s string) string {
	s = filepath.Base(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return ""
		}
		b.WriteRune(r)
		if b.Len() >= maxNameLen {
			break
		}
	}
	if b.String() == "." || b.String() == "/" {
		return ""
	}
	return b.String()
}
