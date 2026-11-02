package run

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

// someoneElsesBranch puts a commit the factory did not make on a branch of the
// remote, cut from nothing the job holds, and answers the commit.
func someoneElsesBranch(t *testing.T, g publishRig, branch string) string {
	t.Helper()
	other := t.TempDir()
	mustGit(t, "", "clone", "-q", g.bare, other)
	mustGit(t, other, "config", "user.name", "someone")
	mustGit(t, other, "config", "user.email", "someone@t")
	mustGit(t, other, "config", "commit.gpgsign", "false")
	mustGit(t, other, "commit", "--allow-empty", "-q", "-m", "someone's work on "+branch)
	mustGit(t, other, "push", "-q", "origin", "HEAD:refs/heads/"+branch)
	return mustGit(t, other, "rev-parse", "HEAD")
}

// reviewed commits the job's work and records the head a person approved.
func reviewed(t *testing.T, g publishRig, file string) (head, trailer string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, file), []byte("package ledger\n\n// "+file+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	mustGit(t, g.dir, "add", "-A")
	mustGit(t, g.dir, "commit", "-q", "-m", "reviewed "+file)
	head = mustGit(t, g.dir, "rev-parse", "HEAD")
	if err := g.st.Update(g.id, func(it *factory.Item) error {
		it.HeadCommit, it.State = head, factory.StateLanded
		it.MarkPublishing("queued", "", "", time.Now())
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return head, testTrailer + " " + head
}

func publishAndSettle(t *testing.T, g publishRig, trailer string) factory.Item {
	t.Helper()
	if err := g.r.Publish(g.id, trailer); err != nil {
		t.Fatal(err)
	}
	return g.settle(t)
}

func remoteTip(t *testing.T, g publishRig, branch string) string {
	t.Helper()
	line := mustGit(t, g.bare, "for-each-ref", "--format=%(objectname)", "refs/heads/"+branch)
	return strings.TrimSpace(line)
}

// The symptom: an older branch of the same name holds commits this job did not
// make. The person still gets a PR of exactly what they approved, and the older
// branch is exactly as it was.
func TestPublishOverABranchItDidNotMakeOpensThePRBesideIt(t *testing.T) {
	g := newPublishRig(t, factory.Item{Num: 12, Kind: factory.KindIssue, Repo: "acme/api", Title: "Fix it"})
	head, trailer := reviewed(t, g, "work.go")
	branch := branchFor(g.item(t))
	theirs := someoneElsesBranch(t, g, branch)

	it := publishAndSettle(t, g, trailer)

	if it.PublishStatus.State != "opened" || it.PullRequest == nil {
		t.Fatalf("no PR: %+v, error %q", it.PublishStatus, it.PublishError)
	}
	if got := remoteTip(t, g, branch); got != theirs {
		t.Fatalf("the older branch moved: %s, want %s", got, theirs)
	}
	if got := remoteTip(t, g, branch+"-2"); got != head {
		t.Fatalf("the approved head is at %q on the side branch, want %s", got, head)
	}
	if len(g.src.got) != 1 || g.src.got[0].Branch != branch+"-2" {
		t.Fatalf("PR writes: %+v", g.src.got)
	}
	if !logMentions(it, branch) || !logMentions(it, "did not make") {
		t.Fatalf("the job does not say why the branch differs: %+v", it.Stream.Log)
	}
}

// Retrying after the PR could not open finds the branch already holding the
// approved head, and neither moves it nor makes a third.
func TestRetryPublishReusesTheBranchItAlreadyPushed(t *testing.T) {
	g := newPublishRig(t, factory.Item{Num: 12, Kind: factory.KindIssue, Repo: "acme/api", Title: "Fix it"})
	head, trailer := reviewed(t, g, "work.go")
	branch := branchFor(g.item(t))
	someoneElsesBranch(t, g, branch)
	g.src.err = errors.New("github is down")
	if it := publishAndSettle(t, g, trailer); it.PublishStatus.State != "failed" {
		t.Fatalf("first publish: %+v", it.PublishStatus)
	}
	g.src.err = nil
	if err := g.st.Update(g.id, func(it *factory.Item) error { it.MarkPublishing("queued", "", "", time.Now()); return nil }); err != nil {
		t.Fatal(err)
	}
	it := publishAndSettle(t, g, trailer)
	if it.PublishStatus.State != "opened" || remoteTip(t, g, branch+"-2") != head || remoteTip(t, g, branch+"-3") != "" {
		t.Fatalf("retry: %+v, -3 at %q", it.PublishStatus, remoteTip(t, g, branch+"-3"))
	}
	if last := g.src.got[len(g.src.got)-1]; last.Branch != branch+"-2" {
		t.Fatalf("retry opened from %s", last.Branch)
	}
}

// The same job's own earlier push, unmoved since: its new history replaces it
// on the same branch, and the same PR stays the job's.
func TestPublishReplacesTheBranchOnlyWhenItIsStillWhatThisMachinePushed(t *testing.T) {
	g := newPublishRig(t, factory.Item{Num: 12, Kind: factory.KindIssue, Repo: "acme/api", Title: "Fix it"})
	base := mustGit(t, g.dir, "rev-parse", "HEAD")
	_, trailer := reviewed(t, g, "work.go")
	branch := branchFor(g.item(t))
	if it := publishAndSettle(t, g, trailer); it.PublishStatus.State != "opened" {
		t.Fatalf("first publish: %+v", it.PublishStatus)
	}

	t.Run("still ours", func(t *testing.T) {
		mustGit(t, g.dir, "reset", "-q", "--keep", base) // A re-run cut afresh: new history, no ancestor.
		head, trailer := reviewed(t, g, "other.go")
		it := publishAndSettle(t, g, trailer)
		if it.PublishStatus.State != "opened" || remoteTip(t, g, branch) != head || remoteTip(t, g, branch+"-2") != "" {
			t.Fatalf("publish: %+v; tip %s want %s", it.PublishStatus, remoteTip(t, g, branch), head)
		}
	})
	t.Run("someone pushed since", func(t *testing.T) {
		other := t.TempDir()
		mustGit(t, "", "clone", "-q", "--branch", branch, g.bare, other)
		mustGit(t, other, "config", "user.name", "someone")
		mustGit(t, other, "config", "user.email", "someone@t")
		mustGit(t, other, "config", "commit.gpgsign", "false")
		mustGit(t, other, "commit", "--allow-empty", "-q", "-m", "a person's fix")
		mustGit(t, other, "push", "-q", "origin", branch)
		theirs := mustGit(t, other, "rev-parse", "HEAD")
		mustGit(t, g.dir, "reset", "-q", "--keep", base)
		head, trailer := reviewed(t, g, "third.go")
		it := publishAndSettle(t, g, trailer)
		if it.PublishStatus.State != "opened" || remoteTip(t, g, branch) != theirs || remoteTip(t, g, branch+"-2") != head {
			t.Fatalf("publish: %+v; tip %s want %s; side %s", it.PublishStatus, remoteTip(t, g, branch), theirs, remoteTip(t, g, branch+"-2"))
		}
	})
}

// When no name is free the job says so in a person's words, with nothing moved.
func TestPublishWithEveryBranchNameTakenSaysSoPlainly(t *testing.T) {
	g := newPublishRig(t, factory.Item{Num: 12, Kind: factory.KindIssue, Repo: "acme/api", Title: "Fix it"})
	_, trailer := reviewed(t, g, "work.go")
	branch := branchFor(g.item(t))
	theirs := someoneElsesBranch(t, g, branch)
	for n := 2; n <= publishBranchNames; n++ {
		mustGit(t, g.bare, "update-ref", fmt.Sprintf("refs/heads/%s-%d", branch, n), theirs)
	}
	it := publishAndSettle(t, g, trailer)
	if it.PublishStatus.State != "failed" || it.PublishStatus.Code != "branch_names_taken" {
		t.Fatalf("publish: %+v", it.PublishStatus)
	}
	if !strings.Contains(it.PublishError, "nothing was overwritten") || strings.Contains(it.PublishError, "non-fast-forward") {
		t.Fatalf("reason = %q", it.PublishError)
	}
	if remoteTip(t, g, branch) != theirs || len(g.src.got) != 0 {
		t.Fatalf("something moved: tip %s, writes %+v", remoteTip(t, g, branch), g.src.got)
	}
}

// A repair to a PR published beside an older branch goes to that PR's branch.
func TestRepairPushGoesToTheBranchThePRComesFrom(t *testing.T) {
	g := newPublishRig(t, factory.Item{Num: 12, Kind: factory.KindIssue, Repo: "acme/api", Title: "Fix it"})
	first, trailer := reviewed(t, g, "work.go")
	branch := branchFor(g.item(t))
	theirs := someoneElsesBranch(t, g, branch)
	opened := publishAndSettle(t, g, trailer)
	if opened.PullRequest == nil || opened.PullRequest.HeadBranch != branch+"-2" {
		t.Fatalf("receipt: %+v", opened.PullRequest)
	}
	head, trailer := reviewed(t, g, "repair.go")
	if err := g.st.Update(g.id, func(it *factory.Item) error {
		it.PullRequest.HeadSHA = first
		it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "waiting_approval", HeadSHA: first}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	fresh := factory.PullRequest{Number: opened.PullRequest.Number, URL: opened.PullRequest.URL, State: factory.PROpen, HeadSHA: first, HeadBranch: branch + "-2"}
	g.r.opts.Source = func(string) factory.Source { return following{g.src, fresh} }
	it := publishAndSettle(t, g, trailer)
	if it.PublishStatus.State != "opened" || remoteTip(t, g, branch+"-2") != head || remoteTip(t, g, branch) != theirs {
		t.Fatalf("repair: %+v, %q; side %s want %s", it.PublishStatus, it.PublishError, remoteTip(t, g, branch+"-2"), head)
	}
}

// A worker's failure report reaches the desk as a reason a person can read.
func TestAFailedPublishReportCarriesAPlainReason(t *testing.T) {
	it := factory.Item{State: factory.StateRunning, Stream: &factory.Stream{}}
	report := factory.RunReport{Publishing: &factory.Publishing{State: "failed", Code: "branch_names_taken", Detail: "To https://github.com/acme/api.git\n ! [rejected]", Attempt: 1}}
	if err := ApplyReport(&it, report, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(it.PublishError, "nothing was overwritten") {
		t.Fatalf("reason = %q", it.PublishError)
	}
	report.Publishing = &factory.Publishing{State: "opened", Attempt: 2}
	if err := ApplyReport(&it, report, time.Now()); err != nil {
		t.Fatal(err)
	}
	if it.PublishError != "" {
		t.Fatalf("an opened publish keeps %q", it.PublishError)
	}
}

func logMentions(it factory.Item, words string) bool {
	for _, l := range it.Stream.Log {
		if strings.Contains(l.Text, words) {
			return true
		}
	}
	return false
}

// following answers the fresh PR a repair is checked against.
type following struct {
	*prSource
	fresh factory.PullRequest
}

func (f following) Follow(context.Context, factory.Item) (factory.PullRequest, error) {
	return f.fresh, nil
}
