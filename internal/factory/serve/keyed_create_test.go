package serve_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/pipeline"
)

// issueGitHub is GitHub's issue endpoint behind a real TLS connection, so the
// factory's own HTTP client, not the fake, is what knows whether the issue
// request went out. down makes every dial fail: nothing reaches GitHub.
type issueGitHub struct {
	srv  *httptest.Server
	down atomic.Bool

	mu     sync.Mutex
	answer string // "" answers 201; "lost", "5xx" and "refuse" are failures
	issues int    // issues GitHub made
}

func newIssueGitHub(t *testing.T) *issueGitHub {
	t.Helper()
	g := &issueGitHub{}
	g.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if r.Method != http.MethodPost || r.URL.Path != "/repos/acme/api/issues" {
			t.Errorf("unexpected GitHub request %s %s", r.Method, r.URL)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		g.mu.Lock()
		answer := g.answer
		if answer == "refuse" {
			g.mu.Unlock()
			// GitHub's own refusal: the issue was not made.
			w.WriteHeader(http.StatusGone)
			_, _ = w.Write([]byte(`{"message":"Issues are disabled for this repo"}`))
			return
		}
		g.issues++
		n := g.issues + 10
		g.mu.Unlock()
		switch answer {
		case "lost":
			// The issue is made and the answer never arrives.
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close()
			}
		case "5xx":
			w.WriteHeader(http.StatusBadGateway)
		default:
			w.WriteHeader(http.StatusCreated)
			_, _ = fmt.Fprintf(w, `{"number":%d,"title":"Fix it","html_url":"https://github.com/acme/api/issues/%d"}`, n, n)
		}
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *issueGitHub) set(answer string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.answer = answer
}

func (g *issueGitHub) made() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.issues
}

// client reaches the fake for https://api.github.com through a real transport.
func (g *issueGitHub) client() *http.Client {
	tr := g.srv.Client().Transport.(*http.Transport).Clone()
	tr.TLSClientConfig.ServerName = "example.com"
	addr := g.srv.Listener.Addr().String()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		if g.down.Load() {
			return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connection refused")}
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	return &http.Client{Transport: tr}
}

// keyedCreate is the factory as the engine reaches it for a keyed create: one
// solo host that opens issues through g and makes jobs in its own store.
type keyedCreate struct {
	*v2Jobs
	github   *issueGitHub
	news     atomic.Int64
	launches atomic.Int64
	launch   func(int) error
	pipeline func() (pipeline.File, error)
}

func newKeyedCreate(t *testing.T) *keyedCreate {
	t.Helper()
	k := &keyedCreate{v2Jobs: newV2Jobs(t), github: newIssueGitHub(t)}
	k.cfg.Mode = api.FactorySolo
	k.cfg.GitHubToken = func(context.Context) string { return "private-token" }
	k.cfg.GitHubHTTPClient = k.github.client()
	k.pipeline = func() (pipeline.File, error) {
		p := pipeline.Defaults()
		p.DefaultBudget = 4
		p.Pipeline[factory.Kind("repair")] = []string{"write", "proof"}
		return p, nil
	}
	k.cfg.Pipeline = func(context.Context, string, string) (pipeline.File, error) { return k.pipeline() }
	k.seam.RecipeAt = func(string) (factory.Recipe, []factory.Problem, string, error) {
		p, err := k.pipeline()
		return p.Recipe(), nil, "", err
	}
	k.seam.New = func(repo, words string) (int, error) {
		k.news.Add(1)
		it, err := k.st.Create(factory.Item{State: factory.StateNew, Repo: repo, Title: "Fix it", Kind: factory.KindIssue, Origin: factory.OriginTerminal})
		return it.ID, err
	}
	launch := k.seam.Launch
	k.launch = launch
	k.seam.Launch = func(id int) error {
		k.launches.Add(1)
		return k.launch(id)
	}
	reopenJobs(t, k.v2Jobs)
	return k
}

func (k *keyedCreate) jobs(t *testing.T) int {
	t.Helper()
	items, err := k.st.List()
	if err != nil {
		t.Fatal(err)
	}
	return len(items)
}

func phaseOf(w *httptest.ResponseRecorder) string { return w.Header().Get(api.PhaseHeader) }

// A KEYED CREATE ANSWERED AFTER ITS FIRST WRITE IS REPLAYED, WHATEVER ITS STATUS.
// The engine calls a 5xx or a lost answer uncertain and the desk resends the
// same key: the resend must get the first answer back and make nothing new,
// neither a second GitHub issue nor a second job.
func TestAKeyedCreateAnsweredAfterItsWriteIsReplayed(t *testing.T) {
	for _, c := range []struct {
		name, path, body string
		arm              func(*keyedCreate)
		heal             func(*keyedCreate)
		status           int
		issues, jobs     int
		launches         int64
	}{
		{name: "GitHub made the issue and its answer was lost", path: "/v1/issues", body: `{"repo":"acme/api","title":"Fix it","text":"brief","source":"app"}`,
			arm: func(k *keyedCreate) { k.github.set("lost") }, heal: func(k *keyedCreate) { k.github.set("") },
			status: http.StatusBadGateway, issues: 1},
		{name: "GitHub answered 5xx to the issue request", path: "/v1/issues", body: `{"repo":"acme/api","title":"Fix it","text":"brief","source":"app"}`,
			arm: func(k *keyedCreate) { k.github.set("5xx") }, heal: func(k *keyedCreate) { k.github.set("") },
			status: http.StatusBadGateway, issues: 1},
		{name: "the job was made and its start failed", path: "/v1/jobs", body: `{"repo":"acme/api","title":"Fix it","text":"brief","source":"app","start":"now"}`,
			arm: func(k *keyedCreate) {
				launch := k.launch
				k.launch = func(id int) error {
					k.launch = launch
					return errors.New("the runner pool is unavailable")
				}
			},
			heal:   func(*keyedCreate) {},
			status: http.StatusInternalServerError, jobs: 1, launches: 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			k := newKeyedCreate(t)
			c.arm(k)
			first := keyedRequest(k.h, c.path, c.body, "workstream:5:ws_ab:3:d_1")
			checkJSON(t, first, c.status)
			if phaseOf(first) != "" {
				t.Fatalf("an answer after a write claims nothing was written: %s", first.Body)
			}
			c.heal(k)
			second := keyedRequest(k.h, c.path, c.body, "workstream:5:ws_ab:3:d_1")
			if second.Code != first.Code || second.Body.String() != first.Body.String() || phaseOf(second) != "" {
				t.Fatalf("resend = %d %s (phase %q), want the first answer %d %s replayed", second.Code, second.Body, phaseOf(second), first.Code, first.Body)
			}
			if got := k.github.made(); got != c.issues {
				t.Fatalf("GitHub issues made = %d, want %d", got, c.issues)
			}
			if got := k.jobs(t); got != c.jobs {
				t.Fatalf("jobs = %d, want %d", got, c.jobs)
			}
			if got := k.launches.Load(); got != c.launches {
				t.Fatalf("starts = %d, want %d", got, c.launches)
			}
		})
	}
}

// A KEYED CREATE REFUSED OR FAILED BEFORE ANYTHING WAS WRITTEN RUNS AGAIN. Its
// answer says so (api.PhaseHeader) and is not kept, so once the cause passes or
// is fixed the same key makes the job, and that success is what replays.
func TestAKeyedCreateThatWroteNothingRunsAgain(t *testing.T) {
	issue := `{"repo":"acme/api","title":"Fix it","text":"brief","source":"app"}`
	for _, c := range []struct {
		name, path, body string
		arm, heal        func(*keyedCreate)
		status           int
		words            string
		issues           int
	}{
		{name: "GitHub was unreachable before the issue request was sent", path: "/v1/issues", body: issue,
			arm: func(k *keyedCreate) { k.github.down.Store(true) }, heal: func(k *keyedCreate) { k.github.down.Store(false) },
			status: http.StatusBadGateway, words: "GitHub isn't answering", issues: 1},
		{name: "GitHub refused the issue", path: "/v1/issues", body: issue,
			arm: func(k *keyedCreate) { k.github.set("refuse") }, heal: func(k *keyedCreate) { k.github.set("") },
			status: http.StatusBadGateway, words: "GitHub refused this job change (HTTP 410)", issues: 1},
		{name: "the pipeline read failed before the job", path: "/v1/jobs", body: `{"repo":"acme/api","title":"Fix it","text":"brief","source":"app","pipeline":"repair"}`,
			arm: func(k *keyedCreate) {
				read := k.pipeline
				k.pipeline = func() (pipeline.File, error) {
					k.pipeline = read
					return pipeline.File{}, errors.New("the factory's store is busy")
				}
			},
			heal: func(*keyedCreate) {}, status: http.StatusInternalServerError, words: "store is busy"},
		{name: "a refusal the person fixed", path: "/v1/issues", body: `{"repo":"acme/api","title":"Fix it","text":"brief","source":"app","pipeline":"triage"}`,
			arm: func(*keyedCreate) {},
			heal: func(k *keyedCreate) {
				read := k.pipeline
				k.pipeline = func() (pipeline.File, error) {
					p, err := read()
					p.Pipeline[factory.Kind("triage")] = []string{"write"}
					return p, err
				}
			},
			status: http.StatusUnprocessableEntity, words: "pipeline not found", issues: 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			k := newKeyedCreate(t)
			c.arm(k)
			first := keyedRequest(k.h, c.path, c.body, "workstream:5:ws_ab:3:d_1")
			checkJSON(t, first, c.status)
			if !strings.Contains(first.Body.String(), c.words) {
				t.Fatalf("first = %d %s, want %q", first.Code, first.Body, c.words)
			}
			if k.github.made() != 0 || k.jobs(t) != 0 {
				t.Fatalf("the failure made something: issues=%d jobs=%d", k.github.made(), k.jobs(t))
			}
			c.heal(k)
			second := keyedRequest(k.h, c.path, c.body, "workstream:5:ws_ab:3:d_1")
			if second.Code != http.StatusCreated {
				t.Fatalf("resend after the cause passed = %d %s, want the job made", second.Code, second.Body)
			}
			checkJSON(t, second, http.StatusCreated)
			if phaseOf(first) != api.PhasePreMutation {
				t.Fatalf("first = %d %s carries phase %q, want it marked as answered before any change", first.Code, first.Body, phaseOf(first))
			}
			if phaseOf(second) != "" {
				t.Fatalf("a made job claims nothing was written: %v", second.Header())
			}
			var made api.JobCreated
			if err := json.Unmarshal(second.Body.Bytes(), &made); err != nil || made.ID == 0 {
				t.Fatalf("created %s: %v", second.Body, err)
			}
			third := keyedRequest(k.h, c.path, c.body, "workstream:5:ws_ab:3:d_1")
			if third.Code != http.StatusCreated || third.Body.String() != second.Body.String() || phaseOf(third) != "" {
				t.Fatalf("a resend after success = %d %s (phase %q), want the success replayed", third.Code, third.Body, phaseOf(third))
			}
			if k.github.made() != c.issues || k.jobs(t) != 1 || k.news.Load() != 1 {
				t.Fatalf("issues=%d jobs=%d news=%d, want issues=%d and one job", k.github.made(), k.jobs(t), k.news.Load(), c.issues)
			}
		})
	}
}

// An unkeyed create carries the same marker: present on a refusal before any
// write, gone once the job exists.
func TestAnUnkeyedCreateSaysWhetherItWroteAnything(t *testing.T) {
	k := newKeyedCreate(t)
	refused := verbRequest(k.h, "POST", "/v1/jobs", `{"repo":"acme/api"}`)
	checkJSON(t, refused, http.StatusUnprocessableEntity)
	if phaseOf(refused) != api.PhasePreMutation {
		t.Fatalf("refusal phase %q", phaseOf(refused))
	}
	made := verbRequest(k.h, "POST", "/v1/jobs", `{"repo":"acme/api","title":"Fix it"}`)
	checkJSON(t, made, http.StatusCreated)
	if phaseOf(made) != "" {
		t.Fatalf("made job phase %q", phaseOf(made))
	}
}
