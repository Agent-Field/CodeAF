package head

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// The head's hands, for the two seconds a person would not have thought about.
//
// Everything durable in this product flows through spawn: compile, plan, graph,
// workers, receipt. That is right for work and it was, until this file, the ONLY
// route anything had — so "open signal.html in a browser" either minted a whole
// job (three model calls, a fresh workspace, the wrong directory) or came back as
// "I can't open a browser for you — that's on your side", which is a refusal the
// orchestrator's own voice law forbids and which was said out loud on 2026-08-11
// anyway, because it was true: the head had no hands at all.
//
// THE TOOL IS CALLED bash, AND THE NAME IS THE FIX. It was called `act` for one
// wave, and on 2026-08-12 the same failure came back wearing different words: a
// story sat on disk, the person said "open the file for me", and the head read
// the file with its own eyes and narrated it instead of putting it on their
// screen. The activity trail shows the loop reaching for read, then board, then
// read again — never the hand. `act` is a concept this codebase invented, and an
// invented concept has to be LEARNED from prose on every turn, which puts it in
// permanent competition with the reading tools whose names a model already
// knows. `bash` is not learned. Every model on the market carries an enormous
// prior for a tool by that name, and under that prior "open the file for me"
// resolves to `open <path>` without anything in a prompt having to argue for it.
// The prose could be rewritten forever; the name only had to be right once.
//
// Three things make this a hand rather than a second executor.
//
//   - The boundary is TIME AND CONSEQUENCE, never topic. There is no list of
//     blessed programs here and there must never be one: `open` is a command like
//     any other, and hardcoding the use cases we happen to have seen is exactly
//     the emergent-capability failure this codebase spent a year unlearning. What
//     is stated is the shape of the thing — one command, whose consequences the
//     gate below can see — and the one principle it belongs to is stated once in
//     the prompt, in the only register that generalises: the hands differ by
//     WHOSE WORLD they change. A read changes what the head knows. This changes
//     what the person experiences, on their machine, right now. Work changes the
//     world durably. Nothing here names an occasion, and nothing here may.
//   - The RESULT is the ground truth. The exit status and the captured output come
//     back as the tool result and the loop narrates that and nothing else. This is
//     not tidiness: the head once said "The site is open — I've launched it" over a
//     job that had found nothing, and a hand whose outcome the model infers rather
//     than reads would have made that hallucination cheaper rather than rarer.
//   - The floor is code, not prose. A description is a persuasion and persuasion
//     is not a membrane, so the same consequence gate that refuses to let anything
//     irreversible ride a reflex (spawn.go) is asked about this command's words
//     too, and a tiny principled list of self-escalating spellings is refused
//     beside it. Both refusals REDIRECT: they hand back the sentence that says to
//     spawn it instead, because a person who asked for something is owed the route
//     that can do it rather than a wall. The gate did NOT move when the bounds
//     below widened, and it is the reason they safely could.

const (
	// bashWindow is how long one command may take. It is the default rather than
	// a constant on the call so a test can shorten it — see bashWindowOf.
	//
	// It was ten seconds while the tool was called `act`, because ten seconds is
	// an order of magnitude past "two seconds without thinking" and the name was
	// making a claim about instantness. The name makes no such claim now, and a
	// ten-second wall was cutting off ordinary shell work — a build's first
	// output, a checkout, a directory walk over a real tree — which sent the loop
	// back to commissioning a whole job for something a person would have waited
	// out at their own keyboard. A minute is the honest ceiling: a turn is
	// seconds-to-a-minute, and anything slower than that is work whether or not
	// the model agrees, so the wall is still a wall.
	bashWindow = 60 * time.Second
	// bashWaitDelay bounds the wait AFTER the deadline kills the command. Killing
	// a shell leaves any child of it still holding the write end of the pipe, and
	// CombinedOutput waits on the pipe rather than on the process it killed —
	// which is how a wall becomes a conversation that stopped answering.
	// terrain.go learned this the same way.
	bashWaitDelay = 500 * time.Millisecond
	// bashOutputBytes bounds what comes back, and it is a PAGE rather than a
	// ceiling on truth: past it the result says out loud that it was cut. Sixteen
	// kilobytes rather than four because the output of a real command is evidence
	// the loop has to reason over, and a four-kilobyte clip turned the tail of a
	// test run or a diff into something the head confidently described from its
	// first page. The raw-in rule holds either way — what is never allowed is a
	// silent clip, and bashOutputCut is why there cannot be one.
	bashOutputBytes = 16 << 10
	// bashCommandBytes bounds the command itself. It is a transport bound and no
	// longer a judgment: "longer than this is a script, and a script is work" was
	// a rule at 400 characters, and a rule that fires on a length is a rule that
	// fires on quoting a path and a long flag. Two kilobytes is past anything a
	// person types in one line, so what is left of the judgment — is this work? —
	// is the model's, made against the prompt's boundary, with the consequence
	// gate underneath it either way.
	bashCommandBytes = 2 << 10
)

// bashOutputCut is what a clipped result says. It is a sentence rather than an
// ellipsis because the loop reads this and then speaks: a cut it cannot see is a
// cut it will describe as the whole.
const bashOutputCut = "\n[output cut here — the rest was not read]"

// bashSpawnInstead is what both refusals say. It is one sentence rather than two
// because the loop's next move is the same either way, and it names spawn out
// loud so a model that reads the refusal already knows the route.
const bashSpawnInstead = " — that is not one reversible command, it is work the person gets to see coming. Commission it with task, in their own words."

// bashWindowOf is how long this head gives one command. The field is unset in
// the running product, where the constant is the answer; it exists so a test can
// prove the ceiling without spending a minute proving it.
func (h *Head) bashWindowOf() time.Duration {
	if h != nil && h.bashWindow > 0 {
		return h.bashWindow
	}
	return bashWindow
}

// bash runs one shell command in the workspace and hands back what happened.
//
// It returns failure only for refusals — an empty command, a gated one, a
// workspace it cannot reach. A command that RAN and exited non-zero is not a
// failure of this tool: it is a fact about the world, and the loop needs to read
// it as one rather than as an error to apologise for.
func (run *beltRun) bash(args map[string]any) (string, bool) {
	command := strings.TrimSpace(beltString(args, "command"))
	if command == "" {
		return "command must be the one shell command to run, exactly as it would be typed", true
	}
	if len(command) > bashCommandBytes {
		return fmt.Sprintf("that is %d characters of shell, past the %d this tool carries — something that long is work: commission it with task",
			len(command), bashCommandBytes), true
	}
	if reason, gated := bashGated(command); gated {
		return reason + bashSpawnInstead, true
	}
	root, err := run.head.workspaceRoot()
	if err != nil {
		return err.Error(), true
	}

	ctx, cancel := context.WithTimeout(context.Background(), run.head.bashWindowOf())
	defer cancel()
	shell := exec.CommandContext(ctx, "sh", "-c", command)
	shell.Dir = root
	shell.WaitDelay = bashWaitDelay
	output, runErr := shell.CombinedOutput()

	// The receipt is written from what happened and never from what was asked
	// for. 5.20's rule that prose turned into work is never a silent side effect
	// does not care that this one journaled nothing: it RAN.
	run.record(0, "Ran "+truncateBytes(firstLine(command), bashReceiptBytes)+".")
	return bashResult(command, root, output, runErr, ctx.Err() != nil, run.head.bashWindowOf()), false
}

// bashReceiptBytes keeps one command to a clause in the receipt. The result text
// carries the whole of it; the receipt only has to be recognisable.
const bashReceiptBytes = 80

// bashResult is the ground truth, said in the words the loop may repeat. Exit
// status first, because whether it worked is the answer; then the output,
// because what it said is the evidence for it.
func bashResult(command, root string, output []byte, runErr error, timedOut bool, window time.Duration) string {
	captured := strings.TrimRight(string(output), "\n")
	if len(captured) > bashOutputBytes {
		captured = truncateBytes(captured, bashOutputBytes) + bashOutputCut
	}
	var verdict string
	switch {
	case timedOut:
		verdict = fmt.Sprintf("`%s` was still running after %s and was stopped. It is not one command's worth of time; if the person wants it done, commission it with task",
			command, window)
	case runErr == nil:
		verdict = fmt.Sprintf("`%s` ran in %s and exited 0", command, root)
	default:
		status := "did not run"
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) {
			status = fmt.Sprintf("exited %d", exitErr.ExitCode())
		}
		verdict = fmt.Sprintf("`%s` ran in %s and %s: %s", command, root, status, runErr.Error())
	}
	if captured == "" {
		return verdict + ". It printed nothing. Say what actually happened and claim nothing beyond it"
	}
	return verdict + ". What it printed:\n" + captured +
		"\n\nSay what actually happened and claim nothing beyond it"
}

// bashGated is the floor under the description, and it is a floor rather than a
// filter: it names only what is irreversible or self-escalating by construction,
// and everything about how small or obvious a command is remains the model's
// judgment made against the tool's own boundary.
//
// The first half is the consequence gate the reflex path already uses (head.go),
// asked of the command's words. It is the same membrane for the same reason —
// buying, sending, publishing, deleting beyond the workspace is ordinary work a
// person gets to see coming, whether it arrives as a sentence or as a shell line.
//
// The second half is the handful of spellings that escalate out of the boundary
// no matter what the rest of the line says. It stays tiny on purpose: a growing
// list of forbidden programs is a topic filter wearing a safety costume, and the
// road it is on has no end.
//
// Nothing in here moved when the tool was renamed and its bounds widened. That
// is the whole reason the bounds could widen: the gate is code, the bounds are
// transport, and only one of the two was ever what kept a reflex honest.
func bashGated(command string) (string, bool) {
	if consequenceGated(command) {
		return "that command's own words say it spends, sends, publishes or deletes beyond the workspace", true
	}
	lower := strings.ToLower(command)
	fields := strings.Fields(lower)
	first := ""
	if len(fields) > 0 {
		first = fields[0]
	}
	switch {
	case first == "sudo" || first == "doas" || strings.Contains(lower, " sudo "):
		// Asking for a power the conversation does not have is not one ordinary
		// command by definition: whatever needs it is a change nobody can take back.
		return "that command escalates privileges", true
	case first == "shutdown" || first == "reboot" || first == "halt" || first == "poweroff":
		return "that command takes the machine down", true
	case bashRecursiveRemoval(fields):
		return "that command recursively removes a directory outside this workspace", true
	}
	return "", false
}

// bashRecursiveRemoval is the one destructive spelling worth naming, because it
// is the one the consequence gate cannot see: `rm` is not a word anybody says in
// a sentence, so the sentence gate never learned it. Recursive removal aimed at
// the root or at a home directory is refused; an ordinary rm is not, because
// deleting a file inside the workspace is exactly the reversible small thing
// this tool exists for and the gate above already catches the paths that are not.
func bashRecursiveRemoval(fields []string) bool {
	if len(fields) == 0 || (fields[0] != "rm" && fields[0] != "rmdir") {
		return false
	}
	recursive := fields[0] == "rmdir"
	for _, field := range fields[1:] {
		if strings.HasPrefix(field, "-") && strings.ContainsAny(field, "rR") {
			recursive = true
		}
	}
	if !recursive {
		return false
	}
	for _, field := range fields[1:] {
		field = strings.Trim(field, `"'`)
		if strings.HasPrefix(field, "-") {
			continue
		}
		if field == "/" || field == "~" || field == "~/" || field == "$HOME" ||
			strings.HasPrefix(field, "/") || strings.HasPrefix(field, "~/") {
			return true
		}
	}
	return false
}
