package serve

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
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
