package session

// THE BEFORE-READING A TASK'S CHECKER GETS, AS OBSERVABLE CONTRACTS.
//
// These tests use real repositories and real commands wherever the contract is
// about a tree. The small arithmetic and packet tests stay pure so a failure
// names the sentence or subtraction that moved rather than a model's choice.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
)

// TestAReadingOfTheBaseSaysWhichChecksWereAlreadyFailing proves C1. The node's
// own checkout has the failure fixed without a commit, so only a detached read
// of the branch can still find the committed red.
func TestAReadingOfTheBaseSaysWhichChecksWereAlreadyFailing(t *testing.T) {
	repo := newGoModuleRepo(t)
	test := filepath.Join(repo, "base_test.go")
	writeFile(t, test, "package taskaudit\n\nimport \"testing\"\n\nfunc TestBaseWasRed(t *testing.T) { t.Fatal(\"old red\") }\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "old red")
	writeFile(t, test, "package taskaudit\n\nimport \"testing\"\n\nfunc TestBaseWasRed(t *testing.T) {}\n")

	command := "go test ./..."
	photograph := (&Agent{}).baseChecksFor(context.Background(), taskTree{
		dir: repo, root: repo, branch: "work", ground: repo, seal: baselineCommitForTest(t, repo),
	}, []string{command})
	if !photograph.read {
		t.Fatal("the base was not read")
	}
	if !reflect.DeepEqual(photograph.red, []string{command}) {
		t.Fatalf("base red = %#v, want the committed failing check", photograph.red)
	}
	if len(photograph.unread) != 0 {
		t.Fatalf("base unread = %#v, want none", photograph.unread)
	}
}

// TestACheckThatCouldNotBeReadIsCountedNeitherWay proves C7. Each case reaches
// the same public fact by a different road: no shell answer, a moved tree, and
// a window that closed before the command was reached.
func TestACheckThatCouldNotBeReadIsCountedNeitherWay(t *testing.T) {
	cases := []struct {
		name    string
		command func(string) string
		ctx     func() (context.Context, context.CancelFunc)
	}{
		{
			name:    "the command would not start",
			command: func(string) string { return "aforge-command-that-does-not-exist-571" },
			ctx:     func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
		},
		{
			name:    "the command moved the tree",
			command: func(dir string) string { return fmt.Sprintf("printf moved > %q", filepath.Join(dir, "moved.txt")) },
			ctx:     func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) },
		},
		{
			name:    "the window never reached the command",
			command: func(string) string { return "printf never-ran" },
			ctx: func() (context.Context, context.CancelFunc) {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx, func() {}
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			command := test.command(dir)
			ctx, cancel := test.ctx()
			defer cancel()
			photograph := readChecksOn(ctx, dir, []string{command})
			if !photograph.read || !reflect.DeepEqual(photograph.unread, []string{command}) {
				t.Fatalf("reading = %+v, want the command unread", photograph)
			}
			if len(photograph.red) != 0 {
				t.Fatalf("red = %#v, want none", photograph.red)
			}
			ground := checkGround{
				before: photograph,
				after:  checkPhotograph{red: []string{command}, read: true},
			}
			if got := ground.alreadyRed(); len(got) != 0 {
				t.Fatalf("already red = %#v, want no answer from an unread command", got)
			}
			if got := ground.turnedRed(); len(got) != 0 {
				t.Fatalf("turned red = %#v, want no answer from an unread command", got)
			}
		})
	}
}

// TestTheAlreadyRedArithmeticIsTheSessionsOwnSubtraction proves C4 across the
// three boundaries that matter: old red, new red, and no before-reading.
func TestTheAlreadyRedArithmeticIsTheSessionsOwnSubtraction(t *testing.T) {
	cases := []struct {
		name       string
		ground     checkGround
		alreadyRed []string
		turnedRed  []string
	}{
		{
			name: "red before and now",
			ground: checkGround{
				before: checkPhotograph{red: []string{"go test ./..."}, read: true},
				after:  checkPhotograph{red: []string{"go test ./..."}, read: true},
			},
			alreadyRed: []string{"go test ./..."},
		},
		{
			name: "green before and red now",
			ground: checkGround{
				before: checkPhotograph{read: true},
				after:  checkPhotograph{red: []string{"go test ./..."}, read: true},
			},
			turnedRed: []string{"go test ./..."},
		},
		{
			name: "nobody read before",
			ground: checkGround{
				after: checkPhotograph{red: []string{"go test ./..."}, read: true},
			},
		},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := test.ground.alreadyRed(); !reflect.DeepEqual(got, test.alreadyRed) {
				t.Errorf("already red = %#v, want %#v", got, test.alreadyRed)
			}
			if got := test.ground.turnedRed(); !reflect.DeepEqual(got, test.turnedRed) {
				t.Errorf("turned red = %#v, want %#v", got, test.turnedRed)
			}
		})
	}
}

// TestTheCheckerIsToldWhatWasAlreadyFailing proves C2 in the exact packet
// block, without asking a model to paraphrase the fact back.
func TestTheCheckerIsToldWhatWasAlreadyFailing(t *testing.T) {
	command := "go test ./..."
	block := checkGroundBlock(checkGround{
		before: checkPhotograph{red: []string{command}, read: true},
		after:  checkPhotograph{red: []string{command}, read: true},
	})
	for _, want := range []string{command, "was red before this work and remains red", "not proof that the requested behavior works"} {
		if !strings.Contains(block, want) {
			t.Errorf("the checker was not told %q:\n%s", want, block)
		}
	}
}

// TestTheBeforeReadingReachesTheCheckersQuestion proves C2 at the composition
// seam. A correct standalone block is not enough if the packet quietly stops
// carrying it to the checker.
func TestTheBeforeReadingReachesTheCheckersQuestion(t *testing.T) {
	node := loneTestNode(t, "keep the requested change")
	command := "go test ./..."
	checks := checkGround{
		before: checkPhotograph{red: []string{command}, read: true},
		after:  checkPhotograph{red: []string{command}, read: true},
	}
	question := auditQuestion(node, taskTree{}, auditGround{}, auditDoor{}, checks, landingFiles{}, "", nil)
	for _, want := range []string{command, "Judge the requested acceptance"} {
		if !strings.Contains(question, want) {
			t.Errorf("the checker question is missing %q:\n%s", want, question)
		}
	}

	withoutReading := auditQuestion(node, taskTree{}, auditGround{}, auditDoor{}, checkGround{}, landingFiles{}, "", nil)
	if strings.Contains(withoutReading, "WHAT THE CHECKS SAID BEFORE THIS WORK") {
		t.Fatalf("an untaken reading wrote a heading into the checker question:\n%s", withoutReading)
	}
}

// TestTheCheckerIsToldTheTreeWasCleanWhenItWas proves C2's clean control. A
// completed before-reading with no red and no unread has one unambiguous fact.
func TestTheCheckerIsToldTheTreeWasCleanWhenItWas(t *testing.T) {
	block := checkGroundBlock(checkGround{before: checkPhotograph{read: true}})
	for _, want := range []string{"every check was passing before this work began", "any red you find is this work's"} {
		if !strings.Contains(strings.ToLower(block), want) {
			t.Errorf("the clean reading did not say %q:\n%s", want, block)
		}
	}
}

// TestNothingIsSaidAboutRedNobodyRead proves C3 on both destinations: the
// checker packet and the finished report stay silent when there was no base.
func TestNothingIsSaidAboutRedNobodyRead(t *testing.T) {
	if block := checkGroundBlock(checkGround{}); block != "" {
		t.Fatalf("an untaken reading wrote a checker block: %q", block)
	}
	landing := (auditVerdict{
		verified: true, answered: true, word: auditVerified, evidence: []string{"the change holds"},
	}).doneOutcome()
	if strings.Contains(landing, "already failing before this work") {
		t.Fatalf("an untaken reading wrote a landing sentence: %q", landing)
	}
}

// TestCheckGroundForRunsOnlyTheReadingsTheContractNeeds proves C3 and C8 at
// the orchestration seam. The counter lives outside the repository so counting
// a run cannot make its own tree reading unread.
func TestCheckGroundForRunsOnlyTheReadingsTheContractNeeds(t *testing.T) {
	repo := newGoModuleRepo(t)
	tree := taskTree{dir: repo, root: repo, branch: "work", ground: repo, seal: baselineCommitForTest(t, repo)}
	agent := &Agent{}
	runs := func(path string) int {
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			return 0
		}
		if err != nil {
			t.Fatal(err)
		}
		return strings.Count(string(data), "read\n")
	}

	var log strings.Builder
	withoutDoor := agent.checkGroundFor(context.Background(), tree,
		auditGround{dir: repo, restored: true}, auditDoor{}, &log)
	if block := checkGroundBlock(withoutDoor); block != "" || log.Len() != 0 {
		t.Fatalf("no door produced block %q and log %q, want silence", block, log.String())
	}

	notRestoredCounter := filepath.Join(t.TempDir(), "not-restored")
	notRestoredCommand := fmt.Sprintf("printf 'read\\n' >> %q", notRestoredCounter)
	log.Reset()
	notRestored := agent.checkGroundFor(context.Background(), tree,
		auditGround{dir: repo}, auditDoor{checks: []string{notRestoredCommand}}, &log)
	if got := runs(notRestoredCounter); got != 0 {
		t.Fatalf("a folder-mode check ran %d times, want 0", got)
	}
	if block := checkGroundBlock(notRestored); block != "" || log.Len() != 0 {
		t.Fatalf("an unrestored ground produced block %q and log %q, want silence", block, log.String())
	}

	greenCounter := filepath.Join(t.TempDir(), "green")
	greenCommand := fmt.Sprintf("printf 'read\\n' >> %q", greenCounter)
	log.Reset()
	green := agent.checkGroundFor(context.Background(), tree,
		auditGround{dir: repo, restored: true}, auditDoor{checks: []string{greenCommand}}, &log)
	if got := runs(greenCounter); got != 1 {
		t.Fatalf("a clean base check ran %d times, want the before-reading only", got)
	}
	if !green.before.read || green.after.read {
		t.Fatalf("clean ground = %+v, want a before-reading and no after-reading", green)
	}

	redCounter := filepath.Join(t.TempDir(), "red")
	redCommand := fmt.Sprintf("printf 'read\\n' >> %q; false", redCounter)
	log.Reset()
	red := agent.checkGroundFor(context.Background(), tree,
		auditGround{dir: repo, restored: true}, auditDoor{checks: []string{redCommand}}, &log)
	if got := runs(redCounter); got != 2 {
		t.Fatalf("an already-red base check ran %d times, want before and after", got)
	}
	if !red.before.read || !red.after.read {
		t.Fatalf("red ground = %+v, want both readings", red)
	}
}

// proposeTaskWithAcceptance declares verification separately from its prose.
// Acceptance describes the requested result; only Checks grants permission to
// execute the verifier on the baseline and completed work.
func proposeTaskWithAcceptance(title, brief, acceptance, ground string, checks []string) step {
	arguments, _ := json.Marshal(taskArguments{
		Title:       title,
		Summary:     "one line the person reads",
		Brief:       brief + "\n" + taskBriefMark,
		Deliverable: "the file named in the brief",
		Acceptance:  acceptance,
		Ground:      ground,
		Checks:      checks,
	})
	return func(context.Context, []ai.Message) (*ai.Response, error) {
		return toolResponse("call-task", "propose_task", string(arguments)), nil
	}
}

// TestAPreExistingRedDoesNotStandBetweenTheWorkAndItsLanding proves C5 through
// the real task door. The task writes an unrelated deliverable over a committed
// failing suite, and the finished report carries the shared session sentence.
func TestAPreExistingRedDoesNotStandBetweenTheWorkAndItsLanding(t *testing.T) {
	repo := newGoModuleRepo(t)
	writeFile(t, filepath.Join(repo, "old_red_test.go"),
		"package taskaudit\n\nimport \"testing\"\n\nfunc TestOldRed(t *testing.T) { t.Fatal(\"already red\") }\n")
	mustGit(t, repo, "add", "-A")
	mustGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-m", "old red")
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeTaskWithAcceptance("Write the note", "write note.txt", "note.txt exists and `go test ./...` passes", repo, []string{"go test ./..."}),
			finalText("handed off"),
		},
		child: []step{
			writeCall("write-note", "note.txt", "the requested note\n"),
			finalText("Wrote note.txt."),
		},
		audit: []step{verdict("VERIFIED — note.txt contains the requested note")},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	graph := agent.graph()
	events := collect(t, mustSubmit(t, agent, "write the note"))
	node := graph.node(1)
	if node == nil {
		t.Fatalf("no node was admitted; events = %#v", events)
	}
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskDone {
		t.Fatalf("state = %q, report = %q, want done", notice.State, notice.Report)
	}
	want := "1 check was red before this work and is not counted as identified new red: go test ./..."
	if !strings.Contains(notice.Report, want) {
		t.Fatalf("report = %q, want %q", notice.Report, want)
	}
}

// TestAGreenCheckTheWorkTurnedRedIsTheWorksOwn proves C6. The base is clean,
// the child's only file introduces a failing test, and the checker's refusal
// remains a refusal without borrowing the old-red sentence.
func TestAGreenCheckTheWorkTurnedRedIsTheWorksOwn(t *testing.T) {
	repo := newGoModuleRepo(t)
	t.Setenv("HOME", t.TempDir())

	completer := &routedCompleter{
		parent: []step{
			proposeTaskWithAcceptance("Add the check", "write work_test.go", "work_test.go exists and `go test ./...` passes", repo, []string{"go test ./..."}),
			finalText("handed off"),
		},
		child: []step{
			writeCall("write-red", "work_test.go",
				"package taskaudit\n\nimport \"testing\"\n\nfunc TestWorkBreaksGreen(t *testing.T) { t.Fatal(\"new red\") }\n"),
			finalText("Wrote work_test.go."),
		},
		audit: []step{
			bashCall("run-check", "go test ./..."),
			verdictFromEvidence("FAIL", "REFUTED — go test ./... now fails: TestWorkBreaksGreen", "VERIFIED — go test ./... passes"),
		},
	}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.Workspace = repo
		config.AskConsent = false
		config.TaskAutoApproveSeconds = 0
	})
	graph := agent.graph()
	events := collect(t, mustSubmit(t, agent, "add the check"))
	node := graph.node(1)
	if node == nil {
		t.Fatalf("no node was admitted; events = %#v", events)
	}
	waitDoneNode(t, node)
	notice := node.notice()

	if notice.State != TaskFailed {
		t.Fatalf("state = %q, report = %q, want failed", notice.State, notice.Report)
	}
	if !strings.Contains(notice.Report, "TestWorkBreaksGreen") {
		t.Fatalf("report = %q, want the check this work broke", notice.Report)
	}
	if strings.Contains(notice.Report, "already failing before this work") {
		t.Fatalf("new red was described as old red: %q", notice.Report)
	}
}

// TestOneBaseIsReadOnceForEveryNodeCutFromIt proves C8 with the cheapest
// observable counter: the command writes outside the repository, so it counts
// executions without invalidating its own reading.
func TestOneBaseIsReadOnceForEveryNodeCutFromIt(t *testing.T) {
	repo := newGoModuleRepo(t)
	counter := filepath.Join(t.TempDir(), "runs")
	command := fmt.Sprintf("printf 'read\\n' >> %q", counter)
	first := taskTree{dir: filepath.Join(t.TempDir(), "first"), root: repo, branch: "work", ground: repo, seal: baselineCommitForTest(t, repo)}
	second := taskTree{dir: filepath.Join(t.TempDir(), "second"), root: repo, branch: "work", ground: repo, seal: baselineCommitForTest(t, repo)}
	agent := &Agent{}

	for _, tree := range []taskTree{first, second} {
		photograph := agent.baseChecksFor(context.Background(), tree, []string{command})
		if !photograph.read || len(photograph.red) != 0 || len(photograph.unread) != 0 {
			t.Fatalf("base reading = %+v, want one clean answer", photograph)
		}
	}
	data, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	if runs := strings.Count(string(data), "read\n"); runs != 1 {
		t.Fatalf("the shared base check ran %d times, want 1", runs)
	}
}

// baselineCommitForTest records the same immutable cut that real task trees carry.
func baselineCommitForTest(t *testing.T, repo string) string {
	t.Helper()
	sha, err := git(repo, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(sha)
}
