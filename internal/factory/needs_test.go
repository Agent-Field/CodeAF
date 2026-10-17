package factory

import (
	"testing"
	"time"
)

func TestContinueRaisesThisJobOnce(t *testing.T) {
	if NextJobCap(5) != 10 || NextJobCap(0) != 0 || NextJobCap(-1) != 0 {
		t.Fatalf("next = %v %v", NextJobCap(5), NextJobCap(0))
	}
	it := &Item{Cap: 5, Stream: &Stream{Spent: 5}}
	if !ContinueJob(it) || it.Cap != 10 {
		t.Fatalf("first continue cap = %v", it.Cap)
	}
	if ContinueJob(it) {
		t.Fatal("a second continue raised the cap again")
	}
	if it.Cap != 10 {
		t.Fatalf("cap after second = %v", it.Cap)
	}
	stop := &Item{Cap: 5, Stream: &Stream{Spent: 5}}
	if IsContinue(false, "stop") || IsContinue(false, "cancel_job") {
		t.Fatal("stop counted as continue")
	}
	if !IsContinue(true, "") || !IsContinue(false, "continue") {
		t.Fatal("continue was refused")
	}
	if !ContinueJob(stop) || stop.Cap != 10 {
		t.Fatalf("exact cap = %v", stop.Cap)
	}
}

func TestClassifyFailureAndOutOfAttempts(t *testing.T) {
	code, detail := ClassifyFailure("the worktree is gone\nstack")
	if code != CodeToolError || detail != "the worktree is gone" {
		t.Fatalf("tool = %s %q", code, detail)
	}
	if code, _ = ClassifyFailure("provider refused"); code != CodeProviderError {
		t.Fatalf("provider = %s", code)
	}
	if code, _ = ClassifyFailure("rate limit"); code != CodeProviderError {
		t.Fatalf("rate = %s", code)
	}
	if code, _ = ClassifyFailure("panic: nil pointer"); code != CodeCrash {
		t.Fatalf("crash = %s", code)
	}
	if OutOfAttemptsChoice(true, "") != ChoiceTryOnceMore || OutOfAttemptsChoice(false, "try_once_more") != ChoiceTryOnceMore {
		t.Fatal("try once more was not read")
	}
	if OutOfAttemptsChoice(false, "send_back: use the other file") != ChoiceSendBack {
		t.Fatal("send back was not read")
	}
	if SendBackReason("send_back: use the other file") != "use the other file" {
		t.Fatal("reason was dropped")
	}
	if OutOfAttemptsChoice(false, "") != ChoiceCancel || OutOfAttemptsChoice(false, "cancel_job") != ChoiceCancel {
		t.Fatal("cancel was not read")
	}
	if !JobPersonOnly(NeedsSpendingLimit) || !JobPersonOnly(NeedsPublishFailed) || JobPersonOnly(NeedsQuestion) {
		t.Fatal("who may answer drifted")
	}
}

func TestMayActAndRoundLines(t *testing.T) {
	when := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	free := Item{State: StateNeedsYou, Question: "which file?", QKind: "step"}
	if got := MayAct(free, "mei"); !got.Answer || !got.Reply || !got.Approve || !got.AssignToMe {
		t.Fatalf("unassigned question = %+v", got)
	}
	owned := Item{Assignee: "sam", State: StateNeedsYou, Question: "budget of $5 reached", QKind: "cap"}
	if got := MayAct(owned, "mei"); got.Answer || got.Approve || got.RunAgain || got.CancelJob || got.RetryPublish || got.AssignToMe || !got.Reply {
		t.Fatalf("someone else = %+v", got)
	}
	if got := MayAct(owned, "@Sam"); !got.Answer || !got.Approve || !got.RunAgain || !got.CancelJob || !got.RetryPublish || got.AssignToMe || !got.Reply {
		t.Fatalf("the job's person = %+v", got)
	}
	if RoundCap(2, 3) != 3 || RoundCap(0, 1) != 1 || RoundCap(40, 1) != RoundLinesMost {
		t.Fatalf("cap = %d %d %d", RoundCap(2, 3), RoundCap(0, 1), RoundCap(40, 1))
	}
	ph := &Phase{Name: "review"}
	NoteRound(ph, 2, RoundLine{Round: 1, Summary: "2 findings", At: when})
	NoteRound(ph, 2, RoundLine{Round: 2, Summary: "1 finding", At: when.Add(time.Minute)})
	NoteRound(ph, 2, RoundLine{Round: 3, Summary: "still red", At: when.Add(2 * time.Minute)})
	if len(ph.Rounds) != 2 || ph.Rounds[0].Round != 2 || ph.Rounds[1].Summary != "still red" {
		t.Fatalf("rounds = %+v", ph.Rounds)
	}
	NoteRound(ph, 2, RoundLine{Round: 2, Summary: "replaced", At: when})
	if ph.Rounds[0].Summary != "replaced" {
		t.Fatalf("same round was appended: %+v", ph.Rounds)
	}
	it := &Item{Stream: &Stream{Cur: 0, Phases: []Phase{{Name: "write", State: PhaseRunning, Round: 2}}}}
	StampFailure(it, NewFailure("write", CodeToolError, "boom", "box", FailureRequeued, 0, when))
	if it.Failure == nil || it.Failure.Attempts != 1 || it.Stream.Phases[0].State != PhaseRunning || it.Stream.Phases[0].Failure == nil {
		t.Fatalf("stamped = %+v phase %+v", it.Failure, it.Stream.Phases[0])
	}
}

func TestGitHubIssueAskText(t *testing.T) {
	issue := Item{Kind: KindIssue, Origin: OriginForge, Num: 12, URL: "https://github.com/acme/api/issues/12", Body: "Fix the total."}
	if !GitHubIssue(issue) || AskText(issue) != "Fix the total." {
		t.Fatalf("issue ask %q", AskText(issue))
	}
	typed := Item{Kind: KindIssue, Origin: OriginTerminal, Num: 1540, Ask: &JobAsk{Text: "Written in the terminal."}}
	if GitHubIssue(typed) || AskText(typed) != "Written in the terminal." {
		t.Fatalf("typed ask %q github %v", AskText(typed), GitHubIssue(typed))
	}
}
