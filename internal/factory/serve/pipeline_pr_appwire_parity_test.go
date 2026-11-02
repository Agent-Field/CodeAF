package serve

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// THE APP WIRE'S COPY OF THE ROUTE'S BOUNDS CANNOT DRIFT. factory.pipeline_pr
// refuses, before anything is sent, exactly what this route would refuse for
// size, title, body and branch; a number or a rule changed in one place and not
// the other fails here.
func TestAppwirePipelinePRBoundsAreTheRoutesOwn(t *testing.T) {
	for name, pair := range map[string][2]int{
		"text":    {appwire.PipelinePRTextMax, pipelinePRMaxText},
		"title":   {appwire.PipelinePRTitleMax, pipelinePRMaxTitle},
		"body":    {appwire.PipelinePRBodyMax, pipelinePRMaxBody},
		"branch":  {appwire.PipelinePRBranchMax, pipelinePRMaxBranch},
		"request": {appwire.PipelinePRRequestMax, 1 << 20}, // h.decode's MaxBytesReader
	} {
		if pair[0] != pair[1] {
			t.Errorf("%s: appwire %d, route %d", name, pair[0], pair[1])
		}
	}
	for _, sha := range []string{strings.Repeat("a", 40), strings.Repeat("A", 40), strings.Repeat("a", 39), strings.Repeat("g", 40), ""} {
		if appwire.ValidPipelinePRCommit(sha) != gitCommitSHA.MatchString(sha) {
			t.Errorf("commit %q disagrees", sha)
		}
	}
	for _, branch := range []string{
		"", "x", "codeaf/pipeline-1", "a/b/c", "-x", "x.", "a..b", "a@{b", "@", "HEAD", "a b", "a~b", "a^b", "a:b", "a?b", "a*b", "a[b", `a\b`, "a//b", "/a", "a/",
		".hidden", "a/.hidden", "x.lock", "a/x.lock", "a\x01b", "a\x7fb", "é", strings.Repeat("b", 200), strings.Repeat("b", 201),
	} {
		if got, want := appwire.ValidPipelinePRBranch(branch), validPipelineBranch(branch); got != want {
			t.Errorf("branch %q: appwire %v, route %v", branch, got, want)
		}
	}
	// Every stage the route can name is one the wire believes, and no more.
	known := map[string]bool{}
	for _, s := range appwire.PipelinePRStages {
		known[s] = true
	}
	for _, s := range []string{"request", "credential", "repository", "permission", "base", "file", "branch", "commit", "pull_request"} {
		if !known[s] {
			t.Errorf("stage %q is not believed by the wire", s)
		}
	}
	if len(known) != 9 {
		t.Errorf("the wire believes %d stages, the route names 9", len(known))
	}
}

// factory.issue_comment refuses, before anything is sent, exactly the bodies
// this route would refuse, and believes exactly the stages it names.
func TestAppwireIssueCommentBoundsAreTheRoutesOwn(t *testing.T) {
	if appwire.IssueCommentBodyMax != api.IssueCommentBodyMax {
		t.Fatalf("body: appwire %d, route %d", appwire.IssueCommentBodyMax, api.IssueCommentBodyMax)
	}
	if strings.Join(appwire.IssueCommentStages, ",") != strings.Join(api.IssueCommentStages, ",") {
		t.Fatalf("stages: appwire %v, route %v", appwire.IssueCommentStages, api.IssueCommentStages)
	}
	if appwire.IssueCommentOutcomeRefused != api.IssueCommentRefused || appwire.IssueCommentOutcomeUnknown != api.IssueCommentUnknown {
		t.Fatal("outcomes drifted")
	}
	for _, body := range []string{"", " \n", "x", "a\x00b", "bad \xff", strings.Repeat("x", api.IssueCommentBodyMax), strings.Repeat("x", api.IssueCommentBodyMax+1)} {
		_, err := appwire.CheckIssueComment(appwire.FactoryIssueCommentParams{Factory: "f", Repo: "a/b", Number: 1, Body: body})
		if (err == nil) != (issueCommentRefusal(body) == "") {
			t.Errorf("body %.20q: appwire %v, route %q", body, err, issueCommentRefusal(body))
		}
	}
}
