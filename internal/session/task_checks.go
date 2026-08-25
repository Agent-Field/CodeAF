package session

// WHAT THE CHECKER MAY RUN, AND WHO GETS TO SAY.
//
// ── THE LAW ──
//
// THE AUDITOR VERIFIES THE WORK BY THE CHECKS THE WORK ITSELF NAMES, NOT BY A
// LIST THE HARNESS KNOWS. There are exactly three sources for the commands one
// audit may run, and none of them is a language, a toolchain, or a build system
// this file has heard of:
//
//	(a) THE CHECK THE WORK DECLARES — every command the node's own document
//	    names, read out of the brief and the frozen acceptance the node was
//	    finished against ([declaredChecks]).
//	(b) THE CHECK THE WORK RAN — every command the last worker itself ran as one
//	    command, read off its own tool receipts ([ranChecks]).
//	(c) THE ALWAYS-SAFE READING COMMANDS — the ones that print and cannot change
//	    the thing under judgement ([auditReadCommands]). They are not verification
//	    and they are here anyway, for the reason stated on the list itself.
//
// Everything else is refused, and the refusal NAMES what this audit may run, so
// a model that reached for the wrong door reads the right one in the same
// breath (task_audit.go's [refuseOutsideAllowlist]).
//
// ── THE MEASURED FAILURE THAT PUT IT HERE ──
//
// The allowlist used to be a constant: `go test`, `go build`, `go vet`, and
// git's read-only reporting. On a live SWE-Marathon run — a Rust deliverable, in
// a container with no git on the PATH — every one of those was either wrong or
// absent. The auditor reached for the project's own check, the one the task
// itself names, and was told:
//
//	refused: bash run_tests.sh is not verification, and an auditor only runs
//	verification. You may run: go test, go build, go vet, git diff, git log,
//	git status, git show, pwd, wc, head, cat
//
// It could not run the build either. So it read source files until its five
// minutes ran out — 21 calls, 300 seconds, six cents — and the node landed on
// "no answer in 5m0s, so nothing was accepted", which is "finished, but needs
// your look", which is a run stalled waiting for a person. That happened on
// EVERY ONE of the six audits of that run's main task. A gate that can only
// verify one language is not a gate, it is a coincidence.
//
// ── WHY THE SAFETY ARGUMENT DOES NOT MOVE ──
//
// The auditor still has no hand that writes, and its bash still runs ONE command
// with no shell composition. What changed is where the list of commands comes
// from, not what a command may be. That matters most for (b): a worker's line
// like `cd x && cargo build 2>&1 | tail -5` is NOT a door, because it cannot be
// re-run without composing it, and this gate does not compose. A check is
// re-run VERBATIM AS THE WORK RAN IT or it is not re-run at all — which is also
// why a command a blanket-allow policy would still stop and ask about
// (internal/approval's critical table) never becomes a door however the work
// spelled it.

import (
	"strings"
	"time"

	"github.com/Agent-Field/aforge-v2/internal/approval"
)

const (
	// auditCheckCount bounds how many checks one source may contribute. The
	// door is read by a model — it is interpolated into the bash description
	// and into every refusal — and a list of forty commands is a paragraph
	// nobody parses. Eight from the work's own words and eight from what it ran
	// is more of both than any real check needs, and it keeps the refusal
	// something a model reads rather than skims.
	auditCheckCount = 8

	// auditReadingDeadline bounds an audit that HAS NOTHING TO RUN.
	//
	// [auditDeadline] is five minutes because a real check is slow: it is a full
	// test run on a real repository plus the reading around it. An audit with no
	// runnable check has no slow half at all — its whole cost is reading files
	// and writing one answer — so five minutes there buys nothing except the
	// exact failure this file was written for: an auditor with no door open to
	// it, spending the person's money until the clock says nobody answered.
	//
	// IT IS DERIVED FROM WHETHER THERE IS A CHECK, NEVER FROM HOW BIG THE WORK
	// IS. A rule that read the size of the tree would be a rule that is wrong on
	// the next tree; this one asks the only question that bounds the cost.
	auditReadingDeadline = auditDeadline / 5
)

// auditReadCommands is the third source: commands that PRINT and cannot change
// the thing under judgement.
//
// They are not verification and they are on the belt anyway, because refusing
// them cost a verdict once: an auditor that cannot ask where it is standing
// spends its steps finding out the hard way, and an audit that died in the wild
// burned two of them on a refused `pwd`. The safety argument the belt rests on
// is about what can CHANGE the thing under judgement, not about which program
// prints it.
//
// NOTHING HERE NAMES A LANGUAGE OR A TOOLCHAIN, and nothing here ever may. The
// moment this list learns what a Go repository or a Rust one looks like, it is
// the constant that was measured failing at the top of this file.
var auditReadCommands = []string{
	"git diff",
	"git log",
	"git status",
	"git show",
	"pwd",
	"wc",
	"head",
	"cat",
}

// auditAllowed is the policy [ranChecks] asks about a command the work ran, and
// it asks the one question that is not this package's to answer: would a gate
// that had been told to allow everything STILL stop and put this command to a
// person?
//
// That is internal/approval's critical table (bash.go), which is the build's one
// floor under a blanket allow and is deliberately short — the handful of shapes
// that destroy a disk or drop the machine. Asking it here rather than writing a
// second table means the auditor's door and the person's gate can never disagree
// about what is critical, which is the drift a second copy guarantees.
var auditAllowed = approval.Policy{Default: approval.ActionAllow}

// auditDoor is what ONE audit may run: the checks it found, and the whole
// allowlist those checks sit at the front of.
//
// THE TWO FIELDS ARE NOT THE SAME QUESTION. `allowed` is what the gate matches
// against. `checks` is whether this audit can VERIFY anything at all — an audit
// holding nothing but the reading commands can look at the work and cannot test
// it, and that changes both what it is told and how long it is given
// ([auditDoor.window], [auditDoor.line]).
type auditDoor struct {
	// checks are the commands the work named or ran, declared first.
	checks []string
	// allowed is checks followed by [auditReadCommands] — the order matters,
	// because it is the order a refusal lists them in and the check is the thing
	// the auditor came for.
	allowed []string
}

// auditDoorFor reads one node's door off the node itself: what its own document
// declares, and what its last worker actually ran.
//
// A NIL NODE STILL GETS THE READING COMMANDS. Every caller here has a node, but
// a door with no allowlist at all would be a bash that refuses everything, and a
// belt whose hand refuses everything is a hand this build would not have put on
// (CLAUDE.md's absent-not-broken law).
func auditDoorFor(node *TaskNode) auditDoor {
	var checks []string
	if node != nil {
		checks = appendChecks(checks, declaredChecks(node.instruction()))
		checks = appendChecks(checks, ranChecks(node.lastReceipts()))
	}
	allowed := make([]string, 0, len(checks)+len(auditReadCommands))
	allowed = append(allowed, checks...)
	allowed = append(allowed, auditReadCommands...)
	return auditDoor{checks: checks, allowed: allowed}
}

// window is how long this audit gets. See [auditReadingDeadline] for why the
// answer turns on whether there is a check and on nothing else.
func (d auditDoor) window() time.Duration {
	if len(d.checks) == 0 {
		return auditReadingDeadline
	}
	return auditDeadline
}

// line is what the auditor is TOLD about its own door, and it is written for the
// two cases separately because they are different jobs.
//
// With a check in hand, the auditor is pointed at it: these are the work's own
// checks, run them, they are the whole reason a verdict is worth anything. With
// nothing runnable it is told SO, plainly, and told to judge from reading and
// answer — because the failure this file exists for is an auditor that kept
// reaching for a door that was never going to open, and a model that has not
// been told there is no door will keep reaching for one.
func (d auditDoor) line() string {
	if len(d.checks) == 0 {
		return "NOTHING THIS WORK DECLARES OR RAN IS A CHECK YOU CAN RE-RUN. Your bash will run only " +
			strings.Join(auditReadCommands, ", ") + ", none of which verifies anything. Do not go looking for a " +
			"command to run: read the files and the change, judge what you can see, and answer now. " +
			"An answer from reading alone is a real answer; running out of time is not.\n"
	}
	return "THE CHECKS THIS WORK NAMES OR RAN, which are the only verification commands your bash will run:\n" +
		"  " + strings.Join(d.checks, "\n  ") + "\n" +
		"Run them as they are written. Anything else is refused, and the refusal will say what you may run.\n"
}

// appendChecks folds one source's commands into the door, keeping the order
// they were found in, never listing one twice, and stopping at
// [auditCheckCount] for that source.
func appendChecks(checks, more []string) []string {
	seen := make(map[string]bool, len(checks))
	for _, command := range checks {
		seen[command] = true
	}
	added := 0
	for _, command := range more {
		if seen[command] || added >= auditCheckCount {
			continue
		}
		seen[command] = true
		checks = append(checks, command)
		added++
	}
	return checks
}

// declaredChecks is source (a): every command the work's OWN DOCUMENT names.
//
// IT READS THE TWO CONVENTIONS PROSE HAS FOR NAMING A COMMAND and no others: a
// span in backticks, and a line that opens with a shell prompt. Both are how a
// person, a planner or a benchmark writes down "this is the thing to run", in
// every language there is, which is exactly why they are the ones read here — a
// rule that looked for a build system's name would be the constant this file
// replaced, wearing a regexp.
//
// A backtick span that is not a command comes back as nothing rather than as a
// door: the filter is [commandLike], and a path, an option or a sentence fails
// it. Some noise survives — a brief that backticks a filename gets that filename
// on the list — and that is the right way to be wrong. A dead entry costs a line
// of a refusal; a missing check costs a verdict.
func declaredChecks(text string) []string {
	var out []string
	// The odd-numbered pieces of a split on the backtick are what was BETWEEN a
	// pair of them. A fenced block splits into empty pieces around its own
	// content, and the content itself carries newlines, so both fail
	// [commandLike] on their own without a special case for fences.
	spans := strings.Split(text, "`")
	for index := 1; index < len(spans); index += 2 {
		if command, ok := commandLike(spans[index]); ok {
			out = append(out, command)
		}
	}
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if !strings.HasPrefix(line, "$ ") {
			continue
		}
		if command, ok := commandLike(strings.TrimPrefix(line, "$ ")); ok {
			out = append(out, command)
		}
	}
	return out
}

// ranChecks is source (b): what the last worker ITSELF ran, read off the
// receipts the node already carries for the auditor's packet (task_audit.go's
// [lastToolReceipts]).
//
// ONLY WHAT IT RAN AS ONE COMMAND. A worker's `cd x && build 2>&1 | tail` is not
// a door: this gate runs one command with no composition, so a composed line
// could only be re-run by taking it apart, and a check taken apart is not the
// check that ran. [approval.Vouchable] is the build's existing answer to "is
// this one simple command" — the same question a standing approval has to ask
// before it may speak for a line somebody typed — and asking it here means the
// two can never drift apart.
//
// AND NOT WHAT A BLANKET ALLOW WOULD STILL ASK ABOUT ([auditAllowed]). The work
// ran with hands this auditor does not have; the fact that it ran something is
// not a reason to hand the judge a way to destroy the tree it is judging.
func ranChecks(receipts []toolReceipt) []string {
	var out []string
	for _, receipt := range receipts {
		command := strings.TrimSpace(receipt.command)
		if command == "" || !approval.Vouchable(command) {
			continue
		}
		if auditAllowed.CheckBash(command).Action != approval.ActionAllow {
			continue
		}
		if command, ok := commandLike(command); ok {
			out = append(out, command)
		}
	}
	return out
}

// commandLike decides whether a fragment of text is a command this door could
// ever open for, and normalizes the ones that are.
//
// IT IS A SHAPE TEST AND NOT A VOCABULARY TEST. It knows nothing about which
// programs exist; it asks whether what it is holding could be typed at a shell
// as one command:
//
//   - no shell composition, which is the gate's own standing law
//     ([shellComposition]) asked one step earlier;
//   - a first word that is a program rather than an option, because a brief
//     backticking `--stdio` is naming a flag and not a check;
//   - a first word with something in it besides wildcards, because a door
//     spelled `*` is not a door, it is an open wall;
//   - short enough to be read back inside a refusal, since a command nobody can
//     read in the door's own list is a command nobody will type.
//
// Whitespace is normalized for the reason the gate normalizes it: "go  test" and
// "go test" are one command, and the door is about which program runs rather
// than about how it was typed.
func commandLike(text string) (string, bool) {
	text = strings.TrimSpace(text)
	if text == "" || strings.ContainsAny(text, shellComposition) {
		return "", false
	}
	fields := strings.Fields(text)
	if len(fields) == 0 {
		return "", false
	}
	program := fields[0]
	if strings.HasPrefix(program, "-") || strings.Trim(program, "*?[]") == "" {
		return "", false
	}
	normalized := strings.Join(fields, " ")
	if len(normalized) > auditCommandLimit {
		return "", false
	}
	return normalized, true
}
