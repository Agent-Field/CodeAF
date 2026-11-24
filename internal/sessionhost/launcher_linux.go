//go:build linux

package sessionhost

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"
)

// Launcher is the process boundary for both per-connection engines and
// short-lived folder maintenance under the same process identity.
type Launcher interface {
	start(uid int, folders layout, env []string) (*engineChild, error)
	reap(uid int, folders layout, mode reapMode) error
}

type linuxLauncher struct {
	self   string
	engine string
	nproc  int
}

func newLauncher(cfg Config) (linuxLauncher, error) {
	self, err := os.Executable()
	if err != nil {
		return linuxLauncher{}, err
	}
	engine := self
	if cfg.Engine != "" {
		engine, err = filepath.Abs(cfg.Engine)
		if err != nil {
			return linuxLauncher{}, err
		}
	}
	return linuxLauncher{self: self, engine: engine, nproc: cfg.NProc}, nil
}

func helperEnv() []string {
	return []string{"PATH=/usr/local/bin:/usr/bin:/bin", "LANG=C.UTF-8", "CODEAF_TELEMETRY=off", "DO_NOT_TRACK=1"}
}

func engineEnv(cfg Config, folders layout, creds credentials) []string {
	env := append(helperEnv(),
		"HOME="+folders.home(),
		"CODEAF_HOME="+folders.home(),
		"TMPDIR="+folders.tmp(),
		"CODEAF_NO_UPDATE_CHECK=1",
		"CODEAF_BASE_URL="+creds.ModelURL,
		"OPENAI_API_KEY="+creds.ModelToken,
		"CODEAF_ENGINE_IDLE="+cfg.Idle.String(),
	)
	if cfg.Require == modeFenced {
		env = append(env, "CODEAF_FENCE_REQUIRE=1")
	}
	if cfg.Model != "" {
		env = append(env, "CODEAF_MODEL="+cfg.Model)
	}
	return env
}

func (l linuxLauncher) command(ctx context.Context, uid int, dir string, env []string, argv ...string) *exec.Cmd {
	id := strconv.Itoa(uid)
	args := []string{"sessionhost", "launch", "--uid", id, "--gid", id, "--dir", dir, "--nproc", strconv.Itoa(l.nproc), "--"}
	cmd := exec.CommandContext(ctx, "/proc/self/exe", append(args, argv...)...) //codeaf:plumbing re-executes the privilege-dropping helper
	cmd.Env = env
	return cmd
}

func (l linuxLauncher) reap(uid int, folders layout, mode reapMode) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{l.self, "sessionhost", "reap", folders.dir}
	if mode != reapAll {
		args = append(args, string(mode))
	}
	return l.command(ctx, uid, "/", helperEnv(), args...).Run()
}

type engineChild struct {
	in     io.WriteCloser
	out    io.ReadCloser
	signal func(os.Signal) error
	done   <-chan struct{}
	// The waiter publishes these fields by closing done.
	waitErr error
	stderr  stderrRing
}

func (l linuxLauncher) start(uid int, folders layout, env []string) (*engineChild, error) {
	cmd := l.command(context.Background(), uid, folders.work(), env, l.engine, "engine", "--workspace", folders.work())
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	// The bridge owns the read end so Wait cannot discard queued stdout
	// when the child exits before the last frame is forwarded.
	out, writeOut, err := os.Pipe()
	if err != nil {
		in.Close()
		return nil, err
	}
	defer writeOut.Close()
	done := make(chan struct{})
	child := &engineChild{in: in, out: out, done: done}
	cmd.Stdout, cmd.Stderr = writeOut, &child.stderr
	cmd.WaitDelay = time.Second
	if err := cmd.Start(); err != nil {
		in.Close()
		out.Close()
		return nil, err
	}
	child.signal = cmd.Process.Signal
	go func() {
		child.waitErr = cmd.Wait()
		close(done)
	}()
	return child, nil
}

type stderrRing struct {
	data [4096]byte
	next int
	size int
}

func (r *stderrRing) Write(p []byte) (int, error) {
	n := len(p)
	if n >= len(r.data) {
		copy(r.data[:], p[n-len(r.data):])
		r.next, r.size = 0, len(r.data)
	} else {
		first := copy(r.data[r.next:], p)
		copy(r.data[:], p[first:])
		r.next = (r.next + n) % len(r.data)
		r.size = min(r.size+n, len(r.data))
	}
	return n, nil
}

func (r *stderrRing) text() string {
	if r.size < len(r.data) {
		return string(r.data[:r.size])
	}
	return string(r.data[r.next:]) + string(r.data[:r.next])
}
