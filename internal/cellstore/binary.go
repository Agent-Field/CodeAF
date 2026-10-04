package cellstore

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sync"
	"time"

	"github.com/Agent-Field/codeaf/internal/furrow"
)

// sealFlag is the engine flag every sealed workspace depends on: without it
// the cell's directory cannot be composed in and the engine would leave its
// own files in the user's folder.
const sealFlag = "--cell-dir"

const probeTimeout = 10 * time.Second

// probed remembers the answer per engine path, so the probe costs one spawn
// per process and not one per seal.
var probed sync.Map // path -> error (nil when capable)

// sealingEngine is the engine that seals: the configured or embedded one and
// never a PATH copy, which may be a different, older program with the same
// name. It is asked what it can do rather than what version it says it is.
func sealingEngine() (string, error) {
	bin, err := furrow.ResolveOwned()
	if err != nil {
		return "", fmt.Errorf("seal: no engine to seal with (set %s, or use a codeaf build that carries one): %w", furrow.BinaryEnvVar, err)
	}
	if known, ok := probed.Load(bin); ok {
		return bin, asError(known)
	}
	verdict := probe(bin)
	probed.Store(bin, verdict)
	return bin, verdict
}

func asError(v any) error {
	err, _ := v.(error)
	return err
}

// probe asks the engine's own help whether it knows [sealFlag].
func probe(bin string) error {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, bin, "hook", "turn-end", "--help") //codeaf:plumbing capability probe of the sealing engine
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("seal: engine %s cannot be probed: %w", bin, err)
	}
	if !bytes.Contains(out.Bytes(), []byte(sealFlag)) {
		return fmt.Errorf("seal: engine %s does not support %s; point %s at a current engine", bin, sealFlag, furrow.BinaryEnvVar)
	}
	return nil
}
