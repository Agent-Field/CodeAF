package furrowbin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/env"
)

const CellsUnavailable = "moving chats needs a newer furrow engine than this build carries"

const BinaryEnvVar = "CODEAF_FURROW"

var cellsAvailable = sync.OnceValue(func() bool {
	binary, err := cellsBinary()
	return err == nil && probeCells(binary)
})

// CellsAvailable checks the configured or embedded engine once per process.
// Version strings cannot answer this: the first source engine with these verbs
// still called itself 0.1.0. No PATH fallback may substitute another engine.
func CellsAvailable() bool { return cellsAvailable() }

// probeCells asks only for help, so discovery cannot create a store, start a
// daemon or send data. The exchange verbs are flattened at the CLI's root.
func probeCells(binary string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// A shared store without named heads would put different chats on the
	// same timeline. Capture tokens and explicit sync are also required so an
	// older engine cannot expose an unscreened or non-durable move (C9/U1/U2/C12).
	root, err := exec.CommandContext(ctx, binary, "--help").Output() //codeaf:plumbing checks timeline routing without opening a workspace.
	if err != nil || !strings.Contains(string(root), "--timeline ") {
		return false
	}
	for _, verb := range []string{"export", "published", "want", "import", "rebind", "materialize", "serve", "capture-plan", "sync"} {
		if err := exec.CommandContext(ctx, binary, verb, "--help").Run(); err != nil { //codeaf:plumbing checks the engine's read-only capability contract
			return false
		}
	}
	help, err := exec.CommandContext(ctx, binary, "hook", "turn-end", "--help").Output() //codeaf:plumbing checks changed-path sealing without opening a workspace
	return err == nil && strings.Contains(string(help), "--changed ") && strings.Contains(string(help), "--changed-stdin") && strings.Contains(string(help), "--cell-dir") && strings.Contains(string(help), "--plan")
}

// cellsBinary follows the same owned-engine choice as internal/furrow, without
// importing its toolbelt dependencies into the on-disk cell layout.
func cellsBinary() (string, error) {
	if binary, configured, err := ConfiguredBinary(); configured {
		return binary, err
	}
	return Ensure()
}

// ConfiguredBinary is the shared explicit-engine door for capability discovery
// and execution. A missing configured file must never fall through to PATH or
// the embedded copy, and a relative file must name that file at execution too.
func ConfiguredBinary() (binary string, configured bool, err error) {
	path := strings.TrimSpace(env.Get(BinaryEnvVar))
	if path == "" {
		return "", false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", true, err
	}
	if info.IsDir() {
		return "", true, fmt.Errorf("%s names a directory: %s", BinaryEnvVar, path)
	}
	binary, err = filepath.Abs(path)
	return binary, true, err
}
