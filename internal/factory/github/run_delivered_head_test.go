package github

import (
	"context"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

// A pull request a run on another machine opened carries no commit this
// factory approved, because no worktree here ever held it. The fifth live
// A+B run shipped five such jobs that Merge refused forever with "no
// approved commit is recorded for this job" while every native job beside
// them read "ready for you to merge" (E10). The follow records the head it
// first reads after the run ended, the way a native publish records the
// head the person approved; a push after that is a commit nobody approved.
func TestFollowRecordsTheHeadARunDeliveredSoMergeIsOffered(t *testing.T) {
	m := newMemAPI(time.Now())
	m.followed = map[string]forge.Pull{"acme/api#241": {Number: 241, URL: "https://github.com/acme/api/pull/241", State: "open", HeadSHA: "f59a2188", Base: "main", Mergeable: boolPtr(true)}}
	st := openStore(t)
	remote, err := st.Create(factory.Item{
		Repo: "acme/api", Num: 233, Kind: factory.KindIssue, Title: "Positives", State: factory.StateShipped, Publish: "pr",
		Stages:          []factory.Stage{{Name: "build", Kind: factory.StageAgentField, Target: "swe-planner.build_issue", On: true}},
		AgentFieldSteps: map[string]*factory.AgentFieldRun{"build": {Target: "swe-planner.build_issue", Status: "succeeded", Ended: time.Now()}},
		// The pull request as the run named it, with the head the follow
		// already read on an earlier engine: this is the state the r5 jobs
		// were left in, and a follow with nothing new to say must still pin it.
		PullRequest: &factory.PullRequest{Number: 241, URL: "https://github.com/acme/api/pull/241", State: factory.PROpen, HeadSHA: "f59a2188"},
	})
	if err != nil {
		t.Fatal(err)
	}
	native, err := st.Create(factory.Item{
		Repo: "acme/api", Num: 232, Kind: factory.KindIssue, Title: "IDs", State: factory.StateShipped, Publish: "pr",
		Stages:      []factory.Stage{{Name: "code", Kind: factory.StageChat, On: true}},
		PullRequest: &factory.PullRequest{Number: 240, URL: "https://github.com/acme/api/pull/240", State: factory.PROpen},
	})
	if err != nil {
		t.Fatal(err)
	}
	m.followed["acme/api#240"] = forge.Pull{Number: 240, URL: "https://github.com/acme/api/pull/240", State: "open", HeadSHA: "pushedbyhand", Base: "main", Mergeable: boolPtr(true)}
	src := New(m, nil, nil)
	for range 2 {
		if err := followOpened(context.Background(), src, st); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := st.Get(remote.ID)
	if got.HeadCommit != "f59a2188" {
		t.Fatalf("a run-delivered pull request recorded head %q, want the head read after the run ended", got.HeadCommit)
	}
	if v := factory.MergeOffered(got); !v.OK {
		t.Fatalf("merge still refused for a run-delivered pull request: %s", v.Why)
	}
	if got.FollowUp != nil {
		t.Fatalf("a run-delivered pull request began a CI follow-up this machine has no checkout to carry out: %+v", got.FollowUp)
	}
	// A NATIVE JOB'S APPROVED COMMIT IS ITS PROOF'S, NEVER THE FORGE'S. One
	// with none recorded stays refused rather than adopting whatever head the
	// pull request carries now.
	n, _ := st.Get(native.ID)
	if n.HeadCommit != "" {
		t.Fatalf("a native job adopted the forge's head %q as its approved commit", n.HeadCommit)
	}

	// A push after the pin is a commit nobody approved.
	m.followed["acme/api#241"] = forge.Pull{Number: 241, URL: "https://github.com/acme/api/pull/241", State: "open", HeadSHA: "later", Base: "main", Mergeable: boolPtr(true)}
	if err := followOpened(context.Background(), src, st); err != nil {
		t.Fatal(err)
	}
	got, _ = st.Get(remote.ID)
	if got.HeadCommit != "f59a2188" {
		t.Fatalf("a later push moved the recorded head to %q", got.HeadCommit)
	}
	if v := factory.MergeOffered(got); v.OK || v.Why != "the pull request has commits you did not approve" {
		t.Fatalf("merge after a later push = %+v, want the unapproved-commits refusal", v)
	}
}

func boolPtr(b bool) *bool { return &b }
