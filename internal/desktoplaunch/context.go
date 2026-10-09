// Package desktoplaunch supplies the context missing from a GUI application
// launch. Command-line launches continue to use their caller's context.
package desktoplaunch

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const markerStart = "\nDESKTOP_LOGIN_PATH_BEGIN\n"
const markerEnd = "\nDESKTOP_LOGIN_PATH_END\n"
const maxOutput = 64 << 10
const shellTimeout = 3 * time.Second

type runner func(context.Context, string, string) ([]byte, error)

// Workspace preserves explicit choices. A packaged GUI's unqualified Now
// starts in the person's home, rather than LaunchServices' inherited '/'.
func Workspace(explicit string, gui bool) (string, error) {
	if strings.TrimSpace(explicit) != "" {
		return explicit, nil
	}
	if gui {
		return os.UserHomeDir()
	}
	return os.Getwd()
}

// Path obtains only PATH, never imports a shell's credentials or other values.
// Linux/Windows and ordinary CLI launches retain their existing environment.
func Path(gui bool) string {
	return loginPath(gui, runtime.GOOS, os.Getenv("SHELL"), os.Getenv("PATH"), runShell)
}

func loginPath(gui bool, platform, shell, inherited string, run runner) string {
	if !gui || platform != "darwin" {
		return inherited
	}
	if shell == "" {
		shell = "/bin/zsh"
	}
	if !filepath.IsAbs(shell) {
		return inherited
	}
	ctx, cancel := context.WithTimeout(context.Background(), shellTimeout)
	defer cancel()
	output, err := run(ctx, shell, `/usr/bin/printf '\nDESKTOP_LOGIN_PATH_BEGIN\n'; /usr/bin/printenv PATH; /usr/bin/printf 'DESKTOP_LOGIN_PATH_END\n'`)
	if err != nil || len(output) > maxOutput {
		return inherited
	}
	text := string(output)
	start := strings.LastIndex(text, markerStart)
	if start < 0 {
		return inherited
	}
	text = text[start+len(markerStart):]
	end := strings.Index(text, markerEnd)
	if end < 0 {
		return inherited
	}
	path := text[:end]
	if strings.ContainsAny(path, "\x00\r\n") {
		return inherited
	}
	// A GUI launch has no meaningful relative PATH. Preserve absolute login
	// entries first, then the inherited system entries, without inventing paths.
	entries := []string{}
	seen := map[string]bool{}
	for _, item := range strings.Split(path+":"+inherited, ":") {
		if !filepath.IsAbs(item) || seen[item] {
			continue
		}
		seen[item] = true
		entries = append(entries, item)
	}
	if len(entries) == 0 {
		return inherited
	}
	return strings.Join(entries, ":")
}

type boundedOutput struct{ bytes.Buffer }

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > maxOutput {
		return 0, errors.New("login shell output exceeds limit")
	}
	return b.Buffer.Write(p)
}

func runShell(ctx context.Context, shell, script string) ([]byte, error) {
	command := exec.CommandContext(ctx, shell, "-l", "-i", "-c", script)
	var output boundedOutput
	command.Stdout = &output
	// Bound inherited pipe lifetime too if startup files spawn another process.
	command.WaitDelay = 100 * time.Millisecond
	err := command.Run()
	return output.Bytes(), err
}
