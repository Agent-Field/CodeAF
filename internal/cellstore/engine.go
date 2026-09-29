package cellstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/cell"
	"github.com/Agent-Field/codeaf/internal/furrow"
	"github.com/Agent-Field/codeaf/internal/home"
)

// dataDirEnv names where the engine keeps its store and catalog. One directory
// per cell means one catalog lock per writer: two writers never wait on each
// other and never share a store.
const dataDirEnv = "FURROW_DATA_DIR"

// repositoryMarker exists inside a folder the engine will accept.
const repositoryMarker = ".git"

// Runner runs one program and returns its stdout. Engine spawns through it so
// tests can count and fake spawns.
type Runner func(ctx context.Context, dir string, env []string, argv ...string) ([]byte, error)

// Engine is the stage 0 Store: one engine spawn per seal.
type Engine struct {
	// Binary is the engine program; empty means whatever internal/furrow resolves.
	Binary string
	// DataRoot holds one store directory per cell; empty means the state root's.
	DataRoot string
	Identity Identity
	Run      Runner
	Now      func() time.Time
}

var _ Store = Engine{}

// LocalDir is the device-local directory of a cell: the engine store and the
// call WAL. It is never inside the cell and never synced.
func (e Engine) LocalDir(c cell.Cell) string { return filepath.Join(e.Root(), c.ID) }

// Root is the directory that holds one local directory per cell.
func (e Engine) Root() string {
	if e.DataRoot == "" {
		return home.Join("v3", "stores")
	}
	return e.DataRoot
}

// Seal implements Store: compose the harness-owned files, snapshot the folder,
// record the turn.
func (e Engine) Seal(ctx context.Context, c cell.Cell, info TurnInfo) (Sealed, error) {
	head, err := Head(c)
	if err != nil {
		return Sealed{}, fmt.Errorf("seal: read head: %w", err)
	}
	receipt := newReceipt(info, transcriptRange(c, head))
	raw, rid, err := receipt.encode()
	if err != nil {
		return Sealed{}, fmt.Errorf("seal: encode receipt: %w", err)
	}
	if err := compose(c, raw, rid, info); err != nil {
		return Sealed{}, fmt.Errorf("seal: compose: %w", err)
	}
	snapshot, err := e.snapshot(ctx, c, rid)
	if err != nil {
		return Sealed{}, err
	}
	turn := newTurn(snapshot, parentOf(head), e.now(), info, rid, e.identity())
	if err := appendTurn(c, turn); err != nil {
		return Sealed{}, fmt.Errorf("seal: record turn: %w", err)
	}
	return Sealed{Turn: turn, Receipt: receipt}, nil
}

func parentOf(head *Sealed) string {
	if head == nil {
		return ""
	}
	return head.Turn.ID
}

func (e Engine) now() time.Time {
	if e.Now == nil {
		return time.Now()
	}
	return e.Now()
}

func (e Engine) identity() Identity {
	id := e.Identity
	if id.Device == "" {
		id.Device = zeroDevice
	}
	return id
}

// snapshot takes one snapshot of the folder through the engine's turn-end
// hook verb, which attaches a folder it has not seen and seals it in one spawn
// with the agent-run trigger. The label carries the receipt id, which is what
// ties a snapshot to its receipt until the engine has a receipt field of its
// own.
func (e Engine) snapshot(ctx context.Context, c cell.Cell, receipt string) (string, error) {
	if err := e.ensureRepository(ctx, c); err != nil {
		return "", err
	}
	out, err := e.engine(ctx, c, "--json", "hook", "turn-end", "--turn", receipt)
	if err != nil {
		return "", fmt.Errorf("seal: snapshot: %w", err)
	}
	return parseSnapshot(out)
}

// ensureRepository gives the folder the .git directory the engine insists on,
// once. One stat per seal is the whole check.
func (e Engine) ensureRepository(ctx context.Context, c cell.Cell) error {
	if _, err := os.Stat(filepath.Join(c.Root, repositoryMarker)); err == nil {
		return nil
	}
	if _, err := e.exec(ctx, c.Root, nil, "git", "init", "-q", "."); err != nil { //codeaf:plumbing the engine requires a git repository to attach a folder
		return fmt.Errorf("seal: prepare folder: %w", err)
	}
	return nil
}

// engine runs one engine verb in the cell's folder against the cell's store.
func (e Engine) engine(ctx context.Context, c cell.Cell, args ...string) ([]byte, error) {
	bin := e.Binary
	if bin == "" {
		resolved, err := furrow.ResolveBinary()
		if err != nil {
			return nil, err
		}
		bin = resolved
	}
	env := append(os.Environ(), dataDirEnv+"="+e.LocalDir(c))
	return e.exec(ctx, c.Root, env, append([]string{bin}, args...)...)
}

func (e Engine) exec(ctx context.Context, dir string, env []string, argv ...string) ([]byte, error) {
	if e.Run != nil {
		return e.Run(ctx, dir, env, argv...)
	}
	return spawn(ctx, dir, env, argv...)
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

func parseSnapshot(out []byte) (string, error) {
	var doc struct {
		Snapshot string `json:"snapshot"`
	}
	if err := json.Unmarshal(out, &doc); err != nil || doc.Snapshot == "" {
		return "", fmt.Errorf("seal: engine answered %q, not a snapshot id", bytes.TrimSpace(out))
	}
	return doc.Snapshot, nil
}
