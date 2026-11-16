package factory

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"
)

var taskNow = time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)

func taskIDs() func() string {
	n := 0
	return func() string { n++; return "te_" + strconv.Itoa(n) }
}

func personTask(id, owner string) WorkstreamTask {
	return WorkstreamTask{
		ID: id, WS: "ws_pantry", Rev: 1, Title: "Run five user interviews", State: TaskUpNext,
		Owner: TaskOwner{Kind: TaskOwnerPerson, ID: owner, Name: owner}, DoneBy: TaskDoneBy{Kind: TaskByPerson, ID: owner, Name: owner},
		WaitingOn: []string{}, Results: []TaskResult{}, CreatedBy: PersonRef{ID: "priya", Name: "priya"},
	}
}

func afiTask(id, owner string, waits ...string) WorkstreamTask {
	t := personTask(id, owner)
	t.Title, t.DoneBy, t.WaitingOn = "Post the notes to Discord", TaskDoneBy{Kind: TaskByAfi}, waits
	if t.WaitingOn == nil {
		t.WaitingOn = []string{}
	}
	return t
}

func asking(t WorkstreamTask, at time.Time) WorkstreamTask {
	t.State = TaskNeeds
	t.Ask = &TaskAsk{ID: "a1", Title: "Publish the notes to the website?", AskedAt: at.Format(time.RFC3339Nano),
		Options: []TaskAskOption{{ID: "publish", Label: "Publish notes", Recommended: true}}}
	return t
}

var byPriya = TaskEventBy{Kind: TaskByPerson, ID: "priya", Name: "priya"}

func TestOnlyTheOwnerAnswersAnAsk(t *testing.T) {
	task := asking(afiTask("t1", "marco"), taskNow)
	if _, err := AnswerTask(task, "publish", "", TaskActor{Handle: "priya", Edit: true}, byPriya, "", taskIDs(), taskNow); !errors.Is(err, ErrTaskNotAnswerer) {
		t.Fatalf("priya answered marco's Ask: %v", err)
	}
	ch, err := AnswerTask(task, "publish", "", TaskActor{Handle: "marco", Edit: true}, byPriya, "", taskIDs(), taskNow)
	if err != nil || ch.Task.Ask != nil || ch.Task.AnsweredBy.ID != "marco" || ch.Events[0].Kind != TaskEventAnswered || ch.Events[0].Option != "publish" {
		t.Fatalf("marco's answer: %+v %v", ch, err)
	}
}

func TestWithOwnerEveryoneTheFirstAnswerDecidesAndIsRecorded(t *testing.T) {
	task := asking(afiTask("t1", ""), taskNow)
	task.Owner = TaskOwner{Kind: TaskOwnerEveryone}
	if MayAnswer(task, TaskActor{Handle: "abir", Edit: false}, "", taskNow) {
		t.Fatal("someone who only views answered")
	}
	ch, err := AnswerTask(task, "publish", "", TaskActor{Handle: "abir", Edit: true}, byPriya, "", taskIDs(), taskNow)
	if err != nil || ch.Task.AnsweredBy == nil || ch.Task.AnsweredBy.ID != "abir" {
		t.Fatalf("abir's answer: %+v %v", ch.Task.AnsweredBy, err)
	}
	if _, err := AnswerTask(ch.Task, "publish", "", TaskActor{Handle: "santosh", Edit: true}, byPriya, "", taskIDs(), taskNow); !errors.Is(err, ErrTaskNoAsk) {
		t.Fatalf("a second answer was taken: %v", err)
	}
}

func TestAnAskUnansweredForADayAlsoGoesToTheFallback(t *testing.T) {
	young := asking(afiTask("t1", "marco"), taskNow.Add(-TaskFallbackAfter+time.Minute))
	old := asking(afiTask("t2", "marco"), taskNow.Add(-TaskFallbackAfter))
	lena := TaskActor{Handle: "lena", Edit: true}
	if MayAnswer(young, lena, "lena", taskNow) {
		t.Fatal("the fallback answered before a day")
	}
	if !MayAnswer(old, lena, "lena", taskNow) {
		t.Fatal("the fallback cannot answer a day-old Ask")
	}
	if MayAnswer(old, lena, "", taskNow) {
		t.Fatal("with no fallback, a day changes nothing")
	}
}

func TestOnlyTheOwnerTicksAPersonTaskAndTickingReleasesItsDependents(t *testing.T) {
	interviews := personTask("t1", "lena")
	post := afiTask("t2", "priya", "t1")
	signoff := personTask("t3", "priya")
	signoff.WaitingOn = []string{"t1"}
	all := []WorkstreamTask{interviews, post, signoff}
	done := TaskDone
	if _, err := ApplyPatch(interviews, TaskPatch{State: &done}, TaskActor{Handle: "priya", Edit: true}, byPriya, all, taskIDs(), taskNow); err == nil {
		t.Fatal("priya ticked lena's task")
	}
	ch, err := ApplyPatch(interviews, TaskPatch{State: &done}, TaskActor{Handle: "lena", Edit: true}, byPriya, all, taskIDs(), taskNow)
	if err != nil || ch.Task.State != TaskDone || ch.Task.DoneAt == "" {
		t.Fatalf("lena's tick: %+v %v", ch.Task, err)
	}
	// Released, not running: nothing has woken Afi (live use, L1 F5). It stays
	// up next, unchanged, and the person task is left to its owner.
	if len(ch.Released) != 1 || ch.Released[0].ID != "t2" || ch.Released[0].State != TaskUpNext || ch.Released[0].Rev != post.Rev {
		t.Fatalf("released %+v; want Afi's waiting task named, still up next", ch.Released)
	}
}

func TestWaitingOnIsTheSameWorkstreamNeverItselfNeverACircle(t *testing.T) {
	a, b := afiTask("a", "priya"), afiTask("b", "priya", "a")
	all := []WorkstreamTask{a, b}
	if err := CheckWaits("a", []string{"a"}, all); err == nil {
		t.Fatal("a task waited on itself")
	}
	if err := CheckWaits("a", []string{"b"}, all); err == nil {
		t.Fatal("a circle was allowed")
	}
	if err := CheckWaits("a", []string{"elsewhere"}, all); err == nil {
		t.Fatal("a wait on a task of another workstream was allowed")
	}
	if err := CheckWaits("c", []string{"a", "b"}, all); err != nil {
		t.Fatal(err)
	}
}

func TestACommentOnADoneTaskNeverReopensIt(t *testing.T) {
	// Live use, L4 F3: a receipt logged on a ticked task reopened it.
	task := afiTask("t1", "priya")
	task.State, task.DoneAt = TaskDone, "2026-11-15T10:00:00Z"
	for _, kind := range []string{TaskEventComment, TaskEventPosted} {
		ch, err := Note(task, TaskNote{Kind: kind, Text: "One more line about search, please"}, TaskActor{Handle: "marco", Edit: true}, byPriya, nil, taskIDs(), taskNow)
		if err != nil || ch.Task.State != TaskDone || ch.Task.DoneAt == "" || ch.Task.Rev != task.Rev || len(ch.Events) != 1 {
			t.Fatalf("%s on done: %+v %v", kind, ch, err)
		}
	}
}

func TestSendBackReopensADoneTaskAndSaysWhoAndWhy(t *testing.T) {
	// Live use, L2 #8: a reviewer could only send work back by commenting.
	memo := personTask("t1", "marco")
	memo.State, memo.DoneAt = TaskDone, "2026-11-15T10:00:00Z"
	all := []WorkstreamTask{memo}
	upNext, why := TaskUpNext, "the numbers in section 2 are last year's"
	ch, err := ApplyPatch(memo, TaskPatch{State: &upNext, StateReason: &why}, TaskActor{Handle: "priya", Edit: true}, byPriya, all, taskIDs(), taskNow)
	words := "Sent back by priya: " + why
	if err != nil || ch.Task.State != TaskUpNext || ch.Task.DoneAt != "" || ch.Task.StateReason != words || len(ch.Events) != 1 || ch.Events[0].Text != words {
		t.Fatalf("send back: %+v %v", ch, err)
	}
	done := TaskDone
	if _, err := ApplyPatch(ch.Task, TaskPatch{State: &done}, TaskActor{Handle: "priya", Edit: true}, byPriya, all, taskIDs(), taskNow); err == nil {
		t.Fatal("a reviewer ticked someone else's task done")
	}
	ticked, err := ApplyPatch(ch.Task, TaskPatch{State: &done}, TaskActor{Handle: "marco", Edit: true}, byPriya, all, taskIDs(), taskNow)
	if err != nil || ticked.Task.StateReason != "" {
		t.Fatalf("the owner's tick keeps the reason: %+v %v", ticked.Task, err)
	}
}

func TestHandingOnChangesTheOwnerAndSaysSo(t *testing.T) {
	task := afiTask("t1", "priya")
	to := TaskOwner{Kind: TaskOwnerPerson, ID: "marco", Name: "Marco"}
	ch, err := ApplyPatch(task, TaskPatch{Owner: &to}, TaskActor{Handle: "abir", Edit: true}, byPriya, []WorkstreamTask{task}, taskIDs(), taskNow)
	if err != nil || ch.Task.Owner != to || ch.Events[0].Kind != TaskEventHanded || ch.Events[0].Text != "Handed to Marco" {
		t.Fatalf("hand on: %+v %v", ch, err)
	}
}

func TestAfiOffersUnderACommentAnyoneTakesItAndTheOwnerUndoesWithinFiveSeconds(t *testing.T) {
	task := afiTask("t1", "priya")
	comment := WorkstreamTaskEvent{ID: "c1", Task: "t1", Kind: TaskEventComment, Text: "Mention the import fix."}
	if _, err := Note(task, TaskNote{Kind: TaskEventOffer, Text: "Afi can add this to the draft", OfferFor: "c1"}, TaskActor{Handle: "marco", Edit: true}, byPriya, []WorkstreamTaskEvent{comment}, taskIDs(), taskNow); err == nil {
		t.Fatal("a person made Afi's offer")
	}
	afi := TaskEventBy{Kind: TaskByAfi, Name: "Afi"}
	ch, err := Note(task, TaskNote{Kind: TaskEventOffer, Text: "Afi can add this to the draft", OfferFor: "c1"}, TaskActor{Handle: "marco", Edit: true}, afi, []WorkstreamTaskEvent{comment}, taskIDs(), taskNow)
	if err != nil || ch.Events[0].Offer.State != OfferOpen || ch.Task.State != task.State {
		t.Fatalf("offer: %+v %v (an offer never acts)", ch, err)
	}
	taken, err := SetOffer(task, ch.Events[0], OfferTaken, TaskActor{Handle: "marco", Edit: true}, TaskEventBy{Name: "marco"}, taskNow)
	if err != nil || taken.Offer.TakenBy.ID != "marco" || taken.UndoUntil != taskNow.Add(TaskOfferUndo).Format(time.RFC3339Nano) {
		t.Fatalf("take: %+v %v", taken, err)
	}
	if _, err := SetOffer(task, taken, OfferOpen, TaskActor{Handle: "priya", Edit: true}, byPriya, taskNow.Add(6*time.Second)); !errors.Is(err, ErrTaskUndoOver) {
		t.Fatalf("undo after five seconds: %v", err)
	}
	undone, err := SetOffer(task, taken, OfferOpen, TaskActor{Handle: "priya", Edit: true}, byPriya, taskNow.Add(4*time.Second))
	if err != nil || undone.Offer.State != OfferOpen || undone.UndoUntil != "" {
		t.Fatalf("owner's undo: %+v %v", undone, err)
	}
}

func TestStartAllNamesAGroupsReadyTasksAndStartsNone(t *testing.T) {
	g := &TaskGroup{ID: "grp_rel", Name: "Release 2.4", Kind: "batch"}
	a, b, c := afiTask("a", "priya"), afiTask("b", "priya", "a"), personTask("c", "lena")
	a.Group, b.Group, c.Group = g, g, g
	started := StartAll("grp_rel", []WorkstreamTask{a, b, c}, taskNow)
	if len(started) != 1 || started[0].ID != "a" || started[0].State != TaskUpNext || started[0].Rev != a.Rev {
		t.Fatalf("started %+v", started)
	}
}

func TestABatchNamedFiveTimesIsOneGroup(t *testing.T) {
	var all []WorkstreamTask
	for i := range 5 {
		task, err := NewTask("ws", "t"+strconv.Itoa(i), TaskCreate{Title: "Part " + strconv.Itoa(i), DoneBy: TaskDoneBy{Kind: TaskByChat, ChatID: "c1", Name: "Release 2.4"},
			Group: &TaskGroup{Name: "Release 2.4", Kind: "batch"}}, PersonRef{ID: "priya"}, TaskOwner{Kind: TaskOwnerEveryone}, all, taskNow)
		if err != nil {
			t.Fatal(err)
		}
		all = append(all, task)
	}
	for _, task := range all {
		if task.Group.ID != all[0].Group.ID {
			t.Fatalf("groups %q and %q", task.Group.ID, all[0].Group.ID)
		}
	}
}

func TestWhoeverAskedOwnsItAndWorkNobodyAskedForGoesToTheDefaultOwner(t *testing.T) {
	asked, _ := NewTask("ws", "t1", TaskCreate{Title: "Draft the notes", DoneBy: TaskDoneBy{Kind: TaskByAfi}}, PersonRef{ID: "priya", Name: "Priya"}, TaskOwner{Kind: TaskOwnerEveryone}, nil, taskNow)
	if asked.Owner.ID != "priya" {
		t.Fatalf("owner %+v", asked.Owner)
	}
	ordered, _ := NewTask("ws", "t2", TaskCreate{Title: "Draft the notes", DoneBy: TaskDoneBy{Kind: TaskByAfi}, CameFrom: &TaskCameFrom{Kind: TaskByOrder, OrderID: "o1", Name: "Every Monday"}},
		PersonRef{ID: "priya"}, TaskOwner{Kind: TaskOwnerPerson, ID: "marco", Name: "Marco"}, nil, taskNow)
	if ordered.Owner.ID != "marco" {
		t.Fatalf("owner %+v", ordered.Owner)
	}
}

func TestAFactoryJobIsATaskInTheAppsWords(t *testing.T) {
	for _, c := range []struct {
		it   Item
		want string
	}{
		{Item{State: StateRunning}, TaskRunning},
		{Item{State: StateQueued}, TaskUpNext},
		{Item{State: StateNew}, TaskUpNext},
		{Item{State: StateNeedsYou, QKind: QKindApprove}, TaskNeeds},
		{Item{State: StateNew, Failure: &Failure{Code: CodeMachineLost}}, TaskNoReport},
		{Item{State: StateNew, Failure: &Failure{Code: CodeCrash}}, TaskInterrupted},
		{Item{State: StateNew, Failure: &Failure{Code: CodeToolError, Detail: "tests failed"}}, TaskFailed},
		{Item{State: StateNew, Failure: &Failure{Code: CodeStopped}}, TaskStopped},
		{Item{State: StateDismissed}, TaskDone},
	} {
		if got, _ := JobTaskState(c.it); got != c.want {
			t.Errorf("%+v reads %q, want %q", c.it, got, c.want)
		}
	}
	for state, words := range map[string]string{TaskFailed: "Failed", TaskInterrupted: "Interrupted", TaskStopped: "Stopped by you", TaskNoReport: "Didn't report back"} {
		if StateWords(state) != words {
			t.Errorf("%s says %q", state, StateWords(state))
		}
	}
}

func TestAFactoryTasksAskIsTheJobsQuestion(t *testing.T) {
	it := Item{ID: 431, Title: "Fix import for recipes without a photo", State: StateNeedsYou, Assignee: "marco",
		Asking: &Asked{Question: "Merge pull request 431?", With: AskedYou, Options: []string{"Merge", "Not yet"}, Pick: "Merge", At: taskNow}}
	task := JobTask(it, "ws", nil, TaskOwner{Kind: TaskOwnerEveryone})
	if task.ID != "job:431" || task.DoneBy.Job != 431 || task.Owner.ID != "marco" || task.State != TaskNeeds {
		t.Fatalf("task %+v", task)
	}
	if task.Ask == nil || task.Ask.Title != "Merge pull request 431?" || !task.Ask.Options[0].Recommended {
		t.Fatalf("ask %+v", task.Ask)
	}
	to := TaskOwner{Kind: TaskOwnerPerson, ID: "priya", Name: "Priya"}
	handed := JobTask(it, "ws", &WorkstreamTask{Rev: 2, Owner: to}, TaskOwner{Kind: TaskOwnerEveryone})
	if handed.Owner != to || handed.Rev != 2 {
		t.Fatalf("the overlay's owner did not win: %+v", handed.Owner)
	}
	stop := TaskStopped
	if _, err := ApplyPatch(task, TaskPatch{State: &stop}, TaskActor{Handle: "marco", Edit: true}, byPriya, []WorkstreamTask{task}, taskIDs(), taskNow); !errors.Is(err, ErrTaskFactoryState) {
		t.Fatalf("a Factory task's state was set by hand: %v", err)
	}
}

// A STANDING ORDER'S TASK NAMES ITS RUN, so the holder's desk makes one task per
// run, and it is owned by the workstream's owner of incoming work.
func TestAStandingOrdersTaskKeepsItsRunAndTheDefaultOwner(t *testing.T) {
	in := TaskCreate{Title: "Draft the release notes", DoneBy: TaskDoneBy{Kind: TaskByAfi}, State: TaskRunning,
		CameFrom: &TaskCameFrom{Kind: TaskByOrder, OrderID: "a1b2", Name: "Every Monday at 09:00", Run: 42},
		Group:    &TaskGroup{ID: "order:a1b2", Name: "Every Monday at 09:00", Kind: "order"}}
	owner := TaskOwner{Kind: TaskOwnerPerson, ID: "marco", Name: "Marco"}
	task, err := NewTask("ws1", "t1", in, PersonRef{ID: "priya", Name: "Priya"}, owner, nil, taskNow)
	if err != nil {
		t.Fatal(err)
	}
	if task.CameFrom.Run != 42 || task.CameFrom.OrderID != "a1b2" || task.Owner != owner || task.State != TaskRunning || task.Group.Kind != "order" {
		t.Fatalf("the order's task = %+v", task)
	}
	raw, _ := json.Marshal(task.CameFrom)
	if !strings.Contains(string(raw), `"run":42`) {
		t.Fatalf("the run is not on the wire: %s", raw)
	}
}
