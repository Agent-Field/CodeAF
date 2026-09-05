package session

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
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

// declaringNode is a node whose CONTRACT declares its verification, which is the
// only road a command reaches a checker's door by. Its prose deliberately names
// no command at all, so a case that passes here passed on the declaration rather
// than on something a backtick left lying in the document.
func declaringNode(checks ...string) *TaskNode {
	node := checkedNode("do the work", "the work is done")
	node.spec.checks = checks
	node.Checks = checks
	return node
}

// THE DOOR IS THE CHECK THE WORK DECLARED, AND IT IS THAT CHECK AND NOT ANOTHER.
//
// This is the measured failure in one assertion. A task whose own words say the
// check is `bash run_tests.sh` had that exact command refused, with a list of Go
// verbs offered instead, on every one of six audits — so the auditor could
// verify nothing, spun for the whole deadline, and the node landed needing a
// person. The declared check now opens, and nothing next to it does.
func TestTheCheckerRunsTheCheckTheWorkDeclaredAndNothingElse(t *testing.T) {
	door := auditDoorFor(declaringNode("bash run_tests.sh"), "")

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
//
// THE STAR IS STILL RESOLVED AGAINST A REAL TREE, because a declared span only
// becomes a door if the checker could run it where it stands ([runnableHere]) —
// and a wildcard matching nothing under the auditor's own feet is a word, not a
// check.
func TestADeclaredCheckKeepsTheWildcardTheWorkWroteIt(t *testing.T) {
	dir := checkedTree(t, "run_tests.sh")
	door := auditDoorFor(declaringNode("run_tests.*"), dir)
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
	// A CONTRACT IS REFUSED AT THE DOOR RATHER THAN QUIETLY EMPTIED, because the
	// model is one repair away from a check that works.
	for _, refused := range []string{"--stdio", "* --anything", "cd x && make check", "rm -rf /"} {
		if got, problem := declaredCheckList([]string{refused}); problem == "" {
			t.Fatalf("%q was accepted as a declared check: %q", refused, got)
		}
	}
	// AND WHAT IS ACCEPTED IS THE COMMAND, WHITESPACE AND ALL NORMALISED, so the
	// door and the contract cannot disagree about what was declared.
	got, problem := declaredCheckList([]string{"make  check", "  ", "curl example.com"})
	if problem != "" {
		t.Fatalf("an ordinary pair of checks was refused: %s", problem)
	}
	if len(got) != 2 || got[0] != "make check" || got[1] != "curl example.com" {
		t.Fatalf("the declared checks came back as %q", got)
	}
	door := auditDoorFor(declaringNode(got...), "")
	if refusal, ok := auditRefusal("make check", door.allowed); !ok {
		t.Fatalf("the check the contract actually names is refused: %s", refusal)
	}
	if _, ok := auditRefusal("--stdio", door.allowed); ok {
		t.Fatal("an option became a door")
	}
}

// A DECLARED DOOR IS SOMETHING THAT CAN ACTUALLY RUN, AND PROSE BACKTICKS A
// GREAT DEAL THAT CANNOT.
//
// This is the third measured failure in one table. A real acceptance backticked
// what prose backticks — a remote, a branch, a repository, a rule identifier, a
// line out of somebody's test — every one of which has the shape of a command,
// so the door offered them: "You may run: origin, main, Agent-Field/agentfield,
// …". The checker ran them in order, collected the shell's 126s and 127s, and
// wrote "Ran the named checks: all refused or exit 126/127" into a finding a
// person then read as the state of the work.
//
// The two halves that are really runnable — a program the shell would find, and
// a file the tree the checker stands in really holds — are still doors, because
// this is a question about what can run and not a narrower reading of what a
// check is.
func TestOnlyARunnableSpanBecomesADeclaredCheck(t *testing.T) {
	tree := checkedTree(t, "run_tests.sh")
	bare := t.TempDir()
	for _, one := range []struct {
		what   string
		ground string
		span   string
		door   bool
		needs  string
	}{
		// The measured acceptance, span by span.
		{what: "a remote", ground: tree, span: "origin"},
		{what: "a branch", ground: tree, span: "main"},
		{what: "a repository", ground: tree, span: "Agent-Field/agentfield"},
		{what: "a rule identifier", ground: tree, span: "js/polynomial-redos"},
		{what: "a branch with slashes in it", ground: tree, span: "fix/codeql-56-url-substring-test"},
		{what: "an assertion lifted out of a test", ground: tree, span: `"api.openai.com" in caplog.text`},
		// And the spans that name something the checker could really start.
		{what: "a script the tree holds", ground: tree, span: "run_tests.sh", door: true},
		{what: "the same script with no such file under the checker", ground: bare, span: "run_tests.sh"},
		{
			what: "a program the shell finds", ground: bare, needs: "gh", door: true,
			span: "gh pr list --repo Agent-Field/agentfield --state open",
		},
		{
			what: "an interpreter and the module it is handed", ground: bare, needs: "python", door: true,
			span: "python -m pytest sdk/python/tests/test_execution_logger.py",
		},
		{
			what: "the same, spelled the way the machine has it", ground: bare, needs: "python3", door: true,
			span: "python3 -m pytest sdk/python/tests/test_execution_logger.py",
		},
	} {
		if one.needs != "" {
			if _, err := exec.LookPath(one.needs); err != nil {
				t.Logf("%s: skipped, %s is not on this machine's PATH", one.what, one.needs)
				continue
			}
		}
		got := declaredChecks("Check it with `"+one.span+"`, please.", one.ground, checksFromWork)
		if one.door && len(got) != 1 {
			t.Errorf("%s (%q) is runnable here and did not become a check: %q", one.what, one.span, got)
		}
		if !one.door && len(got) != 0 {
			t.Errorf("%s (%q) became a check the checker cannot run: %q", one.what, one.span, got)
		}
	}

	// AND A CONTRACT THAT DECLARED ONE OF THOSE WORDS OPENS NOTHING EITHER. The
	// refusal is read by a model that will type whatever it is offered, so a word
	// on that list is a command that is about to be run — and a declaration is not
	// a promise that the thing declared exists.
	declared := auditDoorFor(declaringNode("origin", "main", "Agent-Field/agentfield",
		"js/polynomial-redos"), tree)
	if len(declared.checks) != 0 {
		t.Fatalf("a contract naming words that are not programs opened doors onto them: %q", declared.checks)
	}
	for _, word := range []string{"origin", "main", "Agent-Field/agentfield", "js/polynomial-redos"} {
		if strings.Contains(declared.offer(), word) {
			t.Errorf("the checker is still told it may run %q:\n%s", word, declared.offer())
		}
	}
	// AND IT IS TOLD IT HAS NOTHING TO RUN, which is the honest reading of that
	// contract and the one that ends the audit instead of spinning it.
	if !strings.Contains(declared.line(), "DECLARED NO REPEATABLE CHECK") {
		t.Fatalf("a node with no runnable check is not told so:\n%s", declared.line())
	}
}

// NOTHING A NODE'S DOCUMENT MERELY SAYS IS A CHECKER'S DOOR — NOT THE PERSON'S
// PASTED WORDS, NOT THE BRIEF, NOT THE FROZEN DONE-CONDITION.
//
// A measured tox run harvested `chmod 000 tox.ini` from a pasted issue and tried
// it against the deliverable tree, where success would have made the project's
// own configuration unreadable. That reading is gone entirely: a command reaches
// a node's checker by being DECLARED as verification and by no other road, so
// prose that happens to be command-shaped is prose.
func TestNothingInANodesProseBecomesACheckerDoor(t *testing.T) {
	tree := checkedTree(t, "tox.ini", "run_tests.sh")

	node := checkedNode("repair the tox configuration, checking with `run_tests.sh`",
		"`run_tests.sh` passes")
	node.spec.request = "the reproduction ends with:\n$ chmod 000 tox.ini"
	door := auditDoorFor(node, tree)
	if len(door.checks) != 0 {
		t.Fatalf("a node that declared no verification still has checks: %q", door.checks)
	}
	for _, refused := range []string{"chmod 000 tox.ini", "run_tests.sh", "bash run_tests.sh"} {
		if _, ok := auditRefusal(refused, door.allowed); ok {
			t.Fatalf("%q entered the door out of the node's own prose", refused)
		}
		if strings.Contains(door.offer(), refused) {
			t.Fatalf("the node offered %q, which nobody declared:\n%s", refused, door.offer())
		}
	}

	// AND THE SAME DOCUMENT WITH THE CHECK DECLARED RUNS IT. What moved is where
	// the command comes from, not whether a check can be made.
	node.Checks = []string{"bash run_tests.sh"}
	declared := auditDoorFor(node, tree)
	if refusal, ok := doorRefusal("bash run_tests.sh", declared); !ok {
		t.Fatalf("the declared check does not open the node's door: %s", refusal)
	}
	if _, ok := doorRefusal("chmod 000 tox.ini", declared); ok {
		t.Fatal("declaring one check opened the door to a step out of the pasted reproduction")
	}
}

// A DONE-CONDITION THAT IS THE PERSON'S PASTED ASK CARRIES NOTHING ONTO A DOOR
// EITHER. The auto-started road puts [routeAskAcceptance] in front of the request
// when nobody could write a separate done-condition, and the measured tox
// transcript stays the person's account while it sits in the acceptance field.
func TestANodesDoneWhenThatIsThePastedAskCarriesNoStepOutOfIt(t *testing.T) {
	tree := checkedTree(t, "tox.ini", "run_tests.sh")
	node := checkedNode("repair the tox configuration", routeAskAcceptance+
		"check it with `run_tests.sh`\nthe reproduction ends with:\n$ chmod 000 tox.ini")
	door := auditDoorFor(node, tree)

	if len(door.checks) != 0 {
		t.Fatalf("the ask-fallback done-condition opened a node door: %q\n%s", door.checks, door.offer())
	}
	if _, ok := auditRefusal("chmod 000 tox.ini", door.allowed); ok {
		t.Fatal("the gate allowed a prompt step out of the ask-fallback done-condition")
	}
}

// WHAT THE WORK RAN IS EVIDENCE, NOT PERMISSION TO RUN IT AGAIN.
//
// This is the fourth measured failure in one assertion. A task was asked to run a
// two-minute build script once and report its marker; the worker ran it, exit 0,
// and the checker — handed that receipt as a door — ran the same script again for
// another two minutes. The receipt is still in the checker's packet, where it
// settles that the requested action was carried out; it is not a command the
// checker may issue.
func TestAWorkerReceiptIsNeverACheckerDoor(t *testing.T) {
	dir := t.TempDir()
	node := checkedNode("run the build script once and report its marker", "the marker is reported",
		toolReceipt{tool: "bash", args: `{"command":"./slow-build.sh"}`, result: "exit 0 · wrote QUARTZLINE"},
		toolReceipt{tool: "bash", args: `{"command":"cd ` + dir + ` && cargo build --release 2>&1 | tail -3"}`,
			result: "Finished release"},
		toolReceipt{tool: "bash", args: `{"command":"shutdown -h now"}`, result: "refused"},
	)
	door := auditDoorFor(node, dir)
	if len(door.checks) != 0 {
		t.Fatalf("what the worker ran became a door: %q", door.checks)
	}
	for _, refused := range []string{
		"./slow-build.sh", "slow-build.sh",
		// Including the command inside a composed line, which used to be read out
		// of the receipt and handed over as a check of its own.
		"cargo build --release", "cd " + dir + " && cargo build --release",
		"shutdown -h now",
	} {
		if _, ok := doorRefusal(refused, door); ok {
			t.Fatalf("the checker was handed %q out of a receipt", refused)
		}
	}
	// AND THE RECEIPTS ARE STILL IN FRONT OF IT, whole, because that is how a
	// checker settles that the requested action happened without repeating it.
	packet := auditReceiptBlock(node.lastReceipts(), true)
	for _, want := range []string{"./slow-build.sh", "QUARTZLINE"} {
		if !strings.Contains(packet, want) {
			t.Fatalf("the checker can no longer see that %q ran:\n%s", want, packet)
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

// NO SPAN IS HARVESTED BEFORE ITS ACCOUNT IS ASKED.
//
// The backtick loop once stood before the source test, so it could admit a
// pasted reproduction even though the prompt-line loop below it was guarded.
// This structural assertion keeps the account boundary in front of every loop;
// the behavioural tests then prove what each side of that boundary observes.
func TestNoSpanIsHarvestedBeforeTheAccountIsAsked(t *testing.T) {
	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, "task_checks.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing task_checks.go: %v", err)
	}
	var declared *ast.FuncDecl
	for _, declaration := range parsed.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == "declaredChecks" {
			declared = function
			break
		}
	}
	if declared == nil || declared.Body == nil {
		t.Fatal("task_checks.go has no declaredChecks body to keep behind the account boundary")
	}
	if len(declared.Body.List) == 0 {
		t.Fatal("declaredChecks has an empty body instead of an account guard")
	}
	guard, ok := declared.Body.List[0].(*ast.IfStmt)
	if !ok {
		t.Fatalf("declaredChecks starts with %T, want the account guard as its first statement", declared.Body.List[0])
	}
	mentionsFrom := false
	ast.Inspect(guard.Cond, func(node ast.Node) bool {
		if name, ok := node.(*ast.Ident); ok && name.Name == "from" {
			mentionsFrom = true
		}
		return true
	})
	if !mentionsFrom {
		t.Fatalf("declaredChecks' first condition at line %d does not ask the from parameter", fileSet.Position(guard.Pos()).Line)
	}
	if len(guard.Body.List) != 1 {
		t.Fatalf("declaredChecks' account guard has %d body statements, want only the return", len(guard.Body.List))
	}
	if _, ok := guard.Body.List[0].(*ast.ReturnStmt); !ok {
		t.Fatalf("declaredChecks' account guard contains %T, want only the return", guard.Body.List[0])
	}
	ast.Inspect(declared.Body, func(node ast.Node) bool {
		switch node.(type) {
		case *ast.ForStmt, *ast.RangeStmt:
			if node.Pos() < guard.End() {
				t.Errorf("declaredChecks has a loop at line %d before the account guard ends at line %d",
					fileSet.Position(node.Pos()).Line, fileSet.Position(guard.End()).Line)
			}
		}
		return true
	})
}

// AN AUDITOR WITH NOTHING TO RUN CONCLUDES INSTEAD OF SPINNING.
//
// Five minutes is the price of a real check. An audit that has no runnable check
// has no slow half, so the same five minutes buys only the measured failure: an
// auditor reaching for a door that was never going to open until the clock says
// nobody answered. The window follows whether there is a check and nothing else
// — never the size of the work.
func TestAnAuditWithNothingToRunConcludesLongBeforeTheDeadline(t *testing.T) {
	empty := auditDoorFor(checkedNode("write a paragraph about the API", "the paragraph is there"), "")
	if len(empty.checks) != 0 {
		t.Fatalf("a node that declares and ran nothing has checks: %q", empty.checks)
	}
	if window := empty.window(); window >= auditDeadline/2 {
		t.Fatalf("an audit with nothing to run gets %s of the %s deadline", window, auditDeadline)
	}
	// AND IT IS TOLD SO. A model that has not been told there is no door keeps
	// looking for one, which is what the spin was made of.
	line := empty.line()
	for _, want := range []string{"DECLARED NO REPEATABLE CHECK", "answer now"} {
		if !strings.Contains(line, want) {
			t.Fatalf("the auditor is never told it has nothing to run (%q missing):\n%s", want, line)
		}
	}

	full := auditDoorFor(declaringNode("make check"), "")
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
	note := taskNote(notice, "", unattended.settlePolicy(), landingAddress{person: true})
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
	if asked := taskNote(notice, "", watched.settlePolicy(), landingAddress{person: true}); !strings.Contains(asked, settleAskTail) {
		t.Fatalf("a watched landing stopped offering the person the choice:\n%s", asked)
	}
	told := &Agent{config: Config{AskConsent: true, TaskSettle: string(TaskSettleAuto)}}
	if settle := told.settlePolicy(); settle != TaskSettleAuto {
		t.Fatalf("a watched session that asked for auto settles %q", settle)
	}

	// THE VOCABULARY LAW HOLDS ON BOTH ROADS. Nothing a person or the model
	// reads off a landing says auditor, verdict, verified or refuted.
	for _, text := range []string{note, taskNote(notice, "", watched.settlePolicy(), landingAddress{person: true})} {
		for _, banned := range []string{"auditor", "verdict", "verified", "refuted"} {
			if strings.Contains(strings.ToLower(text), banned) {
				t.Fatalf("the landing says %q to a person:\n%s", banned, text)
			}
		}
	}
}

// doorRefusal asks the gate about a WHOLE door rather than a bare list, which is
// the only way to ask it about the checks that named a file: those are matched by
// which file they are, and a list of strings does not carry a file.
func doorRefusal(command string, door auditDoor) (string, bool) {
	return refuseOutsideDoor(command, door, auditShell)
}

// checkedTree writes the files a declared check might name — scripts that say
// what starts them and are marked runnable, which is the ordinary case — and
// hands back the directory the auditor would be standing in.
func checkedTree(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range names {
		writeCheckFile(t, dir, name, "#!/usr/bin/env bash\nexit 0\n", 0o755)
	}
	return dir
}

// writeCheckFile writes one file with the first line and the mode that decide
// which spellings of it a door will open.
func writeCheckFile(t *testing.T, dir, name, body string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), mode); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
	return path
}

// ONE FILE IS ONE CHECK, HOWEVER THE CHECKER SPELLS IT.
//
// This is the second measured failure in one assertion. The door named the
// project's own script; the auditor tried it with the directory stated first,
// then by absolute path, then bare, then with a program word in front, then with
// a dot and a slash — FIVE SPELLINGS OF THE SAME FILE, all refused, and it spent
// the whole window reading source instead. Every spelling that starts that file
// now opens, and a spelling that starts a different file, or that adds arguments
// the work never declared, still does not.
func TestOneFileIsOneCheckHoweverTheCheckerSpellsIt(t *testing.T) {
	dir := checkedTree(t, "run_tests.sh", "other.sh")
	door := auditDoorFor(declaringNode("bash run_tests.sh"), dir)

	for _, spelling := range []string{
		"run_tests.sh",
		"./run_tests.sh",
		filepath.Join(dir, "run_tests.sh"),
		"bash run_tests.sh",
		"bash " + filepath.Join(dir, "run_tests.sh"),
		// THE INTERPRETER THE FILE ITSELF NAMES, down any path that reaches it.
		"/usr/bin/bash ./run_tests.sh",
	} {
		if refusal, ok := doorRefusal(spelling, door); !ok {
			t.Fatalf("the checker may not run the work's own check spelled %q: %s", spelling, refusal)
		}
	}

	for _, refused := range []string{
		// A different file is a different check, whoever launches it.
		"bash other.sh",
		"other.sh",
		// Arguments after the file are not the check the work declared.
		"bash run_tests.sh --flag",
		"./run_tests.sh --flag",
		// A WORD IN FRONT IS NOT ANY WORD. It is the one the file itself names,
		// so a program that would rewrite the check instead of running it is not
		// a spelling of the check, and neither is the wrong interpreter.
		"rm run_tests.sh",
		"python3 run_tests.sh",
		// And the shape is still one command with one word in front of it.
		"bash -x run_tests.sh",
		"bash run_tests.sh && rm -rf .",
		"rm -rf .",
	} {
		refusal, ok := doorRefusal(refused, door)
		if ok {
			t.Fatalf("the checker was allowed to run %q, which the work never declared", refused)
		}
		// AND THE REFUSAL SAYS HOW THE CHECK IS SPELLED. A no that names a file
		// without saying which shapes start it is the no that was guessed at five
		// times running.
		if !strings.Contains(refusal, "run it as") ||
			!strings.Contains(refusal, "`bash "+filepath.Join(dir, "run_tests.sh")+"`") {
			t.Fatalf("the refusal of %q does not say how to run the check:\n%s", refused, refusal)
		}
	}

	// AND THE AUDITOR IS TOLD THE SAME THING BEFORE IT EVER REACHES A REFUSAL.
	if line := door.line(); !strings.Contains(line, "run it as") {
		t.Fatalf("the door never tells the checker how its own check is spelled:\n%s", line)
	}
}

// A WILDCARD THE WORK WROTE NAMES THE FILE IT MATCHES. The run this was written
// for declared its check as `run_tests.*` — one file on disk, no program in front
// of it — so a rule that only understood literal paths would have left that door
// shut for the same reason it was shut before.
func TestAWildcardCheckNamesTheFileOnDisk(t *testing.T) {
	dir := checkedTree(t, "run_tests.sh", "other.sh")
	door := auditDoorFor(declaringNode("run_tests.*"), dir)
	for _, spelling := range []string{"run_tests.sh", "./run_tests.sh", "bash run_tests.sh"} {
		if refusal, ok := doorRefusal(spelling, door); !ok {
			t.Fatalf("the wildcard the work wrote does not admit %q: %s", spelling, refusal)
		}
	}
	if _, ok := doorRefusal("bash other.sh", door); ok {
		t.Fatal("the wildcard matched a file it does not name")
	}
}

// A WORD IN FRONT OF THE CHECK IS THE ONE THE FILE ITSELF NAMES.
//
// The door admits a program word before the file because a script is usually
// started that way, not because any word may stand there: `rm <the check>` names
// the same file and does not run it. The file answers the question — its first
// line names its own interpreter — so the harness still holds no list of
// launchers and still learns no language.
func TestATwoWordSpellingMustNameTheFilesOwnInterpreter(t *testing.T) {
	dir := t.TempDir()
	writeCheckFile(t, dir, "direct.py", "#!/usr/bin/python3\nprint(1)\n", 0o755)
	writeCheckFile(t, dir, "found.sh", "#!/usr/bin/env bash\nexit 0\n", 0o755)
	writeCheckFile(t, dir, "flagged.py", "#!/usr/bin/env -S python3 -u\nprint(1)\n", 0o755)
	door := auditDoorFor(declaringNode("direct.py", "found.sh", "flagged.py"), dir)

	for _, spelling := range []string{
		// The program the line names, and any path that reaches that program.
		"python3 direct.py",
		"/usr/bin/python3 ./direct.py",
		// A line whose first word is the launcher that GOES AND FINDS a program
		// names that program next, options and all, so it is the word — read by
		// shape, with nothing here knowing what does the finding.
		"bash found.sh",
		"python3 flagged.py",
		// And the file on its own, which the executable bit already vouched for.
		"./direct.py",
		"found.sh",
	} {
		if refusal, ok := doorRefusal(spelling, door); !ok {
			t.Fatalf("the file names its own interpreter and %q was still refused: %s", spelling, refusal)
		}
	}

	for _, refused := range []string{
		// The wrong interpreter is not the interpreter.
		"bash direct.py",
		"python3 found.sh",
		// And a program that would change the check instead of running it is not
		// a spelling of the check at all.
		"rm direct.py",
		"rm ./found.sh",
		"truncate found.sh",
	} {
		refusal, ok := doorRefusal(refused, door)
		if ok {
			t.Fatalf("%q was admitted as a way of running the check", refused)
		}
		if !strings.Contains(refusal, "run it as") {
			t.Fatalf("the refusal of %q never says how the check is run:\n%s", refused, refusal)
		}
	}
	// AND THE DOOR NAMES THE INTERPRETER RATHER THAN A SHAPE. A model that is
	// told the word does not have to guess it.
	if line := door.line(); !strings.Contains(line, "`python3 "+filepath.Join(dir, "direct.py")+"`") {
		t.Fatalf("the door never names the check's own interpreter:\n%s", line)
	}
}

// A FILE THAT DECLARES NO INTERPRETER IS RUN THE WAY THE WORK RAN IT.
//
// A file with no first line naming a program and no executable bit says nothing
// about being started, so nothing is invented for it: the only spellings that open
// are the ones the work itself wrote or ran, and the refusal SAYS SO rather than
// leaving a model to guess at launchers it will never be allowed.
func TestAFileThatDeclaresNoInterpreterIsRunTheWayTheWorkRanIt(t *testing.T) {
	dir := t.TempDir()
	writeCheckFile(t, dir, "plain.sh", "exit 0\n", 0o755)
	writeCheckFile(t, dir, "data.txt", "cases: 4\n", 0o644)
	writeCheckFile(t, dir, "ranonly.sh", "exit 0\n", 0o644)

	// The executable bit alone is a file saying that running it happens, which is
	// what makes the bare spellings work — but it names no program, so no word
	// may stand in front of it.
	marked := auditDoorFor(declaringNode("plain.sh"), dir)
	for _, spelling := range []string{"plain.sh", "./plain.sh", filepath.Join(dir, "plain.sh")} {
		if refusal, ok := doorRefusal(spelling, marked); !ok {
			t.Fatalf("an executable check refused its own bare spelling %q: %s", spelling, refusal)
		}
	}
	if _, ok := doorRefusal("bash plain.sh", marked); ok {
		t.Fatal("a file that names no interpreter was handed one anyway")
	}

	// A file with neither fact is data until the work says otherwise — and when
	// the work says otherwise, that spelling and no other is the door.
	declared := auditDoorFor(declaringNode("bash data.txt"), dir)
	if refusal, ok := doorRefusal("bash data.txt", declared); !ok {
		t.Fatalf("the work's own spelling of its own check was refused: %s", refusal)
	}
	for _, refused := range []string{"data.txt", "./data.txt", "python3 data.txt", "rm data.txt"} {
		if _, ok := doorRefusal(refused, declared); ok {
			t.Fatalf("%q opened a file that declares nothing about being run", refused)
		}
	}
	if refusal, _ := doorRefusal("data.txt", declared); !strings.Contains(refusal, "declares no interpreter") {
		t.Fatalf("the refusal never says the file declares no interpreter:\n%s", refusal)
	}

	// AND A FILE THE WORKER MERELY RAN IS NOT DECLARED. The receipt says the run
	// happened; it does not put the file under contract as verification.
	ran := auditDoorFor(checkedNode("build it", "it builds",
		toolReceipt{tool: "bash", args: `{"command":"bash ranonly.sh"}`, result: "exit 0"},
	), dir)
	for _, refused := range []string{"bash ranonly.sh", "ranonly.sh", "./ranonly.sh"} {
		if _, ok := doorRefusal(refused, ran); ok {
			t.Fatalf("%q was admitted on the strength of a receipt", refused)
		}
	}
}

// ── #468: how the session's harvest opens what the work named ───────────────

// A FILE THE TREE HOLDS OUTRANKS A PROGRAM OF THE SAME NAME ON PATH.
//
// A repository that carries its own `check` or `build` beside the work means THAT
// file. A lookup that answered first would quietly run somebody else's program of
// the same name, against somebody else's assumptions, and report on it — which is
// the exact shape of wrongness this whole reading exists to remove.
func TestAFileTheTreeHoldsOutranksAProgramOnThePath(t *testing.T) {
	// `true` is on every PATH this build runs on, so the collision is real rather
	// than arranged. If it ever is not, there is no ordering to test.
	if !onThePath("true") {
		t.Skip("no program called true on this machine, so there is no collision to read")
	}
	dir := t.TempDir()
	mine := writeCheckFile(t, dir, "true", "#!/bin/sh\nexit 0\n", 0o755)

	got := checkCommand(dir, "true")
	if got != shellQuoted(mine) {
		t.Fatalf("the tree's own file lost to a program of the same name:\n got %q\nwant %q",
			got, shellQuoted(mine))
	}
	// AND AN ORDINARY COMMAND IS LEFT EXACTLY AS THE WORK WROTE IT. Two words ask
	// the tree about the SECOND one — the file a program is being handed — and
	// nothing here is called `--version`, so the program on the path stands.
	if got := checkCommand(dir, "true --version"); got != "true --version" {
		t.Fatalf("a command the tree knows nothing about was not left alone: %q", got)
	}
}

// EVERY PATH HANDED TO A SHELL IS ONE WORD, WHATEVER IS IN IT.
//
// A checkout under a directory with a space in its name split into two words, and
// the check then ran against neither of them — while reporting, forever, that it
// did not pass.
func TestACheckPathWithASpaceRunsWhole(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a tree with spaces")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("making the tree: %v", err)
	}
	writeCheckFile(t, dir, "run.sh", "#!/bin/sh\nexit 0\n", 0o755)

	command := checkCommand(dir, "run.sh")
	if command == "" {
		t.Fatal("a script the tree holds was not a check at all")
	}
	if ran := runOneCheck(context.Background(), dir, command); !ran.Passed {
		t.Fatalf("the check split on the space in its own path: %q said %q", command, ran.Tail)
	}
}

// AND THE PATH IN A TWO-WORD CHECK IS QUOTED TOO.
//
// The work says WHICH program runs its check, and it is not the authority on how
// a shell splits a word: `sh run'tests.sh` handed back as the work wrote it is an
// unterminated quote, and what a shell does with that is not run the check.
func TestATwoWordCheckQuotesTheFileItNames(t *testing.T) {
	dir := t.TempDir()
	name := "run'tests.sh"
	path := writeCheckFile(t, dir, name, "exit 0\n", 0o644)

	command := checkCommand(dir, "sh "+name)
	if want := "sh " + shellQuoted(path); command != want {
		t.Fatalf("the file behind the program was not quoted:\n got %q\nwant %q", command, want)
	}
	if ran := runOneCheck(context.Background(), dir, command); !ran.Passed {
		t.Fatalf("the check broke on the quote in its own path: %q said %q", command, ran.Tail)
	}
	// AND A SECOND WORD THE TREE DOES NOT HOLD IS LEFT ALONE, because there is no
	// path to resolve and the work's own spelling is the whole of what is known.
	if got := checkCommand(dir, "sh missing.sh"); got != "sh missing.sh" {
		t.Fatalf("a command naming no file of ours was rewritten: %q", got)
	}
}

// THE FIRST WORD OF A SHEBANG IS THE PROGRAM, AND `env` IS THE ONE EXCEPTION.
//
// Everything after the first word is an argument handed to that program. Reading
// the LAST word instead promoted a mode flag to a launcher: `#!/usr/bin/python3
// isolated` answered `isolated`, so the door would have admitted `isolated <the
// check>` and refused the interpreter that really starts it.
func TestTheShebangNamesItsFirstWordAndEnvNamesTheNext(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct {
		what string
		name string
		line string
		want string
	}{
		{"a flag after the program is an argument, not the program",
			"isolated.py", "#!/usr/bin/python3 isolated\n", "python3"},
		{"the launcher that finds a program names it next",
			"found.py", "#!/usr/bin/env python3\n", "python3"},
		{"and its own options are skipped on the way",
			"flagged.py", "#!/usr/bin/env -S python3 -u\n", "python3"},
		{"a program alone on the line is the program",
			"plain.sh", "#!/bin/sh\n", "sh"},
	} {
		path := writeCheckFile(t, dir, c.name, c.line+"exit 0\n", 0o644)
		if got, _ := fileFacts(path); got != c.want {
			t.Errorf("%s: %q names %q, want %q", c.what, strings.TrimSpace(c.line), got, c.want)
		}
	}
}

// TestAForgottenAccountReadsAsThePersonsAndNotTheWorks pins the ORDER of the
// [checkSource] constants, which is a safety property and not a detail.
//
// The gate this file exists for asks whose account a candidate came from, and
// answers with a comparison against one of two values before reading either
// spelling. Whichever of them is the zero is the answer a caller gets for free
// when it forgets to say — and this gate's whole reason for being is that the
// free answer used to be "the work's", so a pasted `$ chmod 000 tox.ini` was a
// promise. A caller that forgets now loses every check that text could have
// named. That is the failure this gate should have.
func TestAForgottenAccountReadsAsThePersonsAndNotTheWorks(t *testing.T) {
	var forgotten checkSource
	if forgotten != checksFromAsk {
		t.Fatal("the zero checkSource is not checksFromAsk, so a caller that forgets whose account this is harvests the person's pasted commands")
	}

	ground := t.TempDir()
	pasted := "To see it:\n\n$ echo reproduction-step\n\nand `echo named-check` is the check.\n"
	got := declaredChecks(pasted, ground, forgotten)
	if len(got) != 0 {
		t.Errorf("the zero account harvested commands from the person's words: %q", got)
	}
}
