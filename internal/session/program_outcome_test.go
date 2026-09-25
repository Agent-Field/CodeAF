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
	if unverified := note(programUnverified, 0); !strings.Contains(unverified, "Run the project's checks on its branch yourself") {
		t.Fatalf("an unverified ending is not told to check the branch:\n%s", unverified)
	}
	if passed := note(programPassed, 0); !strings.Contains(passed, "offer to merge") {
		t.Fatalf("a pass is not told to offer the merge:\n%s", passed)
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
			if err := os.WriteFile(filepath.Join(repo, "repair.txt"), []byte("done\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := SetProgramAnswerAttribution(folder, true); err != nil {
				t.Fatal(err)
			}
			folder.Finish("finished")
			message := gitOut(t, repo, "log", "-1", "--format=%B")
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
	if err := os.WriteFile(filepath.Join(repo, "repair.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SetProgramAnswerAttribution(folder, true); err == nil {
		t.Fatal("unreadable call log was accepted")
	}
	folder.Finish("finished")
	if message := gitOut(t, repo, "log", "-1", "--format=%B"); strings.Contains(message, "Assisted-by:") {
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
	if !strings.Contains(last, "task 7 · unverified · run 1") || !strings.Contains(last, "Run the project's checks on its branch yourself") {
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
