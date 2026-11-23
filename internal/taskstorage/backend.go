package taskstorage

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/home"
)

// CLIBackend uses the prototype engine's private archive store, never its relay codec.
type CLIBackend struct{ Binary, Store string }

// The native command inherits the same locked open-file description. If the
// manager dies, another recovery cannot acquire its receipt until this writer exits.
// Windows does not inherit this POSIX open-file-description lease. Native
// retirement/recovery must refuse there until equivalent writer fencing exists;
// plain capture calls without a manager receipt lease remain independent.
func (CLIBackend) archiveRecoveryReady() error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("native task retirement and recovery are unavailable on Windows because writer leases cannot be inherited; saved work and staging were retained")
	}
	return nil
}

type receiptLeaseKey struct{}
type archiveReceipt struct {
	Version  uint16 `json:"version"`
	Snapshot string `json:"snapshot"`
}

func (b CLIBackend) call(ctx context.Context, verb string, args ...string) (archiveReceipt, error) {
	if b.Binary == "" || b.Store == "" {
		return archiveReceipt{}, fmt.Errorf("task archive engine is not configured")
	}
	lease, leased := ctx.Value(receiptLeaseKey{}).(*os.File)
	if leased {
		if err := b.archiveRecoveryReady(); err != nil {
			return archiveReceipt{}, err
		}
	}
	argv := append([]string{"archive", verb, "--store", b.Store}, args...)
	// The archive command is a bounded, local storage operation, not a model shell.
	cmd := exec.CommandContext(ctx, b.Binary, argv...) //codeaf:plumbing invokes the local archive engine
	if leased {
		cmd.ExtraFiles = []*os.File{lease}
	}
	var output, stderr bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if len(detail) > 2048 {
			detail = detail[:2048]
		}
		return archiveReceipt{}, fmt.Errorf("task archive %s failed: %w: %s", verb, err, detail)
	}
	var receipt archiveReceipt
	if err := json.Unmarshal(output.Bytes(), &receipt); err != nil {
		return receipt, fmt.Errorf("read archive receipt: %w", err)
	}
	decoded, err := hex.DecodeString(receipt.Snapshot)
	if receipt.Version != 1 || err != nil || len(decoded) != 32 {
		return receipt, fmt.Errorf("unsupported task archive receipt")
	}
	return receipt, nil
}
func (b CLIBackend) Capture(ctx context.Context, path string) (string, error) {
	r, e := b.call(ctx, "capture", "--workspace", path)
	return r.Snapshot, e
}
func (b CLIBackend) Restore(ctx context.Context, snapshot, destination string) error {
	r, e := b.call(ctx, "restore", "--snapshot", snapshot, "--destination", destination)
	if e == nil && r.Snapshot != snapshot {
		return fmt.Errorf("restored a different task archive")
	}
	return e
}
func (b CLIBackend) Verify(ctx context.Context, snapshot, path string) error {
	r, e := b.call(ctx, "verify", "--snapshot", snapshot, "--workspace", path)
	if e == nil && r.Snapshot != snapshot {
		return fmt.Errorf("checked a different task archive")
	}
	return e
}

func (b CLIBackend) VerifyRemainder(ctx context.Context, snapshot, path string) error {
	r, e := b.call(ctx, "verify-remainder", "--snapshot", snapshot, "--workspace", path)
	if e == nil && r.Snapshot != snapshot {
		return fmt.Errorf("checked a different task remainder")
	}
	return e
}

// Prototype enables this unfinished storage format only inside an explicit test
// profile. Stable binaries and ordinary profiles keep their current lifecycle.
func Prototype(trees string) (Manager, bool) {
	if env.Get("CODEAF_PROTOTYPE_TASK_STORAGE") != "1" || strings.TrimSpace(env.Get(home.EnvVar)) == "" || trees == "" || !filepath.IsAbs(trees) {
		return Manager{}, false
	}
	root := home.Join("task-storage")
	return Manager{Root: root, Trees: trees, Vault: CLIBackend{Binary: env.Get("CODEAF_PROTOTYPE_ARCHIVE_BINARY"), Store: filepath.Join(root, "vault")}}, true
}
