package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/config"
)

// faultMessage is what the user reads when aforge could not keep going. It is
// the whole story: what happened, what it cost them (nothing), and where the
// detail lives. A raw goroutine dump over the alt screen says none of that.
const faultMessage = "aforge hit an internal fault and had to stop. Nothing is lost — the graph is durable, and restarting resumes where it left off. Details: %s\n"

// reportFault writes the stack where it is useful and the sentence where it is
// read, and answers with the process exit code.
func reportFault(stderr io.Writer, detail string, stack []byte) int {
	// A fault has no settings to read — it is what is left when the launch did
	// not get that far — but the profile is an environment pin and is still
	// readable, and it is read HERE for the reason the running log reads it: the
	// surface's log and the crash's append are one file, and a profile that moved
	// the first has to move the second, or "Details: <path>" names a file the
	// running log never touched (chatv3_surface.go's [withSurfaceLogger] is the
	// other reader of this path).
	path := chatLogPath(config.ProfileDir())
	writeFaultLog(path, detail, stack)
	fmt.Fprintf(stderr, faultMessage, displayPath(path))
	return 1
}

// chatLogPath is the ONE name of the file this binary parks the standard logger
// in: EVERY door that opens the v3 surface does it for the surface's whole
// lifetime so a log line cannot tear through the frame (chatv3_surface.go's
// [withSurfaceLogger]), and a fault appends its stack to the same file so that
// "what happened" has one answer.
//
// AN EMPTY PROFILE IS THE STATE ROOT AND NEVER THE WORKING DIRECTORY. Most
// launches set no AFORGE_PROFILE_DIR at all, and joining "chat.log" onto an
// empty string names it RELATIVE — so every repository a person opened a chat in
// grew an untracked chat.log, and the surface's own repository band then counted
// that workspace dirty because of a file the surface itself had written. The
// fallback is [config.BudgetConfigPath]'s, spelled the same way for the same
// reason: internal/home is the one place that knows where state lives, and
// AFORGE_HOME moves this with the rest of it (chatv3_layout.go).
func chatLogPath(profileDir string) string { return config.ProfilePath(profileDir, "chat.log") }

// displayPath prefers the ~ form: it is what the user typed to get here and
// what they will type to read the log.
func displayPath(path string) string {
	// `base` and not `home`: this file has carried an internal/home import
	// before, and a local shadowing a package is the kind of thing that reads
	// fine until somebody adds a line under it.
	base, err := os.UserHomeDir()
	if err != nil || base == "" {
		return path
	}
	prefix := base + string(os.PathSeparator)
	if strings.HasPrefix(path, prefix) {
		return "~/" + filepath.ToSlash(strings.TrimPrefix(path, prefix))
	}
	return path
}

// writeFaultLog appends the fault to the same file the TUI already sends the
// standard logger to, so one file answers "what happened" for every fault.
// Best-effort: a process that is already dying must not die twice.
func writeFaultLog(path, detail string, stack []byte) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "%s fatal fault: %s\n%s\n",
		time.Now().Format("2006/01/02 15:04:05"), detail, stack)
}
