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
)

// daemonEnv turns the engine daemon off when set to 0: every verb then spawns
// the engine, which is always correct and only slower.
const daemonEnv = "CODEAF_ENGINE_DAEMON"

// Target names the store a verb works on: the engine's data directory, the
// tree it seals or restores, and the directory composed in as that tree's
// .cell/ entry (empty when the tree is the cell's own folder).
type Target struct {
	Tree, DataDir, CellDir string
	Timeline               string
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

// Spawn runs standalone verbs and uses the managed daemon for seals and sync.
// A custom Runner still sees every invocation, which keeps fixture seams exact.
type Spawn struct {
	// Socket selects an explicitly owned daemon; empty uses the home address.
	// Isolated clients can release their store handles without stopping other chats.
	Socket string
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
	socket := s.Socket
	if socket == "" {
		socket = socketFor(bin)
	}
	// Seals and durability fences always use codeaf's one daemon. The CLI's
	// implicit daemon is per data directory and escapes codeaf's child lifecycle;
	// invoking its direct sync would also miss the retained writer's pending group.
	if s.Run == nil && (op.Verb() == "seal" || op.Verb() == "sync") {
		return (Daemon{Socket: socket, Binary: bin}).Do(ctx, t, op)
	}
	argv := append([]string{bin}, op.Args()...)
	if t.CellDir != "" {
		argv = append(argv, cellDirArg(t.CellDir)...)
	}
	if t.Timeline != "" {
		argv = append(argv, "--timeline", t.Timeline)
	}
	environ := append(os.Environ(), dataDirEnv+"="+t.DataDir)
	if s.Run == nil {
		settleBeforeSpawn(ctx, socket, bin, t)
	}
	release, err := lockStoreOperation(ctx, t.DataDir)
	if err != nil {
		return nil, err
	}
	defer release()
	out, err := s.run(ctx, t.Tree, append(environ, opEnv(op)...), argv...)
	return out, classifyStoreBusyError(t.DataDir, socket, err)
}

// settleBeforeSpawn asks the daemon that already holds the store to make it
// durable before a one-shot engine process opens it.
//
// WHY THIS IS HERE. The daemon writes with deferred durability, so its newest
// appends sit past the store's durable frontier and past the pack checkpoint
// until its next group commit. A one-shot engine process that opens the store
// then finds object rows beyond the checkpoint, judges the checkpoint unusable
// and re-verifies the WHOLE pack from byte zero before it answers, every time.
// With a chat in a 600 MB folder that was 0.65 s per process (a read of the
// whole pack), and a take-back runs three of them to admit the incoming
// snapshot before it asks for the lease: 2 s of a 9.8 s take-back on the
// loopback rig (docs/design/app-v3/MOVE-TIMING.md). One sync through the daemon costs about
// 10 ms and leaves the pack settled, so the process that follows opens it in
// milliseconds.
//
// Only a daemon that is already answering is asked: a spawn must never start
// one, and a store no daemon writes has no deferred tail to settle. The sync is
// an optimisation, so its failure is not the spawn's: the spawn runs anyway and
// pays the walk, exactly as it did before. It does not help after a one-shot
// process that itself appends (the dirty check's dry-run rewind): the next
// open still walks the pack once, which only the engine can stop doing.
func settleBeforeSpawn(ctx context.Context, socket, bin string, t Target) {
	if t.DataDir == "" || socket == "" {
		return
	}
	conn, err := dial(ctx, socket)
	if err != nil {
		return
	}
	_ = conn.Close()
	_ = syncEngine(ctx, Daemon{Socket: socket, Binary: bin}, Target{Tree: t.Tree, DataDir: t.DataDir})
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
	var stuck *SavingStuckError
	if errors.Is(err, ErrUnavailable) && !errors.As(err, &stuck) {
		return f.Secondary.Do(ctx, t, op)
	}
	return out, err
}

// transportFor is the one place a Transport is chosen: the daemon with the
// spawn as its fallback, or the spawn alone when the daemon is off or the
// caller supplies its own Runner (which is a promise to see every spawn).
func transportFor(binary string, run Runner) Transport {
	direct := Spawn{Binary: binary, Run: run}
	if run != nil {
		return direct
	}
	// Deferred durability retains the same daemon address on both transports;
	// a fallback must never resolve a different home's address at flush time.
	bin, _ := direct.program()
	if bin != "" {
		direct.Binary = bin
	}
	direct.Socket = socketFor(bin)
	if env.Get(daemonEnv) == "0" {
		return direct
	}
	return Fallback{Primary: Daemon{Socket: direct.Socket, Binary: bin}, Secondary: direct}
}
