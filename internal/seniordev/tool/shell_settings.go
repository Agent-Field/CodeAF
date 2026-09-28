//go:build !windows

package tool

import (
	"os"
	"os/exec"
	"path/filepath"
)

func (r *Registry) executionShell() (string, error) {
	settings, err := r.settings()
	if err != nil {
		return "", err
	}
	configured, _ := settings["shell"].(string)
	if configured != "" {
		if shell := resolveShellExecutable(configured); shell != "" && ShellAcceptable(shell) {
			return shell, nil
		}
		return fallbackShell(), nil
	}
	if shell := resolveShellExecutable(os.Getenv("SHELL")); shell != "" && ShellAcceptable(shell) {
		return shell, nil
	}
	return fallbackShell(), nil
}

func resolveShellExecutable(shell string) string {
	if shell == "" {
		return ""
	}
	if filepath.IsAbs(shell) {
		info, err := os.Stat(shell)
		if err == nil && !info.IsDir() {
			return filepath.Clean(shell)
		}
		return ""
	}
	match, err := exec.LookPath(shell)
	if err != nil {
		return ""
	}
	return match
}

func fallbackShell() string {
	if bash := resolveShellExecutable("bash"); bash != "" {
		return bash
	}
	return "/bin/sh"
}
