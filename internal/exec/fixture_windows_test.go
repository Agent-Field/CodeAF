//go:build windows

package exec

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	commandshell "github.com/Agent-Field/codeaf/internal/shell"
)

// K6 starts a copied test executable as a native stand-in for one scripted
// tool. The companion script supplies its behavior; no installed poppler or
// rtk, provider, or network is involved.
func init() {
	self, err := os.Executable()
	if err != nil {
		return
	}
	script := self + ".script"
	if _, err := os.Stat(script); err != nil {
		return
	}
	bash, err := commandshell.Bash()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	args := append([]string{commandshell.ListPath(script)}, os.Args[1:]...)
	command := exec.Command(bash, args...)
	command.Stdin, command.Stdout, command.Stderr = os.Stdin, os.Stdout, os.Stderr
	command.Env = os.Environ()
	for i, entry := range command.Env {
		key, value, _ := strings.Cut(entry, "=")
		if key == stubPDFLogEnv || key == "CODEAF_RTK_STUB_LOG" {
			command.Env[i] = key + "=" + commandshell.ListPath(value)
		}
	}
	if err := command.Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			os.Exit(exit.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
