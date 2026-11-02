package serve

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

const (
	prPullHead = "4444444444444444444444444444444444444444"
	prPullBase = "5555555555555555555555555555555555555555"
)

// openPull is an open pull request from a branch of acme/api into main.
func openPull() *prPull {
	return &prPull{number: 820, state: "open", headRef: "codeaf/pipeline-abc", headSHA: prPullHead, headRepo: "acme/api",
		baseRef: "main", baseSHA: prPullBase, baseRepo: "acme/api"}
}

func TestPipelinePRUpdateCommitsOntoTheOpenPullRequestsHeadBranch(t *testing.T) {
	g := newPRGitHub(t)
	g.file = []byte("old: file\n")
	g.pull = openPull()
	h := soloPR(t, g, factory.Seam{})
	w := prRequest(h, t.Context(), "", prBody(`,"pr":820,"expected_head_sha":"`+prPullHead+`"`))
	if w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	var out api.PipelinePRResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Outcome != api.PipelinePRUpdated || out.PR == nil || out.PR.Number != 820 || out.PR.URL != "https://github.com/acme/api/pull/820" ||
		out.PR.Branch != "codeaf/pipeline-abc" || out.PR.Base != "main" || out.PR.BaseSHA != prPullBase || out.PR.HeadSHA != prHead || out.BaseSHA != prPullBase {
		t.Fatalf("response %s", w.Body)
	}
	want := []string{
		"GET /repos/acme/api",
		"GET /repos/acme/api/pulls/820",
		"GET /repos/acme/api/git/ref/heads/codeaf/pipeline-abc",
		"GET /repos/acme/api/contents/.codeaf/factory.yaml",
		"PUT /repos/acme/api/contents/.codeaf/factory.yaml",
	}
	if strings.Join(g.calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(g.calls, "\n"))
	}
	put := g.puts[0]
	if len(put) != 4 || put["branch"] != "codeaf/pipeline-abc" || put["sha"] != prBlob || put["message"] != "Pipeline: add proof" ||
		put["content"] != base64.StdEncoding.EncodeToString(encodedPipeline(t)) {
		t.Fatalf("contents put %+v", put)
	}
	g.noDeleteEver()
	assertClean(t, w)
}

func TestPipelinePRUpdateUnchangedWhenTheHeadHoldsTheseBytes(t *testing.T) {
	g := newPRGitHub(t)
	g.file = encodedPipeline(t)
	g.pull = openPull()
	w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(`,"pr":820`))
	var out api.PipelinePRResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Outcome != api.PipelinePRUnchanged ||
		out.PR == nil || out.PR.Number != 820 || out.PR.HeadSHA != prPullHead || out.PR.Branch != "codeaf/pipeline-abc" || out.BaseSHA != prPullBase {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if len(g.writes()) != 0 {
		t.Fatalf("writes %v", g.writes())
	}
}

func TestPipelinePRUpdateCreatesTheFileOnTheHeadWhenItIsMissing(t *testing.T) {
	g := newPRGitHub(t)
	g.pull = openPull()
	w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(`,"pr":820`))
	if w.Code != 200 || len(g.puts) != 1 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if _, has := g.puts[0]["sha"]; has {
		t.Fatalf("a new file must not name a blob: %+v", g.puts[0])
	}
}

// A pull request the flow must not write to is refused at stage "head", and
// nothing is written.
func TestPipelinePRUpdateRefusesAPullRequestItMustNotWriteTo(t *testing.T) {
	for name, c := range map[string]struct {
		set    func(*prPull)
		body   string
		status int
	}{
		"closed":       {func(p *prPull) { p.state = "closed" }, `,"pr":820`, 409},
		"from a fork":  {func(p *prPull) { p.headRepo = "mallory/api" }, `,"pr":820`, 409},
		"deleted fork": {func(p *prPull) { p.headRepo = "" }, `,"pr":820`, 409},
		"other base":   {func(p *prPull) { p.baseRef = "release" }, `,"pr":820`, 409},
		"other repo":   {func(p *prPull) { p.baseRepo = "other/api" }, `,"pr":820`, 409},
		"head moved":   {func(*prPull) {}, `,"pr":820,"expected_head_sha":"` + strings.Repeat("a", 40) + `"`, 409},
		"bad head":     {func(p *prPull) { p.refSHA = "main" }, `,"pr":820`, 502},
		"head is base": {func(p *prPull) { p.headRef = "main" }, `,"pr":820`, 409},
	} {
		t.Run(name, func(t *testing.T) {
			g := newPRGitHub(t)
			g.file = []byte("old: file\n")
			g.pull = openPull()
			c.set(g.pull)
			w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(c.body))
			f := decodeFailure(t, w)
			if w.Code != c.status || f.Stage != "head" || f.BranchLeft || len(g.writes()) != 0 {
				t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
			}
			if c.status == 409 && f.Outcome != api.PipelinePRRefused {
				t.Fatalf("outcome %+v", f)
			}
			assertClean(t, w)
		})
	}
	t.Run("missing", func(t *testing.T) {
		g := newPRGitHub(t)
		w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(`,"pr":821`))
		f := decodeFailure(t, w)
		if w.Code != 403 || f.Stage != "head" || f.Outcome != api.PipelinePRRefused || len(g.writes()) != 0 {
			t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
		}
	})
}

// A failed commit onto an existing pull request never leaves a branch: the
// branch was the person's before the call. A 4xx is not_opened, anything else
// unknown; the write is never repeated.
func TestPipelinePRUpdateCommitFailuresLeaveNoBranchAndAreNotRepeated(t *testing.T) {
	for name, c := range map[string]struct {
		code, drop int
		want       int
		outcome    string
	}{
		"409 file moved": {409, 0, 409, api.PipelinePRNotOpened},
		"403":            {403, 0, 403, api.PipelinePRNotOpened},
		"500":            {500, 0, 502, api.PipelinePRUnknown},
		"drop":           {0, 1, 502, api.PipelinePRUnknown},
	} {
		t.Run(name, func(t *testing.T) {
			g := newPRGitHub(t)
			g.file = []byte("old: file\n")
			g.pull = openPull()
			if c.drop == 1 {
				g.drop["commit"] = true
			} else {
				g.status["commit"] = c.code
			}
			w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(`,"pr":820`))
			f := decodeFailure(t, w)
			if w.Code != c.want || f.Outcome != c.outcome || f.Stage != "commit" || f.BranchLeft || f.Branch != "codeaf/pipeline-abc" || len(g.writes()) != 1 {
				t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
			}
			g.noDeleteEver()
			assertClean(t, w)
		})
	}
}

// Fields that only make sense for a NEW pull request are refused with pr, and
// a pr that is not a positive number or a head that is not a commit is refused,
// all before any GitHub call.
func TestPipelinePRUpdateRefusesMixedOrBadFieldsBeforeAnyGitHubCall(t *testing.T) {
	for name, body := range map[string]string{
		"branch":        prBody(`,"pr":820,"branch":"codeaf/x"`),
		"body":          prBody(`,"pr":820,"body":"why"`),
		"base":          prBody(`,"pr":820,"expected_base_sha":"` + prTip + `"`),
		"zero":          prBody(`,"pr":0,"expected_head_sha":"` + prPullHead + `"`),
		"negative":      prBody(`,"pr":-1`),
		"head no pr":    prBody(`,"expected_head_sha":"` + prPullHead + `"`),
		"short head":    prBody(`,"pr":820,"expected_head_sha":"abc"`),
		"string number": prBody(`,"pr":"820"`),
	} {
		t.Run(name, func(t *testing.T) {
			g := newPRGitHub(t)
			g.pull = openPull()
			w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", body)
			if w.Code != 422 || len(g.calls) != 0 {
				t.Fatalf("%d %s %v", w.Code, w.Body, g.calls)
			}
		})
	}
}

// GITHUB'S PULL REQUEST READ LAGS BEHIND A PUSH for a few seconds: the head is
// the head branch's own ref, so a caller pinning the commit it was just given
// is not refused, and the file is read at that commit.
func TestPipelinePRUpdateTakesTheHeadFromTheBranchRefNotTheLaggingPullRead(t *testing.T) {
	const fresh = "6666666666666666666666666666666666666666"
	g := newPRGitHub(t)
	g.file = []byte("old: file\n")
	g.pull = openPull()
	g.pull.refSHA = fresh
	w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(`,"pr":820,"expected_head_sha":"`+fresh+`"`))
	var out api.PipelinePRResponse
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Outcome != api.PipelinePRUpdated || out.PR.HeadSHA != prHead {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	// The pull read's stale head is now the moved one.
	g = newPRGitHub(t)
	g.file = []byte("old: file\n")
	g.pull = openPull()
	g.pull.refSHA = fresh
	w = prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(`,"pr":820,"expected_head_sha":"`+prPullHead+`"`))
	if f := decodeFailure(t, w); w.Code != 409 || f.Stage != "head" || len(g.writes()) != 0 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
