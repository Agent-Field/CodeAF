package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// processStarted is when this codeaf began, the zero of every startup timing.
var processStarted = time.Now()

// startupKeep is how many lines a startup trace keeps: a few dozen starts.
const startupKeep = 400

var startupMu sync.Mutex

// traceStartup appends one timing line to a startup trace (the solo factory's
// is startup.log on its floor), keeping the last startupKeep lines. It says how
// long a start took step by step, so a slow one can be read back afterwards.
// Lines are step names and milliseconds, never a token, key or request body. A
// trace that cannot be written costs the trace, never the start.
func traceStartup(path, format string, args ...any) {
	line := fmt.Sprintf("%s +%d ms ", processStarted.Format("2006-01-02 15:04:05"), time.Since(processStarted).Milliseconds()) + fmt.Sprintf(format, args...)
	startupMu.Lock()
	defer startupMu.Unlock()
	old, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimRight(string(old), "\n"), "\n")
	if len(old) == 0 {
		lines = nil
	}
	lines = append(lines, line)
	if len(lines) > startupKeep {
		lines = lines[len(lines)-startupKeep:]
	}
	_ = os.MkdirAll(filepath.Dir(path), 0o700)
	_ = os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
	// WriteFile keeps an existing file's mode: narrow one an older build left
	// wider.
	_ = os.Chmod(path, 0o600)
}

// traceRoute is a request path as a trace line names it: the API version and
// the collection, with anything after that (an id, a repository) as one
// placeholder, so the trace holds no job numbers or names.
func traceRoute(path string) string {
	parts := strings.SplitN(strings.Trim(path, "/"), "/", 3)
	if len(parts) == 3 {
		parts[2] = "…"
	}
	return "/" + strings.Join(parts, "/")
}

// ms is d in whole milliseconds, for a trace line.
func ms(d time.Duration) int64 { return d.Milliseconds() }
