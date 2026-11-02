package serve

import (
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// READ BY REF: ?ref=<pull request number> or ?ref=<40-hex head commit> reads
// the file at an open pipeline pull request's head, and base_sha is that head.

func TestPipelineEditSourceByPullRequestNumberReadsItsHead(t *testing.T) {
	g := newPRGitHub(t)
	g.file = []byte(editSourceFile)
	g.pull = openPull()
	out := readSource(t, sourceRequest(soloPR(t, g, factory.Seam{}), "", "GET", sourcePath+"?ref=820"))
	if out.BaseSHA != prPullHead || out.Ref != "820" || out.PR != 820 || out.State != api.PipelineEditFile || out.Text != editSourceFile || out.DefaultBranch != "main" {
		t.Fatalf("%+v", out)
	}
	want := []string{
		"GET /repos/acme/api",
		"GET /repos/acme/api/pulls/820",
		"GET /repos/acme/api/git/ref/heads/codeaf/pipeline-abc",
		"GET /repos/acme/api/pulls/820/files",
		"GET /repos/acme/api/contents/.codeaf/factory.yaml",
	}
	if strings.Join(g.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(g.calls, "\n"))
	}
}

func TestPipelineEditSourceByHeadCommitFindsItsOpenPullRequest(t *testing.T) {
	g := newPRGitHub(t)
	g.pull = openPull()
	out := readSource(t, sourceRequest(soloPR(t, g, factory.Seam{}), "", "GET", sourcePath+"?ref="+strings.ToUpper(prPullHead)))
	if out.BaseSHA != prPullHead || out.Ref != prPullHead || out.PR != 820 || out.State != api.PipelineEditMissing {
		t.Fatalf("%+v", out)
	}
	if g.calls[1] != "GET /repos/acme/api/commits/"+prPullHead+"/pulls" {
		t.Fatalf("calls %v", g.calls)
	}
}

// A number is read at the branch's own tip, which GitHub's pull request read
// can lag behind after a push.
func TestPipelineEditSourceByNumberTakesTheBranchTip(t *testing.T) {
	const fresh = "6666666666666666666666666666666666666666"
	g := newPRGitHub(t)
	g.file = []byte(editSourceFile)
	g.pull = openPull()
	g.pull.refSHA = fresh
	if out := readSource(t, sourceRequest(soloPR(t, g, factory.Seam{}), "", "GET", sourcePath+"?ref=820")); out.BaseSHA != fresh {
		t.Fatalf("%+v", out)
	}
}

func TestPipelineEditSourceRefusesARefThatIsNotAnOpenPipelinePullRequest(t *testing.T) {
	for name, c := range map[string]struct {
		set func(*prPull)
		ref string
	}{
		"closed":           {func(p *prPull) { p.state = "closed" }, "820"},
		"from a fork":      {func(p *prPull) { p.headRepo = "mallory/api" }, "820"},
		"other base":       {func(p *prPull) { p.baseRef = "release" }, "820"},
		"other repository": {func(p *prPull) { p.baseRepo = "other/api" }, "820"},
		"not pipeline":     {func(p *prPull) { p.files = []string{factory.PipelineFile, "main.go"} }, "820"},
		"no such number":   {func(*prPull) {}, "821"},
		"no such head":     {func(*prPull) {}, strings.Repeat("7", 40)},
		"head moved":       {func(p *prPull) { p.refSHA = strings.Repeat("6", 40) }, prPullHead},
		"closed by head":   {func(p *prPull) { p.state = "closed" }, prPullHead},
	} {
		t.Run(name, func(t *testing.T) {
			g := newPRGitHub(t)
			g.file = []byte(editSourceFile)
			g.pull = openPull()
			c.set(g.pull)
			w := sourceRequest(soloPR(t, g, factory.Seam{}), "", "GET", sourcePath+"?ref="+c.ref)
			f := decodeSourceFailure(t, w)
			if w.Code < 400 || f.Stage != api.PipelineEditStageHead || len(g.writes()) != 0 {
				t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
			}
			for _, call := range g.calls {
				if strings.Contains(call, "/contents/") {
					t.Fatalf("read the file of a refused ref: %v", g.calls)
				}
			}
		})
	}
}

func TestPipelineEditSourceRefShapesAreRefusedBeforeGitHub(t *testing.T) {
	for _, q := range []string{"?ref=main", "?ref=0", "?ref=042", "?ref=-1", "?ref=%2B1", "?ref=8201234567890", "?ref=abc", "?ref=" + strings.Repeat("a", 39),
		"?ref=820&ref=821", "?ref=820&x=1", "?ref=", "?x=820"} {
		g := newPRGitHub(t)
		g.pull = openPull()
		if w := sourceRequest(soloPR(t, g, factory.Seam{}), "", "GET", sourcePath+q); w.Code != 422 || len(g.calls) != 0 {
			t.Fatalf("%s: %d %s %v", q, w.Code, w.Body, g.calls)
		}
	}
}
