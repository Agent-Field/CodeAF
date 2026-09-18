package session

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/plandb"
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
	close(double.release)
}

func TestOwedRootLandingWakesOnceWithOnlyQuestionAndResult(t *testing.T) {
	question := "What did the repair find?"
	result := "The parser now preserves quoted commas."
	task := &plandb.Task{TaskSpec: plandb.TaskSpec{Question: question}}
	doc := owedLandingDocument(task, result)
	if got := doc.text(); got != question+"\n\n"+result {
		t.Fatalf("owed landing document = %q, want only question and result", got)
	}
	if got := owedLandingCallCeiling(); got != settleCallCeiling {
		t.Fatalf("owed landing ceiling = %d, want settleCallCeiling %d", got, settleCallCeiling)
	}
	if got := owedLandingTier(); got != "low" {
		t.Fatalf("owed landing tier = %q, want low", got)
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
