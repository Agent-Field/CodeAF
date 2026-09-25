package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/plandb"
)

// EVERY WAY A PROGRAM'S RUN ENDS READS AS ONE VERDICT codeaf acts on.
func TestAProgramsEndingReadsAsTheVerdictCodeafActsOn(t *testing.T) {
	for _, tc := range []struct {
		name    string
		summary RunSummary
		want    programVerdict
	}{
		{"checked and passing", RunSummary{Outcome: beltRunOutcomeDone, ProgramVerdict: "pass"}, programPassed},
		{"nothing finished checking it", RunSummary{Outcome: beltRunOutcomeDone, ProgramVerdict: "pass-unverified"}, programUnverified},
		{"a finish that named no verdict", RunSummary{Outcome: beltRunOutcomeDone}, programUnverified},
		{"handed in work that fails", RunSummary{Outcome: "incomplete", Program: &ProgramEnding{Status: delegate.StatusFail}}, programFailed},
		{"its own ceiling", RunSummary{Outcome: "incomplete", Program: &ProgramEnding{Status: delegate.StatusBudget}}, programLimit},
		{"a limit the person set", RunSummary{Outcome: "incomplete", Limit: RunLimitCost}, programLimit},
		{"it crashed", RunSummary{Outcome: "incomplete", Program: &ProgramEnding{Status: delegate.StatusCrashed}}, programCrashed},
		{"it left no ending", RunSummary{Outcome: "incomplete"}, programCrashed},
	} {
		if got := programVerdictOf(tc.summary); got != tc.want {
			t.Errorf("%s: verdict %q, want %q", tc.name, got, tc.want)
		}
	}
}

// THE LINE UNDER THE ENDING SAYS WHAT TO DO NOW, and the two bounds are in it:
// a limit is never handed back on codeaf's own, and neither is a third retry.
func TestTheOutcomeNoteSaysWhatToDoNowAndKeepsBothBounds(t *testing.T) {
	note := func(verdict programVerdict, auto int) string {
		return programOutcomeNote(programOutcome{row: 3, program: "senior-dev", verdict: verdict,
			programAttempt: programAttempt{attempt: auto + 1, auto: auto}}, "the landing line", 1.5)
	}
	first := note(programFailed, 0)
	for _, want := range []string{"the landing line", "[senior-dev ended — for you to act on] task 3 · failed · run 1 · $1.50", "hand the work back to senior-dev", "2 more times"} {
		if !strings.Contains(first, want) {
			t.Fatalf("a first failure's note lacks %q:\n%s", want, first)
		}
	}
	if last := note(programFailed, programAutoRetries); strings.Contains(last, "hand the work back") || !strings.Contains(last, "do not hand it back") {
		t.Fatalf("a failure after the last retry is still told to hand it back:\n%s", last)
	}
	if limit := note(programLimit, 0); !strings.Contains(limit, "ask whether to spend more") || strings.Contains(limit, "hand the work back") {
		t.Fatalf("a limit's note does not say to ask the person first:\n%s", limit)
	}
	if unverified := note(programUnverified, 0); !strings.Contains(unverified, "Run the project's checks on its branch yourself") {
		t.Fatalf("an unverified ending is not told to check the branch:\n%s", unverified)
	}
	if passed := note(programPassed, 0); !strings.Contains(passed, "offer to merge") {
		t.Fatalf("a pass is not told to offer the merge:\n%s", passed)
	}
}

// THE BOUNDS ARE CODE. In the turn a program's ending woke, a hand-off to a
// program after a limit is refused, and so is one past the retry cap; a
// person's own turn is never refused by either, and starts a new line.
func TestARetryPastTheCapOrAfterALimitIsRefusedOnlyInTheOutcomesTurn(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	set := func(o *programOutcome) {
		agent.mu.Lock()
		agent.programOutcomeNow = o
		agent.mu.Unlock()
	}
	if got := agent.programRetryRefusal("senior-dev"); got != "" {
		t.Fatalf("a person's turn was refused a hand-off: %q", got)
	}
	if got := agent.programAttemptOf(); got != (programAttempt{attempt: 1}) {
		t.Fatalf("a person's hand-off starts at %+v, want the first run of a new line", got)
	}
	set(&programOutcome{row: 4, program: "senior-dev", verdict: programFailed, programAttempt: programAttempt{attempt: 1}})
	if got := agent.programRetryRefusal("senior-dev"); got != "" {
		t.Fatalf("a first failure's retry was refused: %q", got)
	}
	if got := agent.programAttemptOf(); got != (programAttempt{attempt: 2, auto: 1}) {
		t.Fatalf("the retry of a first run is %+v, want run 2, codeaf's first", got)
	}
	if got := agent.programRetryRefusal(""); got != "" {
		t.Fatalf("a hand-off to no program was refused: %q", got)
	}
	set(&programOutcome{row: 4, program: "senior-dev", verdict: programFailed, programAttempt: programAttempt{attempt: 3, auto: programAutoRetries}})
	if got := agent.programRetryRefusal("senior-dev"); !strings.Contains(got, "sent back to this work 2 times already") {
		t.Fatalf("a third retry was not refused: %q", got)
	}
	set(&programOutcome{row: 4, program: "senior-dev", verdict: programLimit, programAttempt: programAttempt{attempt: 1}})
	if got := agent.programRetryRefusal("senior-dev"); !strings.Contains(got, "ask the person first") {
		t.Fatalf("a re-run after a limit was not refused: %q", got)
	}
	agent.mu.Lock()
	agent.forgetOwedLocked()
	agent.mu.Unlock()
	if got := agent.programRetryRefusal("senior-dev"); got != "" {
		t.Fatalf("the next turn still carries the last one's ending: %q", got)
	}
}

// A PROGRAM'S LANDING ALWAYS WAKES THE CONVERSATION, owed or not, with the
// playbook as its role page, the conversation's own model, and the ending on
// the note as a fact.
func TestAProgramsLandingWakesATurnWithThePlaybook(t *testing.T) {
	completer := &scriptedCompleter{steps: []step{finalText("It passes; its branch is task/x.")}}
	agent, _ := newTestAgent(t, completer, func(config *Config) { config.AskConsent = false })
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "the run", "1", "Repair", "repair the parser")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	program := testPrograms("senior-dev")[0]
	run := &beltRun{store: store, root: store.RootID(), row: 7, delegate: &program}
	summary := RunSummary{Outcome: beltRunOutcomeDone, Result: "submitted a change", ProgramVerdict: "pass-unverified"}

	agent.deliverBeltRunLanding(run, summary, RunLanding{})
	beltRunWaitFor(t, "the program outcome turn", func() bool { return completer.requests() == 1 })

	request := completer.request(0)
	var playbook bool
	for _, message := range request {
		playbook = playbook || strings.Contains(messageText(message), strings.TrimSpace(programOutcomePrompt))
	}
	if !playbook {
		t.Fatal("the program's outcome turn was not handed the playbook")
	}
	last := messageText(request[len(request)-1])
	if !strings.Contains(last, "task 7 · unverified · run 1") || !strings.Contains(last, "Run the project's checks on its branch yourself") {
		t.Fatalf("the outcome note = %q, want the verdict and what to do now", last)
	}
	if got := completer.model(0); got != agent.model {
		t.Fatalf("the outcome turn ran on %q, want the conversation's own model %q", got, agent.model)
	}
}

// A SECOND RUN IN A FOLDER THE FIRST LEFT ON ITS BRANCH CARRIES ON THERE. It
// names the person's own branch, keeps every run's work on one branch, and a
// run that adds nothing never deletes what the first one committed.
func TestASecondRunCarriesOnOnTheFirstRunsBranch(t *testing.T) {
	repo := newTestRepo(t)
	home := strings.TrimSpace(gitOut(t, repo, "rev-parse", "--abbrev-ref", "HEAD"))
	fake := testPrograms("fake")[0]
	first, err := PrepareProgramFolder(ProgramFolderOrder{Program: fake, Dir: repo, Title: "Build the parser", Holder: "task 1", Keep: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "parser.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if end := first.Finish("did not finish"); !end.Kept {
		t.Fatalf("the first run's work was not kept: %s", end.Sentence())
	}

	second, err := PrepareProgramFolder(ProgramFolderOrder{Program: fake, Dir: repo, Title: "Finish the parser", Holder: "task 2", Keep: t.TempDir()})
	if err != nil {
		t.Fatalf("the second run was refused: %v", err)
	}
	if second.Branch != first.Branch || second.Home != home || second.Start != first.Start || !second.Continues {
		t.Fatalf("the second run = branch %q home %q continues %v, want the first run's branch %q and the person's %q",
			second.Branch, second.Home, second.Continues, first.Branch, home)
	}
	if receipt := delegateReceipt(repo, fake, runCopyOf(second.tree())); !strings.Contains(receipt, "carrying on on its branch "+first.Branch) ||
		!strings.Contains(receipt, "your branch "+home+" does not move") {
		t.Fatalf("the second run's receipt = %q", receipt)
	}
	end := second.Finish("finished")
	if end.Dropped || !end.Kept || strings.TrimSpace(gitOut(t, repo, "rev-parse", "--abbrev-ref", "HEAD")) != first.Branch {
		t.Fatalf("a second run that added nothing threw the first run's work away: %s", end.Sentence())
	}
	if said := end.Sentence(); !strings.Contains(said, "your branch "+home+" is as it was") {
		t.Fatalf("the ending does not name the person's own branch: %s", said)
	}
	if files := gitOut(t, repo, "ls-tree", "--name-only", first.Branch); !strings.Contains(files, "parser.go") {
		t.Fatalf("the first run's work is gone from its branch:\n%s", files)
	}
}
