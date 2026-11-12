package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/exec"
	"github.com/Agent-Field/codeaf/internal/factory"
)

// The object pushed to the PR is the signed object the person approved, with
// their author identity intact and one co-author recognized by Git's parser.
func TestFactoryCommitCoauthorSurvivesExactHeadPublication(t *testing.T) {
	for _, prepared := range []bool{false, true} {
		name := "legacy publish commits"
		if prepared {
			name = "prepared before approval"
		}
		t.Run(name, func(t *testing.T) {
			g := newPublishRig(t, factory.Item{Num: 12, Kind: factory.KindIssue,
				Repo: "acme/api", Title: "Fix dispatch", Assignee: "octocat"})
			trailer, head := testTrailer, ""
			if prepared {
				if err := commitApproved(ExecGit{}, g.dir, g.item(t), ""); err != nil {
					t.Fatal(err)
				}
				head = mustGit(t, g.dir, "rev-parse", "HEAD")
				if err := g.st.Update(g.id, func(it *factory.Item) error {
					it.HeadCommit = head
					return nil
				}); err != nil {
					t.Fatal(err)
				}
				trailer += " " + head
			}
			if err := g.r.Publish(g.id, trailer); err != nil {
				t.Fatal(err)
			}
			it := g.settle(t)
			if it.State != factory.StateShipped || it.PullRequest == nil {
				t.Fatalf("publication: %+v", it.PublishStatus)
			}
			remote := "refs/heads/" + branchFor(it)
			if head != "" && mustGit(t, g.bare, "rev-parse", remote) != head {
				t.Fatal("publication rewrote the approved object")
			}
			if author := mustGit(t, g.bare, "log", "-1", "--format=%an <%ae>", remote); author != "octocat <octocat@users.noreply.github.com>" {
				t.Fatalf("person lost authorship: %q", author)
			}
			coauthors := mustGit(t, g.bare, "log", "-1", "--format=%(trailers:key=Co-Authored-By,valueonly)", remote)
			if want := strings.TrimPrefix(exec.AttributionTrailer, "Co-Authored-By: "); coauthors != want {
				t.Fatalf("parsed coauthors = %q, want exactly %q", coauthors, want)
			}
			message := mustGit(t, g.bare, "log", "-1", "--format=%B", remote)
			if strings.Count(message, exec.AttributionTrailer) != 1 || !strings.Contains(message, exec.AttributionAssistedBy) {
				t.Fatalf("commit signature: %q", message)
			}
			if !prepared && !strings.Contains(message, testTrailer) {
				t.Fatal("legacy approval record was lost")
			}
			// Retrying preparation on the clean branch must preserve its head.
			before := mustGit(t, g.dir, "rev-parse", "HEAD")
			if err := commitApproved(ExecGit{}, g.dir, it, trailer); err != nil {
				t.Fatal(err)
			}
			if after := mustGit(t, g.dir, "rev-parse", "HEAD"); after != before {
				t.Fatal("clean preparation changed the published head")
			}
		})
	}
}

func TestFactoryCommitAttributionHonorsRepositoryPolicy(t *testing.T) {
	for _, folder := range []string{"", ".github", "docs"} {
		t.Run("policy in "+folder, func(t *testing.T) {
			dir := gitRig(t)
			policyDir := filepath.Join(dir, folder)
			if err := os.MkdirAll(policyDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(policyDir, "CONTRIBUTING.md"), []byte("Do not add AI co-authored-by trailers.\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			it := factory.Item{Kind: factory.KindIssue, Repo: "api", Num: 12, Title: "Fix dispatch", Assignee: "octocat"}
			if err := commitApproved(ExecGit{}, dir, it, testTrailer); err != nil {
				t.Fatal(err)
			}
			message := mustGit(t, dir, "log", "-1", "--format=%B")
			if strings.Contains(message, exec.AttributionTrailer) || strings.Contains(message, exec.AttributionAssistedBy) {
				t.Fatalf("repository policy ignored: %q", message)
			}
			if !strings.Contains(message, testTrailer) {
				t.Fatal("repository policy removed the approval record")
			}
		})
	}
}

func TestFactoryAttributionLeavesCleanWorkerCommitUntouched(t *testing.T) {
	dir := gitRig(t)
	mustGit(t, dir, "commit", "--allow-empty", "-q", "-m", "worker's existing commit")
	before := mustGit(t, dir, "rev-parse", "HEAD")
	identity := mustGit(t, dir, "log", "-1", "--format=%an <%ae>")
	if err := commitApproved(ExecGit{}, dir, factory.Item{Title: "Fix dispatch", Assignee: "octocat"}, testTrailer); err != nil {
		t.Fatal(err)
	}
	if after := mustGit(t, dir, "rev-parse", "HEAD"); after != before {
		t.Fatal("attribution rewrote an existing clean commit")
	}
	if after := mustGit(t, dir, "log", "-1", "--format=%an <%ae>"); after != identity {
		t.Fatal("attribution changed the existing author")
	}
}
