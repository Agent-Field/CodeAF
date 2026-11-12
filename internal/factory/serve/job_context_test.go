package serve_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/record"
)

func contextBody(t *testing.T, version int, attachments []factory.JobContext) string {
	t.Helper()
	raw, err := json.Marshal(api.JobContextRequest{Context: attachments, Expect: &api.Expect{Version: &version}})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// E7'/E9/E13: Backlog and queued jobs replace and clear context outside text;
// ordinary reads carry metadata, while worker admission retains complete markdown.
func TestJobContextReplaceClearAndRead(t *testing.T) {
	for _, state := range []factory.State{factory.StateNew, factory.StateQueued} {
		t.Run(string(state), func(t *testing.T) {
			f := newCreateExtrasJobs(t)
			text, markdown := strings.Repeat("d", 20000), strings.Repeat("é", 60000)
			it := createExtrasJob(t, f, "/v1/jobs", api.NewJob{Repo: "acme/api", Title: "Fix", Text: text, Source: factory.AskApp})
			if err := f.st.Update(it.ID, func(cur *factory.Item) error { cur.State = state; return nil }); err != nil {
				t.Fatal(err)
			}
			attachments := []factory.JobContext{{Title: " reference ", Source: "guide", Markdown: markdown}}
			before := f.read(t, it)
			f.post(t, it, "context", contextBody(t, before.Version, attachments), http.StatusOK)
			stored, err := f.st.Get(it.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.Body != text || stored.Ask.Text != text || len(stored.Context) != 1 || stored.Context[0].Title != "reference" || stored.Context[0].Markdown != markdown {
				t.Fatal("context changed description or lost text")
			}
			for _, path := range []string{fmt.Sprintf("/v1/jobs/%d", it.ID), "/v1/queue"} {
				w := verbRequest(f.h, http.MethodGet, path, "")
				checkJSON(t, w, http.StatusOK)
				if strings.Contains(w.Body.String(), "markdown") || !strings.Contains(w.Body.String(), `"context":[{"title":"reference","source":"guide"}]`) {
					t.Fatalf("metadata-only read: %s", path)
				}
			}
			raw, err := json.Marshal(api.WorkerJob(stored))
			if err != nil {
				t.Fatal(err)
			}
			var worker api.Job
			if err := json.Unmarshal(raw, &worker); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(api.ToItem(worker).Context, stored.Context) {
				t.Fatal("worker lost attachment text")
			}
			f.post(t, it, "context", contextBody(t, before.Version, []factory.JobContext{}), http.StatusConflict)
			current := f.read(t, it)
			f.post(t, it, "context", contextBody(t, current.Version, []factory.JobContext{{Title: "replacement", Markdown: "new body"}}), http.StatusOK)
			current = f.read(t, it)
			if len(current.Context) != 1 || current.Context[0].Title != "replacement" {
				t.Fatal("replacement appended context")
			}
			f.post(t, it, "context", contextBody(t, current.Version, []factory.JobContext{}), http.StatusOK)
			if current = f.read(t, it); len(current.Context) != 0 {
				t.Fatal("empty list did not clear context")
			}
			events, err := f.rec.Since(context.Background(), time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, e := range events {
				if e.Job == it.ID && e.Kind == record.ContextSet {
					count++
					if e.Handle != "sam" || e.Verb != "set context" || strings.Contains(e.Detail, markdown) {
						t.Fatalf("context activity: %+v", e)
					}
				}
			}
			if count != 3 {
				t.Fatalf("context activity count %d; want 3", count)
			}
		})
	}
}

// E8'/E13: refusals leave attachments, revision and activity unchanged.
func TestJobContextRefusalsHaveNoEffects(t *testing.T) {
	cases := []struct {
		name   string
		state  factory.State
		stream bool
		person string
		body   string
		status int
		words  string
	}{
		{"started", factory.StateRunning, true, "sam", "", 409, "has started"},
		{"returned to backlog", factory.StateNew, true, "sam", "", 409, "has started"},
		{"queued after run", factory.StateQueued, true, "sam", "", 409, "has started"},
		{"other person", factory.StateNew, false, "alex", "", 403, ""},
		{"finished", factory.StateShipped, false, "sam", "", 409, "Backlog"},
		{"stale", factory.StateNew, false, "sam", `{"expect":{"version":999},"context":[]}`, 409, "stale"},
		{"missing expect", factory.StateNew, false, "sam", `{"context":[]}`, 422, "expect.version"},
		{"missing context", factory.StateNew, false, "sam", `{"expect":{"version":1}}`, 422, "context"},
		{"null context", factory.StateNew, false, "sam", `{"expect":{"version":1},"context":null}`, 422, "context"},
		{"title", factory.StateNew, false, "sam", `{"expect":{"version":1},"context":[{"title":" ","markdown":"body"}]}`, 422, "title"},
		{"markdown", factory.StateNew, false, "sam", `{"expect":{"version":1},"context":[{"title":"t","markdown":""}]}`, 422, "markdown"},
		{"blank markdown", factory.StateNew, false, "sam", `{"expect":{"version":1},"context":[{"title":"t","markdown":" \n\t"}]}`, 422, "markdown"},
		{"oversized", factory.StateNew, false, "sam", `{"expect":{"version":1},"context":[{"title":"t","markdown":"` + strings.Repeat("x", 100001) + `"}]}`, 422, "100000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newV2Jobs(t)
			it := factory.Item{State: tc.state, Assignee: tc.person, Context: []factory.JobContext{{Title: "original", Markdown: "keep me"}}}
			if tc.stream {
				it.Stream = recordedRun()
			}
			it = f.create(t, it)
			before := f.read(t, it)
			body := tc.body
			if body == "" {
				body = contextBody(t, before.Version, []factory.JobContext{})
			}
			w := f.post(t, it, "context", body, tc.status)
			if !strings.Contains(w.Body.String(), tc.words) {
				t.Fatalf("refusal: %s", w.Body)
			}
			if got := f.read(t, it); got.Version != before.Version || !reflect.DeepEqual(got.Context, before.Context) {
				t.Fatal("refused edit changed job")
			}
			events, err := f.rec.Since(context.Background(), time.Time{})
			if err != nil || len(events) != 0 {
				t.Fatalf("refused edit activity: %d %v", len(events), err)
			}
		})
	}
}

// E14: a route that exists answers its ordinary JSON job-not-found refusal.
func TestJobContextMissingJob(t *testing.T) {
	f := newV2Jobs(t)
	w := verbRequest(f.h, http.MethodPost, "/v1/jobs/999/context", `{"expect":{"version":1},"context":[]}`)
	checkJSON(t, w, http.StatusNotFound)
	if !strings.Contains(w.Body.String(), `"error":"job not found"`) {
		t.Fatalf("missing job: %s", w.Body)
	}
}

// E7'/E13: editing steps before the first run does not make context immutable.
func TestJobContextAfterBacklogStepEdit(t *testing.T) {
	f := newV2Jobs(t)
	f.seam.Edit = factory.LocalSeam(f.st, now).Edit
	reopenJobs(t, f)
	it := f.create(t, factory.Item{State: factory.StateNew, Assignee: "sam", Stages: []factory.Stage{
		{Name: "write", Kind: factory.StageChat, Ask: "old ask", On: true},
	}})
	f.post(t, it, "step-edits", `{"ask":{"write":"new ask"},"why":"clarify scope"}`, http.StatusOK)
	before := f.read(t, it)
	f.post(t, it, "context", contextBody(t, before.Version, []factory.JobContext{{Title: "reference", Markdown: "attached body"}}), http.StatusOK)
	if got := f.read(t, it); len(got.Context) != 1 {
		t.Fatal("pre-run edit prevented context attachment")
	}
}

// E13: a job brought back has no run, so its next run reads whatever context
// it is given now, whether or not it ran before the drop.
func TestJobContextAfterRestore(t *testing.T) {
	for _, ran := range []bool{false, true} {
		t.Run(fmt.Sprintf("ran=%v", ran), func(t *testing.T) {
			f := newV2Jobs(t)
			it := factory.Item{State: factory.StateDismissed, Assignee: "sam", Context: []factory.JobContext{{Title: "reference", Markdown: "original body"}}}
			if ran {
				it.Stream = &factory.Stream{Started: now, Ended: now, Phases: []factory.Phase{{Name: "plan", Kind: factory.StageChat, State: factory.PhaseDone}}}
			}
			it = f.create(t, it)
			f.post(t, it, "restore", "{}", http.StatusOK)
			before := f.read(t, it)
			f.post(t, it, "context", contextBody(t, before.Version, []factory.JobContext{}), http.StatusOK)
			if after := f.read(t, it); len(after.Context) != 0 {
				t.Fatalf("brought-back job kept its old context: %+v", after.Context)
			}
		})
	}
}
