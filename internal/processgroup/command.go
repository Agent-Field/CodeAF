package processgroup

import (
	"bytes"
	"fmt"
	"os/exec"
)

// Run is exec.Cmd.Run with launch ownership installed before the child runs.
func Run(cmd *exec.Cmd) error {
	if err := Start(cmd); err != nil {
		return err
	}
	return cmd.Wait()
}

// CombinedOutput preserves exec.Cmd's stream checks while using Start.
func CombinedOutput(cmd *exec.Cmd) ([]byte, error) {
	if cmd.Stdout != nil {
		return nil, fmt.Errorf("exec: Stdout already set")
	}
	if cmd.Stderr != nil {
		return nil, fmt.Errorf("exec: Stderr already set")
	}
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	err := Run(cmd)
	return output.Bytes(), err
}
