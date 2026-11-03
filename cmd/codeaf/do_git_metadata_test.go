package main

// #1351: A WORKER MAY NOT REPLACE THE GIT METADATA OF THE FOLDER IT WAS GIVEN.
//
// On staging 17ae56d34 a `codeaf do --dir <repo>` worker ran, in order:
//
//	rm -rf <repo>/.git
//	cd <parent> && git init
//	git commit
//
// The repository's history was gone and the parent folder had become a
// repository of its own. The folder guard (internal/session's taskoutside.go)
// already refused the `git init` and `git commit` aimed at the parent, because
// the parent is outside the worker's ground. It allowed the first command,
// because <repo>/.git is INSIDE the ground. That one command is the whole of the
// data loss: once the history is deleted there is nothing the later two need to
// do to finish it.
//
// So these tests drive the real headless door with a scripted model that sends
// exactly those three commands, in a repository nested inside a parent that is
// NOT scratch (a run's --dir is never under the temp directory, and the scratch
// exemption would otherwise let the parent through), and assert what the issue
// asks for: the repository's .git survives, the parent never becomes a
// repository, and every attempt is refused with the boundary's own words.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/session"
)

// nestedRunRepo is a committed repository nested one level inside a parent
// folder, both standing OUTSIDE the machine's scratch directories, the way a
// person's `--dir` stands.
//
// It is placed under the module root rather than t.TempDir because the folder
// guard treats the temp directory as scratch a task may write to (it is where
// a shell command keeps what it reads back), and a parent there would be let
// through by that exemption rather than tested by the boundary.
func nestedRunRepo(t *testing.T) (parent, repo string) {
	t.Helper()
	root, err := os.MkdirTemp(".", "issue1351-")
	if err != nil {
		t.Fatal(err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if root, err = filepath.EvalSymlinks(root); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	parent = filepath.Join(root, "parent")
	repo = filepath.Join(parent, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	beltGit(t, repo, "init")
	beltGit(t, repo, "checkout", "-b", "work")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("the project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	beltGit(t, repo, "add", "-A")
	beltGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "first")
	return parent, repo
}

// recordedReplacementSeat is the scripted worker from the issue: it tries the
// three recorded commands, one per turn, and then finishes its task so the run
// ends rather than walling. Every tool result it is handed back is kept so the
// test can read the boundary's own words.
type recordedReplacementSeat struct {
	commands []string
	seen     int
	results  []string
}

func (s *recordedReplacementSeat) CompleteWithMessages(_ context.Context, messages []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	if s.seen > 0 {
		s.results = append(s.results, lastToolResult(messages))
	}
	if doc := beltDocument(messages); strings.Contains(doc, "## Who checks this work") {
		id := briefTaskID(doc)
		return beltToolReply("plandb done " + id + " --agent " + id + " --result 'holds: the acceptance is met'"), nil
	}
	if s.seen < len(s.commands) {
		command := s.commands[s.seen]
		s.seen++
		return beltToolReply(command), nil
	}
	s.seen++
	return beltToolReply(beltFinish(beltAnswer)), nil
}

// lastToolResult is the text of the newest tool message: what the belt said
// back about the command the worker just sent.
func lastToolResult(messages []ai.Message) string {
	for index := len(messages) - 1; index >= 0; index-- {
		if messages[index].Role != "tool" {
			continue
		}
		var b strings.Builder
		for _, part := range messages[index].Content {
			b.WriteString(part.Text)
		}
		return b.String()
	}
	return ""
}

func TestDoOnTheRunEngineRefusesReplacingTheFoldersGitMetadata(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	parent, repo := nestedRunRepo(t)
	head := doGitIn(t, repo, "rev-parse", "HEAD")

	// THE DELETE IS SPELLED RELATIVE, AS A WORKER STANDING IN ITS FOLDER SPELLS
	// IT. The absolute spelling (`rm -rf <repo>/.git`) is already stopped, but
	// only by accident: the critical-command floor (internal/approval) prompts on
	// every absolute recursive delete, so it matches `/*` rather than knowing what
	// .git is. `rm -rf .git` matches nothing there and nothing in the folder guard,
	// because .git is inside the worker's own ground, which is the gap this
	// issue is about.
	seat := &recordedReplacementSeat{commands: []string{
		"rm -rf .git",
		"cd " + parent + " && git init",
		"cd " + parent + " && git -c user.name=t -c user.email=t@t commit --allow-empty -m 'Add add(a,b) and one pytest test'",
	}}
	var stdout, stderr strings.Builder
	runErr := doErrand(doRequest{
		task: "create add.py and its test", workspace: repo, asJSON: true,
		timeout: 60 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return seat },
	})
	if runErr == nil {
		t.Fatalf("a run whose every attempted action was refused exited successfully\nstdout:\n%s\nstderr:\n%s", stdout.String(), stderr.String())
	}
	var status exitStatus
	if !asExitStatus(runErr, &status) || status != exitIncomplete {
		t.Fatalf("the refused-only run exited with %v, want incomplete (%d)", runErr, exitIncomplete)
	}
	// THE RECEIPT IS THE SERIALIZED ENVELOPE, not the private outcome: its `stop`
	// is a JSON field, while headlessOutcome.stop is unexported and never
	// survives a round trip, so the incomplete verdict must be read from the
	// envelope the run actually printed.
	var receipt struct {
		Stop    stopReason `json:"stop"`
		OK      bool       `json:"ok"`
		Settled bool       `json:"settled"`
		Error   string     `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout.String()), &receipt); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout.String())
	}
	if receipt.Stop != stopIncomplete || receipt.OK || !receipt.Settled {
		t.Fatalf("the refused-only receipt is not a settled incomplete outcome: %+v", receipt)
	}
	// The incomplete ending carries a reason in its own words. Which reason
	// depends on where the worker gave up -- the boundary's refusal when it
	// finished after the refusals, the same-step ending when it kept retrying
	// them -- and both are a run that ran, was refused, and did not finish. The
	// boundary's own words are proved against the answers the worker was handed
	// below, which the seat records whichever ending the run reached.
	if strings.TrimSpace(receipt.Error) == "" {
		t.Fatalf("the incomplete receipt omits its reason: %+v", receipt)
	}

	if info, err := os.Stat(filepath.Join(repo, ".git")); err != nil || !info.IsDir() {
		t.Fatalf("the repository's .git is gone after the run (err %v): the worker replaced the folder's history", err)
	}
	if now := doGitIn(t, repo, "rev-parse", "HEAD"); now != head {
		t.Fatalf("the repository's history moved %s -> %s", head, now)
	}
	if _, err := os.Stat(filepath.Join(parent, ".git")); err == nil {
		t.Fatal("the parent folder became a repository: git metadata was created outside the folder the run was given")
	}
	if len(seat.results) < 3 {
		t.Fatalf("the worker saw %d answers to its three commands, want three\nstdout:\n%s\nstderr:\n%s",
			len(seat.results), stdout.String(), stderr.String())
	}
	for index, said := range seat.results[:3] {
		if !strings.Contains(said, "is outside your copy") {
			t.Errorf("command %d (%q) was answered %q, want the boundary's refusal", index+1, seat.commands[index], said)
		}
	}
}

// A REFUSAL IS RECOVERABLE. If the worker reads the door's answer, performs real
// work inside its folder, and only then finishes, the run still exits zero. The
// false-success rule is about a claimed completion over nothing but refused
// actions, never about poisoning a task for having met one refusal.
func TestDoOnTheRunEngineCanRecoverAfterARefusedMetadataChange(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	_, repo := nestedRunRepo(t)

	seat := &recordedReplacementSeat{commands: []string{
		"rm -rf .git",
		"printf 'def add(a, b):\\n    return a + b\\n' > add.py",
	}}
	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "create add.py", workspace: repo, asJSON: true,
		timeout: 60 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return seat },
	}); err != nil {
		t.Fatalf("a worker that recovered after the refusal left with %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	if _, err := os.Stat(filepath.Join(repo, "add.py")); err != nil {
		t.Fatalf("the recovery write did not run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git")); err != nil {
		t.Fatalf("the refused metadata still disappeared: %v", err)
	}
}

// AND THE WORK THE FOLDER WAS GIVEN FOR STILL RUNS. A guard that kept .git
// safe by refusing everything would protect nothing anybody wanted done: the
// same door, in the same nested repository, still writes add.py and its test
// inside the given folder, and the parent is left exactly as it was.
func TestDoOnTheRunEngineStillWritesInsideTheFolderItWasGiven(t *testing.T) {
	beltRunEnv(t)
	t.Setenv("CODEAF_PLANDB_BIN", beltPlandbDoor(t))
	parent, repo := nestedRunRepo(t)
	before, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}

	seat := &recordedReplacementSeat{commands: []string{
		"printf 'def add(a, b):\\n    return a + b\\n' > add.py",
		"printf 'from add import add\\n\\ndef test_add():\\n    assert add(1, 2) == 3\\n' > test_add.py",
	}}
	var stdout, stderr strings.Builder
	if err := doErrand(doRequest{
		task: "create add.py and its test", workspace: repo, asJSON: true,
		timeout: 60 * time.Second, slots: bound(1), stdout: &stdout, stderr: &stderr,
		newBeltCompleter: func(string) session.Completer { return seat },
	}); err != nil {
		t.Fatalf("an ordinary run in its own folder left with %v\nstdout:\n%s\nstderr:\n%s", err, stdout.String(), stderr.String())
	}
	for _, name := range []string{"add.py", "test_add.py"} {
		if _, err := os.Stat(filepath.Join(repo, name)); err != nil {
			t.Fatalf("%s was not written inside the given folder: %v", name, err)
		}
	}
	for _, said := range seat.results {
		if strings.Contains(said, "is outside your copy") {
			t.Fatalf("an ordinary write in the given folder was refused: %q", said)
		}
	}
	after, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("the parent folder changed: %d entries before, %d after", len(before), len(after))
	}
}
