package serve_test

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Agent-Field/codeaf/internal/factory"
	"github.com/Agent-Field/codeaf/internal/factory/api"
	"github.com/Agent-Field/codeaf/internal/factory/lease"
	"github.com/Agent-Field/codeaf/internal/factory/record"
	"github.com/Agent-Field/codeaf/internal/factory/serve"
	"github.com/Agent-Field/codeaf/internal/factory/store"
)

func TestAnsweredQuestionKeepsItsPersonWordsClockAndEvent(t *testing.T) {
	for _, verb := range []string{"answer", "approve"} {
		t.Run(verb, func(t *testing.T) {
			cfg := recordConfig(t)
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			cfg.Store = st
			cfg.LeaseOptions = lease.Options{Now: func() time.Time { return now }}
			job, err := st.Create(factory.Item{State: factory.StateNeedsYou, Question: "continue?", QKind: factory.QKindApprove})
			if err != nil {
				t.Fatal(err)
			}
			seam := factory.Seam{
				Load:   func() (factory.Snapshot, error) { items, err := st.List(); return factory.Snapshot{Items: items}, err },
				GitHub: func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "Sam"}, nil },
				Answer: func(id int, yes bool, words string) error {
					return st.Update(id, func(it *factory.Item) error {
						it.Question, it.QKind = "", ""
						it.State = factory.StateRunning
						return nil
					})
				},
			}
			h := serve.New(seam, cfg)
			body := `{"yes":true,"words":"Keep the flag."}`
			if verb == "approve" {
				body = ""
			}
			w := verbRequest(h, "POST", fmt.Sprintf("/v1/jobs/%d/%s", job.ID, verb), body)
			checkJSON(t, w, 200)
			w = verbRequest(h, "GET", fmt.Sprintf("/v1/jobs/%d", job.ID), "")
			var got api.Job
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if len(got.Questions) != 1 || got.Questions[0].State != "answered" {
				t.Fatalf("question disappeared: %+v", got.Questions)
			}
			a := got.Questions[0].Answer
			words := "Keep the flag."
			if verb == "approve" {
				words = "yes"
			}
			if a == nil || a.By != "sam" || a.Text != words || !a.At.Equal(now) || a.Event == "" {
				t.Fatalf("answer receipt lost: %+v", a)
			}
			events, err := cfg.Record.Since(context.Background(), time.Time{})
			if err != nil || len(events) != 1 || events[0].ID != a.Event {
				t.Fatalf("answer event: %+v %v", events, err)
			}
		})
	}
}

func recordConfig(t *testing.T) serve.Config {
	t.Helper()
	st, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	rec, err := record.Open(st, record.Options{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	cfg := config
	cfg.Record = rec
	return cfg
}

func TestRecordedVerbsAndAuditExport(t *testing.T) {
	cfg := recordConfig(t)
	s, _ := spyVerbs(t, nil)
	s.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "octocat"}, nil }
	h := serve.New(s, cfg)
	for _, tc := range []struct{ path, body, kind string }{
		{"/v1/jobs/2/run", "", record.Started},
		{"/v1/jobs/1/answer", `{"yes":false,"words":"revise, please"}`, record.Answered},
		{"/v1/jobs/9/approve", "", record.Approved},
		{"/v1/jobs/9/request_changes", `{"words":"show restart"}`, record.RequestChanges},
		{"/v1/jobs/2/drop", "", record.Dropped},
		{"/v1/jobs", `{"repo":"owner/repo","title":"fix","autostart":true}`, record.AutoStarted},
	} {
		w := verbRequest(h, "POST", tc.path, tc.body)
		if w.Code != 200 && w.Code != 201 {
			t.Fatalf("%s: %d %s", tc.path, w.Code, w.Body)
		}
		events, err := cfg.Record.Since(context.Background(), time.Time{})
		if err != nil || events[len(events)-1].Kind != tc.kind || events[len(events)-1].Handle != "octocat" {
			t.Fatalf("events=%+v error=%v", events, err)
		}
	}
	w := verbRequest(h, "GET", "/v1/audit", "")
	checkJSON(t, w, 200)
	var audit struct{ Events []record.Event }
	if err := json.Unmarshal(w.Body.Bytes(), &audit); err != nil || len(audit.Events) != 6 {
		t.Fatalf("audit=%s error=%v", w.Body, err)
	}
	w = verbRequest(h, "GET", "/v1/audit?since="+now.Format(time.RFC3339Nano), "")
	checkJSON(t, w, 200)
	if !strings.Contains(w.Body.String(), `"events":[]`) {
		t.Fatal(w.Body)
	}
	w = verbRequest(h, "GET", "/v1/audit?format=csv", "")
	rows, err := csv.NewReader(w.Body).ReadAll()
	if w.Code != 200 || err != nil || len(rows) != 7 || rows[2][6] != "revise, please" || !strings.Contains(w.Header().Get("Content-Disposition"), "factory-record.csv") {
		t.Fatalf("export=%v error=%v headers=%v", rows, err, w.Header())
	}
	for _, path := range []string{"/v1/audit?since=no", "/v1/audit?format=xml"} {
		checkJSON(t, verbRequest(h, "GET", path, ""), 400)
	}
}

func TestDurableApprovalBeforeSignOffAndRecordFailureRefusesConsent(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "disk failure"}[fail], func(t *testing.T) {
			st, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			rec, err := record.Open(st, record.Options{})
			if err != nil {
				t.Fatal(err)
			}
			cfg := config
			cfg.Record = rec
			if fail {
				if err := os.Mkdir(filepath.Join(st.Root(), "events.jsonl"), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			s := factory.FixtureSeam(now)
			s.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "octocat"}, nil }
			called := false
			s.SignOff = func(id int, edited bool) (bool, error) {
				called = true
				reopened, err := record.Open(st, record.Options{})
				if err != nil {
					t.Fatal(err)
				}
				events, err := reopened.Since(context.Background(), time.Time{})
				if err != nil || len(events) != 1 || events[0].Job != id || events[0].Handle != "octocat" || events[0].Kind != record.Approved {
					t.Fatalf("consent has no durable event: %+v %v", events, err)
				}
				return false, nil
			}
			w := verbRequest(serve.New(s, cfg), "POST", "/v1/jobs/9/approve", "")
			status := 200
			if fail {
				status = 500
			}
			checkJSON(t, w, status)
			if called == fail {
				t.Fatalf("consent called=%t disk failed=%t", called, fail)
			}
		})
	}
}

func TestApprovalWithoutRecordRefusesConsent(t *testing.T) {
	s, calls := spyVerbs(t, nil)
	w := verbRequest(serve.New(s, config), "POST", "/v1/jobs/9/approve", "")
	checkJSON(t, w, 500)
	if len(*calls) != 0 {
		t.Fatal("approval without record reached consent")
	}
}

func TestAnswerYesAtApproveRecordsBeforeWakingRunner(t *testing.T) {
	cfg := recordConfig(t)
	s := factory.FixtureSeam(now)
	s.GitHub = func(context.Context) (factory.GitHubLink, error) { return factory.GitHubLink{Login: "octocat"}, nil }
	s.Answer = func(id int, yes bool, words string) error {
		events, err := cfg.Record.Since(context.Background(), time.Time{})
		if err != nil || len(events) != 1 || events[0].Kind != record.Approved || events[0].Job != id || !yes {
			t.Fatalf("answer has no consent: %+v %v", events, err)
		}
		return nil
	}
	w := verbRequest(serve.New(s, cfg), "POST", "/v1/jobs/1/answer", `{"yes":true}`)
	checkJSON(t, w, 200)
}
