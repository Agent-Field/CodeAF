//go:build e2e

package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/appwire"
	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
	"github.com/Agent-Field/codeaf/internal/home"
)

// ASK FOR DETAIL ON AN ISSUE, END TO END, on one real model. A real engine
// (the app backend behind the real wire) drafts the question in a real
// conversation on OpenRouter deepseek/deepseek-v4.1-flash with every role
// pinned to it, serves this machine's factory (factory.local_start) watching
// the repository, and posts the draft on a Backlog job's issue with
// factory.issue_comment. Then the same job is asked twice at once: one
// comment is posted and the other is refused as in flight.
//
// GITHUB IS FAKE BY DEFAULT: a transport that answers only the comment post.
// With CODEAF_LIVE_ISSUE_COMMENT_REPO=<owner/name> and
// CODEAF_LIVE_ISSUE_COMMENT_NUMBER=<n> it is REAL GitHub behind a guard that
// lets through only POST /repos/<repo>/issues/<n>/comments, at most twice, and
// refuses and counts everything else (the factory's own poll included). The
// token is the signed-in gh's, read once into the process and never printed.
// The issue is made and closed outside this test.
//
// Opt-in (CODEAF_LIVE_FACTORY=1), under a cent.
func TestLiveIssueCommentPostsOnceAndRefusesADoubleClick(t *testing.T) {
	if os.Getenv(liveFactoryOptIn) != "1" {
		t.Skipf("opt-in live lane: set %s=1 (spends under a cent on OpenRouter)", liveFactoryOptIn)
	}
	if liveFactoryKey == "" {
		t.Fatal("BLOCKED: no OpenRouter key by any road the product reads; not faking a pass")
	}
	repo, number := "acme/api", 12
	realGitHub := os.Getenv("CODEAF_LIVE_ISSUE_COMMENT_REPO") != ""
	token := "simulated-github-credential"
	if realGitHub {
		repo = os.Getenv("CODEAF_LIVE_ISSUE_COMMENT_REPO")
		n, err := strconv.Atoi(os.Getenv("CODEAF_LIVE_ISSUE_COMMENT_NUMBER"))
		if err != nil || n <= 0 {
			t.Fatal("BLOCKED: CODEAF_LIVE_ISSUE_COMMENT_NUMBER must name the issue")
		}
		number = n
		out, err := exec.Command("gh", "auth", "token").Output()
		if err != nil || strings.TrimSpace(string(out)) == "" {
			t.Fatal("BLOCKED: gh is not signed in")
		}
		token = strings.TrimSpace(string(out))
	}
	owner, name, _ := strings.Cut(repo, "/")
	commentsPath := "/repos/" + repo + "/issues/" + strconv.Itoa(number) + "/comments"

	root := resolvedTempDir(t)
	appHome := filepath.Join(root, "state")
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv(home.EnvVar, appHome)
	t.Setenv(config.ProfileDirEnv, v3Profile(t, map[string]any{
		config.KeyTaskModelsMode: "one", config.KeyTaskModelsOne: liveFactoryModel,
		"models.roles": "", "models.tiers.low": liveFactoryModel, "models.tiers.high": liveFactoryModel,
		"models.tiers.reflex": liveFactoryModel, "models.tiers.mastermind": liveFactoryModel,
		"models.tiers.worker": liveFactoryModel, "task.model": liveFactoryModel, "models.fallbacks": "",
	}))
	t.Setenv("OPENROUTER_API_KEY", liveFactoryKey)
	t.Setenv("GH_TOKEN", token)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv(factoryPostureEnv, "")
	recorder := newLiveRecorder(t, config.DefaultBaseURL)
	t.Setenv("CODEAF_BASE_URL", recorder.server.URL)
	workspace := filepath.Join(root, "work")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	freshEngineProcess(t)

	// ── GitHub: the comment post only, at most twice; the rest refused ───────
	var ghMu sync.Mutex
	var posted []string
	refused := 0
	upstream := http.DefaultTransport
	gh := &http.Client{Transport: roundTripper(func(r *http.Request) (*http.Response, error) {
		ghMu.Lock()
		ok := r.Method == http.MethodPost && r.URL.Host == "api.github.com" && r.URL.Path == commentsPath && len(posted) < 2
		if !ok {
			refused++
			ghMu.Unlock()
			return nil, errors.New("the live lane lets through only the comment post")
		}
		raw, _ := io.ReadAll(r.Body)
		posted = append(posted, string(raw))
		n := len(posted)
		ghMu.Unlock()
		if realGitHub {
			r.Body = io.NopCloser(strings.NewReader(string(raw)))
			return upstream.RoundTrip(r)
		}
		time.Sleep(500 * time.Millisecond) // as long as a real post takes, so a double click overlaps
		id := strconv.Itoa(424240 + n)
		body := `{"id":` + id + `,"html_url":"https://github.com/` + repo + `/issues/` + strconv.Itoa(number) + `#issuecomment-` + id + `","user":{"login":"owner"},"created_at":"2026-11-01T12:00:00Z"}`
		return &http.Response{StatusCode: 201, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})}

	rec := newAppRecorder()
	backend := newAppBackend(rec, appOpenInProcess)
	t.Cleanup(func() { backend.Close() })
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
	if _, err := backend.Send(agent.ID, "A GitHub issue is titled \"Login is flaky\" and says only \"Sometimes I cannot log in.\" "+
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
	if !done.OK || body == "" || len([]rune(body)) > factory.IssueCommentMost {
		t.Fatalf("the draft turn: %+v, %d bytes", done, len(body))
	}

	// ── 2. this machine's factory, watching the repository, its issue a job ──
	var st appwire.FactoryLocalStatus
	w.must("factory.local_start", map[string]any{"caller": "live"}, &st)
	floor := backend.local.host.floor
	if err := floor.SetRepos([]string{repo}); err != nil {
		t.Fatal(err)
	}
	job, err := floor.Create(factory.Item{Product: owner, Repo: name, Num: number, Kind: factory.KindIssue, Origin: factory.OriginForge,
		Title: "Login is flaky", State: factory.StateNew})
	if err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"factory": st.Factory, "job": job.ID, "body": body}

	// ── 3. ONE comment ───────────────────────────────────────────────────────
	var first appwire.FactoryIssueCommented
	w.must("factory.issue_comment", params, &first)
	wantURL := "https://github.com/" + repo + "/issues/" + strconv.Itoa(number) + "#issuecomment-" + first.Comment.ID
	if first.Job != job.ID || first.Comment.ID == "" || !strings.EqualFold(first.Comment.URL, wantURL) || first.Comment.At.IsZero() {
		t.Fatalf("receipt %+v", first)
	}

	// ── 4. a double click: two at once, one posts, one is in flight ──────────
	second := wireOver(t, backend)
	params["body"] = "Thanks. If it happens again, the time it happened would help too."
	type answer struct {
		code   string
		detail *appwire.FactoryRefusal
	}
	answers := make(chan answer, 2)
	for _, wire := range []*factoryWire{w, second} {
		go func(wire *factoryWire) {
			_, code, _ := wire.call("factory.issue_comment", params)
			var detail *appwire.FactoryRefusal
			wire.mu.Lock()
			for k := len(wire.lines) - 1; k >= 0 && code != ""; k-- {
				var fr struct {
					Error *struct {
						Code   string                  `json:"code"`
						Detail *appwire.FactoryRefusal `json:"detail"`
					} `json:"error"`
				}
				if json.Unmarshal([]byte(wire.lines[k]), &fr) == nil && fr.Error != nil && fr.Error.Code == code {
					detail = fr.Error.Detail
					break
				}
			}
			wire.mu.Unlock()
			answers <- answer{code, detail}
		}(wire)
	}
	a, b := <-answers, <-answers
	if a.code != "" {
		a, b = b, a
	}
	ghMu.Lock()
	posts, others := len(posted), refused
	ghMu.Unlock()
	writeLiveFactoryLog(t, map[string]any{
		"real_github": realGitHub, "repo": repo, "issue": number, "first_comment": first.Comment.URL,
		"double_click": []string{a.code, b.code}, "second_detail": b.detail, "posts": posts, "github_refused_locally": others,
		"request_models": recorder.requests, "response_models": recorder.replies, "calls": recorder.calls, "cost_usd": recorder.cost,
	})
	if a.code != "" || b.code != appwire.CodeConflict || b.detail == nil || b.detail.Reason != api.ReasonInFlight || b.detail.Outcome != api.CommentRefused || posts != 2 {
		t.Fatalf("double click: %+v / %+v after %d posts", a, b, posts)
	}

	// ── 5. the model, from the recorder ──────────────────────────────────────
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.calls == 0 || len(recorder.problems) > 0 || recorder.requests[liveFactoryModel] != recorder.calls {
		t.Fatalf("model requests %v problems %v", recorder.requests, recorder.problems)
	}
	if len(recorder.replies) != 1 || recorder.replies[liveFactoryModel] == 0 {
		t.Fatalf("response models %v are not exactly {%s}", recorder.replies, liveFactoryModel)
	}
	for path, data := range snapshotTree(t, root) {
		if strings.Contains(data, liveFactoryKey) || (realGitHub && strings.Contains(data, token)) {
			t.Fatalf("%s holds a secret", path)
		}
	}
}
