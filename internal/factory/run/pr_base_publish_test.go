package run

import (
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
)

func TestPublishPRBaseSyncRequiresTheVerifiedBase(t *testing.T) {
	for _, tc := range []struct {
		name        string
		merge       bool
		baseRace    bool
		noPRBranch  bool
		wantRefusal string
	}{
		{name: "publish verified merge", merge: true},
		{name: "base moved before publishing", merge: true, baseRace: true, wantRefusal: "base"},
		{name: "approved head omits the base", wantRefusal: "base"},
		{name: "existing PR branch unknown", merge: true, noPRBranch: true, wantRefusal: "branch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := newPublishRig(t, factory.Item{Num: 12, Kind: factory.KindIssue, Repo: "acme/api", Title: "Fix it"})
			original := mustGit(t, g.dir, "rev-parse", "HEAD")
			base := mustGit(t, g.dir, "commit-tree", original+"^{tree}", "-p", original, "-m", "base advances")
			mustGit(t, g.dir, "add", "ledger.go")
			mustGit(t, g.dir, "commit", "-q", "-m", "PR work")
			oldHead := mustGit(t, g.dir, "rev-parse", "HEAD")
			localBranch := branchFor(g.item(t))
			prBranch := localBranch + "-2"
			remoteBase := base
			if tc.baseRace {
				remoteBase = mustGit(t, g.dir, "commit-tree", base+"^{tree}", "-p", base, "-m", "base advances again")
			}
			mustGit(t, g.dir, "push", "-q", "origin", remoteBase+":refs/heads/main", oldHead+":refs/heads/"+prBranch, original+":refs/heads/"+localBranch)
			if tc.merge {
				mustGit(t, g.dir, "merge", "--no-edit", base)
			} else {
				// A clean, newer approved commit is insufficient without the base.
				mustGit(t, g.dir, "commit", "--allow-empty", "-q", "-m", "other verified work")
			}
			head := mustGit(t, g.dir, "rev-parse", "HEAD")
			pr := factory.PullRequest{
				Number: 7, URL: "https://github.com/acme/api/pull/7", State: factory.PROpen,
				HeadSHA: oldHead, HeadBranch: prBranch, BaseSHA: base, CI: "ci ✓", ChecksRead: true,
				Checks: factory.PRChecks{Passed: 8}, ReadinessKey: "review of the old head",
			}
			if err := g.st.Update(g.id, func(it *factory.Item) error {
				it.Publish, it.HeadCommit, it.PullRequest = "pr", head, &pr
				it.FollowUp = &factory.PRFollowUp{Kind: "pr", State: "waiting_approval", HeadSHA: oldHead, BaseSyncSHA: base}
				it.MarkPublishing("queued", "", "", time.Now())
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			fresh := pr
			fresh.BaseSHA = remoteBase
			if tc.noPRBranch {
				fresh.HeadBranch = ""
			}
			g.r.opts.Source = func(string) factory.Source { return ciPublishSource{g.src, fresh} }
			it := publishAndSettle(t, g, testTrailer+" "+head)
			if got := remoteTip(t, g, "main"); got != remoteBase {
				t.Fatalf("base branch moved: %s want %s", got, remoteBase)
			}
			if got := remoteTip(t, g, localBranch); got != original {
				t.Fatalf("unrelated branch moved: %s want %s", got, original)
			}
			if len(g.src.got) != 0 {
				t.Fatalf("published a new PR or comment: %+v", g.src.got)
			}
			if tc.wantRefusal != "" {
				if it.PublishStatus == nil || it.PublishStatus.State != "failed" || !strings.Contains(strings.ToLower(it.PublishStatus.Detail), tc.wantRefusal) {
					t.Fatalf("expected a publication refusal mentioning %q, got %+v", tc.wantRefusal, it.PublishStatus)
				}
				if got := remoteTip(t, g, prBranch); got != oldHead {
					t.Fatalf("unverified update was pushed: %s want %s", got, oldHead)
				}
				return
			}
			if it.PublishStatus == nil || it.PublishStatus.State != "opened" || it.PullRequest == nil || it.PullRequest.Number != pr.Number || it.PullRequest.HeadSHA != head || it.PullRequest.BaseSHA != base {
				t.Fatalf("verified sync did not publish to the existing PR: %+v, %+v", it.PublishStatus, it.PullRequest)
			}
			if got := remoteTip(t, g, prBranch); got != head {
				t.Fatalf("PR branch did not receive approved head: %s want %s", got, head)
			}
			mustGit(t, g.bare, "merge-base", "--is-ancestor", base, head)
			mustGit(t, g.bare, "merge-base", "--is-ancestor", oldHead, head)
			if it.PullRequest.ChecksRead || it.PullRequest.CI != "" || it.PullRequest.Checks.Passed != 0 || it.PullRequest.ReadinessKey != "" {
				t.Fatalf("old head's CI or readiness survived publication: %+v", it.PullRequest)
			}
		})
	}
}
