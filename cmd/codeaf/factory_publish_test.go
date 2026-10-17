package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/config"
	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/client"
	factorygithub "github.com/Agent-Field/codeaf/internal/factory/github"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	factoryrun "github.com/Agent-Field/codeaf/internal/factory/run"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
	factoryworker "github.com/Agent-Field/codeaf/internal/factory/worker"
	forge "github.com/Agent-Field/codeaf/internal/praf/github"
)

func publishGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	args = append([]string{"-C", dir, "-c", "user.name=test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// This drives HTTP approval, its durable replay, the real lease and worker,
// the binary's publisher and a fake forge. The server has no publishing client
// and the runner watches no repositories, in both team and solo mode.
func TestFactoryAssigneesRunnerPublishesTeamAndSolo(t *testing.T) {
	for _, mode := range []string{api.FactoryTeam, api.FactorySolo} {
		for _, action := range []string{"new_pr", "existing_pr", "no_diff", "review", "forbidden"} {
			t.Run(mode+"/"+action, func(t *testing.T) {
				keys := config.NewAppCredentials()
				keys.Set("github", "assignee-token")
				defer config.UseAppCredentials(keys)()
				t.Setenv("GH_TOKEN", "server-token")
				g := newRunRig(t)
				publishGit(t, g.workspace, "branch", "-M", "main")
				bare := filepath.Join(t.TempDir(), "origin.git")
				publishGit(t, g.workspace, "init", "--bare", "-q", bare)
				publishGit(t, g.workspace, "remote", "add", "origin", bare)
				publishGit(t, g.workspace, "push", "-q", "origin", "main")
				publishGit(t, g.workspace, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
				if action != "no_diff" {
					publishGit(t, g.workspace, "commit", "--allow-empty", "-q", "-m", "approved work")
				}
				head := publishGit(t, g.workspace, "rev-parse", "HEAD")
				var mu sync.Mutex
				var writes []string
				var bodies []string
				remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Header.Get("Authorization") != "Bearer assignee-token" {
						t.Error("forge did not receive the assignee's credential")
						w.WriteHeader(401)
						return
					}
					if r.Method != "GET" {
						var body struct{ Body string }
						if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
							t.Error(err)
						}
						mu.Lock()
						writes = append(writes, r.Method+" "+r.URL.Path)
						bodies = append(bodies, body.Body)
						mu.Unlock()
					}
					switch r.Method + " " + r.URL.Path {
					case "GET /user":
						fmt.Fprint(w, `{"login":"assignee"}`)
					case "GET /repos/acme/api/pulls":
						if action == "existing_pr" {
							fmt.Fprint(w, `[{"number":7}]`)
						} else {
							fmt.Fprint(w, `[]`)
						}
					case "GET /repos/acme/api":
						fmt.Fprint(w, `{"default_branch":"main"}`)
					case "POST /repos/acme/api/pulls", "PATCH /repos/acme/api/pulls/7":
						if action == "forbidden" {
							w.WriteHeader(403)
							fmt.Fprint(w, `{"message":"Resource not accessible"}`)
							return
						}
						fmt.Fprint(w, `{"number":7,"html_url":"https://github.com/acme/api/pull/7"}`)
					case "POST /repos/acme/api/issues/12/comments":
						fmt.Fprint(w, `{"id":9,"html_url":"https://github.com/acme/api/issues/12#issuecomment-9"}`)
					default:
						t.Errorf("unexpected forge request %s %s", r.Method, r.URL)
						w.WriteHeader(404)
					}
				}))
				defer remote.Close()
				kind := factory.KindIssue
				if action == "review" {
					kind = factory.KindPR
				}
				it, err := g.st.Create(factory.Item{Repo: "acme/api", Num: 12, Kind: kind, URL: "https://github.com/acme/api/pull/12", Title: "fix", Assignee: "assignee", State: factory.StateLanded, HeadCommit: head, WorktreeMachine: "assignee-laptop", Proof: []factory.Claim{{Text: "works", OK: true}}, Stream: &factory.Stream{}})
				if err != nil {
					t.Fatal(err)
				}
				history, err := record.Open(g.st, record.Options{})
				if err != nil {
					t.Fatal(err)
				}
				var clock atomic.Int64
				clock.Store(time.Now().UnixNano())
				desk, err := lease.Open(g.st, lease.Options{})
				if err != nil {
					t.Fatal(err)
				}
				seam := factory.LocalSeam(g.st, time.Now(), factory.WithRunner(lease.NewDoors(desk)))
				seam.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "assignee"}, nil }
				h := serve.New(seam, serve.Config{Mode: mode, OperatorLogin: "server-owner", Store: g.st, Record: history, PullWait: time.Millisecond, Now: func() time.Time { return time.Unix(0, clock.Load()) }})
				server := httptest.NewServer(h)
				t.Cleanup(func() { server.Close(); h.Close() })
				approve := func() *httptest.ResponseRecorder {
					r := httptest.NewRequest("POST", fmt.Sprintf("/v1/jobs/%d/approve", it.ID), strings.NewReader(`{"commit":"`+head+`"}`))
					r.RemoteAddr = "127.0.0.1:1234"
					r.Header.Set("Idempotency-Key", "same-approval")
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					return w
				}
				first, replay := approve(), approve()
				if first.Code != 200 || replay.Code != first.Code || first.Body.String() != replay.Body.String() {
					t.Fatalf("approval/replay = %d %s / %d %s", first.Code, first.Body, replay.Code, replay.Body)
				}
				clock.Add(int64(factory.ApprovalUndo))
				r := httptest.NewRequest("GET", "/v1/queue", nil)
				r.RemoteAddr = "127.0.0.1:1234"
				h.ServeHTTP(httptest.NewRecorder(), r)
				trailer, err := history.TrailerFor(context.Background(), it.ID, head)
				if err != nil {
					t.Fatal(err)
				}
				mirror, err := store.Open(t.TempDir())
				if err != nil {
					t.Fatal(err)
				}
				opts := buildFactoryRunner(mirror, g.st, g.workspace, t.TempDir(), nil, nil).Opts()
				if opts.Publisher == nil {
					t.Fatal("binary runner has no assignee publisher")
				}
				publisher := factoryPublisherWith(t.TempDir(), func(token string) factorygithub.API {
					return forge.NewClient(token).WithBaseURL(remote.URL).WithSleep(func(context.Context, time.Duration) error { return nil })
				})
				opts.Publisher = func(ctx context.Context, it factory.Item) (factoryrun.PublishTarget, error) {
					target, err := publisher(ctx, it)
					// Reconnecting after resolution cannot change this attempt's
					// push or forge account, even when the app keeps a new token.
					keys.Set("github", "reconnected-account-token")
					return target, err
				}
				opts.Workdir = func(factory.Item) (string, error) { return g.workspace, nil }
				opts.Source = func(string) factory.Source { t.Error("publish used the server's watched source"); return nil }
				opts.Push = func(string, string) error { t.Error("publish used the server's push credential"); return nil }
				runner := factoryrun.New(opts)
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan struct{})
				go func() {
					defer close(done)
					var credential string
					factoryworker.Run(ctx, factoryworker.Options{
						Dial:   func(int) *client.WorkerClient { return client.Open(server.URL, "").Worker(credential) },
						Keep:   func(_ int, w *client.WorkerClient) { credential = w.Credential() },
						Attach: api.WorkerAttach{Machine: "assignee-laptop", Name: "assignee runner", Handle: "assignee"},
						Runner: runner, Mirror: mirror, Slots: 1, ReportEvery: time.Millisecond, BeatEvery: time.Millisecond,
					})
				}()
				t.Cleanup(func() { cancel(); <-done })
				var saved factory.Item
				for deadline := time.Now().Add(10 * time.Second); ; time.Sleep(time.Millisecond) {
					saved, _ = g.st.Get(it.ID)
					if saved.State == factory.StateShipped || (saved.PublishStatus != nil && saved.PublishStatus.State == "failed") {
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("publish never finished: %+v", saved)
					}
				}
				mu.Lock()
				defer mu.Unlock()
				if len(writes) != 1 || !strings.HasSuffix(bodies[0], trailer) {
					t.Fatalf("writes %v bodies %v", writes, bodies)
				}
				wantWrite := "POST /repos/acme/api/pulls"
				if action == "existing_pr" {
					wantWrite = "PATCH /repos/acme/api/pulls/7"
				} else if action == "no_diff" || action == "review" {
					wantWrite = "POST /repos/acme/api/issues/12/comments"
				}
				if writes[0] != wantWrite {
					t.Fatalf("write = %s, want %s", writes[0], wantWrite)
				}
				if action == "forbidden" {
					if saved.PublishStatus.Code != "pr_create_forbidden" || factory.NeedsKind(saved) != factory.NeedsPublishFailed {
						t.Fatalf("publish failure = %+v", saved.PublishStatus)
					}
					return
				}
				if saved.PublishStatus == nil || saved.PublishStatus.State != "opened" || (saved.PullRequest == nil && saved.CommentURL == "") {
					t.Fatalf("publish receipt = %+v", saved)
				}
				if action == "new_pr" || action == "existing_pr" {
					branch := "factory/12-fix"
					if pushed := publishGit(t, bare, "rev-parse", "refs/heads/"+branch); pushed != head {
						t.Fatalf("pushed %s instead of approved %s", pushed, head)
					}
				} else if pushed := publishGit(t, bare, "branch", "--list", "factory/*"); pushed != "" {
					t.Fatalf("comment pushed a branch: %s", pushed)
				}
				events, _ := history.Since(context.Background(), time.Time{})
				approved, posted := 0, 0
				for _, event := range events {
					if event.Kind == record.Approved {
						approved++
					}
					if event.Kind == record.Posted && event.Handle == "assignee" {
						posted++
					}
				}
				if approved != 1 || posted != 1 {
					t.Fatalf("approvals %d, assignee posts %d: %v", approved, posted, events)
				}
			})
		}
	}
}

func TestFactoryPublisherRefusesAnotherPersonsGitHubAccount(t *testing.T) {
	keys := config.NewAppCredentials()
	keys.Set("github", "wrong-person-token")
	defer config.UseAppCredentials(keys)()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/user" {
			t.Errorf("wrong account made an outward action: %s %s", r.Method, r.URL)
		}
		fmt.Fprint(w, `{"login":"server-owner"}`)
	}))
	defer server.Close()
	_, err := factoryPublisherWith(t.TempDir(), func(token string) factorygithub.API { return forge.NewClient(token).WithBaseURL(server.URL) })(context.Background(), factory.Item{Repo: "acme/api", Assignee: "assignee"})
	if err == nil || !strings.Contains(err.Error(), "@assignee's GitHub connection") {
		t.Fatalf("wrong identity = %v", err)
	}
}
