package session

// WHAT MAY BE SPOKEN AS A REMEDY.
//
// fixblame.go asks which FAILURES a memory of fixes may learn from. This file
// asks the other half of the same question, on the way back out: of the strings
// the store has paired with a failure, which one may ever be handed to a model
// as the thing to try next.
//
// The two halves are separate because they fail separately. A failure nothing
// could have fixed teaches the store nonsense; a pairing that is not a command,
// or is a command that merely happened to be typed next, is nonsense the store
// already holds and is about to read back out. A wrong line costs the model a
// step chasing it, which is strictly worse than the silence it replaced.
//
// ── THE TWO MEASURED FAILURES THAT PUT THIS HERE ──
//
// On 2026-08-31, on a real session, the project's file read `{asked: 17,
// found: 3, worked: 0}` and offered these:
//
//	grep, "Path not found: …/test_execution_logger.py"
//	  → "what ran next and it went away: /\/+$"
//	bash, "npm error Missing script: build"
//	  → "what ran next and it went away: git log --oneline -5"
//
// The first is not a command at all: it is the REGEX the next grep searched
// with, filed as a remedy because a pattern is what says what a grep call did
// (loop.go's [glossField]). The second is a command, and it fixed nothing — the
// model looked at the log after npm said no, and the store watched two adjacent
// events and wrote down a cause.
//
// ── THE LAW ──
//
// A HINT IS A COMMAND WITH EVIDENCE BEHIND IT, OR IT IS SILENCE. Two questions,
// both asked of every entry before it may be spoken ([fixEntry.worthSaying]):
//
//	(a) COULD ANYBODY RUN THIS? The first word has to name a program this
//	    machine actually has. A regex, a path, a search pattern and a sentence
//	    all fail that, and so does a command for a toolchain that is not
//	    installed here — which is fixblame.go's law (c) read in the other
//	    direction: routing around an absence is not advice, and neither is
//	    advice that names a program the absence is about.
//	(b) DID THE WORLD SAY IT TWICE? One command follows one failure in every
//	    session that has ever failed at anything, and most of those pairs are
//	    the model typing the next thing it was going to type anyway. A pairing
//	    that RECURS is one the world produced twice; a patch that was offered,
//	    taken, and made the error go away needs no second sighting, because
//	    that observation is a cure rather than an adjacency.
//
// ── WHY NOT [commandLike] OR [approval.Vouchable] ──
//
// This package already has two readings of "is this one command", and both are
// deliberately not used here. Both refuse a COMPOSED line, because both exist to
// answer a safety question — may a standing approval speak for what somebody
// typed (approval/bash.go), may an auditor run this from clean (task_checks.go)
// — and an allow rule cannot vouch for what it has not seen. That is the right
// answer to their question and the wrong one to this: `make clean && make build`
// is the archetypal fix this whole sidecar was built for, and nothing here
// RUNS the patch, it only reads it back to a model that decides for itself.
//
// So what is asked is the smaller question those readings both open with: the
// first word, and whether it names a program.

import (
	"os/exec"
	"strings"
)

// fixRunnableRemedy reports whether a recorded patch is something a person could
// type at a shell on this machine.
//
// It is a lookup and not a parse: the first word of the line, resolved through
// [exec.LookPath], which is the same question the shell itself asks first. A
// word with a slash in it is resolved as a path and has to be an executable
// file, so `/\/+$` and `internal/session/loop.go` both fail it — which is the
// whole of the first measured failure above.
//
// TWO KNOWN COSTS, both silence rather than a wrong line. A patch that opens
// with a shell builtin (`cd /tmp/x && npm test`) is not offered on a machine
// with no `/usr/bin/cd`, and a patch that opens with an environment assignment
// (`CGO_ENABLED=0 go build`) is not offered anywhere. Both are the direction to
// fail in: a `cd` into another session's temp directory is advice that would not
// have worked here anyway.
func fixRunnableRemedy(patch string) bool {
	program := fixProgramOf(patch)
	if program == "" {
		return false
	}
	_, err := exec.LookPath(program)
	return err == nil
}

// fixProgramOf reads the word a patch would run, which is its first field. A
// patch is already collapsed to one line with single spaces on the way into the
// store (fixstore.go's [fixCleanPatch]), so there is nothing else to do to it.
func fixProgramOf(patch string) string {
	fields := strings.Fields(strings.TrimSpace(patch))
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
