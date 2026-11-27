//go:build unix

package shell

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/processgroup"
)

const loginPathMarker = "__CODEAF_PATH__"

var loginPathTimeout = 3 * time.Second
var loginPathShell = func() string { return os.Getenv("SHELL") }
var loginPathFallbackDirs = []string{
	"/opt/homebrew/bin",
	"/opt/homebrew/sbin",
	"/usr/local/bin",
	"/home/linuxbrew/.linuxbrew/bin",
	"$HOME/.local/bin",
}

// InheritLoginPath appends login and installed tool directories to the desktop
// engine's PATH so its children can find them. It returns the resulting PATH.
func InheritLoginPath() string {
	original := os.Getenv("PATH")
	seen := make(map[string]bool)
	for _, dir := range filepath.SplitList(original) {
		seen[dir] = true
	}
	var entries []string
	if original != "" {
		entries = append(entries, original)
	}
	appendDir := func(dir string) {
		if !filepath.IsAbs(dir) || seen[dir] {
			return
		}
		seen[dir] = true
		entries = append(entries, dir)
	}
	for _, dir := range filepath.SplitList(probeLoginPath()) {
		appendDir(dir)
	}
	for _, dir := range loginPathFallbackDirs {
		if relative, inHome := strings.CutPrefix(dir, "$HOME/"); inHome {
			home := os.Getenv("HOME")
			if !filepath.IsAbs(home) {
				continue
			}
			dir = filepath.Join(home, relative)
		}
		if seen[dir] {
			continue
		}
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			appendDir(dir)
		}
	}

	path := strings.Join(entries, string(os.PathListSeparator))
	if path != original && os.Setenv("PATH", path) != nil {
		return original
	}
	return path
}

func probeLoginPath() string {
	program := loginPathShell()
	if program == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginPathTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, program, "-l", "-c", `printf "`+loginPathMarker+`%s" "$PATH"`)
	processgroup.Configure(cmd)
	cmd.Cancel = func() error { return processgroup.Kill(cmd.Process.Pid) }
	// The group kill covers children of a timed-out probe. WaitDelay bounds
	// the stdout read even if a child outside that group holds the pipe open.
	cmd.WaitDelay = 100 * time.Millisecond
	var output bytes.Buffer
	cmd.Stdout = &output
	// Unset stdin and stderr use the null device, keeping the app wire quiet.
	if err := cmd.Run(); err != nil || ctx.Err() != nil {
		// Silent for the person, who still gets the usual folders; one line on
		// stderr, which the app collects as its log, for whoever asks why.
		log.Printf("login shell PATH probe skipped: %v", errors.Join(err, ctx.Err()))
		return ""
	}

	_, marked, _ := strings.Cut(output.String(), loginPathMarker)
	path, _, _ := strings.Cut(marked, "\n")
	return path
}
