package session

// A SHELL CALL ALWAYS COMES BACK.
//
// ── WHAT IT COST NOT TO HAVE THIS ──
//
// In the desktop app a canvas agent's bash call (qloop run44, pipeline
// dashboard, 2026-11-20) printed every line its script was meant to print and
// then never answered. Nothing after it happened for the rest of the 900-second
// turn: no model call, no tool result, no `took`, and the call's record in the
// workspace log was opened and never closed. The background-after clock, the
// ten-minute ceiling and the person's own stop are all clocks INSIDE the bash
// tool, and a call that is wedged somewhere else on its way back — a wrapper, a
// record, a lock — is a call none of them can reach. The turn sat "working"
// with nothing working, and the person had no way to learn why.
//
// So the dispatcher holds a clock of its own, OUTSIDE the tool: a bash call
// that has not answered [bashBackstopGrace] after the first bound it was going
// to meet ([BashBoundSeconds]) is answered for it. The process group is killed,
// the model is told in plain words what happened and what to do instead, the
// person's row ends as failed with the same words, and the turn carries on to
// a reply. A stop the person asked for is honoured the same way once
// [bashAbortGrace] has passed without the tool answering it.
//
// AND THE HANG IS NAMED. When the backstop fires it writes every goroutine's
// stack beside the session's job logs (logs/stuck/<call>.txt), because the
// whole reason this file exists is a hang nobody could name afterwards.
//
// ── AND A COMMAND IN THE APP THAT PRINTS NOTHING IS STOPPED ──
//
// A canvas agent's commands are the canvas command and short scripts around
// it; one of those that has printed nothing for [AppShellQuietSeconds] is
// waiting on something that is not coming. The quiet clock that already stops
// a task's silent search ([armQuietClock]) is put on every foreground command
// in the app. It sits beside the handoff clock: a command adopted into a job
// first is the job's, and is not stopped by it.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/exec/bare"
	"github.com/Agent-Field/codeaf/internal/guard"
)

const (
	// AppShellQuietSeconds is how long a foreground command in the desktop app
	// may print nothing before it is stopped. ONE NUMBER: the tool description
	// and the manual read it from here.
	AppShellQuietSeconds = 120

	// bashBackstopGrace is how long past its own first bound a bash call may go
	// without answering before the dispatcher answers for it. It is longer than
	// the bare tool's own drain after a kill (a few seconds), so the backstop
	// never races an ending that is merely slow.
	bashBackstopGrace = 30 * time.Second

	// bashAbortGrace is how long a stopped turn waits for its bash call to
	// answer the stop before the dispatcher answers for it.
	bashAbortGrace = 5 * time.Second

	// droppingStuck is where a backstop's goroutine dump is written.
	droppingStuck = "stuck"
)

// bashBackstopSentence is what the model and the person are told when the
// dispatcher had to answer for a bash call.
func bashBackstopSentence(after time.Duration) string {
	return fmt.Sprintf("Command stopped: it did not finish or answer within %d seconds, so it was ended and its result is lost. "+
		"Do not run it again unchanged: split it into smaller commands, or run a long one with background:true.",
		int(after.Round(time.Second)/time.Second))
}

// bashAbortedSentence is the answer to a stop the call itself never answered.
const bashAbortedSentence = "Command aborted: the turn was stopped and the command did not end on its own, so it was ended."

// appShellQuietSentence is what a quiet foreground command in the app ends with,
// after the output it had.
func appShellQuietSentence(limit time.Duration) string {
	return "Command stopped: it printed nothing for " + strconv.Itoa(int(limit/time.Second)) +
		" seconds. Split it into smaller commands, or run a long one with background:true."
}

// appShellQuietDescription is the clause the bash description carries in the
// app, so the model knows the clock before it meets it.
func (a *Agent) appShellQuietDescription() string {
	if a.foregroundQuietLimit() <= 0 {
		return ""
	}
	return " In this app a foreground command that prints nothing for " + strconv.Itoa(int(a.foregroundQuietLimit()/time.Second)) + "s is stopped."
}

// foregroundQuietLimit is the app's quiet clock for foreground commands, or
// zero outside the app. A test sets [Config.shellQuiet].
func (a *Agent) foregroundQuietLimit() time.Duration {
	if a.config.shellQuiet > 0 {
		return a.config.shellQuiet
	}
	if config.AppMode() && !a.config.InTask {
		return AppShellQuietSeconds * time.Second
	}
	return 0
}

// armForegroundQuiet puts the app's quiet clock on one running foreground call,
// and answers the function that takes it off (nil when nothing was armed).
func (a *Agent) armForegroundQuiet(call *bare.BashCall) func() {
	limit := a.foregroundQuietLimit()
	if limit <= 0 {
		return nil
	}
	return armQuietClock(call, limit, limit/20+time.Millisecond, appShellQuietSentence(limit))
}

// bashBackstopAfter is how long the dispatcher waits for one bash call before it
// answers for it: the first bound the call was going to meet, plus the grace.
func (a *Agent) bashBackstopAfter(args json.RawMessage) time.Duration {
	if a.config.bashBackstop > 0 {
		return a.config.bashBackstop
	}
	// A TASK'S SEARCH IS NEVER HANDED TO A JOB (promote.go), so the handoff
	// clock is not a bound it will meet; its own timeout is.
	backgroundAfter := a.config.BashBackgroundAfterSeconds
	var parsed struct {
		Command string `json:"command"`
	}
	if a.config.InTask && decodeToolArguments(args, &parsed) == nil && commandSearches(parsed.Command) {
		backgroundAfter = 0
	}
	bound := time.Duration(BashBoundSeconds(args, backgroundAfter) * float64(time.Second))
	if quiet := a.foregroundQuietLimit(); quiet > 0 && quiet < bound {
		bound = quiet
	}
	return bound + bashBackstopGrace
}

// bashCallResult is one execution's three answers carried across a goroutine.
type bashCallResult struct {
	text    string
	isError bool
	err     error
}

// executeBashBackstopped runs one bash call under the dispatcher's own clock.
// Any other tool runs exactly as before.
func (a *Agent) executeBashBackstopped(ctx context.Context, callID string, tool bare.Tool, args json.RawMessage, run func() (string, bool, error)) (string, bool, error) {
	if tool.Name != "bash" || keepsRunning(args) {
		return run()
	}
	answered := make(chan bashCallResult, 1)
	guard.Go("session bash backstop", func() {
		text, isError, err := run()
		answered <- bashCallResult{text, isError, err}
	})
	after := a.bashBackstopAfter(args)
	backstop := time.NewTimer(after)
	defer backstop.Stop()
	select {
	case got := <-answered:
		return got.text, got.isError, got.err
	case <-backstop.C:
		a.endStuckBash(callID, after)
		return bashBackstopSentence(after), true, nil
	case <-ctx.Done():
	}
	// THE STOP IS THE TOOL'S TO ANSWER FIRST. It normally does, in well under a
	// second, with what the command had printed; only a call that cannot hear it
	// is answered here.
	abort := time.NewTimer(a.bashAbortAfter())
	defer abort.Stop()
	select {
	case got := <-answered:
		return got.text, got.isError, got.err
	case <-abort.C:
		a.endStuckBash(callID, a.bashAbortAfter())
		return bashAbortedSentence, true, nil
	}
}

// keepsRunning says the call asks for a kept-running service. That road asks a
// person first (keep_running.go), and a person's answer has no clock.
func keepsRunning(args json.RawMessage) bool {
	var parsed struct {
		KeepRunning json.RawMessage `json:"keep_running"`
	}
	return decodeToolArguments(args, &parsed) == nil && len(parsed.KeepRunning) > 0 && string(parsed.KeepRunning) != "null"
}

// bashAbortAfter is [bashAbortGrace], or the test's own figure.
func (a *Agent) bashAbortAfter() time.Duration {
	if a.config.bashAbort > 0 {
		return a.config.bashAbort
	}
	return bashAbortGrace
}

// endStuckBash kills the stuck call's process group, when the call is still
// findable, and writes the goroutine dump that names the hang.
func (a *Agent) endStuckBash(callID string, after time.Duration) {
	if call := a.inFlightBash.find(callID); call != nil {
		call.Kill()
	}
	path := a.writeStuckDump(callID)
	log.Printf("session: bash call %s did not answer within %v; ended by the backstop (stacks: %s)", callID, after, path)
}

// writeStuckDump writes every goroutine's stack to logs/stuck/<call>.txt and
// answers the path, or "" when it could not be written.
func (a *Agent) writeStuckDump(callID string) string {
	directory := droppingsDir(a.config.Place, a.config.Workspace, droppingStuck)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ""
	}
	buffer := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buffer, true)
		if n < len(buffer) || len(buffer) >= 16<<20 {
			buffer = buffer[:n]
			break
		}
		buffer = make([]byte, 2*len(buffer))
	}
	name := callID
	if name == "" {
		name = "call"
	}
	path := filepath.Join(directory, filepath.Base(name)+".txt")
	if err := os.WriteFile(path, buffer, 0o600); err != nil {
		return ""
	}
	return path
}
