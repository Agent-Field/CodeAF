package bare

// EVERY COMMAND IS A STREAM, AND ITS OUTPUT MUST EXIST BEFORE IT EXITS.
//
// This file is one small law with a long reason behind it.
//
// A program that writes to a PIPE rather than a terminal block-buffers its
// output: nothing leaves it until 4KB have piled up or the process exits. That
// is the C library's default and Python's, and it is invisible while commands
// are short. It stops being invisible the moment a command outlives the call
// that made it. A scoring script that takes four minutes and prints a line per
// case wrote NOTHING anybody could read until the last second of it — so the
// job's log sat at zero bytes, every look at it answered `(no output)`, and the
// model reading those answers concluded the work was dead and killed it. Nine
// times in one benchmark. The command was fine; the buffering made it look like
// a corpse.
//
// ── WHY NOT A PTY ──
//
// The textbook fix is to give the command a pseudo-terminal, which is what
// makes every runtime line-buffer at once. It was rejected, and the reason is
// that a tty is not a buffering switch — it is a declaration that a PERSON is
// watching, and programs act on it:
//
//   - `git log` starts a pager and waits forever for a keypress nobody will
//     press.
//   - `ls`, `grep` and half of cargo/npm emit ANSI colour, so every result the
//     model reads is full of escape bytes it has to see past.
//   - the terminal line discipline turns every `\n` into `\r\n`, which changes
//     the bytes of EVERY bash result in the program, golden tests included.
//
// Three regressions across every command, in exchange for a buffering fix that
// two narrower mechanisms deliver with no side effects at all. So this file uses
// those two instead, and neither of them can be observed by the command as
// anything other than "my output is line-buffered".
//
// ── THE TWO MECHANISMS ──
//
//   - `stdbuf -oL -eL` in front of the shell. It is coreutils' own answer, it
//     covers everything that buffers through the C library, and it is inherited
//     by children. It is used only where it is PROVEN to work: [stdbufPrefix]
//     runs it once against `true` and takes it only if that exits clean and
//     silent, because a static binary or a musl system answers a warning on
//     stderr that would then be pasted onto the front of every command's output.
//
//   - `PYTHONUNBUFFERED=1` in the environment. Python does its own buffering
//     above the C library, so `stdbuf` does not reach it — and Python is what
//     the measured defect was actually running. One environment variable, read
//     by every Python since 2.x, and it does nothing to any other program.
//
// A machine with neither is left exactly as it was: this is a capability that
// improves what it can reach and never fails, because a command whose output
// arrives late is still a command that ran.

import (
	"os"
	"os/exec"
	"strings"
	"sync"
)

// MaxResultLines and MaxResultBytes are the caps every tool RESULT in this
// package is bounded by. They are exported so that a caller which has to bound
// output of its own — the sentence a promoted bash call answers with, which is
// the same output the same call would have returned had it finished — bounds it
// by the same two numbers rather than inventing a third.
const (
	MaxResultLines = defaultMaxLines
	MaxResultBytes = defaultMaxBytes
)

// TailForResult bounds text the way a finished bash call's output is bounded:
// the LAST whole lines that fit inside [MaxResultLines] and [MaxResultBytes].
//
// The tail rather than the head, for bash's own reason — the verdict of a
// command is at the end of it — and through the same function, so a partial
// answer and a complete one are cut by one rule.
func TailForResult(text string) string { return truncateTail(text).content }

// StreamingShell is the shell one command runs under, wrapped so its output
// arrives line by line where the machine allows it.
//
// It is the ONE place the shell is chosen for a command whose output somebody
// reads while it runs — the foreground bash tool here, and the background job
// registry in internal/session, which used to keep a hand-copied three-line
// version of the choice and now asks this instead.
func StreamingShell(command string) (string, []string) {
	shell, shellArgs := getShellConfig()
	full := append(append([]string{}, shellArgs...), command)
	if prefix := stdbufPrefix(); len(prefix) > 0 {
		return prefix[0], append(append(append([]string{}, prefix[1:]...), shell), full...)
	}
	return shell, full
}

// StreamingEnv is the environment such a command runs in: the person's own,
// plus the one variable that stops Python holding its output back.
//
// THE PERSON'S OWN SETTING WINS. Somebody who exported PYTHONUNBUFFERED
// themselves — to any value, including an empty one — meant it, and a harness
// that overwrote it would be making a decision about their program that they had
// already made.
func StreamingEnv() []string {
	environment := os.Environ()
	for _, entry := range environment {
		if strings.HasPrefix(entry, "PYTHONUNBUFFERED=") {
			return environment
		}
	}
	return append(environment, "PYTHONUNBUFFERED=1")
}

// stdbufPrefix is the `stdbuf -oL -eL` argv to put in front of a command, or
// nothing at all on a machine where it is missing or does not work.
//
// THE PROBE IS A RUN AND NOT A LOOKUP. stdbuf works by injecting a library into
// the child, and on a system where that injection cannot happen — a static
// build, a libc that is not glibc, a hardened loader — the binary is present,
// exits non-zero or prints a warning, and prepending it would put that warning
// at the top of every command's output for the rest of the session. So it is
// run once against `true`, and it is taken only if it exits clean and says
// nothing at all.
var stdbufPrefix = sync.OnceValue(func() []string {
	path, err := exec.LookPath("stdbuf")
	if err != nil {
		return nil
	}
	probe := exec.Command(path, "-oL", "-eL", "true")
	probe.Env = os.Environ()
	output, err := probe.CombinedOutput()
	if err != nil || len(output) > 0 {
		return nil
	}
	return []string{path, "-oL", "-eL"}
})
