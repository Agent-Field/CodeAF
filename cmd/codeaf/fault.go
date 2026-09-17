package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/telemetry"
)

// faultMessage is what the user reads when codeaf could not keep going. It is
// the whole story: what happened, what it cost them (nothing), and where the
// detail lives. A raw goroutine dump over the alt screen says none of that.
const faultMessage = "codeaf hit an internal fault and had to stop. Nothing is lost — the graph is durable, and restarting resumes where it left off. Details: %s\n"

// reportFault writes the stack where it is useful and the sentence where it is
// read, and answers with the process exit code. The anonymous fault count is
// the last thing it does, once the sentence is out: a flush may spend a second
// this process no longer has, and the person should not wait on it to read
// what happened.
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
	telemetryFault(stack)
	return 1
}

// telemetryFault spools the contract's fault event for a dying process and
// sends what the spool holds. It is given the stack and nothing else. The
// library hashes the fingerprint out of the stack's own function names, and
// `detail` — the panic's text — never crosses this line: a panic value is the
// person's words, a path on their machine or a fragment of the file they were
// working in, and the fault event has no property for any of it.
//
// SpoolSync and then Flush under a one-second context, the same budget
// telemetryEnd gives a run's last event, because this is the last thing the
// process does: there is no later flush to carry the line, and an append that
// has not landed when the process exits is an event that never happened. The
// gate is the opt-out ladder's own answer, so a person who turned the counts
// off gets the same crash report they always got and nothing on the wire.
func telemetryFault(stack []byte) {
	if !telemetry.Enabled() {
		return
	}
	session := currentTelemetrySession
	event := telemetry.FaultEvent(telemetry.Fault{
		Mode:  string(session.mode),
		Scope: telemetry.ScopeMain,
		Stack: stack,
	}, session.sessionID, time.Now())
	_ = telemetry.SpoolSync(event)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = telemetry.Flush(ctx)
}

// chatLogPath is the ONE name of the file this binary parks the standard logger
// in: EVERY door that opens the v3 surface does it for the surface's whole
// lifetime so a log line cannot tear through the frame (chatv3_surface.go's
// [withSurfaceLogger]), and a fault appends its stack to the same file so that
// "what happened" has one answer.
//
// AN EMPTY PROFILE IS THE STATE ROOT AND NEVER THE WORKING DIRECTORY. Most
// launches set no CODEAF_PROFILE_DIR at all, and joining "chat.log" onto an
// empty string names it RELATIVE — so every repository a person opened a chat in
// grew an untracked chat.log, and the surface's own repository band then counted
// that workspace dirty because of a file the surface itself had written. The
// fallback is [config.BudgetConfigPath]'s, spelled the same way for the same
// reason: internal/home is the one place that knows where state lives, and
// CODEAF_HOME moves this with the rest of it (chatv3_layout.go).
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
