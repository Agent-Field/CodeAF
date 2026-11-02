//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/home"
)

// ASK FOR DETAIL ON AN ISSUE, END TO END, on one real model and a FAKE GitHub.
// A real engine (the app backend behind the real wire) drafts the question in
// a real conversation on OpenRouter deepseek/deepseek-v4.1-flash, every role
// pinned to it, then serves this machine's factory (factory.local_start),
// which watches acme/api, and posts the draft with factory.issue_comment. The
// factory's GitHub is a transport that answers only the issue read and the
// comment post and records both: NOTHING REACHES REAL GITHUB.
//
// The model is checked twice: every request and the model each response names
// pass through [liveRecorder], and the conversation's own journal on disk must
// name the pinned model and no other.
//
// Opt-in (CODEAF_LIVE_FACTORY=1), under a cent.
func TestLiveIssueCommentPostsADraftedQuestionOnce(t *testing.T) {
	if os.Getenv(liveFactoryOptIn) != "1" {
		t.Skipf("opt-in live lane: set %s=1 (spends under a cent on OpenRouter)", liveFactoryOptIn)
	}
	if liveFactoryKey == "" {
		t.Fatal("BLOCKED: no OpenRouter key by any road the product reads; not faking a pass")
	}
	root := resolvedTempDir(t)
	appHome := filepath.Join(root, "state")
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, appHome)
	profile := v3Profile(t, map[string]any{
		config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: liveFactoryModel,
		"models.roles": "", "models.tiers.low": liveFactoryModel, "models.tiers.high": liveFactoryModel,
		"models.tiers.reflex": liveFactoryModel, "models.tiers.mastermind": liveFactoryModel,
		"models.tiers.worker": liveFactoryModel, "task.model": liveFactoryModel, "models.fallbacks": "",
	})
	t.Setenv(config.ProfileDirEnv, profile)
	t.Setenv("OPENROUTER_API_KEY", liveFactoryKey)
	t.Setenv("GH_TOKEN", "simulated-github-credential")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv(factoryPostureEnv, "")
	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	t.Setenv("CODEAF_BASE_URL", recorder.server.URL)
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	freshEngineProcess(t)

	// ── the fake GitHub: one issue, one comment, everything recorded ─────────
	var ghMu sync.Mutex
	var ghCalls, posted []string
	gh := &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		ghMu.Lock()
		defer ghMu.Unlock()
		ghCalls = append(ghCalls, r.Method+" "+r.URL.Path)
		status, body := 404, `{"message":"Not Found"}`
		if r.Header.Get("Authorization") != "Bearer simulated-github-credential" {
			t.Errorf("GitHub was asked without the caller's credential: %s %s", r.Method, r.URL.Path)
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/repos/acme/api/issues/7":
			status, body = 200, `{"number":7,"title":"Login is flaky","body":"Sometimes I cannot log in."}`
		case r.Method == http.MethodPost && r.URL.Path == "/repos/acme/api/issues/7/comments":
			raw, _ := io.ReadAll(r.Body)
			posted = append(posted, string(raw))
			status, body = 201, `{"id":424242,"html_url":"https://github.com/acme/api/issues/7#issuecomment-424242","created_at":"2026-11-01T12:00:00Z"}`
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}

	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	backend.local.githubHTTP = gh
	backend.local.rules = func(context.Context, string, string) (pipeline.File, error) { return pipeline.File{}, os.ErrNotExist }
	backend.local.seamHook = func(seam factory.Seam) factory.Seam {
		seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "owner"}, nil }
		return seam
	}
	w := wireOver(t, backend)
	if !strings.Contains(w.hello, appwire.FeatureFactoryIssueComment) {
		t.Fatalf("hello does not announce %s", appwire.FeatureFactoryIssueComment)
	}

	// ── 1. a real model drafts the question ──────────────────────────────────
	agent, err := mustCreate(t, backend, appwire.CreateAsk{Workspace: workspace, Model: liveFactoryModel})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := backend.Send(agent.ID, "Issue #7 on acme/api is titled \"Login is flaky\" and says only \"Sometimes I cannot log in.\" "+
		"Write a short, polite GitHub comment (at most three sentences) asking the author for the details needed to reproduce it. "+
		"Reply with only the comment text. Do not use tools or start tasks.", ""); err != nil {
		t.Fatal(err)
	}
	i := rec.waitFor(t, 3*time.Minute, func(event string, data any) bool {
		done, ok := data.(appwire.TurnDone)
		return event == appwire.EventTurnDone && ok && done.Agent == agent.ID
	})
	var draft strings.Builder
	rec.mu.Lock()
	done := rec.data[i].(appwire.TurnDone)
	for k := 0; k < i; k++ {
		if d, ok := rec.data[k].(appwire.TurnDelta); ok && d.Agent == agent.ID && d.Kind == "text" {
			draft.WriteString(d.Text)
		}
	}
	rec.mu.Unlock()
	body := strings.TrimSpace(draft.String())
	if !done.OK || body == "" || len(body) > appwire.IssueCommentBodyMax {
		t.Fatalf("the draft turn: %+v, %d bytes", done, len(body))
	}

	// ── 2. this machine's factory, watching acme/api ─────────────────────────
	var st appwire.FactoryLocalStatus
	w.must("factory.local_start", map[string]any{"caller": "live"}, &st)
	if st.State != appwire.LocalRunning || st.Factory == "" {
		t.Fatalf("start: %+v", st)
	}
	if err := backend.local.host.floor.SetRepos([]string{"acme/api"}); err != nil {
		t.Fatal(err)
	}

	// ── 3. ONE comment, through the wire ─────────────────────────────────────
	var receipt appwire.FactoryIssueCommentDone
	w.must("factory.issue_comment", appwire.FactoryIssueCommentParams{Factory: st.Factory, Repo: "acme/api", Number: 7, Body: body}, &receipt)
	if receipt.Factory != st.Factory || receipt.Repo != "acme/api" || receipt.Number != 7 || receipt.Comment.ID != 424242 ||
		receipt.Comment.URL != "https://github.com/acme/api/issues/7#issuecomment-424242" || !receipt.Comment.CreatedAt.Equal(time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("receipt %+v", receipt)
	}
	want, _ := json.Marshal(map[string]string{"body": body})
	ghMu.Lock()
	if len(posted) != 1 || posted[0] != string(want) {
		t.Fatalf("posted %d comments; first %q", len(posted), posted)
	}
	calls := append([]string(nil), ghCalls...)
	ghMu.Unlock()

	// ── 4. the model, from the recorder and from the conversation's journal ──
	journalModels := map[string]int{}
	modelField := regexp.MustCompile(`"model":"([^"]+)"`)
	for path, data := range snapshotTree(t, appHome) {
		if strings.Contains(path, agent.ID) || strings.Contains(filepath.Base(filepath.Dir(path)), agent.ID) {
			for _, m := range modelField.FindAllStringSubmatch(data, -1) {
				journalModels[m[1]]++
			}
		}
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	writeLiveFactoryLog(t, map[string]any{
		"requested_model": liveFactoryModel, "request_models": recorder.requests, "response_models": recorder.replies,
		"journal_models": journalModels, "calls": recorder.calls, "cost_usd": recorder.cost, "problems": recorder.problems,
		"github_calls": calls, "comment_bytes": len(body), "receipt": receipt,
	})
	if recorder.calls == 0 || len(recorder.problems) > 0 || recorder.requests[liveFactoryModel] != recorder.calls {
		t.Fatalf("model requests %v problems %v", recorder.requests, recorder.problems)
	}
	if len(recorder.replies) != 1 || recorder.replies[liveFactoryModel] == 0 {
		t.Fatalf("response models %v are not exactly {%s}", recorder.replies, liveFactoryModel)
	}
	if len(journalModels) != 1 || journalModels[liveFactoryModel] == 0 {
		t.Fatalf("the conversation's journal names models %v, not exactly {%s}", journalModels, liveFactoryModel)
	}
	for path, data := range snapshotTree(t, root) {
		if strings.Contains(data, liveFactoryKey) {
			t.Fatalf("%s holds the key", path)
		}
	}
}
