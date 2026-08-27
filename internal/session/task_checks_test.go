package session

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
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
	door := auditDoorFor(node, auditPlace{})

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
	door := auditDoorFor(checkedNode("`run_tests.*` scores the implementation", "it scores"), auditPlace{})
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
	door := auditDoorFor(node, auditPlace{})
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
	door := auditDoorFor(node, auditPlace{})
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
	door := auditDoorFor(node, auditPlace{})
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
	empty := auditDoorFor(checkedNode("write a paragraph about the API", "the paragraph is there"), auditPlace{})
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

	full := auditDoorFor(checkedNode("build it, checked with `make check`", "it builds"), auditPlace{})
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
	node := checkedNode("Build the server. Check it with `bash run_tests.sh`.",
		"the server is built and `bash run_tests.sh` passes")
	door := auditDoorFor(node, auditPlace{ground: dir, ran: dir})

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
	door := auditDoorFor(checkedNode("`run_tests.*` scores the implementation", "it scores"), auditPlace{ground: dir, ran: dir})
	for _, spelling := range []string{"run_tests.sh", "./run_tests.sh", "bash run_tests.sh"} {
		if refusal, ok := doorRefusal(spelling, door); !ok {
			t.Fatalf("the wildcard the work wrote does not admit %q: %s", spelling, refusal)
		}
	}
	if _, ok := doorRefusal("bash other.sh", door); ok {
		t.Fatal("the wildcard matched a file it does not name")
	}
}

// WHAT A WORKER'S LINE RUNS IS ITS FIRST STAGE, AND THE CHECKER MAY RUN THAT.
//
// Every receipt of the measured run was `cd <the tree> && cargo build --release
// 2>&1 | tail -3`, so the old reading — which asked whether the WHOLE line was
// one simple command — handed the auditor nothing, while the command the work
// checked itself with sat in the middle of every one of them. The `cd` states the
// directory the auditor is already standing in; the tail and the redirection only
// read what the first stage printed.
func TestTheCheckerRerunsTheCommandInsideAWorkersLine(t *testing.T) {
	dir := t.TempDir()
	elsewhere := t.TempDir()
	node := checkedNode("build it", "it builds",
		toolReceipt{tool: "bash", command: "cd " + dir + " && cargo build --release 2>&1 | tail -3"},
	)
	door := auditDoorFor(node, auditPlace{ground: dir, ran: dir})
	if len(door.checks) != 1 || door.checks[0] != "cargo build --release" {
		t.Fatalf("the command inside the worker's own line never became a check: %q", door.checks)
	}
	if refusal, ok := doorRefusal("cargo build --release", door); !ok {
		t.Fatalf("the checker may not re-run what the work built with: %s", refusal)
	}
	// AND WHAT IT DERIVED IS STILL ONE COMMAND. The auditor composes nothing: the
	// line it was read out of is refused exactly as it always was.
	if _, ok := doorRefusal("cd "+dir+" && cargo build --release", door); ok {
		t.Fatal("the checker was allowed to compose the line the check was read out of")
	}

	// AND IT IS THE TREE THE WORK RAN IN THAT A RECEIPT IS READ AGAINST, never the
	// one the auditor stands in. The verdict is reached in a clean restore beside
	// the node's own checkout, so in every real audit those are two directories,
	// and a receipt naming the checkout is still the worker saying where it stood.
	restore := auditDoorFor(node, auditPlace{ground: t.TempDir(), ran: dir})
	if len(restore.checks) != 1 || restore.checks[0] != "cargo build --release" {
		t.Fatalf("the worker's own directory was not read as the tree it ran in: %q", restore.checks)
	}

	// A DIRECTORY THAT IS NOT THIS TREE IS NOT A STATEMENT ABOUT THIS TREE, so the
	// line stays composed and contributes nothing.
	away := auditDoorFor(checkedNode("build it", "it builds",
		toolReceipt{tool: "bash", command: "cd " + elsewhere + " && cargo build --release"},
	), auditPlace{ground: dir, ran: dir})
	if len(away.checks) != 0 {
		t.Fatalf("a line that stepped out of the tree became a check: %q", away.checks)
	}

	// AND THE FLOOR UNDER A BLANKET ALLOW STILL STANDS ON WHAT WAS DERIVED. A
	// critical command wrapped in a cd and a pipe is still a critical command.
	critical := auditDoorFor(checkedNode("build it", "it builds",
		toolReceipt{tool: "bash", command: "cd " + dir + " && rm -rf / 2>&1 | tail -1"},
		toolReceipt{tool: "bash", command: "> out.txt | tail -1"},
		toolReceipt{tool: "bash", command: "cd " + dir + " && make it || rm -rf /"},
	), auditPlace{ground: dir, ran: dir})
	if len(critical.checks) != 0 {
		t.Fatalf("a derived command nobody would allow became a check: %q", critical.checks)
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
	node := checkedNode("check it with `direct.py`, `found.sh` and `flagged.py`", "they pass")
	door := auditDoorFor(node, auditPlace{ground: dir, ran: dir})

	for _, spelling := range []string{
		// The program the line names, and any path that reaches that program.
		"python3 direct.py",
		"/usr/bin/python3 ./direct.py",
		// A line that goes and finds the program names it last, so that is the
		// word — read by shape, with no knowledge of what does the finding.
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
	marked := auditDoorFor(checkedNode("check it with `plain.sh`", "it passes"),
		auditPlace{ground: dir, ran: dir})
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
	declared := auditDoorFor(checkedNode("score it with `bash data.txt`", "it scores"),
		auditPlace{ground: dir, ran: dir})
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

	// AND WHAT THE WORKER ITSELF RAN COUNTS AS THE WORK SAYING SO, read out of a
	// composed line the same way any other receipt is.
	ran := auditDoorFor(checkedNode("build it", "it builds",
		toolReceipt{tool: "bash", command: "cd " + dir + " && bash ranonly.sh 2>&1 | tail -1"},
	), auditPlace{ground: dir, ran: dir})
	if refusal, ok := doorRefusal("bash ranonly.sh", ran); !ok {
		t.Fatalf("the checker may not re-run the file the way the work ran it: %s", refusal)
	}
	for _, refused := range []string{"ranonly.sh", "bash ranonly.sh --flag", "rm ranonly.sh"} {
		if _, ok := doorRefusal(refused, ran); ok {
			t.Fatalf("%q was admitted, and the work never ran it that way", refused)
		}
	}
}
