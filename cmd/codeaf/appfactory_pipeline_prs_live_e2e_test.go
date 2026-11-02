//go:build e2e

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/appwire"
)

// THE LIVE OPEN PIPELINE PULL REQUESTS READ. The app's own local factory,
// connected to the signed-in `gh` through the real setup door, answers
// factory.pipeline_prs over the app wire against a real GitHub repository.
//
// IT ONLY READS: the factory's GitHub client is the real transport behind
// readOnlyGitHub, which refuses and counts anything but a GET. No model is
// asked (the endpoint is a counting trap), because listing pull requests makes
// no model call. The answer is checked against gh's own list of the
// repository's open pull requests: everything listed must be open there.
//
// Opt-in: CODEAF_LIVE_FACTORY_PIPELINE_PRS=1 and CODEAF_LIVE_FACTORY_REPO=
// <owner/name>, a repository the signed-in account can read. The read costs
// at most 22 GitHub requests plus the setup door's own.
const livePRsOptIn = "CODEAF_LIVE_FACTORY_PIPELINE_PRS"

func TestLiveLocalServiceListsOpenPipelinePullRequests(t *testing.T) {
	if os.Getenv(livePRsOptIn) != "1" {
		t.Skipf("opt-in live lane: set %s=1 and %s=<owner/name>; it only reads that repository", livePRsOptIn, liveSetupRepoEnv)
	}
	if strings.Count(liveSetupRepo, "/") != 1 || liveSetupGH == "" || liveSetupGHConfig == "" {
		t.Fatalf("BLOCKED: set %s to owner/name and sign gh in; not faking a pass", liveSetupRepoEnv)
	}
	var open []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(liveGH(t, "pr", "list", "-R", liveSetupRepo, "--state", "open", "--limit", "100", "--json", "number"), &open); err != nil {
		t.Fatal(err)
	}
	isOpen := map[int]bool{}
	for _, pr := range open {
		isOpen[pr.Number] = true
	}

	useLocalHome(t)
	var dirs []string
	for _, tool := range []string{"git", "gh"} {
		if p, err := exec.LookPath(tool); err == nil {
			dirs = append(dirs, filepath.Dir(p))
		}
	}
	dirs = append(dirs, filepath.Dir(liveSetupGH))
	t.Setenv("PATH", strings.Join(dirs, string(os.PathListSeparator)))
	t.Setenv("GH_CONFIG_DIR", liveSetupGHConfig)
	t.Setenv("GH_PROMPT_DISABLED", "1")
	t.Setenv(factoryPostureEnv, "")
	t.Setenv("OPENROUTER_API_KEY", "")
	t.Setenv("OPENAI_API_KEY", "")
	var modelHits atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		modelHits.Add(1)
		http.Error(w, "this lane makes no model call", http.StatusServiceUnavailable)
	}))
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)

	w := newFactoryWire(t, testFactoryHub(t))
	gh := &readOnlyGitHub{}
	w.backend.local.githubHTTP = gh.client()
	s := &localSetup{factoryWire: w}
	st := s.start()
	if st.State != appwire.LocalRunning || st.Runner {
		t.Fatalf("start: %+v", st)
	}
	if v := s.setup("factory.setup_connect", map[string]any{"source": "gh"}); !v.GitHub.Connected {
		t.Fatalf("connect: %q", v.GitHub.Error)
	}
	if !strings.Contains(w.hello, `"`+appwire.FeatureFactoryPipelinePRs+`"`) {
		t.Fatal("hello does not announce factory.pipeline_prs")
	}
	gh.mu.Lock()
	readsBefore := gh.reads
	gh.mu.Unlock()

	var out appwire.FactoryPipelinePRs
	w.must("factory.pipeline_prs", map[string]any{"factory": st.Factory, "repo": liveSetupRepo}, &out)
	if out.Repo != liveSetupRepo || out.CheckedAt.IsZero() || out.PRs == nil {
		t.Fatalf("answer: %+v", out)
	}
	for _, pr := range out.PRs {
		if !isOpen[pr.Number] || pr.URL != "https://github.com/"+liveSetupRepo+"/pull/"+strconv.Itoa(pr.Number) {
			t.Fatalf("listed a pull request gh does not call open: %+v", pr)
		}
		t.Logf("open pipeline pull request #%d factory_branch=%v draft=%v", pr.Number, pr.FactoryBranch, pr.Draft)
	}
	gh.mu.Lock()
	reads, refused := gh.reads-readsBefore, gh.refused
	gh.mu.Unlock()
	if len(refused) != 0 || reads > 22 || modelHits.Load() != 0 {
		t.Fatalf("reads %d refused %v model hits %d", reads, refused, modelHits.Load())
	}
	t.Logf("factory.pipeline_prs: %d listed of %d open, complete=%v, %d GitHub GETs, 0 writes, 0 model calls", len(out.PRs), len(open), out.Complete, reads)
}
