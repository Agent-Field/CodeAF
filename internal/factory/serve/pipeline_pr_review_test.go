package serve

import (
	"encoding/json"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

func TestPipelinePRPreservesAnEditableNewBranchName(t *testing.T) {
	g := newPRGitHub(t)
	w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(`,"branch":"feature/Review-Pipeline"`))
	var out api.PipelinePRResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if w.Code != 201 || out.PR == nil || out.PR.Branch != "feature/Review-Pipeline" || g.posts["refs"]["ref"] != "refs/heads/feature/Review-Pipeline" {
		t.Fatalf("branch choice was changed or refused: %d %s", w.Code, w.Body)
	}
}

func TestPipelinePRNeverRecreatesTheDefaultBranchAfterItDisappears(t *testing.T) {
	g := newPRGitHub(t)
	g.defaultBranch = "codeaf/pipeline-protected"
	g.loseDefault = true
	w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(`,"branch":"codeaf/pipeline-protected"`))
	f := decodeFailure(t, w)
	if w.Code != 409 || f.Outcome != api.PipelinePRRefused || f.Stage != "branch" || len(g.writes()) != 0 {
		t.Fatalf("default branch was not protected: %d %+v writes=%v", w.Code, f, g.writes())
	}
}

func TestPipelinePRStopsAfterAnUnverifiableWriteResult(t *testing.T) {
	for _, stage := range []string{"branch", "commit"} {
		t.Run(stage, func(t *testing.T) {
			g := newPRGitHub(t)
			g.body[stage] = `{}`
			w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(`,"branch":"codeaf/pipeline-review"`))
			f := decodeFailure(t, w)
			wantWrites := 1
			if stage == "commit" {
				wantWrites = 2
			}
			if w.Code != 502 || f.Outcome != api.PipelinePRUnknown || f.Stage != stage || !f.BranchLeft || len(g.writes()) != wantWrites {
				t.Fatalf("continued after an unverifiable write: %d %+v writes=%v", w.Code, f, g.writes())
			}
			g.noDeleteEver()
		})
	}
}

func TestPipelinePRRetainsTheObservedStatusForAnUnreadableResponse(t *testing.T) {
	g := newPRGitHub(t)
	g.body["pull_request"] = `<html>`
	w := prRequest(soloPR(t, g, factory.Seam{}), t.Context(), "", prBody(`,"branch":"codeaf/pipeline-status"`))
	f := decodeFailure(t, w)
	if w.Code != 502 || f.Outcome != api.PipelinePRUnknown || f.GitHubStatus != 201 || !f.BranchLeft {
		t.Fatalf("lost the observed GitHub status: %d %+v", w.Code, f)
	}
}
