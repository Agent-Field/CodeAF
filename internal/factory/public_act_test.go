package factory

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const actHead = "1111111111111111111111111111111111111111"

var actNow = time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC)

// actJob is a job whose own pull request is open, a draft, watched by Follow
// PR, with one review thread and one conversation comment read.
func actJob(mode string) Item {
	return Item{ID: 7, Repo: "acme/api", Kind: KindIssue, Num: 12, Publish: "pr", State: StateShipped, Autonomy: mode, Cap: 10, Starter: "me", HeadCommit: actHead,
		PublishStatus: &Publishing{State: "opened"},
		PullRequest: &PullRequest{Number: 7, State: PROpen, Draft: true, HeadSHA: actHead, BaseBranch: "main", HeadBranch: "factory/12",
			FeedbackTracked: true, ChecksRead: true, ReviewsRead: true, Mergeable: MergeableYes, Checks: PRChecks{Pending: 1},
			Reviews: []PRReview{{By: "bob", State: "changes_requested", Association: "COLLABORATOR"}, {By: "me", State: "commented"}},
			FeedbackItems: []PRFeedback{
				{ID: "inline:501", Thread: "PRRT_a", CommentID: 501, ReplyTo: 501, By: "bob", Association: "COLLABORATOR"},
				{ID: "inline:503", Thread: "PRRT_b", CommentID: 503, ReplyTo: 503, By: "bob", Association: "COLLABORATOR", Resolved: true},
				{ID: "conversation:601", CommentID: 601, By: "passer-by", Association: "NONE", Untrusted: true},
			}},
		FollowUp: &PRFollowUp{Kind: "pr", State: "watching", Reason: "Watching CI, reviews and merge conflicts."},
		Stream:   &Stream{Phases: []Phase{{Name: FollowPRStep, Kind: StageChat, State: PhaseWaiting}}, Cur: 0}}
}

// pushWaiting turns the job into one whose checked repair waits to be pushed.
func pushWaiting(it Item) Item {
	it.State = StateLanded
	it.FollowUp = &PRFollowUp{Kind: "pr", State: "waiting_approval", Reason: "Repair and proof passed locally; updating the existing PR."}
	it.HeadCommit = strings.Repeat("2", 40)
	it.Proof = []Claim{{Text: "works", OK: true}}
	return it
}

func reply(thread, text string) PublicAct {
	return PublicAct{Kind: ActKindReply, Thread: thread, Text: text}
}

func TestCleanActTakesEachKindAndRefusesWhatIsWrongInIt(t *testing.T) {
	it := actJob(AutonomyManual)
	failing := actJob(AutonomyManual)
	failing.PullRequest.Checks = PRChecks{Failed: 1}
	long := strings.Repeat("x", ActTextMost+1)
	cases := []struct {
		name string
		on   Item
		act  PublicAct
		want string // a part of the refusal, "" for taken
	}{
		{"a reply in a thread the job has read", it, reply("inline:501", "Renamed it."), ""},
		{"a reply by the comment's bare number", it, reply("501", "Renamed it."), ""},
		{"a reply to a comment on the conversation", it, reply("conversation:601", "It is still needed."), ""},
		{"a reply in a thread already resolved", it, reply("inline:503", "Fixed too."), ""},
		{"a reply with no words", it, reply("inline:501", "  "), "needs some words"},
		{"a reply that is only the footer", it, reply("inline:501", PublicActFooter), "needs some words"},
		{"a reply too long to post", it, reply("inline:501", long), "too long"},
		{"a reply with no thread", it, reply("", "Renamed it."), "Name the thread"},
		{"a reply in a thread the job never read", it, reply("inline:999", "Renamed it."), "no thread inline:999"},
		{"a comment", it, PublicAct{Kind: ActKindComment, Text: "Ready for another look."}, ""},
		{"a comment with no words", it, PublicAct{Kind: ActKindComment}, "needs some words"},
		{"resolving an open thread", it, PublicAct{Kind: ActKindResolveThread, Thread: "inline:501"}, ""},
		{"resolving a thread already resolved", it, PublicAct{Kind: ActKindResolveThread, Thread: "inline:503"}, "already resolved"},
		{"resolving a conversation comment", it, PublicAct{Kind: ActKindResolveThread, Thread: "conversation:601"}, "Only a review thread"},
		{"a change of base", it, PublicAct{Kind: ActKindChangeBase, Base: "dev"}, ""},
		{"a change to the base it has", it, PublicAct{Kind: ActKindChangeBase, Base: "main"}, "already main"},
		{"a change of base to its own branch", it, PublicAct{Kind: ActKindChangeBase, Base: "factory/12"}, "own branch"},
		{"a change of base to nothing", it, PublicAct{Kind: ActKindChangeBase}, "needs the branch"},
		{"a change of base to an option", it, PublicAct{Kind: ActKindChangeBase, Base: "--force"}, "not a branch name"},
		{"a change of base with a space", it, PublicAct{Kind: ActKindChangeBase, Base: "release 2"}, "not a branch name"},
		{"an edit of the title", it, PublicAct{Kind: ActKindEditPR, Title: "Keep the ledger balanced"}, ""},
		{"an edit of the description", it, PublicAct{Kind: ActKindEditPR, Body: "What changed and why."}, ""},
		{"an edit of nothing", it, PublicAct{Kind: ActKindEditPR}, "needs a new title"},
		{"a title of two lines", it, PublicAct{Kind: ActKindEditPR, Title: "one\ntwo"}, "one line"},
		{"a title too long", it, PublicAct{Kind: ActKindEditPR, Title: strings.Repeat("t", ActTitleMost+1)}, "too long"},
		{"asking named reviewers again", it, PublicAct{Kind: ActKindRerequestReview, Reviewers: []string{"@carol", "carol", " dave "}}, ""},
		{"asking everyone who reviewed", it, PublicAct{Kind: ActKindRerequestReview}, ""},
		{"asking only the job's own person", it, PublicAct{Kind: ActKindRerequestReview, Reviewers: []string{"me"}}, "Name who to ask"},
		{"running failed checks again", failing, PublicAct{Kind: ActKindRerunChecks}, ""},
		{"running checks again when none failed", it, PublicAct{Kind: ActKindRerunChecks}, "No check has failed"},
		{"marking the draft ready", it, PublicAct{Kind: ActKindMarkReady}, ""},
		{"a push, which has its own approval", it, PublicAct{Kind: ActKindPush}, "its own approval"},
		{"a merge, which is the person's", it, PublicAct{Kind: ActKindMerge}, "its own approval"},
		{"no kind", it, PublicAct{}, "needs a kind"},
		{"an unknown kind", it, PublicAct{Kind: "force_push"}, "not a public act"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CleanAct(tc.on, tc.act)
			if tc.want == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				return
			}
			var refusal *ActRefusal
			if !errors.As(err, &refusal) || !refusal.Invalid || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %+v, err %v; want an invalid act saying %q", got, err, tc.want)
			}
		})
	}
}

func TestCleanActKeepsWhatWillBePostedExactly(t *testing.T) {
	it := actJob(AutonomyManual)
	got, err := CleanAct(it, reply("501", "  Renamed it in /Users/someone/work/ledger.go.  "))
	if err != nil {
		t.Fatal(err)
	}
	// The thread is named as the job stores it, nothing of this computer is in
	// the words, and the footer is on, once.
	want := "Renamed it in [local home]/work/ledger.go.\n\n" + PublicActFooter
	if got.Thread != "inline:501" || got.Text != want {
		t.Fatalf("thread %q, text %q", got.Thread, got.Text)
	}
	again, err := CleanAct(it, reply("inline:501", got.Text))
	if err != nil || again.Text != want {
		t.Fatalf("a second pass changed the words: %q, %v", again.Text, err)
	}
	asked, err := CleanAct(it, PublicAct{Kind: ActKindRerequestReview})
	if err != nil || fmt.Sprint(asked.Reviewers) != "[bob]" {
		t.Fatalf("reviewers %v, err %v", asked.Reviewers, err)
	}
	named, _ := CleanAct(it, PublicAct{Kind: ActKindRerequestReview, Reviewers: []string{"@carol", "Carol", " dave ", "me"}})
	if fmt.Sprint(named.Reviewers) != "[carol dave]" {
		t.Fatalf("reviewers %v", named.Reviewers)
	}
}

// An act is for the job's own open pull request, and nothing else.
func TestActsNeedTheJobsOwnOpenPullRequest(t *testing.T) {
	cases := map[string]func(*Item){
		"did not open this pull request": func(it *Item) { it.Kind = KindPR },
		"no pull request yet":            func(it *Item) { it.PullRequest, it.Publish = nil, "" },
		"pull request is closed":         func(it *Item) { it.PullRequest.State = PRClosed },
		"pull request is merged":         func(it *Item) { it.PullRequest.State = PRMerged },
		"not been read from GitHub yet":  func(it *Item) { it.PullRequest.State = "" },
	}
	for want, change := range cases {
		it := actJob(AutonomyAuto)
		change(&it)
		_, err := CleanAct(it, PublicAct{Kind: ActKindComment, Text: "Hello."})
		var refusal *ActRefusal
		if !errors.As(err, &refusal) || refusal.Invalid || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v", want, err)
		}
	}
}

// THE STEP THE JOB IS ON IS NEVER A REASON. A job takes acts while a step
// runs, while it is queued, while it waits on its person or on GitHub; it
// refuses only when it is over, stopped or put away.
func TestActsRefusalNeverNamesTheStep(t *testing.T) {
	for _, state := range []State{StateShipped, StateRunning, StateQueued, StateNeedsYou, StateLanded} {
		it := actJob(AutonomyManual)
		it.State = state
		if err := ActsRefusal(it); err != nil {
			t.Errorf("state %s refused: %v", state, err)
		}
	}
	stopped := true
	cases := map[string]func(*Item){
		"dropped":        func(it *Item) { it.DoneHow = "dropped" },
		"finished":       func(it *Item) { it.PullRequest.State = PRMerged },
		"person stopped": func(it *Item) { it.PersonStopped = &stopped },
	}
	for want, change := range cases {
		it := actJob(AutonomyAuto)
		change(&it)
		if err := ActsRefusal(it); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v", want, err)
		}
	}
	spent := actJob(AutonomyAuto)
	spent.SpentBefore = spent.Cap
	if ActsRefusal(spent) != nil || ActsBudgetRefusal(spent) == nil {
		t.Fatal("a spent budget must stop a new proposal, and only that")
	}
	if ActsBudgetRefusal(actJob(AutonomyAuto)) != nil {
		t.Fatal("a job within its budget was refused")
	}
}

func TestActAuthorityIsManualOrAuto(t *testing.T) {
	manual, auto := actJob(AutonomyManual), actJob(AutonomyAuto)
	unset := actJob("")
	taken := actJob(AutonomyAuto)
	taken.Manual = true
	for _, tc := range []struct {
		name string
		it   Item
		by   string
		want string
	}{
		{"Manual, the manager proposes", manual, ActByMainAgent, ActProposed},
		{"Auto, the manager proposes", auto, ActByMainAgent, ActApproved},
		{"Manual, the person asks", manual, ActByYou, ActApproved},
		{"Auto, the person asks", auto, ActByYou, ActApproved},
		{"no mode saved, the manager proposes", unset, ActByMainAgent, ActProposed},
		{"taken over, the manager proposes", taken, ActByMainAgent, ActProposed},
	} {
		if got := ActAuthority(tc.it, tc.by); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestPushRoad(t *testing.T) {
	waiting := pushWaiting(actJob(AutonomyManual))
	auto := pushWaiting(actJob(AutonomyAuto))
	held := pushWaiting(actJob(AutonomyAuto))
	held.RuleHold = "protected · auth/ · auth/login.go"
	publishing := actJob(AutonomyAuto)
	publishing.Publishing = true
	for _, tc := range []struct {
		name      string
		it        Item
		by        string
		road, why string
	}{
		{"Manual: the approval already waits on the person", waiting, ActByMainAgent, RoadWaiting, "already waiting on the person"},
		{"Manual: the person's own ask presses it", waiting, ActByYou, RoadPress, ""},
		{"Auto: the manager presses it", auto, ActByMainAgent, RoadPress, ""},
		{"Auto: a rule keeps the approval for the person", held, ActByMainAgent, RoadWaiting, "protected path"},
		{"nothing checked and ready", actJob(AutonomyAuto), ActByMainAgent, "", "Nothing is checked and ready to push"},
		{"a push already under way", publishing, ActByMainAgent, "", "already under way"},
	} {
		road, words, err := PushRoad(tc.it, tc.by)
		if tc.road == "" {
			if err == nil || !strings.Contains(err.Error(), tc.why) {
				t.Errorf("%s: road %q, err %v", tc.name, road, err)
			}
			continue
		}
		if err != nil || road != tc.road || !strings.Contains(words, tc.why) {
			t.Errorf("%s: road %q, words %q, err %v", tc.name, road, words, err)
		}
	}
}

func cleaned(t *testing.T, it Item, acts ...PublicAct) []PublicAct {
	t.Helper()
	out := make([]PublicAct, 0, len(acts))
	for _, a := range acts {
		c, err := CleanAct(it, a)
		if err != nil {
			t.Fatalf("clean %+v: %v", a, err)
		}
		out = append(out, c)
	}
	return out
}

func TestAddActsProposesInManualAndApprovesInAuto(t *testing.T) {
	manual := actJob(AutonomyManual)
	acts := cleaned(t, manual, reply("inline:501", "Renamed it."), PublicAct{Kind: ActKindChangeBase, Base: "dev"})
	got, err := AddActs(&manual, acts, ActByMainAgent, "Answering the review.", actNow)
	if err != nil || len(got) != 2 || len(manual.Acts) != 2 {
		t.Fatalf("got %+v, err %v", got, err)
	}
	if got[0].ID == "" || got[0].ID == got[1].ID {
		t.Fatalf("ids %q %q", got[0].ID, got[1].ID)
	}
	for _, a := range manual.Acts {
		if a.State != ActProposed || a.By != ActByMainAgent || a.Why != "Answering the review." || !a.At.Equal(actNow) || a.ApprovedBy != "" {
			t.Fatalf("stored %+v", a)
		}
	}
	// The same ask again adds nothing: a manager that asks twice never posts twice.
	again, err := AddActs(&manual, acts, ActByMainAgent, "", actNow.Add(time.Minute))
	if err != nil || len(manual.Acts) != 2 || again[0].ID != got[0].ID {
		t.Fatalf("a repeat added acts: %+v, err %v", manual.Acts, err)
	}

	auto := actJob(AutonomyAuto)
	got, err = AddActs(&auto, cleaned(t, auto, reply("inline:501", "Renamed it.")), ActByMainAgent, "", actNow)
	if err != nil || got[0].State != ActApproved || got[0].ApprovedBy != ActByMainAgent || got[0].AfterPush != "" {
		t.Fatalf("Auto: %+v, err %v", got, err)
	}
	// The person's own ask is approved in Manual too.
	mine := actJob(AutonomyManual)
	got, _ = AddActs(&mine, cleaned(t, mine, PublicAct{Kind: ActKindComment, Text: "On it."}), ActByYou, "", actNow)
	if got[0].State != ActApproved || got[0].ApprovedBy != ActByYou {
		t.Fatalf("the person's own act: %+v", got[0])
	}
}

// A reply posted a moment ago is not posted again for the same ask; the same
// words said much later are a new act.
func TestAddActsDoesNotRepeatAReplyJustPosted(t *testing.T) {
	it := actJob(AutonomyAuto)
	acts := cleaned(t, it, reply("inline:501", "Renamed it."))
	first, _ := AddActs(&it, acts, ActByMainAgent, "", actNow)
	if !BeginAct(&it, first[0].ID, actNow) || !FinishAct(&it, first[0].ID, "https://github.com/acme/api/pull/7#discussion_r502", "Replied in inline:501.", actNow) {
		t.Fatal("could not post the first")
	}
	again, _ := AddActs(&it, acts, ActByMainAgent, "", actNow.Add(time.Minute))
	if len(it.Acts) != 1 || again[0].State != ActDone || again[0].URL == "" {
		t.Fatalf("a repeat within the window: %+v", it.Acts)
	}
	later, _ := AddActs(&it, acts, ActByMainAgent, "", actNow.Add(ActRepeatWithin+time.Minute))
	if len(it.Acts) != 2 || later[0].State != ActApproved || later[0].ID == first[0].ID {
		t.Fatalf("the same words later: %+v", it.Acts)
	}
}

func TestAddActsRefusesMoreThanCanWait(t *testing.T) {
	it := actJob(AutonomyManual)
	for i := 0; i < ActsWaiting; i++ {
		if _, err := AddActs(&it, cleaned(t, it, PublicAct{Kind: ActKindComment, Text: fmt.Sprintf("Note %d.", i)}), ActByMainAgent, "", actNow); err != nil {
			t.Fatal(err)
		}
	}
	before := len(it.Acts)
	if _, err := AddActs(&it, cleaned(t, it, PublicAct{Kind: ActKindComment, Text: "One too many."}), ActByMainAgent, "", actNow); err == nil || len(it.Acts) != before {
		t.Fatalf("took act %d: err %v", before+1, err)
	}
}

func TestApproveAndDeclineCoverEveryActThatWaits(t *testing.T) {
	it := actJob(AutonomyManual)
	_, _ = AddActs(&it, cleaned(t, it, reply("inline:501", "Renamed it."), PublicAct{Kind: ActKindResolveThread, Thread: "inline:501"}), ActByMainAgent, "", actNow)
	if n := ApproveActs(&it, "me", false, actNow); n != 2 || len(ProposedActs(it)) != 0 {
		t.Fatalf("approved %d", n)
	}
	for _, a := range it.Acts {
		if a.State != ActApproved || a.ApprovedBy != "me" || a.AfterPush != "" || a.WithPush {
			t.Fatalf("approved %+v", a)
		}
	}
	if ApproveActs(&it, "me", false, actNow) != 0 || DeclineActs(&it, "no", actNow) != 0 {
		t.Fatal("a second answer found acts to answer")
	}
	no := actJob(AutonomyManual)
	_, _ = AddActs(&no, cleaned(t, no, reply("inline:501", "Renamed it.")), ActByMainAgent, "", actNow)
	if n := DeclineActs(&no, "Say why you renamed it.", actNow); n != 1 {
		t.Fatalf("declined %d", n)
	}
	if a := no.Acts[0]; a.State != ActDeclined || a.Result != "Say why you renamed it." || a.DoneAt.IsZero() {
		t.Fatalf("declined %+v", a)
	}
	if _, ok := NextAct(no, actNow); ok {
		t.Fatal("a declined act is due")
	}
}

func TestActsSummarySaysTheSetInPlainWords(t *testing.T) {
	for _, tc := range []struct {
		acts []PublicAct
		want string
	}{
		{[]PublicAct{{Kind: ActKindReply}, {Kind: ActKindReply}, {Kind: ActKindChangeBase, Base: "dev"}}, "post 2 replies and change the base to dev"},
		{[]PublicAct{{Kind: ActKindReply}}, "post 1 reply"},
		{[]PublicAct{{Kind: ActKindReply}, {Kind: ActKindComment}, {Kind: ActKindResolveThread}, {Kind: ActKindResolveThread}}, "post 1 reply and 1 comment and resolve 2 review threads"},
		{[]PublicAct{{Kind: ActKindEditPR, Title: "t"}}, "edit the pull request's title"},
		{[]PublicAct{{Kind: ActKindEditPR, Title: "t"}, {Kind: ActKindEditPR, Body: "b"}}, "edit the pull request's title and description"},
		{[]PublicAct{{Kind: ActKindRerequestReview, Reviewers: []string{"bob", "carol"}}, {Kind: ActKindRerunChecks}, {Kind: ActKindMarkReady}},
			"ask @bob and @carol to review again, run the failed checks again and mark the pull request ready for review"},
		{nil, ""},
	} {
		if got := ActsSummary(tc.acts); got != tc.want {
			t.Errorf("summary %q, want %q", got, tc.want)
		}
	}
}

// The list stays small: every act still in play, and the last few that are over.
func TestFinishedActsArePruned(t *testing.T) {
	it := actJob(AutonomyAuto)
	for i := 0; i < ActsKept+15; i++ {
		got, err := AddActs(&it, cleaned(t, it, PublicAct{Kind: ActKindComment, Text: fmt.Sprintf("Note %d.", i)}), ActByMainAgent, "", actNow.Add(time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		at := actNow.Add(time.Duration(i) * time.Hour)
		if !BeginAct(&it, got[0].ID, at) || !FinishAct(&it, got[0].ID, "", "Commented.", at) {
			t.Fatalf("act %d did not run", i)
		}
	}
	if len(it.Acts) != ActsKept {
		t.Fatalf("kept %d finished acts, want %d", len(it.Acts), ActsKept)
	}
	if !strings.Contains(it.Acts[len(it.Acts)-1].Text, fmt.Sprintf("Note %d.", ActsKept+14)) {
		t.Fatalf("the newest was not kept: %q", it.Acts[len(it.Acts)-1].Text)
	}
	waiting := actJob(AutonomyManual)
	waiting.Acts = append([]PublicAct(nil), it.Acts...)
	_, _ = AddActs(&waiting, cleaned(t, waiting, reply("inline:501", "Renamed it.")), ActByMainAgent, "", actNow)
	if len(waiting.Acts) != ActsKept+1 || len(ProposedActs(waiting)) != 1 {
		t.Fatalf("an act still waiting was pruned: %d", len(waiting.Acts))
	}
}

// One sender, one act at a time, in the order approved; a try GitHub did not
// answer waits its turn again and blocks the acts behind it.
func TestActsRunInOrderWithBoundedTries(t *testing.T) {
	it := actJob(AutonomyAuto)
	got, _ := AddActs(&it, cleaned(t, it, reply("inline:501", "Renamed it."), PublicAct{Kind: ActKindResolveThread, Thread: "inline:501"}), ActByMainAgent, "", actNow)
	first, second := got[0].ID, got[1].ID
	if next, ok := NextAct(it, actNow); !ok || next.ID != first {
		t.Fatalf("next %+v %v", next, ok)
	}
	if BeginAct(&it, second, actNow) {
		t.Fatal("the second act began before the first")
	}
	if !BeginAct(&it, first, actNow) {
		t.Fatal("the first act did not begin")
	}
	if _, ok := NextAct(it, actNow); ok {
		t.Fatal("an act is due while one is being sent")
	}
	if at := ActsDueAt(it, actNow); !at.Equal(actNow) {
		t.Fatalf("a running act is looked at %v, want now", at)
	}
	now := actNow
	for try := 1; try < ActTriesMost; try++ {
		was, failed := RetryAct(&it, first, "GitHub did not answer.", now)
		if !was || failed {
			t.Fatalf("try %d: was %v, failed %v", try, was, failed)
		}
		a := it.Acts[0]
		if a.State != ActApproved || a.Tries != try || !a.NextAt.After(now) {
			t.Fatalf("try %d left %+v", try, a)
		}
		if _, ok := NextAct(it, now); ok {
			t.Fatal("the act behind a waiting try ran")
		}
		if at := ActsDueAt(it, now); !at.Equal(a.NextAt) {
			t.Fatalf("due %v, want its next try %v", at, a.NextAt)
		}
		now = a.NextAt
		if !BeginAct(&it, first, now) {
			t.Fatalf("try %d did not begin", try+1)
		}
	}
	was, failed := RetryAct(&it, first, "GitHub did not answer.", now)
	if !was || !failed || it.Acts[0].State != ActFailed || !strings.Contains(it.Acts[0].Error, fmt.Sprintf("tried %d times", ActTriesMost)) {
		t.Fatalf("after the last try: %+v", it.Acts[0])
	}
	// A failed act does not block the job: the next one is due.
	if next, ok := NextAct(it, now); !ok || next.ID != second {
		t.Fatalf("after a failure next is %+v %v", next, ok)
	}
	if !BeginAct(&it, second, now) || !FailAct(&it, second, "That review thread is no longer on the pull request.", now) {
		t.Fatal("could not fail the second")
	}
	if FailAct(&it, second, "again", now) || FinishAct(&it, second, "", "", now) {
		t.Fatal("an act that is over was changed again")
	}
	if at := ActsDueAt(it, now); !at.IsZero() {
		t.Fatalf("nothing is left and acts are due at %v", at)
	}
}

// AN ACT FOUND RUNNING IS NEVER SENT AGAIN: what reached GitHub is not known.
func TestAnActCutOffMidSendFailsAndIsNotSentAgain(t *testing.T) {
	it := actJob(AutonomyAuto)
	got, _ := AddActs(&it, cleaned(t, it, reply("inline:501", "Renamed it.")), ActByMainAgent, "", actNow)
	if !BeginAct(&it, got[0].ID, actNow) {
		t.Fatal("did not begin")
	}
	cut := CutOffActs(&it, actNow.Add(time.Minute))
	if len(cut) != 1 || it.Acts[0].State != ActFailed || it.Acts[0].Error != ActCutOff {
		t.Fatalf("cut %+v, left %+v", cut, it.Acts[0])
	}
	if _, ok := NextAct(it, actNow.Add(time.Hour)); ok {
		t.Fatal("a cut-off act is due again")
	}
	if len(CutOffActs(&it, actNow)) != 0 {
		t.Fatal("cut off twice")
	}
}

// An act approved while a push waits follows that push.
func TestActsApprovedBesideAPushFollowIt(t *testing.T) {
	it := pushWaiting(actJob(AutonomyManual))
	commit := it.HeadCommit
	_, _ = AddActs(&it, cleaned(t, it, reply("inline:501", "Fixed in the next push.")), ActByMainAgent, "", actNow)
	// The publish approval's own yes approves the set.
	it.Approval = &Approval{Kind: "proof", State: "approving", Commit: commit, UndoUntil: actNow.Add(ApprovalUndo)}
	if n := ApproveActs(&it, "me", true, actNow); n != 1 {
		t.Fatalf("approved %d", n)
	}
	a := it.Acts[0]
	if a.State != ActApproved || a.AfterPush != commit || !a.WithPush {
		t.Fatalf("approved with the push: %+v", a)
	}
	if _, ok := NextAct(it, actNow); ok || !ActsDueAt(it, actNow).IsZero() {
		t.Fatal("the act is due before the push")
	}
	// The push goes out: still not due while it is under way.
	it.Approval.State, it.Publishing, it.State = "approved", true, StateQueued
	if SettleActs(&it, actNow) {
		t.Fatalf("a push under way changed the act: %+v", it.Acts[0])
	}
	if _, ok := NextAct(it, actNow); ok {
		t.Fatal("the act is due during the push")
	}
	// A publish status that reads opened is not what lets it go: only the
	// write that records this publish is.
	stale := it
	stale.Publishing, stale.State, stale.PublishStatus = false, StateShipped, &Publishing{State: "opened"}
	if SettleActs(&stale, actNow) || stale.Acts[0].AfterPush != commit {
		t.Fatalf("an old publish status freed the act: %+v", stale.Acts[0])
	}
	// It lands, in the write that records the publish: the act runs.
	it.Publishing, it.State = false, StateShipped
	it.MarkPublishing("opened", "", "", actNow)
	it.FollowUp.State = "watching"
	if it.Acts[0].AfterPush != "" || it.Acts[0].WithPush {
		t.Fatalf("the landed push did not free the act: %+v", it.Acts[0])
	}
	if SettleActs(&it, actNow) {
		t.Fatalf("settling a freed act changed it: %+v", it.Acts[0])
	}
	if next, ok := NextAct(it, actNow); !ok || next.ID != a.ID {
		t.Fatal("the act is not due after the push landed")
	}
}

// AN ACT APPROVED ON ITS OWN WHILE A PUSH WAITS DOES NOT WAIT FOR THAT PUSH.
// The push may never be approved, and a new commit would fail an act that
// was never meant to follow it.
func TestActsApprovedOnTheirOwnBesideAPushDoNotFollowIt(t *testing.T) {
	it := pushWaiting(actJob(AutonomyManual))
	_, _ = AddActs(&it, cleaned(t, it, reply("inline:501", "Renamed it.")), ActByMainAgent, "", actNow)
	if n := ApproveActs(&it, "me", false, actNow); n != 1 {
		t.Fatalf("approved %d", n)
	}
	a := it.Acts[0]
	if a.State != ActApproved || a.AfterPush != "" || a.WithPush {
		t.Fatalf("approved on its own beside a push: %+v", a)
	}
	if next, ok := NextAct(it, actNow); !ok || next.ID != a.ID {
		t.Fatal("an act approved on its own waits for a push it was not approved with")
	}
	// The push is never approved: a new round lands another commit, and the
	// act is still sent, not failed for a push it never followed.
	it.HeadCommit = strings.Repeat("3", 40)
	if SettleActs(&it, actNow) || it.Acts[0].State != ActApproved {
		t.Fatalf("after the commit changed: %+v", it.Acts[0])
	}
	// The publish of an unrelated commit lets go only what followed it.
	follows := pushWaiting(actJob(AutonomyManual))
	_, _ = AddActs(&follows, cleaned(t, follows, reply("inline:501", "Fixed in the next push.")), ActByMainAgent, "", actNow)
	follows.Approval = &Approval{Kind: "proof", State: "approving", Commit: follows.HeadCommit}
	ApproveActs(&follows, "me", true, actNow)
	pushed := follows.HeadCommit
	follows.HeadCommit = strings.Repeat("4", 40)
	follows.MarkPublishing("opened", "", "", actNow)
	if follows.Acts[0].AfterPush != pushed {
		t.Fatalf("the publish of another commit freed the act: %+v", follows.Acts[0])
	}
}

// Undo of the push approval takes back the yes it gave the acts.
func TestUndoOfThePushApprovalPutsItsActsBackToWait(t *testing.T) {
	it := pushWaiting(actJob(AutonomyManual))
	_, _ = AddActs(&it, cleaned(t, it, reply("inline:501", "Fixed in the next push.")), ActByMainAgent, "", actNow)
	it.Approval = &Approval{Kind: "proof", State: "approving", Commit: it.HeadCommit}
	ApproveActs(&it, "me", true, actNow)
	it.Approval = nil // Undo
	if !SettleActs(&it, actNow) || it.Acts[0].State != ActProposed || it.Acts[0].ApprovedBy != "" || it.Acts[0].AfterPush != "" {
		t.Fatalf("after Undo: %+v", it.Acts[0])
	}
	// An act the person approved on its own keeps its yes when the push
	// approval is undone: it never depended on that push.
	own := pushWaiting(actJob(AutonomyManual))
	_, _ = AddActs(&own, cleaned(t, own, reply("inline:501", "Renamed it.")), ActByMainAgent, "", actNow)
	ApproveActs(&own, "me", false, actNow)
	if a := own.Acts[0]; a.State != ActApproved || a.AfterPush != "" || a.WithPush {
		t.Fatalf("approved on its own beside a push: %+v", a)
	}
	if SettleActs(&own, actNow) {
		t.Fatalf("a waiting push changed it: %+v", own.Acts[0])
	}
}

// AUTO'S YES LAPSES WITH AUTO. An act the main agent approved in Auto that has
// not gone out comes back to wait for the person once they switch to Manual
// or take the job over; the person's own yes stays.
func TestAutoApprovedActsWaitAgainWhenTheJobLeavesAuto(t *testing.T) {
	for name, leave := range map[string]func(*Item){
		"manual":     func(it *Item) { it.Autonomy = AutonomyManual },
		"taken over": func(it *Item) { it.Manual = true },
	} {
		it := actJob(AutonomyAuto)
		_, _ = AddActs(&it, cleaned(t, it, reply("inline:501", "Renamed it.")), ActByMainAgent, "", actNow)
		_, _ = AddActs(&it, cleaned(t, it, PublicAct{Kind: ActKindComment, Text: "On it."}), ActByYou, "", actNow)
		if it.Acts[0].State != ActApproved || it.Acts[0].ApprovedBy != ActByMainAgent {
			t.Fatalf("%s: Auto did not approve: %+v", name, it.Acts[0])
		}
		leave(&it)
		// Not sent even before a settle has looked at it.
		if next, ok := NextAct(it, actNow); ok || !ActsDueAt(it, actNow).IsZero() {
			t.Fatalf("%s: Auto's act is still due: %+v", name, next)
		}
		if !SettleActs(&it, actNow) {
			t.Fatalf("%s: nothing changed", name)
		}
		if a := it.Acts[0]; a.State != ActProposed || a.ApprovedBy != "" || a.AfterPush != "" || a.WithPush {
			t.Fatalf("%s: Auto's act after leaving Auto: %+v", name, a)
		}
		if a := it.Acts[1]; a.State != ActApproved || a.ApprovedBy != ActByYou {
			t.Fatalf("%s: the person's own act lost its yes: %+v", name, a)
		}
		if waiting := ProposedActs(it); len(waiting) != 1 || waiting[0].ID != it.Acts[0].ID {
			t.Fatalf("%s: waiting %+v", name, waiting)
		}
		if SettleActs(&it, actNow) {
			t.Errorf("%s: settled twice", name)
		}
	}
	// An Auto act approved with an Auto push follows the same rule.
	pushed := pushWaiting(actJob(AutonomyAuto))
	_, _ = AddActs(&pushed, cleaned(t, pushed, reply("inline:501", "Fixed in the next push.")), ActByMainAgent, "", actNow)
	pushed.Approval = &Approval{Kind: "proof", State: "approving", Automatic: true, Commit: pushed.HeadCommit}
	pushed.Autonomy = AutonomyManual
	if !SettleActs(&pushed, actNow) || pushed.Acts[0].State != ActProposed || pushed.Acts[0].AfterPush != "" {
		t.Fatalf("after Manual beside a push: %+v", pushed.Acts[0])
	}
}

// AN ACT APPROVED AS IT IS ADDED FOLLOWS ONLY A PUSH ALREADY UNDER WAY. In
// Manual the person's own act never waits on a push approval that merely
// waits beside it, which they may never give, and Auto's does not either; a
// push already pressed or going out goes first. One that followed a pressed
// push the person then undid goes out without it.
func TestAnActAddedApprovedFollowsOnlyAPushUnderWay(t *testing.T) {
	for _, c := range []struct{ mode, by string }{{AutonomyManual, ActByYou}, {AutonomyAuto, ActByMainAgent}} {
		it := pushWaiting(actJob(c.mode))
		if !OffersPublishApproval(it) {
			t.Fatal("the push does not wait")
		}
		got, err := AddActs(&it, cleaned(t, it, PublicAct{Kind: ActKindComment, Text: "On it."}), c.by, "", actNow)
		if err != nil || got[0].State != ActApproved || got[0].AfterPush != "" || got[0].WithPush {
			t.Fatalf("%s: added beside a waiting push: %+v, err %v", c.by, got, err)
		}
		if next, ok := NextAct(it, actNow); !ok || next.ID != got[0].ID {
			t.Fatalf("%s: the act waits on a push it was not approved with", c.by)
		}
		// The push is never approved and a new round lands another commit:
		// the act is not failed for a push it never followed.
		it.HeadCommit = strings.Repeat("3", 40)
		if SettleActs(&it, actNow) || it.Acts[0].State != ActApproved {
			t.Fatalf("%s: after the commit changed: %+v", c.by, it.Acts[0])
		}
	}
	for name, under := range map[string]func(*Item){
		"pressed":    func(it *Item) { it.Approval = &Approval{Kind: "proof", State: "approving", Commit: it.HeadCommit} },
		"publishing": func(it *Item) { it.Publishing, it.State = true, StateQueued },
	} {
		it := pushWaiting(actJob(AutonomyManual))
		under(&it)
		got, _ := AddActs(&it, cleaned(t, it, PublicAct{Kind: ActKindComment, Text: "On it."}), ActByYou, "", actNow)
		if got[0].AfterPush != it.HeadCommit || got[0].WithPush {
			t.Fatalf("%s: added beside a push under way: %+v", name, got[0])
		}
		if _, ok := NextAct(it, actNow); ok {
			t.Fatalf("%s: the act is due before the push under way", name)
		}
		// ApproveActs on its own follows the same push.
		mine := pushWaiting(actJob(AutonomyManual))
		_, _ = AddActs(&mine, cleaned(t, mine, reply("inline:501", "Renamed it.")), ActByMainAgent, "", actNow)
		under(&mine)
		ApproveActs(&mine, "me", false, actNow)
		if a := mine.Acts[0]; a.AfterPush != mine.HeadCommit || a.WithPush {
			t.Fatalf("%s: approved on its own beside a push under way: %+v", name, a)
		}
	}
	// The person undoes the pressed push: it only waits again, and the act
	// approved on its own goes out without it.
	it := pushWaiting(actJob(AutonomyManual))
	it.Approval = &Approval{Kind: "proof", State: "approving", Commit: it.HeadCommit}
	_, _ = AddActs(&it, cleaned(t, it, PublicAct{Kind: ActKindComment, Text: "On it."}), ActByYou, "", actNow)
	it.Approval = nil // Undo
	if !SettleActs(&it, actNow) || it.Acts[0].State != ActApproved || it.Acts[0].AfterPush != "" {
		t.Fatalf("after Undo: %+v", it.Acts[0])
	}
	if _, ok := NextAct(it, actNow); !ok {
		t.Fatal("the act still waits on the undone push")
	}
}

// A DECISION PUT AWAY IS NOT LEAVING AUTO. Acts the main agent approved in
// Auto are not sent while the person has the job's decision paused, and keep
// their yes through it: once the job is resumed they go out.
func TestAutoApprovedActsStayApprovedThroughAPausedDecision(t *testing.T) {
	it := actJob(AutonomyAuto)
	it.Talk = "talk-7"
	_, _ = AddActs(&it, cleaned(t, it, reply("inline:501", "Renamed it.")), ActByMainAgent, "", actNow)
	if it.Acts[0].State != ActApproved || it.Acts[0].ApprovedBy != ActByMainAgent {
		t.Fatalf("Auto did not approve: %+v", it.Acts[0])
	}
	if !CanPauseDecision(it) {
		t.Fatal("the job's decision cannot be paused")
	}
	PauseDecision(&it, 3)
	if !RecoveryPaused(it) || Automatic(it) {
		t.Fatal("the decision is not paused")
	}
	if _, ok := NextAct(it, actNow); ok {
		t.Fatal("an act is sent while the decision is paused")
	}
	if SettleActs(&it, actNow) {
		t.Fatalf("a paused decision changed the act: %+v", it.Acts[0])
	}
	if a := it.Acts[0]; a.State != ActApproved || a.ApprovedBy != ActByMainAgent {
		t.Fatalf("Auto's yes lapsed on a paused decision: %+v", a)
	}
	// Resume.
	it.PausedRecoveryQuestion = ""
	if SettleActs(&it, actNow) {
		t.Fatalf("Resume changed the act: %+v", it.Acts[0])
	}
	if next, ok := NextAct(it, actNow); !ok || next.ID != it.Acts[0].ID || next.ApprovedBy != ActByMainAgent {
		t.Fatal("Auto's act is not sent after Resume")
	}
}

// An act written to follow a push that will never go out is not sent.
func TestAnActWhosePushNeverHappensFails(t *testing.T) {
	it := pushWaiting(actJob(AutonomyAuto))
	it.Approval = &Approval{Kind: "proof", State: "approving", Automatic: true, Commit: it.HeadCommit}
	got, _ := AddActs(&it, cleaned(t, it, reply("inline:501", "Fixed in the next push.")), ActByMainAgent, "", actNow)
	if got[0].State != ActApproved || got[0].AfterPush != it.HeadCommit {
		t.Fatalf("Auto beside a pressed push: %+v", got[0])
	}
	it.Approval = nil
	// The work was sent back and a new round landed another commit.
	it.HeadCommit = strings.Repeat("3", 40)
	if !SettleActs(&it, actNow) || it.Acts[0].State != ActFailed || it.Acts[0].Error != ActPushGone {
		t.Fatalf("after the commit changed: %+v", it.Acts[0])
	}
}

// A job that ended, or that its person stopped, sends nothing more.
func TestActsEndWithTheJob(t *testing.T) {
	stopped := true
	for name, end := range map[string]func(*Item){
		"merged":  func(it *Item) { it.PullRequest.State = PRMerged },
		"closed":  func(it *Item) { it.PullRequest.State = PRClosed },
		"dropped": func(it *Item) { it.DoneHow = "dropped" },
		"stopped": func(it *Item) { it.PersonStopped = &stopped },
	} {
		it := actJob(AutonomyManual)
		_, _ = AddActs(&it, cleaned(t, it, reply("inline:501", "Renamed it.")), ActByMainAgent, "", actNow)
		_, _ = AddActs(&it, cleaned(t, it, PublicAct{Kind: ActKindComment, Text: "On it."}), ActByYou, "", actNow)
		end(&it)
		if !SettleActs(&it, actNow) {
			t.Fatalf("%s: nothing changed", name)
		}
		if it.Acts[0].State != ActDeclined || it.Acts[1].State != ActFailed || !strings.Contains(it.Acts[1].Error, "not sent") {
			t.Errorf("%s: %+v", name, it.Acts)
		}
		if _, ok := NextAct(it, actNow); ok || !ActsDueAt(it, actNow).IsZero() {
			t.Errorf("%s: an act is still due", name)
		}
		if SettleActs(&it, actNow) {
			t.Errorf("%s: settled twice", name)
		}
	}
}

// ── THE WAIT ────────────────────────────────────────────────────────────────

// stored is the job with its wait and offers as the store sets them.
func stored(it Item) Item {
	SettleWait(Item{}, &it, actNow)
	return it
}

func withProposed(t *testing.T, it Item, acts ...PublicAct) Item {
	t.Helper()
	if len(acts) == 0 {
		acts = []PublicAct{reply("inline:501", "Renamed it."), reply("conversation:601", "It is still needed."), {Kind: ActKindChangeBase, Base: "dev"}}
	}
	if _, err := AddActs(&it, cleaned(t, it, acts...), ActByMainAgent, "", actNow); err != nil {
		t.Fatal(err)
	}
	return it
}

// EVERY WAIT A PROPOSED SET CAN MEET, with what the job then reads, what it
// offers and that the stored wait is never at fault.
func TestWaitWithActsWaiting(t *testing.T) {
	const summary = "post 2 replies and change the base to dev"
	step := func(state State) Item {
		it := actJob(AutonomyManual)
		it.State = state
		it.FollowUp.State = "running"
		it.Stream.Phases[0].State = PhaseRunning
		return it
	}
	mergeable := actJob(AutonomyManual)
	mergeable.PullRequest.Draft = false
	mergeable.PullRequest.Checks = PRChecks{Passed: 2}
	mergeable.PullRequest.ReviewDecision = "approved"
	conflict := actJob(AutonomyManual)
	conflict.FollowUp = nil
	conflict.PullRequest.Conflict, conflict.PullRequest.Mergeable = true, MergeableNo
	budget := actJob(AutonomyManual)
	budget.FollowUp = &PRFollowUp{Kind: "pr", State: "needs_you", BlockedBy: FollowBlockedBudget, Reason: "Job budget reached."}
	asking := step(StateRunning)
	asking.Asking = &Asked{Stage: FollowPRStep, Question: "Which table?", With: AskedManager}
	publishing := pushWaiting(actJob(AutonomyManual))
	publishing.Publishing, publishing.State = true, StateQueued
	approving := pushWaiting(actJob(AutonomyManual))
	approving.Approval = &Approval{Kind: "proof", State: "approving", UndoUntil: actNow.Add(ApprovalUndo)}
	held := step(StateRunning)
	held.Stream.Paused = true
	cases := []struct {
		name string
		base Item
		// under is the wait without the acts, want the wait with them.
		under, want string
		words       string
	}{
		{"a pull request waiting on GitHub", actJob(AutonomyManual), "github:checks", "you:approve_acts", "Approve: " + summary + "?"},
		{"a step that is running does not hide them", step(StateRunning), "working:" + FollowPRStep, "you:approve_acts", "Approve: " + summary + "?"},
		{"a decision nothing narrower names", conflict, "you:decide", "you:approve_acts", "Approve: " + summary + "?"},
		{"a push to approve outranks them and names them", pushWaiting(actJob(AutonomyManual)), "you:approve_publish", "you:approve_publish", "Approving also approves: " + summary + "."},
		{"a merge is never hidden", mergeable, "you:merge", "you:merge", "Also waiting: " + summary + "."},
		{"the budget is never hidden", budget, "you:budget", "you:budget", "Also waiting: " + summary + "."},
		{"a publish under way goes first", publishing, "working:publishing", "working:publishing", ""},
		{"an approval inside its Undo window goes first", approving, "working:approving", "working:approving", ""},
		{"a question the manager holds keeps its clock", asking, "main_agent:" + FollowPRStep, "main_agent:" + FollowPRStep, ""},
		{"a job waiting for a computer", step(StateQueued), "runner:" + FollowPRStep, "runner:" + FollowPRStep, ""},
		{"a job its person put away", held, "you:resume:put away", "you:resume:put away", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := WaitOf(tc.base).key(); got != tc.under {
				t.Fatalf("without acts the job reads %s, want %s", got, tc.under)
			}
			it := stored(withProposed(t, tc.base))
			if got := it.Wait.key(); got != tc.want {
				t.Fatalf("with acts waiting the job reads %s, want %s", got, tc.want)
			}
			if tc.words != "" && !strings.Contains(it.Wait.Words, tc.words) {
				t.Fatalf("words %q, want %q in them", it.Wait.Words, tc.words)
			}
			if err := WaitFault(it); err != nil {
				t.Fatalf("wait fault: %v", err)
			}
			// Whatever the wait reads, both answers are the person's to give.
			if !Offered(it, OfferByYou, DoActsApprove) || !Offered(it, OfferByYou, DoActsDecline) {
				t.Fatalf("offers %v", it.Offers)
			}
			if Offered(it, OfferByMainAgent, DoActsApprove) {
				t.Fatalf("Manual offers the manager the yes: %v", it.Offers)
			}
			// The yes and the no each put the job back where it was.
			for name, answer := range map[string]func(*Item){
				"approved": func(it *Item) { ApproveActs(it, "me", false, actNow) },
				"declined": func(it *Item) { DeclineActs(it, "", actNow) },
			} {
				after := it
				answer(&after)
				after = stored(after)
				if got := after.Wait.key(); got != tc.under {
					t.Fatalf("%s, the job reads %s, want %s", name, got, tc.under)
				}
				if err := WaitFault(after); err != nil {
					t.Fatalf("%s: wait fault: %v", name, err)
				}
				if Offered(after, OfferByYou, DoActsApprove) || Offered(after, OfferByYou, DoActsDecline) {
					t.Fatalf("%s, and still offered: %v", name, after.Offers)
				}
			}
		})
	}
}

// A job that is over, stopped or put away offers no answer to acts: its doors
// refuse them.
func TestNoActsOfferedWhereTheDoorsRefuse(t *testing.T) {
	stopped := true
	for name, change := range map[string]func(*Item){
		"merged":  func(it *Item) { it.PullRequest.State = PRMerged },
		"stopped": func(it *Item) { it.PersonStopped = &stopped },
	} {
		it := withProposed(t, actJob(AutonomyManual))
		change(&it)
		it = stored(it)
		if Offered(it, OfferByYou, DoActsApprove) || it.Wait.Action == ActApproveActs {
			t.Errorf("%s: wait %s, offers %v", name, it.Wait.key(), it.Offers)
		}
		if err := WaitFault(it); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// In Auto the manager may give the yes to acts proposed while the job was in
// Manual; nothing gives it by itself.
func TestAutoOffersTheManagerTheYes(t *testing.T) {
	it := withProposed(t, actJob(AutonomyManual))
	it.Autonomy = AutonomyAuto
	it = stored(it)
	if it.Wait.key() != "you:approve_acts" || !Offered(it, OfferByMainAgent, DoActsApprove) || !Offered(it, OfferByYou, DoActsApprove) {
		t.Fatalf("wait %s, offers %v", it.Wait.key(), it.Offers)
	}
	if SettleActs(&it, actNow) || len(ProposedActs(it)) != 3 {
		t.Fatal("a switch to Auto approved acts by itself")
	}
}

// The wait's clock does not start again when acts join a wait that stays.
func TestActsJoiningAWaitKeepItsClock(t *testing.T) {
	before := stored(pushWaiting(actJob(AutonomyManual)))
	after := withProposed(t, before)
	SettleWait(before, &after, actNow.Add(time.Hour))
	if !after.Wait.Since.Equal(before.Wait.Since) {
		t.Fatalf("since moved from %v to %v", before.Wait.Since, after.Wait.Since)
	}
	if WaitChanged(before, after) {
		t.Fatal("the wait reads as changed")
	}
	github := stored(actJob(AutonomyManual))
	asked := withProposed(t, github)
	SettleWait(github, &asked, actNow.Add(time.Hour))
	if !WaitChanged(github, asked) || !asked.Wait.Since.Equal(actNow.Add(time.Hour)) {
		t.Fatalf("a job that began to wait on its person: %+v", asked.Wait)
	}
}

func TestWaitLineNamesTheActs(t *testing.T) {
	it := stored(withProposed(t, actJob(AutonomyManual)))
	line := WaitLine(it)
	if !strings.Contains(line, "approve what is to be posted or changed on the pull request") || !strings.Contains(line, DoActsApprove) {
		t.Fatalf("line %q", line)
	}
}

// The manager reads what became of its acts: what waits, what is on its way
// and how the last ones ended.
func TestActsStatusSaysWhatBecameOfEachAct(t *testing.T) {
	if got := ActsStatus(actJob(AutonomyManual)); got != nil {
		t.Fatalf("a job with no acts says %v", got)
	}
	it := actJob(AutonomyAuto)
	got, _ := AddActs(&it, cleaned(t, it, reply("inline:501", "Renamed it."), PublicAct{Kind: ActKindComment, Text: "Ready."}, PublicAct{Kind: ActKindChangeBase, Base: "dev"}), ActByMainAgent, "", actNow)
	BeginAct(&it, got[0].ID, actNow)
	FinishAct(&it, got[0].ID, "https://github.com/acme/api/pull/7#discussion_r502", "Replied in inline:501.", actNow)
	BeginAct(&it, got[1].ID, actNow)
	FailAct(&it, got[1].ID, "Your GitHub sign-in may not comment on this pull request.", actNow)
	BeginAct(&it, got[2].ID, actNow)
	RetryAct(&it, got[2].ID, "GitHub did not answer.", actNow)
	it.Autonomy = AutonomyManual
	_, _ = AddActs(&it, cleaned(t, it, PublicAct{Kind: ActKindMarkReady}), ActByMainAgent, "", actNow)
	want := []string{
		"Public acts waiting on the person's approval: mark the pull request ready for review.",
		"Approved, and being tried again (GitHub did not answer): change the base to dev.",
		"Done: Replied in inline:501. https://github.com/acme/api/pull/7#discussion_r502",
		"Not sent: comment on the pull request. Your GitHub sign-in may not comment on this pull request.",
	}
	if lines := ActsStatus(it); fmt.Sprint(lines) != fmt.Sprint(want) {
		t.Fatalf("status:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}
