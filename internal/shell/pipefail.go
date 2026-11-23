package shell

import (
	"slices"
	"strings"
)

// PipefailArgs is the argument list, after the program, that every bash door
// runs a command with: pipefail on, so a failing stage cannot hide behind a
// succeeding reader, and one exception to it.
//
// The exception is a reader that stopped early. In `git log | head` the reader
// exits once it has its lines and the writer dies of SIGPIPE; pipefail alone
// reports that 141 and the whole command reads as failed. The shell itself is
// the only place that knows each stage's status, so the command runs behind a
// short EXIT trap that reads PIPESTATUS of the last pipeline and, when a stage
// other than the last exited 141 (the status SIGPIPE leaves), recomputes
// pipefail with those stages counted as successes. Every other status stands: a real failure in any stage
// still wins, and a last stage that failed is still the answer.
//
// It declines to judge when it cannot read the statuses honestly. Under
// errexit the shell may have stopped part-way, so the trap leaves the status
// alone. A pipeline whose 141 decided an && or || list is left alone too:
// counted as a success, it would have run commands that never ran. A status
// that is not the pipefail value of the recorded stages (an explicit exit, a
// subshell, a function) also stands. A command that sets its own EXIT or ERR
// trap replaces the check and gets today's status. A command with no
// `|` cannot hold a pipeline and runs exactly as written, which keeps bash's
// exec-in-place for a single command. The prefix sits on the command's own
// first line so error messages keep their line numbers, and the trap's own
// trace is sent to /dev/null so a command's `set -x` output is unchanged.
//
// sh has no pipefail and no PIPESTATUS, so for it the command passes through
// untouched, as it always has.
func PipefailArgs(program string, flags []string, command string) []string {
	if program == "sh" {
		return append(slices.Clone(flags), command)
	}
	if strings.Contains(command, "|") {
		command = earlyReaderPrefix + command
	}
	return append(append([]string{"-o", "pipefail"}, flags...), command)
}

// earlyReaderPrefix defines the status check and arms it with two traps.
//
// The ERR trap records the stage statuses of each pipeline whose failure ended
// a list. Bash fires ERR only for a failure nothing else consumed, so a
// pipeline followed by && or || never sets the mark: in `yes | head -1 &&
// false` the reader's 141 skipped the `false`, and forgiving it would report a
// success the command never reached.
//
// The EXIT trap passes the last pipeline's stage statuses followed by the
// shell's exit status. raw is pipefail's own answer from those stages and
// eased is the same answer with a non-final SIGPIPE counted as success. It
// exits with eased only when errexit is off, the statuses are the ones the ERR
// trap marked (the mark starts empty, so a command under nounset can read
// it), and raw is the status the shell is really leaving with. Each
// return names its status, because a bare return inside a trap hands back the
// command's own status and would fire the command's ERR trap a second time.
const earlyReaderPrefix = `__codeaf_early_reader() { ` +
	`local -a s=("$@"); local n=$((${#s[@]} - 1)) raw=0 eased=0 i; ` +
	`[[ $- == *e* || "${s[*]:0:n}" != "$__codeaf_marked" ]] && return 0; ` +
	`for ((i = 0; i < n; i++)); do ` +
	`((s[i])) && raw=${s[i]}; ` +
	`((s[i] && (s[i] != 141 || i == n - 1))) && eased=${s[i]}; ` +
	`done; ` +
	`((raw == s[n] && eased != raw)) && exit "$eased"; return 0; ` +
	`}; __codeaf_marked=; trap '{ __codeaf_marked="${PIPESTATUS[*]}"; } 2>/dev/null' ERR; ` +
	`trap '{ __codeaf_early_reader "${PIPESTATUS[@]}" "$?"; } 2>/dev/null' EXIT; `
