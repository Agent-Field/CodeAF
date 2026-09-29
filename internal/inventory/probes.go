package inventory

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// Probes is data, not code: tool name -> arguments that make it print its
// version. A tool absent from the table is recorded by binary hash alone.
type Probes map[string][]string

// DefaultProbes covers the tools most cells use.
var DefaultProbes = Probes{
	"node":         {"--version"},
	"npm":          {"--version"},
	"pnpm":         {"--version"},
	"yarn":         {"--version"},
	"bun":          {"--version"},
	"python":       {"--version"},
	"python3":      {"--version"},
	"uv":           {"--version"},
	"pip":          {"--version"},
	"go":           {"version"},
	"cargo":        {"--version"},
	"rustc":        {"--version"},
	"ruby":         {"--version"},
	"git":          {"--version"},
	"psql":         {"--version"},
	"postgres":     {"--version"},
	"redis-server": {"--version"},
	"mysqld":       {"--version"},
}

const probeTimeout = 5 * time.Second

// Prober prints the version of the binary at path, or "" when it cannot.
type Prober func(path string, args []string) string

// execProbe runs the binary with its version arguments; the first output line
// is the version string.
func execProbe(path string, args []string) string {
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, args...).Output() //codeaf:plumbing version probe of a binary the executor already ran
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return strings.TrimSpace(line)
}
