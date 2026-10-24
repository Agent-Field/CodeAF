package executor

import (
	"fmt"
	"strings"
)

// seatbelt.go renders the macOS sandbox profile from the same policy the
// Linux jail applies: reads anywhere, writes only in the workspace and a
// private temporary directory, no network unless the policy opens it, and the
// hidden directories (the harness-owned .cell/ and the engine store) neither
// read nor written. The profile is plain text, so it is built and tested on
// every system; only running it needs macOS (jail_darwin.go).

// scope is what one call's profile is built from.
type scope struct {
	root   string    // canonical workspace root
	tmp    string    // canonical private temporary directory
	hidden []string  // canonical paths no call may touch
	net    NetPolicy // the call's network policy
}

// devSinks are the device nodes any tool needs to write.
var devSinks = []string{"/dev/null", "/dev/zero", "/dev/random", "/dev/urandom",
	"/dev/tty", "/dev/dtracehelper", "/dev/stdout", "/dev/stderr", "/dev/fd"}

// profile renders the whole profile. Later rules win, so the hidden paths
// come last.
func (s scope) profile() string {
	return strings.Join([]string{
		"(version 1)",
		"(deny default)",
		runtimePlumbing,
		"(allow file-read*)",
		clause("allow file-write*", subpaths(s.root, s.tmp)+literals(devSinks...)),
		s.network(),
		s.hide(),
	}, "\n") + "\n"
}

// runtimePlumbing is what a process needs to start and run at all.
const runtimePlumbing = `(allow process-exec*)
(allow process-fork)
(allow signal (target self))
(allow sysctl-read)
(allow mach-lookup)
(allow ipc-posix-shm*)`

func (s scope) network() string {
	if s.net.Denies() {
		return "(deny network*)"
	}
	return "(allow network*)"
}

// hide refuses every access beneath the hidden paths; with none it is empty.
func (s scope) hide() string {
	if len(s.hidden) == 0 {
		return ""
	}
	return clause("deny file-read* file-write*", subpaths(s.hidden...))
}

func clause(head, filters string) string { return "(" + head + "\n" + filters + ")" }

// subpaths filters a directory tree; literals filters exact paths.
func subpaths(paths ...string) string { return filter("subpath", paths) }
func literals(paths ...string) string { return filter("literal", paths) }

func filter(kind string, paths []string) string {
	var b strings.Builder
	for _, p := range paths {
		fmt.Fprintf(&b, "    (%s %s)\n", kind, quote(p))
	}
	return b.String()
}

var quoter = strings.NewReplacer(`\`, `\\`, `"`, `\"`)

// quote is a profile string literal.
func quote(p string) string { return `"` + quoter.Replace(p) + `"` }
