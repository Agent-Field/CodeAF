package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"strings"
	"testing"
)

// checkedNode builds a node the way the graph tests do — a spec and a brief,
// with no executor behind it — because everything under test here is read off
// the node's own document and off the receipts it carries.
func checkedNode(brief, acceptance string, receipts ...toolReceipt) *TaskNode {
	graph := &TaskGraph{nodes: map[uint64]*TaskNode{}}
	node := &TaskNode{graph: graph, id: 1, spec: taskSpec{
		title:      "the deliverable",
		request:    "build the thing",
		brief:      brief,
		acceptance: acceptance,
	}}
	node.brief = brief
	node.receipts = receipts
	return node
}

// THE DOOR IS THE CHECK THE WORK NAMED, AND IT IS THAT CHECK AND NOT ANOTHER.
//
// This is the measured failure in one assertion. A task whose own words say the
// check is `bash run_tests.sh` had that exact command refused, with a list of Go
// verbs offered instead, on every one of six audits — so the auditor could
// verify nothing, spun for the whole deadline, and the node landed needing a
// person. The declared check now opens, and nothing next to it does.
func TestTheCheckerRunsTheCheckTheWorkDeclaredAndNothingElse(t *testing.T) {
	node := checkedNode("Build the server. Check it with `bash run_tests.sh`.",
		"the server is built and `bash run_tests.sh` passes")
	door := auditDoorFor(node)

	if refusal, ok := auditRefusal("bash run_tests.sh", door.allowed); !ok {
		t.Fatalf("the checker may not run the check the work itself names: %s", refusal)
	}
	// The flags a check takes come with it, because a prefix is a command prefix.
	if refusal, ok := auditRefusal("bash run_tests.sh --verbose", door.allowed); !ok {
		t.Fatalf("the declared check does not admit its own arguments: %s", refusal)
	}
	for _, refused := range []string{
		"bash anything-else.sh",
		"bash",
		"rm -rf .",
		"bash run_tests.sh && rm -rf .",
	} {
		refusal, ok := auditRefusal(refused, door.allowed)
		if ok {
			t.Fatalf("the checker was allowed to run %q, which the work never named", refused)
		}
		// AND THE REFUSAL NAMES THIS AUDIT'S OWN DOOR. A no that does not say
		// where the door is costs another step, which is what the whole spin was
		// made of.
		if !strings.Contains(refusal, "bash run_tests.sh") {
			t.Fatalf("the refusal of %q does not name what this audit may run:\n%s", refused, refusal)
		}
	}
	// The reading commands are still under the check, and they are still the
	// only thing there that is not the work's own.
	if refusal, ok := auditRefusal("cat main.rs", door.allowed); !ok {
		t.Fatalf("the checker may not read a file: %s", refusal)
	}
}

// A WILDCARD THE WORK WROTE IS A WILDCARD THE DOOR HONOURS. A brief that names
// its check as `run_tests.*` is naming one check whose extension it did not want
// to spell, and a door that took the star literally would open onto nothing.
func TestADeclaredCheckKeepsTheWildcardTheWorkWroteIt(t *testing.T) {
	door := auditDoorFor(checkedNode("`run_tests.*` scores the implementation", "it scores"))
	if refusal, ok := auditRefusal("run_tests.sh", door.allowed); !ok {
		t.Fatalf("the wildcard the work wrote does not admit the file it names: %s", refusal)
	}
	if _, ok := auditRefusal("run_tests_and_delete.sh", door.allowed); ok {
		t.Fatal("the wildcard matched a command it does not name")
	}
}

// A BRIEF NAMES MORE THAN COMMANDS, and the shapes that are not commands do not
// become doors: an option is a flag, and a wall of wildcards is not a check.
func TestOnlyCommandShapedTextBecomesADeclaredCheck(t *testing.T) {
	node := checkedNode("run it with `--stdio`, the data is at `/data/golden.jsonl`, "+
		"and the check is `make check`", "`*` is not a check")
	door := auditDoorFor(node)
	for _, refused := range []string{"--stdio", "* --anything", "curl example.com"} {
		if _, ok := auditRefusal(refused, door.allowed); ok {
			t.Fatalf("%q became a door", refused)
		}
	}
	if refusal, ok := auditRefusal("make check", door.allowed); !ok {
		t.Fatalf("the check the brief actually names is refused: %s", refusal)
	}
}

// WHAT THE WORK RAN, THE CHECKER MAY RUN AGAIN. The worker's own receipts are
// already in the audit's packet so the judge can see which command the check is;
// seeing it and not being allowed to run it is the door the measured audit stood
// at for five minutes.
func TestTheCheckerMayRerunWhatTheWorkItselfRan(t *testing.T) {
	node := checkedNode("build it", "it builds", toolReceipt{
		tool:    "bash",
		command: "cargo build --release",
		result:  "Finished release [optimized] target(s)",
	})
	door := auditDoorFor(node)
	if refusal, ok := auditRefusal("cargo build --release", door.allowed); !ok {
		t.Fatalf("the checker may not re-run what the work ran: %s", refusal)
	}
	if _, ok := auditRefusal("cargo publish", door.allowed); ok {
		t.Fatal("the work running one cargo command opened the door to every cargo command")
	}
}

// A CHECK IS RE-RUN VERBATIM AS THE WORK RAN IT, OR IT IS NOT RE-RUN AT ALL.
// A composed line cannot be re-run without taking it apart, and this gate does
// not compose — so a composed receipt contributes nothing, and the dangerous
// half of a composed line can never arrive as a door of its own.
func TestAComposedCommandTheWorkRanIsNotADoor(t *testing.T) {
	node := checkedNode("build it", "it builds",
		toolReceipt{tool: "bash", command: "rm -rf build && cargo build --release 2>&1 | tail -5"},
		toolReceipt{tool: "bash", command: "shutdown -h now"},
	)
	door := auditDoorFor(node)
	if len(door.checks) != 0 {
		t.Fatalf("a composed line and a critical one became checks: %q", door.checks)
	}
	for _, refused := range []string{"rm -rf build", "cargo build --release", "shutdown -h now"} {
		if _, ok := auditRefusal(refused, door.allowed); ok {
			t.Fatalf("the checker was handed %q out of a line it could never re-run", refused)
		}
	}
}

// NO LANGUAGE AND NO TOOLCHAIN IS SPELLED INTO THE GATE.
//
// The old allowlist was a constant naming three Go verbs, which is why a Rust
// deliverable could not be checked at all. The guard is on STRING LITERALS: the
// comments in both files name the failure they were written for, and history a
// reader can see is worth keeping — a list the code MATCHES against is not.
func TestNoToolchainIsWrittenIntoTheAuditorsDoor(t *testing.T) {
	forbidden := []string{
		"go test", "go build", "go vet", "cargo", "npm", "yarn", "pytest",
		"make check", "mvn", "gradle", "dotnet",
	}
	for _, name := range []string{"task_audit.go", "task_checks.go"} {
		fileSet := token.NewFileSet()
		parsed, err := parser.ParseFile(fileSet, name, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			literal, ok := node.(*ast.BasicLit)
			if !ok || literal.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(literal.Value)
			if err != nil {
				text = literal.Value
			}
			lowered := strings.ToLower(text)
			for _, word := range forbidden {
				if strings.Contains(lowered, word) {
					t.Errorf("%s:%d names a toolchain in code: %q contains %q",
						name, fileSet.Position(literal.Pos()).Line, text, word)
				}
			}
			return true
		})
	}
}

// AN AUDITOR WITH NOTHING TO RUN CONCLUDES INSTEAD OF SPINNING.
//
// Five minutes is the price of a real check. An audit that has no runnable check
// has no slow half, so the same five minutes buys only the measured failure: an
// auditor reaching for a door that was never going to open until the clock says
// nobody answered. The window follows whether there is a check and nothing else
// — never the size of the work.
func TestAnAuditWithNothingToRunConcludesLongBeforeTheDeadline(t *testing.T) {
	empty := auditDoorFor(checkedNode("write a paragraph about the API", "the paragraph is there"))
	if len(empty.checks) != 0 {
		t.Fatalf("a node that declares and ran nothing has checks: %q", empty.checks)
	}
	if window := empty.window(); window >= auditDeadline/2 {
		t.Fatalf("an audit with nothing to run gets %s of the %s deadline", window, auditDeadline)
	}
	// AND IT IS TOLD SO. A model that has not been told there is no door keeps
	// looking for one, which is what the spin was made of.
	line := empty.line()
	for _, want := range []string{"NOTHING THIS WORK DECLARES OR RAN", "answer now"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the auditor is never told it has nothing to run (%q missing):\n%s", want, line)
		}
	}

	full := auditDoorFor(checkedNode("build it, checked with `make check`", "it builds"))
	if window := full.window(); window != auditDeadline {
		t.Fatalf("an audit with a check to run gets %s, want the full %s", window, auditDeadline)
	}
	if !strings.Contains(full.line(), "make check") {
		t.Fatalf("the auditor is never shown its own check:\n%s", full.line())
	}
}

// AN UNATTENDED SESSION NEVER ENDS THE LADDER ON A PERSON.
//
// A node nobody could judge lands needing a look, and in a session with somebody
// in front of it that is right: they have a card, and the choice is theirs. In a
// session with nobody watching — `--once`, a headless door, a benchmark cell —
// there is no card, no settings panel and nobody to read a landing that says it
// is waiting on them, so the run simply stops. It was measured stopping.
//
// The remedy is the road that already exists — the same `task.settle = auto` the
// person's own "decide these for me" sets — reached without a person to press it.
func TestAnUnattendedSessionNeverEndsTheLadderOnAPerson(t *testing.T) {
	notice := TaskNotice{
		ID:     4,
		Title:  "the deliverable",
		State:  TaskUnverified,
		Report: needsLookLead + "nobody could say whether it holds",
	}

	unattended := &Agent{config: Config{AskConsent: false}}
	if settle := unattended.settlePolicy(); settle != TaskSettleAuto {
		t.Fatalf("an unattended session settles %q, so the run waits for somebody who is not there", settle)
	}
	note := taskNote(notice, "", unattended.settlePolicy())
	if !strings.Contains(note, settleAutoLead) {
		t.Fatalf("the unattended landing never hands the decision on:\n%s", note)
	}
	if strings.Contains(note, settleAskTail) {
		t.Fatalf("the unattended landing still leaves the choice with a person:\n%s", note)
	}

	// AND IT NEVER GOES THE OTHER WAY. Somebody who IS there keeps their row,
	// and a blank row still reads as asking.
	watched := &Agent{config: Config{AskConsent: true}}
	if settle := watched.settlePolicy(); settle != TaskSettleAsk {
		t.Fatalf("a watched session with a blank row settles %q, want ask", settle)
	}
	if asked := taskNote(notice, "", watched.settlePolicy()); !strings.Contains(asked, settleAskTail) {
		t.Fatalf("a watched landing stopped offering the person the choice:\n%s", asked)
	}
	told := &Agent{config: Config{AskConsent: true, TaskSettle: string(TaskSettleAuto)}}
	if settle := told.settlePolicy(); settle != TaskSettleAuto {
		t.Fatalf("a watched session that asked for auto settles %q", settle)
	}

	// THE VOCABULARY LAW HOLDS ON BOTH ROADS. Nothing a person or the model
	// reads off a landing says auditor, verdict, verified or refuted.
	for _, text := range []string{note, taskNote(notice, "", watched.settlePolicy())} {
		for _, banned := range []string{"auditor", "verdict", "verified", "refuted"} {
			if strings.Contains(strings.ToLower(text), banned) {
				t.Fatalf("the landing says %q to a person:\n%s", banned, text)
			}
		}
	}
}
