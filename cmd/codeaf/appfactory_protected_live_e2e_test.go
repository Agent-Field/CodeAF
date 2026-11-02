//go:build e2e

package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/factory/api"
)

// THE LIVE PROTECTED KEY. Against a real repository read through the signed-in
// gh, read-only: the factory reads its open issues into Backlog, the factory's
// own rules ask first on one path, and the queue says, per Backlog job, which
// protected paths its issue names. One job must name the path and one must
// name none. Nothing is written to GitHub and no model is asked.
//
// Opt-in: CODEAF_LIVE_FACTORY_SETUP=1, CODEAF_LIVE_FACTORY_REPO=<owner/name>
// and CODEAF_LIVE_PROTECTED=<path rule>, such as `touches auth/`, that some
// open issue names and some does not.
func TestLiveBacklogJobsSayWhichProtectedPathsTheyName(t *testing.T) {
	rule := strings.TrimSpace(os.Getenv("CODEAF_LIVE_PROTECTED"))
	if os.Getenv(liveSetupOptIn) != "1" || rule == "" {
		t.Skipf("opt-in live lane: set %s=1, %s=<owner/name> and CODEAF_LIVE_PROTECTED=<path rule>", liveSetupOptIn, liveSetupRepoEnv)
	}
	if !strings.Contains(liveSetupRepo, "/") || liveSetupGH == "" || liveSetupGHConfig == "" {
		t.Fatal("BLOCKED: no repository, gh or gh configuration; not faking a pass")
	}
	appHome, userHome, _ := useLocalHome(t)
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
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "this lane makes no model call", http.StatusServiceUnavailable)
	}))
	t.Cleanup(model.Close)
	t.Setenv("CODEAF_BASE_URL", model.URL)

	w := newFactoryWire(t, testFactoryHub(t))
	gh := &readOnlyGitHub{}
	w.backend.local.githubHTTP = gh.client()
	s := &localSetup{factoryWire: w, home: appHome, userHm: userHome}
	st := s.start()
	if v := s.setup("factory.setup_connect", map[string]any{"source": "gh"}); !v.GitHub.Connected {
		t.Fatalf("connect: %+v", v.GitHub)
	}
	s.setup("factory.setup_select", map[string]any{"repos": []string{liveSetupRepo}})
	deadline := time.Now().Add(liveSetupWait)
	for {
		if v := s.setup("factory.setup_read", map[string]any{}); repoOf(v, liveSetupRepo).Read == "done" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the repository's issues were never read")
		}
		time.Sleep(200 * time.Millisecond)
	}
	var rules appwire.FactoryRules
	empty := []string{}
	s.must("factory.rules_set", map[string]any{"factory": st.Factory, "rules": empty, "ask_first": []string{rule}, "never": empty}, &rules)

	var q api.Queue
	s.must("factory.queue", map[string]any{"factory": st.Factory}, &q)
	named, none, unjudged := 0, 0, 0
	for _, j := range q.Jobs {
		line, _ := json.Marshal(j.Protected)
		t.Logf("live protected: %s %s %s", j.Ref, j.State, line)
		switch {
		case j.Protected == nil:
			unjudged++
		case len(j.Protected.Paths) > 0:
			named++
		default:
			none++
		}
	}
	t.Logf("live protected summary: jobs %d, naming a protected path %d, naming none %d, not judged %d, GitHub writes refused %d",
		len(q.Jobs), named, none, unjudged, len(gh.refused))
	if named == 0 || none == 0 || unjudged != 0 || len(gh.refused) != 0 {
		t.Fatalf("named %d, none %d, unjudged %d, refused %v", named, none, unjudged, gh.refused)
	}
}
