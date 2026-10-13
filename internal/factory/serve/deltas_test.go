package serve_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/live"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
)

func TestAppFactoryRequiresLaunchTokenEvenOnLoopback(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	f.cfg.ServedBy, f.cfg.AccessToken = "app", "launch-secret"
	f.cfg.Mode = ""
	f.h = serve.New(f.seam, f.cfg)
	for _, path := range []string{"/v1/queue", "/v1/about", "/v1/me", "/v1/events", "/access", "/v1/runners"} {
		for _, secret := range []string{"", "wrong", "launch-secret"} {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			r := httptest.NewRequest("GET", path, nil).WithContext(ctx)
			r.RemoteAddr = "127.0.0.1:9000"
			if secret != "" {
				r.Header.Set("Authorization", "Bearer "+secret)
			}
			w := httptest.NewRecorder()
			f.h.ServeHTTP(w, r)
			if secret != "launch-secret" && w.Code != 401 {
				t.Fatalf("%s token %q: %d %s", path, secret, w.Code, w.Body)
			}
			if secret == "launch-secret" && path != "/access" && w.Code != 200 {
				t.Fatalf("%s: %d %s", path, w.Code, w.Body)
			}
			if secret == "launch-secret" && path == "/v1/about" && (!strings.Contains(w.Body.String(), `"served_by":"app"`) || !strings.Contains(w.Body.String(), `"access":"token"`)) {
				t.Fatal(w.Body)
			}
			if secret == "launch-secret" && path == "/v1/me" && (!strings.Contains(w.Body.String(), `"operator":true`) || !strings.Contains(w.Body.String(), `"mode":"solo"`)) {
				t.Fatal(w.Body)
			}
		}
	}
	// An absent launch secret must fail closed rather than trusting the socket.
	f.h.Close()
	f.cfg.AccessToken = ""
	f.h = serve.New(f.seam, f.cfg)
	w := verbRequest(f.h, "GET", "/v1/queue", "")
	checkJSON(t, w, 401)
}

func handoffFactory(t *testing.T) *v2Jobs {
	t.Helper()
	f := newV2Jobs(t)
	f.h.Close()
	f.seam.New = func(repo, words string) (int, error) {
		it, err := f.st.Create(factory.Item{Repo: repo, Title: words, State: factory.StateNew, Origin: factory.OriginTerminal})
		return it.ID, err
	}
	f.h = serve.New(f.seam, f.cfg)
	return f
}

func TestFactoryHandoffPersistsLinkAssigneeStartAndIdempotency(t *testing.T) {
	for _, start := range []string{"backlog", "now", "queued"} {
		t.Run(start, func(t *testing.T) {
			f := handoffFactory(t)
			body := fmt.Sprintf(`{"repo":"acme/api","title":"Fix it","text":"Keep the brief.","source":"hand-off","from_ref":{"kind":"agent","id":"a_123","title":"Source chat"},"assignee":"@Mei","start":%q}`, start)
			for i := 0; i < 2; i++ {
				r := httptest.NewRequest("POST", "/v1/jobs", strings.NewReader(body))
				r.RemoteAddr = "127.0.0.1:9000"
				r.Header.Set("Idempotency-Key", "handoff-1")
				w := httptest.NewRecorder()
				f.h.ServeHTTP(w, r)
				checkJSON(t, w, 201)
			}
			items, err := f.st.List()
			if err != nil || len(items) != 1 {
				t.Fatalf("duplicate job: %+v %v", items, err)
			}
			it := items[0]
			j := f.read(t, it)
			if it.Person() != "mei" || it.Title != "Fix it" || j.Ask == nil || j.Ask.Source != "hand-off" || j.Ask.Text != "Keep the brief." || j.Ask.FromRef == nil || j.Ask.FromRef.ID != "a_123" || j.Ask.By != "sam" {
				t.Fatalf("hand-off: %+v %+v", it, j.Ask)
			}
			back := api.ToItem(j)
			if back.Ask == nil || back.Ask.FromRef == nil || *back.Ask.FromRef != *it.Ask.FromRef {
				t.Fatalf("link lost: %+v", back.Ask)
			}
			if start == "backlog" && j.State != "backlog" || start != "backlog" && (j.State != "queued" || j.Budget != 4) || it.Marked != (start == "now") {
				t.Fatalf("start %s: %+v", start, j)
			}
		})
	}
}

func TestFactoryHandoffValidatesBeforeCreating(t *testing.T) {
	bodies := []string{
		`{"source":"hand-off"}`,
		`{"source":"hand-off","from_ref":{"kind":"task","id":"t1","title":"Task"}}`,
		`{"source":"hand-off","from_ref":{"kind":"agent","id":" ","title":"Chat"}}`,
		`{"source":"hand-off","from_ref":{"kind":"agent","id":"a1","title":" "}}`,
		`{"source":"unknown"}`,
		`{"from_ref":{"kind":"agent","id":"a1","title":"Chat"}}`,
		`{"start":"later"}`,
		`{"start":"now","autostart":true}`,
		`{"start":"now","assignee":""}`,
	}
	for _, fields := range bodies {
		f := handoffFactory(t)
		body := `{"repo":"acme/api","title":"Fix it",` + fields[1:]
		w := verbRequest(f.h, "POST", "/v1/jobs", body)
		checkJSON(t, w, 422)
		items, _ := f.st.List()
		if len(items) != 0 {
			t.Fatalf("invalid request created job: %s", body)
		}
	}
	f := handoffFactory(t)
	w := verbRequest(f.h, "POST", "/v1/jobs", `{"repo":"acme/api","title":"Fix it","assignee":"","start":"backlog"}`)
	checkJSON(t, w, 201)
	items, _ := f.st.List()
	if items[0].Person() != "" {
		t.Fatal("unassigned backlog acquired a person")
	}
}

func TestFactoryAssignmentOnlyAssigneeAndGitHubFailureKeepsOwner(t *testing.T) {
	f := newV2Jobs(t)
	f.h.Close()
	var writes []string
	f.cfg.ChangeAssignee = func(_ context.Context, _ factory.Item, who string) error {
		writes = append(writes, who)
		if who == "bad" {
			return errors.New("GitHub refused")
		}
		return nil
	}
	f.h = serve.New(f.seam, f.cfg)
	it := f.create(t, factory.Item{Assignee: "sam", State: factory.StateNew})
	f.post(t, it, "assign_to_me", "", 409)
	f.post(t, it, "change_assignee", `{"assignee":"bad"}`, 500)
	if f.read(t, it).Assignee != "sam" {
		t.Fatal("GitHub failure changed owner")
	}
	f.person = "mei"
	f.post(t, it, "change_assignee", `{"assignee":"mei"}`, 403)
	f.post(t, it, "unassign", "", 403)
	if len(writes) != 1 {
		t.Fatal("non-assignee wrote GitHub")
	}
	f.person = "sam"
	f.post(t, it, "change_assignee", `{"assignee":"mei"}`, 200)
	f.person = "mei"
	f.post(t, it, "unassign", "", 200)
	if strings.Join(writes, ",") != "bad,mei," {
		t.Fatalf("GitHub writes %+v", writes)
	}
	j := f.read(t, it)
	if j.Can.ChangeAssignee || j.Can.Unassign || !j.Can.AssignToMe {
		t.Fatalf("unassigned verbs %+v", j.Can)
	}
}

func TestFactoryDismissedQueryIsPersonalCaseInsensitiveAndPriorityIsSticky(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{Assignee: "sam", State: factory.StateNew, Labels: []string{"bug"}})
	f.post(t, it, "dismiss", "", 200)
	f.post(t, it, "priority", `{"priority":"high"}`, 200)
	f.post(t, it, "priority", `{"priority":"highest"}`, 422)
	read := func(q string) api.Queue {
		w := verbRequest(f.h, "GET", "/v1/queue?q="+q, "")
		checkJSON(t, w, 200)
		var got api.Queue
		if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		return got
	}
	if len(read("").Jobs) != 0 || len(read("IS:DISMISSED").Jobs) != 1 {
		t.Fatal("dismissed filter")
	}
	f.person = "mei"
	if len(read("is:dismissed").Jobs) != 0 || len(read("").Jobs) != 1 {
		t.Fatal("dismiss crossed people")
	}
	if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.Labels = []string{"priority:urgent"}; return nil }); err != nil {
		t.Fatal(err)
	}
	j := f.read(t, it)
	if j.Priority.Value != "high" || j.Priority.SetBy != "sam" {
		t.Fatal(j.Priority)
	}
	f.post(t, it, "nudge", `{"words":"Please look"}`, 200)
	events, _, err := live.Read(f.st.Root())
	if err != nil {
		t.Fatal(err)
	}
	seen := false
	for _, e := range events {
		if e.Kind == "activity" && strings.Contains(string(e.Data), "Please look") {
			seen = true
		}
	}
	if !seen {
		t.Fatal("Nudge has no typed Activity event")
	}
}

func TestFactoryUndoBelongsToDeciderAndExpiresAtFiveSeconds(t *testing.T) {
	f := newV2Jobs(t)
	it := f.create(t, factory.Item{State: factory.StateNeedsYou, Assignee: "sam", QKind: factory.QKindApprove, Question: "Approve?"})
	f.post(t, it, "approve", "", 200)
	f.person = "mei"
	f.post(t, it, "undo", "", 409)
	f.person = "sam"
	f.clock.Add(int64(5*time.Second - time.Nanosecond))
	f.post(t, it, "undo", "", 200)
	f.post(t, it, "approve", "", 200)
	f.clock.Add(int64(5 * time.Second))
	f.post(t, it, "undo", "", 409)
}

func TestFactoryNeedsYouKindsReachQueueAndTypedSSE(t *testing.T) {
	f := newV2Jobs(t)
	cases := []struct {
		kind, reason string
		item         factory.Item
	}{
		{"approve_start", "approval_start", factory.Item{State: factory.StateNeedsYou, QKind: factory.QKindAskFirst}},
		{"budget", "budget", factory.Item{State: factory.StateNeedsYou, QKind: "cap"}},
		{"out_of_rounds", "out_of_rounds", factory.Item{State: factory.StateNeedsYou, QKind: factory.QKindOutOfAttempts}},
		{"failed", "failed", factory.Item{State: factory.StateNew, Failure: &factory.Failure{Code: factory.CodeToolError, At: now}}},
		{"runner_offline", "runner_offline", factory.Item{State: factory.StateQueued, Failure: &factory.Failure{Code: factory.CodeMachineLost, At: now}}},
		{"changes_requested", "changes_requested", factory.Item{State: factory.StateShipped, PullRequest: &factory.PullRequest{State: factory.PROpen, ReviewDecision: "changes_requested"}}},
		{"conflict", "conflict", factory.Item{State: factory.StateShipped, PullRequest: &factory.PullRequest{State: factory.PROpen, Conflict: true}}},
	}
	for _, tc := range cases {
		it := tc.item
		it.Assignee = "sam"
		it = f.create(t, it)
		j := f.read(t, it)
		if j.NeedsYou == nil || j.NeedsYou.Kind != tc.kind || j.Reason != tc.reason || j.State != "needs_you" || j.NeedsYou.WhoCanAnswer != "job_person" {
			t.Fatalf("%s: %+v", tc.kind, j)
		}
	}
	w := verbRequest(f.h, "GET", "/v1/queue", "")
	checkJSON(t, w, 200)
	var q api.Queue
	if err := json.Unmarshal(w.Body.Bytes(), &q); err != nil {
		t.Fatal(err)
	}
	if len(q.NeedsYou) != len(cases) {
		t.Fatalf("groups %+v", q.NeedsYou)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/v1/events", nil).WithContext(ctx)
	r.RemoteAddr = "127.0.0.1:9000"
	r.Header.Set("Last-Event-ID", "expired")
	w = httptest.NewRecorder()
	f.h.ServeHTTP(w, r)
	checkStatus := w.Code == 200 && strings.HasPrefix(w.Header().Get("Content-Type"), "text/event-stream")
	if !checkStatus {
		t.Fatalf("SSE %d %s", w.Code, w.Body)
	}
	for _, tc := range cases {
		if !strings.Contains(w.Body.String(), `"kind":"`+tc.kind+`"`) || !strings.Contains(w.Body.String(), `"reason":"`+tc.reason+`"`) {
			t.Fatalf("SSE missing %s: %s", tc.kind, w.Body)
		}
	}
	if !strings.Contains(w.Body.String(), "event: job.state\n") {
		t.Fatal("no typed state event")
	}
}

func TestFactoryHandoffRequiresStoreAndLaunchBeforeCreation(t *testing.T) {
	for _, missing := range []string{"store", "launch"} {
		f := handoffFactory(t)
		f.h.Close()
		if missing == "store" {
			f.cfg.Store = nil
		} else {
			f.seam.Launch = nil
		}
		f.h = serve.New(f.seam, f.cfg)
		body := `{"repo":"acme/api","title":"Fix it","source":"hand-off","from_ref":{"kind":"agent","id":"a1","title":"Chat"},"start":"now"}`
		w := verbRequest(f.h, "POST", "/v1/jobs", body)
		checkJSON(t, w, 501)
		items, _ := f.st.List()
		if len(items) != 0 {
			t.Fatalf("%s created unusable hand-off", missing)
		}
	}
}
