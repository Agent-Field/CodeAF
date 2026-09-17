package main

// The execute() lifecycle for the anonymous usage counts: which invocation is
// a session, which mode it ran in, when the notice is printed, and how a run
// ends in the contract's stop-reason vocabulary. Everything here is driven
// from execute() (main.go), the one exit every command leaves through, so no
// door can be missed by a counter that would then miscount the runs it was
// built to count.

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/telemetry"
	"github.com/Agent-Field/codeaf/internal/trace"
)

// telemetryConfiguredOff is the config's one answer to the ladder, read the
// same way the `telemetry status` verb reads it. An unreadable config is the
// ladder's default — on — never a failure the run inherits: `codeaf version`
// still owes its answer on a machine with no profile, and a config error has
// never been a reason to refuse one.
func telemetryConfiguredOff() bool {
	cwd, err := os.Getwd()
	if err != nil {
		return false
	}
	off, _ := config.TelemetryOffReason(cwd, config.ProfileDir())
	return off
}

// telemetrySession is what execute() needs to close one run: the mode it
// decided, whether it was a resume, and the id the run's events are hashed
// from. A nil mode means this invocation is not a session at all (version,
// help, doctor, the rest) and nothing is spooled for it.
type telemetrySession struct {
	mode      telemetry.Mode
	resumed   bool
	sessionID string
}

// telemetryBegin decides, from the command line alone, whether this
// invocation is a session worth counting, and spools its opening events.
//
// Only session commands emit: chat and resume are "chat", do, exec, run and
// `plan run` are "task", everything else emits NOTHING and sends NOTHING.
// The mode is read from os.Args here rather than handed down from each door
// because a door that had to remember it would be a door that could forget
// it — and the doors differ only in the word they call themselves.
//
// THE NOTICE: printed once per install, to stderr, before the first session's
// events are ever sent — but never into a --json stdout, and never into a
// pipe: a person who cannot see stderr is a person who cannot be asked, so
// the events wait in the spool until a session that can show the notice runs.
// A task command is always shown it, because a task runs unattended and its
// person may never open a chat at all.
func telemetryBegin() telemetrySession {
	args := os.Args[1:]
	mode, resumed, session := telemetryMode(args)
	if !session {
		return telemetrySession{}
	}
	id := telemetrySessionID()
	// A run the ladder has turned off prints no notice either: the notice is
	// the sentence that asks permission to send, and a pipe that will never
	// send has nobody to ask — and marking it shown would create the very
	// directory this run promised not to write.
	if telemetry.Enabled() && !telemetry.NoticeShown() && !telemetryHasJSON(args) &&
		(mode == telemetry.ModeTask || stderrIsTerminal()) {
		telemetry.PrintNotice()
		telemetry.MarkNoticeShown()
	}
	// Both opening events go through SpoolSync, not the fire-and-forget Spool:
	// first_run must be on disk before session_started even exists, and a run's
	// session_started must be spooled before the process can reach the exit and
	// flush — an append on its own goroutine can land after the flush has
	// already renamed the spool, or not at all when a short run exits first.
	//
	// Each constructor is reached only when the ladder is on: the event's own
	// identity mints and writes the install id, so building one under a run
	// that will not send would touch the state root this run promised to leave
	// alone — the same reason telemetryEnd gates its constructor.
	if telemetry.Enabled() && telemetry.FirstRunPending() {
		_ = telemetry.SpoolSync(telemetry.FirstRun(time.Now()))
		telemetry.MarkFirstRunSent()
	}
	if telemetry.Enabled() {
		_ = telemetry.SpoolSync(telemetry.SessionStarted(mode, resumed, id, time.Now()))
	}
	return telemetrySession{mode: mode, resumed: resumed, sessionID: id}
}

// telemetryEnd closes one run at the exit, from the process's own tally
// (telemetry.Snapshot), the time it took, the exit code the run produced, and
// the stop reason the exit code speaks in the contract's vocabulary.
//
// The SessionEnded event goes through SpoolSync and then Flush with a context
// deadline of exactly one second — the whole of what a telemetry failure may
// ever add to a run. Both are no-ops when the ladder is off, which is what
// keeps this defer free for every non-session command: `codeaf version` runs
// it and it costs a mode read and nothing else.
func telemetryEnd(session telemetrySession, code int) {
	// Nothing is built when the ladder is off, and that is load-bearing: the
	// event's identity alone reads the install id, which mints and writes one
	// on a machine that has never sent anything — the very file this run
	// promised not to write. SpoolSync and Flush are no-ops under the same
	// rung, so this gate is what keeps a disabled run from touching the state
	// root at all.
	if session.mode == "" || !telemetry.Enabled() {
		return
	}
	// The run's own tally comes from Snapshot and only the three facts the
	// counters cannot know — how long it took, why it ended, what it left
	// with — are set on top of it.
	stats := telemetry.Snapshot()
	stats.Duration = time.Since(telemetryStarted)
	stats.StopReason = telemetryStopReason(code)
	stats.ExitCode = code
	event := telemetry.SessionEnded(session.mode, stats, session.sessionID, time.Now())
	_ = telemetry.SpoolSync(event)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = telemetry.Flush(ctx)
}

// telemetryInterrupted is whether the person ended the run themselves —
// ctrl-c on the surface, the signal a headless run caught. It is set where
// the signal is caught (do.go's errand), and read once, here, to give the
// session-ended event the contract's "interrupted" word instead of a failure
// word it was not.
var telemetryInterrupted bool

// telemetryStarted is when this process began its session, the honest measure
// of a run's duration: the wall time from the first event to the exit, not a
// guess at when the model first answered.
var telemetryStarted = time.Now()

// telemetrySessionID is the run id the events are hashed from, in preference
// order: the id trace minted at the door (chat, do, exec and `plan run` all
// call openDebugRecord, which calls trace.Begin), the process's own trace run
// where a door did not mint one, and a fresh random id where this process
// never minted a run at all. The package hashes whatever it is handed.
func telemetrySessionID() string {
	if id := trace.RunFrom(context.Background()); id != "" {
		return id
	}
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(raw[:])
}

// telemetryStopReasons maps the exit ladder's rungs (envelope.go) onto the
// contract's stop_reason vocabulary. It is a straight table keyed by the rung
// itself: the five codes the ladder names, each with the one word the exit
// cannot carry. exitLimit is the ladder's single rung for four limits — wall,
// budget, turn cap, price — and the contract gives the rung a single word,
// "budget", which is the one it names for a limit the person set.
var telemetryStopReasons = map[exitStatus]string{
	exitDone:       telemetry.StopDone,
	exitCannotRun:  telemetry.StopError,
	exitIncomplete: telemetry.StopIncomplete,
	exitLimit:      telemetry.StopBudget,
	exitUnanswered: telemetry.StopQuestion,
}

// telemetryStopReason maps the exit code onto the contract's stop_reason
// vocabulary. A code the ladder does not name is "unknown" — the honest
// answer, because the fact that the run ended survives even when the
// vocabulary does not.
//
// A user interrupt is not on the ladder: interrupting a chat is not a failure
// of the conversation, it is the person ending it, and the contract gives it
// its own word. The interrupt path is the one place this build asks a signal
// directly rather than the exit code, because the person's keypress and the
// process's exit are two events and only one of them names why.
func telemetryStopReason(code int) string {
	if telemetryInterrupted {
		return telemetry.StopInterrupted
	}
	if reason, ok := telemetryStopReasons[exitStatus(code)]; ok {
		return reason
	}
	return telemetry.StopUnknown
}

// stderrIsTerminal is whether a person can see the notice: stderr attached to
// a terminal. A redirected or piped stderr is the CI case, and CI is a rung
// of the ladder's own — but the notice's rule is narrower than that, because
// `codeaf do 2>/dev/null` in a person's own script is not CI and still must
// not spend the one line the person will never read.
func stderrIsTerminal() bool {
	return stdinIsTerminal(os.Stderr)
}
