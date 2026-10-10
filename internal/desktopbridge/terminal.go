package desktopbridge

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

const (
	// scrollbackBytes bounds what a terminal remembers; a reattach replays it.
	scrollbackBytes = 512 << 10
	maxTerminals    = 16
	killGrace       = 2 * time.Second
	defaultCols     = 100
	defaultRows     = 30
)

// TerminalInfo is one terminal or job as the window reads it. State is
// "running", "exited" (the command ended by itself) or "closed" (the person
// ended it). Kind is "job" when the terminal was started with a command.
type TerminalInfo struct {
	ID         string `json:"id"`
	Kind       string `json:"kind"`
	Title      string `json:"title"`
	Command    string `json:"command,omitempty"`
	Cwd        string `json:"cwd"`
	Shell      string `json:"shell"`
	State      string `json:"state"`
	ExitCode   *int   `json:"exitCode,omitempty"`
	StartedAt  string `json:"startedAt"`
	EndedAt    string `json:"endedAt,omitempty"`
	DurationMs int64  `json:"durationMs"`
	Cols       int    `json:"cols"`
	Rows       int    `json:"rows"`
	// Bytes is the offset one past the newest output byte.
	Bytes uint64 `json:"bytes"`
}

type terminal struct {
	id, title, command, cwd, shell string
	started                        time.Time

	cmd  *exec.Cmd
	ptmx *os.File

	mu         sync.Mutex
	buf        []byte
	base       uint64 // offset of buf[0]
	changed    chan struct{}
	cols, rows int
	exited     bool
	closing    bool // the person asked to end it
	code       int
	ended      time.Time
	done       chan struct{} // closed once exited is set
}

func (t *terminal) job() bool { return t.command != "" }

// info reads the terminal's public state.
func (t *terminal) info() TerminalInfo {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.infoLocked()
}

func (t *terminal) infoLocked() TerminalInfo {
	out := TerminalInfo{ID: t.id, Kind: "terminal", Title: t.title, Command: t.command, Cwd: t.cwd, Shell: t.shell,
		State: "running", StartedAt: t.started.UTC().Format(time.RFC3339), Cols: t.cols, Rows: t.rows, Bytes: t.base + uint64(len(t.buf))}
	if t.job() {
		out.Kind = "job"
	}
	end := time.Now()
	if t.exited {
		end = t.ended
		code := t.code
		out.ExitCode = &code
		out.State = "exited"
		if t.closing {
			out.State = "closed"
		}
		out.EndedAt = end.UTC().Format(time.RFC3339)
	}
	out.DurationMs = end.Sub(t.started).Milliseconds()
	return out
}

// shellPath is the user's shell on the engine host.
func shellPath() string {
	if sh := os.Getenv("SHELL"); sh != "" && filepath.IsAbs(sh) {
		if st, err := os.Stat(sh); err == nil && !st.IsDir() {
			return sh
		}
	}
	return "/bin/sh"
}

func clampSize(cols, rows int) (int, int) {
	if cols < 2 || cols > 1000 {
		cols = defaultCols
	}
	if rows < 2 || rows > 500 {
		rows = defaultRows
	}
	return cols, rows
}

// startTerminal runs the shell (or the job's command through it) in cwd under a
// new PTY and its own process group.
func startTerminal(id, title, command, cwd string, cols, rows int) (*terminal, error) {
	if st, err := os.Stat(cwd); err != nil || !st.IsDir() {
		return nil, fmt.Errorf("the workspace folder %s is not available on the engine host", cwd)
	}
	cols, rows = clampSize(cols, rows)
	shell := shellPath()
	// A terminal window is an interactive login shell, like the user's native
	// terminal. Login profiles establish PATH before interactive rc aliases run.
	args := []string{"-l", "-i"}
	if command != "" {
		args = []string{"-c", command}
	}
	cmd := exec.Command(shell, args...)
	cmd.Dir = cwd
	cmd.Env = terminalEnv()
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, fmt.Errorf("cannot start %s: %w", filepath.Base(shell), err)
	}
	if title == "" {
		title = command
	}
	if title == "" {
		title = filepath.Base(shell)
	}
	t := &terminal{id: id, title: title, command: command, cwd: cwd, shell: shell, started: time.Now(), cmd: cmd, ptmx: ptmx,
		changed: make(chan struct{}), cols: cols, rows: rows, done: make(chan struct{})}
	readDone := make(chan struct{})
	go t.read(readDone)
	go t.wait(readDone)
	return t, nil
}

// read moves output from the PTY into the bounded scrollback.
func (t *terminal) read(done chan struct{}) {
	defer close(done)
	chunk := make([]byte, 32<<10)
	for {
		n, err := t.ptmx.Read(chunk)
		if n > 0 {
			t.append(chunk[:n])
		}
		if err != nil {
			return
		}
	}
}

func (t *terminal) append(p []byte) {
	t.mu.Lock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - scrollbackBytes; over > 0 {
		t.buf = append([]byte(nil), t.buf[over:]...)
		t.base += uint64(over)
	}
	t.wakeLocked()
	t.mu.Unlock()
}

func (t *terminal) wakeLocked() {
	close(t.changed)
	t.changed = make(chan struct{})
}

// wait reaps the shell, lets the last output drain, then records the ending.
func (t *terminal) wait(readDone chan struct{}) {
	err := t.cmd.Wait()
	select {
	case <-readDone:
	case <-time.After(500 * time.Millisecond):
	}
	_ = t.ptmx.Close()
	code := 0
	if st := t.cmd.ProcessState; st != nil {
		code = st.ExitCode()
		if ws, ok := st.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			code = 128 + int(ws.Signal())
		}
	} else if err != nil {
		code = 1
	}
	t.mu.Lock()
	t.exited, t.code, t.ended = true, code, time.Now()
	t.wakeLocked()
	t.mu.Unlock()
	close(t.done)
}

// signal sends sig to this terminal's own process group, and only while the
// shell it started has not been reaped, so a recycled pid is never signalled.
func (t *terminal) signal(sig syscall.Signal) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.exited || t.cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-t.cmd.Process.Pid, sig)
}

// close ends the terminal's process group: hang up, then kill after a grace.
func (t *terminal) close() {
	t.mu.Lock()
	if t.exited {
		t.mu.Unlock()
		return
	}
	t.closing = true
	t.mu.Unlock()
	t.signal(syscall.SIGHUP)
	t.signal(syscall.SIGTERM)
	select {
	case <-t.done:
	case <-time.After(killGrace):
		t.signal(syscall.SIGKILL)
		<-t.done
	}
}

func (t *terminal) write(p []byte) error {
	t.mu.Lock()
	exited := t.exited
	t.mu.Unlock()
	if exited {
		return errors.New("this terminal has ended")
	}
	_, err := t.ptmx.Write(p)
	return err
}

func (t *terminal) resize(cols, rows int) error {
	cols, rows = clampSize(cols, rows)
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.exited {
		return errors.New("this terminal has ended")
	}
	if err := pty.Setsize(t.ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		return err
	}
	t.cols, t.rows = cols, rows
	return nil
}

// since returns the output from offset after (clamped to what is still kept),
// the offset one past it, whether the start was cut, the change signal and
// whether the terminal has ended.
func (t *terminal) since(after uint64) (data []byte, end uint64, cut bool, changed <-chan struct{}, exited bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if after < t.base {
		after, cut = t.base, true
	}
	end = t.base + uint64(len(t.buf))
	if after < end {
		data = append([]byte(nil), t.buf[after-t.base:]...)
	}
	return data, end, cut, t.changed, t.exited
}

// terminalSet is one conversation's terminals, in creation order.
type terminalSet struct {
	mu    sync.Mutex
	items map[string]*terminal
	order []string
}

func (s *conversation) terminals() *terminalSet {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terms == nil {
		s.terms = &terminalSet{items: map[string]*terminal{}}
	}
	return s.terms
}

func (ts *terminalSet) get(id string) *terminal {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	return ts.items[id]
}

func (ts *terminalSet) list() []TerminalInfo {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	out := make([]TerminalInfo, 0, len(ts.order))
	for _, id := range ts.order {
		out = append(out, ts.items[id].info())
	}
	return out
}

func (ts *terminalSet) add(t *terminal) {
	ts.items[t.id] = t
	ts.order = append(ts.order, t.id)
}

func (ts *terminalSet) remove(id string) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.items, id)
	for i, v := range ts.order {
		if v == id {
			ts.order = append(ts.order[:i:i], ts.order[i+1:]...)
			break
		}
	}
}

// closeAll ends every terminal; the engine is going away.
func (ts *terminalSet) closeAll() {
	ts.mu.Lock()
	all := make([]*terminal, 0, len(ts.items))
	for _, t := range ts.items {
		all = append(all, t)
	}
	ts.mu.Unlock()
	var wg sync.WaitGroup
	for _, t := range all {
		wg.Add(1)
		go func() { defer wg.Done(); t.close() }()
	}
	wg.Wait()
}
