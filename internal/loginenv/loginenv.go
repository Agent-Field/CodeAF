// Package loginenv restores the executable search path missing from GUI launches.
package loginenv

import (
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

const (
	pathStart = "__codeaf_login_path_start__"
	pathEnd   = "__codeaf_login_path_end__"
)

type resolver struct {
	once sync.Once
	path string
}

var process resolver

// Resolve reads login profiles once per process. Other platforms and an explicitly
// set CODEAF_NO_LOGIN_ENV keep the inherited PATH, including an empty opt-out.
func Resolve(ctx context.Context) string {
	return process.resolve(ctx)
}

func (r *resolver) resolve(ctx context.Context) string {
	r.once.Do(func() {
		r.path = os.Getenv("PATH")
		if _, optedOut := os.LookupEnv("CODEAF_NO_LOGIN_ENV"); runtime.GOOS != "darwin" || optedOut {
			return
		}
		home, _ := os.UserHomeDir()
		path, err := resolvePath(ctx, r.path, home, loginPath, os.Stat)
		r.path = path
		if err != nil {
			log.Printf("login PATH: keeping inherited path and installed tool directories (%v)", err)
		}
	})
	return r.path
}

func resolvePath(ctx context.Context, current, home string, read func(context.Context) (string, error), stat func(string) (os.FileInfo, error)) (string, error) {
	login, err := read(ctx)
	if err != nil {
		login = ""
	}
	return mergePath(login, current, home, stat), err
}

func parsePath(output string) (string, error) {
	_, rest, found := strings.Cut(output, pathStart)
	if !found {
		return "", errors.New("login shell did not return the PATH start marker")
	}
	path, _, found := strings.Cut(rest, pathEnd)
	if !found {
		return "", errors.New("login shell did not return the PATH end marker")
	}
	return path, nil
}

func mergePath(login, current, home string, stat func(string) (os.FileInfo, error)) string {
	var entries []string
	seen := make(map[string]bool)
	add := func(entry string) {
		if entry != "" && !seen[entry] {
			entries = append(entries, entry)
			seen[entry] = true
		}
	}
	for _, path := range []string{login, current} {
		for _, entry := range strings.Split(path, ":") {
			add(entry)
		}
	}
	dirs := []string{"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin"}
	if home != "" {
		for _, dir := range []string{".local/bin", "go/bin", ".cargo/bin"} {
			dirs = append(dirs, filepath.Join(home, dir))
		}
	}
	for _, dir := range dirs {
		if seen[dir] {
			continue
		}
		if info, err := stat(dir); err == nil && info.IsDir() {
			add(dir)
		}
	}
	return strings.Join(entries, ":")
}
