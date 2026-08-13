package exec

import (
	"fmt"
	"strings"
	"testing"
)

// THE SUBHARNESS RETURNS AN ACCOUNT OF ITSELF.
//
// The reported defect, in one sentence: a coding run that ended without a
// terminal line reported "The coding run ended without saying how it went", and
// the whole story of what it had done — the files, the diff, the suite it had
// run — went past on the wire and was thrown away. Everything downstream then
// had to act on the void: the delivery gate judged it, and the pass that reads
// a job's remainder kept adding children to re-investigate finished work.
//
// This drives the engine's real event shapes past the consumer and asserts that
// the account comes out the other side: the change set with a kind and a size
// per file, the verification story with a verdict per command, and a deliverable
// that says what happened instead of saying nothing.

// scriptedRun replays one NDJSON stream through the consumer and hands back the
// state it built. The trace is real because consume writes to it.
func scriptedRun(t *testing.T, lines []string) *sweRun {
	t.Helper()
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	trace := newTracer(space, "41")
	run := &sweRun{trace: trace, outcome: &Outcome{}, root: space.Root()}
	for _, line := range lines {
		run.consume(line + "\n")
	}
	trace.close()
	return run
}

// filePart is one message.part.updated carrying a finished tool call, in the
// shape the engine publishes it (internal/swepro/EVENTS-CONTRACT.md).
func filePart(id, tool, metadata string) string {
	return fmt.Sprintf(`{"id":"evt_%s","type":"message.part.updated","properties":{"sessionID":"ses_1",`+
		`"part":{"id":"%s","sessionID":"ses_1","messageID":"msg_1","type":"tool","callID":"call_%s",`+
		`"tool":"%s","state":{"status":"completed","input":{},"output":"done","metadata":%s,`+
		`"time":{"start":1,"end":2}}}}}`, id, id, id, tool, metadata)
}

func TestTheAccountIsBuiltFromWhatTheEngineAlreadyEmits(t *testing.T) {
	space, err := NewWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	trace := newTracer(space, "41")
	run := &sweRun{trace: trace, outcome: &Outcome{}, root: space.Root()}
	// The engine cuts a git worktree under the workspace and merges it back, so
	// the paths it names are absolute and go through machinery no reader of this
	// job has heard of.
	worktree := space.Root() + "/.plandb/wt-task-3"
	lines := []string{
		`{"type":"stage","stage":"bootstrap","status":"ready","data":{}}`,
		filePart("1", "write", fmt.Sprintf(
			`{"filepath":"%s/internal/parser/commas.go","exists":false,`+
				`"diff":"--- a\n+++ b\n+package parser\n+\n+func Trailing() {}\n"}`, worktree)),
		// The bus republishes a finished part; the account must count it once.
		filePart("1", "write", fmt.Sprintf(
			`{"filepath":"%s/internal/parser/commas.go","exists":false,`+
				`"diff":"--- a\n+++ b\n+package parser\n+\n+func Trailing() {}\n"}`, worktree)),
		filePart("2", "edit", fmt.Sprintf(
			`{"filediff":{"file":"%s/internal/parser/commas.go","patch":"…","additions":4,"deletions":1}}`,
			worktree)),
		filePart("3", "apply_patch",
			`{"files":[{"filePath":"/tmp/x","relativePath":"internal/parser/lexer.go",`+
				`"type":"update","additions":2,"deletions":2},`+
				`{"filePath":"/tmp/y","relativePath":"docs/stale.md","type":"delete",`+
				`"additions":0,"deletions":9}]}`),
		`{"type":"stage","stage":"verification","status":"fail","data":{"commands":[` +
			`{"cmd":"go build ./...","exit":0,"tail":"","kind":"build"},` +
			`{"cmd":"go test ./...","exit":1,"tail":"FAIL github.com/x 0.2s\nsecond line","kind":"test"}]}}`,
		`{"type":"stage","stage":"verification","status":"pass","data":{"commands":[` +
			`{"cmd":"go build ./...","exit":0,"tail":"","kind":"build"},` +
			`{"cmd":"go test ./...","exit":0,"tail":"ok  github.com/x 1.2s","kind":"test"}]}}`,
	}
	for _, line := range lines {
		run.consume(line + "\n")
	}
	trace.close()

	account := &run.account
	// THE PATHS ARE THE ONES A READER OF THIS JOB RECOGNISES. The engine's
	// worktree is where the leaf ran, not what it changed.
	want := map[string]FileChange{
		"internal/parser/commas.go": {Change: ChangeAdded, Added: 7, Removed: 1},
		"internal/parser/lexer.go":  {Change: ChangeChanged, Added: 2, Removed: 2},
		"docs/stale.md":             {Change: ChangeDeleted, Added: 0, Removed: 9},
	}
	if len(account.Files) != len(want) {
		t.Fatalf("files = %#v, want one row per path", account.Files)
	}
	for _, file := range account.Files {
		expected, ok := want[file.Path]
		if !ok {
			t.Fatalf("an unrecognised path reached the account: %q", file.Path)
		}
		if file.Change != expected.Change || file.Added != expected.Added || file.Removed != expected.Removed {
			t.Fatalf("%s = %+v, want %+v — a file written then edited is added, once, at its full size",
				file.Path, file, expected)
		}
	}
	if files, added, removed := account.Stat(); files != 3 || added != 9 || removed != 12 {
		t.Fatalf("stat = %d files %+d %+d, want 3 files +9 -12", files, added, -removed)
	}

	// THE LAST VERIFICATION PASS STANDS. It runs once per audit cycle over a
	// tree that keeps changing, so a union of the passes would report a suite
	// both red and green.
	if len(account.Checks) != 2 {
		t.Fatalf("checks = %#v, want the last pass's two commands", account.Checks)
	}
	if !account.Verified() {
		t.Fatalf("the account does not report a green suite: %#v", account.Checks)
	}
	if account.Checks[1].Command != "go test ./..." || !account.Checks[1].Passed {
		t.Fatalf("the test command is missing or red: %+v", account.Checks[1])
	}
}

// THE ENGINE'S OWN BOOKKEEPING IS NOT A CHANGE THE WORK MADE.
//
// The two halves of what a coding leaf reports were read from two places under
// two rules. The artifact list came from the repository and was filtered; the
// account's rows came from the engine's own tool metadata and were not, so a
// delivered account named `.codeaf/contract.json` — the engine writing its own
// contract file, reported to a person as their change, in the one surface a
// dependent leaf reads to decide what happened. Every row goes through the one
// filter now, whichever tool wrote it.
func TestTheEnginesOwnBookkeepingNeverReachesTheAccount(t *testing.T) {
	run := scriptedRun(t, []string{
		filePart("1", "write",
			`{"filepath":".codeaf/contract.json","exists":false,"diff":"--- a\n+++ b\n+{}\n"}`),
		filePart("2", "edit",
			`{"filediff":{"file":".codeaf/plan/architecture.md","patch":"…","additions":40,"deletions":0}}`),
		filePart("3", "apply_patch",
			`{"files":[{"filePath":"/w/.plandb.db","relativePath":".plandb.db","type":"update",`+
				`"additions":1,"deletions":0},`+
				`{"filePath":"/w/internal/parser/commas.go","relativePath":"internal/parser/commas.go",`+
				`"type":"update","additions":3,"deletions":1}]}`),
	})
	if len(run.account.Files) != 1 {
		t.Fatalf("the account reports the engine's machinery as the work: %#v", run.account.Files)
	}
	if run.account.Files[0].Path != "internal/parser/commas.go" {
		t.Fatalf("the one real change is not the row that survived: %#v", run.account.Files[0])
	}
}

// The void sentence, and what replaces it. A run whose engine died without a
// terminal line is the exact case the account exists for.
func TestASilentEndingStillDeliversTheAccount(t *testing.T) {
	run := scriptedRun(t, []string{
		filePart("1", "edit",
			`{"filediff":{"file":"internal/parser/commas.go","patch":"…","additions":6,"deletions":2}}`),
		`{"type":"stage","stage":"verification","status":"pass","data":{"commands":[` +
			`{"cmd":"go test ./...","exit":0,"tail":"ok  github.com/x 1.2s","kind":"test"}]}}`,
	})
	text := run.text(StopError, nil)
	if strings.Contains(text, "without saying how it went") {
		t.Fatalf("the void sentence survived a run with an account: %q", text)
	}
	for _, want := range []string{
		"1 file changed, +6 -2 lines",
		"changed internal/parser/commas.go",
		"passed: go test ./...",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("the deliverable does not carry %q:\n%s", want, text)
		}
	}

	// And with nothing to show, the old sentence is still the honest one.
	bare := scriptedRun(t, []string{`{"type":"stage","stage":"bootstrap","status":"ready","data":{}}`})
	if !strings.Contains(bare.text(StopError, nil), "without saying how it went") {
		t.Fatalf("a run that really did nothing invented an account: %q", bare.text(StopError, nil))
	}
}

// The worker's last word is quoted once per reader that needs it, and never
// twice in the same context. The leaf's own text opens with it, so the report
// under that text leaves it out; the gate is often judging something composed
// since, so the evidence rows carry it.
func TestTheWorkersLastWordIsQuotedWhereItIsMissingAndNotWhereItIsNot(t *testing.T) {
	account := &Account{
		Files: []FileChange{{Path: "a.go", Change: ChangeChanged, Added: 1}},
		Final: "the trailing-comma case parses and the suite is green",
	}
	if strings.Contains(account.Report(), "trailing-comma") {
		t.Fatalf("the deliverable repeats its own opening paragraph:\n%s", account.Report())
	}
	rows := strings.Join(account.Lines(), "\n")
	if !strings.Contains(rows, "What the work said for itself when it finished:") ||
		!strings.Contains(rows, "trailing-comma") {
		t.Fatalf("the gate was never told what the worker signed off with:\n%s", rows)
	}
}

// A red check that was already red before the work began is carried as such.
// The engine is the only thing that photographed the repository beforehand, and
// an account that dropped the distinction would either convict correct work or
// claim a green suite over one anybody can watch failing.
func TestTheAccountKeepsAPreExistingFailureAsOne(t *testing.T) {
	run := scriptedRun(t, []string{
		`{"type":"stage","stage":"verification","status":"pass","data":{"commands":[` +
			`{"cmd":"make all","exit":2,"tail":"--- FAIL: TestOld","kind":"test","preExisting":true}]}}`,
	})
	if len(run.account.Checks) != 1 || !run.account.Checks[0].Known || run.account.Checks[0].Passed {
		t.Fatalf("checks = %#v, want one red check acquitted against the baseline", run.account.Checks)
	}
	if !run.account.Verified() {
		t.Fatal("a suite whose only red was already red does not settle the work")
	}
	rows := strings.Join(run.account.CheckLines(), "\n")
	if !strings.Contains(rows, "already failing before this work began") {
		t.Fatalf("the acquittal is not in the rows:\n%s", rows)
	}

	// A suite killed at the harness ceiling never produced an exit status, and
	// an incomplete observation is not a green one.
	hung := scriptedRun(t, []string{
		`{"type":"stage","stage":"verification","status":"fail","data":{"commands":[` +
			`{"cmd":"pytest","tail":"","kind":"test","timedOut":true}]}}`,
	})
	if hung.account.Verified() {
		t.Fatalf("a hung suite was read as a green one: %#v", hung.account.Checks)
	}
}
