package verify

// Taking the photograph.
//
// [Discover] says how a project checks itself; this runs that command and reads
// what it printed. It is deliberately the whole of the machinery a worker needs
// to compare two readings of a repository, because the reason no worker did
// that before was never that the comparison was hard — it was that discovering
// the command lived somewhere only one worker could import.

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/processgroup"
)

// strictPreamble is prefixed to every command this package runs, exactly as
// internal/swepro/codeaf/full_verification.go prefixes its own. A pipeline
// whose first stage fails and whose last stage succeeds exits 0 without it, so
// a suite could go red behind a `tee` and be read as green.
const strictPreamble = "set -euo pipefail\n"

// readingShell is the shell the preamble above is written for, and it is named
// rather than assumed. `pipefail` is not POSIX: on Debian and Ubuntu /bin/sh is
// dash, which answers the preamble with "Illegal option -o pipefail" and exits
// 2 before the suite runs at all. A reading taken through that shell is red
// every time, names nothing, and would have made this whole measurement a
// generator of phantom regressions on the most common Linux there is.
//
// So there is ONE shell here rather than a preamble that means different things
// in different ones, and where it is absent the reading is not taken — A
// CAPABILITY THAT CANNOT WORK IS ABSENT, NOT BROKEN (CLAUDE.md).
const readingShell = "bash"

// capturedOutputLimit is the most of one reading's output this keeps in memory.
//
// It is a TAIL: every runner this package knows prints its failure summary
// last — pytest's "short test summary info", go test's package FAIL lines,
// cargo's failures block — so the last bytes are the ones that name things. The
// figure is derived from the largest suite output measured in the 2026-08-28
// sweep, textual's 391,519-byte verifier log for twenty failing tests with full
// tracebacks: four mebibytes is ten times that, which is room for a suite an
// order of magnitude noisier than the worst one anybody has run here. Past it
// the head is dropped, and dropping the head can only lose a name that the
// summary at the foot repeats.
const capturedOutputLimit = 4 << 20

// Result is one reading of one entrypoint.
type Result struct {
	// Entrypoint is the command that was actually run, carried so a second
	// reading can be taken of the same thing rather than of a re-discovery.
	Entrypoint Entrypoint
	// Exit is the process's own exit status, or -1 when the process never got
	// far enough to have one.
	Exit int
	// TimedOut says the command was killed at the caller's ceiling without ever
	// exiting. A HUNG SUITE IS AN INCOMPLETE OBSERVATION, NOT A RED ONE: it
	// names nothing, so nothing can be subtracted from it and nothing can be
	// attributed to it.
	TimedOut bool
	// Strategy is HOW this reading was taken: the command that ran, the runner
	// underneath it, and the way its output was read. It is carried so the
	// second reading can be taken the same way as the first — two readings
	// taken with two different commands subtract to noise — and so an autopsy
	// of a run that named nothing can see where the reader looked.
	Strategy Strategy
	// ReadAsPlain says the strategy's own reader found nothing it recognised
	// and the shared PASS/FAIL vocabulary read the same bytes instead. It is
	// the fail-safe firing, and it is recorded rather than silent because a
	// roster that came back through the fallback is a roster whose runner did
	// not answer the way this program expected.
	ReadAsPlain bool
	// Uncollected says the runner produced NO TEST RECORD OF ITS OWN: this
	// strategy asked for a machine-readable report and the reader for that
	// format found none in what came back.
	//
	// It is the shape a suite that failed to COLLECT has — an import that will
	// not resolve, a config that will not load, a syntax error in a test file —
	// and it is a different fact from a suite that ran and went red. ofetch's
	// nemotron n1 run hit it four times: `vitest run --reporter=json` printed no
	// JSON, the shared vocabulary scraped one word out of the error text, and a
	// reading naming ONE check was subtracted against a baseline that named 28.
	//
	// Error is the tail of what the runner said instead, so the record carries
	// the reason rather than a count of nothing.
	Uncollected bool
	Error       string
	// Failing is every test identity the runner named, read by [FailingTests].
	Failing []string
	// Reported is every test identity the runner named at all, red or green,
	// read by [ReportedTests]. It is the roster, and it answers the question
	// redness cannot: WHICH CHECKS EXIST. A check in one reading's roster and
	// absent from the next stopped existing between them, which is the one
	// signal that catches a worker deleting the test that was failing it.
	Reported []string
}

// RunTests takes one reading of the project's own checks: it decides HOW the
// reading is taken from what the project declares (see [ReadingStrategy]), runs
// that, and reads the identities out of what it printed.
//
// ok is false when the plan declares no test entrypoint at all. A PROJECT THAT
// DOES NOT SAY HOW IT IS CHECKED IS NOT A PROJECT THIS CAN CHECK.
func RunTests(
	ctx context.Context, workspace string, plan Plan, timeout time.Duration, focus Focus,
) (Result, bool) {
	strategy, ok := ReadingStrategy(workspace, plan, focus)
	if !ok {
		return Result{}, false
	}
	return RunReading(ctx, workspace, strategy, timeout)
}

// RunReading takes one reading with a strategy that is already decided.
//
// It exists separately from [RunTests] because the SECOND reading of a pair must
// be taken exactly as the first was. Re-deriving the strategy there would let a
// worker that edited its own test script change what the after-reading measures,
// which is the tamper the photograph exists to catch, and it would silently
// re-scope a comparison whenever a project's declarations moved under it.
//
// ok is false when the strategy names no command, or when this machine has no
// shell the preamble can be trusted in. A CAPABILITY THAT CANNOT WORK IS ABSENT
// RATHER THAN BROKEN (CLAUDE.md) — the caller gets no reading rather than an
// empty one it would have to tell apart from a green suite.
func RunReading(
	ctx context.Context, workspace string, strategy Strategy, timeout time.Duration,
) (Result, bool) {
	if strategy.Empty() {
		return Result{}, false
	}
	shell, err := exec.LookPath(readingShell)
	if err != nil {
		return Result{}, false
	}

	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	command := exec.CommandContext(ctx, shell, "-c", strictPreamble+strategy.Command)
	command.Dir = filepath.Join(workspace, strategy.Workdir)
	// A suite spawns children — a test server, a browser, a compiler — and
	// killing only the shell leaves them holding the pipe this reading is being
	// read from. The whole group goes, and WaitDelay bounds the wait on the
	// pipes after it does, which is exec's one remaining way to block forever
	// on a process it has already killed.
	processgroup.Configure(command)
	command.Cancel = func() error {
		if command.Process == nil {
			return nil
		}
		return processgroup.Kill(command.Process.Pid)
	}
	command.WaitDelay = 2 * time.Second

	captured := &tailBuffer{limit: capturedOutputLimit}
	command.Stdout = captured
	command.Stderr = captured

	result := Result{
		Entrypoint: Entrypoint{
			Kind: KindTest, Command: strategy.Command,
			Workdir: strategy.Workdir, Source: strategy.Source,
		},
		Strategy: strategy,
		Exit:     -1,
	}
	err = command.Run()
	switch {
	case err == nil:
		result.Exit = 0
	case ctx.Err() != nil:
		// The ceiling fired. Whatever the process said on the way out is not an
		// exit status, and reporting one for a command that never exited is how
		// a hang gets read as a failure with a cause.
		result.TimedOut = true
	default:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.Exit = exitErr.ExitCode()
		}
	}
	output := captured.String()
	reported, failing, read := strategy.Read.Read(output)
	if !read {
		// The strategy's own reader found nothing it recognised. The same bytes
		// are read with the shared vocabulary, which is where every reading in
		// this program started, and the fall is recorded so a roster that came
		// back the long way is legible as such.
		reported, failing = ReportedTests(output), FailingTests(output)
		result.ReadAsPlain = true
		// AND WHERE THE STRATEGY ASKED FOR A MACHINE-READABLE REPORT, ITS
		// ABSENCE IS A FACT AND NOT A ROSTER. A runner told to print JSON prints
		// it whenever it ran its tests at all; no JSON means it never got that
		// far, which is what a collection failure looks like from out here.
		// Whatever the shared vocabulary scrapes out of an error dump is a guess
		// about a suite that did not run, and it was read as one red check
		// against a baseline of twenty-eight.
		if strategy.Read != FormatPlain {
			result.Uncollected = true
			result.Error = runnerTrouble(output)
		}
	}
	result.Failing = failing
	result.Reported = reported
	return result, true
}

// tailBuffer keeps the last capturedOutputLimit bytes written to it and drops
// the rest, so a runaway suite costs a bounded amount of memory rather than
// however much it felt like printing. See capturedOutputLimit for why the tail
// is the half worth keeping.
type tailBuffer struct {
	limit int
	buf   bytes.Buffer
}

func (t *tailBuffer) Write(chunk []byte) (int, error) {
	written := len(chunk)
	if len(chunk) > t.limit {
		t.buf.Reset()
		chunk = chunk[len(chunk)-t.limit:]
	}
	t.buf.Write(chunk)
	if overflow := t.buf.Len() - t.limit; overflow > 0 {
		t.buf.Next(overflow)
	}
	return written, nil
}

func (t *tailBuffer) String() string { return t.buf.String() }

// uncollectedTail bounds how much of a runner's own error travels with the
// reading. Enough for the first thing that went wrong and nothing like a whole
// stack: the reason is one sentence in a record, not a log.
const uncollectedTail = 400

// runnerTrouble is what the runner said instead of a test record.
//
// The first line that carries something a reader would recognise as trouble —
// which is read by SHAPE, as the first non-empty line that is not part of the
// runner's own banner. Nothing here interprets it; it is quoted so a person
// reading the record sees the runner's own words rather than this program's
// summary of a suite it could not read.
func runnerTrouble(output string) string {
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "}") {
			continue
		}
		// A runner's start-up banner names itself and its version and says
		// nothing about what went wrong. It is the one line before the trouble
		// that is always there, and it is skipped by the shape every runner
		// prints it in: a word, a version, and a path.
		if bannerLine.MatchString(trimmed) {
			continue
		}
		if len(trimmed) > uncollectedTail {
			trimmed = trimmed[:uncollectedTail]
		}
		return trimmed
	}
	return ""
}

// bannerLine is a runner announcing itself: `RUN v0.34.6 /app`, `DEV v1.2.3`.
var bannerLine = regexp.MustCompile(`^[A-Z]{2,}\s+v?\d+\.\d`)
