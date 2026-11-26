package factory

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

var afiBy = &TaskEventBy{Kind: TaskByAfi, Name: "Afi"}

// waitingTasks is n tasks each waiting on one agent Ask, the first `ofKind`
// of kind and the rest of other kinds, one each.
func waitingTasks(n, ofKind int, kind string) []WorkstreamTask {
	var out []WorkstreamTask
	for i := 0; i < n; i++ {
		k := kind
		if i >= ofKind {
			k = fmt.Sprintf("other %d", i)
		}
		out = append(out, WorkstreamTask{ID: fmt.Sprint("t", i), State: TaskNeeds, Owner: TaskOwner{Kind: TaskOwnerPerson, ID: "santosh"},
			Ask: &TaskAsk{ID: fmt.Sprint("a", i), Title: "Which wording?", Kind: k, By: afiBy, AskedAt: taskNow.Format(time.RFC3339Nano),
				Options: []TaskAskOption{{ID: "o1", Label: "Keep Afi's", Recommended: true}, {ID: "o2", Label: "Mine"}}}})
	}
	return out
}

func answered(kind string, recommended bool, at int) WorkstreamDecision {
	return WorkstreamDecision{ID: fmt.Sprint("d", at), State: DecisionKept, At: taskNow.Add(-time.Duration(at) * time.Hour).Format(time.RFC3339Nano),
		From: &DecisionFrom{Kind: DecisionFromAsk, AskKind: kind, Recommended: recommended}}
}

// TestAfiSuggestsAnExceptionOnlyPastTenWaitingAsks pins the threshold of
// owner decisions §13: MORE than ten Asks waiting, and the kind most share.
func TestAfiSuggestsAnExceptionOnlyPastTenWaitingAsks(t *testing.T) {
	r := DefaultRules("santosh")
	if s := SuggestException(r, waitingTasks(10, 6, "docs wording"), nil, taskNow); s != nil {
		t.Fatalf("ten waiting is not too many, got %+v", s)
	}
	s := SuggestException(r, waitingTasks(11, 6, "Docs  wording"), nil, taskNow)
	if s == nil {
		t.Fatal("eleven waiting, six of one kind, suggests nothing")
	}
	if s.Kind != "Docs wording" || s.Title != "Let Afi decide Docs wording here?" || s.Yes != "Yes, stop asking about Docs wording" {
		t.Fatalf("suggestion words: %+v", s)
	}
	if s.Waiting != 11 || s.OfKind != 6 || !strings.Contains(s.Why, "11 Asks are waiting here, and 6 of them are about Docs wording.") {
		t.Fatalf("suggestion counts: %+v", s)
	}
	// Two of a kind is not a pattern.
	if s := SuggestException(r, waitingTasks(11, 2, "docs wording"), nil, taskNow); s != nil {
		t.Fatalf("two of a kind suggests %+v", s)
	}
}

// TestTheSuggestionCitesHowTheStewardAnsweredThatKind pins the evidence line,
// and that a kind the steward mostly answers differently is never offered.
func TestTheSuggestionCitesHowTheStewardAnsweredThatKind(t *testing.T) {
	r := DefaultRules("santosh")
	var ds []WorkstreamDecision
	for i := 0; i < 12; i++ {
		ds = append(ds, answered("docs wording", i < 6, i))
	}
	s := SuggestException(r, waitingTasks(11, 5, "docs wording"), ds, taskNow)
	if s == nil || !strings.HasPrefix(s.Why, "Six of your last ten answers about docs wording took Afi's recommendation unchanged. ") {
		t.Fatalf("evidence: %+v", s)
	}
	var overruled []WorkstreamDecision
	for i := 0; i < 5; i++ {
		overruled = append(overruled, answered("docs wording", i == 0, i))
	}
	if s := SuggestException(r, waitingTasks(11, 5, "docs wording"), overruled, taskNow); s != nil {
		t.Fatalf("a kind answered against Afi four times in five is suggested: %+v", s)
	}
}

// TestNotNowRestsAKindAndAnExceptionEndsItsSuggestion pins the two ways a
// suggestion goes away.
func TestNotNowRestsAKindAndAnExceptionEndsItsSuggestion(t *testing.T) {
	tasks := waitingTasks(12, 5, "docs wording")
	r := DefaultRules("santosh")
	r.NotNow = []AskNotNow{{Kind: "docs wording", At: taskNow.Add(-time.Hour).Format(time.RFC3339Nano)}}
	if s := SuggestException(r, tasks, nil, taskNow); s != nil {
		t.Fatalf("a kind turned down an hour ago is suggested again: %+v", s)
	}
	if s := SuggestException(r, tasks, nil, taskNow.Add(SuggestionRestFor)); s == nil {
		t.Fatal("a week later the suggestion does not come back")
	}
	r = DefaultRules("santosh")
	r.Exceptions = []AskException{{Kind: "Docs wording"}}
	if s := SuggestException(r, tasks, nil, taskNow); s != nil {
		t.Fatalf("a kind already let go is suggested: %+v", s)
	}
}

// TestAPersonsAskAndTheServersOwnAsksAreNeverAKind pins that only agents'
// Asks count, and only agents' Asks are decided by an exception.
func TestAPersonsAskAndTheServersOwnAsksAreNeverAKind(t *testing.T) {
	ex := AskException{Kind: "docs wording"}
	ask := TaskAsk{Title: "Which wording?", Kind: "docs wording", By: afiBy, Options: []TaskAskOption{{ID: "o1", Label: "Keep it", Recommended: true}}}
	line, ok := ExceptionAssumed(ask, ex)
	if !ok || line != "Which wording? · Keep it (Afi decides docs wording here without asking)" {
		t.Fatalf("assumed line %q %v", line, ok)
	}
	person := ask
	person.By = &TaskEventBy{Kind: TaskByPerson, ID: "ravi"}
	if _, ok := ExceptionAssumed(person, ex); ok {
		t.Fatal("a person's Ask was decided for them")
	}
	none := ask
	none.Options = []TaskAskOption{{ID: "o1", Label: "Keep it"}}
	if _, ok := ExceptionAssumed(none, ex); ok {
		t.Fatal("an Ask with no recommended answer was assumed")
	}
	chase := ask
	chase.ID = InviteChaseAskID + "x"
	if _, ok := ExceptionAssumed(chase, ex); ok {
		t.Fatal("the server's own Ask counted as a kind")
	}
}

// TestCleanRulesKeepsTheNewRuleFieldsInShape pins the ask rule's three
// settings, the waits on offer, and exceptions without repeats.
func TestCleanRulesKeepsTheNewRuleFieldsInShape(t *testing.T) {
	r := DefaultRules("santosh")
	r.AsksBefore, r.FallbackAfterHours = AsksBeforeOutward, 24
	r.Exceptions = []AskException{{Kind: " docs  wording "}, {Kind: "Docs wording"}, {Kind: ""}}
	if err := CleanRules(&r); err != nil {
		t.Fatal(err)
	}
	if r.AsksBefore != "" || r.FallbackAfterHours != 0 || len(r.Exceptions) != 1 || r.Exceptions[0].Kind != "docs wording" {
		t.Fatalf("cleaned: %+v", r)
	}
	bad := DefaultRules("santosh")
	bad.AsksBefore = "sometimes"
	if CleanRules(&bad) == nil {
		t.Fatal("an unknown ask rule was kept")
	}
	bad = DefaultRules("santosh")
	bad.FallbackAfterHours = 5
	if CleanRules(&bad) == nil {
		t.Fatal("a wait not on offer was kept")
	}
}

// TestCleanRulesKeepsRulesInWordsTidy pins rules in words: spaces folded,
// blanks and repeats dropped, and none left is none kept.
func TestCleanRulesKeepsRulesInWordsTidy(t *testing.T) {
	r := DefaultRules("santosh")
	r.Said = []string{"  Nothing ships   without Priya's OK ", "", "Nothing ships without Priya's OK", "Keep it short"}
	if err := CleanRules(&r); err != nil {
		t.Fatal(err)
	}
	if len(r.Said) != 2 || r.Said[0] != "Nothing ships without Priya's OK" || r.Said[1] != "Keep it short" {
		t.Fatalf("said = %q", r.Said)
	}
	r.Said = []string{" "}
	if err := CleanRules(&r); err != nil || r.Said != nil {
		t.Fatalf("blank rules kept: %q %v", r.Said, err)
	}
	r.Said = []string{strings.Repeat("x", RulesSaidLongest+1)}
	if CleanRules(&r) == nil {
		t.Fatal("an over-long rule was kept")
	}
}

// TestTheAskRuleSettingsSayWhichTasksAskFirst pins the three settings'
// predictions.
func TestTheAskRuleSettingsSayWhichTasksAskFirst(t *testing.T) {
	edit := WorkstreamTask{Title: "Update the install guide", DoneBy: TaskDoneBy{Kind: TaskByAfi}, State: TaskUpNext}
	r := DefaultRules("santosh")
	if got := AsksFirst(edit, r); got != "" {
		t.Fatalf("outward rule stops an edit: %q", got)
	}
	r.AsksBefore = AsksBeforeSharedFile
	if got := AsksFirst(edit, r); got != "changing a shared file" {
		t.Fatalf("shared-file rule: %q", got)
	}
	r.AsksBefore = AsksBeforeEveryTask
	if got := AsksFirst(WorkstreamTask{Title: "Draft notes", DoneBy: TaskDoneBy{Kind: TaskByAfi}, State: TaskUpNext}, r); got != "starting" {
		t.Fatalf("every-task rule: %q", got)
	}
	if got := AskRuleLine(r); got != AsksBeforeEveryTaskRule {
		t.Fatalf("rule line %q", got)
	}
}

// TestTheStandinAnswersAfterTheWaitTheRulesSet pins who answers when the
// decider is away: the fallback, after the wait set, named on the Ask.
func TestTheStandinAnswersAfterTheWaitTheRulesSet(t *testing.T) {
	r := DefaultRules("santosh")
	r.Fallback, r.FallbackAfterHours = &RulesPerson{ID: "lena"}, 12
	s := StandinFor(r)
	asked := taskNow.Add(-13 * time.Hour).Format(time.RFC3339Nano)
	tasks := []WorkstreamTask{
		{ID: "old", State: TaskNeeds, Owner: TaskOwner{Kind: TaskOwnerPerson, ID: "ravi"}, Ask: &TaskAsk{ID: "a", AskedAt: asked},
			Asks: []TaskAsk{{ID: "b", AskedAt: taskNow.Format(time.RFC3339Nano), Decider: &PersonRef{ID: "maya"}}}},
		{ID: "own", State: TaskNeeds, Owner: TaskOwner{Kind: TaskOwnerPerson, ID: "lena"}, Ask: &TaskAsk{ID: "c", AskedAt: asked}},
	}
	FillCoveredBy(s, "Lena", tasks, taskNow)
	if c := tasks[0].Ask.CoveredBy; c == nil || c.ID != "lena" || c.Name != "Lena" {
		t.Fatalf("a 13-hour-old Ask is not covered: %+v", c)
	}
	if tasks[0].Asks[0].CoveredBy != nil {
		t.Fatal("a fresh Ask is covered")
	}
	if tasks[1].Ask.CoveredBy != nil {
		t.Fatal("the stand-in covers their own Ask")
	}
	lena := TaskActor{Handle: "lena", Edit: true}
	if !MayAnswerAsk(tasks[0], "a", lena, s, taskNow) || MayAnswerAsk(tasks[0], "a", lena, s, taskNow.Add(-2*time.Hour)) {
		t.Fatal("the stand-in's permission does not follow the 12-hour wait")
	}
}

// TestAnInviteThatWaitsThreeDaysAsksTheOwnerToHandItOn pins §13's chase: the
// Ask, its answers, and that it closes when the person joins.
func TestAnInviteThatWaitsThreeDaysAsksTheOwnerToHandItOn(t *testing.T) {
	invited := taskNow.Add(-InviteChaseAfter)
	task := WorkstreamTask{ID: "t1", Title: "Design the badge", State: TaskUpNext,
		Owner: TaskOwner{Kind: TaskOwnerPerson, ID: "santosh", Name: "Santosh"}, DoneBy: TaskDoneBy{Kind: TaskByPerson, ID: "invite:7", Name: "Ana"}}
	if InviteChaseDue(task, invited.Add(time.Minute), taskNow) {
		t.Fatal("chased before three days")
	}
	if !InviteChaseDue(task, invited, taskNow) {
		t.Fatal("not chased after three days")
	}
	asked, ev := ChaseInvite(task, "Ana", PersonRef{ID: "santosh", Name: "Santosh"}, taskIDs(), taskNow)
	if asked.State != TaskNeeds || len(asked.Asks) != 1 || ev.Text != "Afi asks Santosh: Hand to someone else?" {
		t.Fatalf("chase: %+v %+v", asked, ev)
	}
	ask := asked.Asks[0]
	if ask.Title != "Hand to someone else?" || !strings.HasPrefix(ask.Detail, "Ana was invited three days ago and has not joined") || len(ask.Options) != 3 {
		t.Fatalf("chase Ask: %+v", ask)
	}
	if InviteChaseDue(asked, invited, taskNow.Add(InviteChaseAfter)) {
		t.Fatal("chased twice while the first is open")
	}
	santosh := TaskActor{Handle: "santosh", Edit: true}
	by := TaskEventBy{Kind: TaskByPerson, ID: "santosh", Name: "Santosh"}
	ch, err := AnswerTask(asked, ask.ID, InviteChaseMe, "", santosh, by, Standin{}, taskIDs(), taskNow)
	if err != nil || ch.Task.DoneBy.ID != "santosh" || ch.Task.State != TaskUpNext || len(ch.Decisions) != 0 {
		t.Fatalf("Hand it to me: %+v %v", ch.Task, err)
	}
	ch, _ = AnswerTask(asked, ask.ID, InviteChaseAfi, "", santosh, by, Standin{}, taskIDs(), taskNow)
	if ch.Task.DoneBy.Kind != TaskByAfi {
		t.Fatalf("Give it to Afi: %+v", ch.Task.DoneBy)
	}
	ch, _ = AnswerTask(asked, ask.ID, InviteChaseWait, "", santosh, by, Standin{}, taskIDs(), taskNow)
	if ch.Task.DoneBy.ID != "invite:7" || InviteChaseDue(ch.Task, invited, taskNow.Add(time.Hour)) || !InviteChaseDue(ch.Task, invited, taskNow.Add(InviteChaseAfter)) {
		t.Fatal("Keep waiting does not ask again three days later")
	}
	joined, ok := RebindPerson(asked, "invite:7", "ana", "Ana", taskNow)
	if !ok || len(joined.Asks) != 0 || joined.State != TaskUpNext || joined.DoneBy.ID != "ana" {
		t.Fatalf("joining leaves the chase open: %+v", joined)
	}
}

// TestAnsweringRecordsTheKindAndWhetherAfiWasTakenAtItsWord pins the
// evidence a suggestion reads.
func TestAnsweringRecordsTheKindAndWhetherAfiWasTakenAtItsWord(t *testing.T) {
	task := waitingTasks(1, 1, "docs wording")[0]
	who := TaskActor{Handle: "santosh", Edit: true}
	by := TaskEventBy{Kind: TaskByPerson, ID: "santosh", Name: "Santosh"}
	ch, err := AnswerTask(task, "", "o1", "", who, by, Standin{}, taskIDs(), taskNow)
	if err != nil || len(ch.Decisions) != 1 || ch.Decisions[0].From.AskKind != "docs wording" || !ch.Decisions[0].From.Recommended {
		t.Fatalf("decision: %+v %v", ch.Decisions, err)
	}
	ch, _ = AnswerTask(task, "", "o2", "", who, by, Standin{}, taskIDs(), taskNow)
	if ch.Decisions[0].From.Recommended {
		t.Fatal("the other answer read as Afi's")
	}
}
