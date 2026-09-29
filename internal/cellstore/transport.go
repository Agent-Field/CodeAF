package cellstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
)

// daemonEnv turns the engine daemon off when set to 0: every verb then spawns
// the engine, which is always correct and only slower.
const daemonEnv = "CODEAF_ENGINE_DAEMON"

// Target names the store a verb works on: the engine's data directory, the
// tree it seals or restores, and the directory composed in as that tree's
// .cell/ entry (empty when the tree is the cell's own folder).
type Target struct {
	Tree, DataDir, CellDir string
}

// Op is one engine verb. It knows its own two spellings: the daemon's JSON
// arguments (its struct tags) and the engine's command line.
type Op interface {
	// Verb is the daemon's name for the operation.
	Verb() string
	// Args is the operation as engine command-line arguments, without the
	// composed directory, which a Transport adds.
	Args() []string
}

// Transport carries one Op to the engine and answers what the engine printed.
type Transport interface {
	Do(ctx context.Context, t Target, op Op) ([]byte, error)
}

// ErrUnavailable means the transport could not reach an engine at all. It is
// the one error that a Fallback answers by trying its next transport; an
// engine that ran and refused is an answer, not an absence.
var ErrUnavailable = errors.New("engine unavailable")

// Spawn is the Transport that runs the engine program once per verb.
type Spawn struct {
	// Binary is the engine program; empty means the configured or embedded
	// engine, checked to be able to seal (see sealingEngine).
	Binary string
	// Run runs the program; nil spawns it. Tests count and fake spawns here.
	Run Runner
}

var _ Transport = Spawn{}

// Do implements Transport.
func (s Spawn) Do(ctx context.Context, t Target, op Op) ([]byte, error) {
	bin, err := s.program()
	if err != nil {
		return nil, err
	}
	argv := append([]string{bin}, op.Args()...)
	if t.CellDir != "" {
		argv = append(argv, cellDirArg(t.CellDir)...)
	}
	environ := append(os.Environ(), dataDirEnv+"="+t.DataDir)
	return s.run(ctx, t.Tree, append(environ, opEnv(op)...), argv...)
}

// EnvOp is an Op that carries secrets. A spawn hands them to the engine
// through its environment, because argv is readable in ps; the daemon takes
// them in the request's JSON arguments instead.
type EnvOp interface {
	Op
	// Env is the NAME=value pairs the engine program needs.
	Env() []string
}

// opEnv is the environment an Op adds to a spawn: nothing unless it is an EnvOp.
func opEnv(op Op) []string {
	if e, ok := op.(EnvOp); ok {
		return e.Env()
	}
	return nil
}

func (s Spawn) program() (string, error) {
	if s.Binary != "" {
		return s.Binary, nil
	}
	return sealingEngine()
}

func (s Spawn) run(ctx context.Context, dir string, environ []string, argv ...string) ([]byte, error) {
	if s.Run != nil {
		return s.Run(ctx, dir, environ, argv...)
	}
	return spawn(ctx, dir, environ, argv...)
}

// spawn is the default Runner. Stderr comes back in the error, because the
// engine's own wording is what a person will act on.
func spawn(ctx context.Context, dir string, env []string, argv ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...) //codeaf:plumbing the seal drives the embedded engine
	cmd.Dir, cmd.Env = dir, env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w: %s", filepath.Base(argv[0]), err, strings.TrimSpace(errb.String()))
	}
	return out.Bytes(), nil
}

// Fallback tries Primary and, only when it is unavailable, Secondary.
type Fallback struct{ Primary, Secondary Transport }

var _ Transport = Fallback{}

// Do implements Transport.
func (f Fallback) Do(ctx context.Context, t Target, op Op) ([]byte, error) {
	out, err := f.Primary.Do(ctx, t, op)
	if errors.Is(err, ErrUnavailable) {
		return f.Secondary.Do(ctx, t, op)
	}
	return out, err
}

// transportFor is the one place a Transport is chosen: the daemon with the
// spawn as its fallback, or the spawn alone when the daemon is off or the
// caller supplies its own Runner (which is a promise to see every spawn).
func transportFor(binary string, run Runner) Transport {
	direct := Spawn{Binary: binary, Run: run}
	if run != nil || env.Get(daemonEnv) == "0" {
		return direct
	}
	// With no engine to name, the daemon cannot start and the spawn reports the
	// missing engine in its own words.
	bin, _ := Spawn{Binary: binary}.program()
	return Fallback{Primary: Daemon{Socket: socketFor(bin), Binary: bin}, Secondary: direct}
}

// socketFor names the daemon's socket after the engine binary, whose file name
// is already unique per build (furrow-<version>-<hash>). Two engine builds then
// never share a daemon, so a daemon left over from an upgrade is never asked
// to run the new store logic; it idles out on its own.
func socketFor(bin string) string {
	return home.Join("v3", "engine-"+strings.TrimPrefix(filepath.Base(bin), "furrow-")+".sock")
}
