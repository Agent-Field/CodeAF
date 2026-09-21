// Package opener hands a link or path to the desktop without waiting for the
// application that receives it.
package opener

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"

	"github.com/Agent-Field/codeaf/internal/env"
	"github.com/Agent-Field/codeaf/internal/guard"
)

// Command reports the platform program that opens a target. Linux answers
// xdg-open exactly as the chat surface always has, so a machine where that
// works keeps working; a WSL box that has no xdg-open at all falls to wslview,
// the bridge into the Windows desktop, rather than to a sentence saying the
// browser did not open. The order matters: on a WSL install where xdg-open is
// present it is usually wired to wslview already, and preferring wslview over
// it would change which browser opens on a machine that was fine.
func Command() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", nil
	case "linux":
		if _, err := exec.LookPath("xdg-open"); err != nil && strings.TrimSpace(env.Value("WSL_DISTRO_NAME")) != "" {
			return "wslview", nil
		}
		return "xdg-open", nil
	}
	return "", nil
}

// Start hands target to the platform and returns after the process has
// started. The receiving application is allowed to outlive codeaf.
func Start(target string) error {
	name, args := Command()
	if name == "" {
		return errors.New("this machine has no way to open a browser")
	}
	if strings.TrimSpace(target) == "" {
		return errors.New("the browser did not open")
	}
	command := exec.Command(name, append(append([]string(nil), args...), target)...)
	if command.Err != nil {
		return errors.New("the browser did not open")
	}
	guard.Go("opener/start", func() {
		if command.Start() == nil {
			_ = command.Wait()
		}
	})
	return nil
}
