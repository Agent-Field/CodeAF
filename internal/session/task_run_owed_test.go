package session

import (
	"context"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/agentfield/sdk/go/ai"
	"github.com/Agent-Field/codeaf/internal/plandb"
	"github.com/Agent-Field/codeaf/internal/roles"
)

func TestLandingOwesAnswerOnlyAtAnOwedWorkRoot(t *testing.T) {
	question := "What did the repair find?"
	cases := []struct {
		name string
		task *plandb.Task
		want bool
	}{
		{"owed root", &plandb.Task{TaskSpec: plandb.TaskSpec{Question: question}}, true},
		{"unowed root", &plandb.Task{TaskSpec: plandb.TaskSpec{}}, false},
		{"owed child", &plandb.Task{TaskSpec: plandb.TaskSpec{ParentID: "root", Question: question}}, false},
		{"owed check", &plandb.Task{TaskSpec: plandb.TaskSpec{Role: plandb.RoleCheck, Question: question}}, false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := landingOwesAnswer(tc.task); got != tc.want {
				t.Fatalf("landingOwesAnswer = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPersonTypedTaskCarriesNoQuestion(t *testing.T) {
	t.Setenv("CODEAF_TASK_BELT", "bash")
	double := newBeltRunDouble("finished")
	registerBeltRunEngine(t, double)
	dir := t.TempDir()
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(config *Config) {
		config.Workspace = newTestRepo(t)
		config.Place = Place{Dir: dir}
		config.AskConsent = false
	})
	agent.taskNow = (&fakeClock{at: time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)}).now
	id, _, _, err := agent.StartTask(context.Background(), "repair the parser", false)
	if err != nil {
		t.Fatalf("StartTask: %v", err)
	}
	if got := beltRunTaskAt(t, dir, strconv.FormatUint(id, 10)).Question; got != "" {
		t.Fatalf("person-typed /task question = %q, want empty", got)
	}
	endBeltRun(t, agent, double)
}

func TestOwedRootLandingWakesOnceWithOnlyQuestionAndResult(t *testing.T) {
	question := "What did the repair find?"
	summary := RunSummary{Outcome: beltRunOutcomeDone, Result: "The parser now preserves quoted commas."}
	landing := RunLanding{}
	wantDocument := question + "\n\n" + beltRunOutcomeNote(nil, "", summary, landing, 0)

	completer := &scriptedCompleter{steps: []step{finalText("The repair preserved quoted commas.")}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierLow): "test/cheap-model",
		})
	})
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "the run", "1", "Repair", "repair the parser")
	if err != nil {
		t.Fatalf("open owed run store: %v", err)
	}
	defer store.Close()
	if _, err := store.Revise(store.RootID(), plandb.TaskPatch{Question: &question}); err != nil {
		t.Fatalf("mark root answer owed: %v", err)
	}
	run := &beltRun{store: store, root: store.RootID()}

	wakes, stopWakes := agent.WatchWakes()
	defer stopWakes()
	agent.deliverBeltRunLanding(run, summary, landing)
	beltRunWaitFor(t, "the owed landing reply", func() bool { return completer.requests() == 1 })

	request := completer.request(0)
	var promptSeen bool
	for _, message := range request {
		promptSeen = promptSeen || strings.Contains(messageText(message), strings.TrimSpace(landingAnswerPrompt))
	}
	if !promptSeen {
		t.Fatal("owed landing request omitted its dedicated answer-from-result prompt")
	}
	if got := messageText(request[len(request)-1]); got != wantDocument {
		t.Fatalf("owed landing request document = %q, want only question and result note %q", got, wantDocument)
	}
	if got := completer.model(0); got != "test/cheap-model" {
		t.Fatalf("owed landing model = %q, want low-tier model", got)
	}
	if got := completer.requests(); got != 1 {
		t.Fatalf("owed landing made %d model requests, want one reply turn", got)
	}
	select {
	case <-wakes:
	default:
		t.Fatal("owed landing published no wake")
	}
	select {
	case <-wakes:
		t.Fatal("owed landing published more than one wake")
	default:
	}
	if got := owedLandingCallCeiling(); got != settleCallCeiling {
		t.Fatalf("owed landing ceiling = %d, want settleCallCeiling %d", got, settleCallCeiling)
	}
}

func TestAWorkFamilyRepliesOnlyAtTheOwedRoot(t *testing.T) {
	question := "What did the family produce?"
	root := &plandb.Task{TaskSpec: plandb.TaskSpec{ID: "root", Question: question}}
	child := &plandb.Task{TaskSpec: plandb.TaskSpec{ID: "child", ParentID: "root", Question: question}}
	check := &plandb.Task{TaskSpec: plandb.TaskSpec{ID: "check", Role: plandb.RoleCheck, Question: question}}
	if landingOwesAnswer(child) || landingOwesAnswer(check) {
		t.Fatal("a child or check landing claimed the family's reply")
	}
	if !landingOwesAnswer(root) {
		t.Fatal("the owed root landing did not claim the family's one reply")
	}
}

func TestModelHandoffCarriesThePersonsQuestionAndNothingElse(t *testing.T) {
	question := "Which landing behavior is missing?"
	if got := questionAtTaskHandoff([]owedAsk{{from: owedByPerson, text: question}}); got != question {
		t.Fatalf("model handoff question = %q, want the person's ask %q", got, question)
	}
	if got := questionAtTaskHandoff([]owedAsk{{from: owedByBackground, text: "a task landed"}}); got != "" {
		t.Fatalf("background-only handoff question = %q, want empty", got)
	}
	if got := questionAtTaskHandoff(nil); got != "" {
		t.Fatalf("handoff without an ask question = %q, want empty", got)
	}
}

func TestOwedLandingCompletionReaderSeesQuestionAsAskAndOutcomeAsEvidence(t *testing.T) {
	question := "What is the test's name once it lands?"
	summary := RunSummary{Outcome: beltRunOutcomeDone, Result: "Test function name: TestDouble."}
	landing := RunLanding{Branch: "main", Changed: []string{"double.go", "double_test.go"}}
	line := beltRunOutcomeNote(nil, "", summary, landing, 0)

	completer := &scriptedCompleter{steps: []step{finalText("The test is TestDouble."), finalText(checkpointNothingLeft)}}
	agent, _ := newTestAgent(t, completer, func(config *Config) {
		config.AskConsent = false
		config.RolesSource = tierSettings(map[string]string{
			roles.TierKey(roles.TierLow):        "test/cheap-model",
			roles.TierKey(roles.TierMastermind): "test/reader-model",
		})
	})
	store, err := plandb.Open(filepath.Join(t.TempDir(), planStoreFilename), "the run", "1", "Double", "add Double")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.Revise(store.RootID(), plandb.TaskPatch{Question: &question}); err != nil {
		t.Fatal(err)
	}
	run := &beltRun{store: store, root: store.RootID()}
	agent.deliverBeltRunLanding(run, summary, landing)
	beltRunWaitFor(t, "landing answer turn", func() bool {
		agent.mu.Lock()
		defer agent.mu.Unlock()
		return !agent.running && completer.requests() >= 1
	})
	var request []ai.Message
	for index := 0; index < completer.requests(); index++ {
		candidate := completer.request(index)
		if strings.Contains(messageText(candidate[len(candidate)-1]), checkpointRemainsAsk) {
			request = candidate
			break
		}
	}
	if request == nil {
		_ = agent.readRemains(context.Background())
		request = completer.request(completer.requests() - 1)
	}
	page := messageText(request[len(request)-1])
	if !strings.HasPrefix(page, checkpointDigestAsked+"\n"+question+"\n\n") {
		t.Fatalf("completion request ask = %q, want the person's question alone", page)
	}
	found := strings.Index(page, checkpointDigestFound+"\n")
	if found < 0 || !strings.Contains(page[found:], line) {
		t.Fatalf("completion request omitted landing outcome under %q: %q", checkpointDigestFound, page)
	}
}

func TestCompletionSnapshotWithoutLandingIsExactlyUnchanged(t *testing.T) {
	agent, _ := newTestAgent(t, &scriptedCompleter{}, func(*Config) {})
	before := agent.snapshot()
	after := agent.completionSnapshot()
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("ordinary completion snapshot changed:\n got %#v\nwant %#v", after, before)
	}
}
