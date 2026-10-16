//go:build linux || darwin

package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/processgroup"
)

// probeBodyLimit is how much of a health URL's answer is read before the
// connection is closed. The status is the whole verdict; the body is drained a
// little only so a server is not cut off mid-write, and never held.
const probeBodyLimit = 4 << 10

type platformRuntime struct {
	client *http.Client

	mu sync.Mutex
	// own is every leader this codeaf started and has not reaped yet, by pid.
	// A signal to one of them goes through its leader, which is what makes a
	// signal and the reap of the same pid impossible to interleave.
	own map[int]*leader
}

// PlatformRuntime is this machine's runtime.
func PlatformRuntime() Runtime {
	transport := &http.Transport{
		// A check is one request to one address. No proxy: a local server
		// reached through somebody's proxy is not a check of the local server.
		Proxy:                 nil,
		DisableKeepAlives:     true,
		TLSHandshakeTimeout:   CheckTimeout,
		ResponseHeaderTimeout: CheckTimeout,
		// THE CERTIFICATE IS VERIFIED. The person approved a check of an
		// address; an https answer from something that cannot prove it is that
		// address is a failed check, not a passing one.
	}
	return &platformRuntime{
		own: map[int]*leader{},
		client: &http.Client{
			Transport: transport,
			Timeout:   CheckTimeout,
			// 200 MEANS 200. A redirect is an answer, and following it would
			// check some other address than the one the person approved.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// leader is one process this codeaf started, with the group it leads.
//
// EVERY SIGNAL TO IT IS SENT BEFORE IT IS REAPED, OR NOT AT ALL. Until the
// leader is reaped its pid — and so its group number — cannot be handed to
// anybody else, and the identity gate underneath (internal/processgroup) can
// still prove the group is ours. The reap and every signal take the same lock,
// and the reap marks the leader reaped inside it, so no signal can land in
// the instant after the number was let go.
type leader struct {
	mu     sync.Mutex
	group  processgroup.Group
	id     processgroup.Identity
	reaped bool
	// proven says the group's identity was read at launch. A group that could
	// not be proven is never signalled as a group; its leader alone is ended
	// through Go's own handle, which cannot outlive the reap either.
	proven bool
	proc   *os.Process
	// send is the group's signal, a seam a test replaces to prove what is sent.
	send func(g processgroup.Group, kill bool) error
}

func newLeader(proc *os.Process) (*leader, bool) {
	group := processgroup.CaptureGroup(proc.Pid)
	id, ok := group.Identity()
	return &leader{group: group, id: id, proven: ok, proc: proc, send: sendGroup}, ok
}

func sendGroup(g processgroup.Group, kill bool) error {
	if kill {
		return g.Kill()
	}
	return g.Terminate()
}

func (l *leader) signal(kill bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.reaped {
		return nil
	}
	if !l.proven {
		if kill {
			return l.proc.Kill()
		}
		return l.proc.Signal(syscall.SIGTERM)
	}
	return l.send(l.group, kill)
}

// reap waits for the leader, ends whatever it left in its group while the
// leader is still an unreaped zombie, and only then reaps it.
//
// A dev server whose shell died must not leave its compiler holding the port
// the restart is about to need, and a check that forked a watcher must not
// leave it behind every ten seconds.
func (l *leader) reap(cmd *exec.Cmd) Exit {
	waitExited(cmd.Process.Pid)
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.proven {
		_ = l.send(l.group, true)
	}
	l.reaped = true
	return exitOf(cmd.Wait())
}

func (r *platformRuntime) Boot() string { return processgroup.BootID() }

// gateScript is the gate every kept process starts behind, and it is a
// constant: it reads one line from descriptor 3 and, only when that line is
// the commit word, closes the descriptor and replaces itself with the approved
// command, which arrives as separate arguments ("$0" "$@") and is never
// spliced into this text. End-of-file — codeaf gone, or the launch aborted —
// or any other line exits 125 having run nothing.
const gateScript = `IFS= read -r word <&3 || exit 125; [ "$word" = run ] || exit 125; exec 3<&-; exec "$0" "$@"`

// gateWord is what Commit writes.
const gateWord = "run\n"

// gateShell is the fixed shell the gate runs in.
const gateShell = "/bin/sh"

type platformLaunch struct {
	id     processgroup.Identity
	commit *os.File
	once   sync.Once
}

func (l *platformLaunch) Identity() processgroup.Identity { return l.id }

func (l *platformLaunch) Commit() error {
	err := errors.New("the launch was already let go")
	l.once.Do(func() {
		_, err = l.commit.Write([]byte(gateWord))
		if closeErr := l.commit.Close(); err == nil {
			err = closeErr
		}
	})
	return err
}

func (l *platformLaunch) Abort() {
	l.once.Do(func() { _ = l.commit.Close() })
}

func (r *platformRuntime) Launch(spec Spec, logPath string, exited func(processgroup.Identity, Exit)) (Launch, error) {
	if info, err := os.Stat(spec.Dir); err != nil || !info.IsDir() {
		return nil, fmt.Errorf("the folder %s is not there", spec.Dir)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	defer logFile.Close()
	// The gate's end of the pipe is the child's descriptor 3 and nothing
	// else's; codeaf's end is close-on-exec, so no other child of codeaf
	// holds it open and codeaf ending is end-of-file at the gate.
	gateEnd, commitEnd, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	// The command runs whole, as the person approved it, through the same
	// streaming shell and the same scrubbed environment as every command a
	// conversation runs: no provider key, no way into codeaf's tmux.
	shell, args := bare.StreamingShell(spec.Command)
	cmd := exec.Command(gateShell, append([]string{"-c", gateScript, shell}, args...)...)
	cmd.Dir = spec.Dir
	cmd.Env = bare.StreamingEnv()
	cmd.ExtraFiles = []*os.File{gateEnd}
	// The child writes to the file itself, not through a pipe codeaf reads, so
	// nothing breaks when codeaf goes: the service keeps writing its log.
	cmd.Stdout, cmd.Stderr = logFile, logFile
	processgroup.ConfigureDetached(cmd)
	err = cmd.Start()
	_ = gateEnd.Close()
	if err != nil {
		_ = commitEnd.Close()
		return nil, fmt.Errorf("could not start the command: %w", err)
	}
	l, ok := newLeader(cmd.Process)
	if !ok {
		// A SERVICE THAT CANNOT BE PROVEN LATER IS NOT KEPT. Closing the
		// commit end lets the gate exit having run nothing, and the reap takes
		// only our own unreaped child.
		_ = commitEnd.Close()
		_ = cmd.Wait()
		return nil, errors.New("could not record the process's identity, so it was not kept")
	}
	r.mu.Lock()
	r.own[l.id.PID] = l
	r.mu.Unlock()
	go func() {
		exit := l.reap(cmd)
		r.mu.Lock()
		if r.own[l.id.PID] == l {
			delete(r.own, l.id.PID)
		}
		r.mu.Unlock()
		exited(l.id, exit)
	}()
	return &platformLaunch{id: l.id, commit: commitEnd}, nil
}

func (r *platformRuntime) Alive(id processgroup.Identity) bool {
	return processgroup.Restore(id).Alive()
}

// Signal sends through the leader when this codeaf started the process, and
// through the persisted identity's gate when it re-adopted one.
func (r *platformRuntime) Signal(id processgroup.Identity, kill bool) error {
	r.mu.Lock()
	l := r.own[id.PID]
	r.mu.Unlock()
	if l != nil && l.id == id {
		return l.signal(kill)
	}
	return sendGroup(processgroup.Restore(id), kill)
}

func (r *platformRuntime) PortInUse(port int) bool {
	dialer := net.Dialer{Timeout: time.Second}
	conn, err := dialer.Dial("tcp", net.JoinHostPort("localhost", strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func (r *platformRuntime) Probe(ctx context.Context, spec Spec) error {
	ctx, cancel := context.WithTimeout(ctx, CheckTimeout)
	defer cancel()
	switch spec.Health.Kind {
	case HealthPort:
		var dialer net.Dialer
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort("localhost", strconv.Itoa(spec.Health.Port)))
		if err != nil {
			return fmt.Errorf("port %d not open", spec.Health.Port)
		}
		return conn.Close()
	case HealthURL:
		return r.probeURL(ctx, spec.Health.URL)
	case HealthCmd:
		return runCheckCommand(ctx, spec)
	}
	return fmt.Errorf("unknown check %q", spec.Health.Kind)
}

func (r *platformRuntime) probeURL(ctx context.Context, address string) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	response, err := r.client.Do(request)
	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return errors.New("no answer in 5s")
		}
		return errors.New("no answer")
	}
	_, _ = io.CopyN(io.Discard, response.Body, probeBodyLimit)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("answered %d", response.StatusCode)
	}
	return nil
}

// runCheckCommand runs the approved check command whole, in the service's
// folder, in a process group of its own. A check that outlives ctx is ended
// with its whole group — through the leader, before it is reaped — and so is
// anything a finished check left behind.
func runCheckCommand(ctx context.Context, spec Spec) error {
	shell, args := bare.StreamingShell(spec.Health.Cmd)
	cmd := exec.Command(shell, args...)
	cmd.Dir = spec.Dir
	cmd.Env = bare.StreamingEnv()
	// What a check prints goes nowhere: its exit status is the verdict, and
	// no pipe means no descendant can hold the check open by holding one.
	cmd.Stdout, cmd.Stderr = nil, nil
	processgroup.ConfigureDetached(cmd)
	if err := cmd.Start(); err != nil {
		return errors.New("could not run")
	}
	l, _ := newLeader(cmd.Process)
	done := make(chan Exit, 1)
	go func() { done <- l.reap(cmd) }()
	var exit Exit
	select {
	case exit = <-done:
	case <-ctx.Done():
		_ = l.signal(true)
		<-done
		return errors.New("took over 5s")
	}
	if !exit.Known || exit.Code != 0 {
		return fmt.Errorf("exits %d", exit.Code)
	}
	return nil
}
