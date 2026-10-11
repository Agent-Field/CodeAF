package factory

import "testing"

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
