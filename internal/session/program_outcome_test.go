package session

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/delegate"
	"github.com/Agent-Field/codeaf/internal/exec"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/provider"
	"github.com/Agent-Field/codeaf/internal/provider/modelapi"
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
	// A PROGRAM'S OWN CHECK IS A LEAD, NOT A VERDICT: a change it handed in
	// whose check did not pass is looked into before anything is fixed.
	if unverified := note(programUnverified, 0); !strings.Contains(unverified, "run the project's own checks on its branch yourself, and act only on what yours show") ||
		!strings.Contains(unverified, "a lead, not a verdict") || strings.Contains(unverified, "hand the work back") {
		t.Fatalf("an ending whose own check did not pass is not told to check the branch first:\n%s", unverified)
	}
	if passed := note(programPassed, 0); !strings.Contains(passed, "offer to merge") {
		t.Fatalf("a pass is not told to offer the merge:\n%s", passed)
	}
	for _, verdict := range []programVerdict{programPassed, programUnverified, programFailed, programCrashed} {
		for _, want := range []string{"worktree add -q --detach \"$tmp\" <branch>", "cd \"$tmp\" && <check command>", "worktree remove --force \"$tmp\"", "Do not run checks in the person's checkout", "copy was kept", "no git history"} {
			if got := note(verdict, 0); !strings.Contains(got, want) {
				t.Errorf("%s landing lacks %q:\n%s", verdict, want, got)
			}
		}
	}
}

// A passed program's branch was merged into the person's main by the wake
// turn, even though nobody had asked to bring it over. The instruction must
// govern every ending, before the verdict-specific next steps.
func TestAProgramOutcomeLeavesBringingItsBranchOverToThePerson(t *testing.T) {
	for _, want := range []string{
		"For every outcome",
		"merge, rebase, cherry-pick",
		"the person's call",
		"offer it and stop",
		"only when the person asks in a later message",
	} {
		if !strings.Contains(programOutcomePrompt, want) {
			t.Errorf("program outcome prompt does not teach %q", want)
		}
	}
}

// THE BOUNDS ARE CODE. A program ending keeps its retry cap until the person
// speaks, including across an unrelated automatic turn.
func TestARetryPastTheCapOrAfterALimitIsRefusedUntilThePersonSpeaks(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	set := func(o *programOutcome) {
		agent.mu.Lock()
		agent.programOutcomeNow = o
		agent.programHold = o
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
	if got := agent.programRetryRefusal("senior-dev"); !strings.Contains(got, "ask the person first") {
		t.Fatalf("the next automatic turn forgot the limit: %q", got)
	}
}

func TestAutomaticSeniorDevHandOffCapSurvivesTurnsAndReopenUntilPersonSpeaks(t *testing.T) {
	root := t.TempDir()
	config := Config{Workspace: root, Model: "test/model", System: "SYSTEM",
		SessionFile: filepath.Join(root, "conversation.jsonl")}
	a, err := newAgent(config, &scriptedCompleter{})
	if err != nil {
		t.Fatal(err)
	}
	for number := 0; number <= programAutoRetries; number++ {
		outcome := programOutcome{row: uint64(number + 1), program: "senior-dev", verdict: programFailed,
			programAttempt: programAttempt{attempt: number + 1, auto: number}}
		a.mu.Lock()
		a.rememberProgramOutcomeLocked(userMessage{programOutcome: &outcome})
		a.forgetOwedLocked()
		a.mu.Unlock()
		if number < programAutoRetries {
			if got := a.programRetryRefusal("senior-dev"); got != "" {
				t.Fatalf("handoff %d refused: %s", number+1, got)
			}
			a.keepProgramAttempt(uint64(number+2), a.programAttemptOf())
		}
	}
	if got := a.programRetryRefusal("senior-dev"); !strings.Contains(got, "sent back to this work") {
		t.Fatalf("third hand-off passed: %q", got)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := newAgent(config, &scriptedCompleter{})
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got := reopened.programRetryRefusal("senior-dev"); !strings.Contains(got, "sent back to this work") {
		t.Fatalf("reopen forgot the cap: %q", got)
	}
	reopened.mu.Lock()
	reopened.rememberOwedLocked(userText("please try again"))
	reopened.mu.Unlock()
	if got := reopened.programRetryRefusal("senior-dev"); got != "" {
		t.Fatalf("the person's new word did not reset the cap: %q", got)
	}
}

func TestAutomaticSeniorDevHandOffAfterLimitIsRefusedBeyondWakeTurn(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	outcome := programOutcome{row: 4, program: "senior-dev", verdict: programLimit,
		programAttempt: programAttempt{attempt: 1}}
	a.mu.Lock()
	a.rememberProgramOutcomeLocked(userMessage{programOutcome: &outcome})
	a.forgetOwedLocked()
	a.mu.Unlock()
	if got := a.programRetryRefusal("senior-dev"); !strings.Contains(got, "ask the person first") {
		t.Fatalf("later automatic turn passed the limit: %q", got)
	}
}

func TestFailedAutomaticProgramStartDoesNotUseAHandOff(t *testing.T) {
	a, _ := newTestAgent(t, &scriptedCompleter{}, nil)
	outcome := programOutcome{row: 4, program: "senior-dev", verdict: programFailed,
		programAttempt: programAttempt{attempt: 1}}
	a.mu.Lock()
	a.rememberProgramOutcomeLocked(userMessage{programOutcome: &outcome})
	a.mu.Unlock()
	attempt := a.programAttemptOf()
	prior := a.keepProgramAttempt(5, attempt)
	a.rollbackProgramAttempt(5, prior)
	if got := a.programAttemptOf(); got != attempt {
		t.Fatalf("a start that failed consumed an automatic hand-off: %+v, want %+v", got, attempt)
	}
	if got := a.programRetryRefusal("senior-dev"); got != "" {
		t.Fatalf("a start that failed barred another attempt: %s", got)
	}
}

func TestProgramCommitCreditsOnlyItsAnsweredModels(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		turns      []delegate.Turn
	}{
		{"two answered models", exec.AttributionAssistedBy + " (m2.7, k2.6)", []delegate.Turn{
			{Seq: 1, Model: "crew/unused", Refused: "limit", Ended: time.Now()},
			{Seq: 2, Model: "asked/model", Served: "minimax/m2.7", Ended: time.Now(), Reply: "yes"},
			{Seq: 3, Model: "kimi/k2.6", Ended: time.Now(), Reply: "done"},
		}},
		{"no answered calls", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := newTestRepo(t)
			program := testPrograms("senior-dev")[0]
			folder, err := PrepareProgramFolder(ProgramFolderOrder{Program: program, Dir: repo,
				Title: "Repair", Holder: "test", Keep: t.TempDir(), SignModel: "crew/unused"})
			if err != nil {
				t.Fatal(err)
			}
			for _, turn := range tc.turns {
				if err := delegate.AppendTurn(folder.Keep, turn); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(folder.Dir, "repair.txt"), []byte("done\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := SetProgramAnswerAttribution(folder, true); err != nil {
				t.Fatal(err)
			}
			folder.Finish("finished")
			message := gitOut(t, repo, "log", "-1", "--format=%B", folder.Branch)
			if tc.want == "" {
				if strings.Contains(message, "Assisted-by:") {
					t.Fatalf("no answered call still signed the commit: %s", message)
				}
			} else if !strings.Contains(message, tc.want) || strings.Contains(message, "crew/unused") {
				t.Fatalf("commit attribution = %s", message)
			}
		})
	}
}

func TestUnreadableProgramCallLogNeverCreditsTheConfiguredSeat(t *testing.T) {
	repo := newTestRepo(t)
	program := testPrograms("senior-dev")[0]
	folder, err := PrepareProgramFolder(ProgramFolderOrder{Program: program, Dir: repo,
		Title: "Repair", Holder: "test", Keep: t.TempDir(), SignModel: "crew/unused"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(folder.Keep, delegate.ConversationFile), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder.Dir, "repair.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetProgramAnswerAttribution(folder, true); err == nil {
		t.Fatal("unreadable call log was accepted")
	}
	folder.Finish("finished")
	if message := gitOut(t, repo, "log", "-1", "--format=%B", folder.Branch); strings.Contains(message, "Assisted-by:") {
		t.Fatalf("unreadable call log credited the configured seat: %s", message)
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
	if !strings.Contains(last, "task 7 · unverified · run 1") || !strings.Contains(last, "run the project's own checks on its branch yourself") {
		t.Fatalf("the outcome note = %q, want the verdict and what to do now", last)
	}
	if got := completer.model(0); got != agent.model {
		t.Fatalf("the outcome turn ran on %q, want the conversation's own model %q", got, agent.model)
	}
}

// The session writes a limit ending before asking for another model turn,
// because that same limit may refuse the wake.
type chargedSeniorDevStub struct{ calls atomic.Int32 }

func (s *chargedSeniorDevStub) CompleteWithMessages(ctx context.Context, _ []ai.Message, _ ...ai.Option) (*ai.Response, error) {
	s.calls.Add(1)
	if sink := provider.BillingSinkFrom(ctx); sink != nil {
		sink(provider.Billed{Model: "stub/model", PromptTokens: 100, CompletionTokens: 20, Cost: 0.40})
	}
	return textResponse("stub answer"), nil
}

func TestSeniorDevLimitLandingIsVisibleWhenTheWakeIsRefused(t *testing.T) {
	completer := &scriptedCompleter{}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.SpendRailUSD = 1
		config.AskConsent = false
	})
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "the run", "1", "Repair", "repair the parser")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	program := testPrograms("senior-dev")[0]
	updates, stop := agent.WatchTaskUpdates()
	defer stop()
	run := &beltRun{store: store, root: store.RootID(), row: 7, delegate: &program,
		ground: "/project", costCeiling: 1, conversationCostLimit: true}
	agent.beltMu.Lock()
	agent.beltRun = run
	agent.beltMu.Unlock()
	stub := &chargedSeniorDevStub{}
	model, err := modelapi.Open(modelapi.Config{
		Ceiling:      1,
		CompleterFor: func(string) modelapi.Completer { return stub },
		Bank: func(charge modelapi.Charge) {
			run.spent = charge.Spent
			agent.mu.Lock()
			agent.usage.CostUSD = charge.Spent
			agent.mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer model.Close()
	api := model.API()
	for range 3 {
		request, err := http.NewRequest(http.MethodPost, modelapi.ChatURL(api.BaseURL), strings.NewReader(`{"model":"stub/model","messages":[{"role":"user","content":"work"}]}`))
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+api.Token)
		request.Header.Set("Content-Type", "application/json")
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatalf("stub call: status %d, body %s, read error %v", response.StatusCode, body, err)
		}
	}
	if got := stub.calls.Load(); got != 3 || run.spent < 1.19 || run.spent > 1.21 {
		t.Fatalf("stub answered %d calls and spent $%.2f, want three $0.40 calls", got, run.spent)
	}
	agent.deliverBeltRunLanding(run, RunSummary{Outcome: "a limit you set stopped it", Limit: RunLimitCost, USD: run.spent},
		RunLanding{Branch: "task/repair"})
	agent.mu.Lock()
	var transcript string
	for _, message := range agent.messages {
		transcript += messageContentText(message) + "\n"
	}
	agent.mu.Unlock()
	for _, want := range []string{"stopped at the conversation's $1.00 limit", "spent $1.20", "task/repair"} {
		if !strings.Contains(transcript, want) {
			t.Fatalf("limit ending lacks %q: %s", want, transcript)
		}
	}
	if completer.requests() != 0 {
		t.Fatalf("the blocked wake called the model %d times", completer.requests())
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case event := <-updates:
			if event.Kind == EventNotice && strings.Contains(event.Text, "stopped at the conversation's $1.00 limit") {
				return
			}
		case <-deadline:
			t.Fatal("the open surface was not sent the limit line")
		}
	}
}

func TestSeniorDevTimeLimitLineNamesTheFolderWithoutABranch(t *testing.T) {
	program := testPrograms("senior-dev")[0]
	run := &beltRun{delegate: &program, ground: "/project", timeCeiling: 0.5, spent: 0.40}
	for _, summary := range []RunSummary{
		{Limit: RunLimitTime, USD: 0.40},
		{Program: &ProgramEnding{Status: delegate.StatusBudget, Reason: "senior-dev stopped on its own ceiling: wall 1800s >= budget 1800s"}, USD: 0.40},
	} {
		line := programLimitLine(run, summary, RunLanding{})
		for _, want := range []string{"stopped at the run's 30m limit", "spent $0.40", "in the folder /project"} {
			if !strings.Contains(line, want) {
				t.Fatalf("time limit line lacks %q: %s", want, line)
			}
		}
	}
}

// A RUN SENT BACK CARRIES ON ON THE FIRST RUN'S BRANCH. A second run of the
// same line — codeaf handing the work back after an ending, before the person
// has spoken — is handed the branch the first run's row wrote down, so it
// starts from that work in a copy of its own rather than from the person's
// checkout, its receipt says so, and a second run that adds nothing never
// counts the first run's work as nothing. A different program's line is not
// carried, and neither is a first run.
func TestASecondRunCarriesOnOnTheFirstRunsBranch(t *testing.T) {
	double := newBeltRunDouble("done")
	double.work = func(workspace string) { writeFile(t, filepath.Join(workspace, "parser.go"), "package p\n") }
	registerBeltRunEngine(t, double)
	repo := newTestRepo(t)
	home := currentBranch(repo)
	agent, _ := newTestAgent(t, beltRunCompleter{text: "done"}, func(config *Config) {
		config.Workspace = repo
		config.Place = Place{Dir: t.TempDir()}
		config.AskConsent = false
		config.Delegates = testPrograms("fake")
	})
	id, _, _, err := agent.StartDelegate(context.Background(), "fake", "build the parser")
	if err != nil {
		t.Fatal(err)
	}
	<-double.entered
	endBeltRun(t, agent, double)
	if agent.programCarryOf(nil, "fake") != nil {
		t.Fatal("a line's first run carries a branch")
	}
	prior := &programOutcome{row: id, program: "fake", verdict: programFailed}
	if agent.programCarryOf(prior, "another") != nil {
		t.Fatal("another program's line carries this one's branch")
	}
	carry := agent.programCarryOf(prior, "fake")
	first := agent.runRowCopy(id)
	if carry == nil || carry.Branch != first.Branch || canonicalPath(carry.Root) != canonicalPath(repo) || carry.Home != home || carry.Fresh {
		t.Fatalf("the carried branch = %+v, want the first run's %+v", carry, first)
	}
	// A RUN WHOSE WORK PASSED IS TAKEN UP ON A NEW BRANCH, never written again.
	if passed := agent.programCarryOf(&programOutcome{row: id, program: "fake", verdict: programPassed}, "fake"); passed == nil || !passed.Fresh || passed.Branch != first.Branch {
		t.Fatalf("the run after a pass carries %+v, want a new branch cut from %s", passed, first.Branch)
	}

	fake := testPrograms("fake")[0]
	second, err := PrepareProgramFolder(ProgramFolderOrder{Program: fake, Dir: repo, Title: "Finish the parser", Holder: "task 2", Keep: t.TempDir(), Carry: carry})
	if err != nil {
		t.Fatalf("the second run was refused: %v", err)
	}
	if second.Branch != first.Branch || second.Home != home || second.Start != first.HomeSha || !second.Continues {
		t.Fatalf("the second run = branch %q home %q continues %v, want the first run's branch %q and the person's %q",
			second.Branch, second.Home, second.Continues, first.Branch, home)
	}
	if _, err := os.Stat(filepath.Join(second.Dir, "parser.go")); err != nil {
		t.Fatalf("the first run's work is not in the second run's copy: %v", err)
	}
	if receipt := delegateReceipt(repo, fake, runCopyOf(second.tree())); !strings.Contains(receipt, "carrying on on its branch "+first.Branch) {
		t.Fatalf("the second run's receipt = %q", receipt)
	}
	end := second.Finish("finished")
	if !end.Kept || currentBranch(repo) != home {
		t.Fatalf("a second run that added nothing threw the first run's work away: %s", end.Sentence())
	}
	if files := gitOut(t, repo, "ls-tree", "--name-only", first.Branch); !strings.Contains(files, "parser.go") {
		t.Fatalf("the first run's work is gone from its branch:\n%s", files)
	}
}

// A FIX THE CHAT WOULD MAKE ITSELF GOES BACK TO THE PROGRAM. The wake turn is
// the one moment a hand-off carries on from the program's own branch, and the
// page once taught the chat to patch that branch in a worktree instead, so a
// follow-up the program was built for was done by the conversation.
func TestAProgramOutcomeCountsAHandBackAsFixingItYourself(t *testing.T) {
	for _, want := range []string{
		"handing it back to the same program with propose_task and via counts as doing it yourself",
		"only for a trivial gap",
	} {
		if !strings.Contains(programOutcomePrompt, want) {
			t.Errorf("program outcome prompt does not teach %q", want)
		}
	}
}

// A PROGRAM THAT ANSWERS WITH A REPORT IS NEVER SENT LOOKING FOR A BRANCH. Its
// ending changed nothing in the folder, so its note carries none of the
// worktree, check or merge advice a change gets: it says what the report is
// for, the program's own offer, and that nothing starts on this turn.
func TestAReportProgramsEndingAsksForASummaryAndStartsNothing(t *testing.T) {
	const offer = "If it found problems, offer to hand them to senior-dev to fix."
	note := func(verdict programVerdict, auto int) string {
		return programOutcomeNote(programOutcome{row: 9, program: "sec", verdict: verdict,
			report: true, followUp: offer, programAttempt: programAttempt{attempt: auto + 1, auto: auto}}, "done · ran 14m", 0.8)
	}
	passed := note(programPassed, 0)
	for _, want := range []string{"[sec ended — for you to act on] task 9 · passed · run 1 · $0.80",
		"it changed nothing in the folder", offer, "Start nothing on this turn."} {
		if !strings.Contains(passed, want) {
			t.Fatalf("a finished report's note lacks %q:\n%s", want, passed)
		}
	}
	for _, verdict := range []programVerdict{programPassed, programFailed, programLimit, programCrashed} {
		got := note(verdict, 0)
		for _, unwanted := range []string{"worktree", "merge", "branch"} {
			if strings.Contains(got, unwanted) {
				t.Errorf("a %s report's note speaks of a %s:\n%s", verdict, unwanted, got)
			}
		}
	}
	if limit := note(programLimit, 0); !strings.Contains(limit, "ask whether to spend more") || strings.Contains(limit, offer) {
		t.Fatalf("a report that stopped on a limit is not told to ask first:\n%s", limit)
	}
	if failed := note(programFailed, 0); strings.Contains(failed, "hand the same work") {
		t.Fatalf("a report that did not finish is sent back on codeaf's own:\n%s", failed)
	}
	if crashed := note(programCrashed, programAutoRetries); strings.Contains(crashed, "hand the same work") {
		t.Fatalf("a crash after the last retry is still sent back:\n%s", crashed)
	}
}

// A report program's ending that named no `pass` is still a finished answer,
// and its limit line points at nothing in the folder, because it changed
// nothing there.
func TestAReportProgramsRunReadsAsFinishedAndItsLimitLineNamesNoWork(t *testing.T) {
	a := programConversation(t, nil)
	program := testPrograms("sec")[0]
	program.Lands = delegate.LandsText
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "the audit", planRootID, "The audit", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	run := &beltRun{row: 2, root: planRootID, store: store, delegate: &program, ground: "/repo", costCeiling: 2}
	note := a.programLandingNote(run, RunSummary{Outcome: beltRunOutcomeDone}, "done")
	if note.programOutcome == nil || note.programOutcome.verdict != programPassed || !note.programOutcome.report {
		t.Fatalf("a finished report reads as %+v, want a passed report", note.programOutcome)
	}
	line := programLimitLine(run, RunSummary{Outcome: "incomplete", Limit: RunLimitCost, USD: 2}, RunLanding{})
	if !strings.Contains(line, "it changed nothing in /repo") || strings.Contains(line, "its work is") {
		t.Fatalf("a report's limit line = %q", line)
	}
}
